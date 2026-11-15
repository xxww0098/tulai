package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/camoufoxtest"
	"github.com/xxww0098/tulai/internal/store"
)

const visibleDevice = `JSON.stringify([navigator.platform,navigator.oscpu,navigator.hardwareConcurrency,screen.width,screen.height,screen.availWidth,screen.availHeight,screen.colorDepth])`

func checkVisibleDevice(t *testing.T, proc *camoufox.Process, profile string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(profile, ".tulai-identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	fallback := func(key, other string) any {
		if value, ok := config[key]; ok && value != nil {
			return value
		}
		return config[other]
	}
	want, err := json.Marshal([]any{config["navigator.platform"], config["navigator.oscpu"], config["navigator.hardwareConcurrency"],
		config["screen.width"], config["screen.height"], fallback("screen.availWidth", "screen.width"),
		fallback("screen.availHeight", "screen.height"), config["screen.colorDepth"]})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out struct {
		Result           struct{ Value string }
		ExceptionDetails any
	}
	if err := proc.Call(ctx, proc.Page, "Runtime.evaluate", map[string]any{"expression": visibleDevice}, &out); err != nil {
		t.Fatal(err)
	}
	if out.ExceptionDetails != nil || out.Result.Value != string(want) {
		t.Fatalf("saved and page-visible device fingerprints differ:\n%s\n%s\n%v", want, out.Result.Value, out.ExceptionDetails)
	}
	return out.Result.Value
}

func TestCamoufoxStartsOnlyAccountWindow(t *testing.T) {
	config := camoufoxtest.ConfigDir(t)
	t.Setenv("DEBUG", "pw:browser")
	for _, headless := range []bool{true, false} {
		if !headless && runtime.GOOS != "darwin" {
			continue
		}
		t.Run(fmt.Sprintf("headless=%t", headless), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), camoufoxtest.StartTimeout)
			defer cancel()
			var diagnostics bytes.Buffer
			profile := t.TempDir()
			proc, err := camoufox.Start(ctx, config, camoufox.Options{Profile: profile, Headless: headless}, &diagnostics)
			if err != nil {
				t.Fatalf("启动失败: %v\n%s", err, diagnostics.String())
			}
			t.Cleanup(proc.Close)
			checkVisibleDevice(t, proc, profile)
			proc.Close()
			windows, launches := 0, 0
			for _, line := range strings.Split(diagnostics.String(), "\n") {
				if !strings.Contains(line, "<launching>") {
					continue
				}
				launches++
				if !strings.Contains(line, "-juggler-pipe") || strings.Contains(line, "--remote-debugging-") || strings.Contains(line, " -juggler ") {
					t.Fatalf("浏览器控制通道不是 Juggler 私有管道: %s", line)
				}
				if !strings.Contains(line, " -headless ") {
					windows++
				}
			}
			if launches == 0 {
				t.Fatal("没有找到浏览器启动参数")
			}
			want := 0
			if !headless {
				want = 1
			}
			if windows != want {
				t.Fatalf("只应显示账号窗口，实际启动 %d 个有头浏览器，预期 %d 个:\n%s", windows, want, diagnostics.String())
			}
		})
	}
}

func TestCamoufoxMacHeadlessProbeMatchesWindowGPU(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS 有头/无头显卡一致性")
	}
	config := camoufoxtest.ConfigDir(t)
	rt, _ := camoufox.Installed(config)
	ctx, cancel := context.WithTimeout(context.Background(), camoufoxtest.StartTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, rt.Node, "--input-type=module", "-e", `
import assert from 'node:assert/strict';
import {hostGpu} from 'camoufox/dist/host_rendering.js';
const executable = process.argv[1];
const [headless, headed] = await Promise.all([
  hostGpu(executable, true, process.env, {}),
  hostGpu(executable, false, process.env, {}),
]);
assert.deepEqual(headless, headed);
`, rt.Executable)
	cmd.Dir = rt.Dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("显卡探测结果不一致: %v\n%s", err, output)
	}
}

func TestBackgroundTaskReusesHeadlessAndPreservesLoginOnRestart(t *testing.T) {
	config := camoufoxtest.ConfigDir(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "kept", Path: "/", MaxAge: 3600})
		}
		cookie, _ := r.Cookie("sessionid")
		if cookie != nil {
			fmt.Fprint(w, cookie.Value)
		}
	}))
	defer server.Close()
	manager := NewManager()
	manager.configDir = config
	manager.SetAppFocus(func() { t.Error("headless task must not focus the app") })
	root := t.TempDir()
	t.Cleanup(manager.StopAll)
	b := store.Browser{ID: 1, Name: "account", Port: 49010, UserDataDir: filepath.Join(root, "account")}
	addon, err := extbundle.InstallFirefoxBridge(filepath.Join(t.TempDir(), "addon"), "18965", "fixture-token", "tulai-bound-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartTaskWithAddon(context.Background(), b, root, server.URL+"/login", addon); err != nil {
		t.Fatal(err)
	}
	firstPID := manager.pid(b.ID)
	if !manager.IsHeadless(b.ID) {
		t.Fatal("background account should be headless")
	}
	if manager.StopLogin(b.ID) || !manager.IsRunning(b.ID) {
		t.Fatal("login cleanup stopped a background task")
	}
	if _, err := manager.Start(b, root, KindCamoufox, server.URL+"/check", true); err != nil {
		t.Fatal(err)
	}
	if manager.pid(b.ID) != firstPID {
		t.Fatal("viewing an account with a page bridge must not restart the running task")
	}
	if _, err := manager.StartTaskWithAddon(context.Background(), b, root, server.URL+"/check", addon); err != nil {
		t.Fatal(err)
	}
	if manager.pid(b.ID) != firstPID {
		t.Fatal("a headless account should be reused")
	}
	// Simulate an existing visible account without opening a window during the test.
	manager.mu.Lock()
	manager.quiet[b.ID] = false
	manager.mu.Unlock()
	ep, err := manager.StartTaskWithAddon(context.Background(), b, root, server.URL+"/check", addon)
	if err != nil {
		t.Fatal(err)
	}
	if manager.pid(b.ID) == firstPID || !manager.IsHeadless(b.ID) {
		t.Fatal("a visible account must restart headlessly for a task")
	}
	proc, err := camoufox.Find(ep)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var out struct{ Result struct{ Value string } }
	if err := proc.Call(ctx, proc.Page, "Runtime.evaluate", map[string]any{"expression": "document.body.textContent"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Result.Value != "kept" {
		t.Fatalf("login cookie was lost during restart: %q", out.Result.Value)
	}
	manager.mu.Lock()
	manager.quiet[b.ID] = false
	manager.mu.Unlock()
	if !manager.StopLogin(b.ID) || manager.IsRunning(b.ID) {
		t.Fatal("login cleanup did not stop the visible account")
	}
	ep, err = manager.StartTask(context.Background(), b, root, KindCamoufox, server.URL+"/check")
	if err != nil {
		t.Fatal(err)
	}
	proc, err = camoufox.Find(ep)
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Call(ctx, proc.Page, "Runtime.evaluate", map[string]any{"expression": "document.body.textContent"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Result.Value != "kept" {
		t.Fatalf("login cookie was lost after login cleanup: %q", out.Result.Value)
	}
}

func TestCamoufoxAccountIsolationAndRestart(t *testing.T) {
	config := camoufoxtest.ConfigDir(t)
	t.Setenv("CAMOU_CONFIG_1", "{}")
	t.Setenv("CAMOU_PREFS_1", "{}")
	for _, key := range proxyEnvKeys {
		t.Setenv(key, "")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/identity-headers" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"ua": r.UserAgent(), "language": r.Header.Get("Accept-Language")})
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: r.URL.Query().Get("id"), Path: "/", MaxAge: 3600, HttpOnly: true})
		}
		cookie, _ := r.Cookie("sessionid")
		value := ""
		if cookie != nil {
			value = cookie.Value
		}
		fmt.Fprintf(w, `<!doctype html><body>%s<iframe src="/frame"></iframe></body>`, value)
	}))
	defer server.Close()
	manager := NewManager()
	manager.configDir = config
	manager.headless = true
	root := t.TempDir()
	t.Cleanup(manager.StopAll)
	first := store.Browser{ID: 1, Name: "same-name", Port: 49001, UserDataDir: filepath.Join(root, "account-1")}
	second := store.Browser{ID: 2, Name: "same-name", Port: 49002, UserDataDir: filepath.Join(root, "account-2")}
	start := func(b store.Browser, target string) *camoufox.Process {
		t.Helper()
		ep, err := manager.Start(b, root, KindCamoufox, target, false)
		if err != nil {
			t.Fatal(err)
		}
		proc, err := camoufox.Find(ep)
		if err != nil {
			t.Fatal(err)
		}
		return proc
	}
	evaluate := func(p *camoufox.Process, expression string) json.RawMessage {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var out struct {
			Result           struct{ Value json.RawMessage }
			ExceptionDetails any
		}
		if err := p.Call(ctx, p.Page, "Runtime.evaluate", map[string]any{"expression": expression}, &out); err != nil {
			t.Fatal(err)
		}
		if out.ExceptionDetails != nil {
			t.Fatal(out.ExceptionDetails)
		}
		return out.Result.Value
	}
	type started struct {
		id  int64
		ep  string
		err error
	}
	startedCh := make(chan started, 2)
	for _, b := range []store.Browser{first, second} {
		go func() {
			target := server.URL + "/check"
			if b.ID == first.ID {
				target = server.URL + "/login?id=first"
			}
			ep, err := manager.Start(b, root, KindCamoufox, target, false)
			startedCh <- started{b.ID, ep, err}
		}()
	}
	processes := map[int64]*camoufox.Process{}
	for range 2 {
		result := <-startedCh
		if result.err != nil {
			t.Fatal(result.err)
		}
		proc, err := camoufox.Find(result.ep)
		if err != nil {
			t.Fatal(err)
		}
		processes[result.id] = proc
	}
	p1, p2 := processes[first.ID], processes[second.ID]
	if got := string(evaluate(p2, `document.body.firstChild.textContent`)); got != `""` {
		t.Fatalf("cookie leaked to second account: %s", got)
	}
	const identity = `JSON.stringify({ua:navigator.userAgent,platform:navigator.platform,language:navigator.language,languages:navigator.languages,cores:navigator.hardwareConcurrency,screen:[screen.width,screen.height,screen.colorDepth],timezone:Intl.DateTimeFormat().resolvedOptions().timeZone})`
	before := string(evaluate(p1, identity))
	headers := evaluate(p1, `(async()=>{const h=await fetch('/identity-headers').then(r=>r.json());return {ua:navigator.userAgent,headerUA:h.ua,language:navigator.language,acceptLanguage:h.language}})()`)
	var wire struct{ UA, HeaderUA, Language, AcceptLanguage string }
	if err := json.Unmarshal(headers, &wire); err != nil {
		t.Fatal(err)
	}
	if wire.UA != wire.HeaderUA || !strings.EqualFold(wire.Language, strings.TrimSpace(strings.Split(wire.AcceptLanguage, ",")[0])) {
		t.Fatalf("navigator 与请求头的浏览器身份不一致: %s", headers)
	}
	deviceBefore := checkVisibleDevice(t, p1, first.UserDataDir)
	if got := checkVisibleDevice(t, p2, second.UserDataDir); got == deviceBefore {
		t.Fatalf("accounts expose the same device fingerprint: %s", got)
	}
	const rendering = `(async()=>{
 const canvas=document.createElement('canvas');canvas.width=240;canvas.height=60;
 const ctx=canvas.getContext('2d');ctx.fillStyle='#f60';ctx.fillRect(5,5,90,30);ctx.fillStyle='#067';ctx.font='18px Arial';ctx.fillText('tulai fingerprint 文字',10,25);
 const pixels=ctx.getImageData(0,0,canvas.width,canvas.height).data;let hash=2166136261;for(const value of pixels)hash=Math.imul(hash^value,16777619);
 const gl=document.createElement('canvas').getContext('webgl');const ext=gl?.getExtension('WEBGL_debug_renderer_info');
 let adapter=null;try{adapter=navigator.gpu ? await navigator.gpu.requestAdapter() : null}catch{}
 return {canvas:hash,webgl:gl && [gl.getParameter(gl.VENDOR),gl.getParameter(gl.RENDERER),ext && gl.getParameter(ext.UNMASKED_VENDOR_WEBGL),ext && gl.getParameter(ext.UNMASKED_RENDERER_WEBGL),gl.getParameter(gl.MAX_TEXTURE_SIZE)],webgpu:!!adapter};})()`
	type renderingFingerprint struct {
		Canvas int32
		WebGL  json.RawMessage
		WebGPU bool
	}
	readRendering := func() renderingFingerprint {
		t.Helper()
		var value renderingFingerprint
		if err := json.Unmarshal(evaluate(p1, rendering), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	renderingBefore, renderingRepeat := readRendering(), readRendering()
	consistency := evaluate(p1, `(async()=>{
 const snapshot=()=>({ua:navigator.userAgent,platform:navigator.platform,language:navigator.language,languages:Array.from(navigator.languages),cores:navigator.hardwareConcurrency,timeZone:Intl.DateTimeFormat().resolvedOptions().timeZone});
 const source='postMessage(('+snapshot.toString()+')())';const url=URL.createObjectURL(new Blob([source],{type:'application/javascript'}));
 const worker=new Worker(url);const workerValue=await new Promise((resolve,reject)=>{worker.onmessage=e=>resolve(e.data);worker.onerror=reject});worker.terminate();URL.revokeObjectURL(url);
 const frame=document.querySelector('iframe').contentWindow; const n=frame.navigator;
 const frameValue={ua:n.userAgent,platform:n.platform,language:n.language,languages:Array.from(n.languages),cores:n.hardwareConcurrency,timeZone:frame.Intl.DateTimeFormat().resolvedOptions().timeZone};
 return {main:snapshot(),worker:workerValue,frame:frameValue};})()`)
	var worlds struct{ Main, Worker, Frame json.RawMessage }
	if err := json.Unmarshal(consistency, &worlds); err != nil {
		t.Fatal(err)
	}
	if string(worlds.Main) != string(worlds.Worker) || string(worlds.Main) != string(worlds.Frame) {
		t.Fatalf("identity differs between execution worlds: %s", consistency)
	}
	id1, err := os.ReadFile(filepath.Join(first.UserDataDir, ".tulai-identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	id2, err := os.ReadFile(filepath.Join(second.UserDataDir, ".tulai-identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(id1) == string(id2) {
		t.Fatal("accounts share the same fingerprint")
	}
	reused := start(first, server.URL)
	if reused != p1 {
		t.Fatal("reopening an account started another process")
	}
	manager.Stop(first.ID)
	p1 = start(first, server.URL+"/check")
	if got := string(evaluate(p1, `document.body.firstChild.textContent`)); got != `"first"` {
		t.Fatalf("login did not persist: %s", got)
	}
	if got := string(evaluate(p1, identity)); got != before {
		t.Fatalf("fingerprint changed across restart:\n%s\n%s", before, got)
	}
	if got := checkVisibleDevice(t, p1, first.UserDataDir); got != deviceBefore {
		t.Fatalf("device fingerprint changed across restart:\n%s\n%s", deviceBefore, got)
	}
	renderingAfter := readRendering()
	if string(renderingAfter.WebGL) != string(renderingBefore.WebGL) || renderingAfter.WebGPU != renderingBefore.WebGPU ||
		(renderingBefore.Canvas == renderingRepeat.Canvas && renderingAfter.Canvas != renderingBefore.Canvas) {
		t.Fatalf("rendering fingerprint changed across restart:\n%+v\n%+v", renderingBefore, renderingAfter)
	}
	manager.StopAll()
	if manager.IsRunning(first.ID) || manager.IsRunning(second.ID) {
		t.Fatal("StopAll left an account running")
	}
	// StopAll may be used before a settings change; the manager must remain reusable.
	p1 = start(first, server.URL+"/check")
	if got := string(evaluate(p1, `document.body.firstChild.textContent`)); got != `"first"` {
		t.Fatalf("StopAll lost login: %s", got)
	}
}

func TestCamoufoxMultiAccountAntiCorrelationAndBridgeStealth(t *testing.T) {
	if testing.Short() {
		t.Skip("short test skips Camoufox smoke test")
	}
	runtimeDir := camoufoxtest.ConfigDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Unused ephemeral port keeps the Firefox bridge add-on offline-safe.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, bridgePort, err := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	if err != nil {
		t.Fatal(err)
	}

	rootDir := t.TempDir()
	profileRoot := filepath.Join(rootDir, "BrowserData", "Camoufox")
	alphaDir := filepath.Join(profileRoot, "acc-alpha")
	betaDir := filepath.Join(profileRoot, "acc-beta")
	addonRoot := filepath.Join(rootDir, "addons")

	alphaAddon, err := extbundle.InstallFirefoxBridge(filepath.Join(addonRoot, "alpha"), bridgePort, "token-alpha", "acc-alpha")
	if err != nil {
		t.Fatal(err)
	}
	betaAddon, err := extbundle.InstallFirefoxBridge(filepath.Join(addonRoot, "beta"), bridgePort, "token-beta", "acc-beta")
	if err != nil {
		t.Fatal(err)
	}

	alphaGeckoID := readAddonGeckoID(t, alphaAddon)
	betaGeckoID := readAddonGeckoID(t, betaAddon)
	if alphaGeckoID == betaGeckoID {
		t.Fatalf("acc-alpha and acc-beta share gecko add-on id %q", alphaGeckoID)
	}
	assertCleanQianwenScripts(t, alphaAddon)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/login":
			id := r.URL.Query().Get("id")
			http.SetCookie(w, &http.Cookie{Name: "session", Value: id, Path: "/", MaxAge: 3600})
			_, _ = fmt.Fprintf(w, `<!doctype html><html><body><div id="status">ok</div><script>localStorage.setItem('tulai_account', %q);</script></body></html>`, id)
		case "/check":
			who := "anon"
			if c, err := r.Cookie("session"); err == nil {
				who = c.Value
			}
			_, _ = fmt.Fprintf(w, `<!doctype html><html><body><div id="who">%s</div><input type="file" id="probe-upload"></body></html>`, who)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manager := NewManager()
	manager.configDir = runtimeDir
	defer manager.StopAll()

	alpha := store.Browser{ID: 101, Name: "acc-alpha", Platform: "千问", UserDataDir: alphaDir}
	beta := store.Browser{ID: 102, Name: "acc-beta", Platform: "千问", UserDataDir: betaDir}

	startManaged := func(b store.Browser, targetURL, addon string) *camoufox.Process {
		t.Helper()
		endpoint, err := manager.StartTaskWithAddon(ctx, b, rootDir, targetURL, addon)
		if err != nil {
			t.Fatalf("start %s: %v", b.Name, err)
		}
		proc, err := camoufox.Find(endpoint)
		if err != nil {
			t.Fatalf("find %s: %v", b.Name, err)
		}
		return proc
	}

	pAlpha := startManaged(alpha, server.URL+"/login?id=alpha", alphaAddon)
	pBeta := startManaged(beta, server.URL+"/login?id=beta", betaAddon)

	tAlpha := firstPageTarget(t, ctx, pAlpha)
	tBeta := firstPageTarget(t, ctx, pBeta)
	waitDocumentComplete(t, ctx, pAlpha, tAlpha)
	waitDocumentComplete(t, ctx, pBeta, tBeta)
	navigatePage(t, ctx, pAlpha, tAlpha, server.URL+"/check")
	navigatePage(t, ctx, pBeta, tBeta, server.URL+"/check")

	alphaIdentityRaw, alphaIdentity := readSavedIdentity(t, alphaDir)
	betaIdentityRaw, betaIdentity := readSavedIdentity(t, betaDir)
	if bytes.Equal(alphaIdentityRaw, betaIdentityRaw) {
		t.Fatal("acc-alpha and acc-beta wrote identical .tulai-identity.json files")
	}
	if devA, devB := checkVisibleDevice(t, pAlpha, alphaDir), checkVisibleDevice(t, pBeta, betaDir); devA == devB {
		t.Fatalf("acc-alpha and acc-beta share identical visible device fingerprint:\n%s", devA)
	}
	if identityDeviceKey(alphaIdentity) == identityDeviceKey(betaIdentity) &&
		identityViewportKey(alphaIdentity) == identityViewportKey(betaIdentity) {
		t.Fatalf("acc-alpha and acc-beta share identical identity device+viewport config:\nalpha=%s\nbeta=%s",
			identityDeviceKey(alphaIdentity)+identityViewportKey(alphaIdentity),
			identityDeviceKey(betaIdentity)+identityViewportKey(betaIdentity))
	}

	savedVoicesAlpha := extractVoiceNames(alphaIdentity["voices"])
	savedVoicesBeta := extractVoiceNames(betaIdentity["voices"])
	if len(savedVoicesAlpha) == 0 || len(savedVoicesBeta) == 0 {
		t.Fatalf("expected non-empty saved speechSynthesis voices: alpha=%d beta=%d", len(savedVoicesAlpha), len(savedVoicesBeta))
	}
	liveVoicesAlpha := waitPageVoices(t, ctx, pAlpha, tAlpha)
	liveVoicesBeta := waitPageVoices(t, ctx, pBeta, tBeta)
	if !sameSortedStrings(liveVoicesAlpha, savedVoicesAlpha) {
		t.Fatalf("acc-alpha live voices (%d) do not match .tulai-identity.json (%d)", len(liveVoicesAlpha), len(savedVoicesAlpha))
	}
	if !sameSortedStrings(liveVoicesBeta, savedVoicesBeta) {
		t.Fatalf("acc-beta live voices (%d) do not match .tulai-identity.json (%d)", len(liveVoicesBeta), len(savedVoicesBeta))
	}
	if reflect.DeepEqual(liveVoicesAlpha, liveVoicesBeta) {
		t.Logf("note: speechSynthesis voice list matched across macOS fixture accounts (%d voices)", len(liveVoicesAlpha))
	}

	const canvasHashJS = `(() => {
		const c = document.createElement('canvas');
		c.width = 240; c.height = 80;
		const g = c.getContext('2d');
		const grad = g.createLinearGradient(0, 0, 240, 80);
		grad.addColorStop(0, '#1e88e5');
		grad.addColorStop(0.5, '#f4511e');
		grad.addColorStop(1, '#43a047');
		g.fillStyle = grad;
		g.fillRect(0, 0, 240, 80);
		g.font = '17px sans-serif';
		g.fillStyle = 'rgba(255, 255, 255, 0.85)';
		g.fillText('camoufox-canvas-isolation-987', 12, 38);
		g.beginPath();
		g.arc(170, 48, 22, 0, Math.PI * 1.6);
		g.strokeStyle = '#ffd54f';
		g.lineWidth = 4;
		g.stroke();
		const data = g.getImageData(0, 0, c.width, c.height).data;
		let h = 2166136261 >>> 0;
		for (let i = 0; i < data.length; i++) {
			h ^= data[i];
			h = Math.imul(h, 16777619) >>> 0;
		}
		return h.toString(16);
	})()`
	var canvasAlpha1, canvasAlpha2, canvasBeta string
	evalInto(t, ctx, pAlpha, tAlpha, canvasHashJS, &canvasAlpha1)
	evalInto(t, ctx, pAlpha, tAlpha, canvasHashJS, &canvasAlpha2)
	evalInto(t, ctx, pBeta, tBeta, canvasHashJS, &canvasBeta)
	if canvasAlpha1 == "" || canvasAlpha2 == "" || canvasBeta == "" {
		t.Fatalf("2D canvas hashes were empty: alpha=%q repeat=%q beta=%q", canvasAlpha1, canvasAlpha2, canvasBeta)
	}
	rt, _ := camoufox.Installed(runtimeDir)
	probe := exec.CommandContext(ctx, rt.Node, "--input-type=module", "-e", `
import {hostGpu} from 'camoufox/dist/host_rendering.js';
console.log(JSON.stringify(await hostGpu(process.argv[1], true, process.env, {})));
`, rt.Executable)
	probe.Dir = rt.Dir
	output, err := probe.Output()
	if err != nil {
		t.Fatalf("probe canvas rendering device: %v", err)
	}
	var gpu []string
	if err := json.Unmarshal(output, &gpu); err != nil {
		t.Fatalf("decode canvas rendering device: %v", err)
	}
	device := strings.ToLower(strings.TrimSpace(strings.Join(gpu, " ")))
	software := device == "" || strings.Contains(device, "llvmpipe") || strings.Contains(device, "microsoft basic render driver") ||
		strings.Contains(device, "swiftshader") || strings.Contains(device, "generic renderer")
	if software {
		t.Logf("canvas noise is disabled for the software renderer %q", device)
	} else if canvasAlpha1 == canvasBeta || canvasAlpha1 == canvasAlpha2 {
		t.Fatalf("expected randomized canvas readbacks: alpha=%q repeat=%q beta=%q", canvasAlpha1, canvasAlpha2, canvasBeta)
	}

	assertStorageAndCookie(t, ctx, pAlpha, tAlpha, "alpha")
	assertStorageAndCookie(t, ctx, pBeta, tBeta, "beta")

	snapshotBefore := captureLiveFingerprint(t, ctx, pAlpha, tAlpha)
	manager.Stop(alpha.ID)

	pAlpha = startManaged(alpha, server.URL+"/check", alphaAddon)
	tAlpha = firstPageTarget(t, ctx, pAlpha)
	waitDocumentComplete(t, ctx, pAlpha, tAlpha)
	assertStorageAndCookie(t, ctx, pAlpha, tAlpha, "alpha")
	snapshotAfter := captureLiveFingerprint(t, ctx, pAlpha, tAlpha)
	if !reflect.DeepEqual(snapshotAfter, snapshotBefore) {
		t.Fatalf("acc-alpha identity changed across restart:\nbefore=%+v\nafter=%+v", snapshotBefore, snapshotAfter)
	}

	// Phase B: offline HTTPS + CONNECT proxy verifying page stealth on platform domains.
	manager.StopAll()

	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><body><div id="app"><input type="file" id="probe-file"><button id="probe-btn">ok</button></div></body></html>`)
	}))
	defer tlsSrv.Close()

	tlsAddr := tlsSrv.Listener.Addr().String()
	connectProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusBadRequest)
			return
		}
		upstream, err := net.Dial("tcp", tlsAddr)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		client, buf, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		_, _ = buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buf.Flush()
		go func() { defer client.Close(); defer upstream.Close(); _, _ = io.Copy(upstream, buf) }()
		go func() { defer client.Close(); defer upstream.Close(); _, _ = io.Copy(client, upstream) }()
	}))
	defer connectProxy.Close()

	geoip := false
	stealthProc, err := camoufox.Start(ctx, runtimeDir, camoufox.Options{
		Profile:           alphaDir,
		Headless:          true,
		Proxy:             connectProxy.URL,
		GeoIP:             &geoip,
		IgnoreHTTPSErrors: true,
		Addons:            []string{alphaAddon},
		CollisionRoots:    []string{profileRoot},
	}, os.Stderr)
	if err != nil {
		t.Fatalf("start stealth phase Camoufox: %v", err)
	}
	defer stealthProc.Close()

	tStealth := firstPageTarget(t, ctx, stealthProc)
	for _, targetURL := range []string{"https://www.qianwen.com/chat", "https://www.doubao.com/chat"} {
		navigatePage(t, ctx, stealthProc, tStealth, targetURL)
		waitDocumentComplete(t, ctx, stealthProc, tStealth)
		time.Sleep(400 * time.Millisecond)

		var probe struct {
			Webdriver        bool     `json:"webdriver"`
			WindowLeakKeys   []string `json:"windowLeakKeys"`
			DefaultViewLeaks []string `json:"defaultViewLeaks"`
			RootAttrLeaks    []string `json:"rootAttrLeaks"`
			FileSlotMissing  bool     `json:"fileSlotMissing"`
			ShowPickerStr    string   `json:"showPickerStr"`
			ShowPickerCall   string   `json:"showPickerCall"`
			ClickStr         string   `json:"clickStr"`
			ClickCall        string   `json:"clickCall"`
			ToStringStr      string   `json:"toStringStr"`
		}
		evalInto(t, ctx, stealthProc, tStealth, `(() => {
			const hasTulai = name => /tulai/i.test(name);
			const showPicker = HTMLInputElement.prototype.showPicker;
			const click = HTMLElement.prototype.click;
			return {
				webdriver: navigator.webdriver,
				windowLeakKeys: Object.getOwnPropertyNames(window).filter(hasTulai),
				defaultViewLeaks: Object.getOwnPropertyNames(document.defaultView).filter(hasTulai),
				rootAttrLeaks: document.documentElement.getAttributeNames().filter(hasTulai),
				fileSlotMissing: document.getElementById('tulai-qianwen-file-slot') === null,
				showPickerStr: showPicker.toString(),
				showPickerCall: Function.prototype.toString.call(showPicker),
				clickStr: click.toString(),
				clickCall: Function.prototype.toString.call(click),
				toStringStr: Function.prototype.toString.toString(),
			};
		})()`, &probe)

		if probe.Webdriver {
			t.Fatalf("%s: navigator.webdriver = true, want false", targetURL)
		}
		if len(probe.WindowLeakKeys) > 0 || len(probe.DefaultViewLeaks) > 0 {
			t.Fatalf("%s: window leaked tulai keys: window=%v defaultView=%v", targetURL, probe.WindowLeakKeys, probe.DefaultViewLeaks)
		}
		if len(probe.RootAttrLeaks) > 0 {
			t.Fatalf("%s: documentElement leaked attributes: %v", targetURL, probe.RootAttrLeaks)
		}
		if !probe.FileSlotMissing {
			t.Fatalf("%s: found forbidden #tulai-qianwen-file-slot element", targetURL)
		}
		const wantShowPicker = "function showPicker() {\n    [native code]\n}"
		const wantClick = "function click() {\n    [native code]\n}"
		const wantToString = "function toString() {\n    [native code]\n}"
		if probe.ShowPickerStr != wantShowPicker || probe.ShowPickerCall != wantShowPicker {
			t.Fatalf("%s: showPicker.toString()=%q call=%q, want %q", targetURL, probe.ShowPickerStr, probe.ShowPickerCall, wantShowPicker)
		}
		if probe.ClickStr != wantClick || probe.ClickCall != wantClick {
			t.Fatalf("%s: click.toString()=%q call=%q, want %q", targetURL, probe.ClickStr, probe.ClickCall, wantClick)
		}
		if probe.ToStringStr != wantToString {
			t.Fatalf("%s: Function.prototype.toString.toString()=%q, want %q", targetURL, probe.ToStringStr, wantToString)
		}
	}
}

func readAddonGeckoID(t *testing.T, addonDir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(addonDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "tulai") {
		t.Fatalf("%s/manifest.json leaked tulai marker:\n%s", addonDir, string(raw))
	}
	var manifest struct {
		BrowserSpecificSettings struct {
			Gecko struct {
				ID string `json:"id"`
			} `json:"gecko"`
		} `json:"browser_specific_settings"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	id := manifest.BrowserSpecificSettings.Gecko.ID
	if !isGeckoUUID(id) {
		t.Fatalf("%s gecko.id = %q, want {uuid} format", addonDir, id)
	}
	return id
}

func isGeckoUUID(id string) bool {
	if len(id) != 38 || id[0] != '{' || id[37] != '}' {
		return false
	}
	for i := 1; i < 37; i++ {
		if i == 9 || i == 14 || i == 19 || i == 24 {
			if id[i] != '-' {
				return false
			}
			continue
		}
		c := id[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func assertCleanQianwenScripts(t *testing.T, addonDir string) {
	t.Helper()
	forbidden := []string{"__pb_", "__tulai", "data-tulai", "tulai-qianwen", "tulai-qianwen-file-slot"}
	for _, rel := range []string{filepath.Join("qianwen", "chooser.js"), filepath.Join("qianwen", "page.js")} {
		data, err := os.ReadFile(filepath.Join(addonDir, rel))
		if err != nil {
			t.Fatal(err)
		}
		lower := strings.ToLower(string(data))
		for _, marker := range forbidden {
			if strings.Contains(lower, marker) {
				t.Fatalf("%s contains forbidden stealth marker %q", rel, marker)
			}
		}
	}
}

func firstPageTarget(t *testing.T, ctx context.Context, proc *camoufox.Process) string {
	t.Helper()
	var targets struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
		} `json:"targetInfos"`
	}
	if err := proc.Call(ctx, "", "Target.getTargets", map[string]any{}, &targets); err != nil {
		t.Fatalf("Target.getTargets: %v", err)
	}
	for _, info := range targets.TargetInfos {
		if info.Type == "page" && info.TargetID != "" {
			return info.TargetID
		}
	}
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := proc.Call(ctx, "", "Target.createTarget", map[string]any{"url": "about:blank"}, &created); err != nil {
		t.Fatalf("Target.createTarget: %v", err)
	}
	return created.TargetID
}

func navigatePage(t *testing.T, ctx context.Context, proc *camoufox.Process, targetID, targetURL string) {
	t.Helper()
	if err := proc.Call(ctx, targetID, "Page.navigate", map[string]any{"url": targetURL}, nil); err != nil {
		t.Fatalf("Page.navigate(%s): %v", targetURL, err)
	}
	waitDocumentComplete(t, ctx, proc, targetID)
}

func sameSortedStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ca := append([]string(nil), a...)
	cb := append([]string(nil), b...)
	sort.Strings(ca)
	sort.Strings(cb)
	return reflect.DeepEqual(ca, cb)
}

func waitDocumentComplete(t *testing.T, ctx context.Context, proc *camoufox.Process, targetID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var ready string
		evalInto(t, ctx, proc, targetID, `document.readyState`, &ready)
		if ready == "complete" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for document.readyState == complete")
}

func evalInto(t *testing.T, ctx context.Context, proc *camoufox.Process, targetID, expression string, out any) {
	t.Helper()
	var resp struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := proc.Call(ctx, targetID, "Runtime.evaluate", map[string]any{"expression": expression}, &resp); err != nil {
		t.Fatalf("Runtime.evaluate: %v", err)
	}
	if resp.ExceptionDetails != nil {
		t.Fatalf("Runtime.evaluate JS exception: %s\nexpr=%s", resp.ExceptionDetails.Text, expression)
	}
	if out == nil {
		return
	}
	if len(resp.Result.Value) == 0 {
		t.Fatalf("Runtime.evaluate returned empty value for expr=%s", expression)
	}
	if err := json.Unmarshal(resp.Result.Value, out); err != nil {
		t.Fatalf("unmarshal Runtime.evaluate result %s: %v", string(resp.Result.Value), err)
	}
}

func readSavedIdentity(t *testing.T, profileDir string) ([]byte, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(profileDir, ".tulai-identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	return raw, decoded
}

func identityDeviceKey(cfg map[string]any) string {
	b, _ := json.Marshal([]any{
		cfg["navigator.platform"],
		cfg["navigator.oscpu"],
		cfg["navigator.hardwareConcurrency"],
		cfg["screen.width"],
		cfg["screen.height"],
		cfg["screen.availWidth"],
		cfg["screen.availHeight"],
		cfg["screen.colorDepth"],
	})
	return string(b)
}

func identityViewportKey(cfg map[string]any) string {
	b, _ := json.Marshal([]any{
		cfg["window.innerWidth"],
		cfg["window.innerHeight"],
		cfg["window.outerWidth"],
		cfg["window.outerHeight"],
		cfg["window.devicePixelRatio"],
	})
	return string(b)
}

func extractVoiceNames(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	var names []string
	for _, item := range items {
		switch v := item.(type) {
		case string:
			if v != "" {
				names = append(names, v)
			}
		case map[string]any:
			if name, ok := v["name"].(string); ok && name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func waitPageVoices(t *testing.T, ctx context.Context, proc *camoufox.Process, targetID string) []string {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var voices []string
		evalInto(t, ctx, proc, targetID, `speechSynthesis.getVoices().map(v => v.name)`, &voices)
		if len(voices) > 0 {
			return voices
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for non-empty speechSynthesis.getVoices()")
	return nil
}

func assertStorageAndCookie(t *testing.T, ctx context.Context, proc *camoufox.Process, targetID, want string) {
	t.Helper()
	var got struct {
		Who     string `json:"who"`
		Storage string `json:"storage"`
	}
	evalInto(t, ctx, proc, targetID, `({
		who: document.getElementById('who')?.textContent || '',
		storage: localStorage.getItem('tulai_account') || '',
	})`, &got)
	if got.Who != want || got.Storage != want {
		t.Fatalf("cookie/localStorage mismatch: got=%+v want=%q", got, want)
	}
}

type liveFingerprintSnapshot struct {
	UserAgent           string   `json:"userAgent"`
	Platform            string   `json:"platform"`
	Oscpu               string   `json:"oscpu"`
	HardwareConcurrency int      `json:"hardwareConcurrency"`
	Width               int      `json:"width"`
	Height              int      `json:"height"`
	AvailWidth          int      `json:"availWidth"`
	AvailHeight         int      `json:"availHeight"`
	ColorDepth          int      `json:"colorDepth"`
	Vendor              string   `json:"vendor"`
	Renderer            string   `json:"renderer"`
	UnmaskedVendor      string   `json:"unmaskedVendor"`
	UnmaskedRenderer    string   `json:"unmaskedRenderer"`
	Voices              []string `json:"voices"`
}

func captureLiveFingerprint(t *testing.T, ctx context.Context, proc *camoufox.Process, targetID string) liveFingerprintSnapshot {
	t.Helper()
	voices := waitPageVoices(t, ctx, proc, targetID)
	var snap liveFingerprintSnapshot
	evalInto(t, ctx, proc, targetID, `(() => {
		const c = document.createElement('canvas');
		const gl = c.getContext('webgl') || c.getContext('experimental-webgl');
		const dbg = gl && gl.getExtension('WEBGL_debug_renderer_info');
		return {
			userAgent: navigator.userAgent,
			platform: navigator.platform,
			oscpu: navigator.oscpu || '',
			hardwareConcurrency: navigator.hardwareConcurrency || 0,
			width: screen.width,
			height: screen.height,
			availWidth: screen.availWidth,
			availHeight: screen.availHeight,
			colorDepth: screen.colorDepth,
			vendor: gl ? String(gl.getParameter(gl.VENDOR)) : '',
			renderer: gl ? String(gl.getParameter(gl.RENDERER)) : '',
			unmaskedVendor: (gl && dbg) ? String(gl.getParameter(dbg.UNMASKED_VENDOR_WEBGL)) : '',
			unmaskedRenderer: (gl && dbg) ? String(gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL)) : '',
		};
	})()`, &snap)
	snap.Voices = voices
	if snap.Vendor == "" || snap.Renderer == "" {
		t.Fatalf("expected non-empty WebGL VENDOR/RENDERER: %+v", snap)
	}
	return snap
}
