package ui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
)

const (
	logRowH    float32 = 22
	logDetailH float32 = 18
	logMinW    float32 = 420
	logMinH    float32 = 280
)

// logFloat 是盖在工作区上的深色日志框，不换页面。
func (a *App) logFloat(c *ui.Context) {
	if a == nil || !a.logOpen {
		return
	}
	winW, winH := c.Size()
	a.placeLog(winW, winH)
	panel := ui.Column(c.Key("run-log")).Label("运行日志区").Absolute().
		Left(a.logX).Top(a.logY).Size(a.logW, a.logH).Padding(12, 16).Gap(8).
		Background(ui.Hex("#101318")).Border(1, ui.Hex("#2A3140")).Radius(10).
		Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.45)).TextColor(ui.Hex("#E7ECF3"))
	panel.Children(func() {
		a.logHeader(c)
		a.logToolbar(c)
		a.logRows(c)
		a.logFooter(c)
	})
}

func (a *App) placeLog(winW, winH float32) {
	maxW := max(logMinW, winW-24)
	maxH := max(logMinH, winH-topbarH-16)
	if !a.logPlaced {
		a.logW = min(880, maxW)
		a.logH = min(520, maxH)
		a.logX = max(12, winW-a.logW-12)
		a.logY = topbarH + 8
		return
	}
	a.logW = clamp(a.logW, logMinW, maxW)
	a.logH = clamp(a.logH, logMinH, maxH)
	a.logX = clamp(a.logX, 8, max(8, winW-a.logW-8))
	a.logY = clamp(a.logY, topbarH+4, max(topbarH+4, winH-a.logH-8))
}

func (a *App) logHeader(c *ui.Context) {
	head := ui.Row(c.Key("log-header")).Label("日志标题栏").FillWidth().Height(28).Gap(8).AlignItems(ui.Center)
	if dx, dy, ok := head.Dragged(); ok && (dx != 0 || dy != 0) {
		a.logX += dx
		a.logY += dy
		a.logPlaced = true
	}
	head.Cursor(ui.CursorGrab).Children(func() {
		ui.Icon(c, iconTerminal).FontSize(13).TextColor(ui.Hex("#9AA6B8"))
		ui.Text(c, "运行日志").FontSize(14).Bold().TextColor(ui.Hex("#F4F7FB")).SingleLine()
		live := "实时接收"
		dot := ui.Hex("#3DDC97")
		if a.logPaused {
			live = "已暂停"
			dot = ui.Hex("#8B95A7")
		}
		ui.Box(c).Size(7, 7).Radius(4).Background(dot)
		ui.Text(c, live).FontSize(12).TextColor(ui.Hex("#9AA6B8")).SingleLine()
		ui.Spacer(c)
		a.logTool(c, "log-follow", a.followLabel(), func() { a.logPaused = !a.logPaused })
		a.logTool(c, "log-export", "导出", func() { a.exportLog() })
		a.logTool(c, "log-copy", "复制", func() { c.Services().WriteClipboard(logging.ExportText(a.visibleLogs())) })
		a.logTool(c, "log-clear", "清空", func() { logging.Log.Clear() })
		a.logTool(c, "log-close", "关闭", func() { a.logOpen = false })
	})
	grip := ui.Box(c.Key("log-resize")).Label("调整日志大小").Absolute().Right(2).Bottom(2).Size(14, 14).
		Background(ui.Hex("#3A4456")).Radius(3).Cursor(ui.CursorResizeNWSE)
	if dx, dy, ok := grip.Dragged(); ok {
		a.logW += dx
		a.logH += dy
		a.logPlaced = true
	}
}

func (a *App) followLabel() string {
	if a.logPaused {
		return "继续跟随"
	}
	return "暂停跟随"
}

func (a *App) logTool(c *ui.Context, key, label string, click func()) {
	b := ui.ButtonBase(c.Key(key)).Height(24).Padding(0, 8).Radius(6).Center().
		Background(ui.Hex("#1B212C")).Border(1, ui.Hex("#313A4A")).
		Label(label).Cursor(ui.CursorPointer)
	b.OnClick(click)
	b.Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(ui.Hex("#D5DCE8")).SingleLine()
	})
}

func (a *App) logToolbar(c *ui.Context) {
	entries := logging.Log.Entries()
	counts := logging.CountLevels(entries)
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "级别").FontSize(12).TextColor(ui.Hex("#8E9AAD")).Shrink(0).SingleLine()
			a.levelChip(c, "全部", counts.All, 0)
			a.levelChip(c, "调试", counts.Debug, 1)
			a.levelChip(c, "信息", counts.Info, 2)
			a.levelChip(c, "警告", counts.Warn, 3)
			a.levelChip(c, "错误", counts.Error, 4)
		})
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.SearchField(c, &a.logQuery).Placeholder("搜索事件或字段").Label("搜索事件或字段").Grow(1).MinWidth(120)
			a.sourceMenu(c, logging.Sources(entries))
		})
	})
}

func (a *App) levelChip(c *ui.Context, name string, n int, pick int) {
	on := a.logPick == pick
	bg, fg := ui.Hex("#1B212C"), ui.Hex("#C5D0DE")
	if on {
		bg, fg = ui.Hex("#E7ECF3"), ui.Hex("#12161C")
	}
	label := fmt.Sprintf("%s %d", name, n)
	b := ui.ButtonBase(c.Key("log-level-"+name)).Height(24).Padding(0, 8).Radius(6).Center().
		Background(bg).Label(label).Cursor(ui.CursorPointer)
	b.OnClick(func() { a.logPick = pick })
	b.Children(func() {
		ui.Text(c, label).FontSize(12).TextColor(fg).SingleLine()
	})
}

func (a *App) sourceMenu(c *ui.Context, sources []string) {
	label := "全部来源"
	if a.logSource != "" {
		label = trimSource(a.logSource)
	}
	a.logTool(c, "log-source", label, func() {
		options := append([]string{""}, sources...)
		next := 0
		for i, source := range options {
			if source == a.logSource {
				next = (i + 1) % len(options)
				break
			}
		}
		a.logSource = options[next]
	})
}

func (a *App) logRows(c *ui.Context) {
	rows := a.visibleLogs()
	if !a.logPaused {
		a.logScroll.Y = math.MaxFloat32
	}
	scroll := ui.Scroll(c.Key("run-log-lines")).Label("运行日志内容").FillWidth().Grow(1).MinHeight(0)
	scroll.TrackScroll(&a.logScroll)
	if len(rows) == 0 {
		scroll.Children(func() {
			msg := "每个任务的开始、提交和结束会写在这里"
			if strings.TrimSpace(a.logQuery) != "" || a.logSource != "" || a.logPick != 0 {
				msg = "没有匹配的日志"
			}
			ui.Text(c, msg).FontSize(12).TextColor(ui.Hex("#8E9AAD"))
		})
		return
	}
	scroll.Children(func() {
		win := logging.VisibleWindow(a.logRowHeights(rows), scroll.Bounds().H, a.logScroll.Y)
		if win.Top > 0.5 {
			ui.Box(c.Key("log-pad-top")).Height(win.Top)
		}
		for i := win.Start; i < win.End; i++ {
			a.logRow(c, rows[i])
		}
		if win.Bottom > 0.5 {
			ui.Box(c.Key("log-pad-bottom")).Height(win.Bottom)
		}
	})
}

func (a *App) logRowHeights(rows []logging.Entry) []float32 {
	heights := make([]float32, len(rows))
	for i, row := range rows {
		heights[i] = logRowH
		if a.logExpanded[row.Seq] && row.Detail != "" {
			heights[i] += logDetailH
		}
	}
	return heights
}

func (a *App) visibleLogs() []logging.Entry {
	level := logging.Level(-1)
	switch a.logPick {
	case 1:
		level = logging.Debug
	case 2:
		level = logging.Info
	case 3:
		level = logging.Warning
	case 4:
		level = logging.Error
	}
	return logging.Filter(logging.Log.Entries(), logging.View{
		Level:  level,
		Query:  a.logQuery,
		Source: a.logSource,
	})
}

func (a *App) logRow(c *ui.Context, entry logging.Entry) {
	open := a.logExpanded != nil && a.logExpanded[entry.Seq]
	row := ui.Column(c.Key(fmt.Sprintf("log-row-%d", entry.Seq))).FillWidth()
	row.OnClick(func() {
		if a.logExpanded == nil {
			a.logExpanded = map[int]bool{}
		}
		a.logExpanded[entry.Seq] = !a.logExpanded[entry.Seq]
	})
	row.Children(func() {
		ui.Row(c).FillWidth().Height(logRowH).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, entry.Time.Local().Format("15:04:05.000")).Width(108).Shrink(0).
				FontSize(12).FontFeatures("tnum").TextColor(ui.Hex("#8E9AAD")).SingleLine()
			ui.Text(c, logging.LevelCode(entry.Level)).Width(32).Shrink(0).
				FontSize(12).Bold().TextColor(levelInk(entry.Level)).SingleLine()
			ui.Text(c, "["+trimSource(entry.Source)+"]").Width(108).Shrink(0).
				FontSize(12).TextColor(ui.Hex("#7EB6FF")).SingleLine()
			ui.Text(c, entry.Text).Grow(1).MinWidth(0).FontSize(12).
				TextColor(ui.Hex("#E7ECF3")).SingleLine().Tooltip(entry.Text)
			if entry.Elapsed > 0 {
				ui.Text(c, entry.Elapsed.Round(time.Millisecond).String()).Width(64).Shrink(0).
					FontSize(12).FontFeatures("tnum").TextColor(ui.Hex("#7EB6FF")).SingleLine()
			}
		})
		if open && entry.Detail != "" {
			ui.Text(c, entry.Detail).FillWidth().Height(logDetailH).FontSize(11).
				TextColor(ui.Hex("#9AA6B8")).SingleLine()
		}
	})
}

func (a *App) logFooter(c *ui.Context) {
	all := logging.Log.Entries()
	shown := a.visibleLogs()
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		dot := ui.Hex("#3DDC97")
		label := "实时跟随"
		if a.logPaused {
			dot = ui.Hex("#8B95A7")
			label = "已暂停跟随"
		}
		ui.Box(c).Size(7, 7).Radius(4).Background(dot)
		ui.Text(c, label).FontSize(12).TextColor(ui.Hex("#9AA6B8")).SingleLine()
		ui.Text(c, fmt.Sprintf("共 %d 条", len(all))).FontSize(12).TextColor(ui.Hex("#C5D0DE")).SingleLine()
		ui.Text(c, fmt.Sprintf("显示 %d 条", len(shown))).FontSize(12).TextColor(ui.Hex("#8E9AAD")).SingleLine()
		ui.Spacer(c)
		if n := len(all); n > 0 {
			ui.Text(c, "最近收到 "+all[n-1].Time.Local().Format("15:04:05.000")).
				FontSize(12).TextColor(ui.Hex("#8E9AAD")).SingleLine()
		}
	})
}

func (a *App) exportLog() {
	text := logging.ExportText(a.visibleLogs())
	name := "tulai-log-" + time.Now().Format("20060102-150405") + ".log"
	path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Title:       "导出运行日志",
		DefaultPath: name,
		Filters:     []mygo.FileFilter{{Name: "日志", Extensions: []string{"log", "txt"}}},
	})
	if err != nil || path == "" {
		return
	}
	_ = os.WriteFile(path, []byte(text), 0o600)
}

func levelInk(level logging.Level) ui.Color {
	switch level {
	case logging.Debug:
		return ui.Hex("#8E9AAD")
	case logging.Warning:
		return ui.Hex("#E2B340")
	case logging.Error:
		return ui.Hex("#F07178")
	case logging.Success:
		return ui.Hex("#3DDC97")
	default:
		return ui.Hex("#7EB6FF")
	}
}

func trimSource(source string) string {
	if len([]rune(source)) <= 12 {
		return source
	}
	return string([]rune(source)[:11]) + "…"
}

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
