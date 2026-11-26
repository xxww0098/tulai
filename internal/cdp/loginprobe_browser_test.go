package cdp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/fingerprinttest"
)

func TestLoginProbeWaitsForLoginRegisterButton(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<button id="login">登录/注册</button><textarea></textarea>`)
	}))
	defer server.Close()
	proc, endpoint := fingerprinttest.Start(t, server.URL)
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

func TestProbeAccountUsernameDoubaoAndQwen(t *testing.T) {
	pages := map[string]struct {
		html string
		want string
	}{
		"/doubao-dom": {
			html: `<!doctype html><html><body>
				<div data-sidebar-footer-account-menu="true">
					<button type="button" aria-haspopup="menu" title="⌃⇧,">
						<div>
							<img data-testid="chat_header_avatar_button" src="data:image/gif;base64,R0lGODlhAQABAAAAACw=">
							<div><div><span>xxww</span><svg></svg></div></div>
						</div>
					</button>
				</div>
			</body></html>`,
			want: "xxww",
		},
		"/doubao-router": {
			html: `<!doctype html><html><body><script>
				window._ROUTER_DATA = { loaderData: { chat_layout: { chat_layout: {
					accountInfo: { data: { is_visitor_account: false, screen_name: "豆包创作者" } }
				}}}};
			</script></body></html>`,
			want: "豆包创作者",
		},
		"/doubao-visitor": {
			html: `<!doctype html><html><body><script>
				window._ROUTER_DATA = { loaderData: { chat_layout: { chat_layout: {
					accountInfo: { data: { is_visitor_account: true, screen_name: "访客账号" } }
				}}}};
			</script></body></html>`,
			want: "",
		},
		"/qwen-dom": {
			html: `<!doctype html><html><body>
				<aside id="new-nav-tab-wrapper">
					<div data-session-id="abc"><button type="button" data-session-switch-target="session-tools" aria-haspopup="menu">更多</button></div>
					<div><button type="button" aria-haspopup="menu">
						<img class="size-7 rounded-full" src="data:image/gif;base64,R0lGODlhAQABAAAAACw=">
						<span><span>Qwen3464</span></span>
					</button></div>
				</aside>
			</body></html>`,
			want: "Qwen3464",
		},
		"/qwen-global": {
			html: `<!doctype html><html><body><script>
				window._USER_ = { showName: "通义测试号", userId: "123456" };
			</script></body></html>`,
			want: "通义测试号",
		},
		"/logged-out": {
			html: `<!doctype html><html><body>
				<aside id="new-nav-tab-wrapper">
					<button type="button" aria-haspopup="menu"><img src="data:image/gif;base64,R0lGODlhAQABAAAAACw="><span>登录/注册</span></button>
				</aside>
			</body></html>`,
			want: "",
		},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if page, ok := pages[r.URL.Path]; ok {
			fmt.Fprint(w, page.html)
			return
		}
		fmt.Fprint(w, `<!doctype html><html><body>ok</body></html>`)
	}))
	defer server.Close()
	proc, endpoint := fingerprinttest.Start(t, server.URL+"/doubao-dom")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for _, path := range []string{"/doubao-dom", "/doubao-router", "/doubao-visitor", "/qwen-dom", "/qwen-global", "/logged-out"} {
		tc := pages[path]
		t.Run(path, func(t *testing.T) {
			if err := proc.Call(ctx, proc.Page, "Page.navigate", map[string]any{"url": server.URL + path}, nil); err != nil {
				t.Fatalf("navigate %s: %v", path, err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				var ready struct {
					Result struct {
						Value bool `json:"value"`
					} `json:"result"`
				}
				expr := fmt.Sprintf(`location.pathname === %q && document.readyState === 'complete'`, path)
				if err := proc.Call(ctx, proc.Page, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true}, &ready); err == nil && ready.Result.Value {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("timed out waiting for %s", path)
				}
				time.Sleep(25 * time.Millisecond)
			}
			got, err := ProbeAccountUsername(ctx, endpoint)
			if err != nil {
				t.Fatalf("ProbeAccountUsername(%s): %v", path, err)
			}
			if got != tc.want {
				t.Fatalf("ProbeAccountUsername(%s) = %q, want %q", path, got, tc.want)
			}
		})
	}
}
