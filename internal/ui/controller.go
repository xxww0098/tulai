// Package ui implements tulai's NSGo application controller, state bridges, and native views.
package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

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
)

const (
	batchText = iota
	batchImage
)

const settingTaskBrowserVisible = "task_browser_visible"

// Controller aliases App so callers and tests can reference either name.
type Controller = App

type sourceImage struct {
	Path       string
	Hash       string
	Washed     bool
	Washing    bool
	WashOutput string
}

type pageState struct {
	sources     []sourceImage
	srcSel      int
	prompts     string
	queue       []tasks.Task
	progress    float64
	progText    string
	stageTab    int
	stagePicked bool
	flowPin     int
	skipImages  bool
}

// App holds tulai's application state, stores, background task managers, and NSGo scope.
type App struct {
	mu sync.Mutex

	store    *store.Store
	manager  *browser.Manager
	engine   *tasks.Engine
	scope    *core.Scope
	dataRoot string

	// Native signal bumped inside engine goroutine to repaint BrowserStage & ImageEditStage.
	nativeTick *core.Signal[uint64]

	// Subscriber channels for "watchEvents" in TSX.
	watchersMu   sync.Mutex
	watchers     map[int]chan string
	nextWatcher  int
	inFlush      atomic.Bool
	flushEmitted atomic.Bool

	browserBackend   string
	browserChoice    string
	accountProxyID   int64
	accountProxy     string
	accountProxyNote string
	detectedNote     string
	detectedDetail   string
	camoufoxStatus   string
	camoufoxBusy     bool
	camoufoxPercent  float64
	camoufoxDone     int64
	camoufoxTotal    int64
	camoufoxSpeed    int64
	camoufoxLast     int64
	camoufoxStage    string
	camoufoxAt       time.Time
	camoufoxReady    bool

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
	dailyCap           float64
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

	// Download page
	dlURLs          string
	dlDir           string
	dlDepth         float64
	dlMinKB         float64
	dlMaxKB         float64
	dlMinWidth      float64
	dlMaxWidth      float64
	dlMinHeight     float64
	dlMaxHeight     float64
	dlBrowserMode   bool
	dlBrowser       downloadBrowser
	dlBrowserURL    string
	dlBrowserBusy   bool
	dlBrowserTask   *downloadTask
	dlBrowserErr    string
	dlBrowserCancel context.CancelFunc
	dlHost          *download.HostLimiter
	dlPathLock      *download.PathLocker
	dlTasks         []*downloadTask
	dlNextID        int
	dlFilter        int
	dlSearch        string
	dlSearchOn      bool
	dlComposerOn    bool
	dlComposerErr   string
	dlGuideOn       bool
	dlGuideSearch   string
	dlImport        *downloadImportState
	dlImportNotice  string
	dlGroupBySite   bool
	dlSelectedID    int

	// Prompts & structured prompt parts
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

	batchMode    int
	platformAuto bool
	logPaused    bool
	logPick      int
	logQuery     string
	logSource    string
	logExpanded  map[int]bool

	stageTab         int
	stagePicked      bool
	outputNote       string
	flowPin          int
	skipImages       bool
	galleryOn        bool
	galleryTitle     string
	gallerySelected  map[string]bool
	previewOn        bool
	previewPath      string
	statePage        int
	parked           map[int]pageState
	fissionRetryBase []tasks.Task
	runPage          int

	// Captcha & proxy
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

	// Workbench sources & subpages
	sources         []sourceImage
	srcSel          int
	dedup           dedupState
	fileRename      renameState
	edit            imageEditState
	uiUpdates       []func()
	persistDeferral int
	dirtyWorkspace  map[int]struct{}
	workspaceWrites int

	// Browsers & Antigravity accounts
	browserRows      []store.Browser
	browserChecked   map[int64]bool
	browserPending   map[int64]browserOperation
	browserBatch     *browserBatchRun
	browserDrawer    *browserDrawerState
	browserViewingID int64
	browserSel       int
	browserConfigID  int64
	browserConfigOn  bool
	newBrowserPlat   string
	newBrowserName   string
	platSynced       bool
	platSyncedTab    int
	renameID         int64
	renameDraft      string
	loginLaunch      func(store.Browser) error
	loginClosed      []int64
	afterClose       func()
	extToken         string
	extPort          string
	extTokenNote     string

	// Queue & workspace state
	queue           []tasks.Task
	logs            []logging.Entry
	progress        float64
	progText        string
	running         bool
	useRunView      bool
	genSession      int64
	washSession     int64
	fissionSession  int64
	workspacePage   string
	historyOpen     bool
	resultsDoneOpen bool
	laneCollapsed   bool
	stamps          map[string]int64

	tab          int
	settingsTab  int
	navCollapsed bool
	navManual    bool
	logOpen      bool
	darkTheme    bool

	// Update state
	updateBusy         bool
	updateChecks       int
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
	updateClient       *update.Client
	downloadsRunning   int

	// Wizard state
	wizardOn        bool
	wizardStep      wizard.Step
	wizardDir       int
	wizardPlan      wizard.BrowserPlan
	wizardQueue     wizard.Queue
	wizardAccount   store.Browser
	wizardExtra     bool
	wizardChoice    string
	wizardFirst     bool
	wizardAgy       wizardAgyState
	wizardPlanFn    func() wizard.BrowserPlan
	agyLocate       func(string) (string, bool)
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
	loginFresh      map[int64]struct{}
	loginProbed     map[int64]string
	accountProbe    func(store.Browser) (string, error)
	loginPoll       time.Duration
	loginFollow     func(store.Browser, string)
	accountOpen     func(store.Browser) error
	quitting        bool

	// Optional toast notification sent to TSX
	toastSeq   int
	toastText  string
	toastLevel string
}

var (
	activeAppMu sync.RWMutex
	activeApp   *App
)

// SetTestApp registers an isolated controller instance for nsgotest and Go tests.
func SetTestApp(a *App) {
	activeAppMu.Lock()
	activeApp = a
	activeAppMu.Unlock()
}

func getApp() *App {
	activeAppMu.RLock()
	a := activeApp
	activeAppMu.RUnlock()
	return a
}

// NewApp creates a standalone App controller initialized with store, manager, engine, and dataRoot.
func NewApp(st *store.Store, bm *browser.Manager, eng *tasks.Engine, dataRoot string) *App {
	a := &App{
		store:      st,
		manager:    bm,
		engine:     eng,
		dataRoot:   dataRoot,
		srcSel:     -1,
		browserSel: -1,
		dedup:      dedupState{level: dedupDefaultLevel},
		watchers:   map[int]chan string{},
		updateStop: make(chan struct{}),
	}
	if eng != nil {
		eng.DropInactiveExtensionAccounts()
	}
	if st != nil {
		a.loadSettings()
		a.applyWizardGate()
	}
	return a
}

// Run launches the NSGo desktop application hosting root.
func Run(st *store.Store, bm *browser.Manager, eng *tasks.Engine, dataRoot string, root func(*core.Scope) core.Node) {
	a := NewApp(st, bm, eng, dataRoot)
	SetTestApp(a)
	defer SetTestApp(nil)

	if a.engine != nil {
		if err := a.engine.ServeLocal(); err != nil && a.extTokenNote == "" {
			a.extTokenNote = "自动对接未启动，可复制令牌手动填写"
		}
	}
	a.wireHealing()

	loginCtx, loginStop := context.WithCancel(context.Background())
	var loginWG sync.WaitGroup
	defer func() {
		a.shutdown(loginStop, &loginWG)
	}()

	var opts []nsgo.Option
	opts = append(opts,
		nsgo.WindowSize(1160, 740),
		nsgo.WindowConstraints(core.WindowConstraints{MinWidth: 900, MinHeight: 620}),
	)

	nsgo.Run(func(cx *core.Scope) core.Node {
		a.BindScope(cx)
		if runtime.GOOS == "darwin" {
			desktop.SetApplicationMenu(cx, []desktop.MenuItem{
				{Label: "tulai", Items: []desktop.MenuItem{{Role: desktop.MenuAbout}, {Role: desktop.MenuQuit}}},
				{Label: "Edit", Items: []desktop.MenuItem{{Role: desktop.MenuUndo}, {Role: desktop.MenuRedo}, {Role: desktop.MenuCut}, {Role: desktop.MenuCopy}, {Role: desktop.MenuPaste}, {Role: desktop.MenuSelectAll}}},
			})
		}
		var logEmitPending atomic.Bool
		logging.Log.SetSink(func(logging.Entry) {
			if a.isQuitting() {
				return
			}
			if !logEmitPending.CompareAndSwap(false, true) {
				return
			}
			time.AfterFunc(60*time.Millisecond, func() {
				logEmitPending.Store(false)
				if !a.isQuitting() {
					a.emit("logs")
				}
			})
		})
		a.startAgyUsage()
		if a.engine != nil {
			loginWG.Add(1)
			go func() {
				defer loginWG.Done()
				a.engine.WatchLogins(loginCtx)
			}()
			a.engine.SetIdleGuard(func(id int64) bool {
				a.mu.Lock()
				defer a.mu.Unlock()
				return a.browserViewingID == id
			})
			a.engine.EnsureExtensionBridge(func() {
				if a.isQuitting() {
					return
				}
				a.emit("browsers", "workbench")
			})
		}
		a.startUpdateLoop()
		cx.Cleanup(func() {
			a.shutdown(loginStop, &loginWG)
		})
		if root != nil {
			return root(cx)
		}
		return core.View{}
	}, opts...)
}

// BindScope attaches an NSGo root scope to App for tests and runtime execution.
func (a *App) BindScope(cx *core.Scope) {
	if a == nil || cx == nil {
		return
	}
	a.scope = cx
	a.nativeTick = core.NewSignal[uint64](cx, 0)
}

func (a *App) shutdown(loginStop context.CancelFunc, loginWG *sync.WaitGroup) {
	if a == nil {
		return
	}
	a.closeBrowserDrawer()
	a.waitRename()
	if a.store != nil {
		a.saveSettings()
	}
	a.mu.Lock()
	alreadyQuitting := a.quitting
	a.quitting = true
	a.uiUpdates = nil
	a.mu.Unlock()
	if alreadyQuitting {
		return
	}
	logging.Log.SetSink(nil)
	if loginStop != nil {
		loginStop()
	}
	if loginWG != nil {
		loginWG.Wait()
	}
	a.stopAgyUsage()
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
	if a.engine != nil {
		a.engine.Stop()
	}
	if a.updateStop != nil {
		select {
		case <-a.updateStop:
		default:
			close(a.updateStop)
		}
	}
}

func (a *App) hasWindow() bool {
	return a != nil && a.scope != nil && !a.isQuitting()
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

func (a *App) subscribeEvents() (int, <-chan string) {
	a.watchersMu.Lock()
	defer a.watchersMu.Unlock()
	if a.watchers == nil {
		a.watchers = map[int]chan string{}
	}
	a.nextWatcher++
	id := a.nextWatcher
	ch := make(chan string, 64)
	a.watchers[id] = ch
	return id, ch
}

func (a *App) unsubscribeEvents(id int) {
	a.watchersMu.Lock()
	if ch, ok := a.watchers[id]; ok {
		delete(a.watchers, id)
		close(ch)
	}
	a.watchersMu.Unlock()
}

func (a *App) emit(topics ...string) {
	if a == nil || a.isQuitting() {
		return
	}
	if a.inFlush.Load() {
		a.flushEmitted.Store(true)
	}
	if len(topics) == 0 {
		topics = []string{"all"}
	}
	a.watchersMu.Lock()
	for _, ch := range a.watchers {
		for _, t := range topics {
			select {
			case ch <- t:
			default:
			}
		}
	}
	a.watchersMu.Unlock()

	if a.scope != nil && a.nativeTick != nil {
		needNative := false
		for _, t := range topics {
			if t == "browserDrawer" || t == "imageedit" || t == "all" {
				needNative = true
				break
			}
		}
		if needNative {
			tick := a.nativeTick
			a.scope.Post(func() {
				tick.Set(tick.GetUntracked() + 1)
			})
		}
	}
}

func (a *App) pushToast(level, msg string) {
	if a == nil || strings.TrimSpace(msg) == "" {
		return
	}
	a.toastSeq++
	a.toastLevel = level
	a.toastText = msg
	a.emit("toast")
}

// postUI queues callbacks for the engine goroutine. Tests without a scope
// drive the same queue by calling flushUIUpdates on their owning goroutine.
func (a *App) postUI(fn func()) {
	if a == nil || fn == nil {
		return
	}
	a.mu.Lock()
	if a.quitting {
		a.mu.Unlock()
		return
	}
	a.uiUpdates = append(a.uiUpdates, fn)
	sc := a.scope
	a.mu.Unlock()
	if sc != nil {
		sc.Post(a.flushUIUpdates)
	}
}

func (a *App) onUI(fn func()) {
	a.postUI(fn)
}

func (a *App) syncUI(fn func()) {
	if a == nil || fn == nil {
		return
	}
	if !a.hasWindow() {
		fn()
		return
	}
	done := make(chan struct{})
	a.postUI(func() {
		defer close(done)
		fn()
	})
	<-done
}

func (a *App) flushUIUpdates() {
	if a == nil {
		return
	}
	for {
		a.mu.Lock()
		if len(a.uiUpdates) == 0 {
			a.mu.Unlock()
			return
		}
		batch := a.uiUpdates
		a.uiUpdates = nil
		a.mu.Unlock()
		a.flushEmitted.Store(false)
		a.inFlush.Store(true)
		for _, fn := range batch {
			fn()
		}
		a.inFlush.Store(false)
		if !a.flushEmitted.Load() {
			a.emit("all")
		}
	}
}

// onScope runs fn on the NSGo engine goroutine and waits for completion or ctx cancellation.
func (a *App) onScope(ctx context.Context, fn func()) error {
	if a == nil || a.scope == nil {
		fn()
		return nil
	}
	done := make(chan struct{})
	a.scope.Post(func() {
		defer close(done)
		fn()
	})
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) pickFilesAsync(ctx context.Context, title string, multiple bool, types []string, typesName string) ([]string, error) {
	if a == nil || a.scope == nil {
		return nil, nil
	}
	type pickRes struct {
		paths []string
		err   error
	}
	ch := make(chan pickRes, 1)
	a.scope.Post(func() {
		a.scope.PickFiles(core.PickFilesOptions{
			Multiple:  multiple,
			Title:     title,
			Types:     types,
			TypesName: typesName,
		}, func(files []core.File, err error) {
			var paths []string
			for _, f := range files {
				if p := strings.TrimSpace(f.Path); p != "" {
					paths = append(paths, p)
				}
			}
			ch <- pickRes{paths: paths, err: err}
		})
	})
	select {
	case r := <-ch:
		return r.paths, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *App) pickDirsAsync(ctx context.Context, title string, multiple bool) ([]string, error) {
	if a == nil || a.scope == nil {
		return nil, nil
	}
	type pickRes struct {
		paths []string
		err   error
	}
	ch := make(chan pickRes, 1)
	a.scope.Post(func() {
		a.scope.PickDirectories(core.PickDirectoriesOptions{
			Multiple: multiple,
			Title:    title,
		}, func(dirs []core.Directory, err error) {
			var paths []string
			for _, d := range dirs {
				if p := strings.TrimSpace(d.Path); p != "" {
					paths = append(paths, p)
				}
			}
			ch <- pickRes{paths: paths, err: err}
		})
	})
	select {
	case r := <-ch:
		return r.paths, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *App) saveFileAsync(ctx context.Context, name, mime string, data []byte) (bool, string, error) {
	if a == nil || a.scope == nil {
		return false, "", nil
	}
	type saveRes struct {
		saved bool
		path  string
		err   error
	}
	ch := make(chan saveRes, 1)
	a.scope.Post(func() {
		a.scope.SaveFile(core.SaveOptions{
			Name: name,
			MIME: mime,
			Data: data,
		}, func(saved bool, path string, err error) {
			ch <- saveRes{saved: saved, path: path, err: err}
		})
	})
	select {
	case r := <-ch:
		return r.saved, r.path, r.err
	case <-ctx.Done():
		return false, "", ctx.Err()
	}
}

func (a *App) openPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || a == nil || a.scope == nil {
		return nil
	}
	ch := make(chan error, 1)
	a.scope.Post(func() {
		a.scope.OpenFile(core.SharedFile{Path: path}, func(_ bool, err error) {
			ch <- err
		})
	})
	return <-ch
}

func (a *App) revealPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || a == nil || a.scope == nil {
		return nil
	}
	ch := make(chan error, 1)
	a.scope.Post(func() {
		a.scope.RevealFile(path, func(err error) {
			ch <- err
		})
	})
	return <-ch
}

func (a *App) copyText(text string) {
	if a == nil || a.scope == nil {
		return
	}
	a.scope.Post(func() {
		a.scope.SetClipboard(text)
	})
}

func (a *App) confirmAlert(title, message, confirmLabel string) bool {
	if a == nil || a.scope == nil {
		return true
	}
	ch := make(chan int, 1)
	a.scope.Post(func() {
		a.scope.ShowAlert(core.AlertConfig{
			Title:      title,
			Message:    message,
			Cancelable: true,
			Actions: []core.AlertAction{
				{Label: "取消", Role: core.AlertCancel},
				{Label: confirmLabel, Role: core.AlertDefault},
			},
		}, func(idx int) {
			ch <- idx
		})
	})
	return <-ch == 1
}

// loadSettings loads all application configuration from SQLite.
func (a *App) loadSettings() {
	s := a.store
	if s == nil {
		return
	}
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
	chosenPlatform := "豆包"
	for _, p := range s.Platforms() {
		switch p {
		case "豆包", "千问", agy.Platform, "ChatGPT":
			chosenPlatform = p
		default:
			continue
		}
		break
	}
	a.setSinglePlatform(chosenPlatform)
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
	a.platformAuto = false
	a.sendStrategy = pickStr(s.Get("send_strategy"), "顺序循环", []string{"顺序循环", "随机循环"})
	a.concurrencyAuto = s.GetInt("concurrency") != 1
	a.retryCount = float64(clampInt(s.GetInt("retry_count"), 0, 20))
	a.sendWait = float64(clampInt(s.GetInt("send_wait_seconds"), 0, 600))
	a.dailyCap = float64(clampInt(s.GetInt("daily_cap"), 0, 200))
	a.dailyCapSet = true
	capOr := func(key string, fallback float64) (float64, bool) {
		if strings.TrimSpace(s.Get(key)) == "" {
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
	if raw := s.Get("number_start"); raw != "" {
		a.numStart = float64(clampInt(s.GetInt("number_start"), 0, 9_000_000_000_000_000))
	} else {
		a.numStart = 1
	}
	if raw := s.Get("number_width"); raw != "" {
		a.numWidth = float64(clampInt(s.GetInt("number_width"), 0, 16))
	} else {
		a.numWidth = 0
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
		a.downloadConc = 4
	} else {
		a.downloadConc = float64(clampInt(s.GetInt("download_concurrency"), 1, 8))
	}
	if raw := s.Get("download_retries"); raw == "" {
		a.downloadRetries = 1
	} else {
		a.downloadRetries = float64(clampInt(s.GetInt("download_retries"), 0, 10))
	}
	a.downloadRateKB = float64(clampInt(s.GetInt("download_rate_kbps"), 0, 102400))
	a.removeWatermark = s.GetBool("remove_watermark")
	a.prompts = s.Get("prompts")
	a.freeformPrompts = s.Get("prompts_freeform")
	a.userPrompts = prompts.ParseUser(s.Get("user_prompts"))
	a.promptPick = s.Get("prompt_pick")
	a.promptChoice = a.promptPick
	a.genPromptChoice = a.promptPick
	a.promptSuffix = s.Get("prompt_suffix")
	a.useSuffix = s.GetBool("use_suffix")
	a.captchaEnabled = s.GetBool("captcha_enabled")
	a.captchaBackend = pickStr(s.Get("captcha_backend"), "free", []string{"free", "custom", "ikunocr"})
	a.captchaChoice = a.captchaBackend
	a.customURL = s.Get("captcha_custom_url")
	a.customKey = s.Get("captcha_custom_key")
	a.customModel = s.Get("captcha_custom_model")
	a.ikunocrKey = s.Get("captcha_ikunocr_key")
	a.proxyCustom = s.Get("proxy_mode") == "custom"
	a.proxyURL = strings.TrimSpace(s.Get("proxy_server"))
	a.proxySaved = a.proxyURL
	a.pushProxy()
	if a.manager != nil {
		a.manager.SetTaskVisible(s.GetBool(settingTaskBrowserVisible))
	}
	a.darkTheme = s.Get("ui_theme") == "dark"

	a.refreshBrowsers()
	a.newBrowserPlat = "豆包"
	a.logs = logging.Log.Entries()

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
	switch s.Get("workspace_page") {
	case store.PageGen:
		a.tab = pageHome
		a.workspacePage = store.PageGen
	case store.PageFission:
		a.tab = pageFission
		a.workspacePage = store.PageFission
	default:
		a.tab = pageWash
		a.workspacePage = store.PageWash
	}
	a.loadDownloadRecords()
	a.syncPage()
}

// saveSettings persists application configuration to SQLite.
func (a *App) saveSettings() {
	if a == nil || a.store == nil {
		return
	}
	s := a.store
	s.Set("browser_backend", a.browserBackend)
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
	words := a.riskyWords
	if strings.TrimSpace(words) == "" {
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
	a.saveDedupSettings(s)
	a.persistUpdatePrefs()
	a.noteWorkspacePage()
	if a.workspacePage != "" {
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
	s.Set("source_images", joinPaths(sourcePaths(gen.sources)))
	if a.darkTheme {
		s.Set("ui_theme", "dark")
	} else {
		s.Set("ui_theme", "light")
	}
	a.saveImageEditSettings()
	a.savePromptParts()
	a.persistWorkspace(pageHome)
	a.persistWorkspace(pageWash)
	a.persistWorkspace(pageFission)
	a.saveDownloadRecords()
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

func (a *App) configuredBrowsers() []store.Browser {
	if a == nil {
		return nil
	}
	if a.store != nil {
		return a.store.Browsers()
	}
	return a.browserRows
}

func (a *App) missingBrowserNames(rows []store.Browser) []string {
	switch a.browserBackend {
	case browser.KindCamoufox, browser.KindChromeCDP:
	default:
		return nil
	}
	if a.extensionTasks() {
		return nil
	}
	have := map[string]bool{}
	for _, b := range rows {
		if b.ExtAccount != "" {
			continue
		}
		have[b.Platform] = true
	}
	var missing []string
	for _, p := range a.activePlatforms() {
		if !have[p] {
			missing = append(missing, p)
		}
	}
	return missing
}

func (a *App) extensionTasks() bool {
	return false
}

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

func (a *App) taskOptions(platforms, promptList, paths []string) tasks.Options {
	opts := tasks.Options{
		Platforms:           platforms,
		Prompts:             promptList,
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
		Numbering:           a.numberingFor(a.statePage),
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
	switch a.statePage {
	case pageWash:
		opts.Wash = true
		opts.UseSuffix = false
		opts.SoloImages = a.washSoloPaths()
	case pageFission:
		opts.Fission = true
		opts.UseSuffix = false
		opts.StructuredPrompt = ""
		opts.WaitResult = true
		opts.RemoveWatermark = false
	}
	return opts
}

func (a *App) resolvedOutputDir() string {
	dir := strings.TrimSpace(a.outputPath)
	if dir != "" {
		return dir
	}
	if a != nil && strings.TrimSpace(a.dataRoot) != "" {
		return filepath.Join(a.dataRoot, "output")
	}
	return filepath.Join(mustWd(), "output")
}

func (a *App) stopTasks() {
	if a.engine != nil {
		a.engine.Stop()
	}
}

func (a *App) ingestSources(paths []string) {
	if a.running || len(paths) == 0 {
		return
	}
	if !a.hasWindow() {
		a.addLoaded(a.statePage, loadSourceFiles(paths))
		return
	}
	page := a.statePage
	go func() {
		loaded := loadSourceFiles(paths)
		a.postUI(func() { a.addLoaded(page, loaded) })
	}()
}

func loadSourceFiles(paths []string) []sourceImage {
	var loaded []sourceImage
	for _, p := range expandImagePaths(paths) {
		data, err := os.ReadFile(p)
		if err != nil || !images.IsValidImageBytes(data) {
			continue
		}
		loaded = append(loaded, sourceImage{Path: p, Hash: images.SHA256(data)})
	}
	return loaded
}

func expandImagePaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			out = append(out, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			continue
		}
		var names []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			switch strings.ToLower(filepath.Ext(entry.Name())) {
			case ".png", ".jpg", ".jpeg", ".webp", ".gif":
				names = append(names, filepath.Join(p, entry.Name()))
			}
		}
		sort.Strings(names)
		out = append(out, names...)
	}
	return out
}

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

func (a *App) removeSelected() {
	if a.srcSel >= 0 && a.srcSel < len(a.sources) {
		a.sources = append(a.sources[:a.srcSel], a.sources[a.srcSel+1:]...)
		a.srcSel = -1
	}
}

func sourcePaths(list []sourceImage) []string {
	paths := make([]string, 0, len(list))
	for _, si := range list {
		paths = append(paths, si.Path)
	}
	return paths
}

func currentVersion() string {
	if v := strings.TrimSpace(core.App().Version); v != "" {
		return v
	}
	return "未打包"
}

func fileURL(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "file://") || strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") || strings.HasPrefix(p, "data:") {
		return p
	}
	abs, err := filepath.Abs(p)
	if err == nil {
		p = abs
	}
	slashed := filepath.ToSlash(p)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return (&url.URL{Scheme: "file", Path: slashed}).String()
}

func toRGBA(img image.Image) *image.RGBA {
	if img == nil {
		return nil
	}
	if r, ok := img.(*image.RGBA); ok {
		return r
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

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

func pickStr(val, fallback string, options []string) string {
	for _, o := range options {
		if val == o {
			return val
		}
	}
	return fallback
}

func containsStr(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func trimSource(source string) string {
	if len([]rune(source)) <= 12 {
		return source
	}
	return string([]rune(source)[:11]) + "…"
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KB", "MB", "GB"} {
		value /= unit
		if value < unit || suffix == "GB" {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%d B", n)
}

func formatBytesMB(n int64) string {
	if n <= 0 {
		return "0 MB"
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func trimRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return shortLine(s, n)
}

func hasQzoneCredentials(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		if download.QzoneURLHasCredentials(strings.TrimSpace(line)) {
			return true
		}
	}
	return false
}

func redactQzoneURLs(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		lines[i] = download.RedactQzoneCredentials(strings.TrimSpace(line))
	}
	return strings.Join(lines, "\n")
}

// suppress unused import if strconv isn't referenced yet
var _ = strconv.Itoa
var _ = errors.New
