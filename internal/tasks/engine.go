package tasks

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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
			return fmt.Errorf("平台[%s]没有配置浏览器，请先在浏览器管理中添加", task.Platform)
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
			return fmt.Errorf("平台[%s]没有配置浏览器，请先在浏览器管理中添加", p)
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

	defer func() {
		cancel()
		e.mu.Lock()
		e.running = false
		e.cancel = nil
		if e.wake == nudge {
			e.wake = nil
		}
		e.mu.Unlock()
	}()

	total := len(queue)
	done := 0
	var runErr error
	workers := min(max(opts.Concurrency, 1), total)
	jobs := make(chan Task)
	completed := make(chan Task, workers)
	// 按浏览器 ID 隔离。扩展桥的占位 ID 为负数，调度时归到同一个执行空间。
	busy := map[int64]bool{}
	readyAt := map[int64]time.Time{}
	var wg sync.WaitGroup
	logging.Log.Info("任务开始：共 %d 个（并发 %d）", total, workers)
	onProgress(Progress{Done: 0, Total: total})

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				if ctx.Err() != nil {
					completed <- t
					continue
				}
				t.slot = t.BrowserID
				if t.BrowserID > 0 {
					e.setActive(t.BrowserID, true)
					if e.store != nil {
						e.store.TouchBrowser(t.BrowserID, time.Now())
					}
				}
				t.Status = StatusRunning
				e.view.Update(t)
				onTask(t)
				t = e.execute(ctx, t, attempt)
				if t.slot > 0 {
					e.setActive(t.slot, false)
				}
				e.view.Update(t)
				onTask(t)
				completed <- t
			}
		}()
	}
	active := 0
	pending := queue
	ctxDone := ctx.Done()
	for len(pending) > 0 || active > 0 {
		if ctx.Err() != nil {
			pending, ctxDone = nil, nil
			if active == 0 {
				break
			}
		}
		var dispatch chan Task
		var next Task
		index := -1
		var earliest time.Time
		if active < workers && ctx.Err() == nil {
			now := time.Now()
			for i := 0; i < len(pending); i++ {
				task := pending[i]
				wait, dead := e.settleAccount(ctx, &task)
				pending[i] = task
				if dead {
					task.Status = StatusFailed
					if task.Error == "" {
						task.Error = "同平台账号都不可用"
					}
					e.view.Update(task)
					onTask(task)
					done++
					onProgress(Progress{Done: done, Total: total})
					logging.Log.Warning("#%d [%s] %s: %s", task.Index, task.Platform, task.Status, task.Error)
					pending = append(pending[:i], pending[i+1:]...)
					i--
					continue
				}
				if !wait.IsZero() {
					if earliest.IsZero() || wait.Before(earliest) {
						earliest = wait
					}
					continue
				}
				resource := max(task.BrowserID, 0)
				if busy[resource] {
					continue
				}
				if ready := readyAt[resource]; now.Before(ready) {
					if earliest.IsZero() || ready.Before(earliest) {
						earliest = ready
					}
					continue
				}
				index, next, dispatch = i, task, jobs
				break
			}
		}
		if len(pending) == 0 && active == 0 {
			break
		}
		var timer *time.Timer
		var wake <-chan time.Time
		if dispatch == nil && !earliest.IsZero() {
			timer = time.NewTimer(time.Until(earliest))
			wake = timer.C
		}
		select {
		case dispatch <- next:
			busy[max(next.BrowserID, 0)] = true
			if index == 0 {
				pending = pending[1:]
			} else {
				pending = append(pending[:index], pending[index+1:]...)
			}
			active++
		case t := <-completed:
			active--
			resource := t.slot
			if resource == 0 {
				resource = t.BrowserID
			}
			resource = max(resource, 0)
			delete(busy, resource)
			used := t.ranOn
			if used == 0 {
				used = resource
			}
			readyAt[max(used, 0)] = time.Now().Add(time.Duration(opts.SendWait) * time.Second)
			if t.rotated && t.Status == StatusPending {
				t.rotated = false
				pending = append(pending, t)
				if timer != nil {
					timer.Stop()
					timer = nil
				}
				continue
			}
			if t.Status != StatusPending {
				done++
				if t.Status == StatusNeedsUser && ctx.Err() != nil {
					runErr = captcha.ErrVerificationRequired
				}
				onProgress(Progress{Done: done, Total: total})
				if t.Status == StatusSuccess {
					logging.Log.Success("#%d [%s] 完成，保存 %d 张", t.Index, t.Platform, len(t.Results))
				} else {
					logging.Log.Warning("#%d [%s] %s: %s", t.Index, t.Platform, t.Status, t.Error)
				}
			}
		case <-wake:
		case <-nudge:
		case <-ctxDone:
			pending, ctxDone = nil, nil
		}
		if timer != nil {
			timer.Stop()
		}
	}
	close(jobs)
	wg.Wait()
	logging.Log.Info("任务结束：完成 %d/%d", done, total)
	return runErr
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
			t.Status = StatusSkipped
			t.Error = "已停止"
			return t
		default:
		}
		if t.Attempts > 0 {
			t.Status = StatusRetry
		}
		t.Attempts++
		err := attempt(ctx, &t)
		if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if e.cancel != nil {
				e.cancel()
			}
			t.Status = StatusSkipped
			t.Error = "已停止"
			return t
		}
		if err == nil {
			t.Status = StatusSuccess
			t.Error = ""
			return t
		}
		if again, done := e.healAttempt(ctx, &t, err, &sliderTried); done || again {
			if done {
				return t
			}
			continue
		}
		if e.maybeRotate(&t, err, time.Now()) {
			return t
		}
		t.Error = err.Error()
		kind := classifyFault(err)
		if errors.Is(err, captcha.ErrVerificationRequired) {
			t.Status = StatusNeedsUser
			if !e.hasOtherHealthy(t.Platform, t.ranOnOr(t.BrowserID), time.Now()) && e.cancel != nil {
				e.cancel()
			}
			return t
		}
		if errors.Is(err, strategy.ErrSubmissionUncertain) || errors.Is(err, errQianwenLogin) || errors.Is(err, errDoubaoLogin) {
			t.Status = StatusFailed
			if errors.Is(err, strategy.ErrSubmissionUncertain) || !e.hasOtherHealthy(t.Platform, t.ranOnOr(t.BrowserID), time.Now()) {
				if e.cancel != nil {
					e.cancel()
				}
			}
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
			t.Status = StatusSkipped
			t.Error = "已停止"
			return t
		}
	}
	t.Status = StatusFailed
	return t
}

// attempt 单次尝试：会话准备 → 上传 → 发送 → 等结果 → 保存。
func (e *Engine) attempt(ctx context.Context, t *Task) (attemptErr error) {
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
	e.mu.Lock()
	cancelRun := e.cancel
	e.mu.Unlock()
	sess, err := e.session(ctx, t.Platform, t.BrowserID, strat)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			cancelRun()
		}
		return err
	}
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
	if err := e.submitTask(t, strat, sess); err != nil {
		return err
	}
	if !e.opts.WaitResult {
		return nil
	}
	return e.waitAndSave(work, t, strat, sess)
}

func (e *Engine) submitTask(t *Task, strat strategy.Strategy, sess *cdp.Session) error {
	if t.submitted {
		return nil
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
		logging.Log.Info("#%d 已上传 %d 张源图", t.Index, len(t.Images))
	}
	// 上传预览就绪后再标记，避免把源图回显当作生成结果。
	if err := strat.MarkTask(sess); err != nil {
		return err
	}
	// 发送提示词
	if err := strat.SendPrompt(sess, t.Prompt); err != nil {
		return fmt.Errorf("发送: %w", err)
	}
	t.submitted = true
	logging.Log.Info("#%d 已发送提示词（%.20s…）", t.Index, t.Prompt)
	return nil
}

func (e *Engine) waitAndSave(ctx context.Context, t *Task, strat strategy.Strategy, sess *cdp.Session) error {
	deadline := time.Now().Add(time.Duration(e.opts.WaitResultSeconds) * time.Second)
	lastProgress := time.Now()
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		results, waitErr := strat.WaitForResult(sess, time.Until(deadline))
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
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
		if downloads > 0 {
			logging.Log.Info("#%d 收到 %d 个网页下载结果，正在校验保存", t.Index, downloads)
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
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(min(1500*time.Millisecond, time.Until(deadline))):
		}
	}
	return fmt.Errorf("等待生成结果超时：未得到有效的新图片")
}

// browsersOfPlatforms 返回每个平台可用的浏览器。
// browserBackend=extension 时优先用已连上的扩展账号。还没有会话时用占位 ID，跟以前的单连接一样。
func (e *Engine) browsersOfPlatforms() map[string][]int64 {
	if e.useExtension("千问") {
		browsersOf := map[string][]int64{}
		if e.store != nil {
			for _, b := range e.store.Browsers() {
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
	ws, startErr := e.browsers.Start(b, e.dataRoot, backend, home, false)
	if startErr != nil {
		return nil, fmt.Errorf("启动浏览器[%s]: %w", b.Name, startErr)
	}
	client, clientErr := cdp.NewClient(ws)
	if clientErr != nil {
		return nil, clientErr
	}
	sess, err := client.NewSession(strat.HomeURL(), onLog)
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
	captcha.Attach(sess, solver, e.store.GetBool("captcha_enabled"), func() {
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

// saveResults 校验 + 去重 + 按命名规则落盘。
func (e *Engine) saveResults(ctx context.Context, t *Task, strat strategy.Strategy, sess *cdp.Session, results []strategy.Result) ([]string, error) {
	t.matchedSavedResult = false
	var saved []string
	for i, r := range results {
		if err := ctx.Err(); err != nil {
			return saved, err
		}
		data := r.Data
		var err error
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
		src := resultSource(t)
		dir := e.opts.ResultDir(src)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			e.mu.Lock()
			delete(e.seenHash, hash)
			e.mu.Unlock()
			return saved, err
		}
		ext := "." + format
		if format == "jpeg" {
			ext = ".jpg"
		}
		base := strings.TrimSuffix(e.opts.NameFor(src, i+1, len(results)), ".png")
		path, err := writeResultFile(ctx, dir, base, ext, data)
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
	return saved, nil
}

// writeResultFile 独占创建，取消或写入失败只清理本次未完成文件。
func writeResultFile(ctx context.Context, dir, base, ext string, data []byte) (string, error) {
	path := filepath.Join(dir, base+ext)
	var file *os.File
	var err error
	for suffix := 1; ; suffix++ {
		if err = ctx.Err(); err != nil {
			return "", err
		}
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if !errors.Is(err, os.ErrExist) {
			break
		}
		path = filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, suffix, ext))
	}
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err == nil {
		_, err = file.Write(data)
	}
	err = errors.Join(err, file.Close())
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return "", errors.Join(err, os.Remove(path))
	}
	return path, nil
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
