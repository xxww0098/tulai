package ui

import "github.com/egoist/mygo/ui"

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
		radiusS:  8, radiusM: 12, radiusL: 16,
		display: 22, title: 16, body: 14, caption: 12,
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
		radiusS:  8, radiusM: 12, radiusL: 16,
		display: 22, title: 16, body: 14, caption: 12,
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
	th.Radius = 10
	th.Spacing = 4
	th.FontSize = tok.body
	th.Font = "Inter, WenQuanYi Micro Hei"
	c.SetTheme(&th)
	return tok
}
