package camoufox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestChromeLockMatchesManagedDriver(t *testing.T) {
	if err := writeChromePackageLock(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, ok := InstalledChrome(t.TempDir()); ok {
		t.Fatal("empty directory reported as installed")
	}
}

// Set TULAI_CHROME_TEST_INSTALL=1 to install into a test-owned runtime. The
// profile and fixture are always isolated from the user's application data.
func TestManagedChromeCDPLifecycle(t *testing.T) {
	dir := os.Getenv("TULAI_CHROME_TEST_DATA")
	if os.Getenv("TULAI_CHROME_TEST_INSTALL") == "1" {
		if dir == "" {
			dir = t.TempDir()
		}
		ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
		defer cancel()
		if _, err := EnsureChrome(ctx, dir, "", func(p Progress) {
			if p.Done == 0 {
				t.Log(p.Text)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := InstalledChrome(dir); !ok {
		t.Skip("requires a tulai-managed Chrome test runtime")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><title>CDP fixture</title></html>"))
	}))
	defer server.Close()
	profile := filepath.Join(t.TempDir(), "Chrome", "account-test")
	for attempt := 0; attempt < 2; attempt++ {
		p, err := Start(t.Context(), dir, Options{ChromeCDP: true, Headless: true, Profile: profile, DirectNetwork: true}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		Register(9999, p)
		t.Cleanup(func() { Unregister(9999, p) })
		if proc, err := Find(ChromeEndpoint(9999)); err != nil || proc != p {
			t.Fatalf("lookup: %v", err)
		}
		if err := p.Call(t.Context(), p.Page, "Page.navigate", map[string]string{"url": server.URL}, nil); err != nil {
			t.Fatal(err)
		}
		var out struct{ Result struct{ Value string } }
		expr := "document.title + ' ' + navigator.userAgent.includes('Chrome/') + ' ' + localStorage.getItem('account')"
		if err := p.Call(t.Context(), p.Page, "Runtime.evaluate", map[string]string{"expression": expr}, &out); err != nil {
			t.Fatal(err)
		}
		want := "CDP fixture true null"
		if attempt > 0 {
			want = "CDP fixture true persisted"
		}
		if out.Result.Value != want {
			t.Fatalf("page result %q, want %q", out.Result.Value, want)
		}
		if err := p.Call(t.Context(), p.Page, "Runtime.evaluate", map[string]string{"expression": "localStorage.setItem('account','persisted')"}, nil); err != nil {
			t.Fatal(err)
		}
		p.Close()
		if _, err := Find(ChromeEndpoint(9999)); err == nil {
			t.Fatal("closed account still available")
		}
	}
}
