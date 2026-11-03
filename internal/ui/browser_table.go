package ui

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *App) browserEnvironmentTable(c *ui.Context, active map[int64]bool) {
	palette := c.Theme()
	tableTheme := *palette
	tableTheme.Accent = palette.Accent.Mix(palette.Background, 0.90)
	tableTheme.AccentText = palette.Text
	actionInk := palette.Accent
	if !palette.Dark {
		tableTheme.Success = palette.Success.Mix(palette.Text, 0.35)
		tableTheme.Warning = palette.Warning.Mix(palette.Text, 0.35)
		actionInk = palette.Accent.Mix(palette.Text, 0.35)
	}
	w, _ := c.Size()
	columns := []ui.TableColumn{{ID: "name", Title: "环境名称", MinWidth: 200, Fixed: true}}
	if w >= 900 {
		columns = append([]ui.TableColumn{{ID: "id", Title: "编号", Width: 64, Fixed: true}}, columns...)
	}
	columns = append([]ui.TableColumn{{ID: "select", Title: "选择", Width: 56, Fixed: true}}, columns...)
	columns = append(columns,
		ui.TableColumn{ID: "platform", Title: "服务商", Width: 88, Fixed: true},
		ui.TableColumn{ID: "status", Title: "环境状态", Width: 108, Fixed: true})
	if w >= 1100 {
		columns = append(columns, ui.TableColumn{ID: "proxy", Title: "代理", Width: 180, Fixed: true})
	}
	if w >= 1000 {
		columns = append(columns, ui.TableColumn{ID: "usage", Title: "今日用量", Width: 88, Fixed: true})
	}
	columns = append(columns, ui.TableColumn{ID: "actions", Title: "操作", Width: 146, Fixed: true})
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
				ui.Text(c, fmt.Sprintf("%03d", b.ID)).FontSize(12).FontFeatures("tnum").TextColor(c.Theme().TextMuted).SingleLine()
			case "name":
				a.accountRow(c, b)
			case "platform":
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					platformIcon(c, b.Platform, 16, a.look)
					ui.Text(c, b.Platform).FontSize(12).SingleLine()
				})
			case "status":
				state := accountHealthLabel(b, active[b.ID], accountSignedIn(b, a.loginShot(b.ID)))
				ink := tableTheme.Warning
				switch state {
				case "就绪":
					ink = palette.TextMuted
					if b.ExtAccount == "" {
						state = "未启动"
					}
				case "运行中":
					ink = tableTheme.Success
				case "使用中":
					ink = actionInk
				}
				if op, pending := a.browserPending[b.ID]; pending {
					state = "启动中"
					if op.stopping {
						state = "停止中"
					}
					ink = palette.TextMuted
				}
				ui.Column(c).Gap(3).Children(func() {
					ui.Text(c, state).FontSize(12).TextColor(ink).SingleLine()
					if b.LastError != "" && !b.Usable(time.Now()) && !staleLoginCopy(b, accountSignedIn(b, a.loginShot(b.ID))) {
						ui.Text(c, b.LastError).FontSize(12).TextColor(palette.TextMuted).SingleLine().Tooltip(b.LastError)
					}
				})
			case "proxy":
				host, protocol := browserProxySummary(b.Proxy)
				if b.ExtAccount != "" {
					host, protocol = "由扩展管理", "外部浏览器环境"
				}
				ui.Column(c).Gap(3).Children(func() {
					ui.Text(c, host).FontSize(12).SingleLine().Tooltip(host)
					ui.Text(c, protocol).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine()
				})
			case "usage":
				ui.Text(c, fmt.Sprintf("%d / %d", b.UsedToday(time.Now()), a.shownDailyCap())).FontSize(12).FontFeatures("tnum").SingleLine()
			case "actions":
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					label := "启动"
					if b.IsRunning {
						label = "停止"
					}
					if op, pending := a.browserPending[b.ID]; pending {
						label = "启动中"
						if op.stopping {
							label = "停止中"
						}
					}
					button := ui.Button(c, label).Width(62).Height(30).Padding(0, 8).FontSize(12).Label(label + " " + b.Name).
						Disabled(a.running || len(a.browserPending) > 0 || active[b.ID] || b.ExtAccount != "")
					if !b.IsRunning {
						button.Background(palette.Accent.Alpha(0.10)).TextColor(actionInk)
					}
					button.OnClick(func() {
						a.selectBrowserID(b.ID)
						if b.IsRunning {
							a.stopBrowser()
						} else {
							a.launchBrowser()
						}
					})
					ui.Button(c, "配置").Width(56).Height(30).FontSize(12).Label("配置 " + b.Name).OnClick(func() { a.selectBrowserID(b.ID) })
				})
			}
		}).Grow(1).MinHeight(120).FillWidth().Background(c.Theme().Background).Border(1, c.Theme().Border).Radius(8).Label("浏览器环境列表")
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
