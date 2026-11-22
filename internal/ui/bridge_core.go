package ui

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/tsx"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/wizard"
)

func init() {
	tsx.RegisterFunctions("tulai/backend", map[string]any{
		"watchEvents":   watchEventsBridge,
		"getShellState": getShellStateBridge,
		"setTab":        setTabBridge,
		"setDarkTheme":  setDarkThemeBridge,
		"setNavFolded":  setNavFoldedBridge,
		"setLogOpen":    setLogOpenBridge,
		"getLogsState":  getLogsStateBridge,
		"setLogFilter":  setLogFilterBridge,
		"toggleLogItem": toggleLogItemBridge,
		"clearLogs":     clearLogsBridge,
		"exportLogs":    exportLogsBridge,
		"copyText":      copyTextBridge,
		"openPath":      openPathBridge,
		"revealPath":    revealPathBridge,
		"pickFiles":     pickFilesBridge,
		"pickFolder":    pickFolderBridge,
		"wizardState":   wizardStateBridge,
		"wizardAction":  wizardActionBridge,
		"openWizard":    openWizardBridge,
		"finishWizard":  finishWizardBridge,
		"updateAction":  updateActionBridge,
		"quitApp":       quitAppBridge,
	})
}

type NavBadgeDTO struct {
	Count int
	Tone  string
}

type UpdateStateDTO struct {
	Visible      bool
	Version      string
	Current      string
	Checking     bool
	Downloading  bool
	Fraction     float64
	ProgressText string
	Notes        string
	DetailsOpen  bool
	NotesOpen    bool
	Err          string
	Installed    string
	Deferred     bool
}

type PoolSummaryDTO struct {
	Ready   int
	Busy    int
	Issue   int
	Total   int
	Summary string
}

type ShellStateDTO struct {
	Tab          int
	Dark         bool
	NavCollapsed bool
	LogOpen      bool
	WizardOn     bool
	Running      bool
	RunPage      int
	ProgText     string
	Progress     float64
	LogTotal     int
	LogWarnings  int
	LogErrors    int
	Pool         PoolSummaryDTO
	Update       UpdateStateDTO
	ToastSeq     int
	ToastText    string
	ToastLevel   string
	Version      string
	OS           string
}

type LogEntryDTO struct {
	Seq      int
	Time     string
	Level    string
	Code     string
	Source   string
	Text     string
	Detail   string
	Elapsed  string
	Expanded bool
}

type LogsStateDTO struct {
	Open       bool
	Pick       int
	Query      string
	Source     string
	Paused     bool
	Sources    []string
	Total      int
	Shown      int
	CountAll   int
	CountDebug int
	CountInfo  int
	CountWarn  int
	CountError int
	LastAt     string
	Entries    []LogEntryDTO
}

type WizardStepDTO struct {
	ID    string
	Title string
	State string // upcoming / current / done
}

type WizardStateDTO struct {
	On           bool
	Step         string
	Choice       string
	First        bool
	Steps        []WizardStepDTO
	Phase        string
	Note         string
	Err          string
	NavFailed    bool
	AccountID    int64
	AccountName  string
	PlanBackend  string
	PlanKind     string
	PlanTitle    string
	PlanDetail   string
	PlanCanNext  bool
	PlanBlocked  string
	PlanNeedsDL  bool
	CamBusy      bool
	CamPercent   float64
	CamStatus    string
	AgyFound     bool
	AgyPath      string
	AgyCopied    bool
	AgyCmd       string
	AgyShell     string
	AgyPageURL   string
	AgyChooserOn bool
	AgyChooser   AgyChooserDTO
	AgyLoginOn   bool
	AgyLogin     AgyLoginDTO
}

func watchEventsBridge(ctx context.Context, onEvent func(topic string)) error {
	a := getApp()
	if a == nil || onEvent == nil {
		<-ctx.Done()
		return nil
	}
	id, ch := a.subscribeEvents()
	defer a.unsubscribeEvents(id)
	for {
		var first string
		var ok bool
		select {
		case <-ctx.Done():
			return nil
		case first, ok = <-ch:
			if !ok {
				return nil
			}
		}

		seen := map[string]bool{first: true}
		order := []string{first}

		for {
			select {
			case next, ok := <-ch:
				if !ok {
					onEvent(strings.Join(order, ","))
					return nil
				}
				if !seen[next] {
					seen[next] = true
					order = append(order, next)
				}
			default:
				goto sendBatch
			}
		}
	sendBatch:
		onEvent(strings.Join(order, ","))
	}
}

func getShellStateBridge() ShellStateDTO {
	a := getApp()
	if a == nil {
		return ShellStateDTO{OS: runtime.GOOS}
	}
	counts := logging.Log.Counts()
	now := time.Now()
	ready, busy, issue, total := 0, 0, 0, 0
	activeAccounts := a.engine.ActiveAccounts()
	for _, b := range a.laneBrowsers() {
		total++
		if activeAccounts[b.ID] {
			busy++
			continue
		}
		if b.Disabled || !b.Usable(now) || (!b.IsLoggedIn && b.ExtAccount == "") {
			issue++
			continue
		}
		ready++
	}
	summary := fmt.Sprintf("就绪 %d / %d", ready, total)
	if busy > 0 {
		summary = fmt.Sprintf("工作中 %d · 就绪 %d", busy, ready)
	} else if total == 0 {
		summary = "未配置账号"
	}
	return ShellStateDTO{
		Tab:          a.tab,
		Dark:         a.darkTheme,
		NavCollapsed: a.navCollapsed,
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
			Fraction:     a.updateFraction,
			ProgressText: a.updateProgressText,
			Notes:        a.updateNotes,
			DetailsOpen:  a.updateDetailsOpen,
			NotesOpen:    a.updateNotesOpen,
			Err:          a.updateErr,
			Installed:    a.updateInstalled,
			Deferred:     a.updateDeferred,
		},
		ToastSeq:   a.toastSeq,
		ToastText:  a.toastText,
		ToastLevel: a.toastLevel,
		Version:    currentVersion(),
		OS:         runtime.GOOS,
	}
}

func setTabBridge(tab int) {
	a := getApp()
	if a == nil {
		return
	}
	a.tab = tab
	if isWorkPage(tab) {
		a.syncPage()
		a.noteWorkspacePage()
	}
	if tab == pageBrowser {
		// Navigation reads the shell and the destination together.
		a.loadBrowserRows()
	}
}

func setDarkThemeBridge(dark bool) {
	a := getApp()
	if a == nil {
		return
	}
	a.darkTheme = dark
	if a.store != nil {
		if dark {
			a.store.Set("ui_theme", "dark")
		} else {
			a.store.Set("ui_theme", "light")
		}
	}
}

func setNavFoldedBridge(folded bool) {
	a := getApp()
	if a == nil {
		return
	}
	a.navCollapsed = folded
	a.navManual = true
}

func setLogOpenBridge(open bool) {
	a := getApp()
	if a == nil {
		return
	}
	a.logOpen = open
}

func getLogsStateBridge() LogsStateDTO {
	a := getApp()
	if a == nil {
		return LogsStateDTO{}
	}
	if !a.logOpen {
		return LogsStateDTO{Open: false, Total: logging.Log.Counts().All}
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
	return LogsStateDTO{
		Open:       true,
		Pick:       a.logPick,
		Query:      a.logQuery,
		Source:     a.logSource,
		Paused:     a.logPaused,
		Sources:    logging.Sources(all),
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

func setLogFilterBridge(pick int, query, source string, paused bool) {
	a := getApp()
	if a == nil {
		return
	}
	a.logPick = pick
	a.logQuery = query
	a.logSource = source
	a.logPaused = paused
	a.emit("logs")
}

func toggleLogItemBridge(seq int) {
	a := getApp()
	if a == nil {
		return
	}
	if a.logExpanded == nil {
		a.logExpanded = map[int]bool{}
	}
	a.logExpanded[seq] = !a.logExpanded[seq]
	a.emit("logs")
}

func clearLogsBridge() {
	logging.Log.Clear()
	a := getApp()
	if a != nil {
		a.logExpanded = nil
		a.emit("logs", "shell")
	}
}

func exportLogsBridge(ctx context.Context) error {
	a := getApp()
	if a == nil {
		return nil
	}
	text := logging.ExportText(logging.Filter(logging.Log.Entries(), logging.View{Query: a.logQuery, Source: a.logSource}))
	name := "tulai-log-" + time.Now().Format("20060102-150405") + ".log"
	saved, path, err := a.saveFileAsync(ctx, name, "text/plain", []byte(text))
	if err != nil {
		return err
	}
	if saved && path != "" {
		a.pushToast("success", "日志已导出")
	}
	return nil
}

func copyTextBridge(text string) {
	a := getApp()
	if a != nil {
		a.copyText(text)
	}
}

func openPathBridge(ctx context.Context, path string) error {
	a := getApp()
	if a == nil {
		return nil
	}
	return a.openPath(path)
}

func revealPathBridge(ctx context.Context, path string) error {
	a := getApp()
	if a == nil {
		return nil
	}
	return a.revealPath(path)
}

func pickFilesBridge(ctx context.Context, title string, multiple bool, extensions []string) ([]string, error) {
	a := getApp()
	if a == nil {
		return nil, nil
	}
	return a.pickFilesAsync(ctx, title, multiple, extensions, "选择文件")
}

func pickFolderBridge(ctx context.Context, title string, multiple bool) ([]string, error) {
	a := getApp()
	if a == nil {
		return nil, nil
	}
	return a.pickDirsAsync(ctx, title, multiple)
}

func wizardStepID(s wizard.Step) string {
	switch s {
	case wizard.StepPlatform:
		return "platform"
	case wizard.StepInstall:
		return "install"
	case wizard.StepBrowser:
		return "browser"
	case wizard.StepAccount:
		return "account"
	case wizard.StepDone:
		return "done"
	default:
		return fmt.Sprintf("%d", int(s))
	}
}

func wizardStepTitle(s wizard.Step) string {
	switch s {
	case wizard.StepPlatform:
		return "选择平台"
	case wizard.StepInstall:
		return "安装 agy"
	case wizard.StepBrowser:
		return "账号环境"
	case wizard.StepAccount:
		return "登录账号"
	case wizard.StepDone:
		return "完成"
	default:
		return wizardStepID(s)
	}
}

func wizardStateBridge() WizardStateDTO {
	a := getApp()
	if a == nil {
		return WizardStateDTO{}
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
	dto := WizardStateDTO{
		On:          a.wizardOn,
		Step:        wizardStepID(a.wizardStep),
		Choice:      a.wizardChoice,
		First:       a.wizardFirst,
		Steps:       steps,
		Phase:       a.wizardPhase,
		Note:        a.wizardNote,
		Err:         a.wizardErr,
		NavFailed:   a.wizardNavFailed,
		AccountID:   a.wizardAccount.ID,
		AccountName: a.wizardAccount.Name,
		PlanBackend: a.wizardPlan.Backend,
		PlanKind:    a.wizardPlan.Kind,
		PlanTitle:   a.wizardPlan.Title,
		PlanDetail:  a.wizardPlan.Detail,
		PlanCanNext: a.wizardPlan.CanNext,
		PlanBlocked: a.wizardPlan.Blocked,
		PlanNeedsDL: a.wizardPlan.NeedsDownload,
		CamBusy:     a.camoufoxBusy,
		CamPercent:  a.camoufoxPercent,
		CamStatus:   a.camoufoxStatus,
		AgyFound:    a.wizardAgy.found,
		AgyPath:     a.wizardAgy.path,
		AgyCopied:   a.wizardAgy.copied,
		AgyCmd:      agy.InstallCommand(runtime.GOOS),
		AgyShell:    agy.InstallShell(runtime.GOOS),
		AgyPageURL:  agy.InstallPage,
	}
	if a.agyChooser != nil {
		dto.AgyChooserOn = true
		dto.AgyChooser = AgyChooserDTO{
			Hint:      a.agyChooser.hint,
			Home:      a.agyChooser.home,
			Email:     a.agyChooser.email,
			CanImport: a.agyChooser.canImport,
			Keychain:  a.agyChooser.keychain,
			Busy:      a.agyChooser.busy,
			Retry:     a.agyChooser.retry,
			Err:       a.agyChooser.err,
			Status:    a.agyChooser.status,
		}
	}
	if a.agyLogin != nil {
		dto.AgyLoginOn = true
		dto.AgyLogin = AgyLoginDTO{
			ID:      a.agyLogin.id,
			Name:    a.agyLogin.name,
			URL:     a.agyLogin.url,
			Busy:    a.agyLogin.busy,
			Retry:   a.agyLogin.retry,
			Err:     a.agyLogin.err,
			Copied:  a.agyLogin.copied,
			Opening: a.agyLogin.opening,
		}
	}
	return dto
}

func openWizardBridge() {
	a := getApp()
	if a == nil {
		return
	}
	a.openWizard()
	a.emit("wizard", "shell")
}

func finishWizardBridge() {
	a := getApp()
	if a == nil {
		return
	}
	a.wizardFinish()
}

func wizardActionBridge(ctx context.Context, action, value string) error {
	a := getApp()
	if a == nil {
		return nil
	}
	switch action {
	case "choose":
		a.wizardChoice = value
		a.wizardErr = ""
	case "nextPlatform":
		a.wizardNextFromPlatform()
	case "selectEngine":
		a.selectAccountEngine(value)
	case "installBrowser":
		a.installCamoufox()
	case "nextBrowser":
		a.wizardEnterWebAccount(false)
	case "back":
		switch a.wizardStep {
		case wizard.StepBrowser, wizard.StepInstall:
			a.goWizard(wizard.StepPlatform)
		case wizard.StepAccount:
			a.stopWizardLoop()
			if a.wizardChoice == agy.Platform {
				a.abandonAgyAccount()
				if a.wizardAgy.found {
					a.goWizard(wizard.StepPlatform)
				} else {
					a.goWizard(wizard.StepInstall)
				}
			} else {
				a.goWizard(wizard.StepPlatform)
			}
		case wizard.StepDone:
			a.goWizard(wizard.StepPlatform)
		}
	case "recheck":
		a.wizardRecheck()
	case "addMore":
		if a.wizardChoice == agy.Platform {
			a.wizardEnterAgyAccount(true)
		} else {
			a.wizardEnterWebAccount(true)
		}
	case "checkAgy":
		a.checkAgyInstall()
	case "copyAgyCmd":
		a.copyText(agy.InstallCommand(runtime.GOOS))
		a.wizardAgy.copied = true
	case "pickAgyBin":
		paths, err := a.pickFilesAsync(ctx, "选择 agy 可执行文件", false, nil, "可执行文件")
		if err == nil && len(paths) > 0 {
			_ = a.onScope(ctx, func() {
				a.agyBin = paths[0]
				a.checkAgyInstall()
			})
		}
	case "enterAgyAccount":
		a.wizardEnterAgyAccount(false)
	case "skip":
		a.stopWizardLoop()
		a.abandonAgyAccount()
		a.wizardOn = false
		if a.store != nil {
			a.store.Set(wizard.KeyComplete, wizard.CompleteYes)
		}
		a.emit("wizard", "shell")
		return nil
	case "finish":
		a.wizardFinish()
		return nil
	}
	a.emit("wizard", "shell", "browsers")
	return nil
}

func updateActionBridge(action string) {
	a := getApp()
	if a == nil {
		return
	}
	switch action {
	case "check":
		a.checkForUpdates()
	case "install":
		a.applyUpdate()
	case "dismiss":
		a.dismissUpdate()
	case "skip":
		a.skipUpdate()
	case "toggleDetails":
		a.updateDetailsOpen = !a.updateDetailsOpen
	case "toggleNotes":
		a.updateNotesOpen = !a.updateNotesOpen
	}
	a.emit("update", "shell")
}

func quitAppBridge(cx *core.Scope) {
	if cx != nil {
		cx.Quit()
	}
}
