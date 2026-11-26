package browser_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
)

func TestManagedFingerprintAccountSessions(t *testing.T) {
	config := os.Getenv("TULAI_FINGERPRINT_TEST_DATA")
	if _, ok := browser.InstalledRuntime(config, browser.KindFingerprint); !ok {
		t.Skip("requires a tulai-managed Fingerprint test runtime")
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	manager := browser.NewManagerWithConfig(config)
	t.Cleanup(manager.StopAll)
	root := t.TempDir()
	db, err := store.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><input type="file"><canvas id="c" width="120" height="40"></canvas><script>window.__pageLeaks = () => Object.getOwnPropertyNames(window).filter(k => /tulai/i.test(k));</script></html>`))
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "image.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	var canvasHashes []string
	for _, name := range []string{"fp-first", "fp-second"} {
		row, err := db.AddManagedBrowser("豆包", name, browser.ProfileDir(root, browser.KindFingerprint, name), browser.KindFingerprint)
		if err != nil {
			t.Fatal(err)
		}
		endpoint, err := manager.StartTask(t.Context(), row, root, browser.KindFingerprint, server.URL)
		if err != nil {
			t.Fatal(err)
		}
		client, err := cdp.NewClient(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		session, err := client.NewBackgroundSession(server.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(session.Close)
		if !session.EvaluateBool("window.__tulaiBaseInstalled === 4 && localStorage.getItem('owner') === null") {
			t.Fatal("missing bridge or shared profile")
		}
		if !session.EvaluateBool("navigator.webdriver === false && window.__pageLeaks().length === 0 && Object.getOwnPropertyNames(document.defaultView).filter(k => /tulai/i.test(k)).length === 0") {
			t.Fatal("stealth leak detected on Fingerprint account")
		}
		session.Handle("echo", func(value string) string { return value })
		var echo string
		if err := session.EvaluateContext(t.Context(), "window.tulaiCall('echo','hello')", &echo); err != nil || echo != "hello" {
			t.Fatalf("page bridge returned %q: %v", echo, err)
		}
		var hash string
		if err := session.EvaluateContext(t.Context(), `(() => {
			const c = document.getElementById('c');
			const g = c.getContext('2d');
			g.fillStyle = 'rgb(20, 40, 60)';
			g.fillRect(0, 0, 120, 40);
			g.fillStyle = '#00cc88';
			g.font = '16px sans-serif';
			g.fillText('AccountFP', 8, 25);
			return c.toDataURL('image/png');
		})()`, &hash); err != nil || hash == "" {
			t.Fatalf("canvas hash failed: %q %v", hash, err)
		}
		canvasHashes = append(canvasHashes, hash)
		if err := session.SetUploadFilesNth(0, []string{file}); err != nil {
			t.Fatal(err)
		}
		if !session.EvaluateBool("document.querySelector('input').files[0].name === 'image.txt'") {
			t.Fatal("file not uploaded")
		}
		if _, err := session.Evaluate("localStorage.setItem('owner','test')"); err != nil {
			t.Fatal(err)
		}
		png, err := cdp.CapturePNG(t.Context(), endpoint)
		if err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
			t.Fatalf("capture: %v", err)
		}
		if err := browser.RemoveProfile(root, browser.KindFingerprint, row); err == nil {
			t.Fatal("deleted an active account")
		}
		manager.Stop(row.ID)
		if manager.IsRunning(row.ID) {
			t.Fatal("account still running")
		}
	}
	if len(canvasHashes) == 2 && canvasHashes[0] == canvasHashes[1] {
		t.Fatal("two distinct Fingerprint accounts produced identical canvas hashes")
	}
}
