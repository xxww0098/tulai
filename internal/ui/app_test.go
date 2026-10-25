package ui

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"xbs/internal/browser"
	"xbs/internal/store"
	"xbs/internal/tasks"
)

func TestWorkspaceAndSettingsStayVisible(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 220}
	tt := ui.NewTester(a.view, 1280, 840)
	for _, label := range []string{"任务设置", "浏览器", "验证码", "开始任务", "文生图", "图生图", "提示词", "打开网页", "运行日志", "检查更新"} {
		assertVisible(t, tt, label, 1280, 840)
	}
	if tt.HasText("添加图片") {
		t.Fatal("text mode shows the image importer")
	}
	if err := tt.Click("更多设置"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	before, _ := tt.Find("文件名后缀")
	tt.Scroll(200, 500, 0, 2000)
	assertVisible(t, tt, "文件名后缀", 1280, 840)
	after, _ := tt.Find("文件名后缀")
	if after.Y >= before.Y {
		t.Fatal("task settings did not scroll")
	}
	if err := tt.Click("图生图"); err != nil {
		t.Fatal(err)
	}
	for _, tab := range []string{"浏览器", "验证码", "任务设置"} {
		if err := tt.Click(tab); err != nil {
			t.Fatal(err)
		}
		assertVisible(t, tt, "开始任务", 1280, 840)
		assertVisible(t, tt, "添加图片", 1280, 840)
	}
}

func TestWorkspaceSplitKeepsDraggedHeight(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 220, batchMode: batchImage}
	tt := ui.NewTester(a.view, 1280, 840)
	r := assertVisible(t, tt, "Divider", 1280, 840)
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.Press(x, y)
	tt.Move(x, y+60)
	tt.Release(x, y+60)
	height := a.sourceHeight
	if height < 270 {
		t.Fatalf("split drag did not resize source pane: %v", height)
	}
	tt.Frame()
	if a.sourceHeight != height {
		t.Fatalf("source pane height reset after frame: %v, want %v", a.sourceHeight, height)
	}
	assertVisible(t, tt, "添加图片", 1280, 840)
	assertVisible(t, tt, "运行日志", 1280, 840)
}

func TestTextOnlyStartWithoutSources(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{
		store: st, manager: browser.NewManager(), engine: tasks.NewEngine(st, nil, nil, ""),
		splitSz: 380, sourceHeight: 210, browserBackend: "Chrome",
		prompts: "画一只猫\n\n画一条鱼", outputPath: t.TempDir(),
	}
	if a.canStart() {
		t.Fatal("未选平台时不应允许开始")
	}
	tt := ui.NewTester(a.view, 1280, 840)
	for _, label := range []string{"文生图", "待选平台", "检查更新"} {
		if !tt.HasText(label) {
			t.Fatalf("缺少文生图说明 %q", label)
		}
	}
	if tt.HasText("待导入") || tt.HasText("添加图片") {
		t.Fatal("已有提示词的文生图不应要求先导入源图")
	}
	if err := tt.Click("开始任务"); err != nil {
		t.Fatal(err)
	}
	if a.running || a.progText != "" {
		t.Fatal("未选平台时开始按钮仍可触发任务")
	}
	a.platDoubao = true
	tt.Frame()
	if !a.canStart() || !tt.HasText("文生图 · 2 条提示词") {
		t.Fatal("只有提示词时应可以开始文生图")
	}
	if len(a.batchImages()) != 0 || len(splitLines(a.prompts)) != 2 {
		t.Fatalf("文生图输入: prompts=%q images=%v", a.prompts, a.batchImages())
	}
	if err := tt.Click("开始任务"); err != nil {
		t.Fatal(err)
	}
	if !a.running || a.progText != "" {
		t.Fatalf("只有提示词时开始被拦住: running=%v text=%q", a.running, a.progText)
	}
}

func TestAppendSourcesDeduplicatesAcrossImports(t *testing.T) {
	a := &App{sources: []sourceImage{{Path: "first.png", Hash: "same"}}}
	if added := a.appendSources([]sourceImage{
		{Path: "duplicate.png", Hash: "same"},
		{Path: "second.png", Hash: "new"},
		{Path: "batch-duplicate.png", Hash: "new"},
	}); added != 1 || len(a.sources) != 2 || a.sources[1].Path != "second.png" {
		t.Fatalf("unexpected imported sources: added=%d, sources=%+v", added, a.sources)
	}
	if added := a.appendSources([]sourceImage{{Path: "later-duplicate.png", Hash: "new"}}); added != 0 {
		t.Fatalf("a later import added %d duplicate images", added)
	}
}

func TestTaskResultSummary(t *testing.T) {
	for _, tc := range []struct {
		statuses []tasks.Status
		want     string
	}{
		{[]tasks.Status{tasks.StatusSuccess}, "成功 1 · 失败 0 · 待验证 0 · 未完成 0"},
		{[]tasks.Status{tasks.StatusFailed}, "成功 0 · 失败 1 · 待验证 0 · 未完成 0"},
		{[]tasks.Status{tasks.StatusNeedsUser}, "成功 0 · 失败 0 · 待验证 1 · 未完成 0"},
		{[]tasks.Status{tasks.StatusSuccess, tasks.StatusFailed, tasks.StatusNeedsUser, tasks.StatusSkipped, tasks.StatusPending}, "成功 1 · 失败 1 · 待验证 1 · 未完成 2"},
	} {
		var queue []tasks.Task
		for _, status := range tc.statuses {
			queue = append(queue, tasks.Task{Status: status})
		}
		if got := taskResultSummary(queue); got != tc.want {
			t.Errorf("summary = %q, want %q", got, tc.want)
		}
	}
}

func TestWorkspaceResponsiveQueue(t *testing.T) {
	for _, width := range []int{1000, 1280} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("width=%d/dark=%v", width, dark), func(t *testing.T) {
				a := &App{
					engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 210,
					platDoubao: true, batchMode: batchImage, prompts: "保持构图，改为水彩风格", srcSel: 0,
					sources: []sourceImage{{Path: "风景.png", Hash: "source"}},
					queue:   []tasks.Task{{Index: 1, Platform: "豆包", Status: tasks.StatusPending, Images: []string{"风景.png"}, Prompt: "保持构图，改为水彩风格"}},
				}
				tt := ui.NewTester(a.view, width, 720)
				tt.SetDark(dark)
				for _, label := range []string{"xbs", "检查更新", "任务设置", "开始任务", "添加图片", "任务队列", "运行日志", "状态", "结果", "说明", "等待"} {
					assertVisible(t, tt, label, float32(width), 720)
				}
			})
		}
	}
}

func TestWorkspaceNeedsUserVisible(t *testing.T) {
	for _, width := range []int{1000, 1280} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("width=%d/dark=%v", width, dark), func(t *testing.T) {
				a := &App{
					engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 210,
					progress: 1, progText: "失败: 验证码需要手动处理",
					queue: []tasks.Task{{Index: 1, Platform: "豆包", Status: tasks.StatusNeedsUser}},
				}
				tt := ui.NewTester(a.view, width, 720)
				tt.SetDark(dark)
				for _, label := range []string{"待验证", verificationHint, "成功 0 · 失败 0 · 待验证 1 · 未完成 0", "开始任务", "任务队列", "运行日志"} {
					assertVisible(t, tt, label, float32(width), 720)
				}
				if tt.HasText("已结束") || tt.HasText("失败: 验证码需要手动处理") {
					t.Fatal("manual verification is shown as an ordinary failure or completion")
				}
			})
		}
	}
}

func TestSaveSettingsIncludesLatestBoundInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.SetInt("captcha_enabled", 0)
	st.Set("captcha_custom_url", "https://example.test")
	a := &App{
		store: st, manager: browser.NewManager(), engine: tasks.NewEngine(nil, nil, nil, ""),
		splitSz: 380, sourceHeight: 210, tab: 2,
	}
	a.loadSettings()
	tt := ui.NewTester(a.view, 1280, 840)
	if err := tt.Click("自动求解验证码"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("API 地址"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("https://example.test/latest")
	if !a.captchaEnabled || a.customURL != "https://example.test/latest" || !tt.Focused("API 地址") {
		t.Fatalf("bound inputs not current while focused: enabled=%v, URL=%q", a.captchaEnabled, a.customURL)
	}
	// OnQuit uses this callback before the store is closed; no blur or Start is required.
	a.saveSettings()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Get("captcha_enabled") != "1" || st.Get("captcha_custom_url") != "https://example.test/latest" {
		t.Fatalf("latest inputs were not persisted: enabled=%q, URL=%q", st.Get("captcha_enabled"), st.Get("captcha_custom_url"))
	}
}

func TestWatermarkOptionDefaultsPersistsAndReachesTask(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, manager: browser.NewManager()}
	a.loadSettings()
	if a.removeWatermark || a.taskOptions(nil, nil, nil).RemoveWatermark {
		t.Fatal("watermark repair must be disabled until explicitly enabled")
	}
	tt := ui.NewTester(a.taskPane, 380, 1400)
	if err := tt.Click("更多设置"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(time.Second)
	assertVisible(t, tt, "去水印（实验）", 380, 1400)
	if !tt.HasText("已适配豆包/千问方图。另存修复 PNG，保留原图；仅匹配已支持水印，复杂背景可能有痕迹。") {
		t.Fatal("watermark repair does not explain output preservation and limitations")
	}
	if err := tt.Click("去水印（实验）"); err != nil {
		t.Fatal(err)
	}
	if !a.removeWatermark || !a.taskOptions(nil, nil, nil).RemoveWatermark {
		t.Fatal("enabled UI option did not reach the task options")
	}
	a.running = true
	tt.Frame()
	if err := tt.Click("去水印（实验）"); err != nil {
		t.Fatal(err)
	}
	if !a.removeWatermark {
		t.Fatal("watermark option changed while a task was running")
	}
	a.saveSettings()
	restored := &App{store: st, manager: browser.NewManager()}
	restored.loadSettings()
	if st.Get("remove_watermark") != "1" || !restored.taskOptions(nil, nil, nil).RemoveWatermark {
		t.Fatal("enabled watermark option was not restored")
	}
	a.running = false
	tt.Frame()
	if err := tt.Click("去水印（实验）"); err != nil {
		t.Fatal(err)
	}
	a.saveSettings()
	restored.loadSettings()
	if st.Get("remove_watermark") != "0" || restored.taskOptions(nil, nil, nil).RemoveWatermark {
		t.Fatal("disabled watermark option was not restored")
	}
}

func TestWorkspaceActionsRespectRunningState(t *testing.T) {
	a := &App{
		engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 210,
		platDoubao: true, batchMode: batchImage, sources: []sourceImage{{Path: "source.png", Hash: "source"}}, srcSel: 0, running: true,
	}
	tt := ui.NewTester(a.view, 1280, 840)
	for _, label := range []string{"清空", "移除选中"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if len(a.sources) != 1 {
			t.Fatalf("%s changed sources while a task was running", label)
		}
	}
	a.running = false
	a.egoOpening = true
	tt.Frame()
	if err := tt.Click("开始任务"); err != nil {
		t.Fatal(err)
	}
	if tt.Focused("开始任务") || a.running {
		t.Fatal("start is enabled while an ego lite page is opening")
	}
	a.startTasks()
	a.egoOpening = false
	tt.Frame()
	if err := tt.Click("移除选中"); err != nil {
		t.Fatal(err)
	}
	if len(a.sources) != 0 || a.srcSel != -1 || !tt.HasText("待导入") {
		t.Fatal("removing the selected source did not restore the empty state")
	}
	// The disabled action must not reach startTasks without a source or store.
	if err := tt.Click("开始任务"); err != nil {
		t.Fatal(err)
	}
	a.progress = 1
	a.progText = "已完成"
	tt.Frame()
	if !tt.HasText("已结束") {
		t.Fatal("an idle completed task is shown as waiting to start")
	}
}

func TestLoadSettingsRefreshesBrowserRunningState(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b, err := st.AddBrowser("豆包", "工作账号", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(b.ID, true, true)
	a := &App{store: st, manager: browser.NewManager()}
	a.loadSettings()
	if len(a.browserRows) != 1 || a.browserRows[0].IsRunning {
		t.Fatalf("stored running flag survived a fresh manager: %+v", a.browserRows)
	}
}

func TestBrowserSelectionAndBusyActions(t *testing.T) {
	a := &App{
		engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 210,
		tab: 1, browserSel: -1, newBrowserPlat: "豆包", browserBackend: "Chrome",
		browserRows: []store.Browser{{ID: 1, Name: "工作账号", Platform: "豆包", Port: 9333}},
	}
	tt := ui.NewTester(a.view, 1000, 720)
	if err := tt.Click("删除选中"); err != nil {
		t.Fatal(err)
	}
	if tt.Focused("删除选中") {
		t.Fatal("delete is enabled without a selected browser")
	}
	if err := tt.Click("工作账号"); err != nil {
		t.Fatal(err)
	}
	if a.browserSel != 0 {
		t.Fatal("clicking a browser did not select it")
	}
	a.running = true
	a.browserRows[0].IsRunning = true
	tt.Frame()
	for _, label := range []string{"停止选中", "删除选中"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if tt.Focused(label) {
			t.Fatalf("%s is enabled during a task", label)
		}
	}
}

func TestBrowserBackendDefaultsSwitchesAndPersists(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, manager: browser.NewManager(), engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 210, tab: 1}
	a.loadSettings()
	if a.browserBackend != "ego lite" || st.Get("browser_backend") != "ego lite" {
		t.Fatalf("default browser backend = %q", a.browserBackend)
	}
	tt := ui.NewTester(a.view, 1000, 720)
	for _, label := range []string{"网页方式", "已启用 ego lite", "打开豆包网页", "打开千问网页"} {
		assertVisible(t, tt, label, 1000, 720)
	}
	_, chromeInstancesVisible := tt.Find("浏览器实例")
	if tt.HasText("启动选中") || chromeInstancesVisible {
		t.Fatal("ego lite mode shows Chrome instance controls")
	}
	if err := tt.Click("ego lite"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Chrome"); err != nil {
		t.Fatal(err)
	}
	if a.browserBackend != "Chrome" || st.Get("browser_backend") != "Chrome" || !tt.HasText("启动选中") {
		t.Fatal("selecting Chrome did not update the UI and saved backend")
	}
	a.running = true
	tt.Frame()
	if err := tt.Click("Chrome"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyHome)
	tt.Key(0, ui.KeyEnter)
	if a.browserBackend != "Chrome" || st.Get("browser_backend") != "Chrome" {
		t.Fatal("browser backend changed during a task")
	}
	a.running = false
	tt.Frame()
	if err := tt.Click("Chrome"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("ego lite"); err != nil {
		t.Fatal(err)
	}
	a.saveSettings()
	restored := &App{store: st, manager: browser.NewManager()}
	restored.loadSettings()
	if restored.browserBackend != "ego lite" || tt.HasText("启动选中") {
		t.Fatal("ego lite backend was not restored or Chrome controls remained visible")
	}
	a.egoOpening = true
	a.reportEgoOpen("豆包", errors.New("ego lite 未运行"))
	tt.Frame()
	if a.egoOpening || !tt.HasText("打开豆包失败：ego lite 未运行") {
		t.Fatal("the actual ego lite connection failure is not shown")
	}
}

func TestTextBatchStartsFromPromptAlone(t *testing.T) {
	a := &App{
		engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 210,
		platDoubao: true, batchMode: batchText,
		sources: []sourceImage{{Path: "kept.png", Hash: "kept"}},
	}
	if a.canStart() {
		t.Fatal("text mode is ready before a prompt")
	}
	tt := ui.NewTester(a.view, 1280, 840)
	if tt.HasText("添加图片") {
		t.Fatal("saved sources are shown while text mode is selected")
	}
	if err := tt.Click("提示词"); err != nil {
		t.Fatal(err)
	}
	tt.Type("画一只灯笼")
	if a.prompts != "画一只灯笼" || !a.canStart() || len(a.batchImages()) != 0 {
		t.Fatalf("text batch prompt=%q canStart=%v images=%v", a.prompts, a.canStart(), a.batchImages())
	}
	if err := tt.Click("图生图"); err != nil {
		t.Fatal(err)
	}
	if a.batchMode != batchImage || !a.canStart() {
		t.Fatal("image mode dropped the source that was already imported")
	}
	assertVisible(t, tt, "添加图片", 1280, 840)
	if got := a.batchImages(); len(got) != 1 || got[0] != "kept.png" {
		t.Fatalf("image mode images = %v", got)
	}
}

func TestBatchModeInfersAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "src.png")
	writeTinyPNG(t, path)
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fresh := &App{store: st, manager: browser.NewManager()}
	fresh.loadSettings()
	if fresh.batchMode != batchText {
		t.Fatal("a new config should open in text mode")
	}
	st.Set("source_images", path)
	withSources := &App{store: st, manager: browser.NewManager()}
	withSources.loadSettings()
	if withSources.batchMode != batchImage || len(withSources.sources) != 1 {
		t.Fatalf("sources should open as image mode: mode=%d sources=%d", withSources.batchMode, len(withSources.sources))
	}
	withSources.batchMode = batchText
	withSources.saveSettings()
	restored := &App{store: st, manager: browser.NewManager()}
	restored.loadSettings()
	if restored.batchMode != batchText || len(restored.sources) != 1 {
		t.Fatal("an explicit text mode was inferred away, or sources were dropped")
	}
}

func TestOpenSelectedPagesSwitchesChromeTab(t *testing.T) {
	a := &App{browserBackend: "Chrome", platDoubao: true}
	a.openSelectedPages()
	if a.tab != 1 || a.egoOpening {
		t.Fatalf("chrome open: tab=%d opening=%v", a.tab, a.egoOpening)
	}
	ego := &App{browserBackend: "ego lite"}
	ego.openSelectedPages()
	if ego.egoOpening || ego.egoMessage == "" {
		t.Fatal("ego open ran without a selected platform")
	}
}

func writeTinyPNG(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
}

func assertVisible(t *testing.T, tt *ui.Tester, label string, width, height float32) ui.Rect {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok || r.W <= 0 || r.H <= 0 || r.X < 0 || r.Y < 0 || r.X+r.W > width || r.Y+r.H > height {
		t.Fatalf("%q is outside the window: %+v (found=%v)", label, r, ok)
	}
	return r
}
