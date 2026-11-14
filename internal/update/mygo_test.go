package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func TestMyGoSourceUsesReleaseAndMirrors(t *testing.T) {
	payload := []byte(`{"version":"0.2.0"}`)
	sum := sha256.Sum256(payload)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	defer primary.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RequestURI(), "update-darwin-universal.json") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer mirror.Close()
	client := NewClient("0.1.8")
	client.Mirrors = []string{mirror.URL}
	asset := Asset{Name: "update-darwin-universal.json", BrowserDownloadURL: primary.URL + "/update-darwin-universal.json", Size: int64(len(payload)), Digest: "sha256:" + hex.EncodeToString(sum[:])}
	src := &signedSource{client: client, release: Release{Tag: "v0.2.0", Assets: []Asset{asset}}, goos: "darwin", arch: "arm64"}
	body, err := src.Manifest(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	path := body.(*temporaryUpdateFile).Name()
	got, err := io.ReadAll(body)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("manifest %q: %v", got, err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("temporary directory was not removed: %v", err)
	}
	if _, err := src.Open(context.Background(), "https://evil.example/payload"); err == nil {
		t.Fatal("accepted an asset outside the release")
	}
	src.release.Assets[0].Size = 1<<20 + 1
	if _, err := src.Manifest(context.Background(), false); err == nil {
		t.Fatal("accepted an oversized manifest")
	}
}

func TestMyGoInstallLocations(t *testing.T) {
	t.Setenv("APPIMAGE", "")
	setupExeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(setupExeDir, "Uninstall.exe"), []byte("uninst"), 0o644); err != nil {
		t.Fatal(err)
	}
	mygoFilesDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mygoFilesDir, ".mygo-files"), []byte("tulai.exe\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	portableDir := t.TempDir()

	for _, tc := range []struct {
		os, path string
		want     bool
	}{
		{"darwin", "/Applications/tulai.app/Contents/MacOS/tulai", true},
		{"darwin", "/tmp/tulai", false},
		{"windows", `C:\Users\me\AppData\Local\Programs\tulai\tulai.exe`, true},
		{"windows", `C:\Users\me\AppData\Local\Programs\Documents\tulai.exe`, false},
		{"windows", filepath.Join(setupExeDir, "tulai.exe"), true},
		{"windows", filepath.Join(mygoFilesDir, "tulai.exe"), true},
		{"windows", filepath.Join(portableDir, "tulai.exe"), false},
		{"windows", `C:\Downloads\tulai-0.2.0-windows-amd64.exe`, false},
		{"linux", "/home/me/.local/tulai.app/tulai", true},
		{"linux", "/usr/bin/tulai", false},
		{"linux", "/home/me/Downloads/tulai", false},
	} {
		t.Run(tc.os+"/"+tc.path, func(t *testing.T) {
			if got := MyGoInstall(tc.os, tc.path); got != tc.want {
				t.Fatalf("MyGoInstall = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestShouldFallbackUnsigned(t *testing.T) {
	wrappedDisabled := fmt.Errorf("wrap: %w", mygo.ErrUpdatesDisabled)
	for _, tc := range []struct {
		name string
		goos string
		mode Mode
		err  error
		want bool
	}{
		{"windows-setup-disabled", "windows", ModeWindowsSetup, wrappedDisabled, true},
		{"windows-setup-signature-err", "windows", ModeWindowsSetup, errors.New("signature mismatch"), false},
		{"windows-exe-disabled", "windows", ModeWindowsExe, wrappedDisabled, false},
		{"darwin-disabled", "darwin", ModeMacBundle, wrappedDisabled, false},
		{"linux-disabled", "linux", ModeLinuxUser, wrappedDisabled, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldFallbackUnsigned(tc.goos, tc.mode, tc.err); got != tc.want {
				t.Fatalf("ShouldFallbackUnsigned = %v, want %v", got, tc.want)
			}
		})
	}
}
