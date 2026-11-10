package ui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 窗口手动拖矮后，左栏输入区要能上下滚动，开始卡片和保存路径行始终完整露出。
func TestWorkSideScrollsWhenWindowIsShort(t *testing.T) {
	for _, page := range []int{pageHome, pageWash} {
		t.Run(pageLabel(page), func(t *testing.T) {
			a := &App{
				tab: page, statePage: page, platformAuto: true, stagePicked: true,
				outputSizeMode: outputPixels, outputWidth: "1200", outputHeight: "960",
				outputPath: filepath.Join("输出", "结果"),
				sources:    []sourceImage{{Path: "原图.png"}},
			}
			const (
				w float32 = 1024
				h float32 = 460
			)
			tt := ui.NewTester(a.view, int(w), int(h))
			tt.Advance(motionUI)

			start := "开始出图"
			top, below := "每行一条；导入段落会合并", "参考图"
			if page == pageWash {
				start = "开始洗图"
				top, below = "已选 1 张", "洗图指令"
			}
			// 开始卡片在输入区外面，矮窗口里也完整露出。
			assertVisible(t, tt, start, w, h)
			assertVisible(t, tt, "本次发送至：豆包", w, h)
			anchor := assertVisible(t, tt, top, w, h)

			// 在输入区里滚下去，下面的卡片要完整进入窗口，开始按钮不动。
			tt.Scroll(anchor.X+anchor.W/2, anchor.Y+anchor.H/2, 0, 500)
			tt.Advance(motionUI)
			assertVisible(t, tt, below, w, h)
			assertVisible(t, tt, start, w, h)
			assertVisible(t, tt, "本次发送至：豆包", w, h)

			// 再滚回顶部，上面的卡片重新可见。
			tt.Scroll(anchor.X+anchor.W/2, anchor.Y+anchor.H/2, 0, -500)
			tt.Advance(motionUI)
			assertVisible(t, tt, top, w, h)
		})
	}
}
