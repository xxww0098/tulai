package tasks

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/dedup"
	"github.com/xxww0098/tulai/internal/download"
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
	mu                 sync.Mutex
	store              *store.Store
	browsers           *browser.Manager
	solver             captcha.Solver
	opts               Options
	view               *QueueView
	cancel             context.CancelFunc
	running            bool
	runDone            chan struct{}
	closing            bool
	sessions           map[string]*cdp.Session // key: backend:platform:browserID
	washChats          map[*cdp.Session]washChat
	pendingDeleteChats map[*cdp.Session]pendingDeleteChat
	washBatchKey       string // 单次运行的洗图批次标识，用于生成唯一上传文件名
	runBrowsers        map[int64]bool    // 本批实际使用过的受管浏览器账号
	unavailableEngines sync.Map          // account engines that failed this run's local preflight
	srcHashes          map[string]string // 测试注入的源图 SHA256；正式运行走惰性流式缓存 srcHashCache
	srcHashCache       sync.Map          // 源图路径 → SHA256（惰性流式缓存）
	srcSigs            sync.Map          // 源图路径 → dedup.Signature（洗图质检/裂变择优的惰性缓存）
	fissionMu          sync.Mutex
	fissionHashes      []uint64 // 本轮裂变已保存结果的 63 位 pHash，供多候选择优避碰
	dataRoot           string
	active             map[int64]bool
	manual             map[int64]bool
	manualWake         chan struct{}
	liveConcurrency    map[string]int // 运行中动态调整的每平台并发上限（<=0 表示自动默认）
	dlFetch            *download.Fetcher // 结果图改用直接下载时共用：站点并发和限速按这一次运行算

	loginProbe     func(context.Context, store.Browser) (string, error)
	loginFocus     func(context.Context, store.Browser) error
	sliderFn       func(context.Context, store.Browser) error
	onHeal         func()
	onRecover      func(healAction, int64)
	loginLast      map[int64]time.Time
	loginPrev      map[int64]string
	attention      string
	attentionID    int64
	notes          []string
	wake           chan struct{}
	wakes          []chan struct{}
	gap            func(time.Duration) time.Duration // 测试把发送间隔钉死。空则 base±随机
	deleteConvFn   func(context.Context, string, *cdp.Session, string) error
	convIDFn       func(*cdp.Session) string
	noAccountNoted bool

	liveMu      sync.Mutex
	clock       laneClock
	acctMeta    map[int64]accountInfo
	starts      sync.Map // task index → time.Time
	activeSched *slotSched
}

type accountInfo struct {
	name string
	port int
}

// NewEngine 创建引擎（solver 可后置注入）。
func NewEngine(st *store.Store, bm *browser.Manager, solver captcha.Solver, dataRoot string) *Engine {
	return &Engine{
		store:    st,
		browsers: bm,
		solver:   solver,
		sessions: map[string]*cdp.Session{},
		dataRoot: dataRoot,
	}
}

// AccountBusy 报告这个账号是否正在出图。读额度要避开这段时间。
func (e *Engine) AccountBusy(id int64) bool {
	return e.accountBusy(id)
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
	return e.run(ctx, opts, onTask, onProgress, nil)
}

// RunObserved 和 Run 走同一条调度。attempt 换成测试里的页面操作，用来看任务落到哪个账号。
func (e *Engine) RunObserved(ctx context.Context, opts Options, onTask func(Task), onProgress func(Progress), attempt func(context.Context, *Task) error) error {
	return e.run(ctx, opts, onTask, onProgress, attempt)
}

// Retry 只重跑失败、跳过或待验证的任务。提示词沿用已经拼好的正文。
func (e *Engine) Retry(ctx context.Context, opts Options, failed []Task, onTask func(Task), onProgress func(Progress)) error {
	return e.retry(ctx, opts, failed, onTask, onProgress, nil)
}

func (e *Engine) retry(ctx context.Context, opts Options, failed []Task, onTask func(Task), onProgress func(Progress), attempt func(context.Context, *Task) error) error {
	for _, task := range failed {
		if supportsOutputSize(task.Platform) {
			if err := opts.OutputSize.Validate(); err != nil {
				return fmt.Errorf("%s输出尺寸: %w", task.Platform, err)
			}
			break
		}
	}
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

func missingPlatform(platform string) error {
	return fmt.Errorf("平台[%s]没有配置浏览器，请先在浏览器管理中添加", platform)
}

func (e *Engine) run(ctx context.Context, opts Options, onTask func(Task), onProgress func(Progress), attempt func(context.Context, *Task) error) error {
	if opts.Fission {
		validImages := len(opts.Images) == 1
		if opts.FissionTextOnly {
			validImages = len(opts.Images) == 0
		}
		if !validImages || len(opts.Prompts) != 4 || len(opts.Platforms) != 1 {
			if opts.FissionTextOnly {
				return fmt.Errorf("纯提示词裂变需要四个方向和一个服务商")
			}
			return fmt.Errorf("裂变图片需要一张母图、四个方向和一个服务商")
		}
	}
	for _, platform := range opts.Platforms {
		if supportsOutputSize(platform) {
			if err := opts.OutputSize.Validate(); err != nil {
				return fmt.Errorf("%s输出尺寸: %w", platform, err)
			}
			break
		}
	}
	e.mu.Lock()
	if err := e.claimRun(); err != nil {
		e.mu.Unlock()
		return err
	}
	// 按平台列出已配置的账号。
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

// claimRun 在持有 e.mu 时占用引擎。任务已在跑或正在退出时返回错误，锁仍由调用方释放。
func (e *Engine) claimRun() error {
	if e.closing {
		return fmt.Errorf("应用正在退出")
	}
	if e.running {
		return fmt.Errorf("任务已在执行中")
	}
	for key, sess := range e.sessions {
		if sess.Ctx().Err() != nil {
			delete(e.sessions, key)
		}
	}
	return nil
}

// driveLocked 执行已经建好的队列。调用时持有 e.mu。
func (e *Engine) driveLocked(ctx context.Context, opts Options, queue []Task, onTask func(Task), onProgress func(Progress), attempt func(context.Context, *Task) error) error {
	e.opts = opts
	e.washChats = nil
	e.pendingDeleteChats = nil
	e.dlFetch = newResultFetcher(opts)
	e.starts.Clear()
	e.srcSigs.Clear()
	e.fissionMu.Lock()
	e.fissionHashes = nil
	if opts.Fission && len(opts.FissionKeepPaths) > 0 {
		for _, keep := range opts.FissionKeepPaths {
			if sig, err := dedup.Hash(keep); err == nil && !sig.Flat {
				e.fissionHashes = append(e.fissionHashes, sig.Hash)
			}
		}
	}
	e.fissionMu.Unlock()
	e.noAccountNoted = false
	e.view = NewQueueView(queue)
	// 源图哈希改为惰性流式计算，避免大批量任务启动前同步读取全部源图。
	e.srcHashes = nil
	e.srcHashCache.Clear()
	e.liveConcurrency = nil
	e.clearAccountMeta()
	ctx, cancel := context.WithCancel(ctx)
	nudge := make(chan struct{}, 1)
	done := make(chan struct{})
	e.cancel = cancel
	e.running = true
	e.runDone = done
	e.runBrowsers = make(map[int64]bool)
	e.wake = nudge
	e.washBatchKey = strconv.FormatInt(time.Now().UnixNano(), 36)
	e.mu.Unlock()
	started := time.Now()
	e.beginLanes()

	defer func() {
		cancel()
		e.closeRunBrowsers()
		e.finishLanes()
		e.clearAccountMeta()
		logging.Event(logging.Line{Stage: "queue", Msg: "任务结束", Elapsed: time.Since(started)})
		e.mu.Lock()
		e.running = false
		if e.runDone == done {
			e.runDone = nil
		}
		e.washChats = nil
		e.pendingDeleteChats = nil
		e.liveConcurrency = nil
		e.srcHashCache.Clear()
		e.srcSigs.Clear()
		e.cancel = nil
		if e.wake == nudge {
			e.wake = nil
		}
		close(done)
		e.mu.Unlock()
	}()

	logging.SetRun(started.Format("20060102T150405.000"))
	logging.Event(logging.Line{Stage: "queue", Msg: fmt.Sprintf("任务开始：共 %d 个", len(queue)), Images: len(queue)})
	if onProgress == nil {
		onProgress = func(Progress) {}
	}
	onProgress(Progress{Done: 0, Total: len(queue)})
	runErr := e.runPlatforms(ctx, nudge, queue, onTask, onProgress, attempt)
	// 批次正常结束后，清理复用的豆包洗图会话历史（仅保留在未取消且全部成功时）。
	e.flushPendingAutoDelete(ctx)
	// 跑完或停下时，还排着的都改成已停止。界面和计数共用这一份状态。
	e.stopLeftovers()
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

// SetConcurrency 在任务运行中动态调整单平台并发上限，立即唤醒调度器生效。
// limit <= 0 表示自动模式默认上限（PlatformInFlight）；正数限制在 [1, MaxPlatformInFlight]。
func (e *Engine) SetConcurrency(platform string, limit int) {
	if e == nil || strings.TrimSpace(platform) == "" {
		return
	}
	if limit > MaxPlatformInFlight {
		limit = MaxPlatformInFlight
	}
	if limit < 0 {
		limit = 0
	}
	e.mu.Lock()
	if e.liveConcurrency == nil {
		e.liveConcurrency = map[string]int{}
	}
	e.liveConcurrency[platform] = limit
	sched := e.activeSched
	e.mu.Unlock()
	if sched != nil {
		sched.ping()
	}
	e.Wake()
}

// concurrencyLimits 返回该平台的单平台上限与全局并发上限（0 表示不设全局上限）。
// 仅在未设置运行期单平台覆盖且初始 Concurrency > 0 时启用全局上限（例如手动单并发跨平台串行）。
func (e *Engine) concurrencyLimits(platform string) (platLimit, globalLimit int) {
	if e == nil {
		return PlatformInFlight, 0
	}
	e.mu.Lock()
	if e.liveConcurrency != nil {
		if v, ok := e.liveConcurrency[platform]; ok {
			e.mu.Unlock()
			if v > 0 {
				return min(v, MaxPlatformInFlight), 0
			}
			return PlatformInFlight, 0
		}
	}
	c := e.opts.Concurrency
	e.mu.Unlock()
	if c > 0 {
		lim := min(c, MaxPlatformInFlight)
		return lim, lim
	}
	return PlatformInFlight, 0
}

func (e *Engine) platformLimit(platform string) int {
	platLimit, _ := e.concurrencyLimits(platform)
	return platLimit
}

func (e *Engine) hasLoginProbe() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	has := e.loginProbe != nil
	e.mu.Unlock()
	return has
}

func (e *Engine) refreshAccountMeta(rows []store.Browser) {
	if e == nil {
		return
	}
	meta := make(map[int64]accountInfo, len(rows))
	for _, b := range rows {
		meta[b.ID] = accountInfo{name: accountLabel(b), port: b.Port}
	}
	e.liveMu.Lock()
	e.acctMeta = meta
	e.liveMu.Unlock()
}

func (e *Engine) clearAccountMeta() {
	if e == nil {
		return
	}
	e.liveMu.Lock()
	e.acctMeta = nil
	e.liveMu.Unlock()
}

// slotKey 是调度名额。每个受管浏览器账号独立使用自己的 ID，负数占位 ID 同样保持原值。
func (e *Engine) slotKey(id int64) int64 {
	return id
}

// pickIdleAccount 优先使用当日已用次数较多的健康空闲账号（集中耗尽单号日额度后再切下一号）。
func (e *Engine) pickIdleAccount(platform string, preferred, avoid int64, busy map[int64]bool, readyAt map[int64]time.Time, now time.Time) (int64, bool) {
	return e.pickIdleAccountExcept(platform, preferred, avoid, busy, readyAt, now, nil, false)
}

func (e *Engine) pickIdleAccountExcept(platform string, preferred, avoid int64, busy map[int64]bool, readyAt map[int64]time.Time, now time.Time, excluded map[int64]bool, preferFirst bool) (int64, bool) {
	if e == nil || e.store == nil {
		return 0, false
	}
	return e.pickIdleAccountFromRows(e.accountsFor(platform), platform, preferred, avoid, busy, readyAt, nil, nil, nil, now, excluded, preferFirst)
}

func exitLoadFor(exitKey string, busyExitCount map[string]int) int {
	if exitKey == "" || len(busyExitCount) == 0 {
		return 0
	}
	return busyExitCount[exitKey]
}

// pickIdleAccountFromRows 使用已缓存的平台账号快照择优挑选空闲账号：
// 1. 过滤忙碌、发送间隔中以及同出口错峰/风控退避中的账号，避免共享出口队头阻塞独立代理账号；
// 2. 仍优先用当日已用次数更高的账号；同额度且均非首选账号时，优先选择当前负载更低的出口以提升并发与降低风控。
func (e *Engine) pickIdleAccountFromRows(
	rows []store.Browser,
	platform string,
	preferred, avoid int64,
	busy map[int64]bool,
	readyAt map[int64]time.Time,
	exitKeys map[int64]string,
	exitBlocked func(exitKey string, accountID int64) bool,
	busyExitCount map[string]int,
	now time.Time,
	excluded map[int64]bool,
	preferFirst bool,
) (int64, bool) {
	if e == nil || len(rows) == 0 {
		return 0, false
	}
	capN := e.capFor(platform)
	var best *store.Browser
	bestUsed := -1
	bestExitLoad := 0
	for i := range rows {
		b := rows[i]
		if b.ID == avoid || excluded[b.ID] || !openAccount(b, now, capN) {
			continue
		}
		slot := b.ID
		if busy[slot] || now.Before(readyAt[slot]) {
			continue
		}
		exitKey := exitKeys[b.ID]
		if exitKey != "" && exitBlocked != nil && exitBlocked(exitKey, b.ID) {
			continue
		}
		if preferFirst && b.ID == preferred {
			return b.ID, true
		}
		used := b.UsedToday(now)
		exitLoad := exitLoadFor(exitKey, busyExitCount)
		better := best == nil || used > bestUsed
		if !better && used == bestUsed {
			if b.ID == preferred {
				better = true
			} else if best.ID != preferred {
				if exitLoad != bestExitLoad {
					better = exitLoad < bestExitLoad
				} else {
					better = b.LastUsed < best.LastUsed || (b.LastUsed == best.LastUsed && b.ID < best.ID)
				}
			}
		}
		if better {
			row := b
			best = &row
			bestUsed = used
			bestExitLoad = exitLoad
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

// Stop 停止任务。
func (e *Engine) Stop() {
	if e == nil {
		return
	}
	e.mu.Lock()
	cancel := e.cancel
	e.mu.Unlock()
	if cancel != nil {
		cancel()
		logging.Log.Info("已请求停止任务…")
	}
}

// StopAndWait 请求停止当前任务并等待其完全退出。
func (e *Engine) StopAndWait() {
	if e == nil {
		return
	}
	e.mu.Lock()
	cancel := e.cancel
	done := e.runDone
	e.mu.Unlock()
	if cancel != nil {
		cancel()
		logging.Log.Info("已请求停止任务…")
	}
	if done != nil {
		<-done
	}
}

// Shutdown 在应用退出时按序停止任务、禁止再起空闲定时器并关闭全部会话。
func (e *Engine) Shutdown() {
	if e == nil {
		return
	}
	e.mu.Lock()
	e.closing = true
	e.mu.Unlock()
	e.StopAndWait()
	e.CloseSessions()
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
		if t.BrowserID > 0 {
			t.ranOn = t.BrowserID
		}
		err := attempt(ctx, &t)
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
			t.Status = StatusSkipped
			t.Error = "已停止"
			return t
		}
		// 点击后的错误可能同时含网络、验证码或额度信息，先守住不能重发的边界。
		if errors.Is(err, strategy.ErrSubmissionUncertain) {
			t.Status = StatusFailed
			t.Error = err.Error()
			if endedByTimeout(ctx, err) {
				t.Error = "出图超时；" + err.Error()
			}
			return t
		}
		if errors.Is(err, errHumanGaveUp) {
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
		if err == nil {
			t.Status = StatusSuccess
			t.Error = ""
			return t
		}
		if len(t.batch) > 1 && t.submitted {
			if kind := classifyFault(err); kind == faultQuota || kind == faultRate {
				id := t.ranOnOr(t.BrowserID)
				e.noteFault(id, kind, err, time.Now())
				e.announceAccount(id, kind)
			}
			t.Status = StatusFailed
			if t.washWaitTimedOut {
				t.Error = "豆包已确认接收本批，等待生成结果超时，请在原对话核对结果后再重试"
				return t
			}
			msg := err.Error()
			if strings.Contains(msg, "未按源图保存") || strings.Contains(msg, "已按源图关联保存") || strings.Contains(msg, "未匹配结果保存在 ") || isModeration(err) || classifyFault(err) != faultNone || errors.Is(err, strategy.ErrSubmissionUncertain) || errors.Is(err, strategy.ErrPlatformReply) {
				t.Error = msg
			} else {
				t.Error = "本批已发送或发送状态未确认，请检查豆包结果后再重试：" + msg
			}
			return t
		}
		if errors.Is(err, strategy.ErrPlatformReply) {
			t.submitted = false
			t.sendSeq++
			t.sentURL, t.sentWrap = "", ""
			name := e.accountName(t.ranOnOr(t.BrowserID))
			if !t.replyRetried {
				t.replyRetried = true
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
		if e.maybeRotate(&t, err, time.Now()) {
			return t
		}
		if t.submitted && isModeration(err) && !errors.Is(err, strategy.ErrModeration) {
			t.Status, t.Error = StatusFailed, err.Error()
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
		if kind == faultQuota || kind == faultRate || kind == faultAuth {
			t.Status = StatusFailed
			return t
		}
		prevSlot := e.slotKey(t.slot)
		switched := ""
		if kind == faultNone && t.Attempts < maxAttempts && e.rotateOnFailure(&t, time.Now()) {
			switched = e.accountName(t.BrowserID)
		}
		if switched != "" {
			logging.Log.Warning("#%d 第 %d 次尝试失败: %s，改用「%s」", t.Index, t.Attempts, err, switched)
		} else {
			logging.Log.Warning("#%d 第 %d 次尝试失败: %s", t.Index, t.Attempts, err)
		}
		if t.Attempts >= maxAttempts {
			break
		}
		if switched != "" && t.slot != 0 && e.slotKey(t.BrowserID) != prevSlot {
			// 立即释放失败账号与并发名额，把 3 秒退避交给调度器按 retryAfter 非阻塞计时。
			t.rotated = true
			t.Status = StatusPending
			t.Reassigned = switched
			t.ReassignWhy = "失败换号"
			t.pauseNote = "rotate"
			t.retryAfter = time.Now().Add(3 * time.Second)
			return t
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

// setTaskPhase 更新运行中任务的执行子阶段并刷新队列快照，供前端在真正开始网页生图后再播放点阵动效。
func (e *Engine) setTaskPhase(t *Task, phase Phase) {
	if e == nil || t == nil || t.Phase == phase {
		return
	}
	t.Phase = phase
	if e.view != nil && t.Index > 0 {
		e.view.UpdateBatch(batchMembers(*t))
	}
	e.notify()
}

// attempt 单次尝试：会话准备 → 上传 → 发送 → 等结果 → 保存。
func (e *Engine) attempt(ctx context.Context, t *Task) (attemptErr error) {
	if !t.submitted && !t.everSent {
		t.noUse = true
	}
	if t.submitted {
		e.setTaskPhase(t, PhaseGenerating)
	} else {
		e.setTaskPhase(t, PhasePreparing)
	}
	strat := strategy.Factory(t.Platform)
	if strat == nil {
		return fmt.Errorf("不支持的平台 %s", t.Platform)
	}
	if doubao, ok := strat.(*strategy.Doubao); ok && len(t.batch) > 1 {
		cleanup, err := doubao.PrepareWashBatch(t.Images, washBatchUploadNames(e.washBatchPrefix(t), t.Images))
		if err != nil {
			return fmt.Errorf("准备豆包洗图附件: %w", err)
		}
		defer cleanup()
	}
	if t.ranOn <= 0 {
		t.ranOn = t.BrowserID
	}
	browserAt := time.Now()
	sess, err := e.session(ctx, t.Platform, t.BrowserID, strat)
	e.tracePhase(t, "browser", "浏览器就绪", time.Since(browserAt), 0, err)
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
				attemptErr = fmt.Errorf("%w", errHealCrashed)
			}
		}
	}()
	if e.opts.Wash && t.Platform == "豆包" && e.opts.WaitResult {
		defer func() {
			e.finishWashChat(sess, len(t.Images), attemptErr == nil && t.submitted && ctx.Err() == nil)
			e.maybeAutoDeleteHistory(work, t, sess, attemptErr)
		}()
	} else {
		defer func() {
			e.maybeAutoDeleteHistory(work, t, sess, attemptErr)
		}()
	}
	// 发送前等待（等待页面/登录就绪）
	if !t.submitted && e.opts.WaitBeforeSend > 0 {
		select {
		case <-time.After(time.Duration(e.opts.WaitBeforeSend) * time.Second):
		case <-work.Done():
			if ctx.Err() != nil {
				return fmt.Errorf("已停止")
			}
			return fmt.Errorf("%w", errHealCrashed)
		}
	}
	if !t.submitted {
		if row, ok := e.browserByID(t.BrowserID); ok {
			if state, stateErr := sess.LoginState(); stateErr == nil {
				switch state {
				case "out":
					e.applyLogin(row, loginStateOut)
					return fmt.Errorf("未登录")
				case "challenge":
					e.applyLogin(row, loginStateChallenge)
					return errNeedsVerify
				case "in":
					e.applyLogin(row, loginStateIn)
				}
			}
		}
	}
	submitAt := time.Now()
	if err := e.submitTask(work, t, strat, sess); err != nil {
		e.tracePhase(t, "submit", "上传并发送", time.Since(submitAt), len(t.Images), err)
		return err
	}
	e.tracePhase(t, "submit", "上传并发送", time.Since(submitAt), len(t.Images), nil)
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
		strategy.FocusQwenChatFor(sess, t.Platform, t.sentURL, t.sentWrap)
		t.submitted = true
		e.setTaskPhase(t, PhaseGenerating)
		return nil
	}
	if t.submitted {
		e.setTaskPhase(t, PhaseGenerating)
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
	reuse := e.reuseWashChat(t, sess)
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
	if reuse {
		sess.ResetChat()
		logging.Log.Debugf("#%d 复用已完成的豆包洗图对话", t.Index)
	} else {
		if sess != nil {
			e.deletePendingWashChat(sess.Ctx(), sess)
		}
		if err := strategy.FreshChat(sess); err != nil {
			return fmt.Errorf("新对话: %w", err)
		}
	}
	// 清理上个任务残留
	if err := strat.CleanComposer(sess); err != nil {
		return fmt.Errorf("清理输入区: %w", err)
	}
	prompt, targetRatio := e.applyOutputRatio(t, strat, sess)
	// 上传源图
	if len(t.Images) > 0 {
		e.setTaskPhase(t, PhaseUploading)
		if err := strat.UploadFiles(sess, t.Images); err != nil {
			return fmt.Errorf("上传: %w", err)
		}
		e.traceTask(t, "upload", fmt.Sprintf("#%d 已上传 %d 张源图", t.Index, len(t.Images)), len(t.Images), 0, nil)
		// 上传参考图后页面可能重绘输入区工具栏（豆包/千问/千问国际），再确认一次图像模式与原生比例（已一致时零点击秒过）。
		if targetRatio != "" {
			if ensurer, ok := strat.(interface{ EnsureImageMode(*cdp.Session) error }); ok {
				if err := ensurer.EnsureImageMode(sess); err != nil {
					return fmt.Errorf("确认图像模式: %w", err)
				}
			}
			if setter, ok := strat.(interface {
				SetOutputRatio(*cdp.Session, string) (bool, string)
			}); ok {
				if applied, why := setter.SetOutputRatio(sess, targetRatio); applied {
					if e.opts.OutputSize.NativeRatio(t.Platform) == targetRatio {
						prompt = strings.TrimSpace(t.Prompt)
					}
				} else if why != "" {
					logging.Log.Debugf("#%d 上传源图后%s原生比例未恢复 %s（%s）", t.Index, t.Platform, targetRatio, why)
				}
			}
		}
	}
	// 上传预览就绪后再标记，避免把源图回显当作生成结果。
	if err := strat.MarkTask(sess); err != nil {
		return err
	}
	if reuse && len(t.batch) <= 1 {
		if err := strategy.MarkDoubaoTurn(sess, prompt); err != nil {
			return err
		}
	}
	// 发送提示词
	if err := strat.SendPrompt(sess, prompt); err != nil {
		if adoptSentChat(t, sess) {
			t.submitted = true
			t.everSent = true
			t.noUse = false
			t.sentAt = time.Now()
			e.setTaskPhase(t, PhaseGenerating)
			return nil
		}
		if errors.Is(err, strategy.ErrSubmissionUncertain) {
			t.everSent = true
			t.noUse = false
		} else if !t.everSent {
			t.noUse = true
		}
		return fmt.Errorf("发送: %w", err)
	}
	adoptSentChat(t, sess)
	t.submitted = true
	t.everSent = true
	t.noUse = false
	t.sentAt = time.Now()
	e.setTaskPhase(t, PhaseGenerating)
	e.traceTask(t, "submit", fmt.Sprintf("#%d %s 已提交", t.Index, e.accountName(t.BrowserID)), 0, 0, nil)
	return nil
}

// applyOutputRatio 在网页端设置平台原生「比例」控件，并返回发送用提示词与期望的原生比例（供上传源图后复核）。
func (e *Engine) applyOutputRatio(t *Task, strat strategy.Strategy, sess *cdp.Session) (string, string) {
	prompt := e.opts.OutputSize.prompt(t.Platform, t.Prompt)
	setter, ok := strat.(interface {
		SetOutputRatio(*cdp.Session, string) (bool, string)
	})
	if !ok {
		return prompt, ""
	}
	if ratio := e.opts.OutputSize.NativeRatio(t.Platform); ratio != "" {
		if applied, why := setter.SetOutputRatio(sess, ratio); applied {
			prompt = strings.TrimSpace(t.Prompt)
			logging.Log.Info("#%d 已设置%s原生比例 %s", t.Index, t.Platform, ratio)
		} else if why != "" {
			logging.Log.Info("#%d %s原生比例未设置（%s），改用提示词比例", t.Index, t.Platform, why)
		}
		if supportsOutputSize(t.Platform) {
			return prompt, ratio
		}
		return prompt, ""
	}
	if t.Platform == "豆包" {
		if rw, rh, err := e.opts.OutputSize.ratio(); err == nil && rw > 0 {
			if closest := ClosestNativeRatio(rw, rh); closest != "" {
				if applied, why := setter.SetOutputRatio(sess, closest); applied {
					logging.Log.Info("#%d 已按目标比例最接近的豆包原生比例设置 %s", t.Index, closest)
				} else if why != "" {
					logging.Log.Debugf("#%d 豆包原生比例未设置（%s）", t.Index, why)
				}
				return prompt, closest
			}
		}
		if applied, why := setter.SetOutputRatio(sess, "自动"); applied {
			logging.Log.Debugf("#%d 豆包原生比例已确认自动", t.Index)
		} else if why != "" {
			logging.Log.Debugf("#%d 豆包原生比例重置自动未生效（%s）", t.Index, why)
		}
		return prompt, "自动"
	}
	if t.Platform != "千问" && t.Platform != "千问国际" {
		return prompt, ""
	}
	if rw, rh, err := e.opts.OutputSize.ratio(); err == nil && rw > 0 {
		if closest := ClosestNativeRatio(rw, rh); closest != "" {
			if applied, why := setter.SetOutputRatio(sess, closest); applied {
				logging.Log.Info("#%d 已按目标比例最接近的%s原生比例设置 %s", t.Index, t.Platform, closest)
			} else if why != "" {
				logging.Log.Debugf("#%d %s原生比例未设置（%s）", t.Index, t.Platform, why)
			}
			return prompt, closest
		}
		return prompt, ""
	}
	if t.Platform == "千问" {
		if ratio := strings.TrimSpace(e.opts.QianwenOutputRatio); ratio != "" {
			if applied, why := setter.SetOutputRatio(sess, ratio); applied {
				logging.Log.Info("#%d 已设置千问原生比例 %s", t.Index, ratio)
			} else if why != "" {
				logging.Log.Debugf("#%d 千问原生比例未设置（%s）", t.Index, why)
			}
			return prompt, ratio
		}
	}
	if len(t.Images) > 0 {
		if ratio := closestImageNativeRatio(t.Images[0]); ratio != "" {
			if applied, why := setter.SetOutputRatio(sess, ratio); applied {
				logging.Log.Info("#%d 已按源图画幅设置%s原生比例 %s", t.Index, t.Platform, ratio)
			} else if why != "" {
				logging.Log.Debugf("#%d %s原生比例未设置（%s）", t.Index, t.Platform, why)
			}
			return prompt, ratio
		}
		return prompt, ""
	}
	// 纯文生图避免继承上一单发送前失败遗留在空白页的横屏/方图比例，回正至默认比例（千问 3:4，千问国际 16:9）。
	defaultRatio := "3:4"
	if t.Platform == "千问国际" {
		defaultRatio = "16:9"
	}
	if applied, why := setter.SetOutputRatio(sess, defaultRatio); applied {
		logging.Log.Debugf("#%d %s文生图原生比例已确认 %s", t.Index, t.Platform, defaultRatio)
	} else if why != "" {
		logging.Log.Debugf("#%d %s文生图原生比例重置未生效（%s）", t.Index, t.Platform, why)
	}
	return prompt, defaultRatio
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
	if t != nil && (t.Platform == "千问" || t.Platform == "千问国际") && t.Attempts > 1 && d < qianwenRetryWait {
		return qianwenRetryWait
	}
	return d
}

func (e *Engine) waitAndSave(ctx context.Context, t *Task, strat strategy.Strategy, sess *cdp.Session) error {
	budget := e.resultWaitFor(t)
	var used, waited, savedFor time.Duration
	var waitedN, savedN int
	var waitErrSeen, saveErrSeen error
	lastProgress := time.Now()
	defer func() {
		if waited > 0 || waitErrSeen != nil {
			e.tracePhase(t, "generate", "等待生成", waited, waitedN, waitErrSeen)
		}
		if savedFor > 0 || saveErrSeen != nil {
			e.tracePhase(t, "save", "保存结果", savedFor, savedN, saveErrSeen)
		}
	}()
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
		spent := time.Since(start)
		used += spent
		waited += spent
		waitedN = len(results)
		waitErrSeen = waitErr
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
			t.washWaitTimedOut = errors.Is(waitErr, strategy.ErrWashWaitTimeout)
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
		saveAt := time.Now()
		if len(results) > 0 {
			e.setTaskPhase(t, PhaseDownloading)
		}
		if len(t.batch) > 1 {
			indices := make([]int, len(results))
			for i, result := range results {
				indices[i] = result.SourceIndex
			}
			saveErrSeen = e.saveWashResults(ctx, sess, t, results, indices, waitErr)
			savedFor += time.Since(saveAt)
			savedN = len(t.Results)
			return saveErrSeen
		}
		saved, err := e.saveResults(ctx, t, strat, sess, results)
		savedFor += time.Since(saveAt)
		t.Results = append(t.Results, saved...)
		savedN += len(saved)
		if err = errors.Join(waitErr, err); err != nil {
			saveErrSeen = err
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(saved) > 0 || t.matchedSavedResult {
			return nil
		}
		e.setTaskPhase(t, PhaseGenerating)
		if time.Since(lastProgress) >= stallAfter {
			return fmt.Errorf("%w", errHealStuck)
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
	waitErrSeen = fmt.Errorf("出图超时，没有可保存的图片")
	return waitErrSeen
}

// browsersOfPlatforms 返回每个平台可用的受管浏览器。
func (e *Engine) browsersOfPlatforms() map[string][]int64 {
	browsersOf := map[string][]int64{}
	if e.store == nil {
		return browsersOf
	}
	for _, b := range e.store.Browsers() {
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
	b, found := e.browserByID(browserID)
	backend := browser.CanonicalBackend(b.Engine)
	key := fmt.Sprintf("%s:%s:%d", backend, platform, browserID)
	e.mu.Lock()
	if s, ok := e.sessions[key]; ok {
		if e.runBrowsers != nil {
			e.runBrowsers[browserID] = true
		}
		e.mu.Unlock()
		return s, nil
	}
	solver := e.solver
	e.mu.Unlock()

	if e.store == nil || e.browsers == nil {
		return nil, fmt.Errorf("浏览器 %d 不存在", browserID)
	}
	if !found {
		return nil, fmt.Errorf("浏览器 %d 不存在", browserID)
	}
	onLog := func(line string) {
		if len(line) == 0 || len(line) >= 400 {
			return
		}
		if strings.Contains(line, "was preloaded using link preload but not used") ||
			strings.Contains(line, "is ignored when delivered in a report-only policy") {
			return
		}
		logging.Log.Debugf("[页面] %s", line)
	}
	home := ""
	if strat != nil {
		home = strat.HomeURL()
	}
	ws, startErr := e.browsers.StartTask(ctx, b, e.dataRoot, backend, home)
	if startErr != nil {
		return nil, fmt.Errorf("启动浏览器[%s]: %w", b.Name, startErr)
	}
	e.mu.Lock()
	if e.runBrowsers != nil {
		e.runBrowsers[browserID] = true
	}
	e.mu.Unlock()
	client, clientErr := cdp.NewClient(ws)
	if clientErr != nil {
		return nil, clientErr
	}
	sess, err := client.NewBackgroundSession(home, onLog)
	e.traceTask(&Task{Index: 0, Platform: platform, BrowserID: browserID}, "open", "打开页面", 0, 0, err)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("打开页面: %w", err)
	}
	context.AfterFunc(sess.Ctx(), client.Close)
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

func (e *Engine) closeRunBrowsers() {
	e.mu.Lock()
	ids := e.runBrowsers
	e.runBrowsers = nil
	e.mu.Unlock()
	var wg sync.WaitGroup
	for id := range ids {
		wg.Go(func() {
			e.dropSessions(id)
			if e.browsers != nil {
				e.browsers.Stop(id)
				if e.store != nil {
					e.store.SetRunning(id, e.browsers.IsRunning(id))
				}
			}
		})
	}
	wg.Wait()
}

func (e *Engine) accountProxy(id int64) string {
	if row, ok := e.browserByID(id); ok && strings.TrimSpace(row.Proxy) != "" {
		if norm, valid := browser.NormalizeProxy(row.Proxy); valid {
			return norm
		}
	}
	return browser.CurrentProxyServer()
}

func sharedExitKeyFromRows(all []store.Browser, row store.Browser, platform, globalProxy string) string {
	if row.ID <= 0 || len(browser.SamePlatformSharedExit(all, row, globalProxy)) == 0 {
		return ""
	}
	proxy := globalProxy
	if strings.TrimSpace(row.Proxy) != "" {
		if norm, valid := browser.NormalizeProxy(row.Proxy); valid {
			proxy = norm
		}
	}
	key := browser.NormalizeProxyKey(proxy)
	if key == "" {
		key = "direct"
	}
	return platform + "|" + key
}

func buildPlatformExitKeys(all, platformRows []store.Browser, platform string) map[int64]string {
	if len(platformRows) < 2 {
		return nil
	}
	globalProxy := browser.CurrentProxyServer()
	var out map[int64]string
	for _, b := range platformRows {
		if b.Disabled {
			continue
		}
		if key := sharedExitKeyFromRows(all, b, platform, globalProxy); key != "" {
			if out == nil {
				out = make(map[int64]string, len(platformRows))
			}
			out[b.ID] = key
		}
	}
	return out
}

func (e *Engine) sharedExitKey(platform string, id int64) string {
	if e == nil || e.store == nil || id <= 0 {
		return ""
	}
	all := e.store.Browsers()
	row := findBrowser(all, id)
	if row == nil {
		return ""
	}
	return sharedExitKeyFromRows(all, *row, platform, browser.CurrentProxyServer())
}

// sourceHash 按需流式计算并缓存源图 SHA256，用于结果回声过滤。
// 若测试通过 e.srcHashes 预先注入哈希，则优先使用注入值。
func (e *Engine) sourceHash(path string) string {
	if e == nil || strings.TrimSpace(path) == "" {
		return ""
	}
	if e.srcHashes != nil {
		if h, ok := e.srcHashes[path]; ok {
			return h
		}
	}
	if cached, ok := e.srcHashCache.Load(path); ok {
		return cached.(string)
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, f)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return ""
	}
	sum := hex.EncodeToString(h.Sum(nil))
	e.srcHashCache.Store(path, sum)
	return sum
}

func (e *Engine) washBatchPrefix(t *Task) string {
	e.mu.Lock()
	if e.washBatchKey == "" {
		e.washBatchKey = strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	key := e.washBatchKey
	e.mu.Unlock()
	if t == nil {
		return key
	}
	base := key + ":" + strconv.Itoa(t.Index)
	if t.sendSeq <= 0 {
		return base
	}
	return base + ":" + strconv.Itoa(t.sendSeq)
}

func (e *Engine) cacheSourceSignature(path string, img image.Image) {
	if e == nil || img == nil || strings.TrimSpace(path) == "" {
		return
	}
	if _, ok := e.srcSigs.Load(path); ok {
		return
	}
	e.srcSigs.Store(path, dedup.PerceptualHashImage(img))
}

func (e *Engine) sourceSignature(path string) *dedup.Signature {
	if e == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	if cached, ok := e.srcSigs.Load(path); ok {
		sig := cached.(dedup.Signature)
		return &sig
	}
	sig, err := dedup.PerceptualHashFile(path)
	if err != nil {
		return nil
	}
	e.srcSigs.Store(path, sig)
	return &sig
}

type failedSession interface {
	Close()
}

func closeFailedSession(ctx context.Context, sess failedSession, err error) error {
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
	if e.active == nil {
		e.active = map[int64]bool{}
	}
	if on {
		e.active[id] = true
	} else {
		delete(e.active, id)
	}
	e.mu.Unlock()
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

func decodeValidRawResult(data []byte) (image.Image, string, bool) {
	if !images.AcceptGenerated(data) {
		return nil, "", false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Height <= 0 || int64(cfg.Width) > maxResultPixels/int64(cfg.Height) {
		return nil, "", false
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", false
	}
	return decoded, format, true
}

func validRawResult(data []byte) bool {
	_, _, ok := decodeValidRawResult(data)
	return ok
}

// prefetchResults 在同一页里并行下载多张结果。单张仍走原来的逐张抓取。
func prefetchResults(strat strategy.Strategy, sess *cdp.Session, results []strategy.Result) [][]byte {
	out := make([][]byte, len(results))
	fetch, ok := strat.(interface {
		FetchResultList(*cdp.Session, []string) [][]byte
	})
	if !ok || sess == nil {
		return out
	}
	var urls []string
	var idx []int
	for i, r := range results {
		if r.Data == nil && r.URL != "" {
			url := r.URL
			if r.RawURL != "" {
				url = r.RawURL
			}
			urls = append(urls, url)
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

// newResultFetcher 按这次运行的设置建结果图下载器。
// 站点并发和限速在整批任务之间共用，不是每张图各算一份。
// 三项设置都没配时不启用兜底，测试和直接调用引擎的地方不会意外发起网络请求。
func newResultFetcher(opts Options) *download.Fetcher {
	if opts.DownloadConcurrency <= 0 && opts.DownloadRetries <= 0 && opts.DownloadRateKBps <= 0 {
		return nil
	}
	conc := opts.DownloadConcurrency
	if conc <= 0 {
		conc = 2
	}
	var band *download.BandwidthLimiter
	if opts.DownloadRateKBps > 0 {
		band = download.NewBandwidthLimiter(int64(opts.DownloadRateKBps) * 1024)
	}
	return &download.Fetcher{
		Getter:    download.NewHTTPGetter(browser.CurrentProxyServer()),
		Host:      download.NewHostLimiter(conc),
		Bandwidth: band,
		Retries:   opts.DownloadRetries,
	}
}

// fetchViaDownload 是不经过页面、直接按地址取结果的兜底路径。
func (e *Engine) fetchViaDownload(ctx context.Context, rawURL string) ([]byte, error) {
	e.mu.Lock()
	fetcher := e.dlFetch
	if fetcher == nil {
		fetcher = newResultFetcher(e.opts)
		e.dlFetch = fetcher
	}
	e.mu.Unlock()
	if fetcher == nil {
		return nil, errors.New("下载设置没有启用")
	}
	return fetcher.FetchBytes(ctx, rawURL, nil)
}

type preparedResult struct {
	index    int
	result   strategy.Result
	data     []byte
	decoded  image.Image
	format   string
	cfg      image.Config
	hash     string
	usingRaw bool
}

func (e *Engine) selectAndReserveFissionCandidate(sigs []fissionCandidate, motherSig *dedup.Signature) (int, uint64, bool) {
	e.fissionMu.Lock()
	defer e.fissionMu.Unlock()
	chosen := pickBestFissionCandidate(sigs, e.fissionHashes, motherSig)
	if chosen < 0 {
		chosen = 0
	}
	for _, s := range sigs {
		if s.index == chosen {
			if !s.sig.Flat {
				e.fissionHashes = append(e.fissionHashes, s.sig.Hash)
				return chosen, s.sig.Hash, true
			}
			break
		}
	}
	return chosen, 0, false
}

func (e *Engine) reserveFissionHash(decoded image.Image) (uint64, bool) {
	if decoded == nil {
		return 0, false
	}
	sig := dedup.PerceptualHashImage(decoded)
	if sig.Flat {
		return 0, false
	}
	e.fissionMu.Lock()
	e.fissionHashes = append(e.fissionHashes, sig.Hash)
	e.fissionMu.Unlock()
	return sig.Hash, true
}

func (e *Engine) releaseFissionHash(hash uint64) {
	e.fissionMu.Lock()
	defer e.fissionMu.Unlock()
	for i := len(e.fissionHashes) - 1; i >= 0; i-- {
		if e.fissionHashes[i] == hash {
			e.fissionHashes = append(e.fissionHashes[:i], e.fissionHashes[i+1:]...)
			return
		}
	}
}

func (e *Engine) prepareResultCandidate(ctx context.Context, i int, r strategy.Result, batched [][]byte, t *Task, strat strategy.Strategy, sess *cdp.Session) (*preparedResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data := r.Data
	decoded := r.Decoded
	format := r.Format
	var err error
	usingRaw := false
	if data == nil && i < len(batched) && batched[i] != nil {
		data = batched[i]
		if r.RawURL != "" {
			if img, fmtName, ok := decodeValidRawResult(data); ok {
				usingRaw, decoded, format = true, img, fmtName
			} else {
				data = nil
			}
		}
	}
	if data == nil && r.RawURL != "" {
		if fetch, ok := strat.(interface {
			FetchRawResultBytes(*cdp.Session, string) ([]byte, error)
		}); ok {
			data, err = fetch.FetchRawResultBytes(sess, r.RawURL)
		} else {
			data, err = strat.FetchResultBytes(sess, r.RawURL)
		}
		if err == nil {
			if img, fmtName, ok := decodeValidRawResult(data); ok {
				usingRaw, decoded, format = true, img, fmtName
			} else {
				data = nil
			}
		}
	}
	if data == nil {
		data, err = strat.FetchResultBytes(sess, r.URL)
	}
	if data == nil && ctx.Err() == nil {
		// 页面内 fetch 拿不到时，用下载引擎按地址再取一次：带重试、站点并发和限速。
		if via, dlErr := e.fetchViaDownload(ctx, r.URL); dlErr == nil {
			data, err = via, nil
			logging.Log.Debugf("候选图 #%d 页面取回失败，改用下载引擎", i+1)
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
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
		return nil, nil
	}
	if !images.IsValidImageBytes(data) {
		if r.Data != nil {
			return nil, fmt.Errorf("网页下载结果 #%d 校验失败: 文件为空或图片格式不支持", i+1)
		}
		logging.Log.Debugf("候选图 #%d 不是有效图片", i+1)
		return nil, nil
	}
	var cfg image.Config
	if decoded != nil && format != "" {
		b := decoded.Bounds()
		cfg = image.Config{Width: b.Dx(), Height: b.Dy()}
	} else {
		cfg, format, err = image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			if r.Data != nil {
				return nil, fmt.Errorf("网页下载结果 #%d 校验失败: 无法解析图片格式: %w", i+1, err)
			}
			logging.Log.Debugf("候选图 #%d 无法解析图片格式", i+1)
			return nil, nil
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width) > maxResultPixels/int64(cfg.Height) {
			if r.Data != nil {
				return nil, fmt.Errorf("网页下载结果 #%d 校验失败: 图片尺寸超出上限: %dx%d", i+1, cfg.Width, cfg.Height)
			}
			logging.Log.Debugf("候选图 #%d 图片尺寸超出上限: %dx%d", i+1, cfg.Width, cfg.Height)
			return nil, nil
		}
		decoded, _, err = image.Decode(bytes.NewReader(data))
		if err != nil {
			if r.Data != nil {
				return nil, fmt.Errorf("网页下载结果 #%d 校验失败: 无法完整解码: %w", i+1, err)
			}
			logging.Log.Debugf("候选图 #%d 无法完整解码", i+1)
			return nil, nil
		}
	}
	if len(data) < images.MinGeneratedBytes || cfg.Width < images.MinGeneratedSide || cfg.Height < images.MinGeneratedSide {
		logging.Log.Debugf("候选图 #%d 不是生成图（%d 字节，%dx%d），已忽略", i+1, len(data), cfg.Width, cfg.Height)
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash := images.SHA256(data)
	if t.savedHashes[hash] {
		t.matchedSavedResult = true
		return nil, nil
	}
	for _, source := range t.Images {
		if h := e.sourceHash(source); h != "" && h == hash {
			logging.Log.Debugf("候选图 #%d 与本任务的源图相同", i+1)
			return nil, nil
		}
	}
	return &preparedResult{
		index:    i,
		result:   r,
		data:     data,
		decoded:  decoded,
		format:   format,
		cfg:      cfg,
		hash:     hash,
		usingRaw: usingRaw,
	}, nil
}

// prepareInMemoryCandidates 并行解码已在内存中的多张候选结果（不触碰 CDP 会话或任务可变字段），
// 用于加速豆包 4 图自由生图与裂变多候选的解码/校验阶段。
func (e *Engine) prepareInMemoryCandidates(ctx context.Context, results []strategy.Result, batched [][]byte) ([]*preparedResult, []error, []bool) {
	n := len(results)
	cands := make([]*preparedResult, n)
	errs := make([]error, n)
	handled := make([]bool, n)
	var workIdx []int
	for i, r := range results {
		if r.Data != nil || (i < len(batched) && batched[i] != nil) {
			workIdx = append(workIdx, i)
		}
	}
	if len(workIdx) < 2 {
		return cands, errs, handled
	}
	jobs := make(chan int, len(workIdx))
	for _, i := range workIdx {
		jobs <- i
	}
	close(jobs)
	workers := min(4, max(1, runtime.NumCPU()), len(workIdx))
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					errs[i] = err
					handled[i] = true
					continue
				}
				r := results[i]
				data := r.Data
				decoded := r.Decoded
				format := r.Format
				usingRaw := false
				if data == nil && i < len(batched) && batched[i] != nil {
					data = batched[i]
					if r.RawURL != "" {
						if img, fmtName, ok := decodeValidRawResult(data); ok {
							usingRaw, decoded, format = true, img, fmtName
						} else {
							// RawURL 解码无效时交回主循环走 FetchResultBytes 兜底
							continue
						}
					}
				}
				if data == nil {
					continue
				}
				handled[i] = true
				if !images.IsValidImageBytes(data) {
					if r.Data != nil {
						errs[i] = fmt.Errorf("网页下载结果 #%d 校验失败: 文件为空或图片格式不支持", i+1)
					} else {
						logging.Log.Debugf("候选图 #%d 不是有效图片", i+1)
					}
					continue
				}
				var cfg image.Config
				var err error
				if decoded != nil && format != "" {
					b := decoded.Bounds()
					cfg = image.Config{Width: b.Dx(), Height: b.Dy()}
				} else {
					cfg, format, err = image.DecodeConfig(bytes.NewReader(data))
					if err != nil {
						if r.Data != nil {
							errs[i] = fmt.Errorf("网页下载结果 #%d 校验失败: 无法解析图片格式: %w", i+1, err)
						} else {
							logging.Log.Debugf("候选图 #%d 无法解析图片格式", i+1)
						}
						continue
					}
					if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width) > maxResultPixels/int64(cfg.Height) {
						if r.Data != nil {
							errs[i] = fmt.Errorf("网页下载结果 #%d 校验失败: 图片尺寸超出上限: %dx%d", i+1, cfg.Width, cfg.Height)
						} else {
							logging.Log.Debugf("候选图 #%d 图片尺寸超出上限: %dx%d", i+1, cfg.Width, cfg.Height)
						}
						continue
					}
					decoded, _, err = image.Decode(bytes.NewReader(data))
					if err != nil {
						if r.Data != nil {
							errs[i] = fmt.Errorf("网页下载结果 #%d 校验失败: 无法完整解码: %w", i+1, err)
						} else {
							logging.Log.Debugf("候选图 #%d 无法完整解码", i+1)
						}
						continue
					}
				}
				if len(data) < images.MinGeneratedBytes || cfg.Width < images.MinGeneratedSide || cfg.Height < images.MinGeneratedSide {
					logging.Log.Debugf("候选图 #%d 不是生成图（%d 字节，%dx%d），已忽略", i+1, len(data), cfg.Width, cfg.Height)
					continue
				}
				if err := ctx.Err(); err != nil {
					errs[i] = err
					continue
				}
				cands[i] = &preparedResult{
					index:    i,
					result:   r,
					data:     data,
					decoded:  decoded,
					format:   format,
					cfg:      cfg,
					hash:     images.SHA256(data),
					usingRaw: usingRaw,
				}
			}
		})
	}
	wg.Wait()
	return cands, errs, handled
}

func (e *Engine) filterPreparedCandidate(i int, cand *preparedResult, t *Task) *preparedResult {
	if cand == nil {
		return nil
	}
	if t.savedHashes[cand.hash] {
		t.matchedSavedResult = true
		return nil
	}
	for _, source := range t.Images {
		if h := e.sourceHash(source); h != "" && h == cand.hash {
			logging.Log.Debugf("候选图 #%d 与本任务的源图相同", i+1)
			return nil
		}
	}
	return cand
}

// saveResults 校验 + 去重 + 按日期目录落盘。
func (e *Engine) saveResults(ctx context.Context, t *Task, strat strategy.Strategy, sess *cdp.Session, results []strategy.Result) ([]string, error) {
	t.matchedSavedResult = false
	if e.opts.Fission && len(t.Results) > 0 {
		t.matchedSavedResult = true
		return nil, nil
	}
	var saved []string
	batched := prefetchResults(strat, sess, results)
	preCands, preErrs, preHandled := e.prepareInMemoryCandidates(ctx, results, batched)
	nextCandidate := func(i int, r strategy.Result) (*preparedResult, error) {
		if preHandled[i] {
			if preErrs[i] != nil {
				return nil, preErrs[i]
			}
			return e.filterPreparedCandidate(i, preCands[i], t), nil
		}
		return e.prepareResultCandidate(ctx, i, r, batched, t, strat, sess)
	}
	if e.opts.Fission && len(results) > 1 {
		// 裂变场景且平台单次返回多张候选时（如千问每次返回 2 张），
		// 先收集全部有效候选并按 pHash 与已保存结果/母图的最大结构差异择优保留 1 张。
		var pool []*preparedResult
		var sigs []fissionCandidate
		for i, r := range results {
			cand, err := nextCandidate(i, r)
			if err != nil {
				return saved, err
			}
			if cand == nil {
				continue
			}
			sigs = append(sigs, fissionCandidate{
				index: len(pool),
				sig:   dedup.PerceptualHashImage(cand.decoded),
			})
			pool = append(pool, cand)
		}
		if len(pool) > 0 {
			var motherSig *dedup.Signature
			if len(t.Images) == 1 {
				motherSig = e.sourceSignature(t.Images[0])
			}
			chosen, reservedHash, hasReserved := e.selectAndReserveFissionCandidate(sigs, motherSig)
			if chosen < 0 || chosen >= len(pool) {
				chosen = 0
			}
			if len(pool) > 1 {
				logging.Log.Debugf("#%d 裂变多候选择优：共 %d 张有效候选，选用 #%d", t.Index, len(pool), pool[chosen].index+1)
			}
			path, err := e.writePreparedResult(ctx, t, pool[chosen], hasReserved)
			if path != "" {
				saved = append(saved, path)
			} else if hasReserved {
				e.releaseFissionHash(reservedHash)
			}
			if err != nil {
				return saved, err
			}
		}
	} else {
		for i, r := range results {
			if e.opts.Fission && len(saved) > 0 {
				break
			}
			cand, err := nextCandidate(i, r)
			if err != nil {
				return saved, err
			}
			if cand == nil {
				continue
			}
			var reservedHash uint64
			hasReserved := false
			if e.opts.Fission {
				reservedHash, hasReserved = e.reserveFissionHash(cand.decoded)
			}
			path, err := e.writePreparedResult(ctx, t, cand, hasReserved)
			if path != "" {
				saved = append(saved, path)
			} else if hasReserved {
				e.releaseFissionHash(reservedHash)
			}
			if err != nil {
				return saved, err
			}
		}
	}
	if len(saved) > 0 {
		e.traceTask(t, "save", fmt.Sprintf("#%d 保存 %d 张", t.Index, len(saved)), len(saved), 0, nil)
	}
	return saved, nil
}

func (e *Engine) writePreparedResult(ctx context.Context, t *Task, cand *preparedResult, fissionHashReserved bool) (string, error) {
	i := cand.index
	r := cand.result
	data := cand.data
	decoded := cand.decoded
	format := cand.format
	cfg := cand.cfg
	hash := cand.hash
	usingRaw := cand.usingRaw
	if e.opts.Wash && cfg.Width >= 256 && cfg.Height >= 256 {
		var srcSig *dedup.Signature
		if len(t.Images) == 1 {
			srcSig = e.sourceSignature(t.Images[0])
		}
		if rep := inspectWashQuality(decoded, srcSig); !rep.OK() {
			note := rep.Summary()
			if t.QualityNote == "" {
				t.QualityNote = note
			} else if !strings.Contains(t.QualityNote, note) {
				t.QualityNote += "；" + note
			}
			logging.Log.Warning("#%d 图片 #%d 洗图质检提醒：%s（已正常保存）", t.Index, i+1, note)
		}
	}
	// 单次解码/单次编码管线：去水印、洗图转 PNG 与尺寸调整都在已解码像素上串行完成，最后只编码一次。
	needsEncode := false
	if t.Platform == "豆包" && !usingRaw && !r.WatermarkRemoved && watermarkStaticImage(data, format) {
		repaired, changed, repairErr := images.RemoveWatermark(ctx, decoded, t.Platform)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if repairErr == nil && changed {
			decoded, format, needsEncode = repaired, "png", true
		} else if repairErr != nil {
			logging.Log.Warning("#%d 图片 #%d 去水印失败，保留原图: %s", t.Index, i+1, repairErr)
		} else {
			logging.Log.Warning("#%d 图片 #%d 未匹配到已支持的水印，保留原图", t.Index, i+1)
		}
	}
	targetFormat := format
	ext := "." + format
	if format == "jpeg" {
		ext = ".jpg"
	}
	// 洗图结果统一存 PNG，白底主体图后面还要再加工。
	if e.opts.Wash && targetFormat != "png" {
		targetFormat, ext, needsEncode = "png", ".png", true
	}
	if supportsOutputSize(t.Platform) {
		adjusted, resized, sizeErr := e.opts.OutputSize.processImage(ctx, decoded, targetFormat, cfg)
		if sizeErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			backupData := data
			if needsEncode {
				if encoded, encErr := images.EncodePNG(decoded); encErr == nil {
					backupData = encoded
				}
			}
			backup := e.opts
			backup.OutputPath = filepath.Join(backup.OutputPath, "待核对")
			backupStem, backupStyle := backup.resultName(t.Prompt, resultSource(t))
			original, saveErr := writeNumberedFile(ctx, backup.ResultDir(time.Now()), backupStem, backupStyle, ext, backupData)
			if saveErr != nil {
				return "", errors.Join(fmt.Errorf("调整输出尺寸失败: %w", sizeErr), fmt.Errorf("保留未调整尺寸的图片失败: %w", saveErr))
			}
			return "", fmt.Errorf("调整输出尺寸失败，未调整尺寸的图片保存在 %s: %w", original, sizeErr)
		}
		if resized {
			data, needsEncode = adjusted, false
		}
	}
	if needsEncode {
		encoded, encErr := images.EncodePNG(decoded)
		if encErr != nil {
			return "", fmt.Errorf("结果 #%d 编码 PNG 失败: %w", i+1, encErr)
		}
		data = encoded
	}
	dir := e.opts.ResultDir(time.Now())
	stem, style := e.opts.resultName(t.Prompt, resultSource(t))
	path, err := writeNumberedFile(ctx, dir, stem, style, ext, data)
	if err != nil {
		return "", err
	}
	if e.opts.Fission && !fissionHashReserved {
		e.reserveFissionHash(decoded)
	}
	if t.savedHashes == nil {
		t.savedHashes = make(map[string]bool)
	}
	t.savedHashes[hash] = true
	// 扩展下载已经换成去水印 PNG 时，不再按实验开关另存一份。
	if (t.Platform != "千问" && t.Platform != "千问国际") || !e.opts.RemoveWatermark || r.WatermarkRemoved {
		return path, nil
	}
	if !watermarkStaticImage(data, format) {
		logging.Log.Warning("#%d 图片 #%d 跳过去水印：仅支持静态 PNG/JPEG/WebP，原图已保留", t.Index, i+1)
		return path, nil
	}
	logging.Log.Info("#%d 图片 #%d 正在计算去水印，原图已保留", t.Index, i+1)
	repaired, changed, repairErr := images.RemoveWatermark(ctx, decoded, t.Platform)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return path, ctxErr
	}
	if repairErr != nil {
		logging.Log.Warning("#%d 图片 #%d 去水印失败，保留原图: %s", t.Index, i+1, repairErr)
		return path, nil
	}
	if !changed {
		logging.Log.Warning("#%d 图片 #%d 未匹配到已支持的水印，保留原图", t.Index, i+1)
		return path, nil
	}
	repairData, repairErr := images.EncodePNG(repaired)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return path, ctxErr
	}
	if repairErr == nil {
		repairBase := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + "_去水印"
		var repairPath string
		repairPath, repairErr = writeResultFile(ctx, dir, repairBase, ".png", repairData)
		if errors.Is(repairErr, os.ErrExist) {
			repairPath, repairErr = writeNumberedFile(ctx, dir, repairBase, defaultNumberFmt, ".png", repairData)
		}
		if repairErr == nil {
			path = repairPath
			logging.Log.Success("#%d 图片 #%d 已另存去水印版本，原图已保留", t.Index, i+1)
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return path, ctxErr
	}
	if repairErr != nil {
		logging.Log.Warning("#%d 图片 #%d 去水印版本保存失败，保留原图: %s", t.Index, i+1, repairErr)
	}
	return path, nil
}

// numberFmt 是文件名里序号的拼法。默认「词根_01」，自动编号时前缀直接拼补零序号（前缀001）。
type numberFmt struct {
	sep   string // 词根和序号之间的分隔符
	width int    // 补零位数，0 不补零
	start int    // 起始序号，目录里已有更大的号时让位
}

var defaultNumberFmt = numberFmt{sep: "_", width: 2, start: 1}

// fileSlot 串行分配同一个目录、同一个词根的序号。
// 两个任务同时保存同一个名字时，必须先占住下一个号再写，避免写到同一个文件。
type fileSlot struct {
	mu    sync.Mutex
	next  int
	start int // 上次校准用的起始值；起始值变了就重新对表，保证每次运行都生效
}

var fileSlots sync.Map

func slotFor(dir, stem, sep string) *fileSlot {
	key := dir + "\x00" + stem + "\x00" + sep
	if v, ok := fileSlots.Load(key); ok {
		return v.(*fileSlot)
	}
	v, _ := fileSlots.LoadOrStore(key, &fileSlot{})
	return v.(*fileSlot)
}

// writeNumberedFile 按词根和序号格式写成「词根_01」或「前缀001」……同一天已有的号继续往后排，不覆盖。
func writeNumberedFile(ctx context.Context, dir, stem string, style numberFmt, ext string, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(stem) == "" && style.sep != "" {
		stem = "文生图"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建结果目录失败: %w", err)
	}
	slot := slotFor(dir, stem, style.sep)
	slot.mu.Lock()
	defer slot.mu.Unlock()
	if slot.next < 1 || slot.start != style.start {
		slot.next = highestNumber(dir, stem, style.sep) + 1
		if style.start > slot.next {
			slot.next = style.start
		}
		slot.start = style.start
	}
	for slot.next <= 100000 {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		path := filepath.Join(dir, stem+style.sep+padNumber(slot.next, style.width)+ext)
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

// padNumber 按位数补零。位数是 0 或数字已经不小于位数时原样返回。
func padNumber(n, width int) string {
	value := strconv.Itoa(n)
	if width > len(value) {
		return strings.Repeat("0", width-len(value)) + value
	}
	return value
}

// highestNumber 是目录里按「词根+序号」拼法的最大序号。去水印副本带别的后缀，不占这个号。
func highestNumber(dir, stem, sep string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	prefix := stem + sep
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
	e.mu.Lock()
	for k, s := range e.sessions {
		s.Close()
		delete(e.sessions, k)
	}
	e.mu.Unlock()
	e.srcHashCache.Clear()
	e.srcSigs.Clear()
}
