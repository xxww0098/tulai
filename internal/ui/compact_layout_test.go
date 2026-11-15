package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestCompactWorkspaceGivesResultsMoreRoom(t *testing.T) {
	for _, page := range []int{pageHome, pageWash} {
		t.Run(pageLabel(page), func(t *testing.T) {
			a := &App{
				tab: page, statePage: page, platformAuto: true,
				browserRows: []store.Browser{loggedLane(1, "主力", "豆包"), loggedLane(2, "备用", "豆包")},
				queue: []tasks.Task{
					{
						Index: 1, Platform: "豆包", Status: tasks.StatusRunning,
						Prompt: "一只蓝猫",
					},
					{
						Index: 2, Platform: "豆包", Status: tasks.StatusSuccess,
						Prompt: "一只猫", Results: []string{"结果.png"},
					},
				},
			}
			tt := ui.NewTester(a.view, 1024, 700)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			tt.Advance(200 * time.Millisecond)
			pane := assertVisible(t, tt, "出图结果区", 1024, 700)
			if pane.W < 620 || pane.H < 390 {
				t.Fatalf("结果区没有获得足够空间: %+v", pane)
			}
			shot := assertVisible(t, tt, "结果图", 1024, 700)
			if shot.W < 60 || shot.W > 80 || shot.H < 60 || shot.H > 80 || shot.X+shot.W > pane.X+pane.W {
				t.Fatalf("结果缩略图尺寸不符合紧凑网格或超出结果区: shot=%+v pane=%+v", shot, pane)
			}
			doneBtn := assertVisible(t, tt, "已完成 1", 1024, 700)
			if doneBtn.X+doneBtn.W > pane.X+pane.W {
				t.Fatalf("已完成按钮超出结果区: btn=%+v pane=%+v", doneBtn, pane)
			}
		})
	}
}

func TestReferenceActionsFitCompactInput(t *testing.T) {
	for _, width := range []int{810, 900, 1024, 760} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := &App{sources: []sourceImage{{Path: "一.png"}, {Path: "二.png"}}}
			tt := ui.NewTester(a.view, width, 900)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			tt.Advance(200 * time.Millisecond)
			card := assertVisible(t, tt, "参考图输入区", float32(width), 900)
			var prev ui.Rect
			for i, label := range []string{"图库", "添加图片", "清空"} {
				r := assertVisible(t, tt, label, float32(width), 900)
				if r.X < card.X || r.X+r.W > card.X+card.W || r.Y < card.Y || r.Y+r.H > card.Y+card.H || (i > 0 && prev.X+prev.W > r.X) {
					t.Fatalf("%s 按钮超出输入区或与其他按钮重叠: button=%+v card=%+v previous=%+v", label, r, card, prev)
				}
				prev = r
			}
			if err := tt.Click("清空"); err != nil || len(a.sources) != 0 {
				t.Fatalf("清空参考图失败: err=%v sources=%v", err, a.sources)
			}
		})
	}
}

func TestCompactWorkspaceHeaderFitsAtSplitThreshold(t *testing.T) {
	for _, width := range []int{809, 810, 820, 900} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt := ui.NewTester((&App{}).view, width, 700)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			provider := assertVisible(t, tt, "ChatGPT", float32(width), 700)
			history := assertVisible(t, tt, "历史", float32(width), 700)
			newTask := assertVisible(t, tt, "新建任务", float32(width), 700)
			if history.X+history.W > newTask.X || (provider.Y < history.Y+history.H && provider.Y+provider.H > history.Y && provider.X+provider.W > history.X) {
				t.Fatalf("工作区标题操作重叠: provider=%+v history=%+v new=%+v", provider, history, newTask)
			}
		})
	}
}

func TestWorkbenchDistillsSpaceForPromptsAndResults(t *testing.T) {
	a := &App{
		tab: pageHome, statePage: pageHome, platformAuto: true,
		prompts: "第一行提示词",
		browserRows: []store.Browser{
			loggedLane(1, "豆包主力", "豆包"),
			loggedLane(2, "豆包备用", "豆包"),
			loggedLane(3, "豆包三号", "豆包"),
		},
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Advance(200 * time.Millisecond)

	promptField, ok := tt.AccessByRole(ui.RoleTextField, "提示词内容")
	if !ok || !promptField.Multiline || promptField.Bounds.H < 320 {
		t.Fatalf("生图提示词输入框没有撑满左栏剩余高度: %+v, ok=%v", promptField, ok)
	}

	resultsBefore := assertVisible(t, tt, "出图结果区", 1280, 800)
	assertVisible(t, tt, "账号列表", 1280, 800)
	if err := tt.Click("收起账号"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(150 * time.Millisecond)
	if _, visible := tt.Find("账号列表"); visible {
		t.Fatal("收起账号后不应继续显示账号列表")
	}
	resultsAfter := assertVisible(t, tt, "出图结果区", 1280, 800)
	if resultsAfter.H-resultsBefore.H < 90 {
		t.Fatalf("收起账号后结果区高度没有显著增加: before=%+v after=%+v", resultsBefore, resultsAfter)
	}
	if err := tt.Click("展开账号"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(150 * time.Millisecond)
	assertVisible(t, tt, "账号列表", 1280, 800)

	if err := tt.Click("裂变图片"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(150 * time.Millisecond)
	fissionField, ok := tt.AccessByRole(ui.RoleTextField, "裂变指令")
	if !ok || !fissionField.Multiline || fissionField.Bounds.H < 240 {
		t.Fatalf("裂变指令输入框没有撑满左栏剩余高度: %+v, ok=%v", fissionField, ok)
	}
}
