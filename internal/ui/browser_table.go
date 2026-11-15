package ui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/egoist/mygo/ui"
)

func browserStatusTone(state string) string {
	switch state {
	case "运行中":
		return "success"
	case "使用中", "启动中", "停止中":
		return "accent"
	case "就绪", "未启动":
		return "neutral"
	case "冷却中", "需要人工验证":
		return "warning"
	case "未登录", "已用尽", "已禁用":
		return "danger"
	default:
		return "warning"
	}
}

func (a *App) browserEnvironmentTable(c *ui.Context, active map[int64]bool) {
	refreshAtMidnight(c)
	tok := a.uiTokens(c)
	palette := c.Theme()
	tableTheme := *palette
	tableTheme.Accent = tok.accent.Mix(tok.card, 0.90)
	tableTheme.AccentText = tok.text
	w, _ := c.Size()
	columns := []ui.TableColumn{{ID: "name", Title: "环境名称", MinWidth: 190, Fixed: true}}
	if w >= 900 {
		columns = append([]ui.TableColumn{{ID: "id", Title: "编号", Width: 56, Fixed: true}}, columns...)
	}
	columns = append([]ui.TableColumn{{ID: "select", Title: "选择", Width: 50, Fixed: true}}, columns...)
	columns = append(columns,
		ui.TableColumn{ID: "platform", Title: "服务商", Width: 86, Fixed: true},
		ui.TableColumn{ID: "status", Title: "环境状态", Width: 112, Fixed: true})
	if w >= 1100 {
		columns = append(columns, ui.TableColumn{ID: "proxy", Title: "代理", Width: 172, Fixed: true})
	}
	if w >= 1000 {
		columns = append(columns, ui.TableColumn{ID: "usage", Title: "今日用量", Width: 84, Fixed: true})
	}
	columns = append(columns, ui.TableColumn{ID: "actions", Title: "操作", Width: 84, Fixed: true})
	a.browserList.Selected = &a.browserSel
	a.browserList.Selection = &a.browserChecked
	a.browserList.Key = func(i int) any { return a.browserRows[i].ID }
	a.browserList.Label = func(i int) string { return a.browserRows[i].Name }
	ui.Themed(c, &tableTheme).Grow(1).MinHeight(120).FillWidth().Children(func() {
		ui.Table(c.Key("browser-environments"), &a.browserList, columns, len(a.browserRows), func(row, col int) {
			b := a.browserRows[row]
			switch columns[col].ID {
			case "select":
				ui.Themed(c, palette).Children(func() {
					checked := a.browserChecked.Has(b.ID)
					if ui.Checkbox(c, &checked, "").Label("选择环境 " + b.Name).Changed() {
						if checked {
							a.browserChecked.Add(b.ID)
							a.selectBrowserID(b.ID)
						} else {
							a.browserChecked.Remove(b.ID)
						}
					}
				})
			case "id":
				ui.Text(c, fmt.Sprintf("%03d", b.ID)).FontSize(tok.body).FontFeatures("tnum").TextColor(tok.muted).SingleLine()
			case "name":
				a.accountRow(c, b)
			case "platform":
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					platformIcon(c, b.Platform, 16, tok)
					ui.Text(c, b.Platform).FontSize(tok.body).TextColor(tok.text).SingleLine()
				})
			case "status":
				state := accountHealthLabel(b, active[b.ID], accountSignedIn(b, a.loginShot(b.ID)), c.Now())
				if state == "就绪" && b.ExtAccount == "" {
					state = "未启动"
				}
				if op, pending := a.browserPending[b.ID]; pending {
					state = "启动中"
					if op.stopping {
						state = "停止中"
					}
				}
				ui.Column(c).Gap(3).Children(func() {
					uiBadge(c, fmt.Sprintf("browser-state-%d", b.ID), state, browserStatusTone(state), tok)
					if b.LastError != "" && !b.Usable(c.Now()) && !staleLoginCopy(b, accountSignedIn(b, a.loginShot(b.ID))) {
						ui.Text(c, b.LastError).FontSize(tok.caption).TextColor(tok.muted).SingleLine().Tooltip(b.LastError)
					}
				})
			case "proxy":
				host, protocol := browserProxySummary(b.Proxy)
				if b.ExtAccount != "" {
					host, protocol = "由扩展管理", "外部浏览器环境"
				}
				ui.Column(c).Gap(2).Children(func() {
					ui.Text(c, host).FontSize(tok.body).TextColor(tok.text).SingleLine().Tooltip(host)
					ui.Text(c, protocol).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
				})
			case "usage":
				used, capN := b.UsedToday(c.Now()), a.shownDailyCapFor(b.Platform)
				label := fmt.Sprintf("%d / 不限", used)
				ink := tok.text
				if used == 0 {
					ink = tok.muted
				}
				if capN > 0 {
					label = fmt.Sprintf("%d / %d", used, capN)
					if used >= capN {
						ink, _, _ = semanticToneColors(tok.dark, tok.text, tok.danger, 0.14)
					}
				}
				ui.Text(c, label).FontSize(tok.body).FontFeatures("tnum").TextColor(ink).SingleLine()
			case "actions":
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					label := "启动"
					ico := iconPlay
					activeAct := false
					if b.IsRunning {
						label = "停止"
						ico = iconStop
						activeAct = true
					}
					if op, pending := a.browserPending[b.ID]; pending {
						ico = iconClock
						label = "启动中"
						if op.stopping {
							label = "停止中"
						}
					}
					disabled := a.running || len(a.browserPending) > 0 || active[b.ID] || b.ExtAccount != ""
					uiIconBtn(c, "act", label+" "+b.Name, label, ico, 22, !b.IsRunning && !disabled && !activeAct, false, tok).
						Disabled(disabled).OnClick(func() {
						a.selectBrowserID(b.ID)
						if b.IsRunning {
							a.stopBrowser()
						} else {
							a.launchBrowser()
						}
					})
					uiIconBtn(c, "cfg", "配置 "+b.Name, "配置代理与参数", iconSliders, 22, false, false, tok).
						OnClick(func() { a.selectBrowserID(b.ID) })
				})
			}
		}).Grow(1).MinHeight(120).FillWidth().Background(tok.card).Border(1, tok.border).Radius(tok.radiusM).Label("浏览器环境列表")
	})
}

func browserProxySummary(raw string) (string, string) {
	if raw == "" {
		return "跟随全局", "使用全局代理设置"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "代理待检查", "请重新配置"
	}
	return u.Host, strings.ToUpper(u.Scheme) + " · 独立代理"
}
