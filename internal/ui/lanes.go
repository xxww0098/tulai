package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// jobFly 让刚完成的缩略图从账号车道飞到结果格。同一单只标一处。
type jobFly struct{ Task int }

// LaneHeader 是已经在出图时面板顶上的那一行。
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

// LaneStatusLine 是账号面板的标题。完成数只出现在第 ③ 步，这里不重复。
func LaneStatusLine(slots int, started bool) string {
	if slots < 0 {
		slots = 0
	}
	if slots == 0 {
		return "暂无可用账号"
	}
	if !started {
		return fmt.Sprintf("%d 个账号空闲", slots)
	}
	return fmt.Sprintf("%d 个账号出图中", slots)
}

// BlockedAccounts 把不能出图的账号收成一句原因。能用的状态不写进来。
func BlockedAccounts(lanes []tasks.AccountLane) string {
	var parts []string
	seen := map[string]bool{}
	for _, ln := range lanes {
		reason := blockReason(ln.State)
		if reason == "" {
			continue
		}
		name := ln.Platform
		if name == "" {
			name = ln.Name
		}
		piece := name + reason
		if seen[piece] {
			continue
		}
		seen[piece] = true
		parts = append(parts, piece)
	}
	if len(parts) == 0 {
		return "还没有可用账号"
	}
	return "没有可用账号：" + strings.Join(parts, "，")
}

func blockReason(state tasks.LaneState) string {
	switch state {
	case tasks.LaneLogin:
		return "需要重新登录"
	case tasks.LaneFull:
		return "今日已满"
	case tasks.LaneQuota:
		return "额度不足"
	case tasks.LaneCooling:
		return "冷却中"
	case tasks.LaneCaptcha:
		return "需要验证"
	case tasks.LaneOffline:
		return "离线"
	default:
		return ""
	}
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
	var board tasks.Board
	if a != nil && a.engine != nil {
		board = a.engine.AccountBoard(now, capN)
	} else {
		board = tasks.BoardFrom(a.laneBrowsers(), a.queueItems(), capN, now)
	}
	return a.attachAgyLanes(board)
}

func (a *App) laneBrowsers() []store.Browser {
	if a == nil {
		return nil
	}
	ext := a.extensionTasks()
	var out []store.Browser
	for _, b := range a.configuredBrowsers() {
		if b.Platform == agy.Platform {
			out = append(out, b)
			continue
		}
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

func (a *App) accountPanel(c *ui.Context) {
	if a.stagePlatform() == agy.Platform {
		a.agyLoginPanel(c)
	}
	board := filterBoard(a.accountBoard(c.Now()), a.stagePlatform())
	if len(board.Lanes) == 0 {
		ui.Row(c.Key("lane-empty")).Gap(8).AlignItems(ui.Center).Children(func() {
			a.addAccountButton(c)
		})
		return
	}
	a.laneClock(c, board)
	a.laneTable(c, board)
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

func filterBoard(board tasks.Board, platform string) tasks.Board {
	if platform == "" {
		return board
	}
	out := tasks.Board{}
	for _, ln := range board.Lanes {
		if ln.Platform != platform {
			continue
		}
		if ln.State == tasks.LaneRunning {
			out.Active++
		}
		out.Lanes = append(out.Lanes, ln)
	}
	return out
}

func (a *App) laneHeader(c *ui.Context) {
	tok := a.look
	board := filterBoard(a.accountBoard(c.Now()), a.stagePlatform())
	running, idle := 0, 0
	for _, ln := range board.Lanes {
		switch ln.State {
		case tasks.LaneRunning:
			running++
		case tasks.LaneIdle, tasks.LaneGap:
			idle++
		}
	}
	var line string
	switch {
	case a.agyPauseLine(c.Now()) != "":
		line = a.agyPauseLine(c.Now())
	case running > 0:
		line = fmt.Sprintf("%d 个账号出图中", running)
	case idle == 0:
		line = "暂无可用账号"
	default:
		line = fmt.Sprintf("%d 个账号空闲", idle)
	}
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c.Key("lane-header"), line).FontSize(tok.body).Bold().SingleLine().Tooltip(line).Grow(1).MinWidth(0).
			ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseOut})
		a.addAccountButton(c)
	})
}

func (a *App) addAccountButton(c *ui.Context) {
	ui.Button(c, "+ 添加账号").Shrink(0).FontSize(12).Disabled(a.running).OnClick(func() {
		a.addAccountHere()
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

func (a *App) laneAction(c *ui.Context, ln tasks.AccountLane) {
	label := ln.Action
	id := ln.ID
	ui.Button(c.Key("act"), label).FontSize(12).Height(24).Padding(0, 8).Radius(8).Shrink(0).OnClick(func() { a.openLane(id) })
}

func (a *App) laneChip(c *ui.Context, ln tasks.AccountLane) {
	tok := a.look
	fg, bg := laneInk(tok, ln.State)
	text := laneLabel(ln.State)
	if ln.State == tasks.LaneGap {
		text = fmt.Sprintf("等待间隔 %d", gapSeconds(ln.NextReadyAt, c.Now()))
	}
	pill := ui.Row(c.Key("chip")).Padding(1, 7).Radius(999).Shrink(0).AlignItems(ui.Center)
	pill.Background(pill.AnimateColor("bg", bg, motionEaseFast))
	pill.Children(func() {
		ui.Text(c.Key("chip-label"), text).FontSize(tok.caption).TextColor(fg).SingleLine().
			ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseFast})
	})
}

func laneLabel(state tasks.LaneState) string {
	switch state {
	case tasks.LaneRunning:
		return "出图中"
	case tasks.LaneCaptcha:
		return "需要人工验证"
	case tasks.LaneLogin:
		return "需登录"
	case tasks.LaneIdle:
		return "空闲"
	default:
		return string(state)
	}
}

func laneInk(tok tokens, state tasks.LaneState) (fg, bg ui.Color) {
	var base ui.Color
	var wash float32
	switch state {
	case tasks.LaneRunning:
		base, wash = tok.accent, 0.14
	case tasks.LaneGap, tasks.LaneCooling, tasks.LaneCaptcha:
		base, wash = tok.warning, 0.16
	case tasks.LaneFull, tasks.LaneLogin, tasks.LaneQuota:
		base, wash = tok.danger, 0.14
	default:
		return tok.muted, tok.text.Alpha(0.08)
	}
	fg = base
	// 浅色里状态色直接写在浅底上，12px 不到 4.5:1。往正文色收一点，色相还在。
	if !tok.dark {
		fg = base.Mix(tok.text, 0.35)
	}
	return fg, base.Alpha(wash)
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

// laneCaption 车道上正在出的那一行：有提示词就用提示词，否则用文件名。
func laneCaption(prompt, image string) string {
	if line := shortLine(prompt, 22); line != "" {
		return line
	}
	if image != "" {
		if line := shortLine(filepath.Base(image), 22); line != "" {
			return line
		}
	}
	return "正在出图"
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

func platformMark(c *ui.Context, platform string, tok tokens) {
	platformIcon(c, platform, 18, tok)
}

func shotBadge(platform, name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == platform || name == "扩展" {
		return platform
	}
	return shortLine(name, 4)
}

func (a *App) openLane(id int64) {
	for _, row := range a.configuredBrowsers() {
		if row.ID == id && row.Platform == agy.Platform {
			a.openAgyLane(row)
			return
		}
	}
	row := a.showBrowserAccount(id)
	logging.Log.Info("请在「%s」的网页里处理", accountLabel(row))
	go func(b store.Browser) {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_ = a.ensureAccountPage(ctx, b)
	}(row)
}
