package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

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
