package tasks

import (
	"os"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/extbridge"
)

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
		e.keepHeadless = func(id int64) bool { return id == bound.ID }
		keepDir := t.TempDir()
		dropDir := t.TempDir()
		e.runBrowsers = map[int64]struct{}{bound.ID: {}, plain.ID: {}}
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

func TestIdleCloseDefersToRunningEngineAndViewer(t *testing.T) {
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
