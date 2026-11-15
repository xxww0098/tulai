package ui

import "github.com/egoist/mygo/ui"

// interfaceZoom 缩小界面控件，为工作区和图片留出更多窗口空间。
const interfaceZoom float32 = 1.0

// tokens 是出图页的层次：纸色底、侧栏、抬起的卡片、沉下去的槽，加上圆角、字号和状态色。
// 窗口控件仍走 Theme，这里多出来的是卡片、徽标和缩略图要用、Theme 里没有的几档。
type tokens struct {
	dark                          bool
	paper, rail, card, sunken     ui.Color
	sunkenHi                      ui.Color
	border, borderStrong          ui.Color
	text, muted, subtle           ui.Color
	accent, onAccent              ui.Color
	success, warning, danger      ui.Color
	shadow, lift                  ui.Color
	radiusS, radiusM, radiusL     float32
	display, title, body, caption float32
}

func workbenchTokens(dark bool) tokens {
	if dark {
		return tokens{
			dark:         true,
			paper:        ui.Hex("#0B0E14"),
			rail:         ui.Hex("#0F131B"),
			card:         ui.Hex("#141923"),
			sunken:       ui.Hex("#1B2230"),
			sunkenHi:     ui.Hex("#242D3F"),
			border:       ui.Hex("#252F42"),
			borderStrong: ui.Hex("#34425A"),
			text:         ui.Hex("#F1F5F9"),
			muted:        ui.Hex("#94A3B8"),
			subtle:       ui.Hex("#64748B"),
			accent:       ui.Hex("#60A5FA"),
			onAccent:     ui.Hex("#090D16"),
			success:      ui.Hex("#34D399"),
			warning:      ui.Hex("#FBBF24"),
			danger:       ui.Hex("#F87171"),
			shadow:       ui.RGBA(0, 0, 0, 0.36),
			lift:         ui.RGBA(0, 0, 0, 0.46),
			radiusS:      4, radiusM: 6, radiusL: 8,
			display: 13.5, title: 11.5, body: 11, caption: 10,
		}
	}
	return tokens{
		paper:        ui.Hex("#F6F8FA"),
		rail:         ui.Hex("#F9FAFC"),
		card:         ui.Hex("#FFFFFF"),
		sunken:       ui.Hex("#F0F3F7"),
		sunkenHi:     ui.Hex("#E6EBF2"),
		border:       ui.Hex("#E1E6EF"),
		borderStrong: ui.Hex("#CBD4E1"),
		text:         ui.Hex("#0F172A"),
		muted:        ui.Hex("#5B677A"),
		subtle:       ui.Hex("#8A96A8"),
		accent:       ui.Hex("#2563EB"),
		onAccent:     ui.Hex("#FFFFFF"),
		success:      ui.Hex("#15803D"),
		warning:      ui.Hex("#B45309"),
		danger:       ui.Hex("#DC2626"),
		shadow:       ui.RGBA(15, 23, 42, 0.06),
		lift:         ui.RGBA(15, 23, 42, 0.08),
		radiusS:      4, radiusM: 6, radiusL: 8,
		display: 13.5, title: 11.5, body: 11, caption: 10,
	}
}

func lightTokens() tokens { return workbenchTokens(false) }

func darkTokens() tokens { return workbenchTokens(true) }

// applyWorkbenchTheme 把设计令牌写进当前帧的主题，页头和页签跟着走。
func applyWorkbenchTheme(c *ui.Context) tokens {
	tok := workbenchTokens(c.Theme().Dark)
	th := *c.Theme()
	th.Dark = tok.dark
	th.Background = tok.card
	th.Surface = tok.sunken
	th.SurfaceHover = tok.sunkenHi
	th.SurfacePressed = tok.sunkenHi
	th.Border = tok.border
	th.Text = tok.text
	th.TextMuted = tok.muted
	th.Accent = tok.accent
	th.AccentText = tok.onAccent
	if tok.dark {
		th.AccentHover = ui.Hex("#93C5FD")
		th.AccentPressed = ui.Hex("#3B82F6")
		th.Inverse = ui.Hex("#F1F5F9")
		th.InverseText = ui.Hex("#0B0E14")
		th.Scrollbar = ui.RGBA(148, 163, 184, 0.28)
	} else {
		th.AccentHover = ui.Hex("#1D4ED8")
		th.AccentPressed = ui.Hex("#1E40AF")
		th.Inverse = ui.Hex("#0F172A")
		th.InverseText = ui.Hex("#F8FAFC")
		th.Scrollbar = ui.RGBA(15, 23, 42, 0.24)
	}
	th.Success = tok.success
	th.Warning = tok.warning
	th.Danger = tok.danger
	th.Selection = tok.accent.Alpha(0.22)
	th.Focus = tok.accent.Alpha(0.55)
	th.Radius = 5
	th.Spacing = 2
	th.FontSize = tok.body
	th.ScrollbarWidth = 4
	th.Font = "Inter, WenQuanYi Micro Hei"
	c.SetTheme(&th)
	return tok
}

func keyedContext(c *ui.Context, key string) *ui.Context {
	if key != "" {
		return c.Key(key)
	}
	return c
}

// semanticToneColors 计算浅/深色下都满足 >=4.5:1 对比度的前景色、柔底色与细边框色。
func semanticToneColors(dark bool, text, base ui.Color, wash float32) (fg, bg, border ui.Color) {
	fg = base
	if !dark {
		fg = base.Mix(text, 0.25)
	}
	return fg, base.Alpha(wash), fg.Alpha(0.22)
}

func badgeTone(tone string, tok tokens) (fg, bg, border ui.Color) {
	switch tone {
	case "accent":
		return semanticToneColors(tok.dark, tok.text, tok.accent, 0.12)
	case "success":
		return semanticToneColors(tok.dark, tok.text, tok.success, 0.12)
	case "warning":
		return semanticToneColors(tok.dark, tok.text, tok.warning, 0.12)
	case "danger":
		return semanticToneColors(tok.dark, tok.text, tok.danger, 0.12)
	default:
		return semanticToneColors(tok.dark, tok.text, tok.muted, 0.10)
	}
}

func interactiveTone(active, hovered bool, idleFg ui.Color, tok tokens) (fg, bg, border ui.Color) {
	if active {
		fg, bg, border = semanticToneColors(tok.dark, tok.text, tok.accent, 0.14)
		if hovered {
			return fg, tok.accent.Alpha(0.20), fg.Alpha(0.42)
		}
		return fg, bg, border
	}
	if hovered {
		return tok.text, tok.sunkenHi, tok.borderStrong
	}
	return idleFg, tok.sunken, tok.border
}

// uiCardSurface 给任意元素统一卡片底色、1px 细边框与中圆角。
func uiCardSurface(el ui.Element, tok tokens) ui.Element {
	return el.Background(tok.card).Border(1, tok.border).Radius(tok.radiusM)
}

// uiCard 创建统一的小圆角浅边框工作区卡片，减少留白以便内容占主导。
func uiCard(c *ui.Context, key string, tok tokens) ui.Element {
	return uiCardSurface(ui.Column(keyedContext(c, key)).FillWidth().Padding(7, 9).Gap(5), tok)
}

// uiBadge 渲染紧凑的语义状态胶囊，保证浅/深色 >=4.5:1 对比度与等宽数字。
func uiBadge(c *ui.Context, key, label, tone string, tok tokens) ui.Element {
	fg, bg, border := badgeTone(tone, tok)
	pill := ui.Row(keyedContext(c, key)).Height(18).Padding(0, 6).Radius(999).
		Shrink(0).AlignItems(ui.Center).Background(bg).Border(1, border)
	pill.Children(func() {
		ui.Text(c, label).FontSize(tok.caption).FontWeight(500).TextColor(fg).
			SingleLine().FontFeatures("tnum")
	})
	return pill
}

// uiPageHeader 渲染管理/工具页的标准顶部标题行。
func uiPageHeader(c *ui.Context, key string, icon *ui.SVG, title, subtitle string, tok tokens, right func()) {
	ui.Row(keyedContext(c, key)).FillWidth().Gap(8).Padding(0, 0, 6, 0).
		AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(tok.border).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(7).AlignItems(ui.Center).Children(func() {
			if icon != nil {
				ui.Row(c).Size(24, 24).Radius(tok.radiusS).Center().Shrink(0).
					Background(tok.sunken).Border(1, tok.border).Children(func() {
					ui.Icon(c, icon).FontSize(13).TextColor(tok.accent)
				})
			}
			ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
				if title != "" {
					ui.Text(c, title).FontSize(tok.title).Bold().TextColor(tok.text).SingleLine().MinWidth(0)
				}
				if subtitle != "" {
					ui.Text(c, subtitle).FontSize(tok.caption).TextColor(tok.muted).SingleLine().MinWidth(0).Tooltip(subtitle)
				}
			})
		})
		if right != nil {
			ui.Row(c).Shrink(0).Gap(4).AlignItems(ui.Center).Children(right)
		}
	})
}

// uiSectionTitle 渲染分区标题与可选的右侧等宽计数/元信息。
func uiSectionTitle(c *ui.Context, title, meta string, tok tokens) {
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(tok.body).FontWeight(600).TextColor(tok.text).SingleLine().Grow(1).MinWidth(0)
		if meta != "" {
			ui.Text(c, meta).FontSize(tok.caption).TextColor(tok.muted).SingleLine().FontFeatures("tnum").Shrink(0)
		}
	})
}

// uiBtn 把标准按钮统一收成紧凑的小号高度与字号，悬停时强化细边框。
func uiBtn(b ui.Element, tok tokens) ui.Element {
	if b.Hovered() {
		b.BorderColor(tok.borderStrong)
	}
	return b.FontSize(tok.body).Height(24).Padding(0, 8).Radius(tok.radiusS).Shrink(0)
}

// uiMiniBtn 用于表格行内或卡片标题旁的轻量操作按钮，悬停时强化细边框。
func uiMiniBtn(b ui.Element, tok tokens) ui.Element {
	if b.Hovered() {
		b.BorderColor(tok.borderStrong)
	}
	return b.FontSize(tok.caption).Height(20).Padding(0, 6).Radius(tok.radiusS).Shrink(0)
}

// uiIconBtn 构建紧凑的图标按钮，保留 Label 与 Tooltip 以兼顾无障碍与鼠标提示。
func uiIconBtn(c *ui.Context, key, label, tip string, ico *ui.SVG, size float32, active, ghost bool, tok tokens) ui.Element {
	if tip == "" {
		tip = label
	}
	btn := ui.ButtonBase(c.Key(key)).Size(size, size).Padding(0).Radius(tok.radiusS).Center().Shrink(0).
		Cursor(ui.CursorPointer).Label(label).Tooltip(tip)
	hovered := btn.Hovered()
	var fg, bg, border ui.Color
	if ghost && !active {
		if hovered {
			btn.Background(tok.sunken).Border(1, tok.border)
			fg = tok.text
		} else {
			btn.Background(ui.Transparent)
			fg = tok.muted
		}
	} else {
		fg, bg, border = interactiveTone(active, hovered, tok.text, tok)
		btn.Background(bg).Border(1, border)
	}
	hoverPressPose(btn)
	icoSize := size * 0.56
	if icoSize < 11 {
		icoSize = 11
	}
	btn.Children(func() {
		ui.Icon(c, ico).FontSize(icoSize).TextColor(fg)
	})
	return btn
}

// uiIconTextBtn 构建带小图标与极简短字的紧凑 SaaS 按钮（适用于主要 CTA 或需消歧义的动作）。
func uiIconTextBtn(c *ui.Context, key, label, tip string, ico *ui.SVG, h float32, active bool, tok tokens) ui.Element {
	btn := ui.ButtonBase(c.Key(key)).Height(h).Padding(0, 7).Radius(tok.radiusS).Center().Shrink(0).
		Cursor(ui.CursorPointer).Label(label)
	if tip != "" {
		btn.Tooltip(tip)
	}
	fg, bg, border := interactiveTone(active, btn.Hovered(), tok.text, tok)
	btn.Background(bg).Border(1, border)
	hoverPressPose(btn)
	btn.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			icoSize := float32(12)
			if h <= 20 {
				icoSize = 11
			}
			ui.Icon(c, ico).FontSize(icoSize).TextColor(fg)
			t := ui.Text(c, label).FontSize(tok.caption).SingleLine().TextColor(fg)
			if active {
				t.FontWeight(600)
			}
		})
	})
	return btn
}

// uiPill 构建紧凑的单选筛选或开关胶囊按钮。
func uiPill(c *ui.Context, key, label string, active bool, tok tokens) ui.Element {
	btn := ui.ButtonBase(c.Key(key)).Height(20).Padding(0, 8).Radius(999).Center().Shrink(0).
		Cursor(ui.CursorPointer).Label(label)
	fg, bg, border := interactiveTone(active, btn.Hovered(), tok.muted, tok)
	btn.Background(bg).Border(1, border)
	hoverPressPose(btn)
	btn.Children(func() {
		t := ui.Text(c, label).FontSize(tok.caption).SingleLine().TextColor(fg)
		if active {
			t.FontWeight(600)
		}
	})
	return btn
}
