package ui

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// 平台标都用 @lobehub/icons-static-svg 的彩色版（unpkg.com/@lobehub/icons-static-svg/icons/<id>-color.svg）。
//
//go:embed assets/platforms/doubao.svg
var doubaoSVG []byte

//go:embed assets/platforms/qwen.svg
var qwenSVG []byte

//go:embed assets/platforms/antigravity.svg
var antigravitySVG []byte

var (
	iconDoubao      = ui.MustParseSVG(doubaoSVG)
	iconQianwen     = ui.MustParseSVG(qwenSVG)
	iconAntigravity = ui.MustParseSVG(antigravitySVG)
)

// platformIcon 在 16–20 左右画平台标。不认识的平台用中性圆。
func platformIcon(c *ui.Context, platform string, size float32, tok tokens) {
	if size < 12 {
		size = 12
	}
	switch platform {
	case "豆包":
		ui.Image(c.Key("plat-doubao"), iconDoubao).Size(size, size).Fit(ui.Contain).Shrink(0)
	case "千问":
		ui.Image(c.Key("plat-qianwen"), iconQianwen).Size(size, size).Fit(ui.Contain).Shrink(0)
	case "Antigravity":
		ui.Image(c.Key("plat-agy"), iconAntigravity).Size(size, size).Fit(ui.Contain).Shrink(0)
	default:
		ui.Box(c.Key("plat-none")).Size(size, size).Radius(size/2).Shrink(0).
			Background(tok.sunken).Border(1, tok.border)
	}
}
