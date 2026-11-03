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

	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

func unsplashTestDoc(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestUnsplashRoutesAndDownloads(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		search  bool
	}{
		{"https://unsplash.com/photos/LBI7cgq3pbM", "LBI7cgq3pbM", false},
		{"https://unsplash.com/photos/a-view-of-hills-cs-fGIqlKQs?utm_source=x", "cs-fGIqlKQs", false},
		{"https://unsplash.com/s/photos/landscape-photos?orientation=landscape", "", true},
	} {
		u, _ := url.Parse(tc.raw)
		got := unsplashRouteOf(u)
		if got.photo != tc.id || got.search != tc.search || !isUnsplashPage(u) {
			t.Errorf("route %q=%+v", tc.raw, got)
		}
	}
	for _, raw := range []string{
		"https://user@unsplash.com/photos/LBI7cgq3pbM", "https://unsplash.com:444/photos/LBI7cgq3pbM",
		"https://unsplash.com.evil.invalid/photos/LBI7cgq3pbM", "https://127.0.0.1/photos/LBI7cgq3pbM",
		"https://unsplash.com/photos/LBI7cgq3pbM/download", "https://unsplash.com/s/photos/",
		"https://unsplash.com/s/photos/cats%2Fother", "https://unsplash.com/photos/abc",
	} {
		u, _ := url.Parse(raw)
		if isUnsplashPage(u) {
			t.Errorf("accepted route %q", raw)
		}
	}
	page, _ := url.Parse("https://unsplash.com/photos/a-view-LBI7cgq3pbM")
	for _, tc := range []struct{ raw, expected string }{
		{"/photos/LBI7cgq3pbM/download?force=true", "https://unsplash.com/photos/LBI7cgq3pbM/download?force=true"},
		{"https://unsplash.com/photos/LBI7cgq3pbM/download?ixid=abc", "https://unsplash.com/photos/LBI7cgq3pbM/download?ixid=abc"},
		{"https://unsplash.com/photos/BBBBBBBBBBB/download", ""},
		{"https://evil.invalid/photos/LBI7cgq3pbM/download", ""},
		{"https://plus.unsplash.com/photos/LBI7cgq3pbM/download", ""},
		{"https://user@unsplash.com/photos/LBI7cgq3pbM/download", ""},
		{"https://unsplash.com:444/photos/LBI7cgq3pbM/download", ""},
		{"https://unsplash.com/photos/LBI7cgq3pbM/download%2Fextra", ""},
	} {
		got, ok := unsplashDownloadURL(page, tc.raw, "LBI7cgq3pbM")
		if got != tc.expected || ok != (tc.expected != "") {
			t.Errorf("download %q=%q %v", tc.raw, got, ok)
		}
	}
}

func TestUnsplashWWWCanonical(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Hostname() != "unsplash.com" {
			t.Fatalf("expected canonical Unsplash host, got %s", raw)
		}
		return tiebaTestResponse(raw, http.StatusOK, `<a data-testid="non-sponsored-photo-download-button" href="/photos/LBI7cgq3pbM/download?force=true">download</a>`), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.unsplash.com/photos/LBI7cgq3pbM", 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://unsplash.com/photos/LBI7cgq3pbM/download?force=true" {
		t.Fatalf("canonical result=%+v err=%v", got.Items, err)
	}
}

func TestUnsplashPhotoButtonAndEntitlement(t *testing.T) {
	page, _ := url.Parse("https://unsplash.com/photos/beautiful-view-LBI7cgq3pbM?private=1")
	doc := unsplashTestDoc(t, `<title>Fallback</title><meta property="og:title" content="Photo title">
<img src="https://images.unsplash.com/thumbnail.jpg">
<a data-testid="non-sponsored-photo-download-button" title="Download photo" href="/photos/LBI7cgq3pbM/download?force=true">Download</a>
<a data-test="non-sponsored-photo-download-button" href="https://evil.invalid/photos/LBI7cgq3pbM/download">fake</a>`)
	got, err := unsplashPhotoPage(context.Background(), page, doc)
	if err == nil || len(got.Items) != 1 || got.Title != "Fallback" || got.Items[0].URL != "https://unsplash.com/photos/LBI7cgq3pbM/download?force=true" || got.Items[0].Headers["Referer"] != "https://unsplash.com/" || got.Items[0].Metadata["unsplash_photo_id"] != "LBI7cgq3pbM" || !got.Items[0].RequireImage {
		t.Fatalf("photo=%+v err=%v", got, err)
	}
	for _, content := range []string{
		`<a href="/plus">Upgrade to Unsplash+</a><img src="https://plus.unsplash.com/preview.jpg">`,
		`<a data-test="non-sponsored-photo-download-button" href="/photos/XXXXXXXXXXX/download">wrong photo</a>`,
		`<a data-test="other" href="/photos/LBI7cgq3pbM/download">other button</a>`,
	} {
		got, err := unsplashPhotoPage(context.Background(), page, unsplashTestDoc(t, content))
		if err == nil || len(got.Items) != 0 {
			t.Errorf("unavailable=%+v err=%v", got, err)
		}
	}
}

func TestUnsplashSearchPartialAndCancellation(t *testing.T) {
	page, _ := url.Parse("https://unsplash.com/s/photos/cats?color=blue")
	doc := unsplashTestDoc(t, `<title>cats</title>
<a href="/photos/a-AAAAAAAAAAA?utm_source=tracking">first</a>
<a href="/photos/a-AAAAAAAAAAA">duplicate</a>
<a data-testid="non-sponsored-photo-download-button" href="/photos/AAAAAAAAAAA/download?force=true">download first</a>
<a data-testid="non-sponsored-photo-download-button" href="/photos/ZZZZZZZZZZZ/download?force=true">unlinked</a>
<a href="/photos/b-BBBBBBBBBBB">second</a>
<a href="/photos/c-CCCCCCCCCCC">subscription</a>
<a href="https://evil.invalid/photos/d-DDDDDDDDDDD">foreign</a>`)
	calls := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		u, _ := url.Parse(raw)
		if u.Host != "unsplash.com" || u.RawQuery != "" {
			t.Errorf("unsafe child %q", raw)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		if req.Header.Get("Referer") != "https://unsplash.com/" {
			t.Errorf("referer=%q", req.Header.Get("Referer"))
		}
		id := unsplashRouteOf(u).photo
		body := `<a data-test="non-sponsored-photo-download-button" href="/photos/` + id + `/download?force=true">download</a>`
		if id == "CCCCCCCCCCC" {
			body = `<a href="/plus">Subscribe</a>`
		}
		if id == "BBBBBBBBBBB" {
			return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: "https://unsplash.com/photos/XXXXXXXXXXX", Body: io.NopCloser(strings.NewReader(body))}, nil
		}
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got, err := unsplashCollect(context.Background(), getter, page, doc)
	if err == nil || len(got.Items) != 1 || calls != 2 || !strings.Contains(err.Error(), "2 张") || !strings.Contains(err.Error(), "可能不完整") {
		t.Fatalf("search=%+v calls=%d err=%v", got, calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls = 0
	cancelGetter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		if calls == 2 {
			cancel()
			return nil, ctx.Err()
		}
		id := unsplashRouteOf(mustParseURL(t, raw)).photo
		body := `<a data-test="non-sponsored-photo-download-button" href="/photos/` + id + `/download">download</a>`
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got, err = unsplashCollect(ctx, cancelGetter, page, doc)
	if !errors.Is(err, context.Canceled) || len(got.Items) != 2 {
		t.Fatalf("canceled=%+v err=%v", got, err)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestUnsplashSearchLimitAndRedirect(t *testing.T) {
	page, _ := url.Parse("https://unsplash.com/s/photos/cats?color=blue")
	var markup strings.Builder
	for i := 0; i < maxUnsplashPhotos+1; i++ {
		markup.WriteString(`<a href="/photos/` + strings.Repeat("A", 11-len(strconv.Itoa(i))) + strconv.Itoa(i) + `">photo</a>`)
	}
	links, limited := unsplashSearchLinks(page, unsplashTestDoc(t, markup.String()), map[string]bool{}, nil)
	if len(links) != maxUnsplashPhotos || !limited {
		t.Fatalf("links=%d limited=%v", len(links), limited)
	}
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: "https://unsplash.com/s/photos/cats?color=red", Body: io.NopCloser(strings.NewReader(`<p>wrong search</p>`))}, nil
	})
	got, err := discoverUnsplash(context.Background(), getter, page)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect=%+v err=%v", got, err)
	}
}

func TestUnsplashRenderedSearch(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	png := filterPNG(t, 1, 1)
	var childPhotos, downloads, images, badRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "evil.invalid" {
			badRequests.Add(1)
			http.Error(w, "blocked", http.StatusForbidden)
			return
		}
		if r.Host == "images.unsplash.com" {
			images.Add(1)
			if r.Header.Get("Cookie") != "" {
				badRequests.Add(1)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		if r.Host != "unsplash.com" {
			badRequests.Add(1)
			http.Error(w, "unexpected host", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/download") {
			downloads.Add(1)
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "synthetic" {
				badRequests.Add(1)
			}
			http.Redirect(w, r, "https://images.unsplash.com/photo.png", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/s/photos/cats":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/", Secure: true})
			_, _ = io.WriteString(w, `<title>Search cats</title><style>body {height:5000px}</style>
<a href="/photos/cat-AAAAAAAAAAA?utm_source=tracking">one</a>
<a data-testid="non-sponsored-photo-download-button" href="/photos/AAAAAAAAAAA/download?force=true">download one</a>
<a href="/photos/plus-CCCCCCCCCCC">plus</a>
<a href="/photos/redirect-DDDDDDDDDDD">redirect</a>
<button onclick="if (!this.dataset.done) { this.dataset.done='1'; document.body.insertAdjacentHTML('beforeend','&lt;a href=&quot;/photos/cat-BBBBBBBBBBB&quot;&gt;two&lt;/a&gt;'); }">Load more</button>`)
		case "/photos/cat-AAAAAAAAAAA", "/photos/cat-BBBBBBBBBBB":
			childPhotos.Add(1)
			id := unsplashRouteOf(&url.URL{Scheme: "https", Host: "unsplash.com", Path: r.URL.Path}).photo
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "synthetic" {
				badRequests.Add(1)
			}
			_, _ = io.WriteString(w, `<title>Photo</title><a data-test="non-sponsored-photo-download-button" href="/photos/`+id+`/download?force=true">download</a>`)
		case "/photos/plus-CCCCCCCCCCC":
			_, _ = io.WriteString(w, `<a href="/plus">Subscribe</a><img data-src="https://plus.unsplash.com/private.jpg">`)
		case "/photos/redirect-DDDDDDDDDDD":
			http.Redirect(w, r, "https://evil.invalid/steal", http.StatusFound)
		case "/login":
			_, _ = io.WriteString(w, `<a data-testid="non-sponsored-photo-download-button" href="/photos/AAAAAAAAAAA/download">fake login</a>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://unsplash.com/s/photos/cats")
	getter := NewHTTPGetter("")
	getter.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := getter.client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec -- synthetic local TLS server
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "unsplash.com:443" && addr != "images.unsplash.com:443" {
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
	if err := session.run(ctx, camoufox.Navigate(page.String()), chromedp.Evaluate(`window.scrollTo(0,333)`, nil)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || len(got.Items) != 2 || childPhotos.Load() != 1 || !strings.Contains(err.Error(), "不完整") || !strings.Contains(err.Error(), "2 张") || badRequests.Load() != 0 {
		t.Fatalf("search=%+v photos=%d bad=%d err=%v", got, childPhotos.Load(), badRequests.Load(), err)
	}
	var current string
	var scrollY float64
	if err := session.run(ctx, chromedp.Location(&current), chromedp.Evaluate(`window.scrollY`, &scrollY)); err != nil {
		t.Fatal(err)
	}
	if current != page.String() || scrollY != 333 {
		t.Fatalf("original page=%q scroll=%v", current, scrollY)
	}
	if err := New(Options{Getter: session}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("download image: %v", err)
	}
	if downloads.Load() != 1 || images.Load() != 1 || badRequests.Load() != 0 {
		t.Fatalf("downloads=%d images=%d bad=%d", downloads.Load(), images.Load(), badRequests.Load())
	}
	if err := session.run(ctx, camoufox.Navigate("https://unsplash.com/photos/cat-BBBBBBBBBBB")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["unsplash_photo_id"] != "BBBBBBBBBBB" {
		t.Fatalf("current photo=%+v err=%v", got, err)
	}
	if err := session.run(ctx, camoufox.Navigate("https://unsplash.com/login")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("login=%+v err=%v", got, err)
	}
}
