package ui

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/fingerprint"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
	"github.com/xxww0098/tulai/internal/tasks"
)

type browserOperation struct {
	stopping bool
	kind     string // "starting" | "stopping" | "clearing" | "syncing"
	batch    *browserBatchRun
}

func (o browserOperation) opKind() string {
	if o.kind != "" {
		return o.kind
	}
	if o.stopping {
		return "stopping"
	}
	return "starting"
}

type browserBatchRun struct {
	stopping                     bool
	kind                         string // "starting" | "stopping" | "clearing" | "syncing" | "syncing_status"
	statusLabel                  string // "千问" | "Gemini" | "账号"
	fast                         bool
	total, done, failed, skipped int
	deleted                      int
	updated                      int
	current                      string
}

func (b *browserBatchRun) opKind() string {
	if b == nil {
		return ""
	}
	if b.kind != "" {
		return b.kind
	}
	if b.stopping {
		return "stopping"
	}
	return "starting"
}

func (b *browserBatchRun) text() string {
	if b.opKind() == "clearing" {
		mode := "清理历史"
		if b.fast {
			mode = "极速清理历史"
		}
		if b.done >= b.total {
			return fmt.Sprintf("%s完成：%d/%d（共删 %d 条），失败 %d，跳过 %d", mode, b.done, b.total, b.deleted, b.failed, b.skipped)
		}
		base := fmt.Sprintf("正在%s：%d/%d（已删 %d 条）", mode, b.done, b.total, b.deleted)
		if b.current != "" {
			base += " · 当前：" + b.current
		}
		return base
	}
	if b.opKind() == "syncing" {
		if b.done >= b.total {
			return fmt.Sprintf("同步完成：%d/%d（已更新 %d），失败 %d，跳过 %d", b.done, b.total, b.updated, b.failed, b.skipped)
		}
		base := fmt.Sprintf("正在同步账号：%d/%d（已更新 %d）", b.done, b.total, b.updated)
		if b.current != "" {
			base += " · 当前：" + b.current
		}
		return base
	}
	if b.opKind() == "syncing_status" {
		subject := strings.TrimSpace(b.statusLabel)
		if subject == "" {
			subject = "千问"
		}
		if b.done >= b.total {
			return fmt.Sprintf("%s状态同步完成：%d/%d（已更新 %d），失败 %d，跳过 %d", subject, b.done, b.total, b.updated, b.failed, b.skipped)
		}
		base := fmt.Sprintf("正在同步%s状态：%d/%d（已更新 %d）", subject, b.done, b.total, b.updated)
		if b.current != "" {
			base += " · 当前：" + b.current
		}
		return base
	}
	verb := "启动"
	if b.stopping {
		verb = "停止"
	}
	state := "正在批量" + verb
	if b.done == b.total {
		state = "批量" + verb + "完成"
	}
	return fmt.Sprintf("%s：%d/%d，失败 %d，跳过 %d", state, b.done, b.total, b.failed, b.skipped)
}

type browserDrawerState struct {
	row             store.Browser
	startedByDrawer bool
	cancel          context.CancelFunc
	done            chan struct{}
	inputs          chan cdp.PageInput
	held            atomic.Bool
	lastInput       atomic.Int64
	resuming        bool
	loading         bool
	err             string
	title, url      string
	frameVersion    uint64
	width, height   float64
	copyText        string
	copyReady       bool
}

type loginShot struct {
	state string
	hint  string
}

func (a *App) refreshDetected() {
	if a == nil {
		return
	}
	kind := browser.CanonicalBackend(a.browserBackend)
	rt, ok := browser.InstalledRuntime(browser.ConfigDir(), kind)
	a.runtimeReady = ok
	a.detectedDetail = rt.Path
	if ok {
		a.detectedNote = kind + " 已安装"
		a.runtimeStatus = strings.TrimSpace(kind+" "+rt.Version) + " 就绪"
	} else if !browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, kind) {
		a.detectedNote = "当前架构不支持 " + kind
		a.runtimeStatus = planAccountBrowser(kind, false, false).Blocked
	} else {
		a.detectedNote = kind + " 未安装"
		a.runtimeStatus = "安装后即可添加服务商账号"
	}
}

func (a *App) runtimeBadge() (string, string) {
	if a.runtimeBusy {
		return "下载中", "accent"
	}
	if a.runtimeReady {
		return "已安装", "success"
	}
	if !browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, a.browserBackend) {
		return "暂不支持", "neutral"
	}
	if strings.HasPrefix(a.runtimeStatus, "安装失败") {
		return "安装失败", "danger"
	}
	return "未安装", "warning"
}

func (a *App) applyRuntimeProgress(p fingerprint.Progress) {
	a.applyRuntimeProgressAt(time.Now(), p)
}

func (a *App) applyRuntimeProgressAt(now time.Time, p fingerprint.Progress) {
	if p.Text != "" {
		a.runtimeStatus = p.Text
	}
	if p.Stage != "" && p.Stage != a.runtimeStage {
		a.runtimeStage = p.Stage
		a.runtimePercent = -1
		a.runtimeDone, a.runtimeTotal = 0, 0
		a.runtimeSpeed, a.runtimeLast = 0, 0
		a.runtimeAt = time.Time{}
	}
	if p.Done > 0 || p.Total > 0 {
		a.runtimeDone, a.runtimeTotal = p.Done, p.Total
		if p.Total > 0 {
			a.runtimePercent = min(1, float64(p.Done)/float64(p.Total))
		} else {
			a.runtimePercent = -1
		}
		a.trackRuntimeSpeed(now, p.Done)
	}
	if p.Total > 0 && p.Done >= p.Total {
		a.runtimeSpeed = 0
	}
}

func (a *App) trackRuntimeSpeed(now time.Time, done int64) {
	if a.runtimeAt.IsZero() {
		a.runtimeAt, a.runtimeLast = now, done
		return
	}
	dt := now.Sub(a.runtimeAt)
	if dt < 300*time.Millisecond {
		return
	}
	if delta := done - a.runtimeLast; delta >= 0 {
		instant := int64(float64(delta) / dt.Seconds())
		if a.runtimeSpeed == 0 {
			a.runtimeSpeed = instant
		} else {
			a.runtimeSpeed = (a.runtimeSpeed*2 + instant) / 3
		}
	}
	a.runtimeAt, a.runtimeLast = now, done
}

func (a *App) tickRuntimeProgress(now time.Time) {
	if !a.runtimeBusy || a.runtimeSpeed <= 0 || a.runtimeAt.IsZero() {
		return
	}
	if now.Sub(a.runtimeAt) >= 2*time.Second {
		a.runtimeSpeed = 0
	}
}

func (a *App) runtimeTick(done <-chan struct{}) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			a.syncUI(func() {
				a.tickRuntimeProgress(now)
				a.emit("browsers", "wizard")
			})
		}
	}
}

func (a *App) installBrowserRuntime() { a.prepareBrowserRuntime(false) }

func (a *App) prepareBrowserRuntime(update bool) {
	if a.runtimeBusy || a.running || a.renameUpdateActive() {
		return
	}
	a.runtimeBusy = true
	kind := browser.ChoiceLabel(a.browserBackend)
	a.runtimeStatus = "正在准备 " + kind + "…"
	a.runtimePercent = -1
	a.runtimeDone, a.runtimeTotal, a.runtimeSpeed, a.runtimeLast = 0, 0, 0, 0
	a.runtimeAt = time.Time{}
	a.runtimeStage = ""
	a.pushProxy()
	a.emit("browsers", "wizard", "download")
	go func() {
		done := make(chan struct{})
		go a.runtimeTick(done)
		defer close(done)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()
		prepare := browser.EnsureRuntime
		if update {
			prepare = browser.UpdateRuntime
		}
		rt, err := prepare(ctx, browser.ConfigDir(), kind, func(p fingerprint.Progress) {
			a.syncUI(func() {
				a.applyRuntimeProgress(p)
				a.emit("browsers", "wizard")
			})
		})
		a.syncUI(func() {
			a.runtimeBusy = false
			defer a.maybeInstallPending()
			if err != nil {
				a.runtimeStatus = "安装失败：" + err.Error()
				if update {
					a.runtimeStatus = "更新失败：" + err.Error()
				}
			} else {
				a.refreshDetected()
				a.refreshWizardPlan()
				if update {
					a.runtimeStatus = "当前内核版本 " + rt.Version
				}
			}
			a.emit("browsers", "wizard", "workbench", "download")
		})
	}()
}

func (a *App) refreshBrowsers() {
	a.loadBrowserRows()
	a.emit("browsers", "workbench", "shell")
}

func (a *App) loadBrowserRows() {
	if a == nil || a.store == nil {
		return
	}
	rows := a.store.Browsers()
	missing := maps.Clone(a.browserChecked)
	for i := range rows {
		delete(missing, rows[i].ID)
		if a.manager != nil {
			rows[i].IsRunning = a.manager.IsRunning(rows[i].ID)
		}
	}
	a.browserRows = rows
	if a.browserSel >= len(rows) {
		a.browserSel = len(rows) - 1
	}
	for id := range missing {
		delete(a.browserChecked, id)
	}
}

func (a *App) selectedBrowser() (store.Browser, bool) {
	if a.browserSel >= 0 && a.browserSel < len(a.browserRows) {
		return a.browserRows[a.browserSel], true
	}
	return store.Browser{}, false
}

func (a *App) browserByID(id int64) (store.Browser, bool) {
	for _, r := range a.browserRows {
		if r.ID == id {
			return r, true
		}
	}
	return store.Browser{}, false
}

func (a *App) createBrowser(platform, name string) {
	if a == nil || a.running || a.store == nil {
		return
	}
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "豆包"
	}
	if platform != "豆包" && platform != "千问" && platform != "千问国际" && platform != "Gemini" {
		return
	}
	explicitName := strings.TrimSpace(name) != ""
	name = strings.TrimSpace(name)
	if name == "" {
		var existing []string
		for _, r := range a.configuredBrowsers() {
			existing = append(existing, r.Name)
		}
		name = nextInstanceName(platform, existing)
	}
	kind := browser.CanonicalBackend(a.browserBackend)
	dir := ""
	if strings.TrimSpace(a.dataRoot) != "" {
		dir = browser.ProfileDir(a.dataRoot, kind, "account-"+rand.Text())
	}
	row, err := a.store.AddManagedBrowser(platform, name, dir, kind)
	if err != nil {
		logging.Log.Errorf("添加账号失败: %v", err)
		return
	}
	a.markFreshLogin(row.ID)
	if !explicitName {
		a.markAutoName(row.ID, row.Name)
	}
	a.refreshBrowsers()
	a.selectBrowserID(row.ID)
	a.launchBrowserInstance(row, nil)
}

func (a *App) selectBrowserID(id int64) {
	for i := range a.browserRows {
		if a.browserRows[i].ID == id {
			a.browserSel = i
			return
		}
	}
}

func (a *App) beginRename(b store.Browser) {
	a.renameID = b.ID
	a.renameDraft = b.Name
	a.selectBrowserID(b.ID)
}

func (a *App) commitRename() {
	id := a.renameID
	name := strings.TrimSpace(a.renameDraft)
	a.renameID = 0
	if id <= 0 || name == "" || a.store == nil {
		return
	}
	a.clearAutoName(id)
	a.store.RenameBrowser(id, name)
	for i := range a.browserRows {
		if a.browserRows[i].ID == id {
			a.browserRows[i].Name = name
		}
	}
	if a.wizardAccount.ID == id {
		a.wizardAccount.Name = name
		a.wizardName = name
	}
	a.refreshBrowsers()
}

func (a *App) cancelRename() {
	a.renameID = 0
}

func (a *App) browserOperationAllowed(b store.Browser, stopping bool) bool {
	if a.running || b.ID <= 0 || b.IsRunning != stopping {
		return false
	}
	if _, pending := a.browserPending[b.ID]; pending {
		return false
	}
	return a.engine == nil || !a.engine.ActiveAccounts()[b.ID]
}

func (a *App) beginBrowserOperation(b store.Browser, stopping bool, batch *browserBatchRun) bool {
	if !a.browserOperationAllowed(b, stopping) {
		if batch != nil {
			batch.total--
			batch.skipped++
		}
		return false
	}
	if a.browserPending == nil {
		a.browserPending = map[int64]browserOperation{}
	}
	a.browserPending[b.ID] = browserOperation{stopping: stopping, batch: batch}
	return true
}

func (a *App) finishBrowserOperation(id int64, err error) {
	op, ok := a.browserPending[id]
	if ok {
		delete(a.browserPending, id)
		if b := op.batch; b != nil {
			b.done++
			if err != nil {
				b.failed++
			}
			if b.done >= b.total {
				a.browserBatch = nil
			}
		}
	}
	a.refreshBrowsers()
}

func (a *App) runBrowserBatch(stopping bool) {
	if a.running || len(a.browserPending) > 0 {
		return
	}
	var rows []store.Browser
	for _, b := range a.browserRows {
		if a.browserChecked[b.ID] && a.browserOperationAllowed(b, stopping) {
			rows = append(rows, b)
		}
	}
	if len(rows) == 0 {
		return
	}
	batch := &browserBatchRun{stopping: stopping, total: len(rows), skipped: len(a.browserChecked) - len(rows)}
	a.browserBatch = batch
	for _, b := range rows {
		if stopping {
			a.stopBrowserInstance(b, batch)
		} else {
			a.launchBrowserInstance(b, batch)
		}
	}
}

func (a *App) clearHistoryAllowed(b store.Browser) bool {
	if a.running || b.ID <= 0 || (b.Platform != "豆包" && b.Platform != "千问" && b.Platform != "千问国际" && b.Platform != "Gemini") {
		return false
	}
	if _, pending := a.browserPending[b.ID]; pending {
		return false
	}
	return a.engine == nil || !a.engine.ActiveAccounts()[b.ID]
}

func (a *App) clearBrowserHistory(ids []int64, keepRecent int, fastMode bool) error {
	if a.running {
		return errors.New("任务运行中，请先停止任务后再清理聊天记录")
	}
	if a.clearHistoryCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "clearing") {
		return errors.New("正在清理聊天历史，请等待完成或先停止")
	}
	if a.syncNamesCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "syncing") {
		return errors.New("正在同步账号用户名，请等待完成后再清理聊天历史")
	}
	if a.syncQwenCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "syncing_status") {
		return errors.New("正在同步千问状态，请等待完成后再清理聊天历史")
	}
	keepRecent = clampInt(keepRecent, 0, 500)
	var requested []store.Browser
	seen := map[int64]bool{}
	if len(ids) > 0 {
		for _, id := range ids {
			if id <= 0 || seen[id] {
				continue
			}
			if row, ok := a.browserByID(id); ok {
				seen[id] = true
				requested = append(requested, row)
			}
		}
	} else {
		for _, row := range a.browserRows {
			if a.browserChecked[row.ID] && !seen[row.ID] {
				seen[row.ID] = true
				requested = append(requested, row)
			}
		}
	}
	if len(requested) == 0 {
		return errors.New("请先选择要清理聊天记录的账号")
	}
	var targets []store.Browser
	for _, row := range requested {
		if a.clearHistoryAllowed(row) {
			targets = append(targets, row)
		}
	}
	if len(targets) == 0 {
		return errors.New("所选账号当前不可清理（可能正在操作中或不支持）")
	}
	if a.clearHistoryFn == nil && a.manager == nil {
		return errors.New("没有受管浏览器管理器，无法清理聊天历史")
	}
	if a.browserPending == nil {
		a.browserPending = map[int64]browserOperation{}
	}
	batch := &browserBatchRun{
		kind:    "clearing",
		fast:    fastMode,
		total:   len(targets),
		skipped: len(requested) - len(targets),
	}
	for _, row := range targets {
		a.browserPending[row.ID] = browserOperation{kind: "clearing", batch: batch}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	a.browserBatch = batch
	a.clearHistoryCancel = cancel
	a.emit("browsers", "workbench")

	// 按平台分组并行清理（豆包与千问互不等待，同平台内仍串行以降低同出口风控）。
	groups := map[string][]store.Browser{}
	var order []string
	for _, row := range targets {
		if _, ok := groups[row.Platform]; !ok {
			order = append(order, row.Platform)
		}
		groups[row.Platform] = append(groups[row.Platform], row)
	}

	go func() {
		defer func() {
			cancel()
			a.postUI(func() {
				if a.browserBatch == batch {
					a.browserBatch = nil
				}
				a.clearHistoryCancel = nil
				for _, row := range targets {
					if op, ok := a.browserPending[row.ID]; ok && op.batch == batch {
						delete(a.browserPending, row.ID)
					}
				}
				a.refreshBrowsers()
				a.emit("browsers", "workbench")
			})
		}()

		var totalDeleted atomic.Int64
		var wg sync.WaitGroup
		for _, plat := range order {
			rows := groups[plat]
			wg.Add(1)
			go func(list []store.Browser) {
				defer wg.Done()
				for _, row := range list {
					if runCtx.Err() != nil {
						a.postUI(func() {
							a.finishBrowserOperation(row.ID, context.Canceled)
							a.emit("browsers")
						})
						continue
					}
					label := fmt.Sprintf("%s/%s", row.Platform, accountLabel(row))
					a.postUI(func() {
						batch.current = label
						a.emit("browsers")
					})
					lastCount := 0
					opts := strategy.ClearHistoryOptions{
						KeepRecent: keepRecent,
						FastMode:   fastMode,
						OnProgress: func(deleted int, title string) {
							delta := deleted - lastCount
							if delta < 0 {
								delta = 0
							}
							lastCount = deleted
							a.postUI(func() {
								batch.deleted += delta
								a.emit("browsers")
							})
						},
					}
					res, err := a.clearOneBrowserHistory(runCtx, row, opts)
					if res.Deleted > lastCount {
						extra := res.Deleted - lastCount
						a.postUI(func() {
							batch.deleted += extra
							a.emit("browsers")
						})
					} else if res.Deleted < lastCount {
						// 若极速模式首轮核对回滚了未生效计数，同步修正进度条显示。
						revert := lastCount - res.Deleted
						a.postUI(func() {
							batch.deleted = max(0, batch.deleted-revert)
							a.emit("browsers")
						})
					}
					totalDeleted.Add(int64(res.Deleted))
					switch {
					case errors.Is(err, context.Canceled):
						logging.Log.Info("账号[%s] 聊天历史清理已停止（本次已删 %d 条）", label, res.Deleted)
					case err != nil:
						logging.Log.Warning("账号[%s] 清理聊天历史中断（已删 %d 条）：%v", label, res.Deleted, err)
					case res.FastDeleted > 0:
						logging.Log.Success("账号[%s] 极速清理完成：共删 %d 条（快删 %d 条），剩余 %d 条", label, res.Deleted, res.FastDeleted, res.Remaining)
					default:
						logging.Log.Success("账号[%s] 聊天历史清理完成：已删 %d 条，剩余 %d 条", label, res.Deleted, res.Remaining)
					}
					a.postUI(func() {
						a.finishBrowserOperation(row.ID, err)
						a.emit("browsers")
					})
				}
			}(rows)
		}
		wg.Wait()
		if runCtx.Err() == nil {
			logging.Log.Info("本批聊天历史清理结束：共处理 %d 个账号，累计删除 %d 条对话", len(targets), totalDeleted.Load())
		}
	}()
	return nil
}

func (a *App) stopClearBrowserHistory() {
	if a.clearHistoryCancel != nil {
		a.clearHistoryCancel()
		logging.Log.Info("已请求停止聊天历史清理")
	}
}

func (a *App) clearOneBrowserHistory(ctx context.Context, b store.Browser, opts strategy.ClearHistoryOptions) (strategy.ClearHistoryResult, error) {
	if a.clearHistoryFn != nil {
		return a.clearHistoryFn(ctx, b, opts)
	}
	plat := strategy.Factory(b.Platform)
	if plat == nil {
		return strategy.ClearHistoryResult{}, fmt.Errorf("不支持的平台：%s", b.Platform)
	}
	cleaner, ok := plat.(strategy.HistoryCleaner)
	if !ok {
		return strategy.ClearHistoryResult{}, fmt.Errorf("平台 %s 暂不支持历史清理", b.Platform)
	}
	if a.manager == nil {
		return strategy.ClearHistoryResult{}, errors.New("受管浏览器管理器未初始化")
	}
	a.pushProxy()
	wasRunning := a.manager.IsRunning(b.ID)
	ws, err := a.manager.StartView(ctx, b, a.dataRoot, plat.HomeURL())
	if err != nil {
		return strategy.ClearHistoryResult{}, fmt.Errorf("打开受管浏览器失败: %w", err)
	}
	if !wasRunning {
		defer func() {
			a.manager.Stop(b.ID)
			if a.store != nil {
				a.store.SetRunning(b.ID, a.manager.IsRunning(b.ID))
			}
		}()
	}
	client, err := cdp.NewClient(ws)
	if err != nil {
		return strategy.ClearHistoryResult{}, fmt.Errorf("连接受管浏览器失败: %w", err)
	}
	defer client.Close()
	sess, err := client.NewBackgroundSession(plat.HomeURL(), nil)
	if err != nil {
		return strategy.ClearHistoryResult{}, fmt.Errorf("打开平台页面失败: %w", err)
	}
	defer sess.Close()
	strategy.WaitHistorySidebarReady(ctx, sess, b.Platform)
	return cleaner.ClearHistory(ctx, sess, opts)
}

func (a *App) syncAccountUsernames(ids []int64) error {
	if a.running {
		return errors.New("任务运行中，请先停止任务后再同步用户名")
	}
	if a.syncNamesCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "syncing") {
		return errors.New("正在同步账号用户名，请等待完成或先停止")
	}
	if a.syncQwenCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "syncing_status") {
		return errors.New("正在同步账号状态，请等待完成后再同步用户名")
	}
	if a.clearHistoryCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "clearing") {
		return errors.New("正在清理聊天历史，请等待完成后再同步用户名")
	}
	var requested []store.Browser
	seen := map[int64]bool{}
	if len(ids) > 0 {
		for _, id := range ids {
			if id <= 0 || seen[id] {
				continue
			}
			if row, ok := a.browserByID(id); ok {
				seen[id] = true
				requested = append(requested, row)
			}
		}
	} else {
		for _, row := range a.browserRows {
			if a.browserChecked[row.ID] && !seen[row.ID] {
				seen[row.ID] = true
				requested = append(requested, row)
			}
		}
	}
	if len(requested) == 0 {
		return errors.New("请先选择要同步用户名的账号")
	}
	var targets []store.Browser
	for _, row := range requested {
		if a.clearHistoryAllowed(row) {
			targets = append(targets, row)
		}
	}
	if len(targets) == 0 {
		return errors.New("所选账号当前不可同步（可能正在操作中或不支持）")
	}
	if a.syncAccountNameFn == nil && a.manager == nil {
		return errors.New("没有受管浏览器管理器，无法同步用户名")
	}
	if a.browserPending == nil {
		a.browserPending = map[int64]browserOperation{}
	}
	batch := &browserBatchRun{
		kind:    "syncing",
		total:   len(targets),
		skipped: len(requested) - len(targets),
	}
	for _, row := range targets {
		a.browserPending[row.ID] = browserOperation{kind: "syncing", batch: batch}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	a.browserBatch = batch
	a.syncNamesCancel = cancel
	a.emit("browsers", "workbench")

	groups := map[string][]store.Browser{}
	var order []string
	for _, row := range targets {
		if _, ok := groups[row.Platform]; !ok {
			order = append(order, row.Platform)
		}
		groups[row.Platform] = append(groups[row.Platform], row)
	}

	go func() {
		defer func() {
			cancel()
			a.postUI(func() {
				if a.browserBatch == batch {
					a.browserBatch = nil
				}
				a.syncNamesCancel = nil
				for _, row := range targets {
					if op, ok := a.browserPending[row.ID]; ok && op.batch == batch {
						delete(a.browserPending, row.ID)
					}
				}
				a.refreshBrowsers()
				a.emit("browsers", "workbench", "shell")
			})
		}()

		var totalUpdated atomic.Int64
		var wg sync.WaitGroup
		for _, plat := range order {
			rows := groups[plat]
			wg.Add(1)
			go func(list []store.Browser) {
				defer wg.Done()
				for _, row := range list {
					if runCtx.Err() != nil {
						a.postUI(func() {
							a.finishBrowserOperation(row.ID, context.Canceled)
							a.emit("browsers")
						})
						continue
					}
					label := fmt.Sprintf("%s/%s", row.Platform, accountLabel(row))
					a.postUI(func() {
						batch.current = label
						a.emit("browsers")
					})
					scraped, err := a.syncOneAccountUsername(runCtx, row)
					if err == nil {
						scraped = cdp.SanitizeAccountUsername(scraped)
						if scraped == "" {
							err = errors.New("未能从页面识别到用户名")
						}
					}
					var renamed string
					var changed bool
					if err == nil {
						a.syncUI(func() {
							renamed, changed = a.applySyncedAccountName(row.ID, scraped)
							if changed {
								batch.updated++
							}
							if cur, ok := a.browserByID(row.ID); ok && (!cur.IsLoggedIn || accountNeedsLogin(cur)) {
								a.confirmLogin(row.ID, true)
							}
							a.finishBrowserOperation(row.ID, nil)
							a.emit("browsers", "workbench", "shell")
						})
						if changed {
							totalUpdated.Add(1)
							logging.Log.Success("账号[%s] 已同步：%s", label, renamed)
						} else {
							logging.Log.Info("账号[%s] 同步完成：%s", label, renamed)
						}
						continue
					}
					switch {
					case errors.Is(err, context.Canceled):
						logging.Log.Info("账号[%s] 用户名同步已停止", label)
					default:
						logging.Log.Warning("账号[%s] 同步用户名失败：%v", label, err)
					}
					a.postUI(func() {
						a.finishBrowserOperation(row.ID, err)
						a.emit("browsers")
					})
				}
			}(rows)
		}
		wg.Wait()
		if runCtx.Err() == nil {
			logging.Log.Info("本批用户名同步结束：共处理 %d 个账号，更新 %d 个名称", len(targets), totalUpdated.Load())
		}
	}()
	return nil
}

func (a *App) stopSyncAccountUsernames() {
	if a.syncNamesCancel != nil {
		a.syncNamesCancel()
		logging.Log.Info("已请求停止同步账号用户名")
	}
}

func (a *App) syncOneAccountUsername(ctx context.Context, b store.Browser) (string, error) {
	if a.syncAccountNameFn != nil {
		return a.syncAccountNameFn(ctx, b)
	}
	plat := strategy.Factory(b.Platform)
	if plat == nil {
		return "", fmt.Errorf("不支持的平台：%s", b.Platform)
	}
	if a.manager == nil {
		return "", errors.New("受管浏览器管理器未初始化")
	}
	a.pushProxy()
	wasRunning := a.manager.IsRunning(b.ID)
	ws, err := a.manager.StartView(ctx, b, a.dataRoot, plat.HomeURL())
	if err != nil {
		return "", fmt.Errorf("打开内置浏览器失败: %w", err)
	}
	if !wasRunning {
		defer func() {
			a.manager.Stop(b.ID)
			if a.store != nil {
				a.store.SetRunning(b.ID, a.manager.IsRunning(b.ID))
			}
		}()
	}
	openCtx, openCancel := context.WithTimeout(ctx, 20*time.Second)
	err = cdp.OpenURLBackground(openCtx, ws, plat.HomeURL())
	openCancel()
	if err != nil {
		return "", fmt.Errorf("打开平台页面失败: %w", err)
	}
	name, pollErr := a.pollSyncedAccountUsername(ctx, ws)
	if ctx.Err() == nil {
		switch b.Platform {
		case "千问":
			qCtx, qCancel := context.WithTimeout(ctx, 6*time.Second)
			if st, qErr := a.probeQwenStatus(qCtx, ws); qErr == nil && st.OK {
				id := b.ID
				a.postUI(func() { a.applyQwenStatus(id, st) })
			}
			qCancel()
		case "豆包":
			dCtx, dCancel := context.WithTimeout(ctx, 6*time.Second)
			if st, dErr := a.probeDoubaoStatus(dCtx, ws); dErr == nil && st.OK {
				id := b.ID
				a.postUI(func() { a.applyDoubaoStatus(id, st) })
			}
			dCancel()
		case "Gemini":
			gCtx, gCancel := context.WithTimeout(ctx, 8*time.Second)
			if st, gErr := a.probeGeminiStatus(gCtx, ws); gErr == nil && st.OK {
				if name == "" {
					if candidate := cdp.SanitizeAccountUsername(st.Username); candidate != "" {
						name = candidate
						pollErr = nil
					} else if candidate := cdp.SanitizeAccountUsername(st.Email); candidate != "" {
						name = candidate
						pollErr = nil
					}
				}
				id := b.ID
				a.postUI(func() { a.applyGeminiStatus(id, st) })
			}
			gCancel()
		}
	}
	return name, pollErr
}

func (a *App) pollSyncedAccountUsername(ctx context.Context, endpoint string) (string, error) {
	a.mu.Lock()
	nameWait := a.accountNameWait
	poll := a.accountNamePoll
	a.mu.Unlock()
	if nameWait <= 0 {
		nameWait = 4 * time.Second
	}
	if poll <= 0 {
		poll = 300 * time.Millisecond
	}
	overallDeadline := time.Now().Add(15 * time.Second)
	var inSince time.Time
	outCount := 0
	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		state, err := cdp.ProbeLogin(probeCtx, endpoint)
		if err == nil {
			state = cdp.NormalizeLoginState(state)
			switch state {
			case "out":
				outCount++
				if outCount >= 2 {
					cancel()
					return "", errors.New("账号未登录，请先打开登录")
				}
			case cdp.RegionBlocked:
				cancel()
				return "", errors.New("当前地区受限，请检查代理设置")
			case "challenge":
				cancel()
				return "", errors.New("触发安全验证，请先打开账号处理验证")
			case "in":
				outCount = 0
				if inSince.IsZero() {
					inSince = time.Now()
				}
				name, nameErr := cdp.ProbeAccountUsername(probeCtx, endpoint)
				cancel()
				if nameErr == nil {
					if clean := cdp.SanitizeAccountUsername(name); clean != "" {
						return clean, nil
					}
				}
				if time.Since(inSince) >= nameWait {
					return "", errors.New("未能从页面识别到用户名")
				}
				goto waitNext
			}
		}
		cancel()
	waitNext:
		remaining := time.Until(overallDeadline)
		if remaining <= 0 {
			if !inSince.IsZero() {
				return "", errors.New("未能从页面识别到用户名")
			}
			return "", errors.New("页面加载超时，未能读取用户名")
		}
		timer := time.NewTimer(min(poll, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
}

func (a *App) applySyncedAccountName(id int64, rawName string) (string, bool) {
	if a == nil || id <= 0 || a.store == nil {
		return "", false
	}
	rows := a.store.Browsers()
	var target store.Browser
	found := false
	for _, r := range rows {
		if r.ID == id {
			target = r
			found = true
			break
		}
	}
	if !found {
		a.clearAutoName(id)
		return "", false
	}
	var samePlatform []string
	for _, r := range rows {
		if r.ID != id && r.Platform == target.Platform {
			samePlatform = append(samePlatform, r.Name)
		}
	}
	finalName := uniqueAccountName(rawName, samePlatform)
	if finalName == "" {
		return target.Name, false
	}
	a.clearAutoName(id)
	if finalName == strings.TrimSpace(target.Name) {
		return finalName, false
	}
	a.store.RenameBrowser(id, finalName)
	for i := range a.browserRows {
		if a.browserRows[i].ID == id {
			a.browserRows[i].Name = finalName
		}
	}
	if a.wizardAccount.ID == id {
		a.wizardAccount.Name = finalName
		a.wizardName = finalName
	}
	if a.browserDrawer != nil && a.browserDrawer.row.ID == id {
		a.browserDrawer.row.Name = finalName
		a.emit("browserDrawer")
	}
	return finalName, true
}

func (a *App) launchBrowserInstance(b store.Browser, batch *browserBatchRun) {
	if !a.beginBrowserOperation(b, false, batch) {
		return
	}
	platform := strategy.Factory(b.Platform)
	if platform == nil {
		logging.Log.Errorf("无法识别浏览器平台: %s", b.Platform)
		a.finishBrowserOperation(b.ID, fmt.Errorf("无法识别浏览器平台: %s", b.Platform))
		return
	}
	if a.loginLaunch != nil {
		err := a.loginLaunch(b)
		if err != nil {
			logging.Log.Errorf("启动失败: %v", err)
		}
		a.finishBrowserOperation(b.ID, err)
		return
	}
	if a.manager == nil {
		err := errors.New("没有浏览器，无法启动环境")
		logging.Log.Errorf("启动失败: %v", err)
		a.finishBrowserOperation(b.ID, err)
		return
	}
	a.pushProxy()
	a.refreshDetected()
	fresh := !b.IsLoggedIn && a.loginIsFresh(b.ID)
	a.emit("browsers", "workbench")
	go func() {
		var operationErr error
		cleanupFresh := false
		defer func() {
			a.postUI(func() {
				if cleanupFresh && fresh {
					a.abandonUnlogged(b)
				}
				a.finishBrowserOperation(b.ID, operationErr)
			})
		}()
		ws, err := a.manager.Start(b, a.dataRoot, a.browserBackend, platform.HomeURL(), false)
		if err != nil && !errors.Is(err, browser.ErrDebugPort) {
			if isClosedBrowserError(err) {
				cleanupFresh = a.noteClosedLoginBrowser(b, fresh)
				return
			}
			logging.Log.Errorf("启动失败: %v", err)
			operationErr, cleanupFresh = err, true
			return
		}
		if errors.Is(err, browser.ErrDebugPort) {
			if a.store != nil {
				a.store.SetRunning(b.ID, true)
			}
			a.watchLogin(b, loginEndpoint(b))
			return
		}
		if a.store != nil {
			a.store.SetRunning(b.ID, true)
		}
		a.postUI(func() { a.refreshBrowsers() })
		err = cdp.OpenURLContext(context.Background(), ws, platform.HomeURL())
		if isClosedBrowserError(err) {
			if a.manager != nil {
				a.manager.StopLogin(b.ID)
			}
			cleanupFresh = a.noteClosedLoginBrowser(b, fresh)
			return
		}
		if err == nil || errors.Is(err, cdp.ErrDebugUnavailable) {
			a.watchLogin(b, ws)
		}
		if err != nil && !errors.Is(err, cdp.ErrDebugUnavailable) {
			operationErr = err
			logging.Log.Errorf("浏览器[%s] 打开登录页失败：%v", b.Name, err)
		} else if err != nil {
			logging.Log.Warning("浏览器[%s] 页面控制尚未就绪，已打开登录页", b.Name)
		} else {
			logging.Log.Success("浏览器[%s]已打开%s，可在浏览器中登录账号", b.Name, b.Platform)
		}
	}()
}

func (a *App) noteClosedLoginBrowser(b store.Browser, fresh bool) bool {
	if fresh {
		logging.Log.Info("浏览器[%s] 未登录，已从列表移除", b.Name)
		return true
	}
	if a.store != nil {
		a.store.SetRunning(b.ID, false)
	}
	logging.Log.Info("浏览器[%s] 窗口已关闭", b.Name)
	return false
}

func isClosedBrowserError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "target page, context or browser has been closed") ||
		strings.Contains(lower, "browser has been closed") ||
		strings.Contains(lower, "target closed") ||
		strings.Contains(lower, "browser driver closed") ||
		strings.Contains(lower, "账号窗口已关闭") ||
		strings.Contains(lower, "账号浏览器已关闭")
}

func (a *App) stopBrowserInstance(b store.Browser, batch *browserBatchRun) {
	if !a.beginBrowserOperation(b, true, batch) {
		return
	}
	a.emit("browsers", "workbench")
	if a.manager == nil || !a.manager.IsRunning(b.ID) {
		a.finishStopBrowser(b)
		return
	}
	go func() {
		a.manager.Stop(b.ID)
		a.postUI(func() { a.finishStopBrowser(b) })
	}()
}

func (a *App) finishStopBrowser(b store.Browser) {
	if a.store != nil {
		for _, row := range a.store.Browsers() {
			if row.ID == b.ID {
				b.IsLoggedIn = row.IsLoggedIn
				break
			}
		}
	}
	if !b.IsLoggedIn && a.loginIsFresh(b.ID) {
		a.abandonUnlogged(b)
		logging.Log.Info("浏览器[%s] 未登录，已从列表移除", b.Name)
		a.finishBrowserOperation(b.ID, nil)
		return
	}
	if a.store != nil {
		a.store.SetRunning(b.ID, false)
	}
	logging.Log.Info("浏览器[%s] 已停止", b.Name)
	a.finishBrowserOperation(b.ID, nil)
}

func (a *App) restoreBrowser() {
	b, ok := a.selectedBrowser()
	if !ok || a.store == nil {
		return
	}
	a.restoreBrowserByRow(b)
}

func (a *App) restoreBrowserByRow(b store.Browser) {
	if a.store == nil {
		return
	}
	a.store.ResetBrowserHealth(b.ID)
	running := b.IsRunning
	if a.manager != nil {
		running = a.manager.IsRunning(b.ID)
	}
	a.store.UpdateBrowser(b.ID, running, true)
	a.clearLoginHold(b.ID)
	if a.engine != nil {
		a.engine.Wake()
	}
	logging.Log.Info("已恢复账号 %s", b.Name)
	a.refreshBrowsers()
}

func (a *App) deleteBrowser() {
	b, ok := a.selectedBrowser()
	if !ok {
		return
	}
	a.deleteBrowserByRow(b)
}

func (a *App) deleteBrowserByRow(b store.Browser) {
	if a.browserDrawer != nil && a.browserDrawer.row.ID == b.ID {
		a.closeBrowserDrawer()
	}
	if a.manager != nil {
		a.manager.Stop(b.ID)
	}
	a.clearAutoName(b.ID)
	if a.store != nil {
		a.store.DeleteBrowser(b.ID)
	}
	if err := browser.RemoveProfile(a.dataRoot, a.browserBackend, b); err != nil {
		logging.Log.Warning("删除账号 %s 的配置目录失败：%v", accountLabel(b), err)
	}
	logging.Log.Info("浏览器[%s] 已删除", b.Name)
	a.browserSel = -1
	a.refreshBrowsers()
}

func (a *App) saveAccountProxy(id int64, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") || u.Host == "" {
			return fmt.Errorf("代理格式无效，请填 http://主机:端口 或 socks5://主机:端口")
		}
	}
	if a.store != nil {
		a.store.SetBrowserProxy(id, raw)
	}
	a.refreshBrowsers()
	return nil
}

func accountHealthLabel(b store.Browser, active, signedIn bool, now time.Time) string {
	if b.Disabled && !active {
		return "已禁用"
	}
	if !b.Usable(now) {
		if b.Health == store.HealthCooling {
			if strings.Contains(b.LastError, "验证") {
				return "需要人工验证"
			}
			return "冷却中"
		}
		if b.QuotaExhausted(now) {
			if b.PlatformLimited && b.CreditsRemain < 0 {
				return "冷却中"
			}
			return "已用尽"
		}
		if strings.Contains(b.LastError, "登录") {
			if signedIn {
				return accountReadyLabel(b, active)
			}
			return "未登录"
		}
		return "已用尽"
	}
	return accountReadyLabel(b, active)
}

func accountReadyLabel(b store.Browser, active bool) string {
	if active {
		return "使用中"
	}
	if b.IsRunning {
		return "运行中"
	}
	return "就绪"
}

func accountHomeURL(b store.Browser) string {
	p := strategy.Factory(b.Platform)
	if p == nil {
		return ""
	}
	return p.HomeURL()
}

func loginEndpoint(b store.Browser) string {
	return browser.AccountEndpoint(b)
}

// ---- Login tracking & drawer ----

func (a *App) markFreshLogin(id int64) {
	if a == nil || id <= 0 {
		return
	}
	a.mu.Lock()
	if a.loginFresh == nil {
		a.loginFresh = map[int64]struct{}{}
	}
	a.loginFresh[id] = struct{}{}
	a.mu.Unlock()
}

func (a *App) loginIsFresh(id int64) bool {
	if a == nil || id <= 0 {
		return false
	}
	a.mu.Lock()
	_, ok := a.loginFresh[id]
	a.mu.Unlock()
	return ok
}

func (a *App) markAutoName(id int64, placeholder string) {
	if a == nil || id <= 0 {
		return
	}
	a.mu.Lock()
	if a.loginAutoName == nil {
		a.loginAutoName = map[int64]string{}
	}
	a.loginAutoName[id] = strings.TrimSpace(placeholder)
	a.mu.Unlock()
}

func (a *App) clearAutoName(id int64) {
	if a == nil || id <= 0 {
		return
	}
	a.mu.Lock()
	delete(a.loginAutoName, id)
	a.mu.Unlock()
}

func (a *App) autoNamePlaceholder(id int64) (string, bool) {
	if a == nil || id <= 0 {
		return "", false
	}
	a.mu.Lock()
	placeholder, ok := a.loginAutoName[id]
	a.mu.Unlock()
	return placeholder, ok
}

func (a *App) needsAutoAccountName(id int64, fallback store.Browser) bool {
	if a == nil || id <= 0 {
		return false
	}
	if _, ok := a.autoNamePlaceholder(id); ok {
		return true
	}
	if a.store != nil {
		for _, row := range a.store.Browsers() {
			if row.ID == id {
				return !row.IsLoggedIn && isDefaultPlatformName(row.Platform, row.Name)
			}
		}
		return false
	}
	return !fallback.IsLoggedIn && isDefaultPlatformName(fallback.Platform, fallback.Name)
}

func (a *App) probeAccountUsername(ctx context.Context, endpoint string) (string, error) {
	if a == nil {
		return "", nil
	}
	if a.accountNameProbe != nil {
		return a.accountNameProbe(ctx, endpoint)
	}
	if a.wizardProbe != nil {
		return "", nil
	}
	return cdp.ProbeAccountUsername(ctx, endpoint)
}

func (a *App) waitAccountUsername(endpoint string, stale func() bool) string {
	if a == nil || endpoint == "" {
		return ""
	}
	if a.accountNameProbe == nil && a.wizardProbe != nil {
		return ""
	}
	a.mu.Lock()
	waitBudget := a.accountNameWait
	poll := a.accountNamePoll
	a.mu.Unlock()
	if waitBudget <= 0 {
		waitBudget = 4 * time.Second
	}
	if poll <= 0 {
		poll = 300 * time.Millisecond
	}
	deadline := time.Now().Add(waitBudget)
	for {
		if stale != nil && stale() {
			return ""
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		raw, err := a.probeAccountUsername(ctx, endpoint)
		cancel()
		if err == nil {
			if name := cdp.SanitizeAccountUsername(raw); name != "" {
				return name
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ""
		}
		time.Sleep(min(poll, remaining))
	}
}

func (a *App) applyAutoAccountName(id int64, rawName string) string {
	if a == nil || id <= 0 || a.store == nil {
		return ""
	}
	rows := a.store.Browsers()
	var target store.Browser
	found := false
	for _, r := range rows {
		if r.ID == id {
			target = r
			found = true
			break
		}
	}
	if !found {
		a.clearAutoName(id)
		return ""
	}
	if placeholder, tracked := a.autoNamePlaceholder(id); tracked {
		if strings.TrimSpace(target.Name) != placeholder {
			a.clearAutoName(id)
			return ""
		}
	} else if !isDefaultPlatformName(target.Platform, target.Name) {
		return ""
	}
	var samePlatform []string
	for _, r := range rows {
		if r.ID != id && r.Platform == target.Platform {
			samePlatform = append(samePlatform, r.Name)
		}
	}
	finalName := uniqueAccountName(rawName, samePlatform)
	if finalName == "" {
		return ""
	}
	a.store.RenameBrowser(id, finalName)
	for i := range a.browserRows {
		if a.browserRows[i].ID == id {
			a.browserRows[i].Name = finalName
		}
	}
	if a.wizardAccount.ID == id {
		a.wizardAccount.Name = finalName
		a.wizardName = finalName
	}
	a.clearAutoName(id)
	if finalName != target.Name {
		logging.Log.Info("账号[%s] 已自动识别用户名：%s", target.Name, finalName)
	}
	return finalName
}

func (a *App) clearLoginHold(id int64) {
	a.mu.Lock()
	delete(a.loginHold, id)
	a.mu.Unlock()
}

func (a *App) setLoginShot(id int64, state, hint string) {
	a.mu.Lock()
	if a.loginShots == nil {
		a.loginShots = map[int64]loginShot{}
	}
	a.loginShots[id] = loginShot{state: state, hint: hint}
	a.mu.Unlock()
}

func accountNeedsLogin(b store.Browser) bool {
	if !b.IsLoggedIn {
		return true
	}
	return (b.Health == store.HealthExhausted || b.Health == store.HealthCooling) &&
		strings.Contains(b.LastError, "登录")
}

func (a *App) acceptLogin(id int64) {
	a.confirmLogin(id, true)
}

func (a *App) confirmLogin(id int64, wake bool) {
	if a == nil || id <= 0 {
		return
	}
	a.mu.Lock()
	delete(a.loginFresh, id)
	if a.loginWatchGen != nil {
		a.loginWatchGen[id]++
	}
	a.mu.Unlock()
	if a.store != nil {
		a.store.ConfirmLogin(id)
	}
	a.setLoginShot(id, "in", "已登录")
	if wake && a.engine != nil {
		a.engine.NoteLoginConfirmed(id)
		a.engine.Wake()
	}
	a.refreshBrowsers()
}

func (a *App) abandonUnlogged(b store.Browser) {
	if a == nil || b.ID <= 0 || a.store == nil {
		return
	}
	a.mu.Lock()
	delete(a.loginFresh, b.ID)
	delete(a.loginAutoName, b.ID)
	a.mu.Unlock()
	if a.manager != nil {
		a.manager.Stop(b.ID)
	}
	a.store.DeleteBrowser(b.ID)
	_ = browser.RemoveProfile(a.dataRoot, a.browserBackend, b)
	a.refreshBrowsers()
}

// watchLogin 轮询打开的浏览器登录状态。
// 新建账号、未登录/登录失效账号，或页面持续处于未登录态并在扫码后完成登录的窗口会自动关闭；
// 手动打开且本就已登录的健康账号保持打开。
func (a *App) watchLogin(b store.Browser, endpoint string) {
	if a == nil || b.ID <= 0 || endpoint == "" {
		return
	}
	a.mu.Lock()
	if a.loginWatchGen == nil {
		a.loginWatchGen = map[int64]int{}
	}
	a.loginWatchGen[b.ID]++
	gen := a.loginWatchGen[b.ID]
	poll := a.loginPoll
	a.mu.Unlock()
	if poll <= 0 {
		poll = 2 * time.Second
	}
	needsLogin := accountNeedsLogin(b)
	go func() {
		outCount := 0
		for {
			time.Sleep(poll)
			a.mu.Lock()
			stale := a.quitting || a.loginWatchGen[b.ID] != gen
			a.mu.Unlock()
			if stale {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			state, err := a.probeWizardLogin(ctx, endpoint)
			cancel()
			if err != nil {
				continue
			}
			state = cdp.NormalizeLoginState(state)
			if state == "out" {
				outCount++
				continue
			}
			if state != "in" {
				continue
			}
			var scraped string
			if a.needsAutoAccountName(b.ID, b) {
				scraped = a.waitAccountUsername(endpoint, func() bool {
					a.mu.Lock()
					s := a.quitting || a.loginWatchGen[b.ID] != gen
					a.mu.Unlock()
					return s
				})
			}
			var qwenSt cdp.QwenStatus
			var doubaoSt cdp.DoubaoStatus
			var geminiSt cdp.GeminiStatus
			switch b.Platform {
			case "千问":
				if a.wizardProbe == nil || a.qwenStatusProbe != nil {
					qCtx, qCancel := context.WithTimeout(context.Background(), 6*time.Second)
					if st, qErr := a.probeQwenStatus(qCtx, endpoint); qErr == nil && st.OK {
						qwenSt = st
					}
					qCancel()
				}
			case "豆包":
				if a.wizardProbe == nil || a.doubaoStatusProbe != nil {
					dCtx, dCancel := context.WithTimeout(context.Background(), 6*time.Second)
					if st, dErr := a.probeDoubaoStatus(dCtx, endpoint); dErr == nil && st.OK {
						doubaoSt = st
					}
					dCancel()
				}
			case "Gemini":
				if a.wizardProbe == nil || a.geminiStatusProbe != nil {
					gCtx, gCancel := context.WithTimeout(context.Background(), 8*time.Second)
					if st, gErr := a.probeGeminiStatus(gCtx, endpoint); gErr == nil && st.OK {
						geminiSt = st
						if scraped == "" && a.needsAutoAccountName(b.ID, b) {
							if candidate := cdp.SanitizeAccountUsername(st.Username); candidate != "" {
								scraped = candidate
							} else if candidate := cdp.SanitizeAccountUsername(st.Email); candidate != "" {
								scraped = candidate
							}
						}
					}
					gCancel()
				}
			}
			var accepted, closeAfterLogin bool
			a.syncUI(func() {
				a.mu.Lock()
				stale := a.quitting || a.loginWatchGen[b.ID] != gen
				a.mu.Unlock()
				if stale {
					return
				}
				accepted = true
				if scraped != "" {
					if renamed := a.applyAutoAccountName(b.ID, scraped); renamed != "" {
						b.Name = renamed
					}
				}
				if qwenSt.OK {
					a.applyQwenStatus(b.ID, qwenSt)
				}
				if doubaoSt.OK {
					a.applyDoubaoStatus(b.ID, doubaoSt)
				}
				if geminiSt.OK {
					a.applyGeminiStatus(b.ID, geminiSt)
				}
				closeAfterLogin = a.loginIsFresh(b.ID) || needsLogin || outCount >= 2
				if closeAfterLogin {
					a.clearAutoName(b.ID)
				}
				a.confirmLogin(b.ID, !closeAfterLogin)
				if a.loginFollow != nil {
					a.loginFollow(b, endpoint)
				}
			})
			if !accepted {
				return
			}
			if closeAfterLogin {
				a.closeLoginWindow(b)
				if a.engine != nil {
					a.engine.NoteLoginConfirmed(b.ID)
					a.engine.Wake()
				}
			}
			return
		}
	}()
}

// ---- CDP Browser Drawer ----

func (a *App) openBrowserDrawer(row store.Browser) {
	home := accountHomeURL(row)
	if home == "" {
		return
	}
	previous := a.browserDrawer
	if previous != nil && previous.row.ID == row.ID && previous.err == "" {
		return
	}
	wasRunning := a.manager != nil && a.manager.IsRunning(row.ID)
	ctx, cancel := context.WithCancel(context.Background())
	s := &browserDrawerState{
		row:             row,
		startedByDrawer: !wasRunning,
		cancel:          cancel,
		done:            make(chan struct{}),
		inputs:          make(chan cdp.PageInput, 128),
		loading:         true,
	}
	a.browserDrawer = s
	a.mu.Lock()
	a.browserViewingID = row.ID
	if a.loginWatchGen == nil {
		a.loginWatchGen = map[int64]int{}
	}
	a.loginWatchGen[row.ID]++
	a.mu.Unlock()
	var detached <-chan struct{}
	if previous != nil {
		detached = previous.done
		previous.stop(a.engine)
	}
	a.emit("browserDrawer")
	root := a.dataRoot
	go func() {
		defer close(s.done)
		// Keep detach and input in order even if several accounts are selected rapidly.
		if detached != nil {
			<-detached
		}
		if ctx.Err() != nil {
			return
		}
		endpoint, err := a.startDrawerBrowser(ctx, row, root, home)
		if ctx.Err() != nil {
			if err == nil && s.startedByDrawer && a.manager != nil {
				a.manager.Stop(row.ID)
				a.postUI(func() { a.refreshBrowsers() })
			}
			return
		}
		if err != nil {
			a.browserDrawerUpdate(s, func() {
				s.loading = false
				s.err = "无法打开内置页面：" + err.Error()
			})
			return
		}
		// A reused account browser may no longer have its platform tab.
		connectCtx, stop := context.WithTimeout(ctx, 75*time.Second)
		err = cdp.OpenURLBackground(connectCtx, endpoint, home)
		var page *cdp.PageView
		if err == nil {
			page, err = cdp.AttachPageView(connectCtx, endpoint, home)
		}
		stop()
		if err != nil {
			a.browserDrawerUpdate(s, func() {
				s.loading = false
				s.err = "连接内置页面失败：" + err.Error()
			})
			return
		}
		defer page.Close()
		s.held.Store(true)
		a.runBrowserDrawer(ctx, s, page)
	}()
}

func (a *App) startDrawerBrowser(ctx context.Context, row store.Browser, root, home string) (string, error) {
	if a.manager == nil {
		return "", errors.New("没有浏览器管理器")
	}
	endpoint, err := a.manager.StartView(ctx, row, root, home)
	if err == nil {
		a.postUI(func() { a.refreshBrowsers() })
	}
	return endpoint, err
}

func (s *browserDrawerState) releaseManual(engine *tasks.Engine) {
	s.held.Store(false)
	if engine != nil {
		engine.Wake()
	}
}

func (s *browserDrawerState) stop(engine *tasks.Engine) {
	if s.cancel != nil {
		s.cancel()
	}
	s.releaseManual(engine)
}

func (a *App) closeBrowserDrawer() {
	s := a.browserDrawer
	if s == nil {
		return
	}
	a.browserDrawer = nil
	a.setDrawerFrame(nil)
	a.mu.Lock()
	if a.browserViewingID == s.row.ID {
		a.browserViewingID = 0
	}
	a.mu.Unlock()
	s.stop(a.engine)
	a.emit("browserDrawer")
}

// closeBrowserDrawerByUser closes the preview drawer after an explicit user action
// and stops any idle headless browser that has no visible OS window left.
func (a *App) closeBrowserDrawerByUser() {
	s := a.browserDrawer
	if s == nil {
		return
	}
	id := s.row.ID
	a.closeBrowserDrawer()
	if a.running || a.manager == nil || !a.manager.IsHeadless(id) {
		return
	}
	if a.engine != nil && a.engine.ActiveAccounts()[id] {
		return
	}
	target, ok := a.browserByID(id)
	if !ok {
		target = s.row
	}
	target.IsRunning = true
	a.stopBrowserInstance(target, nil)
}

func (a *App) browserDrawerUpdate(s *browserDrawerState, fn func()) {
	a.postUI(func() {
		if a.browserDrawer != s || fn == nil {
			return
		}
		fn()
		a.emit("browserDrawer")
	})
}

func (a *App) browserDrawerBusy(id int64) bool {
	if a == nil || a.browserPending == nil {
		return false
	}
	_, ok := a.browserPending[id]
	return ok
}

const (
	drawerFastInterval = 85 * time.Millisecond
	drawerIdleInterval = 250 * time.Millisecond
	drawerFastWindow   = 1500 * time.Millisecond
)

func (a *App) drawerFrameInterval(s *browserDrawerState) time.Duration {
	if s == nil {
		return drawerIdleInterval
	}
	if a != nil && a.engine != nil && a.engine.AccountAwaitingUser(s.row.ID) {
		return drawerFastInterval
	}
	if ts := s.lastInput.Load(); ts > 0 && time.Since(time.Unix(0, ts)) < drawerFastWindow {
		return drawerFastInterval
	}
	return drawerIdleInterval
}

func (a *App) runBrowserDrawer(ctx context.Context, s *browserDrawerState, page *cdp.PageView) {
	rowSnap := s.row
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	lastProbe := time.Time{}
	lastFrame := time.Time{}
	qwenStatusSynced := false
	resetTimer := func(d time.Duration) {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(d)
	}
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
			return
		case in := <-s.inputs:
			if in.Kind == "resume" {
				s.releaseManual(a.engine)
				a.browserDrawerUpdate(s, func() { s.resuming = false })
				continue
			}
			if ctx.Err() != nil || !s.held.Load() || a.browserDrawerBusy(rowSnap.ID) {
				continue
			}
			s.lastInput.Store(time.Now().UnixNano())
			opCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			text, err := page.Input(opCtx, in)
			stop()
			if err != nil {
				a.browserDrawerUpdate(s, func() { s.err = "网页操作失败，请重新连接：" + err.Error() })
				return
			}
			if (in.Kind == "copy" || in.Kind == "cut") && text != "" {
				a.browserDrawerUpdate(s, func() {
					a.copyText(text)
					s.copyText, s.copyReady = text, true
				})
			}
			if (in.Kind == "mousePressed" || in.Kind == "mouseReleased") && time.Since(lastFrame) >= 50*time.Millisecond {
				resetTimer(12 * time.Millisecond)
			}
		case <-timer.C:
			frameCtx, stop := context.WithTimeout(ctx, 20*time.Second)
			frame, err := page.Frame(frameCtx)
			if err != nil {
				stop()
				a.browserDrawerUpdate(s, func() { s.loading, s.err = false, "浏览器连接已断开，请重新连接："+err.Error() })
				return
			}
			if time.Since(lastProbe) >= 2*time.Second {
				lastProbe = time.Now()
				if state, err := page.LoginState(frameCtx); err == nil && state == "in" {
					if a.needsAutoAccountName(rowSnap.ID, rowSnap) {
						if name, err := page.AccountUsername(frameCtx); err == nil && name != "" {
							a.postUI(func() {
								if renamed := a.applyAutoAccountName(rowSnap.ID, name); renamed != "" {
									if a.browserDrawer == s {
										s.row.Name = renamed
										a.emit("browserDrawer")
									}
									a.refreshBrowsers()
								}
							})
						}
					}
					if !qwenStatusSynced {
						switch rowSnap.Platform {
						case "千问":
							if qSt, qErr := page.QwenStatus(frameCtx); qErr == nil && qSt.OK {
								qwenStatusSynced = true
								id := rowSnap.ID
								a.postUI(func() { a.applyQwenStatus(id, qSt) })
							}
						case "豆包":
							if dSt, dErr := page.DoubaoStatus(frameCtx); dErr == nil && dSt.OK {
								qwenStatusSynced = true
								id := rowSnap.ID
								a.postUI(func() { a.applyDoubaoStatus(id, dSt) })
							}
						case "Gemini":
							if gSt, gErr := page.GeminiStatus(frameCtx); gErr == nil && gSt.OK {
								qwenStatusSynced = true
								id := rowSnap.ID
								a.postUI(func() {
									a.applyGeminiStatus(id, gSt)
									if a.browserDrawer == s {
										if row, ok := a.browserByID(id); ok && row.Name != "" && s.row.Name != row.Name {
											s.row.Name = row.Name
											a.emit("browserDrawer")
										}
									}
								})
							}
						}
					}
					a.acceptLogin(rowSnap.ID)
				}
			}
			stop()
			lastFrame = time.Now()
			jpegBytes := append([]byte(nil), frame.JPEG...)
			a.browserDrawerUpdate(s, func() {
				if len(jpegBytes) > 0 {
					a.setDrawerFrame(jpegBytes)
					s.frameVersion++
				}
				s.width, s.height = frame.Width, frame.Height
				s.title, s.url, s.loading = frame.Title, frame.URL, false
			})
			resetTimer(a.drawerFrameInterval(s))
		}
	}
}

// BrowserInputDTO is a framework-free pointer/wheel/keyboard event sent from
// the frontend BrowserStage component to the active CDP PageView.
type BrowserInputDTO struct {
	Kind    string  `json:"kind"` // mousePressed | mouseReleased | mouseMoved | mouseWheel | keyDown | text | copy | cut
	Button  string  `json:"button,omitempty"`
	Buttons int64   `json:"buttons,omitempty"`
	X       float64 `json:"x,omitempty"`
	Y       float64 `json:"y,omitempty"`
	DX      float64 `json:"dx,omitempty"`
	DY      float64 `json:"dy,omitempty"`
	Clicks  int     `json:"clicks,omitempty"`
	Key     string  `json:"key,omitempty"`
	Text    string  `json:"text,omitempty"`
	Alt     bool    `json:"alt,omitempty"`
	Control bool    `json:"control,omitempty"`
	Meta    bool    `json:"meta,omitempty"`
	Shift   bool    `json:"shift,omitempty"`
}

// BrowserKeyInput is a framework-free keyboard input used by handleBrowserDrawerKey.
type BrowserKeyInput struct {
	Key     string
	Alt     bool
	Control bool
	Meta    bool
	Shift   bool
}

func (a *App) browserDrawerSend(s *browserDrawerState, in cdp.PageInput) bool {
	if s == nil || s.inputs == nil {
		return false
	}
	select {
	case s.inputs <- in:
		return true
	default:
		return false
	}
}

func cdpModifiersFromFlags(alt, control, meta, shift bool) int64 {
	var flags int64
	if alt {
		flags |= 1
	}
	if control {
		flags |= 2
	}
	if meta {
		flags |= 4
	}
	if shift {
		flags |= 8
	}
	return flags
}

func cdpButtonMask(btn string) int64 {
	switch btn {
	case "right":
		return 2
	case "middle":
		return 4
	default:
		return 1
	}
}

func (a *App) handleBrowserDrawerInput(in BrowserInputDTO) bool {
	s := a.browserDrawer
	if s == nil {
		return false
	}
	switch in.Kind {
	case "keyDown":
		return a.handleBrowserDrawerKey(BrowserKeyInput{
			Key:     in.Key,
			Alt:     in.Alt,
			Control: in.Control,
			Meta:    in.Meta,
			Shift:   in.Shift,
		})
	case "mousePressed", "mouseReleased", "mouseMoved", "mouseWheel":
		if !s.held.Load() {
			return false
		}
		btn := in.Button
		if btn == "" && (in.Kind == "mousePressed" || in.Kind == "mouseReleased") {
			btn = "left"
		}
		buttons := in.Buttons
		if in.Kind == "mousePressed" && buttons == 0 {
			buttons = cdpButtonMask(btn)
		}
		clicks := in.Clicks
		if clicks == 0 && (in.Kind == "mousePressed" || in.Kind == "mouseReleased") {
			clicks = 1
		}
		return a.browserDrawerSend(s, cdp.PageInput{
			Kind:      in.Kind,
			Button:    btn,
			Buttons:   buttons,
			X:         in.X,
			Y:         in.Y,
			DX:        in.DX,
			DY:        in.DY,
			Clicks:    clicks,
			Modifiers: cdpModifiersFromFlags(in.Alt, in.Control, in.Meta, in.Shift),
		})
	case "text":
		if !s.held.Load() || in.Text == "" {
			return false
		}
		return a.browserDrawerSend(s, cdp.PageInput{Kind: "text", Text: in.Text})
	case "copy", "cut":
		if !s.held.Load() {
			return false
		}
		return a.browserDrawerSend(s, cdp.PageInput{Kind: in.Kind})
	default:
		return false
	}
}

func (a *App) handleBrowserDrawerKey(ev BrowserKeyInput) bool {
	s := a.browserDrawer
	if s == nil {
		return false
	}
	keyName := strings.TrimSpace(ev.Key)
	if ev.Key == " " {
		keyName = " "
	}
	if strings.EqualFold(keyName, "escape") || strings.EqualFold(keyName, "esc") {
		a.closeBrowserDrawerByUser()
		return true
	}
	if !s.held.Load() || keyName == "" {
		return false
	}
	if (ev.Control || ev.Meta) && strings.EqualFold(keyName, "c") {
		return a.browserDrawerSend(s, cdp.PageInput{Kind: "copy"})
	}
	if (ev.Control || ev.Meta) && strings.EqualFold(keyName, "x") {
		return a.browserDrawerSend(s, cdp.PageInput{Kind: "cut"})
	}
	if len([]rune(keyName)) == 1 && !ev.Alt && !ev.Control && !ev.Meta {
		return a.browserDrawerSend(s, cdp.PageInput{Kind: "text", Text: keyName})
	}
	mods := cdpModifiersFromFlags(ev.Alt, ev.Control, ev.Meta, ev.Shift)
	cdpKey, code := mapKeyToCDP(keyName)
	if cdpKey != "" {
		a.browserDrawerSend(s, cdp.PageInput{Kind: "keyDown", Key: cdpKey, VirtualKey: code, Modifiers: mods})
		a.browserDrawerSend(s, cdp.PageInput{Kind: "keyUp", Key: cdpKey, VirtualKey: code, Modifiers: mods})
		return true
	}
	return false
}

func mapKeyToCDP(k string) (string, int) {
	switch strings.ToLower(k) {
	case "enter", "return":
		return "Enter", 13
	case "backspace":
		return "Backspace", 8
	case "tab":
		return "Tab", 9
	case "space", " ":
		return " ", 32
	case "left", "arrowleft":
		return "ArrowLeft", 37
	case "up", "arrowup":
		return "ArrowUp", 38
	case "right", "arrowright":
		return "ArrowRight", 39
	case "down", "arrowdown":
		return "ArrowDown", 40
	case "delete":
		return "Delete", 46
	case "home":
		return "Home", 36
	case "end":
		return "End", 35
	case "pageup":
		return "PageUp", 33
	case "pagedown":
		return "PageDown", 34
	default:
		return k, 0
	}
}

func (a *App) showAccountWindow(row store.Browser) {
	if a.accountOpen != nil {
		if err := a.accountOpen(row); err != nil {
			logging.Log.Errorf("%s 打开窗口失败: %v", accountLabel(row), err)
		}
		return
	}
	a.selectBrowserID(row.ID)
	a.launchBrowserInstance(row, nil)
}
