package tasks

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

// 账号级失败。不确定的发送不在这里记成账号故障，避免把还能用的号冷却掉。
type faultKind int

const (
	faultNone faultKind = iota
	faultQuota
	faultRate
	faultAuth
	faultCaptcha
	faultUncertain
	faultReply
)

var (
	quotaPattern = regexp.MustCompile(`额度不足|额度已用尽|额度已用|额度用尽|额度用完|次数已用完|次数已用尽|次数用完|免费次数已|已达上限|已达到.{0,8}上限|超出.{0,8}限额|生成次数已|图片生成次数已达上限|暂时无法再生成图片|积分不足|免费额度已用完|今日对话次数已达上限|(?:今日|今天|当日|本周|本月|(?:最?近\s*)?\d+\s*天).{0,12}(?:额度|次数).{0,8}(?:用完|用尽|耗尽|达上限|已达上限)|quota exceeded|daily limit|not enough credits|insufficient credits|out of credits|daily free allowance|today['’]s chat limit has been reached|already use(?:d)? up .{0,24}credits|image generation limit|limit for creating images|reached your .{0,24}limit`)
	ratePattern  = regexp.MustCompile(`过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate limit|too many requests|sending too many requests|operation is too frequent|too many files uploaded|too many requests in the queue|chat limited|global limited|file upload limited`)
	authPattern  = regexp.MustCompile(`未登录|请先登录|请登录|登录已失效|登录失效|登录状态失效`)
)

func classifyFault(err error) faultKind {
	if err == nil {
		return faultNone
	}
	if errors.Is(err, strategy.ErrSubmissionUncertain) {
		return faultUncertain
	}
	if errors.Is(err, strategy.ErrPlatformReply) {
		return faultReply
	}
	if errors.Is(err, errHumanGaveUp) || errors.Is(err, captcha.ErrVerificationRequired) || errors.Is(err, errNeedsVerify) {
		return faultCaptcha
	}
	text := strings.ToLower(err.Error())
	if errors.Is(err, strategy.ErrAccountQuota) || quotaPattern.MatchString(text) {
		return faultQuota
	}
	if errors.Is(err, strategy.ErrAccountRate) || ratePattern.MatchString(text) {
		return faultRate
	}
	if authPattern.MatchString(err.Error()) {
		return faultAuth
	}
	return faultNone
}

func faultLabel(kind faultKind) string {
	switch kind {
	case faultQuota:
		return "额度已用尽"
	case faultRate:
		return "请求过于频繁"
	case faultAuth:
		return "未登录"
	case faultCaptcha:
		return "需要验证"
	case faultReply:
		return "平台回复异常"
	default:
		return "账号不可用"
	}
}

func (e *Engine) noteFault(id int64, kind faultKind, err error, now time.Time) {
	if e == nil || e.store == nil || id <= 0 {
		return
	}
	health := store.HealthCooling
	var until int64
	switch kind {
	case faultQuota:
		health = store.HealthExhausted
		until = nextLocalMidnight(now).Unix()
	case faultRate:
		until = now.Add(15 * time.Minute).Unix()
	case faultCaptcha:
		until = now.Add(10 * time.Minute).Unix()
	case faultAuth:
		health = store.HealthExhausted
		until = 0
	case faultReply:
		until = now.Add(replyCooldown).Unix()
	default:
		return
	}
	prev := findBrowser(e.store.Browsers(), id)
	msg := faultLabel(kind)
	if err != nil && err.Error() != "" {
		msg = err.Error()
	}
	e.store.UpdateBrowserHealth(id, health, until, msg)
	if prev != nil {
		if kind == faultQuota && (prev.Platform == "千问" || prev.Platform == "Gemini") {
			e.store.MarkBrowserCreditsExhausted(id, now)
		} else if (kind == faultQuota || kind == faultRate) && prev.Platform == "豆包" {
			e.store.MarkBrowserPlatformLimited(id, msg, until, now)
		}
	}
	if kind == faultQuota && (prev == nil || prev.Health != store.HealthExhausted || prev.CoolUntil != until) {
		logging.Log.Warning("账号[%s] 额度已用完，今日暂停使用（次日恢复）", browserName(e.store, id))
	}
	e.notify()
}

// replyCooldown 让连着回报错的账号歇一会儿，活先交给别的号。
const replyCooldown = 5 * time.Minute

// replyBackoff 是回报错后换新对话重试前的停顿。
const replyBackoff = 5 * time.Second

func nextLocalMidnight(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
}
