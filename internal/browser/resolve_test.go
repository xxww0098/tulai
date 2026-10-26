package browser

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultBackendAndStoredValues(t *testing.T) {
	if DefaultBackend("windows") != KindEdge || DefaultBackend("darwin") != KindChrome || DefaultBackend("linux") != KindChrome {
		t.Fatal("default backend")
	}
	if NormalizeStoredBackend("windows", "") != KindEdge || NormalizeStoredBackend("windows", "ego lite") != KindEdge {
		t.Fatal("windows empty should become Edge")
	}
	if NormalizeStoredBackend("linux", "") != KindChrome || NormalizeStoredBackend("darwin", "ego lite") != KindChrome {
		t.Fatal("non-windows empty should become Chrome")
	}
	if NormalizeStoredBackend("windows", "Chrome") != KindChrome || NormalizeStoredBackend("linux", "Edge") != KindEdge {
		t.Fatal("explicit choice should stick")
	}
	if NormalizeStoredBackend("linux", "内置浏览器") != KindBuiltin || ChoiceLabel(KindBuiltin) != "内置浏览器" {
		t.Fatal("builtin label")
	}
	if !BuiltinSupported("windows", "amd64") || BuiltinSupported("windows", "arm64") || !BuiltinSupported("linux", "arm64") || !BuiltinSupported("darwin", "arm64") {
		t.Fatal("builtin platform support")
	}
}

func TestResolveSelectionFallback(t *testing.T) {
	edge := `C:\Program Files\Microsoft\Edge\Application\msedge.exe`
	chrome := `C:\Program Files\Google\Chrome\Application\chrome.exe`
	winEnv := func(key string) string {
		if key == "PROGRAMFILES" {
			return `C:\Program Files`
		}
		return ""
	}
	exists := func(paths ...string) func(string) error {
		set := map[string]bool{}
		for _, path := range paths {
			set[path] = true
		}
		return func(path string) error {
			if set[path] {
				return nil
			}
			return os.ErrNotExist
		}
	}
	none := func(string) (string, error) { return "", exec.ErrNotFound }

	t.Run("windows chrome falls back to edge", func(t *testing.T) {
		rt, err := resolve(lookup{goos: "windows", goarch: "amd64", env: winEnv, stat: exists(edge), lookPath: none}, KindChrome, "")
		if err != nil || rt.Kind != KindEdge || rt.Path != edge {
			t.Fatalf("got %+v %v", rt, err)
		}
	})
	t.Run("windows default prefers edge", func(t *testing.T) {
		rt, err := resolve(lookup{goos: "windows", goarch: "amd64", env: winEnv, stat: exists(edge, chrome), lookPath: none}, "", "")
		if err != nil || rt.Kind != KindEdge {
			t.Fatalf("got %+v %v", rt, err)
		}
	})
	t.Run("windows keeps chosen chrome", func(t *testing.T) {
		rt, err := resolve(lookup{goos: "windows", goarch: "amd64", env: winEnv, stat: exists(edge, chrome), lookPath: none}, KindChrome, "")
		if err != nil || rt.Kind != KindChrome || rt.Path != chrome {
			t.Fatalf("got %+v %v", rt, err)
		}
	})
	t.Run("windows arm64 ignores builtin", func(t *testing.T) {
		dir := t.TempDir()
		art, err := artifactFor("windows", "amd64")
		if err != nil {
			t.Fatal(err)
		}
		exe := installedExe(dir, art)
		if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(exe, []byte("cft"), 0o755); err != nil {
			t.Fatal(err)
		}
		rt, err := resolve(lookup{
			goos: "windows", goarch: "arm64", env: winEnv,
			stat: func(path string) error {
				if path == edge || path == exe {
					return nil
				}
				return os.ErrNotExist
			},
			lookPath: none,
		}, KindBuiltin, dir)
		if err != nil || rt.Kind != KindEdge {
			t.Fatalf("arm64 should use Edge, got %+v %v", rt, err)
		}
	})
	t.Run("nothing suggests download", func(t *testing.T) {
		_, err := resolve(lookup{goos: "windows", goarch: "amd64", env: winEnv, stat: exists(), lookPath: none}, KindEdge, "")
		if err == nil || !strings.Contains(err.Error(), "一键下载内置浏览器") {
			t.Fatalf("err = %v", err)
		}
		_, err = resolve(lookup{goos: "windows", goarch: "arm64", env: winEnv, stat: exists(), lookPath: none}, "", "")
		if err == nil || strings.Contains(err.Error(), "一键下载") {
			t.Fatalf("arm64 err = %v", err)
		}
	})
	t.Run("linux chromium via lookpath", func(t *testing.T) {
		rt, err := resolve(lookup{
			goos: "linux", goarch: "amd64", env: func(string) string { return "" },
			stat: exists(),
			lookPath: func(name string) (string, error) {
				if name == "chromium" {
					return "/usr/bin/chromium", nil
				}
				return "", exec.ErrNotFound
			},
		}, KindChrome, "")
		if err != nil || rt.Kind != KindChromium || rt.Path != "/usr/bin/chromium" {
			t.Fatalf("got %+v %v", rt, err)
		}
	})
	t.Run("darwin prefers chrome over edge", func(t *testing.T) {
		chromePath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		edgePath := "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
		rt, err := resolve(lookup{
			goos: "darwin", goarch: "arm64", env: func(string) string { return "" },
			stat: exists(chromePath, edgePath), lookPath: none,
		}, "", "")
		if err != nil || rt.Kind != KindChrome || rt.Path != chromePath {
			t.Fatalf("got %+v %v", rt, err)
		}
	})
	t.Run("explicit edge wins on darwin", func(t *testing.T) {
		chromePath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		edgePath := "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
		rt, err := resolve(lookup{
			goos: "darwin", goarch: "arm64", env: func(string) string { return "" },
			stat: exists(chromePath, edgePath), lookPath: none,
		}, KindEdge, "")
		if err != nil || rt.Kind != KindEdge {
			t.Fatalf("got %+v %v", rt, err)
		}
	})
	t.Run("builtin when nothing else is installed", func(t *testing.T) {
		dir := t.TempDir()
		art, err := artifactFor("darwin", "arm64")
		if err != nil {
			t.Fatal(err)
		}
		exe := installedExe(dir, art)
		if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(exe, []byte("cft"), 0o755); err != nil {
			t.Fatal(err)
		}
		rt, err := resolve(lookup{
			goos: "darwin", goarch: "arm64", env: func(string) string { return "" },
			stat: func(path string) error {
				if path == exe {
					return nil
				}
				return os.ErrNotExist
			},
			lookPath: none,
		}, "", dir)
		if err != nil || rt.Kind != KindBuiltin || rt.Path != exe || rt.Version != art.Version {
			t.Fatalf("got %+v %v", rt, err)
		}
	})
	t.Run("chosen builtin falls back to chrome", func(t *testing.T) {
		chromePath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		rt, err := resolve(lookup{
			goos: "darwin", goarch: "amd64", env: func(string) string { return "" },
			stat: exists(chromePath), lookPath: none,
		}, KindBuiltin, t.TempDir())
		if err != nil || rt.Kind != KindChrome {
			t.Fatalf("got %+v %v", rt, err)
		}
	})
}

func TestChromeLocations(t *testing.T) {
	_, files := chromeLocations("windows", "", func(key string) string {
		switch key {
		case "PROGRAMFILES":
			return `C:\Program Files`
		case "LOCALAPPDATA":
			return `C:\Users\me\AppData\Local`
		default:
			return ""
		}
	})
	if !contains(files, `C:\Program Files\Google\Chrome\Application\chrome.exe`) || !contains(files, `C:\Users\me\AppData\Local\Google\Chrome\Application\chrome.exe`) {
		t.Fatalf("windows chrome: %v", files)
	}
	_, mac := chromeLocations("darwin", "/Users/me", os.Getenv)
	if !contains(mac, "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome") || !contains(mac, "/Users/me/Applications/Google Chrome.app/Contents/MacOS/Google Chrome") {
		t.Fatalf("darwin chrome: %v", mac)
	}
	look, linux := chromiumLocations("linux", "", os.Getenv)
	if !contains(look, "chromium") || !contains(linux, "/usr/bin/chromium") || !contains(linux, "/snap/bin/chromium") {
		t.Fatalf("linux chromium: %v %v", look, linux)
	}
}

func TestFormatDetected(t *testing.T) {
	summary, detail := FormatDetected(nil)
	if summary != "未检测到 Chrome、Edge 或 Chromium" || detail != "" {
		t.Fatalf("empty: %q %q", summary, detail)
	}
	summary, detail = FormatDetected([]Runtime{
		{Kind: KindEdge, Path: `C:\msedge.exe`},
		{Kind: KindChrome, Path: "/opt/google/chrome/chrome"},
		{Kind: KindBuiltin, Path: "/cft/chrome", Version: "1"},
	})
	if !strings.Contains(summary, "Edge") || !strings.Contains(summary, "Chrome") || strings.Contains(summary, "内置") {
		t.Fatalf("summary = %s", summary)
	}
	if !strings.Contains(detail, `C:\msedge.exe`) || !strings.Contains(detail, "/opt/google/chrome/chrome") {
		t.Fatalf("detail = %s", detail)
	}
}
