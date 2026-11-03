package download

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	weishangTestBase = "https://a202006012055527750026253.szwego.com/static/index.html"
	weishangTestPage = weishangTestBase + "#/theme_detail/A202006012055527750026253/I202101041557160310557566"
)

type weishangRoundTrip func(*http.Request) (*http.Response, error)

func (fn weishangRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

type weishangTestPoster struct {
	post func(context.Context, string, []byte, http.Header) (*Response, error)
}

func (g weishangTestPoster) Get(context.Context, string, func(*http.Request)) (*Response, error) {
	return nil, errors.New("unexpected GET")
}
func (g weishangTestPoster) Post(ctx context.Context, raw string, body []byte, headers http.Header) (*Response, error) {
	return g.post(ctx, raw, body, headers)
}

func weishangResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestWeishangProductRoutesAndImages(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{weishangTestPage, true},
		{weishangTestBase + "#theme_detail/_album-1/_item-2?foo=bar", true},
		{weishangTestBase + "#/shop_detail/A202006012055527750026253", false},
		{weishangTestBase, false},
		{"https://user@a202006012055527750026253.szwego.com/static/index.html#/theme_detail/a/b", false},
		{"https://a202006012055527750026253.szwego.com.evil.test/static/index.html#/theme_detail/a/b", false},
		{"https://a202006012055527750026253.szwego.com:8443/static/index.html#/theme_detail/a/b", false},
		{"https://a202006012055527750026253.szwego.com/static/index.html#/theme_detail/a/../b", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isWeishangProduct(u); got != tc.want {
			t.Errorf("route %s = %t", tc.raw, got)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{"https://xcimg.szwego.com/img/a/p.jpg?sign=valid", "https://xcimg.szwego.com/img/a/p.jpg?sign=valid"},
		{"https://qnimg.szwego.com/img/b.png", "https://qnimg.szwego.com/img/b.png"},
		{"http://xcimg.szwego.com/img/a.jpg", ""},
		{"https://xcimg.szwego.com.evil.test/img/a.jpg", ""},
		{"https://user@xcimg.szwego.com/img/a.jpg", ""},
		{"https://xcimg.szwego.com:8443/img/a.jpg", ""},
		{"https://xcimg.szwego.com/img/../secret", ""},
	} {
		got, ok := weishangImageURL(tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q %t", tc.raw, got, ok)
		}
	}
}

func TestWeishangAlbumRoute(t *testing.T) {
	for _, raw := range []string{
		weishangTestBase + "#/shop_detail/A202006012055527750026253",
		weishangTestBase + "#goods_list/_album?tagId=123",
	} {
		u, err := url.Parse(raw)
		if err != nil || !isWeishangPage(u) || isWeishangProduct(u) {
			t.Fatalf("album route %s: %v", raw, err)
		}
	}
}

func TestWeishangAlbumPagination(t *testing.T) {
	page, _ := url.Parse(weishangTestBase + "#/shop_detail/A202006012055527750026253")
	var calls int
	getter := weishangTestPoster{post: func(_ context.Context, raw string, body []byte, headers http.Header) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Host != page.Host || u.Path != "/album/personal/all" || u.Query().Get("albumId") != "A202006012055527750026253" || len(body) != 0 || headers.Get("Accept") != "application/json" {
			t.Fatalf("unexpected album request %s body=%q headers=%v", raw, body, headers)
		}
		calls++
		if calls == 1 {
			return weishangResponse(raw, `{"errcode":0,"result":{"targetAlbum":{"name":"专辑"},"items":[{"goods_id":"I1","time_stamp":100,"imgsSrc":["https://xcimg.szwego.com/img/1.jpg?sign=1"]},{"goods_id":"I2","time_stamp":90,"imgsSrc":["https://qnimg.szwego.com/img/2.jpg"]}],"pagination":{"isLoadMore":true,"pageTimestamp":"90"}}}`), nil
		}
		if u.Query().Get("slipType") != "1" || u.Query().Get("timestamp") != "90" {
			t.Fatalf("wrong cursor %s", raw)
		}
		return weishangResponse(raw, `{"errcode":0,"result":{"items":[{"goods_id":"I2","imgsSrc":["https://qnimg.szwego.com/img/2.jpg"]},{"goods_id":"I3","imgsSrc":["https://xcimg.szwego.com/img/3.jpg"]}],"pagination":{"isLoadMore":false}}}`), nil
	}}
	got, err := discoverWeishang(context.Background(), getter, page)
	if err != nil || calls != 2 || got.Title != "专辑" || len(got.Items) != 3 || got.Items[0].URL != "https://xcimg.szwego.com/img/1.jpg?sign=1" || got.Items[2].Metadata["weishang_item_id"] != "I3" {
		t.Fatalf("album=%+v calls=%d err=%v", got, calls, err)
	}
}

func TestWeishangAlbumPartialAndAuthorization(t *testing.T) {
	page, _ := url.Parse(weishangTestBase + "#/shop_detail/A202006012055527750026253")
	for _, mode := range []string{"login", "failure", "cursor", "repeated", "missing-more", "missing-pagination", "invalid-image", "malformed-image", "malformed-item", "video", "restricted", "cancel", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			getter := weishangTestPoster{post: func(_ context.Context, raw string, _ []byte, _ http.Header) (*Response, error) {
				calls++
				if mode == "cancel" {
					cancel()
					return nil, context.Canceled
				}
				if mode == "failure" && calls == 2 {
					return nil, errors.New("temporary failure")
				}
				body := `{"errcode":0,"result":{"items":[{"goods_id":"I1","time_stamp":100,"imgsSrc":["https://xcimg.szwego.com/img/1.jpg"]}],"pagination":{"isLoadMore":false}}}`
				switch mode {
				case "login":
					body = `{"errcode":9,"success":false}`
				case "failure", "repeated":
					body = strings.Replace(body, `"isLoadMore":false`, `"isLoadMore":true,"pageTimestamp":"100"`, 1)
				case "cursor":
					body = strings.Replace(body, `"time_stamp":100,`, ``, 1)
					body = strings.Replace(body, `"isLoadMore":false`, `"isLoadMore":true`, 1)
				case "missing-more":
					body = strings.Replace(body, `"isLoadMore":false`, `"pageTimestamp":"100"`, 1)
				case "missing-pagination":
					body = strings.Replace(body, `,"pagination":{"isLoadMore":false}`, ``, 1)
				case "invalid-image":
					body = strings.Replace(body, `"https://xcimg.szwego.com/img/1.jpg"`, `"https://xcimg.szwego.com/img/1.jpg","https://evil.test/2.jpg"`, 1)
				case "malformed-image":
					body = strings.Replace(body, `"https://xcimg.szwego.com/img/1.jpg"`, `"https://xcimg.szwego.com/img/1.jpg",123`, 1)
				case "malformed-item":
					body = strings.Replace(body, `}],"pagination"`, `},{"goods_id":"I2","imgsSrc":123}],"pagination"`, 1)
				case "video":
					body = strings.Replace(body, `"goods_id":"I1"`, `"goods_id":"I1","videoUrl":"https://example.test/movie.mp4"`, 1)
				case "restricted":
					body = strings.Replace(body, `"items":`, `"targetAlbum":{"attentionVerify":true},"album":{"isTourist":true},"items":`, 1)
				}
				resp := weishangResponse(raw, body)
				if mode == "redirect" {
					resp.FinalURL = "https://evil.test/album/personal/all"
				}
				return resp, nil
			}}
			got, err := discoverWeishang(ctx, getter, page)
			want := 1
			if mode == "login" || mode == "restricted" || mode == "cancel" || mode == "redirect" {
				want = 0
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s items=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
	filtered, _ := url.Parse(weishangTestBase + "#/shop_detail/A202006012055527750026253?tagId=123")
	getter := weishangTestPoster{post: func(_ context.Context, _ string, _ []byte, _ http.Header) (*Response, error) {
		t.Fatal("filtered album should not request unfiltered items")
		return nil, nil
	}}
	if _, err := discoverWeishang(context.Background(), getter, filtered); err == nil {
		t.Fatal("filter was ignored")
	}
}

func TestWeishangProduct(t *testing.T) {
	page, _ := url.Parse(weishangTestPage)
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequest("GET", raw, nil)
		prepare(req)
		if req.URL.Host != page.Host || req.URL.Path != "/commodity/view" || req.URL.Query().Get("targetAlbumId") != "A202006012055527750026253" || req.URL.Query().Get("itemId") != "I202101041557160310557566" || req.Header.Get("Referer") != "https://"+page.Host+"/" || req.Header.Get("Cookie") != "" {
			t.Fatalf("unsafe request: %s %v", req.URL, req.Header)
		}
		return weishangResponse(raw, `{"errcode":0,"result":{"commodity":{"title":"作品","imgsSrc":["https://xcimg.szwego.com/img/1.jpg?signature=1","https://qnimg.szwego.com/img/2.jpg","https://qnimg.szwego.com/img/2.jpg"]},"recommendation":{"imgsSrc":["https://xcimg.szwego.com/img/other.jpg"]}}}`), nil
	})
	got, err := discoverWeishangProduct(context.Background(), getter, page)
	if err != nil || got.Title != "作品" || len(got.Items) != 2 || got.Items[0].URL != "https://xcimg.szwego.com/img/1.jpg?signature=1" || got.Items[1].URL != "https://qnimg.szwego.com/img/2.jpg" {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Headers["Referer"] != "https://"+page.Host+"/" || item.Headers["Cookie"] != "" || item.Metadata["weishang_item_id"] != "I202101041557160310557566" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestWeishangDiscoverFragment(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Path != "/commodity/view" || u.Query().Get("itemId") != "I202101041557160310557566" {
			t.Fatalf("fragment lost before dispatch: %s", raw)
		}
		return weishangResponse(raw, `{"errcode":0,"result":{"commodity":{"imgsSrc":["https://xcimg.szwego.com/img/1.jpg"]}}}`), nil
	})
	got, err := Discover(context.Background(), getter, weishangTestPage, 1)
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestWeishangProductFailuresAndPartial(t *testing.T) {
	page, _ := url.Parse(weishangTestPage)
	for _, mode := range []string{"login", "invalid", "partial", "malformed-image", "video", "redirect", "other-item", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				body := `{"errcode":0,"result":{"commodity":{"imgsSrc":["https://xcimg.szwego.com/img/1.jpg"]}}}`
				switch mode {
				case "login":
					body = `{"errcode":9,"errmsg":"登录已过期","success":false}`
				case "invalid":
					body = `{invalid`
				case "partial":
					body = strings.Replace(body, `]`, `,"https://evil.test/img/2.jpg"]`, 1)
				case "malformed-image":
					body = strings.Replace(body, `]`, `,123]`, 1)
				case "video":
					body = strings.Replace(body, `"imgsSrc":`, `"videoUrl":"https://example.test/a.mp4","imgsSrc":`, 1)
				case "cancel":
					cancel()
				}
				resp := weishangResponse(raw, body)
				if mode == "redirect" {
					resp.FinalURL = "https://evil.test/commodity/view"
				}
				if mode == "other-item" {
					resp.FinalURL = raw + "&itemId=other"
				}
				return resp, nil
			})
			got, err := discoverWeishangProduct(ctx, getter, page)
			want := 0
			if mode == "partial" || mode == "malformed-image" || mode == "video" {
				want = 1
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestWeishangRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `<html></html>`) }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const page = `<html><title>product</title><img src="https://xcimg.szwego.com/img/recommended.jpg"></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == weishangTestBase {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(page)))
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
	session.start, _ = url.Parse(weishangTestPage)
	if err := session.run(ctx, camoufox.Navigate(weishangTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL(weishangTestBase).WithPath("/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = weishangRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		body, ct := "synthetic image", "image/jpeg"
		if r.URL.Host == "a202006012055527750026253.szwego.com" && r.URL.Path == "/commodity/view" {
			if !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") || r.URL.Query().Get("itemId") != "I202101041557160310557566" {
				t.Errorf("wrong same-origin API request %s %v", r.URL, r.Header)
			}
			body, ct = `{"errcode":0,"result":{"commodity":{"imgsSrc":["https://xcimg.szwego.com/img/1.jpg"]}}}`, "application/json"
		} else if r.URL.Host == "a202006012055527750026253.szwego.com" && r.URL.Path == "/album/personal/all" {
			if r.Method != http.MethodPost || !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") || r.Header.Get("Origin") != "https://a202006012055527750026253.szwego.com" || r.URL.Query().Get("albumId") != "A202006012055527750026253" {
				t.Errorf("wrong same-origin album POST %s %v", r.URL, r.Header)
			}
			body, ct = `{"errcode":0,"result":{"items":[{"goods_id":"I2","imgsSrc":["https://xcimg.szwego.com/img/2.jpg"]}],"pagination":{"isLoadMore":false}}}`, "application/json"
		} else if r.URL.Host != "xcimg.szwego.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("unsafe CDN request %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://xcimg.szwego.com/img/1.jpg" {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	albumURL := weishangTestBase + "#/shop_detail/A202006012055527750026253"
	if err := session.run(ctx, camoufox.Navigate(albumURL)); err != nil {
		t.Fatal(err)
	}
	album, err := session.Capture(ctx)
	if err != nil || len(album.Items) != 1 || album.Items[0].URL != "https://xcimg.szwego.com/img/2.jpg" {
		t.Fatalf("album=%+v page=%s err=%v", album, session.PageURL(), err)
	}
	if calls != 3 {
		t.Fatalf("requests=%d", calls)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestWeishangLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_WEISHANG_LIVE") != "1" {
		t.Skip("set TULAI_WEISHANG_LIVE=1 for a public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := url.Parse(weishangTestPage)
	getter := NewHTTPGetter("")
	got, err := discoverWeishangProduct(ctx, getter, page)
	if err != nil && strings.Contains(err.Error(), "要求登录或授权") {
		t.Skipf("public product requires login: %v", err)
	}
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("items=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("empty image: %v %v", info, err)
	}
	t.Logf("public product images=%d", len(got.Items))
}

func TestWeishangLiveAlbum(t *testing.T) {
	if os.Getenv("TULAI_WEISHANG_LIVE") != "1" {
		t.Skip("set TULAI_WEISHANG_LIVE=1 for a public album")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := url.Parse("https://a2017090616080725510.szwego.com/static/index.html#/shop_detail/A2017090616080725510")
	got, err := discoverWeishang(ctx, NewHTTPGetter(""), page)
	if err != nil && len(got.Items) == 0 && strings.Contains(err.Error(), "要求登录或授权") {
		t.Skipf("public album requires login: %v", err)
	}
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("items=%d err=%v", len(got.Items), err)
	}
	t.Logf("public album images=%d", len(got.Items))
}
