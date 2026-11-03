package ui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestMissingCamoufoxKeepsTasksAndOpensInstall(t *testing.T) {
	config := t.TempDir()
	t.Setenv("HOME", config)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("APPDATA", config)
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "账号", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	failed := tasks.Task{Index: 1, Platform: "豆包", Status: tasks.StatusFailed, Prompt: "旧任务"}
	for _, retry := range []bool{false, true} {
		a := &App{
			store: st, manager: browser.NewManager(), engine: tasks.NewEngine(st, nil, nil, ""),
			platformAuto: true, prompts: "画一只猫", queue: []tasks.Task{failed},
			outputPath: filepath.Join(t.TempDir(), "output"),
		}
		if retry {
			a.tab, a.statePage = pageWash, pageWash
			a.sources = []sourceImage{{Path: "input.png"}}
			a.retryTasks(a.queue)
		} else {
			a.startTasks()
		}
		if a.tab != pageBrowser || a.running || a.engine.View() != nil || !reflect.DeepEqual(a.queue, []tasks.Task{failed}) {
			t.Fatalf("retry=%v: tab=%d running=%v queue=%+v", retry, a.tab, a.running, a.queue)
		}
		if _, err := os.Stat(a.outputPath); !os.IsNotExist(err) {
			t.Fatalf("retry=%v created output directory: %v", retry, err)
		}
		view := ui.NewTester(a.view, 1280, 840)
		if !view.HasText(a.camoufoxStatus) || !view.HasText("安装 Camoufox") {
			t.Fatalf("retry=%v: missing installation guidance: %v", retry, view.Texts())
		}
		if browser.CamoufoxSupported(runtime.GOOS, runtime.GOARCH) && a.camoufoxStatus != "请先安装 Camoufox，再返回任务页重试" {
			t.Fatalf("retry=%v: status=%q", retry, a.camoufoxStatus)
		}
		if retry {
			wash := a.stateOf(pageWash)
			if wash.prompts != "画一只猫" || len(wash.sources) != 1 || wash.sources[0].Path != "input.png" || !reflect.DeepEqual(wash.queue, []tasks.Task{failed}) {
				t.Fatalf("wash state lost after opening installer: %+v", wash)
			}
		}
	}
}

func TestCamoufoxPreflightAllowsReadyAndOtherBackends(t *testing.T) {
	config := t.TempDir()
	t.Setenv("HOME", config)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("APPDATA", config)
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	badOutput := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(badOutput, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, backend, platform string
	}{
		{"extension", "extension", "豆包"},
		{"antigravity", "", agy.Platform},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st.Set(extbridge.SettingBrowserBackend, tc.backend)
			a := &App{store: st, engine: tasks.NewEngine(st, nil, nil, ""), outputPath: badOutput}
			a.runQueue([]string{tc.platform}, []string{"画一只猫"}, nil, nil)
			if a.tab == pageBrowser || a.progText == "" {
				t.Fatalf("unrelated backend was blocked: tab=%d note=%q", a.tab, a.progText)
			}
		})
	}

	t.Run("installed", func(t *testing.T) {
		fakeInstalledCamoufox(t)
		st.Set(extbridge.SettingBrowserBackend, "")
		a := &App{store: st, engine: tasks.NewEngine(st, nil, nil, ""), outputPath: badOutput}
		a.runQueue([]string{"千问"}, []string{"画一只猫"}, nil, nil)
		if a.tab == pageBrowser || a.progText == "" {
			t.Fatalf("installed runtime was blocked: tab=%d note=%q", a.tab, a.progText)
		}
	})
}

func fakeInstalledCamoufox(t *testing.T) {
	t.Helper()
	config := t.TempDir()
	t.Setenv("HOME", config)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("APPDATA", config)
	dir := filepath.Join(browser.ConfigDir(), "runtime", "camoufox", camoufox.Version)
	node := filepath.Join(dir, "node", "bin", "node")
	if runtime.GOOS == "windows" {
		node = filepath.Join(dir, "node", "node.exe")
	}
	exe := filepath.Join(dir, "browser")
	for _, path := range []string{node, exe, filepath.Join(dir, "node_modules", "camoufox", "package.json")} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	lock, err := os.ReadFile("../camoufox/driver/package-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), lock, 0600); err != nil {
		t.Fatal(err)
	}
	marker, err := json.Marshal(map[string]string{"executable": exe, "version": camoufox.Version, "platform": runtime.GOOS + "/" + runtime.GOARCH})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ready.json"), marker, 0600); err != nil {
		t.Fatal(err)
	}
}
