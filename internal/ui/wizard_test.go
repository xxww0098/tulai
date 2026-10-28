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
	if !a.wizardOn || a.wizardStep != wizard.StepBrowser {
		t.Fatal("a new config should open on the browser step")
	}
	a.wizardPlan = wizard.BrowserPlan{
		Backend: browser.KindChrome, Kind: browser.KindChrome,
		Title: "将使用本机 Chrome", Detail: "/opt/google/chrome/chrome", CanNext: true,
	}
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
	for _, label := range []string{"tulai", "准备浏览器", "将使用本机 Chrome", "下一步"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	doubao := assertVisible(t, tt, "豆包", 1280, 900)
	qianwen := assertVisible(t, tt, "千问", 1280, 900)
	if qianwen.X <= doubao.X+40 {
		t.Fatalf("platform cards are stacked or overlapping: doubao=%+v qianwen=%+v", doubao, qianwen)
	}
	next := assertVisible(t, tt, "下一步", 1280, 900)
	if next.Y < doubao.Y+80 {
		t.Fatalf("next crowds the platform cards: card=%+v next=%+v", doubao, next)
	}
	if err := tt.Click("千问"); err != nil {
		t.Fatal(err)
	}
	if !a.platDoubao || !a.platQianwen {
		t.Fatalf("platforms doubao=%v qianwen=%v", a.platDoubao, a.platQianwen)
	}
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"请在弹出的浏览器中扫码登录", "已登录? 重新检测", "豆包 · 账号1"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if a.wizardStep != wizard.StepAccount || probes != 1 {
		t.Fatalf("step=%v probes=%d", a.wizardStep, probes)
	}
	if err := tt.Click("已登录? 重新检测"); err != nil {
		t.Fatal(err)
	}
	if a.wizardStep != wizard.StepDone || probes < 3 {
		t.Fatalf("after login step=%v probes=%d", a.wizardStep, probes)
	}
	for _, label := range []string{"拖入图片 → 选提示词 → 开始", "拖入图片", "选提示词", "开始", "进入主页", "再添加一个账号"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if err := tt.Click("再添加一个账号"); err != nil {
		t.Fatal(err)
	}
	assertVisible(t, tt, "再添加豆包", 1280, 900)
	if err := tt.Click("再添加豆包"); err != nil {
		t.Fatal(err)
	}
	if a.wizardStep != wizard.StepDone {
		t.Fatalf("extra account did not finish login, step=%v phase=%s err=%s", a.wizardStep, a.wizardPhase, a.wizardErr)
	}
	rows := st.Browsers()
	if len(rows) != 3 || rows[0].Name != "账号1" || rows[0].Platform != "豆包" || !rows[0].IsLoggedIn || rows[1].Name != "账号2" || rows[1].Platform != "千问" || !rows[1].IsLoggedIn || rows[2].Name != "账号3" || rows[2].Platform != "豆包" || !rows[2].IsLoggedIn {
		t.Fatalf("accounts: %+v", rows)
	}
	if st.Get("browser_backend") != browser.KindChrome {
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
	if !a.wizardOn || a.wizardStep != wizard.StepBrowser {
		t.Fatal("settings did not reopen the wizard")
	}
	assertVisible(t, tt, "准备浏览器", 1280, 900)
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
		wizardOn: true, wizardStep: wizard.StepPlatform, platDoubao: true,
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
	a := &App{splitSz: 380, sourceHeight: 210, wizardOn: true, wizardStep: wizard.StepBrowser}
	a.wizardPlan = wizard.BrowserPlan{Title: "需要下载内置浏览器", NeedsDownload: true, Blocked: ""}
	tt := ui.NewTester(a.view, 1000, 720)
	assertVisible(t, tt, "需要下载内置浏览器", 1000, 720)
	assertVisible(t, tt, "下载内置浏览器", 1000, 720)
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	if a.wizardStep != wizard.StepBrowser {
		t.Fatal("next advanced without a browser")
	}
}
