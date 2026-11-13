package ui

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestDoubaoOutputSizeSettingsPersistAndReachTasks(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, manager: browser.NewManager(), platformAuto: true}
	a.loadSettings()
	if got := a.doubaoOutputSize(); got.Enabled {
		t.Fatalf("default should preserve output: %+v", got)
	}
	tt := ui.NewTester(a.view, 1280, 1000)
	assertVisible(t, tt, "豆包输出尺寸", 1280, 1000)
	if err := tt.Click("豆包输出尺寸"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("指定像素"); err != nil {
		t.Fatal(err)
	}
	assertVisible(t, tt, "输出宽度", 1280, 1000)
	assertVisible(t, tt, "输出高度", 1280, 1000)
	assertVisible(t, tt, "居中裁剪铺满画面", 1280, 1000)
	if err := tt.Click("常用像素"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if err := tt.Click("1920×1080"); err != nil {
		t.Fatal(err)
	}
	if got := a.doubaoOutputSize(); got.Width != 1920 || got.Height != 1080 {
		t.Fatalf("pixel preset did not update the fields: %+v", got)
	}
	if err := tt.Click("输出宽度"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("abc")
	if err := a.doubaoOutputSize().Validate(); err == nil || !tt.HasText("输出尺寸：宽高须为正整数，单边不超过 65535，总像素不超过 5000 万") {
		t.Fatal("invalid custom pixels were not shown and rejected")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("1200")
	if err := tt.Click("输出高度"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("960")
	if err := tt.Click("居中裁剪铺满画面"); err != nil {
		t.Fatal(err)
	}
	size := a.taskOptions([]string{"豆包"}, nil, nil).OutputSize
	if !size.Enabled || !size.Crop || size.Width != 1200 || size.Height != 960 || size.Validate() != nil {
		t.Fatalf("pixel settings did not reach tasks: %+v", size)
	}
	if err := tt.Click("批量洗图"); err != nil {
		t.Fatal(err)
	}
	assertVisible(t, tt, "豆包输出尺寸", 1280, 1000)
	if got := a.doubaoOutputSize(); got != size {
		t.Fatalf("wash page lost the selected size: %+v", got)
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("豆包输出尺寸") {
		t.Fatal("output size controls still appear in settings")
	}
	a.saveSettings()
	restored := &App{store: st, manager: browser.NewManager()}
	restored.loadSettings()
	if got := restored.doubaoOutputSize(); got != size {
		t.Fatalf("pixel settings not restored: got %+v want %+v", got, size)
	}
	if err := tt.Click("图片生成"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("豆包输出尺寸"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("指定比例"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("常用比例"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("16:9"); err != nil {
		t.Fatal(err)
	}
	if got := a.doubaoOutputSize(); !got.Enabled || got.Ratio != "16:9" || got.Width != 0 || got.Height != 0 {
		t.Fatalf("ratio preset not passed to tasks: %+v", got)
	}
	if err := tt.Click("画面比例"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("7:5")
	a.saveSettings()
	restored.loadSettings()
	if got := restored.doubaoOutputSize(); got.Ratio != "7:5" || got.Validate() != nil || restored.outputWidth != "1200" || restored.outputHeight != "960" {
		t.Fatalf("custom ratio or last pixel dimensions not restored: size=%+v pixels=%s×%s", got, restored.outputWidth, restored.outputHeight)
	}
}

func TestOutputSizeControlsStayVisibleOnWorkspaces(t *testing.T) {
	for _, page := range []int{pageHome, pageWash} {
		for _, size := range [][2]int{{1280, 840}, {1024, 700}, {760, 900}} {
			for _, mode := range []string{outputOriginal, outputRatio, outputPixels} {
				t.Run(fmt.Sprintf("%s/%dx%d/%s", pageLabel(page), size[0], size[1], mode), func(t *testing.T) {
					a := &App{
						tab: page, statePage: page, platformAuto: true, stagePicked: true,
						outputSizeMode: mode, outputRatio: "5:4", outputWidth: "1200", outputHeight: "960",
					}
					if page == pageWash {
						a.sources = []sourceImage{{Path: "原图.png"}}
					}
					tt := ui.NewTester(a.view, size[0], size[1])
					tt.Advance(motionUI)
					w, h := float32(size[0]), float32(size[1])
					assertVisible(t, tt, "豆包输出尺寸", w, h)
					start := "开始出图"
					if page == pageWash {
						start = "开始洗图"
					}
					assertVisible(t, tt, start, w, h)
					if mode != outputOriginal {
						assertVisible(t, tt, "居中裁剪铺满画面", w, h)
					}
					if mode == outputRatio {
						assertVisible(t, tt, "画面比例", w, h)
						assertVisible(t, tt, "常用比例", w, h)
					}
					if mode == outputPixels {
						width := assertVisible(t, tt, "输出宽度", w, h)
						height := assertVisible(t, tt, "输出高度", w, h)
						presets := assertVisible(t, tt, "常用像素", w, h)
						crop := assertVisible(t, tt, "居中裁剪铺满画面", w, h)
						if width.X+width.W > height.X || height.X+height.W > presets.X || (crop.Y < presets.Y+presets.H-1 && presets.X+presets.W > crop.X) {
							t.Fatalf("pixel controls overlap: width=%v height=%v presets=%v crop=%v", width, height, presets, crop)
						}
					}
				})
			}
		}
	}
}

func TestOutputSizeFollowsPlatformAndIsDisabledDuringRun(t *testing.T) {
	a := &App{platformAuto: true, stagePicked: true, outputSizeMode: outputPixels, outputWidth: "1200", outputHeight: "960"}
	tt := ui.NewTester(a.view, 1280, 840)
	assertVisible(t, tt, "豆包输出尺寸", 1280, 840)
	if err := tt.Click("千问"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("豆包输出尺寸") || tt.HasText("输出宽度") {
		t.Fatal("doubao output controls appeared for another platform")
	}
	if err := tt.Click("豆包"); err != nil {
		t.Fatal(err)
	}
	assertVisible(t, tt, "输出宽度", 1280, 840)
	a.running = true
	tt.Frame()
	_ = tt.Click("输出宽度")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("512")
	_ = tt.Click("豆包输出尺寸")
	if a.outputWidth != "1200" || tt.HasText("指定比例") {
		t.Fatalf("output settings changed during a run: width=%q", a.outputWidth)
	}
}
