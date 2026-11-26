package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
)

// BrowserService exposes account browser management, runtime installation,
// and interactive CDP browser drawer IPCs.
type BrowserService struct {
	app *App
}

// BrowserRowDTO describes a single managed provider account row.
type BrowserRowDTO struct {
	ID         int64  `json:"id"`
	Platform   string `json:"platform"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Engine     string `json:"engine"`
	IsRunning  bool   `json:"isRunning"`
	IsLoggedIn bool   `json:"isLoggedIn"`
	Active     bool   `json:"active"`
	Disabled   bool   `json:"disabled"`
	Checked    bool   `json:"checked"`
	Pending    bool   `json:"pending"`
	PendingOp  string `json:"pendingOp"`
	Health     string `json:"health"`
	HealthTone string `json:"healthTone"`
	Proxy      string `json:"proxy"`
	LastError  string `json:"lastError"`
	TodayCount int    `json:"todayCount"`
	UsageLabel string `json:"usageLabel"`
	UsagePct   int    `json:"usagePct"`
}

// BrowserStateDTO represents the full account browser management page snapshot.
type BrowserStateDTO struct {
	Backend       string          `json:"backend"`
	BackendLabel  string          `json:"backendLabel"`
	Engines       []string        `json:"engines"`
	Ready         bool            `json:"ready"`
	Busy          bool            `json:"busy"`
	BadgeLabel    string          `json:"badgeLabel"`
	BadgeTone     string          `json:"badgeTone"`
	Status        string          `json:"status"`
	Detail        string          `json:"detail"`
	Percent       float64         `json:"percent"`
	Done          int64           `json:"done"`
	Total         int64           `json:"total"`
	Speed         int64           `json:"speed"`
	Supported     bool            `json:"supported"`
	Running       bool            `json:"running"`
	Rows          []BrowserRowDTO `json:"rows"`
	SelectedID    int64           `json:"selectedId"`
	CheckedCount  int             `json:"checkedCount"`
	BatchActive   bool            `json:"batchActive"`
	BatchKind     string          `json:"batchKind"`
	BatchText     string          `json:"batchText"`
	MissingHint     string              `json:"missingHint"`
	SharedExitHints []SharedExitHintDTO `json:"sharedExitHints"`
	DlBrowserBusy   bool                `json:"dlBrowserBusy"`
	DlBrowserOpen   bool                `json:"dlBrowserOpen"`
	DlBrowserURL    string              `json:"dlBrowserUrl"`
}

// SharedExitHintDTO describes a same-platform shared-exit warning in the account pool.
type SharedExitHintDTO struct {
	Platform string `json:"platform"`
	Message  string `json:"message"`
}

// BrowserDrawerDTO represents the floating interactive CDP preview drawer state.
type BrowserDrawerDTO struct {
	Open         bool    `json:"open"`
	AccountID    int64   `json:"accountId"`
	Name         string  `json:"name"`
	Platform     string  `json:"platform"`
	Title        string  `json:"title"`
	URL          string  `json:"url"`
	Loading        bool    `json:"loading"`
	Manual         bool    `json:"manual"`
	AwaitingVerify bool    `json:"awaitingVerify"`
	Resuming       bool    `json:"resuming"`
	Busy         bool    `json:"busy"`
	Err          string  `json:"err"`
	Width        float64 `json:"width"`
	Height       float64 `json:"height"`
	CopyText     string  `json:"copyText"`
	CopyReady    bool    `json:"copyReady"`
	FrameVersion uint64  `json:"frameVersion"`
	FrameURL     string  `json:"frameUrl"`
}

func (a *App) buildBrowserState() BrowserStateDTO {
	if a == nil {
		return BrowserStateDTO{
			Engines:         []string{browser.KindFingerprint},
			Rows:            []BrowserRowDTO{},
			SharedExitHints: []SharedExitHintDTO{},
		}
	}
	if a.detectedNote == "" && !a.runtimeBusy {
		a.refreshDetected()
	}
	badgeLabel, badgeTone := a.runtimeBadge()
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
		if b.Platform != "豆包" && b.Platform != "千问" && b.Platform != "千问国际" {
			continue
		}
		if a.manager != nil {
			b.IsRunning = a.manager.IsRunning(b.ID)
		}
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
		lastErr := b.LastError
		// 行可能是换日之前读进缓存的：到期的额度、冷却错误只是历史，不再显示。
		if b.Expired(now) {
			lastErr = ""
		}
		op, pending := a.browserPending[b.ID]
		pendingOp := ""
		if pending {
			pendingOp = op.opKind()
		}
		rows = append(rows, BrowserRowDTO{
			ID:         b.ID,
			Platform:   b.Platform,
			Name:       b.Name,
			Engine:     b.Engine,
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
			LastError:  lastErr,
			TodayCount: b.UsedToday(now),
		})
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
	return BrowserStateDTO{
		Backend:       a.browserBackend,
		BackendLabel:  browser.ChoiceLabel(a.browserBackend),
		Engines:       []string{browser.KindFingerprint},
		Ready:         a.runtimeReady,
		Busy:          a.runtimeBusy,
		BadgeLabel:    badgeLabel,
		BadgeTone:     badgeTone,
		Status:        a.runtimeStatus,
		Detail:        a.detectedDetail,
		Percent:       a.runtimePercent,
		Done:          a.runtimeDone,
		Total:         a.runtimeTotal,
		Speed:         a.runtimeSpeed,
		Supported:     browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, a.browserBackend),
		Running:       a.running,
		Rows:          rows,
		SelectedID:    selectedID,
		CheckedCount:  len(a.browserChecked),
		BatchActive:     a.browserBatch != nil,
		BatchKind:       a.browserBatch.opKind(),
		BatchText:       batchText,
		MissingHint:     missingHint,
		SharedExitHints: a.sharedExitHints(a.browserRows),
		DlBrowserBusy:   a.dlBrowserBusy,
		DlBrowserOpen:   a.dlBrowser != nil && a.dlBrowser.Alive(),
		DlBrowserURL:    a.dlBrowserURL,
	}
}

func (a *App) loadDismissedSharedExitHints(s *store.Store) {
	a.dismissedSharedExitHints = map[string]string{}
	if s == nil {
		return
	}
	raw := strings.TrimSpace(s.Get(settingDismissedSharedExitHints))
	if raw == "" {
		return
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return
	}
	for platform, msg := range parsed {
		platform = strings.TrimSpace(platform)
		msg = strings.TrimSpace(msg)
		if (platform == "豆包" || platform == "千问" || platform == "千问国际") && msg != "" {
			a.dismissedSharedExitHints[platform] = msg
		}
	}
}

func (a *App) persistDismissedSharedExitHints() {
	if a == nil || a.store == nil {
		return
	}
	if len(a.dismissedSharedExitHints) == 0 {
		a.store.Set(settingDismissedSharedExitHints, "")
		return
	}
	data, err := json.Marshal(a.dismissedSharedExitHints)
	if err != nil {
		return
	}
	a.store.Set(settingDismissedSharedExitHints, string(data))
}

func (a *App) dismissSharedExitHints(platforms []string) {
	if a == nil || len(platforms) == 0 {
		return
	}
	changed := false
	for _, raw := range platforms {
		platform := strings.TrimSpace(raw)
		if platform != "豆包" && platform != "千问" && platform != "千问国际" {
			continue
		}
		msg := a.platformSharedExitHint(a.browserRows, platform)
		if msg == "" {
			continue
		}
		if a.dismissedSharedExitHints == nil {
			a.dismissedSharedExitHints = map[string]string{}
		}
		if a.dismissedSharedExitHints[platform] != msg {
			a.dismissedSharedExitHints[platform] = msg
			changed = true
		}
	}
	if !changed {
		return
	}
	a.persistDismissedSharedExitHints()
	a.emit("browsers")
}

func (a *App) sharedExitHints(accounts []store.Browser) []SharedExitHintDTO {
	hints := []SharedExitHintDTO{}
	for _, platform := range []string{"豆包", "千问", "千问国际"} {
		msg := a.platformSharedExitHint(accounts, platform)
		if msg == "" {
			continue
		}
		if a.dismissedSharedExitHints[platform] == msg {
			continue
		}
		hints = append(hints, SharedExitHintDTO{
			Platform: platform,
			Message:  msg,
		})
	}
	return hints
}

func (a *App) platformSharedExitHint(accounts []store.Browser, platform string) string {
	globalProxy := browser.CurrentProxyServer()
	sharedAccounts := 0
	directShared := false
	for _, b := range accounts {
		if b.Platform != platform || b.Disabled {
			continue
		}
		if len(browser.SamePlatformSharedExit(accounts, b, globalProxy)) > 0 {
			sharedAccounts++
			effective := strings.TrimSpace(b.Proxy)
			if effective == "" {
				effective = strings.TrimSpace(globalProxy)
			}
			if effective == "" {
				directShared = true
			}
		}
	}
	if sharedAccounts < 2 {
		return ""
	}
	if directShared {
		return fmt.Sprintf("当前有 %d 个%s账号共用本机直连出口，建议配置独立代理或增大发送间隔以减少触发验证", sharedAccounts, platform)
	}
	return fmt.Sprintf("当前有 %d 个%s账号共用同一代理出口，建议配置独立代理或增大发送间隔以减少触发验证", sharedAccounts, platform)
}

// GetBrowserState returns the managed accounts and browser runtime status snapshot.
func (s *BrowserService) GetBrowserState(ctx context.Context) BrowserStateDTO {
	if s.app == nil {
		return (*App)(nil).buildBrowserState()
	}
	out, err := callValue(s.app, ctx, func() BrowserStateDTO {
		return s.app.buildBrowserState()
	})
	if err != nil {
		return (*App)(nil).buildBrowserState()
	}
	return out
}

// InstallBrowserRuntime triggers installation of the project-managed Fingerprint browser runtime.
func (s *BrowserService) InstallBrowserRuntime(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.installBrowserRuntime()
	})
}

// DismissSharedExitHints dismisses the current shared-exit warnings for the specified platforms.
func (s *BrowserService) DismissSharedExitHints(ctx context.Context, platforms []string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.dismissSharedExitHints(platforms)
	})
}

// RefreshBrowsers re-checks runtime detection and reloads account browser rows.
func (s *BrowserService) RefreshBrowsers(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.refreshDetected()
		s.app.refreshBrowsers()
	})
}

// CreateBrowser creates a new isolated provider account profile.
func (s *BrowserService) CreateBrowser(ctx context.Context, platform, name string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.createBrowser(platform, name)
	})
}

// BrowserRowAction executes an action on one account row (launch, stop, drawer, restore, toggleDisabled, delete).
func (s *BrowserService) BrowserRowAction(ctx context.Context, id int64, action string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		row, ok := a.browserByID(id)
		if !ok {
			a.refreshBrowsers()
			row, ok = a.browserByID(id)
			if !ok {
				return
			}
		}
		a.selectBrowserID(id)
		if a.manager != nil {
			live := a.manager.IsRunning(row.ID)
			row.IsRunning = live
			for i := range a.browserRows {
				if a.browserRows[i].ID == row.ID {
					a.browserRows[i].IsRunning = live
					break
				}
			}
		}
		switch action {
		case "launch":
			a.launchBrowserInstance(row, nil)
		case "stop":
			a.stopBrowserInstance(row, nil)
		case "drawer":
			a.openBrowserDrawer(row)
		case "restore":
			a.restoreBrowserByRow(row)
		case "toggleDisabled":
			a.setAccountDisabled(row.ID, !row.Disabled)
			a.refreshBrowsers()
		case "delete":
			a.deleteBrowserByRow(row)
		}
		a.emit("browsers", "workbench")
	})
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

// SetPlatformAccountsDisabled enables or disables all accounts belonging to a platform.
func (s *BrowserService) SetPlatformAccountsDisabled(ctx context.Context, platform string, disabled bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
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
	})
}

// ToggleBrowserCheck toggles batch-selection checkbox state for an account row.
func (s *BrowserService) ToggleBrowserCheck(ctx context.Context, id int64, on bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.browserChecked == nil {
			a.browserChecked = map[int64]bool{}
		}
		if on {
			a.browserChecked[id] = true
		} else {
			delete(a.browserChecked, id)
		}
		a.emit("browsers")
	})
}

// ToggleAllBrowserChecks checks or unchecks all local account rows.
func (s *BrowserService) ToggleAllBrowserChecks(ctx context.Context, on bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.browserChecked == nil {
			a.browserChecked = map[int64]bool{}
		}
		clear(a.browserChecked)
		if on {
			for _, r := range a.browserRows {
				a.browserChecked[r.ID] = true
			}
		}
		a.emit("browsers")
	})
}

// RunBrowserBatch launches or stops all checked account browsers in a batch.
func (s *BrowserService) RunBrowserBatch(ctx context.Context, stopping bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.runBrowserBatch(stopping)
		s.app.emit("browsers")
	})
}

// ClearBrowserHistory deletes platform chat history records for one or more accounts.
// When ids is empty, currently checked accounts are used.
func (s *BrowserService) ClearBrowserHistory(ctx context.Context, ids []int64, keepRecent int, fastMode bool) error {
	if s.app == nil {
		return nil
	}
	err, callErr := callValue(s.app, ctx, func() error {
		return s.app.clearBrowserHistory(ids, keepRecent, fastMode)
	})
	if callErr != nil {
		return callErr
	}
	return err
}

// StopClearBrowserHistory cancels an in-progress batch chat history cleanup.
func (s *BrowserService) StopClearBrowserHistory(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.stopClearBrowserHistory()
	})
}

// SyncAccountUsernames fetches and applies real platform usernames for one or more accounts.
// When ids is empty, currently checked accounts are used.
func (s *BrowserService) SyncAccountUsernames(ctx context.Context, ids []int64) error {
	if s.app == nil {
		return nil
	}
	err, callErr := callValue(s.app, ctx, func() error {
		return s.app.syncAccountUsernames(ids)
	})
	if callErr != nil {
		return callErr
	}
	return err
}

// StopSyncAccountUsernames cancels an in-progress batch account username sync.
func (s *BrowserService) StopSyncAccountUsernames(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.stopSyncAccountUsernames()
	})
}

// CommitBrowserRename renames an account browser row.
func (s *BrowserService) CommitBrowserRename(ctx context.Context, id int64, newName string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.store == nil || id <= 0 {
			return
		}
		newName = strings.TrimSpace(newName)
		if newName == "" {
			return
		}
		a.renameID = id
		a.renameDraft = newName
		a.commitRename()
	})
}

// SaveAccountProxy validates and persists a per-account proxy URL.
func (s *BrowserService) SaveAccountProxy(ctx context.Context, id int64, proxyURL string) error {
	if s.app == nil {
		return nil
	}
	var opErr error
	if err := s.app.call(ctx, func() {
		opErr = s.app.saveAccountProxy(id, proxyURL)
	}); err != nil {
		return err
	}
	return opErr
}

// OpenBrowserDrawer opens the floating interactive CDP preview drawer for an account.
func (s *BrowserService) OpenBrowserDrawer(ctx context.Context, id int64) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		for _, row := range s.app.configuredBrowsers() {
			if row.ID == id {
				s.app.openBrowserDrawer(row)
				return
			}
		}
	})
}

// CloseBrowserDrawer closes the floating interactive CDP preview drawer.
func (s *BrowserService) CloseBrowserDrawer(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.closeBrowserDrawerByUser()
	})
}

// ResumeBrowserDrawer hands control back from manual intervention to automated tasks.
func (s *BrowserService) ResumeBrowserDrawer(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.browserDrawer == nil {
			return
		}
		st := a.browserDrawer
		if st.resuming || !st.held.Load() {
			return
		}
		st.resuming = true
		a.browserDrawerSend(st, cdp.PageInput{Kind: "resume"})
		a.emit("browserDrawer")
	})
}

// ClearBrowserDrawerCopy clears the captured copy notification banner in the drawer.
func (s *BrowserService) ClearBrowserDrawerCopy(ctx context.Context) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if s.app.browserDrawer != nil {
			s.app.browserDrawer.copyReady = false
			s.app.browserDrawer.copyText = ""
			s.app.emit("browserDrawer")
		}
	})
}

// GetBrowserDrawerState returns the floating CDP preview drawer state and live frame URL.
func (s *BrowserService) GetBrowserDrawerState(ctx context.Context) BrowserDrawerDTO {
	if s.app == nil {
		return BrowserDrawerDTO{}
	}
	out, err := callValue(s.app, ctx, func() BrowserDrawerDTO {
		a := s.app
		if a.browserDrawer == nil {
			return BrowserDrawerDTO{}
		}
		st := a.browserDrawer
		return BrowserDrawerDTO{
			Open:         true,
			AccountID:    st.row.ID,
			Name:         st.row.Name,
			Platform:     st.row.Platform,
			Title:        st.title,
			URL:          st.url,
			Loading:        st.loading,
			Manual:         st.held.Load(),
			AwaitingVerify: a.engine != nil && a.engine.AccountAwaitingUser(st.row.ID),
			Resuming:       st.resuming,
			Busy:         a.browserDrawerBusy(st.row.ID),
			Err:          st.err,
			Width:        st.width,
			Height:       st.height,
			CopyText:     st.copyText,
			CopyReady:    st.copyReady,
			FrameVersion: st.frameVersion,
			FrameURL:     a.ensureMedia().DrawerFrameURL(st.frameVersion),
		}
	})
	if err != nil {
		return BrowserDrawerDTO{}
	}
	return out
}

// SendBrowserDrawerInput forwards mouse, wheel, and keyboard inputs from the frontend
// BrowserStage component to the active CDP PageView.
func (s *BrowserService) SendBrowserDrawerInput(ctx context.Context, in BrowserInputDTO) bool {
	if s.app == nil {
		return false
	}
	ok, err := callValue(s.app, ctx, func() bool {
		return s.app.handleBrowserDrawerInput(in)
	})
	if err != nil {
		return false
	}
	return ok
}

// ---- Test bridge wrappers ----

func getBrowserStateBridge() BrowserStateDTO {
	return (&BrowserService{app: getApp()}).GetBrowserState(context.Background())
}

func setPlatformAccountsDisabledBridge(platform string, disabled bool) {
	(&BrowserService{app: getApp()}).SetPlatformAccountsDisabled(context.Background(), platform, disabled)
}
