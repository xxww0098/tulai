package ui

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
	"github.com/xxww0098/tulai/internal/wizard"
)

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
	a.wizardStep = wizard.StepBrowser
	a.wizardExtra = false
	a.wizardPickExtra = false
	a.wizardPhase = ""
	a.wizardNote = ""
	a.wizardErr = ""
	a.wizardAccount = store.Browser{}
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
			ui.Text(c, fmt.Sprintf("第 %d / %d 步", a.wizardStep.Index(), wizard.Count())).FontSize(13).TextColor(c.Theme().TextMuted).Grow(1)
			ui.Text(c, "当前版本 "+currentVersion()).FontSize(13).TextColor(c.Theme().TextMuted)
			ui.Button(c, "检查更新").Disabled(a.updateBusy).OnClick(func() { a.checkForUpdates() })
		})
		a.updateBanner(c)
		ui.Scroll(c.Key("wizard-body")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(840).Gap(20).Padding(28, 8, 12, 8).Children(func() {
				ui.Text(c, a.wizardStep.Title()).FontSize(28).Bold()
				switch a.wizardStep {
				case wizard.StepPlatform:
					a.wizardPlatformStep(c)
				case wizard.StepAccount:
					a.wizardAccountStep(c)
				case wizard.StepDone:
					a.wizardDoneStep(c)
				default:
					a.wizardBrowserStep(c)
				}
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
			ui.Progress(c, a.cftPercent).FillWidth().Height(6)
		}
		if a.cftStatus != "" {
			ui.Text(c, a.cftStatus).FontSize(13).TextColor(c.Theme().TextMuted)
		}
	}
}

func (a *App) wizardPlatformStep(c *ui.Context) {
	ui.Text(c, "选一个，或两个都选。每个平台用自己的免费账号。").FontSize(15).TextColor(c.Theme().TextMuted)
	ui.Row(c).Gap(20).AlignItems(ui.Stretch).Children(func() {
		a.wizardPlatformCard(c, "豆包", "在豆包里生图。扫码登录一个免费账号。", &a.platDoubao)
		a.wizardPlatformCard(c, "千问", "在千问里生图。扫码登录一个免费账号。", &a.platQianwen)
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
	ui.Button(c.Key("wizard-"+name), "").Grow(1).MinHeight(200).Border(width, border).OnClick(func() {
		*on = !*on
	}).Children(func() {
		ui.Column(c).FillWidth().Gap(12).Padding(8, 4).Children(func() {
			ui.Text(c, name).FontSize(26).Bold()
			ui.Text(c, blurb).FontSize(14).TextColor(c.Theme().TextMuted)
			if *on {
				ui.Text(c, "已选择").FontSize(14).TextColor(c.Theme().Accent)
			} else {
				ui.Text(c, "点击选择").FontSize(14).TextColor(c.Theme().TextMuted)
			}
		})
	})
}

func (a *App) wizardAccountStep(c *ui.Context) {
	platform := a.wizardQueue.Current()
	if platform == "" {
		platform = a.wizardAccount.Platform
	}
	title := "请在弹出的浏览器中扫码登录"
	ui.Text(c, title).FontSize(18).Bold()
	if platform != "" {
		line := platform
		if a.wizardAccount.Name != "" {
			line += " · " + a.wizardAccount.Name
		}
		ui.Text(c, line).FontSize(16).TextColor(c.Theme().TextMuted)
	}
	ui.Column(c).FillWidth().MinHeight(140).Padding(24, 28).Gap(8).Radius(16).Background(c.Theme().Surface).Border(1, c.Theme().Border).Children(func() {
		switch a.wizardPhase {
		case "launching":
			ui.Text(c, a.wizardNote).FontSize(15)
		case "error":
			msg := a.wizardErr
			if msg == "" {
				msg = "还没检测到登录"
			}
			ui.Text(c, msg).FontSize(15).TextColor(c.Theme().Warning)
		default:
			ui.Text(c, "正在检测登录状态…").FontSize(15)
		}
		ui.Text(c, "登录成功后会自动进入下一步。").FontSize(13).TextColor(c.Theme().TextMuted)
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
		ui.Text(c, "要给哪个平台再加一个账号？").FontSize(15)
		ui.Row(c).Gap(12).Children(func() {
			if a.platDoubao {
				ui.Button(c, "再添加豆包").OnClick(func() { a.wizardEnterAccount("豆包") })
			}
			if a.platQianwen {
				ui.Button(c, "再添加千问").OnClick(func() { a.wizardEnterAccount("千问") })
			}
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
		case wizard.StepAccount:
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
		a.wizardStep = wizard.StepPlatform
	case wizard.StepPlatform:
		if !a.wizardCanNext() {
			return
		}
		a.wizardCommitChoices()
		a.wizardEnterAccount("")
	}
}

func (a *App) wizardBack() {
	a.stopWizardLoop()
	a.wizardPickExtra = false
	switch a.wizardStep {
	case wizard.StepDone, wizard.StepAccount:
		a.wizardStep = wizard.StepPlatform
	case wizard.StepPlatform:
		a.wizardStep = wizard.StepBrowser
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
		a.wizardStep = wizard.StepDone
		a.wizardPhase = "ready"
		a.wizardNote = ""
		return
	}
	a.wizardStep = wizard.StepAccount
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
		if err != nil {
			a.wizardUI(func() {
				if gen != a.wizardGen {
					return
				}
				a.wizardPhase = "error"
				a.wizardErr = "启动浏览器失败：" + err.Error()
				a.wizardNote = "请在弹出的浏览器中扫码登录"
			})
			return
		}
		page := "about:blank"
		if plat := strategy.Factory(account.Platform); plat != nil {
			page = plat.HomeURL()
		}
		if err := a.openWizardPage(ws, page); err != nil {
			a.wizardUI(func() {
				if gen != a.wizardGen {
					return
				}
				a.wizardPhase = "error"
				a.wizardErr = "打开登录页失败：" + err.Error()
				a.wizardNote = "请在弹出的浏览器中扫码登录"
			})
			return
		}
		a.wizardUI(func() {
			if gen != a.wizardGen || a.store == nil {
				return
			}
			a.store.UpdateBrowser(account.ID, true, false)
			a.wizardPhase = "waiting"
			a.wizardNote = "请在弹出的浏览器中扫码登录"
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
				action = a.noteProbe(gen, raw, probeErr)
			})
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
			a.wizardStep = wizard.StepDone
			a.wizardPhase = "ready"
			a.wizardNote = ""
			a.wizardErr = ""
			return
		}
		account, err := a.ensureWizardAccount()
		if err != nil {
			a.wizardPhase = "error"
			a.wizardErr = err.Error()
			a.wizardNote = "请在弹出的浏览器中扫码登录"
			return
		}
		a.wizardAccount = account
		a.wizardPhase = "launching"
		a.wizardNote = "正在打开" + account.Platform + "…"
		a.wizardErr = ""
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
	return a.store.AddBrowser(platform, wizard.NextAccountName(names), "")
}

func (a *App) noteProbe(gen int, raw string, err error) wizard.Poll {
	if gen != a.wizardGen {
		return wizard.PollStop
	}
	if err != nil || !wizard.LoggedIn(raw) {
		a.wizardPhase = "waiting"
		a.wizardNote = "请在弹出的浏览器中扫码登录"
		a.wizardErr = ""
		if err != nil {
			a.wizardPhase = "error"
			a.wizardErr = "还没检测到登录，可以在浏览器里完成后再点重新检测"
		}
		return wizard.PollWait
	}
	if a.store != nil && a.wizardAccount.ID != 0 {
		a.store.UpdateBrowser(a.wizardAccount.ID, true, true)
		a.store.ResetBrowserHealth(a.wizardAccount.ID)
	}
	next, poll := wizard.AfterLogin(a.wizardQueue, true)
	a.wizardQueue = next
	if poll == wizard.PollNext {
		a.wizardAccount = store.Browser{}
		a.wizardPhase = "launching"
		a.wizardErr = ""
	}
	if poll == wizard.PollDone {
		a.wizardStep = wizard.StepDone
		a.wizardPhase = "ready"
		a.wizardNote = ""
		a.wizardErr = ""
	}
	return poll
}

func (a *App) launchWizardBrowser(b store.Browser) (string, error) {
	if a.wizardLaunch != nil {
		return a.wizardLaunch(b)
	}
	if a.manager == nil {
		return "", fmt.Errorf("没有浏览器")
	}
	return a.manager.Start(b, a.dataRoot, a.browserBackend, false)
}

func (a *App) openWizardPage(ws, rawURL string) error {
	if a.wizardOpen != nil {
		return a.wizardOpen(ws, rawURL)
	}
	return cdp.OpenURL(ws, rawURL)
}

func (a *App) probeWizardLogin(ctx context.Context, endpoint string) (string, error) {
	if a.wizardProbe != nil {
		return a.wizardProbe(ctx, endpoint)
	}
	return cdp.ProbeLogin(ctx, endpoint)
}
