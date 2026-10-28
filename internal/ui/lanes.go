package ui

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// jobFly 让刚完成的缩略图从账号车道飞到结果格。同一单只标一处。
type jobFly struct{ Task int }

// LaneHeader 是账号面板顶上的那一行。N 是现在能同时出图的账号数。
func LaneHeader(slots, done, total int) string {
	if slots < 0 {
		slots = 0
	}
	if done < 0 {
		done = 0
	}
	if total < 0 {
		total = 0
	}
	return fmt.Sprintf("%d 个账号同时出图 · 完成 %d / 共 %d", slots, done, total)
}

func queueDone(items []tasks.Task) (done, total int) {
	total = len(items)
	for _, task := range items {
		switch task.Status {
		case tasks.StatusSuccess, tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser:
			done++
		}
	}
	return done, total
}

func (a *App) liveParallel() int {
	slots := a.parallelSlots()
	if a == nil || a.concurrencyAuto {
		return slots
	}
	n := a.taskConcurrency()
	if slots > 0 && n > slots {
		return slots
	}
	return n
}

func (a *App) accountBoard(now time.Time) tasks.Board {
	capN := 20
	if a != nil {
		capN = a.shownDailyCap()
	}
	if a != nil && a.engine != nil {
		return a.engine.AccountBoard(now, capN)
	}
	return tasks.BoardFrom(a.laneBrowsers(), a.queueItems(), capN, now)
}

func (a *App) laneBrowsers() []store.Browser {
	if a == nil {
		return nil
	}
	ext := a.extensionTasks()
	var out []store.Browser
	for _, b := range a.configuredBrowsers() {
		if b.Platform != "豆包" && b.Platform != "千问" {
			continue
		}
		if ext != (b.ExtAccount != "") {
			continue
		}
		out = append(out, b)
	}
	return out
}

func (a *App) accountPanel(c *ui.Context, compact bool) {
	board := a.accountBoard(c.Now())
	if len(board.Lanes) == 0 {
		return
	}
	a.laneClock(c, board)
	if compact {
		a.laneStrip(c, board)
		return
	}
	a.laneCards(c, board)
}

// laneClock 让倒计时和已用时间每秒走一格。空闲时不预约下一帧。
func (a *App) laneClock(c *ui.Context, board tasks.Board) {
	now := c.Now()
	var wait time.Duration
	for _, ln := range board.Lanes {
		var left time.Duration
		switch ln.State {
		case tasks.LaneGap:
			left = ln.NextReadyAt.Sub(now)
		case tasks.LaneRunning:
			if ln.Job == nil {
				continue
			}
			elapsed := now.Sub(ln.Job.Started)
			if elapsed < 0 {
				elapsed = 0
			}
			left = time.Second - elapsed%time.Second
		default:
			continue
		}
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

func (a *App) laneHeader(c *ui.Context, board tasks.Board) {
	tok := a.look
	done, total := queueDone(a.queueItems())
	line := LaneHeader(a.liveParallel(), done, total)
	ui.Text(c.Key("lane-header"), line).FontSize(tok.body).Bold().SingleLine().
		ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseOut})
	if board.Active > 0 {
		ui.Text(c, fmt.Sprintf("正在跑 %d 个", board.Active)).FontSize(tok.caption).TextColor(tok.muted)
	}
}

func (a *App) laneStrip(c *ui.Context, board tasks.Board) {
	tok := a.look
	ui.Column(c.Key("lane-strip")).FillWidth().Gap(8).Padding(8, 10).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border).Transition(motionReveal()).Children(func() {
		a.laneHeader(c, board)
		ui.Row(c).Wrap().Gap(8).Children(func() {
			for i, ln := range board.Lanes {
				a.laneChipRow(c, ln, i, true)
			}
		})
	})
}

func (a *App) laneCards(c *ui.Context, board tasks.Board) {
	tok := a.look
	ui.Column(c.Key("lane-board")).FillWidth().Gap(10).Padding(12, 14).Radius(tok.radiusL).
		Background(tok.card).Border(1, tok.border).Shadow(0, 1, 2, 0, tok.shadow).
		Transition(motionReveal()).Children(func() {
		a.laneHeader(c, board)
		width, _ := c.Size()
		cols := int((width - 40) / 280)
		if cols < 1 {
			cols = 1
		}
		if cols > 3 {
			cols = 3
		}
		if n := len(board.Lanes); cols > n {
			cols = n
		}
		ui.Grid(c).Columns(cols).Gap(10).Children(func() {
			for i, ln := range board.Lanes {
				a.laneCard(c, ln, i)
			}
		})
	})
}

func laneEnter(i int) ui.ElementTransition {
	a := motionEaseOut
	a.Delay = time.Duration(i) * 35 * time.Millisecond
	return ui.ElementTransition{
		Animation: &a,
		Position:  true,
		Enter:     &ui.Motion{Y: 10, Scale: 0.97},
		Exit:      &ui.Motion{Y: 6, Scale: 0.97},
	}
}

func (a *App) laneCard(c *ui.Context, ln tasks.AccountLane, i int) {
	tok := a.look
	card := ui.Column(c.Key(ln.ID)).Gap(8).Padding(10, 12).Radius(tok.radiusM).
		Background(tok.paper).Border(1, tok.border).Transition(laneEnter(i))
	if ln.State == tasks.LaneRunning && !c.Preferences().ReduceMotion {
		card.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.AnimationFrame()
			phase := float32(p.Now().UnixMilli()%1400) / 1400
			wave := float32(0.5 + 0.5*math.Sin(float64(phase)*2*math.Pi))
			p.Stroke(ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}, tok.accent.Alpha(0.2+0.45*wave), tok.radiusM, 1.5)
		})
	}
	card.Children(func() {
		a.laneChipRow(c, ln, i, false)
		switch {
		case ln.Job != nil:
			a.laneJob(c, ln)
		case ln.State == tasks.LaneRunning:
			ui.Text(c, "同一窗口正在出图").FontSize(tok.caption).TextColor(tok.muted)
		}
		a.laneQuota(c, ln)
	})
}

func (a *App) laneChipRow(c *ui.Context, ln tasks.AccountLane, i int, compact bool) {
	tok := a.look
	var row ui.Element
	if compact {
		row = ui.Row(c.Key(ln.ID)).Gap(8).AlignItems(ui.Center).
			Padding(4, 8).Radius(tok.radiusS).Background(tok.paper).Border(1, tok.border).Transition(laneEnter(i))
	} else {
		row = ui.Row(c).Gap(8).AlignItems(ui.Center).FillWidth()
	}
	row.Children(func() {
		platformMark(c, ln.Platform, tok)
		name := ui.Text(c, ln.Name).FontSize(tok.body).SingleLine()
		if !compact {
			name.Bold().Grow(1).MinWidth(0)
		}
		if compact {
			ui.Text(c, fmt.Sprintf("%d/%d", ln.UsedToday, ln.Cap)).FontSize(tok.caption).TextColor(tok.muted).
				ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseFast})
		}
		a.laneChip(c, ln)
		if ln.State == tasks.LaneGap {
			gapRing(c, ln, tok)
		}
		if ln.Action != "" {
			label := ln.Action
			id := ln.ID
			ui.Button(c.Key("act"), label).OnClick(func() { a.openLane(id) })
		}
	})
}

func (a *App) laneChip(c *ui.Context, ln tasks.AccountLane) {
	tok := a.look
	fg, bg := laneInk(tok, ln.State)
	text := string(ln.State)
	if ln.State == tasks.LaneGap {
		text = fmt.Sprintf("等待间隔 %d", gapSeconds(ln.NextReadyAt, c.Now()))
	}
	pill := ui.Row(c.Key("chip")).Padding(1, tok.body/2+2).Radius(999).Shrink(0).AlignItems(ui.Center)
	pill.Background(pill.AnimateColor("bg", bg, motionEaseFast))
	pill.Children(func() {
		ui.Text(c.Key("chip-label"), text).FontSize(tok.caption).TextColor(fg).SingleLine().
			ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseFast})
	})
}

func laneInk(tok tokens, state tasks.LaneState) (fg, bg ui.Color) {
	switch state {
	case tasks.LaneRunning:
		return tok.accent, tok.accent.Alpha(0.14)
	case tasks.LaneGap, tasks.LaneCooling, tasks.LaneCaptcha:
		return tok.warning, tok.warning.Alpha(0.16)
	case tasks.LaneFull, tasks.LaneLogin:
		return tok.danger, tok.danger.Alpha(0.14)
	default:
		return tok.muted, tok.text.Alpha(0.08)
	}
}

func gapSeconds(until, now time.Time) int {
	left := until.Sub(now)
	if left <= 0 {
		return 0
	}
	return int((left + time.Second - time.Nanosecond) / time.Second)
}

func gapRing(c *ui.Context, ln tasks.AccountLane, tok tokens) {
	until, span := ln.NextReadyAt, ln.Gap
	reduce := c.Preferences().ReduceMotion
	box := ui.Box(c.Key("ring")).Size(18, 18).Shrink(0)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		left := until.Sub(p.Now())
		if left < 0 {
			left = 0
		}
		frac := float32(1)
		if span > 0 {
			frac = float32(left) / float32(span)
		}
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		if left > 0 && !reduce {
			p.AnimationFrame()
		}
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		rad := r.W/2 - 1.5
		p.StrokePath((&ui.Path{}).Circle(cx, cy, rad), 2, tok.border)
		if frac > 0.02 {
			p.StrokePath((&ui.Path{}).Circle(cx, cy, rad).Trim(0, frac), 2, tok.warning)
		}
	})
}

func (a *App) laneJob(c *ui.Context, ln tasks.AccountLane) {
	tok := a.look
	job := ln.Job
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		frame := ui.Box(c.Key("job")).Size(64, 64).Radius(tok.radiusS).Background(tok.sunken).Shrink(0)
		if job.Index > 0 {
			frame.Transition(ui.ElementTransition{Animation: &motionEaseOut, Position: true, Size: true, Match: jobFly{job.Index}})
		}
		frame.Children(func() {
			if job.Image == "" {
				promptMark(c, tok)
				return
			}
			if bmp := a.cachedThumb(job.Image); bmp != nil {
				shot := ui.Image(c, bmp).Size(64, 64).Fit(ui.Cover).Radius(tok.radiusS)
				shot.DrawOver(func(p *ui.Painter, r ui.Rect) { paintShimmer(c, p, r) })
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			line := shortLine(job.Prompt, 22)
			if line == "" {
				line = "正在出图"
			}
			ui.Text(c, line).FontSize(tok.caption).SingleLine().Tooltip(job.Prompt)
			ui.Text(c.Key("elapsed"), elapsedLabel(c.Now().Sub(job.Started))).FontSize(tok.caption).TextColor(tok.muted).MinWidth(72).
				ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseFast})
		})
	})
}

func elapsedLabel(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d / time.Second)
	if s < 60 {
		return fmt.Sprintf("已用 %d 秒", s)
	}
	return fmt.Sprintf("已用 %d 分 %d 秒", s/60, s%60)
}

func (a *App) laneQuota(c *ui.Context, ln tasks.AccountLane) {
	tok := a.look
	capN := ln.Cap
	if capN < 1 {
		capN = 1
	}
	used := ln.UsedToday
	if used < 0 {
		used = 0
	}
	frac := float64(used) / float64(capN)
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "今日").FontSize(tok.caption).TextColor(tok.muted)
		ui.Text(c.Key("used"), fmt.Sprintf("%d/%d", used, capN)).FontSize(tok.caption).TextColor(tok.text).
			ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseOut})
	})
	motionProgress(c, "quota", frac, false)
}

func platformMark(c *ui.Context, platform string, tok tokens) {
	label, bg := "账", tok.muted
	switch platform {
	case "豆包":
		label, bg = "豆", tok.accent
	case "千问":
		label, bg = "千", tok.success
	}
	ui.Column(c.Key("mark")).Size(22, 22).Radius(7).Background(bg).Center().Shrink(0).Children(func() {
		ui.Text(c, label).FontSize(12).Bold().TextColor(tok.onAccent)
	})
}

func shotBadge(platform, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == platform || name == "扩展" {
		return platform
	}
	return shortLine(name, 4)
}

func (a *App) openLane(id int64) {
	row := store.Browser{ID: id}
	for _, b := range a.configuredBrowsers() {
		if b.ID == id {
			row = b
			break
		}
	}
	logging.Log.Info("请在「%s」的网页里处理", accountLabel(row))
	go func(b store.Browser) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_ = a.ensureAccountPage(ctx, b)
	}(row)
}
