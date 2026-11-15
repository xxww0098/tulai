package ui

import (
	"fmt"
	"math"
	"path/filepath"

	"github.com/egoist/mygo/ui"
)

const (
	editThumbSize float32 = 48
	editThumbGap  float32 = 6
)

// editStage 是右侧舞台：空态放拖放区，有图时是信息栏、大图预览加胶片条。
func (a *App) editStage(c *ui.Context, be editPalette) {
	st := &a.edit
	stage := ui.Column(c.Key("edit-stage")).Grow(1).MinWidth(0).MinHeight(0).
		Background(be.paper)
	// 舞台自己也标了可拖放，落点在这里时只有它能收到文件。
	if paths := stage.DroppedFiles(); len(paths) > 0 {
		a.editAddPaths(paths)
	}
	stage.Children(func() {
		if len(st.files) == 0 {
			wrap := ui.Column(c).Fill().Padding(14)
			wrap.Children(func() {
				a.editDropzone(c, be)
			})
		} else {
			loaded := ui.Column(c).Fill().MinHeight(0)
			loaded.Children(func() {
				a.editStageBar(c, be)
				a.editPreview(c, be)
				a.editFilmstrip(c, be)
			})
		}
		if stage.FileDragOver() {
			overlay := ui.Box(c).Absolute().Left(12).Top(12).Right(12).Bottom(12).Center().
				Radius(be.tok.radiusL).Background(be.transferSoft).
				BorderWidth(2).BorderStyle(ui.BorderDashed).BorderColor(be.transfer)
			overlay.Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconImagePlus).FontSize(18).TextColor(be.transferStrong)
					ui.Text(c, "松开以添加图片").FontSize(14).FontWeight(600).TextColor(be.transferStrong)
				})
			})
		}
	})
}

func (a *App) editStageBar(c *ui.Context, be editPalette) {
	st := &a.edit
	f := st.selectedFile()
	if f == nil {
		return
	}
	bar := ui.Row(c.Key("edit-stage-bar")).FillWidth().Height(32).Padding(0, 12).Gap(8).
		AlignItems(ui.Center).Background(be.sheet).BorderWidth(0, 0, 1, 0).BorderColor(be.rule)
	bar.Children(func() {
		ui.Icon(c, iconFileImage).FontSize(13).TextColor(be.muted).Shrink(0)
		ui.Text(c, f.Name).FontSize(be.tok.body).FontWeight(500).TextColor(be.ink).SingleLine().Shrink(1).MinWidth(0)
		if st.srcW > 0 && st.srcH > 0 {
			ui.Box(c).Size(1, 12).Background(be.rule).Shrink(0)
			ui.Text(c, fmt.Sprintf("%d × %d", st.srcW, st.srcH)).FontSize(be.tok.caption).
				TextColor(be.muted).FontFeatures("tnum").SingleLine().Shrink(0)
			if st.outW > 0 && st.outH > 0 && (st.outW != st.srcW || st.outH != st.srcH) {
				ui.Icon(c, iconChevronRight).FontSize(11).TextColor(be.muted).Shrink(0)
				uiBadge(c, "edit-out-dims", fmt.Sprintf("%d × %d", st.outW, st.outH), "accent", be.tok)
			}
		}
		ui.Box(c).Grow(1)
		switch {
		case f.Status == "ok":
			uiBadge(c, "edit-file-status", "已导出", "success", be.tok)
		case f.Status == "failed":
			uiBadge(c, "edit-file-status", "处理失败", "danger", be.tok)
		case st.compare && !a.editShowCropUI():
			uiBadge(c, "edit-mode-badge", "分屏对比", "accent", be.tok)
		case a.editShowCropUI():
			uiBadge(c, "edit-mode-badge", "框选裁切区域", "accent", be.tok)
		}
	})
}

func (a *App) editDropzone(c *ui.Context, be editPalette) {
	zone := ui.Column(c.Key("edit-dropzone")).Fill().MinHeight(220).Center().Gap(12).
		Radius(be.tok.radiusL).Background(be.sheet).Cursor(ui.CursorPointer)
	zone.BorderWidth(1.5).BorderStyle(ui.BorderDashed).
		BorderColor(be.tok.borderStrong)
	zone.Draw(func(p *ui.Painter, r ui.Rect) {
		if zone.Hovered() {
			p.Fill(r, be.transferSoft.Alpha(0.55), be.tok.radiusL)
			p.Stroke(r, be.transferBorder, be.tok.radiusL, 1.5)
		}
	})
	zone.OnClick(func() { a.editPickFiles() })
	zone.Children(func() {
		hero := ui.Box(c).Size(44, 44).Center().Radius(be.tok.radiusM).Background(be.transferSoft).
			Border(1, be.transferBorder)
		hero.Children(func() { ui.Icon(c, iconImagePlus).FontSize(20).TextColor(be.transfer) })
		ui.Column(c).Center().Gap(4).Children(func() {
			ui.Text(c, "拖入图片或文件夹开始批处理").FontSize(be.tok.display).FontWeight(600).TextColor(be.ink)
			ui.Text(c, "支持 PNG / JPEG / WebP / AVIF，也可直接粘贴文件或路径").FontSize(be.tok.caption).TextColor(be.muted)
		})
		actions := ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(4, 0, 0, 0)
		actions.Children(func() {
			open := a.editPillButton(c, be, "打开图片", iconImagePlus, true)
			open.OnClick(func() { a.editPickFiles() })
			folder := a.editPillButton(c, be, "打开文件夹", iconFolderOpen, false)
			folder.OnClick(func() { a.editPickFolder() })
		})
	})
}

func (a *App) editPillButton(c *ui.Context, be editPalette, label string, icon *ui.SVG, primary bool) ui.Element {
	ink := be.ink
	el := ui.Row(c).Height(30).Padding(0, 12).Gap(6).Radius(be.tok.radiusS).AlignItems(ui.Center).
		Cursor(ui.CursorPointer)
	if primary {
		ink = be.onTransfer
		el.Background(be.transfer).Shadow(0, 1, 2, 0, be.shadow)
	} else {
		el.Background(be.sheet).Border(1, be.rule)
	}
	el.Draw(func(p *ui.Painter, r ui.Rect) {
		if !el.Hovered() {
			return
		}
		if primary {
			p.Fill(r, ui.RGBA(0, 0, 0, 0.08), be.tok.radiusS)
		} else {
			p.Fill(r, be.quiet, be.tok.radiusS)
		}
	})
	el.Children(func() {
		ui.Icon(c, icon).FontSize(14).TextColor(ink)
		ui.Text(c, label).FontSize(12).FontWeight(500).TextColor(ink).SingleLine()
	})
	return el
}

// editPreview 是图片舞台。图、裁切框、对比分割线都由 Draw 画，
// 指针和滚轮在同一个元素上处理。
func (a *App) editPreview(c *ui.Context, be editPalette) {
	st := &a.edit
	showCrop := a.editShowCropUI()
	// HUD 与舞台做兄弟节点：舞台带 HandleInput，做成它的子元素会把
	// HUD 上的点击先吃掉，按钮就点不动了。
	wrap := ui.Box(c.Key("edit-stage-preview")).Grow(1).MinHeight(0).FillWidth()
	wrap.Children(func() {
		preview := ui.Box(c.Key("edit-preview")).Fill().MinHeight(0).
			Clip().Focusable()
		if showCrop {
			preview.Cursor(ui.CursorCrosshair)
		} else if st.panning {
			preview.Cursor(ui.CursorGrabbing)
		} else if st.zoom > 1 {
			preview.Cursor(ui.CursorGrab)
		}
		preview.Draw(func(p *ui.Painter, r ui.Rect) {
			a.editDrawPreview(p, r, c, be)
		})
		preview.HandleInput(func(ev ui.InputEvent) bool {
			return a.editPreviewInput(ev, be)
		})
		preview.Children(func() {
			if st.orig == nil && st.out == nil {
				holder := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center()
				holder.Children(func() { ui.Spinner(c).Size(28, 28) })
			}
		})
		if st.orig != nil || st.out != nil {
			a.editPreviewHUD(c, be, showCrop)
		}
	})
}

// editImageLayout 算出这一帧图片在舞台里的位置，顺手记下供指针换算用。
func (a *App) editImageLayout(r ui.Rect) (ui.Rect, bool) {
	st := &a.edit
	bmp := st.out
	if st.compare && st.orig != nil {
		bmp = st.orig
	}
	if a.editShowCropUI() || bmp == nil {
		bmp = st.orig
	}
	if bmp == nil {
		return ui.Rect{}, false
	}
	bw, bh := bmp.Size()
	if bw <= 0 || bh <= 0 {
		return ui.Rect{}, false
	}
	innerW := float32(math.Max(1, float64(r.W-20)))
	innerH := float32(math.Max(1, float64(r.H-20)))
	// 界面缩放不应把尺寸小于舞台的图片缩到实际大小以下。
	fit := float32(math.Min(1/float64(interfaceZoom), math.Min(float64(innerW)/float64(bw), float64(innerH)/float64(bh))))
	baseW, baseH := float32(bw)*fit, float32(bh)*fit
	z := float32(st.zoom)
	w, h := baseW*z, baseH*z
	cx := r.X + r.W/2 + float32(st.panX)
	cy := r.Y + r.H/2 + float32(st.panY)
	img := ui.Rect{X: cx - w/2, Y: cy - h/2, W: w, H: h}
	st.stageRect = r
	st.imageRect = img
	st.imageLocal = ui.Rect{X: img.X - r.X, Y: img.Y - r.Y, W: img.W, H: img.H}
	st.baseW, st.baseH = baseW, baseH
	st.previewW, st.previewH = r.W, r.H
	return img, true
}

func (a *App) editDrawPreview(p *ui.Painter, r ui.Rect, c *ui.Context, be editPalette) {
	st := &a.edit
	img, ok := a.editImageLayout(r)
	if !ok {
		return
	}
	showCrop := a.editShowCropUI()
	box, hasBox := editCropBox{}, false
	if showCrop {
		if st.liveBox != nil {
			box, hasBox = *st.liveBox, true
		} else if f := st.selectedFile(); f != nil {
			box, hasBox = a.editCropBoxFor(f.Path)
		}
	}

	p.Clip(r, 0, func() {
		switch {
		case st.compare && !showCrop && st.orig != nil && st.out != nil:
			a.editDrawBitmap(p, st.orig, img, 6, be)
			outRect := editContainRect(st.out, img)
			splitX := img.X + img.W*float32(st.split)/100
			if splitX > img.X {
				clip := ui.Rect{X: splitX, Y: img.Y, W: img.X + img.W - splitX, H: img.H}
				p.Clip(clip, 0, func() {
					a.editDrawBitmap(p, st.out, outRect, 6, be)
				})
			}
			a.editDrawSplit(p, img, splitX, be)
			a.editDrawCompareTags(p, c, img)
		default:
			bmp := st.out
			if showCrop || bmp == nil {
				bmp = st.orig
			}
			if bmp != nil {
				a.editDrawBitmap(p, bmp, img, 6, be)
			}
		}
		if showCrop {
			if hasBox {
				a.editDrawCropOverlay(p, c, img, box)
			} else {
				p.Fill(img, ui.RGBA(0, 0, 0, 0.45), 6)
			}
		}
	})
}

// editContainRect 把 bmp 按自身比例放进 box，对应 CSS object-fit: contain。
func editContainRect(bmp *ui.Bitmap, box ui.Rect) ui.Rect {
	if bmp == nil || box.W <= 0 || box.H <= 0 {
		return box
	}
	bw, bh := bmp.Size()
	if bw <= 0 || bh <= 0 {
		return box
	}
	scale := math.Min(float64(box.W)/float64(bw), float64(box.H)/float64(bh))
	w := float32(float64(bw) * scale)
	h := float32(float64(bh) * scale)
	return ui.Rect{X: box.X + (box.W-w)/2, Y: box.Y + (box.H-h)/2, W: w, H: h}
}

func (a *App) editDrawBitmap(p *ui.Painter, bmp *ui.Bitmap, dst ui.Rect, radius float32, be editPalette) {
	if bmp == nil || dst.W <= 0 || dst.H <= 0 {
		return
	}
	p.Shadow(dst, radius, 0, 2, 8, 0, be.shadow)
	p.Clip(dst, radius, func() {
		p.Fill(dst, be.sheet, 0)
		// 透明区域棋盘格，格数随缩放限制上限以保证绘制轻量。
		tile := float32(10)
		for float32(dst.W/tile)*float32(dst.H/tile) > 900 {
			tile *= 1.5
		}
		cols := int(math.Ceil(float64(dst.W / tile)))
		rows := int(math.Ceil(float64(dst.H / tile)))
		for row := 0; row < rows; row++ {
			for col := 0; col < cols; col++ {
				if (row+col)%2 == 0 {
					continue
				}
				x := dst.X + float32(col)*tile
				y := dst.Y + float32(row)*tile
				w := float32(math.Min(float64(tile), float64(dst.X+dst.W-x)))
				h := float32(math.Min(float64(tile), float64(dst.Y+dst.H-y)))
				p.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, be.quiet, 0)
			}
		}
		p.ImageRegion(bmp, ui.Rect{}, dst, ui.ImageOptions{})
	})
	p.Stroke(dst, be.rule.Alpha(0.85), radius, 1)
}

func (a *App) editDrawCompareTags(p *ui.Painter, c *ui.Context, img ui.Rect) {
	a.editDrawChipText(p, c, "原图", img.X+8, img.Y+8, 10)
	a.editDrawChipTextRight(p, c, "效果", img.X+img.W-8, img.Y+8, 10)
}

func (a *App) editDrawChipText(p *ui.Painter, c *ui.Context, text string, x, y, size float32) {
	w, h := c.MeasureText(0, ui.Span{Text: text, Size: size})
	r := ui.Rect{X: x, Y: y, W: w + 16, H: h + 6}
	p.Fill(r, ui.RGBA(15, 23, 42, 0.78), 4)
	p.Text(x+8, y+3, text, size, ui.RGBA(248, 250, 252, 1))
}

func (a *App) editDrawChipTextRight(p *ui.Painter, c *ui.Context, text string, right, y, size float32) {
	w, h := c.MeasureText(0, ui.Span{Text: text, Size: size})
	x := right - (w + 16)
	r := ui.Rect{X: x, Y: y, W: w + 16, H: h + 6}
	p.Fill(r, ui.RGBA(15, 23, 42, 0.78), 4)
	p.Text(x+8, y+3, text, size, ui.RGBA(248, 250, 252, 1))
}

func (a *App) editDrawSplit(p *ui.Painter, img ui.Rect, x float32, be editPalette) {
	line := ui.Rect{X: x - 1, Y: img.Y, W: 2, H: img.H}
	p.Shadow(line, 0, 0, 0, 4, 0, ui.RGBA(0, 0, 0, 0.45))
	p.Fill(line, be.sheet, 0)
	cx := x
	cy := img.Y + img.H/2
	knob := ui.Rect{X: cx - 12, Y: cy - 12, W: 24, H: 24}
	p.Shadow(knob, 12, 0, 1, 3, 0, be.shadow)
	p.Fill(knob, be.sheet, 12)
	p.Stroke(knob, be.transfer, 12, 1.5)
	ink := be.ink
	p.StrokePolyline([][2]float32{{cx - 3, cy - 4}, {cx - 7, cy}, {cx - 3, cy + 4}}, 1.5, ink)
	p.StrokePolyline([][2]float32{{cx + 3, cy - 4}, {cx + 7, cy}, {cx + 3, cy + 4}}, 1.5, ink)
}

func (a *App) editDrawCropOverlay(p *ui.Painter, c *ui.Context, img ui.Rect, box editCropBox) {
	st := &a.edit
	if st.srcW <= 0 || st.srcH <= 0 || box.W <= 0 || box.H <= 0 {
		return
	}
	sx := img.W / float32(st.srcW)
	sy := img.H / float32(st.srcH)
	rect := ui.Rect{
		X: img.X + float32(box.X)*sx,
		Y: img.Y + float32(box.Y)*sy,
		W: float32(box.W) * sx,
		H: float32(box.H) * sy,
	}
	dim := ui.RGBA(0, 0, 0, 0.45)
	if rect.Y > img.Y {
		p.Fill(ui.Rect{X: img.X, Y: img.Y, W: img.W, H: rect.Y - img.Y}, dim, 0)
	}
	if rect.Y+rect.H < img.Y+img.H {
		p.Fill(ui.Rect{X: img.X, Y: rect.Y + rect.H, W: img.W, H: img.Y + img.H - rect.Y - rect.H}, dim, 0)
	}
	if rect.X > img.X {
		p.Fill(ui.Rect{X: img.X, Y: rect.Y, W: rect.X - img.X, H: rect.H}, dim, 0)
	}
	if rect.X+rect.W < img.X+img.W {
		p.Fill(ui.Rect{X: rect.X + rect.W, Y: rect.Y, W: img.X + img.W - rect.X - rect.W, H: rect.H}, dim, 0)
	}
	p.Stroke(rect, ui.RGBA(255, 255, 255, 1), 0, 2)
	white := ui.RGBA(255, 255, 255, 1)
	edge := ui.RGBA(0, 0, 0, 0.35)
	hairline := ui.Rect{X: rect.X + 1, Y: rect.Y + 1, W: rect.W - 2, H: rect.H - 2}
	p.Stroke(hairline, ui.RGBA(0, 0, 0, 0.25), 0, 1)
	for _, handle := range editCropHandles(rect) {
		h := ui.Rect{X: handle.X - 7, Y: handle.Y - 7, W: 14, H: 14}
		p.Shadow(h, 3, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.3))
		p.Fill(h, white, 3)
		p.Stroke(h, edge, 3, 1)
	}
	text := fmt.Sprintf("%d×%d", int(math.Round(box.W)), int(math.Round(box.H)))
	tw, th := c.MeasureText(0, ui.Span{Text: text, Size: 10})
	chip := ui.Rect{X: rect.X + rect.W/2 - tw/2 - 8, Y: rect.Y + rect.H - th - 8, W: tw + 16, H: th + 4}
	if chip.Y < rect.Y {
		chip.Y = rect.Y + 4
	}
	p.Fill(chip, ui.RGBA(0, 0, 0, 0.58), chip.H/2)
	p.Text(chip.X+8, chip.Y+2, text, 10, white)
}

type editHandle struct {
	Name string
	X, Y float32
}

func editCropHandles(r ui.Rect) []editHandle {
	return []editHandle{
		{"nw", r.X, r.Y},
		{"ne", r.X + r.W, r.Y},
		{"sw", r.X, r.Y + r.H},
		{"se", r.X + r.W, r.Y + r.H},
		{"n", r.X + r.W/2, r.Y},
		{"s", r.X + r.W/2, r.Y + r.H},
		{"e", r.X + r.W, r.Y + r.H/2},
		{"w", r.X, r.Y + r.H/2},
	}
}

// editPreviewInput 处理缩放、平移、对比拖动和裁切框。
func (a *App) editPreviewInput(ev ui.InputEvent, be editPalette) bool {
	st := &a.edit
	showCrop := a.editShowCropUI()
	switch ev.Kind {
	case ui.InputScroll:
		if st.orig == nil && st.out == nil {
			return false
		}
		r := st.stageRect
		cx := float64(ev.X - r.W/2)
		cy := float64(ev.Y - r.H/2)
		if ev.DY > 0 {
			a.editZoomBy(0.9, cx, cy)
		} else {
			a.editZoomBy(1.1, cx, cy)
		}
		a.editClampPan()
		return true
	case ui.InputKeyDown:
		return a.editPreviewKey(ev, be)
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		if showCrop {
			a.editCropDown(ev)
			return true
		}
		if st.compare && a.editSplitHit(ev.X, ev.Y) {
			st.dragMode = "split"
			return true
		}
		r := st.stageRect
		cx := float64(ev.X - r.W/2)
		cy := float64(ev.Y - r.H/2)
		if ev.Clicks >= 2 {
			if st.zoom > 1 {
				st.zoom, st.panX, st.panY = 1, 0, 0
			} else {
				a.editZoomBy(2/st.zoom, cx, cy)
				a.editClampPan()
			}
			return true
		}
		st.dragMode = "pan"
		st.panning = true
		st.panStartX = float64(ev.X)
		st.panStartY = float64(ev.Y)
		st.panOriginX = st.panX
		st.panOriginY = st.panY
		return true
	case ui.InputPointerMove:
		switch st.dragMode {
		case "pan":
			st.panX = st.panOriginX + float64(ev.X) - st.panStartX
			st.panY = st.panOriginY + float64(ev.Y) - st.panStartY
			a.editClampPan()
			return true
		case "split":
			img := st.imageLocal
			if img.W <= 0 {
				return true
			}
			pct := (float64(ev.X) - float64(img.X)) / float64(img.W) * 100
			st.split = math.Min(96, math.Max(4, pct))
			return true
		case "crop":
			a.editCropMove(ev)
			return true
		}
		return false
	case ui.InputPointerUp:
		switch st.dragMode {
		case "crop":
			a.editCropUp()
			st.dragMode = ""
			return true
		case "pan":
			st.dragMode = ""
			st.panning = false
			return true
		case "split":
			st.dragMode = ""
			return true
		}
	}
	return false
}

func (a *App) editPreviewKey(ev ui.InputEvent, be editPalette) bool {
	st := &a.edit
	showCrop := a.editShowCropUI()
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	if showCrop && (ev.Key == ui.KeyLeft || ev.Key == ui.KeyRight || ev.Key == ui.KeyUp || ev.Key == ui.KeyDown) {
		f := st.selectedFile()
		if f == nil || st.srcW <= 0 {
			return true
		}
		box, ok := a.editCropBoxFor(f.Path)
		if st.liveBox != nil {
			box, ok = *st.liveBox, true
		}
		if !ok {
			return true
		}
		step := 1.0
		if ev.Mods&ui.Shift != 0 {
			step = 10
		}
		switch ev.Key {
		case ui.KeyLeft:
			box.X -= step
		case ui.KeyRight:
			box.X += step
		case ui.KeyUp:
			box.Y -= step
		case ui.KeyDown:
			box.Y += step
		}
		box.X = math.Min(math.Max(0, box.X), math.Max(0, float64(st.srcW)-box.W))
		box.Y = math.Min(math.Max(0, box.Y), math.Max(0, float64(st.srcH)-box.H))
		a.editSetCropBox(&box)
		return true
	}
	if ev.Key == ui.KeyDelete || ev.Key == ui.KeyBackspace {
		if idx := st.selected; idx >= 0 && idx < len(st.files) && !st.running {
			a.editRemoveFile(idx)
			return true
		}
		return false
	}
	if ev.Key == ui.KeyLeft || ev.Key == ui.KeyRight {
		idx := st.selected
		if ev.Key == ui.KeyRight {
			idx++
		} else {
			idx--
		}
		if idx >= 0 && idx < len(st.files) {
			a.editSelect(idx)
			a.editScrollSelected()
			return true
		}
	}
	return false
}

func (a *App) editSplitHit(x, y float32) bool {
	st := &a.edit
	img := st.imageLocal
	if img.W <= 0 || y < img.Y || y > img.Y+img.H {
		return false
	}
	splitX := img.X + img.W*float32(st.split)/100
	return math.Abs(float64(x-splitX)) <= 12
}

// editCropDown 是裁切模式下的按下：命中把手就拉伸，框内就移动，否则重新画框。
func (a *App) editCropDown(ev ui.InputEvent) {
	st := &a.edit
	f := st.selectedFile()
	if f == nil || st.srcW <= 0 || st.srcH <= 0 || st.imageRect.W <= 0 {
		return
	}
	if st.mode != "crop" {
		return
	}
	pt, ok := a.editSrcPoint(ev.X, ev.Y)
	if !ok {
		return
	}
	box, hasBox := a.editCropBoxFor(f.Path)
	if st.liveBox != nil {
		box, hasBox = *st.liveBox, true
	}
	corner := ""
	if hasBox {
		sx := st.imageLocal.W / float32(st.srcW)
		sy := st.imageLocal.H / float32(st.srcH)
		rect := ui.Rect{
			X: st.imageLocal.X + float32(box.X)*sx,
			Y: st.imageLocal.Y + float32(box.Y)*sy,
			W: float32(box.W) * sx,
			H: float32(box.H) * sy,
		}
		for _, handle := range editCropHandles(rect) {
			if math.Abs(float64(ev.X-handle.X)) <= 9 && math.Abs(float64(ev.Y-handle.Y)) <= 9 {
				corner = handle.Name
				break
			}
		}
	}
	switch {
	case corner != "" && hasBox:
		st.cropDrag = &editCropDrag{Mode: "resize", Corner: corner, StartX: pt.X, StartY: pt.Y, Box: box, Latest: box}
		st.dragMode = "crop"
	case hasBox && pt.X >= box.X && pt.X <= box.X+box.W && pt.Y >= box.Y && pt.Y <= box.Y+box.H:
		st.cropDrag = &editCropDrag{Mode: "move", StartX: pt.X, StartY: pt.Y, Box: box, Latest: box}
		st.dragMode = "crop"
	default:
		newBox := editCropBox{X: pt.X, Y: pt.Y}
		st.cropDrag = &editCropDrag{Mode: "draw", StartX: pt.X, StartY: pt.Y, Box: newBox, Latest: newBox}
		st.liveBox = &newBox
		st.dragMode = "crop"
	}
}

func (a *App) editCropMove(ev ui.InputEvent) {
	st := &a.edit
	d := st.cropDrag
	if d == nil || st.srcW <= 0 || st.srcH <= 0 {
		return
	}
	pt, ok := a.editSrcPoint(ev.X, ev.Y)
	if !ok {
		return
	}
	aspect := a.editCropAspect()
	clamp := func(v, max float64) float64 { return math.Min(math.Max(v, 0), max) }
	if d.Mode == "draw" {
		w := math.Min(math.Abs(pt.X-d.StartX), float64(st.srcW))
		h := math.Min(math.Abs(pt.Y-d.StartY), float64(st.srcH))
		if aspect > 0 {
			w = math.Min(w, float64(st.srcH)*aspect)
			h = w / aspect
		}
		x := d.StartX
		if pt.X < d.StartX {
			x = d.StartX - w
		}
		y := d.StartY
		if pt.Y < d.StartY {
			y = d.StartY - h
		}
		d.Latest = editCropBox{X: clamp(x, float64(st.srcW)-w), Y: clamp(y, float64(st.srcH)-h), W: w, H: h}
		st.liveBox = &d.Latest
		return
	}
	dx := pt.X - d.StartX
	dy := pt.Y - d.StartY
	b := d.Box
	if d.Mode == "move" {
		d.Latest = editCropBox{
			X: clamp(b.X+dx, float64(st.srcW)-b.W),
			Y: clamp(b.Y+dy, float64(st.srcH)-b.H),
			W: b.W, H: b.H,
		}
		st.liveBox = &d.Latest
		return
	}
	x, y, w, h := b.X, b.Y, b.W, b.H
	if hasCorner(d.Corner, "w") {
		x = clamp(b.X+dx, b.X+b.W-8)
		w = b.X + b.W - x
	}
	if hasCorner(d.Corner, "e") {
		w = clamp(b.W+dx, float64(st.srcW)-b.X)
	}
	if hasCorner(d.Corner, "n") {
		y = clamp(b.Y+dy, b.Y+b.H-8)
		h = b.Y + b.H - y
	}
	if hasCorner(d.Corner, "s") {
		h = clamp(b.H+dy, float64(st.srcH)-b.Y)
	}
	if aspect > 0 {
		if d.Corner == "n" || d.Corner == "s" {
			w = h * aspect
		}
		w = math.Min(math.Max(8, w), math.Min(float64(st.srcW), float64(st.srcH)*aspect))
		h = w / aspect
		if h > float64(st.srcH) {
			h = float64(st.srcH)
			w = h * aspect
		}
		switch {
		case hasCorner(d.Corner, "w"):
			x = b.X + b.W - w
		case d.Corner == "n" || d.Corner == "s":
			x = b.X + (b.W-w)/2
		default:
			x = b.X
		}
		if hasCorner(d.Corner, "n") {
			y = b.Y + b.H - h
		} else if hasCorner(d.Corner, "e") || hasCorner(d.Corner, "w") {
			y = b.Y + (b.H-h)/2
		} else {
			y = b.Y
		}
	}
	x = clamp(x, math.Max(0, float64(st.srcW)-w))
	y = clamp(y, math.Max(0, float64(st.srcH)-h))
	d.Latest = editCropBox{X: x, Y: y, W: math.Max(8, w), H: math.Max(8, h)}
	st.liveBox = &d.Latest
}

func (a *App) editCropUp() {
	st := &a.edit
	d := st.cropDrag
	st.cropDrag = nil
	defer func() { st.liveBox = nil }()
	if d == nil || d.Latest.W < 8 || d.Latest.H < 8 {
		return
	}
	box := d.Latest
	a.editSetCropBox(&box)
}

func hasCorner(corner, axis string) bool {
	for i := 0; i+len(axis) <= len(corner); i++ {
		if corner[i:i+len(axis)] == axis {
			return true
		}
	}
	return false
}

// editSrcPoint 把元素的指针坐标换成源图像素坐标。
func (a *App) editSrcPoint(x, y float32) (editCropBox, bool) {
	st := &a.edit
	img := st.imageLocal
	if img.W <= 0 || img.H <= 0 || st.srcW <= 0 || st.srcH <= 0 {
		return editCropBox{}, false
	}
	return editCropBox{
		X: float64((x - img.X) / img.W * float32(st.srcW)),
		Y: float64((y - img.Y) / img.H * float32(st.srcH)),
	}, true
}

// editPreviewHUD 是右下角的缩放组和对比开关。
func (a *App) editPreviewHUD(c *ui.Context, be editPalette, showCrop bool) {
	st := &a.edit
	contentZoom := float64(c.Zoom())
	hud := ui.Row(c).Absolute().Right(12).Bottom(12).Gap(6).AlignItems(ui.Center)
	hud.Children(func() {
		if st.previewPending {
			ui.Spinner(c).Size(16, 16)
		}
		group := ui.Row(c).Height(28).Padding(2).Gap(2).Radius(be.tok.radiusM).
			Border(1, be.tok.borderStrong).Background(be.tok.lift.Alpha(0.95)).
			Shadow(0, 4, 14, 0, be.shadow).AlignItems(ui.Center)
		group.Children(func() {
			a.editZoomButton(c, be, "−", "缩小", 24, func() {
				a.editZoomBy(1/1.2, 0, 0)
				a.editClampPan()
			})
			pct := ui.Box(c).Height(22).MinWidth(44).Center().Radius(be.tok.radiusS).Cursor(ui.CursorPointer).
				Tooltip("重置为适合窗口")
			pct.Draw(func(p *ui.Painter, r ui.Rect) {
				if pct.Hovered() {
					p.Fill(r, be.quiet, be.tok.radiusS)
				}
			})
			pct.OnClick(func() { st.zoom, st.panX, st.panY = 1, 0, 0 })
			pct.Children(func() {
				ui.Text(c, fmt.Sprintf("%.0f%%", st.zoom*100)).FontSize(11).FontWeight(500).
					TextColor(be.ink).FontFeatures("tnum").SingleLine()
			})
			a.editZoomButton(c, be, "+", "放大", 24, func() {
				a.editZoomBy(1.2, 0, 0)
				a.editClampPan()
			})
			ui.Box(c).Size(1, 14).Background(be.rule).Margin(0, 2)
			a.editZoomButton(c, be, "1:1", "实际大小", 34, func() {
				if st.baseW > 0 && st.srcW > 0 {
					z := math.Min(4, math.Max(0.1, float64(st.srcW)/(float64(st.baseW)*contentZoom)))
					st.zoom = z
					st.panX, st.panY = 0, 0
					a.editClampPan()
				}
			})
		})
		if !showCrop && st.orig != nil && st.out != nil {
			compare := ui.Box(c).Height(28).Padding(0, 12).Center().Radius(be.tok.radiusM).
				Border(1, be.tok.borderStrong).Background(be.tok.lift.Alpha(0.95)).
				Shadow(0, 4, 14, 0, be.shadow).
				Cursor(ui.CursorPointer).Tooltip("分割对比：左原图右效果，拖动分割线")
			if st.compare {
				compare.Background(be.transferSoft).BorderColor(be.transferBorder)
			}
			compare.Draw(func(p *ui.Painter, r ui.Rect) {
				if !st.compare && compare.Hovered() {
					p.Fill(r, be.quiet, be.tok.radiusM)
				}
			})
			compare.OnClick(func() { st.compare = !st.compare })
			compare.Children(func() {
				ink := be.ink
				if st.compare {
					ink = be.transferStrong
				}
				ui.Text(c, "对比").FontSize(11.5).FontWeight(500).TextColor(ink)
			})
		}
		a.editPreviewProgress(c, be)
	})
}

func (a *App) editZoomButton(c *ui.Context, be editPalette, label, tip string, width float32, fn func()) {
	btn := ui.Box(c).Height(22).MinWidth(width).Padding(0, 6).Center().Radius(be.tok.radiusS).
		Cursor(ui.CursorPointer).Tooltip(tip)
	btn.Draw(func(p *ui.Painter, r ui.Rect) {
		if btn.Hovered() {
			p.Fill(r, be.quiet, be.tok.radiusS)
		}
	})
	btn.OnClick(fn)
	btn.Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(be.muted).FontFeatures("tnum").SingleLine()
	})
}

func (a *App) editPreviewProgress(c *ui.Context, be editPalette) {
	st := &a.edit
	if !st.running || st.progressTotal <= 0 {
		return
	}
	bar := ui.Row(c).Height(28).MaxWidth(320).Radius(be.tok.radiusM).Clip().
		Border(1, be.tok.borderStrong).Background(be.tok.lift.Alpha(0.95)).
		Shadow(0, 4, 14, 0, be.shadow).AlignItems(ui.Center)
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		frac := float32(st.progressDone) / float32(st.progressTotal)
		if frac > 0 {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * frac, H: r.H}, be.transferSoft, 0)
		}
	})
	bar.Children(func() {
		label := fmt.Sprintf("%d/%d %s", st.progressDone, st.progressTotal, st.progressCurrent)
		ui.Text(c, label).FontSize(11).TextColor(be.ink).FontFeatures("tnum").SingleLine().Padding(0, 10)
	})
}

// ---- 胶片条 ----

func (a *App) editFilmstrip(c *ui.Context, be editPalette) {
	st := &a.edit
	strip := ui.Column(c.Key("edit-filmstrip")).FillWidth().Shrink(0).Gap(5).
		Padding(7, 12, 7, 12).Background(be.sheet).BorderWidth(1, 0, 0, 0).BorderColor(be.rule)
	strip.Children(func() {
		top := ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center)
		top.Children(func() {
			actions := ui.Row(c).Shrink(0).Gap(4).AlignItems(ui.Center)
			actions.Children(func() {
				uiIconBtn(c, "edit-add-img", "打开图片", "打开图片", iconImagePlus, 22, false, false, be.tok).
					OnClick(func() { a.editPickFiles() })
				uiIconBtn(c, "edit-add-dir", "打开文件夹", "打开文件夹", iconFolderOpen, 22, false, false, be.tok).
					OnClick(func() { a.editPickFolder() })
			})
			if picked := len(st.picks); picked > 0 {
				// 勾过图就把提示换成勾选状态，旁边给一键清空。
				middle := ui.Row(c).Grow(1).MinWidth(0).Gap(6).Justify(ui.Center).
					AlignItems(ui.Center)
				middle.Children(func() {
					uiBadge(c, "edit-picked-count", fmt.Sprintf("已选 %d 张，保存只处理这些", picked), "accent", be.tok)
					uiIconBtn(c, "edit-clear-picks", "清空选择", "清空选择", iconX, 18, false, true, be.tok).
						OnClick(func() { a.editClearPicks() })
				})
			} else {
				ui.Text(c, fmt.Sprintf("共 %d 张 · Cmd/Shift 多选，单击切换预览，保存将批量导出", len(st.files))).
					FontSize(be.tok.caption).TextColor(be.muted).FontFeatures("tnum").
					Grow(1).TextAlign(ui.Center).SingleLine()
			}
			uiIconBtn(c, "edit-clear-files", fmt.Sprintf("清空%d张", len(st.files)), fmt.Sprintf("清空全部 %d 张图片", len(st.files)), iconTrash, 22, false, false, be.tok).
				Disabled(st.running || len(st.files) == 0).OnClick(func() { a.editClearFiles() })
		})

		row := ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center)
		row.Children(func() {
			a.editStripArrow(c, be, iconChevronLeft, -220)
			scroll := ui.ScrollHorizontal(c.Key("edit-strip")).Grow(1).MinWidth(0).Gap(editThumbGap).
				TrackScroll(&st.stripScroll)
			scroll.Draw(func(p *ui.Painter, r ui.Rect) {
				st.stripRect = r
				st.stripViewW = r.W
			})
			scroll.Children(func() {
				for i, f := range st.files {
					a.editThumb(c, be, i, f)
				}
			})
			a.editStripArrow(c, be, iconChevronRight, 220)
		})

		if st.stripScroll.MaxX > 0 && st.stripViewW > 0 {
			track := ui.Box(c).Size(64, 3).Radius(2).Background(be.rule)
			track.Draw(func(p *ui.Painter, r ui.Rect) {
				total := st.stripViewW + st.stripScroll.MaxX
				frac := st.stripViewW / total
				width := float32(math.Max(0.18, float64(frac))) * r.W
				left := st.stripScroll.X / st.stripScroll.MaxX * (r.W - width)
				p.Fill(ui.Rect{X: r.X + left, Y: r.Y, W: width, H: r.H}, be.muted, 2)
			})
		}
	})
}

func (a *App) editStripArrow(c *ui.Context, be editPalette, icon *ui.SVG, delta float32) {
	btn := ui.Box(c).Size(18, 34).Center().Radius(be.tok.radiusS).Shrink(0).Cursor(ui.CursorPointer)
	btn.Draw(func(p *ui.Painter, r ui.Rect) {
		if btn.Hovered() {
			p.Fill(r, be.quiet, be.tok.radiusS)
		}
	})
	btn.OnClick(func() {
		st := &a.edit
		target := st.stripScroll.X + delta
		target = float32(math.Max(0, math.Min(float64(st.stripScroll.MaxX), float64(target))))
		st.stripScroll.ScrollTo(target, 0, ui.Smooth)
	})
	btn.Children(func() {
		ink := be.muted
		if btn.Hovered() {
			ink = be.ink
		}
		ui.Icon(c, icon).FontSize(13).TextColor(ink)
	})
}

func (a *App) editThumb(c *ui.Context, be editPalette, index int, f *editFile) {
	st := &a.edit
	on := st.selected == index
	tip := f.Name
	if f.Status == "failed" && f.Err != "" {
		// 失败原因挂在悬停提示里，不用打开结果弹窗也能看到。
		tip = f.Name + " · 失败：" + f.Err
	}
	thumb := ui.Box(c.Key("edit-thumb-"+f.Path)).Size(editThumbSize, editThumbSize).Radius(6).
		Clip().Background(be.quiet).BorderWidth(2).Cursor(ui.CursorPointer).
		Tooltip(tip)
	if on {
		thumb.BorderColor(be.transfer)
	} else {
		thumb.BorderColor(be.rule)
	}
	thumb.OnClick(func() {
		// Cmd 切换勾选、Shift 连选，普通点击只换预览。
		a.editThumbClick(index, thumb.ClickModifiers())
	})
	thumb.Children(func() {
		if f.Thumb != nil {
			img := ui.Image(c, f.Thumb).Fill().Fit(ui.Cover).Radius(4)
			img.Disabled(true)
		} else {
			holder := ui.Box(c).Fill().Center()
			holder.Children(func() { ui.Icon(c, iconFileImage).FontSize(20).TextColor(be.muted) })
		}
		if f.Status == "ok" || f.Status == "failed" {
			dot := ui.Box(c).Absolute().Top(3).Right(3).Size(7, 7).Radius(4)
			if f.Status == "ok" {
				dot.Background(be.success)
			} else {
				dot.Background(be.fault)
			}
		}
		if st.picks[f.Path] {
			// 勾选角标放在右下角，避开左上角的删除按钮和右上角的状态点。
			badge := ui.Box(c).Absolute().Right(3).Bottom(3).Size(14, 14).Center().Radius(7).
				Background(be.transfer).BorderWidth(1).BorderColor(be.onTransfer)
			badge.Children(func() {
				ui.Icon(c, iconCheck).FontSize(8).TextColor(be.onTransfer)
			})
		}
		if thumb.Hovered() && !st.running {
			x := ui.Box(c).Absolute().Top(2).Left(2).Size(14, 14).Center().Radius(7).
				Background(ui.RGBA(15, 23, 42, 0.72)).Cursor(ui.CursorPointer)
			x.OnClick(func() { a.editRemoveFile(index) })
			x.Children(func() {
				ui.Icon(c, iconX).FontSize(8).TextColor(ui.RGBA(248, 250, 252, 1))
			})
		}
	})
}

// editScrollSelected 把选中的缩略图滚进视野。
func (a *App) editScrollSelected() {
	st := &a.edit
	if st.selected < 0 || st.stripViewW <= 0 {
		return
	}
	stride := editThumbSize + editThumbGap
	itemX := float32(st.selected) * stride
	target := st.stripScroll.X
	if itemX < target {
		target = itemX - editThumbGap
	} else if itemX+editThumbSize > target+st.stripViewW {
		target = itemX + editThumbSize - st.stripViewW + editThumbGap
	}
	target = float32(math.Max(0, math.Min(float64(st.stripScroll.MaxX), float64(target))))
	st.stripScroll.ScrollTo(target, 0, ui.Snappy)
}

var _ = filepath.Base
