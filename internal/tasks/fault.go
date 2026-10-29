package tasks

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/extbridge"
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
	faultOffline
	faultUncertain
)

// errExtOffline 表示指定的扩展配置文件这会儿没连上。有别的账号就换，没有就按普通错误重试。
var errExtOffline = errors.New("扩展账号未连接")

var (
	quotaPattern = regexp.MustCompile(`额度不足|额度已用尽|额度已用|额度用尽|次数已用完|次数已用尽|免费次数已|已达上限|超出.{0,8}限额|生成次数已|quota exceeded|daily limit`)
	ratePattern  = regexp.MustCompile(`过于频繁|操作频繁|请求过于频繁|请稍后再试|访问太频繁|稍后重试|rate limit|too many requests`)
	authPattern  = regexp.MustCompile(`未登录|请先登录|请登录|登录已失效|登录失效|登录状态失效`)
)

func classifyFault(err error) faultKind {
	if err == nil {
		return faultNone
	}
	if errors.Is(err, strategy.ErrSubmissionUncertain) || extbridge.CodeOf(err) == extbridge.ESendUncertain {
		return faultUncertain
	}
	if errors.Is(err, errHumanGaveUp) || errors.Is(err, captcha.ErrVerificationRequired) || errors.Is(err, errNeedsVerify) || extbridge.CodeOf(err) == extbridge.ECaptchaHuman {
		return faultCaptcha
	}
	text := strings.ToLower(err.Error())
	if errors.Is(err, strategy.ErrAccountQuota) || extbridge.CodeOf(err) == extbridge.EQuota || quotaPattern.MatchString(text) {
		return faultQuota
	}
	if errors.Is(err, strategy.ErrAccountRate) || extbridge.CodeOf(err) == extbridge.ERateLimit || ratePattern.MatchString(text) {
		return faultRate
	}
	if errors.Is(err, errExtOffline) {
		return faultOffline
	}
	if errors.Is(err, errQianwenLogin) || errors.Is(err, errDoubaoLogin) || extbridge.CodeOf(err) == extbridge.ENotLoggedIn || authPattern.MatchString(err.Error()) {
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
	case faultOffline:
		return "扩展未连接"
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
	case faultOffline:
		until = now.Add(45 * time.Second).Unix()
	default:
		return
	}
	msg := faultLabel(kind)
	if err != nil && err.Error() != "" {
		msg = err.Error()
	}
	e.store.UpdateBrowserHealth(id, health, until, msg)
}

func nextLocalMidnight(now time.Time) time.Time {
	y, m, d := now.Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, now.Location())
}
