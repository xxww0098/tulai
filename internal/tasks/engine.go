package tasks

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/heal"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

// Progress 进度回调参数。
type Progress struct {
	Done  int
	Total int
}

// Engine 批量执行引擎。
type Engine struct {
	mu        sync.Mutex
	store     *store.Store
	browsers  *browser.Manager
	solver    captcha.Solver
	opts      Options
	view      *QueueView
	cancel    context.CancelFunc
	running   bool
	sessions  map[string]*cdp.Session // key: backend:platform:browserID
	seenHash  map[string]bool         // 结果 SHA256 去重
	srcHashes map[string]bool         // 源图哈希（回声过滤）
	dataRoot  string
	active    map[int64]bool

	extMu       sync.Mutex
	extSrv      *extbridge.Server
	extBatch    string
	extToken    string
	extListen   string
	extErr      string
	extOnChange func()
	pair        *extbridge.PairServer
	pairErr     error
	extNotice   string

	loginProbe  func(context.Context, store.Browser) (string, error)
	loginFocus  func(context.Context, store.Browser) error
	sliderFn    func(context.Context, store.Browser) error
	onHeal      func()
	onRecover   func(heal.Action, int64)
	loginLast   map[int64]time.Time
	loginPrev   map[int64]string
	attention   string
	attentionID int64
	notes       []string
	wake        chan struct{}
	wakes       []chan struct{}
	gap         func(time.Duration) time.Duration // 测试把发送间隔钉死。空则 base±随机
	agyNoted    bool

	liveMu sync.Mutex
	clock  laneClock
	starts sync.Map // task index → time.Time
}

// NewEngine 创建引擎（solver 可后置注入）。
func NewEngine(st *store.Store, bm *browser.Manager, solver captcha.Solver, dataRoot string) *Engine {
	return &Engine{
		store:    st,
		browsers: bm,
		solver:   solver,
		sessions: map[string]*cdp.Session{},
		seenHash: map[string]bool{},
		dataRoot: dataRoot,
	}
}

// SetSolver 设置验证码求解后端（UI 启动任务前按配置注入）。
func (e *Engine) SetSolver(s captcha.Solver) {
	e.mu.Lock()
	e.solver = s
	e.mu.Unlock()
}

// IsRunning 是否在跑。
func (e *Engine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// View 队列视图。
func (e *Engine) View() *QueueView {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.view
}

// Run 构建队列并执行（阻塞直至完成或停止；应在 goroutine 中调用）。
// onTask/onProgress 在任务状态变化时回调（用于 UI 刷新）。
func (e *Engine) Run(ctx context.Context, opts Options, onTask func(Task), onProgress func(Progress)) error {
	return e.run(ctx, opts, onTask, onProgress, e.attempt)
}

// RunObserved 和 Run 走同一条调度。attempt 换成测试里的页面操作，用来看任务落到哪个账号。
func (e *Engine) RunObserved(ctx context.Context, opts Options, onTask func(Task), onProgress func(Progress), attempt func(context.Context, *Task) error) error {
	return e.run(ctx, opts, onTask, onProgress, attempt)
}

// Retry 只重跑失败、跳过或待验证的任务。提示词沿用已经拼好的正文。
func (e *Engine) Retry(ctx context.Context, opts Options, failed []Task, onTask func(Task), onProgress func(Progress)) error {
	return e.retry(ctx, opts, failed, onTask, onProgress, e.attempt)
}

func (e *Engine) retry(ctx context.Context, opts Options, failed []Task, onTask func(Task), onProgress func(Progress), attempt func(context.Context, *Task) error) error {
	e.mu.Lock()
	if err := e.claimRun(); err != nil {
		e.mu.Unlock()
		return err
	}
	browsersOf := e.browsersOfPlatforms()
	queue := RebuildRetry(failed)
	if len(queue) == 0 {
		e.mu.Unlock()
		return fmt.Errorf("任务队列为空：请添加提示词，或添加源图片")
	}
	seen := map[string]bool{}
	for _, task := range queue {
		if seen[task.Platform] {
			continue
		}
		seen[task.Platform] = true
		if len(browsersOf[task.Platform]) == 0 {
			e.mu.Unlock()
			return missingPlatform(task.Platform)
		}
	}
	opts.UseSuffix = false
	opts.Images = nil
	for _, task := range queue {
		opts.Images = append(opts.Images, task.Images...)
	}
	return e.driveLocked(ctx, opts, queue, onTask, onProgress, attempt)
}

func (e *Engine) run(ctx context.Context, opts Options, onTask func(Task), onProgress func(Progress), attempt func(context.Context, *Task) error) error {
	e.mu.Lock()
	if err := e.claimRun(); err != nil {
		e.mu.Unlock()
		return err
	}
	// 浏览器映射：平台 → 可用浏览器 ID。Chrome 与 Edge 都用已添加的实例。
	browsersOf := e.browsersOfPlatforms()
	for _, p := range opts.Platforms {
		if len(browsersOf[p]) == 0 {
			e.mu.Unlock()
			return missingPlatform(p)
		}
	}
	queue := BuildQueue(opts, browsersOf)
	if len(queue) == 0 {
		e.mu.Unlock()
		return fmt.Errorf("任务队列为空：请添加提示词，或添加源图片")
	}
	return e.driveLocked(ctx, opts, queue, onTask, onProgress, attempt)
}

// claimRun 在持有 e.mu 时占用引擎。任务已在跑时返回错误，锁仍由调用方释放。
func (e *Engine) claimRun() error {
	if e.running {
		return fmt.Errorf("任务已在执行中")
	}
	for key, sess := range e.sessions {
		if sess.Ctx().Err() != nil {
			delete(e.sessions, key)
		}
	}
	// 扩展会话先登记成账号，再按账号建队。同步会听页面桥，不占 e.mu 以外的引擎锁。
	e.syncExtensionAccounts()
	return nil
}

// driveLocked 执行已经建好的队列。调用时持有 e.mu。
func (e *Engine) driveLocked(ctx context.Context, opts Options, queue []Task, onTask func(Task), onProgress func(Progress), attempt func(context.Context, *Task) error) error {
	e.opts = opts
	e.agyNoted = false
	e.view = NewQueueView(queue)
	// 预计算源图哈希：结果若与源图字节相同（平台回显原图）则跳过
	e.srcHashes = map[string]bool{}
	for _, img := range opts.Images {
		if data, err := os.ReadFile(img); err == nil {
			e.srcHashes[images.SHA256(data)] = true
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	nudge := make(chan struct{}, 1)
	e.cancel = cancel
	e.running = true
	e.wake = nudge
	e.mu.Unlock()
	e.rotateExtBatch()
	e.beginLanes()

	defer func() {
		cancel()
		e.finishLanes()
		e.mu.Lock()
		e.running = false
		e.cancel = nil
		if e.wake == nudge {
			e.wake = nil
		}
		e.mu.Unlock()
	}()

	logging.SetRun(time.Now().Format("20060102T150405.000"))
	logging.Event(logging.Line{Stage: "queue", Msg: fmt.Sprintf("任务开始：共 %d 个", len(queue)), Images: len(queue)})
	onProgress(Progress{Done: 0, Total: len(queue)})
	runErr := e.runPlatforms(ctx, nudge, queue, onTask, onProgress, attempt)
	logging.Event(logging.Line{Stage: "queue", Msg: "任务结束"})
	return runErr
}

// noteCool 把同平台最近的冷却结束时间算进下一轮唤醒。
// 正在跑的任务不会挡住刚恢复的账号。
func noteCool(earliest *time.Time, rows []store.Browser, now time.Time) {
	until := earliestCool(rows, now)
	if until.IsZero() {
		return
	}
	if earliest.IsZero() || until.Before(*earliest) {
		*earliest = until
	}
}

func (e *Engine) sendGap(base time.Duration) time.Duration {
	if e != nil && e.gap != nil {
		return e.gap(base)
	}
	return JitterWait(base, rand.Float64())
}

// dispatchLimit 是这一轮允许同时跑的任务数。
// Concurrency <= 0 时等于当前可用账号名额，跑的过程中账号变多变少都会重算。
// 正数是手动上限，仍然不会超过可用名额。一个可用账号都没有时，手动上限照旧，自动至少留 1，占位任务还能跑。
func (e *Engine) dispatchLimit(now time.Time) int {
	var rows []store.Browser
	seen := map[string]bool{}
	for _, platform := range e.opts.Platforms {
		if seen[platform] {
			continue
		}
		seen[platform] = true
		rows = append(rows, e.accountsFor(platform)...)
	}
	usable := ParallelSlots(rows, now, e.capNow())
	if e.opts.Concurrency > 0 {
		if usable > 0 {
			return min(e.opts.Concurrency, usable)
		}
		return e.opts.Concurrency
	}
	if usable < 1 {
		return 1
	}
	return usable
}

// slotKey 是调度名额。本机浏览器用自己的 ID。
// 扩展桥里同一个配置文件的千问和豆包共用一个名额：background.js 每条命令都会 focusTab，
// 同一个窗口只能有一个前台标签，再开一个标签也抢不到焦点。不同配置文件各有进程和标签，名额分开。
// 还没登记的负数占位 ID 保持原值，-1 和 -2 不再并成 0。
func (e *Engine) slotKey(id int64) int64 {
	if e == nil || e.store == nil || id == 0 {
		return id
	}
	return accountSlot(e.store.Browsers(), id)
}

// pickIdleAccount 在当前名额正忙或还在发送间隔里时，改派到同平台另一个空闲账号。
func (e *Engine) pickIdleAccount(platform string, busySlot int64, busy map[int64]bool, readyAt map[int64]time.Time, now time.Time) (int64, bool) {
	var best *store.Browser
	for _, b := range e.accountsFor(platform) {
		if !openAccount(b, now, e.capNow()) {
			continue
		}
		slot := e.slotKey(b.ID)
		if slot == busySlot || busy[slot] || now.Before(readyAt[slot]) {
			continue
		}
		if best == nil || b.LastUsed < best.LastUsed || (b.LastUsed == best.LastUsed && b.ID < best.ID) {
			row := b
			best = &row
		}
	}
	if best == nil {
		return 0, false
	}
	return best.ID, true
}

// Wake 让正在跑的调度再看一眼账号。恢复账号或新的扩展连上时调用。
func (e *Engine) Wake() {
	if e == nil {
		return
	}
	e.poke()
}

// Stop 停止任务并关闭会话。
func (e *Engine) Stop() {
	e.mu.Lock()
	cancel := e.cancel
	e.mu.Unlock()
	if cancel != nil {
		cancel()
		logging.Log.Info("已请求停止任务…")
	}
}

// execute 执行单个任务（含重试）。
func (e *Engine) execute(ctx context.Context, t Task, attempt func(context.Context, *Task) error) Task {
	maxAttempts := 1 + e.opts.RetryCount
	sliderTried := false
	for t.Attempts < maxAttempts {
		select {
		case <-ctx.Done():
			return stoppedTask(ctx, t)
		default:
		}
		if t.Attempts > 0 {
			t.Status = StatusRetry
		}
		t.Attempts++
		err := attempt(ctx, &t)
		if errors.Is(err, errHumanGaveUp) && !errors.Is(ctx.Err(), context.Canceled) {
			t.submitted = false
			if e.maybeRotate(&t, err, time.Now()) {
				return t
			}
			t.Status = StatusNeedsUser
			t.Error = errHumanGaveUp.Error()
			return t
		}
		if endedByTimeout(ctx, err) {
			t.Status = StatusFailed
			t.Error = "出图超时"
			return t
		}
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			t.Status = StatusSkipped
			t.Error = "已停止"
			return t
		}
		if err == nil {
			t.Status = StatusSuccess
			t.Error = ""
			return t
		}
		if errors.Is(err, strategy.ErrPlatformReply) {
			name := e.accountName(t.ranOnOr(t.BrowserID))
			if !t.replyRetried {
				t.replyRetried = true
				t.submitted = false
				t.sentURL, t.sentWrap = "", ""
				logging.Log.Warning("#%d %s「%s」回复异常（%s），%d 秒后换新对话重试一次", t.Index, t.Platform, name, err, int(replyBackoff/time.Second))
				if t.Attempts >= maxAttempts {
					maxAttempts = t.Attempts + 1
				}
				select {
				case <-time.After(replyBackoff):
				case <-ctx.Done():
					return stoppedTask(ctx, t)
				}
				continue
			}
			logging.Log.Warning("#%d %s「%s」新对话里仍回复异常，账号冷却 %d 分钟", t.Index, t.Platform, name, int(replyCooldown/time.Minute))
			if e.maybeRotate(&t, err, time.Now()) {
				return t
			}
			t.Status = StatusFailed
			t.Error = err.Error()
			return t
		}
		if again, done := e.healAttempt(ctx, &t, err, &sliderTried); done || again {
			if done {
				return t
			}
			continue
		}
		if e.holdAgyQuota(&t, err) {
			return t
		}
		if e.maybeRotate(&t, err, time.Now()) {
			return t
		}
		if again, stop := e.softenModeration(&t, err); again || stop {
			if stop {
				return t
			}
			continue
		}
		t.Error = err.Error()
		kind := classifyFault(err)
		if errors.Is(err, captcha.ErrVerificationRequired) {
			t.Status = StatusNeedsUser
			return t
		}
		if errors.Is(err, strategy.ErrSubmissionUncertain) {
			if t.ReassignWhy == "" {
				t.ReassignWhy = err.Error()
			}
			if e.rotateUncertain(&t, time.Now()) {
				return t
			}
			t.Status = StatusFailed
			return t
		}
		if errors.Is(err, errQianwenLogin) || errors.Is(err, errDoubaoLogin) {
			t.Status = StatusFailed
			return t
		}
		if kind == faultQuota || kind == faultRate || kind == faultAuth {
			t.Status = StatusFailed
			return t
		}
		logging.Log.Warning("#%d 第 %d 次尝试失败: %s", t.Index, t.Attempts, err)
		if t.Attempts >= maxAttempts {
			break
		}
		select {
		case <-time.After(3 * time.Second):
		case <-ctx.Done():
			return stoppedTask(ctx, t)
		}
	}
	t.Status = StatusFailed
	return t
}

// endedByTimeout 是这一单自己的时限到了。用户点停止是 Canceled，不能标成超时。
func endedByTimeout(ctx context.Context, err error) bool {
	if ctx != nil && (errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(context.Cause(ctx), context.DeadlineExceeded)) {
		return true
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// stoppedTask 是这一条任务自己的上下文结束了。不取消还在跑的其他任务。
func stoppedTask(ctx context.Context, t Task) Task {
	if endedByTimeout(ctx, nil) {
		t.Status = StatusFailed
		t.Error = "出图超时"
		return t
	}
	if ctx.Err() != nil {
		t.Status = StatusSkipped
		t.Error = "已停止"
		return t
	}
	t.Status = StatusFailed
	return t
}

// attempt 单次尝试：会话准备 → 上传 → 发送 → 等结果 → 保存。
func (e *Engine) attempt(ctx context.Context, t *Task) (attemptErr error) {
	if t != nil && t.Platform == agy.Platform {
		return e.attemptAntigravity(ctx, t)
	}
	if e.useExtension(t.Platform) {
		if t.Platform == "豆包" {
			return e.attemptDoubaoExtension(ctx, t)
		}
		return e.attemptQianwenExtension(ctx, t)
	}
	strat := strategy.Factory(t.Platform)
	if strat == nil {
		return fmt.Errorf("不支持的平台 %s", t.Platform)
	}
	if t.ranOn <= 0 {
		t.ranOn = t.BrowserID
	}
	sess, err := e.session(ctx, t.Platform, t.BrowserID, strat)
	if err != nil {
		return err
	}
	sess.SetLogAccount(t.Platform, e.accountName(t.BrowserID), e.accountPort(t.BrowserID), t.Index)
	sess.KeepOffscreen()
	work, workCancel := context.WithCancel(ctx)
	defer workCancel()
	// 调试连接断了就结束这一次尝试，整批任务继续，交给自愈重启。
	context.AfterFunc(sess.Ctx(), workCancel)
	closed := make(chan struct{})
	stopSession := context.AfterFunc(ctx, func() {
		sess.Close()
		close(closed)
	})
	defer func() {
		if errors.Is(attemptErr, captcha.ErrVerificationRequired) && ctx.Err() == nil && !errors.Is(attemptErr, context.Canceled) && !errors.Is(attemptErr, context.DeadlineExceeded) {
			if !stopSession() {
				<-closed
			}
			attemptErr = closeFailedSession(ctx, sess, attemptErr)
			e.forgetSession(sess)
			return
		}
		if !stopSession() {
			<-closed
		}
		if ctx.Err() != nil {
			sess.Close()
			e.forgetSession(sess)
			return
		}
		if sess.Ctx().Err() != nil {
			sess.Close()
			e.forgetSession(sess)
			if attemptErr == nil || errors.Is(attemptErr, context.Canceled) || errors.Is(attemptErr, context.DeadlineExceeded) {
				attemptErr = fmt.Errorf("%w", heal.ErrCrashed)
			}
		}
	}()
	// 发送前等待（等待页面/登录就绪）
	if !t.submitted && e.opts.WaitBeforeSend > 0 {
		select {
		case <-time.After(time.Duration(e.opts.WaitBeforeSend) * time.Second):
		case <-work.Done():
			if ctx.Err() != nil {
				return fmt.Errorf("已停止")
			}
			return fmt.Errorf("%w", heal.ErrCrashed)
		}
	}
	if !t.submitted {
		if row, ok := e.browserByID(t.BrowserID); ok && row.ExtAccount == "" {
			if state, stateErr := sess.LoginState(); stateErr == nil {
				switch state {
				case "out":
					e.applyLogin(row, heal.StateOut)
					return fmt.Errorf("未登录")
				case "challenge":
					e.applyLogin(row, heal.StateChallenge)
					return errNeedsVerify
				case "in":
					e.applyLogin(row, heal.StateIn)
				}
			}
		}
	}
	if err := e.submitTask(work, t, strat, sess); err != nil {
		return err
	}
	if !e.opts.WaitResult {
		return nil
	}
	return e.waitAndSave(work, t, strat, sess)
}

func (e *Engine) submitTask(ctx context.Context, t *Task, strat strategy.Strategy, sess *cdp.Session) error {
	if t == nil {
		return nil
	}
	if t.sentURL != "" {
		strategy.FocusQwenChat(sess, t.sentURL, t.sentWrap)
		t.submitted = true
		return nil
	}
	if t.submitted {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := e.holdIfCaptcha(ctx, sess, t); err != nil {
			return err
		}
		err := e.submitOnce(t, strat, sess)
		if err == nil || t.submitted {
			return err
		}
		blocked, _ := captchaVisible(sess)
		if !blocked {
			return err
		}
	}
}

func (e *Engine) submitOnce(t *Task, strat strategy.Strategy, sess *cdp.Session) error {
	if t.submitted {
		return nil
	}
	// 复用的标签还停在上一轮对话里。每单先开新对话，再记基准图。
	// 同一账号只留这一张标签，前面任务漏关的豆包页在这里关掉。
	if sess != nil && strat != nil {
		sess.KeepOnePage(strat.HomeURL())
	}
	// 重试前重新打开新对话页。上一次可能卡在没加载完的页面上，原地再等也不会好。
	if sess != nil && strat != nil && t.Attempts > 1 {
		logging.Log.Info("#%d %s「%s」重试前重新打开新对话页", t.Index, t.Platform, e.accountName(t.BrowserID))
		if err := sess.Navigate(strat.HomeURL()); err != nil {
			logging.Log.Debugf("#%d %s「%s」打开新对话页 %s", t.Index, t.Platform, e.accountName(t.BrowserID), err.Error())
		}
	}
	if err := strategy.FreshChat(sess); err != nil {
		return fmt.Errorf("新对话: %w", err)
	}
	// 清理上个任务残留
	if err := strat.CleanComposer(sess); err != nil {
		return fmt.Errorf("清理输入区: %w", err)
	}
	// 上传源图
	if len(t.Images) > 0 {
		if err := strat.UploadFiles(sess, t.Images); err != nil {
			return fmt.Errorf("上传: %w", err)
		}
		e.traceTask(t, "upload", fmt.Sprintf("#%d 已上传 %d 张源图", t.Index, len(t.Images)), len(t.Images), 0, nil)
	}
	// 上传预览就绪后再标记，避免把源图回显当作生成结果。
	if err := strat.MarkTask(sess); err != nil {
		return err
	}
	// 发送提示词
	if err := strat.SendPrompt(sess, t.Prompt); err != nil {
		if adoptSentChat(t, sess) {
			t.submitted = true
			return nil
		}
		return fmt.Errorf("发送: %w", err)
	}
	adoptSentChat(t, sess)
	t.submitted = true
	e.traceTask(t, "submit", fmt.Sprintf("#%d %s 已提交", t.Index, e.accountName(t.BrowserID)), 0, 0, nil)
	return nil
}

func adoptSentChat(t *Task, sess *cdp.Session) bool {
	if t == nil || sess == nil {
		return false
	}
	urlID, wrapID := sess.BoundChat()
	if urlID == "" {
		return false
	}
	t.sentURL = urlID
	if wrapID != "" {
		t.sentWrap = wrapID
	}
	return true
}

const hardResultWait = 4 * time.Minute

// qianwenRetryWait 盖住千问实际出图的 40–90 秒。重试是回到原对话再读，不是再等一个更短的时限。
const qianwenRetryWait = 90 * time.Second

// resultWait 是这一单等图的上限。配置更长时收到 4 分钟，到点保存已有的图或明确失败。
func (e *Engine) resultWait() time.Duration {
	d := time.Duration(0)
	if e != nil {
		d = time.Duration(e.opts.WaitResultSeconds) * time.Second
	}
	if d <= 0 || d > hardResultWait {
		return hardResultWait
	}
	return d
}

func (e *Engine) resultWaitFor(t *Task) time.Duration {
	d := e.resultWait()
	if t != nil && t.Platform == "千问" && t.Attempts > 1 && d < qianwenRetryWait {
		return qianwenRetryWait
	}
	return d
}

func (e *Engine) waitAndSave(ctx context.Context, t *Task, strat strategy.Strategy, sess *cdp.Session) error {
	budget := e.resultWaitFor(t)
	var used time.Duration
	lastProgress := time.Now()
	for used < budget {
		if err := ctx.Err(); err != nil {
			return err
		}
		held, err := e.holdIfCaptcha(ctx, sess, t)
		if err != nil {
			return err
		}
		if held {
			lastProgress = time.Now()
			if err := e.submitTask(ctx, t, strat, sess); err != nil {
				return err
			}
		}
		left := budget - used
		start := time.Now()
		results, waitErr := strat.WaitForResult(sess, left)
		used += time.Since(start)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(waitErr, captcha.ErrVisible) {
			if err := e.holdForHuman(ctx, sess, t); err != nil {
				return err
			}
			lastProgress = time.Now()
			if err := e.submitTask(ctx, t, strat, sess); err != nil {
				return err
			}
			continue
		}
		if errors.Is(waitErr, context.Canceled) || errors.Is(waitErr, context.DeadlineExceeded) || (waitErr != nil && len(results) == 0) {
			return waitErr
		}
		downloads := 0
		for _, result := range results {
			if result.Data != nil {
				downloads++
			}
		}
		if len(results) > 0 {
			e.traceTask(t, "detect_done", fmt.Sprintf("#%d 检测到 %d 张", t.Index, len(results)), len(results), 0, nil)
		}
		if downloads > 0 {
			e.traceTask(t, "download", fmt.Sprintf("#%d 下载 %d 张", t.Index, downloads), downloads, 0, nil)
		}
		if candidates := len(results) - downloads; candidates > 0 {
			logging.Log.Debugf("#%d 截获 %d 个候选结果", t.Index, candidates)
		}
		saved, err := e.saveResults(ctx, t, strat, sess, results)
		t.Results = append(t.Results, saved...)
		if err = errors.Join(waitErr, err); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(saved) > 0 || t.matchedSavedResult {
			return nil
		}
		if time.Since(lastProgress) >= stallAfter {
			return fmt.Errorf("%w", heal.ErrStuck)
		}
		logging.Log.Debugf("#%d 候选暂无有效生成图，继续等待", t.Index)
		pause := min(1500*time.Millisecond, budget-used)
		if pause < 0 {
			pause = 0
		}
		sleepStart := time.Now()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pause):
		}
		used += time.Since(sleepStart)
	}
	return fmt.Errorf("出图超时，没有可保存的图片")
}

// browsersOfPlatforms 返回每个平台可用的浏览器。
// browserBackend=extension 时优先用已连上的扩展账号。还没有会话时用占位 ID，跟以前的单连接一样。
func (e *Engine) browsersOfPlatforms() map[string][]int64 {
	if e.useExtension("千问") {
		browsersOf := map[string][]int64{}
		if e.store != nil {
			for _, b := range e.store.Browsers() {
				if b.Platform == agy.Platform {
					if b.ExtAccount == "" {
						browsersOf[b.Platform] = append(browsersOf[b.Platform], b.ID)
					}
					continue
				}
				if b.ExtAccount == "" {
					continue
				}
				browsersOf[b.Platform] = append(browsersOf[b.Platform], b.ID)
			}
		}
		if len(browsersOf["豆包"]) == 0 {
			browsersOf["豆包"] = []int64{-1}
		}
		if len(browsersOf["千问"]) == 0 {
			browsersOf["千问"] = []int64{-2}
		}
		return browsersOf
	}
	browsersOf := map[string][]int64{}
	if e.store == nil {
		return browsersOf
	}
	for _, b := range e.store.Browsers() {
		if b.ExtAccount != "" {
			continue
		}
		browsersOf[b.Platform] = append(browsersOf[b.Platform], b.ID)
	}
	return browsersOf
}

func (e *Engine) pageBackend() string {
	raw := ""
	if e.store != nil {
		raw = e.store.Get("browser_backend")
	}
	return browser.NormalizeStoredBackend(runtime.GOOS, raw)
}

// session 取/建平台会话（同一浏览器+平台复用一个标签页，SharedSession 语义）。
func (e *Engine) session(ctx context.Context, platform string, browserID int64, strat strategy.Strategy) (*cdp.Session, error) {
	backend := e.pageBackend()
	key := fmt.Sprintf("%s:%s:%d", backend, platform, browserID)
	e.mu.Lock()
	if s, ok := e.sessions[key]; ok {
		e.mu.Unlock()
		return s, nil
	}
	solver := e.solver
	e.mu.Unlock()

	if e.store == nil || e.browsers == nil {
		return nil, fmt.Errorf("浏览器 %d 不存在", browserID)
	}
	var b store.Browser
	found := false
	for _, cand := range e.store.Browsers() {
		if cand.ID == browserID {
			b, found = cand, true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("浏览器 %d 不存在", browserID)
	}
	onLog := func(line string) {
		if len(line) > 0 && len(line) < 400 {
			logging.Log.Debugf("[页面] %s", line)
		}
	}
	home := ""
	if strat != nil {
		home = strat.HomeURL()
	}
	// 出图冷启动不要盖住 tulai。窗口在创建时放在屏幕外，启动后把 tulai 拉回前面。
	// 登录走界面的 Start(..., false)，窗口可以到前面。
	ws, startErr := e.browsers.Start(b, e.dataRoot, backend, home, true)
	if startErr != nil {
		return nil, fmt.Errorf("启动浏览器[%s]: %w", b.Name, startErr)
	}
	client, clientErr := cdp.NewClient(ws)
	if clientErr != nil {
		return nil, clientErr
	}
	sess, err := client.NewBackgroundSession(home, onLog)
	e.browsers.KeepAppInFront()
	e.traceTask(&Task{Index: 0, Platform: platform, BrowserID: browserID}, "open", "打开页面", 0, 0, err)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("打开页面: %w", err)
	}
	context.AfterFunc(sess.Ctx(), client.Close)
	if err := captcha.PreparePage(sess); err != nil {
		return nil, closeFailedSession(ctx, sess, err)
	}
	// 注入平台 hook + 验证码求解
	if err := strat.InstallHook(sess); err != nil {
		return nil, closeFailedSession(ctx, sess, err)
	}
	// 出图不自动拖验证码。求解会和人工抢滑块。
	captcha.Attach(sess, solver, false, func() {
		e.store.UpdateBrowser(browserID, true, false)
	})
	sess.KeepAlive(ctx, 30*time.Second)

	e.mu.Lock()
	// 竞态：他人先建了就复用
	if s, ok := e.sessions[key]; ok {
		e.mu.Unlock()
		sess.Close()
		return s, nil
	}
	e.sessions[key] = sess
	e.mu.Unlock()
	e.store.UpdateBrowser(browserID, true, true)
	return sess, nil
}

type failedSession interface {
	Ctx() context.Context
	IsEgo() bool
	EgoEvaluate(string, any) error
	HandOff() error
	Close()
}

func closeFailedSession(ctx context.Context, sess failedSession, err error) error {
	if ctx.Err() == nil && errors.Is(err, captcha.ErrVerificationRequired) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		if sess.IsEgo() {
			manualErr := captcha.SetManual(sess)
			handOffErr := sess.HandOff()
			if errors.Is(handOffErr, context.DeadlineExceeded) && ctx.Err() == nil {
				// 交还操作的独立超时不表示整批任务已取消。
				handOffErr = fmt.Errorf("交还浏览器超时: %v", handOffErr)
			}
			err = errors.Join(err, manualErr, handOffErr)
		}
	}
	sess.Close()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

func (e *Engine) accountBusy(id int64) bool {
	if e == nil || id <= 0 {
		return false
	}
	e.mu.Lock()
	on := e.active[id]
	e.mu.Unlock()
	return on
}

func (e *Engine) setActive(id int64, on bool) {
	if id <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active == nil {
		e.active = map[int64]bool{}
	}
	if on {
		e.active[id] = true
	} else {
		delete(e.active, id)
	}
}

// ActiveAccounts 是当前正在跑任务的账号。
func (e *Engine) ActiveAccounts() map[int64]bool {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make(map[int64]bool, len(e.active))
	for id := range e.active {
		out[id] = true
	}
	return out
}

// PairState 告诉界面自动对接有没有在听、扩展最近有没有来取令牌。
func (e *Engine) PairState() (listening, paired bool) {
	if e == nil {
		return false, false
	}
	e.extMu.Lock()
	p := e.pair
	e.extMu.Unlock()
	if p == nil {
		return false, false
	}
	hit := p.LastHit()
	return true, !hit.IsZero() && time.Since(hit) < 2*time.Minute
}

// ServeLocal 启动页面桥和固定端口上的令牌发现。桥没听成时仍然尝试发现端口。
func (e *Engine) ServeLocal() error {
	if e == nil {
		return nil
	}
	bridgeErr := error(nil)
	if _, err := e.extensionServer(); err != nil {
		bridgeErr = err
		logging.Log.Warning("页面桥未监听: %v", err)
	}
	e.extMu.Lock()
	already := e.pair != nil
	e.extMu.Unlock()
	if !already {
		pair, err := extbridge.StartPair(e.pairInfo)
		e.extMu.Lock()
		if err != nil {
			e.pairErr = err
		} else {
			e.pair = pair
			e.pairErr = nil
		}
		e.extMu.Unlock()
		if err != nil {
			logging.Log.Warning("自动对接未启动: %v", err)
			if bridgeErr != nil {
				return bridgeErr
			}
			return err
		}
		logging.Log.Info("页面桥自动对接监听 127.0.0.1:%s", extbridge.PairPort)
	}
	return bridgeErr
}

func (e *Engine) pairInfo() extbridge.PairInfo {
	token := extbridge.DefaultExtToken
	port := extbridge.DefaultExtPort
	if e != nil && e.store != nil {
		token = extbridge.NormalizeExtToken(e.store.Get(extbridge.SettingExtToken))
		port = extbridge.NormalizeExtPort(e.store.Get(extbridge.SettingExtPort))
	}
	return extbridge.PairInfo{Token: token, Port: port}
}

func (e *Engine) forgetSession(sess *cdp.Session) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for key, cached := range e.sessions {
		if cached == sess {
			delete(e.sessions, key)
		}
	}
}

// 限制解码分配，仍允许 8K 结果图。
const maxResultPixels = 32 * 1024 * 1024

// prefetchResults 在同一页里并行下载多张结果。单张仍走原来的逐张抓取。
func prefetchResults(strat strategy.Strategy, sess *cdp.Session, results []strategy.Result) [][]byte {
	out := make([][]byte, len(results))
	fetch, ok := strat.(interface {
		FetchResultList(*cdp.Session, []string) [][]byte
	})
	if !ok || sess == nil || sess.IsEgo() {
		return out
	}
	var urls []string
	var idx []int
	for i, r := range results {
		if r.Data == nil && r.URL != "" {
			urls = append(urls, r.URL)
			idx = append(idx, i)
		}
	}
	if len(urls) < 2 {
		return out
	}
	got := fetch.FetchResultList(sess, urls)
	for n, data := range got {
		if n < len(idx) {
			out[idx[n]] = data
		}
	}
	return out
}

// saveResults 校验 + 去重 + 按日期目录落盘。
func (e *Engine) saveResults(ctx context.Context, t *Task, strat strategy.Strategy, sess *cdp.Session, results []strategy.Result) ([]string, error) {
	t.matchedSavedResult = false
	var saved []string
	batched := prefetchResults(strat, sess, results)
	for i, r := range results {
		if err := ctx.Err(); err != nil {
			return saved, err
		}
		data := r.Data
		var err error
		if data == nil && i < len(batched) && batched[i] != nil {
			data = batched[i]
		}
		if data == nil {
			data, err = strat.FetchResultBytes(sess, r.URL)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return saved, ctxErr
		}
		if err != nil {
			logging.Log.Warning("候选图 #%d 抓取失败: %s", i+1, strings.SplitN(err.Error(), "?", 2)[0])
			if parsed, parseErr := url.Parse(r.URL); parseErr == nil {
				path := []rune(parsed.EscapedPath())
				if len(path) > 220 {
					path = path[:220]
				}
				logging.Log.Debugf("候选图 #%d URL: scheme=%s; host=%s; path=%s", i+1, parsed.Scheme, parsed.Host, string(path))
			}
			continue
		}
		if !images.IsValidImageBytes(data) {
			if r.Data != nil {
				return saved, fmt.Errorf("网页下载结果 #%d 校验失败: 文件为空或图片格式不支持", i+1)
			}
			logging.Log.Debugf("候选图 #%d 不是有效图片", i+1)
			continue
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			if r.Data != nil {
				return saved, fmt.Errorf("网页下载结果 #%d 校验失败: 无法解析图片格式: %w", i+1, err)
			}
			logging.Log.Debugf("候选图 #%d 无法解析图片格式", i+1)
			continue
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width) > maxResultPixels/int64(cfg.Height) {
			if r.Data != nil {
				return saved, fmt.Errorf("网页下载结果 #%d 校验失败: 图片尺寸超出上限: %dx%d", i+1, cfg.Width, cfg.Height)
			}
			logging.Log.Debugf("候选图 #%d 图片尺寸超出上限: %dx%d", i+1, cfg.Width, cfg.Height)
			continue
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			if r.Data != nil {
				return saved, fmt.Errorf("网页下载结果 #%d 校验失败: 无法完整解码: %w", i+1, err)
			}
			logging.Log.Debugf("候选图 #%d 无法完整解码", i+1)
			continue
		}
		if len(data) < images.MinGeneratedBytes || cfg.Width < images.MinGeneratedSide || cfg.Height < images.MinGeneratedSide {
			logging.Log.Debugf("候选图 #%d 不是生成图（%d 字节，%dx%d），已忽略", i+1, len(data), cfg.Width, cfg.Height)
			continue
		}
		if err := ctx.Err(); err != nil {
			return saved, err
		}
		hash := images.SHA256(data)
		if t.savedHashes[hash] {
			t.matchedSavedResult = true
			continue
		}
		e.mu.Lock()
		dup := e.seenHash[hash] || e.srcHashes[hash] // 全局去重 + 源图回声过滤
		e.seenHash[hash] = true
		e.mu.Unlock()
		if dup {
			logging.Log.Debugf("候选图 #%d 与源图或已保存结果重复", i+1)
			continue
		}
		ext := "." + format
		if format == "jpeg" {
			ext = ".jpg"
		}
		// 洗图结果统一存 PNG，白底主体图后面还要再加工。
		if e.opts.Wash && format != "png" {
			png, encErr := images.EncodePNG(decoded)
			if encErr != nil {
				return saved, fmt.Errorf("洗图结果 #%d 转 PNG 失败: %w", i+1, encErr)
			}
			data, ext = png, ".png"
		}
		dir := e.opts.ResultDir(time.Now())
		path, err := writeNumberedFile(ctx, dir, e.opts.resultStem(t.Prompt, resultSource(t)), ext, data)
		if err != nil {
			e.mu.Lock()
			delete(e.seenHash, hash)
			e.mu.Unlock()
			return saved, err
		}
		saved = append(saved, path)
		if t.savedHashes == nil {
			t.savedHashes = make(map[string]bool)
		}
		t.savedHashes[hash] = true
		// 扩展下载已经换成去水印 PNG 时，不再按实验开关另存一份。
		if !e.opts.RemoveWatermark || r.WatermarkRemoved {
			continue
		}
		if !watermarkStaticImage(data, format) {
			logging.Log.Warning("#%d 图片 #%d 跳过去水印：仅支持静态 PNG/JPEG/WebP，原图已保留", t.Index, i+1)
			continue
		}
		logging.Log.Info("#%d 图片 #%d 正在计算去水印，原图已保留", t.Index, i+1)
		repaired, changed, repairErr := images.RemoveWatermark(ctx, decoded, t.Platform)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return saved, ctxErr
		}
		if repairErr != nil {
			logging.Log.Warning("#%d 图片 #%d 去水印失败，保留原图: %s", t.Index, i+1, repairErr)
			continue
		}
		if !changed {
			logging.Log.Warning("#%d 图片 #%d 未匹配到已支持的水印，保留原图", t.Index, i+1)
			continue
		}
		repairData, repairErr := images.EncodePNG(repaired)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return saved, ctxErr
		}
		if repairErr == nil {
			repairBase := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + "_去水印"
			var repairPath string
			repairPath, repairErr = writeResultFile(ctx, dir, repairBase, ".png", repairData)
			if errors.Is(repairErr, os.ErrExist) {
				repairPath, repairErr = writeNumberedFile(ctx, dir, repairBase, ".png", repairData)
			}
			if repairErr == nil {
				saved[len(saved)-1] = repairPath
				logging.Log.Success("#%d 图片 #%d 已另存去水印版本，原图已保留", t.Index, i+1)
			}
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return saved, ctxErr
		}
		if repairErr != nil {
			logging.Log.Warning("#%d 图片 #%d 去水印版本保存失败，保留原图: %s", t.Index, i+1, repairErr)
		}
	}
	if len(saved) > 0 {
		e.traceTask(t, "save", fmt.Sprintf("#%d 保存 %d 张", t.Index, len(saved)), len(saved), 0, nil)
	}
	return saved, nil
}

// fileSlot 串行分配同一个目录、同一个词根的序号。
// 两个任务同时保存同一个名字时，必须先占住下一个号再写，避免写到同一个文件。
type fileSlot struct {
	mu   sync.Mutex
	next int
}

var fileSlots sync.Map

func slotFor(dir, stem string) *fileSlot {
	key := dir + "\x00" + stem
	if v, ok := fileSlots.Load(key); ok {
		return v.(*fileSlot)
	}
	v, _ := fileSlots.LoadOrStore(key, &fileSlot{})
	return v.(*fileSlot)
}

// writeNumberedFile 写成 stem_01、stem_02……同一天已有的号继续往后排，不覆盖。
func writeNumberedFile(ctx context.Context, dir, stem, ext string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(stem) == "" {
		stem = "文生图"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建结果目录失败: %w", err)
	}
	slot := slotFor(dir, stem)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.next < 1 {
		slot.next = highestNumber(dir, stem) + 1
	}
	for slot.next <= 100000 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		path := filepath.Join(dir, fmt.Sprintf("%s_%02d%s", stem, slot.next, ext))
		err := createExclusive(ctx, path, data)
		if errors.Is(err, os.ErrExist) {
			slot.next++
			continue
		}
		if err != nil {
			return "", err
		}
		slot.next++
		return path, nil
	}
	return "", errors.New("结果序号已用尽")
}

// highestNumber 是目录里 stem_序号 的最大序号。去水印副本带别的后缀，不占这个号。
func highestNumber(dir, stem string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	prefix := stem + "_"
	max := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		num := base[len(prefix):]
		if num == "" || strings.TrimLeft(num, "0123456789") != "" {
			continue
		}
		n, err := strconv.Atoi(num)
		if err != nil || n <= 0 {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max
}

// writeResultFile 按给定文件名独占创建。重名返回 os.ErrExist，不覆盖。
func writeResultFile(ctx context.Context, dir, base, ext string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建结果目录失败: %w", err)
	}
	path := filepath.Join(dir, base+ext)
	if err := createExclusive(ctx, path, data); err != nil {
		return "", err
	}
	return path, nil
}

// createExclusive 独占创建。取消或写入失败只清理本次未完成文件。
func createExclusive(ctx context.Context, path string, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err == nil {
		_, err = file.Write(data)
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return errors.Join(err, os.Remove(path))
	}
	return nil
}

func watermarkStaticImage(data []byte, format string) bool {
	switch format {
	case "jpeg":
		return true
	case "png":
		for offset := 8; offset <= len(data)-12; {
			size := uint64(binary.BigEndian.Uint32(data[offset:]))
			if string(data[offset+4:offset+8]) == "acTL" {
				return false
			}
			if size+12 > uint64(len(data)-offset) {
				return false
			}
			offset += int(size) + 12
		}
		return true
	case "webp":
		for offset := 12; offset <= len(data)-8; {
			size := uint64(binary.LittleEndian.Uint32(data[offset+4:]))
			kind := string(data[offset : offset+4])
			if size+8 > uint64(len(data)-offset) {
				return false
			}
			if kind == "ANIM" || kind == "ANMF" || (kind == "VP8X" && size > 0 && data[offset+8]&2 != 0) {
				return false
			}
			offset += int(size) + 8 + int(size&1)
		}
		return true
	}
	return false
}

// CloseSessions 关闭全部页面会话。
func (e *Engine) CloseSessions() {
	e.closeExtensionBridge()
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, s := range e.sessions {
		s.Close()
		delete(e.sessions, k)
	}
}
