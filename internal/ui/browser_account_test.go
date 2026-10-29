package ui

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestRenamePersistsAndShowsOnTheStage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("千问", "千问1", "/data/千问1")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	a := &App{
		store: st, manager: browser.NewManager(),
		tab: pageBrowser, browserChoice: "Chrome", browserBackend: "Chrome",
		detectedNote: "已检测到 Chrome", cftStatus: "内置浏览器未下载",
		stageTab: 1, stagePicked: true,
	}
	a.refreshBrowsers()
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("千问1"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("千问1"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("工作室")
	tt.Key(0, ui.KeyEscape)
	if got := st.Browsers(); len(got) != 1 || got[0].Name != "千问1" || got[0].UserDataDir != "/data/千问1" {
		t.Fatalf("escape saved a rename: %+v", got)
	}

	if err := tt.Click("重命名 千问1"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("工作室")
	tt.Key(0, ui.KeyEnter)
	got := st.Browsers()
	if len(got) != 1 || got[0].Name != "工作室" || got[0].UserDataDir != "/data/千问1" {
		t.Fatalf("rename = %+v", got)
	}
	if a.accountName(row.ID) != "工作室" {
		t.Fatalf("account name = %q", a.accountName(row.ID))
	}

	a.tab = pageHome
	tt.Frame()
	if !tt.HasText("工作室") {
		t.Fatalf("stage tab did not pick up the rename: %v", tt.Texts())
	}
	if tt.HasText("二维码") {
		t.Fatal("QR came back")
	}
}

func TestAddAccountFollowsStageAndNumbersPerPlatform(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, row := range []struct{ platform, name string }{
		{"豆包", "豆包1"},
		{"豆包", "豆包2"},
		{"千问", "千问2"},
	} {
		if _, err := st.AddBrowser(row.platform, row.name, ""); err != nil {
			t.Fatal(err)
		}
	}

	var launched store.Browser
	a := &App{
		store: st, manager: browser.NewManager(),
		tab: pageBrowser, stageTab: 1, stagePicked: true,
		browserChoice: "Chrome", browserBackend: "Chrome",
		detectedNote: "已检测到 Chrome", cftStatus: "内置浏览器未下载",
		loginLaunch: func(b store.Browser) error {
			launched = b
			return nil
		},
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if a.newBrowserPlat != "千问" {
		t.Fatalf("dropdown followed %q, want 千问", a.newBrowserPlat)
	}
	a.tab = pageHome
	tt.Frame()
	btn := assertVisible(t, tt, "+ 添加账号", 1280, 900)
	if btn.W > 160 {
		t.Fatalf("add-account button is a full-width bar: %+v", btn)
	}
	if err := tt.Click("+ 添加账号"); err != nil {
		t.Fatal(err)
	}
	if launched.Platform != "千问" || launched.Name != "千问1" {
		t.Fatalf("launched %+v", launched)
	}
	if a.tab != pageBrowser || a.newBrowserPlat != "千问" {
		t.Fatalf("tab=%d plat=%s", a.tab, a.newBrowserPlat)
	}

	a.tab = pageBrowser
	a.stageTab = 0
	a.platSynced = false
	tt.Frame()
	if a.newBrowserPlat != "豆包" {
		t.Fatalf("dropdown = %s, want 豆包", a.newBrowserPlat)
	}
}

func TestFollowLoginClosesBrowserAfterWebpageLogin(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "豆包1", "/data/豆包1")
	if err != nil {
		t.Fatal(err)
	}
	st.SetRunning(row.ID, true)
	done := make(chan struct{})
	a := &App{
		store: st, manager: browser.NewManager(), loginPoll: time.Hour,
		tab: pageBrowser, browserChoice: "Chrome", browserBackend: "Chrome",
		detectedNote: "已检测到 Chrome", cftStatus: "内置浏览器未下载",
		afterClose: func() { close(done) },
		wizardProbe: func(context.Context, string) (string, error) {
			return wizard.ProbeIn, nil
		},
	}
	a.followLogin(row, "http://127.0.0.1:9333")
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("login window stayed open")
	}
	a.mu.Lock()
	closed := append([]int64(nil), a.loginClosed...)
	a.mu.Unlock()
	if len(closed) != 1 || closed[0] != row.ID {
		t.Fatalf("closed = %v", closed)
	}
	got := st.Browsers()
	if len(got) != 1 || !got[0].IsLoggedIn || got[0].IsRunning || got[0].Name != "豆包1" {
		t.Fatalf("after login: %+v", got)
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if !tt.HasText("已登录") || tt.HasText("二维码") || tt.HasText("用手机扫码") {
		t.Fatalf("login row: %v", tt.Texts())
	}
}
