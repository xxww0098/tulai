package tasks

import (
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/store"
)

// NoteAgyQuota 在 5 小时或 7 天剩余为 0 时把账号标成额度不足。
// 恢复时间用对应窗口的 reset_time，轮换会一直跳过它，直到那个时刻。
// 登录挂起不改写。剩余还在时不动已有的冷却。
func NoteAgyQuota(st *store.Store, id int64, usage agy.GeminiUsage, now time.Time) {
	if st == nil || id <= 0 {
		return
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
