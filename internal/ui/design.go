package ui

import "github.com/egoist/mygo/ui"

// interfaceZoom 缩小界面控件，为工作区和图片留出更多窗口空间。
const interfaceZoom float32 = 1.0

// tokens 是出图页的层次：纸色底、抬起的卡片、沉下去的槽，加上圆角、字号和状态色。
// 窗口控件仍走 Theme，这里多出来的是卡片和缩略图要用、Theme 里没有的几档。
type tokens struct {
	dark                          bool
	paper, card, sunken, sunkenHi ui.Color
	border                        ui.Color
	text, muted                   ui.Color
	accent, onAccent              ui.Color
	success, warning, danger      ui.Color
	shadow, lift                  ui.Color
	radiusS, radiusM, radiusL     float32
	display, title, body, caption float32
}

func lightTokens() tokens {
	return tokens{
		paper:    ui.Hex("#F2F5FA"),
		card:     ui.Hex("#FFFFFF"),
		sunken:   ui.Hex("#E6EDF6"),
		sunkenHi: ui.Hex("#D5E0EF"),
		border:   ui.Hex("#D3DDEA"),
		text:     ui.Hex("#1A2332"),
		muted:    ui.Hex("#5C6B80"),
		accent:   ui.Hex("#2F5BFF"),
		onAccent: ui.Hex("#FFFFFF"),
		success:  ui.Hex("#0E8A5B"),
		warning:  ui.Hex("#C47A12"),
		danger:   ui.Hex("#D64550"),
		shadow:   ui.RGBA(16, 32, 64, 0.08),
		lift:     ui.RGBA(47, 91, 255, 0.16),
		radiusS: 4, radiusM: 6, radiusL: 8,
		display: 13.5, title: 11.5, body: 11, caption: 10,
	}
}

func darkTokens() tokens {
	return tokens{
		dark:     true,
		paper:    ui.Hex("#0E131A"),
		card:     ui.Hex("#181F2A"),
		sunken:   ui.Hex("#10161F"),
		sunkenHi: ui.Hex("#243041"),
		border:   ui.Hex("#2E3B4E"),
		text:     ui.Hex("#E8EEF6"),
		muted:    ui.Hex("#96A3B6"),
		accent:   ui.Hex("#7C9BFF"),
		onAccent: ui.Hex("#0E131A"),
		success:  ui.Hex("#3DDBA0"),
		warning:  ui.Hex("#F0B429"),
		danger:   ui.Hex("#FF7380"),
		shadow:   ui.RGBA(0, 0, 0, 0.45),
		lift:     ui.RGBA(124, 155, 255, 0.25),
		radiusS: 4, radiusM: 6, radiusL: 8,
		display: 13.5, title: 11.5, body: 11, caption: 10,
	}
}

// applyWorkbenchTheme 把设计令牌写进当前帧的主题，页头和页签跟着走。
func applyWorkbenchTheme(c *ui.Context) tokens {
	tok := lightTokens()
	if c.Theme().Dark {
		tok = darkTokens()
	}
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
		th.AccentHover = ui.Hex("#9BB2FF")
		th.AccentPressed = ui.Hex("#6484F0")
		th.Inverse = ui.Hex("#F4F7FB")
		th.InverseText = ui.Hex("#121820")
		th.Scrollbar = ui.RGBA(255, 255, 255, 0.32)
	} else {
		th.AccentHover = ui.Hex("#244BE0")
		th.AccentPressed = ui.Hex("#1C3CC0")
		th.Inverse = ui.Hex("#1A2332")
		th.InverseText = ui.Hex("#F7F9FC")
		th.Scrollbar = ui.RGBA(26, 35, 50, 0.28)
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

// uiCard 创建统一的小圆角浅边框工作区卡片，减少留白以便内容占主导。
func uiCard(c *ui.Context, key string, tok tokens) ui.Element {
	return ui.Column(c.Key(key)).FillWidth().Padding(7, 9).Gap(5).Radius(tok.radiusM).
		Background(tok.card).Border(1, tok.border)
}

// uiBtn 把标准按钮统一收成紧凑的小号高度与字号。
func uiBtn(b ui.Element, tok tokens) ui.Element {
	return b.FontSize(tok.body).Height(24).Padding(0, 8).Radius(tok.radiusS).Shrink(0)
}

// uiMiniBtn 用于表格行内或卡片标题旁的轻量操作按钮。
func uiMiniBtn(b ui.Element, tok tokens) ui.Element {
	return b.FontSize(tok.caption).Height(20).Padding(0, 6).Radius(tok.radiusS).Shrink(0)
}

// uiIconBtn 构建紧凑的图标按钮，保留 Label 与 Tooltip 以兼顾无障碍与鼠标提示。
func uiIconBtn(c *ui.Context, key, label, tip string, ico *ui.SVG, size float32, active, ghost bool, tok tokens) ui.Element {
	if tip == "" {
		tip = label
	}
	btn := ui.ButtonBase(c.Key(key)).Size(size, size).Padding(0).Radius(tok.radiusS).Center().Shrink(0).
		Cursor(ui.CursorPointer).Label(label).Tooltip(tip)
	fg := tok.muted
	switch {
	case active:
		btn.Background(tok.accent.Alpha(0.14)).Border(1, tok.accent.Alpha(0.35))
		fg = tok.accent
	case ghost:
		btn.Background(ui.Transparent)
		fg = tok.muted
	default:
		btn.Background(tok.sunken).Border(1, tok.border)
		fg = tok.text
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
	fg := tok.text
	if active {
		btn.Background(tok.accent.Alpha(0.14)).Border(1, tok.accent.Alpha(0.35))
		fg = tok.accent
	} else {
		btn.Background(tok.sunken).Border(1, tok.border)
	}
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
	if active {
		btn.Background(tok.accent.Alpha(0.14)).Border(1, tok.accent.Alpha(0.35))
	} else {
		btn.Background(tok.sunken).Border(1, tok.border)
	}
	hoverPressPose(btn)
	btn.Children(func() {
		fg := tok.muted
		t := ui.Text(c, label).FontSize(tok.caption).SingleLine()
		if active {
			fg = tok.accent
			t.FontWeight(600)
		}
		t.TextColor(fg)
	})
	return btn
}
