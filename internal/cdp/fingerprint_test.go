package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/fingerprint"
)

func startFingerprintFixture(t *testing.T, headless bool) (*fingerprint.Process, string, *Client) {
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
		Profile:       filepath.Join(t.TempDir(), "fp-profile"),
		DirectNetwork: true,
	}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proc.Close)
	handle := int(9400 + time.Now().UnixNano()%500)
	fingerprint.Register(handle, proc)
	t.Cleanup(func() { fingerprint.Unregister(handle, proc) })
	endpoint := fingerprint.FingerprintEndpoint(handle)
	client, err := NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return proc, endpoint, client
}

func TestFingerprintStrictCSPAndStealthSignals(t *testing.T) {
	_, _, client := startFingerprintFixture(t, true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; worker-src blob:")
		if r.URL.Path == "/probe.js" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`try { eval('1+2'); document.documentElement.dataset.csp='allowed' } catch (_) { document.documentElement.dataset.csp='blocked' }`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/frame" {
			_, _ = w.Write([]byte(`<!doctype html><body>frame</body>`))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><script src="/probe.js"></script><body><iframe src="/frame"></iframe></body>`))
	}))
	defer server.Close()

	s, err := client.NewSession(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.AppendNavigateScript(`window.__tulaiAsyncProbe = new Promise(r => setTimeout(() => r(window.tulaiCall('cspAsync', 'from-timer')), 20));`)
	var asyncCalls atomic.Int32
	s.Handle("cspAsync", func(v string) string {
		asyncCalls.Add(1)
		return `{"got":"` + v + `"}`
	})
	if err := s.Navigate(server.URL); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s.Handle("cspProbe", func(string) string {
		calls.Add(1)
		return `{"ok":true}`
	})
	answer, err := s.EvaluateAsync(`tulaiCall('cspProbe','')`)
	var reply struct{ OK bool }
	if err != nil || json.Unmarshal([]byte(answer), &reply) != nil || !reply.OK {
		t.Fatalf("CSP 下页面桥不可用: answer=%q, err=%v", answer, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("cspProbe invoked %d times, want 1", got)
	}
	asyncAns, err := s.EvaluateAsync(`window.__tulaiAsyncProbe`)
	if err != nil || !strings.Contains(asyncAns, "from-timer") || asyncCalls.Load() != 1 {
		t.Fatalf("strict CSP timer tulaiCall failed: ans=%q calls=%d err=%v", asyncAns, asyncCalls.Load(), err)
	}
	value, err := s.EvaluateAsync(`(async()=>{
 let reads=0;const error=new Error('probe');
 Object.defineProperty(error,'stack',{get(){reads++;return 'stack'}});
 console.log(error);await new Promise(r=>setTimeout(r,250));
 const frame=document.querySelector('iframe').contentWindow;
 const url=URL.createObjectURL(new Blob(['postMessage(navigator.webdriver ?? null)'],{type:'text/javascript'}));
 const worker=new Worker(url);
 const workerWebdriver=await new Promise((resolve,reject)=>{worker.onmessage=e=>resolve(e.data);worker.onerror=reject});
 worker.terminate();URL.revokeObjectURL(url);
 const names=Object.getOwnPropertyNames(document.defaultView).filter(k=>/^(\$?cdc_|__webdriver|__driver|__fxdriver|__selenium|__playwright|__pw|domAutomation|__tulai|tulai)/i.test(k));
 const frameNames=Object.getOwnPropertyNames(frame).filter(k=>/^(?:__tulai|tulai)/i.test(k));
 let glVendor='', glRenderer='';
 try {
   const c=document.createElement('canvas');
   const gl=c.getContext('webgl');
   const ext=gl && gl.getExtension('WEBGL_debug_renderer_info');
   if (gl && ext) {
     glVendor = gl.getParameter(ext.UNMASKED_VENDOR_WEBGL) || '';
     glRenderer = gl.getParameter(ext.UNMASKED_RENDERER_WEBGL) || '';
   }
 } catch (_) {}
 let uaData = null;
 try {
   if (navigator.userAgentData) {
     uaData = {
       brands: navigator.userAgentData.brands,
       platform: navigator.userAgentData.platform,
       mobile: navigator.userAgentData.mobile,
       high: await navigator.userAgentData.getHighEntropyValues(['architecture','bitness','platformVersion','fullVersionList']),
     };
   }
 } catch (_) {}
 return {
   webdriver:navigator.webdriver??null,
   iframeWebdriver:frame.navigator.webdriver??null,
   workerWebdriver,
   stackReads:reads,
   names,
   frameNames,
   userAgent:navigator.userAgent,
   platform:navigator.platform,
   cores:navigator.hardwareConcurrency,
   deviceMemory:navigator.deviceMemory??null,
   glVendor,
   glRenderer,
   uaData,
   outerW:outerWidth,
   outerH:outerHeight,
   innerW:innerWidth,
   innerH:innerHeight,
   screenW:screen.width,
   screenH:screen.height,
   availH:screen.availHeight,
   base:window.__tulaiBaseInstalled,
   literal:'__tulai stays unchanged',
   csp:document.documentElement.dataset.csp
 };
})()`)
	if err != nil {
		t.Fatalf("stealth probe eval failed: %v", err)
	}
	t.Logf("fingerprint telemetry: %s", value)
	var probe struct {
		Webdriver       *bool    `json:"webdriver"`
		IframeWebdriver *bool    `json:"iframeWebdriver"`
		WorkerWebdriver *bool    `json:"workerWebdriver"`
		StackReads      int      `json:"stackReads"`
		Names           []string `json:"names"`
		FrameNames      []string `json:"frameNames"`
		UserAgent       string   `json:"userAgent"`
		OuterW          int      `json:"outerW"`
		OuterH          int      `json:"outerH"`
		InnerW          int      `json:"innerW"`
		InnerH          int      `json:"innerH"`
		ScreenW         int      `json:"screenW"`
		ScreenH         int      `json:"screenH"`
		AvailH          int      `json:"availH"`
		Base            int      `json:"base"`
		Literal         string   `json:"literal"`
		CSP             string   `json:"csp"`
	}
	if err := json.Unmarshal([]byte(value), &probe); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []*bool{probe.Webdriver, probe.IframeWebdriver, probe.WorkerWebdriver} {
		if flag != nil && *flag {
			t.Fatalf("webdriver exposed in a page, iframe or worker: %s", value)
		}
	}
	if probe.Base != 4 || probe.Literal != "__tulai stays unchanged" || probe.CSP != "blocked" || probe.StackReads != 0 || len(probe.Names) != 0 || len(probe.FrameNames) != 0 || strings.Contains(strings.ToLower(probe.UserAgent), "headless") {
		t.Fatalf("automation detection probe reported a signal: %s", value)
	}
	if probe.ScreenW < 1440 || probe.ScreenH < 900 || probe.AvailH >= probe.ScreenH || probe.OuterW > probe.ScreenW || probe.InnerW < 1280 || probe.InnerH < 600 {
		t.Fatalf("headless Fingerprint reported non-desktop display geometry: %s", value)
	}
	value, err = s.Evaluate(`(()=>{Object.defineProperty(document.defaultView,'__tulaiPageCanary',{value:'site',configurable:false,writable:false});window.__tulaiPageCanary='private';return [window.__tulaiPageCanary,document.defaultView.__tulaiPageCanary]})()`)
	if err != nil || value != `["private","site"]` {
		t.Fatalf("页面变量干扰了应用私有状态: %s, %v", value, err)
	}
}

func TestFingerprintCrossOriginNavigationAndRedirects(t *testing.T) {
	_, _, client := startFingerprintFixture(t, true)

	// Origin 1: 127.0.0.1:<p1>
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redir" {
			target := r.URL.Query().Get("to")
			http.Redirect(w, r, target, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<!doctype html><title>srv1:%s</title><body>srv1 %s</body>`, r.URL.Path, r.URL.Path)
	}))
	defer srv1.Close()

	// Origin 2: localhost:<p2> (distinct hostname triggers Chromium Site Isolation process swap)
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv2 := &httptest.Server{
		Listener: ln2,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, `<!doctype html><title>srv2:%s</title><body>srv2 %s</body>`, r.URL.Path, r.URL.Path)
		})},
	}
	srv2.Start()
	defer srv2.Close()
	origin2 := fmt.Sprintf("http://localhost:%d", ln2.Addr().(*net.TCPAddr).Port)

	s, err := client.NewBackgroundSession(srv1.URL+"/step1", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.AppendNavigateScript(`window.__tulaiCustomInit = (window.__tulaiCustomInit || 0) + 1;`)

	var calls atomic.Int32
	s.Handle("echo", func(v string) string {
		calls.Add(1)
		return v
	})

	checkHop := func(label, want string) {
		t.Helper()
		before := calls.Load()
		var got string
		ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
		defer cancel()
		if err := s.EvaluateContext(ctx, fmt.Sprintf(`window.tulaiCall('echo', %q)`, want), &got); err != nil || got != want {
			t.Fatalf("%s: tulaiCall returned %q (err=%v), want %q", label, got, err, want)
		}
		if after := calls.Load(); after != before+1 {
			t.Fatalf("%s: echo called %d times (expected +1, duplicate or missed binding!)", label, after-before)
		}
		if !s.EvaluateBool(`window.__tulaiBaseInstalled === 4`) {
			t.Fatalf("%s: missing __tulaiBaseInstalled", label)
		}
	}

	checkHop("initial-origin1", "hop1")

	if err := s.Navigate(origin2 + "/step2"); err != nil {
		t.Fatalf("nav to origin2: %v", err)
	}
	checkHop("cross-origin-localhost", "hop2")
	if n := s.EvaluateInt(`window.__tulaiCustomInit`); n != 1 {
		t.Fatalf("cross-origin custom init count = %d, want 1", n)
	}

	if err := s.Navigate(srv1.URL + "/redir?to=" + origin2 + "/landed"); err != nil {
		t.Fatalf("302 cross-origin redirect: %v", err)
	}
	checkHop("http-302-cross-origin", "hop3")

	if _, err := s.Evaluate(fmt.Sprintf(`location.href = %q`, srv1.URL+"/js-nav")); err != nil {
		t.Fatalf("js redirect: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.EvaluateBool(`location.pathname === '/js-nav'`) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	checkHop("client-js-cross-origin", "hop4")

	// Reload
	if err := s.client.proc.Call(t.Context(), s.target, "Page.reload", nil, nil); err != nil {
		t.Fatalf("reload: %v", err)
	}
	checkHop("after-reload", "hop5")

	// Rapid concurrent calls
	before := calls.Load()
	out, err := s.EvaluateAsync(`Promise.all([tulaiCall('echo','a'), tulaiCall('echo','b'), tulaiCall('echo','c')])`)
	if err != nil || out != `["a","b","c"]` {
		t.Fatalf("concurrent tulaiCall = %q, err=%v", out, err)
	}
	if after := calls.Load(); after != before+3 {
		t.Fatalf("concurrent calls count = %d, want 3", after-before)
	}
}

func TestFingerprintIsolatedSession(t *testing.T) {
	_, _, client := startFingerprintFixture(t, true)
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><title>iso-test</title><body>ok</body>`))
	}))
	defer srv1.Close()

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv2 := &httptest.Server{
		Listener: ln2,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><title>iso-origin2</title><body>origin2</body>`))
		})},
	}
	srv2.Start()
	defer srv2.Close()
	origin2 := fmt.Sprintf("http://localhost:%d", ln2.Addr().(*net.TCPAddr).Port)

	mainSess, err := client.NewBackgroundSession(srv1.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer mainSess.Close()
	if _, err := mainSess.Evaluate(`localStorage.setItem('k','main'); document.cookie='c=main; path=/'`); err != nil {
		t.Fatal(err)
	}

	iso, err := client.NewIsolatedSession(srv1.URL, nil)
	if err != nil {
		t.Fatalf("NewIsolatedSession failed: %v", err)
	}
	defer iso.Close()

	if !iso.EvaluateBool(`localStorage.getItem('k') === null && document.cookie === ''`) {
		t.Fatal("isolated session leaked storage/cookies from persistent context")
	}
	if !iso.EvaluateBool(`window.__tulaiBaseInstalled === 4`) {
		t.Fatal("isolated session missing base runtime")
	}
	iso.AppendNavigateScript(`window.__tulaiIsoInit = (window.__tulaiIsoInit || 0) + 1;`)
	var isoCalls atomic.Int32
	iso.Handle("isoEcho", func(v string) string {
		isoCalls.Add(1)
		return "iso:" + v
	})
	var got string
	if err := iso.EvaluateContext(t.Context(), `window.tulaiCall('isoEcho','first')`, &got); err != nil || got != "iso:first" {
		t.Fatalf("isolated session tulaiCall before nav = %q, err=%v", got, err)
	}
	if err := iso.Navigate(origin2); err != nil {
		t.Fatalf("isolated session cross-origin nav: %v", err)
	}
	got = ""
	if err := iso.EvaluateContext(t.Context(), `window.tulaiCall('isoEcho','second')`, &got); err != nil || got != "iso:second" {
		t.Fatalf("isolated session tulaiCall after cross-origin nav = %q, err=%v", got, err)
	}
	if n := isoCalls.Load(); n != 2 {
		t.Fatalf("isolated session echo calls = %d, want 2", n)
	}
	iso.Close()

	// Main session must remain healthy after closing isolated session.
	mainSess.Handle("mainEcho", func(v string) string { return "main:" + v })
	if err := mainSess.EvaluateContext(t.Context(), `window.tulaiCall('mainEcho','still-alive')`, &got); err != nil || got != "main:still-alive" {
		t.Fatalf("main session after iso.Close = %q, err=%v", got, err)
	}
}

func TestFingerprintCrossOriginIframeAndPageViewAndInputs(t *testing.T) {
	_, endpoint, client := startFingerprintFixture(t, true)

	// Cross-origin captcha iframe on localhost
	var sliderHits atomic.Int32
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv2 := &httptest.Server{
		Listener: ln2,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/slider-hit" {
				sliderHits.Add(1)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<!doctype html><style>body{margin:0}#track{position:relative;width:300px;height:40px}#aliyunCaptcha-sliding-slider{position:absolute;left:10px;top:4px;width:44px;height:32px;background:#333}</style><div id="track"><div id="aliyunCaptcha-sliding-slider" onmousedown="fetch('/slider-hit')"></div></div>`))
		})},
	}
	srv2.Start()
	defer srv2.Close()
	iframeURL := fmt.Sprintf("http://localhost:%d/punish-captcha", ln2.Addr().(*net.TCPAddr).Port)

	slowStarted := make(chan struct{}, 1)
	resBytes := []byte("secret-resource-payload-12345")
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			select {
			case slowStarted <- struct{}{}:
			default:
			}
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		if r.URL.Path == "/asset.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(resBytes)
			return
		}
		if r.URL.Path == "/denied" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprintf(w, `<!doctype html><title>Interactive Page</title><body style="margin:0">
			<div style="height:600px;background:#ee2222;position:relative">
				<button id="btn" onclick="this.dataset.clicked='yes'">ClickMe</button>
				<textarea id="ta"></textarea>
				<div id="ce" contenteditable="true" style="width:200px;height:60px;border:1px solid #000"></div>
				<button id="pick" onclick="document.getElementById('f').click()">Pick</button>
				<input id="f" type="file" hidden>
				<iframe src="about:blank" style="position:fixed;left:500px;top:10px;width:40px;height:40px;border:0"></iframe>
				<iframe src="%s" style="position:fixed;left:40px;top:120px;width:320px;height:80px;border:0"></iframe>
			</div>
			<div style="height:2400px;background:#2222ee"></div>
		</body>`, iframeURL)
	}))
	defer srv1.Close()

	// 1. OpenURLBackground + AttachPageView + Frame (top + scrolled!) + Input + LoginState
	if err := OpenURLBackground(t.Context(), endpoint, srv1.URL); err != nil {
		t.Fatalf("OpenURLBackground: %v", err)
	}
	pv, err := AttachPageView(t.Context(), endpoint, srv1.URL)
	if err != nil {
		t.Fatalf("AttachPageView: %v", err)
	}
	frame, err := pv.Frame(t.Context())
	if err != nil {
		pv.Close()
		t.Fatalf("PageView.Frame: %v", err)
	}
	if !bytes.HasPrefix(frame.JPEG, []byte{0xff, 0xd8, 0xff}) || frame.Width < 1280 || frame.Height < 600 || frame.Title != "Interactive Page" {
		pv.Close()
		t.Fatalf("bad PageView frame: jpeg=%d w=%.0f h=%.0f title=%q url=%q", len(frame.JPEG), frame.Width, frame.Height, frame.Title, frame.URL)
	}
	if _, err := pv.LoginState(t.Context()); err != nil {
		pv.Close()
		t.Fatalf("PageView.LoginState: %v", err)
	}
	// Scroll down past the red region and verify PageView.Frame captures the scrolled blue viewport.
	if _, err := pv.Input(t.Context(), PageInput{Kind: "mouseWheel", X: 600, Y: 300, DY: 900}); err != nil {
		pv.Close()
		t.Fatalf("PageView.Input mouseWheel: %v", err)
	}
	scrollDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(scrollDeadline) {
		frame, err = pv.Frame(t.Context())
		if err != nil {
			pv.Close()
			t.Fatalf("scrolled PageView.Frame failed: %v", err)
		}
		if frame.ScrollY > 650 {
			break
		}
		_, _ = pv.Input(t.Context(), PageInput{Kind: "mouseWheel", X: 600, Y: 300, DY: 900})
		time.Sleep(30 * time.Millisecond)
	}
	if frame.ScrollY <= 650 {
		pv.Close()
		t.Fatalf("page did not scroll: ScrollY=%v", frame.ScrollY)
	}
	bmp, _, err := image.Decode(bytes.NewReader(frame.JPEG))
	if err != nil {
		pv.Close()
		t.Fatalf("decode scrolled JPEG: %v", err)
	}
	r, _, b, _ := bmp.At(bmp.Bounds().Dx()/2, bmp.Bounds().Dy()/2).RGBA()
	if b <= r {
		pv.Close()
		t.Fatalf("scrolled PageView frame did not show blue region (r=%d b=%d)", r, b)
	}
	// Scroll back to top so fixed/top elements are visible for subsequent checks.
	_, _ = pv.Input(t.Context(), PageInput{Kind: "mouseWheel", X: 600, Y: 300, DY: -2000})
	pv.Close()

	// 2. SliderConn.Look in cross-origin OOPIF iframe (with decoy iframe present
	// so URL-based offset matching is exercised rather than single-iframe fallback).
	conn, err := DialSlider(t.Context(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var look SliderLook
	lookDeadline := time.Now().Add(3 * time.Second)
	for {
		look, err = conn.Look(t.Context())
		if err != nil {
			t.Fatalf("SliderConn.Look failed: %v", err)
		}
		if look.Found || time.Now().After(lookDeadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Logf("SliderConn.Look cross-origin iframe result: found=%v handle=%+v track=%+v", look.Found, look.Hit.Handle, look.Hit.Track)
	if !look.Found || look.Passed || look.Hit.TargetID == "" {
		t.Fatalf("expected SliderConn.Look to find slider in cross-origin OOPIF iframe, got %+v", look)
	}
	// Iframe sits at fixed viewport offset (40, 120); track is (0, 0, 300, 40) and
	// handle is (10, 4, 44, 32) inside the iframe. Allow 1 CSS px tolerance for
	// fingerprint-chromium's deterministic sub-pixel ClientRects noise while strictly
	// verifying the (40, 120) iframe offset was applied.
	assertSliderRectNear(t, "Handle", look.Hit.Handle, 50, 124, 44, 32, 1.0)
	assertSliderRectNear(t, "Track", look.Hit.Track, 40, 120, 300, 40, 1.0)
	clickX := look.Hit.Handle.X + look.Hit.Handle.W/2
	clickY := look.Hit.Handle.Y + look.Hit.Handle.H/2
	if err := conn.Play(t.Context(), look.Hit.TargetID, []SliderEvent{
		{Type: "mouseMoved", X: clickX, Y: clickY},
		{Type: "mousePressed", X: clickX, Y: clickY, Buttons: 1},
		{Type: "mouseReleased", X: clickX, Y: clickY},
	}); err != nil {
		t.Fatalf("SliderConn.Play on shifted OOPIF handle failed: %v", err)
	}
	conn.Close()
	hitDeadline := time.Now().Add(3 * time.Second)
	for sliderHits.Load() == 0 && time.Now().Before(hitDeadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if sliderHits.Load() == 0 {
		t.Fatalf("mouse click at shifted handle center (%.1f, %.1f) did not hit cross-origin OOPIF slider", clickX, clickY)
	}

	// 3. Session.Click, InsertText (textarea + contenteditable multiline), UploadFiles (filechooser), LoadResource
	sess, err := client.NewBackgroundSession(srv1.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	if err := sess.Click("#btn"); err != nil {
		t.Fatalf("Session.Click: %v", err)
	}
	if !sess.EvaluateBool(`document.getElementById('btn').dataset.clicked === 'yes'`) {
		t.Fatal("Session.Click did not click button")
	}
	if err := sess.InsertText("#ta", "line1\nline2"); err != nil {
		t.Fatalf("InsertText textarea: %v", err)
	}
	if !sess.EvaluateBool(`document.getElementById('ta').value === "line1\nline2"`) {
		t.Fatal("textarea value mismatch")
	}
	if err := sess.InsertText("#ce", "hello\nworld"); err != nil {
		t.Fatalf("InsertText contenteditable: %v", err)
	}
	ceText, _ := sess.Evaluate(`document.getElementById('ce').innerText`)
	if !strings.Contains(ceText, "hello") || !strings.Contains(ceText, "world") {
		t.Fatalf("contenteditable text mismatch: %q", ceText)
	}
	upFile := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(upFile, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sess.UploadFiles(`document.getElementById('pick').click()`, []string{upFile}); err != nil {
		t.Fatalf("UploadFiles (chooseUpload): %v", err)
	}
	if !sess.EvaluateBool(`document.getElementById('f').files[0]?.name === 'upload.txt'`) {
		t.Fatal("chooseUpload did not populate file input")
	}
	gotRes, err := sess.LoadResource(srv1.URL + "/asset.png")
	if err != nil || !bytes.Equal(gotRes, resBytes) {
		t.Fatalf("LoadResource = %q, err=%v", gotRes, err)
	}
	// Concurrent LoadResource downloads followed by an HTTP 403 must not hang
	// (regression check against eager CDP bridge attachment on temporary resource tabs).
	var resWG sync.WaitGroup
	for range 3 {
		resWG.Go(func() {
			if data, err := sess.LoadResource(srv1.URL + "/asset.png"); err != nil || !bytes.Equal(data, resBytes) {
				t.Errorf("concurrent LoadResource = %d bytes, err=%v", len(data), err)
			}
		})
	}
	resWG.Wait()
	if data, err := sess.LoadResource(srv1.URL + "/denied"); err == nil || !strings.Contains(err.Error(), "HTTP 403") || data != nil {
		t.Fatalf("LoadResource /denied = %d bytes, err=%v", len(data), err)
	}
	// Cancel an in-flight LoadResource and verify the temporary page closes cleanly.
	cancelCtx, cancelRes := context.WithCancel(t.Context())
	resDone := make(chan error, 1)
	go func() {
		_, err := sess.LoadResourceContext(cancelCtx, srv1.URL+"/slow")
		resDone <- err
	}()
	select {
	case <-slowStarted:
	case <-time.After(5 * time.Second):
		cancelRes()
		t.Fatal("slow resource request did not start")
	}
	cancelRes()
	if err := <-resDone; err == nil {
		t.Fatal("canceled LoadResource unexpectedly succeeded")
	}
	if title, err := sess.Evaluate(`document.title`); err != nil || !strings.Contains(title, "Interactive Page") {
		t.Fatalf("canceled LoadResource damaged main page: %q %v", title, err)
	}
}

func TestFingerprintHeadedLaunchAndPageView(t *testing.T) {
	_, endpoint, client := startFingerprintFixture(t, false)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><title>Headed FP</title><body><textarea></textarea></body>`))
	}))
	defer srv.Close()

	if err := OpenURL(endpoint, srv.URL); err != nil {
		t.Fatalf("OpenURL headed: %v", err)
	}
	state, err := ProbeLogin(t.Context(), endpoint)
	if err != nil || state != "in" {
		t.Fatalf("ProbeLogin headed = %q, err=%v", state, err)
	}
	pv, err := AttachPageView(t.Context(), endpoint, srv.URL)
	if err != nil {
		t.Fatalf("AttachPageView headed: %v", err)
	}
	defer pv.Close()
	frame, err := pv.Frame(t.Context())
	if err != nil || !bytes.HasPrefix(frame.JPEG, []byte{0xff, 0xd8, 0xff}) || frame.Width <= 0 {
		t.Fatalf("headed PageView.Frame failed: len=%d w=%.0f h=%.0f err=%v", len(frame.JPEG), frame.Width, frame.Height, err)
	}
	sess, err := client.NewBackgroundSession(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	sess.Handle("ping", func(v string) string { return "pong:" + v })
	var got string
	if err := sess.EvaluateContext(t.Context(), `window.tulaiCall('ping','headed')`, &got); err != nil || got != "pong:headed" {
		t.Fatalf("headed tulaiCall = %q, err=%v", got, err)
	}
}

func assertSliderRectNear(t *testing.T, label string, got SliderRect, wantX, wantY, wantW, wantH, tol float64) {
	t.Helper()
	if math.Abs(got.X-wantX) > tol || math.Abs(got.Y-wantY) > tol ||
		math.Abs(got.W-wantW) > tol || math.Abs(got.H-wantH) > tol {
		t.Fatalf("%s rect = %+v, want near {X:%.1f Y:%.1f W:%.1f H:%.1f} (tol=%.1f)",
			label, got, wantX, wantY, wantW, wantH, tol)
	}
}

func TestFingerprintProxyEnforcesWebRTCDisableNonProxiedUDP(t *testing.T) {
	config := os.Getenv("TULAI_FINGERPRINT_TEST_DATA")
	if _, ok := fingerprint.InstalledFingerprint(config); !ok || testing.Short() {
		t.Skip("requires a tulai-managed Fingerprint test runtime")
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}

	pageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><title>WebRTC Probe</title><body>probe</body>`))
	}))
	defer pageSrv.Close()

	var proxyHits atomic.Int32
	directTransport := &http.Transport{Proxy: nil}
	defer directTransport.CloseIdleConnections()
	proxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		target, err := url.Parse(r.RequestURI)
		if err != nil || target.Scheme == "" || target.Host == "" {
			http.Error(w, "bad proxy request", http.StatusBadRequest)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		resp, err := directTransport.RoundTrip(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for k, vals := range resp.Header {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxySrv.Close()

	type rtcSnap struct {
		Candidates      []string `json:"candidates"`
		HasUDPCandidate bool     `json:"hasUdpCandidate"`
		SDPHasUDP       bool     `json:"sdpHasUdp"`
	}
	type probeResult struct {
		rtcSnap
		Isolated   *rtcSnap
		UDPPackets int32
		ProxyHits  int32
		Cmdline    string
	}

	probeWebRTC := func(t *testing.T, sess *Session, udpPort int) rtcSnap {
		t.Helper()
		raw, err := sess.EvaluateAsync(fmt.Sprintf(`(async () => {
			const pc = new RTCPeerConnection({ iceServers: [{ urls: 'stun:127.0.0.1:%d' }] });
			const candidates = [];
			pc.createDataChannel('probe');
			const done = new Promise(resolve => {
				const timer = setTimeout(resolve, 1500);
				pc.onicecandidate = e => {
					if (e.candidate) candidates.push(e.candidate.candidate);
					else { clearTimeout(timer); resolve(); }
				};
			});
			const offer = await pc.createOffer();
			await pc.setLocalDescription(offer);
			await done;
			const sdp = pc.localDescription?.sdp || '';
			pc.close();
			return {
				candidates,
				hasUdpCandidate: candidates.some(c => /\budp\b/i.test(c)),
				sdpHasUdp: /a=candidate:.*\budp\b/i.test(sdp),
			};
		})()`, udpPort))
		if err != nil {
			t.Fatalf("WebRTC probe eval failed: %v", err)
		}
		var snap rtcSnap
		if err := json.Unmarshal([]byte(raw), &snap); err != nil {
			t.Fatalf("unmarshal WebRTC probe %q: %v", raw, err)
		}
		return snap
	}

	runProbe := func(t *testing.T, handle int, proxyURL string, directNetwork, checkIsolated bool) probeResult {
		t.Helper()
		// Seed profile Preferences with webrtc.ip_handling_policy="default" so the test
		// proves the launch switch actively overrides profile prefs rather than relying
		// on a fresh profile's default setting.
		profile := filepath.Join(t.TempDir(), "fp-rtc-profile")
		if err := os.MkdirAll(filepath.Join(profile, "Default"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(profile, "Default", "Preferences"),
			[]byte(`{"webrtc":{"ip_handling_policy":"default"}}`),
			0o600,
		); err != nil {
			t.Fatal(err)
		}

		udpConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		udpPort := udpConn.LocalAddr().(*net.UDPAddr).Port
		var udpPackets atomic.Int32
		var wg sync.WaitGroup
		wg.Go(func() {
			buf := make([]byte, 2048)
			for {
				if _, _, err := udpConn.ReadFrom(buf); err != nil {
					return
				}
				udpPackets.Add(1)
			}
		})
		defer func() {
			_ = udpConn.Close()
			wg.Wait()
		}()

		hitsBefore := proxyHits.Load()
		proc, err := fingerprint.Start(t.Context(), config, fingerprint.Options{
			Headless:      true,
			Profile:       profile,
			Proxy:         proxyURL,
			DirectNetwork: directNetwork,
		}, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		defer proc.Close()
		fingerprint.Register(handle, proc)
		defer fingerprint.Unregister(handle, proc)

		client, err := NewClient(fingerprint.FingerprintEndpoint(handle))
		if err != nil {
			t.Fatal(err)
		}
		defer client.Close()

		sess, err := client.NewBackgroundSession(pageSrv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer sess.Close()

		res := probeResult{rtcSnap: probeWebRTC(t, sess, udpPort)}
		if checkIsolated {
			iso, err := client.NewIsolatedSession(pageSrv.URL, nil)
			if err != nil {
				t.Fatalf("NewIsolatedSession under proxied Fingerprint: %v", err)
			}
			isoSnap := probeWebRTC(t, iso, udpPort)
			iso.Close()
			res.Isolated = &isoSnap
		}
		res.UDPPackets = udpPackets.Load()
		res.ProxyHits = proxyHits.Load() - hitsBefore

		if err := sess.Navigate("chrome://version"); err != nil {
			t.Fatalf("navigate chrome://version: %v", err)
		}
		cmdline, err := sess.Evaluate(`document.getElementById('command_line')?.innerText || ''`)
		if err != nil {
			t.Fatalf("read chrome://version command_line: %v", err)
		}
		res.Cmdline = cmdline
		return res
	}

	proxied := runProbe(t, 9701, proxySrv.URL, false, true)
	t.Logf("proxied WebRTC probe: proxyHits=%d udpPackets=%d hasUdp=%v sdpHasUdp=%v candidates=%v isolated=%+v",
		proxied.ProxyHits, proxied.UDPPackets, proxied.HasUDPCandidate, proxied.SDPHasUDP, proxied.Candidates, proxied.Isolated)
	if proxied.ProxyHits == 0 {
		t.Fatal("proxied Fingerprint did not route HTTP probe traffic through configured proxy")
	}
	if !strings.Contains(proxied.Cmdline, "--webrtc-ip-handling-policy=disable_non_proxied_udp") {
		t.Fatalf("proxied launch missing --webrtc-ip-handling-policy=disable_non_proxied_udp in cmdline: %s", proxied.Cmdline)
	}
	if strings.Contains(proxied.Cmdline, "--no-proxy-server") {
		t.Fatalf("proxied launch unexpectedly passed --no-proxy-server: %s", proxied.Cmdline)
	}
	if proxied.UDPPackets != 0 || proxied.HasUDPCandidate || proxied.SDPHasUDP ||
		proxied.Isolated == nil || proxied.Isolated.HasUDPCandidate || proxied.Isolated.SDPHasUDP {
		t.Fatalf("proxied Fingerprint leaked non-proxied WebRTC UDP: packets=%d hasUdp=%v sdpHasUdp=%v candidates=%v isolated=%+v",
			proxied.UDPPackets, proxied.HasUDPCandidate, proxied.SDPHasUDP, proxied.Candidates, proxied.Isolated)
	}

	direct := runProbe(t, 9702, "", true, false)
	t.Logf("direct-network control WebRTC probe: proxyHits=%d udpPackets=%d hasUdp=%v sdpHasUdp=%v candidates=%v",
		direct.ProxyHits, direct.UDPPackets, direct.HasUDPCandidate, direct.SDPHasUDP, direct.Candidates)
	if direct.ProxyHits != 0 {
		t.Fatalf("direct-network launch unexpectedly contacted proxySrv (%d hits)", direct.ProxyHits)
	}
	if !strings.Contains(direct.Cmdline, "--no-proxy-server") {
		t.Fatalf("direct launch missing --no-proxy-server in cmdline: %s", direct.Cmdline)
	}
	if strings.Contains(direct.Cmdline, "--webrtc-ip-handling-policy") {
		t.Fatalf("direct launch unexpectedly passed --webrtc-ip-handling-policy: %s", direct.Cmdline)
	}
	if direct.UDPPackets == 0 && !direct.HasUDPCandidate && !direct.SDPHasUDP {
		t.Fatalf("direct-network control with pref=default showed no WebRTC UDP activity; probe may be vacuous")
	}
}

func TestFingerprintCDPStealthAndEdgeSerialization(t *testing.T) {
	config := os.Getenv("TULAI_FINGERPRINT_TEST_DATA")
	if _, ok := fingerprint.InstalledFingerprint(config); !ok || testing.Short() {
		t.Skip("requires a tulai-managed Fingerprint test runtime")
	}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(name, "")
	}

	var chMu sync.Mutex
	var lastPlatformVersionHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; worker-src blob:")
		w.Header().Set("Accept-CH", "Sec-CH-UA-Platform-Version")
		if v := r.Header.Get("Sec-CH-UA-Platform-Version"); v != "" {
			chMu.Lock()
			lastPlatformVersionHeader = strings.Trim(v, `"`)
			chMu.Unlock()
		}
		if r.URL.Path == "/trap.js" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`
				window.__cdpTraps = {
					dispatchEvents: [],
					customEvents: [],
					qsStacks: [],
					getEventListenersHits: 0,
					protoTraps: [],
					cdpConsoleStack: 0,
					cdpConsoleObj: 0,
					cdpConsoleDiv: 0,
					cdpPrepStack: 0,
					cspBlocked: false,
					windowLeaks: [],
					symbolLeaks: [],
					screenX: screenX,
					screenY: screenY,
				};
				try { eval('1+1'); } catch (_) { window.__cdpTraps.cspBlocked = true; }
				window.__cdpTraps.windowLeaks = Object.getOwnPropertyNames(window).filter(k =>
					/^(\$?cdc_|__webdriver|__driver|__fxdriver|__selenium|__playwright|__pw|domAutomation|__tulai|tulai|_[0-9a-f]{32}|getEventListeners)/i.test(k)
				);
				window.__cdpTraps.symbolLeaks = Object.getOwnPropertySymbols(window).map(String);
				Object.defineProperty(window, 'getEventListeners', {
					get() { window.__cdpTraps.getEventListenersHits++; return undefined; },
					configurable: true,
				});
				for (const prop of ['runner', 'done', 'bubbles', 'cancelable', 'composed']) {
					Object.defineProperty(Object.prototype, prop, {
						get() { window.__cdpTraps.protoTraps.push('get:' + prop); return false; },
						set() { window.__cdpTraps.protoTraps.push('set:' + prop); },
						configurable: true,
					});
				}
				const origDispatch = EventTarget.prototype.dispatchEvent;
				EventTarget.prototype.dispatchEvent = function(ev) {
					if (ev && ev.type) window.__cdpTraps.dispatchEvents.push(ev.type);
					return origDispatch.apply(this, arguments);
				};
				const OrigCE = window.CustomEvent;
				window.CustomEvent = function(type, init) {
					window.__cdpTraps.customEvents.push(type);
					return new OrigCE(type, init);
				};
				window.CustomEvent.prototype = OrigCE.prototype;
				const origQS = Document.prototype.querySelector;
				Document.prototype.querySelector = function(sel) {
					if (sel === '#probe-btn') window.__cdpTraps.qsStacks.push((new Error('qs')).stack || '');
					return origQS.apply(this, arguments);
				};
				const e1 = new Error('cdp-stack');
				Object.defineProperty(e1, 'stack', { get() { window.__cdpTraps.cdpConsoleStack++; return ''; } });
				console.debug(e1);
				console.log(e1);
				const o1 = {};
				Object.defineProperty(o1, 'p', { get() { window.__cdpTraps.cdpConsoleObj++; return 1; } });
				console.debug(o1);
				const d1 = document.createElement('div');
				Object.defineProperty(d1, 'id', { get() { window.__cdpTraps.cdpConsoleDiv++; return 'x'; } });
				console.debug(d1);
				const origPrep = Error.prepareStackTrace;
				Error.prepareStackTrace = function() { window.__cdpTraps.cdpPrepStack++; return ''; };
				console.debug(new Error('cdp-prep'));
				Error.prepareStackTrace = origPrep;
			`))
			return
		}
		if r.URL.Path == "/frame.js" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(`
				window.__frameTrap = { stack: 0, obj: 0, webdriver: navigator.webdriver };
				const e = new Error('f');
				Object.defineProperty(e, 'stack', { get() { window.__frameTrap.stack++; return ''; } });
				console.debug(e);
				const o = {};
				Object.defineProperty(o, 'x', { get() { window.__frameTrap.obj++; return 1; } });
				console.debug(o);
			`))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/frame" {
			_, _ = w.Write([]byte(`<!doctype html><head><script src="/frame.js"></script></head><body>frame</body>`))
			return
		}
		_, _ = w.Write([]byte(`<!doctype html><head><script src="/trap.js"></script></head><body><button id="probe-btn">OK</button><iframe id="f" src="/frame"></iframe></body>`))
	}))
	defer server.Close()

	profile := filepath.Join(t.TempDir(), "fp-cdp-stealth")
	proc, err := fingerprint.Start(t.Context(), config, fingerprint.Options{
		Headless:      true,
		Profile:       profile,
		DirectNetwork: true,
	}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	handle := 9751
	fingerprint.Register(handle, proc)
	defer fingerprint.Unregister(handle, proc)

	client, err := NewClient(fingerprint.FingerprintEndpoint(handle))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	s, err := client.NewSession(server.URL+"/first", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Register base runtime across navigations (same pattern as production sessions)
	// and navigate a second time so Accept-CH Client Hints are sent on the request.
	s.AppendNavigateScript(`window.__tulaiStealthReady = 1;`)
	if err := s.Navigate(server.URL + "/second"); err != nil {
		t.Fatal(err)
	}

	s.Handle("stealthEcho", func(v string) string { return `{"echo":"` + v + `"}` })
	if got, err := s.Evaluate(`document.querySelector('#probe-btn')?.textContent`); err != nil || got != `"OK"` {
		t.Fatalf("querySelector eval failed: got=%q err=%v", got, err)
	}
	if ans, err := s.EvaluateAsync(`tulaiCall('stealthEcho', 'clean')`); err != nil || !strings.Contains(ans, "clean") {
		t.Fatalf("tulaiCall under hooked dispatchEvent/CustomEvent failed: ans=%q err=%v", ans, err)
	}

	// Verify edge-case serialization and error propagation through the direct CDP runner.
	if got, err := s.Evaluate(`document.body`); err != nil || got != "null" {
		t.Fatalf("DOM node return failed: got=%q err=%v", got, err)
	}
	if got, err := s.Evaluate(`document.body.getBoundingClientRect()`); err != nil || !strings.Contains(got, `"width":`) || !strings.Contains(got, `"height":`) {
		t.Fatalf("DOMRect toJSON serialization failed: got=%q err=%v", got, err)
	}
	if got, err := s.Evaluate(`new URL('https://example.test/a')`); err != nil || got != `"https://example.test/a"` {
		t.Fatalf("URL toJSON serialization failed: got=%q err=%v", got, err)
	}
	if got, err := s.Evaluate(`({toJSON(){return {custom:7}}})`); err != nil || got != `{"custom":7}` {
		t.Fatalf("custom toJSON serialization failed: got=%q err=%v", got, err)
	}
	if got, err := s.Evaluate(`new Date(NaN)`); err != nil || got != "null" {
		t.Fatalf("Invalid Date serialization failed: got=%q err=%v", got, err)
	}
	if got, err := s.Evaluate(`JSON.parse('{"__proto__":{"p":1},"a":2}')`); err != nil || !strings.Contains(got, `"a":2`) {
		t.Fatalf("__proto__ object serialization failed: got=%q err=%v", got, err)
	}
	if got, err := s.Evaluate(`(()=>{const a={x:1};a.self=a;return a})()`); err != nil || !strings.Contains(got, `"x":1`) {
		t.Fatalf("circular object return failed: got=%q err=%v", got, err)
	}
	if got, err := s.Evaluate(`()=>42`); err != nil || got != "null" {
		t.Fatalf("function return failed: got=%q err=%v", got, err)
	}
	if _, err := s.Evaluate(`var retainedTopVar = 40; retainedTopVar + 2`); err != nil {
		t.Fatalf("top-level var eval failed: %v", err)
	}
	if got, err := s.Evaluate(`retainedTopVar + 2`); err != nil || got != "42" {
		t.Fatalf("top-level var did not persist across CDP runner calls: got=%q err=%v", got, err)
	}
	if _, err := s.Evaluate(`throw new Error('sync-boom')`); err == nil || !strings.Contains(err.Error(), "sync-boom") {
		t.Fatalf("sync error did not propagate: %v", err)
	}
	if _, err := s.EvaluateAsync(`Promise.reject(new Error('async-boom'))`); err == nil || !strings.Contains(err.Error(), "async-boom") {
		t.Fatalf("async rejection did not propagate: %v", err)
	}

	// Collect all trap telemetry (including iframe + Web Worker CDP checks + UA-CH).
	raw, err := s.EvaluateAsync(`(async () => {
		const code = "let s=0,o=0;const e=new Error('w');Object.defineProperty(e,'stack',{get(){s++;return ''}});console.debug(e);const x={};Object.defineProperty(x,'a',{get(){o++;return 1}});console.debug(x);setTimeout(()=>postMessage({stack:s,obj:o,webdriver:navigator.webdriver??null}),120);";
		const u = URL.createObjectURL(new Blob([code], { type: 'text/javascript' }));
		const w = new Worker(u);
		const workerTrap = await new Promise((resolve, reject) => { w.onmessage = e => resolve(e.data); w.onerror = reject; });
		w.terminate(); URL.revokeObjectURL(u);
		const uaHigh = navigator.userAgentData ? await navigator.userAgentData.getHighEntropyValues(['platformVersion']) : null;
		return {
			traps: window.__cdpTraps,
			frame: document.getElementById('f').contentWindow.__frameTrap,
			worker: workerTrap,
			platformVersion: uaHigh?.platformVersion || '',
			screenX: screenX,
			screenY: screenY,
		};
	})()`)
	if err != nil {
		t.Fatalf("collect traps failed: %v", err)
	}
	var report struct {
		Traps struct {
			DispatchEvents        []string `json:"dispatchEvents"`
			CustomEvents          []string `json:"customEvents"`
			QSStacks              []string `json:"qsStacks"`
			GetEventListenersHits int      `json:"getEventListenersHits"`
			ProtoTraps            []string `json:"protoTraps"`
			CDPConsoleStack       int      `json:"cdpConsoleStack"`
			CDPConsoleObj         int      `json:"cdpConsoleObj"`
			CDPConsoleDiv         int      `json:"cdpConsoleDiv"`
			CDPPrepStack          int      `json:"cdpPrepStack"`
			CSPBlocked            bool     `json:"cspBlocked"`
			WindowLeaks           []string `json:"windowLeaks"`
			SymbolLeaks           []string `json:"symbolLeaks"`
		} `json:"traps"`
		Frame struct {
			Stack     int  `json:"stack"`
			Obj       int  `json:"obj"`
			Webdriver bool `json:"webdriver"`
		} `json:"frame"`
		Worker struct {
			Stack     int   `json:"stack"`
			Obj       int   `json:"obj"`
			Webdriver *bool `json:"webdriver"`
		} `json:"worker"`
		PlatformVersion string `json:"platformVersion"`
		ScreenX         int    `json:"screenX"`
		ScreenY         int    `json:"screenY"`
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("unmarshal traps %q: %v", raw, err)
	}
	if !report.Traps.CSPBlocked {
		t.Fatalf("strict CSP fixture did not block page eval: %s", raw)
	}
	if report.Traps.GetEventListenersHits != 0 || len(report.Traps.ProtoTraps) != 0 {
		t.Fatalf("honeypot getEventListeners / Object.prototype traps fired: %s", raw)
	}
	if len(report.Traps.DispatchEvents) != 0 || len(report.Traps.CustomEvents) != 0 {
		t.Fatalf("page EventTarget.dispatchEvent / CustomEvent hooks detected tulai events: %s", raw)
	}
	if report.Traps.CDPConsoleStack != 0 || report.Traps.CDPConsoleObj != 0 || report.Traps.CDPConsoleDiv != 0 || report.Traps.CDPPrepStack != 0 {
		t.Fatalf("main-world Runtime.enable CDP serialization trap triggered: %s", raw)
	}
	if report.Frame.Stack != 0 || report.Frame.Obj != 0 || report.Frame.Webdriver {
		t.Fatalf("iframe CDP/webdriver trap triggered: %s", raw)
	}
	if report.Worker.Stack != 0 || report.Worker.Obj != 0 || (report.Worker.Webdriver != nil && *report.Worker.Webdriver) {
		t.Fatalf("worker CDP/webdriver trap triggered: %s", raw)
	}
	if len(report.Traps.WindowLeaks) != 0 || len(report.Traps.SymbolLeaks) != 0 {
		t.Fatalf("window leaked properties or symbols: %s", raw)
	}
	if len(report.Traps.QSStacks) == 0 {
		t.Fatalf("expected hooked querySelector stack capture: %s", raw)
	}
	for _, stack := range report.Traps.QSStacks {
		lower := strings.ToLower(stack)
		for _, banned := range []string{"utilityscript", "playwright", "dispatchevent", "customevent", "tulai", "runner"} {
			if strings.Contains(lower, banned) {
				t.Fatalf("hooked DOM stack leaked automation signature %q:\n%s", banned, stack)
			}
		}
	}

	idRaw, err := os.ReadFile(filepath.Join(profile, ".tulai-fingerprint.json"))
	if err != nil {
		t.Fatalf("read persisted identity: %v", err)
	}
	var persisted struct {
		PlatformVersion string `json:"platformVersion"`
		Screen          struct {
			WindowX int `json:"windowX"`
			WindowY int `json:"windowY"`
		} `json:"screen"`
	}
	if err := json.Unmarshal(idRaw, &persisted); err != nil {
		t.Fatalf("parse identity %s: %v", idRaw, err)
	}
	if persisted.PlatformVersion == "" || report.PlatformVersion != persisted.PlatformVersion {
		t.Fatalf("JS platformVersion=%q does not match persisted %q", report.PlatformVersion, persisted.PlatformVersion)
	}
	chMu.Lock()
	hdrPV := lastPlatformVersionHeader
	chMu.Unlock()
	if hdrPV != "" && hdrPV != persisted.PlatformVersion {
		t.Fatalf("HTTP Sec-CH-UA-Platform-Version=%q does not match persisted %q", hdrPV, persisted.PlatformVersion)
	}
	if report.ScreenX != persisted.Screen.WindowX || report.ScreenY != persisted.Screen.WindowY {
		t.Fatalf("headless screenX/Y=(%d,%d) != persisted windowX/Y=(%d,%d)",
			report.ScreenX, report.ScreenY, persisted.Screen.WindowX, persisted.Screen.WindowY)
	}
	// Verify temporary runner handoff slot was immediately deleted from window.
	if leftover, err := s.Evaluate(`Object.getOwnPropertyNames(document.defaultView).filter(k => /^_[0-9a-f]{32}$/i.test(k))`); err != nil || leftover != "[]" {
		t.Fatalf("leftover hidden slot on window: %q err=%v", leftover, err)
	}
}

func TestFingerprintAdversarialPrototypeTamperOOPIFAndClick(t *testing.T) {
	_, endpoint, client := startFingerprintFixture(t, true)

	// Cross-origin server (localhost) hosting both a navigation target and a captcha OOPIF iframe.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv2 := &httptest.Server{
		Listener: ln2,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			if r.URL.Path == "/punish-captcha" {
				_, _ = w.Write([]byte(`<!doctype html><script>
					window.__iframeTraps = { dispatch: [], ce: [], qsaHits: 0 };
					const od = EventTarget.prototype.dispatchEvent;
					EventTarget.prototype.dispatchEvent = function(e) {
						if (e && e.type) window.__iframeTraps.dispatch.push(e.type);
						return od.apply(this, arguments);
					};
					const OCE = window.CustomEvent;
					window.CustomEvent = function(t, i) {
						window.__iframeTraps.ce.push(t);
						return new OCE(t, i);
					};
					const oqsa = Document.prototype.querySelectorAll;
					Document.prototype.querySelectorAll = function() {
						window.__iframeTraps.qsaHits++;
						return oqsa.apply(this, arguments);
					};
					window.addEventListener('message', ev => {
						if (ev.data === 'dump') parent.postMessage({ from: 'iframe', traps: window.__iframeTraps }, '*');
					});
				</script><style>body{margin:0}#track{position:relative;width:300px;height:40px}#aliyunCaptcha-sliding-slider{position:absolute;left:10px;top:4px;width:44px;height:32px;background:#333}</style><div id="track"><div id="aliyunCaptcha-sliding-slider"></div></div>`))
				return
			}
			_, _ = w.Write([]byte(`<!doctype html><script>
				window.__xoTraps = { dispatch: [], ce: [], qsStacks: [], getELHits: 0 };
				Object.defineProperty(window, 'getEventListeners', {
					get() { window.__xoTraps.getELHits++; return undefined; },
					configurable: true,
				});
				const od = EventTarget.prototype.dispatchEvent;
				EventTarget.prototype.dispatchEvent = function(e) {
					if (e && e.type) window.__xoTraps.dispatch.push(e.type);
					return od.apply(this, arguments);
				};
				const OCE = window.CustomEvent;
				window.CustomEvent = function(t, i) {
					window.__xoTraps.ce.push(t);
					return new OCE(t, i);
				};
				const oqs = Document.prototype.querySelector;
				Document.prototype.querySelector = function(sel) {
					if (sel === '#xo-btn') window.__xoTraps.qsStacks.push((new Error('qs')).stack || '');
					return oqs.apply(this, arguments);
				};
			</script><body><button id="xo-btn">XO</button></body>`))
		})},
	}
	srv2.Start()
	defer srv2.Close()
	origin2 := fmt.Sprintf("http://localhost:%d", ln2.Addr().(*net.TCPAddr).Port)

	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/proto-tamper":
			_, _ = w.Write([]byte(`<!doctype html><script>
				Set.prototype.has = function() { throw new Error('Set.has tampered'); };
				Set.prototype.add = function() { throw new Error('Set.add tampered'); };
				Set.prototype.delete = function() { throw new Error('Set.delete tampered'); };
				Array.prototype.push = function() { throw new Error('Array.push tampered'); };
				Date.prototype.toISOString = function() { throw new Error('Date.toISOString tampered'); };
				window.String = function() { throw new Error('String tampered'); };
				Object.getPrototypeOf((function*(){})()).next = function() { throw new Error('Gen.next tampered'); };
			</script><body><button id="pbtn">P</button></body>`))
		default:
			_, _ = w.Write([]byte(fmt.Sprintf(`<!doctype html><body>
				<button id="cbtn" style="position:fixed;left:220px;top:160px;width:120px;height:40px">Click</button>
				<iframe id="cap" src="%s/punish-captcha" style="position:fixed;left:40px;top:240px;width:320px;height:80px;border:0"></iframe>
				<script>
					window.__events = [];
					for (const t of ['mousemove','mousedown','mouseup','click']) {
						window.addEventListener(t, e => {
							window.__events.push({ type: t, x: Math.round(e.clientX), y: Math.round(e.clientY), trusted: e.isTrusted });
						}, true);
					}
					window.__iframeReport = null;
					window.addEventListener('message', ev => { if (ev.data?.from === 'iframe') window.__iframeReport = ev.data.traps; });
				</script>
			</body>`, origin2)))
		}
	}))
	defer srv1.Close()

	s, err := client.NewSession(srv1.URL+"/proto-tamper", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.AppendNavigateScript(`window.__tulaiReady = 1;`)
	s.Handle("tamperEcho", func(v string) string { return `{"echo":"` + v + `"}` })

	// 1. Prototype tampering must not break evaluation, serialization, or tulaiCall.
	if err := s.Navigate(srv1.URL + "/proto-tamper"); err != nil {
		t.Fatal(err)
	}
	if out, err := s.EvaluateAsync(`({a: [1, 2], d: new Date(1700000000000)})`); err != nil || !strings.Contains(out, `"a":[1,2]`) || !strings.Contains(out, "2023-11-14") {
		t.Fatalf("evaluation under prototype tampering failed: out=%q err=%v", out, err)
	}
	if ans, err := s.EvaluateAsync(`tulaiCall('tamperEcho', 'survived')`); err != nil || !strings.Contains(ans, "survived") {
		t.Fatalf("tulaiCall under prototype tampering failed: ans=%q err=%v", ans, err)
	}

	// 2. Cross-origin navigation must retain zero-leak direct CDP execution.
	if err := s.Navigate(origin2 + "/xo"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Evaluate(`document.querySelector('#xo-btn')?.textContent`); err != nil || got != `"XO"` {
		t.Fatalf("cross-origin querySelector failed: got=%q err=%v", got, err)
	}
	if ans, err := s.EvaluateAsync(`tulaiCall('tamperEcho', 'xo-clean')`); err != nil || !strings.Contains(ans, "xo-clean") {
		t.Fatalf("cross-origin tulaiCall failed: ans=%q err=%v", ans, err)
	}
	xoRaw, err := s.Evaluate(`window.__xoTraps`)
	if err != nil {
		t.Fatalf("read __xoTraps: %v", err)
	}
	var xoTraps struct {
		Dispatch  []string `json:"dispatch"`
		CE        []string `json:"ce"`
		QSStacks  []string `json:"qsStacks"`
		GetELHits int      `json:"getELHits"`
	}
	if err := json.Unmarshal([]byte(xoRaw), &xoTraps); err != nil {
		t.Fatalf("parse __xoTraps %q: %v", xoRaw, err)
	}
	if xoTraps.GetELHits != 0 || len(xoTraps.Dispatch) != 0 || len(xoTraps.CE) != 0 || len(xoTraps.QSStacks) == 0 {
		t.Fatalf("cross-origin stealth traps triggered: %s", xoRaw)
	}
	for _, stack := range xoTraps.QSStacks {
		lower := strings.ToLower(stack)
		for _, banned := range []string{"utilityscript", "playwright", "dispatchevent", "customevent", "tulai", "runner"} {
			if strings.Contains(lower, banned) {
				t.Fatalf("cross-origin stack leaked %q:\n%s", banned, stack)
			}
		}
	}

	// 3. Cross-origin OOPIF captcha iframe probe (SliderConn.Look) must run in a genuine isolated world.
	if err := s.Navigate(srv1.URL + "/click-oopif"); err != nil {
		t.Fatal(err)
	}
	conn, err := DialSlider(t.Context(), endpoint)
	if err != nil {
		t.Fatal(err)
	}
	var look SliderLook
	deadline := time.Now().Add(4 * time.Second)
	for {
		look, err = conn.Look(t.Context())
		if err != nil {
			conn.Close()
			t.Fatalf("SliderConn.Look: %v", err)
		}
		if look.Found || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	conn.Close()
	if !look.Found {
		t.Fatalf("expected SliderConn.Look to locate OOPIF captcha slider, got %+v", look)
	}
	iframeRaw, err := s.EvaluateAsync(`(async()=>{
		document.getElementById('cap').contentWindow.postMessage('dump', '*');
		await new Promise(r=>setTimeout(r, 150));
		return window.__iframeReport;
	})()`)
	if err != nil {
		t.Fatalf("read iframe traps: %v", err)
	}
	var iframeTraps struct {
		Dispatch []string `json:"dispatch"`
		CE       []string `json:"ce"`
		QSAHits  int      `json:"qsaHits"`
	}
	if err := json.Unmarshal([]byte(iframeRaw), &iframeTraps); err != nil {
		t.Fatalf("parse iframe traps %q: %v", iframeRaw, err)
	}
	if len(iframeTraps.Dispatch) != 0 || len(iframeTraps.CE) != 0 || iframeTraps.QSAHits != 0 {
		t.Fatalf("OOPIF captcha iframe main world observed probe: %s", iframeRaw)
	}

	// 4. Click must use a multi-step mouse approach with no duplicate mousemove between mousedown and mouseup.
	if err := s.Click("#cbtn"); err != nil {
		t.Fatalf("s.Click: %v", err)
	}
	evRaw, err := s.Evaluate(`window.__events`)
	if err != nil {
		t.Fatalf("read click events: %v", err)
	}
	var evs []struct {
		Type    string `json:"type"`
		X       int    `json:"x"`
		Y       int    `json:"y"`
		Trusted bool   `json:"trusted"`
	}
	if err := json.Unmarshal([]byte(evRaw), &evs); err != nil {
		t.Fatalf("parse click events %q: %v", evRaw, err)
	}
	downIdx, upIdx, movesBeforeDown, movesBetween := -1, -1, 0, 0
	for i, ev := range evs {
		if !ev.Trusted {
			t.Fatalf("untrusted click event %+v in %s", ev, evRaw)
		}
		if ev.Type == "mousedown" && downIdx == -1 {
			downIdx = i
		} else if ev.Type == "mouseup" && upIdx == -1 {
			upIdx = i
		}
	}
	if downIdx < 0 || upIdx <= downIdx {
		t.Fatalf("missing mousedown/mouseup order in %s", evRaw)
	}
	for i := 0; i < downIdx; i++ {
		if evs[i].Type == "mousemove" {
			movesBeforeDown++
		}
	}
	for i := downIdx + 1; i < upIdx; i++ {
		if evs[i].Type == "mousemove" {
			movesBetween++
		}
	}
	if movesBeforeDown < 2 {
		t.Fatalf("expected multi-step mousemove approach before mousedown, got %d in %s", movesBeforeDown, evRaw)
	}
	if movesBetween != 0 {
		t.Fatalf("unexpected synthetic mousemove between mousedown and mouseup (%d) in %s", movesBetween, evRaw)
	}
}
