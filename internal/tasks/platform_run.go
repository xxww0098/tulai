package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

// PlatformInFlight 是豆包、千问自动模式下的默认同时出图上限。
const PlatformInFlight = 2

// MaxPlatformInFlight 是用户手动调高并发时，单平台允许的同时出图上限。
const MaxPlatformInFlight = 8

// sameExitStagger 是同平台共享同一代理出口的两个账号之间的错峰派发间隔。
const sameExitStagger = 2500 * time.Millisecond

// qianwenSameExitStagger 针对千问同出口 IP 放宽错峰间隔，降低同出口并发突发触发风控的概率。
var qianwenSameExitStagger = 8 * time.Second

// sameExitRiskCooldown 在共享出口账号触发限流或安全验证后，让同出口其余账号错峰退避。
var sameExitRiskCooldown = 15 * time.Second

func sameExitStaggerFor(platform string) time.Duration {
	if platform == "千问" || platform == "千问国际" {
		return qianwenSameExitStagger
	}
	return sameExitStagger
}

func taskHitExitRisk(t Task) bool {
	if t.Status == StatusNeedsUser {
		return true
	}
	if t.Error == "" {
		return false
	}
	kind := classifyFault(errors.New(t.Error))
	return kind == faultRate || kind == faultCaptcha || strings.Contains(t.Error, "验证") || strings.Contains(t.Error, "滑块")
}

func (e *Engine) noteExitRisk(platform string, id int64) {
	if e == nil || id <= 0 {
		return
	}
	if e.browsers == nil && e.opts.SendWait <= 0 {
		return
	}
	exitKey := e.sharedExitKey(platform, id)
	if exitKey == "" {
		return
	}
	e.mu.Lock()
	sched := e.activeSched
	e.mu.Unlock()
	if sched == nil {
		return
	}
	cooldown := sameExitRiskCooldown
	if cooldown <= 0 {
		return
	}
	sched.markExit(exitKey, time.Now().Add(cooldown))
	logging.Log.Info("[queue] 同出口账号「%s」遇到限流/验证，同出口其余%s账号错峰退避 %ds", e.accountName(id), platform, max(1, int(cooldown/time.Second)))
}

type runEvent struct {
	task      Task
	terminal  bool
	needsUser bool
}

type exitHold struct {
	until     time.Time
	byAccount int64
}

type schedSnap struct {
	busy           map[int64]bool
	paused         map[int64]bool
	busyExitCount  map[string]int
	ready          map[int64]time.Time
	exitReady      map[string]time.Time
	exitStagger    map[string]exitHold
	activeTotal    int
	activePlatform int
	busyTotal      int
	busyPlatform   int
}

func (ss schedSnap) exitWaitFor(key string, accountID int64, now time.Time) time.Time {
	return exitWaitForState(ss.exitReady, ss.exitStagger, key, accountID, now)
}

func exitWaitForState(exitReady map[string]time.Time, exitStagger map[string]exitHold, key string, accountID int64, now time.Time) time.Time {
	if key == "" {
		return time.Time{}
	}
	var out time.Time
	if until := exitReady[key]; now.Before(until) {
		out = until
	}
	if st := exitStagger[key]; now.Before(st.until) && (accountID == 0 || st.byAccount == 0 || st.byAccount != accountID) {
		if st.until.After(out) {
			out = st.until
		}
	}
	return out
}

// slotSched 只在派发的一瞬间占用名额，出图过程中不拿这把锁。
// 区分风控退避（exitReady，对同出口全部账号生效）与派发错峰（exitStagger，仅对同出口其他账号生效，避免同账号自阻断），
// 并在账号进入人工验证码等待时暂时释放并发计数名额（paused），保持账号独占但允许其他健康账号补位。
type slotSched struct {
	mu           sync.Mutex
	busy         map[int64]bool
	paused       map[int64]bool
	busyPlatform map[int64]string
	busyExit     map[int64]string
	ready        map[int64]time.Time
	exitReady    map[string]time.Time
	exitStagger  map[string]exitHold
	notify       chan struct{}
}

func (s *slotSched) exitWait(key string, now time.Time) time.Time {
	return s.exitWaitFor(key, 0, now)
}

func (s *slotSched) exitWaitFor(key string, accountID int64, now time.Time) time.Time {
	if key == "" {
		return time.Time{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return exitWaitForState(s.exitReady, s.exitStagger, key, accountID, now)
}

func (s *slotSched) markExit(key string, until time.Time) {
	if key == "" || until.IsZero() {
		return
	}
	s.mu.Lock()
	if s.exitReady == nil {
		s.exitReady = map[string]time.Time{}
	}
	if until.After(s.exitReady[key]) {
		s.exitReady[key] = until
	}
	s.mu.Unlock()
}

func (s *slotSched) snap() (map[int64]bool, map[int64]time.Time) {
	ss := s.snapFor("")
	return ss.busy, ss.ready
}

func (s *slotSched) activeCountsLocked(platform string) (activeTotal, activePlatform, busyPlatform int) {
	for slot := range s.busy {
		if platform != "" && s.busyPlatform[slot] == platform {
			busyPlatform++
		}
		if s.paused[slot] {
			continue
		}
		activeTotal++
		if platform != "" && s.busyPlatform[slot] == platform {
			activePlatform++
		}
	}
	return activeTotal, activePlatform, busyPlatform
}

func (s *slotSched) snapFor(platform string) schedSnap {
	s.mu.Lock()
	defer s.mu.Unlock()
	busy := make(map[int64]bool, len(s.busy))
	for k, v := range s.busy {
		busy[k] = v
	}
	var paused map[int64]bool
	if len(s.paused) > 0 {
		paused = make(map[int64]bool, len(s.paused))
		for k, v := range s.paused {
			if v {
				paused[k] = true
			}
		}
	}
	var busyExitCount map[string]int
	if len(s.busyExit) > 0 {
		busyExitCount = make(map[string]int, len(s.busyExit))
		for slot, key := range s.busyExit {
			if s.busy[slot] && key != "" {
				busyExitCount[key]++
			}
		}
	}
	ready := make(map[int64]time.Time, len(s.ready))
	for k, v := range s.ready {
		ready[k] = v
	}
	var exitReady map[string]time.Time
	if len(s.exitReady) > 0 {
		exitReady = make(map[string]time.Time, len(s.exitReady))
		for k, v := range s.exitReady {
			exitReady[k] = v
		}
	}
	var exitStagger map[string]exitHold
	if len(s.exitStagger) > 0 {
		exitStagger = make(map[string]exitHold, len(s.exitStagger))
		for k, v := range s.exitStagger {
			exitStagger[k] = v
		}
	}
	activeTotal, activePlatform, busyPlatform := s.activeCountsLocked(platform)
	return schedSnap{
		busy:           busy,
		paused:         paused,
		busyExitCount:  busyExitCount,
		ready:          ready,
		exitReady:      exitReady,
		exitStagger:    exitStagger,
		activeTotal:    activeTotal,
		activePlatform: activePlatform,
		busyTotal:      len(s.busy),
		busyPlatform:   busyPlatform,
	}
}

// claim 占一个名额。atLimit 说明失败是因为并发上限已经满了（不是这个名额
// 自己忙）：这时候后面的任务也不该再往后找，否则它们会抢在队首任务前面跑。
func (s *slotSched) claim(slot int64, now time.Time, limit int) (claimed, atLimit bool) {
	ok, lim, _ := s.claimFor(slot, "", "", 0, now, 0, limit)
	return ok, lim
}

// claimFor 在单把锁内原子检查账号忙碌、发送间隔、平台/全局并发上限及共享出口错峰，
// 仅在真正占槽成功后才写入 exitStagger，消除并发竞态导致的虚假错峰污染。
func (s *slotSched) claimFor(slot int64, platform, exitKey string, stagger time.Duration, now time.Time, platLimit, globalLimit int) (claimed, atLimit bool, exitUntil time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[slot] || now.Before(s.ready[slot]) {
		return false, false, time.Time{}
	}
	activeTotal, activePlat, busyPlat := s.activeCountsLocked(platform)
	if globalLimit == 1 {
		if len(s.busy) >= 1 {
			return false, true, time.Time{}
		}
	} else if globalLimit > 1 {
		if activeTotal >= globalLimit {
			return false, true, time.Time{}
		}
	}
	if platLimit == 1 {
		if busyPlat >= 1 {
			return false, true, time.Time{}
		}
	} else if platLimit > 1 {
		if activePlat >= platLimit {
			return false, true, time.Time{}
		}
	}
	if until := exitWaitForState(s.exitReady, s.exitStagger, exitKey, slot, now); !until.IsZero() {
		return false, false, until
	}
	if s.busy == nil {
		s.busy = map[int64]bool{}
	}
	if s.busyPlatform == nil {
		s.busyPlatform = map[int64]string{}
	}
	if s.busyExit == nil {
		s.busyExit = map[int64]string{}
	}
	s.busy[slot] = true
	if platform != "" {
		s.busyPlatform[slot] = platform
	}
	if exitKey != "" {
		s.busyExit[slot] = exitKey
		if stagger > 0 {
			if s.exitStagger == nil {
				s.exitStagger = map[string]exitHold{}
			}
			until := now.Add(stagger)
			if until.After(s.exitStagger[exitKey].until) {
				s.exitStagger[exitKey] = exitHold{until: until, byAccount: slot}
			}
		}
	}
	return true, false, time.Time{}
}

func (s *slotSched) setPaused(slot int64, paused bool) {
	if s == nil || slot <= 0 {
		return
	}
	s.mu.Lock()
	if paused {
		if !s.busy[slot] {
			s.mu.Unlock()
			return
		}
		if s.paused == nil {
			s.paused = map[int64]bool{}
		}
		if s.paused[slot] {
			s.mu.Unlock()
			return
		}
		s.paused[slot] = true
	} else {
		if !s.paused[slot] {
			s.mu.Unlock()
			return
		}
		delete(s.paused, slot)
	}
	s.mu.Unlock()
	if paused {
		s.ping()
	}
}

func (s *slotSched) unclaim(slot int64) {
	s.mu.Lock()
	delete(s.busy, slot)
	delete(s.paused, slot)
	delete(s.busyPlatform, slot)
	delete(s.busyExit, slot)
	s.mu.Unlock()
	s.ping()
}

func (s *slotSched) finish(busySlot, readySlot int64, ready time.Time) {
	s.mu.Lock()
	delete(s.busy, busySlot)
	delete(s.paused, busySlot)
	delete(s.busyPlatform, busySlot)
	delete(s.busyExit, busySlot)
	if s.ready == nil {
		s.ready = map[int64]time.Time{}
	}
	if ready.IsZero() {
		delete(s.ready, readySlot)
	} else {
		s.ready[readySlot] = ready
	}
	s.mu.Unlock()
	s.ping()
}

func (e *Engine) setSlotPaused(id int64, paused bool) {
	if e == nil || id <= 0 {
		return
	}
	e.mu.Lock()
	sched := e.activeSched
	e.mu.Unlock()
	if sched != nil {
		sched.setPaused(e.slotKey(id), paused)
	}
}

func (s *slotSched) ping() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notify != nil {
		close(s.notify)
		s.notify = make(chan struct{})
	}
}

func (s *slotSched) watch() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notify
}

func (e *Engine) runPlatforms(ctx context.Context, _ chan struct{}, queue []Task, onTask func(Task), onProgress func(Progress), attempt func(context.Context, *Task) error) error {
	groups := map[string][]Task{}
	var order []string
	for _, task := range queue {
		if _, ok := groups[task.Platform]; !ok {
			order = append(order, task.Platform)
		}
		groups[task.Platform] = append(groups[task.Platform], task)
	}
	events := make(chan runEvent, 64)
	sched := &slotSched{notify: make(chan struct{})}
	e.mu.Lock()
	e.activeSched = sched
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		if e.activeSched == sched {
			e.activeSched = nil
		}
		e.mu.Unlock()
	}()
	var checkLauncher func(string) error
	if attempt == nil {
		attempt = e.attempt
		if e.browsers != nil && e.store != nil {
			rows, fallback := e.store.Browsers(), e.pageBackend()
			checks := map[string]func() error{}
			defer e.unavailableEngines.Clear()
			for _, kind := range []string{browser.KindFingerprint} {
				checks[kind] = sync.OnceValue(func() error {
					err := e.browsers.CheckLauncher(ctx, kind)
					if err != nil {
						e.unavailableEngines.Store(kind, true)
					}
					return err
				})
			}
			checkLauncher = func(platform string) error {
				var errs []error
				kinds := browser.AccountEngines(rows, platform, fallback)
				for _, kind := range kinds {
					if err := checks[kind](); err != nil {
						errs = append(errs, err)
					}
				}
				if len(errs) < len(kinds) {
					return nil
				}
				return errors.Join(errs...)
			}
		}
	}
	var g errgroup.Group
	for _, name := range order {
		tasks := groups[name]
		platform := name
		g.Go(func() error {
			if checkLauncher != nil && (platform == "豆包" || platform == "千问" || platform == "千问国际") {
				if err := checkLauncher(platform); err != nil {
					if ctx.Err() != nil {
						e.stopQueued(tasks, events)
						return nil
					}
					e.stopQueuedWithReason(tasks, events, err.Error())
					logging.Log.Errorf("%s网页浏览器启动检查失败: %v", platform, err)
					return err
				}
			}
			e.drivePlatform(ctx, platform, tasks, sched, events, onTask, attempt)
			return nil
		})
	}
	var groupErr error
	go func() {
		groupErr = g.Wait()
		close(events)
	}()
	done := 0
	total := len(queue)
	var runErr error
	// 停止后成千上万个排队项逐条记「跳过: 已停止」只会刷屏；收成每平台一条汇总。
	// 真正跑到一半被打断的任务另有「#N 账号 结束」一行，不会丢线索。
	skipped := map[string]int{}
	for ev := range events {
		if onTask != nil && ev.task.Status != StatusRunning {
			onTask(ev.task)
		}
		if !ev.terminal {
			continue
		}
		done++
		if ev.needsUser && ctx.Err() != nil {
			runErr = captcha.ErrVerificationRequired
		}
		if onProgress != nil {
			onProgress(Progress{Done: done, Total: total})
		}
		if ev.task.Status == StatusSkipped {
			if ev.task.Error == "已停止" {
				skipped[ev.task.Platform]++
			}
			continue
		}
		line := logging.Line{
			TaskID: ev.task.Index, Platform: ev.task.Platform, Account: e.accountName(ev.task.BrowserID), Port: e.accountPort(ev.task.BrowserID),
			Done: done, Total: total,
		}
		if ev.task.Status == StatusSuccess {
			line.Stage = "saved"
			line.Images = len(ev.task.Results)
			line.Msg = fmt.Sprintf("#%d [%s] 完成，保存 %d 张", ev.task.Index, ev.task.Platform, len(ev.task.Results))
			logging.Emit(logging.Success, line)
		} else if ev.task.Status != "" && ev.task.Status != StatusPending {
			line.Stage = "fail"
			line.Error = ev.task.Error
			line.Msg = fmt.Sprintf("#%d [%s] %s: %s", ev.task.Index, ev.task.Platform, ev.task.Status, ev.task.Error)
			logging.Emit(logging.Warning, line)
		}
	}
	for _, name := range order {
		n := skipped[name]
		if n == 0 {
			continue
		}
		logging.Event(logging.Line{
			Platform: name, Stage: "fail", Error: "已停止", Done: done, Total: total,
			Msg: fmt.Sprintf("已停止：%s %d 个任务跳过", name, n),
		})
	}
	if runErr != nil {
		return runErr
	}
	return groupErr
}

// noteNextReady 把同平台所有开放但尚未忙的账号中最早可派发时刻纳入唤醒时间，
// 避免某账号正忙时忽略另一账号的发送间隔或同出口错峰结束时间。
func noteNextReady(earliest *time.Time, rows []store.Browser, exitKeys map[int64]string, ss schedSnap, now time.Time, capN int) {
	noteCool(earliest, rows, now)
	for _, b := range rows {
		if !openAccount(b, now, capN) {
			continue
		}
		slot := b.ID
		if ss.busy[slot] {
			continue
		}
		need := ss.ready[slot]
		if !now.Before(need) {
			need = time.Time{}
		}
		if exitWait := ss.exitWaitFor(exitKeys[b.ID], b.ID, now); exitWait.After(need) {
			need = exitWait
		}
		if !need.IsZero() && (earliest.IsZero() || need.Before(*earliest)) {
			*earliest = need
		}
	}
}

func (e *Engine) drivePlatform(ctx context.Context, platform string, queue []Task, sched *slotSched, events chan<- runEvent, onTask func(Task), attempt func(context.Context, *Task) error) {
	nudge := make(chan struct{}, 1)
	e.mu.Lock()
	e.wakes = append(e.wakes, nudge)
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		kept := e.wakes[:0]
		for _, ch := range e.wakes {
			if ch != nudge {
				kept = append(kept, ch)
			}
		}
		e.wakes = kept
		e.mu.Unlock()
	}()

	completed := make(chan Task, max(len(queue)+1, MaxPlatformInFlight*4))
	var wg sync.WaitGroup
	defer wg.Wait()

	budget := e.taskBudget()
	active := 0
	pending := queue
	ctxDone := ctx.Done()
	var lastAccount int64
	soloFails := 0
	splitSaves := 0
	switchAfter := 0
	completedByAccount := map[int64]int{}
	if e.opts.TaskSwitching {
		switchAfter = max(1, e.opts.TaskSwitchingCount)
	}
	useExitStagger := e.browsers != nil || e.opts.SendWait > 0

	launchJob := func(t Task) {
		active++
		wg.Go(func() {
			if ctx.Err() != nil || !e.enterAccount(ctx, t.BrowserID) {
				t.slot, t.ranOn = 0, 0
				t.StartedAt, t.Elapsed, t.AccountName = time.Time{}, 0, ""
				if ctx.Err() == nil {
					t.rotated = true
				}
				completed <- t
				return
			}
			tctx, cancel := e.budgetContext(ctx, t.BrowserID, budget)
			t.slot, t.ranOn = t.BrowserID, 0
			t.everSent, t.noUse = t.submitted, false
			previousSoften := t.soften
			if t.BrowserID > 0 && e.store != nil {
				e.store.TouchBrowser(t.BrowserID, time.Now())
			}
			started := time.Now()
			name := e.accountName(t.BrowserID)
			t.StartedAt = started
			t.Elapsed = 0
			t.Undispatched = false
			t.AccountName = name
			t.Status = StatusRunning
			if t.submitted {
				t.Phase = PhaseGenerating
			} else {
				t.Phase = PhasePreparing
			}
			members := batchMembers(t)
			for _, item := range members {
				e.starts.Store(item.Index, started)
			}
			e.laneStart(e.slotKey(t.BrowserID), t.BrowserID, jobOf(t, started))
			if e.view != nil {
				e.view.UpdateBatch(members)
			}
			stopLog := logging.Bind(t.Platform, name, e.accountPort(t.BrowserID), t.Index)
			e.traceTask(&t, "assign", "#"+itoa(t.Index)+" "+name+" 开始", 0, 0, nil)
			if onTask != nil {
				for _, item := range members {
					onTask(item)
				}
			}
			t = e.execute(tctx, t, attempt)
			t.Phase = ""
			cancel()
			if t.rotated && t.Status == StatusPending {
				t.StartedAt = time.Time{}
				t.Elapsed = 0
				t.AccountName = ""
			} else {
				elapsed := time.Since(started)
				if elapsed <= 0 {
					elapsed = time.Millisecond
				}
				t.Elapsed = elapsed
				if t.ranOn > 0 {
					t.BrowserID = t.ranOn
					if actual := e.accountName(t.ranOn); actual != "" {
						t.AccountName = actual
					}
				}
			}
			e.noteUseMembers(batchMembers(t), previousSoften)
			stage, msg := "done", "#"+itoa(t.Index)+" "+name+" 结束"
			if t.rotated && t.Status == StatusPending {
				stage = "retry"
				switch {
				case strings.Contains(t.Error, "已换到"), t.pauseNote == "login", t.pauseNote == "rotate", t.ReassignWhy == "失败换号":
					// 换号那一行已经写过。登录停住用「没有可用账号」，不要再写成额度恢复。
					msg = ""
				default:
					msg = "#" + itoa(t.Index) + " 发送未确认，等待空闲账号"
				}
			}
			// 换号那行已在 maybeRotate 写过，这里 msg 为空就不要再记一条空日志。
			if msg != "" || stage != "retry" {
				var logErr error
				if t.Status != StatusSuccess && t.Error != "" {
					logErr = fmt.Errorf("%s", t.Error)
				}
				e.traceTask(&t, stage, msg, len(t.Results), 0, logErr)
			}
			stopLog()
			if t.slot > 0 {
				e.setActive(t.slot, false)
			}
			completed <- t
		})
	}

	handleCompleted := func(t Task) {
		active--
		resource := t.slot
		if resource == 0 {
			resource = t.BrowserID
		}
		busySlot := e.slotKey(resource)
		used := t.ranOn
		if used == 0 {
			used = resource
		}
		if switchAfter > 0 && t.slot != 0 && t.Attempts > 0 && used > 0 && !(t.rotated && t.Status == StatusPending) {
			lastAccount = used
			if completedByAccount[used] < switchAfter {
				completedByAccount[used] += len(batchMembers(t))
			}
		}
		readySlot := e.slotKey(used)
		gap := time.Duration(0)
		if t.slot != 0 && !(t.rotated && t.Status == StatusPending && !t.submitted) {
			gap = e.sendGap(time.Duration(e.opts.SendWait) * time.Second)
		}
		ready := time.Time{}
		if gap > 0 {
			now := time.Now()
			ready = now.Add(gap)
			if t.Status == StatusSuccess && !t.sentAt.IsZero() {
				ready = t.sentAt.Add(gap)
				if !ready.After(now) {
					ready = time.Time{}
					gap = 0
				} else {
					gap = ready.Sub(now)
				}
			}
		}
		sched.finish(busySlot, readySlot, ready)
		e.laneFinish(busySlot, resource, readySlot, gap)
		if taskHitExitRisk(t) {
			e.noteExitRisk(platform, used)
		}
		if e.opts.Wash && platform == "豆包" && ctxDone != nil && ctx.Err() == nil && len(t.batch) > 1 &&
			t.Status == StatusFailed && len(t.Results) == 0 && isModeration(errors.New(t.Error)) &&
			(soloFails < 3 || splitSaves > 0) {
			subLimit := max(1, len(t.batch)/2)
			logging.Log.Info("[queue]  #%d-%d 豆包批次触发内容审核，自动拆分为每批最多 %d 张重试", t.batch[0].Index, t.batch[len(t.batch)-1].Index, subLimit)
			split := splitModerationBatch(t.batch, subLimit, t.sendSeq+1)
			pending = append(split, pending...)
			if e.view != nil {
				e.view.UpdateBatch(split)
			}
			for _, item := range split {
				events <- runEvent{task: item}
			}
			return
		}
		// 豆包多图洗图只返回部分已验证结果时：先将成功项结案，再自动将缺少的源图单独补跑一次。
		if e.opts.Wash && platform == "豆包" && ctxDone != nil && ctx.Err() == nil && len(t.batch) > 1 &&
			t.Status == StatusFailed && !t.partialRetried && strings.Contains(t.Error, "已按源图关联保存") {
			members := batchMembers(t)
			var succeeded, missing []Task
			for _, item := range members {
				if item.Status == StatusSuccess && len(item.Results) > 0 {
					succeeded = append(succeeded, item)
				} else {
					missing = append(missing, item)
				}
			}
			if len(succeeded) > 0 && len(missing) > 0 {
				logging.Log.Info("[queue]  #%d-%d 豆包批量洗图已保存 %d/%d 张，自动单独补跑缺少的 %d 张",
					t.batch[0].Index, t.batch[len(t.batch)-1].Index, len(succeeded), len(members), len(missing))
				if e.view != nil {
					e.view.UpdateBatch(succeeded)
				}
				for _, item := range succeeded {
					events <- runEvent{task: item, terminal: true}
				}
				requeued := requeuePartialWashMissing(missing, t.sendSeq+1)
				pending = append(requeued, pending...)
				if e.view != nil {
					e.view.UpdateBatch(requeued)
				}
				for _, item := range requeued {
					events <- runEvent{task: item}
				}
				return
			}
		}
		members := batchMembers(t)
		if t.Status != StatusPending && t.splitModeration {
			for _, item := range members {
				if item.Status == StatusSuccess {
					splitSaves++
				}
			}
			if len(t.batch) <= 1 && t.Status == StatusFailed && isModeration(errors.New(t.Error)) {
				soloFails++
			}
		}
		if e.view != nil {
			e.view.UpdateBatch(members)
		}
		if t.rotated && t.Status == StatusPending {
			t.rotated = false
			if t.Attempts > 0 {
				pending = append([]Task{t}, pending...)
			} else {
				pending = append(pending, t)
			}
			for _, item := range members {
				events <- runEvent{task: item}
			}
			return
		}
		if t.Status != StatusPending {
			for _, item := range members {
				events <- runEvent{task: item, terminal: true, needsUser: item.Status == StatusNeedsUser}
			}
		}
	}

	for len(pending) > 0 || active > 0 {
	drainCompleted:
		for {
			select {
			case t := <-completed:
				handleCompleted(t)
			default:
				break drainCompleted
			}
		}
		schedNotify := sched.watch()
		if ctx.Err() != nil {
			e.stopQueued(pending, events)
			pending, ctxDone = nil, nil
			if active == 0 {
				break
			}
		}
		var earliest time.Time
		now := time.Now()
		capN := e.capFor(platform)
		var allBrowsers, platformRows []store.Browser
		var byID map[int64]store.Browser
		var exitKeys map[int64]string
		if ctx.Err() == nil && len(pending) > 0 {
			if e.store != nil {
				allBrowsers = e.store.Browsers()
				e.refreshAccountMeta(allBrowsers)
				platformRows = e.accountsFromRows(allBrowsers, platform, now)
				byID = make(map[int64]store.Browser, len(platformRows))
				for _, b := range platformRows {
					byID[b.ID] = b
				}
				if useExitStagger {
					exitKeys = buildPlatformExitKeys(allBrowsers, platformRows, platform)
				}
			}
			// 额度用尽通常要等到次日，不能把未派发的豆包任务一直留在运行中。
			// 已派发的批次先收完，避免丢失它们可能返回的结果。
			if platform == "豆包" && active == 0 && e.allDoubaoAccountsFullRows(platformRows, now, capN) {
				logging.Log.Warning("豆包所有账号额度已用完，自动停止剩余任务")
				e.stopQueuedWithReason(pending, events, "账号额度已用完，已停止")
				pending = nil
				break
			}
			platLimit, globalLimit := e.concurrencyLimits(platform)
			usable := ParallelSlots(platformRows, now, capN)
			platCap := min(max(usable, 1), platLimit)
			ss := sched.snapFor(platform)
			hasCapacity := func() bool {
				inFlight := ss.activePlatform
				if platLimit == 1 {
					inFlight = ss.busyPlatform
				}
				if inFlight >= platCap {
					return false
				}
				if globalLimit == 1 {
					return ss.busyTotal < 1
				}
				if globalLimit > 1 {
					return ss.activeTotal < globalLimit
				}
				return true
			}
			exitBlocked := func(exitKey string, accountID int64) bool {
				return !ss.exitWaitFor(exitKey, accountID, now).IsZero()
			}
			pickAccount := func(preferred, avoid int64) (int64, bool) {
				if switchAfter > 0 {
					if lastAccount > 0 {
						preferred = lastAccount
					}
					var excluded map[int64]bool
					for id, count := range completedByAccount {
						if count >= switchAfter {
							if excluded == nil {
								excluded = make(map[int64]bool, len(completedByAccount))
							}
							excluded[id] = true
						}
					}
					if id, ok := e.pickIdleAccountFromRows(platformRows, platform, preferred, avoid, ss.busy, ss.ready, exitKeys, exitBlocked, ss.busyExitCount, now, excluded, true); ok {
						return id, true
					}
					return e.pickIdleAccountFromRows(platformRows, platform, preferred, avoid, ss.busy, ss.ready, exitKeys, exitBlocked, ss.busyExitCount, now, nil, true)
				}
				return e.pickIdleAccountFromRows(platformRows, platform, preferred, avoid, ss.busy, ss.ready, exitKeys, exitBlocked, ss.busyExitCount, now, nil, false)
			}
			hasProbe := e.hasLoginProbe()
			probed := map[int64]bool{}
			freshBlocked := false
			reloadSnapshot := func() ([]store.Browser, map[int64]store.Browser) {
				if e.store == nil {
					return nil, nil
				}
				allBrowsers = e.store.Browsers()
				e.refreshAccountMeta(allBrowsers)
				platformRows = e.accountsFromRows(allBrowsers, platform, now)
				byID = make(map[int64]store.Browser, len(platformRows))
				for _, b := range platformRows {
					byID[b.ID] = b
				}
				if useExitStagger {
					exitKeys = buildPlatformExitKeys(allBrowsers, platformRows, platform)
				}
				usable = ParallelSlots(platformRows, now, capN)
				platCap = min(max(usable, 1), platLimit)
				freshBlocked = false
				return platformRows, byID
			}
			if hasCapacity() {
				for i := 0; i < len(pending); i++ {
					if !hasCapacity() {
						break
					}
					task := pending[i]
					if !task.retryAfter.IsZero() && now.Before(task.retryAfter) {
						if earliest.IsZero() || task.retryAfter.Before(earliest) {
							earliest = task.retryAfter
						}
						continue
					}
					isFresh := task.Attempts == 0 && task.Error == "" && !task.submitted && task.avoidBrowser == 0
					if freshBlocked && isFresh && (!hasProbe || task.BrowserID <= 0 || probed[task.BrowserID] || ss.busy[task.BrowserID]) {
						continue
					}
					if task.Attempts == 0 && task.Error == "" && !task.submitted {
						if id, ok := pickAccount(task.BrowserID, 0); ok {
							task.BrowserID = id
						}
					}
					var wait time.Time
					var dead bool
					wait, dead, platformRows, byID = e.settleAccountSnap(ctx, &task, platformRows, byID, probed, ss.busy, now, reloadSnapshot)
					pending[i] = task
					if dead {
						task.Status = StatusFailed
						if task.Error == "" {
							task.Error = "同平台账号都不可用"
						}
						if task.Attempts == 0 && task.StartedAt.IsZero() {
							task.BrowserID = 0
							task.AccountName = ""
						}
						members := batchMembers(task)
						if e.view != nil {
							e.view.UpdateBatch(members)
						}
						e.traceTask(&task, "assign", "#"+itoa(task.Index)+" 跳过："+task.Error, 0, 0, nil)
						for _, item := range members {
							events <- runEvent{task: item, terminal: true}
						}
						pending = append(pending[:i], pending[i+1:]...)
						i--
						continue
					}
					if !wait.IsZero() {
						if earliest.IsZero() || wait.Before(earliest) {
							earliest = wait
						}
						if isFresh {
							freshBlocked = true
						}
						continue
					}
					resource := e.slotKey(task.BrowserID)
					exitKey := exitKeys[task.BrowserID]
					exitBlockedNow := exitKey != "" && !ss.exitWaitFor(exitKey, task.BrowserID, now).IsZero()
					if !task.submitted && (ss.busy[resource] || now.Before(ss.ready[resource]) || exitBlockedNow) {
						if id, ok := pickAccount(0, task.avoidBrowser); ok {
							task.BrowserID = id
							pending[i] = task
							resource = e.slotKey(id)
							exitKey = exitKeys[id]
						}
					}
					if ss.busy[resource] {
						if isFresh {
							freshBlocked = true
						}
						continue
					}
					if ready := ss.ready[resource]; now.Before(ready) {
						if earliest.IsZero() || ready.Before(earliest) {
							earliest = ready
						}
						if isFresh {
							freshBlocked = true
						}
						continue
					}
					if exitWait := ss.exitWaitFor(exitKey, task.BrowserID, now); !exitWait.IsZero() {
						if earliest.IsZero() || exitWait.Before(earliest) {
							earliest = exitWait
						}
						if isFresh {
							freshBlocked = true
						}
						continue
					}
					stagger := time.Duration(0)
					if exitKey != "" {
						stagger = sameExitStaggerFor(platform)
					}
					ok, atLimit, exitUntil := sched.claimFor(resource, platform, exitKey, stagger, now, platLimit, globalLimit)
					if !ok {
						if atLimit {
							// 名额已经满了（快照过期）：后面的任务照样派不出去，而且会抢在
							// 队首任务前面跑。等完成事件唤醒后从队首重新扫描。
							break
						}
						if !exitUntil.IsZero() {
							if earliest.IsZero() || exitUntil.Before(earliest) {
								earliest = exitUntil
							}
							if ss.exitReady == nil {
								ss.exitReady = map[string]time.Time{}
							}
							if exitUntil.After(ss.exitReady[exitKey]) {
								ss.exitReady[exitKey] = exitUntil
							}
						} else {
							ss.busy[resource] = true
						}
						continue
					}
					// 本地快照同步已占名额，使单轮即可派满所有可用并发位。
					ss.busy[resource] = true
					ss.activePlatform++
					ss.busyPlatform++
					ss.activeTotal++
					ss.busyTotal++
					if exitKey != "" {
						if ss.busyExitCount == nil {
							ss.busyExitCount = map[string]int{}
						}
						ss.busyExitCount[exitKey]++
						if stagger > 0 {
							if ss.exitStagger == nil {
								ss.exitStagger = map[string]exitHold{}
							}
							until := now.Add(stagger)
							if until.After(ss.exitStagger[exitKey].until) {
								ss.exitStagger[exitKey] = exitHold{until: until, byAccount: resource}
							}
						}
					}
					task.avoidBrowser = 0
					task.retryAfter = time.Time{}
					pending[i] = task
					maxImages := washChatTasks
					if remaining := switchAfter - completedByAccount[task.BrowserID]; switchAfter > 0 && remaining > 0 {
						maxImages = min(maxImages, remaining)
					}
					row, hasRow := byID[task.BrowserID]
					if len(task.batch) > 1 && !task.submitted {
						if limit := e.washBatchLimitRow(task, row, hasRow, maxImages, now); limit < len(task.batch) {
							head := washBatchOf(task, task.batch[:limit])
							tail := washBatchOf(task, task.batch[limit:])
							pending = slices.Replace(pending, i, i+1, head, tail)
							task = head
						}
					}
					next, batchIndices := e.bundleWashTasksWithRow(pending, i, task, row, hasRow, maxImages, now)
					if switchAfter > 0 {
						for id, count := range completedByAccount {
							if id != next.BrowserID && count >= switchAfter {
								delete(completedByAccount, id)
							}
						}
					}
					for k := len(batchIndices) - 1; k >= 0; k-- {
						j := batchIndices[k]
						pending = append(pending[:j], pending[j+1:]...)
					}
					i--
					launchJob(next)
				}
			}
			if len(pending) > 0 {
				if hasCapacity() {
					noteNextReady(&earliest, platformRows, exitKeys, ss, now, capN)
				} else {
					noteCool(&earliest, platformRows, now)
				}
			}
		}
		if len(pending) == 0 && active == 0 {
			break
		}
		var timer *time.Timer
		var wake <-chan time.Time
		if !earliest.IsZero() {
			timer = time.NewTimer(time.Until(earliest))
			wake = timer.C
		}
		select {
		case t := <-completed:
			handleCompleted(t)
		case <-wake:
		case <-nudge:
		case <-schedNotify:
		case <-ctxDone:
			e.stopQueued(pending, events)
			pending, ctxDone = nil, nil
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

func splitModerationBatch(batch []Task, subLimit, nextSendSeq int) []Task {
	split := make([]Task, 0, len(batch))
	for _, orig := range batch {
		orig.BrowserID = 0
		orig.AccountName = ""
		orig.Attempts = 0
		orig.Status = StatusPending
		orig.Phase = ""
		orig.Error = ""
		orig.QualityNote = ""
		orig.StartedAt = time.Time{}
		orig.Elapsed = 0
		orig.Undispatched = false
		orig.slot = 0
		orig.ranOn = 0
		orig.submitted = false
		orig.sendSeq = nextSendSeq
		orig.everSent = false
		orig.noUse = false
		orig.sentAt = time.Time{}
		orig.batch = nil
		orig.maxBatch = subLimit
		orig.splitModeration = true
		split = append(split, orig)
	}
	return split
}

// stopQueued 把还没开跑的任务标成已停止，卡片和页头不再留在排队里。
func (e *Engine) stopQueued(pending []Task, events chan<- runEvent) {
	e.stopQueuedWithReason(pending, events, "已停止")
}

func (e *Engine) stopQueuedWithReason(pending []Task, events chan<- runEvent, reason string) {
	var updated []Task
	for _, t := range pending {
		switch t.Status {
		case StatusSuccess, StatusFailed, StatusSkipped, StatusNeedsUser:
			continue
		}
		if t.Attempts == 0 && t.StartedAt.IsZero() && len(t.Results) == 0 {
			t.Undispatched = true
		}
		t.Status = StatusSkipped
		t.Phase = ""
		t.Error = reason
		members := batchMembers(t)
		updated = append(updated, members...)
		if events != nil {
			for _, item := range members {
				events <- runEvent{task: item, terminal: true}
			}
		}
	}
	if e != nil && e.view != nil && len(updated) > 0 {
		e.view.UpdateBatch(updated)
	}
}

// stopLeftovers 在这一批结束时再扫一遍。调度丢掉的排队项也要变成已停止。
func (e *Engine) stopLeftovers() {
	if e == nil || e.view == nil {
		return
	}
	var updated []Task
	for _, t := range e.view.Snapshot() {
		switch t.Status {
		case StatusSuccess, StatusFailed, StatusSkipped, StatusNeedsUser:
			continue
		}
		if t.Attempts == 0 && t.StartedAt.IsZero() && len(t.Results) == 0 {
			t.Undispatched = true
		}
		t.Status = StatusSkipped
		t.Phase = ""
		t.Error = "已停止"
		updated = append(updated, t)
	}
	if len(updated) > 0 {
		e.view.UpdateBatch(updated)
	}
}

func requeuePartialWashMissing(missing []Task, nextSendSeq int) []Task {
	out := make([]Task, 0, len(missing))
	for _, orig := range missing {
		orig.BrowserID = 0
		orig.AccountName = ""
		orig.Attempts = 0
		orig.Status = StatusPending
		orig.Phase = ""
		orig.Error = ""
		orig.QualityNote = ""
		orig.StartedAt = time.Time{}
		orig.Elapsed = 0
		orig.Undispatched = false
		orig.slot = 0
		orig.ranOn = 0
		orig.submitted = false
		orig.sendSeq = nextSendSeq
		orig.everSent = false
		orig.noUse = false
		orig.sentAt = time.Time{}
		orig.batch = nil
		orig.maxBatch = 1
		orig.partialRetried = true
		out = append(out, orig)
	}
	return out
}

func (e *Engine) platformCap(platform string, now time.Time) int {
	usable := ParallelSlots(e.accountsFor(platform), now, e.capFor(platform))
	if usable < 1 {
		return 1
	}
	limit := e.platformLimit(platform)
	if usable > limit {
		return limit
	}
	return usable
}

// budgetContext 是这一单的外层时限。验证码暂停时这段时间不计入，且按事件唤醒而非 20ms 轮询；
// 同时扣除任务启动前该账号已累计的暂停时长，避免旧任务验证暂停泄漏到后续任务。
// 用户取消仍会立刻结束。到点取消的原因是 DeadlineExceeded，和 WithTimeout 一样。
func (e *Engine) budgetContext(parent context.Context, id int64, limit time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := context.WithCancelCause(parent)
	started := time.Now()
	var basePaused time.Duration
	if e != nil {
		basePaused = e.pausedFor(id, started)
	}
	go func() {
		for {
			now := time.Now()
			var extra time.Duration
			var paused bool
			var wake <-chan struct{}
			if e != nil {
				extra, paused, wake = e.budgetPauseState(id, now)
			}
			held := extra - basePaused
			if held < 0 {
				held = 0
			}
			spent := now.Sub(started) - held
			if spent < 0 {
				spent = 0
			}
			if !paused && spent >= limit {
				stop(context.DeadlineExceeded)
				return
			}
			if paused {
				select {
				case <-parent.Done():
					return
				case <-ctx.Done():
					return
				case <-wake:
				}
				continue
			}
			remaining := limit - spent
			timer := time.NewTimer(remaining)
			select {
			case <-parent.Done():
				timer.Stop()
				return
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	return ctx, func() { stop(context.Canceled) }
}

func (e *Engine) taskBudget() time.Duration {
	// ponytail: 预算里只留一次人工验证。第二次再等满 5 分钟，外层时限仍可能先到。
	return e.resultWait() + humanVerifyLimit + 3*time.Minute
}

func (e *Engine) accountName(id int64) string {
	if e == nil || id <= 0 {
		return ""
	}
	e.liveMu.Lock()
	if info, ok := e.acctMeta[id]; ok {
		e.liveMu.Unlock()
		return info.name
	}
	e.liveMu.Unlock()
	row, ok := e.browserByID(id)
	if !ok {
		return ""
	}
	return accountLabel(row)
}

func (e *Engine) accountPort(id int64) int {
	if e == nil || id <= 0 {
		return 0
	}
	e.liveMu.Lock()
	if info, ok := e.acctMeta[id]; ok {
		e.liveMu.Unlock()
		return info.port
	}
	e.liveMu.Unlock()
	row, ok := e.browserByID(id)
	if !ok {
		return 0
	}
	return row.Port
}

// tracePhase 记下某一段的本机耗时。Elapsed 仍是从任务开始算的累计时间，句子里的「本段」才是这一段。
func (e *Engine) tracePhase(t *Task, stage, label string, phase time.Duration, n int, err error) {
	if t == nil {
		return
	}
	if phase < 0 {
		phase = 0
	}
	e.traceTask(t, stage, fmt.Sprintf("#%d %s（本段 %.1f 秒）", t.Index, label, phase.Seconds()), n, 0, err)
}

func (e *Engine) traceTask(t *Task, stage, msg string, n, nbytes int, err error) {
	if e == nil || t == nil {
		return
	}
	var elapsed time.Duration
	if v, ok := e.starts.Load(t.Index); ok {
		if started, ok := v.(time.Time); ok {
			elapsed = time.Since(started)
		}
	}
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	id := t.ranOnOr(t.BrowserID)
	logging.Event(logging.Line{
		TaskID: t.Index, Platform: t.Platform, Account: e.accountName(id), Port: e.accountPort(id),
		Stage: stage, Elapsed: elapsed, Images: n, Bytes: nbytes, Error: errText, Msg: msg,
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
