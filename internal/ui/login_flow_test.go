package ui

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestProxyCardAndAccountQR(t *testing.T) {
	settings := &App{
		tab: pageSettings, settingsTab: settingsAdvanced, autoCheck: true, captchaEnabled: true, captchaBackend: "free",
		proxyCustom: true, proxyURL: "http://127.0.0.1:7890", proxySaved: "http://127.0.0.1:7890",
	}
	st := ui.NewTester(settings.view, 1280, 900)
	scrollIntoView(t, st, "跟随系统", 1280, 900)
	for _, label := range []string{"网络", "跟随系统", "自定义代理", "Camoufox 安装使用此代理；账号可在浏览器页设置独立代理。"} {
		assertVisible(t, st, label, 1280, 900)
	}
	follow := assertVisible(t, st, "跟随系统", 1280, 900)
	custom := assertVisible(t, st, "自定义代理", 1280, 900)
	if custom.X < follow.X+follow.W || follow.W > 160 || custom.W > 160 {
		t.Fatalf("proxy choices should sit side by side and stay compact: follow=%+v custom=%+v", follow, custom)
	}

	account := &App{
		tab: pageBrowser, browserSel: -1, browserChoice: "Chrome", browserBackend: "Chrome", newBrowserPlat: "豆包",
		detectedNote: "已检测到 Chrome", camoufoxStatus: "内置浏览器未下载",
		browserRows: []store.Browser{{
			ID: 1, Name: "工作号", Platform: "豆包", Port: 9333, Health: store.HealthReady,
		}},
		loginShots: map[int64]loginShot{1: {state: wizard.ProbeOut}},
	}
	at := ui.NewTester(account.view, 1280, 900)
	for _, label := range []string{"工作号", pageLoginHint, "豆包", "重命名 工作号"} {
		assertVisible(t, at, label, 1280, 900)
	}
	if at.HasText("二维码") || at.HasText("用手机扫码") {
		t.Fatalf("login QR still on the account row: %v", at.Texts())
	}
	name := assertVisible(t, at, "工作号", 1280, 900)
	caption := assertVisible(t, at, pageLoginHint, 1280, 900)
	start := assertVisible(t, at, "启动 工作号", 1280, 900)
	if caption.Y < name.Y || caption.Y > name.Y+200 {
		t.Fatalf("QR caption should sit in the account row: name=%+v caption=%+v", name, caption)
	}
	if start.X <= name.X || start.Y < name.Y-16 || start.Y > caption.Y+16 {
		t.Fatalf("start should be in the same table row: name=%+v caption=%+v start=%+v", name, caption, start)
	}
	if start.W > 160 {
		t.Fatalf("action button is too wide: %+v", start)
	}
	if at.HasText("删除当前") {
		t.Fatal("delete should be hidden until an environment is selected")
	}
	if err := at.Click("配置 工作号"); err != nil {
		t.Fatal(err)
	}
	del := assertVisible(t, at, "删除当前", 1280, 900)
	if del.W > 160 || del.Y < start.Y {
		t.Fatalf("delete should stay compact in the selected-environment panel: %+v start=%+v", del, start)
	}

	timed := &App{
		tab: pageBrowser, browserSel: -1, browserChoice: "Chrome", browserBackend: "Chrome", newBrowserPlat: "豆包",
		detectedNote: "已检测到 Chrome", camoufoxStatus: "内置浏览器未下载",
		browserRows: []store.Browser{{
			ID: 1, Name: "工作号", Platform: "豆包", Port: 9333, Health: store.HealthReady,
		}},
		loginShots: map[int64]loginShot{1: {state: wizard.ProbeOut, hint: "还没检测到登录"}},
	}
	ht := ui.NewTester(timed.view, 1280, 900)
	hint := assertVisible(t, ht, "还没检测到登录", 1280, 900)
	hintName := assertVisible(t, ht, "工作号", 1280, 900)
	if hint.Y < hintName.Y || hint.Y > hintName.Y+160 {
		t.Fatalf("timeout hint should sit in the account row: name=%+v hint=%+v", hintName, hint)
	}

	region := &App{
		wizardOn: true, wizardStep: wizard.StepAccount, wizardPhase: "region", wizardErr: regionProxyHint,
		wizardAccount: store.Browser{ID: 2, Platform: "千问", Name: "备用"},
		wizardName:    "备用", wizardNameFor: 2,
		loginShots: map[int64]loginShot{2: {state: wizard.ProbeRegion}},
	}
	rt := ui.NewTester(region.view, 1000, 720)
	if !rt.HasText(regionProxyHint) {
		t.Fatalf("region hint missing: %v", rt.Texts())
	}
	assertVisible(t, rt, "账号名称", 1000, 720)
	assertVisible(t, rt, pageLoginTitle, 1000, 720)
	if rt.HasText("二维码") {
		t.Fatalf("wizard still shows a QR: %v", rt.Texts())
	}

	stStore, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer stStore.Close()
	row, err := stStore.AddBrowser("豆包", "账号1", "/data/账号1")
	if err != nil {
		t.Fatal(err)
	}
	renamer := &App{store: stStore, wizardAccount: row, wizardNameFor: row.ID, wizardName: "工作室"}
	renamer.syncWizardName()
	got := stStore.Browsers()
	if len(got) != 1 || got[0].Name != "工作室" || got[0].UserDataDir != "/data/账号1" {
		t.Fatalf("rename = %+v", got)
	}

	dir := os.Getenv("TULAI_SHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	saveFrame(t, dir, "settings-proxy", st)
	saveFrame(t, dir, "account-login", at)
	saveFrame(t, dir, "account-login-wait", ht)
}

func TestReloginWatchesQR(t *testing.T) {
	watched := make(chan string, 1)
	a := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		prompts:         "一只猫",
		sources:         []sourceImage{{Path: "风景.png"}},
		dailyCap:        20,
		accountOpen: func(b store.Browser) error {
			if b.ID != 7 || b.Port != 9333 {
				t.Errorf("opened %+v", b)
			}
			return nil
		},
		loginFollow: func(b store.Browser, ws string) {
			if b.ID == 7 {
				watched <- ws
			}
		},
		browserRows: []store.Browser{
			{ID: 7, Name: "没登录", Platform: "豆包", Port: 9333, Health: store.HealthExhausted, LastError: "未登录"},
		},
	}
	tt := ui.NewTester(a.view, 1280, 980)
	tt.Advance(300 * time.Millisecond)
	if err := tt.Click("去重登"); err != nil {
		t.Fatal(err)
	}
	select {
	case ws := <-watched:
		if ws != "camoufox://9333" {
			t.Fatalf("watch endpoint %q", ws)
		}
	case <-time.After(time.Second):
		t.Fatal("去重登 did not start the QR watch")
	}
	if a.tab != pageBrowser || a.browserSel != 0 {
		t.Fatalf("QR row should be selected: tab=%d sel=%d", a.tab, a.browserSel)
	}
}

func TestPublishLoginClearsEngineAttention(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "豆包1", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, true, true)
	e := tasks.NewEngine(st, nil, nil, "")
	e.RaiseAttention("豆包1 需要重新扫码登录", row.ID)
	a := &App{
		store: st, engine: e,
		loginWatchGen: map[int64]int{row.ID: 1},
		loginShots:    map[int64]loginShot{},
	}

	if !a.publishLogin(row.ID, 1, wizard.ProbeIn, "") {
		t.Fatal("login generation changed")
	}
	if text, _, ok := e.Attention(); ok {
		t.Fatalf("stale relogin banner remained: %q", text)
	}
	if shot := a.loginShot(row.ID); shot.state != wizard.ProbeIn {
		t.Fatalf("login shot = %+v", shot)
	}
	if got := st.Browsers(); len(got) != 1 || !got[0].IsLoggedIn {
		t.Fatalf("login was not confirmed: %+v", got)
	}
}

func waitShot(t *testing.T, a *App, id int64, ok func(loginShot) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ok(a.loginShot(id)) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("shot = %+v", a.loginShot(id))
}

func saveFrame(t *testing.T, dir, name string, tt *ui.Tester) {
	t.Helper()
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestIsClosedBrowserError(t *testing.T) {
	closedCases := []string{
		"Browser.open_url: BrowserContext.new_page: Target page, context or browser has been closed",
		"\x1b[2mBrowserContext.new_page: \x1b[22mTarget page, context or browser has been closed",
		"Camoufox 账号窗口已关闭",
		"camoufox driver closed",
	}
	for _, msg := range closedCases {
		if !isClosedBrowserError(errors.New(msg)) {
			t.Fatalf("expected closed error match for %q", msg)
		}
	}
	if isClosedBrowserError(nil) || isClosedBrowserError(errors.New("proxy connection failed")) {
		t.Fatal("non-closed error should return false")
	}
}
