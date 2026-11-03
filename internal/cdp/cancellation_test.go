package cdp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/camoufoxtest"
)

func TestCanceledEvaluationClosesOnlyItsPage(t *testing.T) {
	started := make(chan struct{}, 1)
	var effects atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/started":
			started <- struct{}{}
		case "/effect":
			effects.Add(1)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("fixture"))
	}))
	defer server.Close()
	proc, _ := camoufoxtest.Start(t, server.URL)
	closed := make(chan struct{}, 1)
	unsubscribe := proc.Subscribe(func(event camoufox.Event) {
		if event.Event == "closed" && event.Target == proc.Page {
			closed <- struct{}{}
		}
	})
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- proc.Call(ctx, proc.Page, "Runtime.evaluate", map[string]any{"expression": `(async()=>{await fetch('/started');await new Promise(r=>setTimeout(r,800));await fetch('/effect')})()`}, nil)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("evaluation did not start")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("canceled evaluation succeeded")
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled script left its page running")
	}
	time.Sleep(time.Second)
	if effects.Load() != 0 {
		t.Fatal("canceled script performed a late action")
	}
	if !proc.Alive() {
		t.Fatal("cancel closed the account environment")
	}
	if err := proc.Call(context.Background(), "", "Target.createTarget", map[string]any{"url": "about:blank"}, nil); err != nil {
		t.Fatal(err)
	}
}
