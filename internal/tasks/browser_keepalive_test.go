package tasks

import (
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/extbridge"
)

func TestWebLauncherFailureLeavesTasksUndispatched(t *testing.T) {
	st := openTaskStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	addNamed(t, st, "豆包", "测试")
	e := NewEngine(st, browser.NewManagerWithConfig(t.TempDir()), nil, "")
	t.Cleanup(e.CloseSessions)
	opts := Options{Platforms: []string{"豆包"}, Prompts: []string{"测试一", "测试二"}, RetryCount: 3}
	err := e.Run(t.Context(), opts, nil, func(Progress) {})
	if err == nil || !strings.Contains(err.Error(), "Camoufox 尚未准备好") {
		t.Fatalf("missing startup diagnosis: %v", err)
	}
	queue := e.View().Snapshot()
	if len(queue) != len(opts.Prompts) {
		t.Fatalf("queue length %d", len(queue))
	}
	for _, job := range queue {
		if !job.Undispatched || job.Status != StatusSkipped || job.Error != err.Error() || job.Attempts != 0 || !job.StartedAt.IsZero() {
			t.Fatalf("startup failure dispatched a job or lost its reason: %+v", job)
		}
	}
}

func TestBoundAddonDirCreatedOnceAndReused(t *testing.T) {
	st := openTaskStore(t)
	st.Set(extbridge.SettingExtPort, freePort(t))
	st.Set(extbridge.SettingExtToken, "test-token")
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	row := addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, browser.NewManager(), nil, "")
	t.Cleanup(func() { e.CloseSessions() })

	first, err := e.BoundAddonDir(row)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("bound mode must produce an addon dir")
	}
	if fi, err := os.Stat(first); err != nil || !fi.IsDir() {
		t.Fatalf("addon dir missing: %v", err)
	}
	second, err := e.BoundAddonDir(row)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("addon dir recreated: %q then %q", first, second)
	}

	// 外部扩展账号和未开绑定模式时都不产出目录。
	if ext, err := st.UpsertExtensionAccount("豆包", "external", "外部"); err != nil {
		t.Fatal(err)
	} else if dir, err := e.BoundAddonDir(ext); err != nil || dir != "" {
		t.Fatalf("external account addon=%q err=%v", dir, err)
	}
	st.Set(extbridge.SettingBrowserBackend, "camoufox")
	if dir, err := e.BoundAddonDir(row); err != nil || dir != "" {
		t.Fatalf("non-bound addon=%q err=%v", dir, err)
	}
}

func TestCloseRunBrowsersKeepsHeadlessBoundBrowserUntilIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := openTaskStore(t)
		bound := addNamed(t, st, "豆包", "甲")
		plain := addNamed(t, st, "豆包", "乙")
		e := NewEngine(st, browser.NewManager(), nil, "")
		e.idleAfter = time.Minute
		e.keepAlive = func(id int64) bool { return id == bound.ID }
		keepDir := t.TempDir()
		dropDir := t.TempDir()
		e.runBrowsers = map[int64]string{bound.ID: keepDir, plain.ID: dropDir}
		e.firefoxAddons = map[int64]string{bound.ID: keepDir, plain.ID: dropDir}
		e.mu.Lock()
		e.running = true
		e.mu.Unlock()

		e.closeRunBrowsers()

		if _, err := os.Stat(dropDir); !os.IsNotExist(err) {
			t.Fatal("plain addon dir must be removed")
		}
		if _, ok := e.firefoxAddons[plain.ID]; ok {
			t.Fatal("plain addon entry must be cleared")
		}
		if _, err := os.Stat(keepDir); err != nil {
			t.Fatal("bound headless addon dir must survive the run")
		}
		if e.idleClose[bound.ID] == nil {
			t.Fatal("idle close must be armed for the kept browser")
		}

		// 引擎停稳后快进虚拟时钟：空闲定时器触发，收掉保活浏览器。
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
		time.Sleep(90 * time.Second)
		synctest.Wait()

		if _, err := os.Stat(keepDir); !os.IsNotExist(err) {
			t.Fatal("idle close must remove the kept addon dir")
		}
		if _, ok := e.firefoxAddons[bound.ID]; ok {
			t.Fatal("idle close must clear the addon entry")
		}
		if e.idleClose[bound.ID] != nil {
			t.Fatal("idle close timer must be cleared")
		}
		if st.Browsers()[0].IsRunning || st.Browsers()[1].IsRunning {
			t.Fatal("idle close must mark the account not running")
		}
	})
}

func TestIdleCloseDefersToViewer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		st := openTaskStore(t)
		bound := addNamed(t, st, "豆包", "甲")
		e := NewEngine(st, browser.NewManager(), nil, "")
		e.idleAfter = time.Minute
		var viewed atomic.Bool
		viewed.Store(true)
		e.SetIdleGuard(func(id int64) bool { return id == bound.ID && viewed.Load() })
		keepDir := t.TempDir()
		e.firefoxAddons = map[int64]string{bound.ID: keepDir}

		// 面板还在看：第一轮只顺延。
		e.armIdleClose(bound.ID)
		time.Sleep(90 * time.Second)
		synctest.Wait()
		if _, err := os.Stat(keepDir); err != nil {
			t.Fatal("viewed browser must stay open")
		}
		if e.idleClose[bound.ID] == nil {
			t.Fatal("viewed browser must be rescheduled, not dropped")
		}

		// 面板关了：下一轮到点真关。
		viewed.Store(false)
		time.Sleep(90 * time.Second)
		synctest.Wait()
		if _, err := os.Stat(keepDir); !os.IsNotExist(err) {
			t.Fatal("unviewed idle browser must be closed")
		}
	})
}

func TestIdleBrowserExpiresDuringUnrelatedBatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		prev, current := t.TempDir(), t.TempDir()
		e := &Engine{
			running: true, firefoxAddons: map[int64]string{1: prev, 2: current},
			runBrowsers: map[int64]string{2: current}, active: map[int64]bool{2: true},
			idleAfter: time.Minute,
		}
		e.armIdleClose(1)
		e.armIdleClose(2)
		time.Sleep(time.Minute + time.Second)
		synctest.Wait()
		if _, err := os.Stat(prev); !os.IsNotExist(err) {
			t.Fatalf("previous batch's idle browser survived: %v", err)
		}
		if _, err := os.Stat(current); err != nil || e.idleClose[2] == nil {
			t.Fatalf("active browser was lost: %v", err)
		}
		e.setActive(2, false)
		time.Sleep(time.Minute + time.Second)
		synctest.Wait()
		if _, err := os.Stat(current); !os.IsNotExist(err) {
			t.Fatalf("released account never expired: %v", err)
		}
	})
}

func TestBoundBrowserBudgetProtectsRenewedViewingAndActiveAccounts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := &Engine{
			opts: Options{Platforms: []string{"豆包"}}, firefoxAddons: map[int64]string{},
			active: map[int64]bool{3: true}, idleGuard: func(id int64) bool { return id == 1 },
		}
		defer e.CloseSessions()
		for id := int64(1); id <= 4; id++ {
			e.firefoxAddons[id] = t.TempDir()
			e.armIdleClose(id)
			time.Sleep(time.Second)
		}
		stale := e.idleClose[4]
		e.armIdleClose(4)
		e.closeIdleBrowser(stale, false)
		if e.firefoxAddons[4] == "" {
			t.Fatal("stale timer closed a renewed browser")
		}
		e.trimIdleBrowsers()
		if len(e.firefoxAddons) != 2 || e.firefoxAddons[1] == "" || e.firefoxAddons[3] == "" {
			t.Fatalf("auto budget removed a viewed/active browser: %v", e.firefoxAddons)
		}
		e.opts.Concurrency = 1
		e.SetIdleGuard(nil)
		e.trimIdleBrowsers()
		if len(e.firefoxAddons) != 1 || e.firefoxAddons[3] == "" {
			t.Fatalf("manual concurrency cap: %v", e.firefoxAddons)
		}
	})
}
