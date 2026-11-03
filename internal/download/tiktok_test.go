package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/xxww0098/tulai/internal/camoufox"
	"golang.org/x/net/html"
)

func tiktokTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func tiktokTestDoc(t *testing.T, raw string) *html.Node {
	t.Helper()
	d, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func tiktokTestItem(id, author string) map[string]any {
	return map[string]any{
		"id": id, "desc": "Photo post", "secret": false, "forFriend": false, "privateItem": false,
		"isProhibited": false, "isReviewing": false, "shareEnabled": true, "takeDown": 0, "warnInfo": []any{},
		"author": map[string]any{"uniqueId": author, "privateAccount": false, "secret": false, "downloadSetting": 0},
		"video":  map[string]any{"playAddr": "https://video.invalid/never-download.mp4", "cover": "https://img.invalid/cover.jpg"},
		"imagePost": map[string]any{"images": []any{
			map[string]any{"imageURL": map[string]any{"urlList": []string{"https://p16-common-sign.tiktokcdn-us.com/tos-no1a-i-photomode-no/first~tplv-photomode-image.jpeg?x-signature=synthetic"}}},
			map[string]any{"imageURL": map[string]any{"urlList": []string{"https://p16-sign-sg.tiktokcdn.com/tos-alisg-i-photomode-sg/second~tplv-photomode-image.jpeg?x-signature=synthetic"}}},
		}},
	}
}
func tiktokTestHTML(item map[string]any) string {
	state := map[string]any{"__DEFAULT_SCOPE__": map[string]any{"webapp.video-detail": map[string]any{"statusCode": 0, "itemInfo": map[string]any{"itemStruct": item}}}}
	body, _ := json.Marshal(state)
	return `<html><head><script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">` + string(body) + `</script></head><body><img src="https://evil.test/cover.jpg"></body></html>`
}

func TestTikTokRoutesAndAssets(t *testing.T) {
	for _, tc := range []struct{ raw, kind, author, id string }{
		{"https://www.tiktok.com/@alice/photo/123", "photo", "alice", "123"},
		{"https://www.tiktok.com/@alice/video/123", "video", "alice", "123"},
		{"http://tiktok.com/@alice", "profile", "alice", ""},
		{"https://www.tiktok.com/@alice.name_1/photo/123/", "photo", "alice.name_1", "123"},
		{"https://www.tiktok.com/@alice/photo/0", "", "", ""},
		{"https://www.tiktok.com.evil.test/@alice/photo/123", "", "", ""},
		{"https://x@www.tiktok.com/@alice/photo/123", "", "", ""},
		{"https://www.tiktok.com:444/@alice/photo/123", "", "", ""},
		{"https://www.tiktok.com/@alice%2fother/photo/123", "", "", ""},
		{"https://www.tiktok.com/@alice/photo/123/other", "", "", ""},
	} {
		r := tiktokPageRef(tiktokTestURL(t, tc.raw))
		if r.kind != tc.kind || r.author != tc.author || r.id != tc.id {
			t.Errorf("route %q = %+v", tc.raw, r)
		}
	}
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://p16-common-sign.tiktokcdn-us.com/tos-no1a-i-photomode-no/one~tplv-photomode-image.jpeg?x-signature=synthetic", true},
		{"https://p16-sign-sg.tiktokcdn.com/tos-alisg-i-photomode-sg/two.jpg", true},
		{"http://p16-sign-sg.tiktokcdn.com/tos-alisg-i-photomode-sg/two.jpg", false},
		{"https://x@p16-sign-sg.tiktokcdn.com/tos-alisg-i-photomode-sg/two.jpg", false},
		{"https://p16-sign-sg.tiktokcdn.com:444/tos-alisg-i-photomode-sg/two.jpg", false},
		{"https://p16-sign-sg.tiktokcdn.com.evil.test/tos-alisg-i-photomode-sg/two.jpg", false},
		{"https://127.0.0.1/tos-alisg-i-photomode-sg/two.jpg", false},
		{"https://p16-sign-sg.tiktokcdn.com/other/cover.jpg", false},
		{"https://p16-sign-sg.tiktokcdn.com/tos-alisg-i-photomode-sg/%2e%2e/two.jpg", false},
		{"https://p16-sign-sg.tiktokcdn.com/tos-alisg-i-photomode-sg/two.mp4", false},
	} {
		_, ok := tiktokAsset(tc.raw)
		if ok != tc.want {
			t.Errorf("asset %q = %v", tc.raw, ok)
		}
	}
}
func TestTikTokPhotoAndPermissions(t *testing.T) {
	page := tiktokTestURL(t, "https://www.tiktok.com/@alice/video/123")
	item := tiktokTestItem("123", "alice")
	got, err := tiktokPhotoPage(context.Background(), tiktokTestDoc(t, tiktokTestHTML(item)), page)
	if err != nil || len(got.Items) != 2 || !got.Items[0].RequireImage || !got.Items[0].PublicOnly || got.Items[0].Metadata["tiktok_post_id"] != "123" {
		t.Fatalf("photo=%+v err=%v", got, err)
	}
	for name, change := range map[string]func(map[string]any){
		"wrong post":              func(v map[string]any) { v["id"] = "124" },
		"wrong author":            func(v map[string]any) { v["author"].(map[string]any)["uniqueId"] = "bob" },
		"private author":          func(v map[string]any) { v["author"].(map[string]any)["privateAccount"] = true },
		"unknown private status":  func(v map[string]any) { delete(v["author"].(map[string]any), "privateAccount") },
		"download disabled":       func(v map[string]any) { v["author"].(map[string]any)["downloadSetting"] = 3 },
		"download unknown":        func(v map[string]any) { delete(v["author"].(map[string]any), "downloadSetting") },
		"private post":            func(v map[string]any) { v["privateItem"] = true },
		"friend post":             func(v map[string]any) { v["forFriend"] = true },
		"prohibited":              func(v map[string]any) { v["isProhibited"] = true },
		"unknown review":          func(v map[string]any) { delete(v, "isReviewing") },
		"removed":                 func(v map[string]any) { v["takeDown"] = 1 },
		"advertisement":           func(v map[string]any) { v["isAd"] = true },
		"subscription content":    func(v map[string]any) { v["subVideoMeta"] = map[string]any{"locked": true} },
		"sensitive":               func(v map[string]any) { v["warnInfo"] = []any{map[string]any{"reason": "synthetic"}} },
		"post download forbidden": func(v map[string]any) { v["item_control"] = map[string]any{"can_download": false} },
	} {
		v := tiktokTestItem("123", "alice")
		change(v)
		got, err := tiktokPhotoPage(context.Background(), tiktokTestDoc(t, tiktokTestHTML(v)), page)
		if err == nil || len(got.Items) != 0 {
			t.Errorf("%s accepted: %+v err=%v", name, got, err)
		}
	}
	video := tiktokTestItem("123", "alice")
	delete(video, "imagePost")
	if got, err := tiktokPhotoPage(context.Background(), tiktokTestDoc(t, tiktokTestHTML(video)), page); err == nil || len(got.Items) != 0 {
		t.Fatalf("video accepted %+v %v", got, err)
	}
	partial := tiktokTestItem("123", "alice")
	imgs := partial["imagePost"].(map[string]any)["images"].([]any)
	imgs[1].(map[string]any)["imageURL"] = map[string]any{"urlList": []string{"https://evil.test/image.jpg"}}
	if got, err := tiktokPhotoPage(context.Background(), tiktokTestDoc(t, tiktokTestHTML(partial)), page); err == nil || len(got.Items) != 1 || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("partial %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := tiktokPhotoPage(ctx, tiktokTestDoc(t, tiktokTestHTML(item)), page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
func TestTikTokPhotoAliasAndRedirect(t *testing.T) {
	page := tiktokTestURL(t, "https://www.tiktok.com/@alice/photo/123?share=synthetic")
	fixture := tiktokTestHTML(tiktokTestItem("123", "alice"))
	calls := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		if raw != "https://www.tiktok.com/@alice/video/123" {
			return nil, errors.New("unexpected target")
		}
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(fixture))}, nil
	})
	got, err := discoverTikTok(context.Background(), getter, page)
	if err != nil || len(got.Items) != 2 || calls != 1 {
		t.Fatalf("photo alias count=%d calls=%d err=%v", len(got.Items), calls, err)
	}
	redirect := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: "https://evil.test/@alice/video/123", Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(fixture))}, nil
	})
	if got, err := discoverTikTok(context.Background(), redirect, page); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect=%+v err=%v", got, err)
	}
	profile := tiktokTestURL(t, "https://www.tiktok.com/@alice")
	calls = 0
	if got, err := discoverTikTok(context.Background(), getter, profile); err == nil || len(got.Items) != 0 || calls != 0 {
		t.Fatalf("profile=%+v calls=%d err=%v", got, calls, err)
	}
}

func TestTikTokRenderedCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_TIKTOK_BROWSER_RUNTIME")
	if runtime == "" {
		t.Skip("set TULAI_TIKTOK_BROWSER_RUNTIME to an isolated Camoufox runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	proc, err := camoufox.Start(ctx, runtime, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(proc.Context())
	tab := camoufox.PageContext(life, proc, proc.Page)
	start := tiktokTestURL(t, "https://www.tiktok.com/@alice/photo/123")
	s := &RenderedSession{process: proc, tab: tab, closeTab: closeTab, closeAllocator: func() { proc.Close() }, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: start}
	defer s.Close()

	disabled := tiktokTestItem("125", "alice")
	disabled["author"].(map[string]any)["downloadSetting"] = 3
	pages := map[string]string{
		"https://www.tiktok.com/@alice/photo/123": tiktokTestHTML(tiktokTestItem("123", "alice")),
		"https://www.tiktok.com/@alice/video/123": tiktokTestHTML(tiktokTestItem("123", "alice")),
		"https://www.tiktok.com/@alice/video/124": tiktokTestHTML(tiktokTestItem("124", "alice")),
		"https://www.tiktok.com/@bob/video/123":   tiktokTestHTML(tiktokTestItem("123", "bob")),
		"https://www.tiktok.com/@alice/video/125": tiktokTestHTML(disabled),
		"https://www.tiktok.com/login":            `<html><title>登录</title><img src="https://evil.test/login.png"></html>`,
	}
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(s.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if body, ok := pages[paused.Request.URL]; ok && paused.ResourceType == network.ResourceTypeDocument {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(s.tab, action); err != nil {
				select {
				case interceptErrors <- err:
				default:
				}
			}
		}()
	})
	if err := s.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{start.String(), "https://www.tiktok.com/@alice/video/123"} {
		if err := s.run(ctx, camoufox.Navigate(raw)); err != nil {
			t.Fatal(err)
		}
		got, err := s.Capture(ctx)
		if err != nil || len(got.Items) != 2 || got.Items[0].Metadata["tiktok_post_id"] != "123" || got.Items[0].Metadata["tiktok_author"] != "alice" {
			t.Fatalf("capture %s: count=%d err=%v", raw, len(got.Items), err)
		}
	}
	for _, raw := range []string{"https://www.tiktok.com/@alice/video/124", "https://www.tiktok.com/@bob/video/123", "https://www.tiktok.com/login"} {
		if err := s.run(ctx, camoufox.Navigate(raw)); err != nil {
			t.Fatal(err)
		}
		got, err := s.Capture(ctx)
		if err == nil || !strings.Contains(err.Error(), "已跳转") || len(got.Items) != 0 {
			t.Fatalf("redirect %s: count=%d err=%v", raw, len(got.Items), err)
		}
	}
	blocked := tiktokTestURL(t, "https://www.tiktok.com/@alice/video/125")
	s.start = blocked
	if err := s.run(ctx, camoufox.Navigate(blocked.String())); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Capture(ctx); err == nil || !strings.Contains(err.Error(), "未允许") || len(got.Items) != 0 {
		t.Fatalf("download disabled: count=%d err=%v", len(got.Items), err)
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := s.Capture(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestTikTokRenderedFallback(t *testing.T) {
	runtime := os.Getenv("TULAI_TIKTOK_BROWSER_RUNTIME")
	if runtime == "" {
		t.Skip("set TULAI_TIKTOK_BROWSER_RUNTIME to an isolated Camoufox runtime")
	}
	started := make(chan string, 2)
	releaseCancel, releaseChange := make(chan struct{}), make(chan struct{})
	var leaked atomic.Bool
	leakedRequests := make(chan string, 16)
	markLeaked := func(reason string) {
		leaked.Store(true)
		select {
		case leakedRequests <- reason:
		default:
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "www.tiktok.com" {
			markLeaked("host: " + r.Host)
			http.Error(w, "unexpected host", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if strings.Contains(r.URL.Path, "/photo/") {
			http.SetCookie(w, &http.Cookie{Name: "fixture", Value: "synthetic", Path: "/", Secure: true})
			_, _ = io.WriteString(w, `<html><title>TikTok fixture</title></html>`)
			return
		}
		cookie, err := r.Cookie("fixture")
		if err != nil || cookie.Value != "synthetic" {
			http.Error(w, "fixture login required", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/@alice/video/123":
			_, _ = io.WriteString(w, tiktokTestHTML(tiktokTestItem("123", "alice")))
		case "/@alice/video/124":
			http.Redirect(w, r, "https://www.tiktok.com/@bob/video/124", http.StatusFound)
		case "/@alice/video/125":
			item := tiktokTestItem("125", "alice")
			item["author"].(map[string]any)["downloadSetting"] = 3
			_, _ = io.WriteString(w, tiktokTestHTML(item))
		case "/@alice/video/126":
			_, _ = io.WriteString(w, tiktokTestHTML(tiktokTestItem("999", "alice")))
		case "/@alice/video/127":
			_, _ = io.WriteString(w, `<html><title>Login</title></html>`)
		case "/@alice/video/131":
			fixture := tiktokTestHTML(tiktokTestItem("131", "alice"))
			fixture = strings.Replace(fixture, `</body>`, `<script>history.replaceState({}, '', '/@bob/video/131')</script></body>`, 1)
			_, _ = io.WriteString(w, fixture)
		case "/@alice/video/128", "/@alice/video/129":
			started <- r.URL.Path
			release := releaseCancel
			if strings.HasSuffix(r.URL.Path, "/129") {
				release = releaseChange
			}
			select {
			case <-release:
			case <-r.Context().Done():
			case <-time.After(15 * time.Second):
			}
			_, _ = io.WriteString(w, tiktokTestHTML(tiktokTestItem(strings.TrimPrefix(r.URL.Path, "/@alice/video/"), "alice")))
		default:
			if strings.HasPrefix(r.URL.Path, "/@bob/") || strings.HasPrefix(r.URL.Path, "/@alice/video/") {
				markLeaked("path: " + r.URL.Path)
			}
			http.NotFound(w, r)
		}
	}))
	local, _ := url.Parse(server.URL)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "www.tiktok.com:443" {
			if strings.Contains(r.Host, "evil.test") {
				markLeaked("proxy: " + r.Host)
			}
			http.Error(w, "blocked", http.StatusForbidden)
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
	defer server.Close()
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	geoip := false
	proc, err := camoufox.Start(ctx, runtime, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true, Proxy: proxy.URL, GeoIP: &geoip, IgnoreHTTPSErrors: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(proc.Context())
	tab := camoufox.PageContext(life, proc, proc.Page)
	s := &RenderedSession{process: proc, tab: tab, closeTab: closeTab, closeAllocator: func() { proc.Close() }, releaseProfile: func() {}, getter: NewHTTPGetter("")}
	defer s.Close()
	closed := make(chan struct{}, 16)
	unsubscribe := proc.Subscribe(func(event camoufox.Event) {
		if event.Event == "closed" && event.Target != proc.Page {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	})
	defer unsubscribe()
	navigate := func(id string) string {
		t.Helper()
		raw := "https://www.tiktok.com/@alice/photo/" + id
		s.start = tiktokTestURL(t, raw)
		if err := s.run(ctx, camoufox.Navigate(raw)); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	waitClosed := func() {
		t.Helper()
		select {
		case <-closed:
		case <-time.After(5 * time.Second):
			t.Fatal("child tab was not closed")
		}
	}
	first := navigate("123")
	got, err := s.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[0].Metadata["tiktok_post_id"] != "123" || s.PageURL() != first {
		t.Fatalf("fallback count=%d page=%s err=%v", len(got.Items), s.PageURL(), err)
	}
	waitClosed()
	for _, tc := range []struct{ id, want string }{
		{"124", "登录或验证"}, {"125", "未允许"}, {"126", "身份不匹配"}, {"127", "登录或验证"}, {"131", "采集标签已跳转"},
	} {
		navigate(tc.id)
		got, err := s.Capture(ctx)
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(got.Items) != 0 {
			t.Fatalf("blocked %s: count=%d err=%v", tc.id, len(got.Items), err)
		}
		waitClosed()
	}
	navigate("128")
	cancelCtx, stop := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { _, err := s.Capture(cancelCtx); result <- err }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel fixture did not start")
	}
	stop()
	close(releaseCancel)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	waitClosed()
	navigate("129")
	type captureResult struct {
		discovery Discovery
		err       error
	}
	captured := make(chan captureResult, 1)
	go func() { d, err := s.Capture(ctx); captured <- captureResult{d, err} }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("page-change fixture did not start")
	}
	if err := s.run(ctx, camoufox.Navigate("https://www.tiktok.com/@alice/photo/130")); err != nil {
		t.Fatal(err)
	}
	close(releaseChange)
	changed := <-captured
	if changed.err == nil || !strings.Contains(changed.err.Error(), "原页面已更改") || len(changed.discovery.Items) != 0 {
		t.Fatalf("original page changed: count=%d err=%v", len(changed.discovery.Items), changed.err)
	}
	waitClosed()
	if leaked.Load() {
		t.Fatalf("child navigated or sent a request outside the permitted fixture: %s", <-leakedRequests)
	}
}

func TestTikTokLivePublic(t *testing.T) {
	if os.Getenv("TULAI_TIKTOK_LIVE") != "1" {
		t.Skip("set TULAI_TIKTOK_LIVE=1 for the public photo sample")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	page := tiktokTestURL(t, "https://www.tiktok.com/@ryzen.7.2700x/photo/7594147928399219990")
	got, err := discoverTikTok(ctx, NewHTTPGetter(""), page)
	if err != nil || len(got.Items) != 3 {
		t.Fatalf("live count=%d err=%v", len(got.Items), err)
	}
	for i, item := range got.Items {
		if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: filepath.Join(t.TempDir(), fmt.Sprint(i))}, item, nil); err != nil {
			t.Fatalf("public image %d download: %v", i+1, err)
		}
	}
	t.Logf("public photo post: discovered and downloaded %d images", len(got.Items))
}

func tiktokTestProfileDocument(user map[string]any, content string) string {
	state := map[string]any{"__DEFAULT_SCOPE__": map[string]any{"webapp.user-detail": map[string]any{"statusCode": 0, "userInfo": map[string]any{"user": user}}}}
	data, _ := json.Marshal(state)
	return `<html><head><title>Author fixture</title><script id="__UNIVERSAL_DATA_FOR_REHYDRATION__" type="application/json">` + string(data) + `</script></head><body>` + content + `</body></html>`
}

func tiktokTestProfileUser() map[string]any {
	return map[string]any{"uniqueId": "alice", "privateAccount": false, "secret": false, "downloadSetting": 0}
}

func TestTikTokProfileIdentityAndLinks(t *testing.T) {
	page := tiktokTestURL(t, "https://www.tiktok.com/@alice")
	if err := tiktokCheckProfile(context.Background(), tiktokTestDoc(t, tiktokTestProfileDocument(tiktokTestProfileUser(), "")), page); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(map[string]any){
		"another author":     func(m map[string]any) { m["uniqueId"] = "bob" },
		"private":            func(m map[string]any) { m["privateAccount"] = true },
		"secret":             func(m map[string]any) { m["secret"] = true },
		"not downloadable":   func(m map[string]any) { m["downloadSetting"] = 3 },
		"unknown permission": func(m map[string]any) { delete(m, "downloadSetting") },
	} {
		user := tiktokTestProfileUser()
		change(user)
		if err := tiktokCheckProfile(context.Background(), tiktokTestDoc(t, tiktokTestProfileDocument(user, "")), page); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	dup := tiktokTestProfileDocument(tiktokTestProfileUser(), `<script id="__UNIVERSAL_DATA_FOR_REHYDRATION__">{}</script>`)
	if err := tiktokCheckProfile(context.Background(), tiktokTestDoc(t, dup), page); err == nil {
		t.Error("duplicate state accepted")
	}
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if err := tiktokCheckProfile(cancelled, tiktokTestDoc(t, tiktokTestProfileDocument(tiktokTestProfileUser(), "")), page); !errors.Is(err, context.Canceled) {
		t.Errorf("cancel=%v", err)
	}
	for _, tc := range []struct{ raw, want, kind string }{
		{"/@alice/photo/123?token=synthetic#tracking", "https://www.tiktok.com/@alice/photo/123", "photo"},
		{"https://www.tiktok.com/@ALICE/video/456", "https://www.tiktok.com/@ALICE/video/456", "video"},
		{"https://www.tiktok.com/@bob/photo/123", "", ""},
		{"https://evil.test/@alice/photo/123", "", ""},
		{"https://x@www.tiktok.com/@alice/photo/123", "", ""},
		{"https://www.tiktok.com:444/@alice/photo/123", "", ""},
		{"http://www.tiktok.com/@alice/photo/123", "", ""},
		{"/@alice/photo/123/extra", "", ""},
	} {
		u, ref, ok := tiktokProfileLink(page, tc.raw)
		if tc.want == "" {
			if ok {
				t.Errorf("accepted %q", tc.raw)
			}
			continue
		}
		if !ok || u.String() != tc.want || ref.kind != tc.kind {
			t.Errorf("%q => %v %+v %v", tc.raw, u, ref, ok)
		}
	}
}

func TestTikTokProfileRenderedCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_TIKTOK_BROWSER_RUNTIME")
	if runtime == "" {
		t.Skip("set TULAI_TIKTOK_BROWSER_RUNTIME to an isolated Camoufox runtime")
	}
	var mode atomic.Int32
	var mu sync.Mutex
	requests := map[string]int{}
	var leaked atomic.Bool
	leakReason := make(chan string, 16)
	markLeaked := func(reason string) {
		leaked.Store(true)
		select {
		case leakReason <- reason:
		default:
		}
	}
	entered := make(chan struct{}, 4)
	releaseCancel, releaseChange := make(chan struct{}), make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "www.tiktok.com" {
			markLeaked("host: " + r.Host)
			http.Error(w, "wrong host", 403)
			return
		}
		mu.Lock()
		requests[r.URL.Path]++
		mu.Unlock()
		if r.URL.RawQuery != "" {
			markLeaked("query on: " + r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/@alice" {
			http.SetCookie(w, &http.Cookie{Name: "fixture", Value: "synthetic", Path: "/", Secure: true})
			content := `<style>.card{width:150px;height:100px}.card a{display:block;width:100%;height:100%}</style>
   <div data-e2e="user-page"><div data-e2e="user-post-item-list">
   <div class="card" data-e2e="user-post-item"><a href="/@alice/photo/123?tracking=synthetic">one</a></div>
   <div class="card" data-e2e="user-post-item" style="display:none"><a href="/@alice/photo/999">hidden</a></div>
   <div class="card" data-e2e="user-post-item" style="opacity:0"><a href="/@alice/photo/998">transparent</a></div>
   <div class="card" data-e2e="user-post-item" aria-hidden="true"><a href="/@alice/photo/997">aria hidden</a></div>
   <div class="card" data-e2e="user-post-item"><a href="/@bob/photo/888">other author</a></div>
   <div class="card" data-e2e="user-post-item"><a href="https://evil.test/@alice/photo/887">other host</a></div>
   <div class="card" data-e2e="user-post-item"><a href="/@alice/video/777">video</a></div>
   <div style="height:900px"></div><div id="more"></div></div></div>
   <script>addEventListener('scroll',()=>{const m=document.getElementById('more'); if(!m.hasChildNodes())m.innerHTML='<div class="card" data-e2e="user-post-item"><a href="/@alice/photo/124">two</a></div><div class="card" data-e2e="user-post-item"><a href="/@alice/photo/125">three</a></div>'});</script>`
			switch mode.Load() {
			case 2:
				content = `<div role="dialog" style="height:100px">Log in to TikTok</div>` + content
			case 3:
				content = `<div id="captcha_container" style="height:100px">Verify to continue</div>` + content
			}
			_, _ = io.WriteString(w, tiktokTestProfileDocument(tiktokTestProfileUser(), content))
			return
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/@alice/video/") {
			markLeaked("path: " + r.URL.Path)
			http.NotFound(w, r)
			return
		}
		cookie, err := r.Cookie("fixture")
		if err != nil || cookie.Value != "synthetic" {
			http.Error(w, "login required", 403)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/@alice/video/")
		if id != "123" && id != "124" && id != "125" {
			markLeaked("post: " + r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if (mode.Load() == 4 || mode.Load() == 7) && id == "123" {
			select {
			case entered <- struct{}{}:
			default:
			}
			release := releaseCancel
			if mode.Load() == 7 {
				release = releaseChange
			}
			select {
			case <-release:
			case <-r.Context().Done():
			case <-time.After(15 * time.Second):
			}
		}
		if id == "124" && (mode.Load() == 5 || mode.Load() == 6) {
			status := http.StatusForbidden
			if mode.Load() == 6 {
				status = http.StatusTooManyRequests
			}
			http.Error(w, "fixture access denied", status)
			return
		}
		item := tiktokTestItem(id, "alice")
		for _, entry := range item["imagePost"].(map[string]any)["images"].([]any) {
			imageURL := entry.(map[string]any)["imageURL"].(map[string]any)
			urls := imageURL["urlList"].([]string)
			urls[0] = strings.Replace(urls[0], "~tplv", id+"~tplv", 1)
		}
		if mode.Load() == 1 && id == "124" {
			item["author"].(map[string]any)["downloadSetting"] = 3
		}
		_, _ = io.WriteString(w, tiktokTestHTML(item))
	}))
	local, _ := url.Parse(server.URL)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "www.tiktok.com:443" {
			if strings.Contains(r.Host, "evil.test") || strings.Contains(r.Host, "video.invalid") {
				markLeaked("proxy: " + r.Host)
			}
			http.Error(w, "blocked", 403)
			return
		}
		upstream, err := net.Dial("tcp", local.Host)
		if err != nil {
			http.Error(w, "unavailable", 502)
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
	defer server.Close()
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	geoip := false
	proc, err := camoufox.Start(ctx, runtime, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true, Proxy: proxy.URL, GeoIP: &geoip, IgnoreHTTPSErrors: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(proc.Context())
	tab := camoufox.PageContext(life, proc, proc.Page)
	start := tiktokTestURL(t, "https://www.tiktok.com/@alice")
	s := &RenderedSession{process: proc, tab: tab, closeTab: closeTab, closeAllocator: func() { proc.Close() }, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: start}
	defer s.Close()
	closed := make(chan struct{}, 16)
	unsubscribe := proc.Subscribe(func(e camoufox.Event) {
		if e.Event == "closed" && e.Target != proc.Page {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	})
	defer unsubscribe()
	waitClosed := func(n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Fatalf("only %d of %d child tabs closed", i, n)
			}
		}
	}
	count := func(path string) int { mu.Lock(); defer mu.Unlock(); return requests[path] }
	if err := s.run(ctx, camoufox.Navigate(start.String())); err != nil {
		t.Fatal(err)
	}
	got, err := s.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "不完整") || len(got.Items) != 6 {
		t.Fatalf("profile count=%d err=%v", len(got.Items), err)
	}
	for _, item := range got.Items {
		if item.Metadata["tiktok_author"] != "alice" {
			t.Fatalf("wrong author %+v", item.Metadata)
		}
	}
	if count("/@alice/video/123") != 1 || count("/@alice/video/124") != 1 || count("/@alice/video/125") != 1 || count("/@alice/video/777") != 0 || count("/@alice/video/999") != 0 || count("/@alice/video/998") != 0 || count("/@alice/video/997") != 0 {
		t.Fatalf("unexpected profile requests: %+v", requests)
	}
	var current string
	if err := s.run(ctx, chromedp.Location(&current)); err != nil || current != start.String() {
		t.Fatalf("original page=%q err=%v", current, err)
	}
	waitClosed(4)
	mode.Store(1)
	before125 := count("/@alice/video/125")
	got, err = s.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "未允许") || len(got.Items) != 2 || count("/@alice/video/125") != before125 {
		t.Fatalf("permission stop count=%d err=%v", len(got.Items), err)
	}
	waitClosed(3)
	for _, m := range []int32{5, 6} {
		mode.Store(m)
		before125 = count("/@alice/video/125")
		got, err = s.Capture(ctx)
		if err == nil || !strings.Contains(err.Error(), "登录或验证") || len(got.Items) != 2 || count("/@alice/video/125") != before125 {
			t.Fatalf("HTTP status mode=%d count=%d err=%v", m, len(got.Items), err)
		}
		waitClosed(3)
	}
	for _, tc := range []struct {
		m    int32
		want string
	}{{2, "登录"}, {3, "验证"}} {
		mode.Store(tc.m)
		if err := s.run(ctx, camoufox.Navigate(start.String())); err != nil {
			t.Fatal(err)
		}
		before := count("/@alice/video/123")
		got, err = s.Capture(ctx)
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(got.Items) != 0 || count("/@alice/video/123") != before {
			t.Fatalf("mode=%d count=%d err=%v", tc.m, len(got.Items), err)
		}
		waitClosed(1)
	}
	mode.Store(4)
	if err := s.run(ctx, camoufox.Navigate(start.String())); err != nil {
		t.Fatal(err)
	}
	stopped, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { _, err := s.Capture(stopped); done <- err }()
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("post not reached")
	}
	stop()
	close(releaseCancel)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	waitClosed(2)
	mode.Store(7)
	if err := s.run(ctx, camoufox.Navigate(start.String())); err != nil {
		t.Fatal(err)
	}
	type result struct {
		found Discovery
		err   error
	}
	changed := make(chan result, 1)
	go func() { found, err := s.Capture(ctx); changed <- result{found, err} }()
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("page change fixture did not start")
	}
	if err := s.run(ctx, chromedp.Evaluate(`history.replaceState({}, '', '/@bob')`, nil)); err != nil {
		t.Fatal(err)
	}
	close(releaseChange)
	gotChange := <-changed
	if gotChange.err == nil || !strings.Contains(gotChange.err.Error(), "原页面已更改") || len(gotChange.found.Items) != 0 {
		t.Fatalf("original page changed: count=%d err=%v", len(gotChange.found.Items), gotChange.err)
	}
	waitClosed(2)
	if leaked.Load() {
		t.Fatalf("unexpected host, path, or query requested: %s", <-leakReason)
	}
}
