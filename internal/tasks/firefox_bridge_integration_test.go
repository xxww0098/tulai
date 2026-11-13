package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/camoufoxtest"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
)

func TestFirefoxBridgeObservesStreamAndDownloadsRaw(t *testing.T) {
	config := camoufoxtest.ConfigDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	preview := "https://p3.byteimg.com/test/image.jpeg~tplv-preview.png"
	raw := "https://p3.byteimg.com/test/image.jpeg~tplv-image_raw.png"
	data := resultPNG(t, 29)
	var rawDownloads atomic.Int32
	var activeDownloads, peakDownloads atomic.Int32
	eventJSON, _ := json.Marshal(map[string]any{"event_data": fmt.Sprintf(`{"image":{"image_preview":{"url":%q},"image_ori_raw":{"url":%q}}}`, preview, raw)})
	body := "event: message\nid: 42\ndata: " + string(eventJSON) + "\n\n"
	page := `<!doctype html><html><body>
<div class="guidance-input-surface"><button data-testid="skill_input_exit_button">图像生成</button><textarea style="width:400px;height:80px"></textarea><button data-testid="chat_input_send_button">发送</button></div>
<script>
document.querySelector('[data-testid="chat_input_send_button"]').onclick=async()=>{
  const box=document.querySelector('textarea'), prompt=box.value; box.value='';
  history.pushState({}, '', '/chat/fixture');
  const user=document.createElement('div'); user.setAttribute('data-role','user'); user.textContent=prompt; document.body.append(user);
  const res=await fetch('/samantha/chat/completion',{method:'POST'}); window.fixtureBody=await res.text();
  const reply=document.createElement('div'); reply.setAttribute('data-role','assistant');
  const card=document.createElement('div'); card.setAttribute('data-testid','mdbox_image');
  const img=document.createElement('img'); img.src=` + fmt.Sprintf("%q", preview) + `; img.width=256; img.height=256;
  card.append(img); reply.append(card); document.body.append(reply);
};
</script></body></html>`
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.Host, "byteimg.com"):
			if strings.HasPrefix(r.URL.Path, "/performance/") {
				n := activeDownloads.Add(1)
				for old := peakDownloads.Load(); n > old && !peakDownloads.CompareAndSwap(old, n); old = peakDownloads.Load() {
				}
				defer activeDownloads.Add(-1)
				time.Sleep(350 * time.Millisecond)
			}
			if strings.Contains(r.URL.Path, "image_raw") {
				rawDownloads.Add(1)
			}
			w.Header().Set("Access-Control-Allow-Origin", "https://www.doubao.com")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(data)
		case r.URL.Path == "/samantha/chat/completion":
			w.Header().Set("Content-Type", "text/event-stream")
			for i := 0; i < len(body); i += 23 {
				end := min(i+23, len(body))
				_, _ = io.WriteString(w, body[i:end])
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
		default:
			if r.URL.Path == "/chat/" {
				time.Sleep(400 * time.Millisecond)
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, page)
		}
	}))
	defer fixture.Close()
	local, _ := url.Parse(fixture.URL)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT only", http.StatusBadRequest)
			return
		}
		upstream, err := net.Dial("tcp", local.Host)
		if err != nil {
			http.Error(w, "fixture unavailable", http.StatusBadGateway)
			return
		}
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffer.Flush()
		go func() { defer client.Close(); defer upstream.Close(); _, _ = io.Copy(upstream, buffer) }()
		go func() { defer client.Close(); defer upstream.Close(); _, _ = io.Copy(client, upstream) }()
	}))
	defer proxy.Close()
	events := make(chan extbridge.Envelope, 20)
	srv, err := extbridge.Listen(extbridge.Config{Addr: "127.0.0.1:0", Token: "fixture-token", OnEvent: func(_ *extbridge.Session, env extbridge.Envelope) { events <- env }})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Addr())
	addon, err := extbundle.InstallFirefoxBridge(filepath.Join(t.TempDir(), "addon"), port, "fixture-token", "tulai-bound-1")
	if err != nil {
		t.Fatal(err)
	}
	// 排除页面钩子，确认原图来自 Firefox 网络流。
	manifestPath := filepath.Join(addon, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	var scripts []any
	for _, item := range manifest["content_scripts"].([]any) {
		row := item.(map[string]any)
		if js := row["js"].([]any); len(js) == 1 && js[0] == "doubao/raw.js" {
			continue
		}
		scripts = append(scripts, row)
	}
	manifest["content_scripts"] = scripts
	manifestData, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	geoip := false
	proc, err := camoufox.Start(ctx, config, camoufox.Options{Profile: t.TempDir(), Headless: true, Proxy: proxy.URL, GeoIP: &geoip, IgnoreHTTPSErrors: true, Addons: []string{addon}}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Close()
	const browserPort = 61782
	camoufox.Register(browserPort, proc)
	defer camoufox.Unregister(browserPort, proc)
	client, err := cdp.NewClient(camoufox.Endpoint(browserPort))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	pageSession, err := client.NewSession("https://www.doubao.com/chat/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pageSession.Close()
	var session *extbridge.Session
	for session == nil {
		next, err := srv.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if next.Platform() == extbridge.PlatformDoubao {
			session = next
		}
	}
	if session.Account() != "tulai-bound-1" {
		t.Fatalf("account = %q", session.Account())
	}
	time.Sleep(2 * time.Second)
	if err := pageSession.Navigate("https://www.doubao.com/chat/"); err != nil {
		t.Fatal(err)
	}
	if err := waitBoundPage(ctx, session, "1", "豆包"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := extCall(ctx, session, extbridge.CmdWashChat, "1", "", map[string]any{"mode": "fresh"}, 10*time.Second, "豆包"); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) < 350*time.Millisecond {
		t.Fatal("same-URL navigation returned before the page finished loading")
	}
	if err := waitBoundPage(ctx, session, "1", "豆包"); err != nil {
		t.Fatal(err)
	}
	e := &Engine{}
	task := &Task{Index: 1, BrowserID: 1, Attempts: 1}
	chat, err := e.prepareExtensionWash(ctx, session, task)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunDoubaoExtension(ctx, session, QianwenJob{TaskID: "1", Prompt: "测试图片", Wait: 20 * time.Second})
	if err != nil || !out.Submitted || len(out.Results) != 1 || !out.Results[0].WatermarkRemoved || !bytes.Equal(out.Results[0].Data, data) || rawDownloads.Load() == 0 {
		t.Fatalf("raw download failed: submitted=%v results=%d raw_requests=%d err=%v", out.Submitted, len(out.Results), rawDownloads.Load(), err)
	}
	e.finishExtensionWash(task, chat, out, true)
	task.Index = 2
	reused, err := e.prepareExtensionWash(ctx, session, task)
	if err != nil || reused.count != 1 || reused.url != "https://www.doubao.com/chat/fixture" {
		t.Fatalf("Firefox conversation was not reused: %+v %v", reused, err)
	}
	got, err := pageSession.Evaluate("window.fixtureBody")
	if err != nil || !strings.Contains(got, "image_ori_raw") {
		t.Fatalf("response did not reach the page intact: %q %v", got, err)
	}
	observed := false
	for len(events) > 0 {
		env := <-events
		var payload struct {
			Name   string `json:"name"`
			Status int    `json:"status"`
		}
		_ = json.Unmarshal(env.Payload, &payload)
		if payload.Name == "network" && payload.Status == 200 {
			observed = true
		}
	}
	if !observed {
		t.Fatal("Firefox did not report the completion response status")
	}
	started = time.Now()
	for i := 0; i < 6; i++ {
		url := fmt.Sprintf("https://p3.byteimg.com/performance/serial-%d.png", i)
		got, err := extDownloadURL(ctx, session, "performance", url, "豆包")
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("serial result %d: %v", i, err)
		}
	}
	t.Logf("six serial Firefox bridge downloads: %v", time.Since(started))
	peakDownloads.Store(0)
	items := make([]extImage, 6)
	for i := range items {
		url := fmt.Sprintf("https://p3.byteimg.com/performance/parallel-%d.png", i)
		items[i] = extImage{URL: url, RawURL: url, SourceIndex: len(items) - i}
	}
	started = time.Now()
	results, indices, err := downloadExtensionResults(ctx, session, "performance", "豆包", items, 3)
	if err != nil || len(results) != len(items) || len(indices) != len(items) {
		t.Fatalf("parallel Firefox downloads: results=%d indices=%v err=%v", len(results), indices, err)
	}
	for i, result := range results {
		if !bytes.Equal(result.Data, data) || indices[i] != len(items)-i {
			t.Fatalf("parallel Firefox result %d lost its source association", i)
		}
	}
	if n := peakDownloads.Load(); n < 2 || n > 3 {
		t.Fatalf("Firefox download concurrency = %d, want 2 or 3", n)
	}
	t.Logf("six parallel Firefox bridge downloads: %v, peak concurrent requests: %d", time.Since(started), peakDownloads.Load())
}
