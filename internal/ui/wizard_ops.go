package ui

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

const (
	pageLoginTitle  = "在打开的网页里登录"
	pageLoginHint   = "请在打开的网页里登录"
	regionProxyHint = "当前地区打不开这个站点。请到设置的「网络」里选择自定义代理，填写 http://主机:端口 或 socks5://主机:端口，然后重新打开登录页。"
)

func (a *App) applyWizardGate() {
	if a == nil || a.store == nil {
		return
	}
	if a.store.GetBool(wizardKeyShown) {
		a.wizardOn = false
		return
	}
	if a.store.Get("gen_stage_picked") != "" {
		a.store.Set(WizardKeyComplete, WizardCompleteYes)
	}
	accounts := wizardAccounts(a.store)
	if !wizardShouldShow(a.store.Get(WizardKeyComplete), accounts) {
		if a.store.Get(WizardKeyComplete) != WizardCompleteYes {
			a.store.Set(WizardKeyComplete, WizardCompleteYes)
		}
		a.wizardOn = false
		return
	}
	a.store.SetInt(wizardKeyShown, 1)
	a.openWizard()
}

func (a *App) openWizard() {
	a.stopWizardLoop()
	a.wizardOn = true
	a.wizardDir = 1
	a.wizardStep = wizardStepPlatform
	a.wizardExtra = false
	a.wizardPhase = ""
	a.wizardNote = ""
	a.wizardErr = ""
	a.wizardAccount = store.Browser{}
	a.wizardName = ""
	a.wizardNameFor = 0
	a.wizardChoice = wizardDefaultChoice(a.selectedPlatforms())
	complete := ""
	if a.store != nil {
		complete = a.store.Get(WizardKeyComplete)
	}
	a.wizardFirst = wizardShouldShow(complete, wizardAccounts(a.store))
	a.emit("wizard")
}

func (a *App) refreshWizardPlan() {
	if a.wizardPlanFn != nil {
		a.wizardPlan = a.wizardPlanFn()
		return
	}
	_, ready := browser.InstalledRuntime(browser.ConfigDir(), a.browserBackend)
	a.wizardPlan = planAccountBrowser(a.browserBackend, ready, browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, a.browserBackend))
}

func wizardAccounts(st *store.Store) []wizardAccount {
	if st == nil {
		return nil
	}
	rows := st.Browsers()
	out := make([]wizardAccount, 0, len(rows))
	for _, row := range rows {
		out = append(out, wizardAccount{
			Platform:  row.Platform,
			Name:      row.Name,
			Engine:    row.Engine,
			LoggedIn:  row.IsLoggedIn,
			Health:    row.Health,
			CoolUntil: row.CoolUntil,
		})
	}
	return out
}

func (a *App) wizardFlow() []wizardStep {
	return wizardSteps()
}

func (a *App) goWizard(step wizardStep) {
	cur := a.wizardStepIndex(a.wizardStep)
	next := a.wizardStepIndex(step)
	if next >= cur {
		a.wizardDir = 1
	} else {
		a.wizardDir = -1
	}
	a.wizardStep = step
	a.emit("wizard")
}

func (a *App) wizardStepIndex(step wizardStep) int {
	for i, s := range a.wizardFlow() {
		if s == step {
			return i
		}
	}
	return 0
}

func (a *App) wizardApplyChoice() {
	if !wizardValidPlatform(a.wizardChoice) {
		return
	}
	if a.wizardFirst {
		a.platDoubao, a.platQianwen, a.platQwenIntl, a.platGemini, a.platJimeng = false, false, false, false, false
	}
	switch a.wizardChoice {
	case "豆包":
		a.platDoubao = true
	case "千问":
		a.platQianwen = true
	case "千问国际":
		a.platQwenIntl = true
	case "Gemini":
		a.platGemini = true
	case "即梦":
		a.platJimeng = true
	}
}

func stageIndex(platform string) int {
	switch platform {
	case "千问":
		return 1
	case "千问国际":
		return 2
	case "Gemini":
		return 3
	case "即梦":
		return 4
	default:
		return 0
	}
}

func (a *App) wizardLandOnChoice() {
	if !wizardValidPlatform(a.wizardChoice) {
		return
	}
	idx := stageIndex(a.wizardChoice)
	for _, page := range []int{pageHome, pageWash, pageFission} {
		a.onPage(page, func() {
			a.stageTab = idx
			a.stagePicked = true
		})
	}
}

func (a *App) wizardNextFromPlatform() {
	if !wizardValidPlatform(a.wizardChoice) {
		a.wizardErr = "请先选择一个服务商"
		return
	}
	a.wizardErr = ""
	a.wizardApplyChoice()
	a.refreshWizardPlan()
	if a.wizardPlan.CanNext {
		a.wizardEnterWebAccount(false)
		return
	}
	a.goWizard(wizardStepBrowser)
}

func (a *App) wizardEnterWebAccount(extra bool) {
	a.stopWizardLoop()
	a.wizardExtra = extra
	a.wizardErr = ""
	if !extra && !a.platformNeedsLogin(a.wizardChoice) {
		a.enterDoneStep()
		return
	}
	if extra || a.platformNeedsLogin(a.wizardChoice) {
		a.wizardQueue = newWizardQueue([]string{a.wizardChoice})
	}
	if a.wizardQueue.Done() {
		a.enterDoneStep()
		return
	}
	a.goWizard(wizardStepAccount)
	a.wizardPhase = "launching"
	a.wizardNote = "正在打开登录页…"
	a.startWizardLoop()
}

func (a *App) platformNeedsLogin(platform string) bool {
	now := time.Now()
	for _, account := range wizardAccounts(a.store) {
		if account.Platform == platform && browser.CanonicalBackend(account.Engine) == browser.CanonicalBackend(a.browserBackend) && wizardKeepsClosed(account, now) {
			return false
		}
	}
	return true
}

func (a *App) wizardCommitChoices() {
	if a.store == nil {
		return
	}
	a.store.SetPlatforms(a.selectedPlatforms())
}

func (a *App) wizardFinish() {
	a.stopWizardLoop()
	a.wizardCommitChoices()
	if a.store != nil {
		a.store.Set(WizardKeyComplete, WizardCompleteYes)
		a.wizardLandOnChoice()
		a.saveSettings()
	}
	a.wizardOn = false
	a.refreshBrowsers()
	a.emit("wizard", "shell", "workbench")
}

func (a *App) wizardRecheck() {
	if a.wizardPhase == "launching" {
		return
	}
	a.startWizardLoop()
}

func (a *App) startWizardLoop() {
	a.stopWizardLoop()
	gen := a.wizardGen
	if !a.hasWindow() {
		a.wizardLoop(gen, nil)
		return
	}
	stop := make(chan struct{})
	a.wizardStop = stop
	go a.wizardLoop(gen, stop)
}

func (a *App) stopWizardLoop() {
	a.wizardGen++
	if a.wizardStop != nil {
		close(a.wizardStop)
		a.wizardStop = nil
	}
}

func (a *App) wizardUI(fn func()) {
	if fn == nil {
		return
	}
	a.syncUI(fn)
}

func (a *App) wizardLoop(gen int, stop <-chan struct{}) {
	for {
		if !a.wizardPrepare(gen) {
			return
		}
		var account store.Browser
		a.wizardUI(func() { account = a.wizardAccount })
		ws, err := a.launchWizardBrowser(account)
		if err != nil && !errors.Is(err, browser.ErrDebugPort) {
			a.wizardUI(func() {
				if gen != a.wizardGen {
					return
				}
				a.wizardPhase = "error"
				a.wizardNavFailed = false
				a.wizardErr = "启动浏览器失败：" + err.Error()
				a.wizardNote = pageLoginTitle
				a.emit("wizard")
			})
			return
		}
		debugDown := errors.Is(err, browser.ErrDebugPort)
		if debugDown {
			ws = loginEndpoint(account)
		}
		page := accountHomeURL(account)
		if page == "" {
			page = "about:blank"
		}
		if !debugDown {
			if navErr := a.openWizardPage(ws, page); navErr != nil && !errors.Is(navErr, cdp.ErrDebugUnavailable) {
				msg := account.Name + " 打开登录页失败：" + navErr.Error()
				logging.Log.Errorf("%s", msg)
				a.wizardUI(func() {
					if gen != a.wizardGen {
						return
					}
					a.wizardPhase = "error"
					a.wizardNavFailed = true
					a.wizardErr = msg
					a.wizardNote = "请点「重新打开」"
					a.emit("wizard")
				})
				return
			}
		}
		a.wizardUI(func() {
			if gen != a.wizardGen || a.store == nil {
				return
			}
			a.store.SetRunning(account.ID, true)
			a.wizardPhase = "waiting"
			a.wizardNote = pageLoginTitle
			a.wizardErr = ""
			a.emit("wizard")
		})
		for {
			if a.wizardStale(gen, stop) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			raw, probeErr := a.probeWizardLogin(ctx, ws)
			cancel()
			var scraped string
			var qwenSt cdp.QwenStatus
			var doubaoSt cdp.DoubaoStatus
			var geminiSt cdp.GeminiStatus
			var jimengSt cdp.JimengStatus
			if probeErr == nil && wizardLoggedIn(raw) {
				if a.needsAutoAccountName(account.ID, account) {
					scraped = a.waitAccountUsername(ws, func() bool {
						return a.wizardStale(gen, stop)
					})
				}
				if account.Platform == "千问" && (a.wizardProbe == nil || a.qwenStatusProbe != nil) {
					qCtx, qCancel := context.WithTimeout(context.Background(), 6*time.Second)
					if st, qErr := a.probeQwenStatus(qCtx, ws); qErr == nil && st.OK {
						qwenSt = st
					}
					qCancel()
				} else if account.Platform == "豆包" && (a.wizardProbe == nil || a.doubaoStatusProbe != nil) {
					dCtx, dCancel := context.WithTimeout(context.Background(), 6*time.Second)
					if st, dErr := a.probeDoubaoStatus(dCtx, ws); dErr == nil && st.OK {
						doubaoSt = st
					}
					dCancel()
				} else if account.Platform == "Gemini" && (a.wizardProbe == nil || a.geminiStatusProbe != nil) {
					gCtx, gCancel := context.WithTimeout(context.Background(), 8*time.Second)
					if st, gErr := a.probeGeminiStatus(gCtx, ws); gErr == nil && st.OK {
						geminiSt = st
					}
					gCancel()
				} else if account.Platform == "即梦" && (a.wizardProbe == nil || a.jimengStatusProbe != nil) {
					jCtx, jCancel := context.WithTimeout(context.Background(), 8*time.Second)
					if st, jErr := a.probeJimengStatus(jCtx, ws); jErr == nil && st.OK {
						jimengSt = st
						if scraped == "" && a.needsAutoAccountName(account.ID, account) {
							if candidate := cdp.SanitizeAccountUsername(st.Username); candidate != "" {
								scraped = candidate
							}
						}
					}
					jCancel()
				}
			}
			action := wizardPollWait
			a.wizardUI(func() {
				if scraped != "" {
					if renamed := a.applyAutoAccountName(account.ID, scraped); renamed != "" {
						account.Name = renamed
					}
				}
				if qwenSt.OK {
					a.applyQwenStatus(account.ID, qwenSt)
				}
				if doubaoSt.OK {
					a.applyDoubaoStatus(account.ID, doubaoSt)
				}
				if geminiSt.OK {
					a.applyGeminiStatus(account.ID, geminiSt)
				}
				if jimengSt.OK {
					a.applyJimengStatus(account.ID, jimengSt)
				}
				a.setLoginShot(account.ID, raw, "")
				action = a.noteProbe(gen, raw, probeErr)
				if action == wizardPollNext || action == wizardPollDone {
					a.clearAutoName(account.ID)
				}
				a.emit("wizard")
			})
			if action == wizardPollNext || action == wizardPollDone {
				a.closeLoginWindow(account)
			}
			switch action {
			case wizardPollNext:
				goto nextPlatform
			case wizardPollDone, wizardPollStop:
				return
			}
			if stop == nil {
				return
			}
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-stop:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	nextPlatform:
	}
}

func (a *App) wizardStale(gen int, stop <-chan struct{}) bool {
	if stop != nil {
		select {
		case <-stop:
			return true
		default:
		}
	}
	stale := false
	a.wizardUI(func() { stale = gen != a.wizardGen })
	return stale
}

func (a *App) wizardPrepare(gen int) bool {
	ok := false
	a.wizardUI(func() {
		if gen != a.wizardGen {
			return
		}
		if a.wizardQueue.Done() {
			a.enterDoneStep()
			return
		}
		account, err := a.ensureWizardAccount()
		if err != nil {
			a.wizardPhase = "error"
			a.wizardErr = err.Error()
			a.wizardNote = pageLoginTitle
			return
		}
		a.wizardAccount = account
		a.wizardPhase = "launching"
		a.pushProxy()
		a.wizardNote = "正在打开" + account.Platform + "…"
		a.wizardErr = ""
		a.wizardNavFailed = false
		ok = true
		a.emit("wizard")
	})
	return ok
}

func (a *App) launchWizardBrowser(account store.Browser) (string, error) {
	if a.wizardLaunch != nil {
		return a.wizardLaunch(account)
	}
	if a.manager == nil {
		return "", errors.New("没有浏览器管理器")
	}
	return a.manager.Start(account, a.dataRoot, a.browserBackend, accountHomeURL(account), false)
}

func (a *App) openWizardPage(ws, rawURL string) error {
	if a.wizardOpen != nil {
		return a.wizardOpen(ws, rawURL)
	}
	return cdp.OpenURLContext(context.Background(), ws, rawURL)
}

func (a *App) probeWizardLogin(ctx context.Context, endpoint string) (string, error) {
	if a.wizardProbe != nil {
		return a.wizardProbe(ctx, endpoint)
	}
	return cdp.ProbeLogin(ctx, endpoint)
}

// closeLoginWindow 登录成功后关闭这次打开的可见登录窗口。
// 若用户正在内置浏览器抽屉查看、或任务已接管/切入后台运行，则保留进程。
func (a *App) closeLoginWindow(account store.Browser) {
	if a == nil || account.ID <= 0 {
		return
	}
	a.mu.Lock()
	viewing := a.browserViewingID == account.ID
	a.mu.Unlock()
	if viewing {
		return
	}
	if a.engine != nil && a.engine.ActiveAccounts()[account.ID] {
		return
	}
	if a.manager != nil && !a.manager.StopLogin(account.ID) {
		return
	}
	a.mu.Lock()
	a.loginClosed = append(a.loginClosed, account.ID)
	a.mu.Unlock()
	cdp.ReleaseLoginFront(loginEndpoint(account))
	if a.store != nil {
		a.store.SetRunning(account.ID, false)
	}
	if name := strings.TrimSpace(account.Name); name != "" {
		logging.Log.Success("浏览器[%s] 已登录，窗口已关闭", name)
	}
	a.postUI(func() { a.refreshBrowsers() })
	if a.afterClose != nil {
		a.afterClose()
	}
}

func (a *App) ensureWizardAccount() (store.Browser, error) {
	if a.store == nil {
		return store.Browser{}, fmt.Errorf("没有配置库，无法添加账号")
	}
	platform := a.wizardQueue.Current()
	if platform == "" {
		return store.Browser{}, fmt.Errorf("没有要登录的平台")
	}
	if a.wizardAccount.ID != 0 && a.wizardAccount.Platform == platform {
		for _, row := range a.store.Browsers() {
			if row.ID == a.wizardAccount.ID {
				return row, nil
			}
		}
	}
	if !a.wizardExtra {
		now := time.Now()
		for _, row := range a.store.Browsers() {
			if row.Platform != platform || browser.CanonicalBackend(row.Engine) != browser.CanonicalBackend(a.browserBackend) {
				continue
			}
			snap := wizardAccount{
				Platform: row.Platform, Name: row.Name, LoggedIn: row.IsLoggedIn,
				Health: row.Health, CoolUntil: row.CoolUntil,
			}
			if !wizardKeepsClosed(snap, now) {
				return row, nil
			}
		}
	}
	var names []string
	for _, row := range a.store.Browsers() {
		names = append(names, row.Name)
	}
	name := nextInstanceName(platform, names)
	dir := ""
	kind := browser.CanonicalBackend(a.browserBackend)
	if strings.TrimSpace(a.dataRoot) != "" {
		dir = browser.ProfileDir(a.dataRoot, kind, "account-"+rand.Text())
	}
	row, err := a.store.AddManagedBrowser(platform, name, dir, kind)
	if err == nil {
		a.markFreshLogin(row.ID)
		a.markAutoName(row.ID, row.Name)
	}
	return row, err
}

func (a *App) noteProbe(gen int, raw string, err error) wizardPoll {
	if gen != a.wizardGen {
		return wizardPollStop
	}
	if strings.TrimSpace(raw) == wizardProbeRegion {
		a.wizardPhase = "region"
		a.wizardErr = regionProxyHint
		a.wizardNote = "需要设置代理"
		a.wizardNavFailed = true
		return wizardPollWait
	}
	if err != nil || !wizardLoggedIn(raw) {
		a.wizardPhase = "waiting"
		a.wizardNote = pageLoginTitle
		a.wizardErr = ""
		if err != nil {
			a.wizardPhase = "error"
			a.wizardErr = "还没检测到登录，可以在浏览器里完成后再点重新检测"
		}
		return wizardPollWait
	}
	if a.store != nil && a.wizardAccount.ID != 0 {
		a.acceptLogin(a.wizardAccount.ID)
	}
	a.wizardPhase = "in"
	a.wizardNote = "已登录"
	a.wizardErr = ""
	next, poll := wizardAfterLogin(a.wizardQueue, true)
	a.wizardQueue = next
	if poll == wizardPollNext {
		a.wizardAccount = store.Browser{}
		a.wizardPhase = "launching"
		a.wizardErr = ""
	}
	if poll == wizardPollDone {
		a.enterDoneStep()
	}
	return poll
}

func (a *App) enterDoneStep() {
	a.wizardPhase = "ready"
	a.wizardNote = ""
	a.wizardErr = ""
	a.wizardFirst = false
	a.goWizard(wizardStepDone)
}
