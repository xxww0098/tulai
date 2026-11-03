package browser

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestBackgroundLaunchRefocusesAndLoginDoesNot(t *testing.T) {
	old := appFocusWaits
	appFocusWaits = []time.Duration{20 * time.Millisecond}
	t.Cleanup(func() { appFocusWaits = old })

	background := NewManager()
	var n atomic.Int32
	background.SetAppFocus(func() { n.Add(1) })
	background.KeepAppInFront()
	if n.Load() < 1 {
		t.Fatal("background launch did not refocus")
	}
	deadline := time.Now().Add(time.Second)
	for n.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n.Load() < 2 {
		t.Fatalf("follow-up refocus = %d", n.Load())
	}

	login := NewManager()
	var stolen atomic.Int32
	login.SetAppFocus(func() { stolen.Add(1) })
	if login.focusFn() == nil {
		t.Fatal("login focus hook missing")
	}
	if stolen.Load() != 0 {
		t.Fatal("login launch refocused tulai")
	}
}
