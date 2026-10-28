package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestBuiltinPromptSendsAsOneAndSurvivesRestart(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st}
	a.loadSettings()
	if a.promptChoice != prompts.LabelCustom || len(a.taskPromptList()) != 0 {
		t.Fatalf("fresh pick=%q prompts=%q", a.promptChoice, a.prompts)
	}
	a.promptChoice = "数字油画"
	a.applyPromptChoice()
	got := a.taskPromptList()
	oil, _ := prompts.Builtin(prompts.IDOil)
	if len(got) != 1 || got[0] != strings.TrimSpace(oil.Body) || !strings.Contains(got[0], "反向提示词：") {
		t.Fatalf("sent prompts: %#v", got)
	}
	if strings.Count(got[0], "\n") == 0 {
		t.Fatal("reverse prompt was split off the main prompt")
	}
	queue := tasks.BuildQueue(a.taskOptions([]string{"豆包"}, got, nil), nil)
	if len(queue) != 1 || !strings.Contains(queue[0].Prompt, "反向提示词：") {
		t.Fatalf("queue: %+v", queue)
	}

	a.prompts = oil.Body + "\n用户加了一句"
	a.saveCurrentPrompt()
	if a.promptPick == prompts.IDOil || !strings.Contains(a.prompts, "用户加了一句") {
		t.Fatalf("save did not leave the builtin: pick=%s", a.promptPick)
	}
	if body, _ := prompts.Body(prompts.IDOil, nil); strings.Contains(body, "用户加了一句") {
		t.Fatal("builtin text changed")
	}
	a.saveSettings()

	again := &App{store: st}
	again.loadSettings()
	restored := again.taskPromptList()
	if again.promptPick == "" || again.promptPick == prompts.IDOil || len(restored) != 1 || !strings.Contains(restored[0], "用户加了一句") {
		t.Fatalf("reload pick=%s prompts=%q", again.promptPick, restored)
	}
	again.restoreBuiltinPrompt()
	if again.promptNote == "已恢复内置提示词" || !strings.Contains(again.prompts, "用户加了一句") {
		t.Fatal("restore on a user copy should no-op")
	}
	again.promptChoice = "数字油画"
	again.applyPromptChoice()
	if !prompts.BuiltinUnchanged(prompts.IDOil, again.prompts) {
		t.Fatal("selecting the builtin did not restore its text")
	}
	again.prompts += "再改"
	again.promptChoice = prompts.LabelCustom
	again.applyPromptChoice()
	if !strings.Contains(prompts.FormatUser(again.userPrompts), "再改") {
		t.Fatal("leaving a dirty builtin did not keep a user copy")
	}
	canon, _ := prompts.Builtin(prompts.IDOil)
	if strings.Contains(canon.Body, "再改") || strings.Contains(canon.Body, "用户加了一句") {
		t.Fatal("builtin body drifted")
	}
}
