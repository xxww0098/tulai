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

func TestManagedChromeAccountSessions(t *testing.T) {
	config := os.Getenv("TULAI_CHROME_TEST_DATA")
	if _, ok := browser.InstalledRuntime(config, browser.KindChromeCDP); !ok {
		t.Skip("requires a tulai-managed Chrome test runtime")
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
		_, _ = w.Write([]byte(`<html><input type="file"></html>`))
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "image.txt")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second"} {
		row, err := db.AddManagedBrowser("豆包", name, browser.ProfileDir(root, browser.KindChromeCDP, name), browser.KindChromeCDP)
		if err != nil {
			t.Fatal(err)
		}
		// The saved account owns its engine even if the next new account uses Camoufox.
		endpoint, err := manager.StartTask(t.Context(), row, root, browser.KindCamoufox, server.URL)
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
		if !session.EvaluateBool("window.__tulaiBaseInstalled === 3 && localStorage.getItem('owner') === null") {
			t.Fatal("missing bridge or shared profile")
		}
		session.Handle("echo", func(value string) string { return value })
		var echo string
		if err := session.EvaluateContext(t.Context(), "window.tulaiCall('echo','hello')", &echo); err != nil || echo != "hello" {
			t.Fatalf("page bridge returned %q: %v", echo, err)
		}
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
		if err := browser.RemoveProfile(root, browser.KindCamoufox, row); err == nil {
			t.Fatal("deleted an active account")
		}
		manager.Stop(row.ID)
		if manager.IsRunning(row.ID) {
			t.Fatal("account still running")
		}
	}
}
