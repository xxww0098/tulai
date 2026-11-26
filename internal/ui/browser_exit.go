package ui

import (
	"context"
	"time"

	"github.com/xxww0098/tulai/internal/logging"
)

// handleBrowserExit 同步一个自行退出的账号浏览器：用户关掉窗口或驱动崩溃后，
// 账号行回到「打开」，数据库的运行标记和登录轮询也跟着结束。
func (a *App) handleBrowserExit(id int64) {
	if a == nil || id <= 0 {
		return
	}
	// 窗口已经没了，继续探测登录只会等一个不可达的地址。
	a.mu.Lock()
	if a.loginWatchGen != nil {
		a.loginWatchGen[id]++
	}
	a.mu.Unlock()
	a.postUI(func() {
		if a.isQuitting() {
			return
		}
		listed, inList := a.browserByID(id)
		wasRunning := inList && listed.IsRunning
		if a.store != nil {
			a.store.SetRunning(id, false)
		}
		for i := range a.browserRows {
			if a.browserRows[i].ID == id {
				a.browserRows[i].IsRunning = false
			}
		}
		// 以数据库为准：列表行可能停在打开窗口那一刻，登录确认不一定写回过这一行。
		row, known := listed, inList
		if a.store != nil {
			for _, stored := range a.store.Browsers() {
				if stored.ID == id {
					row, known = stored, true
					break
				}
			}
		}
		row.IsRunning = false
		if a.browserDrawer != nil && a.browserDrawer.row.ID == id {
			a.closeBrowserDrawer()
		}
		if !known {
			a.emit("browsers", "workbench")
			return
		}
		if wasRunning {
			logging.Log.Info("浏览器[%s] 窗口已关闭", row.Name)
		}
		// 刚添加还没登录的账号，关窗口即放弃，和启动时发现窗口已关闭的出口一致。
		if !row.IsLoggedIn && a.loginIsFresh(id) {
			a.abandonUnlogged(row)
			return
		}
		a.emit("browsers", "workbench")
	})
}

// syncRunningBrowsers reconciles cached account running state with browser.Manager
// and periodically probes visible windows so closed browsers always revert to 「打开」.
func (a *App) syncRunningBrowsers() {
	if a == nil || a.isQuitting() || a.manager == nil {
		return
	}
	changed := false
	for i := range a.browserRows {
		id := a.browserRows[i].ID
		if _, pending := a.browserPending[id]; pending {
			continue
		}
		live := a.manager.IsRunning(id)
		if a.browserRows[i].IsRunning != live {
			a.browserRows[i].IsRunning = live
			if a.store != nil {
				a.store.SetRunning(id, live)
			}
			changed = true
		}
	}
	if a.browserDrawer != nil && !a.browserDrawer.loading && !a.manager.IsRunning(a.browserDrawer.row.ID) {
		a.closeBrowserDrawer()
		changed = true
	}
	if changed {
		a.emit("browsers", "workbench")
	}
	if a.running {
		return
	}
	var probeIDs []int64
	for _, id := range a.manager.RunningVisibleIDs() {
		if _, pending := a.browserPending[id]; pending {
			continue
		}
		probeIDs = append(probeIDs, id)
	}
	if len(probeIDs) == 0 || !a.browserProbing.CompareAndSwap(false, true) {
		return
	}
	mgr := a.manager
	go func() {
		defer a.browserProbing.Store(false)
		for _, id := range probeIDs {
			if a.isQuitting() {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = mgr.ProbeVisible(ctx, id)
			cancel()
		}
	}()
}
