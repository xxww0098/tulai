package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestStartupDropsUnloggedDoubaoAndQwen(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	keepDir := filepath.Join(root, "kept")
	if err := os.MkdirAll(keepDir, 0o755); err != nil {
		t.Fatal(err)
	}
	kept, err := st.AddBrowser("豆包", "账号1", keepDir)
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(kept.ID, false, true)
	if _, err := st.SaveLoggedInExtension("千问", "profile-q", "扩展千问"); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"豆包", "千问"} {
		dir := filepath.Join(root, "junk-"+platform)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddBrowser(platform, platform+"半成品", dir); err != nil {
			t.Fatal(err)
		}
		if _, err := st.UpsertExtensionAccount(platform, "bare-"+platform, ""); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{store: st, manager: browser.NewManager(), dataRoot: root, browserBackend: browser.KindChrome}
	a.loadSettings()
	rows := st.Browsers()
	if len(rows) != 2 {
		t.Fatalf("startup rows = %+v", rows)
	}
	var sawLocal, sawQwen bool
	for _, row := range rows {
		if !row.IsLoggedIn {
			t.Fatalf("unlogged row kept: %+v", row)
		}
		if row.ExtAccount == "" && row.Platform == "豆包" {
			sawLocal = true
		}
		if row.ExtAccount == "profile-q" && row.Platform == "千问" {
			sawQwen = true
		}
	}
	if !sawLocal || !sawQwen {
		t.Fatalf("kept = %+v", rows)
	}
	if _, err := os.Stat(keepDir); err != nil {
		t.Fatal(err)
	}
	for _, platform := range []string{"豆包", "千问"} {
		if _, err := os.Stat(filepath.Join(root, "junk-"+platform)); !os.IsNotExist(err) {
			t.Fatalf("%s junk profile still exists: %v", platform, err)
		}
	}
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
