package agy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

const (
	// Window5h 和 WindowWeek 是 Gemini 额度桶的窗口名。
	Window5h   = "5h"
	WindowWeek = "weekly"
	// ReasonReading 是还没读到额度时的提示。
	ReasonReading = "正在读取额度…"
	// ReasonLogin 是还没登录、读不到额度时的提示。
	ReasonLogin = "需登录后才能读取额度"
)

// UsageWindow 是一个额度窗口。Remaining 是剩余比例，1 表示没用过。
type UsageWindow struct {
	Remaining float64
	Reset     time.Time
}

// GeminiUsage 只保留 Gemini 模型组的 5 小时和 7 天窗口。
type GeminiUsage struct {
	FiveHour UsageWindow
	Weekly   UsageWindow
}

// UsageState 是界面上这一格额度的来源。
type UsageState int

const (
	UsageReading UsageState = iota
	UsageOK
	UsageNeedLogin
	UsageFailed
)

// UsageView 是某个账号此刻能显示的额度。失败原因已经是给界面的短句。
type UsageView struct {
	State  UsageState
	Reason string
	Usage  GeminiUsage
	At     time.Time
}

type usageBucket struct {
	Window    string   `json:"window"`
	Remaining *float64 `json:"remaining_fraction"`
	Reset     string   `json:"reset_time"`
}

// ParseUsage 从 agy /usage 的 JSON 里取出 Gemini 的两个窗口。
// 输出里出现登录提示时按需登录处理，不把正文当额度。
func ParseUsage(data []byte) (GeminiUsage, error) {
	if bytes.Contains(data, []byte("Authentication required")) {
		return GeminiUsage{}, ErrNeedLogin
	}
	raw := extractJSON(data)
	var doc struct {
		Command struct {
			Data struct {
				Groups []struct {
					Name    string        `json:"name"`
					Buckets []usageBucket `json:"buckets"`
				} `json:"groups"`
			} `json:"data"`
		} `json:"command"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return GeminiUsage{}, errUsageData
	}
	var group []usageBucket
	for _, g := range doc.Command.Data.Groups {
		if g.Name == "Gemini Models" {
			group = g.Buckets
			break
		}
	}
	if len(group) == 0 {
		return GeminiUsage{}, errUsageGemini
	}
	var usage GeminiUsage
	var saw5h, sawWeek bool
	for _, bucket := range group {
		switch bucket.Window {
		case Window5h:
			if saw5h {
				continue
			}
			w, err := windowFrom(bucket)
			if err != nil {
				return GeminiUsage{}, err
			}
			usage.FiveHour = w
			saw5h = true
		case WindowWeek:
			if sawWeek {
				continue
			}
			w, err := windowFrom(bucket)
			if err != nil {
				return GeminiUsage{}, err
			}
			usage.Weekly = w
			sawWeek = true
		}
	}
	if !saw5h || !sawWeek {
		return GeminiUsage{}, errUsageGemini
	}
	return usage, nil
}

func windowFrom(bucket usageBucket) (UsageWindow, error) {
	if bucket.Remaining == nil || strings.TrimSpace(bucket.Reset) == "" {
		return UsageWindow{}, errUsageData
	}
	reset, err := time.Parse(time.RFC3339, strings.TrimSpace(bucket.Reset))
	if err != nil {
		return UsageWindow{}, errUsageData
	}
	return UsageWindow{Remaining: *bucket.Remaining, Reset: reset.UTC()}, nil
}

func extractJSON(data []byte) []byte {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] == '{' {
		return data
	}
	i := bytes.IndexByte(data, '{')
	j := bytes.LastIndexByte(data, '}')
	if i >= 0 && j > i {
		return data[i : j+1]
	}
	return data
}

// UsedFraction 是已用比例，1 减剩余比例，并收在 0 到 1。
func UsedFraction(remaining float64) float64 {
	if remaining < 0 {
		remaining = 0
	}
	if remaining > 1 {
		remaining = 1
	}
	return 1 - remaining
}

// UsedPercent 是已用百分比，四舍五入到整数。
func UsedPercent(remaining float64) int {
	pct := int(math.Round(UsedFraction(remaining) * 100))
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// WindowTip 是进度条的悬停文字。时刻用 now 的时区，也就是本地时间。
func WindowTip(window string, remaining float64, reset, now time.Time) string {
	loc := time.Local
	if !now.IsZero() {
		loc = now.Location()
	}
	at := reset.In(loc)
	pct := UsedPercent(remaining)
	if window == Window5h {
		return fmt.Sprintf("5小时：已用 %d%%，%s 重置", pct, at.Format("15:04"))
	}
	return fmt.Sprintf("7天：已用 %d%%，%s 重置", pct, at.Format("01-02 15:04"))
}

// QuotaHold 在 5 小时或 7 天剩余为 0 时给出恢复时刻。
// 两个窗口都空时，要等到较晚的那个重置，账号才又能用。
func QuotaHold(usage GeminiUsage) (time.Time, bool) {
	var until time.Time
	hold := false
	consider := func(w UsageWindow) {
		if w.Remaining > 0 {
			return
		}
		hold = true
		if w.Reset.IsZero() {
			return
		}
		if until.IsZero() || w.Reset.After(until) {
			until = w.Reset
		}
	}
	consider(usage.FiveHour)
	consider(usage.Weekly)
	return until, hold
}

// ViewFrom 把一次读取收成界面状态。取消不算失败。
func ViewFrom(usage GeminiUsage, err error, now time.Time) (UsageView, bool) {
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, errUsageCanceled)) {
		return UsageView{}, false
	}
	if errors.Is(err, ErrNeedLogin) {
		return UsageView{State: UsageNeedLogin, Reason: ReasonLogin, At: now}, true
	}
	if err != nil {
		return UsageView{State: UsageFailed, Reason: FailReason(err), At: now}, true
	}
	return UsageView{State: UsageOK, Usage: usage, At: now}, true
}

// FailReason 是失败提示。只留短句，不带命令输出。
func FailReason(err error) string {
	msg := "未知错误"
	if err != nil {
		msg = strings.TrimSpace(strings.ReplaceAll(err.Error(), "\n", " "))
	}
	if msg == "" {
		msg = "未知错误"
	}
	if strings.HasPrefix(msg, "读取额度失败") {
		return clipRunes(msg, 48)
	}
	return clipRunes("读取额度失败："+msg, 48)
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
