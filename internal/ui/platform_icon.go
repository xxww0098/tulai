package ui

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// 豆包使用官方头像 PNG；千问 / Antigravity / ChatGPT 使用 @lobehub/icons-static-svg 的矢量图标。
//
//go:embed assets/platforms/doubao.png
var doubaoPNG []byte

//go:embed assets/platforms/qwen.svg
var qwenSVG []byte

//go:embed assets/platforms/antigravity.svg
var antigravitySVG []byte

//go:embed assets/platforms/openai.svg
var openaiSVG []byte

var (
	iconDoubao      = mustBitmap(doubaoPNG)
	iconQianwen     = ui.MustParseSVG(qwenSVG)
	iconAntigravity = ui.MustParseSVG(antigravitySVG)
	iconChatGPT     = ui.MustParseSVG(openaiSVG)
)

func mustBitmap(data []byte) *ui.Bitmap {
	bmp, err := ui.DecodeBitmap(data)
	if err != nil {
		panic(err)
	}
	return bmp
}

// platformIcon 在 16–20 左右画平台标。不认识的平台用中性圆。
func platformIcon(c *ui.Context, platform string, size float32, tok tokens) {
	if size < 12 {
		size = 12
	}
	switch platform {
	case "豆包":
		ui.Image(c.Key("plat-doubao"), iconDoubao).Size(size, size).Radius(size / 2).Fit(ui.Contain).Shrink(0)
	case "千问":
		ui.Image(c.Key("plat-qianwen"), iconQianwen).Size(size, size).Fit(ui.Contain).Shrink(0)
	case "Antigravity":
		ui.Image(c.Key("plat-agy"), iconAntigravity).Size(size, size).Fit(ui.Contain).Shrink(0)
	case "ChatGPT":
		ui.Icon(c.Key("plat-chatgpt"), iconChatGPT).FontSize(size).TextColor(tok.text).Shrink(0)
	default:
		ui.Box(c.Key("plat-none")).Size(size, size).Radius(size/2).Shrink(0).
			Background(tok.sunken).Border(1, tok.border)
	}
}
