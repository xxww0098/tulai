// Package ui implements tulai's MyGo application controller, bound services, and media handler.
package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"io"
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

	"github.com/egoist/mygo"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/download"
	"github.com/xxww0098/tulai/internal/imageedit"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/update"
)

const (
	batchText = iota
	batchImage
)

const (
	settingTaskBrowserVisible       = "task_browser_visible"
	settingDismissedSharedExitHints = "dismissed_shared_exit_hints"
)

const (
	settingDotMatrixEffect = "dot_matrix_effect"
	defaultDotMatrixEffect = "grain"
)

var validDotMatrixEffects = []string{
	"ripple",
	"fbm",
	"helix",
	"grain",
	"spiral",
	"warp",
	"plasma",
	"band",
	"snowflakes",
}

func normalizeDotMatrixEffect(raw string) string {
	return pickStr(strings.TrimSpace(raw), defaultDotMatrixEffect, validDotMatrixEffects)
}

// appTitle is the product name the window title, the macOS app menu and the
// brand header show. The binary, bundle and module keep the ASCII name tulai.
const appTitle = "图来"

// Controller aliases App so callers and tests can reference either name.
type Controller = App

type sourceImage struct {
	Path       string
	Hash       string
	Washed     bool
	Washing    bool
	WashOutput string
}

type rewashSnapshot struct {
	page         int
	sources      []sourceImage
	queue        []tasks.Task
	progress     float64
	progText     string
	count        int
	rewashCounts map[string]int
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

type workspaceSlot struct {
	platform string
	page     int
}

type platformWorkspace struct {
	pages               map[int]pageState
	batchMode           int
	textPrompts         string
	imgPrompts          string
	freeformPrompts     string
	imgFreeformPrompts  string
	promptPick          string
	imgPromptPick       string
	promptChoice        string
	genPromptChoice     string
	imgGenPromptChoice  string
	washPromptChoice    string
	fissionPromptChoice string
	textRepeat          int
	imageRepeat         int
	imagePromptMode     string
	genSession          int64
	washSession         int64
	fissionSession      int64
	rewashCounts        map[string]int
	rewashUndo          *rewashSnapshot
	resultsDoneOpen     bool
	gallerySelected     map[string]bool
	fissionRetryBase    []tasks.Task
}

// App holds tulai's application state, stores, background task managers, and desktop host.
type App struct {
	mu sync.Mutex

	store    *store.Store
	manager  *browser.Manager
	engine   *tasks.Engine
	host     DesktopHost
	media    *MediaHandler
	dataRoot string

	// Single-goroutine controller actor channels (started in ui.Run).
	actorCh   chan func()
	actorWake chan struct{}
	actorStop chan struct{}
	actorDone chan struct{}

	// Shared media buffers read by MediaHandler HTTP goroutines.
	mediaMu      sync.RWMutex
	drawerFrame  []byte
	editOrigPath string
	editOutJPEG  []byte

	// Subscriber channels for coalesced topic updates.
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
	runtimeStatus    string
	runtimeBusy      bool
	runtimePercent   float64
	runtimeDone      int64
	runtimeTotal     int64
	runtimeSpeed     int64
	runtimeLast      int64
	runtimeStage     string
	runtimeAt        time.Time
	runtimeReady     bool

	platDoubao              bool
	platQianwen             bool
	platQwenIntl            bool
	platGemini              bool
	platJimeng              bool
	sendStrategy            string
	concurrencyAuto         bool
	concurrencyMax          float64
	concurrencyAutoQianwen  bool
	concurrencyMaxQianwen   float64
	concurrencyAutoQwenIntl bool
	concurrencyMaxQwenIntl  float64
	concurrencyAutoGemini   bool
	concurrencyMaxGemini    float64
	concurrencyAutoJimeng   bool
	concurrencyMaxJimeng    float64
	retryCount              float64
	sendWait                float64
	dailyCap                float64
	dailyCapSet             bool
	dailyCapQianwen         float64
	dailyCapQianwenSet      bool
	dailyCapQwenIntl        float64
	dailyCapQwenIntlSet     bool
	dailyCapGemini          float64
	dailyCapGeminiSet       bool
	dailyCapJimeng          float64
	dailyCapJimengSet       bool
	riskyWords              string
	pickFile                func() (string, error)
	waitBefore              float64
	waitResult              bool
	waitResultS             float64
	taskSwitch              bool
	switchCount             float64
	startBrowserIdx         float64
	outputPath              string
	genAutoNumber           bool
	washAutoNumber          bool
	numPrefix               string
	numStart                float64
	numWidth                float64
	doubaoOutput            outputSizePrefs
	qianwenOutput           outputSizePrefs
	qwenIntlOutput          outputSizePrefs
	geminiOutput            outputSizePrefs
	jimengOutput            outputSizePrefs
	outputCaps              map[string]store.ProviderOutputCaps
	outputCapsSyncing       map[string]bool
	washStyle          string
	removeWatermark    bool
	autoDeleteHistory  bool
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
	prompts             string
	textPrompts         string
	imgPrompts          string
	userPrompts         []prompts.UserPrompt
	promptPick          string
	imgPromptPick       string
	promptChoice        string
	freeformPrompts     string
	imgFreeformPrompts  string
	promptNote          string
	genPromptChoice     string
	imgGenPromptChoice  string
	promptEditPick      string
	promptEditChoice    string
	promptEditText      string
	promptEditName      string
	promptEditKind      string
	promptEditDraft     string
	promptDraftName     string
	promptDraftKind     string
	promptFilter        string
	promptEditNote      string
	washPromptChoice    string
	fissionPromptChoice string
	promptSuffix        string
	useSuffix           bool
	structuredGen       promptParts
	structuredWash      promptParts
	structuredReady     bool
	structuredOpen      bool
	structuredPage      int

	batchMode       int
	textRepeat      int
	imageRepeat     int
	imagePromptMode string
	platformAuto    bool
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
	platformSpaces   map[string]platformWorkspace
	fissionRetryBase []tasks.Task
	runPage          int
	runPlatform      string

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
	dirtyWorkspace  map[workspaceSlot]struct{}
	workspaceWrites int
	runPersistTimer *time.Timer
	runPersistDirty map[workspaceSlot]struct{}
	runEmitPending  bool

	// Browsers
	browserRows              []store.Browser
	browserChecked           map[int64]bool
	browserPending           map[int64]browserOperation
	dismissedSharedExitHints map[string]string
	browserProbing   atomic.Bool
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
	renameID           int64
	renameDraft        string
	loginLaunch        func(store.Browser) error
	clearHistoryFn     func(context.Context, store.Browser, strategy.ClearHistoryOptions) (strategy.ClearHistoryResult, error)
	clearHistoryCancel context.CancelFunc
	syncAccountNameFn  func(context.Context, store.Browser) (string, error)
	syncNamesCancel    context.CancelFunc
	syncQwenStatusFn   func(context.Context, store.Browser) (cdp.QwenStatus, error)
	syncDoubaoStatusFn func(context.Context, store.Browser) (cdp.DoubaoStatus, error)
	syncGeminiStatusFn func(context.Context, store.Browser) (cdp.GeminiStatus, error)
	syncJimengStatusFn func(context.Context, store.Browser) (cdp.JimengStatus, error)
	syncQwenCancel     context.CancelFunc
	probeDoubaoImageFn func(context.Context, store.Browser) doubaoImageProbeOutcome
	probeImageCancel   context.CancelFunc
	qwenStatusProbe    func(context.Context, string) (cdp.QwenStatus, error)
	doubaoStatusProbe  func(context.Context, string) (cdp.DoubaoStatus, error)
	geminiStatusProbe  func(context.Context, string) (cdp.GeminiStatus, error)
	jimengStatusProbe  func(context.Context, string) (cdp.JimengStatus, error)
	outputCapsProbe    func(context.Context, string, string) (store.ProviderOutputCaps, error)
	syncOutputCapsFn   func(context.Context, string, store.Browser) (store.ProviderOutputCaps, error)
	qwenLiveProbing    atomic.Bool
	loginClosed        []int64
	afterClose         func()

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
	resultsDoneOpen bool
	laneCollapsed   bool
	rewashUndo      *rewashSnapshot
	rewashCounts    map[string]int
	stamps          map[string]int64

	tab             int
	settingsTab     int
	navCollapsed    bool
	navManual       bool
	logOpen         bool
	darkTheme       bool
	dotMatrixEffect string

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
	updatePhase        string
	updateFraction     float64
	updateDone         int64
	updateTotal        int64
	updateSpeed        int64
	updateEta          int
	updateSource       string
	updateAssetName    string
	updateLastAt       time.Time
	updateProgressText string
	updateCancel       context.CancelFunc
	updateCancelled    bool
	updateGen          uint64
	updateAsset        string
	updateAssetVersion string
	updateAssetMode    update.Mode
	updateAssetSHA256  string
	updateQuitting     bool
	updateInstalled    string
	updateStop         chan struct{}
	updateRel          update.Release
	updateClient       *update.Client
	downloadsRunning   int

	// Wizard state
	wizardOn        bool
	wizardStep      wizardStep
	wizardDir       int
	wizardPlan      wizardBrowserPlan
	wizardQueue     wizardQueue
	wizardAccount   store.Browser
	wizardExtra     bool
	wizardChoice    string
	wizardFirst     bool
	wizardPlanFn    func() wizardBrowserPlan
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
	loginHold        map[int64]struct{}
	loginFresh       map[int64]struct{}
	loginAutoName    map[int64]string
	loginProbed      map[int64]string
	accountProbe     func(store.Browser) (string, error)
	accountNameProbe func(ctx context.Context, endpoint string) (string, error)
	loginPoll        time.Duration
	accountNameWait  time.Duration
	accountNamePoll  time.Duration
	loginFollow      func(store.Browser, string)
	accountOpen      func(store.Browser) error
	quitting         bool

	// Optional toast notification sent to TSX
	toastSeq   int
	toastText  string
	toastLevel string
}

var (
	activeAppMu              sync.RWMutex
	activeApp                *App
	runPersistCoalesceWindow = 200 * time.Millisecond
)

// SetTestApp registers an isolated controller instance for Go unit tests.
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
	a.media = NewMediaHandler(a)
	if bm != nil {
		bm.SetExitHandler(a.handleBrowserExit)
	}
	if st != nil {
		a.loadSettings()
		a.applyWizardGate()
	}
	return a
}

// Run launches the MyGo desktop application hosting tulai's Web Frontend.
func Run(st *store.Store, bm *browser.Manager, eng *tasks.Engine, dataRoot string) {
	a := NewApp(st, bm, eng, dataRoot)
	SetTestApp(a)
	defer SetTestApp(nil)

	a.wireHealing()

	host := &mygoHost{}
	a.BindHost(host)
	a.StartActor()
	stopPump := a.startEventPump(host)

	RegisterBindings(a)
	if err := mygo.Protocol.Handle(MediaScheme, a.media); err != nil {
		logging.Log.Errorf("注册本地媒体协议失败: %v", err)
	}
	applyMyGoTheme(a.darkTheme)

	if runtime.GOOS == "darwin" {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{
				Label: appTitle,
				Submenu: []*mygo.MenuItem{
					{Role: mygo.RoleAbout},
					mygo.Separator(),
					{Role: mygo.RoleHide},
					{Role: mygo.RoleHideOthers},
					{Role: mygo.RoleUnhide},
					mygo.Separator(),
					{Role: mygo.RoleQuit},
				},
			},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleWindowMenu},
		}))
	}

	loginCtx, loginStop := context.WithCancel(context.Background())
	var loginWG sync.WaitGroup
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			a.shutdown(loginStop, &loginWG)
			stopPump()
		})
	}
	defer cleanup()

	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if e != nil && e.SessionEnding {
			// OS logout/shutdown cannot be canceled; avoid dialogs and let OnQuit run fast cleanup.
			return
		}
	})
	mygo.App.OnWillQuit(func(_ *mygo.QuitEvent) {})
	mygo.App.OnQuit(func() {
		cleanup()
	})

	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:                appTitle,
			URL:                  "/",
			Width:                1160,
			Height:               740,
			MinWidth:             900,
			MinHeight:            620,
			StateKey:             "main",
			Hidden:               true,
			BackgroundColor:      "light-dark(#F2F4F7, #0C1017)",
			TitleBarStyle:        mygo.TitleBarHiddenInset,
			TrafficLightPosition: &mygo.Point{X: 14, Y: 14},
			TitleBarHeight:       38,
			Page: mygo.PageOptions{
				DevTools:             mygo.DevToolsAuto,
				BackgroundThrottling: mygo.ThrottlingOff,
			},
		})
		host.setWindow(win)
		win.OnReadyToShow(func() {
			win.Show()
		})
		win.OnFileDrop(func(e *mygo.FileDropEvent) {
			if e == nil || len(e.Paths) == 0 {
				return
			}
			_ = EventFileDrop.Emit(win, FileDropEvent{
				Paths: append([]string(nil), e.Paths...),
				X:     e.X,
				Y:     e.Y,
			})
		})
		if pg := win.Page(); pg != nil {
			pg.OnContextMenu(func(e *mygo.ContextMenuEvent) {
				if e != nil {
					e.PreventDefault()
				}
			})
			pg.OnWillNavigate(func(e *mygo.NavigateEvent) {
				if e == nil || isAllowedAppNavigation(e.URL) {
					return
				}
				e.PreventDefault()
				if e.UserInitiated && (strings.HasPrefix(e.URL, "http://") || strings.HasPrefix(e.URL, "https://")) {
					target := e.URL
					go func() { _ = mygo.Shell.OpenExternal(target) }()
				}
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
		if a.engine != nil {
			loginWG.Add(1)
			go func() {
				defer loginWG.Done()
				a.engine.WatchLogins(loginCtx)
			}()
		}
		a.startUpdateLoop()
	})

	if err := mygo.App.Run(); err != nil {
		logging.Log.Errorf("MyGo 运行异常退出: %v", err)
	}
}

func isAllowedAppNavigation(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	return u == "" ||
		strings.HasPrefix(u, "mygo://") ||
		strings.HasPrefix(u, MediaScheme+"://") ||
		strings.HasPrefix(u, "http://mygo.localhost") ||
		strings.HasPrefix(u, "http://"+MediaScheme+".localhost") ||
		strings.HasPrefix(u, "http://localhost:") ||
		strings.HasPrefix(u, "http://127.0.0.1:") ||
		strings.HasPrefix(u, "about:")
}

// BindHost attaches a DesktopHost implementation to App for runtime or tests.
func (a *App) BindHost(h DesktopHost) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.host = h
	a.mu.Unlock()
}

func (a *App) shutdown(loginStop context.CancelFunc, loginWG *sync.WaitGroup) {
	if a == nil {
		return
	}
	a.StopActor()
	a.mu.Lock()
	alreadyQuitting := a.quitting
	a.quitting = true
	a.uiUpdates = nil
	if a.runPersistTimer != nil {
		a.runPersistTimer.Stop()
		a.runPersistTimer = nil
	}
	a.mu.Unlock()
	if alreadyQuitting {
		return
	}
	a.closeBrowserDrawer()
	a.waitRename()
	if a.store != nil {
		a.saveSettings()
	}
	logging.Log.SetSink(nil)
	if loginStop != nil {
		loginStop()
	}
	if loginWG != nil {
		loginWG.Wait()
	}
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
	if a == nil {
		return false
	}
	a.mu.Lock()
	ok := a.host != nil && !a.quitting
	a.mu.Unlock()
	return ok
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

// postUI queues callbacks for the controller actor goroutine. Headless tests
// without a running actor drive the same queue by calling flushUIUpdates directly.
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
	wakeCh := a.actorWake
	a.mu.Unlock()
	if wakeCh != nil {
		select {
		case wakeCh <- struct{}{}:
		default:
		}
	}
}

func (a *App) onUI(fn func()) {
	a.postUI(fn)
}

func (a *App) syncUI(fn func()) {
	if a == nil || fn == nil {
		return
	}
	if !a.actorActive() {
		fn()
		return
	}
	_ = a.call(context.Background(), fn)
}

func (a *App) flushUIUpdates() {
	if a == nil {
		return
	}
	for {
		a.mu.Lock()
		if len(a.uiUpdates) == 0 {
			hasRuntime := a.host != nil || a.actorCh != nil
			a.mu.Unlock()
			if a.runEmitPending || len(a.runPersistDirty) > 0 {
				if !hasRuntime {
					a.flushRunWorkspaceUpdate()
				} else {
					a.ensureRunPersistTimer()
				}
			}
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

// onScope runs fn on the controller actor goroutine and waits for completion or ctx cancellation.
func (a *App) onScope(ctx context.Context, fn func()) error {
	return a.call(ctx, fn)
}

func (a *App) desktopHost() DesktopHost {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	h := a.host
	a.mu.Unlock()
	return h
}

func (a *App) pickFilesAsync(ctx context.Context, title string, multiple bool, types []string, typesName string) ([]string, error) {
	h := a.desktopHost()
	if h == nil {
		return nil, nil
	}
	var filters []FileFilter
	if len(types) > 0 {
		filters = []FileFilter{{Name: typesName, Extensions: types}}
	}
	return h.PickFiles(ctx, title, multiple, filters)
}

func (a *App) pickDirsAsync(ctx context.Context, title string, multiple bool) ([]string, error) {
	h := a.desktopHost()
	if h == nil {
		return nil, nil
	}
	return h.PickDirectories(ctx, title, multiple)
}

func (a *App) saveFileAsync(ctx context.Context, name, mime string, data []byte) (bool, string, error) {
	h := a.desktopHost()
	if h == nil {
		return false, "", nil
	}
	return h.SaveFile(ctx, name, mime, data)
}

func (a *App) openPath(path string) error {
	path = strings.TrimSpace(path)
	h := a.desktopHost()
	if path == "" || h == nil {
		return nil
	}
	return h.OpenPath(path)
}

func (a *App) openExternal(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("不支持的外部链接: %s", rawURL)
	}
	h := a.desktopHost()
	if h == nil {
		return nil
	}
	return h.OpenExternal(parsed.String())
}

func (a *App) revealPath(path string) error {
	path = strings.TrimSpace(path)
	h := a.desktopHost()
	if path == "" || h == nil {
		return nil
	}
	return h.RevealPath(path)
}

var pngHeaderMagic = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func (a *App) copyText(text string) {
	if h := a.desktopHost(); h != nil {
		_ = h.SetClipboard(text)
	}
}

func (a *App) copyImage(path string) error {
	path = strings.TrimSpace(path)
	h := a.desktopHost()
	if path == "" || h == nil {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pngBytes := raw
	if !bytes.HasPrefix(raw, pngHeaderMagic) {
		img, err := imageedit.DecodeImage(raw)
		if err != nil {
			return fmt.Errorf("无法解析图片格式: %w", err)
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return err
		}
		pngBytes = buf.Bytes()
	}
	return h.SetClipboardImage(pngBytes)
}

func (a *App) trashPath(path string) error {
	path = strings.TrimSpace(path)
	h := a.desktopHost()
	if path == "" || h == nil {
		return nil
	}
	return h.TrashPath(path)
}

func (a *App) confirmAlert(title, message, confirmLabel string) bool {
	h := a.desktopHost()
	if h == nil {
		return true
	}
	return h.ConfirmAlert(title, message, confirmLabel)
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
	chosenPlatform := "豆包"
	for _, p := range s.Platforms() {
		switch p {
		case "豆包", "千问", "千问国际", "Gemini", "即梦":
			chosenPlatform = p
		default:
			continue
		}
		break
	}
	a.setSinglePlatform(chosenPlatform)
	a.platformAuto = false
	a.sendStrategy = pickStr(s.Get("send_strategy"), "顺序循环", []string{"顺序循环", "随机循环"})
	a.concurrencyAuto = s.GetInt("concurrency") != 1
	if raw := strings.TrimSpace(s.Get("concurrency_max")); raw == "" {
		a.concurrencyMax = float64(tasks.PlatformInFlight)
	} else {
		a.concurrencyMax = float64(clampInt(s.GetInt("concurrency_max"), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
	}
	if raw := strings.TrimSpace(s.Get("concurrency_qianwen")); raw == "" {
		a.concurrencyAutoQianwen = a.concurrencyAuto
	} else {
		a.concurrencyAutoQianwen = s.GetInt("concurrency_qianwen") != 1
	}
	if raw := strings.TrimSpace(s.Get("concurrency_max_qianwen")); raw == "" {
		a.concurrencyMaxQianwen = a.concurrencyMax
	} else {
		a.concurrencyMaxQianwen = float64(clampInt(s.GetInt("concurrency_max_qianwen"), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
	}
	if raw := strings.TrimSpace(s.Get("concurrency_qwen_intl")); raw == "" {
		a.concurrencyAutoQwenIntl = a.concurrencyAuto
	} else {
		a.concurrencyAutoQwenIntl = s.GetInt("concurrency_qwen_intl") != 1
	}
	if raw := strings.TrimSpace(s.Get("concurrency_max_qwen_intl")); raw == "" {
		a.concurrencyMaxQwenIntl = a.concurrencyMax
	} else {
		a.concurrencyMaxQwenIntl = float64(clampInt(s.GetInt("concurrency_max_qwen_intl"), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
	}
	if raw := strings.TrimSpace(s.Get("concurrency_gemini")); raw == "" {
		a.concurrencyAutoGemini = a.concurrencyAuto
	} else {
		a.concurrencyAutoGemini = s.GetInt("concurrency_gemini") != 1
	}
	if raw := strings.TrimSpace(s.Get("concurrency_max_gemini")); raw == "" {
		a.concurrencyMaxGemini = a.concurrencyMax
	} else {
		a.concurrencyMaxGemini = float64(clampInt(s.GetInt("concurrency_max_gemini"), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
	}
	if raw := strings.TrimSpace(s.Get("concurrency_jimeng")); raw == "" {
		a.concurrencyAutoJimeng = a.concurrencyAuto
	} else {
		a.concurrencyAutoJimeng = s.GetInt("concurrency_jimeng") != 1
	}
	if raw := strings.TrimSpace(s.Get("concurrency_max_jimeng")); raw == "" {
		a.concurrencyMaxJimeng = a.concurrencyMax
	} else {
		a.concurrencyMaxJimeng = float64(clampInt(s.GetInt("concurrency_max_jimeng"), tasks.PlatformInFlight, tasks.MaxPlatformInFlight))
	}
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
	a.dailyCapQwenIntl, a.dailyCapQwenIntlSet = capOr("daily_cap_qwen_intl", 100)
	a.dailyCapGemini, a.dailyCapGeminiSet = capOr("daily_cap_gemini", 100)
	a.dailyCapJimeng, a.dailyCapJimengSet = capOr("daily_cap_jimeng", 100)
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
	a.autoDeleteHistory = s.GetBool("auto_delete_history")
	a.prompts = s.Get("prompts")
	a.freeformPrompts = s.Get("prompts_freeform")
	a.userPrompts = prompts.ParseUser(s.Get("user_prompts"))
	a.promptPick = s.Get("prompt_pick")
	a.promptChoice = a.promptPick
	a.genPromptChoice = a.resolvedGenPromptChoice()
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
	a.dotMatrixEffect = normalizeDotMatrixEffect(s.Get(settingDotMatrixEffect))

	a.loadDismissedSharedExitHints(s)
	a.refreshBrowsers()
	a.newBrowserPlat = "豆包"
	a.logs = logging.Log.Entries()

	a.loadPlatformWorkspaceFromStore("豆包", s)
	a.loadPlatformWorkspaceFromStore("千问", s)
	a.loadPlatformWorkspaceFromStore("千问国际", s)
	a.loadPlatformWorkspaceFromStore("Gemini", s)
	a.loadPlatformWorkspaceFromStore("即梦", s)
	a.platDoubao = false
	a.platQianwen = false
	a.platQwenIntl = false
	a.platGemini = false
	a.platJimeng = false
	a.setSinglePlatform(chosenPlatform)
	if ws, ok := a.platformSpaces[chosenPlatform]; ok {
		a.restorePlatformWorkspace(ws, chosenPlatform)
	}
	a.loadDedupSettings(s)
	a.autoCheck = update.AutoOn(s.Get(update.KeyAutoCheck))
	a.updateMirror = strings.TrimSpace(s.Get(update.KeyMirror))
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

func (a *App) loadPlatformWorkspaceFromStore(platform string, s *store.Store) {
	if a.platformSpaces == nil {
		a.platformSpaces = map[string]platformWorkspace{}
	}
	a.platDoubao = platform == "豆包"
	a.platQianwen = platform == "千问"
	a.platQwenIntl = platform == "千问国际"
	a.platGemini = platform == "Gemini"
	a.platJimeng = platform == "即梦"
	a.statePage = pageHome
	a.tab = pageHome
	a.restorePlatformWorkspace(defaultPlatformWorkspace(platform), platform)

	a.textPrompts = s.Get(platformSettingKey(platform, "prompts"))
	a.imgPrompts = s.Get(platformSettingKey(platform, "img_prompts"))
	a.prompts = a.textPrompts
	a.freeformPrompts = s.Get(platformSettingKey(platform, "prompts_freeform"))
	a.imgFreeformPrompts = s.Get(platformSettingKey(platform, "img_prompts_freeform"))
	a.promptPick = s.Get(platformSettingKey(platform, "prompt_pick"))
	a.promptChoice = a.promptPick
	a.genPromptChoice = a.resolvedGenPromptChoice()
	a.imgPromptPick = s.Get(platformSettingKey(platform, "img_prompt_pick"))
	a.imgGenPromptChoice = a.resolvedPromptChoiceFor(a.imgPromptPick)
	if a.imgGenPromptChoice == choosePromptLabel {
		a.imgPromptPick = ""
	}
	a.textRepeat = clampRepeat(s.GetInt(platformSettingKey(platform, "free_text_repeat")))
	a.imageRepeat = clampRepeat(s.GetInt(platformSettingKey(platform, "free_image_repeat")))
	a.imagePromptMode = normalizeImagePromptMode(s.Get(platformSettingKey(platform, "image_prompt_mode")))
	if platform == "豆包" {
		for _, p := range splitLines(s.Get("source_images")) {
			data, err := os.ReadFile(p)
			if err != nil || !images.IsValidImageBytes(data) {
				continue
			}
			a.sources = append(a.sources, sourceImage{Path: p, Hash: images.SHA256(data)})
		}
	}
	wash := pageState{srcSel: -1, prompts: s.Get(platformSettingKey(platform, "wash_prompt"))}
	if strings.TrimSpace(wash.prompts) == "" {
		wash.prompts = defaultWashPromptFor(platform)
	}
	if platform == "豆包" {
		for _, p := range splitLines(s.Get("wash_images")) {
			data, err := os.ReadFile(p)
			if err != nil || !images.IsValidImageBytes(data) {
				continue
			}
			wash.sources = append(wash.sources, sourceImage{Path: p, Hash: images.SHA256(data)})
		}
	}
	a.parked = map[int]pageState{pageWash: wash, pageFission: {srcSel: -1, prompts: DefaultFissionPrompt}}
	switch s.Get(platformSettingKey(platform, "batch_mode")) {
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
	if a.batchMode == batchImage {
		if a.imgPrompts == "" && a.textPrompts != "" {
			a.imgPrompts = a.textPrompts
		}
		a.prompts = a.imgPrompts
	}
	a.restoreWorkspaceFor(platform, pageHome)
	a.restoreWorkspaceFor(platform, pageWash)
	a.restoreWorkspaceFor(platform, pageFission)
	a.onPage(pageHome, func() {
		a.stageTab = s.GetInt(platformSettingKey(platform, "gen_stage"))
		a.stagePicked = s.Get(platformSettingKey(platform, "gen_stage_picked")) == "1"
		if s.Get(platformSettingKey(platform, "batch_mode")) == "" && len(a.sources) > 0 {
			a.batchMode = batchImage
		}
		if a.batchMode == batchImage {
			a.imgPrompts = a.prompts
		} else {
			a.textPrompts = a.prompts
		}
	})
	a.onPage(pageWash, func() {
		if strings.TrimSpace(a.prompts) == "提取图中的商品主体，去掉所有文字、水印和背景装饰，纯白背景，保持主体形状、颜色和细节完全不变" ||
			prompts.SameText(a.prompts, prompts.PreviousDefaultWashPrompt) {
			a.prompts = defaultWashPromptFor(platform)
		} else if platform == "豆包" && prompts.SameText(a.prompts, DefaultWashPrompt) {
			a.prompts = defaultWashPromptFor(platform)
		} else if platform == "豆包" && (prompts.SameText(a.prompts, prompts.LegacyDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.PreviousDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.ReviewedDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.PixelReviewDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.FrameReviewDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.WindowReviewDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.FullBleedDoubaoWashPrompt) || prompts.SameText(a.prompts, prompts.RatioDoubaoWashPrompt)) {
			a.prompts = prompts.DoubaoWashPrompt
		}
		a.stageTab = s.GetInt(platformSettingKey(platform, "wash_stage"))
		a.stagePicked = s.Get(platformSettingKey(platform, "wash_stage_picked")) == "1"
	})
	a.onPage(pageFission, func() {
		a.stageTab = s.GetInt(platformSettingKey(platform, "fission_stage"))
		a.stagePicked = s.Get(platformSettingKey(platform, "fission_stage_picked")) == "1"
		a.skipImages = s.Get(platformSettingKey(platform, "fission_skip_images")) == "1"
		if a.skipImages && prompts.SameText(a.prompts, DefaultFissionPrompt) {
			a.prompts = ""
		}
	})
	a.platformSpaces[platform] = a.capturePlatformWorkspace()
}

// saveSettings persists application configuration to SQLite.
func (a *App) saveSettings() {
	if a == nil || a.store == nil {
		return
	}
	s := a.store
	s.Set("browser_backend", a.browserBackend)
	s.SetPlatforms(a.selectedPlatforms())
	s.SetInt("platform_auto", boolInt(a.platformAuto))
	s.Set("send_strategy", a.sendStrategy)
	s.SetInt("concurrency", a.taskConcurrencyFor("豆包"))
	s.SetInt("concurrency_max", a.concurrencyMaxFor("豆包"))
	s.SetInt("concurrency_qianwen", a.taskConcurrencyFor("千问"))
	s.SetInt("concurrency_max_qianwen", a.concurrencyMaxFor("千问"))
	s.SetInt("concurrency_qwen_intl", a.taskConcurrencyFor("千问国际"))
	s.SetInt("concurrency_max_qwen_intl", a.concurrencyMaxFor("千问国际"))
	s.SetInt("concurrency_gemini", a.taskConcurrencyFor("Gemini"))
	s.SetInt("concurrency_max_gemini", a.concurrencyMaxFor("Gemini"))
	s.SetInt("concurrency_jimeng", a.taskConcurrencyFor("即梦"))
	s.SetInt("concurrency_max_jimeng", a.concurrencyMaxFor("即梦"))
	s.SetInt("retry_count", int(a.retryCount))
	s.SetInt("send_wait_seconds", int(a.sendWait))
	s.SetInt("daily_cap", a.shownDailyCap())
	s.SetInt("daily_cap_qianwen", a.shownDailyCapFor("千问"))
	s.SetInt("daily_cap_qwen_intl", a.shownDailyCapFor("千问国际"))
	s.SetInt("daily_cap_gemini", a.shownDailyCapFor("Gemini"))
	s.SetInt("daily_cap_jimeng", a.shownDailyCapFor("即梦"))
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
	s.SetInt("auto_delete_history", boolInt(a.autoDeleteHistory))
	a.saveDedupSettings(s)
	a.persistUpdatePrefs()
	a.noteWorkspacePage()
	if a.workspacePage != "" {
		s.Set("workspace_page", a.workspacePage)
	}
	a.syncPage()
	savePlatformMeta := func(platform string) {
		ws, ok := a.workspaceSnapshotIfPresent(platform)
		if !ok {
			return
		}
		gen := workspacePageState(ws, platform, pageHome)
		wash := workspacePageState(ws, platform, pageWash)
		fission := workspacePageState(ws, platform, pageFission)
		s.SetInt(platformSettingKey(platform, "gen_stage"), gen.stageTab)
		s.SetInt(platformSettingKey(platform, "gen_stage_picked"), boolInt(gen.stagePicked))
		s.SetInt(platformSettingKey(platform, "wash_stage"), wash.stageTab)
		s.SetInt(platformSettingKey(platform, "wash_stage_picked"), boolInt(wash.stagePicked))
		s.SetInt(platformSettingKey(platform, "fission_stage"), fission.stageTab)
		s.SetInt(platformSettingKey(platform, "fission_stage_picked"), boolInt(fission.stagePicked))
		s.SetInt(platformSettingKey(platform, "fission_skip_images"), boolInt(fission.skipImages))
		textPrompts := ws.textPrompts
		imgPrompts := ws.imgPrompts
		if ws.batchMode == batchImage {
			imgPrompts = gen.prompts
		} else {
			textPrompts = gen.prompts
		}
		s.Set(platformSettingKey(platform, "prompts"), textPrompts)
		s.Set(platformSettingKey(platform, "img_prompts"), imgPrompts)
		s.Set(platformSettingKey(platform, "wash_prompt"), strings.TrimSpace(wash.prompts))
		s.Set(platformSettingKey(platform, "prompts_freeform"), ws.freeformPrompts)
		s.Set(platformSettingKey(platform, "img_prompts_freeform"), ws.imgFreeformPrompts)
		s.Set(platformSettingKey(platform, "prompt_pick"), ws.promptPick)
		s.Set(platformSettingKey(platform, "img_prompt_pick"), ws.imgPromptPick)
		s.SetInt(platformSettingKey(platform, "free_text_repeat"), clampRepeat(ws.textRepeat))
		s.SetInt(platformSettingKey(platform, "free_image_repeat"), clampRepeat(ws.imageRepeat))
		s.Set(platformSettingKey(platform, "image_prompt_mode"), normalizeImagePromptMode(ws.imagePromptMode))
		if ws.batchMode == batchImage {
			s.Set(platformSettingKey(platform, "batch_mode"), "image")
		} else {
			s.Set(platformSettingKey(platform, "batch_mode"), "text")
		}
		if platform == "豆包" {
			s.Set("wash_images", joinPaths(sourcePaths(wash.sources)))
			s.Set("source_images", joinPaths(sourcePaths(gen.sources)))
		}
	}
	savePlatformMeta("豆包")
	savePlatformMeta("千问")
	savePlatformMeta("千问国际")
	savePlatformMeta("Gemini")
	savePlatformMeta("即梦")
	s.Set("user_prompts", prompts.FormatUser(a.userPrompts))
	s.Set("prompt_suffix", a.promptSuffix)
	s.SetInt("use_suffix", boolInt(a.useSuffix))
	s.SetInt("captcha_enabled", boolInt(a.captchaEnabled))
	s.Set("captcha_backend", a.captchaBackend)
	s.Set("captcha_custom_url", a.customURL)
	s.Set("captcha_custom_key", a.customKey)
	s.Set("captcha_custom_model", a.customModel)
	s.Set("captcha_ikunocr_key", a.ikunocrKey)
	a.persistProxy()
	if a.darkTheme {
		s.Set("ui_theme", "dark")
	} else {
		s.Set("ui_theme", "light")
	}
	s.Set(settingDotMatrixEffect, a.resolvedDotMatrixEffect())
	a.saveImageEditSettings()
	a.savePromptParts()
	for _, plat := range []string{"豆包", "千问", "千问国际", "Gemini", "即梦"} {
		if _, ok := a.workspaceSnapshotIfPresent(plat); !ok {
			continue
		}
		a.persistWorkspaceFor(plat, pageHome)
		a.persistWorkspaceFor(plat, pageWash)
		a.persistWorkspaceFor(plat, pageFission)
	}
	a.saveDownloadRecords()
}

func (a *App) resolvedDotMatrixEffect() string {
	if a == nil {
		return defaultDotMatrixEffect
	}
	return normalizeDotMatrixEffect(a.dotMatrixEffect)
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
	if browser.CanonicalBackend(a.browserBackend) != browser.KindFingerprint {
		return nil
	}
	have := map[string]bool{}
	for _, b := range rows {
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
	concPlatform := a.selectedPlatform()
	if len(platforms) == 1 && (platforms[0] == "豆包" || platforms[0] == "千问" || platforms[0] == "千问国际" || platforms[0] == "Gemini" || platforms[0] == "即梦") {
		concPlatform = platforms[0]
	}
	opts := tasks.Options{
		Platforms:           platforms,
		Prompts:             promptList,
		PromptSuffix:        a.promptSuffix,
		UseSuffix:           a.useSuffix,
		StructuredPrompt:    a.promptExtras(a.statePage, a.currentPromptParts()),
		Images:              paths,
		Concurrency:         a.taskConcurrencyFor(concPlatform),
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
		OutputSize:          a.outputSizeFor(concPlatform),
		RepeatCount:         a.currentRepeatCount(),
		RemoveWatermark:     a.removeWatermark,
		AutoDeleteHistory:   a.autoDeleteHistory,
		DownloadConcurrency: int(a.downloadConc),
		DownloadRetries:     int(a.downloadRetries),
		DownloadRateKBps:    int(a.downloadRateKB),
		DailyCap:            a.shownDailyCap(),
		DailyCaps:           a.dailyCaps(),
		RiskyWords:          tasks.ParseWords(a.riskyWords),
	}
	switch a.statePage {
	case pageWash:
		opts.Wash = true
		opts.UseSuffix = false
		opts.SoloImages = a.washSoloPaths()
	case pageFission:
		opts.Fission = true
		opts.FissionTextOnly = len(paths) == 0
		opts.UseSuffix = false
		opts.StructuredPrompt = ""
		opts.WaitResult = true
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

func isPromptTextPath(path string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(path))) {
	case ".txt", ".md":
		return true
	default:
		return false
	}
}

func partitionHomeDropPaths(paths []string) (textPaths, otherPaths []string) {
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		info, err := os.Stat(p)
		if err == nil && !info.IsDir() && isPromptTextPath(p) {
			textPaths = append(textPaths, p)
		} else {
			otherPaths = append(otherPaths, p)
		}
	}
	return textPaths, otherPaths
}

func (a *App) ingestSources(paths []string) {
	if a.running || len(paths) == 0 {
		return
	}
	a.syncPage()
	platform := a.selectedPlatform()
	page := a.statePage
	if page == pageHome {
		textPaths, otherPaths := partitionHomeDropPaths(paths)
		if len(textPaths) > 0 && len(otherPaths) == 0 {
			if !a.hasWindow() {
				if err := a.importPromptTextFiles(platform, textPaths); err != nil {
					a.pushToast("error", err.Error())
				}
				return
			}
			go func() {
				imported, count, impErr := readPromptTextFiles(textPaths)
				a.postUI(func() {
					if impErr != nil {
						a.pushToast("error", impErr.Error())
						return
					}
					a.applyImportedPrompts(platform, imported, count)
				})
			}()
			return
		}
		paths = otherPaths
		if len(paths) == 0 {
			return
		}
	}
	needHash := page == pageHome
	if !a.hasWindow() {
		a.addLoadedFor(platform, page, loadSourceFilesFor(paths, needHash))
		return
	}
	go func() {
		loaded := loadSourceFilesFor(paths, needHash)
		a.postUI(func() { a.addLoadedFor(platform, page, loaded) })
	}()
}

func loadSourceFiles(paths []string) []sourceImage {
	return loadSourceFilesFor(paths, true)
}

// loadSourceFilesFor 批量校验并加载源图：洗图/裂变仅读 16 字节文件头验证，自由生图并发流式计算 SHA-256。
func loadSourceFilesFor(paths []string, needHash bool) []sourceImage {
	expanded := expandImagePaths(paths)
	if len(expanded) == 0 {
		return nil
	}
	if !needHash {
		loaded := make([]sourceImage, 0, len(expanded))
		for _, p := range expanded {
			if isValidImageFileHeader(p) {
				loaded = append(loaded, sourceImage{Path: p})
			}
		}
		return loaded
	}
	results := make([]sourceImage, len(expanded))
	valid := make([]bool, len(expanded))
	workers := min(8, max(1, runtime.NumCPU()), len(expanded))
	if workers <= 1 {
		for i, p := range expanded {
			if hash, ok := hashImageFile(p); ok {
				results[i] = sourceImage{Path: p, Hash: hash}
				valid[i] = true
			}
		}
	} else {
		jobs := make(chan int, len(expanded))
		for i := range expanded {
			jobs <- i
		}
		close(jobs)
		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				for i := range jobs {
					p := expanded[i]
					if hash, ok := hashImageFile(p); ok {
						results[i] = sourceImage{Path: p, Hash: hash}
						valid[i] = true
					}
				}
			})
		}
		wg.Wait()
	}
	loaded := make([]sourceImage, 0, len(expanded))
	for i := range expanded {
		if valid[i] {
			loaded = append(loaded, results[i])
		}
	}
	return loaded
}

func isValidImageFileHeader(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var hdr [16]byte
	n, _ := io.ReadFull(f, hdr[:])
	return n > 0 && images.IsValidImageBytes(hdr[:n])
}

func hashImageFile(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	var hdr [16]byte
	n, readErr := io.ReadFull(f, hdr[:])
	if n == 0 || !images.IsValidImageBytes(hdr[:n]) {
		return "", false
	}
	h := sha256.New()
	_, _ = h.Write(hdr[:n])
	if readErr == nil {
		if _, err := io.Copy(h, f); err != nil {
			return "", false
		}
	} else if !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", false
	}
	return hex.EncodeToString(h.Sum(nil)), true
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
	if v := PackagedVersion(); v != "" {
		return v
	}
	if v := getProductVersion(); v != "" {
		return v
	}
	return "未打包"
}

func (a *App) ensureMedia() *MediaHandler {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	if a.media == nil {
		a.media = NewMediaHandler(a)
	}
	m := a.media
	a.mu.Unlock()
	return m
}

func (a *App) mediaURL(p string) string {
	if m := a.ensureMedia(); m != nil {
		return m.FileURL(p)
	}
	return ""
}

func (a *App) setDrawerFrame(jpeg []byte) {
	if a == nil {
		return
	}
	a.mediaMu.Lock()
	a.drawerFrame = jpeg
	a.mediaMu.Unlock()
}

func (a *App) currentDrawerFrame() []byte {
	if a == nil {
		return nil
	}
	a.mediaMu.RLock()
	data := a.drawerFrame
	a.mediaMu.RUnlock()
	return data
}

func (a *App) setEditOrigPath(p string) {
	if a == nil {
		return
	}
	a.mediaMu.Lock()
	a.editOrigPath = p
	a.mediaMu.Unlock()
}

func (a *App) currentEditOrigPath() string {
	if a == nil {
		return ""
	}
	a.mediaMu.RLock()
	p := a.editOrigPath
	a.mediaMu.RUnlock()
	return p
}

func (a *App) setEditPreviewJPEG(jpeg []byte) {
	if a == nil {
		return
	}
	a.mediaMu.Lock()
	a.editOutJPEG = jpeg
	a.mediaMu.Unlock()
}

func (a *App) currentEditPreviewJPEG() []byte {
	if a == nil {
		return nil
	}
	a.mediaMu.RLock()
	data := a.editOutJPEG
	a.mediaMu.RUnlock()
	return data
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
