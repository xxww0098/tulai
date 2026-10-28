// Package ui mygo 原生界面。出图页只有拖图、提示词和「开始出图」，
// 平台、轮换和输出在「高级」里。浏览器、提示词和设置仍是单独的页。
package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/update"
	"github.com/xxww0098/tulai/internal/wizard"
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
	browserChoice  string
	detectedNote   string
	detectedDetail string
	cftStatus      string
	cftBusy        bool
	cftPercent     float64
	cftReady       bool

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
	userPrompts     []prompts.UserPrompt
	promptPick      string
	promptChoice    string
	freeformPrompts string
	promptNote      string
	promptSuffix    string
	useSuffix       bool
	batchMode       int // batchText 文生图 / batchImage 图生图
	platformAuto    bool
	advancedOpen    bool
	logsOpen        bool
	outputNote      string
	thumbs          map[string]*ui.Bitmap
	thumbWait       map[string]bool
	look            tokens
	flowPin         int
	skipImages      bool
	previewOn       bool
	previewPath     string
	previewBmp      *ui.Bitmap

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
	tabSeen      int
	tabTracked   bool
	tabDir       int
	splitSz      float32
	sourceHeight float32
	updateBusy   bool

	autoCheck          bool
	updateMirror       string
	updateNotice       string
	updateNotes        string
	updateErr          string
	updateDeferred     bool
	updatePending      bool
	updateDownloading  bool
	updateFraction     float64
	updateProgressText string
	updateAsset        string
	updateStop         chan struct{}
	updateRel          update.Release

	wizardOn           bool
	wizardStep         wizard.Step
	wizardDir          int
	wizardPlan         wizard.BrowserPlan
	wizardQueue        wizard.Queue
	wizardAccount      store.Browser
	wizardExtra        bool
	wizardPickExtra    bool
	wizardPhase        string
	wizardNote         string
	wizardErr          string
	wizardGen          int
	wizardStop         chan struct{}
	wizardLaunch       func(store.Browser) (string, error)
	wizardOpen         func(ws, rawURL string) error
	wizardProbe        func(ctx context.Context, endpoint string) (string, error)
	wizardNavFailed    bool
	accountOpen        func(store.Browser) error
	pluginWasConnected bool
	pluginWatch        bool
	extensionDir       string
}

type sourceImage struct {
	Path  string
	Thumb *ui.Bitmap
	Hash  string
}

// Run 启动应用（主 goroutine）。
func Run(st *store.Store, bm *browser.Manager, eng *tasks.Engine, dataRoot string) {
	// 菜单「关于 / 退出」用这个名字。可执行文件和 mygo.json 的 name 也是 tulai。
	mygo.App.SetName("tulai")
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
	if dir, err := extbundle.Install(browser.ConfigDir()); err != nil {
		logging.Log.Warning("页面桥扩展没有解压：%v", err)
	} else {
		a.extensionDir = dir
		if bm != nil {
			bm.SetExtensionDir(dir)
			bm.SetExtensionReady(func(wsURL, extDir string) error {
				return cdp.EnsureExtension(context.Background(), wsURL, extDir)
			})
		}
	}
	a.applyWizardGate()
	if a.engine != nil {
		if err := a.engine.ServeLocal(); err != nil && a.extTokenNote == "" {
			a.extTokenNote = "自动对接未启动，可复制令牌手动填写"
		}
	}
	a.wireHealing()
	loginCtx, loginStop := context.WithCancel(context.Background())
	defer loginStop()
	if a.engine != nil {
		go a.engine.WatchLogins(loginCtx)
	}
	mygo.App.SetMenu(a.installMenu())
	a.updateStop = make(chan struct{})
	offQuit := mygo.App.OnQuit(func() {
		a.saveSettings()
		select {
		case <-a.updateStop:
		default:
			close(a.updateStop)
		}
	})
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
			Title:   "tulai",
			Width:   1280,
			Height:  900,
			Content: ui.View(a.view),
		})
		applyAppIcon(a.window)
		a.startUpdateLoop()
		if a.engine != nil {
			a.engine.EnsureExtensionBridge(func() {
				if a.window == nil {
					return
				}
				a.window.Update(func() {})
			})
		}
	})
	if err := mygo.App.Run(); err != nil {
		panic(err)
	}
}

// loadSettings 从配置库读入 UI 状态。
func (a *App) loadSettings() {
	s := a.store
	a.browserBackend = browser.NormalizeStoredBackend(runtime.GOOS, s.Get("browser_backend"))
	s.Set("browser_backend", a.browserBackend)
	a.browserChoice = browser.ChoiceLabel(a.browserBackend)
	a.extToken = extbridge.NormalizeExtToken(s.Get(extbridge.SettingExtToken))
	if extbridge.WeakToken(s.Get(extbridge.SettingExtToken)) {
		if generated, err := extbridge.NewToken(); err == nil {
			a.extToken = generated
			s.Set(extbridge.SettingExtToken, generated)
		}
	}
	a.extPort = extbridge.NormalizeExtPort(s.Get(extbridge.SettingExtPort))
	a.platDoubao = containsStr(s.Platforms(), "豆包")
	a.platQianwen = containsStr(s.Platforms(), "千问")
	if raw := s.Get("platform_auto"); raw == "" {
		a.platformAuto = true
	} else {
		a.platformAuto = raw == "1"
	}
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
	a.freeformPrompts = s.Get("prompts_freeform")
	a.userPrompts = prompts.ParseUser(s.Get("user_prompts"))
	a.promptPick = prompts.NormalizePick(s.Get("prompt_pick"), a.userPrompts)
	if a.promptPick == "" {
		a.freeformPrompts = a.prompts
	}
	a.promptChoice = prompts.LabelFor(a.promptPick, a.userPrompts)
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
		a.sources = append(a.sources, sourceImage{Path: p, Hash: images.SHA256(data)})
	}
	a.autoCheck = update.AutoOn(s.Get(update.KeyAutoCheck))
	a.updateMirror = strings.TrimSpace(s.Get(update.KeyMirror))
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
	s.SetInt("platform_auto", boolInt(a.platformAuto))
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
	s.Set("prompts_freeform", a.freeformPrompts)
	s.Set("user_prompts", prompts.FormatUser(a.userPrompts))
	s.Set("prompt_pick", a.promptPick)
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
	a.persistUpdatePrefs()
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

// view 主视图。向导盖住全部页面；否则是出图、浏览器、提示词或设置。
// 向导和主界面、以及四个页之间各走一段淡入位移，页内状态不变。
func (a *App) view(c *ui.Context) {
	if a.wizardOn {
		ui.Column(c.Key("screen-wizard")).Fill().Transition(screenMotion()).Children(func() {
			a.wizardView(c)
		})
		return
	}
	a.noteTab()
	a.look = applyWorkbenchTheme(c)
	ui.Column(c.Key("screen-main")).Fill().Background(a.look.paper).Transition(screenMotion()).Children(func() {
		a.chrome(c)
		ui.Column(c.Key(fmt.Sprintf("page-%d", a.tab))).Grow(1).MinHeight(0).Padding(8, 28, 12, 28).Transition(a.pageMotion()).Children(func() {
			switch a.tab {
			case pageBrowser:
				a.browserPane(c)
			case pagePrompts:
				a.promptsPage(c)
			case pageSettings:
				a.settingsPage(c)
			default:
				a.homePage(c)
			}
		})
	})
}

// missingBrowserNames 是已勾选、但还没有浏览器实例的平台。
func (a *App) missingBrowserNames() []string {
	// 扩展桥不使用本机实例。空的网页方式（部分测试）也不在这里拦截。
	switch a.browserBackend {
	case browser.KindChrome, browser.KindEdge, browser.KindBuiltin:
	default:
		return nil
	}
	if a.extensionTasks() {
		return nil
	}
	have := map[string]bool{}
	for _, b := range a.configuredBrowsers() {
		if b.ExtAccount != "" {
			continue
		}
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
		loaded := loadSourceFiles(paths)
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
	if added > 0 {
		a.skipImages = false
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
