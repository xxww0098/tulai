package fingerprint

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/realbrowser/fingerprintkernel/engine"
	fpprofile "github.com/xxww0098/realbrowser/fingerprintkernel/profile"
)

func TestFingerprintPlatformSupportAndSafety(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
		want         bool
	}{
		{"darwin", "arm64", true},
		{"windows", "amd64", true},
		{"linux", "amd64", true},
		{"darwin", "amd64", false},
		{"windows", "arm64", false},
		{"linux", "arm64", false},
	} {
		if got := FingerprintSupported(tc.goos, tc.goarch); got != tc.want {
			t.Fatalf("FingerprintSupported(%s, %s) = %t, want %t", tc.goos, tc.goarch, got, tc.want)
		}
	}
	if _, ok := InstalledFingerprint(t.TempDir()); ok {
		t.Fatal("empty directory reported as installed")
	}
	if _, err := Start(t.Context(), t.TempDir(), Options{DirectNetwork: true, Proxy: "http://127.0.0.1:8080", Profile: t.TempDir()}, nil); err == nil || !strings.Contains(err.Error(), "不能同时指定代理") {
		t.Fatalf("expected directNetwork + proxy conflict, got %v", err)
	}
	if _, err := Start(t.Context(), t.TempDir(), Options{DirectNetwork: true, Proxy: "   ", Profile: t.TempDir()}, nil); err == nil || !strings.Contains(err.Error(), "尚未准备好") {
		t.Fatalf("expected whitespace proxy to normalize before directNetwork check, got %v", err)
	}
	if _, err := Start(t.Context(), t.TempDir(), Options{Profile: t.TempDir()}, nil); err == nil {
		t.Fatal("started uninstalled Fingerprint runtime")
	}
	if FingerprintEndpoint(9333) != "fingerprint://9333" {
		t.Fatalf("endpoint = %q", FingerprintEndpoint(9333))
	}
}

func TestFingerprintZipExtractionAndSHA256(t *testing.T) {
	dir := t.TempDir()
	goodZip := filepath.Join(dir, "good.zip")
	writeTestZip(t, goodZip, map[string]string{
		"ungoogled-chromium_148/chrome.exe":     "binary-payload",
		"ungoogled-chromium_148/locales/en.pak": "pak",
	})
	raw, err := os.ReadFile(goodZip)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if err := engine.VerifyFileSHA256(t.Context(), goodZip, hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("valid sha256 rejected: %v", err)
	}
	if err := engine.VerifyFileSHA256(t.Context(), goodZip, strings.Repeat("0", 64)); err == nil {
		t.Fatal("mismatched sha256 accepted")
	}
	dest := filepath.Join(dir, "out")
	if err := (engine.PinnedBuild{File: "kernel.zip", RelExe: "chrome.exe"}).Unpack(t.Context(), goodZip, dest); err != nil {
		t.Fatalf("extract good zip: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "chrome.exe"))
	if err != nil || string(got) != "binary-payload" {
		t.Fatalf("extracted chrome.exe = %q, err = %v", got, err)
	}

	evilZip := filepath.Join(dir, "evil.zip")
	writeTestZip(t, evilZip, map[string]string{
		"top/../../escaped.txt": "nope",
	})
	if err := (engine.PinnedBuild{File: "kernel.zip", RelExe: "chrome.exe"}).Unpack(t.Context(), evilZip, filepath.Join(dir, "evil-out")); err == nil {
		t.Fatal("zip traversal accepted")
	}
}

func writeTestZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprintTarXZExtractionAndSymlinkGuards(t *testing.T) {
	xzBin, err := exec.LookPath("xz")
	if err != nil {
		t.Skip("xz not available on host")
	}
	writeTarXZ := func(t *testing.T, headers []tar.Header, bodies map[string]string) string {
		t.Helper()
		var raw bytes.Buffer
		tw := tar.NewWriter(&raw)
		for _, hdr := range headers {
			h := hdr
			body := bodies[h.Name]
			if h.Typeflag == tar.TypeReg {
				h.Size = int64(len(body))
			}
			if err := tw.WriteHeader(&h); err != nil {
				t.Fatal(err)
			}
			if h.Typeflag == tar.TypeReg && len(body) > 0 {
				if _, err := tw.Write([]byte(body)); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		out := filepath.Join(t.TempDir(), "fixture.tar.xz")
		cmd := exec.Command(xzBin, "-z", "-c")
		cmd.Stdin = &raw
		compressed, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, compressed, 0o600); err != nil {
			t.Fatal(err)
		}
		return out
	}

	good := writeTarXZ(t, []tar.Header{
		{Name: "top/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "top/chrome", Typeflag: tar.TypeReg, Mode: 0o755},
		{Name: "top/lib/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "top/lib/link", Typeflag: tar.TypeSymlink, Linkname: "../chrome"},
	}, map[string]string{"top/chrome": "elf-binary"})
	dest := filepath.Join(t.TempDir(), "good")
	if err := (engine.PinnedBuild{File: "kernel.tar.xz", RelExe: "chrome"}).Unpack(t.Context(), good, dest); err != nil {
		t.Fatalf("extract good tar.xz: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dest, "lib", "link")); err != nil || string(got) != "elf-binary" {
		t.Fatalf("internal relative symlink broken: %q err=%v", got, err)
	}

	// Reject path traversal, escaping relative symlink, absolute symlink, and hardlinks.
	for name, hdrs := range map[string][]tar.Header{
		"traversal": {{Name: "top/../../evil.txt", Typeflag: tar.TypeReg, Mode: 0o644}},
		"rel-escape-symlink": {
			{Name: "top/evil", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"},
		},
		"abs-symlink": {
			{Name: "top/evil", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"},
		},
		"hardlink": {
			{Name: "top/a", Typeflag: tar.TypeReg, Mode: 0o644},
			{Name: "top/b", Typeflag: tar.TypeLink, Linkname: "top/a"},
		},
		"bare-root-file": {
			{Name: "chrome", Typeflag: tar.TypeReg, Mode: 0o755},
		},
	} {
		t.Run(name, func(t *testing.T) {
			bad := writeTarXZ(t, hdrs, map[string]string{"top/../../evil.txt": "x", "top/a": "x", "chrome": "x"})
			if err := (engine.PinnedBuild{File: "kernel.tar.xz", RelExe: "chrome"}).Unpack(t.Context(), bad, filepath.Join(t.TempDir(), "bad")); err == nil {
				t.Fatalf("expected %s archive to be rejected", name)
			}
		})
	}
}

// Set TULAI_FINGERPRINT_TEST_INSTALL=1 (and optionally TULAI_FINGERPRINT_ARCHIVE)
// to install fingerprint-chromium into a test-owned runtime and verify lifecycle,
// per-account seed isolation, persistence, stealth, and isolated-world CDP bindings.
func TestManagedFingerprintLifecycle(t *testing.T) {
	dir := os.Getenv("TULAI_FINGERPRINT_TEST_DATA")
	if os.Getenv("TULAI_FINGERPRINT_TEST_INSTALL") == "1" {
		if dir == "" {
			dir = t.TempDir()
		}
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
		defer cancel()
		if _, err := EnsureFingerprint(ctx, dir, "", func(p Progress) {
			if p.Done == 0 || p.Stage == StageDone {
				t.Log(p.Stage, p.Text)
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := InstalledFingerprint(dir); !ok {
		t.Skip("requires a tulai-managed Fingerprint test runtime")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>FP fixture</title><canvas id="c" width="120" height="40"></canvas><script>window.__pageLeaks = () => Object.getOwnPropertyNames(window).filter(k => /tulai/i.test(k));</script>`))
	}))
	defer server.Close()

	profileRoot := filepath.Join(t.TempDir(), "Fingerprint")
	profileA := filepath.Join(profileRoot, "account-a")
	profileB := filepath.Join(profileRoot, "account-b")

	type probeSnap struct {
		Title           string   `json:"title"`
		UA              string   `json:"ua"`
		Webdriver       bool     `json:"webdriver"`
		Cores           int      `json:"cores"`
		DeviceMemory    int      `json:"deviceMemory"`
		PlatformVersion string   `json:"platformVersion"`
		GLRenderer      string   `json:"glRenderer"`
		ScreenW         int      `json:"screenW"`
		ScreenH         int      `json:"screenH"`
		AvailH          int      `json:"availH"`
		OuterW          int      `json:"outerW"`
		OuterH          int      `json:"outerH"`
		InnerW          int      `json:"innerW"`
		InnerH          int      `json:"innerH"`
		CanvasHash      string   `json:"canvasHash"`
		Storage         string   `json:"storage"`
		PageLeaks       []string `json:"pageLeaks"`
		ViewLeaks       []string `json:"viewLeaks"`
	}
	probeExpr := `(async () => {
		const c = document.getElementById('c');
		const g = c.getContext('2d');
		g.fillStyle = 'rgb(18, 52, 86)';
		g.fillRect(0, 0, 120, 40);
		g.fillStyle = '#ff00aa';
		g.font = '16px sans-serif';
		g.fillText('FingerprintTest', 8, 26);
		let glRenderer = '';
		try {
			const gl = document.createElement('canvas').getContext('webgl');
			const ext = gl && gl.getExtension('WEBGL_debug_renderer_info');
			if (gl && ext) glRenderer = gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) || '';
		} catch (_) {}
		let platformVersion = '';
		try {
			if (navigator.userAgentData) {
				const hi = await navigator.userAgentData.getHighEntropyValues(['platformVersion']);
				platformVersion = hi.platformVersion || '';
			}
		} catch (_) {}
		return JSON.stringify({
			title: document.title,
			ua: navigator.userAgent,
			webdriver: navigator.webdriver,
			cores: navigator.hardwareConcurrency,
			deviceMemory: navigator.deviceMemory || 0,
			platformVersion,
			glRenderer,
			screenW: screen.width,
			screenH: screen.height,
			availH: screen.availHeight,
			outerW: outerWidth,
			outerH: outerHeight,
			innerW: innerWidth,
			innerH: innerHeight,
			canvasHash: c.toDataURL('image/png'),
			storage: localStorage.getItem('account') || '',
			pageLeaks: window.__pageLeaks(),
			viewLeaks: Object.getOwnPropertyNames(document.defaultView).filter(k => /tulai/i.test(k)),
		});
	})()`

	runSession := func(profile string, handle int, writeStorage string) (probeSnap, int) {
		p, err := Start(t.Context(), dir, Options{
			Headless:       true,
			Profile:        profile,
			DirectNetwork:  true,
			CollisionRoots: []string{profileRoot},
		}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		defer p.Close()
		Register(handle, p)
		defer Unregister(handle, p)
		if proc, err := Find(FingerprintEndpoint(handle)); err != nil || proc != p {
			t.Fatalf("lookup: %v", err)
		}
		var mu sync.Mutex
		var gotBinding string
		gotCh := make(chan string, 1)
		unsub := p.Subscribe(func(ev Event) {
			if ev.Event == "binding" {
				var payload string
				_ = json.Unmarshal(ev.Data, &payload)
				mu.Lock()
				gotBinding = payload
				mu.Unlock()
				select {
				case gotCh <- payload:
				default:
				}
			}
		})
		defer unsub()
		if err := p.Call(t.Context(), p.Page, "Runtime.addBinding", map[string]string{"name": "tulaiBridge"}, nil); err != nil {
			t.Fatal(err)
		}
		if err := p.Call(t.Context(), p.Page, "Page.navigate", map[string]string{"url": server.URL + "/step1"}, nil); err != nil {
			t.Fatal(err)
		}
		// Navigate a second time to prove isolated-world binding survives navigations.
		if err := p.Call(t.Context(), p.Page, "Page.navigate", map[string]string{"url": server.URL + "/step2"}, nil); err != nil {
			t.Fatal(err)
		}
		if err := p.Call(t.Context(), p.Page, "Runtime.evaluate", map[string]string{"expression": `tulaiBridge('ping-after-nav')`}, nil); err != nil {
			t.Fatal(err)
		}
		select {
		case payload := <-gotCh:
			if payload != "ping-after-nav" {
				t.Fatalf("binding payload = %q, want ping-after-nav", payload)
			}
		case <-time.After(5 * time.Second):
			mu.Lock()
			b := gotBinding
			mu.Unlock()
			t.Fatalf("timed out waiting for isolated-world binding event (last=%q)", b)
		}
		var out struct {
			Result           struct{ Value string }
			ExceptionDetails any
		}
		if err := p.Call(t.Context(), p.Page, "Runtime.evaluate", map[string]string{"expression": probeExpr}, &out); err != nil {
			t.Fatal(err)
		}
		if out.ExceptionDetails != nil {
			t.Fatalf("probe exception: %+v", out.ExceptionDetails)
		}
		var snap probeSnap
		if err := json.Unmarshal([]byte(out.Result.Value), &snap); err != nil {
			t.Fatalf("unmarshal probe %q: %v", out.Result.Value, err)
		}
		if writeStorage != "" {
			if err := p.Call(t.Context(), p.Page, "Runtime.evaluate", map[string]string{
				"expression": "localStorage.setItem('account'," + strconvQuote(writeStorage) + ")",
			}, nil); err != nil {
				t.Fatal(err)
			}
		}
		id, err := fpprofile.ReadProfile(profile)
		if err != nil {
			t.Fatalf("read persisted fingerprint profile: %v", err)
		}
		if id.Seed == 0 {
			t.Fatalf("missing generated seed in .browserkernel.json: %+v", id)
		}
		return snap, int(id.Seed)
	}

	snapA1, seedA1 := runSession(profileA, 9981, "persisted-a")
	if snapA1.Title != "FP fixture" || snapA1.Webdriver || !strings.Contains(snapA1.UA, "Chrome/") || strings.Contains(snapA1.UA, "HeadlessChrome") {
		t.Fatalf("unexpected snapA1: %+v", snapA1)
	}
	if snapA1.ScreenW < 1440 || snapA1.ScreenH < 900 || snapA1.AvailH >= snapA1.ScreenH || snapA1.InnerW < 1280 || snapA1.InnerH < 600 {
		t.Fatalf("headless Fingerprint reported non-desktop display/viewport: %+v", snapA1)
	}
	if len(snapA1.PageLeaks) > 0 || len(snapA1.ViewLeaks) > 0 {
		t.Fatalf("real window leaked tulai keys: page=%v view=%v", snapA1.PageLeaks, snapA1.ViewLeaks)
	}
	if snapA1.Storage != "" {
		t.Fatalf("fresh profile had storage: %q", snapA1.Storage)
	}

	snapA2, seedA2 := runSession(profileA, 9981, "")
	if seedA2 != seedA1 {
		t.Fatalf("seed changed across restart: %d -> %d", seedA1, seedA2)
	}
	if snapA2.Storage != "persisted-a" {
		t.Fatalf("storage did not persist: %q", snapA2.Storage)
	}
	if snapA2.Cores != snapA1.Cores || snapA2.DeviceMemory != snapA1.DeviceMemory ||
		snapA2.PlatformVersion != snapA1.PlatformVersion || snapA2.GLRenderer != snapA1.GLRenderer ||
		snapA2.ScreenW != snapA1.ScreenW || snapA2.ScreenH != snapA1.ScreenH ||
		snapA2.OuterW != snapA1.OuterW || snapA2.OuterH != snapA1.OuterH ||
		snapA2.CanvasHash != snapA1.CanvasHash {
		t.Fatalf("fingerprint drifted across restart: a1=%+v a2=%+v", snapA1, snapA2)
	}

	snapB, seedB := runSession(profileB, 9982, "")
	if seedB == seedA1 {
		t.Fatalf("two profiles shared seed %d", seedB)
	}
	if snapB.Storage != "" {
		t.Fatalf("profileB saw profileA storage: %q", snapB.Storage)
	}
	if snapB.CanvasHash == snapA1.CanvasHash {
		t.Fatalf("two distinct seeds produced identical canvas hashes")
	}
	if snapB.PlatformVersion == "" || snapA1.PlatformVersion == "" || snapB.PlatformVersion == snapA1.PlatformVersion {
		t.Fatalf("two sibling profiles failed to diversify platformVersion: a=%q b=%q", snapA1.PlatformVersion, snapB.PlatformVersion)
	}
	if snapB.Cores == snapA1.Cores || snapB.DeviceMemory == snapA1.DeviceMemory {
		t.Fatalf("two sibling profiles failed to diversify cores/memory: a=(%d,%d) b=(%d,%d)", snapA1.Cores, snapA1.DeviceMemory, snapB.Cores, snapB.DeviceMemory)
	}
	if snapB.OuterW == snapA1.OuterW && snapB.OuterH == snapA1.OuterH && snapB.ScreenW == snapA1.ScreenW && snapB.ScreenH == snapA1.ScreenH {
		t.Fatalf("two sibling profiles shared identical display/window geometry: a=(%dx%d/%dx%d) b=(%dx%d/%dx%d)",
			snapA1.ScreenW, snapA1.ScreenH, snapA1.OuterW, snapA1.OuterH, snapB.ScreenW, snapB.ScreenH, snapB.OuterW, snapB.OuterH)
	}
	if snapA1.GLRenderer != "" && snapB.GLRenderer != "" && strings.Contains(snapA1.GLRenderer, "Apple") && snapB.GLRenderer == snapA1.GLRenderer {
		t.Fatalf("two sibling macOS profiles failed to diversify WebGL GPU renderer: a=%q b=%q", snapA1.GLRenderer, snapB.GLRenderer)
	}

	// Corrupted persisted fingerprint identity (current or legacy) must fail closed instead of silently regenerating.
	for _, filename := range []string{".browserkernel.json", ".tulai-fingerprint.json"} {
		corruptProfile := filepath.Join(profileRoot, "account-corrupt-"+strings.TrimPrefix(filename, "."))
		if err := os.MkdirAll(corruptProfile, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(corruptProfile, filename), []byte("{invalid"), 0o600); err != nil {
			t.Fatal(err)
		}
		if proc, err := Start(t.Context(), dir, Options{Headless: true, Profile: corruptProfile, DirectNetwork: true}, nil); err == nil {
			proc.Close()
			t.Fatalf("corrupted %s was silently accepted", filename)
		}
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
