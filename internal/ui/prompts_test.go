package ui

import (
	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptLibraryKeepsBuiltinAndUserCopies(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st}
	a.loadSettings()
	wash, ok := prompts.Builtin(prompts.IDWash)
	if !ok || wash.Body != DefaultWashPrompt {
		t.Fatal("the default wash prompt is missing from the library")
	}
	a.promptEditChoice = "数字油画"
	a.applyPromptChoice()
	oil, _ := prompts.Builtin(prompts.IDOil)
	if !prompts.BuiltinUnchanged(prompts.IDOil, a.promptEditText) || a.prompts != "" {
		t.Fatal("editing a builtin should not change the workspace")
	}
	a.promptEditText = oil.Body + "\n用户加了一句"
	a.saveCurrentPrompt()
	if a.promptEditPick == prompts.IDOil || len(a.userPrompts) != 1 || !strings.Contains(a.userPrompts[0].Body, "用户加了一句") {
		t.Fatalf("copy not saved: %+v", a.userPrompts)
	}
	a.saveSettings()

	again := &App{store: st}
	again.loadSettings()
	if len(again.userPrompts) != 1 || !strings.Contains(again.userPrompts[0].Body, "用户加了一句") {
		t.Fatalf("copy not restored: %+v", again.userPrompts)
	}
	again.promptEditChoice = "数字油画"
	again.applyPromptChoice()
	again.promptEditText += "\n再改"
	again.promptEditChoice = newPromptLabel
	again.applyPromptChoice()
	if !strings.Contains(prompts.FormatUser(again.userPrompts), "再改") {
		t.Fatal("leaving a dirty builtin did not keep a copy")
	}
	canon, _ := prompts.Builtin(prompts.IDOil)
	if strings.Contains(canon.Body, "再改") || strings.Contains(canon.Body, "用户加了一句") {
		t.Fatal("builtin body changed")
	}
}

func TestPromptLibrarySelectionAcrossWorkspaces(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, engine: tasks.NewEngine(st, nil, nil, ""), platformAuto: true}
	a.loadSettings()
	a.prompts = "原有生图内容"
	a.tab = pagePrompts
	tt := ui.NewTester(a.view, 1280, 840)
	if err := tt.Click("提示词内容"); err != nil {
		t.Fatal(err)
	}
	const body = "商品白底\n保留主体细节"
	tt.Type(body)
	if err := tt.Click("保存提示词"); err != nil {
		t.Fatal(err)
	}
	if len(a.userPrompts) != 1 || a.prompts != "原有生图内容" {
		t.Fatalf("saving touched workspace: prompts=%q library=%+v", a.prompts, a.userPrompts)
	}
	label := a.userPrompts[0].Label
	a.tab = pageHome
	tt.Frame()
	tt.Advance(motionUI)
	assertVisible(t, tt, "选择生图提示词", 1280, 840)
	if err := tt.Click(choosePromptLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	got := a.taskPromptList()
	if len(got) != 1 || got[0] != body {
		t.Fatalf("gen template must be sent whole: %#v", got)
	}
	queue := tasks.BuildQueue(a.taskOptions([]string{"豆包"}, got, nil), nil)
	if len(queue) != 1 || queue[0].Prompt != body {
		t.Fatalf("queue: %+v", queue)
	}
	a.prompts += "\n生图临时修改"
	if err := tt.Click(choosePromptLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if a.prompts != body {
		t.Fatal("selecting the same gen template again did not reload it")
	}
	a.prompts += "\n生图临时修改"
	a.tab = pageWash
	tt.Frame()
	tt.Advance(motionUI)
	assertVisible(t, tt, "选择洗图提示词", 1280, 840)
	if err := tt.Click(choosePromptLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("默认洗图"); err != nil {
		t.Fatal(err)
	}
	if a.prompts != DefaultWashPrompt {
		t.Fatal("the default wash template is not selectable")
	}
	if err := tt.Click(choosePromptLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if got = a.taskPromptList(); len(got) != 1 || got[0] != body {
		t.Fatalf("wash template: %#v", got)
	}
	a.prompts += "\n洗图临时修改"
	if err := tt.Click(choosePromptLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if a.prompts != body {
		t.Fatal("selecting the same wash template again did not reload it")
	}
	a.prompts += "\n洗图临时修改"
	if a.userPrompts[0].Body != body || a.stateOf(pageHome).prompts != body+"\n生图临时修改" {
		t.Fatal("workspace edits changed another workspace or the library")
	}

	a.tab = pagePrompts
	tt.Frame()
	a.promptEditText = body + "\n模板更新"
	a.saveCurrentPrompt()
	if a.prompts != body+"\n生图临时修改" || a.stateOf(pageWash).prompts != body+"\n洗图临时修改" {
		t.Fatal("editing the library changed an existing workspace")
	}
	a.tab = pageHome
	tt.Frame()
	if err := tt.Click(choosePromptLabel); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	if a.prompts != body+"\n模板更新" {
		t.Fatal("selecting the updated template did not load the latest text")
	}
	a.tab = pagePrompts
	tt.Frame()
	a.deleteUserPrompt()
	if a.promptPick != detachedPromptID || len(a.taskPromptList()) != 1 {
		t.Fatal("deleting a selected template changed how the current prompt is sent")
	}
	a.saveSettings()

	again := &App{store: st}
	again.loadSettings()
	if len(again.userPrompts) != 0 || again.promptPick != detachedPromptID || len(again.taskPromptList()) != 1 {
		t.Fatalf("reload lost detached prompt: pick=%q prompts=%q library=%+v", again.promptPick, again.prompts, again.userPrompts)
	}
	if again.stateOf(pageWash).prompts != body+"\n洗图临时修改" {
		t.Fatal("reload lost wash workspace")
	}
}

func TestPromptSelectorsStayVisibleInCompactWindows(t *testing.T) {
	for _, size := range [][2]float32{{1024, 700}, {760, 900}} {
		a := &App{platformAuto: true, promptPick: prompts.IDArt, promptChoice: "数字画", prompts: "一只猫"}
		tt := ui.NewTester(a.view, int(size[0]), int(size[1]))
		for _, label := range []string{"选择生图提示词", "按行发送"} {
			assertVisible(t, tt, label, size[0], size[1])
		}
		a.tab = pageWash
		tt.Frame()
		tt.Advance(motionUI)
		assertVisible(t, tt, "选择洗图提示词", size[0], size[1])
	}
}

func TestPromptTypesCanBeEditedAndFilterBothWorkspaces(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("user_prompts", `[{"id":"u1","label":"旧模板","body":"旧正文"},{"id":"u2","label":"仅生图模板","body":"生图正文","kind":"gen"},{"id":"u3","label":"仅洗图模板","body":"洗图正文","kind":"wash"}]`)
	a := &App{store: st, platformAuto: true}
	a.loadSettings()
	for _, kind := range []string{prompts.KindGen, prompts.KindWash} {
		if !containsStr(promptLabels(workspacePromptOptions(kind, a.userPrompts)), "旧模板") {
			t.Fatal("legacy prompt must remain available to both workspaces")
		}
	}
	a.tab = pagePrompts
	tt := ui.NewTester(a.view, 1280, 840)
	for _, label := range []string{"名称", "类型", "来源", "编辑 旧模板", "编辑 仅生图模板", "编辑 仅洗图模板"} {
		assertVisible(t, tt, label, 1280, 840)
	}
	if err := tt.Click("筛选洗图"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("编辑 仅生图模板"); ok {
		t.Fatal("wash filter included a gen template")
	}
	if _, ok := tt.Find("编辑 仅洗图模板"); !ok {
		t.Fatal("wash filter hid its wash template")
	}
	if err := tt.Click("筛选全部"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("编辑 旧模板"); err != nil {
		t.Fatal(err)
	}
	if a.promptEditKind != "通用" {
		t.Fatalf("legacy prompt kind = %q", a.promptEditKind)
	}
	if err := tt.Click("提示词类型"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyEnter)
	if a.promptEditKind != "洗图" {
		t.Fatalf("type selection = %q", a.promptEditKind)
	}
	if err := tt.Click("提示词名称"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("已分类旧模板")
	if err := tt.Click("保存提示词"); err != nil {
		t.Fatal(err)
	}
	if a.userPrompts[0].Kind != prompts.KindWash || a.userPrompts[0].Label != "已分类旧模板" {
		t.Fatalf("updated prompt: %+v", a.userPrompts[0])
	}
	again := &App{store: st}
	again.loadSettings()
	if again.userPrompts[0].Kind != prompts.KindWash || again.userPrompts[0].Label != "已分类旧模板" {
		t.Fatalf("updated type did not persist: %+v", again.userPrompts[0])
	}
	if err := tt.Click("筛选生图"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("新建提示词"); err != nil {
		t.Fatal(err)
	}
	if a.promptEditKind != "生图" || a.promptEditText != "" {
		t.Fatalf("new prompt did not inherit the filter: kind=%q body=%q", a.promptEditKind, a.promptEditText)
	}
	if err := tt.Click("提示词内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("晨光森林")
	if err := tt.Click("保存提示词"); err != nil {
		t.Fatal(err)
	}
	if got := a.userPrompts[len(a.userPrompts)-1]; got.Kind != prompts.KindGen || got.Label != "晨光森林" {
		t.Fatalf("new gen prompt: %+v", got)
	}
	a.tab = pageHome
	tt.Frame()
	tt.Advance(motionUI)
	if err := tt.Click(choosePromptLabel); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("晨光森林"); !ok {
		t.Fatal("gen dropdown did not show the gen prompt")
	}
	if _, ok := tt.Find("仅洗图模板"); ok {
		t.Fatal("gen dropdown showed the wash prompt")
	}
	tt.Key(0, ui.KeyEscape)
	a.tab = pageWash
	tt.Frame()
	tt.Advance(motionUI)
	if err := tt.Click(choosePromptLabel); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("仅洗图模板"); !ok {
		t.Fatal("wash dropdown did not show the wash prompt")
	}
	if _, ok := tt.Find("晨光森林"); ok {
		t.Fatal("wash dropdown showed the gen prompt")
	}
	gen := workspacePromptOptions(prompts.KindGen, again.userPrompts)
	wash := workspacePromptOptions(prompts.KindWash, again.userPrompts)
	for _, check := range []struct {
		options []prompts.Template
		label   string
		want    bool
	}{
		{gen, "仅生图模板", true}, {gen, "仅洗图模板", false}, {gen, "已分类旧模板", false},
		{wash, "仅生图模板", false}, {wash, "仅洗图模板", true}, {wash, "已分类旧模板", true},
	} {
		if got := containsStr(promptLabels(check.options), check.label); got != check.want {
			t.Fatalf("options for %q: got %v, want %v", check.label, got, check.want)
		}
	}
}

func TestBuiltinTypeChangeSavesACopy(t *testing.T) {
	a := &App{promptEditChoice: "默认洗图"}
	a.applyPromptChoice()
	a.promptEditKind = "生图"
	a.saveCurrentPrompt()
	if len(a.userPrompts) != 1 || a.userPrompts[0].Kind != prompts.KindGen || a.userPrompts[0].From != prompts.IDWash {
		t.Fatalf("builtin copy: %+v", a.userPrompts)
	}
	wash, _ := prompts.Builtin(prompts.IDWash)
	if wash.Kind != prompts.KindWash {
		t.Fatal("builtin type changed")
	}
}

func TestPromptOptionsDisambiguateReservedLabels(t *testing.T) {
	users := []prompts.UserPrompt{
		{ID: "u1", Label: newPromptLabel, Body: "一"},
		{ID: "u2", Label: choosePromptLabel, Body: "二"},
		{ID: "u3", Label: detachedPromptLabel, Body: "三"},
	}
	for _, first := range []string{newPromptLabel, choosePromptLabel, prompts.LabelCustom} {
		options := promptOptions(first, users, prompts.Template{ID: detachedPromptID, Label: detachedPromptLabel})
		for _, option := range options {
			if got := promptOptionID(option.Label, options); got != option.ID {
				t.Fatalf("%q selects %q, want %q", option.Label, got, option.ID)
			}
		}
	}
}
