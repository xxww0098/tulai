package ui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

// 下载设置要有默认值、能在设置页看到，并且原样传给任务。
func TestDownloadSettingsDefaultsPersistAndReachTask(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a := &App{store: st, manager: browser.NewManager()}
	a.loadSettings()
	if a.downloadConc != 2 || a.downloadRetries != 2 || a.downloadRateKB != 0 {
		t.Fatalf("默认值 = %v/%v/%v", a.downloadConc, a.downloadRetries, a.downloadRateKB)
	}
	opts := a.taskOptions(nil, nil, nil)
	if opts.DownloadConcurrency != 2 || opts.DownloadRetries != 2 || opts.DownloadRateKBps != 0 {
		t.Fatalf("任务默认值 = %+v", opts)
	}

	a.tab = pageSettings
	a.settingsTab = settingsDownload
	tt := ui.NewTester(a.view, 760, 1600)
	for _, label := range []string{"每个站点同时下载", "失败重试", "限速 (KB/s)"} {
		scrollIntoView(t, tt, label, 760, 1600)
		assertVisible(t, tt, label, 760, 1600)
	}

	a.downloadConc = 4
	a.downloadRetries = 5
	a.downloadRateKB = 2048
	a.dlURLs = "https://example.test/a.jpg"
	a.dlDir = t.TempDir()
	a.saveSettings()

	restored := &App{store: st, manager: browser.NewManager()}
	restored.loadSettings()
	if restored.downloadConc != 4 || restored.downloadRetries != 5 || restored.downloadRateKB != 2048 {
		t.Fatalf("恢复值 = %v/%v/%v", restored.downloadConc, restored.downloadRetries, restored.downloadRateKB)
	}
	if restored.dlURLs != a.dlURLs || restored.dlDir != a.dlDir {
		t.Fatalf("下载页输入没有恢复: %q / %q", restored.dlURLs, restored.dlDir)
	}
	opts = restored.taskOptions(nil, nil, nil)
	if opts.DownloadConcurrency != 4 || opts.DownloadRetries != 5 || opts.DownloadRateKBps != 2048 {
		t.Fatalf("任务选项没有跟上设置: %+v", opts)
	}
}

func TestDownloadSettingsLinkOpensDownloadTab(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.tab = pageSettings
	a.settingsTab = settingsAdvanced
	tt := ui.NewTester(a.view, 1280, 800)
	scrollIntoView(t, tt, "跟随系统", 1280, 800)
	if err := tt.Click("下载"); err != nil {
		t.Fatal(err)
	}
	a.dlComposerOn = true
	tt.Frame()
	if err := tt.Click("下载设置"); err != nil {
		t.Fatal(err)
	}
	if a.tab != pageSettings || a.settingsTab != settingsDownload || a.dlComposerOn {
		t.Fatalf("下载设置入口: page=%d tab=%d composer=%v", a.tab, a.settingsTab, a.dlComposerOn)
	}
	assertVisible(t, tt, "每个站点同时下载", 1280, 800)
}
