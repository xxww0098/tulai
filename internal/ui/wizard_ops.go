package ui

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/wizard"
)

const (
	pageLoginTitle  = "在打开的网页里登录"
	pageLoginHint   = "请在打开的网页里登录"
	regionProxyHint = "当前地区打不开这个站点。请到设置的「网络」里选择自定义代理，填写 http://主机:端口 或 socks5://主机:端口，然后重新打开登录页。"
)

type wizardAgyState struct {
	path    string
	found   bool
	copied  bool
	watch   bool
	created int64
}

func (a *App) applyWizardGate() {
	if a == nil || a.store == nil {
		return
	}
	if a.store.GetBool(wizard.KeyShown) {
		a.wizardOn = false
		return
	}
	if a.store.Get("gen_stage_picked") != "" {
		a.store.Set(wizard.KeyComplete, wizard.CompleteYes)
	}
	accounts := wizardAccounts(a.store)
	if !wizard.ShouldShow(a.store.Get(wizard.KeyComplete), accounts) {
		if a.store.Get(wizard.KeyComplete) != wizard.CompleteYes {
			a.store.Set(wizard.KeyComplete, wizard.CompleteYes)
		}
		a.wizardOn = false
		return
	}
	a.store.SetInt(wizard.KeyShown, 1)
	a.openWizard()
}

func (a *App) openWizard() {
	a.stopWizardLoop()
	if a.wizardAgy.created != 0 {
		a.abandonAgyAccount()
	}
	a.wizardOn = true
	a.wizardDir = 1
	a.wizardStep = wizard.StepPlatform
	a.wizardExtra = false
	a.wizardPhase = ""
	a.wizardNote = ""
	a.wizardErr = ""
	a.wizardAccount = store.Browser{}
	a.wizardName = ""
	a.wizardNameFor = 0
	a.wizardChoice = wizard.DefaultChoice(a.selectedPlatforms())
	complete := ""
	if a.store != nil {
		complete = a.store.Get(wizard.KeyComplete)
	}
	a.wizardFirst = wizard.ShouldShow(complete, wizardAccounts(a.store))
	a.wizardAgy = wizardAgyState{watch: a.wizardAgy.watch}
	a.emit("wizard")
}

func (a *App) refreshWizardPlan() {
	if a.wizardPlanFn != nil {
		a.wizardPlan = a.wizardPlanFn()
		return
	}
	_, ready := browser.InstalledRuntime(browser.ConfigDir(), a.browserBackend)
	a.wizardPlan = wizard.PlanAccountBrowser(a.browserBackend, ready, browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, a.browserBackend))
}

func wizardAccounts(st *store.Store) []wizard.Account {
	if st == nil {
		return nil
	}
	rows := st.Browsers()
	out := make([]wizard.Account, 0, len(rows))
	for _, row := range rows {
		out = append(out, wizard.Account{
			Platform:  row.Platform,
			Name:      row.Name,
			Engine:    row.Engine,
			LoggedIn:  row.IsLoggedIn,
			Extension: row.ExtAccount != "",
			Health:    row.Health,
			CoolUntil: row.CoolUntil,
		})
	}
	return out
}

func (a *App) wizardFlow() []wizard.Step {
	return wizard.Flow(a.wizardChoice)
}

func (a *App) goWizard(step wizard.Step) {
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

func (a *App) wizardStepIndex(step wizard.Step) int {
	for i, s := range a.wizardFlow() {
		if s == step {
			return i
		}
	}
	return 0
}

func (a *App) wizardApplyChoice() {
	if !wizard.Valid(a.wizardChoice) {
		return
	}
	if a.wizardFirst {
		a.platDoubao, a.platQianwen, a.platAntigravity, a.platChatGPT = false, false, false, false
	}
	switch a.wizardChoice {
	case "豆包":
		a.platDoubao = true
	case "千问":
		a.platQianwen = true
	case agy.Platform:
		a.platAntigravity = true
	case "ChatGPT":
		a.platChatGPT = true
	}
}

func stageIndex(platform string) int {
	switch platform {
	case "千问":
		return 1
	case agy.Platform:
		return 2
	case "ChatGPT":
		return 3
	default:
		return 0
	}
}

func (a *App) wizardLandOnChoice() {
	if !wizard.Valid(a.wizardChoice) {
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

func (a *App) locateAgy() (string, bool) {
	if a.agyLocate != nil {
		return a.agyLocate(a.agyBin)
	}
	return agy.Locate(a.agyBin)
}

func (a *App) checkAgyInstall() {
	p, ok := a.locateAgy()
	a.wizardAgy.path = p
	a.wizardAgy.found = ok
	if ok && strings.TrimSpace(a.agyBin) != p {
		a.agyBin = p
	}
}

func (a *App) enterInstallStep() {
	a.wizardAgy.copied = false
	a.checkAgyInstall()
	a.watchAgyInstall()
}

func (a *App) watchAgyInstall() {
	if !a.hasWindow() || a.wizardAgy.found || a.wizardAgy.watch {
		return
	}
	a.wizardAgy.watch = true
	go func() {
		defer a.wizardUI(func() { a.wizardAgy.watch = false })
		for range 900 {
			time.Sleep(2 * time.Second)
			stop := false
			a.wizardUI(func() {
				if !a.wizardOn || a.wizardStep != wizard.StepInstall {
					stop = true
					return
				}
				a.checkAgyInstall()
				stop = a.wizardAgy.found
				a.emit("wizard")
			})
			if stop {
				return
			}
		}
	}()
}

func (a *App) wizardNextFromPlatform() {
	if !wizard.Valid(a.wizardChoice) {
		a.wizardErr = "请先选择一个服务商"
		return
	}
	a.wizardErr = ""
	a.wizardApplyChoice()
	if a.wizardChoice == agy.Platform {
		a.enterInstallStep()
		if a.wizardAgy.found {
			a.wizardEnterAgyAccount(false)
			return
		}
		a.goWizard(wizard.StepInstall)
		return
	}
	a.refreshWizardPlan()
	if a.wizardPlan.CanNext {
		a.wizardEnterWebAccount(false)
		return
	}
	a.goWizard(wizard.StepBrowser)
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
		a.wizardQueue = wizard.NewQueue([]string{a.wizardChoice})
	}
	if a.wizardQueue.Done() {
		a.enterDoneStep()
		return
	}
	a.goWizard(wizard.StepAccount)
	a.wizardPhase = "launching"
	a.wizardNote = "正在打开登录页…"
	a.startWizardLoop()
}

func (a *App) wizardEnterAgyAccount(extra bool) {
	a.stopWizardLoop()
	a.wizardExtra = extra
	a.wizardErr = ""
	a.wizardAgy.created = 0
	if !extra && !a.platformNeedsLogin(agy.Platform) {
		a.enterDoneStep()
		return
	}
	a.closeAgyLogin()
	a.agyChooser = newAgyChooser()
	a.goWizard(wizard.StepAccount)
}

func (a *App) closeAgyLogin() {
	if a.agyLogin != nil && a.agyLogin.cancel != nil {
		a.agyLogin.cancel()
	}
	if a.agyLogin != nil && a.agyLogin.sess != nil {
		a.agyLogin.sess.Close()
	}
	if a.agyLogin != nil && a.manager != nil {
		a.manager.Stop(a.agyLogin.id)
	}
	a.agyLogin = nil
}

func (a *App) agyAccountAdded() {
	if !a.wizardOn || a.wizardStep != wizard.StepAccount || a.wizardChoice != agy.Platform {
		return
	}
	a.wizardAgy.created = 0
	a.enterDoneStep()
}

func (a *App) abandonAgyAccount() {
	if a.wizardAgy.created == 0 || a.store == nil {
		return
	}
	id := a.wizardAgy.created
	a.wizardAgy.created = 0
	for _, r := range a.store.Browsers() {
		if r.ID == id && !r.IsLoggedIn {
			a.store.DeleteBrowser(id)
			break
		}
	}
}

func (a *App) platformNeedsLogin(platform string) bool {
	now := time.Now()
	for _, account := range wizardAccounts(a.store) {
		if account.Platform == platform && !account.Extension && browser.CanonicalBackend(account.Engine) == browser.CanonicalBackend(a.browserBackend) && wizard.KeepsClosed(account, now) {
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
	a.persistAgySettings()
}

func (a *App) wizardFinish() {
	a.stopWizardLoop()
	a.wizardCommitChoices()
	if a.store != nil {
		a.store.Set(wizard.KeyComplete, wizard.CompleteYes)
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
			ws = fmt.Sprintf("http://127.0.0.1:%d", account.Port)
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
			action := wizard.PollWait
			a.wizardUI(func() {
				a.setLoginShot(account.ID, raw, "")
				action = a.noteProbe(gen, raw, probeErr)
				a.emit("wizard")
			})
			if action == wizard.PollNext || action == wizard.PollDone {
				a.closeLoginWindow(account)
			}
			switch action {
			case wizard.PollNext:
				goto nextPlatform
			case wizard.PollDone, wizard.PollStop:
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

func (a *App) closeLoginWindow(account store.Browser) {
	if a.manager != nil {
		a.manager.Stop(account.ID)
	}
	if a.store != nil {
		a.store.SetRunning(account.ID, false)
	}
	a.loginClosed = append(a.loginClosed, account.ID)
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
			if row.Platform != platform || row.ExtAccount != "" || browser.CanonicalBackend(row.Engine) != browser.CanonicalBackend(a.browserBackend) {
				continue
			}
			snap := wizard.Account{
				Platform: row.Platform, Name: row.Name, LoggedIn: row.IsLoggedIn,
				Health: row.Health, CoolUntil: row.CoolUntil,
			}
			if !wizard.KeepsClosed(snap, now) {
				return row, nil
			}
		}
	}
	var names []string
	for _, row := range a.store.Browsers() {
		names = append(names, row.Name)
	}
	name := wizard.NextInstanceName(platform, names)
	dir := ""
	kind := browser.CanonicalBackend(a.browserBackend)
	if strings.TrimSpace(a.dataRoot) != "" {
		dir = browser.ProfileDir(a.dataRoot, kind, "account-"+rand.Text())
	}
	row, err := a.store.AddManagedBrowser(platform, name, dir, kind)
	if err == nil {
		a.markFreshLogin(row.ID)
	}
	return row, err
}

func (a *App) noteProbe(gen int, raw string, err error) wizard.Poll {
	if gen != a.wizardGen {
		return wizard.PollStop
	}
	if strings.TrimSpace(raw) == wizard.ProbeRegion {
		a.wizardPhase = "region"
		a.wizardErr = regionProxyHint
		a.wizardNote = "需要设置代理"
		a.wizardNavFailed = true
		return wizard.PollWait
	}
	if err != nil || !wizard.LoggedIn(raw) {
		a.wizardPhase = "waiting"
		a.wizardNote = pageLoginTitle
		a.wizardErr = ""
		if err != nil {
			a.wizardPhase = "error"
			a.wizardErr = "还没检测到登录，可以在浏览器里完成后再点重新检测"
		}
		return wizard.PollWait
	}
	if a.store != nil && a.wizardAccount.ID != 0 {
		a.acceptLogin(a.wizardAccount.ID)
	}
	a.wizardPhase = "in"
	a.wizardNote = "已登录"
	a.wizardErr = ""
	next, poll := wizard.AfterLogin(a.wizardQueue, true)
	a.wizardQueue = next
	if poll == wizard.PollNext {
		a.wizardAccount = store.Browser{}
		a.wizardPhase = "launching"
		a.wizardErr = ""
	}
	if poll == wizard.PollDone {
		a.enterDoneStep()
	}
	return poll
}

func (a *App) enterDoneStep() {
	a.wizardPhase = "ready"
	a.wizardNote = ""
	a.wizardErr = ""
	a.wizardFirst = false
	a.goWizard(wizard.StepDone)
}
