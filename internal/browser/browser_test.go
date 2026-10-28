package browser

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/store"
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
	builtin := ProfileDir("BrowserData", "Builtin", "千问")
	if builtin != filepath.Join("BrowserData", "Builtin", "千问") || builtin == chrome || builtin == edge {
		t.Fatalf("builtin profile = %s", builtin)
	}
	if ProfileDir("BrowserData", "内置浏览器", "千问") != builtin {
		t.Fatal("内置浏览器 profile")
	}
	if ProfileDir("BrowserData", "Chromium", "千问") != chrome {
		t.Fatal("chromium shares the chrome profile directory")
	}
	if pageBackend("ego lite") != "Chrome" || pageBackend("Edge") != "Edge" || pageBackend("Builtin") != "Builtin" || pageBackend("内置浏览器") != "Builtin" {
		t.Fatal("backend normalize")
	}
}

func TestDebugArgsUseLoginURLAndSkipAutomation(t *testing.T) {
	userData := filepath.Join("BrowserData", "Edge", "千问")
	login := "https://www.doubao.com/chat/"
	args := debugArgs(9333, userData, login)
	if args[len(args)-1] != login {
		t.Fatalf("start url = %q, args %v", args[len(args)-1], args)
	}
	joined := strings.Join(args, "\n")
	for _, want := range []string{
		"--remote-debugging-port=9333",
		"--user-data-dir=" + userData,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-features=Translate,msImplicitSignin,msEdgeFirstRunExperience,EdgeSignInPromo",
		"--disable-infobars",
		"--hide-crash-restore-bubble",
		"--disable-session-crashed-bubble",
		"--no-service-autorun",
		"--edge-skip-compat-layer-relaunch",
		"--remote-allow-origins=*",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %v", want, args)
		}
	}
	if strings.Contains(joined, "about:blank") || strings.Contains(joined, "--enable-automation") || strings.Contains(joined, "--headless") {
		t.Fatalf("blank or automation flag leaked: %v", args)
	}
	withExtra := launchArgs(9333, userData, login, "", []string{"--disable-gpu"})
	withExt := launchArgs(9333, userData, login, filepath.Join(userData, "ext"), nil)
	if withExt[len(withExt)-1] != login {
		t.Fatalf("extension flags ate the url: %v", withExt)
	}
	joinedExt := strings.Join(withExt, "\n")
	if !strings.Contains(joinedExt, "--load-extension="+filepath.Join(userData, "ext")) || !strings.Contains(joinedExt, "--enable-unsafe-extension-debugging") {
		t.Fatalf("extension flags: %v", withExt)
	}
	if strings.Contains(joinedExt, "--enable-automation") || strings.Contains(joinedExt, "--disable-extensions-except") || strings.Contains(joinedExt, "--disable-extensions\n") || strings.Contains(joinedExt, "--disable-extensions ") {
		t.Fatalf("extension flags disabled extensions or automation: %v", withExt)
	}
	if withExtra[len(withExtra)-1] != login || !strings.Contains(strings.Join(withExtra, "\n"), "--disable-gpu") {
		t.Fatalf("extra flags: %v", withExtra)
	}
}

func TestStartReusesLiveDebuggerInsteadOfSpawning(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://127.0.0.1/devtools/browser/already",
		})
	})
	go http.Serve(ln, mux)
	m := NewManager()
	b := store.Browser{ID: 7, Name: "账号1", Port: port, UserDataDir: t.TempDir()}
	ws, err := m.Start(b, t.TempDir(), KindEdge, "https://www.doubao.com/chat/", false)
	if err != nil || ws != "ws://127.0.0.1/devtools/browser/already" {
		t.Fatalf("reuse ws=%q err=%v", ws, err)
	}
	if m.IsRunning(7) || m.pid(7) != 0 {
		t.Fatal("reused debugger should not spawn a browser")
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
