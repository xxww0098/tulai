// Package ui mygo 原生界面。左侧是图标导航。
// 生图和洗图左边是输入和开始，右边是账号、进度和运行日志。配置在设置页。
package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/agy"
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
	platAntigravity bool
	agyBin          string
	agyHomeRoot     string
	agyModel        string
	agyTimeoutMin   float64
	agySaved        string
	agyLogin        *agyLoginState
	agyChooser      *agyChooserState
	sendStrategy    string
	concurrencyAuto bool
	concurrency     float64 // 手动上限。自动时仍记住这个数，关掉自动后接着用
	retryCount      float64
	sendWait        float64
	dailyCap        float64
	riskyWords      string
	pickFile        func() (string, error)
	waitBefore      float64
	waitResult      bool
	waitResultS     float64
	taskSwitch      bool
	switchCount     float64
	startBrowserIdx float64
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
	logScroll       ui.ScrollState
	stageTab        int
	stagePicked     bool
	outputNote      string
	thumbs          map[string]*ui.Bitmap
	thumbWait       map[string]bool
	look            tokens
	flowPin         int
	skipImages      bool
	previewOn       bool
	previewPath     string
	previewBmp      *ui.Bitmap
	statePage       int       // App 字段里装的是哪一页：pageHome 或 pageWash
	parked          pageState // 另一页的图片、提示词和进度
	runPage         int       // 引擎这一批属于哪一页

	// 验证码设置
	captchaEnabled bool
	captchaBackend string
	captchaChoice  string
	customURL      string
	customKey      string
	customModel    string
	ikunocrKey     string
	proxyCustom    bool
	proxyURL       string
	proxySaved     string

	// 源图片
	sources []sourceImage
	srcGrid ui.GridState
	srcSel  int

	// 去重页
	dedup dedupState

	// 浏览器
	browserRows    []store.Browser
	browserSel     int
	newBrowserPlat string
	newBrowserName string
	platSynced     bool
	platSyncedTab  int
	renameID       int64
	renameDraft    string
	renameFocus    bool
	loginLaunch    func(store.Browser) error
	loginClosed    []int64
	afterClose     func()
	extToken       string
	extPort        string
	extTokenNote   string

	// 队列与日志
	queue         []tasks.Task
	queueTbl      ui.ListState
	logs          []logging.Entry
	progress      float64
	progText      string
	running       bool
	useRunView    bool
	genSession    int64
	washSession   int64
	workspacePage string // 上次停留的生图或洗图，gen / wash
	historyOpen   bool
	genList       ui.ListState
	washList      ui.ListState
	stamps        map[string]int64

	tab          int
	tabSeen      int
	tabTracked   bool
	tabDir       int
	splitSz      float32
	sourceHeight float32
	updateBusy   bool
	updateChecks int // 手动检查更新的次数。点下去就加，不等网络返回。

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
	wizardName         string
	wizardNameFor      int64
	loginShots         map[int64]loginShot
	loginWatchGen      map[int64]int
	loginHold          map[int64]struct{}
	loginFresh         map[int64]struct{} // 这一轮新建、还没登录成功，离开时可以删
	loginProbed        map[int64]string   // 本进程已经探过的结果，避免每次刷新再堵一次
	accountProbe       func(store.Browser) (string, error)
	loginPoll          time.Duration
	loginFollow        func(store.Browser, string)
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
		dedup:        dedupState{level: dedupDefaultLevel},
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
		if a.manager != nil {
			w := a.window
			a.manager.SetAppFocus(func() {
				if w != nil && !w.IsDestroyed() {
					w.Focus()
				}
			})
		}
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
	a.platAntigravity = containsStr(s.Platforms(), agy.Platform)
	a.agyBin = strings.TrimSpace(s.Get("agy_bin"))
	if a.agyBin == "" {
		a.agyBin = agy.DefaultBin()
	}
	a.agyHomeRoot = strings.TrimSpace(s.Get("agy_home_root"))
	if a.agyHomeRoot == "" {
		a.agyHomeRoot = agy.HomeRoot("", s.File())
	}
	a.agyModel = strings.TrimSpace(s.Get("agy_model"))
	if a.agyModel == "" {
		a.agyModel = agy.DefaultModel
	}
	sec := s.GetInt("agy_timeout_seconds")
	if sec <= 0 {
		sec = int(agy.DefaultTimeout / time.Second)
	}
	if sec < 60 {
		sec = 60
	}
	if sec > 3600 {
		sec = 3600
	}
	a.agyTimeoutMin = float64(sec) / 60
	a.agySaved = a.agySignature()
	if raw := s.Get("platform_auto"); raw == "" {
		a.platformAuto = true
	} else {
		a.platformAuto = raw == "1"
	}
	a.sendStrategy = pickStr(s.Get("send_strategy"), "顺序循环", []string{"顺序循环", "随机循环"})
	if n := s.GetInt("concurrency"); n <= 0 {
		a.concurrencyAuto = true
		a.concurrency = 1
	} else {
		a.concurrencyAuto = false
		a.concurrency = float64(clampInt(n, 1, 16))
	}
	a.retryCount = float64(clampInt(s.GetInt("retry_count"), 0, 20))
	a.sendWait = float64(clampInt(s.GetInt("send_wait_seconds"), 0, 600))
	a.dailyCap = float64(clampInt(s.GetInt("daily_cap"), 1, 200))
	a.riskyWords = s.Get("risky_words")
	if strings.TrimSpace(a.riskyWords) == "" {
		a.riskyWords = tasks.RiskySetting()
	}
	a.waitBefore = float64(clampInt(s.GetInt("wait_before_sending"), 0, 60))
	a.waitResult = s.GetBool("wait_result")
	a.waitResultS = float64(clampInt(s.GetInt("wait_result_seconds"), 10, 1800))
	a.taskSwitch = s.GetBool("task_switching")
	a.switchCount = float64(clampInt(s.GetInt("task_switching_count"), 1, 9999))
	a.startBrowserIdx = float64(clampInt(s.GetInt("start_browser_index"), 1, 999))
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
	a.proxyCustom = s.Get("proxy_mode") == "custom"
	a.proxyURL = strings.TrimSpace(s.Get("proxy_server"))
	a.proxySaved = a.proxyURL
	a.pushProxy()

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
	wash := pageState{srcSel: -1, prompts: s.Get("wash_prompt")}
	if strings.TrimSpace(wash.prompts) == "" {
		wash.prompts = DefaultWashPrompt
	}
	for _, p := range splitLines(s.Get("wash_images")) {
		data, err := os.ReadFile(p)
		if err != nil || !images.IsValidImageBytes(data) {
			continue
		}
		wash.sources = append(wash.sources, sourceImage{Path: p, Hash: images.SHA256(data)})
	}
	a.parked = wash
	a.loadDedupSettings(s)
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
	a.restoreWorkspace(pageHome)
	a.restoreWorkspace(pageWash)
	a.onPage(pageHome, func() {
		a.stageTab = s.GetInt("gen_stage")
		a.stagePicked = s.Get("gen_stage_picked") == "1"
	})
	a.onPage(pageWash, func() {
		a.stageTab = s.GetInt("wash_stage")
		a.stagePicked = s.Get("wash_stage_picked") == "1"
	})
	a.workspacePage = store.PageGen
	if s.Get("workspace_page") == store.PageWash {
		a.tab = pageWash
		a.workspacePage = store.PageWash
	}
	a.syncPage()
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
	if a.platAntigravity {
		plats = append(plats, agy.Platform)
	}
	s.SetPlatforms(plats)
	a.persistAgySettings()
	s.SetInt("platform_auto", boolInt(a.platformAuto))
	s.Set("send_strategy", a.sendStrategy)
	if a.concurrencyAuto {
		s.SetInt("concurrency", 0)
	} else {
		s.SetInt("concurrency", clampInt(int(a.concurrency), 1, 16))
	}
	s.SetInt("retry_count", int(a.retryCount))
	s.SetInt("send_wait_seconds", int(a.sendWait))
	s.SetInt("daily_cap", a.shownDailyCap())
	words := strings.TrimSpace(a.riskyWords)
	if words == "" {
		words = tasks.RiskySetting()
	}
	s.Set("risky_words", words)
	s.SetInt("wait_before_sending", int(a.waitBefore))
	s.SetInt("wait_result", boolInt(a.waitResult))
	s.SetInt("wait_result_seconds", int(a.waitResultS))
	s.SetInt("task_switching", boolInt(a.taskSwitch))
	s.SetInt("task_switching_count", int(a.switchCount))
	s.SetInt("start_browser_index", int(a.startBrowserIdx))
	s.Set("output_path", a.outputPath)
	s.SetInt("remove_watermark", boolInt(a.removeWatermark))
	a.noteWorkspacePage()
	if a.workspacePage == store.PageGen || a.workspacePage == store.PageWash {
		s.Set("workspace_page", a.workspacePage)
	}
	gen, wash := a.stateOf(pageHome), a.stateOf(pageWash)
	s.SetInt("gen_stage", gen.stageTab)
	s.SetInt("gen_stage_picked", boolInt(gen.stagePicked))
	s.SetInt("wash_stage", wash.stageTab)
	s.SetInt("wash_stage_picked", boolInt(wash.stagePicked))
	s.Set("prompts", gen.prompts)
	s.Set("wash_prompt", strings.TrimSpace(wash.prompts))
	s.Set("wash_images", joinPaths(sourcePaths(wash.sources)))
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
	a.persistProxy()
	// 源图列表持久化
	s.Set("source_images", joinPaths(sourcePaths(gen.sources)))
	a.saveDedupSettings(s)
	a.persistUpdatePrefs()
	a.persistWorkspace(pageHome)
	a.persistWorkspace(pageWash)
}

func (a *App) pushProxy() {
	raw := ""
	if a != nil && a.proxyCustom {
		raw = strings.TrimSpace(a.proxyURL)
	}
	browser.SetProxy(raw)
}

func (a *App) persistProxy() {
	if a == nil {
		return
	}
	a.pushProxy()
	if a.store == nil {
		return
	}
	mode := "system"
	if a.proxyCustom {
		mode = "custom"
	}
	a.store.Set("proxy_mode", mode)
	a.store.Set("proxy_server", strings.TrimSpace(a.proxyURL))
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
	a.syncPage()
	a.look = applyWorkbenchTheme(c)
	ui.Column(c.Key("screen-main")).Fill().Background(a.look.paper).Transition(screenMotion()).Children(func() {
		slot := ui.Column(c.Key("update-slot"))
		if a.updateNotice != "" {
			slot.Padding(8, pagePadX, 0, pagePadX)
		}
		slot.Children(func() { a.updateBanner(c) })
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.navRail(c)
			ui.Column(c.Key(fmt.Sprintf("page-%d", a.tab))).Grow(1).MinWidth(0).MinHeight(0).Padding(12, pagePadX, 12, pagePadX).Transition(a.pageMotion()).Children(func() {
				switch a.tab {
				case pageWash:
					a.washPage(c)
				case pageDedup:
					a.dedupPage(c)
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
		Concurrency:        a.taskConcurrency(),
		RetryCount:         int(a.retryCount),
		SendWait:           int(a.sendWait),
		WaitBeforeSend:     int(a.waitBefore),
		WaitResult:         a.waitResult,
		WaitResultSeconds:  int(a.waitResultS),
		TaskSwitching:      a.taskSwitch,
		TaskSwitchingCount: int(a.switchCount),
		StartBrowserIndex:  int(a.startBrowserIdx),
		OutputPath:         a.outputPath,
		RemoveWatermark:    a.removeWatermark,
		DailyCap:           a.shownDailyCap(),
		RiskyWords:         tasks.ParseWords(a.riskyWords),
		AgyBin:             a.agyBin,
		AgyModel:           a.agyModel,
		AgyHomeRoot:        a.agyHomeRoot,
		AgyTimeoutSec:      a.agyTimeoutSec(),
	}
}

const verificationHint = "在浏览器中完成验证后重新开始任务"

func taskResultSummary(queue []tasks.Task) string {
	tally := TallyTasks(queue)
	return fmt.Sprintf("成功 %d · 失败 %d · 待验证 %d · 未完成 %d", tally.Done, tally.Failed, tally.Needs, tally.Incomplete())
}

// stopTasks 停止。
func (a *App) stopTasks() {
	a.engine.Stop()
}

// addImages 文件对话框选图。
func (a *App) addImages() {
	window := a.window
	page := a.statePage
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
		window.Update(func() { a.addLoaded(page, loaded) })
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

func sourcePaths(list []sourceImage) []string {
	paths := make([]string, 0, len(list))
	for _, si := range list {
		paths = append(paths, si.Path)
	}
	return paths
}
