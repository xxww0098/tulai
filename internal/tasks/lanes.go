package tasks

import (
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/store"
)

// LaneState 是账号车道上的一句话。界面按这个上色。
type LaneState string

const (
	LaneIdle    LaneState = "空闲"
	LaneRunning LaneState = "出图中"
	LaneGap     LaneState = "等待间隔"
	LaneCooling LaneState = "冷却中"
	LaneFull    LaneState = "今日已满"
	LaneLogin   LaneState = "需要登录"
	LaneCaptcha LaneState = "验证码"
	LaneOffline LaneState = "离线"
)

// LaneJob 是这个账号正在出的那一单。
type LaneJob struct {
	Index   int
	Prompt  string
	Image   string
	Started time.Time
}

// SendWindow 是发送间隔还要等到什么时候，以及这一段总共多久。界面用它画倒计时环。
type SendWindow struct {
	Until time.Time
	Span  time.Duration
}

// LaneLive 是调度器此刻的占用和间隔。账号健康和今日次数另从配置库读。
type LaneLive struct {
	Busy    map[int64]int64 // 名额 → 正在出图的账号
	Jobs    map[int64]LaneJob
	Ready   map[int64]SendWindow
	Offline map[int64]bool
}

// AccountLane 是界面上一张账号卡。
type AccountLane struct {
	ID          int64
	Platform    string
	Name        string
	Extension   bool
	State       LaneState
	NextReadyAt time.Time
	Gap         time.Duration
	UsedToday   int
	Cap         int
	Action      string
	Job         *LaneJob
}

// Board 是这一帧全部账号。
type Board struct {
	Lanes  []AccountLane
	Active int
}

// LaneFact 是映射成车道之前的原始事实。测试直接构造它。
type LaneFact struct {
	ID        int64
	Platform  string
	Name      string
	Extension bool
	LoggedIn  bool
	Health    string
	CoolUntil int64
	LastError string
	UsedToday int
	Cap       int
	Busy      bool
	Offline   bool
	NextReady time.Time
	Gap       time.Duration
	Job       *LaneJob
}

type laneClock struct {
	busy  map[int64]int64
	jobs  map[int64]LaneJob
	ready map[int64]SendWindow
}

func (c laneClock) snapshot() LaneLive {
	return LaneLive{Busy: cloneMap(c.busy), Jobs: cloneJobs(c.jobs), Ready: cloneGaps(c.ready)}
}

func cloneMap(in map[int64]int64) map[int64]int64 {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int64]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneJobs(in map[int64]LaneJob) map[int64]LaneJob {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int64]LaneJob, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneGaps(in map[int64]SendWindow) map[int64]SendWindow {
	if len(in) == 0 {
		return nil
	}
	out := make(map[int64]SendWindow, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// accountSlot 和调度名额是同一条规则：本机浏览器用自己的 ID，
// 同一个扩展配置文件的豆包和千问共用最小的那个 ID。
func accountSlot(rows []store.Browser, id int64) int64 {
	if id == 0 {
		return 0
	}
	account := ""
	for _, b := range rows {
		if b.ID == id {
			account = b.ExtAccount
			break
		}
	}
	if account == "" {
		return id
	}
	slot := id
	for _, b := range rows {
		if b.ExtAccount == account && b.ID < slot {
			slot = b.ID
		}
	}
	return slot
}

// MapLane 把一条账号收成界面上的状态。出图中优先于其余；扩展没连上是离线，
// 本机没登录是需要登录；满额、验证码、冷却都比发送间隔优先。
func MapLane(f LaneFact, now time.Time) AccountLane {
	cap := f.Cap
	if cap <= 0 {
		cap = DefaultDailyCap
	}
	name := strings.TrimSpace(f.Name)
	if name == "" {
		name = f.Platform
	}
	ln := AccountLane{
		ID: f.ID, Platform: f.Platform, Name: name, Extension: f.Extension,
		UsedToday: f.UsedToday, Cap: cap, NextReadyAt: f.NextReady, Gap: f.Gap,
	}
	switch {
	case f.Busy || f.Job != nil:
		ln.State = LaneRunning
		ln.Job = f.Job
	case f.Extension && f.Offline:
		ln.State = LaneOffline
	case needsLogin(f, now):
		ln.State = LaneLogin
		ln.Action = "去重登"
	case needsCaptcha(f, now):
		ln.State = LaneCaptcha
		ln.Action = "去处理"
	case dayFull(f, now):
		ln.State = LaneFull
	case f.Offline:
		ln.State = LaneOffline
		if !f.Extension {
			ln.Action = "去处理"
		}
	case cooling(f, now):
		ln.State = LaneCooling
	case !f.NextReady.IsZero() && now.Before(f.NextReady):
		ln.State = LaneGap
	default:
		ln.State = LaneIdle
	}
	return ln
}

func needsLogin(f LaneFact, now time.Time) bool {
	if !f.LoggedIn {
		return true
	}
	if !strings.Contains(f.LastError, "登录") {
		return false
	}
	return !factUsable(f, now)
}

func needsCaptcha(f LaneFact, now time.Time) bool {
	if !strings.Contains(f.LastError, "验证") && !strings.Contains(f.LastError, "滑块") {
		return false
	}
	return !factUsable(f, now)
}

func dayFull(f LaneFact, now time.Time) bool {
	cap := f.Cap
	if cap <= 0 {
		cap = DefaultDailyCap
	}
	if f.UsedToday >= cap {
		return true
	}
	if !strings.Contains(f.LastError, "额度") && !strings.Contains(f.LastError, "上限") {
		return false
	}
	return !factUsable(f, now)
}

func cooling(f LaneFact, now time.Time) bool {
	if factUsable(f, now) {
		return false
	}
	return f.Health == store.HealthCooling || f.Health == store.HealthExhausted
}

func factUsable(f LaneFact, now time.Time) bool {
	return (store.Browser{Health: f.Health, CoolUntil: f.CoolUntil}).Usable(now)
}

// Assemble 用配置库里的账号和调度器的占用拼出车道。
func Assemble(rows []store.Browser, live LaneLive, cap int, now time.Time) Board {
	if cap <= 0 {
		cap = DefaultDailyCap
	}
	var board Board
	for _, b := range rows {
		slot := accountSlot(rows, b.ID)
		fact := LaneFact{
			ID: b.ID, Platform: b.Platform, Name: b.Name, Extension: b.ExtAccount != "",
			LoggedIn: b.IsLoggedIn, Health: b.Health, CoolUntil: b.CoolUntil, LastError: b.LastError,
			UsedToday: b.UsedToday(now), Cap: cap, Offline: live.Offline[b.ID],
		}
		if !fact.Offline && !b.Usable(now) && (strings.Contains(b.LastError, "未连接") || strings.Contains(b.LastError, "离线")) {
			fact.Offline = true
		}
		if g, ok := live.Ready[slot]; ok {
			fact.NextReady = g.Until
			fact.Gap = g.Span
		}
		if worker, ok := live.Busy[slot]; ok {
			fact.Busy = true
			if worker == b.ID {
				if job, ok := live.Jobs[b.ID]; ok {
					job := job
					fact.Job = &job
				}
			}
		}
		ln := MapLane(fact, now)
		if ln.State == LaneRunning {
			board.Active++
		}
		board.Lanes = append(board.Lanes, ln)
	}
	return board
}

// BoardFrom 在没有引擎时，用队列里正在跑的任务拼车道。截图和界面测试走这里。
func BoardFrom(rows []store.Browser, items []Task, cap int, now time.Time) Board {
	live := LaneLive{Busy: map[int64]int64{}, Jobs: map[int64]LaneJob{}}
	for _, task := range items {
		if task.Status != StatusRunning && task.Status != StatusRetry {
			continue
		}
		if task.BrowserID <= 0 {
			continue
		}
		live.Busy[accountSlot(rows, task.BrowserID)] = task.BrowserID
		live.Jobs[task.BrowserID] = jobOf(task, now.Add(-time.Duration(4+task.Index)*time.Second))
	}
	return Assemble(rows, live, cap, now)
}

func jobOf(t Task, started time.Time) LaneJob {
	img := ""
	if len(t.Images) > 0 {
		img = t.Images[0]
	}
	return LaneJob{Index: t.Index, Prompt: t.Prompt, Image: img, Started: started}
}

func (e *Engine) beginLanes() {
	e.liveMu.Lock()
	e.clock.busy = map[int64]int64{}
	e.clock.jobs = map[int64]LaneJob{}
	e.clock.ready = map[int64]SendWindow{}
	e.liveMu.Unlock()
}

func (e *Engine) finishLanes() {
	e.liveMu.Lock()
	e.clock.busy = nil
	e.clock.jobs = nil
	e.clock.ready = nil
	e.liveMu.Unlock()
}

func (e *Engine) laneStart(slot, browserID int64, job LaneJob) {
	if e == nil || browserID == 0 {
		return
	}
	e.liveMu.Lock()
	defer e.liveMu.Unlock()
	if e.clock.busy == nil {
		e.clock.busy = map[int64]int64{}
		e.clock.jobs = map[int64]LaneJob{}
	}
	e.clock.busy[slot] = browserID
	e.clock.jobs[browserID] = job
	delete(e.clock.ready, slot)
}

func (e *Engine) laneFinish(busySlot, browserID, readySlot int64, gap time.Duration) {
	if e == nil {
		return
	}
	e.liveMu.Lock()
	defer e.liveMu.Unlock()
	delete(e.clock.busy, busySlot)
	delete(e.clock.jobs, browserID)
	if e.clock.ready == nil {
		e.clock.ready = map[int64]SendWindow{}
	}
	if gap > 0 {
		e.clock.ready[readySlot] = SendWindow{Until: time.Now().Add(gap), Span: gap}
		return
	}
	delete(e.clock.ready, readySlot)
}

// AccountBoard 是界面读的快照。调用时不占用引擎锁，和调度、页面操作可以同时进行。
func (e *Engine) AccountBoard(now time.Time, cap int) Board {
	if e == nil {
		return Board{}
	}
	e.liveMu.Lock()
	live := e.clock.snapshot()
	e.liveMu.Unlock()
	rows := e.laneRows()
	if live.Offline == nil && len(rows) > 0 {
		live.Offline = map[int64]bool{}
	}
	for _, b := range rows {
		if e.extDown(b) {
			if live.Offline == nil {
				live.Offline = map[int64]bool{}
			}
			live.Offline[b.ID] = true
		}
	}
	return Assemble(rows, live, cap, now)
}

func (e *Engine) laneRows() []store.Browser {
	if e == nil || e.store == nil {
		return nil
	}
	wantExt := e.useExtension("豆包")
	var out []store.Browser
	for _, b := range e.store.Browsers() {
		if b.Platform != "豆包" && b.Platform != "千问" {
			continue
		}
		if wantExt != (b.ExtAccount != "") {
			continue
		}
		out = append(out, b)
	}
	return out
}

func (e *Engine) extDown(b store.Browser) bool {
	if e == nil || b.ExtAccount == "" {
		return false
	}
	e.extMu.Lock()
	srv := e.extSrv
	e.extMu.Unlock()
	if srv == nil {
		return true
	}
	platform := extbridge.PlatformQianwen
	if b.Platform == "豆包" {
		platform = extbridge.PlatformDoubao
	}
	sess := srv.SessionByAccount(platform, b.ExtAccount)
	return sess == nil || !sess.Alive()
}
