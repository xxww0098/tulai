package browser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEdgeLocations(t *testing.T) {
	look, files := edgeLocations("windows", "", func(key string) string {
		switch key {
		case "PROGRAMFILES":
			return `C:\Program Files`
		case "PROGRAMFILES(X86)":
			return `C:\Program Files (x86)`
		case "LOCALAPPDATA":
			return `C:\Users\me\AppData\Local`
		default:
			return ""
		}
	})
	if len(look) != 0 || len(files) != 3 {
		t.Fatalf("windows locations: look=%v files=%v", look, files)
	}
	for _, want := range []string{
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Users\me\AppData\Local\Microsoft\Edge\Application\msedge.exe`,
	} {
		if !contains(files, want) {
			t.Fatalf("missing %s in %v", want, files)
		}
	}

	_, mac := edgeLocations("darwin", "/Users/me", os.Getenv)
	if !contains(mac, "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge") ||
		!contains(mac, "/Users/me/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge") {
		t.Fatalf("darwin locations: %v", mac)
	}

	linuxLook, linuxFiles := edgeLocations("linux", "", os.Getenv)
	if !contains(linuxLook, "microsoft-edge-stable") || !contains(linuxFiles, "/usr/bin/microsoft-edge") || !contains(linuxFiles, "/opt/microsoft/msedge/msedge") {
		t.Fatalf("linux locations: look=%v files=%v", linuxLook, linuxFiles)
	}
}

func TestFindOnDiskUsesFirstExistingFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "msedge")
	if err := os.WriteFile(bin, []byte("edge"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := findOnDisk(nil, []string{filepath.Join(dir, "missing"), bin}, ErrEdgeNotFound)
	if err != nil || got != bin {
		t.Fatalf("find = %q, %v", got, err)
	}
	if _, err := findOnDisk(nil, []string{filepath.Join(dir, "missing")}, ErrEdgeNotFound); err != ErrEdgeNotFound {
		t.Fatalf("missing edge err = %v", err)
	}
}

func TestProfileDirSeparatesEdgeFromChrome(t *testing.T) {
	chrome := ProfileDir("BrowserData", "Chrome", "千问")
	edge := ProfileDir("BrowserData", "Edge", "千问")
	if chrome != filepath.Join("BrowserData", "千问") {
		t.Fatalf("chrome profile = %s", chrome)
	}
	if edge != filepath.Join("BrowserData", "Edge", "千问") || chrome == edge {
		t.Fatalf("edge profile = %s", edge)
	}
	if pageBackend("ego lite") != "Chrome" || pageBackend("Edge") != "Edge" {
		t.Fatal("backend normalize")
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want || strings.HasSuffix(item, want) {
			return true
		}
	}
	return false
}
