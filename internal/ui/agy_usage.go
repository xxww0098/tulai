package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// usageRefreshGap 是没有新任务时，同一个账号两次读额度的最短间隔。
const usageRefreshGap = 5 * time.Minute

// agyUsageSlot 是内存里的一份额度。不把界面元素放进来。
type agyUsageSlot struct {
	view     agy.UsageView
	inflight bool
	again    bool
	done     chan struct{}
}

func (a *App) startAgyUsage() {
	if a == nil || a.store == nil || a.agyUsageStop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.agyUsageStop = cancel
	a.agyUsageWake = make(chan struct{}, 1)
	if a.engine != nil {
		eng := a.engine
		eng.SetAgyUsageRefresh(a.refreshAgyUsageBeforeRun)
		eng.SetAgyUsageKick(func(id int64) {
			a.markAgyUsageForce(id)
			a.wakeAgyUsage()
		})
	}
	go a.agyUsageLoop(ctx)
}

func (a *App) stopAgyUsage() {
	if a == nil || a.agyUsageStop == nil {
		return
	}
	a.agyUsageStop()
	a.agyUsageStop = nil
	if a.engine != nil {
		a.engine.SetAgyUsageRefresh(nil)
	}
}

func (a *App) refreshAgyUsageBeforeRun(ctx context.Context) {
	bin, rows := a.agyUsageSnap()
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		if row.Disabled {
			continue
		}
		if saved, ok := tasks.SavedAgyUsage(a.store, row.ID); ok && (!saved.Usage.FiveHour.Reset.After(time.Now()) || !saved.Usage.Weekly.Reset.After(time.Now())) {
			a.markAgyUsageForce(row.ID)
		}
		if a.beginAgyUsage(row.ID, time.Now()) {
			a.fetchAgyAccount(ctx, row, bin)
			a.finishAgyUsage(row.ID)
			continue
		}
		a.agyUsageMu.Lock()
		done := a.agyUsage[row.ID].done
		a.agyUsageMu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (a *App) wakeAgyUsage() {
	if a == nil || a.agyUsageWake == nil {
		return
	}
	select {
	case a.agyUsageWake <- struct{}{}:
	default:
	}
}

func (a *App) markAgyUsageForce(id int64) {
	if a == nil || id <= 0 {
		return
	}
	a.agyUsageMu.Lock()
	defer a.agyUsageMu.Unlock()
	if a.agyUsageForce == nil {
		a.agyUsageForce = map[int64]bool{}
	}
	a.agyUsageForce[id] = true
}

func (a *App) resetAgyUsage(id int64) {
	if a == nil || id <= 0 {
		return
	}
	tasks.ClearAgyQuota(a.store, id)
	a.publishUsage(id, agy.UsageView{State: agy.UsageReading, Reason: agy.ReasonReading})
	a.markAgyUsageForce(id)
	a.wakeAgyUsage()
}

func (a *App) agyUsageLoop(ctx context.Context) {
	a.pullDueAgyUsage(ctx)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.agyUsageWake:
			a.pullDueAgyUsage(ctx)
		case <-tick.C:
			a.pullDueAgyUsage(ctx)
		}
	}
}

func (a *App) pullDueAgyUsage(ctx context.Context) {
	if a == nil || ctx.Err() != nil {
		return
	}
	bin, rows := a.agyUsageSnap()
	now := time.Now()
	for _, row := range rows {
		if ctx.Err() != nil {
			return
		}
		if !a.beginAgyUsage(row.ID, now) {
			continue
		}
		row := row
		run := func() {
			defer a.finishAgyUsage(row.ID)
			a.fetchAgyAccount(ctx, row, bin)
		}
		if a.agyUsageSync {
			run()
			continue
		}
		go run()
	}
}

func (a *App) beginAgyUsage(id int64, now time.Time) bool {
	force := false
	a.agyUsageMu.Lock()
	if a.agyUsageForce[id] {
		force = true
	}
	slot := a.agyUsage[id]
	busy := a.usageBusy(id)
	if slot.inflight {
		if force {
			slot.again = true
			if a.agyUsage == nil {
				a.agyUsage = map[int64]agyUsageSlot{}
			}
			a.agyUsage[id] = slot
		}
		a.agyUsageMu.Unlock()
		return false
	}
	if !agyUsageDue(slot.view.At, busy, force, now, usageRefreshGap) {
		a.agyUsageMu.Unlock()
		return false
	}
	delete(a.agyUsageForce, id)
	slot.inflight = true
	slot.done = make(chan struct{})
	if a.agyUsage == nil {
		a.agyUsage = map[int64]agyUsageSlot{}
	}
	a.agyUsage[id] = slot
	a.agyUsageMu.Unlock()
	return true
}

func (a *App) finishAgyUsage(id int64) {
	a.agyUsageMu.Lock()
	slot := a.agyUsage[id]
	slot.inflight = false
	if slot.done != nil {
		close(slot.done)
		slot.done = nil
	}
	again := slot.again
	slot.again = false
	if a.agyUsage == nil {
		a.agyUsage = map[int64]agyUsageSlot{}
	}
	a.agyUsage[id] = slot
	if again {
		if a.agyUsageForce == nil {
			a.agyUsageForce = map[int64]bool{}
		}
		a.agyUsageForce[id] = true
	}
	a.agyUsageMu.Unlock()
	if again {
		a.wakeAgyUsage()
	}
}

// agyUsageDue 决定要不要再读一次。正在出图或正在读时不读。
// 从未读过，或这一轮是任务结束后的强制刷新，就读。其余至少隔 5 分钟。
func agyUsageDue(last time.Time, busy, force bool, now time.Time, gap time.Duration) bool {
	if busy {
		return false
	}
	if force || last.IsZero() {
		return true
	}
	return !now.Before(last.Add(gap))
}

func (a *App) usageBusy(id int64) bool {
	if a != nil && a.agyUsageBusy != nil {
		return a.agyUsageBusy(id)
	}
	return a != nil && a.engine != nil && a.engine.AccountBusy(id)
}

func (a *App) agyUsageSnap() (string, []store.Browser) {
	bin := agy.DefaultBin()
	if a == nil {
		return bin, nil
	}
	a.onUI(func() {
		if s := strings.TrimSpace(a.agyBin); s != "" {
			bin = s
		}
	})
	if a.store == nil {
		return bin, nil
	}
	var rows []store.Browser
	for _, b := range a.store.Browsers() {
		if b.Platform != agy.Platform || strings.TrimSpace(b.UserDataDir) == "" {
			continue
		}
		rows = append(rows, b)
	}
	return bin, rows
}

func (a *App) fetchAgyAccount(ctx context.Context, row store.Browser, bin string) {
	now := time.Now()
	if !agy.LoggedIn(row.UserDataDir) {
		a.publishUsage(row.ID, agy.UsageView{State: agy.UsageNeedLogin, Reason: agy.ReasonLogin, At: now})
		return
	}
	usage, err := a.runAgyUsageFetch(ctx, bin, row.UserDataDir)
	view, ok := agy.ViewFrom(usage, err, time.Now())
	if !ok {
		return
	}
	if errors.Is(err, agy.ErrNeedLogin) {
		// 读额度失败不要删凭证：可能是网络抖动。真正出图遇到需登录时再 ForgetToken。
		if a.store != nil {
			a.store.UpdateBrowser(row.ID, false, false)
			a.store.UpdateBrowserHealth(row.ID, store.HealthExhausted, 0, agy.ErrNeedLogin.Error())
		}
		if a.engine != nil {
			a.engine.Wake()
		}
	}
	if err == nil && a.store != nil {
		tasks.NoteAgyQuota(a.store, row.ID, usage, time.Now())
		if a.engine != nil {
			a.engine.Wake()
		}
	}
	if view.State == agy.UsageFailed {
		a.logUsageFailure(row, view)
	}
	a.publishUsage(row.ID, view)
}

func (a *App) runAgyUsageFetch(ctx context.Context, bin, home string) (agy.GeminiUsage, error) {
	if a != nil && a.agyUsageFetch != nil {
		return a.agyUsageFetch(ctx, bin, home)
	}
	return agy.FetchUsage(ctx, bin, home)
}

func (a *App) logUsageFailure(row store.Browser, view agy.UsageView) {
	a.agyUsageMu.Lock()
	prev := a.agyUsage[row.ID].view
	a.agyUsageMu.Unlock()
	if prev.State == view.State && prev.Reason == view.Reason {
		return
	}
	name := strings.TrimSpace(row.Name)
	if name == "" {
		name = agy.Platform
	}
	logging.Log.Warning("「%s」%s", name, view.Reason)
}

func (a *App) publishUsage(id int64, view agy.UsageView) {
	if a == nil || id <= 0 {
		return
	}
	a.agyUsageMu.Lock()
	if a.agyUsage == nil {
		a.agyUsage = map[int64]agyUsageSlot{}
	}
	slot := a.agyUsage[id]
	slot.view = view
	a.agyUsage[id] = slot
	a.agyUsageMu.Unlock()
	a.syncUI(func() {})
}

func (a *App) agyUsageView(id int64) agy.UsageView {
	if a == nil {
		return agy.UsageView{State: agy.UsageReading, Reason: agy.ReasonReading}
	}
	a.agyUsageMu.Lock()
	slot, ok := a.agyUsage[id]
	a.agyUsageMu.Unlock()
	if !ok || slot.view.At.IsZero() {
		if saved, found := tasks.SavedAgyUsage(a.store, id); found {
			return saved
		}
		return agy.UsageView{State: agy.UsageReading, Reason: agy.ReasonReading}
	}
	view := slot.view
	switch view.State {
	case agy.UsageOK:
		return view
	case agy.UsageNeedLogin:
		if view.Reason == "" {
			view.Reason = agy.ReasonLogin
		}
	case agy.UsageFailed:
		if view.Reason == "" {
			view.Reason = agy.FailReason(errors.New("未知错误"))
		}
	default:
		view.State = agy.UsageReading
		if view.Reason == "" {
			view.Reason = agy.ReasonReading
		}
	}
	return view
}
