package tasks

import (
	"errors"
	"strings"
	"time"
)

// 运行中能自愈的几类问题。额度和限流仍走原来的换号，不在这里。
type healKind int

const (
	healKindNone healKind = iota
	healKindCrash
	healKindStuck
	healKindNetwork
	healKindVerify
)

// healAction 是自愈状态机给出的下一步。
type healAction int

const (
	healActionNone healAction = iota
	healActionRelaunch
	healActionReload
	healActionBackoff
	healActionTrySlider
	healActionSwitch
	healActionPause
	healActionFail
)

// 这些错误让执行器不用再猜字符串。
var (
	errHealCrashed = errors.New("浏览器崩溃")
	errHealStuck   = errors.New("页面长时间没有进展")
	errHealNetwork = errors.New("网络中断")
)

type healInput struct {
	Kind            healKind
	Attempt         int
	MaxAttempts     int
	SliderAvailable bool
	SliderTried     bool
	OtherHealthy    bool
	Submitted       bool
	Label           string
}

type healPlan struct {
	Action healAction
	Log    string
	Reason string
	Notice string
	Wait   time.Duration
	Focus  bool
}

// decideHeal 根据这一次失败和已经用掉的次数，决定重启、刷新、退避、拖滑块、换号或停住。
// 次数用完就停。滑块只试一次；过了就换健康账号，没有别的账号就停住等用户。
func decideHeal(in healInput) healPlan {
	if in.MaxAttempts < 1 {
		in.MaxAttempts = 1
	}
	if in.Attempt < 1 {
		in.Attempt = 1
	}
	label := in.Label
	if label == "" {
		label = "账号"
	}
	switch in.Kind {
	case healKindCrash:
		if in.Attempt < in.MaxAttempts {
			return healPlan{Action: healActionRelaunch, Log: label + " 浏览器崩溃，已自动重启"}
		}
		reason := "浏览器崩溃了"
		if in.Attempt > 1 {
			reason = "浏览器崩溃，自动重启后仍失败"
		}
		return healPlan{Action: healActionFail, Reason: reason, Log: label + " " + reason}
	case healKindStuck:
		if in.Attempt < in.MaxAttempts {
			return healPlan{Action: healActionReload, Log: label + " 页面卡住，已刷新重试"}
		}
		return healPlan{Action: healActionFail, Reason: "页面长时间没有进展", Log: label + " 页面长时间没有进展"}
	case healKindNetwork:
		if in.Attempt < in.MaxAttempts {
			return healPlan{Action: healActionBackoff, Wait: healBackoff(in.Attempt), Log: label + " 网络中断，稍后自动重试"}
		}
		return healPlan{Action: healActionFail, Reason: "网络中断，请重试", Log: label + " 网络重试已用完"}
	case healKindVerify:
		if in.SliderAvailable && !in.SliderTried {
			return healPlan{Action: healActionTrySlider, Log: label + " 遇到滑块，正在自动拖动"}
		}
		notice := label + " 需要完成安全验证"
		if in.OtherHealthy && !in.Submitted {
			return healPlan{
				Action: healActionSwitch,
				Notice: notice,
				Focus:  true,
				Log:    label + " 验证未通过，已换到其他账号",
				Reason: "需要在网页里完成验证",
			}
		}
		return healPlan{
			Action: healActionPause,
			Notice: notice,
			Focus:  true,
			Log:    label + " 需要完成安全验证",
			Reason: "需要在网页里完成验证",
		}
	default:
		return healPlan{}
	}
}

// healBackoff 是网络错误的等待。第 1 次 2 秒，之后翻倍，最多 8 秒。
func healBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	wait := time.Second << attempt
	if wait > 8*time.Second {
		return 8 * time.Second
	}
	return wait
}

// 登录探测结果。down 是浏览器没开或连不上，不当成退出登录。
const (
	loginStateIn        = "in"
	loginStateOut       = "out"
	loginStateChallenge = "challenge"
	loginStateRegion    = "region"
	loginStateUnknown   = "unknown"
	loginStateDown      = "down"
)

// 周期性检查和派发前检查的间隔。派发前更短，避免每一张图都重新连一次调试端口。
const (
	loginInterval     = 45 * time.Second
	loginMinGap       = 5 * time.Second
	loginRecheckAfter = 5 * time.Second
)

func loginProbeDue(last, now time.Time, beforeDispatch bool) bool {
	if last.IsZero() {
		return true
	}
	gap := loginInterval
	if beforeDispatch {
		gap = loginMinGap
	}
	return !now.Before(last.Add(gap))
}

type loginOutcome struct {
	Mark        string // logout、verify、restore、confirm，或空
	Notice      string
	Log         string
	Focus       bool
	ClearNotice bool
}

func normalizeLoginState(raw string, err error) string {
	if err != nil {
		return loginStateDown
	}
	switch strings.TrimSpace(raw) {
	case loginStateIn, loginStateOut, loginStateChallenge, loginStateRegion, loginStateUnknown, loginStateDown:
		return strings.TrimSpace(raw)
	default:
		return loginStateUnknown
	}
}

func observeLoginState(prev, next, label string) loginOutcome {
	if strings.TrimSpace(label) == "" {
		label = "账号"
	}
	switch next {
	case loginStateOut:
		if prev == loginStateOut {
			return loginOutcome{}
		}
		return loginOutcome{
			Mark:   "logout",
			Notice: label + " 需要重新扫码登录",
			Log:    label + " 已退出登录，请重新扫码",
			Focus:  true,
		}
	case loginStateChallenge:
		if prev == loginStateChallenge {
			return loginOutcome{}
		}
		return loginOutcome{
			Mark:   "verify",
			Notice: label + " 需要完成安全验证",
			Log:    label + " 遇到安全验证",
			Focus:  true,
		}
	case loginStateRegion:
		if prev == loginStateRegion {
			return loginOutcome{}
		}
		return loginOutcome{
			Mark:   "region",
			Notice: label + " 当前地区无法访问，请到设置的「网络」里填写代理",
			Log:    label + " 遇到地区限制，请设置代理",
		}
	case loginStateIn:
		if prev == loginStateOut || prev == loginStateChallenge || prev == loginStateRegion {
			return loginOutcome{
				Mark:        "restore",
				Log:         label + " 已恢复，回到轮换",
				ClearNotice: true,
			}
		}
		if prev == "" {
			return loginOutcome{Mark: "confirm"}
		}
	}
	return loginOutcome{}
}
