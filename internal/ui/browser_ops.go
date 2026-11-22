package ui

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"image"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/wizard"
)

type browserOperation struct {
	stopping bool
	batch    *browserBatchRun
}

type browserBatchRun struct {
	stopping                     bool
	total, done, failed, skipped int
}

func (b *browserBatchRun) text() string {
	verb := "启动"
	if b.stopping {
		verb = "停止"
	}
	state := "正在批量" + verb
	if b.done == b.total {
		state = "批量" + verb + "完成"
	}
	return fmt.Sprintf("%s：%d/%d，失败 %d，跳过 %d", state, b.done, b.total, b.failed, b.skipped)
}

type browserDrawerState struct {
	row           store.Browser
	cancel        context.CancelFunc
	done          chan struct{}
	inputs        chan cdp.PageInput
	held          atomic.Bool
	resuming      bool
	loading       bool
	err           string
	title, url    string
	rgba          *image.RGBA
	frameVersion  uint64
	width, height float64
	copyText      string
	copyReady     bool
}

type agyChooserState struct {
	hint      string
	home      string
	email     string
	canImport bool
	keychain  bool
	busy      bool
	retry     bool
	err       string
	status    string
}

type agyLoginState struct {
	id      int64
	home    string
	name    string
	url     string
	code    string
	err     string
	busy    bool
	retry   bool
	copied  bool
	opening bool
	sess    *agy.Login
	cancel  context.CancelFunc
	gen     int
}

type loginShot struct {
	state string
	hint  string
}

func (a *App) refreshDetected() {
	if a == nil {
		return
	}
	kind := browser.CanonicalBackend(a.browserBackend)
	rt, ok := browser.InstalledRuntime(browser.ConfigDir(), kind)
	a.camoufoxReady = ok
	a.detectedDetail = rt.Path
	if ok {
		a.detectedNote = kind + " 已安装"
		a.camoufoxStatus = kind + " 就绪"
	} else {
		a.detectedNote = kind + " 未安装"
		a.camoufoxStatus = "安装后即可添加服务商账号"
	}
}

func (a *App) selectAccountEngine(choice string) {
	next := browser.NormalizeStoredBackend(runtime.GOOS, choice)
	a.browserBackend = next
	a.browserChoice = browser.ChoiceLabel(next)
	if a.store != nil {
		a.store.Set("browser_backend", next)
	}
	a.refreshDetected()
	a.refreshWizardPlan()
	a.emit("browsers", "wizard", "workbench")
}

func (a *App) camoufoxRuntimeBadge() (string, string) {
	if a.camoufoxBusy {
		return "下载中", "accent"
	}
	if a.camoufoxReady {
		return "已安装", "success"
	}
	if strings.HasPrefix(a.camoufoxStatus, "安装失败") {
		return "安装失败", "danger"
	}
	return "未安装", "warning"
}

func (a *App) applyCamoufoxProgress(p camoufox.Progress) {
	a.applyCamoufoxProgressAt(time.Now(), p)
}

func (a *App) applyCamoufoxProgressAt(now time.Time, p camoufox.Progress) {
	if p.Text != "" {
		a.camoufoxStatus = p.Text
	}
	if p.Stage != "" && p.Stage != a.camoufoxStage {
		a.camoufoxStage = p.Stage
		a.camoufoxPercent = -1
		a.camoufoxDone, a.camoufoxTotal = 0, 0
		a.camoufoxSpeed, a.camoufoxLast = 0, 0
		a.camoufoxAt = time.Time{}
	}
	if p.Done > 0 || p.Total > 0 {
		a.camoufoxDone, a.camoufoxTotal = p.Done, p.Total
		if p.Total > 0 {
			a.camoufoxPercent = min(1, float64(p.Done)/float64(p.Total))
		} else {
			a.camoufoxPercent = -1
		}
		a.trackCamoufoxSpeed(now, p.Done)
	}
	if p.Total > 0 && p.Done >= p.Total {
		a.camoufoxSpeed = 0
	}
}

func (a *App) trackCamoufoxSpeed(now time.Time, done int64) {
	if a.camoufoxAt.IsZero() {
		a.camoufoxAt, a.camoufoxLast = now, done
		return
	}
	dt := now.Sub(a.camoufoxAt)
	if dt < 300*time.Millisecond {
		return
	}
	if delta := done - a.camoufoxLast; delta >= 0 {
		instant := int64(float64(delta) / dt.Seconds())
		if a.camoufoxSpeed == 0 {
			a.camoufoxSpeed = instant
		} else {
			a.camoufoxSpeed = (a.camoufoxSpeed*2 + instant) / 3
		}
	}
	a.camoufoxAt, a.camoufoxLast = now, done
}

func (a *App) tickCamoufoxProgress(now time.Time) {
	if !a.camoufoxBusy || a.camoufoxSpeed <= 0 || a.camoufoxAt.IsZero() {
		return
	}
	if now.Sub(a.camoufoxAt) >= 2*time.Second {
		a.camoufoxSpeed = 0
	}
}

func (a *App) camoufoxTick(done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			a.syncUI(func() {
				a.tickCamoufoxProgress(now)
				a.emit("browsers", "wizard")
			})
		}
	}
}

func (a *App) installCamoufox() {
	if a.camoufoxBusy || a.running || a.renameUpdateActive() {
		return
	}
	a.camoufoxBusy = true
	kind := browser.ChoiceLabel(a.browserBackend)
	a.camoufoxStatus = "正在准备 " + kind + "…"
	a.camoufoxPercent = -1
	a.camoufoxDone, a.camoufoxTotal, a.camoufoxSpeed, a.camoufoxLast = 0, 0, 0, 0
	a.camoufoxAt = time.Time{}
	a.camoufoxStage = ""
	a.pushProxy()
	a.emit("browsers", "wizard")
	go func() {
		done := make(chan struct{})
		go a.camoufoxTick(done)
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()
		_, err := browser.EnsureRuntime(ctx, browser.ConfigDir(), kind, func(p camoufox.Progress) {
			a.syncUI(func() {
				a.applyCamoufoxProgress(p)
				a.emit("browsers", "wizard")
			})
		})
		a.syncUI(func() {
			a.camoufoxBusy = false
			defer a.maybeInstallPending()
			if err != nil {
				a.camoufoxStatus = "安装失败：" + err.Error()
			} else {
				a.refreshDetected()
				a.refreshWizardPlan()
			}
			a.emit("browsers", "wizard", "workbench")
		})
	}()
}

func (a *App) refreshBrowsers() {
	a.loadBrowserRows()
	a.emit("browsers", "workbench")
}

func (a *App) loadBrowserRows() {
	if a == nil || a.store == nil {
		return
	}
	rows := a.store.Browsers()
	missing := maps.Clone(a.browserChecked)
	for i := range rows {
		delete(missing, rows[i].ID)
		if a.manager != nil && rows[i].ExtAccount == "" {
			rows[i].IsRunning = a.manager.IsRunning(rows[i].ID)
		}
	}
	a.browserRows = rows
	if a.browserSel >= len(rows) {
		a.browserSel = len(rows) - 1
	}
	for id := range missing {
		delete(a.browserChecked, id)
	}
}

func (a *App) selectedBrowser() (store.Browser, bool) {
	if a.browserSel >= 0 && a.browserSel < len(a.browserRows) {
		return a.browserRows[a.browserSel], true
	}
	return store.Browser{}, false
}

func (a *App) browserByID(id int64) (store.Browser, bool) {
	for _, r := range a.browserRows {
		if r.ID == id {
			return r, true
		}
	}
	return store.Browser{}, false
}

func (a *App) createBrowser(platform, name string) {
	if a == nil || a.running || a.store == nil {
		return
	}
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "豆包"
	}
	if platform == agy.Platform {
		a.addAgyAccount()
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		var existing []string
		for _, r := range a.configuredBrowsers() {
			existing = append(existing, r.Name)
		}
		name = wizard.NextInstanceName(platform, existing)
	}
	kind := browser.CanonicalBackend(a.browserBackend)
	dir := ""
	if strings.TrimSpace(a.dataRoot) != "" {
		dir = browser.ProfileDir(a.dataRoot, kind, "account-"+rand.Text())
	}
	row, err := a.store.AddManagedBrowser(platform, name, dir, kind)
	if err != nil {
		logging.Log.Errorf("添加账号失败: %v", err)
		return
	}
	a.markFreshLogin(row.ID)
	a.refreshBrowsers()
	a.selectBrowserID(row.ID)
	a.launchBrowserInstance(row, nil)
}

func (a *App) selectBrowserID(id int64) {
	for i := range a.browserRows {
		if a.browserRows[i].ID == id {
			a.browserSel = i
			return
		}
	}
}

func (a *App) beginRename(b store.Browser) {
	a.renameID = b.ID
	a.renameDraft = b.Name
	a.selectBrowserID(b.ID)
}

func (a *App) commitRename() {
	id := a.renameID
	name := strings.TrimSpace(a.renameDraft)
	a.renameID = 0
	if id <= 0 || name == "" || a.store == nil {
		return
	}
	a.store.RenameBrowser(id, name)
	for i := range a.browserRows {
		if a.browserRows[i].ID == id {
			a.browserRows[i].Name = name
		}
	}
	if a.wizardAccount.ID == id {
		a.wizardAccount.Name = name
		a.wizardName = name
	}
	a.refreshBrowsers()
}

func (a *App) cancelRename() {
	a.renameID = 0
}

func (a *App) browserOperationAllowed(b store.Browser, stopping bool) bool {
	if a.running || b.ID <= 0 || b.ExtAccount != "" || b.IsRunning != stopping {
		return false
	}
	if _, pending := a.browserPending[b.ID]; pending {
		return false
	}
	return a.engine == nil || !a.engine.ActiveAccounts()[b.ID]
}

func (a *App) beginBrowserOperation(b store.Browser, stopping bool, batch *browserBatchRun) bool {
	if !a.browserOperationAllowed(b, stopping) {
		if batch != nil {
			batch.total--
			batch.skipped++
		}
		return false
	}
	if a.browserPending == nil {
		a.browserPending = map[int64]browserOperation{}
	}
	a.browserPending[b.ID] = browserOperation{stopping: stopping, batch: batch}
	return true
}

func (a *App) finishBrowserOperation(id int64, err error) {
	op, ok := a.browserPending[id]
	if ok {
		delete(a.browserPending, id)
		if b := op.batch; b != nil {
			b.done++
			if err != nil {
				b.failed++
			}
			if b.done >= b.total {
				a.browserBatch = nil
			}
		}
	}
	a.refreshBrowsers()
}

func (a *App) runBrowserBatch(stopping bool) {
	if a.running || len(a.browserPending) > 0 {
		return
	}
	var rows []store.Browser
	for _, b := range a.browserRows {
		if a.browserChecked[b.ID] && a.browserOperationAllowed(b, stopping) {
			rows = append(rows, b)
		}
	}
	if len(rows) == 0 {
		return
	}
	batch := &browserBatchRun{stopping: stopping, total: len(rows), skipped: len(a.browserChecked) - len(rows)}
	a.browserBatch = batch
	for _, b := range rows {
		if stopping {
			a.stopBrowserInstance(b, batch)
		} else {
			a.launchBrowserInstance(b, batch)
		}
	}
}

func (a *App) launchBrowserInstance(b store.Browser, batch *browserBatchRun) {
	if !a.beginBrowserOperation(b, false, batch) {
		return
	}
	platform := strategy.Factory(b.Platform)
	if platform == nil {
		logging.Log.Errorf("无法识别浏览器平台: %s", b.Platform)
		a.finishBrowserOperation(b.ID, fmt.Errorf("无法识别浏览器平台: %s", b.Platform))
		return
	}
	if a.loginLaunch != nil {
		err := a.loginLaunch(b)
		if err != nil {
			logging.Log.Errorf("启动失败: %v", err)
		}
		a.finishBrowserOperation(b.ID, err)
		return
	}
	if a.manager == nil {
		err := errors.New("没有浏览器，无法启动环境")
		logging.Log.Errorf("启动失败: %v", err)
		a.finishBrowserOperation(b.ID, err)
		return
	}
	a.pushProxy()
	a.refreshDetected()
	fresh := !b.IsLoggedIn && a.loginIsFresh(b.ID)
	a.emit("browsers")
	go func() {
		var operationErr error
		cleanupFresh := false
		defer func() {
			a.postUI(func() {
				if cleanupFresh && fresh {
					a.abandonUnlogged(b)
				}
				a.finishBrowserOperation(b.ID, operationErr)
			})
		}()
		ws, err := a.manager.Start(b, a.dataRoot, a.browserBackend, platform.HomeURL(), false)
		if err != nil && !errors.Is(err, browser.ErrDebugPort) {
			if isClosedBrowserError(err) {
				cleanupFresh = a.noteClosedLoginBrowser(b, fresh)
				return
			}
			logging.Log.Errorf("启动失败: %v", err)
			operationErr, cleanupFresh = err, true
			return
		}
		if errors.Is(err, browser.ErrDebugPort) {
			if a.store != nil {
				a.store.SetRunning(b.ID, true)
			}
			a.watchLogin(b, loginEndpoint(b))
			return
		}
		err = cdp.OpenURLContext(context.Background(), ws, platform.HomeURL())
		if isClosedBrowserError(err) {
			if a.manager != nil {
				a.manager.StopLogin(b.ID)
			}
			cleanupFresh = a.noteClosedLoginBrowser(b, fresh)
			return
		}
		if a.store != nil {
			a.store.SetRunning(b.ID, true)
		}
		if err == nil || errors.Is(err, cdp.ErrDebugUnavailable) {
			a.watchLogin(b, ws)
		}
		if err != nil && !errors.Is(err, cdp.ErrDebugUnavailable) {
			operationErr = err
			logging.Log.Errorf("浏览器[%s] 打开登录页失败：%v", b.Name, err)
		} else if err != nil {
			logging.Log.Warning("浏览器[%s] 页面控制尚未就绪，已打开登录页", b.Name)
		} else {
			logging.Log.Success("浏览器[%s]已打开%s，可在浏览器中登录账号", b.Name, b.Platform)
		}
	}()
}

func (a *App) noteClosedLoginBrowser(b store.Browser, fresh bool) bool {
	if fresh {
		logging.Log.Info("浏览器[%s] 未登录，已从列表移除", b.Name)
		return true
	}
	if a.store != nil {
		a.store.SetRunning(b.ID, false)
	}
	logging.Log.Info("浏览器[%s] 窗口已关闭", b.Name)
	return false
}

func isClosedBrowserError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "target page, context or browser has been closed") ||
		strings.Contains(lower, "browser has been closed") ||
		strings.Contains(lower, "target closed") ||
		strings.Contains(lower, "camoufox driver closed") ||
		strings.Contains(lower, "账号窗口已关闭") ||
		strings.Contains(lower, "账号浏览器已关闭")
}

func (a *App) stopBrowserInstance(b store.Browser, batch *browserBatchRun) {
	if !a.beginBrowserOperation(b, true, batch) {
		return
	}
	if a.manager == nil || !a.manager.IsRunning(b.ID) {
		a.finishStopBrowser(b)
		return
	}
	go func() {
		a.manager.Stop(b.ID)
		a.postUI(func() { a.finishStopBrowser(b) })
	}()
}

func (a *App) finishStopBrowser(b store.Browser) {
	if a.store != nil {
		for _, row := range a.store.Browsers() {
			if row.ID == b.ID {
				b.IsLoggedIn = row.IsLoggedIn
				break
			}
		}
	}
	if !b.IsLoggedIn && a.loginIsFresh(b.ID) {
		a.abandonUnlogged(b)
		logging.Log.Info("浏览器[%s] 未登录，已从列表移除", b.Name)
		a.finishBrowserOperation(b.ID, nil)
		return
	}
	if a.store != nil {
		a.store.SetRunning(b.ID, false)
	}
	logging.Log.Info("浏览器[%s] 已停止", b.Name)
	a.finishBrowserOperation(b.ID, nil)
}

func (a *App) restoreBrowser() {
	b, ok := a.selectedBrowser()
	if !ok || a.store == nil {
		return
	}
	a.restoreBrowserByRow(b)
}

func (a *App) restoreBrowserByRow(b store.Browser) {
	if a.store == nil {
		return
	}
	a.store.ResetBrowserHealth(b.ID)
	running := b.IsRunning
	if a.manager != nil {
		running = a.manager.IsRunning(b.ID)
	}
	a.store.UpdateBrowser(b.ID, running, true)
	a.clearLoginHold(b.ID)
	if a.engine != nil {
		a.engine.Wake()
	}
	logging.Log.Info("已恢复账号 %s", b.Name)
	a.refreshBrowsers()
}

func (a *App) deleteBrowser() {
	b, ok := a.selectedBrowser()
	if !ok {
		return
	}
	a.deleteBrowserByRow(b)
}

func (a *App) deleteBrowserByRow(b store.Browser) {
	ids := a.pairedProfileIDs(b)
	if a.browserDrawer != nil && a.browserDrawer.row.ID == b.ID {
		a.closeBrowserDrawer()
	}
	if a.manager != nil {
		a.manager.Stop(b.ID)
	}
	if a.store != nil {
		a.store.DeleteBrowser(b.ID)
		for _, id := range ids {
			a.store.DeleteByExtensionAccount(id)
		}
	}
	if err := browser.RemoveProfile(a.dataRoot, a.browserBackend, b); err != nil {
		logging.Log.Warning("删除账号 %s 的配置目录失败：%v", accountLabel(b), err)
	}
	logging.Log.Info("浏览器[%s] 已删除", b.Name)
	a.browserSel = -1
	a.refreshBrowsers()
}

func (a *App) pairedProfileIDs(b store.Browser) []string {
	if id := strings.TrimSpace(b.ExtAccount); id != "" {
		return []string{id}
	}
	return nil
}

func (a *App) saveAccountProxy(id int64, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") || u.Host == "" {
			return fmt.Errorf("代理格式无效，请填 http://主机:端口 或 socks5://主机:端口")
		}
	}
	if a.store != nil {
		a.store.SetBrowserProxy(id, raw)
	}
	a.refreshBrowsers()
	return nil
}

func (a *App) regenerateExtensionToken() {
	if a.store == nil {
		return
	}
	token, err := extbridge.NewToken()
	if err != nil {
		a.extTokenNote = "生成令牌失败：" + err.Error()
		return
	}
	a.extToken = token
	a.store.Set(extbridge.SettingExtToken, token)
	a.extTokenNote = "已重新生成令牌，请在扩展设置中更新"
	a.emit("browsers")
}

func (a *App) bridgePairLabel() string {
	if a.engine == nil {
		return "自动对接未启动，可复制令牌手动填写"
	}
	listening, paired := a.engine.PairState()
	if !listening {
		return "自动对接未启动，可复制令牌手动填写"
	}
	if paired {
		return "已自动对接"
	}
	return "等待扩展自动对接，也可复制令牌手动填写"
}

func accountHealthLabel(b store.Browser, active, signedIn bool, now time.Time) string {
	if b.Disabled && !active {
		return "已禁用"
	}
	if !b.Usable(now) {
		if b.Health == store.HealthCooling {
			if strings.Contains(b.LastError, "验证") {
				return "需要人工验证"
			}
			return "冷却中"
		}
		if strings.Contains(b.LastError, "登录") {
			if signedIn {
				return accountReadyLabel(b, active)
			}
			return "未登录"
		}
		return "已用尽"
	}
	return accountReadyLabel(b, active)
}

func accountReadyLabel(b store.Browser, active bool) string {
	if active {
		return "使用中"
	}
	if b.IsRunning && b.ExtAccount == "" {
		return "运行中"
	}
	return "就绪"
}

func accountHomeURL(b store.Browser) string {
	p := strategy.Factory(b.Platform)
	if p == nil {
		return ""
	}
	return p.HomeURL()
}

func loginEndpoint(b store.Browser) string {
	return browser.AccountEndpoint(b)
}

// ---- Login tracking & drawer ----

func (a *App) markFreshLogin(id int64) {
	if a == nil || id <= 0 {
		return
	}
	a.mu.Lock()
	if a.loginFresh == nil {
		a.loginFresh = map[int64]struct{}{}
	}
	a.loginFresh[id] = struct{}{}
	a.mu.Unlock()
}

func (a *App) loginIsFresh(id int64) bool {
	if a == nil || id <= 0 {
		return false
	}
	a.mu.Lock()
	_, ok := a.loginFresh[id]
	a.mu.Unlock()
	return ok
}

func (a *App) clearLoginHold(id int64) {
	a.mu.Lock()
	delete(a.loginHold, id)
	a.mu.Unlock()
}

func (a *App) setLoginShot(id int64, state, hint string) {
	a.mu.Lock()
	if a.loginShots == nil {
		a.loginShots = map[int64]loginShot{}
	}
	a.loginShots[id] = loginShot{state: state, hint: hint}
	a.mu.Unlock()
}

func (a *App) acceptLogin(id int64) {
	if a == nil || id <= 0 {
		return
	}
	a.mu.Lock()
	delete(a.loginFresh, id)
	if a.loginWatchGen != nil {
		a.loginWatchGen[id]++
	}
	a.mu.Unlock()
	if a.store != nil {
		a.store.ConfirmLogin(id)
	}
	a.setLoginShot(id, "in", "已登录")
	if a.engine != nil {
		a.engine.Wake()
	}
	a.refreshBrowsers()
}

func (a *App) abandonUnlogged(b store.Browser) {
	if a == nil || b.ID <= 0 || a.store == nil {
		return
	}
	a.mu.Lock()
	delete(a.loginFresh, b.ID)
	a.mu.Unlock()
	if a.manager != nil {
		a.manager.Stop(b.ID)
	}
	a.store.DeleteBrowser(b.ID)
	_ = browser.RemoveProfile(a.dataRoot, a.browserBackend, b)
	a.refreshBrowsers()
}

func (a *App) watchLogin(b store.Browser, endpoint string) {
	if a == nil || b.ID <= 0 || endpoint == "" {
		return
	}
	a.mu.Lock()
	if a.loginWatchGen == nil {
		a.loginWatchGen = map[int64]int{}
	}
	a.loginWatchGen[b.ID]++
	gen := a.loginWatchGen[b.ID]
	poll := a.loginPoll
	a.mu.Unlock()
	if poll <= 0 {
		poll = 2 * time.Second
	}
	go func() {
		for {
			time.Sleep(poll)
			a.mu.Lock()
			stale := a.quitting || a.loginWatchGen[b.ID] != gen
			a.mu.Unlock()
			if stale {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			state, err := cdp.ProbeLogin(ctx, endpoint)
			cancel()
			if err == nil && state == "in" {
				a.postUI(func() {
					a.acceptLogin(b.ID)
					if a.loginFollow != nil {
						a.loginFollow(b, endpoint)
					}
				})
				return
			}
		}
	}()
}

// ---- CDP Browser Drawer ----

func (a *App) openBrowserDrawer(row store.Browser) {
	home := accountHomeURL(row)
	if row.ExtAccount != "" || home == "" {
		return
	}
	previous := a.browserDrawer
	if previous != nil && previous.row.ID == row.ID && previous.err == "" {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &browserDrawerState{
		row:     row,
		cancel:  cancel,
		done:    make(chan struct{}),
		inputs:  make(chan cdp.PageInput, 128),
		loading: true,
	}
	a.browserDrawer = s
	a.mu.Lock()
	a.browserViewingID = row.ID
	if a.loginWatchGen == nil {
		a.loginWatchGen = map[int64]int{}
	}
	a.loginWatchGen[row.ID]++
	a.mu.Unlock()
	var detached <-chan struct{}
	if previous != nil {
		detached = previous.done
		previous.stop(a.engine)
	}
	a.emit("browserDrawer")
	root := a.dataRoot
	go func() {
		defer close(s.done)
		// Keep detach and input in order even if several accounts are selected rapidly.
		if detached != nil {
			<-detached
		}
		if ctx.Err() != nil {
			return
		}
		endpoint, err := a.startDrawerBrowser(ctx, row, root, home)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			a.browserDrawerUpdate(s, func() {
				s.loading = false
				s.err = "无法打开内置页面：" + err.Error()
			})
			return
		}
		// A reused account browser may no longer have its platform tab.
		connectCtx, stop := context.WithTimeout(ctx, 75*time.Second)
		err = cdp.OpenURLBackground(connectCtx, endpoint, home)
		var page *cdp.PageView
		if err == nil {
			page, err = cdp.AttachPageView(connectCtx, endpoint, home)
		}
		stop()
		if err != nil {
			a.browserDrawerUpdate(s, func() {
				s.loading = false
				s.err = "连接内置页面失败：" + err.Error()
			})
			return
		}
		defer page.Close()
		s.held.Store(true)
		a.runBrowserDrawer(ctx, s, page)
	}()
}

func (a *App) startDrawerBrowser(ctx context.Context, row store.Browser, root, home string) (string, error) {
	if a.manager == nil {
		return "", errors.New("没有浏览器管理器")
	}
	addon, err := a.engine.BoundAddonDir(row)
	if err != nil {
		logging.Log.Warning("账号[%s] 未能预装页面桥：%v", row.Name, err)
	}
	return a.manager.StartView(ctx, row, root, home, addon)
}

func (s *browserDrawerState) releaseManual(engine *tasks.Engine) {
	s.held.Store(false)
	if engine != nil {
		engine.Wake()
	}
}

func (s *browserDrawerState) stop(engine *tasks.Engine) {
	if s.cancel != nil {
		s.cancel()
	}
	s.releaseManual(engine)
}

func (a *App) closeBrowserDrawer() {
	s := a.browserDrawer
	if s == nil {
		return
	}
	a.browserDrawer = nil
	a.mu.Lock()
	if a.browserViewingID == s.row.ID {
		a.browserViewingID = 0
	}
	a.mu.Unlock()
	s.stop(a.engine)
	a.emit("browserDrawer")
}

func (a *App) browserDrawerUpdate(s *browserDrawerState, fn func()) {
	a.postUI(func() {
		if a.browserDrawer != s || fn == nil {
			return
		}
		fn()
		a.emit("browserDrawer")
	})
}

func (a *App) browserDrawerBusy(id int64) bool {
	if a == nil || a.browserPending == nil {
		return false
	}
	_, ok := a.browserPending[id]
	return ok
}

func (a *App) runBrowserDrawer(ctx context.Context, s *browserDrawerState, page *cdp.PageView) {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	lastProbe := time.Time{}
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case in := <-s.inputs:
			if in.Kind == "resume" {
				s.releaseManual(a.engine)
				a.browserDrawerUpdate(s, func() { s.resuming = false })
				continue
			}
			if ctx.Err() != nil || !s.held.Load() || a.browserDrawerBusy(s.row.ID) {
				continue
			}
			opCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			text, err := page.Input(opCtx, in)
			stop()
			if err != nil {
				a.browserDrawerUpdate(s, func() { s.err = "网页操作失败，请重新连接：" + err.Error() })
				return
			}
			if (in.Kind == "copy" || in.Kind == "cut") && text != "" {
				a.browserDrawerUpdate(s, func() {
					a.copyText(text)
					s.copyText, s.copyReady = text, true
				})
			}
		case <-tick.C:
			frameCtx, stop := context.WithTimeout(ctx, 20*time.Second)
			frame, err := page.Frame(frameCtx)
			var img image.Image
			if err == nil {
				img, _, err = image.Decode(bytes.NewReader(frame.JPEG))
			}
			if err != nil {
				stop()
				a.browserDrawerUpdate(s, func() { s.loading, s.err = false, "浏览器连接已断开，请重新连接："+err.Error() })
				return
			}
			if time.Since(lastProbe) >= 2*time.Second {
				lastProbe = time.Now()
				if state, err := page.LoginState(frameCtx); err == nil && state == "in" {
					a.acceptLogin(s.row.ID)
				}
			}
			stop()
			rgba := toRGBA(img)
			a.browserDrawerUpdate(s, func() {
				s.rgba = rgba
				s.frameVersion++
				s.width, s.height = frame.Width, frame.Height
				s.title, s.url, s.loading = frame.Title, frame.URL, false
			})
		}
	}
}

func (a *App) showAccountWindow(row store.Browser) {
	if row.Platform == agy.Platform {
		a.openAgyLane(row)
		return
	}
	if a.accountOpen != nil {
		if err := a.accountOpen(row); err != nil {
			logging.Log.Errorf("%s 打开窗口失败: %v", accountLabel(row), err)
		}
		return
	}
	a.selectBrowserID(row.ID)
	a.launchBrowserInstance(row, nil)
}

// ---- Antigravity ops ----

func (a *App) agySignature() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.agyBin) + "\n" + strings.TrimSpace(a.agyHomeRoot) + "\n" + strings.TrimSpace(a.agyModel) + "\n" + fmt.Sprintf("%d", a.agyTimeoutSec())
}

func (a *App) agyTimeoutSec() int {
	m := 6.0
	if a != nil && a.agyTimeoutMin > 0 {
		m = a.agyTimeoutMin
	}
	sec := int(m * 60)
	if sec < 60 {
		sec = 60
	}
	if sec > 3600 {
		sec = 3600
	}
	return sec
}

func (a *App) persistAgySettings() {
	if a == nil || a.store == nil {
		return
	}
	sig := a.agySignature()
	if sig == a.agySaved {
		return
	}
	a.agySaved = sig
	bin := strings.TrimSpace(a.agyBin)
	if bin == agy.DefaultBin() {
		bin = ""
	}
	root := strings.TrimSpace(a.agyHomeRoot)
	if root == agy.HomeRoot("", a.store.File()) {
		root = ""
	}
	model := strings.TrimSpace(a.agyModel)
	if model == agy.DefaultModel {
		model = ""
	}
	a.store.Set("agy_bin", bin)
	a.store.Set("agy_home_root", root)
	a.store.Set("agy_model", model)
	a.store.SetInt("agy_timeout_seconds", a.agyTimeoutSec())
}

func (a *App) attachAgyLanes(board tasks.Board, rows []store.Browser) tasks.Board {
	if a == nil || len(board.Lanes) == 0 {
		return board
	}
	byID := map[int64]store.Browser{}
	for _, b := range rows {
		byID[b.ID] = b
	}
	for i := range board.Lanes {
		ln := &board.Lanes[i]
		b, ok := byID[ln.ID]
		if !ok || b.Platform != agy.Platform {
			continue
		}
		if strings.TrimSpace(b.UserDataDir) != "" {
			ln.Email = agy.Email(b.UserDataDir)
		}
		ln.CoolUntil = b.CoolUntil
		if strings.TrimSpace(b.UserDataDir) != "" && !b.IsLoggedIn && !b.Disabled && ln.State != tasks.LaneRunning {
			ln.State = tasks.LaneLogin
			ln.Action = "去登录"
		}
	}
	return board
}

func (a *App) agyPauseLine(now time.Time) string {
	pauseAt, paused := tasksAgyPaused(a.configuredBrowsers(), now, a.shownDailyCapFor(agy.Platform))
	if !paused || pauseAt.IsZero() {
		return ""
	}
	return "Antigravity 全部限流，预计恢复 " + pauseAt.Local().Format("15:04:05")
}

func tasksAgyPaused(rows []store.Browser, now time.Time, capN int) (time.Time, bool) {
	return pausedAgy(rows, now, capN)
}

func pausedAgy(rows []store.Browser, now time.Time, capN int) (time.Time, bool) {
	var total int
	var earliest time.Time
	for _, b := range rows {
		if b.Platform != agy.Platform || b.Disabled {
			continue
		}
		total++
		if b.Usable(now) && (capN <= 0 || b.UsedToday(now) < capN) {
			return time.Time{}, false
		}
		if b.Health == store.HealthCooling && b.CoolUntil > 0 {
			coolAt := time.Unix(b.CoolUntil, 0)
			if coolAt.After(now) && (earliest.IsZero() || coolAt.Before(earliest)) {
				earliest = coolAt
			}
		}
	}
	return earliest, total > 0 && !earliest.IsZero()
}

func newAgyChooser() *agyChooserState {
	s := &agyChooserState{}
	det := agy.DetectLocal()
	s.hint = det.Hint
	s.home = det.Home
	s.email = det.Email
	s.canImport = det.CanImport()
	s.keychain = det.Keychain
	return s
}

func (a *App) addAgyAccount() {
	if a == nil || a.running {
		return
	}
	a.closeAgyLogin()
	a.agyChooser = newAgyChooser()
	a.emit("browsers")
}

func (a *App) startAgyOAuth() {
	if a == nil || a.running {
		return
	}
	if a.agyChooser != nil && a.agyChooser.busy {
		return
	}
	if a.store == nil {
		logging.Log.Errorf("还没有配置库，无法添加 Antigravity 账号")
		return
	}
	root := a.agyAccountRoot()
	dir, name, err := agy.NextAccountDir(root)
	if err != nil {
		logging.Log.Errorf("创建 Antigravity 账号目录失败: %v", err)
		return
	}
	row, err := a.store.AddBrowser(agy.Platform, name, dir)
	if err != nil {
		logging.Log.Errorf("添加 Antigravity 账号失败: %v", err)
		_ = os.RemoveAll(dir)
		return
	}
	if a.wizardOn {
		a.wizardAgy.created = row.ID
	}
	a.agyChooser = nil
	a.refreshBrowsers()
	a.beginAgyLogin(row)
	a.emit("browsers", "wizard")
}

func (a *App) agyAccountRoot() string {
	root := ""
	if a != nil {
		root = strings.TrimSpace(a.agyHomeRoot)
	}
	if root == "" && a != nil && a.store != nil {
		root = agy.HomeRoot("", a.store.File())
	}
	if root == "" {
		root = "agy-homes"
	}
	return root
}

func (a *App) agyEmails() []string {
	if a == nil {
		return nil
	}
	var out []string
	for _, b := range a.configuredBrowsers() {
		if b.Platform != agy.Platform || strings.TrimSpace(b.UserDataDir) == "" {
			continue
		}
		if email := agy.Email(b.UserDataDir); email != "" {
			out = append(out, email)
		}
	}
	return out
}

func (a *App) agySelectImportFolder(folder string) {
	if a == nil || a.agyChooser == nil || a.agyChooser.busy {
		return
	}
	det := agy.DetectAt(folder)
	a.agyChooser.home = det.Home
	a.agyChooser.email = det.Email
	a.agyChooser.hint = det.Hint
	a.agyChooser.canImport = det.CanImport()
	a.agyChooser.keychain = det.Keychain
	a.agyChooser.err = ""
	a.emit("browsers", "wizard")
}

func (a *App) startAgyImport() {
	if a == nil || a.running || a.agyChooser == nil || a.agyChooser.busy {
		return
	}
	s := a.agyChooser
	if !s.canImport || strings.TrimSpace(s.home) == "" {
		s.err = agy.MsgNotFound
		s.status = ""
		return
	}
	if a.store == nil {
		logging.Log.Errorf("还没有配置库，无法添加 Antigravity 账号")
		return
	}
	if agy.WithinAccountRoot(s.home, a.agyAccountRoot()) || agy.EmailUsed(s.email, a.agyEmails()) {
		s.err = agy.ErrAlreadyImported.Error()
		s.retry = false
		return
	}
	s.busy = true
	s.err = ""
	s.status = ""
	s.retry = false
	req := agy.ImportRequest{
		SourceHome:        s.home,
		AccountRoot:       a.agyAccountRoot(),
		ExistingEmails:    a.agyEmails(),
		Bin:               strings.TrimSpace(a.agyBin),
		Model:             strings.TrimSpace(a.agyModel),
		Timeout:           agy.VerifyTimeout,
		AllowMissingToken: s.keychain,
	}
	a.emit("browsers", "wizard")
	go func() {
		res, err := agy.Import(context.Background(), req)
		a.postUI(func() { a.finishAgyImport(res, err) })
	}()
}

func (a *App) finishAgyImport(res agy.ImportResult, err error) {
	if a == nil {
		if err == nil && res.Home != "" {
			_ = os.RemoveAll(res.Home)
		}
		return
	}
	if err != nil {
		if a.agyChooser != nil {
			a.agyChooser.busy = false
			a.agyChooser.retry = true
			a.agyChooser.status = ""
			a.agyChooser.err = err.Error()
		}
		a.emit("browsers", "wizard")
		return
	}
	if a.store == nil {
		_ = os.RemoveAll(res.Home)
		if a.agyChooser != nil {
			a.agyChooser.busy = false
			a.agyChooser.retry = true
			a.agyChooser.err = "还没有配置库，无法添加 Antigravity 账号"
		}
		a.emit("browsers", "wizard")
		return
	}
	row, addErr := a.store.AddBrowser(agy.Platform, res.Name, res.Home)
	if addErr != nil {
		_ = os.RemoveAll(res.Home)
		logging.Log.Errorf("添加 Antigravity 账号失败: %v", addErr)
		if a.agyChooser != nil {
			a.agyChooser.busy = false
			a.agyChooser.retry = true
			a.agyChooser.err = "添加 Antigravity 账号失败"
		}
		a.emit("browsers", "wizard")
		return
	}
	a.resetAgyUsage(row.ID)
	a.refreshBrowsers()
	if a.engine != nil {
		a.engine.Wake()
	}
	who := strings.TrimSpace(res.Email)
	if who == "" {
		who = res.Name
	}
	logging.Log.Success("%s：%s", agy.MsgImported, who)
	a.agyChooser = nil
	a.agyAccountAdded()
	a.emit("browsers", "wizard")
}

func (a *App) beginAgyLogin(row store.Browser) {
	if a == nil || row.ID <= 0 {
		return
	}
	if a.agyLogin != nil && a.agyLogin.sess != nil {
		a.agyLogin.sess.Close()
	}
	if a.agyLogin != nil && a.agyLogin.cancel != nil {
		a.agyLogin.cancel()
	}
	gen := 1
	if a.agyLogin != nil {
		gen = a.agyLogin.gen + 1
	}
	state := &agyLoginState{
		id:   row.ID,
		home: row.UserDataDir,
		name: row.Name,
		gen:  gen,
		busy: true,
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	a.agyLogin = state
	bin := strings.TrimSpace(a.agyBin)
	model := strings.TrimSpace(a.agyModel)
	if bin == "" {
		bin = agy.DefaultBin()
	}
	if _, err := os.Stat(bin); err != nil {
		cancel()
		state.err = "找不到 agy：" + bin
		state.busy = false
		state.retry = true
		a.emit("browsers", "wizard")
		return
	}
	a.emit("browsers", "wizard")
	go func() {
		sess, err := agy.StartLogin(ctx, bin, row.UserDataDir, model)
		a.postUI(func() {
			if a.agyLogin != state {
				if sess != nil {
					go sess.Close()
				}
				return
			}
			state.busy = false
			if err != nil {
				state.err, state.retry = err.Error(), true
				a.emit("browsers", "wizard")
				return
			}
			state.sess = sess
			a.emit("browsers", "wizard")
			go a.watchAgyLogin(gen, sess, row.ID, row.Name, row.UserDataDir)
		})
	}()
}

func (a *App) watchAgyLogin(gen int, sess *agy.Login, id int64, name, home string) {
	if sess == nil {
		return
	}
	deadline := time.Now().Add(agy.LoginTimeout)
	for time.Now().Before(deadline) {
		rawURL := sess.URL()
		if rawURL != "" {
			a.syncUI(func() {
				if a.agyLogin != nil && a.agyLogin.gen == gen && a.agyLogin.sess == sess && a.agyLogin.url == "" {
					a.agyLogin.url = rawURL
					a.emit("browsers", "wizard")
				}
			})
			break
		}
		select {
		case <-sess.Done():
			deadline = time.Time{}
		case <-time.After(80 * time.Millisecond):
		}
		if deadline.IsZero() {
			break
		}
	}
	err := sess.Wait()
	ok := err == nil && agy.LoggedIn(home)
	a.syncUI(func() {
		if a.agyLogin == nil || a.agyLogin.gen != gen || a.agyLogin.sess != sess {
			return
		}
		if ok {
			a.finishAgyLogin(id, name)
			return
		}
		a.agyLogin.busy = false
		a.agyLogin.retry = true
		if err != nil {
			a.agyLogin.err = err.Error()
		} else {
			a.agyLogin.err = "登录未完成，请点重试"
		}
		a.emit("browsers", "wizard")
	})
}

func (a *App) finishAgyLogin(id int64, name string) {
	if a.store != nil {
		a.store.ConfirmLogin(id)
	}
	a.resetAgyUsage(id)
	a.agyLogin = nil
	a.wizardAgy.created = 0
	if a.engine != nil {
		a.engine.Wake()
	}
	logging.Log.Success("Antigravity 账号 %s 已登录", name)
	a.refreshBrowsers()
	a.agyAccountAdded()
	a.emit("browsers", "wizard")
}

func (a *App) submitAgyCode(code string) {
	st := a.agyLogin
	if st == nil || st.sess == nil || st.busy {
		return
	}
	code = strings.TrimSpace(code)
	if code == "" {
		st.err = "请先粘贴授权码或回调链接"
		a.emit("browsers", "wizard")
		return
	}
	st.busy = true
	st.err = ""
	st.code = ""
	a.emit("browsers", "wizard")
	go func() {
		err := st.sess.Submit(code)
		a.postUI(func() {
			if a.agyLogin != st {
				return
			}
			st.busy = false
			if err != nil {
				st.err = err.Error()
			}
			a.emit("browsers", "wizard")
		})
	}()
}

func (a *App) openAgyAuthURL() {
	st := a.agyLogin
	if st == nil || strings.TrimSpace(st.url) == "" || st.opening {
		return
	}
	if a.manager == nil {
		st.err = "没有浏览器管理器，无法打开授权页"
		a.emit("browsers", "wizard")
		return
	}
	st.opening = true
	rawURL := st.url
	row := store.Browser{
		ID:       st.id,
		Platform: agy.Platform,
		Name:     st.name,
		Engine:   browser.KindCamoufox,
	}
	if existing, ok := a.browserByID(st.id); ok {
		row = existing
		row.Engine = browser.KindCamoufox
	}
	a.emit("browsers", "wizard")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		ws, err := a.manager.Start(row, a.dataRoot, browser.KindCamoufox, rawURL, false)
		if err == nil && ws != "" {
			_ = cdp.OpenURLContext(ctx, ws, rawURL)
		}
		a.postUI(func() {
			if a.agyLogin == st {
				st.opening = false
				if err != nil && !errors.Is(err, browser.ErrDebugPort) {
					st.err = "打开授权页失败：" + err.Error()
				}
				a.emit("browsers", "wizard")
			}
		})
	}()
}

func (a *App) retryAgyLogin() {
	st := a.agyLogin
	if st == nil {
		return
	}
	if row, ok := a.browserByID(st.id); ok {
		a.beginAgyLogin(row)
	}
}

func (a *App) openAgyLane(row store.Browser) {
	a.selectBrowserID(row.ID)
	a.beginAgyLogin(row)
}

func formatAgyUsageView(uv agy.UsageView) (string, int, string) {
	switch uv.State {
	case agy.UsageOK:
		pct := int(uv.Usage.FiveHour.Remaining*100 + 0.5)
		if pct < 0 {
			pct = 0
		}
		if pct > 100 {
			pct = 100
		}
		label := fmt.Sprintf("5h %d%%", pct)
		if !uv.Usage.Weekly.Reset.IsZero() || uv.Usage.Weekly.Remaining > 0 {
			wk := int(uv.Usage.Weekly.Remaining*100 + 0.5)
			label = fmt.Sprintf("5h %d%% · 周 %d%%", pct, wk)
		}
		return label, pct, ""
	case agy.UsageNeedLogin:
		return "未登录", 0, uv.Reason
	case agy.UsageFailed:
		return "读取失败", 0, uv.Reason
	default:
		if strings.TrimSpace(uv.Reason) != "" {
			return uv.Reason, 0, ""
		}
		return "读取中…", 0, ""
	}
}

var _ = filepath.Join
