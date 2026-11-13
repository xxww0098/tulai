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
				queue: []tasks.Task{{
					Index: 1, Platform: "豆包", Status: tasks.StatusSuccess,
					Prompt: "一只猫", Results: []string{"结果.png"},
				}},
			}
			tt := ui.NewTester(a.view, 1024, 700)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			tt.Advance(200 * time.Millisecond)
			pane := assertVisible(t, tt, "出图结果区", 1024, 700)
			if pane.W < 620 || pane.H < 390 {
				t.Fatalf("结果区没有获得足够空间: %+v", pane)
			}
			shot := assertVisible(t, tt, "结果图", 1024, 700)
			if shot.W < 110 || shot.H < 110 || shot.X+shot.W > pane.X+pane.W {
				t.Fatalf("结果缩略图太小或超出结果区: shot=%+v pane=%+v", shot, pane)
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
