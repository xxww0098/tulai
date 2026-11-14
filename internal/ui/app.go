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

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/download"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/update"
	"github.com/xxww0098/tulai/internal/wizard"
	"github.com/xxww0098/tulai/resources"
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

	store            *store.Store
	manager          *browser.Manager
	engine           *tasks.Engine
	window           *mygo.Window
	dataRoot         string
	browserBackend   string
	browserChoice    string
	accountProxyID   int64
	accountProxy     string
	accountProxyNote string
	detectedNote     string
	detectedDetail   string
	camoufoxStatus   string
	camoufoxBusy     bool
	camoufoxPercent  float64 // 小于 0 表示这一步给不出百分比
	camoufoxDone     int64
	camoufoxTotal    int64
	camoufoxSpeed    int64
	camoufoxLast     int64
	camoufoxStage    string
	camoufoxAt       time.Time
	camoufoxReady    bool

	// 任务设置（与 settings 表一一对应）
	platDoubao         bool
	platQianwen        bool
	platAntigravity    bool
	platChatGPT        bool
	chatGPTModel       string
	agyBin             string
	agyHomeRoot        string
	agyModel           string
	agyTimeoutMin      float64
	agySaved           string
	agyLogin           *agyLoginState
	agyChooser         *agyChooserState
	agyUsageMu         sync.Mutex
	agyUsage           map[int64]agyUsageSlot
	agyUsageForce      map[int64]bool
	agyUsageStop       context.CancelFunc
	agyUsageWake       chan struct{}
	agyUsageSync       bool
	agyUsageFetch      func(context.Context, string, string) (agy.GeminiUsage, error)
	agyUsageBusy       func(int64) bool
	sendStrategy       string
	concurrencyAuto    bool
	retryCount         float64
	sendWait           float64
	dailyCap           float64 // 豆包每日上限；旧配置的全局值也读到这里
	dailyCapSet        bool
	dailyCapQianwen    float64
	dailyCapQianwenSet bool
	dailyCapAgy        float64
	dailyCapAgySet     bool
	dailyCapChatGPT    float64
	dailyCapChatGPTSet bool
	riskyWords         string
	pickFile           func() (string, error)
	waitBefore         float64
	waitResult         bool
	waitResultS        float64
	taskSwitch         bool
	switchCount        float64
	startBrowserIdx    float64
	outputPath         string
	genAutoNumber      bool
	washAutoNumber     bool
	numPrefix          string
	numStart           float64
	numWidth           float64
	outputSizeMode     string
	outputSizePreset   string
	outputRatio        string
	outputWidth        string
	outputHeight       string
	outputCrop         bool
	washStyle          string
	removeWatermark    bool
	downloadConc       float64
	downloadRetries    float64
	downloadRateKB     float64

	// 下载页
	dlURLs           string
	dlDir            string
	dlDepth          float64
	dlMinKB          float64
	dlMaxKB          float64
	dlMinWidth       float64
	dlMaxWidth       float64
	dlMinHeight      float64
	dlMaxHeight      float64
	dlBrowserMode    bool
	dlBrowser        downloadBrowser
	dlBrowserURL     string
	dlBrowserBusy    bool
	dlBrowserErr     string
	dlBrowserCancel  context.CancelFunc
	dlHost           *download.HostLimiter
	dlPathLock       *download.PathLocker
	dlTasks          []*downloadTask
	dlNextID         int
	dlFilter         int
	dlSearch         string
	dlSearchOn       bool
	dlComposerOn     bool
	dlComposerErr    string
	dlGuideOn        bool
	dlGuideSearch    string
	dlGuideScroll    ui.ScrollState
	dlImport         *downloadImportState
	dlImportNotice   string
	dlGroupBySite    bool
	dlSelectedID     int
	dlAllChecked     bool
	prompts          string
	userPrompts      []prompts.UserPrompt
	promptPick       string
	promptChoice     string
	freeformPrompts  string
	promptNote       string
	genPromptChoice  string
	promptEditPick   string
	promptEditChoice string
	promptEditText   string
	promptEditName   string
	promptEditKind   string
	promptEditDraft  string
	promptDraftName  string
	promptDraftKind  string
	promptFilter     string
	promptEditNote   string
	washPromptChoice string
	promptSuffix     string
	useSuffix        bool
	structuredGen    promptParts
	structuredWash   promptParts
	structuredReady  bool
	structuredOpen   bool
	structuredPage   int
	batchMode        int // batchText 文生图 / batchImage 图生图
	platformAuto     bool
	logScroll        ui.ScrollState
	workInputScroll  ui.ScrollState
	stageTab         int
	stagePicked      bool
	outputNote       string
	thumbs           map[string]*ui.Bitmap
	thumbWait        map[string]bool
	look             tokens
	flowPin          int
	skipImages       bool
	galleryOn        bool
	galleryTitle     string
	gallerySelected  map[string]bool
	galleryDrag      *galleryDragState
	previewOn        bool
	previewPath      string
	previewBmp       *ui.Bitmap
	statePage        int               // App 字段里装的是哪一页的工作区
	parked           map[int]pageState // 其他工作区的图片、提示词和进度
	fissionRetryBase []tasks.Task      // 重试时保留同一组中未重试的结果
	runPage          int               // 引擎这一批属于哪一页

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
	dedup      dedupState
	fileRename renameState
	edit       imageEditState
	uiUpdates  []func() // 无窗口时由 mu 保护，UI 帧统一应用后台结果。

	// 浏览器
	browserRows      []store.Browser
	browserList      ui.ListState
	browserChecked   ui.Selection[int64]
	browserPending   map[int64]browserOperation
	browserBatch     *browserBatchRun
	browserDrawer    *browserDrawerState
	browserViewingID int64 // 由 mu 保护，供登录探测协程检查。
	browserSel       int
	newBrowserPlat   string
	newBrowserName   string
	platSynced       bool
	platSyncedTab    int
	renameID         int64
	renameDraft      string
	renameFocus      bool
	loginLaunch      func(store.Browser) error
	loginClosed      []int64
	afterClose       func()
	extToken         string
	extPort          string
	extTokenNote     string

	// 队列与日志
	queue          []tasks.Task
	queueTbl       ui.ListState
	logs           []logging.Entry
	progress       float64
	progText       string
	running        bool
	useRunView     bool
	genSession     int64
	washSession    int64
	fissionSession int64
	workspacePage  string // 上次停留的图片生成模式
	historyOpen    bool
	genList        ui.ListState
	washList       ui.ListState
	fissionList    ui.ListState
	stamps         map[string]int64

	tab          int
	settingsTab  int
	navCollapsed bool
	navManual    bool
	logOpen      bool
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
	updateDetailsOpen  bool
	updateNotesOpen    bool
	updateHidden       bool
	updateErr          string
	updateDeferred     bool
	updatePending      bool
	updateDownloading  bool
	updateFraction     float64
	updateProgressText string
	updateAsset        string
	updateInstalled    string
	updateStop         chan struct{}
	updateRel          update.Release
	updateSigned       *mygo.Update
	updateSignedClient *update.Client
	downloadsRunning   int

	wizardOn        bool
	wizardStep      wizard.Step
	wizardDir       int
	wizardPlan      wizard.BrowserPlan
	wizardQueue     wizard.Queue
	wizardAccount   store.Browser
	wizardExtra     bool
	wizardChoice    string                      // 第一步选中的平台，三选一
	wizardFirst     bool                        // 首次设置时，选定的平台就是全部勾选
	wizardAgy       wizardAgyState              // 安装步骤里对 agy 的检测结果
	wizardPlanFn    func() wizard.BrowserPlan   // 测试可换掉本机浏览器检测
	agyLocate       func(string) (string, bool) // 测试可换掉 agy 查找
	wizardPhase     string
	wizardNote      string
	wizardErr       string
	wizardGen       int
	wizardStop      chan struct{}
	wizardLaunch    func(store.Browser) (string, error)
	wizardOpen      func(ws, rawURL string) error
	wizardProbe     func(ctx context.Context, endpoint string) (string, error)
	wizardNavFailed bool
	wizardName      string
	wizardNameFor   int64
	loginShots      map[int64]loginShot
	loginWatchGen   map[int64]int
	loginHold       map[int64]struct{}
	loginFresh      map[int64]struct{} // 这一轮新建、还没登录成功，离开时可以删
	loginProbed     map[int64]string   // 本进程已经探过的结果，避免每次刷新再堵一次
	accountProbe    func(store.Browser) (string, error)
	loginPoll       time.Duration
	loginFollow     func(store.Browser, string)
	accountOpen     func(store.Browser) error
	quitting        bool
}

type sourceImage struct {
	Path       string
	Thumb      *ui.Bitmap
	Hash       string
	Washed     bool
	Washing    bool
	WashOutput string
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
	// 向导和账号列表都先看到干净的库：不走扩展的方式下，遗留的扩展账号行不算账号。
	eng.DropInactiveExtensionAccounts()
	a.loadSettings()
	defer func() {
		for _, task := range a.dlTasks {
			if task.cancel != nil {
				task.cancel()
			}
		}
		if a.dlBrowserCancel != nil {
			a.dlBrowserCancel()
		}
		if a.dlBrowser != nil {
			a.dlBrowser.Close()
		}
	}()

	a.applyWizardGate()
	if a.engine != nil {
		if err := a.engine.ServeLocal(); err != nil && a.extTokenNote == "" {
			a.extTokenNote = "自动对接未启动，可复制令牌手动填写"
		}
	}
	a.wireHealing()
	defer logging.Log.SetSink(nil)
	defer a.stopAgyUsage()
	loginCtx, loginStop := context.WithCancel(context.Background())
	var loginWG sync.WaitGroup
	defer func() {
		loginStop()
		loginWG.Wait()
	}()
	// 窗口里不要菜单栏：检查更新和帮助在侧栏与设置页里，WebView 自己处理编辑快捷键。
	// macOS 的菜单栏在屏幕顶部，没有它连 Cmd+Q 和复制粘贴都会失效，所以只在那儿留最少的菜单。
	if runtime.GOOS == "darwin" {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{{Role: mygo.RoleAppMenu}, {Role: mygo.RoleEditMenu}}))
	} else {
		mygo.App.SetMenu(nil)
	}
	a.updateStop = make(chan struct{})
	offQuit := mygo.App.OnQuit(func() {
		a.closeBrowserDrawer()
		a.waitRename()
		a.saveSettings()
		a.mu.Lock()
		a.quitting = true
		a.uiUpdates = nil
		a.mu.Unlock()
		logging.Log.SetSink(nil)
		loginStop()
		if a.engine != nil {
			a.engine.Stop()
		}
		select {
		case <-a.updateStop:
		default:
			close(a.updateStop)
		}
	})
	defer offQuit()
	mygo.App.WhenReady(func() {
		a.window = mygo.NewWindow(mygo.WindowOptions{
			Title:   "tulai",
			Width:   1080,
			Height:  720,
			Content: ui.View(a.view),
		})
		// 窗口初始化后再发布后台回调，避免读到尚未赋值的 window。
		logging.Log.SetSink(func(e logging.Entry) {
			if a.isQuitting() || a.window == nil || a.window.IsDestroyed() {
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
		a.startAgyUsage()
		if a.engine != nil {
			loginWG.Add(1)
			go func() {
				defer loginWG.Done()
				a.engine.WatchLogins(loginCtx)
			}()
			// 查看面板正盯着的账号不做空闲关闭，免得看到一半断开。
			a.engine.SetIdleGuard(func(id int64) bool {
				a.mu.Lock()
				defer a.mu.Unlock()
				return a.browserViewingID == id
			})
		}
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
				if a.isQuitting() || a.window == nil || a.window.IsDestroyed() {
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
	a.platChatGPT = containsStr(s.Platforms(), "ChatGPT")
	a.chatGPTModel = s.Get("chatgpt_model")
	if a.chatGPTModel == "" {
		a.chatGPTModel = "gpt-image-2"
	}
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
	a.concurrencyAuto = s.GetInt("concurrency") != 1
	a.retryCount = float64(clampInt(s.GetInt("retry_count"), 0, 20))
	a.sendWait = float64(clampInt(s.GetInt("send_wait_seconds"), 0, 600))
	a.dailyCap = float64(clampInt(s.GetInt("daily_cap"), 0, 200))
	a.dailyCapSet = true
	capOr := func(key string, fallback float64) (float64, bool) {
		if strings.TrimSpace(s.Get(key)) == "" {
			// 旧配置没有分供应商字段时，按当时的全局值初始化一次，之后各自独立。
			return fallback, true
		}
		return float64(clampInt(s.GetInt(key), 0, 200)), true
	}
	a.dailyCapQianwen, a.dailyCapQianwenSet = capOr("daily_cap_qianwen", a.dailyCap)
	a.dailyCapAgy, a.dailyCapAgySet = capOr("daily_cap_agy", a.dailyCap)
	a.dailyCapChatGPT, a.dailyCapChatGPTSet = capOr("daily_cap_chatgpt", a.dailyCap)
	a.riskyWords = s.Get("risky_words")
	if strings.TrimSpace(a.riskyWords) == "" {
		a.riskyWords = tasks.RiskySetting()
	}
	a.loadImageEditSettings()
	a.loadPromptParts()
	a.waitBefore = float64(clampInt(s.GetInt("wait_before_sending"), 0, 60))
	a.waitResult = s.GetBool("wait_result")
	a.waitResultS = float64(clampInt(s.GetInt("wait_result_seconds"), 10, 1800))
	a.taskSwitch = s.GetBool("task_switching")
	a.switchCount = float64(clampInt(s.GetInt("task_switching_count"), 1, 9999))
	a.startBrowserIdx = float64(clampInt(s.GetInt("start_browser_index"), 1, 999))
	a.outputPath = s.Get("output_path")
	a.genAutoNumber = s.GetBool("gen_auto_number")
	a.washAutoNumber = s.GetBool("wash_auto_number")
	a.numPrefix = s.Get("number_prefix")
	a.numStart = 1
	if raw := s.Get("number_start"); raw != "" {
		a.numStart = float64(clampInt(s.GetInt("number_start"), 0, 9_000_000_000_000_000))
	}
	a.numWidth = 3
	if raw := s.Get("number_width"); raw != "" {
		a.numWidth = float64(clampInt(s.GetInt("number_width"), 0, 16))
	}
	a.loadOutputSize()
	a.loadWashStyle()
	a.dlURLs = s.Get("download_urls")
	a.dlDir = strings.TrimSpace(s.Get("download_dir"))
	a.dlDepth = float64(clampInt(s.GetInt("download_depth"), 1, 32767))
	a.dlMinKB = float64(clampInt(s.GetInt("download_min_kb"), 0, 10000000))
	a.dlMaxKB = float64(clampInt(s.GetInt("download_max_kb"), 0, 10000000))
	a.dlMinWidth = float64(clampInt(s.GetInt("download_min_width"), 0, 10000000))
	a.dlMaxWidth = float64(clampInt(s.GetInt("download_max_width"), 0, 10000000))
	a.dlMinHeight = float64(clampInt(s.GetInt("download_min_height"), 0, 10000000))
	a.dlMaxHeight = float64(clampInt(s.GetInt("download_max_height"), 0, 10000000))
	a.dlBrowserMode = s.GetBool("download_browser_mode")
	if raw := s.Get("download_concurrency"); raw == "" {
		a.downloadConc = 2
	} else {
		a.downloadConc = float64(clampInt(s.GetInt("download_concurrency"), 1, 8))
	}
	if raw := s.Get("download_retries"); raw == "" {
		a.downloadRetries = 2
	} else {
		a.downloadRetries = float64(clampInt(s.GetInt("download_retries"), 0, 10))
	}
	a.downloadRateKB = float64(clampInt(s.GetInt("download_rate_kbps"), 0, 102400))
	a.removeWatermark = s.GetBool("remove_watermark")
	a.prompts = s.Get("prompts")
	a.freeformPrompts = s.Get("prompts_freeform")
	a.userPrompts = prompts.ParseUser(s.Get("user_prompts"))
	a.promptPick = s.Get("prompt_pick")
	if a.promptPick != detachedPromptID {
		oldPick := strings.TrimSpace(a.promptPick)
		a.promptPick = prompts.NormalizePick(a.promptPick, a.userPrompts)
		// 下架模板的正文仍在工作区，保持整段发送。
		if a.promptPick == "" && (oldPick == "oil" || oldPick == "art" || oldPick == "circle" || oldPick == prompts.IDWash || oldPick == prompts.IDDoubaoPixelWash) {
			a.promptPick = detachedPromptID
		}
	}
	if a.promptPick == "" {
		a.freeformPrompts = a.prompts
	}
	a.promptChoice = a.workspacePromptLabel()
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
	a.parked = map[int]pageState{pageWash: wash, pageFission: {srcSel: -1, prompts: DefaultFissionPrompt}}
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
	a.restoreWorkspace(pageFission)
	a.onPage(pageHome, func() {
		a.stageTab = s.GetInt("gen_stage")
		a.stagePicked = s.Get("gen_stage_picked") == "1"
	})
	a.onPage(pageWash, func() {
		// 只升级未经修改的旧默认，保留用户改过的内容。
		if strings.TrimSpace(a.prompts) == "提取图中的商品主体，去掉所有文字、水印和背景装饰，纯白背景，保持主体形状、颜色和细节完全不变" {
			a.prompts = DefaultWashPrompt
		} else if prompts.SameText(a.prompts, prompts.LegacyDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.PreviousDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.ReviewedDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.PixelReviewDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.FrameReviewDoubaoWashPrompt) {
			a.prompts = prompts.DoubaoWashPrompt
		}
		a.stageTab = s.GetInt("wash_stage")
		a.stagePicked = s.Get("wash_stage_picked") == "1"
	})
	a.onPage(pageFission, func() {
		a.stageTab = s.GetInt("fission_stage")
		a.stagePicked = s.Get("fission_stage_picked") == "1"
	})
	a.workspacePage = store.PageGen
	if s.Get("workspace_page") == store.PageWash {
		a.tab = pageWash
		a.workspacePage = store.PageWash
	} else if s.Get("workspace_page") == store.PageFission {
		a.tab = pageFission
		a.workspacePage = store.PageFission
	}
	a.loadDownloadRecords()
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
	s.SetPlatforms(a.selectedPlatforms())
	s.Set("chatgpt_model", a.chatGPTModel)
	a.persistAgySettings()
	s.SetInt("platform_auto", boolInt(a.platformAuto))
	s.Set("send_strategy", a.sendStrategy)
	s.SetInt("concurrency", a.taskConcurrency())
	s.SetInt("retry_count", int(a.retryCount))
	s.SetInt("send_wait_seconds", int(a.sendWait))
	s.SetInt("daily_cap", a.shownDailyCap())
	s.SetInt("daily_cap_qianwen", a.shownDailyCapFor("千问"))
	s.SetInt("daily_cap_agy", a.shownDailyCapFor(agy.Platform))
	s.SetInt("daily_cap_chatgpt", a.shownDailyCapFor("ChatGPT"))
	a.dailyCapSet = true
	a.dailyCapQianwenSet = true
	a.dailyCapAgySet = true
	a.dailyCapChatGPTSet = true
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
	s.SetInt("gen_auto_number", boolInt(a.genAutoNumber))
	s.SetInt("wash_auto_number", boolInt(a.washAutoNumber))
	s.Set("number_prefix", a.numPrefix)
	s.SetInt("number_start", clampInt(int(a.numStart), 0, 9_000_000_000_000_000))
	s.SetInt("number_width", clampInt(int(a.numWidth), 0, 16))
	a.saveOutputSize()
	a.saveWashStyle()
	s.Set("download_urls", redactQzoneURLs(a.dlURLs))
	s.Set("download_dir", strings.TrimSpace(a.dlDir))
	s.SetInt("download_depth", clampInt(int(a.dlDepth), 1, 32767))
	s.SetInt("download_min_kb", clampInt(int(a.dlMinKB), 0, 10000000))
	s.SetInt("download_max_kb", clampInt(int(a.dlMaxKB), 0, 10000000))
	s.SetInt("download_min_width", clampInt(int(a.dlMinWidth), 0, 10000000))
	s.SetInt("download_max_width", clampInt(int(a.dlMaxWidth), 0, 10000000))
	s.SetInt("download_min_height", clampInt(int(a.dlMinHeight), 0, 10000000))
	s.SetInt("download_max_height", clampInt(int(a.dlMaxHeight), 0, 10000000))
	s.SetInt("download_browser_mode", boolInt(a.dlBrowserMode))
	s.SetInt("download_concurrency", clampInt(int(a.downloadConc), 1, 8))
	s.SetInt("download_retries", clampInt(int(a.downloadRetries), 0, 10))
	s.SetInt("download_rate_kbps", clampInt(int(a.downloadRateKB), 0, 102400))
	s.SetInt("remove_watermark", boolInt(a.removeWatermark))
	a.noteWorkspacePage()
	a.saveImageEditSettings()
	a.savePromptParts()
	if a.workspacePage == store.PageGen || a.workspacePage == store.PageWash || a.workspacePage == store.PageFission {
		s.Set("workspace_page", a.workspacePage)
	}
	gen, wash, fission := a.stateOf(pageHome), a.stateOf(pageWash), a.stateOf(pageFission)
	s.SetInt("gen_stage", gen.stageTab)
	s.SetInt("gen_stage_picked", boolInt(gen.stagePicked))
	s.SetInt("wash_stage", wash.stageTab)
	s.SetInt("wash_stage_picked", boolInt(wash.stagePicked))
	s.SetInt("fission_stage", fission.stageTab)
	s.SetInt("fission_stage_picked", boolInt(fission.stagePicked))
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
	a.persistWorkspace(pageFission)
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
// 向导和主界面之间淡入，页面切换沿侧栏方向轻移；页内状态不变。
func (a *App) view(c *ui.Context) {
	c.SetZoom(interfaceZoom)
	a.flushUIUpdates()
	a.look = applyWorkbenchTheme(c)
	a.updateBanner(c)
	if a.wizardOn {
		ui.Column(c.Key("screen-wizard")).Fill().Transition(screenMotion()).Children(func() {
			a.wizardView(c)
		})
		return
	}
	a.noteTab()
	a.syncPage()
	ui.Column(c.Key("screen-main")).Fill().Background(a.look.paper).Transition(screenMotion()).Children(func() {
		a.topbar(c)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.navRail(c)
			pageKey := a.tab
			if pageKey == pageWash || pageKey == pageFission {
				pageKey = pageHome
			}
			page := ui.Column(c.Key(fmt.Sprintf("page-%d", pageKey))).Grow(1).MinWidth(0).MinHeight(0).Transition(a.pageMotion())
			if a.tab != pageRename && a.tab != pageImageEdit {
				page.Padding(6, pagePadX, 6, pagePadX)
			}
			page.Children(func() {
				switch a.tab {
				case pageWash:
					a.washPage(c)
				case pageFission:
					a.fissionPage(c)
				case pageDedup:
					a.dedupPage(c)
				case pageBrowser:
					a.browserPane(c)
				case pagePrompts:
					a.promptsPage(c)
				case pageDownload:
					a.downloadPage(c)
				case pageRename:
					a.renamePage(c)
				case pageImageEdit:
					a.imageEditPage(c)
				case pageSettings:
					a.settingsPage(c)
				default:
					a.homePage(c)
				}
			})
		})
	})
	a.browserDrawerView(c)
}

// missingBrowserNames 是已勾选、但还没有浏览器实例的平台。
func (a *App) missingBrowserNames() []string {
	// 扩展桥不使用本机实例。空的网页方式（部分测试）也不在这里拦截。
	switch a.browserBackend {
	case browser.KindCamoufox:
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

func (a *App) isQuitting() bool {
	if a == nil {
		return true
	}
	a.mu.Lock()
	q := a.quitting
	a.mu.Unlock()
	return q
}

// syncUI 把界面更新送回窗口线程。
func (a *App) syncUI(fn func()) {
	if fn == nil || a.isQuitting() || a.window == nil || a.window.IsDestroyed() {
		return
	}
	a.window.Update(fn)
}

// postUI 把后台结果送回界面；没有窗口时由下一帧统一应用。
func (a *App) postUI(fn func()) {
	if fn == nil || a.isQuitting() {
		return
	}
	if a.window != nil {
		if !a.window.IsDestroyed() {
			a.window.Update(fn)
		}
		return
	}
	a.mu.Lock()
	if !a.quitting {
		a.uiUpdates = append(a.uiUpdates, fn)
	}
	a.mu.Unlock()
}

func (a *App) flushUIUpdates() {
	a.mu.Lock()
	pending := a.uiUpdates
	a.uiUpdates = nil
	a.mu.Unlock()
	for _, fn := range pending {
		fn()
	}
}

// numberingFor 按页面取自动编号设置：生图、洗图各自勾选，前缀和序号参数共用。
func (a *App) numberingFor(page int) tasks.Numbering {
	enabled := a.genAutoNumber
	if page == pageWash {
		enabled = a.washAutoNumber
	}
	return tasks.Numbering{
		Enabled: enabled,
		Prefix:  a.numPrefix,
		Start:   clampInt(int(a.numStart), 0, 9_000_000_000_000_000),
		Width:   clampInt(int(a.numWidth), 0, 16),
	}
}

func (a *App) taskOptions(platforms, prompts, paths []string) tasks.Options {
	return tasks.Options{
		Platforms:           platforms,
		Prompts:             prompts,
		PromptSuffix:        a.promptSuffix,
		UseSuffix:           a.useSuffix,
		StructuredPrompt:    a.promptExtras(a.statePage, a.currentPromptParts()),
		Images:              paths,
		Concurrency:         a.taskConcurrency(),
		RetryCount:          int(a.retryCount),
		SendWait:            int(a.sendWait),
		WaitBeforeSend:      int(a.waitBefore),
		WaitResult:          a.waitResult,
		WaitResultSeconds:   int(a.waitResultS),
		TaskSwitching:       a.taskSwitch,
		TaskSwitchingCount:  int(a.switchCount),
		StartBrowserIndex:   int(a.startBrowserIdx),
		OutputPath:          a.outputPath,
		OutputSize:          a.doubaoOutputSize(),
		RemoveWatermark:     a.removeWatermark,
		DownloadConcurrency: int(a.downloadConc),
		DownloadRetries:     int(a.downloadRetries),
		DownloadRateKBps:    int(a.downloadRateKB),
		DailyCap:            a.shownDailyCap(),
		DailyCaps:           a.dailyCaps(),
		RiskyWords:          tasks.ParseWords(a.riskyWords),
		AgyBin:              a.agyBin,
		AgyModel:            a.agyModel,
		ChatGPTModel:        a.chatGPTModel,
		AgyHomeRoot:         a.agyHomeRoot,
		AgyTimeoutSec:       a.agyTimeoutSec(),
	}
}

const verificationHint = "在浏览器中完成验证后重新开始任务"

func taskResultSummary(queue []tasks.Task) string {
	tally := TallyTasks(queue)
	summary := fmt.Sprintf("成功 %d · 失败 %d · 待验证 %d", tally.Done, tally.Failed, tally.Needs)
	if tally.Review > 0 {
		summary += fmt.Sprintf(" · 需核对 %d", tally.Review)
	}
	return summary + fmt.Sprintf(" · 未完成 %d", tally.Incomplete())
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
			Multiple: page != pageFission,
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

// appendSources 在 UI 线程合并导入结果。洗图按路径去重，生图按内容去重。
func (a *App) appendSources(loaded []sourceImage, wash bool) int {
	key := func(si sourceImage) string {
		if wash {
			return filepath.Clean(si.Path)
		}
		return si.Hash
	}
	seen := make(map[string]bool, len(a.sources)+len(loaded))
	for _, si := range a.sources {
		seen[key(si)] = true
	}
	added := 0
	for _, si := range loaded {
		if !seen[key(si)] {
			a.sources = append(a.sources, si)
			seen[key(si)] = true
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
	return resources.AppIcon, nil
}

func sourcePaths(list []sourceImage) []string {
	paths := make([]string, 0, len(list))
	for _, si := range list {
		paths = append(paths, si.Path)
	}
	return paths
}
