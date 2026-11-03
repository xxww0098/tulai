package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type doubanRoundTrip func(*http.Request) (*http.Response, error)

func (fn doubanRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func doubanTestResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDoubanRoutes(t *testing.T) {
	for _, tc := range []struct{ raw, kind string }{
		{"https://www.douban.com/photos/album/123/", "album"},
		{"https://www.douban.com/photos/photo/456/", "photo"},
		{"https://movie.douban.com/photos/photo/456/", "movie-photo"},
		{"https://movie.douban.com/subject/123/photos?type=S", "movie-subject"},
		{"https://movie.douban.com/celebrity/123/photos/", "movie-celebrity"},
		{"https://www.douban.com/personage/123/photos/", "personage-album"},
		{"https://www.douban.com/personage/123/photo/456/", "personage-photo"},
		{"https://douban.com/photos/album/123", "album"},
		{"https://www.douban.com.evil.test/photos/album/123/", ""},
		{"https://user@www.douban.com/photos/album/123/", ""},
		{"https://www.douban.com:8443/photos/album/123/", ""},
		{"https://www.douban.com/photos/album/0/", ""},
		{"https://www.douban.com/photos/album/1/other", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := doubanRouteOf(u).kind; got != tc.kind {
			t.Errorf("route %s = %q, want %q", tc.raw, got, tc.kind)
		}
	}
}

func TestDoubanOriginalRestriction(t *testing.T) {
	base, _ := url.Parse("https://www.douban.com/photos/photo/123/")
	for _, tc := range []struct{ raw, want string }{
		{"https://img3.doubanio.com/view/photo/m/public/p123.jpg", "https://img3.doubanio.com/view/photo/raw/public/p123.jpg"},
		{"http://img9.douban.com/view/photo/l/public/p123.png?x=1", "https://img9.douban.com/view/photo/raw/public/p123.png?x=1"},
		{"https://img2.doubanio.com/view/photo/raw/public/p123.gif", "https://img2.doubanio.com/view/photo/raw/public/p123.gif"},
		{"https://img3.doubanio.com.evil.test/view/photo/l/public/p123.jpg", ""},
		{"https://user@img3.doubanio.com/view/photo/l/public/p123.jpg", ""},
		{"https://img3.doubanio.com:8443/view/photo/l/public/p123.jpg", ""},
		{"https://asset.doubanio.com/view/photo/l/public/p123.jpg", ""},
		{"https://img3.doubanio.com/view/photo/l/public/../../etc", ""},
	} {
		got, _, ok := doubanOriginal(base, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q, %t; want %q", tc.raw, got, ok, tc.want)
		}
	}
}

func TestDoubanAlbumPaginationAndHeaders(t *testing.T) {
	root, _ := url.Parse("https://www.douban.com/photos/album/12/?secret=not-forwarded")
	var paths []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
		prepare(req)
		if req.URL.Host != "www.douban.com" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Referer") != "https://www.douban.com/" {
			t.Fatalf("page request %s headers=%v", req.URL, req.Header)
		}
		paths = append(paths, req.URL.RawQuery)
		if req.URL.Query().Get("m_start") == "18" {
			return doubanTestResponse(raw, 200, `<html><title>album</title><div class="photolst"><img src="https://img3.doubanio.com/view/photo/m/public/p2.jpg"></div><img src="https://img3.doubanio.com/view/photo/m/public/p999.jpg"></html>`), nil
		}
		return doubanTestResponse(raw, 200, `<html><title>album</title><div class="photolst clearfix"><a class="photolst_photo"><img src="https://img3.doubanio.com/view/photo/m/public/p1.jpg"></a></div><link rel="next" href="?m_start=18"></html>`), nil
	})
	got, err := discoverDouban(context.Background(), getter, root)
	if err != nil || len(got.Items) != 2 || got.Title != "album" || strings.Join(paths, ",") != "secret=not-forwarded,m_start=18" {
		t.Fatalf("album=%+v paths=%v err=%v", got, paths, err)
	}
	for i, item := range got.Items {
		want := fmt.Sprintf("https://img3.doubanio.com/view/photo/raw/public/p%d.jpg", i+1)
		if item.URL != want || item.Headers["Referer"] != "https://www.douban.com/" || item.Headers["Cookie"] != "" || item.Metadata["album_id"] != "12" || item.Metadata["photo_id"] != fmt.Sprint(i+1) {
			t.Fatalf("item %d = %+v", i, item)
		}
	}
}

func TestDoubanSinglePhotoSelection(t *testing.T) {
	root, _ := url.Parse("https://www.douban.com/photos/photo/123/")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return doubanTestResponse(raw, 200, `<html><title>photo</title><a class="mainphoto" href="/photos/photo/456/"><img src="https://img9.doubanio.com/view/photo/l/public/p123.jpg"></a><img src="https://img9.doubanio.com/view/photo/l/public/p456.jpg"><link rel="next" href="/photos/photo/456/"></html>`), nil
	})
	got, err := discoverDouban(context.Background(), getter, root)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://img9.doubanio.com/view/photo/raw/public/p123.jpg" {
		t.Fatalf("single photo=%+v err=%v", got, err)
	}
}

func TestDoubanPreservesPartialAndRejectsUnsafePage(t *testing.T) {
	root, _ := url.Parse("https://www.douban.com/photos/album/12/")
	for _, mode := range []string{"next-host", "next-album", "loop", "status", "login-redirect", "cancel", "invalid-image"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				if calls == 2 {
					if mode == "cancel" {
						cancel()
					}
					if mode == "status" {
						return doubanTestResponse(raw, 403, "blocked"), nil
					}
					if mode == "login-redirect" {
						r := doubanTestResponse(raw, 200, "<html>login</html>")
						r.FinalURL = "https://sec.douban.com/c"
						return r, nil
					}
				}
				next := "?m_start=18"
				switch mode {
				case "next-host":
					next = "https://attacker.test/photos/album/12/?m_start=18"
				case "next-album":
					next = "/photos/album/99/?m_start=18"
				case "loop":
					next = "/photos/album/12/"
				}
				image := "https://img3.doubanio.com/view/photo/m/public/p1.jpg"
				if mode == "invalid-image" {
					image = "https://attacker.test/image.jpg"
					next = ""
				}
				return doubanTestResponse(raw, 200, fmt.Sprintf(`<html><div class="photolst"><img src="%s"></div><link rel="next" href="%s"></html>`, image, next)), nil
			})
			got, err := discoverDouban(ctx, getter, root)
			if err == nil || (mode != "invalid-image" && len(got.Items) != 1) || (mode == "cancel" && !errors.Is(err, context.Canceled)) || ((mode == "next-host" || mode == "next-album" || mode == "loop") && calls != 1) {
				t.Fatalf("mode=%s images=%d calls=%d err=%v", mode, len(got.Items), calls, err)
			}
		})
	}
}

func TestDoubanPageLimitReportsPartial(t *testing.T) {
	root, _ := url.Parse("https://www.douban.com/photos/album/12/")
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		body := fmt.Sprintf(`<html><div class="photolst"><img src="https://img3.doubanio.com/view/photo/m/public/p%d.jpg"></div><link rel="next" href="?m_start=%d"></html>`, calls, calls)
		return doubanTestResponse(raw, 200, body), nil
	})
	got, err := discoverDouban(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "500") || len(got.Items) != maxDoubanPages || calls != maxDoubanPages {
		t.Fatalf("page limit images=%d calls=%d err=%v", len(got.Items), calls, err)
	}
}

func TestDoubanRenderedCaptureCookieScope(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<html><title>synthetic start</title></html>`)
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const target = "https://www.douban.com/photos/album/12/"
	const imageURL = "https://img3.doubanio.com/view/photo/raw/public/p1.jpg"
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, 200).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(`<html><title>synthetic album</title><img src="https://example.invalid/recommended.jpg"></html>`)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case responses <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	session.start, _ = url.Parse(target)
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.douban.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	type capturedRequest struct{ host, cookie, authorization string }
	var mu sync.Mutex
	var requests []capturedRequest
	session.getter.client.Transport = doubanRoundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests = append(requests, capturedRequest{r.URL.Host, r.Header.Get("Cookie"), r.Header.Get("Authorization")})
		mu.Unlock()
		body := "image"
		if r.URL.Host == "www.douban.com" {
			body = `<html><title>synthetic album</title><div class="photolst"><img src="https://img3.doubanio.com/view/photo/m/public/p2.jpg"></div></html>`
			if r.URL.Query().Get("m_start") == "" {
				body = `<html><title>synthetic album</title><div class="photolst"><img src="https://img3.doubanio.com/view/photo/m/public/p1.jpg"></div><link rel="next" href="?m_start=18"></html>`
			}
		}
		h := make(http.Header)
		h.Set("Content-Type", "text/html; charset=utf-8")
		return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != imageURL || session.PageURL() != target {
		t.Fatalf("capture=%+v url=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, func(r *http.Request) { r.Header.Set("Referer", got.Items[0].Headers["Referer"]) })
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 3 || requests[0].host != "www.douban.com" || requests[1].host != "www.douban.com" ||
		!strings.Contains(requests[0].cookie, "synthetic_session=example") || !strings.Contains(requests[1].cookie, "synthetic_session=example") ||
		requests[2].host != "img3.doubanio.com" || requests[2].cookie != "" || requests[2].authorization != "" {
		t.Fatalf("incorrect synthetic credential scope: %+v", requests)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestDoubanLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_DOUBAN_LIVE") != "1" {
		t.Skip("set TULAI_DOUBAN_LIVE=1 for public Douban probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	for _, raw := range []string{"https://www.douban.com/photos/photo/1642354684/", "https://www.douban.com/photos/album/74539453/", "https://www.douban.com/photos/album/120704503/"} {
		root, _ := url.Parse(raw)
		got, err := discoverDouban(ctx, getter, root)
		if err != nil || len(got.Items) == 0 {
			t.Fatalf("public %s: count=%d err=%v", raw, len(got.Items), err)
		}
		if strings.Contains(raw, "120704503") && len(got.Items) < 19 {
			t.Fatalf("public album pagination count=%d", len(got.Items))
		}
	}
}

func TestDoubanLiveDownload(t *testing.T) {
	if os.Getenv("TULAI_DOUBAN_LIVE_DOWNLOAD") != "1" {
		t.Skip("set TULAI_DOUBAN_LIVE_DOWNLOAD=1 to test public CDN access")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	root, _ := url.Parse("https://www.douban.com/photos/photo/1642354684/")
	got, err := discoverDouban(ctx, getter, root)
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("public photo discovery images=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	err = New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil)
	entries, readErr := os.ReadDir(dir)
	if err != nil || readErr != nil || len(entries) != 1 {
		t.Fatalf("public photo download err=%v entries=%v read=%v", err, entries, readErr)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("download is empty: info=%v err=%v", info, err)
	}
}
