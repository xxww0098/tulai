package ui

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
	"github.com/xxww0098/tulai/internal/wizard"
)

const pageLoginTitle = "在打开的网页里登录"
const pageLoginHint = "请在打开的网页里登录"

const regionProxyHint = "当前地区打不开这个站点。请到设置的「网络」里选择自定义代理，填写 http://主机:端口 或 socks5://主机:端口，然后重新打开登录页。"

type loginShot struct {
	state string
	hint  string
}

// applyWizardGate 在启动时决定要不要盖住主界面。
// 已经有登录过且还能恢复的账号时写下完成标记并跳过。
func (a *App) applyWizardGate() {
	if a == nil || a.store == nil {
		return
	}
	accounts := wizardAccounts(a.store)
	if !wizard.ShouldShow(a.store.Get(wizard.KeyComplete), accounts, time.Now()) {
		if a.store.Get(wizard.KeyComplete) != wizard.CompleteYes {
			a.store.Set(wizard.KeyComplete, wizard.CompleteYes)
		}
		a.wizardOn = false
		return
	}
	a.openWizard()
}

// openWizard 从第一步重新打开。已经登录的账号会留着。
func (a *App) openWizard() {
	a.stopWizardLoop()
	a.wizardOn = true
	a.wizardDir = 1
	a.wizardStep = wizard.StepBrowser
	a.wizardExtra = false
	a.wizardPickExtra = false
	a.wizardPhase = ""
	a.wizardNote = ""
	a.wizardErr = ""
	a.wizardAccount = store.Browser{}
	a.wizardName = ""
	a.wizardNameFor = 0
	a.refreshWizardPlan()
}

func (a *App) refreshWizardPlan() {
	dir := browser.ConfigDir()
	var found []wizard.Found
	for _, rt := range browser.Detect(dir) {
		if rt.Kind == browser.KindBuiltin {
			continue
		}
		found = append(found, wizard.Found{Kind: rt.Kind, Path: rt.Path})
	}
	_, builtinReady := browser.InstalledBuiltin(dir)
	a.wizardPlan = wizard.PlanBrowser(runtime.GOOS, runtime.GOARCH, found, builtinReady, browser.BuiltinSupported(runtime.GOOS, runtime.GOARCH))
}

func wizardAccounts(st *store.Store) []wizard.Account {
	if st == nil {
		return nil
	}
	rows := st.Browsers()
	out := make([]wizard.Account, 0, len(rows))
	for _, row := range rows {
		out = append(out, wizard.Account{
			Platform:  row.Platform,
			Name:      row.Name,
			LoggedIn:  row.IsLoggedIn,
			Extension: row.ExtAccount != "",
			Health:    row.Health,
			CoolUntil: row.CoolUntil,
		})
	}
	return out
}

func (a *App) wizardView(c *ui.Context) {
	ui.Column(c).Fill().Padding(36, 48).Gap(8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Text(c, "tulai").FontSize(22).Bold()
			wizardSteps(c, a.wizardStep)
			ui.Text(c, fmt.Sprintf("第 %d / %d 步", a.wizardStep.Index(), wizard.Count())).FontSize(13).TextColor(c.Theme().TextMuted).Grow(1)
			ui.Text(c, "当前版本 "+currentVersion()).FontSize(13).TextColor(c.Theme().TextMuted)
			ui.Button(c, "检查更新").Disabled(a.updateBusy).OnClick(func() { a.checkForUpdates() })
		})
		a.updateBanner(c)
		ui.Scroll(c.Key("wizard-body")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(840).Gap(20).Padding(28, 8, 12, 8).Children(func() {
				ui.Column(c.Key(a.wizardStep)).FillWidth().Gap(20).Transition(a.stepMotion()).Children(func() {
					ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
						if a.wizardStep == wizard.StepDone {
							doneMark(c)
						}
						ui.Text(c, a.wizardStep.Title()).FontSize(28).Bold()
					})
					switch a.wizardStep {
					case wizard.StepPlatform:
						a.wizardPlatformStep(c)
					case wizard.StepAccount:
						a.wizardAccountStep(c)
					case wizard.StepPlugin:
						a.wizardPluginStep(c)
					case wizard.StepDone:
						a.wizardDoneStep(c)
					default:
						a.wizardBrowserStep(c)
					}
				})
			})
		})
		a.wizardFooter(c)
	})
}

func (a *App) wizardBrowserStep(c *ui.Context) {
	plan := a.wizardPlan
	if plan.Title == "" {
		plan.Title = "正在检查本机浏览器…"
	}
	ui.Text(c, "已自动选好。确认后点下一步即可。").FontSize(15).TextColor(c.Theme().TextMuted)
	ui.Column(c).FillWidth().MinHeight(168).Padding(28, 28).Gap(10).Radius(16).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
		ui.Text(c, plan.Title).FontSize(22).Bold()
		if plan.Detail != "" {
			ui.Text(c, plan.Detail).FontSize(14).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(plan.Detail)
		}
		if plan.Blocked != "" {
			ui.Text(c, plan.Blocked).FontSize(14).TextColor(c.Theme().Warning)
		}
	})
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		if plan.NeedsDownload {
			label := "下载内置浏览器"
			if a.cftReady {
				label = "重新下载内置浏览器"
			}
			ui.Button(c, label).Disabled(a.cftBusy || !browser.BuiltinSupported(runtime.GOOS, runtime.GOARCH)).OnClick(func() { a.downloadBuiltin() })
		}
		ui.Button(c, "重新检测").Disabled(a.cftBusy).OnClick(func() {
			a.refreshDetected()
			a.refreshWizardPlan()
		})
	})
	if a.cftBusy || (plan.NeedsDownload && a.cftStatus != "") {
		if a.cftBusy {
			motionProgress(c, "cft-progress", a.cftPercent, a.cftPercent <= 0)
		}
		if a.cftStatus != "" {
			ui.Text(c, a.cftStatus).FontSize(13).TextColor(c.Theme().TextMuted)
		}
	}
}

func (a *App) wizardPlatformStep(c *ui.Context) {
	ui.Text(c, "选一个，或两个都选。每个平台用自己的免费账号。").FontSize(15).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(20).AlignItems(ui.Stretch).Children(func() {
		a.wizardPlatformCard(c, "豆包", "在豆包里生图。在打开的网页里登录一个免费账号。", &a.platDoubao)
		a.wizardPlatformCard(c, "千问", "在千问里生图。在打开的网页里登录一个免费账号。", &a.platQianwen)
	})
	if !wizard.CanChoosePlatform(a.platDoubao, a.platQianwen) {
		ui.Text(c, "请至少选择一个平台。").FontSize(14).TextColor(c.Theme().Warning)
	}
}

func (a *App) wizardPlatformCard(c *ui.Context, name, blurb string, on *bool) {
	border := c.Theme().Border
	width := float32(1)
	if *on {
		border = c.Theme().Accent
		width = 2
	}
	card := ui.Button(c.Key("wizard-"+name), "").Grow(1).MinHeight(200).Border(width, border).OnClick(func() {
		*on = !*on
	})
	card.Transition(motionColors())
	hoverPressPose(card)
	card.Children(func() {
		ui.Column(c).FillWidth().Gap(12).Padding(8, 4).Children(func() {
			ui.Text(c, name).FontSize(26).Bold()
			ui.Text(c, blurb).FontSize(14).TextColor(c.Theme().TextMuted)
			label, color := "点击选择", c.Theme().TextMuted
			if *on {
				label, color = "已选择", c.Theme().Accent
			}
			ui.Text(c.Key("pick"), label).FontSize(14).TextColor(color).
				ContentTransition(ui.ContentTransition{Animation: &motionEaseFast})
		})
	})
}

func (a *App) wizardAccountStep(c *ui.Context) {
	a.syncWizardName()
	platform := a.wizardQueue.Current()
	if platform == "" {
		platform = a.wizardAccount.Platform
	}
	ui.Text(c, pageLoginTitle).FontSize(18).Bold()
	ui.Text(c, "浏览器会打开平台登录页。登录成功后窗口自动关闭，账号留在列表里。").FontSize(14).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "账号名称").FontSize(13).TextColor(c.Theme().TextMuted)
		ui.TextInput(c, &a.wizardName).Width(220).Placeholder("例如工作号")
	})
	name := strings.TrimSpace(a.wizardName)
	if name == "" {
		name = a.wizardAccount.Name
	}
	line := platform
	if name != "" {
		if line != "" {
			line += " · "
		}
		line += name
	}
	ui.Column(c).Width(320).Padding(16, 16).Gap(10).Radius(16).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
		if line != "" {
			ui.Text(c, line).FontSize(15).Bold().SingleLine()
		}
		a.loginStatusBody(c, a.wizardAccount, true)
	})
}

func (a *App) loginStatusBody(c *ui.Context, b store.Browser, inWizard bool) {
	shot := a.loginShot(b.ID)
	loggedIn := b.IsLoggedIn || shot.state == wizard.ProbeIn || (inWizard && a.wizardPhase == "in")
	if loggedIn {
		ui.Text(c, "已登录").FontSize(15).TextColor(c.Theme().Success)
		return
	}
	if shot.state == wizard.ProbeRegion || (inWizard && a.wizardPhase == "region") {
		msg := regionProxyHint
		if inWizard && a.wizardErr != "" {
			msg = a.wizardErr
		}
		ui.Text(c, msg).FontSize(13).TextColor(c.Theme().Warning)
		return
	}
	if shot.hint != "" {
		ui.Text(c, shot.hint).FontSize(13).TextColor(c.Theme().Warning)
		return
	}
	msg := pageLoginHint
	color := c.Theme().Text
	waiting := true
	if inWizard {
		switch a.wizardPhase {
		case "launching":
			if a.wizardNote != "" {
				msg = a.wizardNote
			}
		case "error":
			waiting = false
			msg = a.wizardErr
			if msg == "" {
				msg = "还没检测到登录"
			}
			color = c.Theme().Warning
		case "waiting":
			if a.wizardNote != "" {
				msg = a.wizardNote
			}
		}
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if waiting {
			waitingDot(c)
		}
		ui.Text(c.Key("wizard-phase"), msg).FontSize(13).TextColor(color)
	})
}

func (a *App) wizardDoneStep(c *ui.Context) {
	ui.Text(c, "拖入图片 → 选提示词 → 开始").FontSize(20).Bold()
	ui.Text(c, "图生图先加图片；只写提示词就是文生图。").FontSize(15).TextColor(c.Theme().TextMuted)
	ui.Column(c).FillWidth().Gap(14).Padding(8, 0).Children(func() {
		for _, tip := range []struct{ n, text string }{
			{"1", "拖入图片"},
			{"2", "选提示词"},
			{"3", "开始"},
		} {
			ui.Row(c).Gap(16).AlignItems(ui.Center).Padding(16, 20).Radius(14).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
				ui.Text(c, tip.n).FontSize(20).Bold().TextColor(c.Theme().Accent)
				ui.Text(c, tip.text).FontSize(18)
			})
		}
	})
	if summary := a.wizardDoneSummary(); summary != "" {
		ui.Text(c, "已登录 "+summary).FontSize(14).TextColor(c.Theme().TextMuted)
	}
	ui.Text(c, "再添加一个账号后，额度用尽、限流或验证码会自动换号。").FontSize(14).TextColor(c.Theme().TextMuted)
	if a.wizardPickExtra {
		ui.Column(c.Key("extra-account")).FillWidth().Gap(12).Transition(motionReveal()).Children(func() {
			ui.Text(c, "要给哪个平台再加一个账号？").FontSize(15)
			ui.Row(c).Gap(12).Children(func() {
				if a.platDoubao {
					ui.Button(c, "再添加豆包").OnClick(func() { a.wizardEnterAccount("豆包") })
				}
				if a.platQianwen {
					ui.Button(c, "再添加千问").OnClick(func() { a.wizardEnterAccount("千问") })
				}
			})
		})
	}
}

func (a *App) wizardDoneSummary() string {
	if a.store == nil {
		return ""
	}
	var parts []string
	for _, row := range a.store.Browsers() {
		if row.ExtAccount != "" || !row.IsLoggedIn || row.Name == "" {
			continue
		}
		parts = append(parts, row.Platform+" · "+row.Name)
	}
	return joinChinese(parts)
}

func joinChinese(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += "，"
		}
		out += part
	}
	return out
}

func (a *App) wizardFooter(c *ui.Context) {
	ui.Row(c).Gap(12).AlignItems(ui.Center).Padding(12, 8, 0, 8).Children(func() {
		if a.wizardStep != wizard.StepBrowser {
			ui.Button(c, "上一步").OnClick(func() { a.wizardBack() })
		}
		ui.Column(c).Grow(1)
		switch a.wizardStep {
		case wizard.StepDone:
			if !a.wizardPickExtra {
				ui.Button(c, "再添加一个账号").OnClick(func() { a.wizardAddAnother() })
			}
			ui.PrimaryButton(c, "进入主页").OnClick(func() { a.wizardFinish() })
		case wizard.StepPlugin:
			ui.Button(c, "跳过").OnClick(func() { a.goWizard(wizard.StepDone) })
			ui.PrimaryButton(c, "下一步").OnClick(func() { a.goWizard(wizard.StepDone) })
		case wizard.StepAccount:
			if a.wizardNavFailed {
				ui.Button(c, "重新打开").Disabled(a.wizardPhase == "launching").OnClick(func() { a.wizardRecheck() })
			}
			ui.Button(c, "已登录? 重新检测").Disabled(a.wizardPhase == "launching").OnClick(func() { a.wizardRecheck() })
		default:
			ui.PrimaryButton(c, "下一步").Disabled(!a.wizardCanNext()).OnClick(func() { a.wizardNext() })
		}
	})
}

func (a *App) wizardCanNext() bool {
	switch a.wizardStep {
	case wizard.StepBrowser:
		return a.wizardPlan.CanNext && !a.cftBusy
	case wizard.StepPlatform:
		return wizard.CanChoosePlatform(a.platDoubao, a.platQianwen)
	default:
		return false
	}
}

func (a *App) wizardNext() {
	switch a.wizardStep {
	case wizard.StepBrowser:
		if !a.wizardCanNext() {
			return
		}
		a.wizardCommitChoices()
		a.goWizard(wizard.StepPlatform)
	case wizard.StepPlatform:
		if !a.wizardCanNext() {
			return
		}
		a.wizardCommitChoices()
		a.wizardEnterAccount("")
	}
}

func (a *App) wizardBack() {
	account := a.wizardAccount
	leavingLogin := a.wizardStep == wizard.StepAccount
	a.stopWizardLoop()
	a.wizardPickExtra = false
	if leavingLogin {
		a.abandonUnlogged(account)
		a.wizardAccount = store.Browser{}
	}
	switch a.wizardStep {
	case wizard.StepDone, wizard.StepPlugin, wizard.StepAccount:
		a.goWizard(wizard.StepPlatform)
	case wizard.StepPlatform:
		a.goWizard(wizard.StepBrowser)
	}
}

func (a *App) wizardCommitChoices() {
	if a.store == nil {
		return
	}
	if a.wizardPlan.Backend != "" {
		a.browserBackend = a.wizardPlan.Backend
		a.browserChoice = browser.ChoiceLabel(a.browserBackend)
		a.store.Set("browser_backend", a.browserBackend)
	}
	if ps := wizard.SelectedPlatforms(a.platDoubao, a.platQianwen); len(ps) > 0 {
		a.store.SetPlatforms(ps)
	}
}

func (a *App) wizardAddAnother() {
	platforms := wizard.SelectedPlatforms(a.platDoubao, a.platQianwen)
	if len(platforms) <= 1 {
		extra := "豆包"
		if len(platforms) == 1 {
			extra = platforms[0]
		}
		a.wizardEnterAccount(extra)
		return
	}
	a.wizardPickExtra = true
}

func (a *App) wizardEnterAccount(extra string) {
	a.wizardPickExtra = false
	a.wizardExtra = extra != ""
	var platforms []string
	if extra != "" {
		platforms = []string{extra}
	} else {
		for _, platform := range wizard.SelectedPlatforms(a.platDoubao, a.platQianwen) {
			if a.platformNeedsLogin(platform) {
				platforms = append(platforms, platform)
			}
		}
	}
	a.wizardQueue = wizard.NewQueue(platforms)
	a.wizardAccount = store.Browser{}
	a.wizardErr = ""
	if a.wizardQueue.Done() {
		a.enterPluginStep()
		return
	}
	a.goWizard(wizard.StepAccount)
	a.wizardPhase = "launching"
	a.wizardNote = "正在打开登录页…"
	a.wizardErr = ""
	a.startWizardLoop()
}

func (a *App) platformNeedsLogin(platform string) bool {
	now := time.Now()
	for _, account := range wizardAccounts(a.store) {
		if account.Platform == platform && !account.Extension && wizard.KeepsClosed(account, now) {
			return false
		}
	}
	return true
}

func (a *App) wizardFinish() {
	a.stopWizardLoop()
	a.wizardCommitChoices()
	if a.store != nil {
		a.store.Set(wizard.KeyComplete, wizard.CompleteYes)
		a.saveSettings()
	}
	a.wizardOn = false
	a.wizardPickExtra = false
	a.refreshBrowsers()
}

func (a *App) wizardRecheck() {
	if a.wizardPhase == "launching" {
		return
	}
	a.startWizardLoop()
}

func (a *App) startWizardLoop() {
	a.stopWizardLoop()
	gen := a.wizardGen
	if a.window == nil {
		a.wizardLoop(gen, nil)
		return
	}
	stop := make(chan struct{})
	a.wizardStop = stop
	go a.wizardLoop(gen, stop)
}

func (a *App) stopWizardLoop() {
	a.wizardGen++
	if a.wizardStop != nil {
		close(a.wizardStop)
		a.wizardStop = nil
	}
}

func (a *App) wizardUI(fn func()) {
	if fn == nil {
		return
	}
	if a.window == nil {
		fn()
		return
	}
	done := make(chan struct{})
	a.window.Update(func() {
		defer close(done)
		fn()
	})
	<-done
}

func (a *App) wizardLoop(gen int, stop <-chan struct{}) {
	for {
		if !a.wizardPrepare(gen) {
			return
		}
		var account store.Browser
		a.wizardUI(func() { account = a.wizardAccount })
		ws, err := a.launchWizardBrowser(account)
		if err != nil && !errors.Is(err, browser.ErrDebugPort) {
			a.wizardUI(func() {
				if gen != a.wizardGen {
					return
				}
				a.wizardPhase = "error"
				a.wizardNavFailed = false
				a.wizardErr = "启动浏览器失败：" + err.Error()
				a.wizardNote = pageLoginTitle
			})
			return
		}
		debugDown := errors.Is(err, browser.ErrDebugPort)
		if debugDown {
			ws = fmt.Sprintf("http://127.0.0.1:%d", account.Port)
		}
		page := accountHomeURL(account)
		if page == "" {
			page = "about:blank"
		}
		if !debugDown {
			if navErr := a.openWizardPage(ws, page); navErr != nil && !errors.Is(navErr, cdp.ErrDebugUnavailable) {
				msg := account.Name + " 打开登录页失败：" + navErr.Error()
				logging.Log.Errorf("%s", msg)
				a.wizardUI(func() {
					if gen != a.wizardGen {
						return
					}
					a.wizardPhase = "error"
					a.wizardNavFailed = true
					a.wizardErr = msg
					a.wizardNote = "请点「重新打开」"
				})
				return
			}
		}
		a.wizardUI(func() {
			if gen != a.wizardGen || a.store == nil {
				return
			}
			a.store.SetRunning(account.ID, true)
			a.wizardPhase = "waiting"
			a.wizardNote = pageLoginTitle
			a.wizardErr = ""
		})
		for {
			if a.wizardStale(gen, stop) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			raw, probeErr := a.probeWizardLogin(ctx, ws)
			cancel()
			action := wizard.PollWait
			a.wizardUI(func() {
				a.setLoginShot(account.ID, raw, "")
				action = a.noteProbe(gen, raw, probeErr)
			})
			if action == wizard.PollNext || action == wizard.PollDone {
				a.closeLoginWindow(account)
			}
			switch action {
			case wizard.PollNext:
				goto nextPlatform
			case wizard.PollDone, wizard.PollStop:
				return
			}
			if stop == nil {
				return
			}
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-stop:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	nextPlatform:
	}
}

func (a *App) wizardStale(gen int, stop <-chan struct{}) bool {
	if stop != nil {
		select {
		case <-stop:
			return true
		default:
		}
	}
	stale := false
	a.wizardUI(func() { stale = gen != a.wizardGen })
	return stale
}

func (a *App) wizardPrepare(gen int) bool {
	ok := false
	a.wizardUI(func() {
		if gen != a.wizardGen {
			return
		}
		if a.wizardQueue.Done() {
			a.enterPluginStep()
			return
		}
		account, err := a.ensureWizardAccount()
		if err != nil {
			a.wizardPhase = "error"
			a.wizardErr = err.Error()
			a.wizardNote = pageLoginTitle
			return
		}
		a.wizardAccount = account
		a.wizardPhase = "launching"
		a.pushProxy()
		a.wizardNote = "正在打开" + account.Platform + "…"
		a.wizardErr = ""
		a.wizardNavFailed = false
		ok = true
	})
	return ok
}

func (a *App) ensureWizardAccount() (store.Browser, error) {
	if a.store == nil {
		return store.Browser{}, fmt.Errorf("没有配置库，无法添加账号")
	}
	platform := a.wizardQueue.Current()
	if platform == "" {
		return store.Browser{}, fmt.Errorf("没有要登录的平台")
	}
	if a.wizardAccount.ID != 0 && a.wizardAccount.Platform == platform {
		for _, row := range a.store.Browsers() {
			if row.ID == a.wizardAccount.ID {
				return row, nil
			}
		}
	}
	if !a.wizardExtra {
		now := time.Now()
		for _, row := range a.store.Browsers() {
			if row.Platform != platform || row.ExtAccount != "" {
				continue
			}
			snap := wizard.Account{
				Platform: row.Platform, Name: row.Name, LoggedIn: row.IsLoggedIn,
				Health: row.Health, CoolUntil: row.CoolUntil,
			}
			if !wizard.KeepsClosed(snap, now) {
				return row, nil
			}
		}
	}
	var names []string
	for _, row := range a.store.Browsers() {
		names = append(names, row.Name)
	}
	name := wizard.NextInstanceName(platform, names)
	dir := ""
	if strings.TrimSpace(a.dataRoot) != "" {
		dir = browser.ProfileDir(a.dataRoot, a.browserBackend, name)
	}
	row, err := a.store.AddBrowser(platform, name, dir)
	if err == nil {
		a.markFreshLogin(row.ID)
	}
	return row, err
}

func (a *App) noteProbe(gen int, raw string, err error) wizard.Poll {
	if gen != a.wizardGen {
		return wizard.PollStop
	}
	if strings.TrimSpace(raw) == wizard.ProbeRegion {
		a.wizardPhase = "region"
		a.wizardErr = regionProxyHint
		a.wizardNote = "需要设置代理"
		a.wizardNavFailed = true
		return wizard.PollWait
	}
	if err != nil || !wizard.LoggedIn(raw) {
		a.wizardPhase = "waiting"
		a.wizardNote = pageLoginTitle
		a.wizardErr = ""
		if err != nil {
			a.wizardPhase = "error"
			a.wizardErr = "还没检测到登录，可以在浏览器里完成后再点重新检测"
		}
		return wizard.PollWait
	}
	if a.store != nil && a.wizardAccount.ID != 0 {
		a.store.ConfirmLogin(a.wizardAccount.ID)
		a.clearLoginHold(a.wizardAccount.ID)
	}
	a.wizardPhase = "in"
	a.wizardNote = "已登录"
	a.wizardErr = ""
	next, poll := wizard.AfterLogin(a.wizardQueue, true)
	a.wizardQueue = next
	if poll == wizard.PollNext {
		a.wizardAccount = store.Browser{}
		a.wizardPhase = "launching"
		a.wizardErr = ""
	}
	if poll == wizard.PollDone {
		a.enterPluginStep()
	}
	return poll
}

func (a *App) enterPluginStep() {
	a.wizardPhase = "ready"
	a.wizardNote = ""
	a.wizardErr = ""
	a.pluginWasConnected = a.extensionConnected()
	a.goWizard(wizard.StepPlugin)
	a.watchPlugin()
}

func (a *App) extensionConnected() bool {
	if a == nil || a.engine == nil {
		return false
	}
	_, paired := a.engine.PairState()
	st := a.engine.BridgeStatus()
	return paired || st.Qianwen || st.Doubao
}

func (a *App) watchPlugin() {
	if a.pluginWatch {
		return
	}
	a.pluginWatch = true
	go func() {
		for i := 0; i < 180; i++ {
			time.Sleep(time.Second)
			var step wizard.Step
			var connected, already bool
			a.wizardUI(func() {
				step = a.wizardStep
				already = a.pluginWasConnected
				connected = a.extensionConnected()
				if step == wizard.StepPlugin && connected && !already {
					a.goWizard(wizard.StepDone)
				}
			})
			if step != wizard.StepPlugin || (connected && !already) {
				a.wizardUI(func() { a.pluginWatch = false })
				return
			}
		}
		a.wizardUI(func() { a.pluginWatch = false })
	}()
}

func (a *App) wizardPluginStep(c *ui.Context) {
	ui.Text(c, "tulai 自己打开的浏览器会带上页面桥。日常使用的 Chrome 或 Edge 需要装一次。").FontSize(15).TextColor(c.Theme().TextMuted)
	if a.extensionConnected() {
		ui.Text(c, "扩展已连接").FontSize(16).TextColor(c.Theme().Accent)
	}
	if notice := a.extensionNotice(); notice != "" {
		ui.Text(c, notice).FontSize(15).TextColor(c.Theme().Warning)
	}
	dir := a.extensionDir
	if dir == "" {
		dir = "扩展目录还没准备好"
	}
	ui.Text(c, dir).FontSize(13).TextColor(c.Theme().TextMuted).SingleLine().Tooltip(dir)
	for _, step := range []struct{ n, text string }{
		{"1", "打开「开发者模式」"},
		{"2", "加载已解压的扩展程序"},
		{"3", "选这个文件夹"},
	} {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Padding(14, 18).Radius(14).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
			ui.Text(c, step.n).FontSize(20).Bold().TextColor(c.Theme().Accent)
			ui.Text(c, step.text).FontSize(16)
		})
	}
	ui.Button(c, "打开扩展页面").OnClick(func() { a.openExtensionInstall() })
}

func (a *App) extensionNotice() string {
	if a == nil || a.engine == nil {
		return ""
	}
	return a.engine.ExtensionNotice()
}

func (a *App) openExtensionInstall() {
	kind := a.browserBackend
	if kind != browser.KindEdge {
		kind = browser.KindChrome
	}
	if err := browser.OpenExtensionsPage(kind); err != nil {
		logging.Log.Errorf("打开扩展页面失败：%v", err)
	}
	if a.extensionDir != "" {
		if err := browser.OpenFolder(a.extensionDir); err != nil {
			logging.Log.Errorf("打开扩展目录失败：%v", err)
		}
	}
}

func (a *App) launchWizardBrowser(b store.Browser) (string, error) {
	if a.wizardLaunch != nil {
		return a.wizardLaunch(b)
	}
	if a.manager == nil {
		return "", fmt.Errorf("没有浏览器")
	}
	a.pushProxy()
	return a.manager.Start(b, a.dataRoot, a.browserBackend, accountHomeURL(b), false)
}

func accountHomeURL(b store.Browser) string {
	if plat := strategy.Factory(b.Platform); plat != nil {
		return plat.HomeURL()
	}
	return ""
}

func (a *App) openWizardPage(ws, rawURL string) error {
	if a.wizardOpen != nil {
		return a.wizardOpen(ws, rawURL)
	}
	return cdp.OpenURLContext(context.Background(), ws, rawURL)
}

func (a *App) probeWizardLogin(ctx context.Context, endpoint string) (string, error) {
	if a.wizardProbe != nil {
		return a.wizardProbe(ctx, endpoint)
	}
	return cdp.ProbeLogin(ctx, endpoint)
}

func (a *App) setLoginShot(id int64, state, hint string) {
	if a == nil || id == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loginShots == nil {
		a.loginShots = map[int64]loginShot{}
	}
	shot := a.loginShots[id]
	if state != "" {
		shot.state = state
	}
	shot.hint = hint
	if state == wizard.ProbeIn || state == wizard.ProbeRegion {
		shot.hint = ""
	}
	a.loginShots[id] = shot
}

// closeLoginWindow 登录成功后关掉这次打开的窗口。账号留在库里，出图时再在屏幕外启动。
func (a *App) closeLoginWindow(b store.Browser) {
	if a == nil || b.ID <= 0 {
		return
	}
	a.mu.Lock()
	a.loginClosed = append(a.loginClosed, b.ID)
	a.mu.Unlock()
	cdp.ReleaseLoginFront(loginEndpoint(b))
	if a.manager != nil {
		a.manager.Stop(b.ID)
	}
	if a.store != nil {
		a.store.SetRunning(b.ID, false)
	}
	a.onUI(func() { a.refreshBrowsers() })
	if a.afterClose != nil {
		a.afterClose()
	}
}

func (a *App) loginShot(id int64) loginShot {
	if a == nil {
		return loginShot{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loginShots[id]
}

func (a *App) syncWizardName() {
	if a == nil || a.wizardAccount.ID == 0 {
		return
	}
	if a.wizardNameFor != a.wizardAccount.ID {
		a.wizardName = a.wizardAccount.Name
		a.wizardNameFor = a.wizardAccount.ID
		return
	}
	name := strings.TrimSpace(a.wizardName)
	if name == "" || name == a.wizardAccount.Name || a.store == nil {
		return
	}
	a.store.RenameBrowser(a.wizardAccount.ID, name)
	a.wizardAccount.Name = name
}

func loginEndpoint(b store.Browser) string {
	if b.Port <= 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", b.Port)
}

// watchLogin 在打开的网页上探测登录。测试可换成 loginFollow。
func (a *App) watchLogin(b store.Browser, ws string) {
	if a == nil || b.ID == 0 {
		return
	}
	if strings.TrimSpace(ws) == "" {
		ws = loginEndpoint(b)
	}
	if ws == "" {
		return
	}
	if a.loginFollow != nil {
		a.loginFollow(b, ws)
		return
	}
	a.followLogin(b, ws)
}

func (a *App) followLogin(b store.Browser, ws string) {
	if a == nil || b.ID == 0 || ws == "" {
		return
	}
	a.mu.Lock()
	if a.loginWatchGen == nil {
		a.loginWatchGen = map[int64]int{}
	}
	a.loginWatchGen[b.ID]++
	gen := a.loginWatchGen[b.ID]
	a.mu.Unlock()
	go func() {
		a.publishLogin(b.ID, gen, wizard.ProbeOut, "")
		for i := 0; i < 150; i++ {
			if !a.loginCurrent(b.ID, gen) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			state, err := a.probeWizardLogin(ctx, ws)
			cancel()
			hint := ""
			if err != nil && state != wizard.ProbeRegion {
				hint = "还没检测到登录，可以在打开的网页里完成后等自动识别"
				state = wizard.ProbeOut
			}
			if !a.publishLogin(b.ID, gen, state, hint) {
				return
			}
			if state == wizard.ProbeIn {
				a.closeLoginWindow(b)
				return
			}
			if !a.loginSleep(b.ID, gen) {
				return
			}
		}
	}()
}

func (a *App) loginCurrent(id int64, gen int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.loginWatchGen[id] == gen
}

func (a *App) loginSleep(id int64, gen int) bool {
	gap := 2 * time.Second
	if a.loginPoll > 0 {
		gap = a.loginPoll
	}
	deadline := time.Now().Add(gap)
	for {
		if !a.loginCurrent(id, gen) {
			return false
		}
		if !time.Now().Before(deadline) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (a *App) publishLogin(id int64, gen int, state, hint string) bool {
	if !a.loginCurrent(id, gen) {
		return false
	}
	run := func() {
		if !a.loginCurrent(id, gen) {
			return
		}
		a.setLoginShot(id, state, hint)
		if state == wizard.ProbeIn && a.store != nil {
			a.store.ConfirmLogin(id)
			a.clearLoginHold(id)
			a.refreshBrowsers()
		}
	}
	if a == nil || a.window == nil {
		run()
		return a.loginCurrent(id, gen)
	}
	done := make(chan struct{})
	a.window.Update(func() {
		defer close(done)
		run()
	})
	<-done
	return a.loginCurrent(id, gen)
}
