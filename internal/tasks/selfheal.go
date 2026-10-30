package tasks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/heal"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

// errNeedsVerify 是页面上的滑块或安全验证。标签页留着，方便自动拖动或让用户处理。
var errNeedsVerify = errors.New("需要安全验证")

const (
	autoNoteCap = 8
	stallAfter  = 45 * time.Second
)

var (
	netRe   = regexp.MustCompile(`(?i)connection refused|connection reset|broken pipe|i/o timeout|network is unreachable|no such host|temporary failure|网络中断|网络暂时`)
	crashRe = regexp.MustCompile(`(?i)websocket: close|target closed|cdp 已断开|连接已断开|浏览器崩溃|browser has been closed|session closed`)
	stuckRe = regexp.MustCompile(`页面长时间没有进展|页面卡住|无进展`)
)

func runtimeKind(err error) heal.Kind {
	if err == nil {
		return heal.KindNone
	}
	if errors.Is(err, heal.ErrNetwork) || netRe.MatchString(err.Error()) {
		return heal.KindNetwork
	}
	if errors.Is(err, heal.ErrCrashed) || crashRe.MatchString(err.Error()) {
		return heal.KindCrash
	}
	if errors.Is(err, heal.ErrStuck) || stuckRe.MatchString(err.Error()) {
		return heal.KindStuck
	}
	if errors.Is(err, captcha.ErrVerificationRequired) || errors.Is(err, errNeedsVerify) || extbridge.CodeOf(err) == extbridge.ECaptchaHuman || strings.Contains(err.Error(), "安全验证") {
		return heal.KindVerify
	}
	return heal.KindNone
}

// SetLoginProbe 注入登录探测。为空时不巡检，测试也不访问网页。
func (e *Engine) SetLoginProbe(fn func(context.Context, store.Browser) (string, error)) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.loginProbe = fn
	e.mu.Unlock()
}

// SetLoginFocus 注入把浏览器窗口拉到前面的动作。
func (e *Engine) SetLoginFocus(fn func(context.Context, store.Browser) error) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.loginFocus = fn
	e.mu.Unlock()
}

// SetSlider 注入千问滑块处理。为空时遇到验证就换号或停住，不连调试端口。
func (e *Engine) SetSlider(fn func(context.Context, store.Browser) error) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.sliderFn = fn
	e.mu.Unlock()
}

// SetHealNotify 在提示或运行日志变化时刷新界面。
func (e *Engine) SetHealNotify(fn func()) {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.onHeal = fn
	e.mu.Unlock()
}

// Attention 是工作台上的一条提示。text 已经是中文，按钮由界面写成「去处理」。
func (e *Engine) Attention() (text string, id int64, ok bool) {
	if e == nil {
		return "", 0, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.attention == "" {
		return "", 0, false
	}
	return e.attention, e.attentionID, true
}

// RaiseAttention 给界面测试和登录探测放上一条提示。
func (e *Engine) RaiseAttention(text string, id int64) {
	if e == nil {
		return
	}
	e.setAttention(text, id)
	e.notify()
}

// AutoNotes 是最近几条自动处理，给「运行日志」用。
func (e *Engine) AutoNotes() []string {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]string, len(e.notes))
	copy(out, e.notes)
	return out
}

// RecordAuto 记下一条自动处理，并写进运行日志。
func (e *Engine) RecordAuto(text string) {
	if e == nil {
		return
	}
	e.noteAuto(text)
}

// WatchLogins 按间隔巡检每个本机账号。扩展账号仍在任务里发现未登录。
func (e *Engine) WatchLogins(ctx context.Context) {
	if e == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.scanLogins(ctx)
		}
	}
}

func (e *Engine) scanLogins(ctx context.Context) {
	if e == nil || e.store == nil {
		return
	}
	for _, b := range e.store.Browsers() {
		if ctx.Err() != nil {
			return
		}
		e.refreshLogin(ctx, b.ID, false)
	}
}

// settleAccount 在派发前探测当前账号。探测把它标成不可用时，改派到下一个并再探一次。
func (e *Engine) settleAccount(ctx context.Context, task *Task) (wait time.Time, dead bool) {
	e.syncAgyAccounts()
	seen := map[int64]bool{}
	for n := 0; n < 8; n++ {
		if task.BrowserID > 0 && !seen[task.BrowserID] {
			seen[task.BrowserID] = true
			e.refreshLogin(ctx, task.BrowserID, true)
		}
		wait, dead = e.prepareAccount(task, time.Now())
		if dead || task.BrowserID <= 0 || seen[task.BrowserID] {
			return wait, dead
		}
	}
	return wait, dead
}

func (e *Engine) refreshLogin(ctx context.Context, id int64, before bool) {
	if e == nil || e.store == nil || id <= 0 {
		return
	}
	e.mu.Lock()
	probe := e.loginProbe
	last := e.loginLast[id]
	e.mu.Unlock()
	if probe == nil {
		return
	}
	now := time.Now()
	if !heal.Due(last, now, before) {
		return
	}
	var row store.Browser
	found := false
	for _, b := range e.store.Browsers() {
		if b.ID == id {
			row, found = b, true
			break
		}
	}
	if !found || row.ExtAccount != "" || row.Platform == agy.Platform {
		return
	}
	if e.accountBusy(id) {
		return
	}
	e.mu.Lock()
	if e.loginLast == nil {
		e.loginLast = map[int64]time.Time{}
	}
	e.loginLast[id] = now
	e.mu.Unlock()
	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	state, err := probe(pctx, row)
	cancel()
	e.applyLogin(row, heal.NormalizeState(state, err))
}

func (e *Engine) applyLogin(b store.Browser, state string) {
	if e == nil || e.store == nil || b.ID <= 0 {
		return
	}
	if state == heal.StateOut && e.accountBusy(b.ID) {
		logging.Event(logging.Line{
			Stage: "probe", Platform: b.Platform, Account: accountLabel(b), Port: b.Port,
			Msg: "生成中的探测结果是退出登录，没有正面证据，已忽略", Error: "busy",
		})
		return
	}
	e.mu.Lock()
	if e.loginPrev == nil {
		e.loginPrev = map[int64]string{}
	}
	prev := e.loginPrev[b.ID]
	if state == heal.StateIn || state == heal.StateOut || state == heal.StateChallenge || state == heal.StateRegion {
		e.loginPrev[b.ID] = state
	}
	e.mu.Unlock()
	// 先按库里的当前行确认。上次探测已经是 in 时，Observe 不会再发事件，
	// 但任务可能把这一行写成了 exhausted / 未登录。
	repaired := false
	if state == heal.StateIn {
		repaired = e.store.ConfirmLogin(b.ID)
	}
	out := heal.Observe(prev, state, accountLabel(b))
	if out.Mark == "" && out.Log == "" && out.Notice == "" && !repaired {
		return
	}
	switch out.Mark {
	case "logout":
		e.store.UpdateBrowser(b.ID, b.IsRunning, false)
		e.store.UpdateBrowserHealth(b.ID, store.HealthExhausted, 0, "未登录")
	case "verify":
		// coolUntil 为 0 时不会按时间自动放行。探测回到 in 才恢复。
		e.store.UpdateBrowserHealth(b.ID, store.HealthCooling, 0, "需要安全验证")
	case "restore":
		e.store.ResetBrowserHealth(b.ID)
		e.store.UpdateBrowser(b.ID, true, true)
	case "confirm":
		e.store.ConfirmLogin(b.ID)
	}
	if out.Log != "" {
		e.noteAuto(out.Log)
	}
	if out.Notice != "" {
		e.setAttention(out.Notice, b.ID)
	}
	if out.ClearNotice {
		e.clearAttention(b.ID)
	}
	if out.Focus {
		e.focusBrowser(b)
	}
	if out.Mark == "restore" || repaired {
		e.poke()
	}
	e.notify()
}

// announceAccount 在任务里发现退出登录时补上工作台提示。
// 探测已经提示过的账号不会再弹一次。
func (e *Engine) announceAccount(id int64, kind faultKind) {
	if kind != faultAuth || e == nil {
		return
	}
	row, _ := e.browserByID(id)
	if row.ID <= 0 {
		row.ID = id
	}
	e.applyLogin(row, heal.StateOut)
}

func heldForUser(b store.Browser) bool {
	return strings.Contains(b.LastError, "登录") || strings.Contains(b.LastError, "验证")
}

// accountsNeedUser 是验证码或登录把号按住了。这种不要干等冷却，也不要取消别的任务。
func accountsNeedUser(rows []store.Browser, now time.Time, cap int) bool {
	saw := false
	for _, b := range rows {
		if openAccount(b, now, cap) {
			return false
		}
		if b.CoolUntil <= 0 || !now.Before(time.Unix(b.CoolUntil, 0)) {
			continue
		}
		if !heldForUser(b) {
			return false
		}
		saw = true
	}
	return saw
}

func (e *Engine) accountWaitingOnUser(rows []store.Browser, now time.Time) bool {
	e.mu.Lock()
	probe := e.loginProbe
	e.mu.Unlock()
	if probe == nil {
		return false
	}
	for _, b := range rows {
		if b.Usable(now) || b.CoolUntil > 0 {
			continue
		}
		if heldForUser(b) {
			return true
		}
	}
	return false
}

// FocusAccount 把这个账号的浏览器拉到前面。
func (e *Engine) FocusAccount(ctx context.Context, b store.Browser) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	fn := e.loginFocus
	e.mu.Unlock()
	if fn == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return fn(ctx, b)
}

func (e *Engine) focusBrowser(b store.Browser) {
	e.mu.Lock()
	fn := e.loginFocus
	e.mu.Unlock()
	if fn == nil || b.ID <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	_ = fn(ctx, b)
}

func (e *Engine) setAttention(text string, id int64) {
	e.mu.Lock()
	e.attention = text
	e.attentionID = id
	e.mu.Unlock()
}

func (e *Engine) clearAttention(id int64) {
	e.mu.Lock()
	if id == 0 || e.attentionID == id {
		e.attention = ""
		e.attentionID = 0
	}
	e.mu.Unlock()
}

func (e *Engine) noteAuto(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	e.mu.Lock()
	e.notes = append(e.notes, text)
	if len(e.notes) > autoNoteCap {
		e.notes = append([]string(nil), e.notes[len(e.notes)-autoNoteCap:]...)
	}
	e.mu.Unlock()
	logging.Log.Info("%s", text)
}

func (e *Engine) notify() {
	e.mu.Lock()
	fn := e.onHeal
	e.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (e *Engine) poke() {
	e.mu.Lock()
	chs := make([]chan struct{}, 0, len(e.wakes)+1)
	if e.wake != nil {
		chs = append(chs, e.wake)
	}
	chs = append(chs, e.wakes...)
	e.mu.Unlock()
	for _, ch := range chs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (e *Engine) browserByID(id int64) (store.Browser, bool) {
	if e == nil || e.store == nil || id <= 0 {
		return store.Browser{ID: id}, id > 0
	}
	for _, b := range e.store.Browsers() {
		if b.ID == id {
			return b, true
		}
	}
	return store.Browser{ID: id, Name: "账号"}, false
}

func accountLabel(b store.Browser) string {
	if name := strings.TrimSpace(b.Name); name != "" {
		return name
	}
	if b.ID > 0 {
		return fmt.Sprintf("账号%d", b.ID)
	}
	return "账号"
}

// healAttempt 处理崩溃、卡住、网络和验证。again 为真时继续同一张图。
// finished 为真时任务已经有最终状态。
func (e *Engine) healAttempt(ctx context.Context, t *Task, err error, sliderTried *bool) (again bool, finished bool) {
	kind := runtimeKind(err)
	if kind == heal.KindNone {
		return false, false
	}
	id := t.ranOnOr(t.BrowserID)
	row, _ := e.browserByID(id)
	label := accountLabel(row)
	e.mu.Lock()
	sliderFn := e.sliderFn
	hook := e.onRecover
	e.mu.Unlock()
	sliderOK := sliderFn != nil && t.Platform == "千问"
	for {
		plan := heal.Decide(heal.Input{
			Kind:            kind,
			Attempt:         t.Attempts,
			MaxAttempts:     1 + e.opts.RetryCount,
			SliderAvailable: sliderOK,
			SliderTried:     sliderTried != nil && *sliderTried,
			OtherHealthy:    e.hasOtherHealthy(t.Platform, id, time.Now()),
			Submitted:       t.submitted,
			Label:           label,
		})
		if hook != nil && plan.Action != heal.ActionTrySlider {
			hook(plan.Action, id)
		}
		switch plan.Action {
		case heal.ActionTrySlider:
			if sliderTried != nil {
				*sliderTried = true
			}
			e.noteAuto(plan.Log)
			var serr error
			if sliderFn != nil {
				serr = sliderFn(ctx, row)
			} else {
				serr = errors.New("没有滑块处理")
			}
			if serr == nil {
				if t.Attempts > 0 {
					t.Attempts--
				}
				e.noteAuto(label + " 滑块已通过，继续出图")
				return true, false
			}
			continue
		case heal.ActionRelaunch, heal.ActionReload, heal.ActionBackoff:
			e.noteAuto(plan.Log)
			e.recoverBrowser(id, plan.Action == heal.ActionRelaunch)
			if plan.Wait > 0 {
				select {
				case <-time.After(plan.Wait):
				case <-ctx.Done():
					t.Status = StatusSkipped
					t.Error = "已停止"
					return false, true
				}
			}
			return true, false
		case heal.ActionSwitch, heal.ActionPause:
			e.finishVerify(t, err, plan, row)
			return false, true
		case heal.ActionFail:
			e.noteAuto(plan.Log)
			t.Status = StatusFailed
			t.Error = plan.Reason
			return false, true
		default:
			return false, false
		}
	}
}

func (e *Engine) finishVerify(t *Task, err error, plan heal.Plan, row store.Browser) {
	if plan.Log != "" {
		e.noteAuto(plan.Log)
	}
	if plan.Notice != "" {
		e.setAttention(plan.Notice, row.ID)
	}
	if plan.Focus {
		e.focusBrowser(row)
	}
	e.notify()
	if e.maybeRotate(t, err, time.Now()) {
		return
	}
	t.Status = StatusNeedsUser
	if err != nil && err.Error() != "" {
		t.Error = err.Error()
	} else if plan.Reason != "" {
		t.Error = plan.Reason
	}
}

func (e *Engine) recoverBrowser(id int64, restart bool) {
	e.dropSessions(id)
	if !restart || e.browsers == nil || e.store == nil || id <= 0 {
		return
	}
	for _, b := range e.store.Browsers() {
		if b.ID == id && b.ExtAccount == "" {
			e.browsers.Stop(id)
			return
		}
	}
}

func (e *Engine) dropSessions(id int64) {
	if e == nil || id <= 0 {
		return
	}
	suffix := fmt.Sprintf(":%d", id)
	var closing []interface{ Close() }
	e.mu.Lock()
	for key, sess := range e.sessions {
		if strings.HasSuffix(key, suffix) && sess != nil {
			closing = append(closing, sess)
			delete(e.sessions, key)
		}
	}
	e.mu.Unlock()
	for _, sess := range closing {
		sess.Close()
	}
}
