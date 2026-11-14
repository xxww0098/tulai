package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/tasks"
)

const (
	// 图片保持合适的最小物理尺寸，避免缩得过小影响辨识。
	resultTileMin = 108 / interfaceZoom
	resultTileMax = 152 / interfaceZoom
	resultTileGap = 6
)

// resultTile 是结果格里的一张图，或还没有文件时的一条状态。
type resultTile struct {
	key  string
	task tasks.Task
	path string
	fly  bool
}

// resultRow 是虚拟列表的一行：账号分组标题，或一行缩略图。
type resultRow struct {
	key     string
	header  bool
	title   string
	summary string
	tiles   []resultTile
}

func (a *App) resultList() *ui.ListState {
	if a.fissioning() {
		return &a.fissionList
	}
	if a.washing() {
		return &a.washList
	}
	return &a.genList
}

// resultsPane 撑满账号表下面的高度。缩略图按工作账号分组，没工作的账号和未派发任务不显示。
func (a *App) resultsPane(c *ui.Context) {
	tok := a.look
	w, _ := c.Size()
	items := tasksForPlatform(a.queueItems(), a.stagePlatform())
	rows, tileW := a.buildResultRowsAt(w, c.Now())
	a.resultsClock(c, items)
	pane := ui.Column(c.Key("results")).Label("出图结果区").FillWidth().Padding(8, 10).Gap(5).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border)
	if homeSplit(w) {
		pane.Grow(1).MinHeight(0)
	}
	pane.Children(func() {
		a.resultsHead(c)
		if TallyTasks(items).Needs > 0 {
			ui.Text(c, verificationHint).FontSize(tok.caption).TextColor(tok.warning)
		}
		if len(rows) == 0 {
			a.resultsEmpty(c)
			return
		}
		st := a.resultList()
		st.Key = func(i int) any {
			if i < 0 || i >= len(rows) {
				return i
			}
			return rows[i].key
		}
		st.Header = func(i int) bool {
			return i >= 0 && i < len(rows) && rows[i].header
		}
		list := ui.List(c.Key("result-list"), st, len(rows), func(i int) {
			if i < 0 || i >= len(rows) {
				return
			}
			row := rows[i]
			if row.header {
				a.resultHeader(c, row)
				return
			}
			a.resultStrip(c, row, tileW)
		}).FillWidth()
		if homeSplit(w) {
			list.Grow(1).MinHeight(0)
		} else {
			list.Height(320)
		}
	})
}

func (a *App) resultsClock(c *ui.Context, items []tasks.Task) {
	now := c.Now()
	var wait time.Duration
	for _, task := range items {
		if !task.Worked() || !isTaskActiveStatus(task.Status) || task.StartedAt.IsZero() {
			continue
		}
		elapsed := now.Sub(task.StartedAt)
		if elapsed < 0 {
			elapsed = 0
		}
		left := time.Second - elapsed%time.Second
		if left <= 0 {
			left = time.Millisecond
		}
		if wait == 0 || left < wait {
			wait = left
		}
	}
	if wait > 0 {
		c.After(wait)
	}
}

func (a *App) resultsHead(c *ui.Context) {
	tok := a.look
	items := tasksForPlatform(a.queueItems(), a.stagePlatform())
	failed := FailedTasks(items)
	tally := TallyTasks(items)
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "出图进度").FontSize(tok.title).Bold().Shrink(0)
				ui.Text(c, "当前服务商："+a.stagePlatform()).FontSize(tok.caption).TextColor(tok.muted).MinWidth(0).SingleLine()
			})
			if len(items) > 0 {
				ui.Text(c, FormatProgressTally(tally.Done, tally.Running, tally.Queued, tally.Failed, tally.Needs, tally.Review, tally.Stopped, a.runningOn(a.stagePlatform()), a.liveParallel())).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
			}
		})
		if len(failed) > 0 {
			uiIconTextBtn(c, "retry-failed", "全部重试失败项", "全部重试失败项", iconRotate, 20, false, tok).
				Disabled(a.running).OnClick(func() { a.retryTasks(failed) })
		}
	})
}

func (a *App) resultsEmpty(c *ui.Context) {
	tok := a.look
	text := "出图结果会出现在这里"
	if a.washing() {
		text = "洗图结果会出现在这里"
	} else if a.fissioning() {
		text = "4 张裂变结果会出现在这里"
	}
	ui.Row(c.Key("stage-empty")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconImages).FontSize(16).TextColor(tok.muted)
		ui.Text(c, text).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
	})
}

func (a *App) buildResultRows(windowW float32) ([]resultRow, float32) {
	return a.buildResultRowsAt(windowW, time.Now())
}

func (a *App) buildResultRowsAt(windowW float32, _ time.Time) ([]resultRow, float32) {
	cols, tileW := resultLayoutForStage(a.homeStageW(windowW))
	items := tasksForPlatform(a.queueItems(), a.stagePlatform())
	browsers := a.configuredBrowsers()
	byID := make(map[int64]string, len(browsers))
	for _, b := range browsers {
		title := accountLabel(b)
		if b.Platform == agy.Platform && strings.TrimSpace(b.UserDataDir) != "" {
			if email := strings.TrimSpace(agy.Email(b.UserDataDir)); email != "" {
				title = email
			}
		}
		byID[b.ID] = title
	}
	type group struct {
		key, title string
		tasks      []tasks.Task
		tiles      []resultTile
	}
	var order []string
	groups := map[string]*group{}
	for pos, task := range items {
		if !task.Worked() {
			continue
		}
		key, title := a.resultAccountGroup(task, byID)
		g := groups[key]
		if g == nil {
			g = &group{key: key, title: title}
			groups[key] = g
			order = append(order, key)
		}
		g.tasks = append(g.tasks, task)
		if len(task.Results) == 0 {
			g.tiles = append(g.tiles, resultTile{
				key:  fmt.Sprintf("t-%d-%d-%s", task.Index, pos, task.Status),
				task: task,
			})
			continue
		}
		for i, path := range task.Results {
			g.tiles = append(g.tiles, resultTile{
				key:  fmt.Sprintf("p-%d-%d-%d", task.Index, pos, i),
				task: task,
				path: path,
				fly:  i == 0,
			})
		}
	}
	if len(groups) == 0 {
		return nil, tileW
	}
	ordered := make([]string, 0, len(groups))
	added := make(map[string]bool, len(groups))
	for _, b := range browsers {
		key := fmt.Sprintf("acct:%d", b.ID)
		if groups[key] != nil && !added[key] {
			ordered = append(ordered, key)
			added[key] = true
		}
	}
	for _, key := range order {
		if !added[key] {
			ordered = append(ordered, key)
			added[key] = true
		}
	}
	var rows []resultRow
	for _, key := range ordered {
		g := groups[key]
		rows = append(rows, resultRow{
			key:     "h:" + key,
			header:  true,
			title:   g.title,
			summary: accountGroupSummary(g.tasks),
		})
		for i := 0; i < len(g.tiles); i += cols {
			end := i + cols
			if end > len(g.tiles) {
				end = len(g.tiles)
			}
			rows = append(rows, resultRow{
				key:   fmt.Sprintf("r:%s:%d", key, i),
				tiles: g.tiles[i:end],
			})
		}
	}
	return rows, tileW
}

func (a *App) resultAccountGroup(task tasks.Task, byID map[int64]string) (key, title string) {
	if task.BrowserID > 0 {
		title = strings.TrimSpace(byID[task.BrowserID])
		if title == "" {
			title = strings.TrimSpace(task.AccountName)
		}
		if title == "" {
			if plat := strings.TrimSpace(task.Platform); plat != "" {
				title = fmt.Sprintf("%s账号%d", plat, task.BrowserID)
			} else {
				title = fmt.Sprintf("账号%d", task.BrowserID)
			}
		}
		return fmt.Sprintf("acct:%d", task.BrowserID), title
	}
	if name := strings.TrimSpace(task.AccountName); name != "" {
		return "acct-name:" + name, name
	}
	plat := strings.TrimSpace(task.Platform)
	if plat == "" && a != nil {
		plat = strings.TrimSpace(a.stagePlatform())
	}
	if plat == "" {
		return "plat:", "工作账号"
	}
	return "plat:" + plat, plat + "账号"
}

func accountGroupSummary(items []tasks.Task) string {
	if len(items) == 0 {
		return ""
	}
	tally := TallyTasks(items)
	parts := []string{fmt.Sprintf("已派发 %d 项", len(items))}
	if tally.Running > 0 {
		parts = append(parts, fmt.Sprintf("进行中 %d", tally.Running))
	}
	if tally.Done > 0 {
		parts = append(parts, fmt.Sprintf("完成 %d", tally.Done))
	}
	if tally.Failed > 0 {
		parts = append(parts, fmt.Sprintf("失败 %d", tally.Failed))
	}
	if tally.Needs > 0 {
		parts = append(parts, fmt.Sprintf("待验证 %d", tally.Needs))
	}
	if tally.Review > 0 {
		parts = append(parts, fmt.Sprintf("需核对 %d", tally.Review))
	}
	if tally.Stopped > 0 {
		parts = append(parts, fmt.Sprintf("已停止 %d", tally.Stopped))
	}
	return strings.Join(parts, " · ")
}

func taskSource(task tasks.Task) string {
	if len(task.Images) == 0 {
		return ""
	}
	return strings.TrimSpace(task.Images[0])
}

// resultLayout 按默认窗口宽度排 1 到 5 列。单张不拉成整栏宽。
func resultLayout(windowW float32) (cols int, tile float32) {
	return resultLayoutForStage(homeStageWDefault(windowW))
}

func resultLayoutForStage(stageW float32) (cols int, tile float32) {
	inner := stageW - 24
	if inner < resultTileMin {
		inner = resultTileMin
	}
	cols = int(inner / (resultTileMin + resultTileGap))
	if cols < 1 {
		cols = 1
	}
	if cols > 5 {
		cols = 5
	}
	tile = (inner - resultTileGap*float32(cols-1)) / float32(cols)
	if tile > resultTileMax {
		tile = resultTileMax
	}
	return cols, tile
}

func (a *App) resultHeader(c *ui.Context, row resultRow) {
	tok := a.look
	title := row.title
	if title == "" {
		title = "未命名"
	}
	ui.Row(c.Key(row.key)).Label("账号分组"+title).FillWidth().Padding(4, 8).Gap(8).
		Radius(tok.radiusS).Background(tok.sunken).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(tok.body).Bold().SingleLine().MinWidth(0).Grow(1).Tooltip(title)
		if row.summary != "" {
			ui.Text(c, row.summary).FontSize(tok.caption).FontFeatures("tnum").TextColor(tok.muted).SingleLine().Shrink(0).Tooltip(row.summary)
		}
	})
}

func (a *App) resultStrip(c *ui.Context, row resultRow, tileW float32) {
	ui.Row(c.Key(row.key)).Gap(resultTileGap).AlignItems(ui.Start).Children(func() {
		for _, tile := range row.tiles {
			a.resultTile(c, tile, tileW)
		}
	})
}

func (a *App) resultTile(c *ui.Context, tile resultTile, tileW float32) {
	tok := a.look
	task := tile.task
	title := tileTitle(tile)
	ui.Column(c.Key(tile.key)).Width(tileW).Shrink(0).Gap(4).Label("结果卡" + title).Children(func() {
		if a.washing() {
			tip := taskSource(task)
			source := filepath.Base(tip)
			if tip == "" {
				source, tip = "未命名", "未命名"
			}
			ui.Text(c.Key("source"), source).FillWidth().FontSize(tok.body).Bold().SingleLine().Tooltip(tip)
		}
		a.resultFrame(c, tile, tileW)
		ui.Row(c.Key("name")).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(tok.caption).SingleLine().MinWidth(0).Grow(1).Tooltip(title)
			if tile.path != "" && filePresent(tile.path) {
				a.revealButton(c, tile.path)
			}
		})
		elapsed := taskElapsedText(task, c.Now())
		ui.Row(c.Key("meta")).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			statusPill(c, tileStatus(task))
			ui.Text(c.Key("elapsed"), elapsed).FontSize(tok.caption).FontFeatures("tnum").
				TextColor(tok.muted).Grow(1).MinWidth(0).SingleLine().TextAlign(ui.End).Tooltip(elapsed)
		})
		if task.Rewritten {
			line := "已改写：" + shortLine(task.Prompt, 36)
			ui.Text(c.Key("rewrite"), line).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(line)
		}
		if reason, action, retry := tileFailure(task); reason != "" {
			color := tok.danger
			if task.Status == tasks.StatusNeedsUser || task.Status == tasks.StatusAwaitUser || task.Status == tasks.StatusNeedsReview {
				color = tok.warning
			}
			ui.Text(c.Key("reason"), reason).FontSize(tok.caption).TextColor(color).SingleLine().Tooltip(reason)
			if action != "" && len(retry) > 0 {
				label := action
				card := ProgressCard{Action: action, Retry: retry}
				uiMiniBtn(ui.Button(c.Key("retry"), label), tok).Disabled(a.running).OnClick(func() { a.failAction(card) })
			}
		}
	})
}

// revealButton 是文件名旁边的文件夹图标，按内容收缩，不拉成缩略图那么宽。
func (a *App) revealButton(c *ui.Context, path string) {
	tok := a.look
	b := ui.ButtonBase(c.Key("folder")).Size(20, 20).Radius(5).Shrink(0).Center().
		Label("在文件夹中显示").Tooltip("在文件夹中显示").Cursor(ui.CursorPointer)
	ink := tok.muted
	switch {
	case b.Pressed():
		b.Background(tok.text.Alpha(0.12))
		ink = tok.text
	case b.Hovered() || b.FocusVisible():
		b.Background(tok.sunkenHi)
		ink = tok.text
	}
	if b.FocusVisible() {
		b.Border(1, tok.accent)
	}
	b.OnClick(func() { a.revealResult(path) })
	b.Children(func() {
		ui.Icon(c, iconFolder).FontSize(12).TextColor(ink)
	})
}

func (a *App) resultFrame(c *ui.Context, tile resultTile, tileW float32) {
	tok := a.look
	path := tile.path
	present := path != "" && filePresent(path)
	var frame ui.Element
	if path != "" {
		frame = ui.ButtonBase(c.Key("shot")).Size(tileW, tileW).Radius(tok.radiusS).
			Background(tok.sunken).Clip().Shrink(0).Label("结果图")
		if present {
			frame.Cursor(ui.CursorPointer)
			p := path
			frame.OnClick(func() { a.openResult(p) })
		}
		if frame.FocusVisible() {
			frame.Border(2, tok.accent)
		}
	} else {
		frame = ui.Box(c.Key("shot")).Size(tileW, tileW).Radius(tok.radiusS).Background(tok.sunken).Clip().Shrink(0)
	}
	if tile.fly && path != "" {
		frame.Transition(ui.ElementTransition{
			Animation: &motionEaseOut,
			Position:  true,
			Size:      true,
			Match:     jobFly{tile.task.Index},
		})
	}
	status := tileStatus(tile.task)
	if status == "进行中" {
		// 底部一条细进度。整张扫光会在右缘留下一条竖的半透明带。
		reduce := c.Preferences().ReduceMotion
		accent := tok.accent
		frame.DrawOver(func(p *ui.Painter, r ui.Rect) {
			paintGenBar(p, ui.Rect{X: r.X + 8, Y: r.Y + r.H - 10, W: r.W - 16, H: 4}, accent, reduce)
		})
	}
	frame.Children(func() {
		switch {
		case present:
			if bmp := a.cachedThumb(path); bmp != nil {
				ui.Image(c, bmp).Size(tileW, tileW).Fit(ui.Cover).Radius(tok.radiusS)
			}
		case path != "":
			ui.Column(c).Size(tileW, tileW).Center().Padding(6).Children(func() {
				ui.Text(c, "文件已移除").FontSize(tok.caption).TextColor(tok.muted).SingleLine()
			})
		default:
			src := taskSource(tile.task)
			if filePresent(src) {
				if bmp := a.cachedThumb(src); bmp != nil {
					ui.Image(c, bmp).Size(tileW, tileW).Fit(ui.Cover).Radius(tok.radiusS)
				}
			} else {
				promptMarkBox(c, tok, tileW)
			}
		}
		if path != "" {
			if badge := shotBadge(tile.task.Platform, a.accountName(tile.task.BrowserID)); badge != "" {
				ui.Row(c).Label("产出"+badge).Absolute().Bottom(4).Left(4).Padding(1, 4).Radius(4).
					Background(tok.accent.Alpha(0.92)).Children(func() {
					ui.Text(c, badge).FontSize(10).TextColor(tok.onAccent).SingleLine()
				})
			}
		}
	})
}

func isTaskActiveStatus(status tasks.Status) bool {
	return status == tasks.StatusRunning || status == tasks.StatusRetry || status == tasks.StatusAwaitUser
}

func taskElapsedText(task tasks.Task, now time.Time) string {
	if isTaskActiveStatus(task.Status) {
		if task.StartedAt.IsZero() {
			return "耗时 0秒"
		}
		d := now.Sub(task.StartedAt)
		if d < 0 {
			d = 0
		}
		return "耗时 " + formatElapsedDuration(d, false)
	}
	if task.Elapsed > 0 {
		return "耗时 " + formatElapsedDuration(task.Elapsed, true)
	}
	return "耗时 --"
}

func formatElapsedDuration(d time.Duration, round bool) string {
	if d < 0 {
		d = 0
	}
	var sec int
	if round {
		sec = int((d + time.Second/2) / time.Second)
		if sec < 1 {
			sec = 1
		}
	} else {
		sec = int(d / time.Second)
	}
	if sec < 60 {
		return fmt.Sprintf("%d秒", sec)
	}
	return fmt.Sprintf("%d分%02d秒", sec/60, sec%60)
}

func tileTitle(tile resultTile) string {
	if tile.path != "" {
		return filepath.Base(tile.path)
	}
	if src := taskSource(tile.task); src != "" {
		return filepath.Base(src)
	}
	if line := oneLine(tile.task.Prompt); line != "" {
		return shortLine(line, 28)
	}
	return "未命名"
}

func tileStatus(task tasks.Task) string {
	switch task.Status {
	case tasks.StatusRunning, tasks.StatusRetry:
		return "进行中"
	case tasks.StatusSuccess:
		return "完成"
	case tasks.StatusFailed:
		return "失败"
	case tasks.StatusSkipped:
		return "已停止"
	case tasks.StatusPending:
		return "排队中"
	case tasks.StatusNeedsUser:
		return "待验证"
	case tasks.StatusAwaitUser:
		return "需要人工验证"
	default:
		if task.Status == "" {
			return "排队中"
		}
		return string(task.Status)
	}
}

func tileFailure(task tasks.Task) (reason, action string, retry []tasks.Task) {
	switch task.Status {
	case tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser, tasks.StatusNeedsReview:
		hint := tasks.ClassifyFailure(task.Status, task.Error)
		label := hint.Button
		if label == "" {
			label = "重试"
		}
		return hint.Sentence, label, []tasks.Task{task}
	case tasks.StatusAwaitUser:
		return "请在弹出的窗口里完成验证", "", nil
	default:
		return "", "", nil
	}
}

func filePresent(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func (a *App) revealResult(path string) {
	dir := strings.TrimSpace(filepath.Dir(path))
	if dir == "" || dir == "." {
		a.outputNote = "无法在文件夹中显示"
		logging.Log.Errorf("无法在文件夹中显示")
		return
	}
	if err := mygo.Shell.OpenPath(dir); err != nil {
		a.outputNote = "无法在文件夹中显示"
		logging.Log.Errorf("无法在文件夹中显示: %v", err)
	}
}
