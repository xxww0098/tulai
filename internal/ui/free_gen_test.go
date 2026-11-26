package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"

	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestFreeGenIndependentDraftsTemplatesRepeatAndPersistence(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("workspace_page", store.PageGen)
	b, err := st.AddBrowser("豆包", "豆包1", filepath.Join(dir, "p1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(b.ID)

	a := NewApp(st, nil, nil, dir)
	a.userPrompts = []prompts.UserPrompt{
		{ID: "u1", Label: "日系水彩", Kind: prompts.KindGen, Body: "第一行水彩\n第二行柔光"},
	}
	SetTestApp(a)
	svc := &WorkbenchService{app: a}
	ctx := context.Background()

	// 文生图: write batch prompts, set repeat=3
	svc.SetWorkbenchField(ctx, "mode", "free")
	svc.SetWorkbenchField(ctx, "batchMode", "0")
	svc.SetWorkbenchField(ctx, "prompts", "多啦A梦\n蜡笔小新")
	svc.SetWorkbenchField(ctx, "repeatCount", "3")

	wbText := svc.GetWorkbenchState(ctx)
	if wbText.BatchMode != batchText || wbText.RepeatCount != 3 || wbText.PromptUnitCount != 2 || wbText.PlannedTasks != 6 {
		t.Fatalf("unexpected text-to-image state: %+v", wbText)
	}
	if wbText.StartLabel != "开始文生图 (6)" {
		t.Fatalf("StartLabel = %q, want 开始文生图 (6)", wbText.StartLabel)
	}

	// Switch to 图生图: independent empty prompt draft, repeat defaults to 1
	svc.SetWorkbenchField(ctx, "batchMode", "1")
	wbImg := svc.GetWorkbenchState(ctx)
	if wbImg.BatchMode != batchImage || wbImg.Prompts != "" || wbImg.RepeatCount != 1 {
		t.Fatalf("image-to-image should have independent draft/repeat: batch=%d prompts=%q repeat=%d", wbImg.BatchMode, wbImg.Prompts, wbImg.RepeatCount)
	}
	if wbImg.StartLabel != "开始图生图" {
		t.Fatalf("StartLabel with 0 images = %q, want 开始图生图", wbImg.StartLabel)
	}

	// Edit 图生图 prompt, set per_line mode & repeat=2, add 2 sources
	svc.SetWorkbenchField(ctx, "prompts", "改水彩\n改赛博")
	svc.SetWorkbenchField(ctx, "imagePromptMode", "per_line")
	svc.SetWorkbenchField(ctx, "repeatCount", "2")
	a.addLoaded(pageHome, []sourceImage{
		{Path: filepath.Join(dir, "r1.png"), Hash: "h1"},
		{Path: filepath.Join(dir, "r2.png"), Hash: "h2"},
	})
	wbImg = svc.GetWorkbenchState(ctx)
	if wbImg.ImagePromptMode != "per_line" || wbImg.PromptUnitCount != 2 || wbImg.PlannedTasks != 4 || wbImg.StartLabel != "开始图生图 (4)" {
		t.Fatalf("unexpected image-to-image per_line state: mode=%q units=%d planned=%d label=%q", wbImg.ImagePromptMode, wbImg.PromptUnitCount, wbImg.PlannedTasks, wbImg.StartLabel)
	}
	opts := a.taskOptions([]string{"豆包"}, a.taskPromptList(), a.batchImages())
	q := tasks.BuildQueue(opts, map[string][]int64{"豆包": {b.ID}})
	if len(q) != 4 || q[0].Prompt != "改水彩" || q[1].Prompt != "改水彩" || q[2].Prompt != "改赛博" || q[3].Prompt != "改赛博" {
		t.Fatalf("unexpected img2img queue: %+v", q)
	}

	// Switch back to 文生图: original draft & repeat preserved
	svc.SetWorkbenchField(ctx, "batchMode", "0")
	wbBack := svc.GetWorkbenchState(ctx)
	if wbBack.Prompts != "多啦A梦\n蜡笔小新" || wbBack.RepeatCount != 3 {
		t.Fatalf("text-to-image draft lost after round-trip: prompts=%q repeat=%d", wbBack.Prompts, wbBack.RepeatCount)
	}

	// Apply template in 文生图 -> templateLocked=true -> editing unlocks immediately; detachTemplate switches back to line splitting while keeping text
	svc.ApplyWorkbenchPrompt(ctx, "日系水彩")
	wbTpl := svc.GetWorkbenchState(ctx)
	if !wbTpl.TemplateLocked || wbTpl.PromptUnitCount != 1 {
		t.Fatalf("template should lock whole-paragraph send: locked=%v units=%d", wbTpl.TemplateLocked, wbTpl.PromptUnitCount)
	}
	svc.SetWorkbenchField(ctx, "prompts", "第一行水彩\n第二行柔光\n第三行")
	wbEdited := svc.GetWorkbenchState(ctx)
	if wbEdited.TemplateLocked || wbEdited.PromptUnitCount != 3 {
		t.Fatalf("editing locked template should unlock line splitting: locked=%v units=%d", wbEdited.TemplateLocked, wbEdited.PromptUnitCount)
	}
	svc.ApplyWorkbenchPrompt(ctx, "日系水彩")
	svc.SetWorkbenchField(ctx, "detachTemplate", "")
	wbDetached := svc.GetWorkbenchState(ctx)
	if wbDetached.TemplateLocked || wbDetached.Prompts != "第一行水彩\n第二行柔光" || wbDetached.PromptUnitCount != 2 {
		t.Fatalf("detachTemplate failed: locked=%v prompts=%q units=%d", wbDetached.TemplateLocked, wbDetached.Prompts, wbDetached.PromptUnitCount)
	}

	// Persist & reload from SQLite: both drafts, repeats, and imagePromptMode survive restart
	a.saveSettings()
	reloaded := NewApp(st, nil, nil, dir)
	SetTestApp(reloaded)
	rSvc := &WorkbenchService{app: reloaded}
	rText := rSvc.GetWorkbenchState(ctx)
	if rText.BatchMode != batchText || rText.Prompts != "第一行水彩\n第二行柔光" || rText.RepeatCount != 3 {
		t.Fatalf("reloaded text-to-image mismatch: batch=%d prompts=%q repeat=%d", rText.BatchMode, rText.Prompts, rText.RepeatCount)
	}
	rSvc.SetWorkbenchField(ctx, "batchMode", "1")
	rImg := rSvc.GetWorkbenchState(ctx)
	if rImg.Prompts != "改水彩\n改赛博" || rImg.RepeatCount != 2 || rImg.ImagePromptMode != "per_line" {
		t.Fatalf("reloaded image-to-image mismatch: prompts=%q repeat=%d mode=%q", rImg.Prompts, rImg.RepeatCount, rImg.ImagePromptMode)
	}
}

func TestImportPromptTextFileAndQianwenNativeRatio(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("workspace_page", store.PageGen)

	a := NewApp(st, nil, nil, dir)
	SetTestApp(a)
	svc := &WorkbenchService{app: a}
	ctx := context.Background()

	// Import a TXT file into 文生图: strictly one prompt per non-empty line
	txtPath := filepath.Join(dir, "prompts.txt")
	if err := os.WriteFile(txtPath, []byte("\uFEFF一只橘猫\r\n戴着墨镜\n\n赛博城市夜景\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.importPromptTextFile("豆包", txtPath); err != nil {
		t.Fatalf("importPromptTextFile failed: %v", err)
	}
	wb := svc.GetWorkbenchState(ctx)
	if wb.BatchMode != batchText || wb.Prompts != "一只橘猫\n戴着墨镜\n赛博城市夜景" || wb.PromptUnitCount != 3 {
		t.Fatalf("unexpected imported text prompts: batch=%d prompts=%q units=%d", wb.BatchMode, wb.Prompts, wb.PromptUnitCount)
	}

	// Dropping a GBK-encoded TXT file on 文生图 appends prompts via ingestSources
	gbkBytes, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("水彩樱花\n\n国风山水\n"))
	if err != nil {
		t.Fatal(err)
	}
	gbkPath := filepath.Join(dir, "gbk.txt")
	if err := os.WriteFile(gbkPath, gbkBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	a.ingestSources([]string{gbkPath})
	wbAfterDrop := svc.GetWorkbenchState(ctx)
	if wbAfterDrop.Prompts != "一只橘猫\n戴着墨镜\n赛博城市夜景\n水彩樱花\n国风山水" || wbAfterDrop.PromptUnitCount != 5 {
		t.Fatalf("unexpected prompts after GBK drop: prompts=%q units=%d", wbAfterDrop.Prompts, wbAfterDrop.PromptUnitCount)
	}

	// UTF-16LE BOM TXT import also decodes cleanly
	u16Bytes, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes([]byte("霓虹像素\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	u16Path := filepath.Join(dir, "u16.txt")
	if err := os.WriteFile(u16Path, u16Bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.importPromptTextFile("豆包", u16Path); err != nil {
		t.Fatalf("UTF-16 importPromptTextFile failed: %v", err)
	}
	wbAfterU16 := svc.GetWorkbenchState(ctx)
	if wbAfterU16.Prompts != "一只橘猫\n戴着墨镜\n赛博城市夜景\n水彩樱花\n国风山水\n霓虹像素" || wbAfterU16.PromptUnitCount != 6 {
		t.Fatalf("unexpected prompts after UTF-16 import: prompts=%q units=%d", wbAfterU16.Prompts, wbAfterU16.PromptUnitCount)
	}

	// Switch to 千问 and configure full unified output size without affecting Doubao OutputSize
	svc.SetOutputSize(ctx, "4:3（标准横）", "", "", "", "", false)
	svc.SetPlatformSelected(ctx, "千问", true)
	svc.SetOutputSize(ctx, "16:9（宽屏）", "", "", "", "", false)

	wbQ := svc.GetWorkbenchState(ctx)
	if !wbQ.Qianwen || wbQ.OutputPreset != "16:9（宽屏）" || wbQ.OutputRatio != "16:9" {
		t.Fatalf("unexpected Qianwen output ratio state: preset=%q ratio=%q", wbQ.OutputPreset, wbQ.OutputRatio)
	}
	qOpts := a.taskOptions([]string{"千问"}, []string{"多啦A梦"}, nil)
	if qOpts.OutputSize.Ratio != "16:9" {
		t.Fatalf("taskOptions OutputSize.Ratio = %q, want 16:9", qOpts.OutputSize.Ratio)
	}

	// Custom dimensions & crop also work identically on Qianwen and persist across reload
	svc.SetOutputSize(ctx, "自定义尺寸", "", "", "", "", false)
	svc.SetOutputSize(ctx, "自定义尺寸", "固定尺寸", "", "1200", "900", true)
	wbQCustom := svc.GetWorkbenchState(ctx)
	if wbQCustom.OutputPreset != "自定义尺寸" || wbQCustom.OutputMode != "固定尺寸" || wbQCustom.OutputWidth != "1200" || wbQCustom.OutputHeight != "900" || !wbQCustom.OutputCrop {
		t.Fatalf("unexpected Qianwen custom output state: %+v", wbQCustom)
	}
	reloadedQ := NewApp(st, nil, nil, dir)
	if qCustom := reloadedQ.outputSizeFor("千问"); qCustom.Width != 1200 || qCustom.Height != 900 || !qCustom.Crop {
		t.Fatalf("Qianwen custom output size lost across reload: %+v", qCustom)
	}

	// Switching back to Doubao preserves Doubao's 4:3 setting
	svc.SetPlatformSelected(ctx, "豆包", true)
	wbD := svc.GetWorkbenchState(ctx)
	if wbD.OutputPreset != "4:3（标准横）" || wbD.OutputRatio != "4:3" {
		t.Fatalf("Doubao output ratio altered by Qianwen: preset=%q ratio=%q", wbD.OutputPreset, wbD.OutputRatio)
	}
}

func TestUnifiedWashStyleAndOutputSizeParityAndLegacyQianwenMigration(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.SetPlatforms([]string{"千问"})
	st.Set("qianwen_output_ratio", "16:9")

	a := NewApp(st, nil, nil, dir)
	SetTestApp(a)
	t.Cleanup(func() { SetTestApp(nil) })
	svc := &WorkbenchService{app: a}
	ctx := context.Background()

	wbQ := svc.GetWorkbenchState(ctx)
	if wbQ.OutputPreset != "16:9（宽屏）" || wbQ.OutputMode != outputRatio || wbQ.OutputRatio != "16:9" {
		t.Fatalf("legacy qianwen_output_ratio not migrated: preset=%q mode=%q ratio=%q", wbQ.OutputPreset, wbQ.OutputMode, wbQ.OutputRatio)
	}

	svc.SetPlatformSelected(ctx, "豆包", true)
	wbD := svc.GetWorkbenchState(ctx)
	if len(wbQ.OutputPresets) != len(wbD.OutputPresets) {
		t.Fatalf("outputPresets length mismatch: qianwen=%v doubao=%v", wbQ.OutputPresets, wbD.OutputPresets)
	}
	for i := range wbD.OutputPresets {
		if wbQ.OutputPresets[i] != wbD.OutputPresets[i] {
			t.Fatalf("outputPresets[%d] mismatch: qianwen=%q doubao=%q", i, wbQ.OutputPresets[i], wbD.OutputPresets[i])
		}
	}
	if len(wbQ.WashStyleOptions) != len(wbD.WashStyleOptions) {
		t.Fatalf("washStyleOptions length mismatch: qianwen=%v doubao=%v", wbQ.WashStyleOptions, wbD.WashStyleOptions)
	}
	for i := range wbD.WashStyleOptions {
		if wbQ.WashStyleOptions[i] != wbD.WashStyleOptions[i] {
			t.Fatalf("washStyleOptions[%d] mismatch: qianwen=%q doubao=%q", i, wbQ.WashStyleOptions[i], wbD.WashStyleOptions[i])
		}
	}
}
