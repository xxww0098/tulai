package download

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"

	"github.com/xxww0098/tulai/internal/fingerprint"
)

func TestDownloadAssistantRequiresTokenAndValidPage(t *testing.T) {
	called := 0
	a := &downloadAssistant{token: "private-token", session: &RenderedSession{}, handler: func(_ context.Context, _ *RenderedSession, _, _ string) error {
		called++
		return nil
	}}
	marker := "12345678-1234-1234-1234-123456789abc"
	for _, tc := range []struct {
		name, token, url, marker string
		status                   int
		ok                       bool
	}{
		{"missing token", "", "https://example.test/", marker, http.StatusForbidden, false},
		{"file URL", "private-token", "file:///tmp/private", marker, http.StatusOK, false},
		{"credentials", "private-token", "https://user:pass@example.test/", marker, http.StatusOK, false},
		{"qzone token", "private-token", "https://user.qzone.qq.com/12?g_tk=secret", marker, http.StatusOK, false},
		{"missing marker", "private-token", "https://example.test/", "", http.StatusOK, false},
		{"valid", "private-token", "https://example.test/", marker, http.StatusOK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"url": tc.url, "marker": tc.marker})
			req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/download", bytes.NewReader(body))
			req.Header.Set("X-Tulai-Token", tc.token)
			out := httptest.NewRecorder()
			a.serveHTTP(out, req)
			if out.Code != tc.status {
				t.Fatalf("status = %d, want %d", out.Code, tc.status)
			}
			if tc.status == http.StatusOK {
				var reply struct{ OK bool }
				if err := json.Unmarshal(out.Body.Bytes(), &reply); err != nil || reply.OK != tc.ok {
					t.Fatalf("reply = %q, err=%v", out.Body.String(), err)
				}
			}
		})
	}
	if called != 1 {
		t.Fatalf("handler called %d times, want 1", called)
	}
}

func TestTemuAssistantAutoLoadKeepsEarlierCards(t *testing.T) {
	runtimeDir := dedicatedFixtureData(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ready") }))
	defer server.Close()
	result := make(chan struct {
		found Discovery
		err   error
	}, 1)
	handler := func(ctx context.Context, session *RenderedSession, raw, marker string) error {
		err := session.BindCurrent(ctx, raw, marker)
		var found Discovery
		if err == nil {
			found, err = session.Capture(ctx)
		}
		result <- struct {
			found Discovery
			err   error
		}{found, err}
		return err
	}
	session, err := openDedicatedFixture(t, runtimeDir, filepath.Join(t.TempDir(), "profile"), server.URL, handler)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	const page = "https://www.temu.com/search_result.html?search_key=fixture"
	const login = "https://www.temu.com/login.html"
	const returned = "https://www.temu.com/search_result.html?search_key=fixture&is_back=1"
	const body = `<html><head><title>Temu test</title></head><body><div class="js-search-goodsList">
<div class="EKDT7a3v" id="old"><div data-tooltip="goodName-601099513224343" data-tooltip-title="First"><a href="https://www.temu.com/item-g-601099513224343.html">First</a></div><img class="goods-img-external" width="40" height="40" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==" data-src="https://img.kwcdn.com/product/first.gif"></div>
<div role="button" aria-label="See more items" id="more" style="width:100px;height:40px">See more</div></div>
<script>document.getElementById('more').onclick = () => { document.getElementById('old').remove(); document.getElementById('more').remove(); document.querySelector('.js-search-goodsList').insertAdjacentHTML('beforeend', '<div class="EKDT7a3v" id="new"><div data-tooltip="goodName-601099513224344" data-tooltip-title="Second"><a href="https://www.temu.com/item-g-601099513224344.html">Second</a></div><img class="goods-img-external" width="40" height="40" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==" data-src="https://img.kwcdn.com/product/second.gif"></div>'); };</script></body></html>`
	const returnedBody = `<html><head><title>Temu test</title></head><body><div class="js-search-goodsList"><div class="EKDT7a3v"><div data-tooltip="goodName-601099513224344" data-tooltip-title="Second"><a href="https://www.temu.com/item-g-601099513224344.html">Second</a></div><img class="goods-img-external" width="40" height="40" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==" data-src="https://img.kwcdn.com/product/second.gif"></div></div></body></html>`
	fingerprint.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action fingerprint.Action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			if paused.ResourceType == network.ResourceTypeDocument {
				html := ""
				switch paused.Request.URL {
				case page:
					html = body
				case login:
					html = `<html><body>Login</body></html>`
				case returned:
					html = returnedBody
				}
				if html != "" {
					action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(html)))
				}
			}
			_ = fingerprint.Run(session.tab, action)
		}()
	})
	if err := session.run(ctx, fetch.Enable(), fingerprint.Navigate(page)); err != nil {
		t.Fatal(err)
	}
	type bounds struct {
		Ready bool
		X, Y  float64
		W, H  float64
	}
	readBounds := func() bounds {
		var b bounds
		markers := session.downloadMarkers()
		script := fmt.Sprintf(`(() => { const h=document.querySelector(%q); if (!h) return {}; const r=h.getBoundingClientRect(); return {ready:getComputedStyle(h).display!=='none'&&r.width>0&&r.height>0,x:r.x,y:r.y,w:r.width,h:r.height}; })()`, "["+markers.HostAttr+"]")
		if err := session.run(ctx, fingerprint.Evaluate(script, &b)); err != nil {
			t.Fatal(err)
		}
		return b
	}
	deadline := time.Now().Add(20 * time.Second)
	var b bounds
	for time.Now().Before(deadline) {
		b = readBounds()
		if b.Ready && b.W > 0 && b.H > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !b.Ready || b.W == 0 {
		t.Fatal("TEMU 搜索页未显示自动加载面板")
	}
	click := func(x, y float64) {
		for _, kind := range []string{"mousePressed", "mouseReleased"} {
			if err := session.run(ctx, fingerprint.ActionFunc(func(ec context.Context) error {
				return cdp.Execute(ec, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": x, "y": y, "button": "left", "clickCount": 1}, nil)
			})); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The Temu panel can have a right margin, and the status below the
	// buttons can change its height. Anchor both clicks to the panel's top left.
	buttonOffset := b.H - 32
	click(b.X+194, b.Y+buttonOffset)
	deadline = time.Now().Add(20 * time.Second)
	var loaded bool
	for time.Now().Before(deadline) {
		if err := session.run(ctx, fingerprint.Evaluate(`!!document.getElementById('new')`, &loaded)); err != nil {
			t.Fatal(err)
		}
		if loaded {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !loaded {
		t.Fatal("自动加载未取得第二张商品卡")
	}
	time.Sleep(500 * time.Millisecond)
	b = readBounds()
	click(b.X+70, b.Y+buttonOffset)
	select {
	case got := <-result:
		if got.err != nil || len(got.found.Items) != 2 || got.found.Items[0].Metadata["album_title"] != "First" || got.found.Items[1].Metadata["album_title"] != "Second" {
			t.Fatalf("自动加载后丢失商品: items=%+v err=%v", got.found.Items, got.err)
		}
	case <-ctx.Done():
		t.Fatal("停止并下载没有提交任务")
	}
	if err := session.run(ctx, fingerprint.Navigate(login), fingerprint.Navigate(returned)); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		b = readBounds()
		if b.Ready && b.W > 0 && b.H > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !b.Ready || b.W == 0 {
		t.Fatal("登录返回后未显示 TEMU 下载面板")
	}
	click(b.X+70, b.Y+buttonOffset)
	select {
	case got := <-result:
		if got.err != nil || len(got.found.Items) != 2 {
			t.Fatalf("登录返回后丢失商品: items=%+v err=%v", got.found.Items, got.err)
		}
	case <-ctx.Done():
		t.Fatal("登录返回后没有提交任务")
	}
}

func TestDownloadAssistantBrowserButton(t *testing.T) {
	runtimeDir := dedicatedFixtureData(t)
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyRequests.Add(1)
		http.Error(w, "unexpected proxy request", http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("ALL_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/style.css" {
			w.Header().Set("Content-Type", "text/css")
			_, _ = io.WriteString(w, `.tile { width:40px; height:40px; background-image:url('/image.png'); }`)
			return
		}
		if r.URL.Path == "/image.png" {
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "synthetic" {
				http.Error(w, "sign in", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageData.Bytes())
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self'; style-src 'self'; script-src 'none'")
		if r.URL.Path == "/gallery" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/"})
			_, _ = io.WriteString(w, `<title>测试相册</title><img src="/image.png">`)
			return
		}
		_, _ = io.WriteString(w, `<title>没有图片</title><link rel="stylesheet" href="/style.css">`)
	}))
	defer server.Close()
	result := make(chan error, 1)
	var sequence atomic.Int32
	dir := t.TempDir()
	profile := filepath.Join(t.TempDir(), "profile")
	if err := os.Mkdir(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	handler := func(ctx context.Context, session *RenderedSession, rawURL, marker string) error {
		err := session.BindCurrent(ctx, rawURL, marker)
		if err == nil {
			var found Discovery
			found, err = session.Capture(ctx)
			if err == nil && len(found.Items) != 1 {
				err = fmt.Errorf("采集到 %d 张图片", len(found.Items))
			}
			if err == nil {
				item := found.Items[0]
				item.Filename = fmt.Sprintf("image-%d.png", sequence.Add(1))
				err = New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: dir}, item, nil)
			}
		}
		result <- err
		return err
	}
	session, err := openDedicatedFixture(t, runtimeDir, profile, server.URL+"/empty", handler)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	type buttonState struct {
		Found, Visible bool
		X, Y           float64
		Position       string
	}
	readButton := func(ctx, tab context.Context, s *RenderedSession) buttonState {
		var state buttonState
		markers := s.downloadMarkers()
		script := fmt.Sprintf(`(() => { const host=document.querySelector(%q); if(!host)return {found:false}; const r=host.getBoundingClientRect(); return {found:true,visible:r.width>0&&r.height>0,position:getComputedStyle(host).position,x:r.x+r.width/2,y:r.y+r.height/2}; })()`, "["+markers.HostAttr+"]")
		err := runRendered(ctx, tab, fingerprint.Evaluate(script, &state))
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	waitButtonFor := func(ctx, tab context.Context, s *RenderedSession, visible bool) buttonState {
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			state := readButton(ctx, tab, s)
			if state.Found && state.Visible == visible {
				return state
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("下载按钮未进入预期状态 visible=%v", visible)
		return buttonState{}
	}
	waitButton := func(ctx, tab context.Context, visible bool) buttonState {
		return waitButtonFor(ctx, tab, session, visible)
	}
	click := func(ctx, tab context.Context, state buttonState) {
		if state.Position != "fixed" {
			t.Fatalf("下载按钮位置不正确: %q", state.Position)
		}
		for _, kind := range []string{"mousePressed", "mouseReleased"} {
			if err := runRendered(ctx, tab, fingerprint.ActionFunc(func(ec context.Context) error {
				return cdp.Execute(ec, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": state.X, "y": state.Y, "button": "left", "clickCount": 1}, nil)
			})); err != nil {
				t.Fatal(err)
			}
		}
	}
	waitButton(ctx, session.tab, false)
	if err := session.run(ctx, fingerprint.Evaluate(`document.cookie='session=synthetic; path=/'; const img=document.createElement('img'); img.id='dynamic'; img.src='/image.png'; document.body.append(img)`, nil)); err != nil {
		t.Fatal(err)
	}
	waitButton(ctx, session.tab, true)
	if err := session.run(ctx, fingerprint.Evaluate(`document.getElementById('dynamic').remove()`, nil)); err != nil {
		t.Fatal(err)
	}
	waitButton(ctx, session.tab, false)
	if err := session.run(ctx, fingerprint.Evaluate(`const tile=document.createElement('div'); tile.id='tile'; tile.className='tile'; document.body.append(tile)`, nil)); err != nil {
		t.Fatal(err)
	}
	waitButton(ctx, session.tab, true)
	if err := session.run(ctx, fingerprint.Evaluate(`document.getElementById('tile').remove()`, nil)); err != nil {
		t.Fatal(err)
	}
	waitButton(ctx, session.tab, false)
	if err := session.run(ctx, fingerprint.Evaluate(`const link=document.createElement('a'); link.id='direct'; link.textContent='image'; document.body.append(link)`, nil)); err != nil {
		t.Fatal(err)
	}
	waitButton(ctx, session.tab, false)
	if err := session.run(ctx, fingerprint.Evaluate(`document.getElementById('direct').href='/image.png'`, nil)); err != nil {
		t.Fatal(err)
	}
	waitButton(ctx, session.tab, true)
	if err := session.run(ctx, fingerprint.Evaluate(`document.getElementById('direct').remove()`, nil)); err != nil {
		t.Fatal(err)
	}
	waitButton(ctx, session.tab, false)
	if err := session.run(ctx, fingerprint.Navigate(server.URL+"/gallery")); err != nil {
		t.Fatal(err)
	}
	waitButton(ctx, session.tab, true)

	second, closePage, err := fingerprint.NewPage(ctx, session.tab, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closePage()
	if err := runRendered(ctx, second, fingerprint.Navigate(server.URL+"/gallery")); err != nil {
		t.Fatal(err)
	}
	state := waitButton(ctx, second, true)
	click(ctx, second, state)
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("点击按钮后没有收到下载请求")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "image-1.png")); err != nil || !bytes.Equal(data, imageData.Bytes()) {
		t.Fatalf("登录态图片未正确下载: %v", err)
	}
	if !strings.HasSuffix(session.PageURL(), "/gallery") {
		t.Fatalf("采集的不是点击按钮的页面: %q", session.PageURL())
	}
	if err := session.run(ctx, fingerprint.Navigate(server.URL+"/empty")); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Capture(ctx); err == nil || !strings.Contains(err.Error(), "页面已变化") {
		t.Fatalf("页面切换后不应采集其他内容: %v", err)
	}
	session.Close()
	cancel()
	reopened, err := openDedicatedFixture(t, runtimeDir, profile, server.URL+"/gallery", handler)
	if err != nil {
		t.Fatalf("重新打开下载浏览器: %v", err)
	}
	defer reopened.Close()
	reopenCtx, cancelReopen := context.WithTimeout(t.Context(), time.Minute)
	defer cancelReopen()
	click(reopenCtx, reopened.tab, waitButtonFor(reopenCtx, reopened.tab, reopened, true))
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("重启后按钮无法发送: %v", err)
		}
	case <-reopenCtx.Done():
		t.Fatal("重启后按钮没有发送请求")
	}
	if got := proxyRequests.Load(); got != 0 {
		t.Fatalf("下载专用环境经过了代理：%d 次请求", got)
	}
}

func TestDownloadAssistantThreadsRandomizedMarkers(t *testing.T) {
	a, _, err := startDownloadAssistant(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.close()
	if !strings.HasPrefix(a.markers.HostAttr, "data-") || !strings.HasPrefix(a.markers.RequestAttr, "data-") || !strings.HasPrefix(a.markers.SnapshotAttr, "data-") {
		t.Fatalf("expected randomized data-* markers, got %+v", a.markers)
	}
	s := &RenderedSession{assistant: a, markers: a.markers}
	if got := s.downloadMarkers(); got != a.markers {
		t.Fatalf("downloadMarkers = %+v, want %+v", got, a.markers)
	}
	var unset RenderedSession
	if got := unset.downloadMarkers(); got.HostAttr == "" || got.RequestAttr == "" || got.SnapshotAttr == "" {
		t.Fatalf("unset session missing safe fallback markers: %+v", got)
	}
}

func TestDownloadAssistantPopupAndMultiTabLifecycle(t *testing.T) {
	runtimeDir := dedicatedFixtureData(t)
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageData.Bytes())
		case "/popup":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<title>Popup Gallery</title><img src="/image.png">`)
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<title>Index</title><a id="open" target="_blank" href="/popup">Open</a>`)
		}
	}))
	defer server.Close()

	captured := make(chan Discovery, 1)
	handler := func(ctx context.Context, session *RenderedSession, rawURL, marker string) error {
		if err := session.BindCurrent(ctx, rawURL, marker); err != nil {
			return err
		}
		found, err := session.Capture(ctx)
		if err == nil {
			captured <- found
		}
		return err
	}
	session, err := openDedicatedFixture(t, runtimeDir, filepath.Join(t.TempDir(), "profile"), server.URL+"/index", handler)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	// Open a site-initiated target=_blank popup tab from the index page.
	if err := session.run(ctx, fingerprint.Evaluate(`document.getElementById('open').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	var popupTarget string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var targets struct {
			Infos []struct {
				ID   string `json:"targetId"`
				Type string `json:"type"`
				URL  string `json:"url"`
			} `json:"targetInfos"`
		}
		if err := session.process.Call(ctx, "", "Target.getTargets", nil, &targets); err == nil {
			for _, info := range targets.Infos {
				if info.Type == "page" && strings.HasSuffix(info.URL, "/popup") {
					popupTarget = info.ID
					break
				}
			}
		}
		if popupTarget != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if popupTarget == "" {
		t.Fatal("target=_blank popup tab was not created")
	}

	// Verify download assistant button injects and works inside the popup tab.
	popupTab := fingerprint.PageContext(session.life, session.process, popupTarget)
	markers := session.downloadMarkers()
	type btn struct {
		Visible bool
		X, Y    float64
	}
	var state btn
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		script := fmt.Sprintf(`(() => { const h=document.querySelector(%q); if(!h)return {}; const r=h.getBoundingClientRect(); return {visible:r.width>0&&r.height>0,x:r.x+r.width/2,y:r.y+r.height/2}; })()`, "["+markers.HostAttr+"]")
		if err := runRendered(ctx, popupTab, fingerprint.Evaluate(script, &state)); err == nil && state.Visible {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !state.Visible {
		t.Fatal("target=_blank 新开标签页未显示下载助手按钮")
	}
	for _, kind := range []string{"mousePressed", "mouseReleased"} {
		if err := runRendered(ctx, popupTab, fingerprint.ActionFunc(func(ec context.Context) error {
			return cdp.Execute(ec, "Input.dispatchMouseEvent", map[string]any{"type": kind, "x": state.X, "y": state.Y, "button": "left", "clickCount": 1}, nil)
		})); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case found := <-captured:
		if len(found.Items) != 1 || !strings.HasSuffix(found.Items[0].URL, "/image.png") {
			t.Fatalf("popup capture unexpected: %+v", found)
		}
	case <-ctx.Done():
		t.Fatal("popup download button did not trigger capture")
	}

	// Closing the bound popup tab while the index tab is still open must NOT kill the session.
	if err := session.process.Call(ctx, popupTarget, "Page.close", nil, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if !session.Alive() {
		t.Fatal("关闭其中一个标签页后误杀了整个浏览器会话")
	}
	select {
	case <-session.Done():
		t.Fatal("仍有标签页存活时不应关闭 session.Done()")
	default:
	}
	// Focus and OpenTarget must still succeed on the surviving tab.
	if err := session.Focus(ctx); err != nil {
		t.Fatalf("Focus after closing popup tab failed: %v", err)
	}
	if err := session.OpenTarget(ctx, server.URL+"/popup"); err != nil {
		t.Fatalf("OpenTarget after closing popup tab failed: %v", err)
	}

	// Closing the last remaining tab MUST end the session and close Done().
	if err := session.run(ctx, fingerprint.ActionFunc(func(ec context.Context) error {
		return cdp.Execute(ec, "Page.close", nil, nil)
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("关闭最后一个标签页后未触发 session.Done()")
	}
	if session.Alive() {
		t.Fatal("关闭最后一个标签页后 session.Alive() 仍为 true")
	}
}
