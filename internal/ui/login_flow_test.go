package ui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestProxyCardAndAccountQR(t *testing.T) {
	settings := &App{
		tab: pageSettings, autoCheck: true, captchaEnabled: true, captchaBackend: "free",
		proxyCustom: true, proxyURL: "http://127.0.0.1:7890", proxySaved: "http://127.0.0.1:7890",
	}
	st := ui.NewTester(settings.view, 1280, 900)
	for _, label := range []string{"网络", "跟随系统", "自定义代理", "浏览器和内置浏览器下载走同一条代理。"} {
		assertVisible(t, st, label, 1280, 900)
	}
	follow := assertVisible(t, st, "跟随系统", 1280, 900)
	custom := assertVisible(t, st, "自定义代理", 1280, 900)
	if custom.X < follow.X+follow.W || follow.W > 160 || custom.W > 160 {
		t.Fatalf("proxy choices should sit side by side and stay compact: follow=%+v custom=%+v", follow, custom)
	}

	qr := sampleQR(180)
	account := &App{
		tab: pageBrowser, browserSel: -1, browserChoice: "Chrome", browserBackend: "Chrome", newBrowserPlat: "豆包",
		detectedNote: "已检测到 Chrome", cftStatus: "内置浏览器未下载",
		browserRows: []store.Browser{{
			ID: 1, Name: "工作号", Platform: "豆包", Port: 9333, Health: store.HealthReady,
		}},
		loginShots: map[int64]loginShot{1: {state: wizard.ProbeOut, bmp: ui.NewBitmap(qr)}},
	}
	at := ui.NewTester(account.view, 1280, 900)
	for _, label := range []string{"工作号", "用手机扫码，过期会自动刷新", "豆包"} {
		assertVisible(t, at, label, 1280, 900)
	}
	name := assertVisible(t, at, "工作号", 1280, 900)
	caption := assertVisible(t, at, "用手机扫码，过期会自动刷新", 1280, 900)
	start := assertVisible(t, at, "启动选中", 1280, 900)
	if caption.Y < name.Y || caption.Y > name.Y+200 {
		t.Fatalf("QR caption should sit in the account row: name=%+v caption=%+v", name, caption)
	}
	if start.Y < caption.Y || start.Y > name.Y+280 {
		t.Fatalf("instance list should hug its row, actions just under it: name=%+v start=%+v", name, start)
	}
	if start.W > 160 {
		t.Fatalf("action button is too wide: %+v", start)
	}
	del := assertVisible(t, at, "删除选中", 1280, 900)
	if del.W > 160 || del.Y > start.Y+80 {
		t.Fatalf("delete should stay a compact button under the actions: %+v start=%+v", del, start)
	}

	timed := &App{
		tab: pageBrowser, browserSel: -1, browserChoice: "Chrome", browserBackend: "Chrome", newBrowserPlat: "豆包",
		detectedNote: "已检测到 Chrome", cftStatus: "内置浏览器未下载",
		browserRows: []store.Browser{{
			ID: 1, Name: "工作号", Platform: "豆包", Port: 9333, Health: store.HealthReady,
		}},
		loginShots: map[int64]loginShot{1: {state: wizard.ProbeOut, hint: cdp.ErrLoginQR.Error()}},
	}
	ht := ui.NewTester(timed.view, 1280, 900)
	hint := assertVisible(t, ht, cdp.ErrLoginQR.Error(), 1280, 900)
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
	assertVisible(t, rt, "用手机扫码登录", 1000, 720)

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
	saveFrame(t, dir, "account-qr", at)
	saveFrame(t, dir, "account-qr-timeout", ht)
}

func TestAccountNeedsQRWhileLoginButtonIsUp(t *testing.T) {
	row := store.Browser{ID: 4, Platform: "豆包", Name: "工作号", IsLoggedIn: true}
	if !accountNeedsQR(row, loginShot{state: wizard.ProbeOut}) {
		t.Fatal("a stale logged-in flag must not hide the QR once the page is logged out")
	}
	if accountNeedsQR(row, loginShot{state: wizard.ProbeIn}) {
		t.Fatal("logged-in probe should hide the QR")
	}
	if accountNeedsQR(store.Browser{ExtAccount: "ext"}, loginShot{state: wizard.ProbeOut}) {
		t.Fatal("extension accounts do not show a page QR")
	}
	if !accountNeedsQR(store.Browser{ID: 4}, loginShot{hint: cdp.ErrLoginQR.Error()}) {
		t.Fatal("timeout hint should stay on the row")
	}
}

func TestFollowLoginPublishesQRThenClearsIt(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, sampleQR(48)); err != nil {
		t.Fatal(err)
	}
	pngBytes := append([]byte(nil), buf.Bytes()...)
	stop := func(a *App, id int64) {
		t.Helper()
		a.mu.Lock()
		a.loginWatchGen[id]++
		a.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	timed := &App{
		loginPoll: time.Hour,
		wizardProbe: func(context.Context, string) (string, error) {
			return wizard.ProbeOut, nil
		},
		wizardQR: func(context.Context, string) ([]byte, bool, error) {
			return nil, false, cdp.ErrLoginQR
		},
	}
	timed.followLogin(store.Browser{ID: 9, Name: "工作号", Platform: "豆包", Port: 9333}, "http://127.0.0.1:9333")
	waitShot(t, timed, 9, func(shot loginShot) bool {
		return shot.hint == cdp.ErrLoginQR.Error() && shot.bmp == nil && shot.state == wizard.ProbeOut
	})
	stop(timed, 9)

	live := &App{
		loginPoll: time.Hour,
		wizardProbe: func(context.Context, string) (string, error) {
			return wizard.ProbeOut, nil
		},
		wizardQR: func(context.Context, string) ([]byte, bool, error) {
			return pngBytes, false, nil
		},
	}
	live.followLogin(store.Browser{ID: 9, Name: "工作号", Platform: "豆包", Port: 9333}, "http://127.0.0.1:9333")
	waitShot(t, live, 9, func(shot loginShot) bool { return shot.bmp != nil && shot.hint == "" })
	stop(live, 9)
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
		if ws != "http://127.0.0.1:9333" {
			t.Fatalf("watch endpoint %q", ws)
		}
	case <-time.After(time.Second):
		t.Fatal("去重登 did not start the QR watch")
	}
	if a.tab != pageBrowser || a.browserSel != 0 {
		t.Fatalf("QR row should be selected: tab=%d sel=%d", a.tab, a.browserSel)
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

func sampleQR(n int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			c := color.NRGBA{R: 248, G: 248, B: 246, A: 255}
			if qrFinder(x, y, 0, 0, n) || qrFinder(x, y, n-n/4, 0, n) || qrFinder(x, y, 0, n-n/4, n) || (x*3+y*5)%7 < 3 {
				c = color.NRGBA{R: 22, G: 26, B: 32, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func qrFinder(x, y, ox, oy, n int) bool {
	size := n / 4
	if x < ox || y < oy || x >= ox+size || y >= oy+size {
		return false
	}
	lx, ly := x-ox, y-oy
	edge := lx < size/7 || ly < size/7 || lx >= size-size/7 || ly >= size-size/7
	core := lx > size/3 && lx < size-size/3 && ly > size/3 && ly < size-size/3
	return edge || core
}
