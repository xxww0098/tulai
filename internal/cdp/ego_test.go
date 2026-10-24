package cdp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestEgoPageHostsKeepsRedirectAndPortIdentity(t *testing.T) {
	for _, tc := range []struct{ url, match, reject string }{
		{"https://www.tongyi.com/", "qianwen.com", "other.qianwen.com"},
		{"https://qianwen.com/?ch=tongyi_redirect", "www.tongyi.com", "qianwen.com:8443"},
		{"https://www.doubao.com/chat/", "doubao.com", "not-doubao.com"},
		{"https://www.tongyi.com:443/", "www.qianwen.com", "www.qianwen.com:443"},
		{"http://127.0.0.1:1234/", "127.0.0.1:1234", "127.0.0.1:1235"},
		{"http://[::1]:1234/", "[::1]:1234", "[::1]:1235"},
	} {
		hosts, err := egoPageHosts(tc.url)
		if err != nil || !slices.Contains(hosts, tc.match) || slices.Contains(hosts, tc.reject) {
			t.Fatalf("%s => %v, %v", tc.url, hosts, err)
		}
	}
	if _, err := egoPageHosts("not a URL"); err == nil {
		t.Fatal("invalid page URL accepted")
	}
}

func testEgoTransport(t *testing.T) (*egoTransport, net.Conn, context.Context) {
	t.Helper()
	client, server := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	transport := &egoTransport{conn: client, pending: map[uint64]chan egoResponse{}, done: make(chan struct{}), onClose: cancel}
	go transport.read(json.NewDecoder(client))
	t.Cleanup(func() { transport.close(); server.Close() })
	return transport, server, ctx
}

func TestEgoTransportResponseAndFatalError(t *testing.T) {
	transport, server, sessionCtx := testEgoTransport(t)
	go func() {
		decoder := json.NewDecoder(server)
		for i := range 3 {
			var request struct {
				ID uint64 `json:"id"`
			}
			if decoder.Decode(&request) != nil {
				return
			}
			response := egoResponse{ID: request.ID, Result: json.RawMessage(`42`)}
			if i > 0 {
				response.Error = "JS failed"
				response.Fatal = i == 2
			}
			json.NewEncoder(server).Encode(response)
		}
	}()
	var got int
	if err := transport.request(context.Background(), "cdp", nil, &got); err != nil || got != 42 {
		t.Fatalf("response = %d, %v", got, err)
	}
	if err := transport.request(context.Background(), "cdp", nil, nil); err == nil || sessionCtx.Err() != nil {
		t.Fatalf("ordinary JS error should retain session: %v, %v", err, sessionCtx.Err())
	}
	if err := transport.request(context.Background(), "cdp", nil, nil); err == nil || sessionCtx.Err() == nil {
		t.Fatalf("lost control should cancel session: %v, %v", err, sessionCtx.Err())
	}
}

func TestEgoTransportTimeoutAndDisconnect(t *testing.T) {
	for _, mode := range []string{"write timeout", "response timeout", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			transport, server, sessionCtx := testEgoTransport(t)
			if mode != "write timeout" {
				go func() {
					var request any
					json.NewDecoder(server).Decode(&request)
					if mode == "disconnect" {
						server.Close()
					}
				}()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := transport.request(ctx, "cdp", nil, nil); err == nil {
				t.Fatal("expected transport error")
			}
			if sessionCtx.Err() == nil {
				t.Fatal("failed transport did not cancel session")
			}
		})
	}
}

func TestEgoTransportChildFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ego-browser"), []byte("#!/bin/sh\necho fake-ego-failure >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	scripts := egoTestTempDir(t)
	if _, err := startEgoTransport(context.Background(), nil, nil); err == nil || !strings.Contains(err.Error(), "fake-ego-failure") {
		t.Fatalf("child failure not preserved: %v", err)
	}
	if files, err := os.ReadDir(scripts); err != nil || len(files) != 0 {
		t.Fatalf("failed child left scripts: %v, %v", files, err)
	}
}

func TestEgoTransportStartupCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ego-browser"), []byte("#!/bin/sh\nwhile :; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	scripts := egoTestTempDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err := startEgoTransport(ctx, nil, nil); err == nil {
		t.Fatal("canceled startup succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("startup cancellation did not stop waiting for IPC")
	}
	if files, err := os.ReadDir(scripts); err != nil || len(files) != 0 {
		t.Fatalf("canceled startup left scripts: %v, %v", files, err)
	}
}

func TestEgoSessionLocalPage(t *testing.T) {
	if testing.Short() || os.Getenv("XBS_EGO_E2E") != "1" {
		t.Skip("需显式 XBS_EGO_E2E=1，在软件自有 ego lite 空间测试本地页面")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body><div id="ready">local test</div><input type="file"><textarea>old text</textarea><div id="editor" contenteditable="true">old text</div><script>window.changes=0;window.trustedEditorInputs=0;document.querySelector('input').addEventListener('change',()=>window.changes++);document.addEventListener('input',event=>{if(event.isTrusted&&event.target.id==='editor')window.trustedEditorInputs++})</script></body></html>`))
	}))
	defer server.Close()
	s, err := NewEgoSession(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.ego.request(ctx, "closePage", nil, nil); err != nil {
			t.Logf("关闭本地测试页: %v", err)
		}
	}()
	if out, err := s.Evaluate(`40+2`); err != nil || out != "42" {
		t.Fatalf("evaluate = %s, %v", out, err)
	}
	if err := s.WaitForSelector("#ready", 3*time.Second); err != nil {
		t.Fatal(err)
	}
	s.AppendNavigateScript(`window.__egoNavigationCheck = true`)
	if err := s.Navigate(server.URL + "/next"); err != nil || !s.EvaluateBool(`window.__egoNavigationCheck === true`) {
		t.Fatalf("navigation injection failed: %v", err)
	}
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, []byte("upload test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetUploadFilesNth(0, []string{path}); err != nil || s.EvaluateInt(`document.querySelector('input').files.length`) != 1 {
		t.Fatalf("upload failed: %v", err)
	}
	if changes := s.EvaluateInt(`window.changes`); changes != 1 {
		t.Fatalf("Ego upload generated %d changes, want 1", changes)
	}
	if err := s.InsertText("#editor", "可信提示词"); err != nil || !s.EvaluateBool(`document.querySelector('#editor').innerText==='可信提示词'&&window.trustedEditorInputs>=1`) {
		t.Fatalf("Ego trusted editor input failed: %v", err)
	}
	s.Handle("ping", func(data string) string { return `{"success":true,"data":"` + data + `"}` })
	out, err := s.EvaluateAsync(`xbsCall('ping','bridge-test')`)
	if err != nil || !strings.Contains(out, "bridge-test") {
		t.Fatalf("page bridge = %q, %v", out, err)
	}
	pngData, err := s.CaptureElementScreenshot("#ready")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.DecodeConfig(bytes.NewReader(pngData)); err != nil {
		t.Fatal(err)
	}
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>second local page</body></html>`))
	}))
	defer secondServer.Close()
	second, err := NewEgoSession(context.Background(), secondServer.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !second.EvaluateBool(`document.body.innerText.includes('second local page')`) || !s.EvaluateBool(`document.body.innerText.includes('local test')`) {
		t.Fatal("two sessions did not retain separate live pages")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := second.ego.request(ctx, "closePage", nil, nil); err != nil {
		t.Error(err)
	}
	second.Close()
	if _, err := s.Evaluate(`window.__egoRetained = 'login-state'`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := OpenEgoURL(server.URL); err != nil {
		t.Fatal(err)
	}
	parent, stopParent := context.WithCancel(context.Background())
	defer stopParent()
	resumed, err := NewEgoSession(parent, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	s = resumed
	if !s.EvaluateBool(`window.__egoRetained === 'login-state'`) {
		t.Fatal("handoff/resume navigated away from the retained page")
	}
	stopParent()
	select {
	case <-s.ego.processDone:
	case <-time.After(3 * time.Second):
		t.Fatal("parent cancellation did not reap control process")
	}
	resumed, err = NewEgoSession(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	s = resumed
}

func TestEgoRenewPageSkipsLegacyNewDocument(t *testing.T) {
	if testing.Short() || os.Getenv("XBS_EGO_E2E") != "1" {
		t.Skip("需显式 XBS_EGO_E2E=1；只迁移隔离本地页面")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><body>isolated legacy-hook migration</body></html>`))
	}))
	defer server.Close()
	origin, _ := json.Marshal(server.URL)
	runLocal := func(source string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		path, err := egoExecutable()
		if err != nil {
			return err
		}
		cmd, cleanup, err := egoCommand(ctx, path, "const origin="+string(origin)+";\n"+source)
		if err != nil {
			return err
		}
		defer cleanup()
		if data, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%w: %s", err, data)
		}
		return nil
	}
	s, err := NewEgoSession(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		s.Close()
		if err := runLocal(`const task=await taskSpace("小帮手");for(const tab of await task.tabs()){if(tab.label&&new URL(tab.url).origin===origin)await task.page(tab.label).close()}await task.handOff();`); err != nil {
			t.Errorf("清理本地迁移页: %v", err)
		}
	}()
	s.AppendNavigateScript(`window.__legacyInjected=true;const observe=()=>{window.__legacyObserver=new MutationObserver(()=>{});window.__legacyObserver.observe(document.body,{childList:true,subtree:true})};if(document.body)observe();else addEventListener('DOMContentLoaded',observe,{once:true})`)
	if !s.EvaluateBool(`window.__legacyInjected===true&&!!window.__legacyObserver`) {
		t.Fatal("legacy new-document observer was not installed")
	}
	if err := s.RenewPage(); err != nil {
		t.Fatal(err)
	}
	if !s.EvaluateBool(`!window.__legacyInjected&&!window.__legacyObserver`) || len(s.newDoc) != 0 || s.navigateHook != "" {
		t.Fatal("renewed page inherited legacy scripts")
	}
	if err := runLocal(`
const task=await taskSpace("小帮手");
const tabs=(await task.tabs()).filter(tab=>tab.label&&new URL(tab.url).origin===origin);
if(tabs.length!==2)throw new Error('renew must retain exactly two local pages');
let retired=0;
for(const tab of tabs){const page=task.page(tab.label);if(await page.evaluate(()=>sessionStorage.getItem('__xbsRetiredHookPage')==='1')){retired++;await page.reload();if(!await page.evaluate(()=>sessionStorage.getItem('__xbsRetiredHookPage')==='1'&&window.__legacyInjected===true))throw new Error('retired marker or legacy new-document script did not survive reload')}}
if(retired!==1)throw new Error('legacy page was not retired');
`); err != nil {
		t.Fatal(err)
	}
	s.AppendNavigateScript(`window.__freshHook=true`)
	if err := s.Navigate(server.URL + "/next"); err != nil || !s.EvaluateBool(`window.__freshHook===true&&!window.__legacyInjected`) {
		t.Fatalf("new-document chain retained legacy hook: %v", err)
	}
	if _, err := s.Evaluate(`window.__freshReuseMarker='retained'`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	resumed, err := NewEgoSession(context.Background(), server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	s = resumed
	if !s.EvaluateBool(`window.__freshReuseMarker==='retained'&&!window.__legacyInjected`) {
		t.Fatal("a later session reused the retired page or opened another page")
	}
}
