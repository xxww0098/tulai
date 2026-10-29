package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/heal"
	"github.com/xxww0098/tulai/internal/store"
)

func TestCrashRelaunchesThenStopsAtCap(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts.RetryCount = 1
	var relaunch, reload int
	e.onRecover = func(action heal.Action, id int64) {
		if id != 7 {
			t.Errorf("account = %d", id)
		}
		switch action {
		case heal.ActionRelaunch:
			relaunch++
		case heal.ActionReload:
			reload++
		}
	}
	calls := 0
	got := e.execute(context.Background(), Task{Index: 1, Platform: "豆包", BrowserID: 7}, func(context.Context, *Task) error {
		calls++
		return heal.ErrCrashed
	})
	if calls != 2 || relaunch != 1 || reload != 0 || got.Status != StatusFailed || got.Error != "浏览器崩溃，自动重启后仍失败" {
		t.Fatalf("crash heal calls=%d relaunch=%d reload=%d task=%+v", calls, relaunch, reload, got)
	}
	notes := e.AutoNotes()
	if len(notes) == 0 || !strings.Contains(notes[0], "账号7 浏览器崩溃，已自动重启") {
		t.Fatalf("notes = %v", notes)
	}
}

func TestCrashWithoutRetriesFailsOnce(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	relaunch := 0
	e.onRecover = func(action heal.Action, _ int64) {
		if action == heal.ActionRelaunch {
			relaunch++
		}
	}
	got := e.execute(context.Background(), Task{Platform: "豆包", BrowserID: 1}, func(context.Context, *Task) error {
		return heal.ErrCrashed
	})
	if relaunch != 0 || got.Status != StatusFailed || got.Attempts != 1 || got.Error != "浏览器崩溃了" {
		t.Fatalf("cap without retry: relaunch=%d task=%+v", relaunch, got)
	}
}

func TestStuckReloads(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts.RetryCount = 1
	reload := 0
	e.onRecover = func(action heal.Action, _ int64) {
		if action == heal.ActionReload {
			reload++
		}
	}
	calls := 0
	got := e.execute(context.Background(), Task{Platform: "千问", BrowserID: 3}, func(context.Context, *Task) error {
		calls++
		if calls == 1 {
			return heal.ErrStuck
		}
		return nil
	})
	if calls != 2 || reload != 1 || got.Status != StatusSuccess {
		t.Fatalf("stuck reload calls=%d reload=%d task=%+v notes=%v", calls, reload, got, e.AutoNotes())
	}
	if notes := strings.Join(e.AutoNotes(), "\n"); !strings.Contains(notes, "页面卡住，已刷新重试") {
		t.Fatalf("notes = %v", e.AutoNotes())
	}
}

func TestGenerateTimeoutDoesNotReload(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	reload := 0
	e.onRecover = func(action heal.Action, _ int64) {
		if action == heal.ActionReload {
			reload++
		}
	}
	got := e.execute(context.Background(), Task{Platform: "豆包", BrowserID: 1}, func(context.Context, *Task) error {
		return errors.New("出图超时，没有可保存的图片")
	})
	if reload != 0 || got.Status != StatusFailed || got.Error != "出图超时，没有可保存的图片" {
		t.Fatalf("timeout must fail clearly: reload=%d task=%+v", reload, got)
	}
}

func TestResultWaitHardCap(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts.WaitResultSeconds = 1800
	if e.resultWait() != 4*time.Minute {
		t.Fatalf("cap = %s", e.resultWait())
	}
	e.opts.WaitResultSeconds = 30
	if e.resultWait() != 30*time.Second {
		t.Fatalf("short = %s", e.resultWait())
	}
	e.opts.WaitResultSeconds = 0
	if e.resultWait() != 4*time.Minute {
		t.Fatalf("default = %s", e.resultWait())
	}
}

func TestNetworkStopsAtCap(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	got := e.execute(context.Background(), Task{Platform: "豆包", BrowserID: 1}, func(context.Context, *Task) error {
		return errors.New("websocket: connection refused")
	})
	if got.Status != StatusFailed || got.Attempts != 1 || got.Error != "网络中断，请重试" {
		t.Fatalf("network cap = %+v", got)
	}
}

func TestNetworkBackoffRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.opts.RetryCount = 1
		start := time.Now()
		calls := 0
		got := e.execute(context.Background(), Task{Platform: "豆包", BrowserID: 1}, func(context.Context, *Task) error {
			calls++
			if calls == 1 {
				return heal.ErrNetwork
			}
			return nil
		})
		if calls != 2 || got.Status != StatusSuccess || time.Since(start) != 2*time.Second {
			t.Fatalf("backoff calls=%d elapsed=%s task=%+v", calls, time.Since(start), got)
		}
	})
}

func TestVerificationSwitchesAfterSliderFails(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "千问", "账号1")
	second := addNamed(t, st, "千问", "账号2")
	e := NewEngine(st, nil, nil, "")
	slides := 0
	e.SetSlider(func(context.Context, store.Browser) error {
		slides++
		return errors.New("拖动后滑块仍在")
	})
	var focused int64
	e.SetLoginFocus(func(_ context.Context, b store.Browser) error {
		focused = b.ID
		return nil
	})
	err := e.run(context.Background(), Options{
		Platforms: []string{"千问"}, Images: []string{"a.png"}, Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		if task.BrowserID == first.ID {
			return captcha.ErrVerificationRequired
		}
		return nil
	})
	queue := e.View().Snapshot()
	if err != nil || slides != 1 || focused != first.ID || len(queue) != 1 || queue[0].Status != StatusSuccess || queue[0].BrowserID != second.ID {
		t.Fatalf("slider then switch: err=%v slides=%d focus=%d queue=%+v", err, slides, focused, queue)
	}
	text, id, ok := e.Attention()
	if !ok || id != first.ID || text != "账号1 需要完成安全验证" {
		t.Fatalf("banner = %q id=%d ok=%v", text, id, ok)
	}
	if row := browserByID(t, st, first.ID); row.Health != store.HealthCooling || row.Usable(time.Now()) {
		t.Fatalf("paused account = %+v", row)
	}
	if notes := strings.Join(e.AutoNotes(), "\n"); !strings.Contains(notes, "遇到滑块") || !strings.Contains(notes, "已换到其他账号") {
		t.Fatalf("notes = %v", e.AutoNotes())
	}
}

func TestSliderSuccessRetriesSameAccount(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "千问", "账号1")
	addNamed(t, st, "千问", "账号2")
	e := NewEngine(st, nil, nil, "")
	e.SetSlider(func(context.Context, store.Browser) error { return nil })
	calls := 0
	err := e.run(context.Background(), Options{
		Platforms: []string{"千问"}, Images: []string{"a.png"}, Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		calls++
		if calls == 1 && task.BrowserID == first.ID {
			return errNeedsVerify
		}
		return nil
	})
	queue := e.View().Snapshot()
	if err != nil || calls != 2 || len(queue) != 1 || queue[0].Status != StatusSuccess || queue[0].BrowserID != first.ID {
		t.Fatalf("slider should keep the account: calls=%d err=%v queue=%+v", calls, err, queue)
	}
	if notes := strings.Join(e.AutoNotes(), "\n"); !strings.Contains(notes, "滑块已通过，继续出图") {
		t.Fatalf("notes = %v", e.AutoNotes())
	}
}

func TestBusyAccountProbeCannotLogOut(t *testing.T) {
	st := openRotateStore(t)
	row := addNamed(t, st, "豆包", "豆包2")
	st.UpdateBrowser(row.ID, true, true)
	e := NewEngine(st, nil, nil, "")
	var focused int64
	e.SetLoginFocus(func(_ context.Context, b store.Browser) error {
		focused = b.ID
		return nil
	})
	e.setActive(row.ID, true)
	e.applyLogin(browserByID(t, st, row.ID), heal.StateOut)
	held := browserByID(t, st, row.ID)
	if !held.IsLoggedIn || held.Health == store.HealthExhausted || focused != 0 {
		t.Fatalf("busy account was logged out: %+v focus=%d", held, focused)
	}
	if notes := strings.Join(e.AutoNotes(), "\n"); strings.Contains(notes, "已退出登录") {
		t.Fatalf("notes = %v", e.AutoNotes())
	}
	e.setActive(row.ID, false)
	e.applyLogin(browserByID(t, st, row.ID), heal.StateOut)
	held = browserByID(t, st, row.ID)
	if held.IsLoggedIn || focused != row.ID {
		t.Fatalf("idle logout should stick: %+v focus=%d", held, focused)
	}
}

func TestLogoutProbeRestoresWithoutManualReset(t *testing.T) {
	st := openRotateStore(t)
	row := addNamed(t, st, "豆包", "账号2")
	st.UpdateBrowser(row.ID, true, true)
	e := NewEngine(st, nil, nil, "")
	e.SetLoginProbe(func(context.Context, store.Browser) (string, error) {
		return heal.StateUnknown, nil
	})
	var focused int64
	e.SetLoginFocus(func(_ context.Context, b store.Browser) error {
		focused = b.ID
		return nil
	})
	e.applyLogin(browserByID(t, st, row.ID), heal.StateOut)
	held := browserByID(t, st, row.ID)
	task := &Task{Platform: "豆包", BrowserID: row.ID}
	wait, dead := e.prepareAccount(task, time.Now())
	if dead || wait.IsZero() || held.Usable(time.Now()) || held.IsLoggedIn || held.Health != store.HealthExhausted {
		t.Fatalf("held = %+v wait=%s dead=%v", held, time.Until(wait), dead)
	}
	text, id, ok := e.Attention()
	if !ok || id != row.ID || text != "账号2 需要重新扫码登录" || focused != row.ID {
		t.Fatalf("banner=%q id=%d focus=%d ok=%v", text, id, focused, ok)
	}
	e.applyLogin(held, heal.StateOut)
	if again, _, ok := e.Attention(); !ok || again != text {
		t.Fatalf("repeat probe changed the banner: %q ok=%v", again, ok)
	}
	e.applyLogin(browserByID(t, st, row.ID), heal.StateIn)
	back := browserByID(t, st, row.ID)
	if !back.Usable(time.Now()) || !back.IsLoggedIn || back.Health != store.HealthReady {
		t.Fatalf("restored = %+v", back)
	}
	if _, _, ok := e.Attention(); ok {
		t.Fatal("banner should clear after login returns")
	}
	wait, dead = e.prepareAccount(task, time.Now())
	if dead || !wait.IsZero() {
		t.Fatalf("restored account should take work: wait=%s dead=%v", wait, dead)
	}
	if notes := strings.Join(e.AutoNotes(), "\n"); !strings.Contains(notes, "账号2 已退出登录，请重新扫码") || !strings.Contains(notes, "账号2 已恢复，回到轮换") {
		t.Fatalf("notes = %v", e.AutoNotes())
	}
}

func TestProbeLogoutWaitsThenDispatches(t *testing.T) {
	st := openRotateStore(t)
	row := addNamed(t, st, "豆包", "账号2")
	st.UpdateBrowser(row.ID, true, true)
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		var n int
		e.SetLoginProbe(func(context.Context, store.Browser) (string, error) {
			n++
			if n < 3 {
				return heal.StateOut, nil
			}
			return heal.StateIn, nil
		})
		e.SetLoginFocus(func(context.Context, store.Browser) error { return nil })
		calls := 0
		err := e.run(context.Background(), Options{
			Platforms: []string{"豆包"}, Images: []string{"a.png"}, Concurrency: 1,
		}, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
			calls++
			return nil
		})
		queue := e.View().Snapshot()
		if err != nil || calls != 1 || len(queue) != 1 || queue[0].Status != StatusSuccess || n < 3 {
			t.Fatalf("wait for login: err=%v calls=%d probes=%d queue=%+v", err, calls, n, queue)
		}
		if back := browserByID(t, st, row.ID); !back.Usable(time.Now()) || !back.IsLoggedIn {
			t.Fatalf("account after restore = %+v", back)
		}
	})
}

func TestProbeInClearsStaleLogout(t *testing.T) {
	for _, platform := range []string{"豆包", "千问"} {
		t.Run(platform, func(t *testing.T) {
			st := openRotateStore(t)
			row := addNamed(t, st, platform, platform+"2")
			st.UpdateBrowser(row.ID, true, false)
			st.UpdateBrowserHealth(row.ID, store.HealthExhausted, 0, "未登录")
			e := NewEngine(st, nil, nil, "")
			e.applyLogin(browserByID(t, st, row.ID), heal.StateIn)
			back := browserByID(t, st, row.ID)
			if !back.Usable(time.Now()) || !back.IsLoggedIn || back.Health != store.HealthReady || back.LastError != "" {
				t.Fatalf("confirm = %+v", back)
			}
			st.UpdateBrowser(row.ID, false, false)
			st.UpdateBrowserHealth(row.ID, store.HealthExhausted, 0, "未登录")
			e.applyLogin(browserByID(t, st, row.ID), heal.StateIn)
			back = browserByID(t, st, row.ID)
			if !back.Usable(time.Now()) || !back.IsLoggedIn || back.Health != store.HealthReady || back.LastError != "" {
				t.Fatalf("stale prev still blocked rotation: %+v", back)
			}
		})
	}
}

func TestLoginCheckSkipsWhenRecent(t *testing.T) {
	st := openRotateStore(t)
	row := addNamed(t, st, "豆包", "账号1")
	e := NewEngine(st, nil, nil, "")
	n := 0
	e.SetLoginProbe(func(context.Context, store.Browser) (string, error) {
		n++
		return heal.StateIn, nil
	})
	e.refreshLogin(context.Background(), row.ID, true)
	e.refreshLogin(context.Background(), row.ID, true)
	if n != 1 {
		t.Fatalf("second dispatch check should wait: probes=%d", n)
	}
	e.mu.Lock()
	e.loginLast[row.ID] = time.Now().Add(-heal.LoginMinGap)
	e.mu.Unlock()
	e.refreshLogin(context.Background(), row.ID, true)
	if n != 2 {
		t.Fatalf("check after the gap = %d", n)
	}
	if got := browserByID(t, st, row.ID); !got.IsLoggedIn {
		t.Fatalf("confirm should mark logged in: %+v", got)
	}
}
