package ui

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

//go:embed assets/platforms/qianwen.svg
var qianwenSVG []byte

//go:embed assets/platforms/antigravity.svg
var antigravitySVG []byte

//go:embed assets/platforms/doubao-192.png
var doubaoPNG []byte

// 千问用官方矢量，颜色留在稿里。豆包没有官方 SVG，用官网 192 的圆形头像。
// Antigravity 用同一套强调色画的简单标记。
var (
	iconQianwen     = ui.MustParseSVG(qianwenSVG)
	iconAntigravity = ui.MustParseSVG(antigravitySVG)
	iconDoubao      = mustPlatformBitmap(doubaoPNG)
)

func mustPlatformBitmap(data []byte) *ui.Bitmap {
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		panic(err)
	}
	return bmp
}

// platformIcon 在 16–20 左右画平台标。豆包裁成圆；不认识的平台用中性圆，不再写「豆」「千」。
func platformIcon(c *ui.Context, platform string, size float32, tok tokens) {
	if size < 12 {
		size = 12
	}
	switch platform {
	case "豆包":
		ui.Box(c.Key("plat-doubao")).Size(size, size).Radius(size / 2).Clip().Shrink(0).Children(func() {
			ui.Image(c, iconDoubao).Size(size, size).Fit(ui.Cover)
		})
	case "千问":
		ui.Image(c.Key("plat-qianwen"), iconQianwen).Size(size, size).Fit(ui.Contain).Shrink(0)
	case "Antigravity":
		ui.Image(c.Key("plat-agy"), iconAntigravity).Size(size, size).Fit(ui.Contain).Shrink(0)
	default:
		ui.Box(c.Key("plat-none")).Size(size, size).Radius(size/2).Shrink(0).
			Background(tok.sunken).Border(1, tok.border)
	}
}
