package ui

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/camoufox"
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
// 已有账号或完成标记的旧用户，以及已经展示过向导的用户，直接进入主界面。
func (a *App) applyWizardGate() {
	if a == nil || a.store == nil {
		return
	}
	if a.store.GetBool(wizard.KeyShown) {
		a.wizardOn = false
		return
	}
	// 旧版正常退出时保存工作区状态，可用来识别没有向导标记的老用户。
	// shortcut: 未保存过工作区的旧用户依赖账号和完成标记，若有其他历史记录再补迁移。
	if a.store.Get("gen_stage_picked") != "" {
		a.store.Set(wizard.KeyComplete, wizard.CompleteYes)
	}
	accounts := wizardAccounts(a.store)
	if !wizard.ShouldShow(a.store.Get(wizard.KeyComplete), accounts) {
		if a.store.Get(wizard.KeyComplete) != wizard.CompleteYes {
			a.store.Set(wizard.KeyComplete, wizard.CompleteYes)
		}
		a.wizardOn = false
		return
	}
	a.store.SetInt(wizard.KeyShown, 1)
	a.openWizard()
}

// openWizard 从第一步重新打开。已经登录的账号会留着。
func (a *App) openWizard() {
	a.stopWizardLoop()
	if a.wizardAgy.created != 0 {
		a.abandonAgyAccount()
	}
	a.wizardOn = true
	a.wizardDir = 1
	a.wizardStep = wizard.StepPlatform
	a.wizardExtra = false
	a.wizardPhase = ""
	a.wizardNote = ""
	a.wizardErr = ""
	a.wizardAccount = store.Browser{}
	a.wizardName = ""
	a.wizardNameFor = 0
	a.wizardChoice = wizard.DefaultChoice(a.selectedPlatforms())
	complete := ""
	if a.store != nil {
		complete = a.store.Get(wizard.KeyComplete)
	}
	a.wizardFirst = wizard.ShouldShow(complete, wizardAccounts(a.store))
	a.wizardAgy = wizardAgyState{watch: a.wizardAgy.watch}
}

func (a *App) refreshWizardPlan() {
	if a.wizardPlanFn != nil {
		a.wizardPlan = a.wizardPlanFn()
		return
	}
	_, ready := browser.InstalledCamoufox(browser.ConfigDir())
	a.wizardPlan = wizard.PlanBrowser(runtime.GOOS, runtime.GOARCH, nil, ready, browser.CamoufoxSupported(runtime.GOOS, runtime.GOARCH))
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

const (
	wizardSideWidth float32 = 248
	wizardBodyWidth float32 = 640
)

// wizardFlow 是选定平台要走的步骤，左侧步骤栏照它画。
func (a *App) wizardFlow() []wizard.Step { return wizard.Flow(a.wizardChoice) }

func (a *App) wizardView(c *ui.Context) {
	tok := a.look
	w, _ := c.Size()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(tok.paper).Children(func() {
		a.wizardSidebar(c)
		ui.Column(c.Key("wizard-main")).Grow(1).MinWidth(0).Children(func() {
			ui.Scroll(c.Key("wizard-body")).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(56, 40, 28, 40).Children(func() {
					ui.Column(c.Key(a.wizardStep)).FillWidth().MaxWidth(wizardBodyWidth).Gap(24).Transition(a.stepMotion()).Children(func() {
						a.wizardHeader(c)
						switch a.wizardStep {
						case wizard.StepBrowser:
							a.wizardBrowserStep(c)
						case wizard.StepInstall:
							a.wizardInstallStep(c)
						case wizard.StepAccount:
							a.wizardAccountStep(c)
						case wizard.StepDone:
							a.wizardDoneStep(c)
						default:
							a.wizardPlatformStep(c, min(wizardBodyWidth, w-wizardSideWidth-80))
						}
					})
				})
			})
			a.wizardFooter(c)
		})
	})
}

// wizardSidebar 左侧栏：品牌、竖排步骤、版本和检查更新。
func (a *App) wizardSidebar(c *ui.Context) {
	tok := a.look
	ui.Column(c.Key("wizard-side")).Width(wizardSideWidth).FillHeight().Shrink(0).Padding(32, 24, 20, 24).
		Background(tok.card).BorderWidth(0, 1, 0, 0).BorderColor(tok.border).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Column(c.Key("wizard-logo")).Size(36, 36).Radius(10).Center().Shrink(0).Background(tok.accent.Alpha(0.12)).Children(func() {
				ui.Icon(c, iconLogo).FontSize(22).TextColor(tok.accent)
			})
			ui.Column(c).Children(func() {
				ui.Text(c, "tulai").FontSize(20).Bold()
				ui.Text(c, "快速设置").FontSize(tok.caption).TextColor(tok.muted)
			})
		})
		ui.Box(c).Height(40)
		for i, step := range a.wizardFlow() {
			state := railUpcoming
			switch {
			case step < a.wizardStep || a.wizardStep == wizard.StepDone:
				state = railDone
			case step == a.wizardStep:
				state = railCurrent
			}
			if i > 0 {
				link := ui.Box(c.Key(fmt.Sprintf("rail-link-%d", i))).Width(2).Height(20).Margin(3, 0, 3, 10).Radius(1)
				col := tok.border
				if state != railUpcoming {
					col = tok.accent
				}
				link.Background(link.AnimateColor("c", col, motionEaseFast))
			}
			ui.Row(c.Key(fmt.Sprintf("rail-%d", i))).Gap(12).AlignItems(ui.Center).Children(func() {
				wizardBadge(c, i, state, tok)
				label := ui.Text(c, step.Label()).FontSize(tok.body)
				switch state {
				case railCurrent:
					label.Bold()
				case railUpcoming:
					label.TextColor(tok.muted)
				}
			})
		}
		ui.Spacer(c)
		ui.Column(c).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "当前版本 "+currentVersion()).FontSize(tok.caption).TextColor(tok.muted)
			label := "检查更新"
			if a.updateHidden && a.updateDownloading {
				label = "显示更新进度"
			}
			ui.Button(c, label).FillWidth().Disabled(a.updateBusy && !(a.updateHidden && a.updateDownloading)).OnClick(func() { a.checkForUpdates() })
		})
	})
}

const (
	railUpcoming = iota
	railCurrent
	railDone
)

func wizardBadge(c *ui.Context, i, state int, tok tokens) {
	b := ui.Column(c.Key(fmt.Sprintf("rail-badge-%d", i))).Size(22, 22).Radius(11).Center().Shrink(0)
	switch state {
	case railDone:
		b.Background(b.AnimateColor("bg", tok.accent, motionEaseFast))
		b.Children(func() { ui.Icon(c, iconCheck).FontSize(13).TextColor(tok.onAccent) })
	case railCurrent:
		b.Border(2, tok.accent).Background(tok.accent.Alpha(0.1))
		b.Children(func() { ui.Text(c, fmt.Sprint(i+1)).FontSize(tok.caption).Bold().TextColor(tok.accent) })
	default:
		b.Border(1, tok.border)
		b.Children(func() { ui.Text(c, fmt.Sprint(i+1)).FontSize(tok.caption).TextColor(tok.muted) })
	}
}

func (a *App) wizardHeader(c *ui.Context) {
	tok := a.look
	lead, sub := wizardIntro(a.wizardStep, a.wizardIsAgy())
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			if a.wizardStep == wizard.StepDone {
				doneMark(c)
			}
			ui.Text(c, a.wizardStep.Title()).FontSize(26).Bold()
		})
		if lead != "" {
			ui.Text(c, lead).FontSize(15)
		}
		ui.Text(c, sub).FontSize(tok.body).TextColor(tok.muted)
	})
}

func wizardIntro(step wizard.Step, agyFlow bool) (lead, sub string) {
	switch step {
	case wizard.StepBrowser:
		return "", "已自动选好。确认后点下一步即可。"
	case wizard.StepInstall:
		return "", "tulai 通过 Antigravity 命令行（agy）出图，不用浏览器。先装好它，再登录 Google 账号。"
	case wizard.StepAccount:
		if agyFlow {
			return "", "选一种方式登录。登录成功后，账号会留在列表里。"
		}
		return pageLoginTitle, "浏览器会打开平台登录页。登录成功后窗口自动关闭，账号留在列表里。"
	case wizard.StepDone:
		return "", "图生图先加图片；只写提示词就是文生图。"
	default:
		return "", "先选一个平台开始，之后可以再添加别的平台。每个平台用自己的免费账号。"
	}
}

func wizardCard(e ui.Element, tok tokens) ui.Element {
	e = e.Radius(tok.radiusL).Background(tok.card).Border(1, tok.border).Shadow(0, 1, 2, 0, tok.shadow)
	if !tok.dark {
		e.InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, 0.9))
	}
	return e
}

func wizardTile(c *ui.Context, key string, icon *ui.SVG, size float32, tok tokens) {
	ui.Column(c.Key(key)).Size(size, size).Radius(size / 4).Center().Shrink(0).Background(tok.accent.Alpha(0.12)).Children(func() {
		ui.Icon(c, icon).FontSize(size * 0.5).TextColor(tok.accent)
	})
}

func wizardPill(c *ui.Context, key, text string, fg ui.Color) {
	ui.Row(c.Key(key)).Padding(2, 8).Radius(999).Shrink(0).AlignItems(ui.Center).Background(fg.Alpha(0.14)).Children(func() {
		ui.Text(c, text).FontSize(12).TextColor(fg).SingleLine()
	})
}

func (a *App) wizardBrowserStep(c *ui.Context) {
	tok := a.look
	plan := a.wizardPlan
	if plan.Title == "" {
		plan.Title = "正在检查本机浏览器…"
	}
	wizardCard(ui.Row(c.Key("browser-card")).FillWidth().Gap(16).AlignItems(ui.Center).Padding(20), tok).Children(func() {
		wizardTile(c, "browser-tile", iconGlobe, 48, tok)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Text(c, plan.Title).FontSize(18).Bold()
			if plan.Detail != "" {
				detail := ui.Text(c, plan.Detail).FontSize(13).TextColor(tok.muted)
				if !plan.NeedsDownload {
					detail.SingleLine().Tooltip(plan.Detail)
				}
			}
			if plan.Blocked != "" {
				ui.Text(c, plan.Blocked).FontSize(13).TextColor(tok.warning)
			}
		})
		switch {
		case plan.Blocked != "":
			wizardPill(c, "browser-pill", "不可用", tok.warning)
		case plan.NeedsDownload && a.camoufoxBusy:
			wizardPill(c, "browser-pill", "下载中", tok.accent)
		case plan.NeedsDownload:
			wizardPill(c, "browser-pill", "待下载", tok.warning)
		case plan.CanNext:
			wizardPill(c, "browser-pill", "已就绪", tok.success)
		}
	})
	ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
		if plan.NeedsDownload {
			label := "安装 Camoufox"
			if a.camoufoxReady {
				label = "检查 Camoufox"
			}
			ui.PrimaryButton(c, label).Disabled(a.camoufoxBusy || !browser.CamoufoxSupported(runtime.GOOS, runtime.GOARCH)).OnClick(func() { a.installCamoufox() })
		}
		ui.Button(c, "重新检测").Disabled(a.camoufoxBusy).OnClick(func() {
			a.refreshDetected()
			a.refreshWizardPlan()
		})
	})
	if a.camoufoxBusy {
		a.camoufoxProgressBlock(c, "camoufox-progress")
	} else if plan.NeedsDownload && a.camoufoxStatus != "" {
		ui.Text(c, a.camoufoxStatus).FontSize(13).TextColor(tok.muted)
	}
}

func (a *App) wizardPlatformStep(c *ui.Context, width float32) {
	a.wizardNormalizeChoice()
	ui.RadioGroup(c, func() {
		for _, name := range wizard.Platforms() {
			a.wizardPlatformCard(c, name, wizardBlurb(name), width)
		}
	}).Label("选择一个平台").Row().Wrap().FillWidth().Gap(16).AlignItems(ui.Stretch)
}

func (a *App) wizardPlatformCard(c *ui.Context, name, blurb string, available float32) {
	tok := a.look
	on := a.wizardChoice == name
	card := ui.RadioBase(c.Key("wizard-"+name), &a.wizardChoice, name).Grow(1).Basis((available - 16) / 2).MinWidth(196).MinHeight(188).
		AlignItems(ui.Stretch).Radius(tok.radiusL).Cursor(ui.CursorPointer)
	bg, border, width := tok.card, tok.border, float32(1)
	switch {
	case on:
		bg, border, width = tok.accent.Alpha(0.07), tok.accent, 1.5
	case card.Hovered():
		border = tok.accent.Alpha(0.5)
	}
	card.Background(bg).Border(width, border).Transition(motionColors())
	if on {
		card.Shadow(0, 8, 24, 0, tok.lift)
	} else {
		card.Shadow(0, 1, 2, 0, tok.shadow)
	}
	hoverPressPose(card)
	card.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(14).Padding(20).Children(func() {
			ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c.Key("plat-tile")).Size(44, 44).Radius(12).Center().Shrink(0).Background(tok.sunken).Children(func() {
					platformIcon(c, name, 26, tok)
				})
				ui.Text(c, name).FontSize(18).Bold().SingleLine().MinWidth(0)
			})
			ui.Text(c, blurb).FontSize(tok.body).TextColor(tok.muted).Grow(1)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				mark := ui.Column(c.Key("pick-mark")).Size(18, 18).Radius(9).Center().Shrink(0).Transition(motionColors())
				label, color := "点击选择", tok.muted
				if on {
					label, color = "已选择", tok.accent
					mark.Background(tok.accent).Children(func() {
						ui.Icon(c, iconCheck).FontSize(12).TextColor(tok.onAccent)
					})
				} else {
					mark.Border(1.5, tok.muted.Alpha(0.55))
				}
				ui.Text(c.Key("pick"), label).FontSize(tok.body).TextColor(color).
					ContentTransition(ui.ContentTransition{Animation: &motionEaseFast})
			})
		})
	})
}

func (a *App) wizardAccountStep(c *ui.Context) {
	if a.wizardIsAgy() {
		a.wizardAgyAccountStep(c)
		return
	}
	tok := a.look
	a.syncWizardName()
	platform := a.wizardQueue.Current()
	if platform == "" {
		platform = a.wizardAccount.Platform
	}
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
	wizardCard(ui.Column(c.Key("login-card")).FillWidth(), tok).Children(func() {
		ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Center).Padding(16, 20).Children(func() {
			ui.Column(c.Key("login-tile")).Size(44, 44).Radius(12).Center().Shrink(0).Background(tok.sunken).Children(func() {
				platformIcon(c, platform, 26, tok)
			})
			if line != "" {
				ui.Text(c, line).FontSize(tok.title).Bold().SingleLine().Grow(1).MinWidth(0)
			}
		})
		ui.Divider(c)
		ui.Column(c).FillWidth().Padding(16, 20).Children(func() {
			a.loginStatusBody(c, a.wizardAccount, true)
		})
	})
	ui.Column(c.Key("login-name")).Gap(6).Children(func() {
		ui.Text(c, "账号名称").FontSize(tok.caption).TextColor(tok.muted)
		ui.TextInput(c, &a.wizardName).Width(280).Placeholder("例如工作号")
	})
}

func (a *App) loginStatusBody(c *ui.Context, b store.Browser, inWizard bool) {
	tok := a.look
	shot := a.loginShot(b.ID)
	loggedIn := b.IsLoggedIn || shot.state == wizard.ProbeIn || (inWizard && a.wizardPhase == "in")
	if loggedIn {
		wizardStatus(c, "ok", "已登录", tok.success, tok)
		return
	}
	if shot.state == wizard.ProbeRegion || (inWizard && a.wizardPhase == "region") {
		msg := regionProxyHint
		if inWizard && a.wizardErr != "" {
			msg = a.wizardErr
		}
		wizardStatus(c, "warn", msg, tok.warning, tok)
		return
	}
	if shot.hint != "" {
		wizardStatus(c, "warn", shot.hint, tok.warning, tok)
		return
	}
	msg := pageLoginHint
	kind, color := "wait", tok.text
	if inWizard {
		switch a.wizardPhase {
		case "launching":
			if a.wizardNote != "" {
				msg = a.wizardNote
			}
		case "error":
			kind, color = "warn", tok.warning
			msg = a.wizardErr
			if msg == "" {
				msg = "还没检测到登录"
			}
		case "waiting":
			if a.wizardNote != "" {
				msg = a.wizardNote
			}
		}
	}
	if msg == pageLoginTitle {
		msg = "等待登录…"
	}
	wizardStatus(c, kind, msg, color, tok)
}

// wizardStatus 是登录卡片里的一行状态：左边一个标记，右边文字，多行时标记对齐第一行。
func wizardStatus(c *ui.Context, kind, text string, color ui.Color, tok tokens) {
	ui.Row(c.Key("wizard-status")).FillWidth().Gap(10).AlignItems(ui.Start).Children(func() {
		switch kind {
		case "wait":
			ui.Row(c).Shrink(0).Margin(6, 0, 0, 0).Children(func() { waitingDot(c) })
		case "ok":
			ui.Icon(c, iconCheck).FontSize(16).TextColor(tok.success).Shrink(0).Margin(2, 0, 0, 0)
		default:
			ui.Box(c.Key("warn-dot")).Size(8, 8).Radius(4).Shrink(0).Margin(6, 0, 0, 0).Background(tok.warning)
		}
		ui.Text(c.Key("wizard-phase"), text).FontSize(tok.body).TextColor(color).Grow(1).MinWidth(0)
	})
}

type doneAccount struct{ platform, name string }

func (a *App) wizardDoneStep(c *ui.Context) {
	tok := a.look
	ui.Row(c.Key("done-tips")).FillWidth().Gap(12).AlignItems(ui.Stretch).Children(func() {
		for i, tip := range []struct {
			icon *ui.SVG
			text string
		}{
			{iconImages, "拖入图片"},
			{iconPen, "选提示词"},
			{iconPlay, "开始"},
		} {
			wizardCard(ui.Column(c.Key(fmt.Sprintf("tip-%d", i))).Grow(1).Basis(0).Gap(16).Padding(16, 18), tok).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					wizardTile(c, "tip-tile", tip.icon, 36, tok)
					ui.Spacer(c)
					ui.Text(c, fmt.Sprint(i+1)).FontSize(tok.caption).TextColor(tok.muted)
				})
				ui.Text(c, tip.text).FontSize(16).Bold()
			})
		}
	})
	if accounts := a.wizardDoneAccounts(); len(accounts) > 0 {
		ui.Column(c.Key("done-accounts")).FillWidth().Gap(8).Children(func() {
			ui.Text(c, "已登录").FontSize(tok.caption).TextColor(tok.muted)
			ui.Row(c).FillWidth().Gap(8).Wrap().Children(func() {
				for i, acct := range accounts {
					ui.Row(c.Key(fmt.Sprintf("acct-%d", i))).Gap(8).AlignItems(ui.Center).Padding(6, 12).Radius(999).
						Background(tok.card).Border(1, tok.border).Children(func() {
						platformIcon(c, acct.platform, 16, tok)
						ui.Text(c, acct.platform+" · "+acct.name).FontSize(13).SingleLine()
					})
				}
			})
		})
	}
	ui.Row(c.Key("done-hint")).FillWidth().Gap(10).AlignItems(ui.Start).Padding(14, 16).Radius(tok.radiusM).Background(tok.sunken).Children(func() {
		ui.Icon(c, iconRefresh).FontSize(16).TextColor(tok.accent).Shrink(0).Margin(2, 0, 0, 0)
		ui.Text(c, "再添加一个账号后，额度用尽、限流或验证码会自动换号。").FontSize(13).TextColor(tok.muted).Grow(1).MinWidth(0)
	})
}

func (a *App) wizardDoneAccounts() []doneAccount {
	if a.store == nil {
		return nil
	}
	var out []doneAccount
	for _, row := range a.store.Browsers() {
		if row.ExtAccount != "" || !row.IsLoggedIn || row.Name == "" {
			continue
		}
		out = append(out, doneAccount{row.Platform, row.Name})
	}
	return out
}

func (a *App) wizardFooter(c *ui.Context) {
	tok := a.look
	_, hasPrev := wizard.Prev(a.wizardChoice, a.wizardStep)
	ui.Column(c.Key("wizard-footer")).FillWidth().AlignItems(ui.Center).Padding(14, 40).
		Background(tok.card).BorderWidth(1, 0, 0, 0).BorderColor(tok.border).Children(func() {
		ui.Row(c).FillWidth().MaxWidth(wizardBodyWidth).Gap(12).AlignItems(ui.Center).Children(func() {
			if hasPrev {
				ui.Button(c, "上一步").OnClick(func() { a.wizardBack() })
			}
			ui.Column(c).Grow(1)
			switch a.wizardStep {
			case wizard.StepDone:
				ui.Button(c, "再添加一个账号").OnClick(func() { a.wizardAddAnother() })
				ui.PrimaryButton(c, "进入主页").OnClick(func() { a.wizardFinish() })
			case wizard.StepAccount:
				// Antigravity 的登录方式和重试都在页面里，底栏只留上一步。
				if a.wizardIsAgy() {
					break
				}
				if a.wizardNavFailed {
					ui.Button(c, "重新打开").Disabled(a.wizardPhase == "launching").OnClick(func() { a.wizardRecheck() })
				}
				ui.Button(c, "已登录? 重新检测").Disabled(a.wizardPhase == "launching").OnClick(func() { a.wizardRecheck() })
			default:
				ui.PrimaryButton(c, "下一步").Disabled(!a.wizardCanNext()).OnClick(func() { a.wizardNext() })
			}
		})
	})
}

func (a *App) wizardCanNext() bool {
	switch a.wizardStep {
	case wizard.StepPlatform:
		return true
	case wizard.StepBrowser:
		return a.wizardPlan.CanNext && !a.camoufoxBusy
	case wizard.StepInstall:
		return a.wizardAgy.found
	default:
		return false
	}
}

func (a *App) wizardNext() {
	if !a.wizardCanNext() {
		return
	}
	switch a.wizardStep {
	case wizard.StepPlatform:
		a.wizardNormalizeChoice()
		a.wizardApplyChoice()
		if next, ok := wizard.Next(a.wizardChoice, wizard.StepPlatform); ok {
			a.goWizard(next)
		}
	case wizard.StepBrowser, wizard.StepInstall:
		a.wizardCommitChoices()
		a.wizardEnterAccount(false)
	}
}

func (a *App) wizardBack() {
	account := a.wizardAccount
	leavingLogin := a.wizardStep == wizard.StepAccount
	a.stopWizardLoop()
	if leavingLogin {
		a.abandonUnlogged(account)
		a.wizardAccount = store.Browser{}
		a.abandonAgyAccount()
	}
	// 完成页回到第一步：可以改选，也可以再选另一个平台。
	if a.wizardStep == wizard.StepDone {
		a.goWizard(wizard.StepPlatform)
		return
	}
	if prev, ok := wizard.Prev(a.wizardChoice, a.wizardStep); ok {
		a.goWizard(prev)
	}
}

func (a *App) wizardCommitChoices() {
	if a.store == nil {
		return
	}
	if a.wizardNeedsBrowser() && a.wizardPlan.Backend != "" {
		a.browserBackend = a.wizardPlan.Backend
		a.browserChoice = browser.ChoiceLabel(a.browserBackend)
		a.store.Set("browser_backend", a.browserBackend)
	}
	a.wizardNormalizeChoice()
	a.wizardApplyChoice()
	if ps := a.selectedPlatforms(); len(ps) > 0 {
		a.store.SetPlatforms(ps)
	}
}

// wizardAddAnother 给刚设置的这个平台再加一个账号。想加别的平台，点上一步回到第一步重选。
func (a *App) wizardAddAnother() { a.wizardEnterAccount(true) }

// wizardEnterAccount 进入登录步骤。extra 为真是再加一个账号，哪怕这个平台已经有能用的。
func (a *App) wizardEnterAccount(extra bool) {
	a.wizardExtra = extra
	a.wizardQueue = wizard.NewQueue(nil)
	a.wizardAccount = store.Browser{}
	a.wizardErr = ""
	if a.wizardIsAgy() {
		a.wizardEnterAgyAccount(extra)
		return
	}
	if extra || a.platformNeedsLogin(a.wizardChoice) {
		a.wizardQueue = wizard.NewQueue([]string{a.wizardChoice})
	}
	if a.wizardQueue.Done() {
		a.enterDoneStep()
		return
	}
	a.goWizard(wizard.StepAccount)
	a.wizardPhase = "launching"
	a.wizardNote = "正在打开登录页…"
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
		a.wizardLandOnChoice()
		a.saveSettings()
	}
	a.wizardOn = false
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
			a.enterDoneStep()
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
		dir = browser.ProfileDir(a.dataRoot, a.browserBackend, "account-"+rand.Text())
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
		a.enterDoneStep()
	}
	return poll
}

func (a *App) enterDoneStep() {
	a.wizardPhase = "ready"
	a.wizardNote = ""
	a.wizardErr = ""
	// 已经有账号了：回到第一步再选别的平台，是在原来的基础上加，不是重来。
	a.wizardFirst = false
	a.goWizard(wizard.StepDone)
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
	viewing := a.browserViewingID == b.ID
	a.mu.Unlock()
	if viewing {
		return
	}
	if a.manager != nil && !a.manager.StopLogin(b.ID) {
		return
	}
	a.mu.Lock()
	a.loginClosed = append(a.loginClosed, b.ID)
	a.mu.Unlock()
	cdp.ReleaseLoginFront(loginEndpoint(b))
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
	return camoufox.Endpoint(b.Port)
}

// watchLogin 在新账号登录后关闭窗口；手动打开的已有环境保持打开。
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
			closeAfterLogin := state == wizard.ProbeIn && a.loginIsFresh(b.ID)
			if !a.publishLogin(b.ID, gen, state, hint) {
				return
			}
			if state == wizard.ProbeIn {
				if closeAfterLogin && a.loginCurrent(b.ID, gen) {
					a.closeLoginWindow(b)
				}
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
