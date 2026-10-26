package heal

import (
	"strings"
	"time"
)

// 登录探测结果。down 是浏览器没开或连不上，不当成退出登录。
const (
	StateIn        = "in"
	StateOut       = "out"
	StateChallenge = "challenge"
	StateUnknown   = "unknown"
	StateDown      = "down"
)

// 周期性检查和派发前检查的间隔。派发前更短，避免每一张图都重新连一次调试端口。
const (
	LoginInterval = 45 * time.Second
	LoginMinGap   = 5 * time.Second
	// RecheckAfter 是账号因登录或验证被挂起、又没有别的账号时，调度器过多久再看一眼。
	RecheckAfter = 5 * time.Second
)

// Due 报告这个账号现在该不该探测。
// beforeDispatch 为真时用较短的间隔，普通巡检用 LoginInterval。
func Due(last, now time.Time, beforeDispatch bool) bool {
	if last.IsZero() {
		return true
	}
	gap := LoginInterval
	if beforeDispatch {
		gap = LoginMinGap
	}
	return !now.Before(last.Add(gap))
}

// LoginOutcome 是一次探测相对上次结果的变化。
type LoginOutcome struct {
	Mark        string // logout、verify、restore、confirm，或空
	Notice      string
	Log         string
	Focus       bool
	ClearNotice bool
}

// NormalizeState 把探测返回收成已知的五种。出错一律当成 down，避免把网络故障记成退出登录。
func NormalizeState(raw string, err error) string {
	if err != nil {
		return StateDown
	}
	switch strings.TrimSpace(raw) {
	case StateIn, StateOut, StateChallenge, StateUnknown, StateDown:
		return strings.TrimSpace(raw)
	default:
		return StateUnknown
	}
}

// Observe 比较上次和这次的登录状态。
// 同一种异常不重复弹提示。从退出或验证回到已登录时，标记可以回到轮换。
func Observe(prev, next, label string) LoginOutcome {
	if strings.TrimSpace(label) == "" {
		label = "账号"
	}
	switch next {
	case StateOut:
		if prev == StateOut {
			return LoginOutcome{}
		}
		return LoginOutcome{
			Mark:   "logout",
			Notice: label + " 需要重新扫码登录",
			Log:    label + " 已退出登录，请重新扫码",
			Focus:  true,
		}
	case StateChallenge:
		if prev == StateChallenge {
			return LoginOutcome{}
		}
		return LoginOutcome{
			Mark:   "verify",
			Notice: label + " 需要完成安全验证",
			Log:    label + " 遇到安全验证",
			Focus:  true,
		}
	case StateIn:
		if prev == StateOut || prev == StateChallenge {
			return LoginOutcome{
				Mark:        "restore",
				Log:         label + " 已恢复，回到轮换",
				ClearNotice: true,
			}
		}
		if prev == "" {
			return LoginOutcome{Mark: "confirm"}
		}
	}
	return LoginOutcome{}
}
