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
	panel := ui.Themed(c.Key("run-log"), logDarkTheme(c.Theme())).Label("运行日志区").Absolute().
		Left(a.logX).Top(a.logY).Size(a.logW, a.logH).Gap(0).
		Background(ui.Hex("#0B0E14")).Border(1, ui.Hex("#252F42")).Radius(8).
		Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.50)).TextColor(ui.Hex("#F1F5F9"))
	panel.Children(func() {
		ui.Column(c).FillWidth().Padding(10, 14, 9, 14).Gap(8).Radius(8, 8, 0, 0).
			Background(ui.Hex("#0F131B")).BorderWidth(0, 0, 1, 0).BorderColor(ui.Hex("#252F42")).Children(func() {
			a.logHeader(c)
			a.logToolbar(c)
		})
		a.logRows(c)
		a.logFooter(c)
		a.logResizeGrip(c)
	})
}

func logDarkTheme(base *ui.Theme) *ui.Theme {
	tok := darkTokens()
	var th ui.Theme
	if base != nil {
		th = *base
	}
	th.Dark = true
	th.Background = ui.Hex("#141923")
	th.Surface = ui.Hex("#1B2230")
	th.SurfaceHover = ui.Hex("#242D3F")
	th.SurfacePressed = ui.Hex("#242D3F")
	th.Border = ui.Hex("#252F42")
	th.Text = ui.Hex("#F1F5F9")
	th.TextMuted = ui.Hex("#94A3B8")
	th.Accent = tok.accent
	th.AccentText = tok.onAccent
	th.Selection = tok.accent.Alpha(0.22)
	th.Focus = tok.accent.Alpha(0.55)
	th.Scrollbar = ui.RGBA(148, 163, 184, 0.28)
	th.Radius = 5
	return &th
}

func (a *App) logResizeGrip(c *ui.Context) {
	grip := ui.Box(c.Key("log-resize")).Label("调整日志大小").Absolute().Right(3).Bottom(3).Size(12, 12).
		Cursor(ui.CursorResizeNWSE)
	if dx, dy, ok := grip.Dragged(); ok {
		a.logW += dx
		a.logH += dy
		a.logPlaced = true
	}
	grip.Draw(func(p *ui.Painter, r ui.Rect) {
		col := ui.Hex("#475569")
		if grip.Hovered() {
			col = ui.Hex("#94A3B8")
		}
		p.Line(r.X+r.W-3, r.Y+r.H-1, r.X+r.W-1, r.Y+r.H-3, 1.25, col)
		p.Line(r.X+r.W-7, r.Y+r.H-1, r.X+r.W-1, r.Y+r.H-7, 1.25, col)
		p.Line(r.X+r.W-11, r.Y+r.H-1, r.X+r.W-1, r.Y+r.H-11, 1.25, col)
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
	head := ui.Row(c.Key("log-header")).Label("日志标题栏").FillWidth().Height(26).Gap(8).AlignItems(ui.Center)
	if dx, dy, ok := head.Dragged(); ok && (dx != 0 || dy != 0) {
		a.logX += dx
		a.logY += dy
		a.logPlaced = true
	}
	head.Cursor(ui.CursorGrab).Children(func() {
		ui.Icon(c, iconTerminal).FontSize(13).TextColor(ui.Hex("#94A3B8"))
		ui.Text(c, "运行日志").FontSize(13.5).FontWeight(600).TextColor(ui.Hex("#F1F5F9")).SingleLine()
		live := "实时接收"
		dot := ui.Hex("#34D399")
		if a.logPaused {
			live = "已暂停"
			dot = ui.Hex("#94A3B8")
		}
		ui.Row(c).Height(20).Padding(0, 7).Radius(999).Gap(5).AlignItems(ui.Center).
			Background(ui.Hex("#141923")).Border(1, ui.Hex("#252F42")).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(dot)
			ui.Text(c, live).FontSize(11).TextColor(ui.Hex("#CBD5E1")).SingleLine()
		})
		ui.Spacer(c)
		a.logTool(c, "log-follow", a.followLabel(), func() { a.logPaused = !a.logPaused })
		a.logTool(c, "log-export", "导出", func() { a.exportLog() })
		a.logTool(c, "log-copy", "复制", func() { c.Services().WriteClipboard(logging.ExportText(a.visibleLogs())) })
		a.logTool(c, "log-clear", "清空", func() { logging.Log.Clear() })
		a.logTool(c, "log-close", "关闭", func() { a.logOpen = false })
	})
}

func (a *App) followLabel() string {
	if a.logPaused {
		return "继续跟随"
	}
	return "暂停跟随"
}

func (a *App) logTool(c *ui.Context, key, label string, click func()) {
	b := ui.ButtonBase(c.Key(key)).Height(24).Padding(0, 8).Radius(5).Center().
		Label(label).Cursor(ui.CursorPointer)
	bg, border, fg := ui.Hex("#1B2230"), ui.Hex("#252F42"), ui.Hex("#CBD5E1")
	switch {
	case b.Pressed():
		bg, border, fg = ui.Hex("#141923"), ui.Hex("#34425A"), ui.Hex("#F1F5F9")
	case b.Hovered():
		bg, border, fg = ui.Hex("#242D3F"), ui.Hex("#34425A"), ui.Hex("#F1F5F9")
	}
	b.Background(bg).Border(1, border)
	hoverPressPose(b)
	b.OnClick(click)
	b.Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(fg).SingleLine()
	})
}

func (a *App) logToolbar(c *ui.Context) {
	entries := logging.Log.Entries()
	counts := logging.CountLevels(entries)
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "级别").FontSize(11.5).TextColor(ui.Hex("#94A3B8")).Shrink(0).SingleLine()
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
	bg, border, fg := ui.Hex("#141923"), ui.Hex("#252F42"), ui.Hex("#CBD5E1")
	if on {
		switch pick {
		case 0:
			bg, border, fg = ui.Hex("#F1F5F9"), ui.Hex("#F1F5F9"), ui.Hex("#090D16")
		case 1:
			bg, border, fg = ui.Hex("#242D3F"), ui.Hex("#475569"), ui.Hex("#F1F5F9")
		case 2:
			fg, bg, border = badgeTone("accent", darkTokens())
		case 3:
			fg, bg, border = badgeTone("warning", darkTokens())
		case 4:
			fg, bg, border = badgeTone("danger", darkTokens())
		}
	}
	label := fmt.Sprintf("%s %d", name, n)
	b := ui.ButtonBase(c.Key("log-level-"+name)).Height(22).Padding(0, 8).Radius(5).Center().
		Label(label).Cursor(ui.CursorPointer)
	if !on && b.Hovered() {
		bg, border, fg = ui.Hex("#1B2230"), ui.Hex("#34425A"), ui.Hex("#F1F5F9")
	}
	b.Background(bg).Border(1, border)
	b.OnClick(func() { a.logPick = pick })
	b.Children(func() {
		t := ui.Text(c, label).FontSize(11.5).FontFeatures("tnum").TextColor(fg).SingleLine()
		if on {
			t.FontWeight(600)
		}
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
	ui.Column(c).FillWidth().Grow(1).MinHeight(0).Padding(6, 10).Background(ui.Hex("#0B0E14")).Children(func() {
		scroll := ui.Scroll(c.Key("run-log-lines")).Label("运行日志内容").FillWidth().Grow(1).MinHeight(0)
		scroll.TrackScroll(&a.logScroll)
		if len(rows) == 0 {
			scroll.Children(func() {
				msg := "每个任务的开始、提交和结束会写在这里"
				if strings.TrimSpace(a.logQuery) != "" || a.logSource != "" || a.logPick != 0 {
					msg = "没有匹配的日志"
				}
				ui.Text(c, msg).FontSize(12).TextColor(ui.Hex("#94A3B8"))
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
	row := ui.Column(c.Key(fmt.Sprintf("log-row-%d", entry.Seq))).FillWidth().Radius(4).Padding(0, 6).Cursor(ui.CursorPointer)
	if open || row.Hovered() {
		row.Background(ui.Hex("#141923"))
	}
	row.OnClick(func() {
		if a.logExpanded == nil {
			a.logExpanded = map[int]bool{}
		}
		a.logExpanded[entry.Seq] = !a.logExpanded[entry.Seq]
	})
	badgeFg, badgeBg, badgeBorder := levelBadgeStyle(entry.Level)
	row.Children(func() {
		ui.Row(c).FillWidth().Height(logRowH).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, entry.Time.Local().Format("15:04:05.000")).Width(100).Shrink(0).
				FontSize(11.5).FontFeatures("tnum").TextColor(ui.Hex("#94A3B8")).SingleLine()
			ui.Row(c).Width(36).Height(16).Radius(4).Center().Shrink(0).
				Background(badgeBg).Border(1, badgeBorder).Children(func() {
				ui.Text(c, logging.LevelCode(entry.Level)).FontSize(10).FontWeight(700).
					TextColor(badgeFg).SingleLine()
			})
			ui.Text(c, "["+trimSource(entry.Source)+"]").Width(108).Shrink(0).
				FontSize(11.5).TextColor(ui.Hex("#7EB6FF")).SingleLine()
			ui.Text(c, entry.Text).Grow(1).MinWidth(0).FontSize(12).
				TextColor(ui.Hex("#F1F5F9")).SingleLine().Tooltip(entry.Text)
			if entry.Elapsed > 0 {
				ui.Text(c, entry.Elapsed.Round(time.Millisecond).String()).Width(64).Shrink(0).
					FontSize(11.5).FontFeatures("tnum").TextColor(ui.Hex("#93C5FD")).SingleLine()
			}
		})
		if open && entry.Detail != "" {
			ui.Text(c, entry.Detail).FillWidth().Height(logDetailH).Padding(0, 6, 0, 8).FontSize(11).
				TextColor(ui.Hex("#94A3B8")).SingleLine()
		}
	})
}

func (a *App) logFooter(c *ui.Context) {
	all := logging.Log.Entries()
	shown := a.visibleLogs()
	ui.Row(c).FillWidth().Padding(8, 22, 8, 14).Gap(10).AlignItems(ui.Center).
		Radius(0, 0, 8, 8).Background(ui.Hex("#0F131B")).
		BorderWidth(1, 0, 0, 0).BorderColor(ui.Hex("#252F42")).Children(func() {
		dot := ui.Hex("#34D399")
		label := "实时跟随"
		if a.logPaused {
			dot = ui.Hex("#94A3B8")
			label = "已暂停跟随"
		}
		ui.Box(c).Size(7, 7).Radius(4).Background(dot)
		ui.Text(c, label).FontSize(11.5).TextColor(ui.Hex("#CBD5E1")).SingleLine()
		ui.Text(c, fmt.Sprintf("共 %d 条", len(all))).FontSize(11.5).FontFeatures("tnum").TextColor(ui.Hex("#CBD5E1")).SingleLine()
		ui.Text(c, fmt.Sprintf("显示 %d 条", len(shown))).FontSize(11.5).FontFeatures("tnum").TextColor(ui.Hex("#94A3B8")).SingleLine()
		ui.Spacer(c)
		if n := len(all); n > 0 {
			ui.Text(c, "最近收到 "+all[n-1].Time.Local().Format("15:04:05.000")).
				FontSize(11.5).FontFeatures("tnum").TextColor(ui.Hex("#94A3B8")).SingleLine()
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

func levelBadgeStyle(level logging.Level) (fg, bg, border ui.Color) {
	tok := darkTokens()
	switch level {
	case logging.Debug:
		return badgeTone("neutral", tok)
	case logging.Warning:
		return badgeTone("warning", tok)
	case logging.Error:
		return badgeTone("danger", tok)
	case logging.Success:
		return badgeTone("success", tok)
	default:
		return badgeTone("accent", tok)
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
