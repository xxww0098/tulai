package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// 账号表每列的宽度。账号和当前任务分剩下的宽度。
const (
	laneColState = 74
	laneColQuota = 132
	laneColOps   = 96
	laneRowH     = 34
)

func laneColumns(winW float32, mail bool) (state, quota, ops, accountGrow, taskGrow float32) {
	if mail {
		if winW < 1150 {
			return 0, 126, 72, 1, 0
		}
		return 64, laneColQuota, 72, 2, 1
	}
	if winW < 1150 {
		return 0, 128, laneColOps, 1, 0
	}
	return laneColState, laneColQuota, laneColOps, 2, 1
}

// laneTable 一个账号一行：账号、状态、今日或额度、当前任务、操作。
func (a *App) laneTable(c *ui.Context, board tasks.Board) {
	tok := a.look
	winW, winH := c.Size()
	mail := a.stagePlatform() == "Antigravity"
	compact := winW < 1150
	stateW, quotaW, opsW, accountGrow, taskGrow := laneColumns(winW, mail)
	reveal := motionReveal()
	// 展开历史时立即让出位置，避免卡片挡住恢复按钮。
	reveal.Position = false
	reveal.Colors = true
	ui.Column(c.Key("lane-table")).Label("账号表格").FillWidth().Gap(4).Padding(6, 9).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border).Shadow(0, 1, 2, 0, tok.shadow).Transition(reveal).Children(func() {
		a.laneHeader(c)
		if mail {
			a.agyLoginPanel(c)
			a.agyChooserPanel(c)
		}
		if len(board.Lanes) == 0 {
			if !mail || (a.agyChooser == nil && a.agyLogin == nil) {
				hint := "添加账号后，在这里查看状态与今日用量。"
				if mail {
					hint = "添加账号后，在这里查看状态与 Gemini 用量。"
				}
				ui.Text(c, hint).FontSize(tok.caption).TextColor(tok.muted).FillWidth().Padding(12, 0).BorderWidth(1, 0, 0, 0).BorderColor(tok.border)
			}
			return
		}
		ui.Row(c.Key("lane-head")).FillWidth().Gap(6).Padding(2, 0, 4, 0).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
			head := func(label string) ui.Element {
				return ui.Text(c, label).FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).SingleLine()
			}
			head("账号").Grow(accountGrow).Basis(0).MinWidth(0)
			if !compact {
				head("状态").Width(stateW).Shrink(0)
			}
			quotaLabel := "今日"
			if mail {
				quotaLabel = "Gemini 已用"
			}
			head(quotaLabel).Width(quotaW).Shrink(0)
			if !compact {
				head("当前任务").Grow(taskGrow).Basis(0).MinWidth(0)
			}
			ui.Row(c).Width(opsW).Shrink(0).Justify(ui.End).Children(func() { head("操作") })
		})
		capN := 3
		if winH < 640 {
			capN = 2
		}
		if mail && winH < 760 && (a.agyChooser != nil || a.agyLogin != nil) {
			capN = 1
		}
		rowsH := float32(len(board.Lanes) * laneRowH)
		if len(board.Lanes) > capN {
			rowsH = float32(laneRowH * capN)
		}
		ui.Scroll(c.Key("lane-rows")).Label("账号列表").FillWidth().Height(rowsH).MaxHeight(float32(laneRowH*capN) + 4).Children(func() {
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
	winW, _ := c.Size()
	isAgy := ln.Platform == agy.Platform
	compact := winW < 1150
	row := ui.Row(c.Key(ln.ID)).FillWidth().Height(laneRowH).Gap(6).Padding(3, 0).AlignItems(ui.Center).
		Radius(tok.radiusS).Label("车道"+ln.Name).Transition(laneEnter(i)).BorderWidth(0, 0, 1, 0).BorderColor(tok.border.Alpha(0.6))
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
	stateW, quotaW, opsW, accountGrow, taskGrow := laneColumns(winW, isAgy)
	row.Children(func() {
		ui.Row(c).Grow(accountGrow).Basis(0).MinWidth(0).Gap(5).AlignItems(ui.Center).Children(func() {
			on := !ln.Disabled
			if ui.Switch(c.Key("enabled"), &on).Size(26, 15).Radius(7.5).Label("启用账号 " + ln.Name).Tooltip("禁用后不再接新任务，当前任务会完成").Changed() {
				var err error
				switch {
				case a.engine != nil && a.store != nil:
					err = a.engine.SetAccountDisabled(ln.ID, !on)
				case a.store != nil:
					err = a.store.SetBrowserDisabled(ln.ID, !on)
				}
				if err != nil {
					logging.Log.Errorf("更改账号状态失败: %v", err)
				} else {
					for i := range a.browserRows {
						if a.browserRows[i].ID == ln.ID {
							a.browserRows[i].Disabled = !on
						}
					}
				}
			}
			if a.renameID == ln.ID {
				a.accountNameField(c, store.Browser{ID: ln.ID, Name: ln.Name})
				return
			}
			title := laneAccountTitle(ln)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, title).FontSize(tok.body).FontWeight(500).SingleLine().MinWidth(0).Tooltip(title)
				if compact {
					ui.Row(c).FillWidth().MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
						a.laneState(c, ln)
						if ln.Job != nil || ln.State == tasks.LaneRunning {
							ui.Column(c).Grow(1).MinWidth(0).Children(func() { a.laneTask(c, ln, false) })
						} else if ln.Email != "" && ln.Name != ln.Email {
							ui.Text(c, ln.Name).FontSize(tok.caption).TextColor(tok.muted).Grow(1).SingleLine().MinWidth(0).Tooltip(ln.Name)
						}
					})
				} else if ln.Email != "" && ln.Name != ln.Email {
					ui.Text(c, ln.Name).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0).Tooltip(ln.Name)
				}
			})
		})
		if !compact {
			ui.Row(c).Width(stateW).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() { a.laneState(c, ln) })
		}
		if ln.Platform == agy.Platform {
			ui.Column(c).Width(quotaW).Shrink(0).Gap(4).Justify(ui.Center).Children(func() {
				a.agyQuotaCell(c, ln.ID)
			})
		} else {
			used, capN, frac, fill := laneQuotaFill(tok, ln)
			label, tip := fmt.Sprintf("%d/%d", used, capN), "今日已用次数 / 每日上限"
			if capN <= 0 {
				label, tip = fmt.Sprintf("%d/不限", used), "今日已用次数 / 每日上限（不限制）"
			}
			ui.Column(c).Width(quotaW).Shrink(0).Gap(2).Justify(ui.Center).Children(func() {
				ui.Row(c).FillWidth().Height(14).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Text(c.Key("used"), label).FontSize(tok.caption).FontWeight(600).
						FontFeatures("tnum").SingleLine().Shrink(0).Tooltip(tip).
						ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseOut})
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						quotaBar(c, "quota", frac, fill, tok.text.Alpha(0.12))
					})
				})
				ui.Row(c).Height(14).Gap(6).AlignItems(ui.Center).Children(func() {
					for _, stat := range []struct {
						label string
						count int
						color ui.Color
						icon  *ui.SVG
					}{{"成功", ln.SuccessToday, tok.success, iconCheck}, {"失败", ln.FailedToday, tok.danger, iconX}} {
						label := fmt.Sprintf("今日%s %d", stat.label, stat.count)
						ui.Row(c.Key(stat.label)).Shrink(0).Gap(2).AlignItems(ui.Center).Label(label).
							Tooltip(label + "（只统计已完成任务，更新前的记录未计入）").Children(func() {
							ui.Icon(c, stat.icon).FontSize(14).TextColor(stat.color)
							ui.Text(c, fmt.Sprint(stat.count)).FontSize(tok.caption).
								FontFeatures("tnum").TextColor(tok.text).SingleLine()
						})
					}
				})
			})
		}
		if !compact {
			ui.Row(c).Grow(taskGrow).Basis(0).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() { a.laneTask(c, ln, true) })
		}
		ui.Row(c).Width(opsW).Shrink(0).Gap(6).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			a.laneBrowserButton(c, ln)
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

func (a *App) laneState(c *ui.Context, ln tasks.AccountLane) {
	if ln.State != tasks.LaneDisabled && a.engine != nil && a.engine.AccountManual(ln.ID) {
		ui.Text(c, "手动接管").FontSize(a.look.caption).TextColor(a.look.accent).SingleLine()
	} else {
		a.laneChip(c, ln)
	}
}

func (a *App) laneTask(c *ui.Context, ln tasks.AccountLane, showElapsed bool) {
	tok := a.look
	ui.Row(c.Key("task-caption")).FillWidth().MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
		switch {
		case ln.Job != nil:
			line := laneCaption(ln.Job.Prompt, ln.Job.Image)
			tip := line
			if !showElapsed {
				tip += " · " + elapsedLabel(c.Now().Sub(ln.Job.Started))
			}
			ui.Text(c, line).FontSize(tok.caption).SingleLine().MinWidth(0).Shrink(1).Tooltip(tip)
			if showElapsed {
				ui.Text(c.Key("elapsed"), elapsedLabel(c.Now().Sub(ln.Job.Started))).FontSize(tok.caption).FontFeatures("tnum").TextColor(tok.muted).Shrink(0).SingleLine().ContentTransition(ui.ContentTransition{Numeric: true, Animation: &motionEaseFast})
			}
		case ln.State == tasks.LaneRunning:
			text := "同一窗口正在出图"
			if ln.Platform == agy.Platform {
				text = "正在出图"
			}
			ui.Text(c, text).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
		default:
			ui.Text(c, "—").FontSize(tok.caption).TextColor(tok.muted)
		}
	})
}

func (a *App) laneRenameButton(c *ui.Context, ln tasks.AccountLane) {
	uiIconBtn(c, "pen", "改名 "+ln.Name, "改名", iconPen, 22, false, false, a.look).
		Disabled(a.running).OnClick(func() {
		a.renameID, a.renameDraft, a.renameFocus = ln.ID, ln.Name, true
	})
}

// agyMeterLevels 把已用比例画成绿、橙、红。80% 起变橙，用尽变红。
// 颜色只是辅助：读屏读不到悬停，进度条的名字就是同一句用量。
var agyMeterLevels = &ui.MeterLevels{Warning: 0.8, Critical: 1}

// agyQuotaCell 展示 Gemini 的两个窗口及已用百分比，不放今日次数。
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
	ui.Column(c.Key("bars")).FillWidth().Gap(2).Children(func() {
		for _, item := range []struct {
			key, label, tip string
			remaining       float64
		}{
			{"h5", "5小时", fiveTip, five.Remaining},
			{"week", "7天", weekTip, week.Remaining},
		} {
			ui.Row(c.Key(item.key + "-row")).FillWidth().Height(16).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, item.label).FontSize(tok.caption).Width(36).Shrink(0).SingleLine().TextColor(tok.muted)
				ui.Meter(c.Key(item.key), agy.UsedFraction(item.remaining), 0, 1, agyMeterLevels).
					Grow(1).MinWidth(0).Height(7).Label(item.tip).Tooltip(item.tip)
				ui.Text(c, fmt.Sprintf("%d%%", agy.UsedPercent(item.remaining))).FontSize(tok.caption).FontWeight(600).FontFeatures("tnum").Width(40).Shrink(0).SingleLine().TextAlign(ui.End)
			})
		}
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
	used = max(ln.UsedToday, 0)
	capN = max(ln.Cap, 0)
	if capN == 0 {
		return used, 0, 0, tok.accent
	}
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
