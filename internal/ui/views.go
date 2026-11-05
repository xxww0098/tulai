package ui

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
	"github.com/xxww0098/tulai/internal/wizard"
)

const browserInstanceHint = "同一平台可以登录多个账号。每个账号的登录资料与指纹相互隔离，额度用尽、限流或验证码时自动换号。"

// browserPane 管理 Camoufox 服务商账号。
func (a *App) browserPane(c *ui.Context) {
	a.syncBrowserChoice()
	if a.detectedNote == "" && !a.camoufoxBusy {
		a.refreshDetected()
	}
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().Gap(16).Children(func() {
		a.browserSetup(c)
		a.downloadBrowserEnvironment(c)
		a.browserInstances(c)
	})
}

func (a *App) browserSetup(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(20).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Text(c, "Camoufox 账号环境").FontSize(18).Bold()
			ui.Text(c, "集中管理独立环境，登录与验证在打开的网页中完成。").FontSize(12).
				TextColor(c.Theme().TextMuted).SingleLine().Tooltip(browserInstanceHint)
		})
		ui.Column(c).Width(260).Shrink(0).Gap(6).AlignItems(ui.End).Children(func() {
			a.camoufoxSection(c)
		})
	})
}

func (a *App) browserInstances(c *ui.Context) {
	a.followStagePlatform()
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
			if rows[i].Platform == agy.Platform {
				continue
			}
			if rows[i].ExtAccount != "" && !rows[i].IsLoggedIn {
				continue
			}
			shown = append(shown, rows[i])
		}
		a.browserRows = shown
	}
	a.pruneBrowserSelection()
	ui.Column(c).Grow(1).MinHeight(0).FillWidth().Gap(12).Children(func() {
		running := 0
		for _, b := range a.browserRows {
			if b.IsRunning {
				running++
			}
		}
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "浏览器实例").FontSize(14).Bold()
			ui.Text(c, fmt.Sprintf("共 %d 个账号环境 · %d 个运行中", len(a.browserRows), running)).FontSize(12).TextColor(c.Theme().TextMuted)
			if hint := missingBrowserHint(a.missingBrowserNames()); hint != "" {
				ui.Text(c, hint).FontSize(12).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0).SingleLine().Tooltip(hint)
			}
		})
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			motionSelect(c, &a.newBrowserPlat, []string{"豆包", "千问", "ChatGPT"}).Width(100).Shrink(0).Label("浏览器平台")
			ui.TextInput(c, &a.newBrowserName).Grow(1).MaxWidth(280).MinWidth(0).Placeholder("环境名称（可选）").Label("浏览器名称")
			ui.Button(c, "添加环境").Shrink(0).Disabled(a.running || len(a.browserPending) > 0).Background(c.Theme().Accent).TextColor(c.Theme().AccentText).OnClick(func() { a.addBrowser() })
			ui.Box(c).Grow(1)
			ui.Button(c, "刷新列表").Shrink(0).OnClick(func() { a.refreshBrowsers() })
		})
		a.browserBatchToolbar(c)
		if len(a.browserRows) == 0 {
			ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Border(1, c.Theme().Border).Radius(8).Children(func() {
				ui.Icon(c, iconGlobe).FontSize(28).TextColor(c.Theme().TextMuted)
				ui.Text(c, "添加第一个浏览器环境").FontSize(15).Bold()
				ui.Text(c, "选择服务商并点击「添加环境」，在打开的页面中登录账号。").FontSize(12).TextColor(c.Theme().TextMuted)
			})
		} else {
			a.browserEnvironmentTable(c, active)
		}
		selected, hasSelection := a.selectedBrowser()
		ui.Column(c).FillWidth().Padding(12, 14).Gap(8).Background(c.Theme().Background).Border(1, c.Theme().Border).Radius(8).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				title := "选择环境以配置代理"
				if hasSelection {
					title = "环境配置 · " + selected.Name
				}
				ui.Text(c, title).FontSize(13).Bold().Grow(1).MinWidth(0).SingleLine()
				ui.Button(c, "恢复当前").FontSize(12).Shrink(0).Disabled(!hasSelection || len(a.browserPending) > 0).OnClick(func() { a.restoreBrowser() })
				ui.Button(c, "删除当前").FontSize(12).Shrink(0).Disabled(a.running || !hasSelection || len(a.browserPending) > 0).TextColor(c.Theme().Danger).OnClick(func() { a.deleteBrowser() })
			})
			if hasSelection && selected.ExtAccount == "" && selected.Platform != "Antigravity" {
				a.accountProxySettings(c, selected)
			}
		})
		ui.Text(c, "勾选环境可批量启停；新账号登录后窗口自动关闭，手动启动的环境保持打开。").FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
	})
}

// followStagePlatform 让添加账号的平台下拉跟着右侧当前 Tab，直到用户自己改下拉。
func (a *App) followStagePlatform() {
	if a == nil || a.stagePlatform() == agy.Platform {
		return
	}
	if a.platSynced && a.platSyncedTab == a.stageTab && a.newBrowserPlat != "" {
		return
	}
	a.newBrowserPlat = a.stagePlatform()
	a.platSynced = true
	a.platSyncedTab = a.stageTab
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
	a.tab = pageBrowser
}

func (a *App) refreshBrowsers() {
	focused, hadFocus := a.selectedBrowser()
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
	if hadFocus {
		a.browserSel = -1
		a.selectBrowserID(focused.ID)
	}
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
// 或探测明确返回 out 的未登录档案。标志是 0、窗口没开、探测失败、地区或验证，都留下。
// 扩展行同样不能凭 is_logged_in=0 删，要等 hello 明确说没登录。
// shortcut: 窗口开着时同步探测，每个账号每进程只探一次；需要持续观察页面时改为后台探测。
func (a *App) dropAbandoned() {
	if a == nil || a.store == nil || a.running {
		return
	}
	keep := map[int64]struct{}{}
	for _, id := range a.loginKeepIDs() {
		keep[id] = struct{}{}
	}
	for id := range a.browserPending {
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

// accountLoginState 是删档案前的登录探测。出错、窗口没开、地区和验证都不是 out。
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
	a.detectedNote, a.detectedDetail = browser.FormatDetected(browser.Detect(browser.ConfigDir()))
	_, a.camoufoxReady = browser.InstalledCamoufox(browser.ConfigDir())
	if a.camoufoxBusy {
		return
	}
	if a.camoufoxReady {
		a.camoufoxStatus = "Camoufox 已就绪"
		return
	}
	if !browser.CamoufoxSupported(runtime.GOOS, runtime.GOARCH) {
		a.camoufoxStatus = "当前架构不支持 Camoufox；Windows 请使用 x64 版本"
		return
	}
	a.camoufoxStatus = "安装后即可添加服务商账号"
}

func (a *App) camoufoxSection(c *ui.Context) {
	ui.Text(c, a.camoufoxStatus).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(a.camoufoxStatus + "\n" + a.detectedDetail)
	label := "安装 Camoufox"
	if a.camoufoxReady {
		label = "检查 Camoufox"
	}
	ui.Button(c, label).AlignSelf(ui.End).FontSize(12).Disabled(a.running || a.camoufoxBusy || a.renameUpdateActive() || !browser.CamoufoxSupported(runtime.GOOS, runtime.GOARCH)).OnClick(func() { a.installCamoufox() })
	if a.camoufoxBusy {
		a.camoufoxCompactProgress(c, "camoufox-progress")
	}
}

func (a *App) installCamoufox() {
	if a.camoufoxBusy || a.running || a.renameUpdateActive() {
		return
	}
	a.camoufoxBusy = true
	a.camoufoxStatus = "正在准备 Camoufox…"
	a.camoufoxPercent = -1
	a.camoufoxDone, a.camoufoxTotal, a.camoufoxSpeed, a.camoufoxLast = 0, 0, 0, 0
	a.camoufoxAt = time.Time{}
	a.camoufoxStage = ""
	a.pushProxy()
	go func() {
		done := make(chan struct{})
		go a.camoufoxTick(done)
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()
		_, err := browser.EnsureCamoufox(ctx, browser.ConfigDir(), func(p camoufox.Progress) {
			a.syncUI(func() { a.applyCamoufoxProgress(p) })
		})
		a.syncUI(func() {
			a.camoufoxBusy = false
			defer a.maybeInstallPending()
			if err != nil {
				a.camoufoxStatus = "安装失败：" + err.Error()
				return
			}
			a.refreshDetected()
			a.refreshWizardPlan()
		})
	}()
}

func (a *App) addBrowser() {
	if a == nil || a.running {
		return
	}
	name := strings.TrimSpace(a.newBrowserName)
	a.newBrowserName = ""
	a.createBrowser(a.newBrowserPlat, name)
}

// addAccountHere 给右侧当前 Tab 的平台直接加一个账号。
func (a *App) addAccountHere() {
	if a == nil || a.running {
		return
	}
	if a.stagePlatform() == agy.Platform {
		a.addAgyAccount()
		return
	}
	platform := a.stagePlatform()
	a.newBrowserPlat = platform
	a.platSynced = true
	a.platSyncedTab = a.stageTab
	a.tab = pageBrowser
	a.createBrowser(platform, "")
}

func (a *App) createBrowser(platform, name string) {
	if a == nil || a.store == nil || a.running {
		return
	}
	if a.manager == nil {
		logging.Log.Errorf("没有浏览器，无法添加账号")
		return
	}
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "豆包"
	}
	if strings.TrimSpace(name) == "" {
		var used []string
		for _, row := range a.store.Browsers() {
			used = append(used, row.Name)
		}
		name = wizard.NextInstanceName(platform, used)
	}
	dir := ""
	if strings.TrimSpace(a.dataRoot) != "" {
		dir = browser.ProfileDir(a.dataRoot, a.browserBackend, "account-"+rand.Text())
	}
	row, err := a.store.AddBrowser(platform, name, dir)
	if err != nil {
		logging.Log.Errorf("添加浏览器失败: %v", err)
		return
	}
	a.markFreshLogin(row.ID)
	logging.Log.Info("正在打开 %s（%s），请在网页里登录，成功后窗口自动关闭", name, platform)
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

func (a *App) accountRow(c *ui.Context, b store.Browser) {
	shot := a.loginShot(b.ID)
	ui.Column(c).FillWidth().MinWidth(0).MinHeight(44).Justify(ui.Center).Gap(3).Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			a.accountNameField(c, b)
		})
		if b.ExtAccount != "" {
			ui.Text(c, "扩展会话").FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
		} else {
			a.accountLoginStatus(c, b, shot)
		}
	})
}

func (a *App) accountNameField(c *ui.Context, b store.Browser) {
	if a.renameID == b.ID {
		field := ui.TextInput(c.Key("rename"), &a.renameDraft).Width(148).FontSize(13).OnTextKey(func(k ui.TextKey) bool {
			switch k.Key {
			case ui.KeyEnter:
				a.renameDraft = strings.TrimSpace(k.Text())
				a.commitRename()
				return true
			case ui.KeyEscape:
				a.cancelRename()
				return true
			default:
				return false
			}
		})
		if a.renameFocus {
			field.Focus()
			a.renameFocus = false
		}
		return
	}
	name := ui.Text(c, b.Name).Bold().SingleLine().Grow(1).MinWidth(0).Tooltip(b.Name)
	if name.Clicked() {
		a.selectBrowserID(b.ID)
	}
	if name.DoubleClicked() {
		a.beginRename(b)
	}
	pen := ui.ButtonBase(c.Key("pen")).Size(20, 20).Radius(6).Shrink(0).Center().
		Label("重命名 " + b.Name).Cursor(ui.CursorPointer)
	if pen.Hovered() {
		pen.Background(c.Theme().Border.Alpha(0.45))
	}
	if pen.Clicked() {
		a.beginRename(b)
	}
	pen.Children(func() {
		ui.Icon(c, iconPen).FontSize(12).TextColor(c.Theme().TextMuted)
	})
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
	a.renameFocus = true
	a.selectBrowserID(b.ID)
}

func (a *App) commitRename() {
	id := a.renameID
	name := strings.TrimSpace(a.renameDraft)
	a.renameID = 0
	a.renameFocus = false
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
	a.renameFocus = false
}

func (a *App) accountLoginStatus(c *ui.Context, b store.Browser, shot loginShot) {
	if b.ExtAccount != "" {
		return
	}
	switch {
	case shot.state == wizard.ProbeRegion:
		ui.Text(c, regionProxyHint).FontSize(12).TextColor(c.Theme().Warning).SingleLine().Tooltip(regionProxyHint)
	case accountSignedIn(b, shot):
		ui.Text(c, "已登录").FontSize(12).TextColor(c.Theme().Success)
	case shot.hint != "":
		ui.Text(c, shot.hint).FontSize(12).TextColor(c.Theme().Warning).SingleLine().Tooltip(shot.hint)
	default:
		ui.Text(c, pageLoginHint).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(pageLoginHint)
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
	a.launchBrowserInstance(b, nil)
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

func (a *App) stopBrowser() {
	b, ok := a.selectedBrowser()
	if !ok {
		return
	}
	a.stopBrowserInstance(b, nil)
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

// pairedProfileIDs 返回旧手动扩展账号携带的标识。
func (a *App) pairedProfileIDs(b store.Browser) []string {
	if id := strings.TrimSpace(b.ExtAccount); id != "" {
		return []string{id}
	}
	return nil
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

func (a *App) accountProxySettings(c *ui.Context, b store.Browser) {
	if a.accountProxyID != b.ID {
		a.accountProxyID = b.ID
		a.accountProxy = b.Proxy
		a.accountProxyNote = ""
	}
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.End).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Field(c, "当前账号代理", func() {
				ui.TextInput(c.Key("account-proxy"), &a.accountProxy).Label("当前账号代理").Placeholder("留空使用全局代理").Disabled(a.running || b.IsRunning || len(a.browserPending) > 0)
			})
		})
		ui.Button(c, "保存账号代理").Shrink(0).Disabled(a.store == nil || a.running || b.IsRunning || len(a.browserPending) > 0).OnClick(func() { a.saveAccountProxy(b) })
	})
	ui.Text(c, "支持 HTTP 代理（可带用户名和密码）或无认证的 SOCKS5 代理。下次打开此账号生效。").FontSize(12).TextColor(c.Theme().TextMuted)
	if b.IsRunning {
		ui.Text(c, "先停止此账号窗口，再修改代理。").FontSize(12).TextColor(c.Theme().TextMuted)
	}
	if a.accountProxyNote != "" {
		ui.Text(c, a.accountProxyNote).FontSize(12).TextColor(c.Theme().TextMuted)
	}
}

func (a *App) saveAccountProxy(b store.Browser) {
	raw := strings.TrimSpace(a.accountProxy)
	if raw != "" {
		var ok bool
		raw, ok = browser.NormalizeProxy(raw)
		if !ok {
			a.accountProxyNote = "代理地址无效，请填写协议、主机和端口"
			return
		}
		if strings.HasPrefix(raw, "socks5://") && strings.Contains(raw, "@") {
			a.accountProxyNote = "SOCKS5 认证不受支持，请改用 HTTP 代理"
			return
		}
	}
	if err := a.store.SetBrowserProxy(b.ID, raw); err != nil {
		a.accountProxyNote = "保存失败：" + err.Error()
		return
	}
	a.accountProxy = raw
	a.accountProxyNote = "已保存，下次打开此账号生效"
	a.refreshBrowsers()
}
