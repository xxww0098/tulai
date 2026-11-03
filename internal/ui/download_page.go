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

// 下载页照 Moza 的迅雷式队列做：页签、右上角搜索和新建、任务行和空态。
const (
	dlPagePadX  float32 = 24
	dlPagePadR  float32 = 4
	dlRowH      float32 = 38
	dlSizeColW  float32 = 84
	dlProgColW  float32 = 150
	dlSpeedColW float32 = 80
	dlEtaColW   float32 = 70
	dlActColW   float32 = 88
	dlColGap    float32 = 12
)

// dlFaint 是列表里的次要灰，比 muted 更浅。
func dlFaint(tok tokens) ui.Color {
	if tok.dark {
		return ui.Hex("#8E8E95")
	}
	return ui.Hex("#64708A")
}

// dlQuiet 是工具栏小胶囊的底色。
func dlQuiet(tok tokens) ui.Color {
	if tok.dark {
		return ui.Hex("#232328")
	}
	return ui.Hex("#F7F9FC")
}

func dlTransferSoft(tok tokens) ui.Color {
	if tok.dark {
		return tok.accent.Alpha(0.16)
	}
	return ui.Hex("#E8F0FE")
}

func dlTransferStrong(tok tokens) ui.Color {
	if tok.dark {
		return ui.Hex("#93C5FD")
	}
	return ui.Hex("#2563EB")
}

func dlTabIdle(tok tokens) ui.Color {
	if tok.dark {
		return ui.Hex("#8E8E93")
	}
	return ui.Hex("#9B9BA3")
}

func dlTabHover(tok tokens) ui.Color {
	if tok.dark {
		return ui.Hex("#B5B5BB")
	}
	return ui.Hex("#6E6E78")
}

// downloadPage 是整页：页签、工具条、任务列表或空态，弹窗盖在上面。
func (a *App) downloadPage(c *ui.Context) {
	ui.Column(c.Key("download-page")).Fill().MinHeight(0).Children(func() {
		a.downloadHeader(c)
		if a.dlImportNotice != "" {
			ui.Text(c.Key("dl-import-notice"), a.dlImportNotice).FontSize(12).TextColor(a.look.muted).Margin(0, 0, 8, dlPagePadX)
		}
		a.downloadSubToolbar(c)
		a.downloadBatchBar(c)
		a.downloadBody(c)
	})
	a.downloadComposer(c)
	a.downloadImportModal(c)
}

func (a *App) downloadHeader(c *ui.Context) {
	tok := a.look
	ui.Row(c.Key("dl-header")).FillWidth().MinHeight(64).Padding(0, dlPagePadR, 12, dlPagePadX).
		AlignItems(ui.End).Gap(12).Children(func() {
		ui.Row(c.Key("dl-tabs")).Gap(44).Grow(1).MinWidth(0).MinHeight(32).AlignItems(ui.Center).Clip().Children(func() {
			for i, label := range dlFilterLabels {
				a.downloadTab(c, tok, i, label)
			}
		})
		ui.Row(c.Key("dl-fabs")).Gap(6).Shrink(0).AlignItems(ui.Center).Children(func() {
			a.downloadSearchFab(c, tok)
			a.downloadNewFab(c, tok)
		})
	})
}

func (a *App) downloadTab(c *ui.Context, tok tokens, index int, label string) {
	active := a.dlFilter == index
	t := ui.Text(c.Key(fmt.Sprintf("dl-tab-%d", index)), label).FontSize(16).FontWeight(500)
	switch {
	case active:
		t.TextColor(tok.text)
	case t.Hovered():
		t.TextColor(dlTabHover(tok))
	default:
		t.TextColor(dlTabIdle(tok))
	}
	t.Shrink(0).Cursor(ui.CursorPointer)
	t.OnClick(func() {
		if a.dlFilter != index {
			a.dlFilter = index
			a.dlSelectedID = 0
			a.clearDownloadChecked()
		}
	})
}

func (a *App) downloadSearchFab(c *ui.Context, tok tokens) {
	b := ui.ButtonBase(c.Key("dl-search-fab")).Size(32, 32).Radius(16).Center().Label("搜索任务")
	b.Background(tok.card).BorderWidth(1)
	if b.Hovered() {
		b.BorderColor(dlFaint(tok))
	} else {
		b.BorderColor(tok.border)
	}
	b.TextColor(tok.muted).Cursor(ui.CursorPointer)
	b.OnClick(func() { a.dlSearchOn = !a.dlSearchOn })
	b.Children(func() {
		ui.Icon(c, iconSearch).FontSize(15)
	})
	dlTip(c, b, "搜索")
	if a.dlSearchOn {
		ui.Popover(c, b, &a.dlSearchOn, func() {
			ui.Row(c.Key("dl-search-pop")).Width(340).Padding(11, 8, 11, 14).Gap(8).
				AlignItems(ui.Center).Background(tok.card).BorderWidth(1).BorderColor(tok.border).
				Radius(12).Shadow(0, 18, 48, 0, tok.shadow).Children(func() {
				ui.TextInput(c.Key("dl-search-input"), &a.dlSearch).Grow(1).MinWidth(0).
					Label("搜索任务").Placeholder("请输入搜索内容")
				close := ui.ButtonBase(c.Key("dl-search-close")).Size(22, 22).Radius(11).Center().
					Label("清除并关闭搜索").Background(dlQuiet(tok)).TextColor(dlFaint(tok)).
					Cursor(ui.CursorPointer)
				close.OnClick(func() {
					if a.dlSearch != "" {
						a.dlSearch = ""
					} else {
						a.dlSearchOn = false
					}
				})
				close.Children(func() { ui.Icon(c, iconX).FontSize(13) })
			})
		})
	}
}

func (a *App) downloadNewFab(c *ui.Context, tok tokens) {
	b := ui.ButtonBase(c.Key("dl-new-fab")).Size(32, 32).Radius(16).Center().Label("新建任务")
	bg := tok.accent
	if b.Hovered() {
		bg = dlTransferStrong(tok)
	}
	b.Background(bg).TextColor(tok.onAccent).Cursor(ui.CursorPointer)
	b.OnClick(func() {
		a.dlComposerErr = ""
		a.dlComposerOn = true
	})
	b.Children(func() {
		ui.Icon(c, iconPlus).FontSize(16)
	})
	dlTip(c, b, "新建任务")
}

func (a *App) downloadSubToolbar(c *ui.Context) {
	tok := a.look
	tasks := a.downloadView()
	count := "无任务"
	if len(tasks) > 0 {
		count = fmt.Sprintf("%d 个任务", len(tasks))
	}
	if len(a.dlTasks) > len(tasks) {
		count += fmt.Sprintf(" · 共 %d", len(a.dlTasks))
	}
	ui.Row(c.Key("dl-sub")).FillWidth().Padding(0, dlPagePadR, 10, dlPagePadX).Gap(8).
		AlignItems(ui.Center).Children(func() {
		ui.Text(c.Key("dl-count"), count).FontSize(14).FontWeight(500).TextColor(tok.text).Shrink(0)
		ui.Spacer(c)
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
	ui.Row(c.Key("dl-sub-actions")).Gap(8).Shrink(0).AlignItems(ui.Center).Children(func() {
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
		}
		if a.dlFilter != dlFilterCanceled {
			a.dlPill(c, tok, "dl-prune", "清理", iconTrash, false, false, func() {
				a.pruneDownloads()
			})
		}
	})
}

// dlPill 是工具条上的灰色小胶囊，选中时变蓝。
func (a *App) dlPill(c *ui.Context, tok tokens, key, label string, icon *ui.SVG, active, disabled bool, click func()) {
	b := ui.ButtonBase(c.Key(key)).Height(28).Radius(14).Padding(0, 10).Gap(5).
		AlignItems(ui.Center).Label(label).Disabled(disabled)
	bg, fg := dlQuiet(tok), tok.muted
	switch {
	case active:
		bg, fg = dlTransferSoft(tok), dlTransferStrong(tok)
	case b.Hovered() && !disabled:
		bg, fg = tok.sunken, tok.text
	}
	if disabled {
		fg = dlFaint(tok).Alpha(0.55)
	} else {
		b.Cursor(ui.CursorPointer).OnClick(click)
	}
	b.Background(bg).TextColor(fg)
	b.Children(func() {
		ui.Icon(c, icon).FontSize(14)
		ui.Text(c, label).FontSize(12)
	})
}

// downloadCheck 画一个 16px 的方形勾选框，点它切换 *checked。
func downloadCheck(c *ui.Context, tok tokens, checked *bool, label string) ui.Element {
	b := ui.CheckboxBase(c, checked).Size(24, 16).Shrink(0).Center().Label(label).Cursor(ui.CursorPointer)
	b.Children(func() {
		box := ui.Column(c).Size(16, 16).Radius(4).Center().BorderWidth(1)
		if *checked {
			box.Background(tok.accent).BorderColor(tok.accent)
			box.Children(func() { ui.Icon(c, iconCheck).FontSize(11).TextColor(tok.onAccent) })
			return
		}
		box.Background(tok.card)
		if b.Hovered() {
			box.BorderColor(dlFaint(tok))
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
	ui.Row(c.Key("dl-batch")).FillWidth().Padding(6, dlPagePadR, 6, dlPagePadX).Gap(6).
		AlignItems(ui.Center).Background(dlTransferSoft(tok).Alpha(0.8)).
		BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
		downloadCheck(c.Key("dl-batch-all"), tok, &a.dlAllChecked, "全选当前列表").
			OnChange(func() { a.setDownloadChecked(a.dlAllChecked) })
		ui.Text(c, fmt.Sprintf("已选 %d", len(checked))).FontSize(12).FontWeight(500).
			TextColor(dlTransferStrong(tok)).Shrink(0)
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
	ui.Column(c.Key("dl-body")).Grow(1).MinHeight(0).Children(func() {
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
	faint := dlFaint(tok)
	cell := func(label string, width float32) {
		ui.Text(c, label).FontSize(11.5).FontWeight(500).TextColor(faint).
			Width(width).TextAlign(ui.End).SingleLine()
	}
	ui.Row(c.Key("dl-columns")).FillWidth().Padding(4, dlPagePadR, 6, dlPagePadX).Gap(dlColGap).
		AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
		ui.Box(c).Width(24).Shrink(0)
		ui.Text(c, "名称").FontSize(11.5).FontWeight(500).TextColor(faint).Grow(1).MinWidth(0)
		cell("大小", dlSizeColW)
		cell("进度", dlProgColW)
		cell("速度", dlSpeedColW)
		cell("剩余时间", dlEtaColW)
		ui.Text(c, "操作").FontSize(11.5).FontWeight(500).TextColor(faint).
			Width(dlActColW).TextAlign(ui.End).SingleLine()
	})
}

func (a *App) downloadGroupHead(c *ui.Context, group dlSiteGroup) {
	tok := a.look
	ui.Row(c).FillWidth().Padding(6, dlPagePadR, 4, dlPagePadX).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, group.Label).FontSize(12).FontWeight(500).TextColor(tok.text).SingleLine()
		ui.Text(c, fmt.Sprintf("%d", len(group.Tasks))).FontSize(11).FontWeight(500).
			TextColor(dlFaint(tok)).Padding(1, 6).Radius(9).Background(dlQuiet(tok))
	})
}

// downloadRow 是单条任务：勾选框、站点与文件名、大小、进度、速度、剩余时间、操作。
func (a *App) downloadRow(c *ui.Context, t *downloadTask) {
	tok := a.look
	selected := a.dlSelectedID == t.ID
	row := ui.Row(c.Key(fmt.Sprintf("dl-row-%d", t.ID))).FillWidth().MinHeight(dlRowH).
		Padding(4, dlPagePadR, 4, dlPagePadX).Gap(dlColGap).AlignItems(ui.Center)
	bg := ui.Color{}
	switch {
	case selected:
		bg = tok.accent.Alpha(0.07)
	case row.Hovered():
		bg = dlQuiet(tok)
	case t.Checked:
		bg = dlTransferSoft(tok).Alpha(0.55)
	}
	row.Background(bg).BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Cursor(ui.CursorPointer)
	row.OnClick(func() { a.dlSelectedID = t.ID })
	showActions := row.Hovered() || selected || t.Checked

	faint := dlFaint(tok)
	row.Children(func() {
		downloadCheck(c, tok, &t.Checked, "选择 "+t.Site)
		a.downloadSourceCell(c, tok, t)
		size := taskSizeText(t)
		ui.Text(c, size).FontSize(11).TextColor(tok.muted).FontFeatures("tnum").
			Width(dlSizeColW).TextAlign(ui.End).SingleLine()
		a.downloadProgressCell(c, tok, t)
		ui.Text(c, formatSpeed(t.Speed)).FontSize(11).FontFeatures("tnum").
			TextColor(speedColor(tok, t)).Width(dlSpeedColW).TextAlign(ui.End).SingleLine()
		ui.Text(c, taskETA(t)).FontSize(11).FontFeatures("tnum").
			TextColor(faint).Width(dlEtaColW).TextAlign(ui.End).SingleLine()
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
		ink = dlFaint(tok)
	}
	tileBg, tileFg := tileColors(tok, t.Phase)
	ui.Row(c).Grow(1).MinWidth(0).Gap(8).AlignItems(ui.Center).Children(func() {
		tile := ui.Column(c).Size(20, 20).Radius(6).Center().Background(tileBg)
		tile.Children(func() { ui.Icon(c, iconGlobe).FontSize(13).TextColor(tileFg) })
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).Gap(6).MinWidth(0).AlignItems(ui.Center).Children(func() {
				ui.Text(c, t.Site).FontSize(12).FontWeight(500).TextColor(ink).
					Shrink(0).MaxWidth(180).SingleLine().Tooltip(t.Site)
				a.downloadBadge(c, tok, t)
				ui.Text(c, compactURL(t.URL)).FontSize(11).TextColor(dlFaint(tok)).
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
				ui.Text(c, sub).FontSize(10.5).TextColor(color).MinWidth(0).
					SingleLine().Tooltip(sub)
			}
		})
	})
}

func tileColors(tok tokens, phase dlPhase) (ui.Color, ui.Color) {
	switch phase {
	case dlDone:
		return tok.success.Alpha(0.14), tok.success
	case dlFailed:
		return tok.warning.Alpha(0.16), tok.warning
	case dlPaused, dlQueued, dlCanceled:
		return tok.sunken, dlFaint(tok)
	default:
		return tok.accent.Alpha(0.12), tok.accent
	}
}

func (a *App) downloadBadge(c *ui.Context, tok tokens, t *downloadTask) {
	label := t.Phase.label()
	bg, fg := tok.sunken, tok.muted
	var icon *ui.SVG = iconClock
	switch t.Phase {
	case dlActive:
		bg, fg, icon = tok.accent.Alpha(0.12), dlTransferStrong(tok), iconDownload
	case dlDone:
		bg, fg, icon = tok.success.Alpha(0.14), tok.success, iconCheck
	case dlFailed:
		bg, fg, icon = tok.danger.Alpha(0.12), tok.danger, iconAlert
	case dlPaused:
		icon = iconPause
	case dlCanceled:
		icon = iconBan
	}
	ui.Row(c).Height(20).Radius(10).Padding(0, 7).Gap(4).Shrink(0).
		AlignItems(ui.Center).Background(bg).Children(func() {
		ui.Icon(c, icon).FontSize(12).TextColor(fg)
		ui.Text(c, label).FontSize(11).FontWeight(500).TextColor(fg).SingleLine()
	})
}

func (a *App) downloadProgressCell(c *ui.Context, tok tokens, t *downloadTask) {
	frac := float32(taskProgress(t) / 100)
	bar := tok.accent
	switch t.Phase {
	case dlPaused, dlQueued, dlCanceled:
		bar = dlFaint(tok).Alpha(0.55)
	case dlFailed:
		bar = tok.danger
	case dlDone:
		bar = tok.success
	}
	trackBg := tok.accent.Alpha(0.10)
	if tok.dark {
		trackBg = tok.border.Alpha(0.7)
	}
	ui.Row(c).Width(dlProgColW).Shrink(0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Height(4).Grow(1).MinWidth(40).Radius(2).Background(trackBg).
			Draw(func(p *ui.Painter, r ui.Rect) {
				if frac <= 0 {
					return
				}
				p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * frac, H: r.H}, bar, 2)
			})
		ui.Text(c, fmt.Sprintf("%d%%", int(taskProgress(t)+0.5))).FontSize(12).FontWeight(500).
			TextColor(progressNumberColor(tok, t)).Width(34).TextAlign(ui.End).
			FontFeatures("tnum").SingleLine()
	})
}

func progressNumberColor(tok tokens, t *downloadTask) ui.Color {
	if t.Phase == dlActive {
		return dlTransferStrong(tok)
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
	b := ui.ButtonBase(c.Key(key)).Size(24, 24).Radius(6).Center().Label(label)
	bg, fg := ui.Color{}, tok.muted
	switch {
	case danger && b.Hovered():
		bg, fg = tok.danger.Alpha(0.12), tok.danger
	case b.Hovered():
		bg, fg = tok.sunken, tok.text
	}
	b.Background(bg).TextColor(fg).Cursor(ui.CursorPointer)
	b.OnClick(click)
	b.Children(func() { ui.Icon(c, icon).FontSize(14) })
	dlTip(c, b, label)
}

// downloadEmpty 是空态：淡蓝圆底图标、一句话和蓝色胶囊按钮。
func (a *App) downloadEmpty(c *ui.Context) {
	tok := a.look
	searching := strings.TrimSpace(a.dlSearch) != ""
	title, subtitle := "暂无下载任务", "点击右上角 + 新建任务，或直接拖入链接"
	if searching {
		title, subtitle = "没有匹配的任务", "试试其他关键词，或清除搜索"
	}
	ui.Column(c.Key("dl-empty")).Grow(1).FillWidth().MinHeight(280).Center().
		Padding(40, 28, 40, 28).Gap(4).Children(func() {
		circle := ui.Column(c).Size(84, 84).Radius(42).Center().Margin(0, 0, 10, 0).
			Background(dlTransferSoft(tok))
		circle.Children(func() { ui.Icon(c, iconDownload).FontSize(34).TextColor(dlTransferStrong(tok)) })
		ui.Text(c, title).FontSize(15).FontWeight(500).TextColor(tok.text).TextAlign(ui.Center)
		ui.Text(c, subtitle).FontSize(12.5).TextColor(tok.muted).MaxWidth(280).
			TextAlign(ui.Center).Margin(6, 0, 0, 0)
		ui.Row(c).Margin(20, 0, 0, 0).Justify(ui.Center).Children(func() {
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
	b := ui.ButtonBase(c.Key("dl-empty-"+label)).Height(38).MinWidth(112).Radius(19).
		Padding(0, 16).Gap(6).AlignItems(ui.Center).Center().Label(label)
	bg, fg := tok.card, tok.text
	if primary {
		bg, fg = tok.accent, tok.onAccent
		if b.Hovered() {
			bg = dlTransferStrong(tok)
		}
	} else {
		b.BorderWidth(1).BorderColor(tok.border)
		if b.Hovered() {
			fg = dlTransferStrong(tok)
		}
	}
	b.Background(bg).TextColor(fg).Cursor(ui.CursorPointer)
	b.OnClick(click)
	b.Children(func() {
		ui.Icon(c, icon).FontSize(15)
		ui.Text(c, label).FontSize(13).FontWeight(500)
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
	width := min(float32(560), max(float32(240), w-80))
	bodyHeight := max(float32(120), h-195)
	ui.Modal(c, &a.dlComposerOn, func() {
		if a.dlGuideOn {
			a.downloadGuide(c, tok)
			return
		}
		ui.Column(c.Key("dl-composer")).Width(width).Gap(14).Padding(4, 4, 4, 4).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
					ui.Text(c, "新建任务").FontSize(18).FontWeight(500).TextColor(tok.text)
					ui.Spacer(c)
					ui.Button(c.Key("dl-guide-open"), "支持哪些页面？").FontSize(12).OnClick(func() {
						a.dlGuideSearch = ""
						a.dlGuideScroll.Y = 0
						a.dlGuideOn = true
					})
				})
				ui.Text(c, "粘贴网页或图片链接，每行一个。").FontSize(13).TextColor(tok.muted)
			})
			ui.Scroll(c.Key("dl-composer-options")).FillWidth().MaxHeight(bodyHeight).Label("下载任务选项").Children(func() {
				ui.Column(c).FillWidth().Gap(14).Children(func() {
					ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
						ui.Text(c, "链接").FontSize(13).FontWeight(500).TextColor(tok.text)
						ui.Spacer(c)
						ui.Button(c, "导入 CSV / XLSX").FontSize(12).OnClick(a.pickDownloadSpreadsheet)
						ui.Text(c, "Ctrl + Enter 提交").FontSize(12).TextColor(dlFaint(tok))
					})
					ui.TextArea(c.Key("dl-composer-urls"), &a.dlURLs).Height(130).FillWidth().Label("链接").
						Placeholder("https://example.com/gallery\nhttps://example.com/photo.jpg")
					a.dlComposerMeta(c, tok, valid, invalid, sites)
					ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "扫描深度").FontSize(13).FontWeight(500).TextColor(tok.text)
						ui.NumberInput(c.Key("dl-depth"), &a.dlDepth, 1, 32767, 1).Label("扫描深度").Disabled(a.dlBrowserMode)
						if a.dlBrowserMode {
							ui.Text(c, "浏览器模式采集当前页面").FontSize(12).TextColor(tok.muted)
						} else {
							ui.Text(c, "1 为当前页，2 可跟进详情页").FontSize(12).TextColor(tok.muted)
						}
					})
					ui.Checkbox(c.Key("dl-browser-mode"), &a.dlBrowserMode, "使用浏览器登录并抓取动态页面")
					if a.dlBrowserMode {
						a.downloadBrowserControls(c, tok)
					}
					ui.Text(c, "下载到").FontSize(13).FontWeight(500).TextColor(tok.text)
					a.dlComposerDest(c, tok)
					a.dlComposerSettingsRow(c, tok)
				})
			})
			ui.Row(c).FillWidth().Margin(4, 0, 0, 0).Justify(ui.End).Gap(8).Children(func() {
				cancel := ui.Button(c, "取消").FontSize(13).Padding(8, 16).Radius(tok.radiusS).
					Background(dlQuiet(tok)).TextColor(tok.text)
				hoverPressPose(cancel)
				cancel.OnClick(func() { a.dlComposerOn = false })
				a.dlComposerSubmit(c, tok, len(valid))
			})
		})
	})
}

func (a *App) dlComposerMeta(c *ui.Context, tok tokens, valid []string, invalid int, sites string) {
	ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
		if len(valid) > 0 {
			text := fmt.Sprintf("%d 个有效链接", len(valid))
			if sites != "" {
				text += " · " + sites
			}
			ui.Text(c, text).FontSize(12).TextColor(tok.success).SingleLine()
		}
		if invalid > 0 {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).FontSize(12).TextColor(tok.danger)
				ui.Text(c, fmt.Sprintf("%d 行无效", invalid)).FontSize(12).TextColor(tok.danger)
			})
		}
		if a.dlComposerErr != "" {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, iconAlert).FontSize(12).TextColor(tok.danger)
				ui.Text(c, a.dlComposerErr).FontSize(12).TextColor(tok.danger)
			})
		}
		if len(valid) == 0 && invalid == 0 && a.dlComposerErr == "" {
			ui.Text(c, "支持 HTTP / HTTPS").FontSize(12).TextColor(dlFaint(tok))
		}
	})
}

func (a *App) dlComposerDest(c *ui.Context, tok tokens) {
	row := ui.Row(c.Key("dl-composer-dest")).FillWidth().Height(56).Padding(10, 14).Gap(12).
		AlignItems(ui.Center).Radius(tok.radiusM).BorderWidth(1).BorderColor(tok.border)
	if row.Hovered() {
		row.Background(dlQuiet(tok))
	}
	row.Cursor(ui.CursorPointer).OnClick(func() { a.pickDownloadDir() })
	row.Children(func() {
		icon := ui.Column(c).Size(32, 32).Radius(8).Center().Background(dlTransferSoft(tok))
		icon.Children(func() { ui.Icon(c, iconFolder).FontSize(16).TextColor(dlTransferStrong(tok)) })
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, "保存目录").FontSize(13).FontWeight(500).TextColor(tok.text)
			ui.Text(c, shortenPath(a.downloadDir(), 60)).FontSize(12).TextColor(tok.muted).
				SingleLine().Tooltip(a.downloadDir())
		})
		ui.Icon(c, iconChevronRight).FontSize(15).TextColor(dlFaint(tok))
	})
}

func (a *App) dlComposerSettingsRow(c *ui.Context, tok tokens) {
	row := ui.Row(c.Key("dl-composer-settings")).FillWidth().Height(56).Padding(10, 14).Gap(12).
		AlignItems(ui.Center).Radius(tok.radiusM).BorderWidth(1).BorderColor(tok.border)
	if row.Hovered() {
		row.Background(dlQuiet(tok))
	}
	row.Cursor(ui.CursorPointer).OnClick(func() {
		a.dlComposerOn = false
		a.settingsTab = settingsDownload
		a.tab = pageSettings
	})
	row.Children(func() {
		icon := ui.Column(c).Size(32, 32).Radius(8).Center().Background(tok.sunken)
		icon.Children(func() { ui.Icon(c, iconSettings).FontSize(16).TextColor(tok.muted) })
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, "下载设置").FontSize(13).FontWeight(500).TextColor(tok.text)
			ui.Text(c, a.dlSettingsSummary()).FontSize(12).TextColor(tok.muted).SingleLine()
		})
		ui.Icon(c, iconChevronRight).FontSize(15).TextColor(dlFaint(tok))
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
	b := ui.Button(c, "立即下载").FontSize(13).Padding(8, 16).Radius(tok.radiusS).
		Background(tok.accent).TextColor(tok.onAccent).Disabled(count == 0)
	if count > 0 {
		if b.Hovered() {
			b.Background(dlTransferStrong(tok))
		}
		hoverPressPose(b)
		b.Cursor(ui.CursorPointer).OnClick(a.submitDownloadComposer)
	} else {
		b.Background(tok.sunken).TextColor(dlFaint(tok))
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
