// Package ui mygo 原生界面：左侧是任务设置、浏览器和验证码，
// 右侧是模式、提示词、源图、队列、日志和开始停止。
package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"xbs/internal/browser"
	"xbs/internal/captcha"
	"xbs/internal/extbridge"
	"xbs/internal/images"
	"xbs/internal/logging"
	"xbs/internal/store"
	"xbs/internal/tasks"
)

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func joinPaths(ps []string) string { return strings.Join(ps, "\n") }

func mustWd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

const (
	batchText = iota
	batchImage
)

// App 应用状态。
type App struct {
	mu sync.Mutex

	store          *store.Store
	manager        *browser.Manager
	engine         *tasks.Engine
	window         *mygo.Window
	dataRoot       string
	browserBackend string

	// 任务设置（与 settings 表一一对应）
	platDoubao      bool
	platQianwen     bool
	sendStrategy    string
	concurrency     float64
	retryCount      float64
	sendWait        float64
	waitBefore      float64
	waitResult      bool
	waitResultS     float64
	taskSwitch      bool
	switchCount     float64
	startBrowserIdx float64
	saveMode        string
	namingRule      string
	filePrefix      string
	prefixOn        bool
	fileSuffix      string
	suffixOn        bool
	outputPath      string
	removeWatermark bool
	prompts         string
	promptSuffix    string
	useSuffix       bool
	batchMode       int // batchText 文生图 / batchImage 图生图
	moreSettings    bool

	// 验证码设置
	captchaEnabled bool
	captchaBackend string
	customURL      string
	customKey      string
	customModel    string
	ikunocrKey     string

	// 源图片
	sources []sourceImage
	srcGrid ui.GridState
	srcSel  int

	// 浏览器
	browserRows    []store.Browser
	browserList    ui.ListState
	browserSel     int
	newBrowserPlat string
	newBrowserName string
	extToken       string
	extPort        string
	extTokenNote   string

	// 队列与日志
	queue    []tasks.Task
	queueTbl ui.ListState
	logs     []logging.Entry
	logList  ui.ListState
	progress float64
	progText string
	running  bool

	tab          int
	splitSz      float32
	sourceHeight float32
	updateBusy   bool
}

type sourceImage struct {
	Path  string
	Thumb *ui.Bitmap
	Hash  string
}

// Run 启动应用（主 goroutine）。
func Run(st *store.Store, bm *browser.Manager, eng *tasks.Engine, dataRoot string) {
	a := &App{
		store:        st,
		manager:      bm,
		engine:       eng,
		dataRoot:     dataRoot,
		splitSz:      380,
		sourceHeight: 210,
		srcSel:       -1,
		browserSel:   -1,
	}
	a.loadSettings()
	mygo.App.SetMenu(a.installMenu())
	offQuit := mygo.App.OnQuit(a.saveSettings)
	defer offQuit()
	logging.Log.SetSink(func(e logging.Entry) {
		if a.window == nil {
			return
		}
		a.window.Update(func() {
			a.mu.Lock()
			a.logs = append(a.logs, e)
			if len(a.logs) > 500 {
				a.logs = a.logs[len(a.logs)-400:]
			}
			a.mu.Unlock()
		})
	})
	mygo.App.WhenReady(func() {
		a.window = mygo.NewWindow(mygo.WindowOptions{
			Title:   "图来",
			Width:   1280,
			Height:  840,
			Content: ui.View(a.view),
		})
		applyAppIcon(a.window)
	})
	if err := mygo.App.Run(); err != nil {
		panic(err)
	}
}

// loadSettings 从配置库读入 UI 状态。
func (a *App) loadSettings() {
	s := a.store
	a.browserBackend = pickStr(s.Get("browser_backend"), "Chrome", []string{"Chrome", "Edge"})
	s.Set("browser_backend", a.browserBackend)
	a.extToken = extbridge.NormalizeExtToken(s.Get(extbridge.SettingExtToken))
	a.extPort = extbridge.NormalizeExtPort(s.Get(extbridge.SettingExtPort))
	a.platDoubao = containsStr(s.Platforms(), "豆包")
	a.platQianwen = containsStr(s.Platforms(), "千问")
	a.sendStrategy = pickStr(s.Get("send_strategy"), "顺序循环", []string{"顺序循环", "随机循环"})
	a.concurrency = float64(clampInt(s.GetInt("concurrency"), 1, 16))
	a.retryCount = float64(clampInt(s.GetInt("retry_count"), 0, 20))
	a.sendWait = float64(clampInt(s.GetInt("send_wait_seconds"), 0, 600))
	a.waitBefore = float64(clampInt(s.GetInt("wait_before_sending"), 0, 60))
	a.waitResult = s.GetBool("wait_result")
	a.waitResultS = float64(clampInt(s.GetInt("wait_result_seconds"), 10, 1800))
	a.taskSwitch = s.GetBool("task_switching")
	a.switchCount = float64(clampInt(s.GetInt("task_switching_count"), 1, 9999))
	a.startBrowserIdx = float64(clampInt(s.GetInt("start_browser_index"), 1, 999))
	a.saveMode = pickStr(s.Get("save_mode"), "单文件夹汇总", []string{"单文件夹汇总", "按源图片分文件夹"})
	a.namingRule = pickStr(s.Get("naming_rule"), "原文件名", []string{"原文件名", "时间戳", "序号"})
	a.filePrefix = s.Get("file_prefix")
	a.prefixOn = s.GetBool("file_prefix_enabled")
	a.fileSuffix = s.Get("file_suffix")
	a.suffixOn = s.GetBool("file_suffix_enabled")
	a.outputPath = s.Get("output_path")
	a.removeWatermark = s.GetBool("remove_watermark")
	a.prompts = s.Get("prompts")
	a.promptSuffix = s.Get("prompt_suffix")
	a.useSuffix = s.GetBool("use_suffix")

	a.captchaEnabled = s.GetBool("captcha_enabled")
	a.captchaBackend = pickStr(s.Get("captcha_backend"), "free", []string{"free", "custom", "ikunocr"})
	a.customURL = s.Get("captcha_custom_url")
	a.customKey = s.Get("captcha_custom_key")
	a.customModel = s.Get("captcha_custom_model")
	a.ikunocrKey = s.Get("captcha_ikunocr_key")

	a.refreshBrowsers()
	a.newBrowserPlat = "豆包"
	a.logs = logging.Log.Entries()
	// 恢复上次会话的源图片列表
	for _, p := range splitLines(s.Get("source_images")) {
		data, err := os.ReadFile(p)
		if err != nil || !images.IsValidImageBytes(data) {
			continue
		}
		thumb, _ := images.Thumb(p, 200)
		var bmp *ui.Bitmap
		if thumb != nil {
			bmp = ui.NewBitmap(thumb)
		}
		a.sources = append(a.sources, sourceImage{Path: p, Thumb: bmp, Hash: images.SHA256(data)})
	}
	switch s.Get("batch_mode") {
	case "image":
		a.batchMode = batchImage
	case "text":
		a.batchMode = batchText
	default:
		if len(a.sources) > 0 {
			a.batchMode = batchImage
		} else {
			a.batchMode = batchText
		}
	}
}

// saveSettings 写回配置库。
func (a *App) saveSettings() {
	s := a.store
	s.Set("browser_backend", a.browserBackend)
	a.extToken = extbridge.NormalizeExtToken(a.extToken)
	a.extPort = extbridge.NormalizeExtPort(a.extPort)
	s.Set(extbridge.SettingExtToken, a.extToken)
	s.Set(extbridge.SettingExtPort, a.extPort)
	var plats []string
	if a.platDoubao {
		plats = append(plats, "豆包")
	}
	if a.platQianwen {
		plats = append(plats, "千问")
	}
	s.SetPlatforms(plats)
	s.Set("send_strategy", a.sendStrategy)
	s.SetInt("concurrency", int(a.concurrency))
	s.SetInt("retry_count", int(a.retryCount))
	s.SetInt("send_wait_seconds", int(a.sendWait))
	s.SetInt("wait_before_sending", int(a.waitBefore))
	s.SetInt("wait_result", boolInt(a.waitResult))
	s.SetInt("wait_result_seconds", int(a.waitResultS))
	s.SetInt("task_switching", boolInt(a.taskSwitch))
	s.SetInt("task_switching_count", int(a.switchCount))
	s.SetInt("start_browser_index", int(a.startBrowserIdx))
	s.Set("save_mode", a.saveMode)
	s.Set("naming_rule", a.namingRule)
	s.Set("file_prefix", a.filePrefix)
	s.SetInt("file_prefix_enabled", boolInt(a.prefixOn))
	s.Set("file_suffix", a.fileSuffix)
	s.SetInt("file_suffix_enabled", boolInt(a.suffixOn))
	s.Set("output_path", a.outputPath)
	s.SetInt("remove_watermark", boolInt(a.removeWatermark))
	s.Set("prompts", a.prompts)
	s.Set("prompt_suffix", a.promptSuffix)
	s.SetInt("use_suffix", boolInt(a.useSuffix))
	if a.batchMode == batchImage {
		s.Set("batch_mode", "image")
	} else {
		s.Set("batch_mode", "text")
	}
	s.SetInt("captcha_enabled", boolInt(a.captchaEnabled))
	s.Set("captcha_backend", a.captchaBackend)
	s.Set("captcha_custom_url", a.customURL)
	s.Set("captcha_custom_key", a.customKey)
	s.Set("captcha_custom_model", a.customModel)
	s.Set("captcha_ikunocr_key", a.ikunocrKey)
	// 源图列表持久化
	var paths []string
	for _, si := range a.sources {
		paths = append(paths, si.Path)
	}
	s.Set("source_images", joinPaths(paths))
}

// captchaConfig 由当前 UI 状态构造求解配置。
func (a *App) captchaConfig() captcha.Config {
	return captcha.Config{
		Backend:       a.captchaBackend,
		CustomURL:     a.customURL,
		CustomKey:     a.customKey,
		CustomModel:   a.customModel,
		CustomTimeout: 30,
		IkunocrKey:    a.ikunocrKey,
		IkunocrTimeut: 30,
	}
}

// view 主视图。
func (a *App) view(c *ui.Context) {
	width, _ := c.Size()
	sidebarWidth := min(a.splitSz, max(300, width*0.32))
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(14, 20).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "xbs").FontSize(22).Bold()
			subtitle := "图来 · 豆包 / 千问"
			if v := mygo.App.Version(); v != "" {
				subtitle += " · " + v
			}
			ui.Text(c, subtitle).FontSize(13).TextColor(c.Theme().TextMuted).Grow(1).MinWidth(0)
			ui.Button(c, "检查更新").Disabled(a.updateBusy).OnClick(func() { a.checkForUpdates() })
		})
		ui.Divider(c)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Width(sidebarWidth).FillHeight().MinHeight(0).Shrink(0).Padding(14).Gap(12).Background(c.Theme().Surface).Children(func() {
				a.settingsPane(c)
			})
			ui.Divider(c).Width(1).FillHeight()
			ui.Column(c).Grow(1).FillHeight().MinWidth(0).MinHeight(0).Padding(16).Gap(12).Children(func() {
				a.workspace(c)
			})
		})
	})
}

// workspace 右侧：控制条 + 源图 + 队列 + 日志。
func (a *App) workspace(c *ui.Context) {
	items := a.queue
	if view := a.engine.View(); view != nil {
		items = view.Snapshot()
	}
	needsUser := false
	for _, task := range items {
		if task.Status == tasks.StatusNeedsUser {
			needsUser = true
			break
		}
	}
	prompts := splitLines(a.prompts)
	ready := fmt.Sprintf("%d 条提示词", len(prompts))
	if a.batchMode == batchImage {
		ready = fmt.Sprintf("%d 张源图 · %d 条提示词", len(a.sources), len(prompts))
	} else {
		ready = "文生图 · " + ready
	}
	status, hint := "待开始", ready
	missingBrowsers := a.missingBrowserNames()
	switch {
	case a.running:
		status = "进行中"
	case a.progress > 0 || a.engine.View() != nil:
		status = "已结束"
	case !a.platDoubao && !a.platQianwen:
		status, hint = "待选平台", "勾选豆包或千问"
	case a.batchMode == batchImage && len(a.sources) == 0:
		status, hint = "待导入", "添加源图，或切回文生图"
	case a.batchMode == batchText && len(prompts) == 0:
		status, hint = "待写提示词", "写下提示词即可开始，不用源图"
	case len(missingBrowsers) > 0:
		status, hint = "待添加浏览器", missingBrowserHint(missingBrowsers)
	}
	if a.progText != "" && status != "待添加浏览器" {
		hint = a.progText
	}
	if needsUser {
		status, hint = string(tasks.StatusNeedsUser), taskResultSummary(items)
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Segmented(c, &a.batchMode, "文生图", "图生图").Disabled(a.running).OnChange(func() {
			if !a.running && a.progress == 0 {
				a.progText = ""
			}
		})
		ui.Checkbox(c, &a.platDoubao, "豆包").Disabled(a.running)
		ui.Checkbox(c, &a.platQianwen, "千问").Disabled(a.running)
		ui.Badge(c, status)
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, hint).FontSize(12).TextColor(c.Theme().TextMuted).SingleLine().Grow(1).MinWidth(0).Tooltip(hint)
		ui.Button(c, "管理浏览器").Disabled(a.running).OnClick(func() { a.openSelectedPages() })
		if a.running {
			ui.Button(c, "停止任务").OnClick(func() { a.stopTasks() })
		} else {
			ui.PrimaryButton(c, "开始任务").Disabled(!a.canStart()).OnClick(func() { a.startTasks() })
		}
	})
	ui.TextArea(c, &a.prompts).Height(64).Label("提示词").Placeholder("每行一条提示词")
	if needsUser {
		ui.Text(c, verificationHint).FontSize(12).TextColor(c.Theme().Warning)
	}
	if a.running || a.progress > 0 {
		ui.Progress(c, a.progress).FillWidth().Height(4)
	}
	if a.batchMode != batchImage {
		ui.Column(c).Grow(1).MinHeight(0).Padding(12, 0, 0, 0).Gap(12).Children(func() { a.bottomPane(c) })
		return
	}
	ui.SplitVertical(c, &a.sourceHeight, func() {
		a.sourcesPane(c)
	}, func() {
		ui.Column(c).Fill().Padding(12, 0, 0, 0).Gap(12).Children(func() { a.bottomPane(c) })
	}).Grow(1).MinHeight(0).FillWidth()
}

// sourcesPane 源图片管理。
func (a *App) sourcesPane(c *ui.Context) {
	ui.Column(c).Fill().Padding(14).Gap(10).Background(c.Theme().Surface).Border(1, c.Theme().Border).Radius(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("源图片（%d）", len(a.sources))).Bold().Grow(1)
			ui.Button(c, "添加图片").Disabled(a.running).OnClick(func() { a.addImages() })
			ui.Button(c, "移除选中").Disabled(a.running || a.srcSel < 0 || a.srcSel >= len(a.sources)).OnClick(func() { a.removeSelected() })
			ui.Button(c, "清空").Disabled(a.running || len(a.sources) == 0).OnClick(func() {
				a.sources = nil
				a.srcSel = -1
			})
		})
		if len(a.sources) == 0 {
			ui.Column(c).Grow(1).Center().Gap(6).Children(func() {
				ui.Text(c, "还没有源图").FontSize(16).Bold()
				ui.Text(c, "点「添加图片」可多选导入，重复图片会自动过滤").FontSize(12).TextColor(c.Theme().TextMuted)
			})
			return
		}
		a.srcGrid.Selected = &a.srcSel
		a.srcGrid.Key = func(i int) any { return a.sources[i].Path }
		ui.GridView(c, &a.srcGrid, len(a.sources), 120, 128, func(i int) {
			si := a.sources[i]
			ui.Column(c).Gap(6).Children(func() {
				if si.Thumb != nil {
					ui.Image(c, si.Thumb).Size(100, 100).Radius(6)
				} else {
					ui.Text(c, "图片预览").Height(100)
				}
				ui.Text(c, filepath.Base(si.Path)).FontSize(11).SingleLine().Tooltip(si.Path)
			})
		}).Grow(1).MinHeight(0)
	})
}

// bottomPane 队列 + 日志（见 views.go）。
func (a *App) bottomPane(c *ui.Context) { a.bottomView(c) }

// canStart 是当前模式下「开始任务」是否可点。
func (a *App) canStart() bool {
	if a.running || (!a.platDoubao && !a.platQianwen) || len(a.missingBrowserNames()) > 0 {
		return false
	}
	if a.batchMode == batchImage {
		return len(a.sources) > 0
	}
	return len(splitLines(a.prompts)) > 0
}

// missingBrowserNames 是已勾选、但还没有浏览器实例的平台。
// 扩展桥不使用本机实例。未选择 Chrome/Edge（测试里的空值）也不在这里拦截。
func (a *App) missingBrowserNames() []string {
	if a.extensionTasks() || (a.browserBackend != "Chrome" && a.browserBackend != "Edge") {
		return nil
	}
	have := map[string]bool{}
	for _, b := range a.configuredBrowsers() {
		have[b.Platform] = true
	}
	var missing []string
	if a.platDoubao && !have["豆包"] {
		missing = append(missing, "豆包")
	}
	if a.platQianwen && !have["千问"] {
		missing = append(missing, "千问")
	}
	return missing
}

func missingBrowserHint(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, "、") + "还没有浏览器，请先在浏览器管理中添加"
}

func (a *App) extensionTasks() bool {
	return a.store != nil && extbridge.NormalizeBrowserBackend(a.store.Get(extbridge.SettingBrowserBackend)) == "extension"
}

func (a *App) configuredBrowsers() []store.Browser {
	if a.store != nil {
		return a.store.Browsers()
	}
	return a.browserRows
}

// batchImages 是这次要上传的源图。文生图不上传，即使列表里还留着上次的图。
func (a *App) batchImages() []string {
	if a.batchMode != batchImage {
		return nil
	}
	paths := make([]string, 0, len(a.sources))
	for _, si := range a.sources {
		paths = append(paths, si.Path)
	}
	return paths
}

// startTasks 按当前模式建队：文生图只提交提示词，图生图再上传源图。
func (a *App) startTasks() {
	if !a.canStart() {
		return
	}
	prompts := splitLines(a.prompts)
	srcPaths := a.batchImages()
	var plats []string
	if a.platDoubao {
		plats = append(plats, "豆包")
	}
	if a.platQianwen {
		plats = append(plats, "千问")
	}
	if len(plats) == 0 {
		a.progText = "请至少选择一个平台"
		return
	}
	a.saveSettings()
	opts := a.taskOptions(plats, prompts, srcPaths)
	if opts.OutputPath == "" {
		opts.OutputPath = filepath.Join(mustWd(), "输出结果")
		_ = os.MkdirAll(opts.OutputPath, 0o755)
		a.outputPath = opts.OutputPath
	}
	a.running = true
	a.progress = 0
	a.engine.SetSolver(captcha.Build(a.captchaConfig()))
	go func() {
		err := a.engine.Run(context.Background(), opts,
			func(t tasks.Task) {
				a.syncUI(func() {})
			},
			func(p tasks.Progress) {
				a.syncUI(func() {
					if p.Total > 0 {
						a.progress = float64(p.Done) / float64(p.Total)
					}
					a.progText = fmt.Sprintf("%d / %d", p.Done, p.Total)
				})
			})
		a.syncUI(func() {
			a.running = false
			if err != nil {
				a.progText = "失败: " + err.Error()
				logging.Log.Errorf("任务终止: %v", err)
			} else if view := a.engine.View(); view != nil {
				a.progText = taskResultSummary(view.Snapshot())
			}
		})
	}()
}

// syncUI 把界面更新送回窗口线程。
func (a *App) syncUI(fn func()) {
	if fn == nil || a.window == nil {
		return
	}
	a.window.Update(fn)
}

func (a *App) taskOptions(platforms, prompts, paths []string) tasks.Options {
	return tasks.Options{
		Platforms:          platforms,
		Prompts:            prompts,
		PromptSuffix:       a.promptSuffix,
		UseSuffix:          a.useSuffix,
		Images:             paths,
		Concurrency:        int(a.concurrency),
		RetryCount:         int(a.retryCount),
		SendWait:           int(a.sendWait),
		WaitBeforeSend:     int(a.waitBefore),
		WaitResult:         a.waitResult,
		WaitResultSeconds:  int(a.waitResultS),
		TaskSwitching:      a.taskSwitch,
		TaskSwitchingCount: int(a.switchCount),
		StartBrowserIndex:  int(a.startBrowserIdx),
		SaveMode:           a.saveMode,
		NamingRule:         a.namingRule,
		FilePrefix:         a.filePrefix,
		FilePrefixEnabled:  a.prefixOn,
		FileSuffix:         a.fileSuffix,
		FileSuffixEnabled:  a.suffixOn,
		OutputPath:         a.outputPath,
		RemoveWatermark:    a.removeWatermark,
	}
}

const verificationHint = "在浏览器中完成验证后重新开始任务"

func taskResultSummary(queue []tasks.Task) string {
	success, failed, needsUser := 0, 0, 0
	for _, task := range queue {
		switch task.Status {
		case tasks.StatusSuccess:
			success++
		case tasks.StatusFailed:
			failed++
		case tasks.StatusNeedsUser:
			needsUser++
		}
	}
	return fmt.Sprintf("成功 %d · 失败 %d · 待验证 %d · 未完成 %d", success, failed, needsUser, len(queue)-success-failed-needsUser)
}

// stopTasks 停止。
func (a *App) stopTasks() {
	a.engine.Stop()
}

// addImages 文件对话框选图。
func (a *App) addImages() {
	window := a.window
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:   window,
			Title:    "选择源图片",
			Multiple: true,
			Filters:  []mygo.FileFilter{{Name: "图片", Extensions: []string{"png", "jpg", "jpeg", "webp", "gif"}}},
		})
		if err != nil {
			logging.Log.Errorf("选择源图片失败: %v", err)
			return
		}
		if len(paths) == 0 {
			return
		}
		var loaded []sourceImage
		for _, p := range paths {
			data, err := os.ReadFile(p)
			if err != nil || !images.IsValidImageBytes(data) {
				continue
			}
			thumb, _ := images.Thumb(p, 200)
			var bmp *ui.Bitmap
			if thumb != nil {
				bmp = ui.NewBitmap(thumb)
			}
			loaded = append(loaded, sourceImage{Path: p, Thumb: bmp, Hash: images.SHA256(data)})
		}
		window.Update(func() {
			logging.Log.Info("添加 %d 张源图", a.appendSources(loaded))
		})
	}()
}

// appendSources 在 UI 线程合并导入结果，包含本批次及导入期间新增的源图去重。
func (a *App) appendSources(loaded []sourceImage) int {
	seen := make(map[string]bool, len(a.sources)+len(loaded))
	for _, si := range a.sources {
		seen[si.Hash] = true
	}
	added := 0
	for _, si := range loaded {
		if !seen[si.Hash] {
			a.sources = append(a.sources, si)
			seen[si.Hash] = true
			added++
		}
	}
	return added
}

// removeSelected 移除选中源图。
func (a *App) removeSelected() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.srcSel >= 0 && a.srcSel < len(a.sources) {
		a.sources = append(a.sources[:a.srcSel], a.sources[a.srcSel+1:]...)
		a.srcSel = -1
	}
}

// applyAppIcon 把 resources/icon.png 用到窗口、任务栏和 Dock。
// 打包时同一张图还是 macOS icns、Windows 安装器和 Linux 桌面项的图标。
func applyAppIcon(w *mygo.Window) {
	data, err := readAppIcon()
	if err != nil || len(data) == 0 {
		return
	}
	_ = mygo.App.Dock.SetIcon(data)
	if w != nil {
		_ = w.SetIcon(data)
	}
}

func readAppIcon() ([]byte, error) {
	if dir, err := mygo.App.Path(mygo.PathResources); err == nil {
		if data, err := os.ReadFile(filepath.Join(dir, "icon.png")); err == nil && len(data) > 0 {
			return data, nil
		}
	}
	return os.ReadFile(filepath.Join("resources", "icon.png"))
}
