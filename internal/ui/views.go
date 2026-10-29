package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
	"github.com/xxww0098/tulai/internal/wizard"
)

const browserInstanceHint = "只有登录成功的账号会留在这里。同一平台再登录一个后，额度用尽、限流或验证码会自动换号。扩展账号只保留当前连上的页面桥。"

// browserPane 浏览器管理。网页方式是 Chrome、Edge，或下载到本机的内置浏览器。
func (a *App) browserPane(c *ui.Context) {
	a.syncBrowserChoice()
	if a.detectedNote == "" && !a.cftBusy {
		a.refreshDetected()
	}
	w, _ := c.Size()
	if homeSplit(w) {
		ui.Row(c).Grow(1).MinHeight(0).Gap(16).AlignItems(ui.Stretch).Children(func() {
			ui.Scroll(c.Key("browser-setup")).Width(340).Shrink(0).MinHeight(0).Children(func() {
				ui.Column(c).Gap(12).Padding(0, 2, 12, 0).Children(func() {
					a.browserSetup(c)
				})
			})
			ui.Column(c).Grow(1).MinWidth(0).MinHeight(0).MaxWidth(720).Gap(12).Children(func() {
				a.browserInstances(c)
			})
		})
		return
	}
	ui.Column(c).Grow(1).MinHeight(0).MaxWidth(720).Gap(12).Children(func() {
		a.browserSetup(c)
		a.browserInstances(c)
	})
}

func (a *App) browserSetup(c *ui.Context) {
	ui.Field(c, "网页方式", func() {
		motionSelect(c.Key("browser-backend"), &a.browserChoice, []string{"Chrome", "Edge", "内置浏览器"}).Disabled(a.running).OnChange(func() {
			a.browserBackend = browser.CanonicalBackend(a.browserChoice)
			if a.store != nil {
				a.store.Set("browser_backend", a.browserBackend)
			}
		})
	})
	a.extensionBridgeSection(c)
	a.builtinBrowserSection(c)
}

func (a *App) browserInstances(c *ui.Context) {
	ui.Text(c, "浏览器实例").FontSize(15).Bold()
	ui.Text(c, browserInstanceHint).FontSize(12).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		motionSelect(c, &a.newBrowserPlat, []string{"豆包", "千问"}).Width(88).Shrink(0).Label("浏览器平台")
		ui.TextInput(c, &a.newBrowserName).Grow(1).MinWidth(0).Placeholder("名称（可选）").Label("浏览器名称")
		ui.Button(c, "添加").Shrink(0).Disabled(a.running).OnClick(func() { a.addBrowser() })
	})
	if hint := missingBrowserHint(a.missingBrowserNames()); hint != "" {
		ui.Text(c, hint).Padding(12, 0).FontSize(12).TextColor(c.Theme().TextMuted)
	} else if len(a.browserRows) == 0 {
		ui.Text(c, "尚无已登录的账号。添加豆包或千问，在打开的页面登录后即可使用。").Padding(12, 0).FontSize(12).TextColor(c.Theme().TextMuted)
	}
	active := map[int64]bool{}
	if a.engine != nil {
		active = a.engine.ActiveAccounts()
	}
	if a.store != nil {
		rows := a.store.Browsers()
		shown := make([]store.Browser, 0, len(rows))
		for i := range rows {
			if a.manager != nil {
				rows[i].IsRunning = a.manager.IsRunning(rows[i].ID)
			}
			if active[rows[i].ID] {
				rows[i].IsRunning = true
			}
			if rows[i].ExtAccount != "" && !rows[i].IsLoggedIn {
				continue
			}
			shown = append(shown, rows[i])
		}
		a.browserRows = shown
	}
	if len(a.browserRows) > 0 {
		ui.Scroll(c.Key("accounts")).FillWidth().Shrink(0).MaxHeight(420).Children(func() {
			ui.Column(c).FillWidth().Border(1, c.Theme().Border).Radius(12).Children(func() {
				for i := range a.browserRows {
					b := a.browserRows[i]
					idx := i
					face := c.Theme().Background
					if idx == a.browserSel {
						face = c.Theme().Selection
					}
					ui.ButtonBase(c.Key(b.ID)).FillWidth().AlignItems(ui.Stretch).Justify(ui.Start).
						Background(face).OnClick(func() { a.browserSel = idx }).Children(func() {
						a.accountRow(c, b, active[b.ID])
					})
				}
			})
		})
	}
	selected, hasSelection := a.selectedBrowser()
	ui.Row(c).Gap(8).AlignSelf(ui.Start).Children(func() {
		ui.Button(c, "启动选中").Shrink(0).Disabled(a.running || !hasSelection || selected.IsRunning || selected.ExtAccount != "").OnClick(func() { a.launchBrowser() })
		ui.Button(c, "停止选中").Shrink(0).Disabled(a.running || !hasSelection || !selected.IsRunning || selected.ExtAccount != "").OnClick(func() { a.stopBrowser() })
		ui.Button(c, "刷新列表").Shrink(0).OnClick(func() { a.refreshBrowsers() })
		ui.Button(c, "恢复选中").Shrink(0).Disabled(!hasSelection).OnClick(func() { a.restoreBrowser() })
	})
	ui.Button(c, "删除选中").Shrink(0).AlignSelf(ui.Start).Disabled(a.running || !hasSelection).TextColor(c.Theme().Danger).OnClick(func() { a.deleteBrowser() })
	ui.Text(c, "想重新登录或再添加账号，点左侧导航栏里的设置向导。").FontSize(12).TextColor(c.Theme().TextMuted)
}

// extensionBridgeSection 展示页面桥令牌和端口。扩展弹出页要填同一组值。
// 令牌单独占一行，避免侧栏里被按钮挤掉，复制时看不清。
func (a *App) extensionBridgeSection(c *ui.Context) {
	a.extToken = extbridge.NormalizeExtToken(a.extToken)
	a.extPort = extbridge.NormalizeExtPort(a.extPort)
	clip := c.Services()
	status, detail, warn := a.bridgeStatusLine()
	detailColor := c.Theme().TextMuted
	if warn {
		detailColor = c.Theme().Warning
	}
	summary := a.bridgePairLabel()
	if status != "" {
		summary += " · " + status
	}
	summary += " · 端口 " + a.extPort
	if detail == "" {
		detail = "填进扩展弹出页后保存"
	}
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, "页面桥").FontSize(15).Bold()
		if a.engine != nil {
			if notice := a.engine.ExtensionNotice(); notice != "" {
				ui.Text(c, notice).FontSize(13).TextColor(c.Theme().Warning)
			}
		}
		ui.Text(c, summary).FontSize(12).TextColor(detailColor).Tooltip(detail)
		ui.TextInput(c.Key("ext-bridge-token"), &a.extToken).ReadOnly(true).FontSize(12).Label("令牌")
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "复制令牌").OnClick(func() {
				a.extToken = extbridge.NormalizeExtToken(a.extToken)
				clip.WriteClipboard(a.extToken)
				a.extTokenNote = "已复制令牌"
			})
			ui.Button(c, "重新生成").Disabled(a.running).OnClick(func() { a.regenerateExtensionToken() })
		})
		if a.extTokenNote != "" {
			ui.Text(c, a.extTokenNote).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(a.extTokenNote)
		}
	})
}

// regenerateExtensionToken 换一枚令牌并立刻写入配置库。正在跑的任务仍用旧令牌，所以按钮在运行时关掉。
func (a *App) regenerateExtensionToken() {
	token, err := extbridge.NewToken()
	if err != nil {
		a.extTokenNote = "生成令牌失败"
		logging.Log.Errorf("生成页面桥令牌失败: %v", err)
		return
	}
	a.extToken = token
	if a.store != nil {
		a.store.Set(extbridge.SettingExtToken, token)
	}
	if a.engine != nil {
		if err := a.engine.ReloadExtensionBridge(); err != nil {
			a.extTokenNote = "已生成新令牌，但桥没有重新监听"
			logging.Log.Errorf("重新监听页面桥失败: %v", err)
			return
		}
	}
	a.extTokenNote = "已生成新令牌。扩展会自动对接；若弹出页仍是手动配对，请复制过去或点「恢复自动对接」"
	logging.Log.Info("已重新生成页面桥令牌")
}

// ---- 浏览器操作 ----

// openSelectedPages 从工作台切到浏览器页，方便添加或启动实例。
func (a *App) openSelectedPages() {
	if a.running {
		return
	}
	a.tab = 1
}

func (a *App) refreshBrowsers() {
	a.dropAbandoned()
	a.mu.Lock()
	if a.store != nil {
		a.browserRows = a.store.Browsers()
		for i := range a.browserRows {
			if a.manager != nil {
				a.browserRows[i].IsRunning = a.manager.IsRunning(a.browserRows[i].ID)
			}
		}
	}
	a.mu.Unlock()
	a.refreshDetected()
}

func (a *App) loginKeepIDs() []int64 {
	if a == nil {
		return nil
	}
	var ids []int64
	if a.wizardOn && a.wizardStep == wizard.StepAccount && a.wizardAccount.ID > 0 {
		ids = append(ids, a.wizardAccount.ID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id := range a.loginHold {
		ids = append(ids, id)
	}
	return ids
}

func (a *App) holdLogin(id int64) {
	if a == nil || id <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loginHold == nil {
		a.loginHold = map[int64]struct{}{}
	}
	a.loginHold[id] = struct{}{}
}

// markFreshLogin 记下这一轮新建、还没登录过的档案。离开向导或停掉时可以删。
// 库里已有的账号不是 fresh，标志过期也不能删。
func (a *App) markFreshLogin(id int64) {
	if a == nil || id <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loginHold == nil {
		a.loginHold = map[int64]struct{}{}
	}
	if a.loginFresh == nil {
		a.loginFresh = map[int64]struct{}{}
	}
	a.loginHold[id] = struct{}{}
	a.loginFresh[id] = struct{}{}
}

func (a *App) loginIsFresh(id int64) bool {
	if a == nil || id <= 0 {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.loginFresh[id]
	return ok
}

func (a *App) clearLoginHold(id int64) {
	if a == nil || id <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.loginHold, id)
	delete(a.loginFresh, id)
}

// dropAbandoned 只删两类本机档案：这一轮新建后放弃的（走 abandonUnlogged），
// 或探测明确返回 out 的未登录档案。标志是 0、端口没开、探测失败、地区或验证，都留下。
// 扩展行同样不能凭 is_logged_in=0 删，要等 hello 明确说没登录。
// ponytail: 端口开着时同步探测，每个账号每进程只探一次。要跟着页面变化再探，放到后台。
func (a *App) dropAbandoned() {
	if a == nil || a.store == nil || a.running {
		return
	}
	keep := map[int64]struct{}{}
	for _, id := range a.loginKeepIDs() {
		keep[id] = struct{}{}
	}
	if a.manager != nil {
		for _, b := range a.store.Browsers() {
			if b.ExtAccount == "" && a.manager.IsRunning(b.ID) {
				keep[b.ID] = struct{}{}
			}
		}
	}
	var confirmedOut []int64
	for _, b := range a.store.Browsers() {
		if b.ExtAccount != "" || b.IsLoggedIn {
			continue
		}
		if a.loginShot(b.ID).state == wizard.ProbeIn {
			a.store.ConfirmLogin(b.ID)
			a.clearLoginHold(b.ID)
			continue
		}
		if _, held := keep[b.ID]; held {
			continue
		}
		switch a.accountLoginState(b) {
		case wizard.ProbeIn:
			a.store.ConfirmLogin(b.ID)
			a.clearLoginHold(b.ID)
		case wizard.ProbeOut:
			confirmedOut = append(confirmedOut, b.ID)
		}
	}
	for _, b := range a.store.DropUnloggedLocal(confirmedOut) {
		if err := browser.RemoveProfile(a.dataRoot, a.browserBackend, b); err != nil {
			logging.Log.Warning("删除未登录账号 %s 的配置目录失败：%v", b.Name, err)
		}
	}
}

// accountLoginState 是删档案前的登录探测。出错、端口没开、地区和验证都不是 out。
func (a *App) accountLoginState(b store.Browser) string {
	if a != nil && a.accountProbe != nil {
		state, err := a.accountProbe(b)
		if err != nil {
			return wizard.ProbeUnknown
		}
		return strings.TrimSpace(state)
	}
	if a != nil {
		a.mu.Lock()
		state, ok := a.loginProbed[b.ID]
		a.mu.Unlock()
		if ok {
			return state
		}
	}
	state := wizard.ProbeUnknown
	if b.Port > 0 && debugPortOpen(b.Port) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		probed, err := cdp.ProbeLogin(ctx, loginEndpoint(b))
		cancel()
		if err == nil {
			state = strings.TrimSpace(probed)
		}
	}
	if a != nil {
		a.mu.Lock()
		if a.loginProbed == nil {
			a.loginProbed = map[int64]string{}
		}
		a.loginProbed[b.ID] = state
		a.mu.Unlock()
	}
	return state
}

// abandonUnlogged 停掉并删掉这一轮新建、还没登录成功的本机账号。
// 库里原来就有的账号不动，哪怕登录标志是过期的 0。
func (a *App) abandonUnlogged(b store.Browser) {
	if a == nil || a.store == nil || b.ID <= 0 || !a.loginIsFresh(b.ID) {
		return
	}
	a.clearLoginHold(b.ID)
	var row store.Browser
	found := false
	for _, item := range a.store.Browsers() {
		if item.ID == b.ID {
			row = item
			found = true
			break
		}
	}
	if !found || row.IsLoggedIn || row.ExtAccount != "" {
		return
	}
	if a.manager != nil {
		a.manager.Stop(row.ID)
	}
	a.store.DeleteBrowser(row.ID)
	if err := browser.RemoveProfile(a.dataRoot, a.browserBackend, row); err != nil {
		logging.Log.Warning("删除未登录账号 %s 的配置目录失败：%v", row.Name, err)
	}
}

func (a *App) syncBrowserChoice() {
	label := browser.ChoiceLabel(a.browserBackend)
	if a.browserChoice != label {
		a.browserChoice = label
	}
}

func (a *App) refreshDetected() {
	dir := browser.ConfigDir()
	a.detectedNote, a.detectedDetail = browser.FormatDetected(browser.Detect(dir))
	if rt, ok := browser.InstalledBuiltin(dir); ok {
		a.cftReady = true
		if a.cftBusy {
			return
		}
		ver := rt.Version
		if ver == "" {
			ver = browser.PinnedCfTVersion()
		}
		a.cftStatus = "内置浏览器已就绪（" + ver + "）"
		return
	}
	a.cftReady = false
	if a.cftBusy {
		return
	}
	if !browser.BuiltinSupported(runtime.GOOS, runtime.GOARCH) {
		a.cftStatus = "当前架构没有可下载的内置浏览器，请使用系统 Edge 或已安装的 Chrome"
		return
	}
	a.cftStatus = "内置浏览器未下载"
}

func (a *App) builtinBrowserSection(c *ui.Context) {
	note := a.detectedNote
	if note == "" {
		note = "正在检查本机浏览器…"
	}
	if a.cftStatus != "" {
		note += " · " + a.cftStatus
	}
	label := "下载内置浏览器"
	if a.cftReady {
		label = "重新下载内置浏览器"
	}
	supported := browser.BuiltinSupported(runtime.GOOS, runtime.GOARCH)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, note).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine().Tooltip(a.detectedDetail)
		ui.Button(c, label).Disabled(a.running || a.cftBusy || !supported).OnClick(func() { a.downloadBuiltin() })
	})
	if a.cftBusy {
		motionProgress(c, "cft-progress", a.cftPercent, a.cftPercent <= 0)
	}
}

func (a *App) downloadBuiltin() {
	if a.cftBusy || a.running {
		return
	}
	if !browser.BuiltinSupported(runtime.GOOS, runtime.GOARCH) {
		a.cftStatus = "当前架构没有可下载的内置浏览器，请使用系统 Edge 或已安装的 Chrome"
		return
	}
	force := a.cftReady
	a.cftBusy = true
	a.cftPercent = 0
	a.cftStatus = "正在下载内置浏览器…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		dir := browser.ConfigDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			a.syncUI(func() {
				a.cftBusy = false
				a.cftStatus = "下载失败：" + err.Error()
			})
			logging.Log.Errorf("下载内置浏览器失败: %v", err)
			return
		}
		var err error
		progress := func(done, total int64) {
			a.syncUI(func() {
				if total > 0 && done >= total {
					a.cftPercent = 1
					a.cftStatus = "正在解压内置浏览器…"
					return
				}
				if total > 0 && done > 0 {
					a.cftPercent = float64(done) / float64(total)
					a.cftStatus = fmt.Sprintf("正在下载内置浏览器 %d%%", done*100/total)
				}
			})
		}
		if force {
			_, err = browser.RedownloadCfT(ctx, dir, nil, progress)
		} else {
			_, err = browser.EnsureCfT(ctx, dir, nil, progress)
		}
		a.syncUI(func() {
			a.cftBusy = false
			if err != nil {
				a.cftStatus = "下载失败：" + err.Error()
				logging.Log.Errorf("下载内置浏览器失败: %v", err)
				return
			}
			logging.Log.Success("内置浏览器已就绪")
			a.refreshDetected()
			a.refreshWizardPlan()
		})
	}()
}

func (a *App) addBrowser() {
	if a == nil || a.store == nil || a.running {
		return
	}
	if a.manager == nil {
		logging.Log.Errorf("没有浏览器，无法添加账号")
		return
	}
	name := strings.TrimSpace(a.newBrowserName)
	if name == "" {
		name = fmt.Sprintf("浏览器%d", len(a.browserRows)+1)
	}
	dir := ""
	if strings.TrimSpace(a.dataRoot) != "" {
		dir = browser.ProfileDir(a.dataRoot, a.browserBackend, name)
	}
	row, err := a.store.AddBrowser(a.newBrowserPlat, name, dir)
	if err != nil {
		logging.Log.Errorf("添加浏览器失败: %v", err)
		return
	}
	a.newBrowserName = ""
	a.markFreshLogin(row.ID)
	logging.Log.Info("正在打开 %s（%s），登录成功后才会留在列表里", name, a.newBrowserPlat)
	a.refreshBrowsers()
	for i := range a.browserRows {
		if a.browserRows[i].ID == row.ID {
			a.browserSel = i
			break
		}
	}
	a.launchBrowser()
}

func (a *App) selectedBrowser() (store.Browser, bool) {
	if a.browserSel < 0 || a.browserSel >= len(a.browserRows) {
		return store.Browser{}, false
	}
	return a.browserRows[a.browserSel], true
}

func accountNeedsQR(b store.Browser, shot loginShot) bool {
	if b.ExtAccount != "" || shot.state == wizard.ProbeIn {
		return false
	}
	switch shot.state {
	case wizard.ProbeOut, wizard.ProbeUnknown, wizard.ProbeRegion:
		return true
	}
	return shot.bmp != nil || shot.hint != ""
}

func (a *App) accountRow(c *ui.Context, b store.Browser, active bool) {
	shot := a.loginShot(b.ID)
	ui.Column(c).FillWidth().Padding(10, 12).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				signedIn := accountSignedIn(b, shot)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, b.Name).Bold().SingleLine()
					ui.Badge(c, accountHealthLabel(b, active, signedIn)).Shrink(0)
				})
				line := fmt.Sprintf("%s · 端口 %d", b.Platform, b.Port)
				if b.ExtAccount != "" {
					line = b.Platform + " · 扩展会话"
				}
				ui.Text(c, line).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
				ui.Text(c, fmt.Sprintf("今日已用 %d/%d", b.UsedToday(time.Now()), a.shownDailyCap())).FontSize(12).TextColor(c.Theme().TextMuted)
				if b.LastError != "" && !b.Usable(time.Now()) && !staleLoginCopy(b, signedIn) {
					ui.Text(c, b.LastError).FontSize(12).TextColor(c.Theme().Danger).SingleLine().Tooltip(b.LastError)
				}
			})
			if shot.bmp != nil && accountNeedsQR(b, shot) && shot.state != wizard.ProbeRegion {
				ui.Image(c, shot.bmp).Size(132, 132).Shrink(0).Fit(ui.Contain).Radius(8).Border(1, c.Theme().Border)
			}
		})
		a.accountLoginStatus(c, b, shot)
	})
}

func (a *App) accountLoginStatus(c *ui.Context, b store.Browser, shot loginShot) {
	if b.ExtAccount != "" {
		return
	}
	if accountNeedsQR(b, shot) {
		switch {
		case shot.state == wizard.ProbeRegion:
			ui.Text(c, regionProxyHint).FontSize(12).TextColor(c.Theme().Warning)
		case shot.hint != "":
			ui.Text(c, shot.hint).FontSize(12).TextColor(c.Theme().Warning)
		case shot.bmp != nil:
			ui.Text(c, "用手机扫码，过期会自动刷新").FontSize(12).TextColor(c.Theme().TextMuted)
		default:
			ui.Text(c, loginQRWaitHint).FontSize(12).TextColor(c.Theme().TextMuted)
		}
		return
	}
	if accountSignedIn(b, shot) {
		ui.Text(c, "已登录").FontSize(12).TextColor(c.Theme().Success)
	}
}

func accountSignedIn(b store.Browser, shot loginShot) bool {
	switch shot.state {
	case wizard.ProbeIn:
		return true
	case wizard.ProbeOut, wizard.ProbeRegion:
		return false
	default:
		return b.IsLoggedIn
	}
}

func staleLoginCopy(b store.Browser, signedIn bool) bool {
	return signedIn && strings.Contains(b.LastError, "登录")
}

func (a *App) launchBrowser() {
	b, ok := a.selectedBrowser()
	if !ok {
		return
	}
	platform := strategy.Factory(b.Platform)
	if platform == nil {
		logging.Log.Errorf("无法识别浏览器平台: %s", b.Platform)
		return
	}
	a.pushProxy()
	a.refreshDetected()
	go func() {
		ws, err := a.manager.Start(b, a.dataRoot, a.browserBackend, platform.HomeURL(), false)
		if err != nil && !errors.Is(err, browser.ErrDebugPort) {
			logging.Log.Errorf("启动失败: %v", err)
			if !b.IsLoggedIn && a.loginIsFresh(b.ID) {
				a.onUI(func() {
					a.abandonUnlogged(b)
					a.browserSel = -1
					a.refreshBrowsers()
				})
			}
			return
		}
		if errors.Is(err, browser.ErrDebugPort) {
			a.store.SetRunning(b.ID, true)
			a.watchLogin(b, loginEndpoint(b))
			a.onUI(func() { a.refreshBrowsers() })
			return
		}
		err = cdp.OpenURLBackground(context.Background(), ws, platform.HomeURL())
		a.store.SetRunning(b.ID, true)
		if err == nil || errors.Is(err, cdp.ErrDebugUnavailable) {
			a.watchLogin(b, ws)
		}
		if err != nil && !errors.Is(err, cdp.ErrDebugUnavailable) {
			logging.Log.Errorf("浏览器[%s] 打开登录页失败：%v", b.Name, err)
		} else if err != nil {
			logging.Log.Warning("浏览器[%s] 调试端口还没连上，已打开登录页", b.Name)
		} else {
			logging.Log.Success("浏览器[%s]已打开%s，可在浏览器中登录账号", b.Name, b.Platform)
		}
		a.onUI(func() { a.refreshBrowsers() })
	}()
}

func (a *App) stopBrowser() {
	b, ok := a.selectedBrowser()
	if !ok {
		return
	}
	if a.store != nil {
		for _, row := range a.store.Browsers() {
			if row.ID == b.ID {
				b.IsLoggedIn = row.IsLoggedIn
				break
			}
		}
	}
	if a.manager != nil {
		a.manager.Stop(b.ID)
	}
	if !b.IsLoggedIn && a.loginIsFresh(b.ID) {
		a.abandonUnlogged(b)
		logging.Log.Info("浏览器[%s] 未登录，已从列表移除", b.Name)
		a.browserSel = -1
		a.refreshBrowsers()
		return
	}
	if a.store != nil {
		a.store.SetRunning(b.ID, false)
	}
	logging.Log.Info("浏览器[%s] 已停止", b.Name)
	a.refreshBrowsers()
}

func (a *App) restoreBrowser() {
	b, ok := a.selectedBrowser()
	if !ok || a.store == nil {
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

func (a *App) accountName(id int64) string {
	if id <= 0 {
		return "扩展"
	}
	for _, b := range a.browserRows {
		if b.ID == id && b.Name != "" {
			return b.Name
		}
	}
	if a.store != nil {
		for _, b := range a.store.Browsers() {
			if b.ID == id && b.Name != "" {
				return b.Name
			}
		}
	}
	return fmt.Sprintf("%d", id)
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

func accountHealthLabel(b store.Browser, active, signedIn bool) string {
	if !b.Usable(time.Now()) {
		if b.Health == store.HealthCooling {
			if strings.Contains(b.LastError, "验证") {
				return "待验证"
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

func (a *App) deleteBrowser() {
	b, ok := a.selectedBrowser()
	if !ok {
		return
	}
	ids := a.pairedProfileIDs(b)
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

// pairedProfileIDs 是这个账号页面桥写在自己配置里的标识。
// 调试端口还开着时从扩展里读；扩展账号行本身就带着这个标识。
func (a *App) pairedProfileIDs(b store.Browser) []string {
	if id := strings.TrimSpace(b.ExtAccount); id != "" {
		return []string{id}
	}
	if b.Port <= 0 || !debugPortOpen(b.Port) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	id, _, _, err := cdp.ExtensionPairState(ctx, fmt.Sprintf("http://127.0.0.1:%d", b.Port))
	if err != nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	return []string{id}
}

func (a *App) pickOutputDir() {
	window := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    window,
			Title:     "选择输出目录",
			Directory: true,
		})
		if err != nil {
			logging.Log.Errorf("选择输出目录失败: %v", err)
			return
		}
		if len(paths) > 0 {
			window.Update(func() { a.outputPath = paths[0] })
		}
	}()
}

// ---- 小工具 ----

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func pickStr(v, def string, allowed []string) string {
	if containsStr(allowed, v) {
		return v
	}
	return def
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
