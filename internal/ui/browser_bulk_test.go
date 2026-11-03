package ui

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestBrowserTableMultiSelection(t *testing.T) {
	a := browserTableTestApp(t, 3)
	tt := ui.NewTester(a.view, 1280, 840)
	rows := a.store.Browsers()
	for i, b := range rows[:2] {
		if err := tt.Click("选择环境 " + b.Name); err != nil {
			t.Fatal(err)
		}
		if a.browserChecked.Len() != i+1 || !a.browserChecked.Has(b.ID) {
			t.Fatalf("checkbox changed other choices: %+v", a.browserChecked)
		}
	}
	if err := tt.Click("配置 " + rows[2].Name); err != nil {
		t.Fatal(err)
	}
	if a.browserChecked.Len() != 2 || a.browserSel != 2 {
		t.Fatal("configuration changed the batch selection")
	}
	if dir := os.Getenv("TULAI_SHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		saveFrame(t, dir, "browser-bulk-partial-selection", tt)
	}
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	if a.browserChecked.Len() != 3 {
		t.Fatal("select all did not include every environment")
	}
	if err := tt.Click("刷新列表"); err != nil {
		t.Fatal(err)
	}
	for _, b := range rows {
		if !a.browserChecked.Has(b.ID) {
			t.Fatal("refresh lost a checked environment")
		}
	}
	a.store.DeleteBrowser(rows[0].ID)
	if err := tt.Click("刷新列表"); err != nil {
		t.Fatal(err)
	}
	focused, ok := a.selectedBrowser()
	if a.browserChecked.Len() != 2 || a.browserChecked.Has(rows[0].ID) || !ok || focused.ID != rows[2].ID {
		t.Fatalf("row removal lost selection identity: checked=%+v focused=%+v", a.browserChecked, focused)
	}
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	if a.browserChecked.Len() != 0 {
		t.Fatal("select all did not clear a complete selection")
	}
	if err := tt.Click("选择环境 " + rows[1].Name); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("清空"); err != nil {
		t.Fatal(err)
	}
	if a.browserChecked.Len() != 0 {
		t.Fatal("clear left environments checked")
	}
}

func TestBrowserBatchStartsAndStopsEligibleEnvironments(t *testing.T) {
	a := browserTableTestApp(t, 4)
	external, err := a.store.SaveLoggedInExtension("豆包", "bulk-fixture", "扩展环境")
	if err != nil {
		t.Fatal(err)
	}
	rows := a.store.Browsers()
	var launched []int64
	a.loginLaunch = func(b store.Browser) error {
		launched = append(launched, b.ID)
		if b.ID == rows[2].ID {
			return errors.New("启动测试失败")
		}
		a.store.SetRunning(b.ID, true)
		return nil
	}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("批量启动"); err != nil {
		t.Fatal(err)
	}
	if len(launched) != 3 || launched[0] != rows[0].ID || launched[1] != rows[2].ID || launched[2] != rows[3].ID {
		t.Fatalf("wrong batch launch targets: %v", launched)
	}
	if a.browserBatch == nil || a.browserBatch.text() != "批量启动完成：3/3，失败 1，跳过 2" || len(a.browserPending) != 0 {
		t.Fatalf("wrong launch result: %+v pending=%v", a.browserBatch, a.browserPending)
	}
	if err := tt.Click("批量停止"); err != nil {
		t.Fatal(err)
	}
	if a.browserBatch.text() != "批量停止完成：3/3，失败 0，跳过 2" || len(a.browserPending) != 0 {
		t.Fatalf("wrong stop result: %+v pending=%v", a.browserBatch, a.browserPending)
	}
	for _, b := range a.store.Browsers() {
		if b.IsRunning != (b.ID == external.ID) {
			t.Fatalf("wrong running state after batch stop: %+v", b)
		}
	}
}

func TestBrowserBatchStopSurvivesFreshEnvironmentRemoval(t *testing.T) {
	a := browserTableTestApp(t, 3)
	a.dataRoot = t.TempDir()
	for _, b := range a.store.Browsers() {
		a.store.UpdateBrowser(b.ID, true, false)
		a.markFreshLogin(b.ID)
	}
	tt := ui.NewTester(a.view, 1280, 840)
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("批量停止"); err != nil {
		t.Fatal(err)
	}
	if len(a.store.Browsers()) != 0 || a.browserChecked.Len() != 0 || a.browserBatch.done != 3 || a.browserBatch.total != 3 {
		t.Fatalf("removing a fresh row changed later stop targets: rows=%v batch=%+v", a.store.Browsers(), a.browserBatch)
	}
}

func TestBrowserBatchPendingAndTaskGuards(t *testing.T) {
	a := browserTableTestApp(t, 3)
	a.engine = tasks.NewEngine(a.store, nil, nil, "")
	a.platformAuto, a.prompts = true, "画一只猫"
	launched := 0
	a.loginLaunch = func(store.Browser) error { launched++; return nil }
	tt := ui.NewTester(a.view, 1280, 840)
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	if !a.canStart() {
		t.Fatal("task fixture was not ready before the browser operation")
	}
	b := a.browserRows[0]
	batch := &browserBatchRun{total: 1}
	a.browserBatch = batch
	if !a.beginBrowserOperation(b, false, batch) || a.beginBrowserOperation(b, false, nil) {
		t.Fatal("pending operation did not prevent a duplicate")
	}
	a.store.UpdateBrowser(b.ID, false, false)
	a.accountProbe = func(store.Browser) (string, error) { return wizard.ProbeOut, nil }
	a.refreshBrowsers()
	if len(a.browserRows) != 3 {
		t.Fatal("refresh deleted an environment while it was starting")
	}
	tt.Frame()
	for _, label := range []string{"批量启动", "批量停止", "启动 " + a.browserRows[2].Name, "删除当前"} {
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	a.runBrowserBatch(false)
	if launched != 0 || len(a.browserPending) != 1 || a.canStart() {
		t.Fatal("pending browser operation allowed conflicting work")
	}
	a.store.ConfirmLogin(b.ID)
	a.finishBrowserOperation(b.ID, errors.New("测试失败"))
	if len(a.browserPending) != 0 || batch.done != 1 || batch.failed != 1 || !a.canStart() {
		t.Fatalf("failed operation did not finish: %+v", batch)
	}
	a.running = true
	a.runBrowserBatch(false)
	a.launchBrowserInstance(b, nil)
	a.stopBrowserInstance(a.browserRows[1], nil)
	if launched != 0 || len(a.browserPending) != 0 || !a.store.Browsers()[1].IsRunning {
		t.Fatal("running tasks did not block browser operations")
	}
}

func TestBrowserBatchAsyncFailureReturnsToUI(t *testing.T) {
	a := browserTableTestApp(t, 3)
	a.manager = browser.NewManager()
	tt := ui.NewTester(a.view, 1280, 840)
	for _, i := range []int{0, 2} {
		a.browserRows[i].Proxy = "invalid"
		a.browserChecked.Add(a.browserRows[i].ID)
	}
	a.runBrowserBatch(false)
	if len(a.browserPending) != 2 {
		t.Fatal("batch did not mark both environments as starting")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		a.mu.Lock()
		queued := len(a.uiUpdates)
		a.mu.Unlock()
		if queued == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("browser startup errors did not reach the UI")
		}
		time.Sleep(time.Millisecond)
	}
	if len(a.browserPending) != 2 || a.browserBatch.done != 0 {
		t.Fatal("background startup changed UI state directly")
	}
	tt.Frame()
	if len(a.browserPending) != 0 || a.browserBatch.done != 2 || a.browserBatch.failed != 2 {
		t.Fatalf("startup errors left a pending batch: %+v", a.browserBatch)
	}
}
