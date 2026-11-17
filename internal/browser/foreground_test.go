package browser

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestHoldForegroundUnlocksAfterLastRelease(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls []bool
		old := foregroundLock
		foregroundLock = func(on bool) { calls = append(calls, on) }
		defer func() { foregroundLock = old }()

		first := holdForeground()
		second := holdForeground()
		first()
		first() // 重复释放只算一次
		time.Sleep(foregroundSettle + time.Second)
		synctest.Wait()
		if len(calls) != 2 || !calls[0] || !calls[1] {
			t.Fatalf("still held by second start, calls=%v", calls)
		}
		second()
		time.Sleep(foregroundSettle + time.Second)
		synctest.Wait()
		if len(calls) != 3 || calls[2] {
			t.Fatalf("last release must unlock, calls=%v", calls)
		}
	})
}

func TestTaskVisibleToggle(t *testing.T) {
	m := NewManager()
	if m.TaskVisible() {
		t.Fatal("task windows must default to hidden")
	}
	m.SetTaskVisible(true)
	if !m.TaskVisible() {
		t.Fatal("SetTaskVisible(true) not kept")
	}
	if m.LoadedAddon(1) != "" {
		t.Fatal("no browser running, addon must be empty")
	}
	var nilManager *Manager
	nilManager.SetTaskVisible(true)
	if nilManager.TaskVisible() {
		t.Fatal("nil manager must report hidden")
	}
}
