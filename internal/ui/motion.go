package ui

import (
	"time"

	"github.com/egoist/mygo/ui"

	"xbs/internal/wizard"
)

// 桌面界面的动效令牌。MyGo 把界面画在 GPU 上（macOS Metal、Windows
// Direct3D、Linux OpenGL，浏览器里是同一套着色器的 WebGL），不是一张
// CSS 页，所以时长和缓动放在这里，作用和样式表里的自定义属性一样。
//
// 短交互 150ms，步骤和列表 220ms，都在 150–250ms 里。进入用 ease-out。
// Transition 一次只有一条曲线，离开和进入共用它；离开的位移更短。
// 系统要求减少动态效果时（macOS 的 Reduce Motion、Windows 的动画效果、
// GNOME 的动画，以及浏览器的 prefers-reduced-motion），位移和缩放取消，
// 只保留淡入淡出。
//
// 列表进出不折叠高度，避免几十张图一起重排。悬停和按压只用 AnimatePose，
// 画的是 transform 和 opacity，不重跑布局。
const (
	motionFast  = 150 * time.Millisecond
	motionUI    = 220 * time.Millisecond
	motionSlide = 12
)

var (
	motionEaseOut  = ui.Animation{Duration: motionUI, Ease: ui.EaseOut}
	motionEaseFast = ui.Animation{Duration: motionFast, Ease: ui.EaseOut}
)

// motionTravel 是带方向的淡入加一小段位移。不做出场副本：整页叠在一起会糊成两层字。
// 位移只有十几个 DIP，动画停在第一帧时，控件仍在窗口里，点得到。
func motionTravel(x, y float32) ui.ElementTransition {
	return ui.ElementTransition{
		Animation: &motionEaseOut,
		Position:  true,
		Enter:     &ui.Motion{X: x, Y: y, Scale: 0.98},
	}
}

func screenMotion() ui.ElementTransition {
	t := motionTravel(0, motionSlide)
	t.Exit = &ui.Motion{Y: -6, Scale: 0.98}
	return t
}

func motionBanner() ui.ElementTransition {
	return ui.ElementTransition{
		Animation: &motionEaseOut,
		Position:  true,
		Enter:     &ui.Motion{Y: -motionSlide, Scale: 0.98},
		Exit:      &ui.Motion{Y: -8, Scale: 0.98},
	}
}

func motionPopup() ui.ElementTransition {
	return ui.ElementTransition{
		Animation: &motionEaseFast,
		Position:  true,
		Enter:     &ui.Motion{Y: -6, Scale: 0.98},
		Exit:      &ui.Motion{Y: -4, Scale: 0.98},
	}
}

// motionItem 是缩略图和进度卡的进出，加上位置 FLIP。不改尺寸，避免格子重排。
func motionItem() ui.ElementTransition {
	return ui.ElementTransition{
		Animation: &motionEaseOut,
		Position:  true,
		Enter:     &ui.Motion{Y: 8, Scale: 0.96},
		Exit:      &ui.Motion{Y: 6, Scale: 0.96},
	}
}

// motionReveal 是错误、提示词正文这类出现：只向下淡入，不左右晃。
func motionReveal() ui.ElementTransition {
	return ui.ElementTransition{
		Animation: &motionEaseOut,
		Position:  true,
		Enter:     &ui.Motion{Y: 6},
		Exit:      &ui.Motion{Y: 4},
	}
}

func motionColors() ui.ElementTransition {
	return ui.ElementTransition{Colors: true, Duration: motionFast, Ease: ui.EaseOut}
}

func (a *App) noteTab() {
	if !a.tabTracked {
		a.tabSeen = a.tab
		a.tabTracked = true
		return
	}
	if a.tab == a.tabSeen {
		return
	}
	if a.tab > a.tabSeen {
		a.tabDir = 1
	} else {
		a.tabDir = -1
	}
	a.tabSeen = a.tab
}

func (a *App) pageMotion() ui.ElementTransition {
	dir := a.tabDir
	if dir == 0 {
		dir = 1
	}
	return motionTravel(float32(dir)*motionSlide, 0)
}

func (a *App) stepMotion() ui.ElementTransition {
	dir := a.wizardDir
	if dir == 0 {
		dir = 1
	}
	return motionTravel(float32(dir)*motionSlide, 0)
}

// goWizard 记下这一步相对上一步的方向，供步骤切换时滑入。不改向导该去哪。
func (a *App) goWizard(step wizard.Step) {
	if step > a.wizardStep {
		a.wizardDir = 1
	} else if step < a.wizardStep {
		a.wizardDir = -1
	}
	a.wizardStep = step
}

func pressPose(e ui.Element) ui.Element {
	pose := ui.Pose{}
	if e.Pressed() {
		pose.Scale = 0.985
	}
	return e.AnimatePose(pose, motionEaseFast)
}

func hoverPressPose(e ui.Element) ui.Element {
	pose := ui.Pose{}
	switch {
	case e.Pressed():
		pose.Scale = 0.985
	case e.Hovered():
		pose.Scale = 1.012
	}
	return e.AnimatePose(pose, motionEaseFast)
}

// motionProgress 把进度条的填充缓动到 fraction。indeterminate 时画一条来回的光带，
// 只重绘、不重跑界面，生成中的卡片多了也不逐张重建视图。
func motionProgress(c *ui.Context, key string, fraction float64, indeterminate bool) {
	host := ui.Box(c.Key(key)).FillWidth().Height(6)
	if indeterminate {
		host.Children(func() {
			ui.Progress(c, -1).FillWidth().Height(6)
		})
		return
	}
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	shown := float64(host.AnimateTo("fraction", float32(fraction), motionEaseOut))
	host.Children(func() {
		ui.Progress(c, shown).FillWidth().Height(6)
	})
}

func wizardSteps(c *ui.Context, step wizard.Step) {
	t := c.Theme()
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		for i := 0; i < wizard.Count(); i++ {
			w := float32(8)
			color := t.Border
			switch {
			case i == int(step):
				w = 22
				color = t.Accent
			case i < int(step):
				color = t.Accent.Alpha(0.45)
			}
			dot := ui.Box(c.Key(i)).Height(4).Radius(2)
			dot.Width(dot.AnimateTo("w", w, motionEaseFast))
			dot.Background(dot.AnimateColor("c", color, motionEaseFast))
		}
	})
}

func waitingDot(c *ui.Context) {
	dot := ui.Box(c.Key("waiting-dot")).Size(8, 8).Radius(4).Shrink(0).Background(c.Theme().Accent)
	if c.Preferences().ReduceMotion {
		return
	}
	dot.Opacity(0.35 + 0.65*dot.Loop("pulse", 1200*time.Millisecond, ui.Bounce(ui.EaseInOut)))
}

func doneMark(c *ui.Context) {
	accent := c.Theme().Accent
	mark := ui.Box(c.Key("done-mark")).Size(28, 28).Shrink(0)
	drawn := float32(1)
	if !c.Preferences().ReduceMotion {
		drawn = mark.Keyframes("check", []ui.Keyframe{
			{At: 0, Value: 0},
			{At: motionUI, Value: 1, Ease: ui.EaseOut},
		})
	}
	mark.Draw(func(p *ui.Painter, r ui.Rect) {
		p.Stroke(ui.Rect{X: r.X + 1.5, Y: r.Y + 1.5, W: r.W - 3, H: r.H - 3}, accent, 13, 1.6)
		check := (&ui.Path{}).MoveTo(r.X+7, r.Y+14).LineTo(r.X+12, r.Y+19).LineTo(r.X+21, r.Y+9)
		p.StrokePath(check.Trim(0, drawn), 2, accent)
	})
}

func statusPill(c *ui.Context, status string) {
	t := c.Theme()
	fg, bg := t.TextMuted, t.Text.Alpha(0.08)
	switch status {
	case "完成":
		fg, bg = t.Success, t.Success.Alpha(0.16)
	case "失败":
		fg, bg = t.Danger, t.Danger.Alpha(0.14)
	case "生成中":
		fg, bg = t.Accent, t.Accent.Alpha(0.14)
	case "待验证":
		fg, bg = t.Warning, t.Warning.Alpha(0.16)
	}
	size := t.FontSize - 2
	if size < 11 {
		size = 11
	}
	pill := ui.Row(c.Key("status")).Padding(1, t.Space(1.5)).Radius(999).Shrink(0).AlignItems(ui.Center)
	pill.Background(pill.AnimateColor("bg", bg, motionEaseFast))
	pill.Children(func() {
		ui.Text(c.Key("status-label"), status).FontSize(size).TextColor(fg).SingleLine().
			ContentTransition(ui.ContentTransition{Animation: &motionEaseFast})
	})
}

// motionSelect 是带弹出过渡的下拉。外观跟主题里的 Select 对齐，打开和关上时面板淡入淡出。
func motionSelect(c *ui.Context, selected *string, options []string) ui.Element {
	t := c.Theme()
	sel := ui.SelectBase(c, selected)
	b := sel.Trigger
	bg := t.Surface
	if b.Pressed() {
		bg = t.SurfacePressed
	} else if b.Hovered() {
		bg = t.SurfaceHover
	}
	b.Padding(t.Space(1.5), t.Space(3.5)).Gap(t.Space(1.5)).Radius(t.Radius).
		Justify(ui.SpaceBetween).AlignItems(ui.Center).
		Background(b.AnimateColor("face", bg, motionEaseFast)).
		TextColor(t.Text).Border(1, t.Border)
	open := sel.Open()
	b.Children(func() {
		ui.Text(c, *selected).SingleLine().Grow(1).MinWidth(0)
		chev := ui.Box(c).Size(10, 10).Shrink(0)
		angle := float32(0)
		if open {
			angle = 180
		}
		chev.Rotate(chev.AnimateTo("chev", angle, motionEaseFast))
		chev.Draw(func(p *ui.Painter, r ui.Rect) {
			path := (&ui.Path{}).MoveTo(r.X+1, r.Y+3).LineTo(r.X+5, r.Y+7).LineTo(r.X+9, r.Y+3)
			p.StrokePath(path, 1.5, t.TextMuted)
		})
	})
	sel.Popup(func(panel ui.Element) {
		panel.Margin(t.Space(1), 0, 0, 0).Padding(t.Space(1)).Radius(t.Radius+2).
			Background(t.Background).Border(1, t.Border).
			Shadow(0, 6, 20, 0, ui.RGBA(0, 0, 0, 0.18)).
			Transition(motionPopup())
		ui.Scroll(c).MaxHeight(280).Children(func() {
			ui.Column(c).Gap(2).FillWidth().Children(func() {
				for _, opt := range options {
					item := sel.Item(opt).Padding(t.Space(1.5), t.Space(2.5)).Radius(t.Radius).FillWidth()
					switch {
					case item.Highlighted():
						item.Background(t.Accent).TextColor(t.AccentText)
					case opt == *selected:
						item.Background(t.Surface)
					}
					item.Children(func() { ui.Text(c, opt).SingleLine() })
				}
			})
		})
	})
	return b
}
