package download

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/chromedp"
)

type downloadNavigationExecutor func(string, any, any) error

func (fn downloadNavigationExecutor) Execute(_ context.Context, method string, params, result any) error {
	return fn(method, params, result)
}

func TestDownloadPageNavigation(t *testing.T) {
	for _, tc := range []struct {
		name, input, entry, replacement string
		wantErr                         bool
	}{
		{"guide homepage", "https://pinterest.com", "https://www.pinterest.com/", "", false},
		{"homepage query", "https://pinterest.com/?ref=guide", "https://www.pinterest.com/?ref=guide", "https://www.pinterest.com/login/", false},
		{"pin link", "https://pinterest.com/pin/12345/", "https://pinterest.com/pin/12345/", "", false},
		{"other site", "https://pinterest.com/", "https://www.pinterest.com/", "https://pinterest.com.example.test/", true},
		{"downgrade", "https://pinterest.com/", "https://www.pinterest.com/", "http://www.pinterest.com/", true},
		{"error page", "https://example.test/", "https://example.test/", "about:neterror", true},
		{"credentials", "https://example.test/", "https://example.test/", "https://user@example.test/login", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			target, err := url.Parse(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			executor := downloadNavigationExecutor(func(method string, payload, result any) error {
				params := payload.(map[string]any)
				if method != "Page.navigate" || params["url"] != tc.entry || params["recoverInterrupted"] != true {
					t.Fatalf("unexpected navigation: %s %v", method, params)
				}
				if (params["wait"] == "commit") != (pinterestHost(target) && (target.Path == "" || target.Path == "/")) {
					t.Fatalf("unexpected wait for %s: %v", target, params["wait"])
				}
				data, err := json.Marshal(map[string]string{"replacementURL": tc.replacement})
				if err != nil {
					return err
				}
				return json.Unmarshal(data, result)
			})
			err = navigateDownloadPage(target).Do(cdp.WithExecutor(t.Context(), executor))
			if (err != nil) != tc.wantErr || target.String() != tc.input {
				t.Fatalf("navigation changed %s to %s or returned an unexpected error: %v", tc.input, target, err)
			}
		})
	}
}

func TestDedicatedDownloadPageReplacesPendingNavigation(t *testing.T) {
	data := dedicatedFixtureData(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pending := make(chan struct{}, 1)
	triggerArmed := make(chan struct{}, 1)
	trigger := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/entry":
			http.Redirect(w, r, "/home", http.StatusFound)
		case "/trigger":
			select {
			case triggerArmed <- struct{}{}:
			default:
			}
			select {
			case <-trigger:
				w.Header().Set("Content-Type", "text/plain")
				_, _ = w.Write([]byte("go"))
			case <-r.Context().Done():
			case <-ctx.Done():
			}
		case "/pending":
			select {
			case pending <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-ctx.Done():
			}
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<title>Download fixture</title>"))
		}
	}))
	defer server.Close()
	session, err := openDedicatedFixture(t, data, filepath.Join(t.TempDir(), "profile"), server.URL+"/entry", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	// Arm an in-page replacement trigger before starting /pending: Chromium's CDP
	// queues concurrent Runtime.evaluate calls behind an in-flight Page.navigate,
	// whereas real page/script redirects run on the active document immediately.
	if err := session.run(ctx, chromedp.Evaluate(`void fetch('/trigger').then(() => location.replace('/landing'))`, nil)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-triggerArmed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	opened := make(chan error, 1)
	go func() { opened <- session.OpenTarget(ctx, server.URL+"/pending") }()
	select {
	case <-pending:
	case err := <-opened:
		t.Fatalf("navigation finished before reaching the server: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(trigger)
	if err := <-opened; err != nil {
		t.Fatal(err)
	}
	var current string
	if err := session.run(ctx, chromedp.Location(&current)); err != nil || current != server.URL+"/landing" || !session.Alive() {
		t.Fatalf("replacement tab = %q, alive = %v, err = %v", current, session.Alive(), err)
	}
	stopped, stop := context.WithCancel(ctx)
	stop()
	if err := session.OpenTarget(stopped, server.URL+"/pending"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled navigation returned %v", err)
	}
}
