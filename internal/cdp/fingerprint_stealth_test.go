package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/xxww0098/tulai/internal/fingerprint"
)

// These tests need the managed Fingerprint kernel: the properties they check
// come from its patches and switches, not from Chromium.

// startFingerprintProfile launches one account profile. The returned stop
// function closes it early; otherwise it closes when the test ends.
func startFingerprintProfile(t *testing.T, headless bool, profile string) (*Client, func()) {
	t.Helper()
	config := os.Getenv("TULAI_FINGERPRINT_TEST_DATA")
	if _, ok := fingerprint.InstalledFingerprint(config); !ok || testing.Short() {
		t.Skip("requires a tulai-managed Fingerprint test runtime")
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	proc, err := fingerprint.Start(t.Context(), config, fingerprint.Options{
		Headless:      headless,
		Profile:       profile,
		DirectNetwork: true,
	}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	handle := int(9200 + time.Now().UnixNano()%200)
	fingerprint.Register(handle, proc)
	t.Cleanup(func() { fingerprint.Unregister(handle, proc) })
	client, err := NewClient(fingerprint.FingerprintEndpoint(handle))
	if err != nil {
		proc.Close()
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			client.Close()
			proc.Close()
		})
	}
	t.Cleanup(stop)
	return client, stop
}

func TestFingerprintPageSurfaceStaysSelfConsistent(t *testing.T) {
	client, _ := startFingerprintProfile(t, true, filepath.Join(t.TempDir(), "fp-profile"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bundle.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`window.__bundleLoaded = true;`))
		case "/sw.js":
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`self.addEventListener('fetch', () => {});`))
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head><script src="/bundle.js"></script></head><body><input id="in">
<script>
window.__stacks = [];
for (const [object, name] of [[JSON, 'stringify'], [Object, 'keys'], [Document.prototype, 'createElement']]) {
  const original = object[name];
  object[name] = function () {
    if (window.__stacks.length < 20) { try { window.__stacks.push(new Error().stack) } catch (_) {} }
    return original.apply(this, arguments);
  };
}
</script></body></html>`))
		}
	}))
	defer server.Close()

	s, err := client.NewSession(server.URL+"/page", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.EvaluateBool(`window.__bundleLoaded === true`) {
		t.Fatal("the page's own script did not load")
	}

	// Values a page can verify against each other must agree.
	surface, err := s.EvaluateAsync(`(async () => {
  const span = document.createElement('span');
  span.style.cssText = 'font:16px sans-serif;white-space:nowrap;position:absolute;left:107.5px;top:63.25px';
  span.textContent = 'Width of text';
  document.body.appendChild(span);
  const rect = span.getBoundingClientRect();
  span.remove();
  const canvas = document.createElement('canvas'); canvas.width = 64; canvas.height = 16;
  const context = canvas.getContext('2d');
  context.fillStyle = '#f60'; context.fillRect(0, 0, 64, 16); context.fillStyle = '#069'; context.fillText('tulai', 2, 12);
  const out = {
    left: rect.left, top: rect.top,
    canvasStable: canvas.toDataURL() === canvas.toDataURL() &&
      context.getImageData(0, 0, 64, 16).data.join() === context.getImageData(0, 0, 64, 16).data.join(),
    fakeShadowRoot: 'fakeShadowRoot' in Element.prototype,
    registerOwn: Object.prototype.hasOwnProperty.call(navigator.serviceWorker, 'register'),
    registerNative: /\{\s*\[native code\]\s*\}/.test(String(navigator.serviceWorker.register)),
  };
  const offline = new OfflineAudioContext(1, 44100, 44100);
  const oscillator = offline.createOscillator(); oscillator.connect(offline.destination); oscillator.start(0);
  out.audioRate = (await offline.startRendering()).sampleRate;
  try {
    const registration = await navigator.serviceWorker.register('/sw.js', { scope: '/' });
    out.serviceWorker = 'registered';
    await registration.unregister();
  } catch (error) { out.serviceWorker = 'ERR:' + error.name }
  return out;
})()`)
	if err != nil {
		t.Fatalf("surface probe failed: %v", err)
	}
	var got struct {
		Left, Top, AudioRate         float64
		CanvasStable, FakeShadowRoot bool
		RegisterOwn, RegisterNative  bool
		ServiceWorker                string
	}
	if err := json.Unmarshal([]byte(surface), &got); err != nil {
		t.Fatalf("surface probe %q: %v", surface, err)
	}
	if got.Left != 107.5 || got.Top != 63.25 {
		t.Fatalf("layout rectangles are shifted: left=%v top=%v", got.Left, got.Top)
	}
	if got.AudioRate != 44100 {
		t.Fatalf("OfflineAudioContext rendered at %v Hz instead of the requested 44100 Hz", got.AudioRate)
	}
	if !got.CanvasStable {
		t.Fatal("reading the same canvas twice gave different pixels")
	}
	if got.FakeShadowRoot {
		t.Fatal("Element.prototype.fakeShadowRoot is visible to pages")
	}
	if got.RegisterOwn || !got.RegisterNative || got.ServiceWorker != "registered" {
		t.Fatalf("navigator.serviceWorker.register is tampered with: own=%v native=%v register=%q", got.RegisterOwn, got.RegisterNative, got.ServiceWorker)
	}

	// Code tulai evaluates must look like the site's own: a site hooking natives
	// sees frames under the page's script URL, never the tooling that submitted them.
	if _, err := s.Evaluate(`(()=>{window.__stacks.length=0;JSON.stringify({a:1});Object.keys({b:2});document.createElement('i');return window.__stacks.length})()`); err != nil {
		t.Fatal(err)
	}
	stacks, err := s.EvaluateAsync(`window.__stacks`)
	if err != nil {
		t.Fatal(err)
	}
	s.AppendNavigateScript(`window.__navProbe = function () { JSON.stringify({n: 1}); Object.keys({m: 2}); };`)
	if err := s.Navigate(server.URL + "/page"); err != nil {
		t.Fatal(err)
	}
	navStacks, err := s.EvaluateAsync(`(()=>{window.__stacks.length=0;window.__navProbe();return window.__stacks})()`)
	if err != nil {
		t.Fatal(err)
	}
	siteFrame := regexp.MustCompile(`^\s+at .+ \(http://127\.0\.0\.1:\d+/(?:page|bundle\.js):\d+:\d+\)$`)
	// The site's bundle is the first same-origin script the page loaded.
	runnerFrame := regexp.MustCompile(`^\s+at .+ \(http://127\.0\.0\.1:\d+/bundle\.js:\d+:\d+\)$`)
	injectedFrame := regexp.MustCompile(`^\s+at window\.__navProbe \(http://127\.0\.0\.1:\d+/page:\d+:\d+\)$`)
	for _, check := range []struct {
		name, raw string
		hooks     int
		wanted    *regexp.Regexp
	}{
		{"evaluated", stacks, 3, runnerFrame},
		{"injected", navStacks, 2, injectedFrame},
	} {
		var list []string
		if err := json.Unmarshal([]byte(check.raw), &list); err != nil || len(list) != check.hooks {
			t.Fatalf("%s stacks %q: %v", check.name, check.raw, err)
		}
		underSiteURL := false
		for _, stack := range list {
			for _, line := range strings.Split(stack, "\n")[1:] {
				if !siteFrame.MatchString(line) {
					t.Fatalf("%s code leaked an automation frame: %q", check.name, stack)
				}
				underSiteURL = underSiteURL || check.wanted.MatchString(line)
			}
		}
		if !underSiteURL {
			t.Fatalf("%s code did not run under the expected URL: %q", check.name, check.raw)
		}
	}
}

type fpDisplay struct {
	Width, Height, AvailHeight, AvailTop, ColorDepth int
	DPR                                              float64
	P3                                               bool

	HDR bool
}

func readFPDisplay(t *testing.T, s *Session) fpDisplay {
	t.Helper()
	raw, err := s.EvaluateAsync(`({width:screen.width,height:screen.height,availHeight:screen.availHeight,availTop:screen.availTop,
  colorDepth:screen.colorDepth,dpr:devicePixelRatio,p3:matchMedia('(color-gamut: p3)').matches,
  hdr:matchMedia('(dynamic-range: high)').matches})`)
	var display fpDisplay
	if err != nil || json.Unmarshal([]byte(raw), &display) != nil {
		t.Fatalf("read display: %q %v", raw, err)
	}
	return display
}

func TestFingerprintHeadedDisplayIsReplayedHeadless(t *testing.T) {
	accounts := filepath.Join(t.TempDir(), "Fingerprint")
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>display</title><body>display</body>`))
	}))
	defer page.Close()

	headedClient, stopHeaded := startFingerprintProfile(t, false, filepath.Join(accounts, "account-1"))
	headed, err := headedClient.NewSession(page.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := readFPDisplay(t, headed)
	stopHeaded()

	recorded, err := os.ReadFile(filepath.Join(accounts, ".tulai-host-display.json"))
	if err != nil {
		t.Fatalf("a headed session did not record the host display: %v", err)
	}
	var record struct {
		Width, Height, AvailTop, AvailBottom, ColorDepth int
		DPR                                              float64
		P3                                               bool

		HDR bool
	}
	if err := json.Unmarshal(recorded, &record); err != nil {
		t.Fatalf("host display record %q: %v", recorded, err)
	}
	if want.Width < 320 || want.Height < 240 || want.DPR <= 0 {
		t.Fatalf("headed page reported an unusable display: %+v", want)
	}
	if record.Width != want.Width || record.Height != want.Height || record.AvailTop != want.AvailTop ||
		record.AvailBottom != want.Height-want.AvailTop-want.AvailHeight || record.ColorDepth != want.ColorDepth ||
		record.DPR != want.DPR || record.P3 != want.P3 || record.HDR != want.HDR {
		t.Fatalf("host display record %+v differs from what the headed page reports %+v", record, want)
	}

	// Another account of the same host, without a window, shows the same display.
	headlessClient, _ := startFingerprintProfile(t, true, filepath.Join(accounts, "account-2"))
	headless, err := headlessClient.NewSession(page.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer headless.Close()
	got := readFPDisplay(t, headless)
	near := func(a, b float64) bool { return a-b < 1 && b-a < 1 }
	if got.Width != want.Width || got.Height != want.Height || got.ColorDepth != want.ColorDepth || got.P3 != want.P3 || got.HDR != want.HDR ||
		!near(float64(got.AvailTop), float64(want.AvailTop)) || !near(float64(got.AvailHeight), float64(want.AvailHeight)) || !near(got.DPR, want.DPR) {
		t.Fatalf("headless display %+v differs from the headed one %+v (record %+v)", got, want, record)
	}
	if raw, _ := os.ReadFile(filepath.Join(accounts, ".tulai-host-display.json")); string(raw) != string(recorded) {
		t.Fatalf("a headless session rewrote the host display record: %q -> %q", recorded, raw)
	}
}

// Chromium shows its "unsupported command-line flag" bar on the first tab of a
// headed window whenever the command line carries a switch from its bad-flag
// list. Pages cannot see the bar, only the viewport height it takes from that
// tab, and that tab is the one the account window and the download browser
// drive. It has to report the same browser chrome as every other tab.
func TestFingerprintHeadedWindowOpensWithoutStartupInfobar(t *testing.T) {
	config := os.Getenv("TULAI_FINGERPRINT_TEST_DATA")
	if _, ok := fingerprint.InstalledFingerprint(config); !ok || testing.Short() {
		t.Skip("requires a tulai-managed Fingerprint test runtime")
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>tabs</title><body>tabs</body>`))
	}))
	defer server.Close()
	proc, err := fingerprint.Start(t.Context(), config, fingerprint.Options{
		Profile:       filepath.Join(t.TempDir(), "fp-profile"),
		DirectNetwork: true,
	}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.Close)

	// chromeHeight reads how much of the window the browser chrome takes, once
	// the window has stopped laying itself out.
	chromeHeight := func(target string) int {
		t.Helper()
		if err := proc.Call(t.Context(), target, "Page.navigate", map[string]any{"url": server.URL}, nil); err != nil {
			t.Fatal(err)
		}
		previous, same := -1, 0
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			var reply struct {
				Result *struct {
					Value int `json:"value"`
				} `json:"result"`
				ExceptionDetails *struct {
					Text string `json:"text"`
				} `json:"exceptionDetails"`
			}
			if err := proc.Call(t.Context(), target, "Runtime.evaluate", map[string]any{"expression": "outerHeight - innerHeight"}, &reply); err != nil {
				t.Fatal(err)
			}
			if reply.ExceptionDetails != nil || reply.Result == nil {
				t.Fatalf("reading the browser chrome height of %s: %+v", target, reply)
			}
			if reply.Result.Value != previous {
				previous, same = reply.Result.Value, 0
			} else if same++; same == 4 {
				return reply.Result.Value
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("the window of %s kept resizing for 15s", target)
		return 0
	}
	first := chromeHeight(proc.Page)

	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := proc.Call(t.Context(), "", "Target.createTarget", map[string]any{}, &created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = proc.Call(closeCtx, created.TargetID, "Page.close", nil, nil)
	})
	if other := chromeHeight(created.TargetID); other != first {
		t.Fatalf("the first tab of a headed window reports %dpx of browser chrome, every other tab %dpx", first, other)
	}
}

// The canvas noise layer is what keeps accounts apart: every account renders
// the same drawing differently, and keeps doing so across sessions.
func TestFingerprintAccountsRenderDifferentlyAndStably(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><title>render</title><body>render</body>`))
	}))
	defer page.Close()
	accounts := filepath.Join(t.TempDir(), "Fingerprint")
	render := func(account string) string {
		client, stop := startFingerprintProfile(t, true, filepath.Join(accounts, account))
		defer stop()
		session, err := client.NewSession(page.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer session.Close()
		hashes, err := session.EvaluateAsync(`(() => {
  const hash = bytes => { let h = 0x811c9dc5; for (let i = 0; i < bytes.length; i++) { h ^= bytes[i]; h = Math.imul(h, 0x01000193); } return (h >>> 0).toString(16); };
  const canvas = document.createElement('canvas'); canvas.width = 280; canvas.height = 120;
  const context = canvas.getContext('2d');
  context.textBaseline = 'top'; context.font = '14px Arial'; context.fillStyle = '#f60'; context.fillRect(100, 1, 62, 20);
  context.fillStyle = '#069'; context.font = '11pt no-real-font'; context.fillText('Cwm fjordbank \u{1F603} gql vext?, \u{1F600}', 2, 15);
  const glCanvas = document.createElement('canvas'); glCanvas.width = 64; glCanvas.height = 64;
  const gl = glCanvas.getContext('webgl', { preserveDrawingBuffer: true });
  const program = gl.createProgram();
  const compile = (type, source) => { const shader = gl.createShader(type); gl.shaderSource(shader, source); gl.compileShader(shader); return shader; };
  gl.attachShader(program, compile(gl.VERTEX_SHADER, 'attribute vec2 p; varying vec2 v; void main(){ v = p; gl_Position = vec4(p, 0.0, 1.0); }'));
  gl.attachShader(program, compile(gl.FRAGMENT_SHADER, 'precision highp float; varying vec2 v; void main(){ gl_FragColor = vec4(v.x, v.y, 0.5 + 0.5 * sin(v.x * 31.0 + v.y * 17.0), 1.0); }'));
  gl.linkProgram(program); gl.useProgram(program);
  gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
  const attribute = gl.getAttribLocation(program, 'p'); gl.enableVertexAttribArray(attribute); gl.vertexAttribPointer(attribute, 2, gl.FLOAT, false, 0, 0);
  gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
  const pixels = new Uint8Array(64 * 64 * 4); gl.readPixels(0, 0, 64, 64, gl.RGBA, gl.UNSIGNED_BYTE, pixels);
  return { canvas: hash(context.getImageData(0, 0, 280, 120).data), webgl: hash(pixels) };
})()`)
		if err != nil {
			t.Fatalf("%s render probe: %v", account, err)
		}
		var hash struct{ Canvas, WebGL string }
		if json.Unmarshal([]byte(hashes), &hash) != nil || hash.Canvas == "" || hash.WebGL == "" {
			t.Fatalf("%s render probe returned %q", account, hashes)
		}
		return fmt.Sprintf("canvas=%s webgl=%s", hash.Canvas, hash.WebGL)
	}

	first := render("account-1")
	if again := render("account-1"); again != first {
		t.Fatalf("account-1 changed its rendering between sessions: %s -> %s", first, again)
	}
	if other := render("account-2"); other == first {
		t.Fatalf("two accounts render identically (%s); they are linkable by canvas or WebGL", first)
	}
}

const fpColorPage = `<!doctype html><title>colors</title><body style="margin:0;background:#fff">
<div style="width:100px;height:100px;background:#f00;float:left"></div><div style="width:100px;height:100px;background:#0f0;float:left"></div>
<div style="width:100px;height:100px;background:#00f;float:left"></div><div style="width:100px;height:100px;background:#808080;float:left"></div></body>`

// A wide-gamut host is replayed without recoloring what the browser drawer
// captures: HDR through the hdr10 profile, P3 without HDR through the emulated
// media feature. The display-p3-d65 profile, which would also answer the P3
// query, shifts every capture and is therefore not used.
func TestFingerprintHeadlessReplaysWideGamutDisplaysWithoutRecoloringCaptures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fpColorPage))
	}))
	defer server.Close()
	for _, display := range []struct {
		name string
		hdr  bool
	}{{name: "hdr", hdr: true}, {name: "p3"}} {
		t.Run(display.name, func(t *testing.T) {
			accounts := filepath.Join(t.TempDir(), "Fingerprint")
			if err := os.MkdirAll(accounts, 0o700); err != nil {
				t.Fatal(err)
			}
			record := fmt.Sprintf(`{"width":1512,"height":982,"availLeft":0,"availTop":37,"availRight":0,"availBottom":70,"dpr":2,`+
				`"colorDepth":30,"p3":true,"hdr":%t,"extended":true}`, display.hdr)
			if err := os.WriteFile(filepath.Join(accounts, ".tulai-host-display.json"), []byte(record), 0o600); err != nil {
				t.Fatal(err)
			}
			client, _ := startFingerprintProfile(t, true, filepath.Join(accounts, "account-1"))
			session, err := client.NewSession(server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			summary, _ := session.Evaluate(`({width:screen.width,height:screen.height,depth:screen.colorDepth,extended:screen.isExtended,
  p3:matchMedia('(color-gamut: p3)').matches,hdr:matchMedia('(dynamic-range: high)').matches,
  scheme:matchMedia('(prefers-color-scheme: dark)').matches,motion:matchMedia('(prefers-reduced-motion: reduce)').matches})`)
			var replayed struct {
				Width, Height, Depth int
				Extended, P3, HDR    bool
				Scheme, Motion       bool
			}
			if err := json.Unmarshal([]byte(summary), &replayed); err != nil || replayed.Width != 1512 || replayed.Height != 982 ||
				replayed.Depth != 30 || !replayed.Extended || !replayed.P3 || replayed.HDR != display.hdr || replayed.Scheme || replayed.Motion {
				t.Fatalf("the recorded display was not replayed: %s", summary)
			}

			dpr := session.EvaluateInt(`devicePixelRatio`)
			if dpr < 1 {
				t.Fatalf("the page reports devicePixelRatio %d", dpr)
			}
			for _, format := range []page.CaptureScreenshotFormat{"png", "jpeg"} {
				var raw []byte
				err := session.exec(func(executor context.Context) (err error) {
					raw, err = page.CaptureScreenshot().WithFormat(format).Do(executor)
					return err
				})
				if err != nil {
					t.Fatalf("%s capture: %v", format, err)
				}
				img, _, err := image.Decode(bytes.NewReader(raw))
				if err != nil {
					t.Fatalf("%s capture is not a decodable image: %v", format, err)
				}
				for _, patch := range []struct {
					name string
					x    int
					want [3]int
				}{{"red", 50, [3]int{255, 0, 0}}, {"green", 150, [3]int{0, 255, 0}}, {"blue", 250, [3]int{0, 0, 255}}, {"gray", 350, [3]int{128, 128, 128}}} {
					red, green, blue, _ := img.At(patch.x*dpr, 50*dpr).RGBA()
					got := [3]int{int(red >> 8), int(green >> 8), int(blue >> 8)}
					for i := range got {
						if got[i]-patch.want[i] > 8 || patch.want[i]-got[i] > 8 {
							t.Fatalf("%s capture recolored the %s patch: got %v want %v", format, patch.name, got, patch.want)
						}
					}
				}
			}
		})
	}
}
