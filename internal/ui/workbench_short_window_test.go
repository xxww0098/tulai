package ui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// 窗口手动拖矮后，左栏输入区要能上下滚动，开始卡片和保存路径行始终完整露出。
// 滚动本身用 TrackScroll 的位移断言；视口外的文本在无头渲染里可能是 0×0，不能当可见性证据。
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
				h float32 = 400
			)
			tt := ui.NewTester(a.view, int(w), int(h))
			tt.Advance(motionUI)

			start := "开始出图"
			top := "提示词内容"
			if page == pageWash {
				start = "开始洗图"
				top = "1 张"
			}
			// 开始卡片在输入区外面，矮窗口里也完整露出。
			assertVisible(t, tt, start, w, h)
			assertVisible(t, tt, "本次发送至：豆包", w, h)
			anchor := assertVisible(t, tt, top, w, h)
			if a.workInputScroll.MaxY <= 0 {
				t.Fatalf("矮窗口里输入区应该溢出可滚: %+v", a.workInputScroll)
			}
			if a.workInputScroll.Y != 0 {
				t.Fatalf("初始应停在顶部: %+v", a.workInputScroll)
			}

			// 在输入区里滚下去，再滚回顶部；开始按钮始终不动。
			tt.Scroll(anchor.X+anchor.W/2, anchor.Y+anchor.H/2, 0, 500)
			tt.Advance(motionUI)
			if a.workInputScroll.Y <= 0 {
				t.Fatalf("输入区无法向下滚动: %+v", a.workInputScroll)
			}
			assertVisible(t, tt, start, w, h)
			assertVisible(t, tt, "本次发送至：豆包", w, h)

			tt.Scroll(anchor.X+anchor.W/2, anchor.Y+anchor.H/2, 0, -500)
			tt.Advance(motionUI)
			if a.workInputScroll.Y != 0 {
				t.Fatalf("滚不回顶部: %+v", a.workInputScroll)
			}
			assertVisible(t, tt, top, w, h)
		})
	}
}
