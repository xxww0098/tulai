package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

const (
	topbarH           float32 = 34
	sidebarExpandedW  float32 = 116
	sidebarCollapsedW float32 = 44
	sidebarFoldW      float32 = 840
)

type navItem struct {
	page  int
	group string
	label string
	sub   string
	icon  *ui.SVG
}

var saasNavItems = []navItem{
	{pageHome, "工作台", "图片生成", "生图 / 洗图 / 裂变工作台", iconImages},
	{pageDedup, "工作台", "去重", "按相似图分组清理", iconDedup},
	{pageBrowser, "资产与环境", "浏览器", "Camoufox 实例与账号绑定", iconGlobe},
	{pagePrompts, "资产与环境", "提示词", "词库与常用模板", iconPen},
	{pageDownload, "批处理", "下载", "批量抓取原图", iconDownload},
	{pageRename, "批处理", "批量重命名", "规则替换与序号重排", iconRename},
	{pageImageEdit, "批处理", "图片编辑", "批量裁切 / 压缩 / 水印", iconCrop},
	{pageSettings, "系统", "设置", "全局输出、下载与账号参数", iconSettings},
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
	if tab == pageWash || tab == pageFission {
		tab = pageHome
	}
	for _, it := range saasNavItems {
		if it.page == tab {
			return it
		}
	}
	return saasNavItems[0]
}

// topbar 渲染 SaaS 平台全局顶栏：工作台页左侧直接展示服务商切换，其余页展示当前页面标识，右侧为多账号池健康状态与全局运行日志弹出开关。
func (a *App) topbar(c *ui.Context) {
	tok := a.look
	winW, _ := c.Size()

	ui.Row(c.Key("topbar")).FillWidth().Height(topbarH).Shrink(0).Padding(0, 12).Gap(8).
		AlignItems(ui.Center).Background(tok.card).
		BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
			if a.isWorkbenchTab() {
				if homeSplit(winW) {
					a.platformTabs(c)
				}
			} else {
				it := activeNavItem(a.tab)
				ui.Row(c.Key("topbar-page")).Shrink(0).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, it.icon).FontSize(13).TextColor(tok.accent)
					ui.Text(c, it.label).FontSize(tok.body).FontWeight(600).TextColor(tok.text).SingleLine()
				})
				if it.sub != "" && winW >= 920 {
					ui.Text(c, it.sub).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0)
				}
			}
		})

		ui.Row(c.Key("topbar-right")).Shrink(0).Gap(4).AlignItems(ui.Center).Children(func() {
			if winW >= 920 {
				a.topbarPoolControl(c)
			}
			if a.running && !a.isWorkbenchTab() {
				runFg, runBg, _ := badgeTone("accent", tok)
				ui.Row(c.Key("topbar-running")).Height(20).Padding(0, 8).Radius(999).Shrink(0).Gap(5).
					AlignItems(ui.Center).Background(runBg).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(runFg)
					ui.Text(c, "运行中").FontSize(tok.caption).FontWeight(600).TextColor(runFg).SingleLine()
				})
			}
			a.topbarLogControl(c)
		})
	})
}

func (a *App) accountPoolCounts(now time.Time) (ready, total int) {
	if a == nil {
		return 0, 0
	}
	board := a.accountBoard(now)
	for _, lane := range board.Lanes {
		total++
		switch lane.State {
		case tasks.LaneIdle, tasks.LaneRunning, tasks.LaneGap:
			ready++
		}
	}
	return ready, total
}

func (a *App) topbarPoolControl(c *ui.Context) {
	tok := a.look
	ready, total := a.accountPoolCounts(c.Now())
	tone, dot := "neutral", tok.subtle
	switch {
	case total == 0:
		tone, dot = "neutral", tok.subtle
	case ready == 0:
		tone, dot = "danger", tok.danger
	case ready < total:
		tone, dot = "warning", tok.warning
	default:
		tone, dot = "success", tok.success
	}
	fg, bg, _ := badgeTone(tone, tok)
	if tone == "neutral" && a.tab != pageBrowser {
		fg, bg = tok.muted, tok.sunken
	}
	if a.tab == pageBrowser {
		fg, bg, _ = badgeTone("accent", tok)
	}
	summary := fmt.Sprintf("%d/%d 账号", ready, total)
	tip := fmt.Sprintf("多账号调度池：%d/%d 可用（点击查看详情）", ready, total)
	b := ui.ButtonBase(c.Key("topbar-pool")).Height(24).Padding(0, 8).Radius(tok.radiusS).
		Center().Shrink(0).Cursor(ui.CursorPointer).
		Background(bg).
		Label(fmt.Sprintf("账号池 %d/%d", ready, total)).Tooltip(tip)
	hoverPressPose(b)
	b.OnClick(func() { a.tab = pageBrowser })
	b.Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(dot).Shrink(0)
			ui.Text(c, summary).FontSize(tok.caption).FontWeight(600).FontFeatures("tnum").
				TextColor(fg).SingleLine()
		})
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

	b := ui.ButtonBase(c.Key("topbar-log")).Height(24).Padding(0, 8).Radius(tok.radiusS).
		Center().Shrink(0).Cursor(ui.CursorPointer).Label("运行日志").Tooltip("运行日志")
	fg, bg, _ := interactiveTone(a.logOpen, b.Hovered(), tok.muted, tok)
	if !a.logOpen {
		bg = ui.Transparent
		if b.Hovered() {
			bg = tok.sunken
			fg = tok.text
		}
	}
	b.Background(bg)
	hoverPressPose(b)
	b.OnClick(func() { a.logOpen = !a.logOpen })
	b.Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconTerminal).FontSize(12).TextColor(fg)
			ui.Text(c, "日志").FontSize(tok.caption).TextColor(fg).SingleLine()
			if len(notes) > 0 {
				ui.Row(c).Height(15).Padding(0, 5).Radius(999).Center().
					Background(tok.sunkenHi).Children(func() {
					if hasDot {
						ui.Box(c).Size(5, 5).Radius(2.5).Background(dotColor).Margin(0, 3, 0, 0)
					}
					ui.Text(c, fmt.Sprintf("%d", len(notes))).FontSize(9.5).FontFeatures("tnum").
						TextColor(fg).SingleLine()
				})
			}
		})
	})
}

func (a *App) navRail(c *ui.Context) {
	tok := a.look
	winW, _ := c.Size()
	collapsed := a.isSidebarCollapsed(winW)
	w, align, padH := sidebarExpandedW, ui.Stretch, float32(7)
	toggleLabel := "收起侧栏"
	if collapsed {
		w = sidebarCollapsedW
		align = ui.Center
		padH = 0
		toggleLabel = "展开侧栏"
	}
	railToggle := func(size float32) {
		btn := ui.ButtonBase(c.Key("nav-toggle")).Size(size, size).Radius(tok.radiusS).Center().Shrink(0).
			Cursor(ui.CursorPointer).Label(toggleLabel).Tooltip(toggleLabel).
			OnClick(func() { a.toggleSidebar(winW) })
		icoColor := tok.railMuted
		if btn.Hovered() {
			btn.Background(tok.railHover)
			icoColor = tok.railText
		} else {
			btn.Background(ui.Transparent)
		}
		hoverPressPose(btn)
		btn.Children(func() {
			ui.Icon(c, iconPanelLeft).FontSize(13).TextColor(icoColor)
		})
	}
	ui.Column(c.Key("nav-rail")).Label("nav-rail").Width(w).FillHeight().Padding(0).Gap(0).
		Background(tok.railBg).BorderWidth(0, 1, 0, 0).BorderColor(tok.railBorder).Children(func() {
		head := ui.Row(c.Key("nav-head")).FillWidth().Height(topbarH).Shrink(0).
			BorderWidth(0, 0, 1, 0).BorderColor(tok.railBorder).AlignItems(ui.Center)
		if collapsed {
			head.Justify(ui.Center).Children(func() {
				railToggle(28)
			})
		} else {
			head.Padding(0, 8, 0, 10).Gap(6).Children(func() {
				ui.Image(c.Key("nav-brand"), iconBrandMark).Size(18, 18).Fit(ui.Contain).Shrink(0)
				ui.Text(c, "tulai").FontSize(tok.title).Bold().TextColor(tok.railText).Grow(1).MinWidth(0).SingleLine()
				railToggle(24)
			})
		}
		ui.Column(c).Grow(1).FillWidth().MinHeight(0).Padding(8, padH, 8, padH).Gap(3).
			AlignItems(align).Children(func() {
			a.pageTabs(c, collapsed)
			ui.Spacer(c)
			if collapsed {
				ui.Box(c).Width(16).Height(1).Margin(4, 0).Background(tok.railBorder)
			} else {
				ui.Box(c).FillWidth().Height(1).Margin(4, 2).Background(tok.railBorder)
			}
			a.railButton(c, "nav-wizard", "设置向导", iconCompass, collapsed, a.running, false, func() { a.openWizard() })
			updateLabel := "检查更新"
			if a.updateHidden && a.updateDownloading {
				updateLabel = "显示更新进度"
			}
			a.railButton(c, "nav-update", updateLabel, iconRefresh, collapsed, a.updateBusy && !(a.updateHidden && a.updateDownloading), a.updateNotice != "", func() { a.checkForUpdates() })
		})
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
			newGroup := i > 0 && it.group != saasNavItems[i-1].group
			if newGroup {
				if collapsed {
					ui.Box(c).Width(16).Height(1).Margin(4, 0).Background(tok.railBorder)
				} else {
					ui.Box(c).FillWidth().Height(1).Margin(4, 4).Background(tok.railBorder)
				}
			}
			var tab ui.Element
			if collapsed {
				tab = tabs.Tab(i).Size(30, 30).Radius(tok.radiusS).Center().Label(it.label).Cursor(ui.CursorPointer)
				a.paintNavItem(c, tab, it.icon, "", i == selected, false, true)
				railTip(c, tab, it.label)
			} else {
				tab = tabs.Tab(i).FillWidth().Height(28).Padding(0, 8).Radius(tok.radiusS).AlignItems(ui.Center).
					Label(it.label).Cursor(ui.CursorPointer)
				a.paintNavItem(c, tab, it.icon, it.label, i == selected, false, false)
			}
		}
	})
}

func (a *App) railButton(c *ui.Context, key, label string, icon *ui.SVG, collapsed, disabled, dot bool, click func()) {
	tok := a.look
	var b ui.Element
	if collapsed {
		b = ui.ButtonBase(c.Key(key)).Size(30, 30).Radius(tok.radiusS).Center().Label(label).Disabled(disabled)
	} else {
		b = ui.ButtonBase(c.Key(key)).FillWidth().Height(27).Padding(0, 8).Radius(tok.radiusS).AlignItems(ui.Center).Label(label).Disabled(disabled)
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
	bg, fg := ui.Transparent, tok.railMuted
	iconColor := tok.railMuted
	switch {
	case on:
		bg, fg = tok.railActive, tok.railText
		iconColor = tok.railAccent
	case !b.IsDisabled() && b.Pressed():
		bg, fg = tok.railHover, tok.railText
		iconColor = tok.railText
	case !b.IsDisabled() && b.Hovered():
		bg, fg = tok.railHover, tok.railText
		iconColor = tok.railText
	}
	b.Background(b.AnimateColor("bg", bg, motionEaseFast))
	b.TextColor(b.AnimateColor("ink", fg, motionEaseFast))
	if !b.IsDisabled() {
		hoverPressPose(b)
	}
	b.Children(func() {
		if collapsed {
			ui.Icon(c, icon).FontSize(14).TextColor(iconColor)
			if dot {
				ui.Box(c.Key("dot")).Size(5, 5).Radius(2.5).Background(tok.danger).Absolute().Top(5).Right(5)
			}
			return
		}
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).FontSize(13).TextColor(iconColor).Shrink(0)
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

func (a *App) openLogFile() {
	path := logging.Path()
	if path == "" {
		return
	}
	_ = mygo.Shell.OpenPath(path)
}
