package ui

import (
	"image"
	"math"
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/tsx"
	nsgoui "github.com/nsgo-dev/nsgo/ui"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/resources"
)

func init() {
	tsx.Register("tulai/native", map[string]core.Node{
		"BrandLogo":      BrandLogo{},
		"BrowserStage":   BrowserStage{},
		"ImageEditStage": ImageEditStage{},
		"OutlineBadge":   OutlineBadge{},
	})
}

// OutlineBadge is nsgo/ui's Badge in its "outline" look, which 0.3.0 removed:
// no fill, a rim halfway between the background and the muted text, and the
// text's color.
type OutlineBadge struct {
	Text string
}

func (o OutlineBadge) Render(cx *core.Scope) core.Node {
	colors := nsgoui.ColorsOf(cx)
	return core.View{
		Style: core.Style{
			Padding:        core.Symmetric(2, 8),
			AlignSelf:      core.AlignStart,
			CornerRadius:   12,
			JustifyContent: core.JustifyCenter,
			BorderWidth:    1,
		},
		StyleFunc: func() core.Style {
			return core.Style{BorderColor: nsgoui.Mix(colors.Background(), colors.Muted(), 0.5)}
		},
		Children: []core.Node{
			nsgoui.Text(o.Text, 11, nsgoui.Medium, func() core.Color { return colors.Text() }),
		},
	}
}

// BrandLogo renders the application icon mark via resources.AppIcon.
type BrandLogo struct {
	Class  string
	Style  core.Style
	Size   core.Points
	TestID string
}

func (b BrandLogo) Render(*core.Scope) core.Node {
	sz := b.Size
	if sz <= 0 {
		sz = 28
	}
	return core.Image{
		Class:      b.Class,
		Style:      b.Style,
		Source:     core.ImageSource{Data: resources.AppIcon},
		ResizeMode: core.Contain,
		TestID:     b.TestID,
		Label:      "tulai",
		StyleFunc: func() core.Style {
			s := b.Style
			s.Width = core.Pt(sz)
			s.Height = core.Pt(sz)
			if s.CornerRadius == 0 {
				s.CornerRadius = sz * 0.22
			}
			return s
		},
	}
}

// BrowserStage displays the interactive live CDP frame stream for the login/verification drawer.
type BrowserStage struct {
	Class  string
	Style  core.Style
	TestID string
}

func (b BrowserStage) Render(cx *core.Scope) core.Node {
	stageRef := new(core.Ref)
	stageW := core.NewSignal[float32](cx, 0)
	stageH := core.NewSignal[float32](cx, 0)

	fitRect := func() (x, y, w, h, vw, vh float64) {
		app := getApp()
		if app == nil || app.browserDrawer == nil {
			return 0, 0, 0, 0, 0, 0
		}
		s := app.browserDrawer
		sw, sh := float64(stageW.Get()), float64(stageH.Get())
		vw, vh = s.width, s.height
		if sw <= 0 || sh <= 0 || vw <= 0 || vh <= 0 {
			return 0, 0, 0, 0, vw, vh
		}
		scale := math.Min(sw/vw, sh/vh)
		rw, rh := vw*scale, vh*scale
		return (sw - rw) / 2, (sh - rh) / 2, rw, rh, vw, vh
	}

	toPage := func(pt core.Point) (float64, float64, bool) {
		ix, iy, iw, ih, vw, vh := fitRect()
		if iw <= 0 || ih <= 0 || vw <= 0 || vh <= 0 {
			return 0, 0, false
		}
		px := (float64(pt.X) - ix) * vw / iw
		py := (float64(pt.Y) - iy) * vh / ih
		if px < 0 || py < 0 || px > vw || py > vh {
			return 0, 0, false
		}
		return px, py, true
	}

	return core.View{
		Class:     b.Class,
		Style:     b.Style,
		Ref:       stageRef,
		TestID:    b.TestID,
		Label:     "内置浏览器画面",
		Focusable: func() bool { return true },
		OnLayout: func(bounds core.Bounds) {
			stageW.Set(float32(bounds.Size.Width))
			stageH.Set(float32(bounds.Size.Height))
		},
		OnPressDown: func(ev core.PressEvent) {
			stageRef.Focus()
			app := getApp()
			if app == nil || app.browserDrawer == nil {
				return
			}
			if px, py, ok := toPage(ev.Position); ok {
				app.browserDrawerSend(app.browserDrawer, cdp.PageInput{
					Kind:      "mousePressed",
					Button:    "left",
					X:         px,
					Y:         py,
					Clicks:    1,
					Modifiers: cdpModifiers(ev.Modifiers),
				})
			}
		},
		OnPressUp: func(ev core.PressEvent) {
			app := getApp()
			if app == nil || app.browserDrawer == nil {
				return
			}
			if px, py, ok := toPage(ev.Position); ok {
				app.browserDrawerSend(app.browserDrawer, cdp.PageInput{
					Kind:      "mouseReleased",
					Button:    "left",
					X:         px,
					Y:         py,
					Clicks:    1,
					Modifiers: cdpModifiers(ev.Modifiers),
				})
			}
		},
		OnMouseMove: func(ev core.MouseEvent) {
			app := getApp()
			if app == nil || app.browserDrawer == nil {
				return
			}
			if px, py, ok := toPage(ev.Position); ok {
				app.browserDrawerSend(app.browserDrawer, cdp.PageInput{
					Kind:      "mouseMoved",
					X:         px,
					Y:         py,
					Modifiers: cdpModifiers(ev.Modifiers),
				})
			}
		},
		OnWheel: func(ev core.WheelEvent) {
			app := getApp()
			if app == nil || app.browserDrawer == nil {
				return
			}
			if px, py, ok := toPage(ev.Position); ok {
				app.browserDrawerSend(app.browserDrawer, cdp.PageInput{
					Kind:      "mouseWheel",
					X:         px,
					Y:         py,
					DX:        float64(ev.Delta.X) * -40,
					DY:        float64(ev.Delta.Y) * -40,
					Modifiers: cdpModifiers(ev.Modifiers),
				})
			}
		},
		OnKeyDown: func(ev core.KeyEvent) bool {
			app := getApp()
			if app == nil || app.browserDrawer == nil {
				return false
			}
			return app.handleBrowserDrawerKey(ev)
		},
		Children: []core.Node{
			core.Image{
				Class:      "w-full h-full",
				ResizeMode: core.Contain,
				SourceFunc: func() core.ImageSource {
					app := getApp()
					if app == nil {
						return core.ImageSource{}
					}
					if app.nativeTick != nil {
						_ = app.nativeTick.Get()
					}
					s := app.browserDrawer
					if s == nil || s.rgba == nil {
						return core.ImageSource{}
					}
					return core.ImageSource{
						Pixels:  s.rgba,
						Version: s.frameVersion,
					}
				},
			},
		},
	}
}

func cdpModifiers(m core.Modifiers) int64 {
	var flags int64
	if m.Alt {
		flags |= 1
	}
	if m.Control {
		flags |= 2
	}
	if m.Platform {
		flags |= 4
	}
	if m.Shift {
		flags |= 8
	}
	return flags
}

// ImageEditStage draws the image editor preview, compare split, pan/zoom, and interactive crop handles.
type ImageEditStage struct {
	Class  string
	Style  core.Style
	TestID string
}

func (e ImageEditStage) Render(*core.Scope) core.Node {
	var dragActive bool
	var dragStart core.Point
	var panOriginX, panOriginY float64
	var splitStart float64
	var splitDragging bool
	var cropDragging bool

	viewMetrics := func(size core.Size, st *imageEditState) (dx, dy, dw, dh float32) {
		sw, sh := float32(size.Width), float32(size.Height)
		img := st.outPixels
		if img == nil {
			img = st.origPixels
		}
		if img == nil || sw <= 0 || sh <= 0 {
			return 0, 0, 0, 0
		}
		iw, ih := float32(img.Bounds().Dx()), float32(img.Bounds().Dy())
		if iw <= 0 || ih <= 0 {
			return 0, 0, 0, 0
		}
		pad := float32(16)
		availW := max(1, sw-pad*2)
		availH := max(1, sh-pad*2)
		fitScale := math.Min(float64(availW/iw), float64(availH/ih))
		if fitScale <= 0 {
			fitScale = 1
		}
		baseW := float32(float64(iw) * fitScale)
		baseH := float32(float64(ih) * fitScale)
		st.baseW, st.baseH = baseW, baseH
		st.previewW, st.previewH = sw, sh
		z := float32(math.Max(0.1, st.zoom))
		dw = baseW * z
		dh = baseH * z
		dx = (sw-dw)/2 + float32(st.panX)
		dy = (sh-dh)/2 + float32(st.panY)
		return dx, dy, dw, dh
	}

	stageCropScreenBox := func(st *imageEditState, dx, dy, dw, dh float32) (x, y, w, h float32, ok bool) {
		if st.srcW <= 0 || st.srcH <= 0 {
			return 0, 0, 0, 0, false
		}
		var box editCropBox
		if st.liveBox != nil {
			box = *st.liveBox
		} else {
			app := getApp()
			if app == nil {
				return 0, 0, 0, 0, false
			}
			f := st.selectedFile()
			if f == nil {
				return 0, 0, 0, 0, false
			}
			var has bool
			box, has = app.editCropBoxFor(f.Path)
			if !has {
				return 0, 0, 0, 0, false
			}
		}
		sx := dw / float32(st.srcW)
		sy := dh / float32(st.srcH)
		return dx + float32(box.X)*sx, dy + float32(box.Y)*sy, float32(box.W) * sx, float32(box.H) * sy, true
	}

	return core.View{
		Class:     e.Class,
		Style:     e.Style,
		TestID:    e.TestID,
		Label:     "图片编辑预览画布",
		Focusable: func() bool { return true },
		OnPressDown: func(ev core.PressEvent) {
			app := getApp()
			if app == nil {
				return
			}
			st := &app.edit
			dx, dy, dw, dh := viewMetrics(core.Size{Width: core.Points(st.previewW), Height: core.Points(st.previewH)}, st)
			if st.compare && dw > 0 {
				sx := dx + dw*float32(st.split/100)
				if math.Abs(float64(float32(ev.Position.X)-sx)) <= 10 {
					dragActive = true
					splitDragging = true
					cropDragging = false
					dragStart = ev.Position
					splitStart = st.split
					return
				}
			}
			if app.editShowCropUI() && dw > 0 && st.srcW > 0 && st.srcH > 0 {
				px := (float64(ev.Position.X) - float64(dx)) * float64(st.srcW) / float64(dw)
				py := (float64(ev.Position.Y) - float64(dy)) * float64(st.srcH) / float64(dh)
				if px >= 0 && py >= 0 && px <= float64(st.srcW) && py <= float64(st.srcH) {
					cropDragging = true
					dragActive = true
					splitDragging = false
					dragStart = ev.Position
					box := editCropBox{X: px, Y: py, W: 0, H: 0}
					st.cropDrag = &editCropDrag{
						Mode:   "draw",
						StartX: px,
						StartY: py,
						Box:    box,
						Latest: box,
					}
					st.liveBox = &box
					app.emit("imageedit")
					return
				}
			}
			dragActive = true
			splitDragging = false
			cropDragging = false
			dragStart = ev.Position
			panOriginX, panOriginY = st.panX, st.panY
		},
		OnMouseMove: func(ev core.MouseEvent) {
			if !dragActive {
				return
			}
			app := getApp()
			if app == nil {
				return
			}
			st := &app.edit
			if splitDragging && st.baseW > 0 {
				dw := float64(st.baseW) * st.zoom
				if dw > 0 {
					delta := (float64(ev.Position.X - dragStart.X)) / dw * 100
					st.split = math.Max(5, math.Min(95, splitStart+delta))
					app.emit("imageedit")
				}
				return
			}
			if cropDragging && st.cropDrag != nil && st.srcW > 0 && st.srcH > 0 {
				dx, dy, dw, dh := viewMetrics(core.Size{Width: core.Points(st.previewW), Height: core.Points(st.previewH)}, st)
				if dw <= 0 || dh <= 0 {
					return
				}
				px := math.Max(0, math.Min(float64(st.srcW), (float64(ev.Position.X)-float64(dx))*float64(st.srcW)/float64(dw)))
				py := math.Max(0, math.Min(float64(st.srcH), (float64(ev.Position.Y)-float64(dy))*float64(st.srcH)/float64(dh)))
				x0 := math.Min(st.cropDrag.StartX, px)
				y0 := math.Min(st.cropDrag.StartY, py)
				bw := math.Abs(px - st.cropDrag.StartX)
				bh := math.Abs(py - st.cropDrag.StartY)
				if aspect := app.editCropAspect(); aspect > 0 && bw > 0 {
					bh = bw / aspect
					if y0+bh > float64(st.srcH) {
						bh = float64(st.srcH) - y0
						bw = bh * aspect
					}
				}
				b := editCropBox{X: x0, Y: y0, W: bw, H: bh}
				st.cropDrag.Latest = b
				st.liveBox = &b
				app.emit("imageedit")
				return
			}
			st.panX = panOriginX + float64(ev.Position.X-dragStart.X)
			st.panY = panOriginY + float64(ev.Position.Y-dragStart.Y)
			app.editClampPan()
			app.emit("imageedit")
		},
		OnPressUp: func(core.PressEvent) {
			if !dragActive {
				return
			}
			dragActive = false
			app := getApp()
			if app == nil {
				return
			}
			st := &app.edit
			if cropDragging && st.cropDrag != nil {
				cropDragging = false
				b := st.cropDrag.Latest
				st.cropDrag = nil
				if b.W >= 8 && b.H >= 8 {
					app.editSetCropBox(&b)
					app.editEnsurePreview()
				}
				st.liveBox = nil
				app.emit("imageedit")
			}
			splitDragging = false
		},
		OnWheel: func(ev core.WheelEvent) {
			app := getApp()
			if app == nil {
				return
			}
			st := &app.edit
			factor := 1.0
			if ev.Delta.Y > 0 {
				factor = 0.9
			} else if ev.Delta.Y < 0 {
				factor = 1.1
			}
			if factor == 1.0 {
				return
			}
			cx := float64(ev.Position.X) - float64(st.previewW)/2
			cy := float64(ev.Position.Y) - float64(st.previewH)/2
			app.editZoomBy(factor, cx, cy)
			app.editClampPan()
			app.emit("imageedit")
		},
		Children: []core.Node{
			core.Canvas{
				Class: "w-full h-full",
				Draw: func(d *core.Drawing, size core.Size) {
					app := getApp()
					if app == nil {
						return
					}
					if app.nativeTick != nil {
						_ = app.nativeTick.Get()
					}
					st := &app.edit
					sw, sh := float32(size.Width), float32(size.Height)
					// Checkerboard-like backdrop
					d.SetFillColor(core.RGB(0x0F172A))
					d.FillRect(0, 0, sw, sh)

					dx, dy, dw, dh := viewMetrics(size, st)
					if dw <= 0 || dh <= 0 {
						return
					}

					var mainPx *image.RGBA
					var mainVer uint64
					if st.outPixels != nil {
						mainPx, mainVer = st.outPixels, st.outVersion
					} else if st.origPixels != nil {
						mainPx, mainVer = st.origPixels, st.origVersion
					}
					if mainPx != nil {
						d.DrawImageIn(core.ImageSource{Pixels: mainPx, Version: mainVer}, dx, dy, dw, dh)
					}

					if st.compare && st.origPixels != nil {
						splitX := dx + dw*float32(st.split/100)
						d.Save()
						d.BeginPath()
						d.Rect(dx, dy, max(0, splitX-dx), dh)
						d.Clip()
						d.DrawImageIn(core.ImageSource{Pixels: st.origPixels, Version: st.origVersion}, dx, dy, dw, dh)
						d.Restore()

						d.SetStrokeColor(core.RGB(0x3B82F6))
						d.SetLineWidth(2)
						d.BeginPath()
						d.MoveTo(splitX, dy)
						d.LineTo(splitX, dy+dh)
						d.Stroke()
					}

					if app.editShowCropUI() {
						if cx, cy, cw, ch, ok := stageCropScreenBox(st, dx, dy, dw, dh); ok && cw > 0 && ch > 0 {
							d.SetStrokeColor(core.RGB(0x60A5FA))
							d.SetLineWidth(1.5)
							d.StrokeRect(cx, cy, cw, ch)
						}
					}
				},
			},
		},
	}
}

func (a *App) browserDrawerSend(s *browserDrawerState, in cdp.PageInput) bool {
	if s == nil || s.inputs == nil {
		return false
	}
	select {
	case s.inputs <- in:
		return true
	default:
		return false
	}
}

func (a *App) handleBrowserDrawerKey(ev core.KeyEvent) bool {
	s := a.browserDrawer
	if s == nil {
		return false
	}
	keyName := strings.TrimSpace(ev.Key)
	if strings.EqualFold(keyName, "escape") || strings.EqualFold(keyName, "esc") {
		a.closeBrowserDrawer()
		return true
	}
	if !s.held.Load() || keyName == "" {
		return false
	}
	mods := cdpModifiers(ev.Modifiers)
	cdpKey, code := mapKeyToCDP(keyName)
	if len([]rune(keyName)) == 1 && mods == 0 {
		a.browserDrawerSend(s, cdp.PageInput{Kind: "text", Text: keyName})
		return true
	}
	if ev.Modifiers.Control && strings.EqualFold(keyName, "c") {
		a.browserDrawerSend(s, cdp.PageInput{Kind: "copy"})
		return true
	}
	if ev.Modifiers.Control && strings.EqualFold(keyName, "x") {
		a.browserDrawerSend(s, cdp.PageInput{Kind: "cut"})
		return true
	}
	if cdpKey != "" {
		a.browserDrawerSend(s, cdp.PageInput{Kind: "keyDown", Key: cdpKey, VirtualKey: code, Modifiers: mods})
		a.browserDrawerSend(s, cdp.PageInput{Kind: "keyUp", Key: cdpKey, VirtualKey: code, Modifiers: mods})
		return true
	}
	return false
}

func mapKeyToCDP(k string) (string, int) {
	switch strings.ToLower(k) {
	case "enter", "return":
		return "Enter", 13
	case "backspace":
		return "Backspace", 8
	case "tab":
		return "Tab", 9
	case "space", " ":
		return " ", 32
	case "left":
		return "ArrowLeft", 37
	case "up":
		return "ArrowUp", 38
	case "right":
		return "ArrowRight", 39
	case "down":
		return "ArrowDown", 40
	case "delete":
		return "Delete", 46
	case "home":
		return "Home", 36
	case "end":
		return "End", 35
	case "pageup":
		return "PageUp", 33
	case "pagedown":
		return "PageDown", 34
	default:
		return k, 0
	}
}
