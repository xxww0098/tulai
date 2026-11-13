package ui

import (
	"context"
	"fmt"
	"os"
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
	composerW    float32 = 248
	homeSplitMin float32 = 810
	pagePadX     float32 = 8
	homeGap      float32 = 6
)

// columnHeight 扣掉全局顶栏和页面上下边距，让左右两栏自然贴合视口高度。
func columnHeight(winH float32) float32 {
	h := winH - topbarH - 12
	if h < 200 {
		return 200
	}
	return h
}

func homeSplit(w float32) bool { return w >= homeSplitMin }

func (a *App) pageInner(w float32) float32 {
	inner := w - a.sidebarWidth(w) - pagePadX*2
	if inner < 200 {
		return 200
	}
	return inner
}

func pageInnerDefault(w float32) float32 {
	side := sidebarExpandedW
	if w > 0 && w < sidebarFoldW {
		side = sidebarCollapsedW
	}
	inner := w - side - pagePadX*2
	if inner < 200 {
		return 200
	}
	return inner
}

func (a *App) homeStageW(w float32) float32 {
	if !homeSplit(w) {
		return a.pageInner(w)
	}
	stage := a.pageInner(w) - composerW - homeGap
	if stage < 220 {
		return 220
	}
	return stage
}

func homeStageWDefault(w float32) float32 {
	if !homeSplit(w) {
		return pageInnerDefault(w)
	}
	stage := pageInnerDefault(w) - composerW - homeGap
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

func (a *App) homePage(c *ui.Context) {
	a.workColumns(c, func() { a.genInput(c) })
}

// homeStage 上面是账号列表，出图结果撑满剩下的高度。
func (a *App) homeStage(c *ui.Context) {
	ui.Column(c.Key("home-stage")).Grow(1).MinWidth(0).MinHeight(0).Gap(6).Children(func() {
		a.platformStage(c)
	})
}

func (a *App) platformStage(c *ui.Context) {
	a.historyPanel(c)
	a.accountPanel(c)
	a.resultsPane(c)
}

func (a *App) platformPicker(c *ui.Context) {
	w, _ := c.Size()
	ui.Row(c.Key("platform-picker")).Label("服务商选择").Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "服务商").FontSize(a.look.body).Bold().Shrink(0)
		a.platformTabs(c)
		if w >= 1100 {
			ui.Text(c, "每次只向选中的服务商发送任务").FontSize(a.look.caption).TextColor(a.look.muted).SingleLine()
		}
	})
}

func (a *App) platformTabs(c *ui.Context) {
	doubaoN := a.runningOn("豆包")
	qianwenN := a.runningOn("千问")
	agyN := a.runningOn(agy.Platform)
	chatGPTN := a.runningOn("ChatGPT")
	if !a.stagePicked {
		a.stageTab = 0
		best := doubaoN
		if qianwenN > best {
			a.stageTab = 1
			best = qianwenN
		}
		if agyN > best {
			a.stageTab = 2
			best = agyN
		}
		if chatGPTN > best {
			a.stageTab = 3
		}
	}
	ui.Row(c.Key("platform-tabs")).Label("服务商选择").Gap(4).AlignItems(ui.Center).Children(func() {
		a.stageButton(c, 0, "豆包", doubaoN)
		a.stageButton(c, 1, "千问", qianwenN)
		a.stageButton(c, 2, agy.Platform, agyN)
		a.stageButton(c, 3, "ChatGPT", chatGPTN)
	})
}

func (a *App) stageButton(c *ui.Context, index int, label string, n int) {
	text := label
	if n > 0 {
		text = fmt.Sprintf("%s %d", label, n)
	}
	tok := a.look
	bg, fg, border := tok.sunken, tok.muted, tok.border
	on := a.stageTab == index
	if on {
		bg, fg, border = tok.accent.Alpha(0.14), tok.accent, tok.accent.Alpha(0.35)
	}
	b := ui.ButtonBase(c.Key("stage-"+label)).Height(22).Padding(0, 7).Gap(4).Radius(tok.radiusS).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Shrink(0).
		Background(bg).TextColor(fg).Border(1, border).Label(text).Tooltip("切换到 " + label)
	if !on && b.Hovered() {
		b.Background(tok.sunkenHi).TextColor(tok.text)
	}
	b.OnClick(func() {
		a.stageTab = index
		a.stagePicked = true
	})
	b.Children(func() {
		platformIcon(c, label, 12, a.look)
		t := ui.Text(c, text).FontSize(a.look.caption).SingleLine()
		if on {
			t.FontWeight(600)
		}
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
	case 3:
		return "ChatGPT"
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

func (a *App) stepCard(c *ui.Context, key string, state int) ui.Element {
	tok := a.look
	reveal := motionReveal()
	if a.tab == pageHome || a.tab == pageWash || a.tab == pageFission {
		reveal.Enter.Opacity = 0.9
	}
	card := ui.Column(c.Key(key)).FillWidth().Padding(8, 10).Gap(5).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border).Shadow(0, 1, 2, 0, tok.shadow).
		Transition(reveal)
	if !tok.dark {
		card.InsetShadow(0, 1, 0, 0, ui.RGBA(255, 255, 255, 0.9))
	}
	if state == stepCurrent {
		card.Border(1.5, tok.accent).Shadow(0, 8, 24, 0, tok.lift)
	}
	return card
}

const (
	washThumbSize    float32 = 50
	washThumbGap     float32 = 4
	washThumbCols            = 4
	washThumbMaxRows         = 2
)

// washThumbViewport 是原图网格的可视高度：窗口极矮时一整行，常规高度显示两整行，不把一行切在缩略图中间。
func washThumbViewport(n int, winH float32) float32 {
	if n <= 0 {
		return 0
	}
	rows := (n + washThumbCols - 1) / washThumbCols
	maxRows := washThumbMaxRows
	if winH < 640 {
		maxRows = 1
	}
	if rows > maxRows {
		rows = maxRows
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
	_, winH := c.Size()
	ui.Scroll(c.Key("wash-thumbs")).FillWidth().Height(washThumbViewport(n, winH)).Children(func() {
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
		label, bg, fg := "未洗", ui.RGBA(20, 28, 42, 0.78), ui.Hex("#FFFFFF")
		if si.Washed {
			label, bg = "已洗", a.look.success
		} else if si.Washing {
			label, bg = "洗图中", a.look.accent
		}
		badge := ui.Row(c).Label(label).Absolute().Left(4).Bottom(4).Padding(1, 4).Radius(4).
			Background(bg)
		if si.WashOutput != "" {
			badge.Tooltip("已保存：" + si.WashOutput)
		}
		badge.Children(func() {
			ui.Text(c, label).FontSize(10).TextColor(fg).SingleLine()
		})
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
	frame := ui.Box(c.Key(si.Path)).Size(30, 30).Radius(5).Background(a.look.sunken).
		Label(filepath.Base(si.Path)).Transition(motionItem())
	frame.Children(func() {
		if bmp := a.cachedThumb(si.Path); bmp != nil {
			ui.Image(c, bmp).Size(30, 30).Fit(ui.Cover).Radius(5)
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
	clear(a.gallerySelected)
	a.galleryDrag = nil
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
	washed, pending := a.washCounts()
	// 没有可用账号时关掉开始按钮。
	// 另一页在跑时引擎占着，这一页只能等。
	disabled := (!running && (plan.Action == StartNone || a.renameUpdateActive())) || (a.running && !running) ||
		(!running && a.washing() && (len(a.sources) == 0 || pending == 0)) ||
		(!running && a.fissioning() && len(a.sources) != 1) || len(a.browserPending) > 0
	if !running && plan.Action == StartGenerate && a.liveParallel() == 0 {
		disabled = true
	}
	label := "开始出图"
	if a.washing() {
		switch {
		case len(a.sources) > 0 && pending == 0:
			label = "已全部洗完"
		case washed > 0:
			label = "继续洗图"
		default:
			label = "开始洗图"
		}
	}
	if a.fissioning() {
		label = "开始裂变"
	}
	if running {
		label = "停止"
	}
	b := ui.ButtonBase(c.Key("run-go")).Label(label).Padding(6, 12).Gap(6).Radius(th.Radius).Shrink(0).
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

// stepLead 是出图步骤左侧的一行。跑起来写进度，没有能用的账号就写原因，避免和面板标题重复。
func (a *App) stepLead(board tasks.Board) string {
	if a != nil && a.running {
		if !a.ownsRun() {
			return pageLabel(a.runPage) + "正在运行，结束后再开始"
		}
		if notice := a.pausedAccountNotice(board); notice != "" {
			return notice
		}
		if a.washing() {
			return "正在洗图"
		}
		if a.fissioning() {
			return "正在生成裂变图片"
		}
		return "正在出图"
	}
	if a != nil && a.washing() && len(a.sources) == 0 {
		return "先添加要洗的图片"
	}
	if a != nil && a.fissioning() && len(a.sources) != 1 {
		return "先添加一张母图"
	}
	if a != nil && a.washing() {
		washed, pending := a.washCounts()
		switch {
		case washed > 0 && pending == 0:
			return fmt.Sprintf("已洗 %d 张，全部完成", washed)
		case washed > 0:
			return fmt.Sprintf("已洗 %d 张，继续处理剩余 %d 张", washed, pending)
		}
	}
	if a.liveParallel() == 0 {
		if len(board.Lanes) > 0 {
			return BlockedAccounts(board.Lanes)
		}
		if len(a.sources) == 0 && !a.hasTaskPrompt() {
			return "先添加图片，或写一条提示词"
		}
		return "还没有可用账号"
	}
	return RunSummary(len(a.sources), a.hasTaskPrompt())
}

// pausedAccountNotice 是这一页已经开跑，但一个号都出不了图。用和停跑时同一句「没有可用账号」。
// 还有任务在生成，或额度那句已经写在账号标题上时，这里不重复。
func (a *App) pausedAccountNotice(board tasks.Board) string {
	if a == nil || len(board.Lanes) == 0 {
		return ""
	}
	for _, ln := range board.Lanes {
		if ln.State == tasks.LaneRunning || ln.State == tasks.LaneIdle || ln.State == tasks.LaneGap {
			return ""
		}
	}
	for _, task := range a.queueItems() {
		switch task.Status {
		case tasks.StatusRunning, tasks.StatusRetry, tasks.StatusAwaitUser:
			return ""
		}
	}
	if a.agyPauseLine(time.Now()) != "" {
		return ""
	}
	notice := BlockedAccounts(board.Lanes)
	if !strings.HasPrefix(notice, "没有可用账号") {
		return ""
	}
	return notice
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

func (a *App) failAction(card ProgressCard) {
	switch card.Action {
	case "去重登":
		a.reloginCard(card)
	case "换图":
		a.replaceCardImage(card)
	case "查看待核对":
		for _, task := range card.Retry {
			if _, path, ok := strings.Cut(task.Error, "未匹配结果保存在 "); ok {
				path, _, _ = strings.Cut(path, "\n")
				path = strings.TrimSpace(path)
				if info, err := os.Stat(path); err == nil && info.IsDir() {
					if err := mygo.Shell.OpenPath(path); err == nil {
						return
					}
				}
				a.outputNote = "无法打开待核对文件夹"
				logging.Log.Errorf("无法打开待核对文件夹: %s", path)
				return
			}
		}
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
		ui.Column(c).MaxWidth(760 / interfaceZoom).Gap(12).Children(func() {
			ui.Text(c, filepath.Base(a.previewPath)).FontSize(tok.title).Bold()
			if a.previewBmp != nil {
				ui.Image(c, a.previewBmp).MaxWidth(720 / interfaceZoom).MaxHeight(480 / interfaceZoom).Fit(ui.Contain).Radius(tok.radiusM)
			} else {
				ui.Spinner(c).Size(28, 28)
			}
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
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
