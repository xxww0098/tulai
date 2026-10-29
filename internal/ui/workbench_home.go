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

const (
	railWidth    float32 = 56
	composerW    float32 = 360
	homeSplitMin float32 = 900
	pagePadX     float32 = 16
	homeGap      float32 = 12
)

func homeSplit(w float32) bool { return w >= homeSplitMin }

func pageInner(w float32) float32 {
	inner := w - railWidth - pagePadX*2
	if inner < 200 {
		return 200
	}
	return inner
}

func homeStageW(w float32) float32 {
	if !homeSplit(w) {
		return pageInner(w)
	}
	stage := pageInner(w) - composerW - homeGap
	if stage < 220 {
		return 220
	}
	return stage
}

func composerInner(w float32) float32 {
	if !homeSplit(w) {
		return pageInner(w)
	}
	return composerW
}

func fitCols(inner, min float32, max int) int {
	cols := int(inner / min)
	if cols < 1 {
		cols = 1
	}
	if max > 0 && cols > max {
		cols = max
	}
	return cols
}

// gridCols 按右栏实际宽度分列。张数更少时不留空列；最后一行只剩一张时少排一列，避免孤卡。
func gridCols(windowW, minCol float32, n, max int) int {
	cols := fitCols(homeStageW(windowW)-24, minCol, max)
	if n > 0 && cols > n {
		cols = n
	}
	if cols > 2 && n > cols && n%cols == 1 {
		cols--
	}
	if cols < 1 {
		return 1
	}
	return cols
}

func (a *App) navRail(c *ui.Context) {
	tok := a.look
	ui.Column(c.Key("nav-rail")).Width(railWidth).FillHeight().Padding(12, 0, 10, 0).Gap(4).
		AlignItems(ui.Center).Background(tok.card).
		BorderWidth(0, 1, 0, 0).BorderColor(tok.border).Children(func() {
		logo := ui.Column(c.Key("logo")).Size(36, 36).Radius(10).Center().
			Label("tulai").Cursor(ui.CursorPointer)
		if logo.Hovered() {
			logo.Background(tok.sunken)
		}
		if logo.Clicked() && !a.running {
			a.tab = pageHome
		}
		logo.Children(func() {
			ui.Icon(c, iconLogo).FontSize(22).TextColor(tok.text)
		})
		railTip(c, logo, "tulai")
		ui.Box(c).Height(8)
		a.pageTabs(c)
		ui.Spacer(c)
		ui.Box(c).Width(18).Height(1).Margin(6, 0).Background(tok.border)
		a.railButton(c, "nav-wizard", "设置向导", iconCompass, a.running, false, func() { a.openWizard() })
		a.railButton(c, "nav-update", "检查更新", iconRefresh, a.updateBusy, a.updateNotice != "", func() { a.checkForUpdates() })
	})
}

func (a *App) pageTabs(c *ui.Context) {
	type item struct {
		label string
		icon  *ui.SVG
	}
	items := []item{
		{"出图", iconImages},
		{"浏览器", iconGlobe},
		{"提示词", iconPen},
		{"设置", iconSettings},
	}
	tabs := ui.TabsBase(c.Key("main-tabs"), &a.tab, len(items))
	tabs.List.Column().Gap(4).AlignItems(ui.Center).Children(func() {
		for i, it := range items {
			tab := tabs.Tab(i).Size(36, 36).Radius(10).Center().Label(it.label).Disabled(a.running)
			if !a.running {
				tab.Cursor(ui.CursorPointer)
			}
			a.paintNavIcon(c, tab, it.icon, i == a.tab, false)
			railTip(c, tab, it.label)
		}
	})
}

func (a *App) railButton(c *ui.Context, key, label string, icon *ui.SVG, disabled, dot bool, click func()) {
	b := ui.ButtonBase(c.Key(key)).Size(36, 36).Radius(10).Center().Label(label).Disabled(disabled)
	if !disabled {
		b.Cursor(ui.CursorPointer).OnClick(click)
	}
	a.paintNavIcon(c, b, icon, false, dot)
	railTip(c, b, label)
}

func (a *App) paintNavIcon(c *ui.Context, b ui.Element, icon *ui.SVG, on, dot bool) {
	tok := a.look
	bg, fg := tok.card, tok.muted
	switch {
	case on:
		bg, fg = tok.accent.Alpha(0.14), tok.accent
	case !b.IsDisabled() && b.Pressed():
		bg, fg = tok.sunken, tok.text
	case !b.IsDisabled() && b.Hovered():
		bg, fg = tok.sunkenHi, tok.text
	}
	b.Background(b.AnimateColor("bg", bg, motionEaseFast))
	b.TextColor(b.AnimateColor("ink", fg, motionEaseFast))
	if !b.IsDisabled() {
		hoverPressPose(b)
	}
	b.Children(func() {
		ui.Icon(c, icon).FontSize(18)
		if dot {
			ui.Box(c.Key("dot")).Size(6, 6).Radius(3).Background(tok.danger).Absolute().Top(5).Right(5)
		}
	})
}

// railTip 把提示放在图标右侧。窄栏上居中的提示会伸出窗口左边。
func railTip(c *ui.Context, anchor ui.Element, text string) {
	anchor.Description(text)
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		th := c.Theme()
		tip.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 8).
			Padding(4, 8).Radius(6).Background(th.Inverse).TextColor(th.InverseText).
			Shadow(0, 4, 14, 0, ui.RGBA(16, 32, 64, 0.18)).Transition(motionPopup())
		tip.Children(func() {
			ui.Text(c, text).FontSize(12).SingleLine()
		})
	})
}

func (a *App) homePage(c *ui.Context) {
	flow := a.flowNow()
	w, _ := c.Size()
	if homeSplit(w) {
		ui.Column(c).Grow(1).MinHeight(0).Gap(10).Children(func() {
			a.loginBanner(c)
			ui.Row(c).Grow(1).MinHeight(0).Gap(homeGap).AlignItems(ui.Stretch).Children(func() {
				a.homeComposer(c, flow)
				a.homeStage(c, flow)
			})
		})
	} else {
		ui.Scroll(c.Key("home")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Padding(4, 0, 8, 0).Children(func() {
				a.loginBanner(c)
				a.stepImages(c, flow.Images)
				a.stepPrompt(c, flow.Prompt)
				a.stepRun(c, flow.Run)
				a.platformTabs(c)
				if flow.Run.State == stepCurrent {
					a.accountPanel(c, false)
				} else {
					a.accountPanel(c, true)
				}
				a.progressPane(c)
				a.runLog(c)
				ui.Collapsible(c, "更多设置", &a.advancedOpen, func() {
					a.advancedFields(c)
				})
			})
		})
	}
	a.previewModal(c)
}

func (a *App) homeComposer(c *ui.Context, flow flowLayout) {
	ui.Scroll(c.Key("home-side")).Width(composerW).Shrink(0).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(10).Padding(0, 0, 8, 0).Children(func() {
			a.stepImages(c, flow.Images)
			a.stepPrompt(c, flow.Prompt)
			a.stepRun(c, flow.Run)
			ui.Collapsible(c, "更多设置", &a.advancedOpen, func() {
				a.advancedFields(c)
			})
		})
	})
}

func (a *App) homeStage(c *ui.Context, flow flowLayout) {
	_, winH := c.Size()
	ui.Scroll(c.Key("home-stage")).Grow(1).MinWidth(0).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().MinHeight(winH-48).Gap(10).Padding(0, 0, 8, 0).Children(func() {
			a.platformStage(c, flow)
		})
	})
}

func (a *App) platformStage(c *ui.Context, flow flowLayout) {
	a.platformTabs(c)
	if flow.Run.State == stepCurrent {
		a.accountPanel(c, false)
	} else {
		a.accountPanel(c, true)
	}
	if len(a.platformCards()) == 0 {
		a.stageEmpty(c)
	} else {
		a.progressPane(c)
	}
	a.runLog(c)
}

func (a *App) platformTabs(c *ui.Context) {
	doubaoN := a.runningOn("豆包")
	qianwenN := a.runningOn("千问")
	if !a.stagePicked {
		if qianwenN > doubaoN {
			a.stageTab = 1
		} else {
			a.stageTab = 0
		}
	}
	ui.Row(c.Key("platform-tabs")).Gap(6).AlignItems(ui.Center).Children(func() {
		a.stageButton(c, 0, "豆包", doubaoN)
		a.stageButton(c, 1, "千问", qianwenN)
	})
}

func (a *App) stageButton(c *ui.Context, index int, label string, n int) {
	text := label
	if n > 0 {
		text = fmt.Sprintf("%s %d", label, n)
	}
	b := ui.Button(c, text)
	if a.stageTab == index {
		b.Border(1, c.Theme().Accent).TextColor(c.Theme().Accent)
	}
	b.OnClick(func() {
		a.stageTab = index
		a.stagePicked = true
	})
}

func (a *App) stagePlatform() string {
	if a != nil && a.stageTab == 1 {
		return "千问"
	}
	return "豆包"
}

func (a *App) runningOn(platform string) int {
	n := 0
	for _, ln := range a.accountBoard(time.Now()).Lanes {
		if ln.Platform == platform && ln.State == tasks.LaneRunning {
			n++
		}
	}
	return n
}

func (a *App) platformCards() []ProgressCard {
	return ProgressCards(tasksForPlatform(a.queueItems(), a.stagePlatform()))
}

func tasksForPlatform(items []tasks.Task, platform string) []tasks.Task {
	if platform == "" {
		return items
	}
	var out []tasks.Task
	for _, task := range items {
		if task.Platform == platform {
			out = append(out, task)
		}
	}
	return out
}

func (a *App) runLog(c *ui.Context) {
	tok := a.look
	ui.Column(c.Key("run-log")).FillWidth().Gap(4).Padding(8, 10).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "运行日志").FontSize(tok.body).Bold().Grow(1)
			ui.Button(c, "打开日志文件").OnClick(func() { a.openLogFile() })
		})
		notes := logging.VisibleTail(logging.Log.Entries(), 6)
		if len(notes) == 0 {
			ui.Text(c, "每个任务的开始、提交和结束会写在这里").FontSize(tok.caption).TextColor(tok.muted)
			return
		}
		for _, line := range notes {
			text := line.Text
			ui.Text(c, text).FontSize(tok.caption).SingleLine().Tooltip(text)
		}
	})
}

func (a *App) openLogFile() {
	path := logging.Path()
	if path == "" {
		return
	}
	_ = mygo.Shell.OpenPath(path)
}

func (a *App) stageEmpty(c *ui.Context) {
	tok := a.look
	ui.Column(c.Key("stage-empty")).Grow(1).MinHeight(140).FillWidth().Center().Gap(8).Children(func() {
		ui.Icon(c, iconImages).FontSize(22).TextColor(tok.muted.Alpha(0.55))
		ui.Text(c, "出图结果会出现在这里").FontSize(tok.caption).TextColor(tok.muted)
	})
}

func (a *App) flowNow() flowLayout {
	return LayoutFlow(len(a.sources) > 0, len(a.taskPromptList()) > 0, a.skipImages, len(a.queueItems()) > 0, a.flowPin)
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
	cols := fitCols(composerInner(width)-36, 148, 4)
	if cols < 2 {
		cols = 2
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
		ui.Text(c, title).FontSize(tok.body).Bold().MinWidth(0).SingleLine()
		if head.Clicked() {
			a.flowPin = 1
		}
		ui.Row(c).Grow(1).MinWidth(0)
		if len(a.sources) > 0 {
			ui.Button(c, "添加图片").Disabled(a.running).OnClick(func() { a.addImages() })
			ui.Button(c, "清空").Disabled(a.running).OnClick(func() { a.clearSources() })
			return
		}
		ui.Button(c, "添加图片").Disabled(a.running).OnClick(func() { a.addImages() })
		link := ui.Text(c, "只用提示词").FontSize(tok.body).TextColor(tok.accent)
		if link.Clicked() && !a.running {
			a.skipImages = true
			a.flowPin = 2
		}
	})
	if len(a.sources) == 0 {
		return
	}
	ui.ScrollHorizontal(c).FillWidth().Height(40).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			for i := range a.sources {
				a.miniThumb(c, i)
			}
		})
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
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "② 写提示词").FontSize(tok.title).Bold().Grow(1).MinWidth(0)
		ui.Button(c, "编辑提示词").Disabled(a.running).OnClick(func() { a.tab = pagePrompts })
	})
	motionSelect(c.Key("prompt-template"), &a.promptChoice, labels).FillWidth().Disabled(a.running).OnChange(func() {
		a.applyPromptChoice()
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
		if !a.running && (plan.Action == StartWizard || a.loggedOutCallout()) {
			a.accountCallout(c)
		}
		board := filterBoard(a.accountBoard(c.Now()), a.stagePlatform())
		ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(2).Clip().Children(func() {
				summary := a.stepLead(board)
				ui.Text(c, summary).FontSize(tok.body).MinWidth(0).SingleLine().Tooltip(summary)
				if note := a.runNote(); note != "" {
					ui.Text(c.Key("prog"), note).FontSize(tok.caption).TextColor(tok.muted).MinWidth(0).SingleLine().Tooltip(note)
				}
				a.outputLink(c, tok)
			})
			a.runButton(c, plan)
		})
	})
}

func (a *App) outputLink(c *ui.Context, tok tokens) {
	path := a.resolvedOutputDir()
	line := "保存到 " + path
	row := ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0).Cursor(ui.CursorPointer)
	if row.Clicked() {
		a.openOutputFolder()
	}
	row.Children(func() {
		paintFolder(c, tok.muted)
		ui.Text(c, line).FontSize(tok.caption).TextColor(tok.muted).MinWidth(0).SingleLine().Tooltip(line)
	})
}

func paintFolder(c *ui.Context, ink ui.Color) {
	ui.Box(c).Size(14, 12).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * 0.52, H: 3.2}, ink, 1)
		p.Fill(ui.Rect{X: r.X, Y: r.Y + 2.2, W: r.W, H: r.H - 2.2}, ink, 1.6)
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

// loggedOutCallout 是这一页已经有账号，但没有一个是登录的。
// 开始按钮保持关掉，另外留一句去登录。已登录只是额度满了，不说成没登录。
func (a *App) loggedOutCallout() bool {
	if a == nil || a.workbenchPlan().Action != StartNone {
		return false
	}
	in := a.workbenchInput()
	if !in.HasImages && !in.HasPrompts {
		return false
	}
	stage := a.stagePlatform()
	any := false
	for _, account := range in.Accounts {
		if account.Platform != stage || account.Extension != in.ExtensionMode {
			continue
		}
		if account.LoggedIn {
			return false
		}
		any = true
	}
	return any
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
	if a != nil && a.running {
		return "正在出图"
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
	return RunSummary(len(a.sources), len(a.taskPromptList()) > 0)
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
	if id > 0 {
		a.holdLogin(id)
	}
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
	cards := a.platformCards()
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
		})
		if needs > 0 {
			ui.Text(c, verificationHint).FontSize(tok.caption).TextColor(tok.warning)
		}
		ui.Grid(c).Columns(a.progressCols(c)).FillWidth().Gap(8).AlignItems(ui.Stretch).Children(func() {
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
	id := int64(0)
	if len(card.Retry) > 0 {
		id = card.Retry[0].BrowserID
	}
	row := a.showBrowserAccount(id)
	if row.Platform == "" && len(card.Retry) > 0 {
		row.Platform = card.Retry[0].Platform
	}
	logging.Log.Info("请在「%s」的网页里重新登录", accountLabel(row))
	go func(b store.Browser) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_ = a.ensureAccountPage(ctx, b)
	}(row)
}

// showBrowserAccount 切到浏览器页并选中这个账号，方便在打开的网页里登录。
func (a *App) showBrowserAccount(id int64) store.Browser {
	if id > 0 {
		a.holdLogin(id)
	}
	row := store.Browser{ID: id}
	for _, b := range a.configuredBrowsers() {
		if id != 0 && b.ID == id {
			row = b
			break
		}
	}
	if id != 0 {
		a.tab = pageBrowser
	}
	a.refreshBrowsers()
	for i, b := range a.browserRows {
		if id != 0 && b.ID == id {
			a.browserSel = i
			row = b
			break
		}
	}
	return row
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

// progressCols 按窗口里出图进度实际能用的宽度分列，避免卡片比格子宽、叠出面板。
func (a *App) progressCols(c *ui.Context) int {
	width, _ := c.Size()
	return gridCols(width, 230, len(a.platformCards()), 3)
}

func (a *App) progressCard(c *ui.Context, card ProgressCard) {
	tok := a.look
	box := ui.Column(c.Key("card:"+card.Key)).MinWidth(0).Gap(6).Padding(8).Radius(tok.radiusM).
		Background(tok.paper).Border(1, tok.border).Clip().Label("结果卡" + card.Title).Transition(motionItem())
	if card.Status == "生成中" {
		accent := c.Theme().Accent
		reduce := c.Preferences().ReduceMotion
		box.DrawOver(func(p *ui.Painter, r ui.Rect) {
			paintGenBar(p, ui.Rect{X: r.X + 10, Y: r.Y + r.H - 16, W: r.W - 20, H: 6}, accent, reduce)
		})
	}
	box.Children(func() {
		ui.Row(c).FillWidth().MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
			a.inputThumb(c, card)
			if len(card.Shots) > 0 {
				shown, extra := visibleShots(len(card.Shots))
				ui.Row(c).Gap(4).Grow(1).MinWidth(0).Wrap().Children(func() {
					flown := map[int]bool{}
					for _, shot := range card.Shots[:shown] {
						fly := shot.Task > 0 && !flown[shot.Task]
						if fly {
							flown[shot.Task] = true
						}
						a.resultThumb(c, shot, card.Status == "生成中", fly)
					}
					if extra > 0 {
						ui.Box(c.Key("more")).Size(56, 56).Radius(tok.radiusS).Background(tok.sunken).Center().Children(func() {
							ui.Text(c, fmt.Sprintf("+%d", extra)).FontSize(12).TextColor(tok.muted)
						})
					}
				})
			}
			statusPill(c, card.Status)
		})
		ui.Text(c, card.Title).Bold().FontSize(tok.body).SingleLine().MinWidth(0).Tooltip(card.Title)
		if line := a.siteLine(card); line != "" {
			ui.Text(c, line).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0).Tooltip(line)
		}
		if card.Moved != "" {
			ui.Text(c.Key("moved"), card.Moved).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0).Tooltip(card.Moved)
		}
		if card.MoveWhy != "" {
			ui.Text(c.Key("movewhy"), card.MoveWhy).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0).Tooltip(card.MoveWhy)
		}
		switch card.Status {
		case "生成中":
			ui.Box(c.Key("gen-slot")).FillWidth().Height(6)
		case "排队中":
			ui.Box(c.Key("wait-slot")).FillWidth().Height(6).Radius(3).Background(tok.sunken)
		}
		if card.Reason != "" {
			color := tok.danger
			if card.Status == "待验证" || card.Status == "需要人工验证" {
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
	frame := ui.Box(c.Key("in")).Size(56, 56).Radius(tok.radiusS).Background(tok.sunken).Shrink(0)
	frame.Children(func() {
		if card.Source == "" {
			promptMarkBox(c, tok, 56)
			return
		}
		if bmp := a.cachedThumb(card.Source); bmp != nil {
			shot := ui.Image(c, bmp).Size(56, 56).Fit(ui.Cover).Radius(tok.radiusS)
			if card.Status == "生成中" {
				shot.DrawOver(func(p *ui.Painter, r ui.Rect) { paintShimmer(c, p, r) })
			}
		}
	})
}

// visibleShots 把结果缩略图都摆出来。太多时留下一格显示还剩几张。
func visibleShots(n int) (shown, extra int) {
	if n <= 6 {
		return n, 0
	}
	return 5, n - 5
}

func (a *App) resultThumb(c *ui.Context, shot CardShot, shimmer, fly bool) {
	tok := a.look
	tile := ui.Box(c.Key(shot.Path)).Size(56, 56).Radius(tok.radiusS).Background(tok.sunken).Shrink(0).Label("结果图")
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
			img := ui.Image(c, bmp).Size(56, 56).Fit(ui.Cover).Radius(tok.radiusS).
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

func promptMark(c *ui.Context, tok tokens) { promptMarkBox(c, tok, 72) }

func promptMarkBox(c *ui.Context, tok tokens, size float32) {
	ui.Box(c).Size(size, size).Draw(func(p *ui.Painter, r ui.Rect) {
		ink := tok.muted
		for i := 0; i < 3; i++ {
			w := r.W * 0.46
			if i == 2 {
				w = r.W * 0.28
			}
			p.Fill(ui.Rect{X: r.X + r.W*0.22, Y: r.Y + r.H*0.28 + float32(i)*r.H*0.16, W: w, H: 2}, ink, 1)
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
