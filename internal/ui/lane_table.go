package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// 账号表每列的宽度。账号和当前任务分剩下的宽度。
const (
	laneColState    = 96
	laneColQuota    = 92
	laneColAgyQuota = 96
	laneColOps      = 104
	laneRowH        = 30
	// laneTableMaxH 是高窗口大约 8 行的上限。矮窗口用 laneRowCap，给结果区留高度。
	laneTableMaxH = laneRowH*8 + 8
)

// laneRowCap 矮窗口少露几行，账号在表里滚，出图结果才有高度。
func laneRowCap(winH float32) int {
	if winH < 760 {
		return 5
	}
	return 8
}

// laneTable 一个账号一行：账号、状态、今日或额度、当前任务、操作。
func (a *App) laneTable(c *ui.Context, board tasks.Board) {
	tok := a.look
	ui.Column(c.Key("lane-table")).FillWidth().Gap(4).Padding(6, 8).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border).Transition(motionReveal()).Children(func() {
		a.laneHeader(c)
		mail := a.stagePlatform() == "Antigravity"
		ui.Row(c.Key("lane-head")).FillWidth().Gap(8).Padding(2, 6, 4, 6).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
			head := func(label string) ui.Element {
				return ui.Text(c, label).FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).SingleLine()
			}
			if mail {
				// Antigravity 账号列直接显示邮箱，不再单独开邮箱列。
				head("账号").Grow(2).Basis(0).MinWidth(0)
				head("状态").Width(88).Shrink(0)
				head("额度").Width(laneColAgyQuota).Shrink(0)
				head("当前任务").Grow(1).Basis(0).MinWidth(0)
				ui.Row(c).Width(88).Shrink(0).Justify(ui.End).Children(func() { head("操作") })
				return
			}
			head("账号").Grow(2).Basis(0).MinWidth(0)
			head("状态").Width(laneColState).Shrink(0)
			head("今日").Width(laneColQuota).Shrink(0)
			head("当前任务").Grow(3).Basis(0).MinWidth(0)
			ui.Row(c).Width(laneColOps).Shrink(0).Justify(ui.End).Children(func() { head("操作") })
		})
		_, winH := c.Size()
		capN := laneRowCap(winH)
		rowsH := float32(len(board.Lanes)) * laneRowH
		if len(board.Lanes) > capN {
			rowsH = laneRowH * float32(capN)
		}
		ui.Scroll(c.Key("lane-rows")).Label("账号列表").FillWidth().Height(rowsH).MaxHeight(laneRowH*float32(capN) + 8).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for i, ln := range board.Lanes {
					a.laneRow(c, ln, i)
				}
			})
		})
	})
}

func (a *App) laneRow(c *ui.Context, ln tasks.AccountLane, i int) {
	tok := a.look
	row := ui.Row(c.Key(ln.ID)).FillWidth().Height(laneRowH).Gap(8).Padding(0, 6).AlignItems(ui.Center).
		Radius(tok.radiusS).Label("车道" + ln.Name).Transition(laneEnter(i))
	if i%2 == 1 {
		row.Background(tok.text.Alpha(0.035))
	}
	if row.Hovered() {
		row.Background(tok.sunken)
	}
	if ln.State == tasks.LaneRunning && !c.Preferences().ReduceMotion {
		row.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.AnimationFrame()
			phase := float32(p.Now().UnixMilli()%1400) / 1400
			wave := float32(0.5 + 0.5*math.Sin(float64(phase)*2*math.Pi))
			p.Fill(ui.Rect{X: r.X, Y: r.Y + 6, W: 2, H: r.H - 12}, tok.accent.Alpha(0.35+0.5*wave), 1)
		})
	}
	mail := ln.Platform == "Antigravity"
	stateW, quotaW, opsW := float32(laneColState), float32(laneColQuota), float32(laneColOps)
	accountGrow := float32(2)
	taskGrow := float32(3)
	if mail {
		stateW, quotaW, opsW = 88, laneColAgyQuota, 88
		taskGrow = 1
	}
	row.Children(func() {
		ui.Row(c).Grow(accountGrow).Basis(0).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			platformMark(c, ln.Platform, tok)
			if a.renameID == ln.ID {
				a.accountNameField(c, store.Browser{ID: ln.ID, Name: ln.Name})
				return
			}
			title := laneAccountTitle(ln)
			ui.Text(c, title).FontSize(tok.body).SingleLine().MinWidth(0).Tooltip(title)
		})
		ui.Row(c).Width(stateW).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
			a.laneChip(c, ln)
		})
		gap := float32(2)
		if ln.Platform == agy.Platform {
			gap = 4
		}
		ui.Column(c).Width(quotaW).Shrink(0).Gap(gap).Justify(ui.Center).Children(func() {
			if ln.Platform == agy.Platform {
				a.agyQuotaCell(c, ln.ID)
				return
			}
			used, capN, frac, fill := laneQuotaFill(tok, ln)
			ui.Text(c.Key("used"), fmt.Sprintf("%d/%d", used, capN)).FontSize(tok.caption).
				FontFeatures("tnum").SingleLine().
				ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseOut})
			quotaBar(c, "quota", frac, fill, tok.text.Alpha(0.12))
		})
		ui.Row(c).Grow(taskGrow).Basis(0).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			switch {
			case ln.Job != nil:
				line := laneCaption(ln.Job.Prompt, ln.Job.Image)
				ui.Text(c, line).FontSize(tok.caption).SingleLine().MinWidth(0).Shrink(1).Tooltip(line)
				ui.Text(c.Key("elapsed"), elapsedLabel(c.Now().Sub(ln.Job.Started))).FontSize(tok.caption).
					FontFeatures("tnum").TextColor(tok.muted).Shrink(0).SingleLine().
					ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseFast})
			case ln.State == tasks.LaneRunning:
				ui.Text(c, "同一窗口正在出图").FontSize(tok.caption).TextColor(tok.muted).SingleLine()
			default:
				ui.Text(c, "—").FontSize(tok.caption).TextColor(tok.muted)
			}
		})
		ui.Row(c).Width(opsW).Shrink(0).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if ln.State == tasks.LaneGap {
				gapRing(c, ln, tok)
			}
			if ln.Action != "" {
				a.laneAction(c, ln)
			}
			if !ln.Extension {
				a.laneRenameButton(c, ln)
			}
		})
	})
}

func (a *App) laneRenameButton(c *ui.Context, ln tasks.AccountLane) {
	tok := a.look
	pen := ui.ButtonBase(c.Key("pen")).Size(24, 24).Radius(6).Shrink(0).Center().
		Label("改名 " + ln.Name).Tooltip("改名").Cursor(ui.CursorPointer)
	ink := tok.muted
	switch {
	case pen.Pressed():
		pen.Background(tok.text.Alpha(0.12))
		ink = tok.text
	case pen.Hovered() || pen.FocusVisible():
		pen.Background(tok.sunkenHi)
		ink = tok.text
	}
	if pen.FocusVisible() {
		pen.Border(1, tok.accent)
	}
	if pen.Clicked() && !a.running {
		a.renameID, a.renameDraft, a.renameFocus = ln.ID, ln.Name, true
	}
	pen.Children(func() {
		ui.Icon(c, iconPen).FontSize(12).TextColor(ink)
	})
}

// agyMeterLevels 把已用比例画成绿、橙、红。80% 起变橙，用尽变红。
// 颜色只是辅助：读屏读不到悬停，进度条的名字就是同一句用量。
var agyMeterLevels = &ui.MeterLevels{Warning: 0.8, Critical: 1}

// agyQuotaCell 只放两根额度条，或一条说明原因的破折号。不放今日次数。
func (a *App) agyQuotaCell(c *ui.Context, id int64) {
	tok := a.look
	view := a.agyUsageView(id)
	if view.State != agy.UsageOK {
		reason := view.Reason
		ui.Text(c.Key("dash"), "—").Role(ui.RoleStatus).FontSize(tok.caption).TextColor(tok.muted).
			SingleLine().Label(reason).Tooltip(reason)
		return
	}
	now := c.Now()
	five := view.Usage.FiveHour
	week := view.Usage.Weekly
	fiveTip := agy.WindowTip(agy.Window5h, five.Remaining, five.Reset, now)
	weekTip := agy.WindowTip(agy.WindowWeek, week.Remaining, week.Reset, now)
	ui.Column(c.Key("bars")).FillWidth().Gap(3).Children(func() {
		ui.Meter(c.Key("h5"), agy.UsedFraction(five.Remaining), 0, 1, agyMeterLevels).
			FillWidth().MinWidth(0).Height(7).Label(fiveTip).Tooltip(fiveTip)
		ui.Meter(c.Key("week"), agy.UsedFraction(week.Remaining), 0, 1, agyMeterLevels).
			FillWidth().MinWidth(0).Height(7).Label(weekTip).Tooltip(weekTip)
	})
}

// laneAccountTitle Antigravity 优先显示 Google 邮箱；没有邮箱才用文件夹名。
func laneAccountTitle(ln tasks.AccountLane) string {
	if ln.Platform == agy.Platform {
		if email := strings.TrimSpace(ln.Email); email != "" {
			return email
		}
	}
	return ln.Name
}

// laneQuotaFill 是今日用量和进度条颜色：快满变橙，满了变灰。
func laneQuotaFill(tok tokens, ln tasks.AccountLane) (used, capN int, frac float64, fill ui.Color) {
	capN = max(ln.Cap, 1)
	used = max(ln.UsedToday, 0)
	frac = min(float64(used)/float64(capN), 1)
	fill = tok.accent
	switch {
	case used >= capN:
		// 满额用能看清的中性色。浅灰贴在白底上几乎看不见。
		fill = tok.muted
	case used*5 >= capN*4:
		fill = tok.warning
	}
	return
}
