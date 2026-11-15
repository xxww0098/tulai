package ui

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/download"
	"github.com/xxww0098/tulai/internal/logging"
)

const (
	dlRowPadX   float32 = 10
	dlRowH      float32 = 34
	dlSizeColW  float32 = 84
	dlProgColW  float32 = 150
	dlSpeedColW float32 = 80
	dlEtaColW   float32 = 70
	dlActColW   float32 = 88
	dlColGap    float32 = 12
)

// downloadPage 是整页：SaaS 页头、分段状态筛选与工具条、任务列表卡片或空态，弹窗盖在上面。
func (a *App) downloadPage(c *ui.Context) {
	ui.Column(c.Key("download-page")).Fill().MinHeight(0).Gap(6).Children(func() {
		a.downloadHeader(c)
		if a.dlImportNotice != "" {
			a.downloadImportNoticeBar(c)
		}
		a.downloadSubToolbar(c)
		a.downloadBatchBar(c)
		a.downloadBody(c)
	})
	a.downloadComposer(c)
	a.downloadImportModal(c)
}

func (a *App) downloadImportNoticeBar(c *ui.Context) {
	tok := a.look
	fg, bg, border := badgeTone("success", tok)
	ui.Row(c.Key("dl-import-notice")).FillWidth().Padding(5, 10).Gap(8).Radius(tok.radiusM).
		Background(bg).Border(1, border).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconCheck).FontSize(13).TextColor(fg)
		ui.Text(c, a.dlImportNotice).FontSize(tok.body).TextColor(fg).FontFeatures("tnum").
			Grow(1).MinWidth(0).SingleLine().Tooltip(a.dlImportNotice)
		uiIconBtn(c, "dl-import-notice-close", "关闭导入提示", "关闭导入提示", iconX, 20, false, true, tok).
			OnClick(func() { a.dlImportNotice = "" })
	})
}

func (a *App) downloadHeader(c *ui.Context) {
	tok := a.look
	activeCount := 0
	for _, t := range a.dlTasks {
		if t.Phase == dlActive {
			activeCount++
		}
	}
	sub := "支持网页解析、直链队列与表格批量导入"
	if len(a.dlTasks) > 0 {
		sub = fmt.Sprintf("%d 个进行中 · 队列共 %d 个任务", activeCount, len(a.dlTasks))
	}
	uiPageHeader(c, "dl-header", iconDownload, "下载任务", sub, tok, func() {
		a.downloadSearchFab(c, tok)
		a.downloadNewFab(c, tok)
	})
}

func (a *App) downloadFilterTabs(c *ui.Context, tok tokens) {
	counts := [4]int{}
	for _, t := range a.dlTasks {
		counts[dlFilterAll]++
		switch {
		case t.Phase == dlDone:
			counts[dlFilterDone]++
		case t.Phase == dlCanceled:
			counts[dlFilterCanceled]++
		default:
			counts[dlFilterOpen]++
		}
	}
	activeBg := tok.card
	if tok.dark {
		activeBg = tok.sunkenHi
	}
	ui.Row(c.Key("dl-tabs")).Height(28).Padding(2).Gap(2).Radius(tok.radiusM).
		Background(tok.sunken).Border(1, tok.border).Shrink(0).AlignItems(ui.Center).Children(func() {
		for i, label := range dlFilterLabels {
			index := i
			active := a.dlFilter == index
			tab := ui.ButtonBase(c.Key(fmt.Sprintf("dl-tab-%d", index))).Height(22).Padding(0, 8).
				Radius(tok.radiusS).Center().Shrink(0).Label(label).Cursor(ui.CursorPointer)
			fg := tok.muted
			switch {
			case active:
				tab.Background(activeBg).Border(1, tok.borderStrong)
				fg = tok.text
			case tab.Hovered():
				tab.Background(tok.sunkenHi.Alpha(0.55))
				fg = tok.text
			}
			tab.TextColor(fg)
			tab.OnClick(func() {
				if a.dlFilter != index {
					a.dlFilter = index
					a.dlSelectedID = 0
					a.clearDownloadChecked()
				}
			})
			tab.Children(func() {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					t := ui.Text(c, label).FontSize(tok.caption).SingleLine()
					if active {
						t.FontWeight(600)
					} else {
						t.FontWeight(500)
					}
					if counts[index] > 0 {
						cntColor := tok.muted
						if active {
							cntColor = tok.accent
						}
						ui.Text(c, fmt.Sprintf("%d", counts[index])).FontSize(tok.caption).
							TextColor(cntColor).FontFeatures("tnum").SingleLine()
					}
				})
			})
		}
	})
}

func (a *App) downloadSearchFab(c *ui.Context, tok tokens) {
	hasQuery := strings.TrimSpace(a.dlSearch) != ""
	tip := "搜索任务"
	if hasQuery {
		tip = "搜索：" + a.dlSearch
	}
	b := uiIconBtn(c, "dl-search-fab", "搜索任务", tip, iconSearch, 28, a.dlSearchOn || hasQuery, false, tok).
		OnClick(func() { a.dlSearchOn = !a.dlSearchOn })
	if a.dlSearchOn {
		ui.Popover(c, b, &a.dlSearchOn, func() {
			ui.Row(c.Key("dl-search-pop")).Width(320).Padding(8, 10).Gap(6).
				AlignItems(ui.Center).Background(tok.card).Border(1, tok.borderStrong).
				Radius(tok.radiusM).Shadow(0, 8, 24, 0, tok.lift).Children(func() {
				ui.TextInput(c.Key("dl-search-input"), &a.dlSearch).Grow(1).MinWidth(0).
					Label("搜索任务").Placeholder("搜索站点、文件名、链接或错误信息")
				uiIconBtn(c, "dl-search-close", "清除并关闭搜索", "清除并关闭搜索", iconX, 24, false, false, tok).
					OnClick(func() {
						if a.dlSearch != "" {
							a.dlSearch = ""
						} else {
							a.dlSearchOn = false
						}
					})
			})
		})
	}
}

func (a *App) downloadNewFab(c *ui.Context, tok tokens) {
	b := ui.ButtonBase(c.Key("dl-new-fab")).Height(28).Padding(0, 10).Radius(tok.radiusS).
		Center().Shrink(0).Label("新建任务").Cursor(ui.CursorPointer)
	bg := tok.accent
	if b.Pressed() {
		bg = c.Theme().AccentPressed
	} else if b.Hovered() {
		bg = c.Theme().AccentHover
	}
	b.Background(bg).TextColor(tok.onAccent).Border(1, bg)
	hoverPressPose(b)
	b.OnClick(func() {
		a.dlComposerErr = ""
		a.dlComposerOn = true
	})
	b.Children(func() {
		ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconPlus).FontSize(13).TextColor(tok.onAccent)
			ui.Text(c, "新建任务").FontSize(tok.body).FontWeight(600).TextColor(tok.onAccent).SingleLine()
		})
	})
	dlTip(c, b, "新建下载任务")
}

func (a *App) downloadSubToolbar(c *ui.Context) {
	tok := a.look
	w, _ := c.Size()
	tasks := a.downloadView()
	count := "无任务"
	if len(tasks) > 0 {
		count = fmt.Sprintf("%d 个任务", len(tasks))
	}
	if len(a.dlTasks) > len(tasks) {
		count += fmt.Sprintf(" · 共 %d", len(a.dlTasks))
	}
	if w > 0 && w < 860 {
		ui.Column(c.Key("dl-sub")).FillWidth().Gap(6).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				a.downloadFilterTabs(c, tok)
				ui.Spacer(c)
				ui.Text(c.Key("dl-count"), count).FontSize(tok.caption).TextColor(tok.muted).
					FontFeatures("tnum").SingleLine().Shrink(0)
			})
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				a.downloadSubActions(c, tok, tasks)
			})
		})
		return
	}
	ui.Row(c.Key("dl-sub")).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		a.downloadFilterTabs(c, tok)
		ui.Spacer(c)
		ui.Text(c.Key("dl-count"), count).FontSize(tok.caption).TextColor(tok.muted).
			FontFeatures("tnum").SingleLine().Shrink(0)
		a.downloadSubActions(c, tok, tasks)
	})
}

func (a *App) downloadSubActions(c *ui.Context, tok tokens, tasks []*downloadTask) {
	canPause, canResume := false, false
	for _, t := range tasks {
		switch t.Phase {
		case dlActive:
			canPause = true
		case dlPaused:
			canResume = true
		}
	}
	trash := 0
	for _, t := range a.dlTasks {
		if t.Phase == dlCanceled {
			trash++
		}
	}
	ui.Row(c.Key("dl-sub-actions")).Gap(4).Shrink(0).AlignItems(ui.Center).Children(func() {
		switch a.dlFilter {
		case dlFilterOpen:
			a.dlPill(c, tok, "dl-start-all", "全部开始", iconPlay, false, !canResume, a.resumeAllDownloads)
			a.dlPill(c, tok, "dl-pause-all", "全部暂停", iconPause, false, !canPause, a.pauseAllDownloads)
		case dlFilterCanceled:
			a.dlPill(c, tok, "dl-clear-trash", "清空回收站", iconTrash, false, trash == 0, func() {
				a.clearDownloadTrash()
			})
		}
		if a.dlFilter != dlFilterCanceled {
			a.dlPill(c, tok, "dl-group", "按站点分组", iconLayers, a.dlGroupBySite, false, func() {
				a.dlGroupBySite = !a.dlGroupBySite
			})
			a.dlPill(c, tok, "dl-prune", "清理", iconTrash, false, false, func() {
				a.pruneDownloads()
			})
		}
	})
}

// dlPill 是工具条上的紧凑 SaaS 胶囊按钮，统一用 design.go 语义与边框层级。
func (a *App) dlPill(c *ui.Context, tok tokens, key, label string, icon *ui.SVG, active, disabled bool, click func()) {
	b := ui.ButtonBase(c.Key(key)).Height(24).Radius(tok.radiusS).Padding(0, 8).Gap(4).
		AlignItems(ui.Center).Shrink(0).Label(label).Tooltip(label).Disabled(disabled)
	var fg, bg, border ui.Color
	if disabled {
		fg, bg, border = tok.muted.Alpha(0.5), tok.sunken, tok.border
	} else {
		fg, bg, border = interactiveTone(active, b.Hovered(), tok.muted, tok)
		b.Cursor(ui.CursorPointer).OnClick(click)
		hoverPressPose(b)
	}
	b.Background(bg).Border(1, border).TextColor(fg)
	b.Children(func() {
		ui.Icon(c, icon).FontSize(12).TextColor(fg)
		t := ui.Text(c, label).FontSize(tok.caption).SingleLine().TextColor(fg)
		if active {
			t.FontWeight(600)
		} else {
			t.FontWeight(500)
		}
	})
}

// downloadCheck 画一个 16px 的方形勾选框，点它切换 *checked。
func downloadCheck(c *ui.Context, tok tokens, checked *bool, label string) ui.Element {
	b := ui.CheckboxBase(c, checked).Size(24, 16).Shrink(0).Center().Label(label).Cursor(ui.CursorPointer)
	b.Children(func() {
		box := ui.Column(c).Size(16, 16).Radius(tok.radiusS).Center().BorderWidth(1)
		if *checked {
			box.Background(tok.accent).BorderColor(tok.accent)
			box.Children(func() { ui.Icon(c, iconCheck).FontSize(11).TextColor(tok.onAccent) })
			return
		}
		box.Background(tok.card)
		if b.Hovered() {
			box.BorderColor(tok.borderStrong)
		} else {
			box.BorderColor(tok.border)
		}
	})
	return b
}

func (a *App) downloadBatchBar(c *ui.Context) {
	checked := a.checkedDownloadTasks()
	if len(checked) == 0 {
		return
	}
	tok := a.look
	visible := a.downloadView()
	all := true
	for _, t := range visible {
		if !t.Checked {
			all = false
			break
		}
	}
	a.dlAllChecked = all
	fg, bg, border := semanticToneColors(tok.dark, tok.text, tok.accent, 0.08)
	ui.Row(c.Key("dl-batch")).FillWidth().Padding(5, 10).Gap(6).Radius(tok.radiusM).
		AlignItems(ui.Center).Background(bg).Border(1, border).Children(func() {
		downloadCheck(c.Key("dl-batch-all"), tok, &a.dlAllChecked, "全选当前列表").
			OnChange(func() { a.setDownloadChecked(a.dlAllChecked) })
		ui.Text(c, fmt.Sprintf("已选 %d", len(checked))).FontSize(tok.caption).FontWeight(600).
			TextColor(fg).FontFeatures("tnum").Shrink(0)
		ui.Spacer(c)
		a.dlPill(c, tok, "dl-batch-pause", "暂停", iconPause, false, false, func() {
			for _, t := range checked {
				a.pauseTask(t)
			}
		})
		a.dlPill(c, tok, "dl-batch-resume", "继续", iconPlay, false, false, func() {
			for _, t := range checked {
				a.resumeTask(t)
			}
		})
		a.dlPill(c, tok, "dl-batch-cancel", "取消", iconCircleX, false, false, func() {
			for _, t := range checked {
				a.cancelTask(t)
			}
		})
		a.dlPill(c, tok, "dl-batch-retry", "重试", iconRotate, false, false, func() {
			for _, t := range checked {
				a.retryTask(t)
			}
		})
		a.dlPill(c, tok, "dl-batch-open", "打开目录", iconFolderOpen, false, false, func() {
			if dir := strings.TrimSpace(downloadTaskDir(checked[0])); dir != "" {
				a.openResultFolder(dir)
			}
		})
		a.dlPill(c, tok, "dl-batch-remove", "移除", iconTrash, false, false, func() {
			for _, t := range checked {
				a.removeTask(t)
			}
		})
		a.dlPill(c, tok, "dl-batch-clear", "取消选择", iconX, false, false, a.clearDownloadChecked)
	})
}

func (a *App) setDownloadChecked(checked bool) {
	for _, t := range a.downloadView() {
		t.Checked = checked
	}
}

func (a *App) downloadBody(c *ui.Context) {
	tasks := a.downloadView()
	if len(tasks) == 0 {
		a.downloadEmpty(c)
		return
	}
	tok := a.look
	uiCardSurface(ui.Column(c.Key("dl-body")).Grow(1).MinHeight(0).Clip(), tok).Children(func() {
		a.downloadColumns(c)
		ui.Scroll(c.Key("dl-list")).Grow(1).MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				if a.dlGroupBySite {
					for _, group := range groupBySite(tasks) {
						a.downloadGroupHead(c, group)
						for _, t := range group.Tasks {
							a.downloadRow(c, t)
						}
					}
					return
				}
				for _, t := range tasks {
					a.downloadRow(c, t)
				}
			})
		})
	})
}

func (a *App) downloadColumns(c *ui.Context) {
	tok := a.look
	cell := func(label string, width float32) {
		ui.Text(c, label).FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).
			Width(width).TextAlign(ui.End).SingleLine()
	}
	ui.Row(c.Key("dl-columns")).FillWidth().Padding(6, dlRowPadX, 6, dlRowPadX).Gap(dlColGap).
		AlignItems(ui.Center).Background(tok.sunken).
		BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
		ui.Box(c).Width(24).Shrink(0)
		ui.Text(c, "名称").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).Grow(1).MinWidth(0)
		cell("大小", dlSizeColW)
		cell("进度", dlProgColW)
		cell("速度", dlSpeedColW)
		cell("剩余时间", dlEtaColW)
		ui.Text(c, "操作").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).
			Width(dlActColW).TextAlign(ui.End).SingleLine()
	})
}

func (a *App) downloadGroupHead(c *ui.Context, group dlSiteGroup) {
	tok := a.look
	ui.Row(c).FillWidth().Padding(5, dlRowPadX, 5, dlRowPadX).Gap(6).AlignItems(ui.Center).
		Background(tok.rail).BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
		ui.Icon(c, iconGlobe).FontSize(12).TextColor(tok.muted)
		ui.Text(c, group.Label).FontSize(tok.caption).FontWeight(600).TextColor(tok.text).SingleLine()
		uiBadge(c, "dl-group-"+group.Label, fmt.Sprintf("%d", len(group.Tasks)), "neutral", tok)
	})
}

// downloadRow 是单条任务：勾选框、站点与文件名、大小、进度、速度、剩余时间、操作。
func (a *App) downloadRow(c *ui.Context, t *downloadTask) {
	tok := a.look
	selected := a.dlSelectedID == t.ID
	row := ui.Row(c.Key(fmt.Sprintf("dl-row-%d", t.ID))).FillWidth().MinHeight(dlRowH).
		Padding(5, dlRowPadX, 5, dlRowPadX).Gap(dlColGap).AlignItems(ui.Center)
	bg := tok.card
	switch {
	case selected:
		bg = tok.accent.Alpha(0.08)
	case row.Hovered():
		bg = tok.sunken
	case t.Checked:
		bg = tok.accent.Alpha(0.05)
	}
	row.Background(bg).BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Cursor(ui.CursorPointer)
	row.OnClick(func() { a.dlSelectedID = t.ID })
	showActions := row.Hovered() || selected || t.Checked

	row.Children(func() {
		downloadCheck(c, tok, &t.Checked, "选择 "+t.Site)
		a.downloadSourceCell(c, tok, t)
		size := taskSizeText(t)
		ui.Text(c, size).FontSize(tok.body).TextColor(tok.muted).FontFeatures("tnum").
			Width(dlSizeColW).TextAlign(ui.End).SingleLine()
		a.downloadProgressCell(c, tok, t)
		ui.Text(c, formatSpeed(t.Speed)).FontSize(tok.body).FontFeatures("tnum").
			TextColor(speedColor(tok, t)).Width(dlSpeedColW).TextAlign(ui.End).SingleLine()
		ui.Text(c, taskETA(t)).FontSize(tok.body).FontFeatures("tnum").
			TextColor(tok.muted).Width(dlEtaColW).TextAlign(ui.End).SingleLine()
		if showActions {
			a.downloadRowActions(c, tok, t)
		} else {
			ui.Box(c).Width(dlActColW).Shrink(0)
		}
	})
}

func speedColor(tok tokens, t *downloadTask) ui.Color {
	if t.Phase == dlActive && t.Speed > 0 {
		return tok.accent
	}
	return tok.muted
}

func (a *App) downloadSourceCell(c *ui.Context, tok tokens, t *downloadTask) {
	ink := tok.text
	if t.Phase == dlCanceled {
		ink = tok.muted
	}
	tileBg, tileFg := tileColors(tok, t.Phase)
	ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		tile := ui.Column(c).Size(22, 22).Radius(tok.radiusS).Center().Shrink(0).
			Background(tileBg).Border(1, tok.border)
		tile.Children(func() { ui.Icon(c, iconGlobe).FontSize(12).TextColor(tileFg) })
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Row(c).Gap(6).MinWidth(0).AlignItems(ui.Center).Children(func() {
				ui.Text(c, t.Site).FontSize(tok.body).FontWeight(600).TextColor(ink).
					Shrink(0).MaxWidth(180).SingleLine().Tooltip(t.Site)
				a.downloadBadge(c, tok, t)
				ui.Text(c, compactURL(t.URL)).FontSize(tok.caption).TextColor(tok.muted).
					Grow(1).MinWidth(0).SingleLine().Tooltip(t.URL)
			})
			sub, color := t.Name, tok.muted
			if t.ImagesFiltered > 0 {
				sub += fmt.Sprintf(" · 已过滤 %d 张", t.ImagesFiltered)
			}
			if t.Err != "" {
				sub, color = t.Err, tok.danger
			}
			if sub != "" {
				ui.Text(c, sub).FontSize(tok.caption).TextColor(color).MinWidth(0).
					SingleLine().Tooltip(sub)
			}
		})
	})
}

func tileColors(tok tokens, phase dlPhase) (ui.Color, ui.Color) {
	switch phase {
	case dlDone:
		fg, bg, _ := badgeTone("success", tok)
		return bg, fg
	case dlFailed:
		fg, bg, _ := badgeTone("danger", tok)
		return bg, fg
	case dlPaused:
		fg, bg, _ := badgeTone("warning", tok)
		return bg, fg
	case dlQueued, dlCanceled:
		return tok.sunken, tok.muted
	default:
		fg, bg, _ := badgeTone("accent", tok)
		return bg, fg
	}
}

func (a *App) downloadBadge(c *ui.Context, tok tokens, t *downloadTask) {
	label := t.Phase.label()
	tone := "neutral"
	var icon *ui.SVG = iconClock
	switch t.Phase {
	case dlActive:
		tone, icon = "accent", iconDownload
	case dlDone:
		tone, icon = "success", iconCheck
	case dlFailed:
		tone, icon = "danger", iconAlert
	case dlPaused:
		tone, icon = "warning", iconPause
	case dlCanceled:
		tone, icon = "neutral", iconBan
	}
	fg, bg, border := badgeTone(tone, tok)
	ui.Row(c).Height(18).Radius(999).Padding(0, 6).Gap(3).Shrink(0).
		AlignItems(ui.Center).Background(bg).Border(1, border).Children(func() {
		ui.Icon(c, icon).FontSize(11).TextColor(fg)
		ui.Text(c, label).FontSize(tok.caption).FontWeight(500).TextColor(fg).SingleLine()
	})
}

func (a *App) downloadProgressCell(c *ui.Context, tok tokens, t *downloadTask) {
	frac := float32(taskProgress(t) / 100)
	bar := tok.accent
	switch t.Phase {
	case dlPaused, dlQueued, dlCanceled:
		bar = tok.muted.Alpha(0.6)
	case dlFailed:
		bar = tok.danger
	case dlDone:
		bar = tok.success
	}
	trackBg := tok.sunkenHi
	ui.Row(c).Width(dlProgColW).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Height(4).Grow(1).MinWidth(40).Radius(2).Background(trackBg).
			Draw(func(p *ui.Painter, r ui.Rect) {
				if frac <= 0 {
					return
				}
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * frac, H: r.H}, bar, 2)
			})
		ui.Text(c, fmt.Sprintf("%d%%", int(taskProgress(t)+0.5))).FontSize(tok.body).FontWeight(500).
			TextColor(progressNumberColor(tok, t)).Width(34).TextAlign(ui.End).
			FontFeatures("tnum").SingleLine()
	})
}

func progressNumberColor(tok tokens, t *downloadTask) ui.Color {
	if t.Phase == dlActive {
		fg, _, _ := badgeTone("accent", tok)
		return fg
	}
	return tok.text
}

func (a *App) downloadRowActions(c *ui.Context, tok tokens, t *downloadTask) {
	ui.Row(c.Key(fmt.Sprintf("dl-actions-%d", t.ID))).Width(dlActColW).Shrink(0).
		Justify(ui.End).Gap(2).AlignItems(ui.Center).Children(func() {
		switch t.Phase {
		case dlActive:
			a.dlIconButton(c, tok, fmt.Sprintf("dl-pause-%d", t.ID), "暂停", iconPause, false,
				func() { a.pauseTask(t) })
			a.dlIconButton(c, tok, fmt.Sprintf("dl-cancel-%d", t.ID), "取消", iconCircleX, false,
				func() { a.cancelTask(t) })
		case dlPaused, dlQueued:
			a.dlIconButton(c, tok, fmt.Sprintf("dl-resume-%d", t.ID), "继续", iconPlay, false,
				func() { a.resumeTask(t) })
			a.dlIconButton(c, tok, fmt.Sprintf("dl-cancel-%d", t.ID), "取消", iconCircleX, false,
				func() { a.cancelTask(t) })
		default:
			a.dlIconButton(c, tok, fmt.Sprintf("dl-retry-%d", t.ID), "重新下载", iconRotate, false,
				func() { a.retryTask(t) })
			a.dlIconButton(c, tok, fmt.Sprintf("dl-open-%d", t.ID), "打开目录", iconFolderOpen, false,
				func() { a.openResultFolder(downloadTaskDir(t)) })
			a.dlIconButton(c, tok, fmt.Sprintf("dl-remove-%d", t.ID), "移除", iconTrash, true,
				func() { a.removeTask(t) })
		}
	})
}

func (a *App) dlIconButton(c *ui.Context, tok tokens, key, label string, icon *ui.SVG, danger bool, click func()) {
	b := ui.ButtonBase(c.Key(key)).Size(22, 22).Radius(tok.radiusS).Center().Label(label)
	bg, fg, border := ui.Transparent, tok.muted, ui.Transparent
	switch {
	case danger && b.Hovered():
		fg, bg, border = tok.danger, tok.danger.Alpha(0.12), tok.danger.Alpha(0.28)
	case b.Hovered():
		fg, bg, border = tok.text, tok.sunkenHi, tok.border
	}
	b.Background(bg).Border(1, border).TextColor(fg).Cursor(ui.CursorPointer)
	hoverPressPose(b)
	b.OnClick(click)
	b.Children(func() { ui.Icon(c, icon).FontSize(12) })
	dlTip(c, b, label)
}

// downloadEmpty 是空态：克制统一的 SaaS 容器、明确指引和主按钮。
func (a *App) downloadEmpty(c *ui.Context) {
	tok := a.look
	searching := strings.TrimSpace(a.dlSearch) != ""
	title, subtitle := "暂无下载任务", "点击右上角 + 新建任务，或直接拖入链接"
	if searching {
		title, subtitle = "没有匹配的任务", "试试其他关键词，或清除搜索"
	}
	fg, bg, border := badgeTone("accent", tok)
	uiCardSurface(ui.Column(c.Key("dl-empty")).Grow(1).FillWidth().MinHeight(220).Center().
		Padding(28, 20, 28, 20).Gap(6), tok).Children(func() {
		well := ui.Column(c).Size(40, 40).Radius(tok.radiusL).Center().Margin(0, 0, 4, 0).
			Background(bg).Border(1, border)
		well.Children(func() { ui.Icon(c, iconDownload).FontSize(20).TextColor(fg) })
		ui.Text(c, title).FontSize(tok.display).FontWeight(600).TextColor(tok.text).TextAlign(ui.Center)
		ui.Text(c, subtitle).FontSize(tok.body).TextColor(tok.muted).MaxWidth(300).
			TextAlign(ui.Center).Margin(2, 0, 0, 0)
		ui.Row(c).Margin(12, 0, 0, 0).Justify(ui.Center).Gap(8).Children(func() {
			if searching {
				a.dlEmptyButton(c, tok, "清除搜索", iconX, false, func() { a.dlSearch = "" })
				return
			}
			a.dlEmptyButton(c, tok, "新建任务", iconPlus, true, func() {
				a.dlComposerErr = ""
				a.dlComposerOn = true
			})
		})
	})
}

func (a *App) dlEmptyButton(c *ui.Context, tok tokens, label string, icon *ui.SVG, primary bool, click func()) {
	b := ui.ButtonBase(c.Key("dl-empty-"+label)).Height(28).MinWidth(92).Radius(tok.radiusS).
		Padding(0, 12).Gap(5).AlignItems(ui.Center).Center().Label(label).Cursor(ui.CursorPointer)
	var bg, fg, border ui.Color
	if primary {
		bg, fg = tok.accent, tok.onAccent
		if b.Hovered() {
			bg = c.Theme().AccentHover
		}
		border = bg
	} else {
		bg, fg, border = tok.sunken, tok.text, tok.border
		if b.Hovered() {
			bg, border = tok.sunkenHi, tok.borderStrong
		}
	}
	b.Background(bg).TextColor(fg).Border(1, border)
	hoverPressPose(b)
	b.OnClick(click)
	b.Children(func() {
		ui.Icon(c, icon).FontSize(13).TextColor(fg)
		ui.Text(c, label).FontSize(tok.body).FontWeight(600).TextColor(fg)
	})
}

// downloadComposer 是「新建任务」弹窗：贴链接、看有效行数、选目录、立即下载。
func (a *App) downloadComposer(c *ui.Context) {
	if !a.dlComposerOn {
		a.dlGuideOn = false
		return
	}
	tok := a.look
	valid, invalid := parseDownloadURLsSplit(a.dlURLs)
	sites := dlComposerSites(valid)
	w, h := c.Size()
	width := min(float32(560), max(float32(260), w-80))
	bodyHeight := max(float32(120), h-195)
	ui.Modal(c, &a.dlComposerOn, func() {
		if a.dlGuideOn {
			a.downloadGuide(c, tok)
			return
		}
		ui.Column(c.Key("dl-composer")).Width(width).Gap(10).Padding(2, 2, 2, 2).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, "新建任务").FontSize(15).FontWeight(600).TextColor(tok.text)
					ui.Text(c, "粘贴网页或图片链接，每行一个。").FontSize(tok.caption).TextColor(tok.muted)
				})
				uiIconTextBtn(c, "dl-guide-open", "支持哪些页面？", "查看支持的网站与页面类型", iconCompass, 24, false, tok).
					OnClick(func() {
						a.dlGuideSearch = ""
						a.dlGuideScroll.Y = 0
						a.dlGuideOn = true
					})
			})
			ui.Scroll(c.Key("dl-composer-options")).FillWidth().MaxHeight(bodyHeight).Label("下载任务选项").Children(func() {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "链接").FontSize(tok.body).FontWeight(600).TextColor(tok.text)
						ui.Spacer(c)
						uiMiniBtn(ui.Button(c, "导入 CSV / XLSX"), tok).
							Background(tok.sunken).TextColor(tok.text).Border(1, tok.border).
							OnClick(a.pickDownloadSpreadsheet)
						ui.Text(c, "Ctrl + Enter 提交").FontSize(tok.caption).TextColor(tok.muted)
					})
					ui.TextArea(c.Key("dl-composer-urls"), &a.dlURLs).Height(124).FillWidth().Label("链接").
						Placeholder("https://example.com/gallery\nhttps://example.com/photo.jpg")
					a.dlComposerMeta(c, tok, valid, invalid, sites)
					ui.Column(c).FillWidth().Padding(8, 10).Gap(8).Radius(tok.radiusM).
						Background(tok.sunken).Border(1, tok.border).Children(func() {
						ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "扫描深度").FontSize(tok.body).FontWeight(600).TextColor(tok.text).Shrink(0)
							ui.NumberInput(c.Key("dl-depth"), &a.dlDepth, 1, 32767, 1).Label("扫描深度").Disabled(a.dlBrowserMode)
							if a.dlBrowserMode {
								ui.Text(c, "浏览器模式采集当前页面").FontSize(tok.caption).TextColor(tok.muted).Grow(1).MinWidth(0)
							} else {
								ui.Text(c, "1 为当前页，2 可跟进详情页").FontSize(tok.caption).TextColor(tok.muted).Grow(1).MinWidth(0)
							}
						})
						ui.Checkbox(c.Key("dl-browser-mode"), &a.dlBrowserMode, "使用浏览器登录并抓取动态页面")
						if a.dlBrowserMode {
							a.downloadBrowserControls(c, tok)
						}
					})
					ui.Text(c, "下载到").FontSize(tok.body).FontWeight(600).TextColor(tok.text)
					a.dlComposerDest(c, tok)
					a.dlComposerSettingsRow(c, tok)
				})
			})
			ui.Row(c).FillWidth().Padding(6, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(tok.border).
				Justify(ui.End).Gap(8).Children(func() {
				cancel := uiBtn(ui.Button(c, "取消"), tok).Height(28).Padding(0, 14).
					Background(tok.sunken).TextColor(tok.text).Border(1, tok.border)
				hoverPressPose(cancel)
				cancel.OnClick(func() { a.dlComposerOn = false })
				a.dlComposerSubmit(c, tok, len(valid))
			})
		})
	})
}

func (a *App) dlComposerMeta(c *ui.Context, tok tokens, valid []string, invalid int, sites string) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
		if len(valid) > 0 {
			text := fmt.Sprintf("%d 个有效链接", len(valid))
			if sites != "" {
				text += " · " + sites
			}
			uiBadge(c, "dl-composer-valid", text, "success", tok)
		}
		if invalid > 0 {
			uiBadge(c, "dl-composer-invalid", fmt.Sprintf("%d 行无效", invalid), "danger", tok)
		}
		if a.dlComposerErr != "" {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).FontSize(12).TextColor(tok.danger)
				ui.Text(c, a.dlComposerErr).FontSize(tok.caption).TextColor(tok.danger)
			})
		}
		if len(valid) == 0 && invalid == 0 && a.dlComposerErr == "" {
			ui.Text(c, "支持 HTTP / HTTPS").FontSize(tok.caption).TextColor(tok.muted)
		}
	})
}

func dlComposerOptionRow(c *ui.Context, key string, icon *ui.SVG, title, sub, tooltip string, iconBg, iconFg ui.Color, tok tokens, onClick func()) {
	row := ui.Row(c.Key(key)).FillWidth().Height(40).Padding(6, 10).Gap(8).
		AlignItems(ui.Center).Radius(tok.radiusS).Background(tok.card).Border(1, tok.border)
	if row.Hovered() {
		row.Background(tok.sunken).BorderColor(tok.borderStrong)
	}
	row.Cursor(ui.CursorPointer).OnClick(onClick)
	row.Children(func() {
		box := ui.Column(c).Size(24, 24).Radius(tok.radiusS).Center().Shrink(0).
			Background(iconBg).Border(1, tok.border)
		box.Children(func() { ui.Icon(c, icon).FontSize(13).TextColor(iconFg) })
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, title).FontSize(tok.body).FontWeight(600).TextColor(tok.text)
			t := ui.Text(c, sub).FontSize(tok.caption).TextColor(tok.muted).SingleLine()
			if tooltip != "" {
				t.Tooltip(tooltip)
			}
		})
		ui.Icon(c, iconChevronRight).FontSize(13).TextColor(tok.muted)
	})
}

func (a *App) dlComposerDest(c *ui.Context, tok tokens) {
	fg, bg, _ := badgeTone("accent", tok)
	dlComposerOptionRow(c, "dl-composer-dest", iconFolder, "保存目录",
		shortenPath(a.downloadDir(), 60), a.downloadDir(),
		bg, fg, tok,
		func() { a.pickDownloadDir() })
}

func (a *App) dlComposerSettingsRow(c *ui.Context, tok tokens) {
	dlComposerOptionRow(c, "dl-composer-settings", iconSettings, "下载设置",
		a.dlSettingsSummary(), "",
		tok.sunken, tok.muted, tok,
		func() {
			a.dlComposerOn = false
			a.settingsTab = settingsDownload
			a.tab = pageSettings
		})
}

func (a *App) dlSettingsSummary() string {
	summary := fmt.Sprintf("并发 %d · 重试 %d", clampInt(int(a.downloadConc), 1, 8),
		clampInt(int(a.downloadRetries), 0, 10))
	if a.downloadRateKB > 0 {
		summary += fmt.Sprintf(" · 限速 %d KB/s", int(a.downloadRateKB))
	} else {
		summary += " · 不限速"
	}
	if a.downloadImageFilter() != (download.ImageFilter{}) {
		summary += " · 图片筛选已启用"
	}
	return summary
}

func (a *App) dlComposerSubmit(c *ui.Context, tok tokens, count int) {
	b := uiBtn(ui.Button(c, "立即下载"), tok).Height(28).Padding(0, 14).Disabled(count == 0)
	if count > 0 {
		bg := tok.accent
		if b.Hovered() {
			bg = c.Theme().AccentHover
		}
		b.Background(bg).TextColor(tok.onAccent).Border(1, bg).Cursor(ui.CursorPointer).
			OnClick(a.submitDownloadComposer)
		hoverPressPose(b)
	} else {
		b.Background(tok.sunken).TextColor(tok.muted).Border(1, tok.border)
	}
}

// submitDownloadComposer 把弹窗里的链接加进队列并关窗。
func (a *App) submitDownloadComposer() {
	if hasQzoneCredentials(a.dlURLs) {
		a.dlComposerErr = "QQ 相册网址包含登录令牌，请移除令牌后再添加"
		return
	}
	if err := a.downloadImageFilter().Validate(); err != nil {
		a.dlComposerErr = err.Error()
		return
	}
	if a.dlBrowserMode {
		valid := parseDownloadURLs(a.dlURLs)
		if len(valid) != 1 {
			a.dlComposerErr = "浏览器模式每次需要一个网址"
			return
		}
		if a.dlBrowserBusy || a.dlBrowser == nil || !a.dlBrowser.Alive() || a.dlBrowserURL != valid[0] {
			a.dlComposerErr = "请先打开该网址的下载浏览器并完成登录"
			return
		}
	}
	if !ensureDownloadDir(a.downloadDir()) {
		a.dlComposerErr = "无法创建保存目录"
		return
	}
	added, invalid := a.addDownloadURLs(a.dlURLs)
	if added == 0 {
		if invalid > 0 {
			a.dlComposerErr = "没有可用的 HTTP / HTTPS 地址"
		} else {
			a.dlComposerErr = "先粘贴至少一个地址"
		}
		return
	}
	a.dlComposerErr = ""
	a.dlURLs = ""
	a.dlComposerOn = false
	a.saveSettings()
	// 测试里没有窗口，直接跑完队列，好让断言看到结果。
	if a.window == nil {
		a.runDownloadQueue()
	}
}

// runDownloadQueue 在没有窗口的测试里同步跑完队列。
func (a *App) runDownloadQueue() {
	for {
		var next *downloadTask
		for _, t := range a.dlTasks {
			if t.Phase == dlActive {
				next = t
				break
			}
		}
		if next == nil {
			return
		}
		// 等这条任务自己结束；队列里的协程会继续派下一条。
		for i := 0; i < 2000 && next.Phase == dlActive; i++ {
			time.Sleep(time.Millisecond)
		}
		if next.Phase == dlActive {
			return
		}
	}
}

// pickDownloadDir 让用户选一个保存目录。
func (a *App) pickDownloadDir() {
	window := a.window
	if window == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    window,
			Title:     "选择下载目录",
			Directory: true,
		})
		if err != nil {
			logging.Log.Errorf("选择下载目录失败: %v", err)
			return
		}
		if len(paths) > 0 {
			window.Update(func() { a.dlDir = paths[0] })
		}
	}()
}

// downloadDir 是这次下载写到哪儿。没手选就落在输出目录下的「下载」。
func (a *App) downloadDir() string {
	if dir := strings.TrimSpace(a.dlDir); dir != "" {
		return dir
	}
	return filepath.Join(a.resolvedOutputDir(), "下载")
}

// parseDownloadURLs 按行收地址，只留 HTTP(S)，并去掉重复。
func parseDownloadURLs(raw string) []string {
	valid, _ := parseDownloadURLsSplit(raw)
	return valid
}

// parseDownloadURLsSplit 同时给出有效地址和无效行数，弹窗里两行都写出来。
func parseDownloadURLsSplit(raw string) ([]string, int) {
	seen := map[string]bool{}
	var valid []string
	invalid := 0
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		u, err := url.Parse(line)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || download.QzoneURLHasCredentials(line) {
			invalid++
			continue
		}
		valid = append(valid, line)
	}
	return valid, invalid
}

func hasQzoneCredentials(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		if download.QzoneURLHasCredentials(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func redactQzoneURLs(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if at := strings.Index(line, trimmed); at >= 0 {
			lines[i] = line[:at] + download.RedactQzoneCredentials(trimmed) + line[at+len(trimmed):]
		}
	}
	return strings.Join(lines, "\n")
}

func dlComposerSites(urls []string) string {
	var names []string
	seen := map[string]bool{}
	for _, raw := range urls {
		site := dlSite(raw)
		if site == "" || seen[site] {
			continue
		}
		seen[site] = true
		names = append(names, site)
		if len(names) == 3 {
			break
		}
	}
	return strings.Join(names, "、")
}

// shortenPath 把长路径压成「…/最后两级」。
func shortenPath(path string, max int) string {
	path = strings.TrimSpace(path)
	if len([]rune(path)) <= max {
		return path
	}
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) <= 2 {
		runes := []rune(path)
		return "…" + string(runes[len(runes)-(max-1):])
	}
	tail := parts[len(parts)-2] + "/" + parts[len(parts)-1]
	if len([]rune(tail))+2 >= max {
		runes := []rune(path)
		return "…" + string(runes[len(runes)-(max-1):])
	}
	return "…/" + tail
}

// dlTip 把提示放在元素上方。
func dlTip(c *ui.Context, anchor ui.Element, text string) {
	anchor.Description(text)
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		th := c.Theme()
		tip.AttachTo(anchor, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, 6, 0).
			Padding(4, 8).Radius(6).Background(th.Inverse).TextColor(th.InverseText).
			Shadow(0, 4, 14, 0, ui.RGBA(16, 32, 64, 0.18)).Transition(motionPopup())
		tip.Children(func() {
			ui.Text(c, text).FontSize(12).SingleLine()
		})
	})
}

// ensureDownloadDir 准备目录；失败时把原因写进日志并返回 false。
func ensureDownloadDir(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logging.Log.Errorf("创建下载目录失败: %v", err)
		return false
	}
	return true
}
