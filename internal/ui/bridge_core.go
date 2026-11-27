package ui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/logging"
)

// CoreService exposes shell, logging, desktop OS helpers, onboarding wizard,
// updater, and lifecycle IPCs to the MyGo frontend.
type CoreService struct {
	app *App
}

// NavBadgeDTO describes a compact numeric badge.
type NavBadgeDTO struct {
	Count int    `json:"count"`
	Tone  string `json:"tone"`
}

// UpdateStateDTO describes the in-app release updater state.
type UpdateStateDTO struct {
	Visible      bool    `json:"visible"`
	Version      string  `json:"version"`
	Current      string  `json:"current"`
	Checking     bool    `json:"checking"`
	Downloading  bool    `json:"downloading"`
	Phase        string  `json:"phase"`
	Fraction     float64 `json:"fraction"`
	DoneBytes    int64   `json:"doneBytes"`
	TotalBytes   int64   `json:"totalBytes"`
	SpeedBps     int64   `json:"speedBps"`
	EtaSeconds   int     `json:"etaSeconds"`
	Source       string  `json:"source"`
	AssetName    string  `json:"assetName"`
	ProgressText string  `json:"progressText"`
	Notes        string  `json:"notes"`
	DetailsOpen  bool    `json:"detailsOpen"`
	NotesOpen    bool    `json:"notesOpen"`
	Err          string  `json:"err"`
	Installed    string  `json:"installed"`
	Deferred     bool    `json:"deferred"`
}

// PoolSummaryDTO summarizes account readiness across platforms.
type PoolSummaryDTO struct {
	Ready   int    `json:"ready"`
	Busy    int    `json:"busy"`
	Issue   int    `json:"issue"`
	Total   int    `json:"total"`
	Summary string `json:"summary"`
}

// PlatformNavDTO summarizes account status for one provider in the sidebar.
type PlatformNavDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Ready int    `json:"ready"`
	Busy  int    `json:"busy"`
	Total int    `json:"total"`
}

// ShellStateDTO is the top-level shell snapshot pushed to the frontend.
type ShellStateDTO struct {
	Tab          int              `json:"tab"`
	Platform     string           `json:"platform"`
	Platforms    []PlatformNavDTO `json:"platforms"`
	Dark         bool             `json:"dark"`
	NavCollapsed bool             `json:"navCollapsed"`
	NavManual    bool             `json:"navManual"`
	LogOpen      bool             `json:"logOpen"`
	WizardOn     bool             `json:"wizardOn"`
	Running      bool             `json:"running"`
	RunPage      int              `json:"runPage"`
	ProgText     string           `json:"progText"`
	Progress     float64          `json:"progress"`
	LogTotal     int              `json:"logTotal"`
	LogWarnings  int              `json:"logWarnings"`
	LogErrors    int              `json:"logErrors"`
	Pool         PoolSummaryDTO   `json:"pool"`
	Update       UpdateStateDTO   `json:"update"`
	ToastSeq     int              `json:"toastSeq"`
	ToastText       string           `json:"toastText"`
	ToastLevel      string           `json:"toastLevel"`
	Version         string           `json:"version"`
	OS              string           `json:"os"`
	MediaOrigin     string           `json:"mediaOrigin"`
	DotMatrixEffect string           `json:"dotMatrixEffect"`
}

// LogEntryDTO represents a single entry in the log drawer.
type LogEntryDTO struct {
	Seq      int    `json:"seq"`
	Time     string `json:"time"`
	Level    string `json:"level"`
	Code     string `json:"code"`
	Source   string `json:"source"`
	Text     string `json:"text"`
	Detail   string `json:"detail"`
	Elapsed  string `json:"elapsed"`
	Expanded bool   `json:"expanded"`
}

// LogsStateDTO represents the filtered state of the log drawer.
type LogsStateDTO struct {
	Open       bool          `json:"open"`
	Pick       int           `json:"pick"`
	Query      string        `json:"query"`
	Source     string        `json:"source"`
	Paused     bool          `json:"paused"`
	Sources    []string      `json:"sources"`
	Total      int           `json:"total"`
	Shown      int           `json:"shown"`
	CountAll   int           `json:"countAll"`
	CountDebug int           `json:"countDebug"`
	CountInfo  int           `json:"countInfo"`
	CountWarn  int           `json:"countWarn"`
	CountError int           `json:"countError"`
	LastAt     string        `json:"lastAt"`
	Entries    []LogEntryDTO `json:"entries"`
}

// WizardStepDTO represents one step in the first-run onboarding wizard.
type WizardStepDTO struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	State string `json:"state"` // upcoming / current / done
}

// WizardStateDTO represents the first-run onboarding wizard snapshot.
type WizardStateDTO struct {
	On             bool            `json:"on"`
	Step           string          `json:"step"`
	Choice         string          `json:"choice"`
	First          bool            `json:"first"`
	Steps          []WizardStepDTO `json:"steps"`
	Phase          string          `json:"phase"`
	Note           string          `json:"note"`
	Err            string          `json:"err"`
	NavFailed      bool            `json:"navFailed"`
	AccountID      int64           `json:"accountId"`
	AccountName    string          `json:"accountName"`
	PlanBackend    string          `json:"planBackend"`
	PlanKind       string          `json:"planKind"`
	PlanTitle      string          `json:"planTitle"`
	PlanDetail     string          `json:"planDetail"`
	PlanCanNext    bool            `json:"planCanNext"`
	PlanBlocked    string          `json:"planBlocked"`
	PlanNeedsDL    bool            `json:"planNeedsDl"`
	RuntimeBusy    bool            `json:"runtimeBusy"`
	RuntimePercent float64         `json:"runtimePercent"`
	RuntimeStatus  string          `json:"runtimeStatus"`
}

func defaultPlatformNavs() []PlatformNavDTO {
	return []PlatformNavDTO{
		{ID: "doubao", Name: "豆包"},
		{ID: "qianwen", Name: "千问"},
		{ID: "qwen_intl", Name: "千问国际"},
		{ID: "gemini", Name: "Gemini"},
	}
}

func (a *App) buildShellState() ShellStateDTO {
	if a == nil {
		return ShellStateDTO{
			Platform:        "豆包",
			Platforms:       defaultPlatformNavs(),
			Version:         currentVersion(),
			OS:              runtime.GOOS,
			MediaOrigin:     MediaOrigin(),
			DotMatrixEffect: defaultDotMatrixEffect,
		}
	}
	counts := logging.Log.Counts()
	now := time.Now()
	ready, busy, issue, total := 0, 0, 0, 0
	platforms := defaultPlatformNavs()
	platIndex := make(map[string]int, len(platforms))
	for i, p := range platforms {
		platIndex[p.Name] = i
	}
	var activeAccounts map[int64]bool
	if a.engine != nil {
		activeAccounts = a.engine.ActiveAccounts()
	}
	for _, b := range a.laneBrowsers() {
		total++
		idx, hasPlat := platIndex[b.Platform]
		if hasPlat {
			platforms[idx].Total++
		}
		if activeAccounts[b.ID] {
			busy++
			if hasPlat {
				platforms[idx].Busy++
			}
			continue
		}
		if b.Disabled || !b.Usable(now) || !b.IsLoggedIn {
			issue++
			continue
		}
		ready++
		if hasPlat {
			platforms[idx].Ready++
		}
	}
	summary := fmt.Sprintf("就绪 %d / %d", ready, total)
	if busy > 0 {
		summary = fmt.Sprintf("工作中 %d · 就绪 %d", busy, ready)
	} else if total == 0 {
		summary = "未配置账号"
	}
	return ShellStateDTO{
		Tab:          a.tab,
		Platform:     a.selectedPlatform(),
		Platforms:    platforms,
		Dark:         a.darkTheme,
		NavCollapsed: a.navCollapsed,
		NavManual:    a.navManual,
		LogOpen:      a.logOpen,
		WizardOn:     a.wizardOn,
		Running:      a.running,
		RunPage:      a.runPage,
		ProgText:     a.progText,
		Progress:     a.progress,
		LogTotal:     counts.All,
		LogWarnings:  counts.Warn,
		LogErrors:    counts.Error,
		Pool: PoolSummaryDTO{
			Ready:   ready,
			Busy:    busy,
			Issue:   issue,
			Total:   total,
			Summary: summary,
		},
		Update: UpdateStateDTO{
			Visible:      a.updateNotice != "" && !(a.updateHidden && a.updateDownloading),
			Version:      a.updateNotice,
			Current:      currentVersion(),
			Checking:     a.updateBusy,
			Downloading:  a.updateDownloading,
			Phase:        a.updatePhase,
			Fraction:     a.updateFraction,
			DoneBytes:    a.updateDone,
			TotalBytes:   a.updateTotal,
			SpeedBps:     a.updateSpeed,
			EtaSeconds:   a.updateEta,
			Source:       a.updateSource,
			AssetName:    a.updateAssetName,
			ProgressText: a.updateProgressText,
			Notes:        a.updateNotes,
			DetailsOpen:  a.updateDetailsOpen,
			NotesOpen:    a.updateNotesOpen,
			Err:          a.updateErr,
			Installed:    a.updateInstalled,
			Deferred:     a.updateDeferred,
		},
		ToastSeq:    a.toastSeq,
		ToastText:   a.toastText,
		ToastLevel:  a.toastLevel,
		Version:         currentVersion(),
		OS:              runtime.GOOS,
		MediaOrigin:     a.ensureMedia().Origin(),
		DotMatrixEffect: a.resolvedDotMatrixEffect(),
	}
}

// GetShellState returns the global shell, navigation, toast, pool, and update state.
func (s *CoreService) GetShellState(ctx context.Context) ShellStateDTO {
	if s.app == nil {
		return (*App)(nil).buildShellState()
	}
	out, err := callValue(s.app, ctx, func() ShellStateDTO {
		return s.app.buildShellState()
	})
	if err != nil {
		return (*App)(nil).buildShellState()
	}
	return out
}

// SetTab switches the active workspace or module tab.
func (s *CoreService) SetTab(ctx context.Context, tab int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.tab = tab
		if isWorkPage(tab) {
			s.app.syncPage()
			s.app.noteWorkspacePage()
		}
		if tab == pageBrowser {
			s.app.loadBrowserRows()
		}
		s.app.emit("shell")
	})
}

// SetDarkTheme toggles dark mode, persists it to SQLite, and synchronizes native window chrome.
func (s *CoreService) SetDarkTheme(ctx context.Context, dark bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.darkTheme = dark
		if s.app.store != nil {
			if dark {
				s.app.store.Set("ui_theme", "dark")
			} else {
				s.app.store.Set("ui_theme", "light")
			}
		}
		s.app.emit("shell")
	})
	if h := s.app.desktopHost(); h != nil {
		h.SetDarkTheme(dark)
	}
}

// SetNavFolded sets whether the sidebar navigation rail is folded.
func (s *CoreService) SetNavFolded(ctx context.Context, folded bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.navCollapsed = folded
		s.app.navManual = true
		s.app.emit("shell")
	})
}

// SetLogOpen opens or closes the floating log console drawer.
func (s *CoreService) SetLogOpen(ctx context.Context, open bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.logOpen = open
		s.app.emit("logs", "shell")
	})
}

func (a *App) buildLogsState() LogsStateDTO {
	if a == nil {
		return LogsStateDTO{Sources: []string{}, Entries: []LogEntryDTO{}}
	}
	if !a.logOpen {
		return LogsStateDTO{
			Open:    false,
			Total:   logging.Log.Counts().All,
			Sources: []string{},
			Entries: []LogEntryDTO{},
		}
	}
	all := logging.Log.Entries()
	counts := logging.CountLevels(all)
	level := logging.Level(-1)
	switch a.logPick {
	case 1:
		level = logging.Debug
	case 2:
		level = logging.Info
	case 3:
		level = logging.Warning
	case 4:
		level = logging.Error
	}
	shown := logging.Filter(all, logging.View{
		Level:  level,
		Query:  a.logQuery,
		Source: a.logSource,
	})
	totalShown := len(shown)
	if len(shown) > 300 {
		shown = shown[len(shown)-300:]
	}
	rows := make([]LogEntryDTO, 0, len(shown))
	for _, e := range shown {
		elapsed := ""
		if e.Elapsed > 0 {
			elapsed = e.Elapsed.Round(time.Millisecond).String()
		}
		rows = append(rows, LogEntryDTO{
			Seq:      e.Seq,
			Time:     e.Time.Local().Format("15:04:05.000"),
			Level:    logging.LevelCode(e.Level),
			Code:     fmt.Sprintf("%d", e.Level),
			Source:   trimSource(e.Source),
			Text:     e.Text,
			Detail:   e.Detail,
			Elapsed:  elapsed,
			Expanded: a.logExpanded != nil && a.logExpanded[e.Seq],
		})
	}
	lastAt := ""
	if len(all) > 0 {
		lastAt = all[len(all)-1].Time.Local().Format("15:04:05.000")
	}
	sources := logging.Sources(all)
	if sources == nil {
		sources = []string{}
	}
	return LogsStateDTO{
		Open:       true,
		Pick:       a.logPick,
		Query:      a.logQuery,
		Source:     a.logSource,
		Paused:     a.logPaused,
		Sources:    sources,
		Total:      len(all),
		Shown:      totalShown,
		CountAll:   counts.All,
		CountDebug: counts.Debug,
		CountInfo:  counts.Info,
		CountWarn:  counts.Warn,
		CountError: counts.Error,
		LastAt:     lastAt,
		Entries:    rows,
	}
}

// GetLogsState returns the current log drawer entries and filter counts.
func (s *CoreService) GetLogsState(ctx context.Context) LogsStateDTO {
	if s.app == nil {
		return (*App)(nil).buildLogsState()
	}
	out, err := callValue(s.app, ctx, func() LogsStateDTO {
		return s.app.buildLogsState()
	})
	if err != nil {
		return (*App)(nil).buildLogsState()
	}
	return out
}

// SetLogFilter updates log drawer level, search query, source filter, and paused state.
func (s *CoreService) SetLogFilter(ctx context.Context, pick int, query, source string, paused bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.logPick = pick
		s.app.logQuery = query
		s.app.logSource = source
		s.app.logPaused = paused
		s.app.emit("logs")
	})
}

// ToggleLogItem expands or collapses a log entry detail block.
func (s *CoreService) ToggleLogItem(ctx context.Context, seq int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if s.app.logExpanded == nil {
			s.app.logExpanded = map[int]bool{}
		}
		s.app.logExpanded[seq] = !s.app.logExpanded[seq]
		s.app.emit("logs")
	})
}

// ClearLogs empties the in-memory log buffer.
func (s *CoreService) ClearLogs(ctx context.Context) {
	logging.Log.Clear()
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.logExpanded = nil
		s.app.emit("logs", "shell")
	})
}

// ExportLogs prompts the user with a native save dialog and writes filtered logs to disk.
func (s *CoreService) ExportLogs(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	view, err := callValue(s.app, ctx, func() logging.View {
		return logging.View{Query: s.app.logQuery, Source: s.app.logSource}
	})
	if err != nil {
		return err
	}
	text := logging.ExportText(logging.Filter(logging.Log.Entries(), view))
	name := "tulai-log-" + time.Now().Format("20060102-150405") + ".log"
	saved, path, err := s.app.saveFileAsync(ctx, name, "text/plain", []byte(text))
	if err != nil {
		return err
	}
	if saved && path != "" {
		_ = s.app.call(ctx, func() {
			s.app.pushToast("success", "日志已导出")
		})
	}
	return nil
}

// CopyText writes plain text to the system clipboard.
func (s *CoreService) CopyText(_ context.Context, text string) {
	if s.app != nil {
		s.app.copyText(text)
	}
}

// CopyImage writes a local image file to the system clipboard as PNG.
func (s *CoreService) CopyImage(_ context.Context, path string) error {
	if s.app == nil {
		return nil
	}
	return s.app.copyImage(path)
}

// OpenPath opens a local file or folder with the OS default application.
func (s *CoreService) OpenPath(_ context.Context, path string) error {
	if s.app == nil {
		return nil
	}
	return s.app.openPath(path)
}

// OpenExternal opens an http(s) URL in the user's default browser.
func (s *CoreService) OpenExternal(_ context.Context, rawURL string) error {
	if s.app == nil {
		return nil
	}
	return s.app.openExternal(rawURL)
}

// RevealPath reveals a local file in Finder / Explorer / file manager.
func (s *CoreService) RevealPath(_ context.Context, path string) error {
	if s.app == nil {
		return nil
	}
	return s.app.revealPath(path)
}

// PickFiles opens a native file picker dialog.
func (s *CoreService) PickFiles(ctx context.Context, title string, multiple bool, extensions []string) ([]string, error) {
	if s.app == nil {
		return []string{}, nil
	}
	paths, err := s.app.pickFilesAsync(ctx, title, multiple, extensions, "选择文件")
	if err != nil {
		return nil, err
	}
	if paths == nil {
		return []string{}, nil
	}
	return paths, nil
}

// PickFolder opens a native directory picker dialog.
func (s *CoreService) PickFolder(ctx context.Context, title string, multiple bool) ([]string, error) {
	if s.app == nil {
		return []string{}, nil
	}
	paths, err := s.app.pickDirsAsync(ctx, title, multiple)
	if err != nil {
		return nil, err
	}
	if paths == nil {
		return []string{}, nil
	}
	return paths, nil
}

func wizardStepID(s wizardStep) string {
	switch s {
	case wizardStepPlatform:
		return "platform"
	case wizardStepBrowser:
		return "browser"
	case wizardStepAccount:
		return "account"
	case wizardStepDone:
		return "done"
	default:
		return fmt.Sprintf("%d", int(s))
	}
}

func wizardStepTitle(s wizardStep) string {
	switch s {
	case wizardStepPlatform:
		return "选择平台"
	case wizardStepBrowser:
		return "账号环境"
	case wizardStepAccount:
		return "登录账号"
	case wizardStepDone:
		return "完成"
	default:
		return wizardStepID(s)
	}
}

func (a *App) buildWizardState() WizardStateDTO {
	if a == nil {
		return WizardStateDTO{Steps: []WizardStepDTO{}}
	}
	flow := a.wizardFlow()
	curIdx := a.wizardStepIndex(a.wizardStep)
	steps := make([]WizardStepDTO, 0, len(flow))
	for i, s := range flow {
		st := "upcoming"
		if i < curIdx {
			st = "done"
		} else if i == curIdx {
			st = "current"
		}
		steps = append(steps, WizardStepDTO{
			ID:    wizardStepID(s),
			Title: wizardStepTitle(s),
			State: st,
		})
	}
	return WizardStateDTO{
		On:             a.wizardOn,
		Step:           wizardStepID(a.wizardStep),
		Choice:         a.wizardChoice,
		First:          a.wizardFirst,
		Steps:          steps,
		Phase:          a.wizardPhase,
		Note:           a.wizardNote,
		Err:            a.wizardErr,
		NavFailed:      a.wizardNavFailed,
		AccountID:      a.wizardAccount.ID,
		AccountName:    a.wizardAccount.Name,
		PlanBackend:    a.wizardPlan.Backend,
		PlanKind:       a.wizardPlan.Kind,
		PlanTitle:      a.wizardPlan.Title,
		PlanDetail:     a.wizardPlan.Detail,
		PlanCanNext:    a.wizardPlan.CanNext,
		PlanBlocked:    a.wizardPlan.Blocked,
		PlanNeedsDL:    a.wizardPlan.NeedsDownload,
		RuntimeBusy:    a.runtimeBusy,
		RuntimePercent: a.runtimePercent,
		RuntimeStatus:  a.runtimeStatus,
	}
}

// WizardState returns the onboarding wizard snapshot.
func (s *CoreService) WizardState(ctx context.Context) WizardStateDTO {
	if s.app == nil {
		return (*App)(nil).buildWizardState()
	}
	out, err := callValue(s.app, ctx, func() WizardStateDTO {
		return s.app.buildWizardState()
	})
	if err != nil {
		return (*App)(nil).buildWizardState()
	}
	return out
}

// OpenWizard re-opens the onboarding wizard.
func (s *CoreService) OpenWizard(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.openWizard()
		s.app.emit("wizard", "shell")
	})
}

// FinishWizard completes and dismisses the onboarding wizard.
func (s *CoreService) FinishWizard(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.wizardFinish()
	})
}

// WizardAction executes a step action inside the onboarding wizard.
func (s *CoreService) WizardAction(ctx context.Context, action, value string) error {
	if s.app == nil {
		return nil
	}
	return s.app.call(ctx, func() {
		s.app.applyWizardAction(action, value)
	})
}

func (a *App) applyWizardAction(action, value string) {
	switch action {
	case "choose":
		a.wizardChoice = value
		a.wizardErr = ""
	case "nextPlatform":
		a.wizardNextFromPlatform()
	case "installBrowser":
		a.installBrowserRuntime()
	case "nextBrowser":
		a.wizardEnterWebAccount(false)
	case "back":
		switch a.wizardStep {
		case wizardStepBrowser, wizardStepAccount:
			a.stopWizardLoop()
			a.goWizard(wizardStepPlatform)
		case wizardStepDone:
			a.goWizard(wizardStepPlatform)
		}
	case "recheck":
		a.wizardRecheck()
	case "addMore":
		a.wizardEnterWebAccount(true)
	case "skip":
		a.stopWizardLoop()
		a.wizardOn = false
		if a.store != nil {
			a.store.Set(WizardKeyComplete, WizardCompleteYes)
		}
		a.emit("wizard", "shell")
		return
	case "finish":
		a.wizardFinish()
		return
	}
	a.emit("wizard", "shell", "browsers")
}

// UpdateAction handles release update actions (check, install, cancel, dismiss, skip, toggleDetails, toggleNotes).
func (s *CoreService) UpdateAction(ctx context.Context, action string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		switch strings.TrimSpace(action) {
		case "check":
			s.app.checkForUpdates()
		case "install":
			s.app.applyUpdate()
		case "cancel":
			s.app.cancelUpdate()
		case "dismiss":
			s.app.dismissUpdate()
		case "skip":
			s.app.skipUpdate()
		case "toggleDetails":
			s.app.updateDetailsOpen = !s.app.updateDetailsOpen
		case "toggleNotes":
			s.app.updateNotesOpen = !s.app.updateNotesOpen
		}
		s.app.emit("update", "shell", "settings")
	})
}

// QuitApp requests a clean application exit.
func (s *CoreService) QuitApp(_ context.Context) {
	if s.app == nil {
		return
	}
	if h := s.app.desktopHost(); h != nil {
		h.Quit()
	}
}

// ---- Test bridge wrappers (preserve internal/ui/*_test.go compatibility) ----

func getShellStateBridge() ShellStateDTO {
	return (&CoreService{app: getApp()}).GetShellState(context.Background())
}

func setTabBridge(tab int) {
	(&CoreService{app: getApp()}).SetTab(context.Background(), tab)
}

func setDarkThemeBridge(dark bool) {
	(&CoreService{app: getApp()}).SetDarkTheme(context.Background(), dark)
}

func setNavFoldedBridge(folded bool) {
	(&CoreService{app: getApp()}).SetNavFolded(context.Background(), folded)
}

func setLogOpenBridge(open bool) {
	(&CoreService{app: getApp()}).SetLogOpen(context.Background(), open)
}

func getLogsStateBridge() LogsStateDTO {
	return (&CoreService{app: getApp()}).GetLogsState(context.Background())
}

func wizardStateBridge() WizardStateDTO {
	return (&CoreService{app: getApp()}).WizardState(context.Background())
}

func finishWizardBridge() {
	(&CoreService{app: getApp()}).FinishWizard(context.Background())
}

func wizardActionBridge(ctx context.Context, action, value string) error {
	return (&CoreService{app: getApp()}).WizardAction(ctx, action, value)
}
