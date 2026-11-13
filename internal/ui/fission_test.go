package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestFissionModeKeepsOneMotherAndIndependentHistory(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	first, second := filepath.Join(dir, "first.png"), filepath.Join(dir, "second.png")
	writeTinyPNG(t, first)
	writeTinyPNG(t, second)
	a := &App{store: st, platformAuto: true}
	a.loadSettings()
	a.prompts = "生图草稿"
	if err := a.persistWorkspace(pageHome); !err {
		t.Fatal("保存生图草稿失败")
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("裂变图片"); err != nil {
		t.Fatal(err)
	}
	if a.statePage != pageFission || a.prompts != DefaultFissionPrompt || !tt.HasText("裂变指令") || !tt.HasText("开始裂变") {
		t.Fatalf("裂变页面不正确: page=%d prompt=%q", a.statePage, a.prompts)
	}
	a.addLoaded(pageFission, loadSourceFiles([]string{first, second}))
	if len(a.sources) != 1 || a.sources[0].Path != first || len(a.taskPromptList()) != 4 {
		t.Fatalf("母图或四个方向不正确: %+v", a.sources)
	}
	a.prompts = "保留母图的花纹"
	a.saveSettings()
	if err := tt.Click("批量洗图"); err != nil {
		t.Fatal(err)
	}
	a.prompts = "洗图草稿"
	if err := tt.Click("自由生图"); err != nil {
		t.Fatal(err)
	}
	if a.prompts != "生图草稿" {
		t.Fatalf("切页丢失生图草稿: %q", a.prompts)
	}
	if err := tt.Click("裂变图片"); err != nil {
		t.Fatal(err)
	}
	if a.prompts != "保留母图的花纹" || len(a.sources) != 1 || a.sources[0].Path != first {
		t.Fatalf("切页丢失裂变草稿: %q %+v", a.prompts, a.sources)
	}
	a.newWorkspace()
	history, err := st.History(store.PageFission)
	if err != nil || len(history) != 1 || a.prompts != DefaultFissionPrompt || len(a.sources) != 0 {
		t.Fatalf("裂变历史未独立保存: %+v, %v", history, err)
	}
	a.reopenSession(history[0].ID)
	if a.prompts != "保留母图的花纹" || len(a.sources) != 1 || a.sources[0].Path != first {
		t.Fatal("裂变历史恢复失败")
	}
	a.saveSettings()
	restored := &App{store: st}
	restored.loadSettings()
	if restored.tab != pageFission || restored.prompts != a.prompts || len(restored.sources) != 1 || restored.sources[0].Path != first {
		t.Fatalf("重启后未恢复裂变工作区: %+v", restored.stateOf(pageFission))
	}
	if restored.stateOf(pageHome).prompts != "生图草稿" || restored.stateOf(pageWash).prompts != "洗图草稿" {
		t.Fatal("其他页面草稿被裂变覆盖")
	}
	restored.queue = []tasks.Task{{Prompt: "旧结果", Status: tasks.StatusSuccess}}
	restored.addLoaded(pageFission, loadSourceFiles([]string{second}))
	if len(restored.sources) != 1 || restored.sources[0].Path != second || len(restored.queue) != 0 {
		t.Fatal("替换母图后仍保留旧母图的任务")
	}
	history, err = st.History(store.PageFission)
	if err != nil || len(history) != 1 {
		t.Fatalf("替换母图未保存旧任务历史: %+v, %v", history, err)
	}
}

func TestFissionRetryPreservesOtherDirections(t *testing.T) {
	base := []tasks.Task{
		{Index: 1, Platform: "豆包", Prompt: "方向一", Status: tasks.StatusSuccess, Results: []string{"one.png"}},
		{Index: 2, Platform: "豆包", Prompt: "方向二", Status: tasks.StatusNeedsReview, Error: "裂变相似度过高", Results: []string{"old.png"}},
		{Index: 3, Platform: "豆包", Prompt: "方向三", Status: tasks.StatusSuccess, Results: []string{"three.png"}},
	}
	retry := freshFissionRetries(base[1:2])
	if len(retry) != 1 || len(retry[0].Results) != 0 || retry[0].Prompt != "方向二" {
		t.Fatalf("需要重新生成的任务不正确: %+v", retry)
	}
	got := mergeFissionRetry(base, []tasks.Task{{Index: 1, Platform: "豆包", Prompt: "方向二", Status: tasks.StatusSuccess, Results: []string{"new.png"}}})
	if len(got) != 3 || got[0].Results[0] != "one.png" || got[1].Index != 2 || got[1].Results[0] != "new.png" || got[2].Results[0] != "three.png" {
		t.Fatalf("重试破坏同组结果: %+v", got)
	}
}

func TestFissionRetryRequiresMatchingMother(t *testing.T) {
	a := &App{statePage: pageFission, engine: tasks.NewEngine(nil, nil, nil, "")}
	item := tasks.Task{Platform: "豆包", Prompt: "方向一", Images: []string{"mother.png"}, Status: tasks.StatusFailed}
	a.retryTasks([]tasks.Task{item})
	if a.progText != "请先添加该结果对应的母图" || a.running {
		t.Fatal("没有母图时不能重试")
	}
	a.sources = []sourceImage{{Path: "another.png"}}
	a.retryTasks([]tasks.Task{item})
	if a.progText != "请先添加该结果对应的母图" || a.running {
		t.Fatal("母图不同时不能重试")
	}
}

func TestFissionModeLayout(t *testing.T) {
	for _, width := range []int{760, 810, 1280} {
		a := &App{tab: pageFission, statePage: pageFission, prompts: DefaultFissionPrompt}
		tt := ui.NewTester(a.view, width, 900)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		tt.Advance(200 * time.Millisecond)
		wash := assertVisible(t, tt, "批量洗图", float32(width), 900)
		fission := assertVisible(t, tt, "裂变图片", float32(width), 900)
		history := assertVisible(t, tt, "历史", float32(width), 900)
		if wash.X+wash.W > fission.X+0.01 || fission.X+fission.W > history.X+0.01 {
			t.Fatalf("宽度 %d 的模式标签重叠: %+v %+v %+v", width, wash, fission, history)
		}
		assertVisible(t, tt, "母图输入区", float32(width), 900)
		assertVisible(t, tt, "裂变指令", float32(width), 900)
		assertVisible(t, tt, "开始裂变", float32(width), 900)
		if err := tt.Click("批量洗图"); err != nil {
			t.Fatal(err)
		}
		tt.Key(0, ui.KeyRight)
		if a.tab != pageFission || !tt.HasText("裂变指令") {
			t.Fatalf("宽度 %d 时不能用键盘进入裂变模式", width)
		}
		if path := os.Getenv("TULAI_FISSION_SHOT"); path != "" && width == 1280 {
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
