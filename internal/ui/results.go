package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/tasks"
)

const (
	resultTileMin = 96
	resultTileMax = 168
	resultTileGap = 8
)

// resultTile 是结果格里的一张图，或还没有文件时的一条状态。
type resultTile struct {
	key  string
	task tasks.Task
	path string
	fly  bool
}

// resultRow 是虚拟列表的一行：分组标题，或一行缩略图。
type resultRow struct {
	key    string
	header bool
	title  string
	tiles  []resultTile
}

func (a *App) resultList() *ui.ListState {
	if a.washing() {
		return &a.washList
	}
	return &a.genList
}

// resultsPane 撑满账号表下面的高度。缩略图按提示词或源图分组，滚进视口才解码。
func (a *App) resultsPane(c *ui.Context) {
	tok := a.look
	w, _ := c.Size()
	rows, tileW := a.buildResultRows(w)
	pane := ui.Column(c.Key("results")).FillWidth().Padding(10, 12).Gap(8).Radius(tok.radiusL).
		Background(tok.card).Border(1, tok.border)
	if homeSplit(w) {
		pane.Grow(1).MinHeight(0)
	}
	pane.Children(func() {
		a.resultsHead(c)
		if TallyTasks(tasksForPlatform(a.queueItems(), a.stagePlatform())).Needs > 0 {
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

func (a *App) resultsHead(c *ui.Context) {
	tok := a.look
	items := tasksForPlatform(a.queueItems(), a.stagePlatform())
	failed := FailedTasks(items)
	tally := TallyTasks(items)
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, "出图进度").FontSize(tok.title).Bold()
			if len(items) > 0 {
				ui.Text(c, FormatProgressTally(tally.Done, tally.Running, tally.Queued, tally.Failed, tally.Needs, tally.Stopped, a.liveParallel())).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
			}
		})
		if len(failed) > 0 {
			ui.Button(c, "全部重试失败项").FontSize(tok.caption).Padding(3, 8).Radius(tok.radiusS).Shrink(0).
				Disabled(a.running).OnClick(func() { a.retryTasks(failed) })
		}
	})
}

func (a *App) resultsEmpty(c *ui.Context) {
	tok := a.look
	text := "出图结果会出现在这里"
	if a.washing() {
		text = "洗图结果会出现在这里"
	}
	ui.Row(c.Key("stage-empty")).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconImages).FontSize(16).TextColor(tok.muted)
		ui.Text(c, text).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
	})
}

func (a *App) buildResultRows(windowW float32) ([]resultRow, float32) {
	cols, tileW := resultLayout(windowW)
	items := tasksForPlatform(a.queueItems(), a.stagePlatform())
	type group struct {
		key, title string
		tiles      []resultTile
	}
	var order []string
	groups := map[string]*group{}
	for _, task := range items {
		key, title := a.resultGroup(task)
		g := groups[key]
		if g == nil {
			g = &group{key: key, title: title}
			groups[key] = g
			order = append(order, key)
		}
		if len(task.Results) == 0 {
			g.tiles = append(g.tiles, resultTile{
				key:  fmt.Sprintf("t-%d-%s", task.Index, task.Status),
				task: task,
			})
			continue
		}
		for i, path := range task.Results {
			g.tiles = append(g.tiles, resultTile{
				key:  fmt.Sprintf("p-%d-%d", task.Index, i),
				task: task,
				path: path,
				fly:  i == 0,
			})
		}
	}
	var rows []resultRow
	for _, key := range order {
		g := groups[key]
		rows = append(rows, resultRow{key: "h:" + key, header: true, title: g.title})
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

// resultGroup 生图按提示词分组，洗图按源图分组。
func (a *App) resultGroup(task tasks.Task) (key, title string) {
	if a != nil && a.washing() {
		if src := taskSource(task); src != "" {
			return "img:" + src, filepath.Base(src)
		}
	}
	if p := oneLine(task.Prompt); p != "" {
		return "prompt:" + p, p
	}
	if src := taskSource(task); src != "" {
		return "img:" + src, filepath.Base(src)
	}
	return "prompt:", "未命名"
}

func taskSource(task tasks.Task) string {
	if len(task.Images) == 0 {
		return ""
	}
	return strings.TrimSpace(task.Images[0])
}

// resultLayout 按右栏宽度排 1 到 5 列。单张不拉成整栏宽。
func resultLayout(windowW float32) (cols int, tile float32) {
	inner := homeStageW(windowW) - 28
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
	ui.Row(c.Key(row.key)).FillWidth().Padding(4, 2).Background(tok.card).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(tok.body).Bold().SingleLine().MinWidth(0).Grow(1).Tooltip(title)
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
		a.resultFrame(c, tile, tileW)
		ui.Row(c.Key("name")).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(tok.caption).SingleLine().MinWidth(0).Grow(1).Tooltip(title)
			if tile.path != "" && filePresent(tile.path) {
				a.revealButton(c, tile.path)
			}
		})
		if task.Rewritten {
			line := "已改写：" + shortLine(task.Prompt, 36)
			ui.Text(c.Key("rewrite"), line).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(line)
		}
		if reason, action, retry := tileFailure(task); reason != "" {
			color := tok.danger
			if task.Status == tasks.StatusNeedsUser || task.Status == tasks.StatusAwaitUser {
				color = tok.warning
			}
			ui.Text(c.Key("reason"), reason).FontSize(tok.caption).TextColor(color).SingleLine().Tooltip(reason)
			if action != "" && len(retry) > 0 {
				label := action
				card := ProgressCard{Action: action, Retry: retry}
				ui.Button(c.Key("retry"), label).FontSize(tok.caption).Padding(2, 8).Radius(tok.radiusS).Shrink(0).
					Disabled(a.running).OnClick(func() { a.failAction(card) })
			}
		}
	})
}

// revealButton 是文件名旁边的文件夹图标，按内容收缩，不拉成缩略图那么宽。
func (a *App) revealButton(c *ui.Context, path string) {
	tok := a.look
	b := ui.ButtonBase(c.Key("folder")).Size(24, 24).Radius(6).Shrink(0).Center().
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
		ui.Icon(c, iconFolder).FontSize(14).TextColor(ink)
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
		statusPill(c, status).Absolute().Top(6).Left(6)
		if path != "" {
			if badge := shotBadge(tile.task.Platform, a.accountName(tile.task.BrowserID)); badge != "" {
				ui.Row(c).Label("产出"+badge).Absolute().Bottom(4).Left(4).Padding(1, 4).Radius(4).
					Background(tok.accent.Alpha(0.92)).Children(func() {
					ui.Text(c, badge).FontSize(12).TextColor(tok.onAccent).SingleLine()
				})
			}
		}
	})
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
	case tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser:
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
