package ui

import (
	"fmt"
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

const (
	galleryTileMin = 88
	galleryTileMax = 120
	galleryTileGap = 6
	galleryTileCap = 30
)

type galleryDragState struct {
	startX, startY float32
	endX, endY     float32
	base           map[string]bool
	moved          bool
}

func (a *App) openImageGallery(title string) {
	if a.running {
		return
	}
	if title == "" {
		title = "图片"
	}
	a.galleryTitle = title
	a.gallerySelected = map[string]bool{}
	a.galleryDrag = nil
	a.galleryOn = true
}

func (a *App) gallerySelectedCount() int {
	n := 0
	for _, source := range a.sources {
		if a.gallerySelected[source.Path] {
			n++
		}
	}
	return n
}

func (a *App) galleryAllSelected() bool {
	return len(a.sources) > 0 && a.gallerySelectedCount() == len(a.sources)
}

func (a *App) setAllGallerySelection(on bool) {
	if a.gallerySelected == nil {
		a.gallerySelected = map[string]bool{}
	}
	clear(a.gallerySelected)
	if !on {
		return
	}
	for _, source := range a.sources {
		a.gallerySelected[source.Path] = true
	}
}

func (a *App) toggleGallerySelection(path string, on bool) {
	if a.gallerySelected == nil {
		a.gallerySelected = map[string]bool{}
	}
	if on {
		a.gallerySelected[path] = true
		return
	}
	delete(a.gallerySelected, path)
}

func (a *App) removeGallerySource(path string) {
	if a.running {
		return
	}
	for i, source := range a.sources {
		if source.Path != path {
			continue
		}
		a.removeSourceAt(i)
		delete(a.gallerySelected, path)
		return
	}
}

func (a *App) galleryDragInput(ev ui.InputEvent, cols int, tileW, offsetX, offsetY float32) bool {
	ev.X += offsetX
	ev.Y += offsetY
	contentW := galleryGridWidth(cols, tileW)
	contentH := galleryGridHeight(len(a.sources), cols, tileW)
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 || a.running || len(a.sources) == 0 {
			return false
		}
		a.galleryDrag = &galleryDragState{
			startX: clampGalleryPoint(ev.X, contentW),
			startY: clampGalleryPoint(ev.Y, contentH),
			endX:   clampGalleryPoint(ev.X, contentW),
			endY:   clampGalleryPoint(ev.Y, contentH),
			base:   cloneGallerySelection(a.gallerySelected),
		}
		a.updateGalleryDrag(cols, tileW)
		return true
	case ui.InputPointerMove:
		if ev.Button != 0 || a.galleryDrag == nil {
			return false
		}
		drag := a.galleryDrag
		drag.endX = clampGalleryPoint(ev.X, contentW)
		drag.endY = clampGalleryPoint(ev.Y, contentH)
		if galleryDistance2(drag.startX, drag.startY, drag.endX, drag.endY) >= 9 {
			drag.moved = true
		}
		a.updateGalleryDrag(cols, tileW)
		return true
	case ui.InputPointerUp:
		if a.galleryDrag == nil {
			return false
		}
		if ev.Canceled {
			a.gallerySelected = a.galleryDrag.base
		} else {
			a.galleryDrag.endX = clampGalleryPoint(ev.X, contentW)
			a.galleryDrag.endY = clampGalleryPoint(ev.Y, contentH)
			a.updateGalleryDrag(cols, tileW)
		}
		a.galleryDrag = nil
		return true
	default:
		return false
	}
}

func (a *App) updateGalleryDrag(cols int, tileW float32) {
	drag := a.galleryDrag
	if drag == nil {
		return
	}
	selected := cloneGallerySelection(drag.base)
	box := galleryDragRect(drag.startX, drag.startY, drag.endX, drag.endY)
	for i, source := range a.sources {
		if galleryRectsIntersect(box, galleryTileRect(i, cols, tileW)) {
			selected[source.Path] = true
		}
	}
	a.gallerySelected = selected
}

func cloneGallerySelection(src map[string]bool) map[string]bool {
	out := make(map[string]bool, len(src))
	for path, selected := range src {
		if selected {
			out[path] = true
		}
	}
	return out
}

func galleryDistance2(x1, y1, x2, y2 float32) float32 {
	dx, dy := x2-x1, y2-y1
	return dx*dx + dy*dy
}

func clampGalleryPoint(value, limit float32) float32 {
	return min(max(float32(0), value), max(float32(0), limit))
}

func galleryDragRect(x1, y1, x2, y2 float32) ui.Rect {
	left, top := min(x1, x2), min(y1, y2)
	return ui.Rect{X: left, Y: top, W: max(x1, x2) - left, H: max(y1, y2) - top}
}

func galleryRectsIntersect(a, b ui.Rect) bool {
	return a.X <= b.X+b.W && a.X+a.W >= b.X && a.Y <= b.Y+b.H && a.Y+a.H >= b.Y
}

func galleryTileHeight(tileW float32) float32 {
	return tileW + galleryTileCap
}

func galleryGridWidth(cols int, tileW float32) float32 {
	if cols <= 0 {
		return 0
	}
	return float32(cols)*tileW + float32(cols-1)*galleryTileGap
}

func galleryGridHeight(total, cols int, tileW float32) float32 {
	if total <= 0 || cols <= 0 {
		return 0
	}
	rows := (total + cols - 1) / cols
	return float32(rows)*galleryTileHeight(tileW) + float32(rows-1)*galleryTileGap
}

func galleryTileRect(index, cols int, tileW float32) ui.Rect {
	if cols <= 0 {
		return ui.Rect{}
	}
	row, col := index/cols, index%cols
	return ui.Rect{
		X: float32(col) * (tileW + galleryTileGap),
		Y: float32(row) * (galleryTileHeight(tileW) + galleryTileGap),
		W: tileW,
		H: galleryTileHeight(tileW),
	}
}

func (a *App) deleteSelectedGallerySources() {
	if a.running || a.gallerySelectedCount() == 0 {
		return
	}
	kept := a.sources[:0]
	for _, source := range a.sources {
		if !a.gallerySelected[source.Path] {
			kept = append(kept, source)
		}
	}
	a.sources = kept
	if a.srcSel >= len(a.sources) {
		a.srcSel = len(a.sources) - 1
	}
	clear(a.gallerySelected)
	if len(a.sources) == 0 {
		a.skipImages = false
	}
}

// clearWashedGallerySources 只从洗图列表移除已成功保存结果的原图。
func (a *App) clearWashedGallerySources() {
	if a.running || !a.washing() {
		return
	}
	washed, _ := a.washCounts()
	if washed == 0 {
		return
	}
	kept := a.sources[:0]
	selectedIndex := -1
	for i, source := range a.sources {
		if source.Washed {
			delete(a.gallerySelected, source.Path)
			continue
		}
		if i == a.srcSel {
			selectedIndex = len(kept)
		}
		kept = append(kept, source)
	}
	clear(a.sources[len(kept):])
	a.sources = kept
	if selectedIndex >= 0 {
		a.srcSel = selectedIndex
	} else if a.srcSel >= len(a.sources) {
		a.srcSel = len(a.sources) - 1
	}
	if len(a.sources) == 0 {
		a.skipImages = false
	}
	a.galleryDrag = nil
}

func (a *App) galleryModal(c *ui.Context) {
	if !a.galleryOn {
		return
	}
	tok := a.look
	winW, winH := c.Size()
	panelW := galleryPanelWidth(winW)
	viewportH := galleryViewportHeight(winH)
	cols, tileW := galleryLayout(panelW)
	selected := a.gallerySelectedCount()
	total := len(a.sources)
	all := a.galleryAllSelected()
	washed, _ := a.washCounts()

	ui.Modal(c, &a.galleryOn, func() {
		panel := ui.Column(c.Key("image-gallery-modal")).Width(panelW).Gap(12).Padding(14).
			Background(tok.card).Border(1, tok.border).Radius(tok.radiusL).
			Shadow(0, 18, 48, 0, tok.lift).Role(ui.RoleDialog).
			Label(a.galleryTitle + "图库")
		if paths := panel.DroppedFiles(); len(paths) > 0 {
			a.ingestSources(paths)
		}
		if panel.FileDragOver() && !a.running {
			panel.Border(2, tok.accent).Background(tok.accent.Alpha(0.08))
		}
		panel.Children(func() {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
					ui.Text(c, a.galleryTitle+"图库").FontSize(tok.title).Bold().SingleLine()
					ui.Text(c, fmt.Sprintf("共 %d 张 · 按住左键拖动可选择 · 勾选后可批量删除", total)).
						FontSize(tok.caption).TextColor(tok.muted).SingleLine()
				})
				ui.Button(c, "关闭").FontSize(tok.caption).Padding(3, 9).Radius(tok.radiusS).
					OnClick(func() { a.galleryOn = false })
			})
			ui.Row(c).FillWidth().Gap(8).Wrap().AlignItems(ui.Center).Children(func() {
				a.quietButton(c, "添加图片").OnClick(func() { a.addImages() })
				if a.washing() {
					a.quietButton(c, "选文件夹").OnClick(func() { a.addFolder() })
				}
				ui.Spacer(c)
				if a.washing() {
					a.quietButton(c.Key("gallery-clear-washed"), "清除已洗").
						Tooltip("从图库移除已洗原图，保留原文件和洗图结果").
						Disabled(a.running || washed == 0).
						OnClick(func() { a.clearWashedGallerySources() })
				}
				clearButton := a.quietButton(c, "清空").Disabled(a.running || total == 0)
				clearButton.OnClick(func() { a.clearSources() })
			})
			if total == 0 {
				a.galleryEmpty(c, viewportH)
				return
			}

			ui.Scroll(c.Key("image-gallery-scroll")).FillWidth().Height(viewportH).Children(func() {
				tracks := make([]ui.Track, cols)
				for i := range tracks {
					tracks[i] = ui.Fixed(tileW)
				}
				grid := ui.Grid(c).ColumnTracks(tracks...).Gap(galleryTileGap).
					JustifyItems(ui.Start).AlignItems(ui.Start).Padding(0, 0, 12, 0).Cursor(ui.CursorPointer)
				grid.Children(func() {
					for i, source := range a.sources {
						a.galleryTile(c, i, source, cols, tileW)
					}
					a.galleryDragOverlay(c)
				})
			})
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				checked := all
				allBox := ui.Checkbox(c.Key("gallery-all"), &checked, "全选").Label("全选")
				allBox.OnChange(func() { a.setAllGallerySelection(checked) })
				ui.Text(c, fmt.Sprintf("已选 %d 张", selected)).FontSize(tok.caption).
					TextColor(tok.muted).FontFeatures("tnum").Grow(1)
				remove := ui.Button(c, fmt.Sprintf("删除已选 %d 张", selected)).
					FontSize(tok.caption).Padding(3, 9).Radius(tok.radiusS).
					Background(tok.danger.Alpha(0.12)).TextColor(tok.danger).
					Disabled(a.running || selected == 0)
				remove.OnClick(func() { a.deleteSelectedGallerySources() })
			})
		})
	})
}

func (a *App) galleryDragOverlay(c *ui.Context) {
	drag := a.galleryDrag
	if drag == nil || !drag.moved {
		return
	}
	box := galleryDragRect(drag.startX, drag.startY, drag.endX, drag.endY)
	ui.Box(c.Key("gallery-drag-box")).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).
		Background(a.look.accent.Alpha(0.10)).Border(1, a.look.accent.Alpha(0.75)).
		Radius(a.look.radiusS).IgnoreInput(true)
}

func (a *App) galleryEmpty(c *ui.Context, height float32) {
	tok := a.look
	zone := ui.Column(c.Key("gallery-empty")).FillWidth().Height(min(height, float32(260))).
		Radius(tok.radiusM).Border(1.5, tok.accent.Alpha(0.45)).BorderStyle(ui.BorderDashed).
		Background(tok.sunken).Center().Gap(8).Cursor(ui.CursorPointer)
	if zone.Hovered() && !a.running {
		zone.Border(1.5, tok.accent).Background(tok.accent.Alpha(0.08))
	}
	if zone.Clicked() && !a.running {
		a.addImages()
	}
	zone.Children(func() {
		ui.Icon(c, iconImages).FontSize(30).TextColor(tok.accent)
		ui.Text(c, "拖入图片或文件夹").FontSize(tok.body).Bold()
		ui.Text(c, "图库会保持打开，方便连续添加和整理").FontSize(tok.caption).TextColor(tok.muted)
	})
}

func (a *App) galleryTile(c *ui.Context, index int, source sourceImage, cols int, tileW float32) {
	tok := a.look
	name := filepath.Base(source.Path)
	tileRect := galleryTileRect(index, cols, tileW)
	selected := a.gallerySelected[source.Path]
	borderColor := tok.border
	borderWidth := float32(1)
	if selected {
		borderColor = tok.accent
		borderWidth = 2
	}
	ui.Column(c.Key("gallery-"+source.Path)).Width(tileW).Height(galleryTileHeight(tileW)).Shrink(0).Gap(5).
		Padding(1).Radius(tok.radiusM).Border(borderWidth, borderColor).Background(tok.card).
		Children(func() {
			host := ui.Box(c.Key("image")).Size(tileW, tileW).Radius(tok.radiusS).
				Background(tok.sunken).Clip()
			host.Children(func() {
				if bmp := a.cachedThumb(source.Path); bmp != nil {
					ui.Image(c, bmp).Size(tileW, tileW).Fit(ui.Cover).Radius(tok.radiusS)
				}
				ui.Box(c.Key("drag-select")).Size(tileW, tileW).Absolute().Top(0).Left(0).
					Label("拖动选择 " + name).Cursor(ui.CursorPointer).HandleInput(func(ev ui.InputEvent) bool {
					return a.galleryDragInput(ev, cols, tileW, tileRect.X, tileRect.Y)
				})
				checked := selected
				pick := ui.Checkbox(c.Key("pick"), &checked, "").
					Label(fmt.Sprintf("选择 %02d %s", index+1, name)).
					Size(24, 24).Absolute().Top(7).Left(7)
				pick.OnChange(func() { a.toggleGallerySelection(source.Path, checked) })
				remove := ui.ButtonBase(c.Key("remove")).Size(45, 24).Radius(6).Center().
					Label("移除 " + name).Tooltip("从图库移除").
					Background(ui.RGBA(16, 24, 38, 0.82)).TextColor(ui.RGBA(255, 255, 255, 0.96)).
					Absolute().Top(7).Right(7).Disabled(a.running)
				remove.OnClick(func() { a.removeGallerySource(source.Path) })
				remove.Children(func() {
					ui.Text(c, "移除").FontSize(11).TextColor(ui.RGBA(255, 255, 255, 0.96))
				})
			})
			ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Padding(0, 1).Children(func() {
				ui.Text(c, fmt.Sprintf("%02d", index+1)).Width(24).Shrink(0).
					FontSize(tok.caption).TextColor(tok.muted).FontFeatures("tnum")
				ui.Text(c, name).FontSize(tok.caption).Grow(1).MinWidth(0).SingleLine().Tooltip(source.Path)
			})
		})
}

func galleryPanelWidth(winW float32) float32 {
	if winW <= 0 {
		return 900
	}
	return min(float32(980), max(float32(280), winW-48))
}

func galleryViewportHeight(winH float32) float32 {
	if winH <= 0 {
		return 540
	}
	return min(float32(560), max(float32(220), winH-230))
}

func galleryLayout(panelW float32) (int, float32) {
	inner := max(float32(galleryTileMin), panelW-28)
	cols := int((inner + galleryTileGap) / (galleryTileMin + galleryTileGap))
	cols = min(max(cols, 1), 6)
	tile := (inner - float32(cols-1)*galleryTileGap) / float32(cols)
	if tile > galleryTileMax {
		tile = galleryTileMax
	}
	return cols, tile
}
