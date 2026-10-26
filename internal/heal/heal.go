// Package heal 决定账号和浏览器出了问题时下一步做什么。
// 它不打开浏览器，也不访问豆包或千问。调用方拿着 Plan 去重启、刷新或换号。
package heal

import (
	"errors"
	"time"
)

// 运行中能自愈的几类问题。额度和限流仍走原来的换号，不在这里。
const (
	KindNone Kind = iota
	KindCrash
	KindStuck
	KindNetwork
	KindVerify
)

// Kind 是一次失败该怎么处理。
type Kind int

// 自愈动作。
const (
	ActionNone Action = iota
	ActionRelaunch
	ActionReload
	ActionBackoff
	ActionTrySlider
	ActionSwitch
	ActionPause
	ActionFail
)

// Action 是状态机给出的下一步。
type Action int

// 这些错误让执行器不用再猜字符串。
var (
	ErrCrashed = errors.New("浏览器崩溃")
	ErrStuck   = errors.New("页面长时间没有进展")
	ErrNetwork = errors.New("网络中断")
)

// Input 是做决定时已经知道的事实。
type Input struct {
	Kind            Kind
	Attempt         int
	MaxAttempts     int
	SliderAvailable bool
	SliderTried     bool
	OtherHealthy    bool
	Submitted       bool
	Label           string
}

// Plan 是一步自愈。Log 写进运行日志，Reason 是这张图最终失败时的一行说明。
type Plan struct {
	Action Action
	Log    string
	Reason string
	Notice string
	Wait   time.Duration
	Focus  bool
}

// Decide 根据这一次失败和已经用掉的次数，决定重启、刷新、退避、拖滑块、换号或停住。
// 次数用完就停。滑块只试一次；过了就换健康账号，没有别的账号就停住等用户。
func Decide(in Input) Plan {
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
	case KindCrash:
		if in.Attempt < in.MaxAttempts {
			return Plan{Action: ActionRelaunch, Log: label + " 浏览器崩溃，已自动重启"}
		}
		reason := "浏览器崩溃了"
		if in.Attempt > 1 {
			reason = "浏览器崩溃，自动重启后仍失败"
		}
		return Plan{Action: ActionFail, Reason: reason, Log: label + " " + reason}
	case KindStuck:
		if in.Attempt < in.MaxAttempts {
			return Plan{Action: ActionReload, Log: label + " 页面卡住，已刷新重试"}
		}
		return Plan{Action: ActionFail, Reason: "页面长时间没有进展", Log: label + " 页面长时间没有进展"}
	case KindNetwork:
		if in.Attempt < in.MaxAttempts {
			return Plan{Action: ActionBackoff, Wait: Backoff(in.Attempt), Log: label + " 网络中断，稍后自动重试"}
		}
		return Plan{Action: ActionFail, Reason: "网络中断，请重试", Log: label + " 网络重试已用完"}
	case KindVerify:
		if in.SliderAvailable && !in.SliderTried {
			return Plan{Action: ActionTrySlider, Log: label + " 遇到滑块，正在自动拖动"}
		}
		notice := label + " 需要完成安全验证"
		if in.OtherHealthy && !in.Submitted {
			return Plan{
				Action: ActionSwitch,
				Notice: notice,
				Focus:  true,
				Log:    label + " 验证未通过，已换到其他账号",
				Reason: "需要在网页里完成验证",
			}
		}
		return Plan{
			Action: ActionPause,
			Notice: notice,
			Focus:  true,
			Log:    label + " 需要完成安全验证",
			Reason: "需要在网页里完成验证",
		}
	default:
		return Plan{}
	}
}

// Backoff 是网络错误的等待。第 1 次 2 秒，之后翻倍，最多 8 秒。
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	wait := time.Second << attempt
	if wait > 8*time.Second {
		return 8 * time.Second
	}
	return wait
}
