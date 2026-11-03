package download

import (
	"context"
	"crypto/tls"
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

func yupooTestDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestYupooRoutesAndSources(t *testing.T) {
	for _, tc := range []struct {
		raw, owner, kind, id string
	}{
		{"https://store.x.yupoo.com/", "store", "home", ""},
		{"https://store.x.yupoo.com/albums?tab=gallery", "store", "home", ""},
		{"https://store.x.yupoo.com/albums/123?uid=1", "store", "album", "123"},
		{"https://store.x.yupoo.com/categories/234", "store", "categories", "234"},
		{"https://store.x.yupoo.com/collections/345", "store", "collections", "345"},
		{"https://store.x.yupoo.com/search/album?q=coat", "store", "search", ""},
		{"https://x.yupoo.com/photos/Store/albums/123", "store", "album", "123"},
	} {
		u, _ := url.Parse(tc.raw)
		got := yupooRouteOf(u)
		if got.owner != tc.owner || got.kind != tc.kind || got.id != tc.id || !isYupooPage(u) {
			t.Errorf("route %q = %+v", tc.raw, got)
		}
	}
	for _, raw := range []string{
		"https://evil.example/albums/123", "https://store.x.yupoo.com.evil.example/albums/123",
		"https://store.x.yupoo.com:444/albums/123", "https://user@store.x.yupoo.com/albums/123",
		"https://x.yupoo.com/photos/a/../b/albums/123", "https://store.x.yupoo.com/albums/abc",
		"https://store.x.yupoo.com/albums/%31%32%33",
	} {
		u, _ := url.Parse(raw)
		if isYupooPage(u) {
			t.Errorf("accepted unsafe route %q", raw)
		}
	}
	page, _ := url.Parse("https://store.x.yupoo.com/albums/123?uid=1")
	for _, tc := range []struct{ raw, want string }{
		{"//photo.yupoo.com/store/a/original.jpg", "https://photo.yupoo.com/store/a/original.jpg"},
		{"https://photo.yupoo.com/store/a/original.png?sig=1", "https://photo.yupoo.com/store/a/original.png?sig=1"},
		{"https://photo.yupoo.com:444/store/a.jpg", ""},
		{"https://user@photo.yupoo.com/store/a.jpg", ""},
		{"https://photo.yupoo.com.evil.example/store/a.jpg", ""},
		{"https://127.0.0.1/a.jpg", ""},
		{"https://photo.yupoo.com/a/../other.jpg", ""},
	} {
		got, ok := yupooImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("source %q = %q, %v", tc.raw, got, ok)
		}
	}
	home, _ := url.Parse("https://store.x.yupoo.com/")
	if next, ok := yupooPaginationLink(home, "/albums?page=2"); !ok || next.String() != "https://store.x.yupoo.com/albums?page=2" {
		t.Fatalf("homepage pagination = %v, %v", next, ok)
	}
}

func TestYupooAlbumOriginalAndIdentity(t *testing.T) {
	page, _ := url.Parse("https://store.x.yupoo.com/albums/123?uid=1")
	doc := yupooTestDoc(t, `<title>album</title><h1 class="showalbumheader__gallerytitle">Original album</h1>
<div class="showalbum__children"><div class="image__imagewrap" data-type="photo"><img data-album-id="123" data-origin-src="//photo.yupoo.com/store/a/original.jpg" data-src="//photo.yupoo.com/store/a/big.jpg"></div><h3 title="Photo name">ignored</h3></div>
<div class="image__imagewrap" data-type="video"><img data-album-id="123" data-origin-src="//uvd.yupoo.com/store/video.jpg" data-path="/store/video.mp4"></div>
<img data-album-id="999" data-origin-src="//photo.yupoo.com/other/not-ours.jpg">
<img data-album-id="123" data-origin-src="https://evil.example/leak.jpg"><img src="//photo.yupoo.com/store/cover.jpg">`)
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	})
	got, err := yupooCollect(context.Background(), getter, page, doc)
	if err == nil || len(got.Items) != 1 || got.Title != "Original album" || got.Items[0].URL != "https://photo.yupoo.com/store/a/original.jpg" || got.Items[0].Metadata["title"] != "Photo name" || got.Items[0].Metadata["album_id"] != "123" || got.Items[0].Headers["Referer"] != "https://store.x.yupoo.com/" || !got.Items[0].RequireImage {
		t.Fatalf("album=%+v err=%v", got, err)
	}
	if !strings.Contains(err.Error(), "视频") || !strings.Contains(err.Error(), "不属于") || !strings.Contains(err.Error(), "来源") {
		t.Fatalf("partial warnings=%v", err)
	}
}

func TestYupooListingPaginationAndPartial(t *testing.T) {
	page, _ := url.Parse("https://store.x.yupoo.com/albums")
	doc := yupooTestDoc(t, `<title>Store</title>
<a class="album__main" href="/albums/11?uid=1">one</a>
<a class="album3__main" href="https://other.x.yupoo.com/albums/99?uid=1">outside</a>
<a href="?page=2">next</a>
<a href="?page=3&amp;tab=other">wrong filter</a>
<div class="showheader__category"><a href="/categories/33">category</a></div>`)
	var fetched []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Host != "store.x.yupoo.com" {
			t.Errorf("cross-tenant request %q", raw)
			return nil, errors.New("unexpected host")
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		if req.Header.Get("Referer") != "https://store.x.yupoo.com/" {
			t.Errorf("page referer = %q", req.Header.Get("Referer"))
		}
		fetched = append(fetched, u.Path+"?"+u.RawQuery)
		status := 200
		var body string
		switch {
		case u.Path == "/albums" && u.Query().Get("page") == "2":
			body = `<a class="album4__main" href="/albums/11?uid=1">duplicate</a><a class="album6__main" href="/albums/13?uid=1">three</a>`
		case u.Path == "/categories/33":
			body = `<a class="album__main other" href="/albums/12?uid=1">two</a>`
		case u.Path == "/albums/11" && u.Query().Get("page") == "":
			body = `<h1 class="showalbumheader__gallerytitle">One</h1><img data-album-id="11" data-origin-src="//photo.yupoo.com/store/1/a.jpg"><a href="?page=2">next</a>`
		case u.Path == "/albums/11" && u.Query().Get("page") == "2":
			body = `<h1 class="showalbumheader__gallerytitle">One</h1><img data-album-id="11" data-origin-src="//photo.yupoo.com/store/1/b.jpg">`
		case u.Path == "/albums/13":
			body = `<h1 class="showalbumheader__gallerytitle">Three</h1><img data-album-id="13" data-origin-src="//photo.yupoo.com/store/3/a.jpg">`
		case u.Path == "/albums/12":
			status, body = 567, `<title>Access Restricted</title>`
		default:
			t.Errorf("unexpected request %q", raw)
			return nil, errors.New("unexpected request")
		}
		return &Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got, err := yupooCollect(context.Background(), getter, page, doc)
	if err == nil || len(got.Items) != 3 || len(fetched) != 6 {
		t.Fatalf("listing=%+v fetched=%v err=%v", got, fetched, err)
	}
	if !strings.Contains(err.Error(), "分页参数不一致") {
		t.Fatalf("pagination scope warning=%v", err)
	}
	if got.Items[0].Metadata["album_id"] != "11" || got.Items[1].Metadata["album_id"] != "11" || got.Items[2].Metadata["album_id"] != "13" {
		t.Fatalf("listing metadata = %+v", got.Items)
	}
}

func TestYupooRedirectAndCancellation(t *testing.T) {
	page, _ := url.Parse("https://x.yupoo.com/photos/store/albums/123?uid=1")
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: "https://x.yupoo.com/photos/other/albums/123?uid=1", Body: io.NopCloser(strings.NewReader(`<img data-origin-src="//photo.yupoo.com/other/a.jpg">`))}, nil
	})
	got, err := discoverYupoo(context.Background(), getter, page)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("cross-user redirect=%+v err=%v", got, err)
	}
	blocked := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 567, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader("blocked"))}, nil
	})
	if _, err := discoverYupoo(context.Background(), blocked, page); err == nil || !strings.Contains(err.Error(), "验证") {
		t.Fatalf("blocked error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := yupooCollect(ctx, getter, page, yupooTestDoc(t, `<p></p>`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error=%v", err)
	}
}

func TestYupooCancellationPreservesImages(t *testing.T) {
	page, _ := url.Parse("https://store.x.yupoo.com/albums/123?uid=1")
	doc := yupooTestDoc(t, `<h1 class="showalbumheader__gallerytitle">one</h1><img data-album-id="123" data-origin-src="//photo.yupoo.com/store/a.jpg"><a href="?page=2">next</a>`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		cancel()
		return nil, ctx.Err()
	})
	got, err := yupooCollect(ctx, getter, page, doc)
	if !errors.Is(err, context.Canceled) || len(got.Items) != 1 {
		t.Fatalf("canceled album=%+v err=%v", got, err)
	}
}

func TestYupooEmptyAndProtectedAlbum(t *testing.T) {
	page, _ := url.Parse("https://store.x.yupoo.com/albums/123?uid=1")
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request")
		return nil, nil
	})
	for _, tc := range []struct{ body, expected string }{
		{`<title>this album is not exist</title>`, "未返回原图"},
		{`<form><input type="password"></form>`, "需要密码"},
		{`<title>Access Restricted</title>`, "安全验证"},
	} {
		got, err := yupooCollect(context.Background(), getter, page, yupooTestDoc(t, tc.body))
		if err == nil || !strings.Contains(err.Error(), tc.expected) || len(got.Items) != 0 {
			t.Errorf("protected album=%+v err=%v", got, err)
		}
	}
}

func TestYupooPageLimit(t *testing.T) {
	page, _ := url.Parse("https://store.x.yupoo.com/albums?page=1")
	doc := yupooTestDoc(t, `<a href="?page=2">next</a>`)
	calls := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		u, _ := url.Parse(raw)
		n, _ := yupooPageNumber(u)
		body := `<a href="?page=` + strconv.Itoa(n+1) + `">next</a>`
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	_, err := yupooCollect(context.Background(), getter, page, doc)
	if err == nil || !strings.Contains(err.Error(), "500 页上限") || calls != maxYupooPages-1 {
		t.Fatalf("page limit calls=%d err=%v", calls, err)
	}
}

func TestYupooRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	png := filterPNG(t, 1, 1)
	var subpages, imageRequests, badRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "photo.yupoo.com" {
			imageRequests.Add(1)
			if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "https://store.x.yupoo.com/" {
				badRequests.Add(1)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		if r.Host == "evil.invalid" {
			if r.Header.Get("Cookie") != "" {
				badRequests.Add(1)
			}
			_, _ = io.WriteString(w, `<html><img data-origin-src="//photo.yupoo.com/evil.png"></html>`)
			return
		}
		if r.Host != "store.x.yupoo.com" {
			badRequests.Add(1)
			http.Error(w, "unexpected host", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/albums/11" && r.URL.Query().Get("page") == "" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/", Secure: true})
			_, _ = io.WriteString(w, `<html><title>Eleven</title><h1 class="showalbumheader__gallerytitle">Eleven</h1><img data-album-id="11" data-origin-src="//photo.yupoo.com/store/one.png"><a href="?uid=1&amp;page=2">next</a></html>`)
			return
		}
		if r.URL.Path == "/albums/11" && r.URL.Query().Get("page") == "2" {
			subpages.Add(1)
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "synthetic" || r.URL.Query().Get("uid") != "1" {
				badRequests.Add(1)
			}
			_, _ = io.WriteString(w, `<html><h1 class="showalbumheader__gallerytitle">Eleven</h1><img data-album-id="11" data-origin-src="//photo.yupoo.com/store/two.png"></html>`)
			return
		}
		if r.URL.Path == "/albums/12" {
			_, _ = io.WriteString(w, `<html><title>Twelve</title><img data-album-id="12" data-origin-src="//photo.yupoo.com/store/three.png"><img data-album-id="11" data-origin-src="//photo.yupoo.com/store/wrong.png"></html>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://store.x.yupoo.com/albums/11?uid=1")
	getter := NewHTTPGetter("")
	transport := getter.client.Transport.(*http.Transport).Clone()
	// These test-only destinations point to the synthetic local TLS server.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "store.x.yupoo.com:443" && addr != "photo.yupoo.com:443" {
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
	if err != nil || len(got.Items) != 2 || subpages.Load() != 1 || badRequests.Load() != 0 {
		t.Fatalf("rendered album=%+v pages=%d unexpected=%d err=%v", got, subpages.Load(), badRequests.Load(), err)
	}
	if err := New(Options{Getter: session}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("download image: %v", err)
	}
	if imageRequests.Load() != 1 || badRequests.Load() != 0 {
		t.Fatalf("image requests=%d unexpected=%d", imageRequests.Load(), badRequests.Load())
	}
	if err := session.run(ctx, camoufox.Navigate("https://store.x.yupoo.com/albums/12?uid=1")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || len(got.Items) != 1 || got.Items[0].Metadata["album_id"] != "12" {
		t.Fatalf("current album=%+v err=%v", got, err)
	}
	if err := session.run(ctx, camoufox.Navigate("https://evil.invalid/login")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || len(got.Items) != 0 || badRequests.Load() != 0 {
		t.Fatalf("foreign page=%+v unexpected=%d err=%v", got, badRequests.Load(), err)
	}
}
