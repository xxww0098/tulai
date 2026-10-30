package ui

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// 账号表每列的宽度。账号和当前任务分剩下的宽度。
const (
	laneColState = 96
	laneColQuota = 92
	laneColOps   = 104
	laneRowH     = 30
	// laneTableMaxH 大约放 8 行，账号再多就在表里滚动，不把进度和日志挤出窗口。
	laneTableMaxH = laneRowH*8 + 8
)

// laneTable 一个账号一行：账号、状态、今日、当前任务、操作。
func (a *App) laneTable(c *ui.Context, board tasks.Board) {
	tok := a.look
	ui.Column(c.Key("lane-table")).FillWidth().Gap(6).Padding(8, 10).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border).Transition(motionReveal()).Children(func() {
		a.laneHeader(c)
		ui.Row(c.Key("lane-head")).FillWidth().Gap(8).Padding(2, 6, 4, 6).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
			head := func(label string) ui.Element {
				return ui.Text(c, label).FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).SingleLine()
			}
			head("账号").Grow(2).Basis(0).MinWidth(0)
			head("状态").Width(laneColState).Shrink(0)
			head("今日").Width(laneColQuota).Shrink(0)
			head("当前任务").Grow(3).Basis(0).MinWidth(0)
			ui.Row(c).Width(laneColOps).Shrink(0).Justify(ui.End).Children(func() { head("操作") })
		})
		ui.Scroll(c.Key("lane-rows")).FillWidth().MaxHeight(laneTableMaxH).Children(func() {
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
	row.Children(func() {
		ui.Row(c).Grow(2).Basis(0).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			platformMark(c, ln.Platform, tok)
			if a.renameID == ln.ID {
				a.accountNameField(c, store.Browser{ID: ln.ID, Name: ln.Name})
				return
			}
			ui.Text(c, ln.Name).FontSize(tok.body).SingleLine().MinWidth(0).Tooltip(ln.Name)
		})
		ui.Row(c).Width(laneColState).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
			a.laneChip(c, ln)
		})
		ui.Column(c).Width(laneColQuota).Shrink(0).Gap(2).Justify(ui.Center).Children(func() {
			used, capN, frac, fill := laneQuotaFill(tok, ln)
			ui.Text(c.Key("used"), fmt.Sprintf("%d/%d", used, capN)).FontSize(tok.caption).
				FontFeatures("tnum").SingleLine().
				ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseOut})
			quotaBar(c, "quota", frac, fill, tok.text.Alpha(0.12))
		})
		ui.Row(c).Grow(3).Basis(0).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
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
		ui.Row(c).Width(laneColOps).Shrink(0).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
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
