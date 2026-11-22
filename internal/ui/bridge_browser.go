package ui

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/tsx"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
)

func init() {
	tsx.RegisterFunctions("tulai/backend", map[string]any{
		"getBrowserState":           getBrowserStateBridge,
		"selectAccountEngine":       selectAccountEngineBridge,
		"installCamoufox":           installCamoufoxBridge,
		"refreshBrowsers":           refreshBrowsersBridge,
		"createBrowser":             createBrowserBridge,
		"browserRowAction":          browserRowActionBridge,
		"setPlatformAccountsDisabled": setPlatformAccountsDisabledBridge,
		"toggleBrowserCheck":        toggleBrowserCheckBridge,
		"toggleAllBrowserChecks":    toggleAllBrowserChecksBridge,
		"runBrowserBatch":           runBrowserBatchBridge,
		"commitBrowserRename":       commitBrowserRenameBridge,
		"saveAccountProxy":          saveAccountProxyBridge,
		"regenerateExtensionToken":  regenerateExtensionTokenBridge,
		"agyAction":                 agyActionBridge,
		"openBrowserDrawer":         openBrowserDrawerBridge,
		"closeBrowserDrawer":        closeBrowserDrawerBridge,
		"resumeBrowserDrawer":       resumeBrowserDrawerBridge,
		"getBrowserDrawerState":     getBrowserDrawerStateBridge,
		"clearBrowserDrawerCopy":    clearBrowserDrawerCopyBridge,
	})
}

type BrowserRowDTO struct {
	ID         int64
	Platform   string
	Name       string
	Email      string
	Engine     string
	Extension  bool
	IsRunning  bool
	IsLoggedIn bool
	Active     bool
	Disabled   bool
	Checked    bool
	Pending    bool
	PendingOp  string
	Health     string
	HealthTone string
	Proxy      string
	LastError  string
	TodayCount int
	UsageLabel string
	UsagePct   int
}

type AgyChooserDTO struct {
	Hint      string
	Home      string
	Email     string
	CanImport bool
	Keychain  bool
	Busy      bool
	Retry     bool
	Err       string
	Status    string
}

type AgyLoginDTO struct {
	ID      int64
	Name    string
	URL     string
	Busy    bool
	Retry   bool
	Err     string
	Copied  bool
	Opening bool
}

type BridgeStatusDTO struct {
	Listening bool
	Addr      string
	Err       string
	PairLabel string
	Token     string
	TokenNote string
	Port      string
	Doubao    bool
	Qianwen   bool
}

type BrowserStateDTO struct {
	Backend       string
	BackendLabel  string
	Engines       []string
	Ready         bool
	Busy          bool
	BadgeLabel    string
	BadgeTone     string
	Status        string
	Detail        string
	Percent       float64
	Done          int64
	Total         int64
	Speed         int64
	Supported     bool
	Running       bool
	Rows          []BrowserRowDTO
	SelectedID    int64
	CheckedCount  int
	BatchActive   bool
	BatchText     string
	MissingHint   string
	DlBrowserBusy bool
	DlBrowserOpen bool
	DlBrowserURL  string
	Bridge        BridgeStatusDTO
	AgyChooserOn  bool
	AgyChooser    AgyChooserDTO
	AgyLoginOn    bool
	AgyLogin      AgyLoginDTO
}

type BrowserDrawerDTO struct {
	Open      bool
	AccountID int64
	Name      string
	Platform  string
	Title     string
	URL       string
	Loading   bool
	Manual    bool
	Resuming  bool
	Busy      bool
	Err       string
	Width     float64
	Height    float64
	CopyText  string
	CopyReady bool
}

func getBrowserStateBridge() BrowserStateDTO {
	a := getApp()
	if a == nil {
		return BrowserStateDTO{}
	}
	if a.detectedNote == "" && !a.camoufoxBusy {
		a.refreshDetected()
	}
	badgeLabel, badgeTone := a.camoufoxRuntimeBadge()
	now := time.Now()
	var activeMap map[int64]bool
	if a.engine != nil {
		activeMap = a.engine.ActiveAccounts()
	}
	rows := make([]BrowserRowDTO, 0, len(a.browserRows))
	var selectedID int64
	if a.browserSel >= 0 && a.browserSel < len(a.browserRows) {
		selectedID = a.browserRows[a.browserSel].ID
	}
	for _, b := range a.browserRows {
		active := activeMap != nil && activeMap[b.ID]
		health := accountHealthLabel(b, active, b.IsLoggedIn, now)
		tone := "neutral"
		switch health {
		case "就绪", "使用中", "运行中":
			tone = "success"
		case "未登录", "冷却中", "需要人工验证":
			tone = "warning"
		case "已禁用", "已用尽":
			tone = "danger"
		}
		email := ""
		usageLbl := ""
		usagePct := 0
		if b.Platform == agy.Platform {
			if strings.TrimSpace(b.UserDataDir) != "" {
				email = agy.Email(b.UserDataDir)
			}
			uv := a.agyUsageView(b.ID)
			usageLbl, usagePct, _ = formatAgyUsageView(uv)
		}
		op, pending := a.browserPending[b.ID]
		pendingOp := ""
		if pending {
			if op.stopping {
				pendingOp = "stopping"
			} else {
				pendingOp = "starting"
			}
		}
		rows = append(rows, BrowserRowDTO{
			ID:         b.ID,
			Platform:   b.Platform,
			Name:       b.Name,
			Email:      email,
			Engine:     b.Engine,
			Extension:  b.ExtAccount != "",
			IsRunning:  b.IsRunning,
			IsLoggedIn: b.IsLoggedIn,
			Active:     active,
			Disabled:   b.Disabled,
			Checked:    a.browserChecked != nil && a.browserChecked[b.ID],
			Pending:    pending,
			PendingOp:  pendingOp,
			Health:     health,
			HealthTone: tone,
			Proxy:      b.Proxy,
			LastError:  b.LastError,
			TodayCount: b.UsedToday(now),
			UsageLabel: usageLbl,
			UsagePct:   usagePct,
		})
	}
	var bs BridgeStatusDTO
	bs.Token = a.extToken
	bs.TokenNote = a.extTokenNote
	bs.Port = a.extPort
	bs.PairLabel = a.bridgePairLabel()
	if a.engine != nil {
		st := a.engine.BridgeStatus()
		bs.Listening = st.Listening
		bs.Addr = st.Addr
		bs.Err = st.Err
		bs.Doubao = st.Doubao
		bs.Qianwen = st.Qianwen
	}
	batchText := ""
	if a.browserBatch != nil {
		batchText = a.browserBatch.text()
	}
	missing := a.missingBrowserNames(a.browserRows)
	missingHint := ""
	if len(missing) > 0 {
		missingHint = "以下勾选平台尚未添加账号：" + strings.Join(missing, "、")
	}
	dto := BrowserStateDTO{
		Backend:       a.browserBackend,
		BackendLabel:  browser.ChoiceLabel(a.browserBackend),
		Engines:       []string{browser.KindCamoufox, browser.KindChromeCDP},
		Ready:         a.camoufoxReady,
		Busy:          a.camoufoxBusy,
		BadgeLabel:    badgeLabel,
		BadgeTone:     badgeTone,
		Status:        a.camoufoxStatus,
		Detail:        a.detectedDetail,
		Percent:       a.camoufoxPercent,
		Done:          a.camoufoxDone,
		Total:         a.camoufoxTotal,
		Speed:         a.camoufoxSpeed,
		Supported:     browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, a.browserBackend),
		Running:       a.running,
		Rows:          rows,
		SelectedID:    selectedID,
		CheckedCount:  len(a.browserChecked),
		BatchActive:   a.browserBatch != nil,
		BatchText:     batchText,
		MissingHint:   missingHint,
		DlBrowserBusy: a.dlBrowserBusy,
		DlBrowserOpen: a.dlBrowser != nil && a.dlBrowser.Alive(),
		DlBrowserURL:  a.dlBrowserURL,
		Bridge:        bs,
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

func selectAccountEngineBridge(choice string) {
	if a := getApp(); a != nil {
		a.selectAccountEngine(choice)
	}
}

func installCamoufoxBridge() {
	if a := getApp(); a != nil {
		a.installCamoufox()
	}
}

func refreshBrowsersBridge() {
	if a := getApp(); a != nil {
		a.refreshDetected()
		a.refreshBrowsers()
	}
}

func createBrowserBridge(platform, name string) {
	if a := getApp(); a != nil {
		a.createBrowser(platform, name)
	}
}

func browserRowActionBridge(id int64, action string) {
	a := getApp()
	if a == nil {
		return
	}
	row, ok := a.browserByID(id)
	if !ok {
		a.refreshBrowsers()
		row, ok = a.browserByID(id)
		if !ok {
			return
		}
	}
	a.selectBrowserID(id)
	switch action {
	case "launch":
		if row.Platform == agy.Platform {
			a.openAgyLane(row)
		} else {
			a.launchBrowserInstance(row, nil)
		}
	case "stop":
		a.stopBrowserInstance(row, nil)
	case "drawer":
		a.openBrowserDrawer(row)
	case "restore":
		a.restoreBrowserByRow(row)
	case "toggleDisabled":
		a.setAccountDisabled(row.ID, !row.Disabled)
		a.refreshBrowsers()
	case "refreshUsage":
		a.markAgyUsageForce(row.ID)
	case "delete":
		a.deleteBrowserByRow(row)
	}
	a.emit("browsers", "workbench")
}

func (a *App) setAccountDisabled(id int64, disabled bool) {
	if a == nil {
		return
	}
	if a.engine != nil {
		_ = a.engine.SetAccountDisabled(id, disabled)
		return
	}
	if a.store != nil {
		_ = a.store.SetBrowserDisabled(id, disabled)
	}
}

func setPlatformAccountsDisabledBridge(platform string, disabled bool) {
	a := getApp()
	if a == nil {
		return
	}
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return
	}
	changed := false
	for _, b := range a.laneBrowsers() {
		if b.Platform == platform && b.Disabled != disabled {
			a.setAccountDisabled(b.ID, disabled)
			changed = true
		}
	}
	if !changed {
		return
	}
	a.refreshBrowsers()
	a.emit("shell")
}

func toggleBrowserCheckBridge(id int64, on bool) {
	a := getApp()
	if a == nil {
		return
	}
	if a.browserChecked == nil {
		a.browserChecked = map[int64]bool{}
	}
	if on {
		a.browserChecked[id] = true
	} else {
		delete(a.browserChecked, id)
	}
	a.emit("browsers")
}

func toggleAllBrowserChecksBridge(on bool) {
	a := getApp()
	if a == nil {
		return
	}
	if a.browserChecked == nil {
		a.browserChecked = map[int64]bool{}
	}
	clear(a.browserChecked)
	if on {
		for _, r := range a.browserRows {
			if r.ExtAccount == "" {
				a.browserChecked[r.ID] = true
			}
		}
	}
	a.emit("browsers")
}

func runBrowserBatchBridge(stopping bool) {
	if a := getApp(); a != nil {
		a.runBrowserBatch(stopping)
		a.emit("browsers")
	}
}

func commitBrowserRenameBridge(id int64, newName string) {
	a := getApp()
	if a == nil || a.store == nil || id <= 0 {
		return
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return
	}
	a.renameID = id
	a.renameDraft = newName
	a.commitRename()
}

func saveAccountProxyBridge(id int64, proxyURL string) error {
	a := getApp()
	if a == nil {
		return nil
	}
	return a.saveAccountProxy(id, proxyURL)
}

func regenerateExtensionTokenBridge() {
	if a := getApp(); a != nil {
		a.regenerateExtensionToken()
	}
}

func agyActionBridge(ctx context.Context, action, value string) error {
	a := getApp()
	if a == nil {
		return nil
	}
	switch action {
	case "openChooser":
		a.addAgyAccount()
	case "closeChooser":
		a.agyChooser = nil
	case "startOAuth":
		a.startAgyOAuth()
	case "pickFolder":
		dirs, err := a.pickDirsAsync(ctx, "选择 Antigravity 目录", false)
		if err != nil {
			return err
		}
		if len(dirs) > 0 {
			_ = a.onScope(ctx, func() { a.agySelectImportFolder(dirs[0]) })
		}
	case "startImport":
		a.startAgyImport()
	case "openAuthURL":
		a.openAgyAuthURL()
	case "copyAuthURL":
		if a.agyLogin != nil && a.agyLogin.url != "" {
			a.copyText(a.agyLogin.url)
			a.agyLogin.copied = true
		}
	case "submitCode":
		a.submitAgyCode(value)
	case "retryLogin":
		a.retryAgyLogin()
	case "cancelLogin":
		a.closeAgyLogin()
	}
	a.emit("browsers", "wizard")
	return nil
}

func openBrowserDrawerBridge(id int64) {
	a := getApp()
	if a == nil {
		return
	}
	for _, row := range a.configuredBrowsers() {
		if row.ID == id {
			a.openBrowserDrawer(row)
			return
		}
	}
}

func closeBrowserDrawerBridge() {
	if a := getApp(); a != nil {
		a.closeBrowserDrawer()
	}
}

func resumeBrowserDrawerBridge() {
	a := getApp()
	if a == nil || a.browserDrawer == nil {
		return
	}
	s := a.browserDrawer
	if s.resuming || !s.held.Load() {
		return
	}
	s.resuming = true
	a.browserDrawerSend(s, cdp.PageInput{Kind: "resume"})
	a.emit("browserDrawer")
}

func clearBrowserDrawerCopyBridge() {
	a := getApp()
	if a != nil && a.browserDrawer != nil {
		a.browserDrawer.copyReady = false
		a.browserDrawer.copyText = ""
		a.emit("browserDrawer")
	}
}

func getBrowserDrawerStateBridge() BrowserDrawerDTO {
	a := getApp()
	if a == nil || a.browserDrawer == nil {
		return BrowserDrawerDTO{}
	}
	s := a.browserDrawer
	return BrowserDrawerDTO{
		Open:      true,
		AccountID: s.row.ID,
		Name:      s.row.Name,
		Platform:  s.row.Platform,
		Title:     s.title,
		URL:       s.url,
		Loading:   s.loading,
		Manual:    s.held.Load(),
		Resuming:  s.resuming,
		Busy:      a.browserDrawerBusy(s.row.ID),
		Err:       s.err,
		Width:     s.width,
		Height:    s.height,
		CopyText:  s.copyText,
		CopyReady: s.copyReady,
	}
}
