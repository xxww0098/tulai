package download

import (
	"context"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func cnuTestDoc(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestCNURoutesAndImagePaths(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{"http://www.cnu.cc/works/662087", "662087"},
		{"https://cnu.cc/works/123?from=search", "123"},
		{"http://www.cnu.cc:80/works/10/", "10"},
	} {
		page, _ := url.Parse(tc.raw)
		if got := cnuPageID(page, "works"); got != tc.id || !isCNUWork(page) {
			t.Errorf("work %q = %q", tc.raw, got)
		}
	}
	for _, raw := range []string{
		"https://user@www.cnu.cc/works/123", "https://www.cnu.cc:444/works/123",
		"https://www.cnu.cc.evil.invalid/works/123", "https://127.0.0.1/works/123",
		"https://www.cnu.cc/works/0", "https://www.cnu.cc/works/abc", "https://www.cnu.cc/works/%31%32%33",
		"https://www.cnu.cc/works/123/extra", "https://www.cnu.cc/users/123",
	} {
		page, _ := url.Parse(raw)
		if isCNUWork(page) {
			t.Errorf("unsafe work %q", raw)
		}
	}
	for _, raw := range []string{"http://www.cnu.cc/users/3984", "https://cnu.cc/users/3984?page=2"} {
		page, _ := url.Parse(raw)
		if !isCNUUser(page) || isCNUWork(page) {
			t.Errorf("missing user page %q", raw)
		}
	}
	if err := cnuUserUnsupported(); err == nil || !strings.Contains(err.Error(), "单个作品") {
		t.Errorf("user warning = %v", err)
	}
	for _, tc := range []struct {
		raw, want string
		legacy    bool
	}{
		{"2502/13/a.jpg", "http://imgoss.cnu.cc/2502/13/a.jpg?x-oss-process=style/content", false},
		{"old_path/abc.JPG", "http://imgoss.cnu.cc/old_path/abc.JPG?x-oss-process=style/content", false},
		{"2015/a.png", "http://img.cnu.cc/forum/2015/a.png", true},
		{"https://evil.invalid/a.jpg", "", false}, {"//evil.invalid/a.jpg", "", false},
		{"/2502/13/a.jpg", "", false}, {"2502/../secret.jpg", "", false},
		{"2502/%2e%2e/secret.jpg", "", false}, {"2502/a.jpg?token=secret", "", false},
		{"2502/a.jpg#fragment", "", false}, {"2502/a\\b.jpg", "", false},
		{"2502/a.jpg/other.png", "http://imgoss.cnu.cc/2502/a.jpg/other.png?x-oss-process=style/content", false},
		{"2502/a.mp4", "", false}, {"2502/a.jpg\r\n", "", false},
	} {
		got, ok := cnuImageURL(tc.raw, tc.legacy)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %q = %q, %v", tc.raw, got, ok)
		}
	}
}

func TestCNUWorkJSONAndPartial(t *testing.T) {
	page, _ := url.Parse("http://www.cnu.cc/works/662087?private=1")
	doc := cnuTestDoc(t, `<title>Work title</title><img src="http://imgoss.cnu.cc/avatars/unrelated.jpg">
<div id="imgs_json" style="display:none">[{"img":"2502/13/a.jpg","width":1440,"height":960},{"img":"2502/13/b.png","height":900},{"img":"2502/13/a.jpg"},{"img":"../other.jpg"},{"img":4}]</div>
<a href="/works/999">recommendation</a>`)
	got, err := cnuWorkPage(context.Background(), doc, page)
	if err == nil || len(got.Items) != 2 || got.Title != "Work title" || !strings.Contains(err.Error(), "路径无效") {
		t.Fatalf("work=%+v err=%v", got, err)
	}
	for i, name := range []string{"a.jpg", "b.png"} {
		item := got.Items[i]
		want := "http://imgoss.cnu.cc/2502/13/" + name + "?x-oss-process=style/content"
		if item.URL != want || item.Headers["Referer"] != "http://www.cnu.cc/" || item.Metadata["cnu_work_id"] != "662087" || !item.RequireImage {
			t.Errorf("item %d=%+v", i, item)
		}
	}
	for _, source := range []string{
		`<img src="http://imgoss.cnu.cc/unrelated.jpg">`,
		`<div id="imgs_json">[]</div>`,
		`<div id="imgs_json">not json</div>`,
		`<div id="imgs_json">[{"img":"2502/a.jpg"}]</div><div id="imgs_json">[{"img":"2502/b.jpg"}]</div>`,
	} {
		got, err := cnuWorkPage(context.Background(), cnuTestDoc(t, source), page)
		if err == nil || len(got.Items) != 0 {
			t.Errorf("invalid page=%+v err=%v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cnuWorkPage(ctx, doc, page); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled=%v", err)
	}
}

func TestCNULegacyImageAndHTTPSReferer(t *testing.T) {
	page, _ := url.Parse("https://www.cnu.cc/works/123")
	doc := cnuTestDoc(t, `<div id="imgs_json">[{"img":"old/one.jpg","height":"auto"},{"img":"new/two.jpg","height":500}]</div>`)
	got, err := cnuWorkPage(context.Background(), doc, page)
	if err == nil || len(got.Items) != 2 || got.Items[0].URL != "http://img.cnu.cc/forum/old/one.jpg" || !strings.Contains(err.Error(), "尚未在线验证") {
		t.Fatalf("legacy=%+v err=%v", got, err)
	}
	for _, item := range got.Items {
		if item.Headers["Referer"] != "" {
			t.Errorf("HTTPS page referer leaked to HTTP image: %+v", item.Headers)
		}
	}
}

func TestCNUImageLimitPreservesPartial(t *testing.T) {
	page, _ := url.Parse("http://www.cnu.cc/works/123")
	var source strings.Builder
	source.WriteString(`<div id="imgs_json">[`)
	for i := 0; i <= maxCNUImages; i++ {
		if i > 0 {
			source.WriteByte(',')
		}
		source.WriteString(`{"img":"2502/` + strconv.Itoa(i) + `.jpg"}`)
	}
	source.WriteString(`]</div>`)
	got, err := cnuWorkPage(context.Background(), cnuTestDoc(t, source.String()), page)
	if err == nil || len(got.Items) != maxCNUImages || !strings.Contains(err.Error(), "10000") {
		t.Fatalf("image limit got=%d err=%v", len(got.Items), err)
	}
}

func TestCNUDiscoveryRedirectAndHTTPS(t *testing.T) {
	page, _ := url.Parse("http://cnu.cc/works/123")
	body := `<div id="imgs_json">[{"img":"2502/a.jpg","height":500}]</div>`
	for _, tc := range []struct {
		final string
		ok    bool
	}{
		{"http://www.cnu.cc/works/123", true}, {"http://www.cnu.cc/works/124", false},
		{"http://evil.invalid/works/123", false}, {"http://www.cnu.cc/login", false},
	} {
		getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
			return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: tc.final, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		got, err := discoverCNUWork(context.Background(), getter, page)
		if tc.ok && (err != nil || len(got.Items) != 1) {
			t.Errorf("valid redirect=%+v err=%v", got, err)
		}
		if !tc.ok && (err == nil || len(got.Items) != 0) {
			t.Errorf("invalid redirect=%+v err=%v", got, err)
		}
	}
	secure, _ := url.Parse("https://www.cnu.cc/works/123")
	attempts := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		attempts++
		if raw != secure.String() {
			t.Fatalf("silently downgraded to %q", raw)
		}
		return nil, errors.New("TLS failure")
	})
	if _, err := discoverCNUWork(context.Background(), getter, secure); err == nil || attempts != 1 || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("HTTPS err=%v attempts=%d", err, attempts)
	}
	downgrade := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: "http://www.cnu.cc/works/123", Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if got, err := discoverCNUWork(context.Background(), downgrade, secure); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect downgrade=%+v err=%v", got, err)
	}
}

func TestCNURenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	png := filterPNG(t, 1, 1)
	var images, badRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "imgoss.cnu.cc" {
			if r.URL.Path == "/login.png" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			images.Add(1)
			// HTTPS pages must not disclose their Referer to legacy HTTP images.
			if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" || r.URL.Query().Get("x-oss-process") != "style/content" {
				badRequests.Add(1)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		if r.Host != "www.cnu.cc" {
			badRequests.Add(1)
			http.Error(w, "unexpected host", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/works/123":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/"})
			_, _ = io.WriteString(w, `<title>First</title><div id="imgs_json">[{"img":"2025/one.png"},{"img":"2025/two.png"}]</div>`)
		case "/works/124":
			_, _ = io.WriteString(w, `<title>Second</title><div id="imgs_json">[{"img":"2025/three.png"}]</div>`)
		case "/login":
			_, _ = io.WriteString(w, `<img src="http://imgoss.cnu.cc/login.png">`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://www.cnu.cc/works/123")
	getter := NewHTTPGetter("")
	transport := getter.client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "www.cnu.cc:80" && addr != "imgoss.cnu.cc:80" {
			badRequests.Add(1)
			return nil, errors.New("blocked test destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	getter.client.Transport = transport
	session := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(page.String())); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[0].Metadata["cnu_work_id"] != "123" {
		t.Fatalf("first work=%+v err=%v", got, err)
	}
	if err := New(Options{Getter: session}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("download image: %v", err)
	}
	if images.Load() != 1 || badRequests.Load() != 0 {
		t.Fatalf("images=%d bad=%d", images.Load(), badRequests.Load())
	}
	if err := session.run(ctx, camoufox.Navigate("https://www.cnu.cc/works/124")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["cnu_work_id"] != "124" {
		t.Fatalf("current work=%+v err=%v", got, err)
	}
	if err := session.run(ctx, camoufox.Navigate("https://www.cnu.cc/login")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || len(got.Items) != 0 || badRequests.Load() != 0 {
		t.Fatalf("login=%+v bad=%d err=%v", got, badRequests.Load(), err)
	}
}
