package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestPromptPartsComposeValidateAndKeepSelectionsSeparate(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, manager: browser.NewManager(), platformAuto: true, tab: pageHome}
	a.loadSettings()
	a.prompts = "蓝杯\n红杯"
	if got := a.taskOptions([]string{"豆包"}, a.taskPromptList(), nil).StructuredPrompt; got != "" {
		t.Fatalf("unselected options changed prompts: %q", got)
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("组合参数"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("组合提示词参数") || !tt.HasText("组合预览（第 1 条）") {
		t.Fatalf("modal missing: %v", tt.Texts())
	}
	if err := tt.Click("勾选背景"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("背景参数内容"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("浅灰背景")
	if err := tt.Click("勾选风格"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("背景：浅灰背景") || !tt.HasText("风格：写实摄影") {
		t.Fatalf("preview missing selected values: %v", tt.Texts())
	}
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	if a.prompts != "蓝杯\n红杯" {
		t.Fatalf("raw input changed: %q", a.prompts)
	}
	queue := tasks.BuildQueue(a.taskOptions([]string{"豆包"}, a.taskPromptList(), nil), nil)
	if len(queue) != 2 {
		t.Fatalf("parameters changed task count: %+v", queue)
	}
	for i, task := range queue {
		base := []string{"蓝杯", "红杯"}[i]
		if !strings.HasPrefix(task.Prompt, base+"\n") || !strings.Contains(task.Prompt, "背景：浅灰背景") || !strings.Contains(task.Prompt, "风格：写实摄影") {
			t.Fatalf("wrong composed prompt: %q", task.Prompt)
		}
	}
	queue[0].Status = tasks.StatusFailed
	retried := tasks.RebuildRetry(queue[:1])
	if len(retried) != 1 || retried[0].Prompt != queue[0].Prompt || strings.Count(retried[0].Prompt, "背景：浅灰背景") != 1 {
		t.Fatalf("retry did not preserve the original combination: %+v", retried)
	}

	a.tab = pageWash
	tt.Frame()
	if got := a.currentPromptParts().text(); got != "" {
		t.Fatalf("wash inherited gen selection: %q", got)
	}
	if err := tt.Click("组合参数"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("勾选主体"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	if got := a.currentPromptParts().text(); !strings.Contains(got, "主体：") || strings.Contains(got, "浅灰背景") {
		t.Fatalf("wrong wash combination: %q", got)
	}
	a.saveSettings()
	again := &App{store: st, manager: browser.NewManager()}
	again.loadSettings()
	if got := again.promptPartsFor(pageHome).text(); !strings.Contains(got, "背景：浅灰背景") || !strings.Contains(got, "风格：写实摄影") {
		t.Fatalf("gen selection not restored: %q", got)
	}
	if got := again.promptPartsFor(pageWash).text(); !strings.Contains(got, "主体：") || strings.Contains(got, "浅灰背景") {
		t.Fatalf("wash selection not restored independently: %q", got)
	}
}

func TestEmptySelectedPromptPartStopsBeforeTaskStarts(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), platformAuto: true}
	parts := a.currentPromptParts()
	parts.Other.Enabled = true
	parts.Other.Value = "  "
	a.structuredOpen = true
	tt := ui.NewTester(a.view, 1280, 900)
	if !tt.HasText("其他参数不能为空") {
		t.Fatalf("missing field validation: %v", tt.Texts())
	}
	if err := tt.Click("完成"); err != nil {
		t.Fatal(err)
	}
	if !a.structuredOpen {
		t.Fatal("invalid parameter closed the editor")
	}
	a.structuredOpen = false
	a.runQueue([]string{"豆包"}, []string{"杯子"}, nil, nil)
	if a.running || !strings.Contains(a.progText, "请填写已勾选的其他参数") {
		t.Fatalf("empty selected parameter was not rejected: running=%v note=%q", a.running, a.progText)
	}
}

func TestPromptPartsCanFormPromptWithoutFreeText(t *testing.T) {
	a := &App{platformAuto: true}
	parts := a.currentPromptParts()
	parts.Subject.Enabled = true
	parts.Subject.Value = "蓝色水杯"
	parts.Background.Enabled = true
	if !a.workbenchInput().HasPrompts {
		t.Fatal("selected parameters did not count as a prompt")
	}
	queue := tasks.BuildQueue(a.taskOptions([]string{"豆包"}, a.taskPromptList(), nil), nil)
	if len(queue) != 1 || !strings.HasPrefix(queue[0].Prompt, "主体：蓝色水杯") || !strings.Contains(queue[0].Prompt, "背景：纯白背景") {
		t.Fatalf("parameters did not form one prompt: %+v", queue)
	}
}

func TestPromptPartsModalFitsCompactWindow(t *testing.T) {
	for _, size := range [][2]int{{760, 720}, {1024, 700}, {1280, 900}} {
		a := &App{platformAuto: true, prompts: "商品"}
		tt := ui.NewTester(a.view, size[0], size[1])
		if err := tt.Click("组合参数"); err != nil {
			t.Fatalf("%v: %v", size, err)
		}
		assertVisible(t, tt, "勾选主体", float32(size[0]), float32(size[1]))
		assertVisible(t, tt, "完成", float32(size[0]), float32(size[1]))
	}
}
