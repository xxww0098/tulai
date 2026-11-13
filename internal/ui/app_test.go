package ui

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestWorkspaceAndSettingsStayVisible(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), platformAuto: true}
	tt := ui.NewTester(a.view, 1280, 840)
	for _, label := range []string{"图片生成", "自由生图", "批量洗图", "浏览器", "提示词", "设置", "开始出图", "提示词内容", "拖入或点击添加图片", "检查更新", "设置向导"} {
		assertVisible(t, tt, label, 1280, 840)
	}
	if tt.HasText("更多设置") || tt.HasText("输出目录") || tt.HasText("每日上限") {
		t.Fatal("生图页还留着配置")
	}
	prompt := assertVisible(t, tt, "提示词", 1280, 840)
	drop := assertVisible(t, tt, "拖入或点击添加图片", 1280, 840)
	start := assertVisible(t, tt, "开始出图", 1280, 840)
	if prompt.Y > drop.Y {
		t.Fatal("提示词应该在参考图上面")
	}
	if start.Y < drop.Y+drop.H+24 {
		t.Fatalf("start button crowds the drop zone: drop=%+v start=%+v", drop, start)
	}
	if start.X < drop.X+80 {
		t.Fatalf("start button is not to the right: %+v", start)
	}
	if tt.HasText("清空") {
		t.Fatal("empty drop zone shows the filled-state importer")
	}
	for _, tab := range []struct{ label, want string }{
		{"浏览器", "Camoufox 账号环境"},
		{"提示词", "保存提示词"},
		{"设置", "账号与并发"},
		{"图片生成", "开始出图"},
		{"批量洗图", "开始洗图"},
		{"自由生图", "开始出图"},
	} {
		if err := tt.Click(tab.label); err != nil {
			t.Fatal(err)
		}
		tt.Advance(motionUI)
		assertVisible(t, tt, tab.want, 1280, 840)
	}
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	for _, section := range []struct {
		tab    string
		labels []string
	}{
		{"输出", []string{"输出目录", "生图和洗图按日期放进各自的文件夹。"}},
		{"生成规则", []string{"审核改写词", "去水印（实验）"}},
		{"验证码 / 高级", []string{"自动求解验证码", "跟随系统"}},
	} {
		if err := tt.Click(section.tab); err != nil {
			t.Fatal(err)
		}
		for _, label := range section.labels {
			scrollIntoView(t, tt, label, 1280, 840)
		}
	}
	if tt.HasText("更多设置") || tt.HasText("保存方式") || tt.HasText("命名规则") {
		t.Fatal("设置页还留着已经去掉的保存选项")
	}
}

func TestSettingsShowsBothDayFolders(t *testing.T) {
	root := t.TempDir()
	day := time.Now().Format("2006-01-02")
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), tab: pageSettings, outputPath: root, platformAuto: true}
	tt := ui.NewTester(a.view, 1280, 900)
	gen := "生图保存到 " + filepath.Join(root, "生图", day)
	wash := "洗图保存到 " + filepath.Join(root, "洗图", day)
	if !tt.HasText(gen) || !tt.HasText(wash) {
		t.Fatalf("missing day folders: %v", tt.Texts())
	}
}

func TestSettingsTabsSwitchAndKeepValues(t *testing.T) {
	a := &App{tab: pageSettings, outputPath: "/tmp/original", platformAuto: true, downloadConc: 2}
	tt := ui.NewTester(a.view, 760, 900)
	for _, label := range []string{"输出", "账号与并发", "输出目录"} {
		assertVisible(t, tt, label, 760, 900)
	}
	if tt.HasText("每个站点同时下载") || tt.HasText("每日上限") {
		t.Fatal("未选中的设置不应显示")
	}
	if err := tt.Click("输出目录"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("/tmp/changed")
	if err := tt.Click("输出"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyRight)
	if a.settingsTab != settingsDownload || !tt.HasText("每个站点同时下载") || tt.HasText("输出目录") || tt.HasText("每日上限") {
		t.Fatalf("下载标签内容不正确: tab=%d", a.settingsTab)
	}
	tt.Key(0, ui.KeyRight)
	if a.settingsTab != settingsAccount || !tt.HasText("每日上限") || tt.HasText("每个站点同时下载") {
		t.Fatalf("账号标签内容不正确: tab=%d", a.settingsTab)
	}
	if err := tt.Click("自动使用已登录的平台"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("账号与并发"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyLeft)
	if err := tt.Click("每个站点同时下载"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("5")
	if err := tt.Click("输出"); err != nil {
		t.Fatal(err)
	}
	if a.settingsTab != settingsOutput || a.outputPath != "/tmp/changed" || a.platformAuto || a.downloadConc != 5 || !tt.HasText("输出目录") {
		t.Fatalf("切换后设置丢失: tab=%d path=%q auto=%v downloads=%v", a.settingsTab, a.outputPath, a.platformAuto, a.downloadConc)
	}
}

func TestEverySettingsSectionHasATab(t *testing.T) {
	sections := []struct{ tab, content string }{
		{"输出", "输出目录"},
		{"下载", "每个站点同时下载"},
		{"账号与并发", "每日上限"},
		{"Antigravity", "agy 路径"},
		{"生成规则", "审核改写词"},
		{"验证码 / 高级", "自动求解验证码"},
		{"更新", "自动检查更新"},
	}
	for _, width := range []int{480, 760} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			a := &App{tab: pageSettings, platformAuto: true}
			tt := ui.NewTester(a.view, width, 900)
			for _, section := range sections {
				assertVisible(t, tt, section.tab, float32(width), 900)
			}
			if err := tt.Click("输出"); err != nil {
				t.Fatal(err)
			}
			for i, section := range sections {
				if i > 0 {
					tt.Key(0, ui.KeyRight)
				}
				if a.settingsTab != i {
					t.Fatalf("%s: tab=%d, want %d", section.tab, a.settingsTab, i)
				}
				for j, other := range sections {
					if tt.HasText(other.content) != (i == j) {
						t.Fatalf("%s: 内容 %q 的显示状态不正确", section.tab, other.content)
					}
				}
			}
		})
	}
}

func TestSettingsTabsKeepAgyAndProxySaved(t *testing.T) {
	a, st := newDownloadApp(t)
	a.tab = pageSettings
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("Antigravity"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("模型"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("test-model")
	if err := tt.Click("输出"); err != nil {
		t.Fatal(err)
	}
	if got := st.Get("agy_model"); got != "test-model" {
		t.Fatalf("切换后模型未保存: %q", got)
	}
	if err := tt.Click("验证码 / 高级"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("自定义代理"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyTab)
	tt.Type("http://127.0.0.1:7890")
	if err := tt.Click("输出"); err != nil {
		t.Fatal(err)
	}
	if got := st.Get("proxy_server"); got != "http://127.0.0.1:7890" {
		t.Fatalf("切换后代理未保存: %q", got)
	}
}

func TestTextOnlyStartWithoutSources(t *testing.T) {
	fakeInstalledCamoufox(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{
		store: st, manager: browser.NewManager(), engine: tasks.NewEngine(st, nil, nil, ""),
		browserBackend: browser.KindCamoufox, platformAuto: true,
		prompts: "画一只猫\n\n画一条鱼", outputPath: t.TempDir(),
	}
	tt := ui.NewTester(a.view, 1280, 840)
	for _, label := range []string{"开始出图", "拖入或点击添加图片", "检查更新"} {
		if !tt.HasText(label) {
			t.Fatalf("缺少文生图说明 %q", label)
		}
	}
	if tt.HasText("清空") {
		t.Fatal("没有源图时不应先要求导入")
	}
	if err := tt.Click("开始出图"); err != nil {
		t.Fatal(err)
	}
	if a.running || a.progText != "" || a.wizardOn {
		t.Fatal("没有可用账号时应留在工作台")
	}
	row, err := st.AddBrowser("豆包", "工作", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	tt.Frame()
	if !a.canStart() {
		t.Fatal("只有提示词和已登录账号时应可以出图")
	}
	if len(a.batchImages()) != 0 || len(splitLines(a.prompts)) != 2 {
		t.Fatalf("文生图输入: prompts=%q images=%v", a.prompts, a.batchImages())
	}
	if err := tt.Click("开始出图"); err != nil {
		t.Fatal(err)
	}
	finishRecoveryRun(t, a)
	view := a.engine.View()
	if view == nil || a.wizardOn {
		t.Fatalf("只有提示词时未创建任务: view=%v wizard=%v", view, a.wizardOn)
	}
	queue := view.Snapshot()
	want := []string{"画一只猫", "画一条鱼"}
	if len(queue) != len(want) {
		t.Fatalf("文生图任务数量不符: %+v", queue)
	}
	for i, task := range queue {
		if task.Prompt != want[i] || len(task.Images) != 0 || task.Attempts != 1 || task.BrowserID != row.ID {
			t.Fatalf("文生图未按原提示词执行: %+v", queue)
		}
	}
}

func TestMissingAccountDoesNotOpenWizardWhenStartingOrRetrying(t *testing.T) {
	for _, page := range []int{pageHome, pageWash} {
		t.Run(pageLabel(page), func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			failed := tasks.Task{Index: 1, Platform: "豆包", Status: tasks.StatusFailed, Prompt: "画一只猫"}
			a := &App{
				store: st, engine: tasks.NewEngine(st, nil, nil, ""), platformAuto: true,
				tab: page, statePage: page, prompts: "画一只猫", queue: []tasks.Task{failed},
			}
			if page == pageWash {
				a.sources = []sourceImage{{Path: "input.png"}}
			}
			tt := ui.NewTester(a.view, 1280, 900)
			if _, ok := tt.Find("去设置"); ok {
				t.Fatal("提示条已移除，不应再有「去设置」")
			}
			a.startTasks()
			if a.wizardOn || a.running {
				t.Fatal("开始任务不应打开向导或开始运行")
			}
			a.retryTasks([]tasks.Task{failed})
			if a.wizardOn || a.running || a.progText != "没有可用账号，请先登录后重试" || len(a.queue) != 1 || a.queue[0].Status != tasks.StatusFailed {
				t.Fatalf("重试状态: wizard=%v running=%v note=%q queue=%+v", a.wizardOn, a.running, a.progText, a.queue)
			}
			tt.Frame()
			if !tt.HasText(a.progText) {
				t.Fatal("重试失败时应提示先登录")
			}
		})
	}
}

func TestAppendSourcesDeduplicatesAcrossImports(t *testing.T) {
	a := &App{sources: []sourceImage{{Path: "first.png", Hash: "same"}}}
	if added := a.appendSources([]sourceImage{
		{Path: "duplicate.png", Hash: "same"},
		{Path: "second.png", Hash: "new"},
		{Path: "batch-duplicate.png", Hash: "new"},
	}, false); added != 1 || len(a.sources) != 2 || a.sources[1].Path != "second.png" {
		t.Fatalf("unexpected imported sources: added=%d, sources=%+v", added, a.sources)
	}
	if added := a.appendSources([]sourceImage{{Path: "later-duplicate.png", Hash: "new"}}, false); added != 0 {
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
				for _, label := range []string{"检查更新", "服务商选择", "本次发送至：豆包", "当前服务商：豆包", "开始出图", "出图进度", "排队中", "添加图片", "运行日志"} {
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
				for _, label := range []string{"待验证", verificationHint, FormatProgressTally(0, 0, 0, 0, 1, 0, 0, 0, a.liveParallel()), "开始出图", "出图进度", "重试"} {
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
		tab: pageSettings, settingsTab: settingsAdvanced,
	}
	a.loadSettings()
	tt := ui.NewTester(a.view, 1280, 1800)
	scrollIntoView(t, tt, "自动求解验证码", 1280, 1800)
	if err := tt.Click("自动求解验证码"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("免费离线识别"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("自定义视觉模型"); err != nil {
		t.Fatal(err)
	}
	if a.captchaBackend != "custom" {
		t.Fatalf("backend = %q", a.captchaBackend)
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
	a.tab = pageSettings
	a.settingsTab = settingsRules
	tt := ui.NewTester(a.view, 760, 900)
	scrollIntoView(t, tt, "去水印（实验）", 760, 900)
	assertVisible(t, tt, "去水印（实验）", 760, 900)
	if !tt.HasText("豆包自动去水印后保存；勾选后千问方图另存修复 PNG。未匹配时保留原图，复杂背景可能有痕迹。") {
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
		engine: tasks.NewEngine(nil, nil, nil, ""), platformAuto: true,
		platDoubao: true, sources: []sourceImage{{Path: "source.png", Hash: "source"}}, srcSel: 0, running: true,
	}
	tt := ui.NewTester(a.view, 1280, 840)
	for _, label := range []string{"清空", "移除"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		if len(a.sources) != 1 {
			t.Fatalf("%s changed sources while a task was running", label)
		}
	}
	a.running = false
	tt.Frame()
	if err := tt.Click("移除"); err != nil {
		t.Fatal(err)
	}
	if len(a.sources) != 0 || !tt.HasText("拖入或点击添加图片") {
		t.Fatal("removing the source did not restore the empty drop zone")
	}
	if err := tt.Click("开始出图"); err != nil {
		t.Fatal(err)
	}
	a.progress = 1
	a.progText = "已完成"
	tt.Frame()
	if !tt.HasText("已完成") {
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
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "工作账号", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	a := &App{
		store:  st,
		engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 210,
		tab: pageBrowser, browserSel: -1, newBrowserPlat: "豆包", browserBackend: "Chrome",
	}
	tt := ui.NewTester(a.view, 1000, 720)
	if tt.HasText("删除当前") {
		t.Fatal("delete should be hidden without a selected browser")
	}
	if err := tt.Click("工作账号"); err != nil {
		t.Fatal(err)
	}
	if a.browserSel != 0 {
		t.Fatal("clicking a browser did not select it")
	}
	a.running = true
	st.SetRunning(row.ID, true)
	tt.Frame()
	for _, label := range []string{"停止 工作账号", "删除当前"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		rows := st.Browsers()
		if len(rows) != 1 || !rows[0].IsRunning {
			t.Fatalf("%s changed an account during a task: %+v", label, rows)
		}
	}
}

func TestBrowserBackendMigratesToCamoufoxAndPersists(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Edge")
	a := &App{store: st, manager: browser.NewManager(), engine: tasks.NewEngine(nil, nil, nil, ""), splitSz: 380, sourceHeight: 210, tab: pageBrowser}
	a.loadSettings()
	if a.browserBackend != browser.KindCamoufox || st.Get("browser_backend") != browser.KindCamoufox {
		t.Fatal("legacy browser was not migrated")
	}
	tt := ui.NewTester(a.view, 1000, 720)
	if !tt.HasText("Camoufox 账号环境") {
		t.Fatalf("missing Camoufox: %v", tt.Texts())
	}
	for _, old := range []string{"Chrome", "Edge", "下载内置浏览器"} {
		if tt.HasText(old) {
			t.Fatalf("old browser choice: %s", old)
		}
	}
	a.saveSettings()
	restored := &App{store: st, manager: browser.NewManager()}
	restored.loadSettings()
	if restored.browserBackend != browser.KindCamoufox {
		t.Fatal("Camoufox not persisted")
	}
	a.camoufoxBusy = true
	a.camoufoxStatus = "正在下载 Camoufox 内核…"
	tt.Frame()
	if !tt.HasText(a.camoufoxStatus) {
		t.Fatal("install status missing")
	}
}

func TestTextBatchStartsFromPromptAlone(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "工作", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	a := &App{
		store: st, engine: tasks.NewEngine(st, nil, nil, ""), platformAuto: true,
	}
	if a.canStart() {
		t.Fatal("text mode is ready before a prompt")
	}
	tt := ui.NewTester(a.view, 1280, 840)
	if tt.HasText("清空") {
		t.Fatal("an empty drop zone shows the filled-state importer")
	}
	if err := tt.Click("提示词内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("画一只灯笼")
	if a.prompts != "画一只灯笼" || !a.canStart() || len(a.batchImages()) != 0 {
		t.Fatalf("text batch prompt=%q canStart=%v images=%v", a.prompts, a.canStart(), a.batchImages())
	}
	a.appendSources([]sourceImage{{Path: "kept.png", Hash: "kept"}}, false)
	tt.Frame()
	if !a.canStart() {
		t.Fatal("adding a source blocked start")
	}
	assertVisible(t, tt, "添加图片", 1280, 840)
	if got := a.batchImages(); len(got) != 1 || got[0] != "kept.png" {
		t.Fatalf("images = %v", got)
	}
	a.sources = nil
	tt.Frame()
	if !a.canStart() || len(a.batchImages()) != 0 {
		t.Fatal("clearing images did not fall back to the prompt")
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

func TestOpenSelectedPagesSwitchesBrowserTab(t *testing.T) {
	for _, backend := range []string{"Chrome", "Edge"} {
		a := &App{browserBackend: backend, platDoubao: true}
		a.openSelectedPages()
		if a.tab != pageBrowser {
			t.Fatalf("%s open: tab=%d", backend, a.tab)
		}
	}
}

func TestExtensionTokenVisibleCopyAndRegenerate(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set(extbridge.SettingExtToken, "  ")
	st.Set(extbridge.SettingExtPort, "18766")
	a := &App{
		store: st, manager: browser.NewManager(), engine: tasks.NewEngine(st, nil, nil, ""),
		splitSz: 380, sourceHeight: 210, tab: pageBrowser,
	}
	a.loadSettings()
	if extbridge.WeakToken(a.extToken) || a.extToken != st.Get(extbridge.SettingExtToken) || len(a.extToken) != 32 || a.extPort != "18766" {
		t.Fatalf("blank token should be replaced with a strong token, port=%q token=%q", a.extPort, a.extToken)
	}
	tt := ui.NewTester(func(c *ui.Context) { a.extensionBridgeSection(c) }, 1000, 720)
	for _, label := range []string{"页面桥", "令牌", "复制令牌", "重新生成"} {
		assertVisible(t, tt, label, 1000, 720)
	}
	if !tt.HasText("自动对接未启动，可复制令牌手动填写") {
		t.Fatal("pair status missing")
	}
	if !tt.HasText("端口 18766") {
		t.Fatal("stored bridge port is not shown")
	}
	if err := tt.Click("复制令牌"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != a.extToken || !tt.HasText("已复制令牌") {
		t.Fatalf("copy: clipboard=%q", tt.Clipboard())
	}

	st.Set(extbridge.SettingExtToken, "from-desktop")
	a.loadSettings()
	tt.Frame()
	if a.extToken != "from-desktop" {
		t.Fatalf("stored token was replaced: %q", a.extToken)
	}
	if err := tt.Click("复制令牌"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != "from-desktop" {
		t.Fatalf("copy stored token: %q", tt.Clipboard())
	}

	before := a.extToken
	if err := tt.Click("重新生成"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.extToken == before || a.extToken != st.Get(extbridge.SettingExtToken) || len(a.extToken) != 32 {
		t.Fatalf("regenerate token=%q stored=%q", a.extToken, st.Get(extbridge.SettingExtToken))
	}
	if !tt.HasText("已生成新令牌。扩展会自动对接；若弹出页仍是手动配对，请复制过去或点「恢复自动对接」") {
		t.Fatal("regenerate did not explain auto-pair")
	}
	if err := tt.Click("复制令牌"); err != nil {
		t.Fatal(err)
	}
	if tt.Clipboard() != a.extToken {
		t.Fatalf("copy after regenerate: clipboard=%q token=%q", tt.Clipboard(), a.extToken)
	}

	kept := a.extToken
	a.running = true
	tt.Frame()
	if err := tt.Click("重新生成"); err != nil {
		t.Fatal(err)
	}
	if tt.Focused("重新生成") || a.extToken != kept || st.Get(extbridge.SettingExtToken) != kept {
		t.Fatal("token changed while a task was running")
	}
}

func TestCamoufoxQianwenWithoutAccountCannotStart(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{
		store: st, manager: browser.NewManager(), engine: tasks.NewEngine(st, nil, nil, ""),
		browserBackend: browser.KindCamoufox, platformAuto: true,
		detectedNote: "Camoufox 已就绪", camoufoxStatus: "Camoufox 已就绪", camoufoxReady: true,
		platQianwen: true, prompts: "画一只猫", outputPath: t.TempDir(),
	}
	tt := ui.NewTester(a.view, 1280, 840)
	const hint = "千问还没有浏览器，请先在浏览器管理中添加"
	if a.canStart() {
		t.Fatal("没有登录账号时不应直接出图")
	}
	if err := tt.Click("开始出图"); err != nil {
		t.Fatal(err)
	}
	if a.running || a.progText != "" || a.wizardOn {
		t.Fatal("没有可用账号时开始按钮应留在工作台")
	}
	tt.Frame()
	if err := tt.Click("浏览器"); err != nil {
		t.Fatal(err)
	}
	if a.tab != pageBrowser || !tt.HasText(hint) {
		t.Fatal("browser page did not show the empty-state hint")
	}

	row, err := st.AddBrowser("千问", "网页", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	a.tab = pageHome
	a.stageTab = 1
	a.stagePicked = true
	tt.Frame()
	if !a.canStart() {
		t.Fatal("登录千问后仍不能出图")
	}

	st.Set(extbridge.SettingBrowserBackend, "extension")
	tt.Frame()
	if a.canStart() {
		t.Fatal("扩展方式仍把本机浏览器当成可用账号")
	}
	if _, err := st.UpsertExtensionAccount("千问", "profile-1", "扩展千问"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertExtensionAccount("豆包", "profile-2", "扩展豆包"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.canStart() {
		t.Fatal("只连上、还没登录的扩展账号不能出图")
	}
	if _, err := st.SaveLoggedInExtension("千问", "profile-1", "扩展千问"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.canStart() {
		t.Fatal("已登录的千问扩展账号不能出图")
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
