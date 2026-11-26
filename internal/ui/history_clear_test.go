package ui

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestAutoDeleteHistorySettingPersistenceAndTaskOptions(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	svc := &SettingsService{app: app}
	ctx := context.Background()

	if got := svc.GetSettingsState(ctx); got.AutoDeleteHistory {
		t.Fatal("autoDeleteHistory should default to false")
	}

	svc.SetSettingField(ctx, "autoDeleteHistory", "true")
	if got := svc.GetSettingsState(ctx); !got.AutoDeleteHistory {
		t.Fatal("autoDeleteHistory should be true after SetSettingField")
	}
	if !st.GetBool("auto_delete_history") {
		t.Fatal("auto_delete_history should be persisted to sqlite")
	}

	var optVal bool
	_ = app.call(ctx, func() {
		optVal = app.taskOptions([]string{"豆包"}, []string{"测试"}, nil).AutoDeleteHistory
	})
	if !optVal {
		t.Fatal("taskOptions should propagate AutoDeleteHistory=true")
	}

	// 新 App 实例重读 SQLite 应恢复该开关
	app2 := NewApp(st, nil, nil, root)
	app2.StartActor()
	defer app2.StopActor()
	if got := (&SettingsService{app: app2}).GetSettingsState(ctx); !got.AutoDeleteHistory {
		t.Fatal("reopened app should restore autoDeleteHistory=true")
	}
}

func TestClearBrowserHistorySingleBatchAndGuards(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	b1, err := st.AddBrowser("豆包", "豆包1", filepath.Join(root, "p1"))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := st.AddBrowser("千问", "千问1", filepath.Join(root, "p2"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(b1.ID)
	st.ConfirmLogin(b2.ID)

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	svc := &BrowserService{app: app}
	ctx := context.Background()

	// 任务运行中不允许清理历史
	_ = app.call(ctx, func() { app.running = true })
	if err := svc.ClearBrowserHistory(ctx, []int64{b1.ID}, 0, true); err == nil {
		t.Fatal("expected error while tasks are running")
	}
	_ = app.call(ctx, func() { app.running = false })

	started := make(chan int64, 4)
	release := make(chan struct{})
	var gotKeep atomic.Int64
	var gotFast atomic.Bool
	_ = app.call(ctx, func() {
		app.clearHistoryFn = func(runCtx context.Context, b store.Browser, opts strategy.ClearHistoryOptions) (strategy.ClearHistoryResult, error) {
			gotKeep.Store(int64(opts.KeepRecent))
			gotFast.Store(opts.FastMode)
			started <- b.ID
			select {
			case <-release:
			case <-runCtx.Done():
				return strategy.ClearHistoryResult{Stopped: "cancelled"}, runCtx.Err()
			}
			if opts.OnProgress != nil {
				opts.OnProgress(2, "示例会话")
			}
			return strategy.ClearHistoryResult{Deleted: 2, FastDeleted: 1, Remaining: opts.KeepRecent}, nil
		}
	})

	if err := svc.ClearBrowserHistory(ctx, []int64{b1.ID, b2.ID}, 5, true); err != nil {
		t.Fatalf("ClearBrowserHistory failed: %v", err)
	}

	// 豆包与千问跨平台并行启动：释放前两个账号都应进入执行
	seen := map[int64]bool{}
	for range 2 {
		select {
		case id := <-started:
			seen[id] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for both platforms to start in parallel, seen=%v", seen)
		}
	}
	if !seen[b1.ID] || !seen[b2.ID] {
		t.Fatalf("expected both %d and %d to run in parallel, got %v", b1.ID, b2.ID, seen)
	}

	mid := svc.GetBrowserState(ctx)
	if !mid.BatchActive || mid.BatchKind != "clearing" {
		t.Fatalf("expected active clearing batch, got active=%v kind=%q text=%q", mid.BatchActive, mid.BatchKind, mid.BatchText)
	}
	for _, r := range mid.Rows {
		if (r.ID == b1.ID || r.ID == b2.ID) && (!r.Pending || r.PendingOp != "clearing") {
			t.Fatalf("row %d should be pending clearing, got pending=%v op=%q", r.ID, r.Pending, r.PendingOp)
		}
	}

	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		after := svc.GetBrowserState(ctx)
		if !after.BatchActive {
			if gotKeep.Load() != 5 || !gotFast.Load() {
				t.Fatalf("keepRecent=%d fast=%v, want 5 and true", gotKeep.Load(), gotFast.Load())
			}
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("clearing batch did not finish in time")
}
