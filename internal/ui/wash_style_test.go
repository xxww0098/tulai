package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestWashStyleSelectionPersistsAndReachesEachTask(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, manager: browser.NewManager(), platformAuto: true}
	a.loadSettings()
	a.tab = pageWash
	a.syncPage()
	a.prompts = "保留原图中的花朵和背景"
	parts := a.currentPromptParts()
	parts.Background = promptPart{Enabled: true, Value: "浅蓝背景"}
	parts.Style = promptPart{Enabled: true, Value: "水彩风格"}

	tt := ui.NewTester(a.view, 1280, 900)
	assertVisible(t, tt, "洗图风格", 1280, 900)
	if a.washStyle != washStyleOriginal {
		t.Fatalf("default style changed: %q", a.washStyle)
	}
	if err := tt.Click("洗图风格"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	if err := tt.Click("像素风格"); err != nil {
		t.Fatal(err)
	}
	if a.washStyle != "像素风格" || st.Get("wash_style") != "像素风格" {
		t.Fatalf("selection did not persist: selected=%q stored=%q", a.washStyle, st.Get("wash_style"))
	}
	opts := a.taskOptions([]string{"豆包", "千问"}, a.taskPromptList(), []string{"a.png", "b.png"})
	opts.Wash = true
	queue := tasks.BuildQueue(opts, nil)
	if len(queue) != 4 {
		t.Fatalf("incorrect image/platform task count: %d", len(queue))
	}
	for _, task := range queue {
		if !strings.Contains(task.Prompt, "保留原图中的花朵和背景") ||
			!strings.Contains(task.Prompt, "背景：浅蓝背景") ||
			!strings.Contains(task.Prompt, "本次洗图风格：像素风格") ||
			!strings.Contains(task.Prompt, "风格转换优先") || strings.Contains(task.Prompt, "风格：水彩风格") {
			t.Fatalf("wrong task prompt for %s: %q", task.Platform, task.Prompt)
		}
	}
	if a.prompts != "保留原图中的花朵和背景" || parts.Style.Value != "水彩风格" || !parts.Style.Enabled {
		t.Fatal("preset changed the source prompt or the saved custom style")
	}
	queue[0].Status = tasks.StatusFailed
	a.washStyle = "油画风格"
	retry := tasks.RebuildRetry(queue[:1])
	if len(retry) != 1 || retry[0].Prompt != queue[0].Prompt || strings.Count(retry[0].Prompt, "本次洗图风格：") != 1 {
		t.Fatalf("retry changed the original task's style: %+v", retry)
	}

	restored := &App{store: st, manager: browser.NewManager()}
	restored.loadSettings()
	if restored.washStyle != "像素风格" {
		t.Fatalf("selected style not restored: %q", restored.washStyle)
	}
	a.tab = pageHome
	a.syncPage()
	if extra := a.taskOptions([]string{"豆包"}, []string{"自由生图"}, nil).StructuredPrompt; strings.Contains(extra, "本次洗图风格") {
		t.Fatalf("wash style leaked into image generation: %q", extra)
	}
	st.Set("wash_style", "已不存在的风格")
	restored.loadWashStyle()
	if restored.washStyle != washStyleOriginal {
		t.Fatalf("unknown stored style was not reset: %q", restored.washStyle)
	}
}

func TestWashStylePresetsOverrideOnlyCustomStylePart(t *testing.T) {
	for _, style := range washStyles {
		t.Run(style.label, func(t *testing.T) {
			a := &App{tab: pageWash, statePage: pageWash, washStyle: style.label, structuredPage: pageWash}
			parts := a.currentPromptParts()
			parts.Background = promptPart{Enabled: true, Value: "蓝色背景"}
			parts.Style = promptPart{Enabled: true, Value: "自定义风格"}
			extra := a.promptExtras(pageWash, parts)
			if !strings.Contains(extra, "背景：蓝色背景") {
				t.Fatalf("lost another parameter: %q", extra)
			}
			if style.label == washStyleOriginal {
				if !strings.Contains(extra, "风格：自定义风格") || strings.Contains(extra, "本次洗图风格：") {
					t.Fatalf("default should retain custom prompt parts: %q", extra)
				}
			} else if !strings.Contains(extra, "本次洗图风格："+style.label) || strings.Contains(extra, "风格：自定义风格") {
				t.Fatalf("wrong preset precedence: %q", extra)
			}
			if preview := a.promptPartsPreview(parts); !strings.Contains(preview, extra) {
				t.Fatalf("preview does not show what will be sent: %q", preview)
			}
		})
	}
}

func TestWashStyleControlsAreVisibleAndDisabledWhileRunning(t *testing.T) {
	for _, size := range [][2]int{{1280, 840}, {1024, 700}, {1024, 460}, {760, 900}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			a := &App{tab: pageWash, statePage: pageWash, platformAuto: true, stagePicked: true,
				outputSizeMode: outputPixels, outputWidth: "1200", outputHeight: "960",
				sources: []sourceImage{{Path: "原图.png"}}}
			tt := ui.NewTester(a.view, size[0], size[1])
			tt.Advance(motionUI)
			assertVisible(t, tt, "洗图风格", float32(size[0]), float32(size[1]))
			assertVisible(t, tt, "开始洗图", float32(size[0]), float32(size[1]))
			a.running = true
			tt.Frame()
			_ = tt.Click("洗图风格")
			if a.washStyle != washStyleOriginal || tt.HasText("像素风格") {
				t.Fatalf("style control changed during a run: %q", a.washStyle)
			}
		})
	}
}

func TestWashStyleMenuCanReachLastPresetAndOtherPlatforms(t *testing.T) {
	a := &App{tab: pageWash, statePage: pageWash, platformAuto: true, stagePicked: true}
	tt := ui.NewTester(a.view, 1280, 840)
	if err := tt.Click("洗图风格"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300 * time.Millisecond)
	first := assertVisible(t, tt, "像素风格", 1280, 840)
	tt.Scroll(first.X+first.W/2, first.Y+first.H/2, 0, 500)
	tt.Advance(motionUI)
	if err := tt.Click("复古版画"); err != nil {
		t.Fatal(err)
	}
	if a.washStyle != "复古版画" {
		t.Fatalf("last preset not selected: %q", a.washStyle)
	}
	if err := tt.Click("千问"); err != nil {
		t.Fatal(err)
	}
	assertVisible(t, tt, "洗图风格", 1280, 840)
	if a.washStyle != "复古版画" {
		t.Fatalf("switching platform lost the selected style: %q", a.washStyle)
	}
	if err := tt.Click("自由生图"); err != nil {
		t.Fatal(err)
	}
	if tt.HasText("洗图风格") {
		t.Fatal("wash style control appeared in image generation")
	}
}

func TestWashStyleOverrideExplainedInPromptParts(t *testing.T) {
	a := &App{tab: pageWash, statePage: pageWash, platformAuto: true, washStyle: "像素风格"}
	parts := a.currentPromptParts()
	parts.Style = promptPart{Enabled: true, Value: ""}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("组合参数"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("当前洗图风格预设优先，此项暂不发送") || tt.HasText("风格参数不能为空") {
		t.Fatalf("override was not explained: %v", tt.Texts())
	}
	if err := tt.Click("完成"); err != nil || a.structuredOpen {
		t.Fatalf("unused blank custom style blocked the dialog: %v", err)
	}
	a.washStyle = washStyleOriginal
	if err := tt.Click("组合参数"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("风格参数不能为空") {
		t.Fatal("restored custom style no longer validates")
	}
}
