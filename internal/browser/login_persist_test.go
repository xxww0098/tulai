package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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
	if _, err := manager.StartTaskWithAddon(b, root, server.URL+"/login", addon); err != nil {
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
	if _, err := manager.StartTaskWithAddon(b, root, server.URL+"/check", addon); err != nil {
		t.Fatal(err)
	}
	if manager.pid(b.ID) != firstPID {
		t.Fatal("a headless account should be reused")
	}
	// Simulate an existing visible account without opening a window during the test.
	manager.mu.Lock()
	manager.quiet[b.ID] = false
	manager.mu.Unlock()
	ep, err := manager.StartTaskWithAddon(b, root, server.URL+"/check", addon)
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
	ep, err = manager.StartTask(b, root, KindCamoufox, server.URL+"/check")
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
