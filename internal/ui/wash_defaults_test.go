package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestDoubaoWashDefaultFromDraftToTask(t *testing.T) {
	for _, draft := range []string{"", DefaultWashPrompt, prompts.WindowReviewDoubaoWashPrompt, prompts.FullBleedDoubaoWashPrompt, "只把背景换成蓝色"} {
		t.Run(draft, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			st.Set("wash_prompt", draft)
			st.Set("workspace_page", store.PageWash)
			a := NewApp(st, nil, nil, "")
			want := prompts.DoubaoWashPrompt
			if draft == "只把背景换成蓝色" {
				want = draft
			}
			if got := a.washPrompt(); got != want {
				t.Fatalf("restored draft = %q, want %q", got, want)
			}
			list := a.taskPromptList()
			if len(list) != 1 || list[0] != want {
				t.Fatalf("wash instruction was changed or split before dispatch: %v", list)
			}
			SetTestApp(a)
			defer SetTestApp(nil)
			setWorkbenchFieldBridge("resetWashPrompt", "")
			if a.prompts != prompts.DoubaoWashPrompt {
				t.Fatal("default button did not restore pure 2D artwork instructions")
			}
		})
	}
}

func TestWashPlatformSwitchPreservesCustomInstructions(t *testing.T) {
	a := &App{statePage: pageWash, tab: pageWash, platDoubao: true, prompts: prompts.DoubaoWashPrompt}
	a.setSinglePlatform("千问")
	if a.prompts != DefaultWashPrompt {
		t.Fatal("another provider lost its general redraw default")
	}
	a.setSinglePlatform("豆包")
	if a.prompts != prompts.DoubaoWashPrompt {
		t.Fatal("Doubao did not select its artwork extraction default")
	}
	a.prompts = "保留画框，按用户自己的要求处理"
	a.setSinglePlatform("千问")
	a.setSinglePlatform("豆包")
	if a.prompts != "保留画框，按用户自己的要求处理" {
		t.Fatal("switching providers overwrote a custom instruction")
	}
	// The wash draft must also follow defaults while another page is open.
	a.statePage, a.tab = pageHome, pageHome
	a.parked = map[int]pageState{pageWash: {prompts: prompts.DoubaoWashPrompt}}
	a.setSinglePlatform("千问")
	if a.stateOf(pageWash).prompts != DefaultWashPrompt {
		t.Fatal("parked wash default was not updated")
	}
}

func TestWorkbenchOnlyShowsActiveCooldownError(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := st.AddBrowser("豆包", "测试", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(b.ID, false, true)
	a := NewApp(st, nil, nil, "")
	SetTestApp(a)
	defer SetTestApp(nil)
	for _, seconds := range []int64{-60, 60, 0} {
		until := int64(0)
		if seconds != 0 {
			until = time.Now().Unix() + seconds
		}
		st.UpdateBrowserHealth(b.ID, store.HealthExhausted, until, "账号额度已用尽")
		a.refreshBrowsers()
		lanes := getWorkbenchStateBridge().Lanes
		if len(lanes) != 1 {
			t.Fatalf("lanes = %d", len(lanes))
		}
		if got := lanes[0].LastError != ""; got != (seconds >= 0) {
			t.Fatalf("cooldown offset %d: displayed error = %q", seconds, lanes[0].LastError)
		}
	}
}

func TestFissionTaskPromptListBuildsFourDirectionalQueue(t *testing.T) {
	a := &App{statePage: pageFission, tab: pageFission, platDoubao: true, prompts: "保留核心主体花纹"}
	list := a.taskPromptList()
	if len(list) != 4 {
		t.Fatalf("fission taskPromptList len = %d, want 4", len(list))
	}
	opts := a.taskOptions([]string{"豆包"}, list, []string{"mother.png"})
	queue := tasks.BuildQueue(opts, map[string][]int64{"豆包": {1}})
	if len(queue) != 4 {
		t.Fatalf("fission BuildQueue len = %d, want 4", len(queue))
	}
}

func TestWorkbenchPlanStartGuardsAndTextOnlyFission(t *testing.T) {
	rows := []store.Browser{
		{ID: 1, Platform: "豆包", Name: "1", IsLoggedIn: true, Health: store.HealthReady},
		{ID: 2, Platform: "千问", Name: "1", IsLoggedIn: true, Health: store.HealthReady},
	}

	// Wash requires at least 1 pending source image
	a := &App{statePage: pageWash, tab: pageWash, platDoubao: true, prompts: prompts.DoubaoWashPrompt}
	if plan := a.workbenchPlan(rows); plan.Action != StartNone {
		t.Fatalf("wash with 0 images should not be startable: %+v", plan)
	}
	a.sources = []sourceImage{{Path: "a.png"}}
	if plan := a.workbenchPlan(rows); plan.Action != StartGenerate {
		t.Fatalf("wash with 1 pending image should be startable: %+v", plan)
	}

	// Free mode image-to-image requires images; text-to-image with blank promptPick cannot start
	a = &App{statePage: pageHome, tab: pageHome, batchMode: batchImage, platDoubao: true, prompts: "油画"}
	if plan := a.workbenchPlan(rows); plan.Action != StartNone {
		t.Fatalf("free batchImage with 0 images should not be startable: %+v", plan)
	}
	a = &App{statePage: pageHome, tab: pageHome, batchMode: batchText, platDoubao: true, promptPick: "u1", prompts: "   "}
	if got := a.taskPromptList(); len(got) != 0 {
		t.Fatalf("blank prompt with promptPick should yield empty list, got %v", got)
	}
	if plan := a.workbenchPlan(rows); plan.Action != StartNone {
		t.Fatalf("free batchText with blank prompt should not be startable: %+v", plan)
	}

	// Fission with !skipImages requires 1 mother image; with skipImages requires real text theme/directions
	a = &App{statePage: pageFission, tab: pageFission, platQianwen: true, prompts: DefaultFissionPrompt}
	if plan := a.workbenchPlan(rows); plan.Action != StartNone {
		t.Fatalf("fission with 0 images and skipImages=false should not be startable: %+v", plan)
	}
	a.skipImages = true
	if plan := a.workbenchPlan(rows); plan.Action != StartNone {
		t.Fatalf("fission with skipImages=true and mother-image default prompt should not be startable: %+v", plan)
	}
	a.prompts = "太空猫水彩画"
	if plan := a.workbenchPlan(rows); plan.Action != StartGenerate {
		t.Fatalf("fission with skipImages=true and custom theme should be startable: %+v", plan)
	}
	list := a.taskPromptList()
	opts := a.taskOptions([]string{"千问"}, list, nil)
	if !opts.Fission || !opts.FissionTextOnly {
		t.Fatalf("expected FissionTextOnly=true, got %+v", opts)
	}
	if queue := tasks.BuildQueue(opts, map[string][]int64{"千问": {2}}); len(queue) != 4 || len(queue[0].Images) != 0 {
		t.Fatalf("text-only fission queue invalid: %+v", queue)
	}
}

func TestResultTileDTOPropagatesQualityNote(t *testing.T) {
	a := &App{
		statePage: pageWash,
		tab:       pageWash,
		platDoubao: true,
		queue: []tasks.Task{
			{
				Index:       1,
				Platform:    "千问",
				Status:      tasks.StatusSuccess,
				Results:     []string{"/tmp/out.png"},
				QualityNote: "边缘疑似留白/黑边(left/right)",
			},
		},
	}
	wb := a.buildWorkbenchState()
	if len(wb.DoneTiles) != 1 || wb.DoneTiles[0].QualityNote != "边缘疑似留白/黑边(left/right)" {
		t.Fatalf("ResultTileDTO QualityNote mismatch: %+v", wb.DoneTiles)
	}
}

func TestFissionSkipImagesLifecyclePersistenceAndMotherImport(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("workspace_page", store.PageFission)

	a := NewApp(st, nil, nil, "")
	SetTestApp(a)
	defer SetTestApp(nil)
	if a.statePage != pageFission || a.prompts != DefaultFissionPrompt || a.skipImages {
		t.Fatalf("unexpected initial fission state: page=%d skip=%v prompts=%q", a.statePage, a.skipImages, a.prompts)
	}

	// Toggling skipImages=true while DefaultFissionPrompt is active must clear the mother-image boilerplate.
	setWorkbenchFieldBridge("skipImages", "true")
	if !a.skipImages || a.prompts != "" {
		t.Fatalf("skipImages=true should clear DefaultFissionPrompt: skip=%v prompts=%q", a.skipImages, a.prompts)
	}
	setWorkbenchFieldBridge("prompts", "宇航员橘猫")
	a.saveSettings()

	// Restarting restores skipImages=true and keeps the text-only prompt.
	restarted := NewApp(st, nil, nil, "")
	if !restarted.skipImages || restarted.prompts != "宇航员橘猫" {
		t.Fatalf("restart did not restore fission skipImages: skip=%v prompts=%q", restarted.skipImages, restarted.prompts)
	}

	// Importing a mother image on pageFission must automatically clear skipImages=false.
	restarted.addLoaded(pageFission, []sourceImage{{Path: "/tmp/mother.png", Hash: "abc"}})
	if restarted.skipImages || len(restarted.sources) != 1 {
		t.Fatalf("addLoaded on pageFission must clear skipImages: skip=%v sources=%d", restarted.skipImages, len(restarted.sources))
	}
	if st.Get("fission_skip_images") != "0" {
		t.Fatalf("fission_skip_images not cleared in store: %q", st.Get("fission_skip_images"))
	}

	// Toggling skipImages=false with empty prompt restores DefaultFissionPrompt.
	SetTestApp(restarted)
	setWorkbenchFieldBridge("prompts", "")
	setWorkbenchFieldBridge("skipImages", "true")
	setWorkbenchFieldBridge("skipImages", "false")
	if restarted.skipImages || restarted.prompts != DefaultFissionPrompt {
		t.Fatalf("toggling skipImages off with blank prompt should restore DefaultFissionPrompt: skip=%v prompts=%q", restarted.skipImages, restarted.prompts)
	}
}

func TestQualityNotePersistsAcrossRestartAndClearsOnRewashOrReplace(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("workspace_page", store.PageWash)

	src := filepath.Join(dir, "src.png")
	out := filepath.Join(dir, "out.png")
	rep := filepath.Join(dir, "rep.png")
	// Minimal valid 1x1 PNG header bytes
	validPNG := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xdd, 0x8d, 0xb0, 0x00, 0x00, 0x00,
		0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	for _, p := range []string{src, out, rep} {
		if err := os.WriteFile(p, validPNG, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	a := NewApp(st, nil, nil, "")
	a.queue = []tasks.Task{{
		Index:       1,
		Platform:    "千问",
		Images:      []string{src},
		Status:      tasks.StatusSuccess,
		Results:     []string{out},
		QualityNote: "边缘疑似留白/黑边(top/bottom)",
	}}
	if !a.persistWorkspace(pageWash) {
		t.Fatal("persistWorkspace failed")
	}

	// Restarting restores QualityNote from SQLite
	restarted := NewApp(st, nil, nil, "")
	if len(restarted.queue) != 1 || restarted.queue[0].QualityNote != "边缘疑似留白/黑边(top/bottom)" {
		t.Fatalf("QualityNote lost across restart: %+v", restarted.queue)
	}

	// RequeueDoneTasks clears stale QualityNote, UndoRequeueTasks restores it
	svc := &WorkbenchService{app: restarted}
	svc.RequeueDoneTasks(context.Background(), []int{1})
	if restarted.queue[0].QualityNote != "" {
		t.Fatalf("RequeueDoneTasks did not clear QualityNote: %q", restarted.queue[0].QualityNote)
	}
	svc.UndoRequeueTasks(context.Background())
	if restarted.queue[0].QualityNote != "边缘疑似留白/黑边(top/bottom)" {
		t.Fatalf("UndoRequeueTasks did not restore QualityNote: %q", restarted.queue[0].QualityNote)
	}

	// replaceCardImage clears QualityNote after replacing with a clean local image
	if err := restarted.replaceCardImage(1, rep); err != nil {
		t.Fatal(err)
	}
	if restarted.queue[0].QualityNote != "" {
		t.Fatalf("replaceCardImage did not clear QualityNote: %q", restarted.queue[0].QualityNote)
	}
}

func TestFreeBatchImageUnifiedMultilinePromptAndAddLoadedSwitch(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("workspace_page", store.PageGen)

	a := NewApp(st, nil, nil, "")
	a.batchMode = batchText
	a.prompts = "第一行要求：宫崎骏水彩风格\n第二行要求：保留主体轮廓\n第三行要求：暖阳草地背景"

	// Text mode splits per line
	if got := a.taskPromptList(); len(got) != 3 {
		t.Fatalf("batchText taskPromptList len=%d, want 3: %v", len(got), got)
	}

	// Importing reference images on pageHome switches to batchImage and keeps unified multiline prompt
	a.addLoaded(pageHome, []sourceImage{{Path: "/tmp/ref1.png", Hash: "h1"}, {Path: "/tmp/ref2.png", Hash: "h2"}})
	if a.batchMode != batchImage {
		t.Fatalf("addLoaded on pageHome did not switch to batchImage: got %d", a.batchMode)
	}
	if st.Get("batch_mode") != "image" {
		t.Fatalf("batch_mode not persisted: %q", st.Get("batch_mode"))
	}
	list := a.taskPromptList()
	if len(list) != 1 || list[0] != a.prompts {
		t.Fatalf("batchImage must treat multiline description as unified prompt, got %v", list)
	}
	opts := a.taskOptions([]string{"豆包"}, list, a.batchImages())
	queue := tasks.BuildQueue(opts, map[string][]int64{"豆包": {1}})
	if len(queue) != 2 || queue[0].Prompt != a.prompts || queue[1].Prompt != a.prompts {
		t.Fatalf("batchImage queue prompts should match unified prompt on every source: %+v", queue)
	}
}

func TestQianwenWashDefaultUpgradeAndWashStyleStructuredOrder(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("workspace_page", store.PageWash)
	st.SetPlatforms([]string{"千问"})
	st.Set("wash_prompt", prompts.PreviousDefaultWashPrompt)

	a := NewApp(st, nil, nil, "")
	if a.washPrompt() != DefaultWashPrompt {
		t.Fatalf("previous Qianwen wash default not upgraded: got %q", a.washPrompt())
	}

	// Wash style instruction is appended after structured extras so style precedence holds cleanly.
	parts := defaultPromptParts()
	parts.Light.Enabled = true
	a.washStyle = "水彩插画"
	extras := a.promptExtras(pageWash, &parts)
	lightIdx := len("光线：光线柔和均匀")
	if len(extras) <= lightIdx || extras[:lightIdx] != "光线：光线柔和均匀" {
		t.Fatalf("expected structured extra before wash style instruction, got %q", extras)
	}
}

func TestFreeBatchTextStructuredOnlyStartableAndRunningGuards(t *testing.T) {
	rows := []store.Browser{
		{ID: 1, Platform: "豆包", Name: "1", IsLoggedIn: true, Health: store.HealthReady},
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("workspace_page", store.PageGen)
	a := NewApp(st, nil, nil, "")
	a.platDoubao = true
	a.batchMode = batchText
	a.prompts = ""
	if plan := a.workbenchPlan(rows); plan.Action != StartNone {
		t.Fatalf("empty prompt and no structured parts should not be startable: %+v", plan)
	}
	a.currentPromptParts().Light.Enabled = true
	if plan := a.workbenchPlan(rows); plan.Action != StartGenerate {
		t.Fatalf("structured-only free text mode should be startable to match BuildQueue: %+v", plan)
	}

	// Running state protects batchMode and skipImages from mid-run mutation.
	SetTestApp(a)
	t.Cleanup(func() { SetTestApp(nil) })
	a.running = true
	a.runPage = pageHome
	setWorkbenchFieldBridge("batchMode", "image")
	if a.batchMode != batchText {
		t.Fatalf("batchMode mutated while pageHome was running: got %d", a.batchMode)
	}
	a.running = false
	setWorkbenchFieldBridge("mode", "fission")
	a.running = true
	a.runPage = pageFission
	a.skipImages = false
	setWorkbenchFieldBridge("skipImages", "true")
	if a.skipImages {
		t.Fatal("skipImages mutated while pageFission was running")
	}
}

func TestPromptTemplatesInjectAndIsolateAcrossAllThreeWorkbenchModes(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("workspace_page", store.PageWash)
	st.SetPlatforms([]string{"千问"})

	a := NewApp(st, nil, nil, "")
	svc := &WorkbenchService{app: a}
	psvc := &PromptsService{app: a}
	ctx := context.Background()

	psvc.CreateNewPrompt(ctx, "电商去背景", "洗图", "纯白底去阴影重绘")
	psvc.CreateNewPrompt(ctx, "赛博霓虹", "生图", "雨夜赛博街景\n全息广告牌")
	psvc.CreateNewPrompt(ctx, "多角度主图", "裂变", "保留包装瓶型，多角度重绘")
	psvc.CreateNewPrompt(ctx, "通用极简", "通用", "极简杂志质感")

	// 1) Wash mode (on 千问): exposes built-in 豆包洗图 + 洗图/通用 first, plus other user templates; injects cleanly
	wbWash := svc.GetWorkbenchState(ctx)
	if wbWash.Mode != "wash" || len(wbWash.PromptTemplates) != 5 {
		t.Fatalf("unexpected wash prompt templates: mode=%s len=%d items=%+v", wbWash.Mode, len(wbWash.PromptTemplates), wbWash.PromptTemplates)
	}
	if wbWash.PromptTemplates[0].Label != "豆包洗图" || !wbWash.PromptTemplates[0].Builtin || !wbWash.PromptTemplates[0].Matched {
		t.Fatalf("expected built-in 豆包洗图 matched first in wash mode: %+v", wbWash.PromptTemplates[0])
	}
	svc.ApplyWorkbenchPrompt(ctx, "豆包洗图")
	if got := svc.GetWorkbenchState(ctx); got.Prompts != prompts.DoubaoWashPrompt || got.PromptChoice != "豆包洗图" {
		t.Fatalf("failed to inject built-in 豆包洗图 in wash mode: choice=%q", got.PromptChoice)
	}
	// Editing text clears wash template choice badge, and resetWashPrompt restores Qianwen default
	svc.SetWorkbenchField(ctx, "prompts", "自定义改写洗图词")
	if got := svc.GetWorkbenchState(ctx); got.PromptChoice != choosePromptLabel {
		t.Fatalf("expected edited wash prompt to clear active template label, got %q", got.PromptChoice)
	}
	svc.ApplyWorkbenchPrompt(ctx, "电商去背景")
	svc.SetWorkbenchField(ctx, "resetWashPrompt", "")
	if got := svc.GetWorkbenchState(ctx); got.Prompts != DefaultWashPrompt || got.PromptChoice != choosePromptLabel {
		t.Fatalf("resetWashPrompt did not restore Qianwen default & clear choice: choice=%q prompts=%q", got.PromptChoice, got.Prompts)
	}
	// Selecting 选择提示词 in wash mode also restores defaultWashPrompt
	svc.ApplyWorkbenchPrompt(ctx, "通用极简")
	svc.ApplyWorkbenchPrompt(ctx, choosePromptLabel)
	if got := svc.GetWorkbenchState(ctx); got.Prompts != DefaultWashPrompt || got.PromptChoice != choosePromptLabel {
		t.Fatalf("choosing reset in wash mode failed: choice=%q", got.PromptChoice)
	}

	// 2) Free mode: excludes built-in 豆包洗图, preserves freeform text across template selection & reset
	svc.SetWorkbenchField(ctx, "mode", "free")
	svc.SetWorkbenchField(ctx, "prompts", "自由词第一行")
	wbFree := svc.GetWorkbenchState(ctx)
	for _, item := range wbFree.PromptTemplates {
		if item.ID == prompts.IDDoubaoWash {
			t.Fatalf("built-in wash template should not appear in free mode: %+v", item)
		}
	}
	svc.ApplyWorkbenchPrompt(ctx, "赛博霓虹")
	if got := svc.GetWorkbenchState(ctx); got.PromptChoice != "赛博霓虹" || got.Prompts != "雨夜赛博街景\n全息广告牌" {
		t.Fatalf("free mode template injection failed: %+v", got.PromptChoice)
	}
	if list := a.taskPromptList(); len(list) != 1 || list[0] != "雨夜赛博街景\n全息广告牌" {
		t.Fatalf("free mode template should dispatch as single multi-line prompt, got %v", list)
	}

	// 3) Fission mode: independent choice, injects template & respects skipImages on reset
	svc.SetWorkbenchField(ctx, "mode", "fission")
	wbFis := svc.GetWorkbenchState(ctx)
	if wbFis.PromptChoice != choosePromptLabel {
		t.Fatalf("fission mode should not inherit free mode choice, got %q", wbFis.PromptChoice)
	}
	svc.ApplyWorkbenchPrompt(ctx, "多角度主图")
	if got := svc.GetWorkbenchState(ctx); got.PromptChoice != "多角度主图" || got.Prompts != "保留包装瓶型，多角度重绘" {
		t.Fatalf("fission mode template injection failed: choice=%q prompts=%q", got.PromptChoice, got.Prompts)
	}
	svc.ApplyWorkbenchPrompt(ctx, choosePromptLabel)
	if got := svc.GetWorkbenchState(ctx); got.PromptChoice != choosePromptLabel || got.Prompts != DefaultFissionPrompt {
		t.Fatalf("fission reset with !skipImages should restore DefaultFissionPrompt: choice=%q prompts=%q", got.PromptChoice, got.Prompts)
	}
	svc.SetWorkbenchField(ctx, "skipImages", "true")
	svc.ApplyWorkbenchPrompt(ctx, "通用极简")
	if got := svc.GetWorkbenchState(ctx); got.PromptChoice != "通用极简" || got.Prompts != "极简杂志质感" {
		t.Fatalf("text-only fission template injection failed: choice=%q prompts=%q", got.PromptChoice, got.Prompts)
	}
	svc.ApplyWorkbenchPrompt(ctx, choosePromptLabel)
	if got := svc.GetWorkbenchState(ctx); got.PromptChoice != choosePromptLabel || got.Prompts != "" {
		t.Fatalf("text-only fission reset must clear prompt instead of restoring mother-image default: choice=%q prompts=%q", got.PromptChoice, got.Prompts)
	}

	// Free mode choice stays intact and survives restart as human-readable label (not raw user ID)
	svc.SetWorkbenchField(ctx, "mode", "free")
	if got := svc.GetWorkbenchState(ctx); got.PromptChoice != "赛博霓虹" {
		t.Fatalf("free mode choice polluted after fission edits: got %q", got.PromptChoice)
	}
	a.saveSettings()
	restarted := NewApp(st, nil, nil, "")
	rsvc := &WorkbenchService{app: restarted}
	rsvc.SetWorkbenchField(ctx, "mode", "free")
	if got := rsvc.GetWorkbenchState(ctx); got.PromptChoice != "赛博霓虹" {
		t.Fatalf("restarted free mode PromptChoice = %q, want 赛博霓虹", got.PromptChoice)
	}
	// Deleting user prompt clears active choices across modes
	psvc2 := &PromptsService{app: restarted}
	psvc2.SelectPrompt(ctx, restarted.promptPick)
	psvc2.DeleteUserPrompt(ctx)
	if got := rsvc.GetWorkbenchState(ctx); got.PromptChoice != choosePromptLabel {
		t.Fatalf("deleting active prompt should reset PromptChoice, got %q", got.PromptChoice)
	}
}
