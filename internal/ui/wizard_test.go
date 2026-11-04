package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestWizardWalkthroughCreatesAccountsAndPersists(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{
		store: st, manager: browser.NewManager(), engine: tasks.NewEngine(st, nil, nil, ""),
		dataRoot: t.TempDir(), splitSz: 380, sourceHeight: 210,
	}
	a.loadSettings()
	a.applyWizardGate()
	if !a.wizardOn || a.wizardStep != wizard.StepPlatform {
		t.Fatal("a new config should open on the platform step")
	}
	a.wizardPlanFn = func() wizard.BrowserPlan {
		return wizard.BrowserPlan{
			Backend: browser.KindCamoufox, Kind: browser.KindCamoufox,
			Title: "Camoufox 已就绪", Detail: "/camoufox", CanNext: true,
		}
	}
	a.refreshWizardPlan()
	var probes int
	a.wizardLaunch = func(b store.Browser) (string, error) {
		if b.Name == "" || b.Platform == "" {
			t.Fatalf("launch without a profile: %+v", b)
		}
		return "http://127.0.0.1:9", nil
	}
	a.wizardOpen = func(ws, rawURL string) error {
		if ws == "" || (rawURL != "https://www.doubao.com/chat/" && rawURL != "https://www.tongyi.com/") {
			t.Fatalf("open ws=%q url=%q", ws, rawURL)
		}
		return nil
	}
	a.wizardProbe = func(context.Context, string) (string, error) {
		probes++
		if probes == 1 {
			return wizard.ProbeOut, nil
		}
		return wizard.ProbeIn, nil
	}

	tt := ui.NewTester(a.view, 1280, 900)
	for _, label := range []string{"tulai", "选择平台", "豆包", "千问", "Antigravity", "下一步"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	doubao := assertVisible(t, tt, "豆包", 1280, 900)
	qianwen := assertVisible(t, tt, "千问", 1280, 900)
	agyCard := assertVisible(t, tt, "Antigravity", 1280, 900)
	if qianwen.X <= doubao.X+40 || agyCard.X <= qianwen.X+40 {
		t.Fatalf("platform cards are stacked or overlapping: doubao=%+v qianwen=%+v agy=%+v", doubao, qianwen, agyCard)
	}
	next := assertVisible(t, tt, "下一步", 1280, 900)
	if next.Y < doubao.Y+80 {
		t.Fatalf("next crowds the platform cards: card=%+v next=%+v", doubao, next)
	}
	if a.wizardChoice != "豆包" {
		t.Fatalf("a new config should start on doubao, got %q", a.wizardChoice)
	}
	// 三选一：点千问之后豆包就不再选中。
	if err := tt.Click("千问"); err != nil {
		t.Fatal(err)
	}
	if a.wizardChoice != "千问" {
		t.Fatalf("choice = %q", a.wizardChoice)
	}
	if err := tt.Click("豆包"); err != nil {
		t.Fatal(err)
	}
	if a.wizardChoice != "豆包" {
		t.Fatalf("choice = %q", a.wizardChoice)
	}
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	if a.wizardStep != wizard.StepBrowser {
		t.Fatalf("doubao should go to the browser step, got %v", a.wizardStep)
	}
	for _, label := range []string{"准备浏览器", "Camoufox 已就绪"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"在打开的网页里登录", "已登录? 重新检测", "豆包 · 豆包1", "账号名称"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if a.wizardStep != wizard.StepAccount || probes != 1 {
		t.Fatalf("step=%v probes=%d", a.wizardStep, probes)
	}
	if err := tt.Click("已登录? 重新检测"); err != nil {
		t.Fatal(err)
	}
	if a.wizardStep != wizard.StepDone || probes < 2 {
		t.Fatalf("after login step=%v probes=%d", a.wizardStep, probes)
	}
	for _, label := range []string{"可以开始了", "拖入图片", "选提示词", "开始", "进入主页", "再添加一个账号"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if !a.platDoubao || a.platQianwen || a.platAntigravity {
		t.Fatalf("only the chosen platform is selected: doubao=%v qianwen=%v agy=%v", a.platDoubao, a.platQianwen, a.platAntigravity)
	}
	if err := tt.Click("再添加一个账号"); err != nil {
		t.Fatal(err)
	}
	if a.wizardStep != wizard.StepDone {
		t.Fatalf("extra account did not finish login, step=%v phase=%s err=%s", a.wizardStep, a.wizardPhase, a.wizardErr)
	}
	rows := st.Browsers()
	if len(rows) != 2 || rows[0].Name != "豆包1" || rows[0].Platform != "豆包" || !rows[0].IsLoggedIn || rows[0].IsRunning || rows[1].Name != "豆包2" || rows[1].Platform != "豆包" || !rows[1].IsLoggedIn || rows[1].IsRunning {
		t.Fatalf("accounts: %+v", rows)
	}
	if len(a.loginClosed) < 2 {
		t.Fatalf("login windows left open: %v", a.loginClosed)
	}
	if st.Get("browser_backend") != browser.KindCamoufox {
		t.Fatalf("backend = %q", st.Get("browser_backend"))
	}
	if err := tt.Click("进入主页"); err != nil {
		t.Fatal(err)
	}
	if a.wizardOn || st.Get(wizard.KeyComplete) != wizard.CompleteYes {
		t.Fatal("finish did not leave the wizard or save the flag")
	}
	assertVisible(t, tt, "开始出图", 1280, 900)
	assertVisible(t, tt, "设置向导", 1280, 900)
}

func TestWizardShownOnlyOnFirstLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	first := &App{store: st}
	first.loadSettings()
	first.applyWizardGate()
	if !first.wizardOn || !st.GetBool(wizard.KeyShown) || st.Get(wizard.KeyComplete) == wizard.CompleteYes {
		t.Fatal("first launch should show and record the wizard without marking it complete")
	}
	first.saveSettings()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	second := &App{store: st, splitSz: 380, sourceHeight: 210}
	second.loadSettings()
	second.applyWizardGate()
	if second.wizardOn || st.Get(wizard.KeyComplete) == wizard.CompleteYes {
		t.Fatal("an interrupted first setup should not reopen or be marked complete")
	}
	tt := ui.NewTester(second.view, 1280, 900)
	if err := tt.Click("设置向导"); err != nil {
		t.Fatal(err)
	}
	if !second.wizardOn || !second.wizardFirst {
		t.Fatal("the user should still be able to finish the first setup manually")
	}
}

func TestWizardSkipsReturningUsersWithoutUsableAccounts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		complete bool
		account  bool
		saved    bool
	}{
		{name: "completed and account removed", complete: true},
		{name: "completed and account logged out", complete: true, account: true},
		{name: "legacy account logged out", account: true},
		{name: "legacy saved workspace without accounts", saved: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			if tc.complete {
				st.Set(wizard.KeyComplete, wizard.CompleteYes)
			}
			if tc.saved {
				st.Set("gen_stage_picked", "0")
			}
			if tc.account {
				row, err := st.AddBrowser("豆包", "账号1", "")
				if err != nil {
					t.Fatal(err)
				}
				st.UpdateBrowserHealth(row.ID, store.HealthExhausted, 0, "未登录")
			}
			a := &App{store: st}
			a.applyWizardGate()
			if a.wizardOn || st.Get(wizard.KeyComplete) != wizard.CompleteYes {
				t.Fatal("returning users should skip and keep the completion marker")
			}
			a.openWizard()
			if a.wizardFirst {
				t.Fatal("manually reopening should preserve the existing platform selection")
			}
		})
	}
}

func TestWizardSkipsHealthyAccountAndCanReopen(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "账号1", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	a := &App{
		store: st, manager: browser.NewManager(), engine: tasks.NewEngine(st, nil, nil, ""),
		splitSz: 380, sourceHeight: 210,
	}
	a.loadSettings()
	a.applyWizardGate()
	if a.wizardOn || st.Get(wizard.KeyComplete) != wizard.CompleteYes {
		t.Fatal("an existing logged-in account should skip and record completion")
	}
	tt := ui.NewTester(a.view, 1280, 900)
	assertVisible(t, tt, "开始出图", 1280, 900)
	if err := tt.Click("设置向导"); err != nil {
		t.Fatal(err)
	}
	if !a.wizardOn || a.wizardStep != wizard.StepPlatform {
		t.Fatal("settings did not reopen the wizard on the platform step")
	}
	assertVisible(t, tt, "选择平台", 1280, 900)
	if a.wizardChoice != "豆包" || a.wizardFirst {
		t.Fatalf("reopen: choice=%q first=%v", a.wizardChoice, a.wizardFirst)
	}
}

func TestWizardReopenWhenLoginPageStaysBlank(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{
		store: st, manager: browser.NewManager(), engine: tasks.NewEngine(st, nil, nil, ""),
		dataRoot: t.TempDir(), splitSz: 380, sourceHeight: 210,
		wizardOn: true, wizardStep: wizard.StepBrowser, wizardChoice: "豆包", platDoubao: true,
	}
	var opens int
	a.wizardLaunch = func(b store.Browser) (string, error) {
		if b.Platform != "豆包" || b.Name == "" {
			t.Fatalf("launch %+v", b)
		}
		return "http://127.0.0.1:9", nil
	}
	a.wizardOpen = func(ws, rawURL string) error {
		opens++
		if rawURL != "https://www.doubao.com/chat/" {
			t.Fatalf("open %s", rawURL)
		}
		return errors.New("窗口仍停在空白页")
	}
	a.wizardProbe = func(context.Context, string) (string, error) {
		t.Fatal("blank page should not be probed")
		return "", nil
	}
	a.wizardPlan = wizard.BrowserPlan{Title: "Camoufox 已就绪", CanNext: true}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	if a.wizardStep != wizard.StepAccount || !a.wizardNavFailed || opens != 1 {
		t.Fatalf("step=%v nav=%v opens=%d err=%s", a.wizardStep, a.wizardNavFailed, opens, a.wizardErr)
	}
	assertVisible(t, tt, "重新打开", 1280, 900)
	if !strings.Contains(a.wizardErr, "打开登录页失败") || !strings.Contains(a.wizardErr, "空白页") {
		t.Fatalf("wizard err %q", a.wizardErr)
	}
	found := false
	for _, entry := range logging.Log.Entries() {
		if strings.Contains(entry.Text, "打开登录页失败") && strings.Contains(entry.Text, "空白页") {
			found = true
		}
	}
	if !found {
		t.Fatal("运行日志没有打开登录页失败")
	}
	if err := tt.Click("重新打开"); err != nil {
		t.Fatal(err)
	}
	if opens != 2 {
		t.Fatalf("重新打开 opens=%d", opens)
	}
}

func TestWizardBlocksNextUntilBrowserIsReady(t *testing.T) {
	a := &App{splitSz: 380, sourceHeight: 210, wizardOn: true, wizardStep: wizard.StepBrowser, wizardChoice: "豆包"}
	a.wizardPlan = wizard.BrowserPlan{Title: "需要安装 Camoufox", NeedsDownload: true, Blocked: ""}
	tt := ui.NewTester(a.view, 1000, 720)
	assertVisible(t, tt, "需要安装 Camoufox", 1000, 720)
	assertVisible(t, tt, "安装 Camoufox", 1000, 720)
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	if a.wizardStep != wizard.StepBrowser {
		t.Fatal("next advanced without a browser")
	}
}
