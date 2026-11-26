package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/ui"
)

func setupServiceSuite(t *testing.T, withBrowser bool, setup func(*store.Store)) (ui.Services, *ui.App, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if withBrowser {
		b, err := st.AddBrowser("豆包", "豆包1", filepath.Join(dir, "profile-doubao-1"))
		if err != nil {
			t.Fatal(err)
		}
		st.UpdateBrowser(b.ID, false, true)
	}
	if setup != nil {
		setup(st)
	}

	bm := browser.NewManager()
	eng := tasks.NewEngine(st, bm, nil, dir)
	t.Cleanup(eng.Shutdown)

	app := ui.NewApp(st, bm, eng, dir)
	app.StartActor()
	t.Cleanup(app.StopActor)
	return ui.NewServices(app), app, st, dir
}

func writeSamplePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 64, G: 130, B: 230, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestServicesShellAndAllModulesSnapshot(t *testing.T) {
	svcs, _, _, _ := setupServiceSuite(t, true, nil)
	ctx := context.Background()

	shell := svcs.Core.GetShellState(ctx)
	if shell.OS != runtime.GOOS {
		t.Fatalf("shell.OS = %q, want %q", shell.OS, runtime.GOOS)
	}
	if shell.MediaOrigin != ui.MediaOrigin() {
		t.Fatalf("shell.MediaOrigin = %q, want %q", shell.MediaOrigin, ui.MediaOrigin())
	}
	if len(shell.Platforms) != 3 || shell.Platforms[0].ID != "doubao" || shell.Platforms[1].ID != "qianwen" || shell.Platforms[2].ID != "qwen_intl" {
		t.Fatalf("unexpected shell platforms: %+v", shell.Platforms)
	}
	if shell.Pool.Ready != 1 || shell.Pool.Total != 1 {
		t.Fatalf("unexpected pool summary: %+v", shell.Pool)
	}

	// Switch across all 8 module tabs cleanly.
	for _, tab := range []int{1, 0, 2, 3, 4, 5, 6, 7, 8, 9} {
		svcs.Core.SetTab(ctx, tab)
		if got := svcs.Core.GetShellState(ctx).Tab; got != tab {
			t.Fatalf("after SetTab(%d), shell.Tab = %d", tab, got)
		}
	}

	// Verify all 9 domain snapshots initialize non-nil slices and clean defaults.
	if wb := svcs.Workbench.GetWorkbenchState(ctx); wb.Sources == nil || wb.DoneTiles == nil || wb.Lanes == nil {
		t.Fatalf("workbench DTO returned nil slices: %+v", wb)
	}
	if br := svcs.Browser.GetBrowserState(ctx); br.Rows == nil || len(br.Engines) != 1 || br.Engines[0] != browser.KindFingerprint {
		t.Fatalf("browser DTO unexpected: %+v", br)
	}
	if pr := svcs.Prompts.GetPromptsState(ctx); len(pr.Items) == 0 {
		t.Fatalf("expected built-in prompt templates, got %+v", pr)
	}
	if dl := svcs.Download.GetDownloadState(ctx); dl.Tasks == nil || dl.GuideGroups == nil {
		t.Fatalf("download DTO returned nil slices: %+v", dl)
	}
	if dd := svcs.Dedup.GetDedupState(ctx); dd.Items == nil || dd.Groups == nil {
		t.Fatalf("dedup DTO returned nil slices: %+v", dd)
	}
	if rn := svcs.Rename.GetRenameState(ctx); rn.Rows == nil || len(rn.TemplateNames) == 0 {
		t.Fatalf("rename DTO unexpected: %+v", rn)
	}
	if ie := svcs.ImageEdit.GetImageEditState(ctx); ie.Files == nil || ie.SizePresets == nil || ie.RatioPresets == nil {
		t.Fatalf("imageedit DTO returned nil slices: %+v", ie)
	}
	if st := svcs.Settings.GetSettingsState(ctx); st.Version == "" || st.SendStrategy == "" {
		t.Fatalf("settings DTO unexpected: %+v", st)
	}
}

func TestServicesWorkbenchModesSourcesAndRewash(t *testing.T) {
	svcs, _, _, dir := setupServiceSuite(t, true, nil)
	ctx := context.Background()

	// Three workbench modes
	for _, tc := range []struct {
		mode string
		want string
	}{
		{"wash", "wash"},
		{"free", "free"},
		{"fission", "fission"},
		{"wash", "wash"},
	} {
		svcs.Workbench.SetWorkbenchField(ctx, "mode", tc.mode)
		wb := svcs.Workbench.GetWorkbenchState(ctx)
		if wb.Mode != tc.want {
			t.Fatalf("mode = %q, want %q", wb.Mode, tc.want)
		}
	}

	// Single-select platform
	svcs.Workbench.SetPlatformSelected(ctx, "千问", true)
	if sh := svcs.Core.GetShellState(ctx); sh.Platform != "千问" {
		t.Fatalf("platform = %q, want 千问", sh.Platform)
	}
	svcs.Workbench.SetPlatformSelected(ctx, "豆包", true)

	// Ingest source images and verify opaque tulaimedia URLs
	p1 := filepath.Join(dir, "src1.png")
	p2 := filepath.Join(dir, "src2.png")
	writeSamplePNG(t, p1, 128, 128)
	writeSamplePNG(t, p2, 128, 128)
	svcs.Workbench.AddWorkbenchSources(ctx, []string{p1, p2})

	wb := svcs.Workbench.GetWorkbenchState(ctx)
	if wb.SourcesTotal != 2 || len(wb.Sources) != 2 {
		t.Fatalf("sources total=%d len=%d, want 2", wb.SourcesTotal, len(wb.Sources))
	}
	if !strings.HasPrefix(wb.Sources[0].FileURL, ui.MediaOrigin()+"/file?t=") {
		t.Fatalf("unexpected source media URL: %q", wb.Sources[0].FileURL)
	}

	// Switching to 千问 / 千问国际 opens independent workspaces without Doubao's sources, and switching back restores them
	svcs.Workbench.SetPlatformSelected(ctx, "千问", true)
	if wbQ := svcs.Workbench.GetWorkbenchState(ctx); wbQ.SourcesTotal != 0 || len(wbQ.Sources) != 0 || wbQ.Platform != "千问" || !wbQ.Qianwen || wbQ.QwenIntl {
		t.Fatalf("expected qianwen workspace isolated from doubao sources, got %+v", wbQ)
	}
	svcs.Workbench.SetPlatformSelected(ctx, "千问国际", true)
	if wbI := svcs.Workbench.GetWorkbenchState(ctx); wbI.SourcesTotal != 0 || len(wbI.Sources) != 0 || wbI.Platform != "千问国际" || !wbI.QwenIntl || wbI.Qianwen || wbI.Doubao {
		t.Fatalf("expected qwen_intl workspace isolated, got %+v", wbI)
	}
	svcs.Workbench.SetWorkbenchField(ctx, "prompts", "international prompt only")
	svcs.Workbench.SetPlatformSelected(ctx, "千问", true)
	if wbQ := svcs.Workbench.GetWorkbenchState(ctx); wbQ.Prompts == "international prompt only" {
		t.Fatalf("expected domestic qianwen prompts isolated from qwen_intl")
	}
	svcs.Workbench.SetPlatformSelected(ctx, "千问国际", true)
	if wbI := svcs.Workbench.GetWorkbenchState(ctx); wbI.Prompts != "international prompt only" {
		t.Fatalf("expected qwen_intl prompt preserved, got %q", wbI.Prompts)
	}
	svcs.Workbench.SetPlatformSelected(ctx, "豆包", true)
	if wbD := svcs.Workbench.GetWorkbenchState(ctx); wbD.SourcesTotal != 2 || len(wbD.Sources) != 2 {
		t.Fatalf("expected doubao sources preserved after switch back, got total=%d len=%d", wbD.SourcesTotal, len(wbD.Sources))
	}

	// Gallery & preview modal toggles
	svcs.Workbench.SetGalleryOpen(ctx, true)
	svcs.Workbench.ToggleGallerySelect(ctx, p1, true)
	svcs.Workbench.SetPreviewPath(ctx, p1)
	wb = svcs.Workbench.GetWorkbenchState(ctx)
	if !wb.GalleryOn || wb.GallerySelected != 1 || !wb.PreviewOn || !strings.HasPrefix(wb.PreviewURL, ui.MediaOrigin()+"/file?t=") {
		t.Fatalf("gallery/preview state mismatch: galleryOn=%v sel=%d previewOn=%v url=%q", wb.GalleryOn, wb.GallerySelected, wb.PreviewOn, wb.PreviewURL)
	}

	// Results done accordion toggle
	svcs.Workbench.SetResultsDoneOpen(ctx, true)
	if !svcs.Workbench.GetWorkbenchState(ctx).ResultsDoneOpen {
		t.Fatal("expected ResultsDoneOpen true")
	}

	// Batch mode accepts both numeric ("1"/"0") and string ("image"/"text") values
	svcs.Workbench.SetWorkbenchField(ctx, "batchMode", "1")
	if got := svcs.Workbench.GetWorkbenchState(ctx).BatchMode; got != 1 {
		t.Fatalf("after batchMode=1, got %d, want 1", got)
	}
	svcs.Workbench.SetWorkbenchField(ctx, "batchMode", "0")
	if got := svcs.Workbench.GetWorkbenchState(ctx).BatchMode; got != 0 {
		t.Fatalf("after batchMode=0, got %d, want 0", got)
	}

	// Web-native output size presets only (no custom dimensions or fixed pixel sizes)
	svcs.Workbench.SetOutputSize(ctx, "3:2（相机横）", "", "")
	wb = svcs.Workbench.GetWorkbenchState(ctx)
	if wb.OutputPreset != "3:2（相机横）" || wb.OutputRatio != "3:2" {
		t.Fatalf("unexpected 3:2 preset state: preset=%q ratio=%q mode=%q", wb.OutputPreset, wb.OutputRatio, wb.OutputMode)
	}
	for _, preset := range wb.OutputPresets {
		if preset == "自定义尺寸" || preset == "1024×1024" || preset == "2048×2048" {
			t.Fatalf("non-native preset %q must not be exposed", preset)
		}
	}
	svcs.Workbench.SetOutputSize(ctx, "自定义尺寸", "", "")
	wb = svcs.Workbench.GetWorkbenchState(ctx)
	if wb.OutputPreset != "3:2（相机横）" || wb.OutputRatio != "3:2" {
		t.Fatalf("custom output size must be rejected: %+v", wb)
	}
}

func TestServicesThemeLogsWizardAndSettings(t *testing.T) {
	svcs, _, st, dir := setupServiceSuite(t, false, nil)
	ctx := context.Background()

	// First-run wizard gate can be skipped
	if !svcs.Core.WizardState(ctx).On {
		t.Fatal("expected first-run wizard active when no accounts exist")
	}
	if err := svcs.Core.WizardAction(ctx, "skip", ""); err != nil {
		t.Fatal(err)
	}
	if svcs.Core.WizardState(ctx).On {
		t.Fatal("expected wizard dismissed after skip")
	}

	// Dark theme toggle & SQLite persistence
	svcs.Core.SetDarkTheme(ctx, true)
	if !svcs.Core.GetShellState(ctx).Dark || st.Get("ui_theme") != "dark" {
		t.Fatalf("dark theme not persisted: shell=%v db=%q", svcs.Core.GetShellState(ctx).Dark, st.Get("ui_theme"))
	}
	svcs.Core.SetDarkTheme(ctx, false)
	if svcs.Core.GetShellState(ctx).Dark || st.Get("ui_theme") != "light" {
		t.Fatalf("light theme not persisted: shell=%v db=%q", svcs.Core.GetShellState(ctx).Dark, st.Get("ui_theme"))
	}

	// Log drawer open, filter, clear
	logging.Log.Info("service-layer log smoke entry")
	svcs.Core.SetLogOpen(ctx, true)
	svcs.Core.SetLogFilter(ctx, 0, "smoke", "", false)
	logs := svcs.Core.GetLogsState(ctx)
	if !logs.Open || len(logs.Entries) == 0 {
		t.Fatalf("expected matching log entry, got %+v", logs)
	}
	svcs.Core.ClearLogs(ctx)
	if got := svcs.Core.GetLogsState(ctx).Total; got != 0 {
		t.Fatalf("expected cleared logs total=0, got %d", got)
	}

	// Settings round-trip (including independent Doubao/Qwen workbench concurrency reflection)
	outDir := filepath.Join(dir, "custom-out")
	svcs.Settings.SetSettingField(ctx, "outputPath", outDir)
	svcs.Settings.SetSettingField(ctx, "sendStrategy", "随机循环")
	svcs.Settings.SetSettingField(ctx, "captchaBackend", "ikunocr")
	svcs.Settings.SetSettingField(ctx, "concurrencyAutoDoubao", "true")
	svcs.Settings.SetSettingField(ctx, "concurrencyMaxDoubao", "5")
	svcs.Settings.SetSettingField(ctx, "concurrencyAutoQianwen", "false")
	svcs.Settings.SetSettingField(ctx, "concurrencyMaxQianwen", "3")
	svcs.Settings.SetSettingField(ctx, "concurrencyAutoQwenIntl", "true")
	svcs.Settings.SetSettingField(ctx, "concurrencyMaxQwenIntl", "4")
	svcs.Settings.SetSettingField(ctx, "dailyCapQianwen", "60")
	svcs.Settings.SetSettingField(ctx, "dailyCapQwenIntl", "80")
	svcs.Settings.SaveSettingsNow(ctx)
	cfg := svcs.Settings.GetSettingsState(ctx)
	if cfg.OutputPath != outDir || cfg.SendStrategy != "随机循环" || cfg.CaptchaBackend != "ikunocr" || !cfg.ConcurrencyAuto || cfg.ConcurrencyMax != 5 || cfg.DailyCapQianwen != 60 || cfg.DailyCapQwenIntl != 80 {
		t.Fatalf("settings mismatch: %+v", cfg)
	}
	svcs.Workbench.SetPlatformSelected(ctx, "豆包", true)
	wbDoubao := svcs.Workbench.GetWorkbenchState(ctx)
	if !wbDoubao.ConcurrencyAuto || wbDoubao.ConcurrencyMax != 5 {
		t.Fatalf("doubao workbench concurrency mismatch: auto=%v max=%d", wbDoubao.ConcurrencyAuto, wbDoubao.ConcurrencyMax)
	}
	svcs.Workbench.SetPlatformSelected(ctx, "千问", true)
	wbQianwen := svcs.Workbench.GetWorkbenchState(ctx)
	if wbQianwen.ConcurrencyAuto || wbQianwen.ConcurrencyMax != 3 {
		t.Fatalf("qianwen workbench concurrency mismatch: auto=%v max=%d", wbQianwen.ConcurrencyAuto, wbQianwen.ConcurrencyMax)
	}
	svcs.Workbench.SetPlatformSelected(ctx, "千问国际", true)
	wbIntl := svcs.Workbench.GetWorkbenchState(ctx)
	if !wbIntl.ConcurrencyAuto || wbIntl.ConcurrencyMax != 4 {
		t.Fatalf("qwen_intl workbench concurrency mismatch: auto=%v max=%d", wbIntl.ConcurrencyAuto, wbIntl.ConcurrencyMax)
	}
	// Turning Qwen ON leaves Doubao and QwenIntl untouched
	svcs.Settings.SetSettingField(ctx, "concurrencyAutoQianwen", "true")
	svcs.Workbench.SetPlatformSelected(ctx, "千问", true)
	if wbQ := svcs.Workbench.GetWorkbenchState(ctx); !wbQ.ConcurrencyAuto || wbQ.ConcurrencyMax != 3 {
		t.Fatalf("expected qianwen concurrency (true, 3), got (%v, %d)", wbQ.ConcurrencyAuto, wbQ.ConcurrencyMax)
	}
	svcs.Workbench.SetPlatformSelected(ctx, "千问国际", true)
	if wbI := svcs.Workbench.GetWorkbenchState(ctx); !wbI.ConcurrencyAuto || wbI.ConcurrencyMax != 4 {
		t.Fatalf("expected qwen_intl concurrency unchanged (true, 4), got (%v, %d)", wbI.ConcurrencyAuto, wbI.ConcurrencyMax)
	}
	svcs.Workbench.SetPlatformSelected(ctx, "豆包", true)
	if wbD := svcs.Workbench.GetWorkbenchState(ctx); !wbD.ConcurrencyAuto || wbD.ConcurrencyMax != 5 {
		t.Fatalf("expected doubao concurrency unchanged (true, 5), got (%v, %d)", wbD.ConcurrencyAuto, wbD.ConcurrencyMax)
	}
}

func TestServicesImageEditCropBoxAndStageURLs(t *testing.T) {
	svcs, _, _, dir := setupServiceSuite(t, true, nil)
	ctx := context.Background()

	imgPath := filepath.Join(dir, "sample.png")
	writeSamplePNG(t, imgPath, 400, 300)
	svcs.ImageEdit.EditAddPaths(ctx, []string{imgPath})
	svcs.ImageEdit.EditSetFeature(ctx, "crop", true)
	svcs.ImageEdit.EditSetField(ctx, "mode", "crop")
	svcs.ImageEdit.EditSetField(ctx, "split", "35")

	ie := svcs.ImageEdit.GetImageEditState(ctx)
	if !ie.HasSelection || len(ie.Files) != 1 || ie.Split != 35 {
		t.Fatalf("unexpected imageedit state: hasSel=%v files=%d split=%v", ie.HasSelection, len(ie.Files), ie.Split)
	}
	if !strings.HasPrefix(ie.Files[0].FileURL, ui.MediaOrigin()+"/file?t=") {
		t.Fatalf("unexpected edit file media URL: %q", ie.Files[0].FileURL)
	}

	// Set and clear crop box via new IPC methods
	svcs.ImageEdit.EditSetCropBox(ctx, 10, 20, 120, 80)
	svcs.ImageEdit.EditClearCropBox(ctx)
}

func TestServicesDTOJSONCamelCaseContract(t *testing.T) {
	svcs, _, _, _ := setupServiceSuite(t, true, nil)
	ctx := context.Background()

	checkKeys := func(name string, v any, wantKeys ...string) {
		t.Helper()
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("%s marshal failed: %v", name, err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("%s unmarshal failed: %v", name, err)
		}
		for _, k := range wantKeys {
			if _, ok := m[k]; !ok {
				t.Fatalf("%s JSON missing expected camelCase key %q; payload=%s", name, k, string(raw))
			}
		}
	}

	checkKeys("ShellStateDTO", svcs.Core.GetShellState(ctx), "mediaOrigin", "os", "version", "platforms", "dark", "dotMatrixEffect")
	checkKeys("DownloadStateDTO", svcs.Download.GetDownloadState(ctx), "urls", "selectedId", "minKb", "maxKb", "tasks")
	checkKeys("RenameStateDTO", svcs.Rename.GetRenameState(ctx), "aiUrl", "aiKey", "aiModel", "rows", "rules")
	checkKeys("BrowserDrawerDTO", svcs.Browser.GetBrowserDrawerState(ctx), "accountId", "frameVersion", "frameUrl")
	checkKeys("ImageEditStateDTO", svcs.ImageEdit.GetImageEditState(ctx), "origUrl", "previewUrl", "origVersion", "outVersion", "showCropUi", "cropAspect")
	checkKeys("BrowserStateDTO", svcs.Browser.GetBrowserState(ctx), "missingHint", "sharedExitHints", "checkedCount")
	checkKeys("WorkbenchStateDTO", svcs.Workbench.GetWorkbenchState(ctx), "laneCollapsed", "concurrencyAuto", "concurrencyMax", "promptTemplates")
	checkKeys("SettingsStateDTO", svcs.Settings.GetSettingsState(ctx), "downloadRateKb", "proxyUrl", "customUrl", "sendStrategy", "captchaBackend", "dotMatrixEffect")
}

func TestServicesPromptsLibraryKindsAndBuiltinFork(t *testing.T) {
	svcs, _, _, _ := setupServiceSuite(t, true, nil)
	ctx := context.Background()

	// Create fission template and verify exact kindLbl roundtrip
	svcs.Prompts.CreateNewPrompt(ctx, "视角裂变", "裂变", "多角度商业摄影重构")
	st := svcs.Prompts.GetPromptsState(ctx)
	if st.SelectedKind != "裂变" || st.SelectedName != "视角裂变" || st.SelectedIsBln {
		t.Fatalf("unexpected created fission prompt state: %+v", st)
	}

	// Select built-in doubao_wash, edit with custom name & kind -> forks user copy instead of overwriting built-in
	svcs.Prompts.SelectPrompt(ctx, "doubao_wash")
	svcs.Prompts.SavePromptEdits(ctx, "我的精简洗图", "洗图", "精简二维重绘指令")
	forked := svcs.Prompts.GetPromptsState(ctx)
	if forked.SelectedIsBln || forked.SelectedName != "我的精简洗图" || forked.SelectedKind != "洗图" || forked.SelectedText != "精简二维重绘指令" {
		t.Fatalf("expected custom copy preserving edited name/kind, got: %+v", forked)
	}

	// Built-in template stays intact and can be deleted cleanly for user copy
	svcs.Prompts.DeleteUserPrompt(ctx)
	afterDel := svcs.Prompts.GetPromptsState(ctx)
	for _, item := range afterDel.Items {
		if item.Name == "我的精简洗图" {
			t.Fatalf("deleted user copy still listed: %+v", item)
		}
	}

	// Suffix persistence
	svcs.Prompts.SetPromptSuffix(ctx, "4K 无水印", true)
	withSuffix := svcs.Prompts.GetPromptsState(ctx)
	if !withSuffix.UseSuffix || withSuffix.Suffix != "4K 无水印" {
		t.Fatalf("suffix not persisted: %+v", withSuffix)
	}
}
