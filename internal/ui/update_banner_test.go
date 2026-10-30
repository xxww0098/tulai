package ui

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/update"
)

func TestUpdateBannerAndSkip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{
		store: st, engine: tasks.NewEngine(st, nil, nil, ""),
		updateNotice: "0.2.0", updateNotes: "修复了下载中断",
	}
	tt := ui.NewTester(a.view, 1280, 900)
	for _, label := range []string{"发现新版本 0.2.0", "修复了下载中断", "立即更新", "稍后", "跳过此版本"} {
		assertVisible(t, tt, label, 1280, 900)
	}
	if err := tt.Click("跳过此版本"); err != nil {
		t.Fatal(err)
	}
	if a.updateNotice != "" {
		t.Fatal("banner stayed after skip")
	}
	if update.Skipped(st) != "0.2.0" {
		t.Fatalf("skip stored %q", update.Skipped(st))
	}
	st.Close()
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if update.Skipped(st) != "0.2.0" {
		t.Fatalf("skip did not survive reopen: %q", update.Skipped(st))
	}
}

func TestUpdateDefersWhileTasksRun(t *testing.T) {
	a := &App{
		engine:  tasks.NewEngine(nil, nil, nil, ""),
		running: true, updateNotice: "0.2.0",
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("立即更新"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.updatePending || !a.updateDeferred {
		t.Fatalf("pending=%v deferred=%v", a.updatePending, a.updateDeferred)
	}
	if !tt.HasText(update.DeferMessage) {
		t.Fatal("banner did not say the update waits for idle")
	}
	if update.NextStep(a.tasksRunning(), a.updatePending, false) != update.StepDefer {
		t.Fatal("next step would install during a run")
	}
	a.running = false
	if update.NextStep(a.tasksRunning(), a.updatePending, false) != update.StepDownload {
		t.Fatal("idle pending update did not move to download")
	}
}

func TestSettingsShowVersionAndCheck(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), tab: pageSettings, autoCheck: true}
	tt := ui.NewTester(a.view, 1280, 900)
	scrollIntoView(t, tt, "当前版本 "+currentVersion(), 1280, 900)
	for _, label := range []string{"当前版本 " + currentVersion(), "自动检查更新", "检查更新"} {
		assertVisible(t, tt, label, 1280, 900)
	}
}
