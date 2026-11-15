package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/tasks"
)

func washPromptBounds(t *testing.T, tt *ui.Tester) ui.Rect {
	t.Helper()
	field, ok := tt.AccessByRole(ui.RoleTextField, "洗图指令")
	if !ok || !field.Multiline || field.Bounds.H <= 0 {
		t.Fatalf("洗图指令输入框不可见: %+v, found=%v", field, ok)
	}
	return field.Bounds
}

func TestWashPromptFillsAvailableHeight(t *testing.T) {
	for _, count := range []int{0, 9} {
		t.Run(fmt.Sprintf("images=%d", count), func(t *testing.T) {
			a := &App{tab: pageWash, statePage: pageWash, prompts: DefaultWashPrompt, platformAuto: true}
			for i := 0; i < count; i++ {
				a.sources = append(a.sources, sourceImage{Path: fmt.Sprintf("/wash/t%d.png", i)})
			}
			tt := ui.NewTester(a.view, 1024, 700)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			small := washPromptBounds(t, tt)
			if small.H < 200 {
				t.Fatalf("提示词输入框没有利用可用空间: %+v", small)
			}

			tt.SetSize(1024, 900)
			large := washPromptBounds(t, tt)
			if delta := large.H - small.H; delta < 198 || delta > 202 {
				t.Fatalf("输入框没有随窗口等量伸缩: small=%+v large=%+v", small, large)
			}
			choice := assertVisible(t, tt, "选择洗图提示词", 1024, 900)
			target := assertVisible(t, tt, "本次发送至：豆包", 1024, 900)
			assertVisible(t, tt, "开始洗图", 1024, 900)
			if large.Y+large.H > choice.Y || choice.Y+choice.H > target.Y {
				t.Fatalf("输入框与底部操作重叠: input=%+v choice=%+v target=%+v", large, choice, target)
			}

			// 长指令应在输入框内滚动，不能把底部操作推出窗口。
			a.prompts = strings.Repeat("保留主体和构图。", 200)
			tt.Frame()
			long := washPromptBounds(t, tt)
			if d := long.H - large.H; d < -1 || d > 1 {
				t.Fatalf("长指令改变了输入框的可用高度: short=%+v long=%+v", large, long)
			}
			assertVisible(t, tt, "选择洗图提示词", 1024, 900)
			assertVisible(t, tt, "开始洗图", 1024, 900)
		})
	}
}

func TestWashPromptResizeKeepsFocusAndShortWindowScroll(t *testing.T) {
	a := &App{tab: pageWash, statePage: pageWash, platformAuto: true, prompts: "修改"}
	tt := ui.NewTester(a.view, 1024, 900)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	field := washPromptBounds(t, tt)
	tt.ClickAt(field.X+10, field.Y+10)
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("第一段")
	for _, h := range []int{400, 600, 900} {
		tt.SetSize(1024, h)
		tt.Advance(200 * time.Millisecond)
		washPromptBounds(t, tt)
		if !tt.Focused("洗图指令") {
			t.Fatalf("窗口高度 %d 时输入框失去焦点", h)
		}
		if h == 400 && a.workInputScroll.MaxY <= 0 {
			t.Fatal("矮窗口的输入区应该可以滚动")
		}
		tt.Type("加")
	}
	if a.prompts != "第一段加加加" {
		t.Fatalf("调整窗口后无法继续输入: %q", a.prompts)
	}
}

func TestWashPromptFitsWithBannerAndOutputOptions(t *testing.T) {
	engine := tasks.NewEngine(nil, nil, nil, "")
	engine.RaiseAttention("需要登录", 1)
	a := &App{
		engine: engine, tab: pageWash, statePage: pageWash, prompts: DefaultWashPrompt, platformAuto: true,
		outputSizeMode: outputPixels, outputWidth: "0", outputHeight: "960", progText: "上次执行失败",
		sources: []sourceImage{{Path: "one.png"}},
	}
	tt := ui.NewTester(a.view, 1024, 560)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	assertVisible(t, tt, "需要登录", 1024, 560)
	assertVisible(t, tt, "开始洗图", 1024, 560)
	if err := tt.Click("选择洗图提示词"); err != nil {
		t.Fatalf("提示词选择器被遮挡: %v", err)
	}
}

func TestWashPromptAndRunControlsShareCard(t *testing.T) {
	for _, size := range [][2]int{{1024, 900}, {1024, 560}, {760, 900}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			a := &App{tab: pageWash, statePage: pageWash, prompts: DefaultWashPrompt, platformAuto: true}
			tt := ui.NewTester(a.view, size[0], size[1])
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			prompt := assertVisible(t, tt, "洗图指令区", float32(size[0]), float32(size[1]))
			run := assertVisible(t, tt, "洗图操作区", float32(size[0]), float32(size[1]))
			if dx, dw, dy := prompt.X-run.X, prompt.W-run.W, prompt.Y+prompt.H-run.Y; dx < -1 || dx > 1 || dw < -1 || dw > 1 || dy < -1 || dy > 1 {
				t.Fatalf("指令和操作区没有贴合: prompt=%+v run=%+v", prompt, run)
			}
			assertVisible(t, tt, "选择洗图提示词", float32(size[0]), float32(size[1]))
			assertVisible(t, tt, "开始洗图", float32(size[0]), float32(size[1]))
		})
	}
}

func TestWashRunControlsStayPinnedWhileScrolling(t *testing.T) {
	a := &App{
		tab: pageWash, statePage: pageWash, platformAuto: true, stagePicked: true,
		outputSizeMode: outputPixels, outputWidth: "1200", outputHeight: "960",
		sources: []sourceImage{{Path: "原图.png"}},
	}
	tt := ui.NewTester(a.view, 1024, 400)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	anchor := assertVisible(t, tt, "1 张", 1024, 400)
	before := assertVisible(t, tt, "洗图操作区", 1024, 400)
	tt.Scroll(anchor.X+anchor.W/2, anchor.Y+anchor.H/2, 0, 500)
	tt.Advance(200 * time.Millisecond)
	if a.workInputScroll.Y <= 0 {
		t.Fatal("矮窗口的输入区无法滚动")
	}
	after := assertVisible(t, tt, "洗图操作区", 1024, 400)
	if dy := before.Y - after.Y; dy < -0.5 || dy > 0.5 {
		t.Fatalf("滚动时底部操作区移动: before=%+v after=%+v", before, after)
	}
	assertVisible(t, tt, "开始洗图", 1024, 400)
}
