package ui

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
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
			Label("回到生图").Cursor(ui.CursorPointer)
		if logo.Hovered() {
			logo.Background(tok.sunken)
		}
		if logo.Clicked() && !a.running {
			a.tab = pageHome
		}
		logo.Children(func() {
			ui.Icon(c, iconLogo).FontSize(22).TextColor(tok.text)
		})
		railTip(c, logo, "回到生图")
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
		{"生图", iconImages},
		{"洗图", iconWash},
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
	a.workColumns(c, func() { a.genInput(c) })
}

// homeStage 上面是平台和账号，出图结果撑满剩下的高度并自己滚动。
func (a *App) homeStage(c *ui.Context) {
	_, winH := c.Size()
	ui.Column(c.Key("home-stage")).Grow(1).MinWidth(0).Height(winH-48).Gap(10).Padding(0, 0, 8, 0).Children(func() {
		a.platformTabs(c)
		a.accountPanel(c)
		if len(a.platformCards()) == 0 {
			a.stageEmpty(c)
			return
		}
		ui.Scroll(c.Key("home-stage-results")).FillWidth().Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(10).Children(func() {
				a.progressPane(c)
			})
		})
	})
}

func (a *App) platformStage(c *ui.Context) {
	a.platformTabs(c)
	a.accountPanel(c)
	if len(a.platformCards()) == 0 {
		a.stageEmpty(c)
	} else {
		a.progressPane(c)
	}
}

func (a *App) platformTabs(c *ui.Context) {
	doubaoN := a.runningOn("豆包")
	qianwenN := a.runningOn("千问")
	agyN := a.runningOn(agy.Platform)
	if !a.stagePicked {
		a.stageTab = 0
		best := doubaoN
		if qianwenN > best {
			a.stageTab = 1
			best = qianwenN
		}
		if agyN > best {
			a.stageTab = 2
		}
	}
	ui.Row(c.Key("platform-tabs")).Gap(6).AlignItems(ui.Center).Children(func() {
		a.stageButton(c, 0, "豆包", doubaoN)
		a.stageButton(c, 1, "千问", qianwenN)
		a.stageButton(c, 2, agy.Platform, agyN)
	})
}

func (a *App) stageButton(c *ui.Context, index int, label string, n int) {
	text := label
	if n > 0 {
		text = fmt.Sprintf("%s %d", label, n)
	}
	th := c.Theme()
	bg, fg, border := th.Surface, th.Text, th.Border
	on := a.stageTab == index
	if on {
		bg, fg, border = th.Accent.Alpha(0.12), th.Accent, th.Accent
	}
	b := ui.ButtonBase(c.Key("stage-"+label)).Padding(4, 10).Gap(6).Radius(8).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Background(bg).TextColor(fg).Border(1, border)
	if !on && b.Hovered() {
		b.Background(th.SurfaceHover)
	}
	b.OnClick(func() {
		a.stageTab = index
		a.stagePicked = true
	})
	b.Children(func() {
		platformIcon(c, label, 16, a.look)
		ui.Text(c, text).FontSize(a.look.body).SingleLine()
	})
}

func (a *App) stagePlatform() string {
	if a == nil {
		return "豆包"
	}
	switch a.stageTab {
	case 1:
		return "千问"
	case 2:
		return agy.Platform
	default:
		return "豆包"
	}
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

// runLog 在左栏里撑满剩下的高度，日志在卡片里滚动，停在底部时跟着最新一行。
func (a *App) runLog(c *ui.Context, fill bool) {
	tok := a.look
	card := ui.Column(c.Key("run-log")).Label("运行日志区").FillWidth().Gap(4).Padding(8, 10).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border)
	limit := 6
	if fill {
		card.Grow(1).Shrink(0).MinHeight(140)
		limit = 300
	}
	card.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "运行日志").FontSize(tok.body).Bold().Grow(1)
			ui.Button(c, "打开日志文件").OnClick(func() { a.openLogFile() })
		})
		notes := logging.VisibleTail(logging.Log.Entries(), limit)
		if len(notes) == 0 {
			ui.Text(c, "每个任务的开始、提交和结束会写在这里").FontSize(tok.caption).TextColor(tok.muted)
			return
		}
		lines := func() {
			for _, line := range notes {
				text := line.Text
				ui.Text(c, text).FontSize(tok.caption).SingleLine().Tooltip(text)
			}
		}
		if !fill {
			lines()
			return
		}
		if a.logScroll.Y >= a.logScroll.MaxY {
			a.logScroll.Y = math.MaxFloat32
		}
		ui.Scroll(c.Key("run-log-lines")).FillWidth().Grow(1).MinHeight(0).TrackScroll(&a.logScroll).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(lines)
		})
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

const (
	washThumbSize    float32 = 68
	washThumbGap     float32 = 8
	washThumbCols            = 4
	washThumbMaxRows         = 2
)

// washThumbViewport 是原图网格的可视高度：最多两整行，不把一行切在缩略图中间。
func washThumbViewport(n int) float32 {
	if n <= 0 {
		return 0
	}
	rows := (n + washThumbCols - 1) / washThumbCols
	if rows > washThumbMaxRows {
		rows = washThumbMaxRows
	}
	return float32(rows)*washThumbSize + float32(rows-1)*washThumbGap
}

func (a *App) washSourceGrid(c *ui.Context) {
	n := len(a.sources)
	if n == 0 {
		return
	}
	tracks := make([]ui.Track, washThumbCols)
	for i := range tracks {
		tracks[i] = ui.Fixed(washThumbSize)
	}
	ui.Scroll(c.Key("wash-thumbs")).FillWidth().Height(washThumbViewport(n)).Children(func() {
		ui.Grid(c).ColumnTracks(tracks...).Gap(washThumbGap).JustifyItems(ui.Start).AlignItems(ui.Start).Children(func() {
			for i := range a.sources {
				a.washThumb(c, i)
			}
		})
	})
}

func (a *App) washThumb(c *ui.Context, i int) {
	si := a.sources[i]
	name := filepath.Base(si.Path)
	frame := ui.Box(c.Key(si.Path)).Size(washThumbSize, washThumbSize).Radius(a.look.radiusS).
		Background(a.look.sunken).Label(name).Tooltip(si.Path)
	frame.Children(func() {
		if bmp := a.cachedThumb(si.Path); bmp != nil {
			ui.Image(c, bmp).Size(washThumbSize, washThumbSize).Fit(ui.Cover).Radius(a.look.radiusS)
		}
		a.thumbRemove(c, frame, i)
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

func (a *App) miniThumb(c *ui.Context, i int) {
	si := a.sources[i]
	frame := ui.Box(c.Key(si.Path)).Size(36, 36).Radius(6).Background(a.look.sunken).
		Label(filepath.Base(si.Path)).Transition(motionItem())
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
	running := a.running && a.ownsRun()
	// 一个账号都没有时，按钮仍打开设置向导。有账号但今天都不能用，则关掉。
	// 另一页在跑时引擎占着，这一页只能等。
	disabled := (!running && plan.Action == StartNone) || (a.running && !running) ||
		(!running && a.washing() && len(a.sources) == 0)
	if !running && plan.Action == StartGenerate && a.liveParallel() == 0 {
		disabled = true
	}
	label := "开始出图"
	if a.washing() {
		label = "开始洗图"
	}
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
		if account.Platform != stage {
			continue
		}
		if account.Platform != agy.Platform && account.Extension != in.ExtensionMode {
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
		text := "还没有可用账号。先去设置登录，再开始出图。"
		button := "去设置"
		if a.stagePlatform() == agy.Platform {
			text = "还没有可用账号。在右侧点「+ 添加账号」登录。"
			button = "+ 添加账号"
		}
		ui.Text(c, text).FontSize(tok.body).Grow(1).MinWidth(0)
		ui.Button(c, button).OnClick(func() {
			if a.stagePlatform() == agy.Platform {
				a.addAgyAccount()
				return
			}
			a.openWizard()
		})
	})
}

// stepLead 是出图步骤左侧的一行。跑起来写进度，没有能用的账号就写原因，避免和面板标题重复。
func (a *App) stepLead(board tasks.Board) string {
	if a != nil && a.running {
		if !a.ownsRun() {
			return pageLabel(a.runPage) + "正在运行，结束后再开始"
		}
		if a.washing() {
			return "正在洗图"
		}
		return "正在出图"
	}
	if a != nil && a.washing() && len(a.sources) == 0 {
		return "先添加要洗的图片"
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
		a.siteRow(c, card)
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

func (a *App) siteRow(c *ui.Context, card ProgressCard) {
	if len(card.Sites) == 0 {
		return
	}
	tok := a.look
	ui.Row(c).Gap(10).AlignItems(ui.Center).MinWidth(0).Children(func() {
		for i, site := range card.Sites {
			line := site.Platform
			if site.BrowserID > 0 {
				if name := a.accountName(site.BrowserID); name != "" && name != line {
					line += " · " + name
				}
			}
			if line == "" {
				continue
			}
			ui.Row(c.Key(i)).Gap(4).AlignItems(ui.Center).Shrink(1).MinWidth(0).Children(func() {
				platformIcon(c, site.Platform, 16, tok)
				ui.Text(c, line).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0).Tooltip(line)
			})
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
