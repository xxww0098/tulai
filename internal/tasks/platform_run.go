package tasks

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/logging"
)

// PlatformInFlight 是每个平台同时出图的上限。豆包、千问各 2，两边一起最多 4。
const PlatformInFlight = 2

type runEvent struct {
	task      Task
	terminal  bool
	needsUser bool
}

// slotSched 只在派发的一瞬间占用名额，出图过程中不拿这把锁。
// 同一个扩展配置文件的豆包和千问仍共用一个名额，因为那个窗口只能有一个前台标签。
type slotSched struct {
	mu     sync.Mutex
	busy   map[int64]bool
	ready  map[int64]time.Time
	notify chan struct{}
}

func (s *slotSched) snap() (map[int64]bool, map[int64]time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	busy := make(map[int64]bool, len(s.busy))
	for k, v := range s.busy {
		busy[k] = v
	}
	ready := make(map[int64]time.Time, len(s.ready))
	for k, v := range s.ready {
		ready[k] = v
	}
	return busy, ready
}

func (s *slotSched) claim(slot int64, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy[slot] || now.Before(s.ready[slot]) {
		return false
	}
	if s.busy == nil {
		s.busy = map[int64]bool{}
	}
	s.busy[slot] = true
	return true
}

func (s *slotSched) unclaim(slot int64) {
	s.mu.Lock()
	delete(s.busy, slot)
	s.mu.Unlock()
	s.ping()
}

func (s *slotSched) finish(busySlot, readySlot int64, ready time.Time) {
	s.mu.Lock()
	delete(s.busy, busySlot)
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

func (s *slotSched) ping() {
	if s == nil || s.notify == nil {
		return
	}
	select {
	case s.notify <- struct{}{}:
	default:
	}
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
	events := make(chan runEvent, 32)
	sched := &slotSched{notify: make(chan struct{}, 1)}
	g, _ := errgroup.WithContext(ctx)
	for _, name := range order {
		tasks := groups[name]
		platform := name
		g.Go(func() error {
			e.drivePlatform(ctx, platform, tasks, sched, events, onTask, attempt)
			return nil
		})
	}
	go func() {
		_ = g.Wait()
		close(events)
	}()
	done := 0
	total := len(queue)
	var runErr error
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
		line := logging.Line{
			TaskID: ev.task.Index, Platform: ev.task.Platform, Account: e.accountName(ev.task.BrowserID), Port: e.accountPort(ev.task.BrowserID),
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
	return runErr
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

	jobs := make(chan Task)
	completed := make(chan Task, len(queue)+1)
	var wg sync.WaitGroup
	workers := PlatformInFlight
	budget := e.taskBudget()
	if platform == agy.Platform {
		workers = 1
		budget = e.agyBudget()
	}
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range jobs {
				if ctx.Err() != nil {
					completed <- t
					continue
				}
				tctx, cancel := e.budgetContext(ctx, t.BrowserID, budget)
				t.slot = t.BrowserID
				if t.BrowserID > 0 {
					e.setActive(t.BrowserID, true)
					if e.store != nil {
						e.store.TouchBrowser(t.BrowserID, time.Now())
					}
				}
				e.starts.Store(t.Index, time.Now())
				e.laneStart(e.slotKey(t.BrowserID), t.BrowserID, jobOf(t, time.Now()))
				t.Status = StatusRunning
				if e.view != nil {
					e.view.Update(t)
				}
				name := e.accountName(t.BrowserID)
				stopLog := logging.Bind(t.Platform, name, e.accountPort(t.BrowserID), t.Index)
				e.traceTask(&t, "assign", "#"+itoa(t.Index)+" "+name+" 开始", 0, 0, nil)
				if onTask != nil {
					onTask(t)
				}
				t = e.execute(tctx, t, attempt)
				cancel()
				e.noteUse(t)
				stage, msg := "done", "#"+itoa(t.Index)+" "+name+" 结束"
				if t.rotated && t.Status == StatusPending {
					stage = "retry"
					switch {
					case strings.Contains(t.Error, "已换到"), t.pauseNote == "login":
						// 换号那一行已经写过。登录停住用「没有可用账号」，不要再写成额度恢复。
						msg = ""
					case t.Platform == agy.Platform && t.Error == "":
						msg = "#" + itoa(t.Index) + " 等待账号额度恢复"
					default:
						msg = "#" + itoa(t.Index) + " 发送未确认，等待空闲账号"
					}
				}
				var logErr error
				if t.Status != StatusSuccess && t.Error != "" {
					logErr = fmt.Errorf("%s", t.Error)
				}
				e.traceTask(&t, stage, msg, len(t.Results), 0, logErr)
				stopLog()
				if t.slot > 0 {
					e.setActive(t.slot, false)
				}
				completed <- t
			}
		}()
	}
	defer func() {
		close(jobs)
		wg.Wait()
	}()

	active := 0
	pending := queue
	ctxDone := ctx.Done()
	var claimed int64
	for len(pending) > 0 || active > 0 {
		if ctx.Err() != nil {
			e.stopQueued(pending, events)
			pending, ctxDone = nil, nil
			if active == 0 {
				break
			}
		}
		var dispatch chan Task
		var next Task
		index := -1
		var earliest time.Time
		now := time.Now()
		if active < e.platformCap(platform, now) && ctx.Err() == nil {
			busy, readyAt := sched.snap()
			for i := 0; i < len(pending); i++ {
				task := pending[i]
				wait, dead := e.settleAccount(ctx, &task)
				pending[i] = task
				if dead {
					task.Status = StatusFailed
					if task.Error == "" {
						task.Error = "同平台账号都不可用"
					}
					if e.view != nil {
						e.view.Update(task)
					}
					e.traceTask(&task, "assign", "#"+itoa(task.Index)+" 跳过："+task.Error, 0, 0, nil)
					events <- runEvent{task: task, terminal: true}
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
				resource := e.slotKey(task.BrowserID)
				if busy[resource] || now.Before(readyAt[resource]) {
					if id, ok := e.pickIdleAccount(task.Platform, resource, busy, readyAt, now); ok && id != task.avoidSend {
						task.BrowserID = id
						pending[i] = task
						resource = e.slotKey(id)
					}
				}
				if busy[resource] {
					noteCool(&earliest, e.accountsFor(task.Platform), now)
					continue
				}
				if ready := readyAt[resource]; now.Before(ready) {
					if earliest.IsZero() || ready.Before(earliest) {
						earliest = ready
					}
					noteCool(&earliest, e.accountsFor(task.Platform), now)
					continue
				}
				if !sched.claim(resource, now) {
					continue
				}
				claimed = resource
				index, next, dispatch = i, task, jobs
				break
			}
		} else if ctx.Err() == nil {
			noteCool(&earliest, e.accountsFor(platform), now)
		}
		if len(pending) == 0 && active == 0 {
			if claimed != 0 {
				sched.unclaim(claimed)
			}
			break
		}
		var timer *time.Timer
		var wake <-chan time.Time
		if dispatch == nil && !earliest.IsZero() {
			timer = time.NewTimer(time.Until(earliest))
			wake = timer.C
		}
		sent := false
		select {
		case dispatch <- next:
			sent = true
			claimed = 0
			if index == 0 {
				pending = pending[1:]
			} else if index > 0 {
				pending = append(pending[:index], pending[index+1:]...)
			}
			active++
		case t := <-completed:
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
			readySlot := e.slotKey(used)
			gap := e.sendGap(time.Duration(e.opts.SendWait) * time.Second)
			ready := time.Time{}
			if gap > 0 {
				ready = time.Now().Add(gap)
			}
			sched.finish(busySlot, readySlot, ready)
			e.laneFinish(busySlot, resource, readySlot, gap)
			if e.view != nil {
				e.view.Update(t)
			}
			if t.rotated && t.Status == StatusPending {
				t.rotated = false
				pending = append(pending, t)
				events <- runEvent{task: t}
				if timer != nil {
					timer.Stop()
					timer = nil
				}
				if claimed != 0 {
					sched.unclaim(claimed)
					claimed = 0
				}
				continue
			}
			if t.Status != StatusPending {
				events <- runEvent{task: t, terminal: true, needsUser: t.Status == StatusNeedsUser}
			}
		case <-wake:
		case <-nudge:
		case <-sched.notify:
		case <-ctxDone:
			e.stopQueued(pending, events)
			pending, ctxDone = nil, nil
		}
		if !sent && claimed != 0 {
			sched.unclaim(claimed)
			claimed = 0
		}
		if timer != nil {
			timer.Stop()
		}
	}
}

// stopQueued 把还没开跑的任务标成已停止，卡片和页头不再留在排队里。
func (e *Engine) stopQueued(pending []Task, events chan<- runEvent) {
	for _, t := range pending {
		switch t.Status {
		case StatusSuccess, StatusFailed, StatusSkipped, StatusNeedsUser:
			continue
		}
		t.Status = StatusSkipped
		t.Error = "已停止"
		if e != nil && e.view != nil {
			e.view.Update(t)
		}
		if events != nil {
			events <- runEvent{task: t, terminal: true}
		}
	}
}

// stopLeftovers 在这一批结束时再扫一遍。调度丢掉的排队项也要变成已停止。
func (e *Engine) stopLeftovers() {
	if e == nil || e.view == nil {
		return
	}
	for _, t := range e.view.Snapshot() {
		switch t.Status {
		case StatusSuccess, StatusFailed, StatusSkipped, StatusNeedsUser:
			continue
		}
		t.Status = StatusSkipped
		t.Error = "已停止"
		e.view.Update(t)
	}
}

func (e *Engine) platformCap(platform string, now time.Time) int {
	usable := ParallelSlots(e.accountsFor(platform), now, e.capNow())
	if platform == agy.Platform {
		return 1
	}
	if usable < 1 {
		return 1
	}
	if usable > PlatformInFlight {
		return PlatformInFlight
	}
	return usable
}

// budgetContext 是这一单的外层时限。验证码暂停时这段时间不计入。
// 用户取消仍会立刻结束。到点取消的原因是 DeadlineExceeded，和 WithTimeout 一样。
func (e *Engine) budgetContext(parent context.Context, id int64, limit time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := context.WithCancelCause(parent)
	started := time.Now()
	go func() {
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-parent.Done():
				return
			case <-ctx.Done():
				return
			case now := <-tick.C:
				spent := time.Since(started)
				if e != nil {
					spent -= e.pausedFor(id, now)
				}
				if spent >= limit {
					stop(context.DeadlineExceeded)
					return
				}
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
	row, ok := e.browserByID(id)
	if !ok {
		return ""
	}
	return accountLabel(row)
}

func (e *Engine) accountPort(id int64) int {
	row, ok := e.browserByID(id)
	if !ok {
		return 0
	}
	return row.Port
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
	logging.Event(logging.Line{
		TaskID: t.Index, Platform: t.Platform, Account: e.accountName(t.BrowserID), Port: e.accountPort(t.BrowserID),
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
