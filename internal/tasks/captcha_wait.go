package tasks

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/notify"
)

// errHumanGaveUp 是等了 5 分钟还没人完成验证。任务改派，这个账号标成需验证。
var errHumanGaveUp = errors.New("需要人工验证")

// humanVerifyLimit 是等人完成验证的上限。这段时间不算出图超时。
var humanVerifyLimit = 5 * time.Minute

// 测试替换这些函数。出图代码不为此另做接口。
var (
	captchaVisible = captcha.Visible
	captchaManual  = stopSolving
	captchaShow    = func(s *cdp.Session) {
		if s != nil {
			s.PresentForCaptcha()
		}
	}
	captchaHide = func(s *cdp.Session) {
		if s != nil {
			s.KeepOffscreen()
		}
	}
	captchaAlert = notify.Send
)

func stopSolving(s *cdp.Session) {
	if s == nil {
		return
	}
	if s.IsEgo() {
		_ = captcha.SetManual(s)
		return
	}
	_, _ = s.Evaluate(`(window.__tulaiCaptcha && window.__tulaiCaptcha.setManual(true), true)`)
}

// holdIfCaptcha 在验证码还在时等人。held 为真表示刚刚等过，出图计时从现在重新算。
func (e *Engine) holdIfCaptcha(ctx context.Context, sess *cdp.Session, t *Task) (bool, error) {
	blocked, err := captchaVisible(sess)
	if err != nil || !blocked {
		return false, nil
	}
	if err := e.holdForHuman(ctx, sess, t); err != nil {
		return true, err
	}
	return true, nil
}

// holdForHuman 把窗口拿到前台，暂停出图计时，最多等 humanVerifyLimit。
// 验证通过后放开前台占用。超时留下窗口，方便人接着点。
func (e *Engine) holdForHuman(ctx context.Context, sess *cdp.Session, t *Task) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil && !endedByTimeout(ctx, nil) {
		return ctx.Err()
	}
	blocked, readErr := captchaVisible(sess)
	if readErr == nil && !blocked {
		return nil
	}
	captchaManual(sess)
	e.setAwaiting(t, true)
	// 单次出图和外层任务时限都从这里停表，验证结束再接着走。
	e.pauseBudget(budgetIDs(t)...)
	defer e.resumeBudget(budgetIDs(t)...)
	captchaShow(sess)
	name := ""
	if t != nil {
		name = e.accountName(t.ranOnOr(t.BrowserID))
		if name == "" {
			name = t.Platform
		}
	}
	if name == "" {
		name = "账号"
	}
	logging.Log.Warning("#%d %s 需要人工验证，请在弹出的浏览器窗口里完成", taskIndex(t), name)
	captchaAlert("需要人工验证", fmt.Sprintf("%s 出图遇到验证码，请在浏览器窗口里完成", name))
	passed := false
	defer func() {
		e.setAwaiting(t, false)
		if passed || (ctx.Err() != nil && !endedByTimeout(ctx, nil)) {
			captchaHide(sess)
		}
	}()

	waitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), humanVerifyLimit)
	defer cancel()
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	ignoreDeadline := false
	for {
		if !ignoreDeadline && ctx.Err() != nil && !endedByTimeout(ctx, nil) {
			return ctx.Err()
		}
		if sess != nil && sess.Ctx() != nil && sess.Ctx().Err() != nil {
			return sess.Ctx().Err()
		}
		blocked, readErr = captchaVisible(sess)
		if readErr == nil && !blocked {
			passed = true
			logging.Log.Info("#%d %s 验证已通过，继续出图", taskIndex(t), name)
			return nil
		}
		var user <-chan struct{}
		if !ignoreDeadline {
			user = ctx.Done()
		}
		select {
		case <-user:
			if endedByTimeout(ctx, nil) {
				ignoreDeadline = true
				continue
			}
			return ctx.Err()
		case <-waitCtx.Done():
			if ctx.Err() != nil && !endedByTimeout(ctx, nil) {
				return ctx.Err()
			}
			logging.Log.Warning("#%d %s 等待人工验证超过 %s，交给同平台其他账号", taskIndex(t), name, humanVerifyLimit)
			return errHumanGaveUp
		case <-tick.C:
		}
	}
}

func budgetIDs(t *Task) []int64 {
	if t == nil {
		return nil
	}
	return []int64{t.BrowserID, t.ranOn}
}

// pauseBudget 停住这个账号的出图时限。验证码还在时，单次尝试和外层任务都不该算超时。
func (e *Engine) pauseBudget(ids ...int64) {
	if e == nil {
		return
	}
	e.liveMu.Lock()
	defer e.liveMu.Unlock()
	if e.clock.holdStart == nil {
		e.clock.holdStart = map[int64]time.Time{}
	}
	now := time.Now()
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if _, ok := e.clock.holdStart[id]; !ok {
			e.clock.holdStart[id] = now
		}
	}
}

func (e *Engine) resumeBudget(ids ...int64) {
	if e == nil {
		return
	}
	e.liveMu.Lock()
	defer e.liveMu.Unlock()
	if e.clock.held == nil {
		e.clock.held = map[int64]time.Duration{}
	}
	now := time.Now()
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		if e.clock.holdStart == nil {
			continue
		}
		start, ok := e.clock.holdStart[id]
		if !ok {
			continue
		}
		delete(e.clock.holdStart, id)
		if !start.IsZero() && now.After(start) {
			e.clock.held[id] += now.Sub(start)
		}
	}
}

func (e *Engine) pausedFor(id int64, now time.Time) time.Duration {
	if e == nil || id <= 0 {
		return 0
	}
	e.liveMu.Lock()
	defer e.liveMu.Unlock()
	extra := time.Duration(0)
	if e.clock.held != nil {
		extra = e.clock.held[id]
	}
	if e.clock.holdStart != nil {
		if start, ok := e.clock.holdStart[id]; ok && !start.IsZero() && now.After(start) {
			extra += now.Sub(start)
		}
	}
	return extra
}

func taskIndex(t *Task) int {
	if t == nil {
		return 0
	}
	return t.Index
}

func (e *Engine) setAwaiting(t *Task, on bool) {
	if e == nil || t == nil {
		return
	}
	id := t.ranOnOr(t.BrowserID)
	e.liveMu.Lock()
	if e.clock.verify == nil {
		e.clock.verify = map[int64]bool{}
	}
	if on && id > 0 {
		e.clock.verify[id] = true
	} else {
		delete(e.clock.verify, id)
	}
	e.liveMu.Unlock()
	if e.view != nil && t.Index > 0 {
		shown := *t
		if on {
			shown.Status = StatusAwaitUser
			shown.Error = errHumanGaveUp.Error()
		} else if shown.Status == "" || shown.Status == StatusRunning || shown.Status == StatusAwaitUser {
			shown.Status = StatusRunning
			shown.Error = ""
		}
		e.view.Update(shown)
	}
	e.notify()
}
