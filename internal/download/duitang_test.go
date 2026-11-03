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

type duitangRoundTrip func(*http.Request) (*http.Response, error)

func (fn duitangRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func duitangResponse(raw, body, contentType string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDuitangRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, kind, id string }{
		{"https://www.duitang.com/album/?id=123", "album", "123"},
		{"http://duitang.com/blog/?id=456", "blog", "456"},
		{"https://www.duitang.com/blog/?id=0", "", ""},
		{"https://www.duitang.com/blog/?id=1&id=2", "", ""},
		{"https://www.duitang.com/album/123", "", ""},
		{"https://www.duitang.com.evil.test/album/?id=123", "", ""},
		{"https://user@www.duitang.com/album/?id=123", "", ""},
		{"https://www.duitang.com:8443/album/?id=123", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := duitangRouteOf(u); got.kind != tc.kind || got.id != tc.id {
			t.Errorf("route %s = %+v", tc.raw, got)
		}
	}
	page, _ := url.Parse("https://www.duitang.com/blog/?id=456")
	for _, tc := range []struct{ raw, want string }{
		{"https://a-ho-img.dtstatic.com/uploads/item/a.jpg?k=sig", "https://a-ho-img.dtstatic.com/uploads/item/a.jpg?k=sig"},
		{"http://c-ssl.duitang.com/uploads/blog/b.jpg", "https://c-ssl.duitang.com/uploads/blog/b.jpg"},
		{"https://a-ho-img.dtstatic.com.evil.test/uploads/item/a.jpg", ""},
		{"https://user@a-ho-img.dtstatic.com/uploads/item/a.jpg", ""},
		{"https://a-ho-img.dtstatic.com:8443/uploads/item/a.jpg", ""},
		{"https://a-ho-img.dtstatic.com/uploads/../private", ""},
		{"https://a-ho-img.dtstatic.com/other/a.jpg", ""},
	} {
		got, ok := duitangImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q %t", tc.raw, got, ok)
		}
	}
}

func TestDuitangPost(t *testing.T) {
	root, _ := url.Parse("https://www.duitang.com/blog/?id=456&private=source")
	body := `<html><title>作品</title><div id="detailcnt"><a class="vieworg" href="https://a-ho-img.dtstatic.com/uploads/item/a.jpg?k=sig"></a></div><a class="vieworg" href="https://a-ho-img.dtstatic.com/uploads/item/recommended.jpg"></a></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return duitangResponse(raw, body, "text/html"), nil
	})
	got, err := discoverDuitang(context.Background(), getter, root)
	if err != nil || len(got.Items) != 1 || got.Title != "作品" || got.Items[0].URL != "https://a-ho-img.dtstatic.com/uploads/item/a.jpg?k=sig" || got.Items[0].Headers["Referer"] != "https://www.duitang.com/" || got.Items[0].Metadata["duitang_post_id"] != "456" || !got.Items[0].RequireImage {
		t.Fatalf("post=%+v err=%v", got, err)
	}
}

func TestDuitangAlbumPagination(t *testing.T) {
	root, _ := url.Parse("https://www.duitang.com/album/?id=123&private=source")
	var requests int
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequest("GET", raw, nil)
		prepare(req)
		if req.URL.Path == "/album/" {
			return duitangResponse(raw, `<title>专辑</title><span class="album-count">3张图片</span><img src="https://a-ho-img.dtstatic.com/uploads/item/recommended.jpg">`, "text/html"), nil
		}
		if req.URL.Host != "www.duitang.com" || req.URL.Path != "/napi/vienna/blog/by_album/" || req.URL.Query().Get("album_id") != "123" || req.URL.Query().Get("limit") != "24" || req.Header.Get("Referer") != "https://www.duitang.com/" || req.Header.Get("Cookie") != "" {
			t.Fatalf("unsafe request %s %v", req.URL, req.Header)
		}
		requests++
		if req.URL.Query().Get("after_id") == "0" {
			return duitangResponse(raw, `{"status":1,"data":{"more":1,"after":"[100]","object_list":[{"id":1,"photo":{"path":"https://a-ho-img.dtstatic.com/uploads/item/1.jpg?k=sig"}},{"id":2,"photo":{"path":"https://c-ssl.duitang.com/uploads/item/2.jpg"}}]}}`, "application/json"), nil
		}
		if req.URL.Query().Get("after_id") != "[100]" {
			t.Fatalf("cursor=%s", req.URL.RawQuery)
		}
		return duitangResponse(raw, `{"status":1,"data":{"more":0,"after":"","object_list":[{"id":2,"photo":{"path":"https://c-ssl.duitang.com/uploads/item/2.jpg"}},{"id":"3","photo":{"path":"https://a-ho-img.dtstatic.com/uploads/blog/3.jpg"}}]}}`, "application/json"), nil
	})
	got, err := discoverDuitang(context.Background(), getter, root)
	if err != nil || requests != 2 || len(got.Items) != 3 || got.Items[0].URL != "https://a-ho-img.dtstatic.com/uploads/item/1.jpg?k=sig" || got.Items[2].Metadata["duitang_post_id"] != "3" || got.Items[0].Metadata["album_id"] != "123" {
		t.Fatalf("album=%+v requests=%d err=%v", got, requests, err)
	}
}

func TestDuitangPartialAndSafety(t *testing.T) {
	root, _ := url.Parse("https://www.duitang.com/album/?id=123")
	for _, mode := range []string{"count", "cursor", "repeated-cursor", "invalid-image", "error", "redirect", "duplicate-query", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Path == "/album/" {
					return duitangResponse(raw, `<span class="album-count">2张图片</span>`, "text/html"), nil
				}
				if mode == "cancel" {
					cancel()
					return nil, context.Canceled
				}
				if mode == "error" {
					return nil, errors.New("temporary failure")
				}
				body := `{"status":1,"data":{"more":0,"after":"","object_list":[{"id":1,"photo":{"path":"https://a-ho-img.dtstatic.com/uploads/item/1.jpg"}}]}}`
				if mode == "cursor" {
					body = strings.Replace(body, `"more":0,"after":""`, `"more":1,"after":"bad"`, 1)
				}
				if mode == "repeated-cursor" {
					body = strings.Replace(body, `"more":0,"after":""`, `"more":1,"after":"[100]"`, 1)
				}
				if mode == "invalid-image" {
					body = strings.Replace(body, `]}}`, `,{"id":2,"photo":{"path":"https://evil.test/uploads/item/2.jpg"}}]}}`, 1)
				}
				resp := duitangResponse(raw, body, "application/json")
				if mode == "redirect" {
					resp.FinalURL = "https://evil.test/napi/vienna/blog/by_album/?album_id=123&after_id=0"
				}
				if mode == "duplicate-query" {
					resp.FinalURL = raw + "&album_id=999"
				}
				return resp, nil
			})
			got, err := discoverDuitang(ctx, getter, root)
			want := 1
			if mode == "error" || mode == "redirect" || mode == "duplicate-query" || mode == "cancel" {
				want = 0
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestDuitangRenderedCapture(t *testing.T) {
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
	const target = "https://www.duitang.com/album/?id=123"
	const page = `<html><title>album</title><span class="album-count">1张图片</span><img src="https://a-ho-img.dtstatic.com/uploads/item/recommended.jpg"></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
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
	session.start, _ = url.Parse(target)
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.duitang.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = duitangRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		body, ct := "synthetic image", "image/jpeg"
		if r.URL.Host == "www.duitang.com" && r.URL.Path == "/napi/vienna/blog/by_album/" {
			if !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") {
				t.Errorf("missing synthetic API cookie")
			}
			body, ct = `{"status":1,"data":{"more":0,"after":"","object_list":[{"id":1,"photo":{"path":"https://a-ho-img.dtstatic.com/uploads/item/1.jpg"}}]}}`, "application/json"
		} else if r.URL.Host != "a-ho-img.dtstatic.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("unsafe CDN request %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || session.PageURL() != target || len(got.Items) != 1 || got.Items[0].URL != "https://a-ho-img.dtstatic.com/uploads/item/1.jpg" {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 2 {
		t.Fatalf("requests=%d", calls)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestDuitangLive(t *testing.T) {
	if os.Getenv("TULAI_DUITANG_LIVE") != "1" {
		t.Skip("set TULAI_DUITANG_LIVE=1 for public Duitang checks")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	post, _ := url.Parse("https://www.duitang.com/blog/?id=1137325349")
	postResult, err := discoverDuitang(ctx, getter, post)
	if err != nil || len(postResult.Items) != 1 {
		t.Fatalf("post items=%d err=%v", len(postResult.Items), err)
	}
	album, _ := url.Parse("https://www.duitang.com/album/?id=86215915")
	got, err := discoverDuitang(ctx, getter, album)
	if len(got.Items) < 100 || (err != nil && (!strings.Contains(err.Error(), "接口返回") || strings.Contains(err.Error(), "\n"))) {
		t.Fatalf("album items=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	err = New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil)
	entries, readErr := os.ReadDir(dir)
	if err != nil || readErr != nil || len(entries) != 1 {
		t.Fatalf("download err=%v entries=%v read=%v", err, entries, readErr)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("empty image: %v %v", info, err)
	}
	t.Logf("public album images=%d", len(got.Items))
}
