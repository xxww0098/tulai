package tasks

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/store"
)

// NoteAgyQuota 保存 Gemini 两个额度窗口，剩余为 0 时把账号标成额度不足。
// 恢复时间用对应窗口的 reset_time，轮换会一直跳过它，直到那个时刻。
// 登录挂起不改写。剩余还在时不动已有的冷却。
func NoteAgyQuota(st *store.Store, id int64, usage agy.GeminiUsage, now time.Time) {
	if st == nil || id <= 0 {
		return
	}
	view := agy.UsageView{State: agy.UsageOK, Usage: usage, At: now}
	if data, err := json.Marshal(view); err == nil {
		st.Set(agyUsageKey(id), string(data))
	}
	until, hold := agy.QuotaHold(usage)
	if !hold {
		return
	}
	for _, b := range st.Browsers() {
		if b.ID != id {
			continue
		}
		if strings.Contains(b.LastError, "登录") && !b.Usable(now) {
			return
		}
		break
	}
	cool := int64(0)
	if !until.IsZero() {
		cool = until.Unix()
	}
	st.UpdateBrowserHealth(id, store.HealthExhausted, cool, "额度不足")
}

func agyUsageKey(id int64) string { return "agy_usage:" + strconv.FormatInt(id, 10) }

// ClearAgyQuota invalidates the old identity's quota after a successful OAuth login.
func ClearAgyQuota(st *store.Store, id int64) {
	if st != nil && id > 0 {
		st.Set(agyUsageKey(id), "")
	}
}

// SavedAgyUsage is the last successful Gemini reading, shared by the UI and scheduler.
func SavedAgyUsage(st *store.Store, id int64) (agy.UsageView, bool) {
	var view agy.UsageView
	if st == nil || id <= 0 || json.Unmarshal([]byte(st.Get(agyUsageKey(id))), &view) != nil || view.State != agy.UsageOK || view.At.IsZero() {
		return view, false
	}
	for _, w := range []agy.UsageWindow{view.Usage.FiveHour, view.Usage.Weekly} {
		if math.IsNaN(w.Remaining) || math.IsInf(w.Remaining, 0) || w.Remaining < 0 || w.Remaining > 1 {
			return agy.UsageView{}, false
		}
	}
	return view, true
}

// Either window can exhaust an account, so rank by the smaller remaining fraction.
func agyRemaining(st *store.Store, id int64, now time.Time) float64 {
	view, ok := SavedAgyUsage(st, id)
	if !ok || !view.Usage.FiveHour.Reset.After(now) || !view.Usage.Weekly.Reset.After(now) {
		return -1
	}
	return min(view.Usage.FiveHour.Remaining, view.Usage.Weekly.Remaining)
}

func (e *Engine) hasAgyUsage(now time.Time) bool {
	for _, row := range e.accountsFor(agy.Platform) {
		if agyRemaining(e.store, row.ID, now) >= 0 {
			return true
		}
	}
	return false
}
