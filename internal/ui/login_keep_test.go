package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestStartupKeepsStaleLoginWhenProbeUnknown(t *testing.T) {
	probes := []struct {
		name  string
		probe func(store.Browser) (string, error)
	}{
		{name: "unknown", probe: func(store.Browser) (string, error) { return wizard.ProbeUnknown, nil }},
		{name: "region", probe: func(store.Browser) (string, error) { return wizard.ProbeRegion, nil }},
		{name: "challenge", probe: func(store.Browser) (string, error) { return "challenge", nil }},
		{name: "down", probe: func(store.Browser) (string, error) { return "", fmt.Errorf("network down") }},
		{name: "closed", probe: nil},
	}
	for _, tc := range probes {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			st := openKeepStore(t)
			keptDir := filepath.Join(root, "豆包1")
			if err := os.MkdirAll(keptDir, 0o755); err != nil {
				t.Fatal(err)
			}
			kept, err := st.AddBrowser("豆包", "豆包1", keptDir)
			if err != nil {
				t.Fatal(err)
			}
			st.UpdateBrowser(kept.ID, false, true)
			dirs := map[int64]string{kept.ID: keptDir}
			for _, platform := range []string{"豆包", "千问"} {
				dir := filepath.Join(root, platform+"2")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				row, err := st.AddBrowser(platform, platform+"2", dir)
				if err != nil {
					t.Fatal(err)
				}
				if row.IsLoggedIn {
					t.Fatalf("new row already logged in: %+v", row)
				}
				dirs[row.ID] = dir
				if _, err := st.UpsertExtensionAccount(platform, "bare-"+platform, ""); err != nil {
					t.Fatal(err)
				}
			}
			a := &App{
				store: st, manager: browser.NewManager(), dataRoot: root,
				browserBackend: browser.KindChrome, accountProbe: tc.probe,
			}
			a.loadSettings()
			rows := st.Browsers()
			if len(rows) != 5 {
				t.Fatalf("startup rows = %+v", rows)
			}
			for _, row := range rows {
				if row.Name == "豆包1" && !row.IsLoggedIn {
					t.Fatalf("logged-in row cleared: %+v", row)
				}
				if row.Name == "豆包2" || row.Name == "千问2" {
					if row.IsLoggedIn {
						t.Fatalf("unknown probe marked logged in: %+v", row)
					}
				}
				if row.ExtAccount != "" && row.IsLoggedIn {
					t.Fatalf("unconfirmed extension marked logged in: %+v", row)
				}
				if dir := dirs[row.ID]; dir != "" {
					if _, err := os.Stat(dir); err != nil {
						t.Fatalf("%s profile gone: %v", row.Name, err)
					}
				}
			}
		})
	}
}

func TestStartupDropsConfirmedLoggedOutProfile(t *testing.T) {
	for _, platform := range []string{"豆包", "千问"} {
		t.Run(platform, func(t *testing.T) {
			root := t.TempDir()
			st := openKeepStore(t)
			keepDir := filepath.Join(root, "kept")
			dropDir := filepath.Join(root, "never")
			for _, dir := range []string{keepDir, dropDir} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			kept, err := st.AddBrowser(platform, platform+"1", keepDir)
			if err != nil {
				t.Fatal(err)
			}
			st.UpdateBrowser(kept.ID, false, true)
			junk, err := st.AddBrowser(platform, platform+"2", dropDir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.UpsertExtensionAccount(platform, "bare-"+platform, ""); err != nil {
				t.Fatal(err)
			}
			a := &App{
				store: st, manager: browser.NewManager(), dataRoot: root,
				browserBackend: browser.KindChrome,
				accountProbe: func(b store.Browser) (string, error) {
					if b.ID == kept.ID || b.IsLoggedIn {
						t.Fatalf("probed a stored login: %+v", b)
					}
					return wizard.ProbeOut, nil
				},
			}
			a.loadSettings()
			var sawKept, sawBare bool
			for _, row := range st.Browsers() {
				if row.ID == junk.ID {
					t.Fatalf("confirmed out kept: %+v", row)
				}
				if row.ID == kept.ID && row.IsLoggedIn {
					sawKept = true
				}
				if row.ExtAccount == "bare-"+platform && !row.IsLoggedIn {
					sawBare = true
				}
			}
			if !sawKept || !sawBare {
				t.Fatalf("rows = %+v", st.Browsers())
			}
			if _, err := os.Stat(keepDir); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dropDir); !os.IsNotExist(err) {
				t.Fatalf("logged-out profile kept: %v", err)
			}
		})
	}
}

func TestProbeAndRestorePersistLogin(t *testing.T) {
	for _, platform := range []string{"豆包", "千问"} {
		t.Run(platform, func(t *testing.T) {
			root := t.TempDir()
			st := openKeepStore(t)
			dir := filepath.Join(root, platform+"2")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			row, err := st.AddBrowser(platform, platform+"2", dir)
			if err != nil {
				t.Fatal(err)
			}
			a := &App{
				store: st, manager: browser.NewManager(), dataRoot: root,
				browserBackend: browser.KindChrome,
				loginWatchGen:  map[int64]int{row.ID: 1},
			}
			if !a.publishLogin(row.ID, 1, wizard.ProbeIn, "") {
				t.Fatal("publish")
			}
			if got := keepBrowser(t, st, row.ID); !got.IsLoggedIn {
				t.Fatalf("probe in did not persist: %+v", got)
			}
			st.UpdateBrowser(row.ID, false, false)
			a.browserRows = []store.Browser{{ID: row.ID, Platform: platform, Name: row.Name, UserDataDir: dir}}
			a.browserSel = 0
			a.restoreBrowser()
			if got := keepBrowser(t, st, row.ID); !got.IsLoggedIn {
				t.Fatalf("restore did not persist: %+v", got)
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatal(err)
			}
			st.UpdateBrowser(row.ID, false, false)
			a.accountProbe = func(store.Browser) (string, error) { return wizard.ProbeIn, nil }
			a.loadSettings()
			if got := keepBrowser(t, st, row.ID); !got.IsLoggedIn {
				t.Fatalf("startup probe in did not persist: %+v", got)
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func openKeepStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func keepBrowser(t *testing.T, st *store.Store, id int64) store.Browser {
	t.Helper()
	for _, row := range st.Browsers() {
		if row.ID == id {
			return row
		}
	}
	t.Fatalf("missing browser %d in %+v", id, st.Browsers())
	return store.Browser{}
}

func TestWizardBackDropsUnloggedAccount(t *testing.T) {
	for _, platform := range []string{"豆包", "千问"} {
		t.Run(platform, func(t *testing.T) {
			root := t.TempDir()
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			a := &App{
				store: st, manager: browser.NewManager(), dataRoot: root,
				browserBackend: browser.KindChrome, wizardOn: true, wizardStep: wizard.StepPlatform,
				platDoubao: platform == "豆包", platQianwen: platform == "千问",
			}
			a.wizardLaunch = func(b store.Browser) (string, error) {
				if b.Platform != platform {
					t.Fatalf("launch %+v", b)
				}
				return "http://127.0.0.1:9", nil
			}
			a.wizardOpen = func(string, string) error { return nil }
			a.wizardProbe = func(context.Context, string) (string, error) {
				return wizard.ProbeOut, nil
			}
			tt := ui.NewTester(a.view, 1000, 720)
			if err := tt.Click("下一步"); err != nil {
				t.Fatal(err)
			}
			rows := st.Browsers()
			if len(rows) != 1 || rows[0].IsLoggedIn || rows[0].Platform != platform {
				t.Fatalf("pending = %+v", rows)
			}
			if rows[0].UserDataDir != "" {
				if err := os.MkdirAll(rows[0].UserDataDir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			dir := rows[0].UserDataDir
			if err := tt.Click("上一步"); err != nil {
				t.Fatal(err)
			}
			if left := st.Browsers(); len(left) != 0 {
				t.Fatalf("unlogged %s kept: %+v", platform, left)
			}
			if dir != "" {
				if _, err := os.Stat(dir); !os.IsNotExist(err) {
					t.Fatalf("profile kept: %v", err)
				}
			}
		})
	}
}

func TestLoggedInRowDoesNotAlsoSayLoggedOut(t *testing.T) {
	a := &App{
		tab: pageBrowser, browserSel: -1, browserChoice: "Chrome", browserBackend: "Chrome",
		newBrowserPlat: "豆包", detectedNote: "已检测到 Chrome", cftStatus: "内置浏览器未下载",
		browserRows: []store.Browser{
			{ID: 2, Name: "豆包2", Platform: "豆包", Port: 9340, Health: store.HealthExhausted, LastError: "未登录"},
			{ID: 3, Name: "千问2", Platform: "千问", Port: 9341, IsLoggedIn: true, Health: store.HealthExhausted, LastError: "未登录"},
		},
		loginShots: map[int64]loginShot{2: {state: wizard.ProbeIn}},
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if !tt.HasText("已登录") || tt.HasText("未登录") {
		t.Fatalf("conflicting login labels: %v", tt.Texts())
	}
	if !tt.HasText("豆包2") || !tt.HasText("千问2") {
		t.Fatalf("rows missing: %v", tt.Texts())
	}
}
