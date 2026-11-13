package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

const (
	topbarH           float32 = 34
	sidebarExpandedW  float32 = 134
	sidebarCollapsedW float32 = 42
	sidebarFoldW      float32 = 840
)

type navItem struct {
	page  int
	label string
	sub   string
	icon  *ui.SVG
}

var saasNavItems = []navItem{
	{pageHome, "图片生成", "生图 / 洗图 / 裂变工作台", iconImages},
	{pageDedup, "去重", "按相似图分组清理", iconDedup},
	{pageBrowser, "浏览器", "Camoufox 实例与账号绑定", iconGlobe},
	{pagePrompts, "提示词", "词库与常用模板", iconPen},
	{pageDownload, "下载", "批量抓取原图", iconDownload},
	{pageRename, "批量重命名", "规则替换与序号重排", iconRename},
	{pageImageEdit, "图片编辑", "批量裁切 / 压缩 / 水印", iconCrop},
	{pageSettings, "设置", "全局输出、下载与账号参数", iconSettings},
}

func (a *App) isSidebarCollapsed(winW float32) bool {
	if a != nil && a.navManual {
		return a.navCollapsed
	}
	if winW > 0 && winW < sidebarFoldW {
		return true
	}
	return a != nil && a.navCollapsed
}

func (a *App) sidebarWidth(winW float32) float32 {
	if a.isSidebarCollapsed(winW) {
		return sidebarCollapsedW
	}
	return sidebarExpandedW
}

func (a *App) toggleSidebar(winW float32) {
	a.navCollapsed = !a.isSidebarCollapsed(winW)
	a.navManual = true
}

func (a *App) isWorkbenchTab() bool {
	return a.tab == pageHome || a.tab == pageWash || a.tab == pageFission
}

func activeNavItem(tab int) navItem {
	for _, it := range saasNavItems {
		if it.page == tab {
			return it
		}
	}
	return saasNavItems[0]
}

// topbar 渲染 SaaS 平台全局顶栏：左测为侧栏伸缩与当前上下文，右侧为快捷任务操作与全局运行日志弹出开关。
func (a *App) topbar(c *ui.Context) {
	tok := a.look
	winW, _ := c.Size()
	collapsed := a.isSidebarCollapsed(winW)
	toggleLabel := "收起侧栏"
	if collapsed {
		toggleLabel = "展开侧栏"
	}

	ui.Row(c.Key("topbar")).FillWidth().Height(topbarH).Shrink(0).Padding(0, 10).Gap(8).
		AlignItems(ui.Center).Background(tok.card).
		BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(6).AlignItems(ui.Center).Children(func() {
			uiIconBtn(c, "nav-toggle", toggleLabel, toggleLabel, iconPanelLeft, 24, !collapsed, true, tok).
				OnClick(func() { a.toggleSidebar(winW) })

			if a.isWorkbenchTab() {
				a.workspaceModeTabs(c)
				if homeSplit(winW) {
					ui.Box(c).Width(1).Height(14).Margin(0, 2).Background(tok.border).Shrink(0)
					a.platformTabs(c)
				}
			} else {
				it := activeNavItem(a.tab)
				ui.Row(c.Key("topbar-page")).Shrink(0).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, it.icon).FontSize(13).TextColor(tok.accent)
					ui.Text(c, it.label).FontSize(tok.body).FontWeight(600).SingleLine()
				})
				if it.sub != "" && winW >= 920 {
					ui.Text(c, it.sub).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0)
				}
			}
		})

		ui.Row(c.Key("topbar-right")).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
			if a.isWorkbenchTab() {
				a.historyControl(c)
				uiIconBtn(c, "new-task", "新建任务", "新建任务", iconFilePlus, 24, false, false, tok).
					Disabled(a.running).OnClick(func() { a.newWorkspace() })
				ui.Box(c).Width(1).Height(14).Margin(0, 2).Background(tok.border).Shrink(0)
			}
			if a.running {
				ui.Row(c.Key("topbar-running")).Height(20).Padding(0, 7).Radius(999).Shrink(0).Gap(4).
					AlignItems(ui.Center).Background(tok.accent.Alpha(0.12)).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(tok.accent)
					ui.Text(c, "运行中").FontSize(tok.caption).FontWeight(600).TextColor(tok.accent).SingleLine()
				})
			}
			a.topbarLogControl(c)
		})
	})
}

func (a *App) workspaceModeTabs(c *ui.Context) {
	tok := a.look
	labels := []string{"自由生图", "批量洗图", "裂变图片"}
	tabs := ui.TabsBase(c.Key("workspace-modes"), &a.tab, len(labels))
	tabs.List.Row().Height(24).Padding(2).Radius(tok.radiusS).Background(tok.sunken).
		Shrink(0).AlignItems(ui.Stretch).Children(func() {
		for i, label := range labels {
			tab := tabs.Tab(i).Padding(0, 7).Radius(tok.radiusS - 1).Center().Label(label).Cursor(ui.CursorPointer)
			if i == a.tab {
				tab.Background(tok.card).TextColor(tok.text)
			} else {
				tab.TextColor(tok.muted)
			}
			tab.Children(func() {
				t := ui.Text(c, label).FontSize(tok.caption).SingleLine()
				if i == a.tab {
					t.FontWeight(600)
				}
			})
		}
	})
}

func (a *App) topbarLogControl(c *ui.Context) {
	tok := a.look
	notes := logging.VisibleTail(logging.Log.Entries(), 300)
	var dotColor ui.Color
	hasDot := false
	for _, n := range notes {
		switch n.Level {
		case logging.Error:
			dotColor = tok.danger
			hasDot = true
		case logging.Warning:
			if dotColor != tok.danger {
				dotColor = tok.warning
				hasDot = true
			}
		}
	}

	b := ui.ButtonBase(c.Key("topbar-log")).Height(24).Padding(0, 7).Radius(tok.radiusS).
		Center().Shrink(0).Cursor(ui.CursorPointer).Label("运行日志").Tooltip("运行日志")
	fg := tok.text
	if a.logOpen {
		b.Background(tok.accent.Alpha(0.14)).Border(1, tok.accent.Alpha(0.35))
		fg = tok.accent
	} else {
		b.Background(tok.sunken).Border(1, tok.border)
	}
	hoverPressPose(b)
	b.OnClick(func() { a.logOpen = !a.logOpen })
	b.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconTerminal).FontSize(12).TextColor(fg)
			ui.Text(c, "日志").FontSize(tok.caption).TextColor(fg).SingleLine()
			if len(notes) > 0 {
				ui.Row(c).Height(15).Padding(0, 4).Radius(999).Center().
					Background(tok.card).Border(1, tok.border).Children(func() {
					if hasDot {
						ui.Box(c).Size(5, 5).Radius(2.5).Background(dotColor).Margin(0, 3, 0, 0)
					}
					ui.Text(c, fmt.Sprintf("%d", len(notes))).FontSize(9.5).FontFeatures("tnum").
						TextColor(fg).SingleLine()
				})
			}
		})
	})

	if !a.logOpen {
		return
	}
	winW, winH := c.Size()
	popW := min(float32(460), max(float32(300), winW-24))
	popH := min(float32(360), max(float32(200), winH-topbarH-20))
	ui.PopoverBase(c, b, &a.logOpen, func(panel ui.Element) {
		panel.AttachTo(b, ui.AnchorBottomRight, ui.AnchorTopRight).Margin(4, 0, 0, 0).
			AttachEdgeMargin(8).Width(popW).Height(popH).Padding(8).Radius(tok.radiusM).
			Background(tok.card).Border(1, tok.border).Shadow(0, 8, 24, 0, tok.lift).
			Transition(motionPopup())
		panel.Children(func() {
			a.runLog(c, true)
		})
	})
}

func (a *App) navRail(c *ui.Context) {
	tok := a.look
	winW, _ := c.Size()
	collapsed := a.isSidebarCollapsed(winW)
	w := sidebarExpandedW
	align := ui.Stretch
	padH := float32(6)
	if collapsed {
		w = sidebarCollapsedW
		align = ui.Center
		padH = 0
	}
	ui.Column(c.Key("nav-rail")).Label("nav-rail").Width(w).FillHeight().Padding(8, padH, 8, padH).Gap(3).
		AlignItems(align).Background(tok.card).
		BorderWidth(0, 1, 0, 0).BorderColor(tok.border).Children(func() {
		a.pageTabs(c, collapsed)
		ui.Spacer(c)
		if collapsed {
			ui.Box(c).Width(16).Height(1).Margin(4, 0).Background(tok.border)
		} else {
			ui.Box(c).FillWidth().Height(1).Margin(4, 2).Background(tok.border)
		}
		a.railButton(c, "nav-wizard", "设置向导", iconCompass, collapsed, a.running, false, func() { a.openWizard() })
		updateLabel := "检查更新"
		if a.updateHidden && a.updateDownloading {
			updateLabel = "显示更新进度"
		}
		a.railButton(c, "nav-update", updateLabel, iconRefresh, collapsed, a.updateBusy && !(a.updateHidden && a.updateDownloading), a.updateNotice != "", func() { a.checkForUpdates() })
	})
}

func (a *App) pageTabs(c *ui.Context, collapsed bool) {
	tok := a.look
	selected := 0
	for i, it := range saasNavItems {
		if it.page == a.tab {
			selected = i
			break
		}
	}
	// 运行中也能翻看其它页面：页面里的输入和操作各自按运行状态禁用。
	tabs := ui.TabsBase(c.Key("main-tabs"), &selected, len(saasNavItems))
	align := ui.Stretch
	if collapsed {
		align = ui.Center
	}
	tabs.List.Column().FillWidth().Gap(2).AlignItems(align).OnChange(func() {
		a.tab = saasNavItems[selected].page
		if a.tab == pageHome && a.workspacePage == store.PageWash {
			a.tab = pageWash
		} else if a.tab == pageHome && a.workspacePage == store.PageFission {
			a.tab = pageFission
		}
	}).Children(func() {
		for i, it := range saasNavItems {
			if i == 1 || i == len(saasNavItems)-1 {
				if collapsed {
					ui.Box(c).Width(16).Height(1).Margin(3, 0).Background(tok.border)
				} else {
					ui.Box(c).FillWidth().Height(1).Margin(4, 2).Background(tok.border)
				}
			}
			var tab ui.Element
			if collapsed {
				tab = tabs.Tab(i).Size(28, 28).Radius(6).Center().Label(it.label).Cursor(ui.CursorPointer)
				a.paintNavItem(c, tab, it.icon, "", i == selected, false, true)
				railTip(c, tab, it.label)
			} else {
				tab = tabs.Tab(i).FillWidth().Height(26).Padding(0, 8).Radius(6).AlignItems(ui.Center).
					Label(it.label).Cursor(ui.CursorPointer)
				a.paintNavItem(c, tab, it.icon, it.label, i == selected, false, false)
			}
		}
	})
}

func (a *App) railButton(c *ui.Context, key, label string, icon *ui.SVG, collapsed, disabled, dot bool, click func()) {
	var b ui.Element
	if collapsed {
		b = ui.ButtonBase(c.Key(key)).Size(28, 28).Radius(6).Center().Label(label).Disabled(disabled)
	} else {
		b = ui.ButtonBase(c.Key(key)).FillWidth().Height(24).Padding(0, 8).Radius(6).AlignItems(ui.Center).Label(label).Disabled(disabled)
	}
	if !disabled {
		b.Cursor(ui.CursorPointer).OnClick(click)
	}
	a.paintNavItem(c, b, icon, label, false, dot, collapsed)
	if collapsed {
		railTip(c, b, label)
	}
}

func (a *App) paintNavItem(c *ui.Context, b ui.Element, icon *ui.SVG, label string, on, dot, collapsed bool) {
	tok := a.look
	bg, fg := tok.card, tok.muted
	switch {
	case on:
		bg, fg = tok.accent.Alpha(0.14), tok.accent
	case !b.IsDisabled() && b.Pressed():
		bg, fg = tok.sunken, tok.text
	case !b.IsDisabled() && b.Hovered():
		bg, fg = tok.sunkenHi, tok.text
	}
	b.Background(b.AnimateColor("bg", bg, motionEaseFast))
	b.TextColor(b.AnimateColor("ink", fg, motionEaseFast))
	if !b.IsDisabled() {
		hoverPressPose(b)
	}
	b.Children(func() {
		if collapsed {
			ui.Icon(c, icon).FontSize(14)
			if dot {
				ui.Box(c.Key("dot")).Size(5, 5).Radius(2.5).Background(tok.danger).Absolute().Top(4).Right(4)
			}
			return
		}
		ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(13).Shrink(0)
			t := ui.Text(c, label).FontSize(tok.body).SingleLine().Grow(1).MinWidth(0)
			if on {
				t.FontWeight(600)
			}
			if dot {
				ui.Box(c.Key("dot")).Size(5, 5).Radius(2.5).Background(tok.danger).Shrink(0)
			}
		})
	})
}

// railTip 把提示放在图标右侧。窄栏上居中的提示会伸出窗口左边。
func railTip(c *ui.Context, anchor ui.Element, text string) {
	anchor.Description(text)
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		th := c.Theme()
		tip.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).Margin(0, 0, 0, 8).
			Padding(4, 8).Radius(5).Background(th.Inverse).TextColor(th.InverseText).
			Shadow(0, 4, 14, 0, ui.RGBA(16, 32, 64, 0.18)).Transition(motionPopup())
		tip.Children(func() {
			ui.Text(c, text).FontSize(11).SingleLine()
		})
	})
}

// logLineH 是运行日志一行的高度。跟视口对齐后，贴底滚动不会把第一行切掉半个字。
const logLineH float32 = 17

// logTailPad 让内容高度减去视口之后正好是整行。贴底时视口顶部落在行边界上。
func logTailPad(viewport float32) float32 {
	if viewport <= 1 {
		return 0
	}
	rem := math.Mod(float64(viewport), float64(logLineH))
	if rem < 0.01 {
		return 0
	}
	return float32(rem)
}

// runLog 在顶部日志弹出面板（或测试独立容器）里展示运行日志，贴底时自动跟随最新一行。
func (a *App) runLog(c *ui.Context, fill bool) {
	tok := a.look
	card := ui.Column(c.Key("run-log")).Label("运行日志区").FillWidth().Gap(4)
	if fill {
		card.Grow(1).MinHeight(0)
	}
	limit := 6
	if fill {
		limit = 300
	}
	card.Children(func() {
		notes := logging.VisibleTail(logging.Log.Entries(), limit)
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconTerminal).FontSize(12).TextColor(tok.muted)
			ui.Text(c, "运行日志").FontSize(tok.body).Bold()
			if len(notes) > 0 {
				ui.Row(c).Height(16).Padding(0, 5).Radius(999).Center().Background(tok.sunken).Children(func() {
					ui.Text(c, fmt.Sprintf("%d", len(notes))).FontSize(9.5).FontFeatures("tnum").TextColor(tok.muted).SingleLine()
				})
			}
			ui.Spacer(c)
			uiIconBtn(c, "open-log", "打开日志文件", "打开日志文件", iconExternalLink, 20, false, false, tok).
				OnClick(func() { a.openLogFile() })
			if a.logOpen {
				uiIconBtn(c, "close-log", "收起", "关闭日志面板", iconX, 20, false, true, tok).
					OnClick(func() { a.logOpen = false })
			}
		})
		if len(notes) == 0 {
			ui.Text(c, "每个任务的开始、提交和结束会写在这里").FontSize(tok.caption).TextColor(tok.muted)
			return
		}
		lines := func() {
			for _, line := range notes {
				bg := tok.sunken
				switch line.Level {
				case logging.Success:
					bg = tok.success.Alpha(0.14)
				case logging.Warning:
					bg = tok.warning.Alpha(0.14)
				case logging.Error:
					bg = tok.danger.Alpha(0.14)
				}
				msg := strings.TrimSpace(line.Text)
				ui.Row(c).FillWidth().Height(logLineH).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Text(c, line.Time.Local().Format("15:04:05")).Width(52).Shrink(0).
						FontSize(tok.caption).FontFeatures("tnum").TextColor(tok.muted).SingleLine()
					ui.Row(c).Size(28, 15).Shrink(0).Radius(4).Background(bg).Center().Children(func() {
						ui.Text(c, line.Level.String()).FontSize(9).TextColor(tok.text).SingleLine()
					})
					ui.Text(c, msg).Grow(1).MinWidth(0).FontSize(tok.caption).TextColor(tok.text).
						SingleLine().Tooltip(msg)
				})
			}
		}
		if !fill {
			lines()
			return
		}
		if a.logScroll.Y >= a.logScroll.MaxY {
			a.logScroll.Y = math.MaxFloat32
		}
		scroll := ui.Scroll(c.Key("run-log-lines")).Label("运行日志内容").FillWidth().Grow(1).MinHeight(0)
		pad := float32(0)
		if a.logScroll.MaxY > 0 {
			pad = logTailPad(scroll.Bounds().H)
		}
		scroll.TrackScroll(&a.logScroll).Children(func() {
			ui.Column(c).FillWidth().Gap(0).Children(func() {
				lines()
				if pad > 0.5 {
					ui.Box(c.Key("log-pad")).Height(pad)
				}
			})
		})
	})
}

func (a *App) openLogFile() {
	path := logging.Path()
	if path == "" {
		return
	}
	_ = mygo.Shell.OpenPath(path)
}
