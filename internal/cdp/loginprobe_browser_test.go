package cdp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/camoufoxtest"
)

func TestLoginProbeWaitsForLoginRegisterButton(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<button id="login">登录/注册</button><textarea></textarea>`)
	}))
	defer server.Close()
	proc, endpoint := camoufoxtest.Start(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var state string
	for state == "" || state == "unknown" {
		var err error
		state, err = ProbeLogin(ctx, endpoint)
		if err != nil {
			t.Fatal(err)
		}
		if state == "unknown" {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if state != "out" {
		t.Fatalf("login button: state=%q", state)
	}
	if err := proc.Call(ctx, proc.Page, "Runtime.evaluate", map[string]any{
		"expression": `document.getElementById('login').remove(); document.body.insertAdjacentHTML('beforeend', '<div class="avatar"></div>')`,
	}, nil); err != nil {
		t.Fatal(err)
	}
	if state, err := ProbeLogin(ctx, endpoint); err != nil || state != "in" {
		t.Fatalf("logged in: state=%q err=%v", state, err)
	}
}
