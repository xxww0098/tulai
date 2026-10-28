package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func (a *App) chrome(c *ui.Context) {
	tok := a.look
	ui.Column(c).Padding(10, 28, 8, 28).Gap(8).Background(tok.card).
		BorderWidth(0, 0, 1, 0).BorderColor(tok.border).
		Shadow(0, 1, 0, 0, tok.shadow).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "tulai").Font("Inter").FontSize(tok.display).Bold()
			subtitle := "豆包 / 千问"
			if v := mygo.App.Version(); v != "" {
				subtitle += " · " + v
			}
			ui.Text(c, subtitle).FontSize(tok.caption).TextColor(tok.muted).Grow(1).MinWidth(0).SingleLine()
			ui.Button(c, "设置向导").Disabled(a.running).OnClick(func() { a.openWizard() })
			ui.Button(c, "检查更新").Disabled(a.updateBusy).OnClick(func() { a.checkForUpdates() })
		})
		a.updateBanner(c)
		a.tabBar(c)
	})
}

func (a *App) tabBar(c *ui.Context) {
	tok := a.look
	labels := []string{"出图", "浏览器", "提示词", "设置"}
	parts := ui.SegmentedBase(c.Key("main-tabs"), &a.tab, len(labels))
	parts.Track.Padding(4).Gap(4).Radius(tok.radiusM).Background(tok.sunken).Children(func() {
		for i, label := range labels {
			seg := parts.Segment(i).Padding(6, 16).Radius(tok.radiusS).Disabled(a.running)
			if i == a.tab {
				seg.Background(tok.card).Shadow(0, 1, 2, 0, tok.shadow)
			}
			chosen := i == a.tab
			seg.Children(func() {
				tx := ui.Text(c, label).FontSize(tok.body)
				if chosen {
					tx.Bold().TextColor(tok.accent)
				} else {
					tx.TextColor(tok.muted)
				}
			})
		}
	})
}

func (a *App) homePage(c *ui.Context) {
	flow := a.flowNow()
	ui.Scroll(c.Key("home")).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(10).Padding(4, 0, 8, 0).Children(func() {
			a.loginBanner(c)
			a.flowStepper(c, flow)
			if flow.Run.State != stepCurrent {
				a.accountPanel(c, true)
			}
			a.stepImages(c, flow.Images)
			a.stepPrompt(c, flow.Prompt)
			a.stepRun(c, flow.Run)
			if flow.Run.State == stepCurrent {
				a.accountPanel(c, false)
			}
			a.progressPane(c)
			ui.Collapsible(c, "高级", &a.advancedOpen, func() {
				a.advancedFields(c)
			})
		})
	})
	a.previewModal(c)
}

func (a *App) flowNow() flowLayout {
	return LayoutFlow(len(a.sources) > 0, len(a.taskPromptList()) > 0, a.skipImages, len(a.queueItems()) > 0, a.flowPin)
}

func (a *App) flowStepper(c *ui.Context, flow flowLayout) {
	tok := a.look
	type item struct {
		n, label string
		state    int
		pin      int
	}
	items := []item{
		{"1", "选图片", flow.Images.State, 1},
		{"2", "写提示词", flow.Prompt.State, 2},
		{"3", "出图", flow.Run.State, 3},
	}
	ui.Row(c.Key("flow-steps")).Gap(0).AlignItems(ui.Center).Children(func() {
		for i, it := range items {
			if i > 0 {
				line := tok.border
				if items[i-1].state == stepDone {
					line = tok.accent.Alpha(0.5)
				}
				ui.Box(c).Width(28).Height(2).Radius(1).Margin(0, 8).Background(line)
			}
			pin := it.pin
			state := it.state
			mark := it.n
			name := it.label
			if state == stepDone {
				mark = "✓"
			}
			step := ui.Row(c.Key(pin)).Gap(8).AlignItems(ui.Center).Padding(2, 0)
			if step.Clicked() {
				if a.flowPin == pin {
					a.flowPin = 0
				} else {
					a.flowPin = pin
				}
			}
			step.Children(func() {
				fg, fill := tok.muted, tok.sunken
				switch state {
				case stepCurrent:
					fg, fill = tok.onAccent, tok.accent
				case stepDone:
					fg, fill = tok.accent, tok.accent.Alpha(0.14)
				}
				ui.Column(c).Size(26, 26).Radius(13).Background(fill).Center().Children(func() {
					ui.Text(c, mark).FontSize(12).Bold().TextColor(fg)
				})
				label := ui.Text(c, name).FontSize(tok.body).TextColor(tok.muted)
				if state == stepCurrent {
					label.Bold().TextColor(tok.text)
				} else if state == stepDone {
					label.TextColor(tok.text)
				}
			})
		}
	})
}

func (a *App) stepCard(c *ui.Context, key string, state int) ui.Element {
	tok := a.look
	card := ui.Column(c.Key(key)).FillWidth().Padding(12, 14).Gap(8).Radius(tok.radiusL).
		Background(tok.card).Border(1, tok.border).Shadow(0, 1, 2, 0, tok.shadow).
		Transition(motionReveal())
	if !tok.dark {
		card.InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, 0.9))
	}
	if state == stepCurrent {
		card.Border(1.5, tok.accent).Shadow(0, 8, 24, 0, tok.lift)
	}
	return card
}

func (a *App) stepImages(c *ui.Context, step flowStep) {
	key := "step-images"
	if !step.Open {
		key = "step-images-compact"
	}
	card := a.stepCard(c, key, step.State)
	if paths := card.DroppedFiles(); len(paths) > 0 {
		a.ingestSources(paths)
	}
	if card.FileDragOver() && !a.running {
		card.Border(2, a.look.accent).Background(a.look.accent.Alpha(0.08))
	}
	card.Children(func() {
		if step.Open {
			a.imageStepOpen(c)
			return
		}
		a.imageStepCompact(c)
	})
}

func (a *App) imageStepOpen(c *ui.Context) {
	tok := a.look
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "① 选图片").FontSize(tok.title).Bold().Grow(1)
		if n := len(a.sources); n > 0 {
			ui.Text(c, fmt.Sprintf("已选 %d 张", n)).FontSize(tok.body).TextColor(tok.muted)
			ui.Button(c, "添加图片").Disabled(a.running).OnClick(func() { a.addImages() })
			ui.Button(c, "清空").Disabled(a.running).OnClick(func() { a.clearSources() })
		}
	})
	if len(a.sources) == 0 {
		a.emptyDrop(c)
		link := ui.Text(c, "不使用图片，只用提示词").FontSize(tok.body).TextColor(tok.accent)
		if link.Clicked() && !a.running {
			a.skipImages = true
			a.flowPin = 2
		}
		return
	}
	a.sourceGrid(c)
}

func (a *App) emptyDrop(c *ui.Context) {
	tok := a.look
	zone := ui.Column(c.Key("drop-zone")).FillWidth().MinHeight(156).Radius(tok.radiusM).
		Border(1.5, tok.accent.Alpha(0.45)).BorderStyle(ui.BorderDashed).
		Background(tok.sunken).Center().Gap(6).Cursor(ui.CursorPointer)
	if zone.Hovered() && !a.running {
		zone.Border(1.5, tok.accent).Background(tok.accent.Alpha(0.08))
	}
	hoverPressPose(zone)
	if zone.Clicked() && !a.running {
		a.addImages()
	}
	zone.Children(func() {
		ui.Text(c, "拖入或点击添加图片").FontSize(tok.title).Bold()
		ui.Text(c, "可以一次选多张。不放图片也可以只写提示词").FontSize(tok.caption).TextColor(tok.muted)
	})
}

func (a *App) sourceGrid(c *ui.Context) {
	tok := a.look
	width, _ := c.Size()
	cols := int((width - 48) / 168)
	if cols < 2 {
		cols = 2
	}
	if cols > 5 {
		cols = 5
	}
	ui.Grid(c).Columns(cols).Gap(12).Children(func() {
		for i := range a.sources {
			a.sourceTile(c, i)
		}
		a.addTile(c, tok)
	})
}

func (a *App) sourceTile(c *ui.Context, i int) {
	tok := a.look
	si := a.sources[i]
	ui.Column(c.Key(si.Path)).Gap(6).Transition(motionItem()).Children(func() {
		frame := ui.Box(c).FillWidth().AspectRatio(1).Radius(tok.radiusM).Background(tok.sunken)
		frame.Children(func() {
			if bmp := a.cachedThumb(si.Path); bmp != nil {
				ui.Image(c, bmp).FillWidth().AspectRatio(1).Fit(ui.Cover).Radius(tok.radiusM).
					ContentTransition(ui.ContentTransition{Animation: &motionEaseOut})
			}
			a.thumbRemove(c, frame, i)
		})
		ui.Text(c, filepath.Base(si.Path)).FontSize(tok.caption).SingleLine().Tooltip(si.Path)
	})
}

func (a *App) addTile(c *ui.Context, tok tokens) {
	ui.Column(c.Key("add-tile")).Gap(6).Transition(motionItem()).Children(func() {
		tile := ui.Column(c).FillWidth().AspectRatio(1).Radius(tok.radiusM).Center().Gap(4).
			Border(1.5, tok.accent.Alpha(0.45)).BorderStyle(ui.BorderDashed).
			Background(tok.sunken).Cursor(ui.CursorPointer)
		if tile.Hovered() && !a.running {
			tile.Border(1.5, tok.accent).Background(tok.accent.Alpha(0.08))
		}
		hoverPressPose(tile)
		if tile.Clicked() && !a.running {
			a.addImages()
		}
		tile.Children(func() {
			ui.Text(c, "拖入或点击添加图片").FontSize(tok.caption).Bold().TextColor(tok.accent)
		})
		ui.Text(c, "PNG · JPG · WEBP").FontSize(tok.caption).TextColor(tok.muted)
	})
}

func (a *App) thumbRemove(c *ui.Context, host ui.Element, index int) {
	btn := ui.ButtonBase(c.Key("rm")).Label("移除").Tooltip("移除").
		Size(22, 22).Radius(11).Absolute().Top(6).Right(6).Center().
		Background(ui.RGBA(20, 28, 42, 0.78)).Disabled(a.running)
	op := float32(0)
	if host.Hovered() || btn.Hovered() || btn.Focused() {
		op = 1
	}
	btn.Opacity(btn.AnimateTo("rm", op, motionEaseFast))
	btn.OnClick(func() { a.removeSourceAt(index) })
	btn.Children(func() {
		ui.Text(c, "×").FontSize(13).TextColor(ui.Hex("#ffffff"))
	})
}

func (a *App) imageStepCompact(c *ui.Context) {
	tok := a.look
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		title := "还没选图片"
		if a.skipImages && len(a.sources) == 0 {
			title = "未选图片，按提示词出图"
		}
		if n := len(a.sources); n > 0 {
			title = fmt.Sprintf("已选 %d 张", n)
		}
		head := ui.Text(c, "① 选图片").FontSize(tok.caption).TextColor(tok.muted)
		ui.Text(c, title).FontSize(tok.body).Bold()
		if head.Clicked() {
			a.flowPin = 1
		}
		if len(a.sources) > 0 {
			ui.ScrollHorizontal(c).Grow(1).MinWidth(0).Height(36).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					for i := range a.sources {
						a.miniThumb(c, i)
					}
				})
			})
			ui.Button(c, "添加图片").Disabled(a.running).OnClick(func() { a.addImages() })
			ui.Button(c, "清空").Disabled(a.running).OnClick(func() { a.clearSources() })
			return
		}
		ui.Row(c).Grow(1).MinWidth(0)
		ui.Button(c, "添加图片").Disabled(a.running).OnClick(func() { a.addImages() })
		link := ui.Text(c, "只用提示词").FontSize(tok.body).TextColor(tok.accent)
		if link.Clicked() && !a.running {
			a.skipImages = true
			a.flowPin = 2
		}
	})
}

func (a *App) miniThumb(c *ui.Context, i int) {
	si := a.sources[i]
	frame := ui.Box(c.Key(si.Path)).Size(36, 36).Radius(6).Background(a.look.sunken).Transition(motionItem())
	frame.Children(func() {
		if bmp := a.cachedThumb(si.Path); bmp != nil {
			ui.Image(c, bmp).Size(36, 36).Fit(ui.Cover).Radius(6)
		}
		a.thumbRemove(c, frame, i)
	})
}

func (a *App) clearSources() {
	if a.running {
		return
	}
	a.sources = nil
	a.srcSel = -1
	a.skipImages = false
}

func (a *App) stepPrompt(c *ui.Context, step flowStep) {
	key := "step-prompt"
	if !step.Open {
		key = "step-prompt-compact"
	}
	card := a.stepCard(c, key, step.State)
	card.Children(func() {
		if !step.Open {
			a.promptCompact(c)
			return
		}
		a.promptOpen(c)
	})
}

func (a *App) promptOpen(c *ui.Context) {
	tok := a.look
	labels := prompts.Labels(a.userPrompts)
	if !containsStr(labels, a.promptChoice) {
		a.promptChoice = prompts.LabelFor(a.promptPick, a.userPrompts)
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "② 写提示词").FontSize(tok.title).Bold()
		motionSelect(c.Key("prompt-template"), &a.promptChoice, labels).Grow(1).MinWidth(0).Disabled(a.running).OnChange(func() {
			a.applyPromptChoice()
		})
		ui.Button(c, "编辑提示词").Disabled(a.running).OnClick(func() { a.tab = pagePrompts })
	})
	h := float32(72)
	if len(a.queueItems()) > 0 {
		h = 48
	}
	ui.TextArea(c, &a.prompts).Height(h).FillWidth().Label("提示词内容").Placeholder("不放图片时，每行一条。选了模板就整段发送")
	if a.promptNote != "" {
		ui.Text(c, a.promptNote).FontSize(tok.caption).TextColor(tok.muted)
	}
}

func (a *App) promptCompact(c *ui.Context) {
	tok := a.look
	preview := oneLine(a.prompts)
	if preview == "" {
		preview = "还没写提示词"
	}
	rs := []rune(preview)
	if len(rs) > 32 {
		preview = string(rs[:32]) + "…"
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "② 写提示词").FontSize(tok.caption).TextColor(tok.muted)
		body := ui.Text(c, preview).FontSize(tok.body).Grow(1).MinWidth(0).SingleLine().Tooltip(oneLine(a.prompts))
		if body.Clicked() {
			a.flowPin = 2
		}
		ui.Button(c, "修改").Disabled(a.running).OnClick(func() { a.flowPin = 2 })
	})
}

func (a *App) stepRun(c *ui.Context, step flowStep) {
	tok := a.look
	plan := a.workbenchPlan()
	card := a.stepCard(c, "step-run", step.State)
	card.Children(func() {
		ui.Text(c, "③ 出图").FontSize(tok.title).Bold()
		if plan.Action == StartWizard && !a.running {
			a.accountCallout(c)
		}
		board := a.accountBoard(c.Now())
		ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				summary := a.stepLead(board)
				ui.Text(c, summary).FontSize(tok.body).SingleLine().Tooltip(summary)
				if note := a.runNote(); note != "" {
					ui.Text(c.Key("prog"), note).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(note)
				}
				path := a.resolvedOutputDir()
				ui.Text(c, path).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(path)
			})
			a.runButton(c, plan)
		})
	})
}

// runButton 是出图步骤右侧的按钮。开始是主按钮，跑起来以后同一位置换成「停止」，文字交叉淡出。
func (a *App) runButton(c *ui.Context, plan WorkbenchPlan) {
	tok := a.look
	th := c.Theme()
	running := a.running
	// 一个账号都没有时，按钮仍打开设置向导。有账号但今天都不能用，则关掉。
	disabled := !running && plan.Action == StartNone
	if !running && plan.Action == StartGenerate && a.liveParallel() == 0 {
		disabled = true
	}
	label := "开始出图"
	if running {
		label = "停止"
	}
	b := ui.ButtonBase(c.Key("run-go")).Padding(8, 16).Gap(8).Radius(th.Radius).Shrink(0).
		AlignItems(ui.Center).AlignSelf(ui.Center)
	bg, fg := th.Accent, th.AccentText
	border := ui.Color{}
	switch {
	case disabled:
		bg, fg, border = th.Surface, th.TextMuted, th.Border
	case running:
		bg, fg, border = th.Surface, th.Text, th.Border
	}
	if !disabled {
		switch {
		case b.Pressed():
			if running {
				bg = th.SurfacePressed
			} else {
				bg = th.AccentPressed
			}
		case b.Hovered():
			if running {
				bg = th.SurfaceHover
			} else {
				bg = th.AccentHover
			}
		}
	}
	if border.A == 0 {
		border = bg
	}
	b.Background(b.AnimateColor("face", bg, motionEaseFast)).TextColor(fg).Border(1, border)
	if !disabled {
		hoverPressPose(b)
	}
	b.Disabled(disabled)
	if running {
		b.OnClick(func() { a.stopTasks() })
	} else if !disabled {
		b.OnClick(func() { a.startTasks() })
	}
	b.Children(func() {
		runGlyph(c, running, fg)
		ui.Text(c.Key("run-label"), label).FontSize(tok.body).
			ContentTransition(ui.ContentTransition{Animation: &motionEaseFast})
	})
}

func runGlyph(c *ui.Context, stop bool, ink ui.Color) {
	box := ui.Box(c.Key("run-glyph")).Size(12, 12).Shrink(0)
	target := float32(0)
	if stop {
		target = 1
	}
	shown := box.AnimateTo("stop", target, motionEaseFast)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		if shown < 0.98 {
			play := (&ui.Path{}).MoveTo(r.X+2, r.Y+1).LineTo(r.X+2, r.Y+r.H-1).LineTo(r.X+r.W-1, r.Y+r.H/2).Close()
			p.FillPath(play, ink.Alpha(1-shown))
		}
		if shown > 0.02 {
			p.Fill(ui.Rect{X: r.X + 2, Y: r.Y + 2, W: r.W - 4, H: r.H - 4}, ink.Alpha(shown), 1.5)
		}
	})
}

func (a *App) accountCallout(c *ui.Context) {
	tok := a.look
	ui.Row(c.Key("account-callout")).FillWidth().Gap(10).AlignItems(ui.Center).Padding(10, 12).
		Radius(tok.radiusM).Background(tok.warning.Alpha(0.14)).Border(1, tok.warning).
		Transition(motionBanner()).Children(func() {
		ui.Text(c, "还没有可用账号。先去设置登录，再开始出图。").FontSize(tok.body).Grow(1).MinWidth(0)
		ui.Button(c, "去设置").OnClick(func() { a.openWizard() })
	})
}

// stepLead 是出图步骤左侧的一行。跑起来写进度，没有能用的账号就写原因，避免和面板标题重复。
func (a *App) stepLead(board tasks.Board) string {
	cards := ProgressCards(a.queueItems())
	done, _, _, _, _ := ProgressTally(cards)
	if a != nil && a.running {
		return fmt.Sprintf("正在出图 · 已完成 %d/%d", done, len(cards))
	}
	if a.liveParallel() == 0 {
		if len(board.Lanes) > 0 {
			return BlockedAccounts(board.Lanes)
		}
		if len(a.sources) == 0 && len(a.taskPromptList()) == 0 {
			return "先添加图片，或写一条提示词"
		}
		return "还没有可用账号"
	}
	return RunSummary(len(a.sources), len(a.taskPromptList()) > 0, a.liveParallel())
}

// runNote 是出图步骤上的一行进度。待验证已经写在卡片上，这里不再重复成一条失败。
func (a *App) runNote() string {
	if a == nil || a.progText == "" {
		return ""
	}
	for _, task := range a.queueItems() {
		if task.Status == tasks.StatusNeedsUser {
			return ""
		}
	}
	return a.progText
}

func (a *App) loginBanner(c *ui.Context) {
	text, id, ok := "", int64(0), false
	if a.engine != nil {
		text, id, ok = a.engine.Attention()
	}
	if !ok || text == "" {
		return
	}
	ui.Row(c.Key("login-banner")).FillWidth().Gap(10).AlignItems(ui.Center).Padding(10, 12).
		Background(a.look.card).Border(1, a.look.warning).Radius(a.look.radiusM).Transition(motionBanner()).Children(func() {
		ui.Text(c, text).FontSize(a.look.body).TextColor(a.look.warning).Grow(1).MinWidth(0).SingleLine().Tooltip(text)
		ui.Button(c, "去处理").OnClick(func() { a.openAttention(id) })
	})
}

func (a *App) openAttention(id int64) {
	a.tab = pageBrowser
	a.refreshBrowsers()
	for i, row := range a.browserRows {
		if row.ID == id {
			a.browserSel = i
			break
		}
	}
	var row store.Browser
	if a.store != nil {
		for _, b := range a.store.Browsers() {
			if b.ID == id {
				row = b
				break
			}
		}
	}
	if row.ID == 0 || a.engine == nil {
		return
	}
	go func(b store.Browser) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_ = a.ensureAccountPage(ctx, b)
	}(row)
}

func (a *App) progressPane(c *ui.Context) {
	items := a.queueItems()
	cards := ProgressCards(items)
	if len(cards) == 0 {
		return
	}
	tok := a.look
	failed := FailedTasks(items)
	done, running, queued, failN, needs := ProgressTally(cards)
	ui.Column(c.Key("progress-pane")).FillWidth().Padding(12, 14).Gap(10).Radius(tok.radiusL).
		Background(tok.card).Border(1, tok.border).Shadow(0, 1, 2, 0, tok.shadow).
		Transition(motionReveal()).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, "出图进度").FontSize(tok.title).Bold()
				ui.Text(c, FormatProgressTally(done, running, queued, failN, needs)).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
			})
			if len(failed) > 0 {
				ui.Button(c, "全部重试失败项").Disabled(a.running).OnClick(func() { a.retryTasks(failed) })
			}
			ui.Button(c, "打开输出文件夹").OnClick(func() { a.openOutputFolder() })
		})
		if needs > 0 {
			ui.Text(c, verificationHint).FontSize(tok.caption).TextColor(tok.warning)
		}
		width, _ := c.Size()
		cols := int((width - 40) / 280)
		if cols < 1 {
			cols = 1
		}
		if cols > 3 {
			cols = 3
		}
		ui.Grid(c).Columns(cols).Gap(12).Children(func() {
			for _, card := range cards {
				a.progressCard(c, card)
			}
		})
	})
}

func (a *App) failAction(card ProgressCard) {
	switch card.Action {
	case "去重登":
		a.reloginCard(card)
	case "换图":
		a.replaceCardImage(card)
	default:
		a.retryTasks(card.Retry)
	}
}

func (a *App) reloginCard(card ProgressCard) {
	row := store.Browser{}
	if len(card.Retry) > 0 {
		row.ID = card.Retry[0].BrowserID
		row.Platform = card.Retry[0].Platform
	}
	for _, b := range a.configuredBrowsers() {
		if b.ID == row.ID && row.ID != 0 {
			row = b
			break
		}
	}
	logging.Log.Info("请在「%s」的网页里重新登录", accountLabel(row))
	go func(b store.Browser) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_ = a.ensureAccountPage(ctx, b)
	}(row)
}

func (a *App) replaceCardImage(card ProgressCard) {
	go func() {
		path, err := a.chooseReplacement()
		if err != nil || strings.TrimSpace(path) == "" {
			return
		}
		items := append([]tasks.Task(nil), card.Retry...)
		for i := range items {
			items[i].Images = []string{path}
		}
		run := func() { a.retryTasks(items) }
		if a.window != nil {
			a.window.Update(run)
			return
		}
		run()
	}()
}

func (a *App) chooseReplacement() (string, error) {
	if a != nil && a.pickFile != nil {
		return a.pickFile()
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:  a.window,
		Title:   "换一张图片",
		Filters: []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "webp", "gif"}}},
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func (a *App) progressCard(c *ui.Context, card ProgressCard) {
	tok := a.look
	box := ui.Column(c.Key("card:"+card.Key)).FillWidth().Gap(8).Padding(10).Radius(tok.radiusM).
		Background(tok.paper).Border(1, tok.border).AlignSelf(ui.Stretch).Transition(motionItem())
	if card.Status == "生成中" {
		accent := c.Theme().Accent
		reduce := c.Preferences().ReduceMotion
		box.DrawOver(func(p *ui.Painter, r ui.Rect) {
			paintGenBar(p, ui.Rect{X: r.X + 10, Y: r.Y + r.H - 16, W: r.W - 20, H: 6}, accent, reduce)
		})
	}
	box.Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			a.inputThumb(c, card)
			if len(card.Shots) > 0 {
				ui.Row(c).Gap(6).Grow(1).MinWidth(0).Children(func() {
					n := len(card.Shots)
					if n > 3 {
						n = 3
					}
					flown := map[int]bool{}
					for _, shot := range card.Shots[:n] {
						fly := shot.Task > 0 && !flown[shot.Task]
						if fly {
							flown[shot.Task] = true
						}
						a.resultThumb(c, shot, card.Status == "生成中", fly)
					}
				})
			}
			statusPill(c, card.Status)
		})
		ui.Text(c, card.Title).Bold().FontSize(tok.body).SingleLine().Tooltip(card.Title)
		if line := a.siteLine(card); line != "" {
			ui.Text(c, line).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(line)
		}
		if card.Status == "生成中" {
			ui.Box(c.Key("gen-slot")).FillWidth().Height(6)
		}
		if card.Reason != "" {
			color := tok.danger
			if card.Status == "待验证" {
				color = tok.warning
			}
			ui.Text(c.Key("reason"), card.Reason).FontSize(tok.caption).TextColor(color).SingleLine().Tooltip(card.Reason).Transition(motionReveal())
		}
		if card.Rewrite != "" {
			line := "已改写：" + card.Rewrite
			ui.Text(c.Key("rewrite"), line).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(line)
		}
		if (card.Status == "失败" || card.Status == "待验证") && len(card.Retry) > 0 || len(card.Shots) > 0 {
			ui.Row(c).Gap(8).Children(func() {
				if (card.Status == "失败" || card.Status == "待验证") && len(card.Retry) > 0 {
					label := card.Action
					if label == "" {
						label = "重试"
					}
					ui.Button(c.Key("retry"), label).Disabled(a.running).Transition(motionReveal()).OnClick(func() { a.failAction(card) })
				}
				if len(card.Shots) > 0 {
					path := card.Shots[0].Path
					ui.Button(c, "打开文件").OnClick(func() { a.openResult(path) })
				}
			})
		}
	})
}

func (a *App) inputThumb(c *ui.Context, card ProgressCard) {
	tok := a.look
	frame := ui.Box(c.Key("in")).Size(72, 72).Radius(tok.radiusS).Background(tok.sunken).Shrink(0)
	frame.Children(func() {
		if card.Source == "" {
			promptMark(c, tok)
			return
		}
		if bmp := a.cachedThumb(card.Source); bmp != nil {
			shot := ui.Image(c, bmp).Size(72, 72).Fit(ui.Cover).Radius(tok.radiusS)
			if card.Status == "生成中" {
				shot.DrawOver(func(p *ui.Painter, r ui.Rect) { paintShimmer(c, p, r) })
			}
		}
	})
}

func (a *App) resultThumb(c *ui.Context, shot CardShot, shimmer, fly bool) {
	tok := a.look
	tile := ui.Box(c.Key(shot.Path)).Size(72, 72).Radius(tok.radiusS).Background(tok.sunken).Shrink(0)
	if fly {
		tile.Transition(ui.ElementTransition{
			Animation: &motionEaseOut,
			Position:  true,
			Size:      true,
			Match:     jobFly{shot.Task},
		})
	} else {
		tile.Transition(motionItem())
	}
	tile.Children(func() {
		bmp := a.cachedThumb(shot.Path)
		if bmp != nil {
			img := ui.Image(c, bmp).Size(72, 72).Fit(ui.Cover).Radius(tok.radiusS).
				ContentTransition(ui.ContentTransition{Animation: &motionEaseOut}).
				Cursor(ui.CursorPointer).Label("预览")
			if shimmer {
				img.DrawOver(func(p *ui.Painter, r ui.Rect) { paintShimmer(c, p, r) })
			}
			if img.Clicked() {
				a.showPreview(shot.Path)
			}
		}
		if badge := shotBadge(shot.Platform, a.accountName(shot.BrowserID)); badge != "" {
			ui.Row(c).Label("产出"+badge).Absolute().Bottom(4).Left(4).Padding(1, 4).Radius(4).
				Background(tok.accent.Alpha(0.92)).Children(func() {
				ui.Text(c, badge).FontSize(10).TextColor(tok.onAccent).SingleLine()
			})
		}
	})
}

func promptMark(c *ui.Context, tok tokens) {
	ui.Box(c).Size(72, 72).Draw(func(p *ui.Painter, r ui.Rect) {
		ink := tok.muted
		for i := 0; i < 3; i++ {
			w := r.W * 0.46
			if i == 2 {
				w = r.W * 0.28
			}
			p.Fill(ui.Rect{X: r.X + 16, Y: r.Y + 20 + float32(i)*12, W: w, H: 3}, ink, 1.5)
		}
	})
}

func (a *App) siteLine(card ProgressCard) string {
	var parts []string
	for _, site := range card.Sites {
		line := site.Platform
		if site.BrowserID > 0 {
			if name := a.accountName(site.BrowserID); name != "" && name != line {
				line += " · " + name
			}
		}
		if line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, "、")
}

// paintShimmer 在生成中的缩略图上扫一条高光。只重绘，不重建视图。
func paintShimmer(c *ui.Context, p *ui.Painter, r ui.Rect) {
	if c.Preferences().ReduceMotion {
		return
	}
	p.AnimationFrame()
	phase := float32(p.Now().UnixMilli()%1200) / 1200
	w := r.W * 0.45
	x := r.X - w + (r.W+w)*phase
	p.Clip(r, 8, func() {
		p.Fill(ui.Rect{X: x, Y: r.Y, W: w, H: r.H}, ui.RGBA(255, 255, 255, 0.28), 0)
	})
}

func (a *App) showPreview(path string) {
	if path == "" {
		return
	}
	a.previewPath = path
	a.previewOn = true
	a.previewBmp = nil
	win := a.window
	go func() {
		img, err := images.Thumb(path, 960)
		apply := func() {
			if a.previewPath != path {
				return
			}
			if err == nil && img != nil {
				a.previewBmp = ui.NewBitmap(img)
			}
		}
		if win != nil {
			win.Update(apply)
			return
		}
		apply()
	}()
}

func (a *App) previewModal(c *ui.Context) {
	if !a.previewOn {
		return
	}
	tok := a.look
	ui.Modal(c, &a.previewOn, func() {
		ui.Column(c).MaxWidth(760).Gap(12).Children(func() {
			ui.Text(c, filepath.Base(a.previewPath)).FontSize(tok.title).Bold()
			if a.previewBmp != nil {
				ui.Image(c, a.previewBmp).MaxWidth(720).MaxHeight(480).Fit(ui.Contain).Radius(tok.radiusM)
			} else {
				ui.Spinner(c).Size(28, 28)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				path := a.previewPath
				ui.Button(c, "打开文件").OnClick(func() { a.openResult(path) })
				ui.Button(c, "关闭").OnClick(func() { a.previewOn = false })
			})
		})
	})
}

func (a *App) openResult(path string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if err := mygo.Shell.OpenPath(path); err != nil {
		a.outputNote = "无法打开文件"
		logging.Log.Errorf("打开文件失败: %v", err)
	}
}
