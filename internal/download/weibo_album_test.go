package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

func TestWeiboContentTarget(t *testing.T) {
	for raw, want := range map[string]string{
		"https://weibo.com/1737755501/PAAEFz9wY":                            "post",
		"https://weibo.com/detail/12345":                                    "post",
		"https://m.weibo.cn/status/PAAEFz9wY":                               "post",
		"https://weibo.com/u/1737755501?tabtype=album":                      "albums",
		"https://weibo.com/1737755501/album":                                "albums",
		"https://photo.weibo.com/123/albums":                                "old-albums",
		"https://photo.weibo.com/123/albums/detail/album_id/456":            "old-album",
		"https://photo.weibo.com/123/albums/detail/album_id/456?type=3":     "old-album",
		"https://photo.weibo.com/123/albums/detail/album_id/456?type=bogus": "",
		"https://user:pass@weibo.com/detail/123":                            "",
		"https://weibo.com:444/detail/123":                                  "",
		"https://weibo.com.evil.test/detail/123":                            "",
		"https://127.0.0.1/detail/123":                                      "",
		"https://weibo.com/search/test":                                     "",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := weiboContentTarget(u).kind; got != want {
			t.Errorf("weiboContentTarget(%q) = %q, want %q", raw, got, want)
		}
	}
}

func weiboFakeGetter(t *testing.T, host string, handler func(context.Context, *http.Request) (int, string, string)) Getter {
	t.Helper()
	return GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if req.URL.Host != host || req.URL.Scheme != "https" {
			t.Fatalf("cross-origin API request: %q", raw)
		}
		req.Header.Set("Cookie", "synthetic=value")
		prepare(req)
		if req.Header.Get("Cookie") != "synthetic=value" || req.Header.Get("Referer") != "https://"+host+"/" || req.Header.Get("X-XSRF-TOKEN") != "" {
			t.Fatalf("unexpected API headers: %v", req.Header)
		}
		status, body, final := handler(ctx, req)
		if final == "" {
			final = raw
		}
		return &Response{StatusCode: status, Header: http.Header{}, FinalURL: final, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
}

func TestWeiboPostOriginalsAndUnsafeImages(t *testing.T) {
	page, _ := url.Parse("https://weibo.com/123/AbC123?token=private")
	getter := weiboFakeGetter(t, "weibo.com", func(ctx context.Context, req *http.Request) (int, string, string) {
		if req.URL.Path != "/ajax/statuses/show" || req.URL.Query().Get("id") != "AbC123" || req.URL.Query().Has("token") {
			t.Fatalf("request: %v", req.URL)
		}
		return 200, `{"mblogid":"AbC123","text_raw":"图片帖子","pic_ids":["a","b","c","d","e"],"pic_infos":{"a":{"original":{"url":"https://wx1.sinaimg.cn/large/a.jpg"}},"b":{"bmiddle":{"url":"//wx2.sinaimg.cn/bmiddle/b.jpg"}},"c":{"original":{"url":"http://127.0.0.1/large/c.jpg"}},"d":{"original":{"url":"https://sinaimg.cn.evil.test/large/d.jpg"}},"e":{"original":{"url":"https://user:password@wx1.sinaimg.cn:444/large/e.jpg"}}},"mix_media_info":{"items":[{"type":"pic","data":{"pic_info":{"original":{"url":"https://wx3.sinaimg.cn/large/f.jpg"}}}},{"type":"video","data":{"url":"https://wx1.sinaimg.cn/large/video.jpg"}}]},"retweeted_status":{"pic_infos":{"f":{"original":{"url":"https://wx4.sinaimg.cn/large/g.jpg"}}}}}`, ""
	})
	got, err := discoverWeiboContent(context.Background(), getter, page)
	if err == nil || !got.IsPage || got.Title != "图片帖子" || len(got.Items) != 4 {
		t.Fatalf("post = %+v, err=%v", got, err)
	}
	for i, want := range []string{"https://wx1.sinaimg.cn/large/a.jpg", "https://wx2.sinaimg.cn/large/b.jpg", "https://wx3.sinaimg.cn/large/f.jpg", "https://wx4.sinaimg.cn/large/g.jpg"} {
		if got.Items[i].URL != want || got.Items[i].Headers["Referer"] != "https://weibo.com/" || len(got.Items[i].Headers) != 1 {
			t.Errorf("image %d = %+v, want %q", i, got.Items[i], want)
		}
	}
	if strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "token") {
		t.Fatalf("secret in error: %v", err)
	}
}

func TestWeiboPostLoginIdentityAndMobile(t *testing.T) {
	for _, tc := range []struct {
		name, root  string
		status      int
		body, final string
		wantErr     bool
	}{
		{"forbidden", "https://weibo.com/1/abc", 403, "forbidden", "", true},
		{"login redirect", "https://weibo.com/1/abc", 200, "{}", "https://passport.weibo.com/visitor/visitor", true},
		{"wrong post", "https://weibo.com/1/abc", 200, `{"mblogid":"different","pic_infos":{"a":{"original":{"url":"https://wx1.sinaimg.cn/large/a.jpg"}}}}`, "", true},
		{"mobile unverified", "https://m.weibo.cn/status/abc", 200, `{}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, _ := url.Parse(tc.root)
			getter := weiboFakeGetter(t, page.Host, func(ctx context.Context, req *http.Request) (int, string, string) {
				if page.Host == "m.weibo.cn" {
					t.Fatal("unverified mobile endpoint must not be requested")
				}
				return tc.status, tc.body, tc.final
			})
			got, err := discoverWeiboContent(context.Background(), getter, page)
			if (err != nil) != tc.wantErr || (tc.wantErr && len(got.Items) != 0) || (!tc.wantErr && len(got.Items) != 1) {
				t.Fatalf("got %+v, err=%v", got, err)
			}
		})
	}
}

func TestWeiboImageWallAndAlbumPagination(t *testing.T) {
	page, _ := url.Parse("https://weibo.com/u/123?tabtype=album&secret=keep-private")
	requests := []string{}
	getter := weiboFakeGetter(t, "weibo.com", func(ctx context.Context, req *http.Request) (int, string, string) {
		requests = append(requests, req.URL.Path+"?"+req.URL.RawQuery)
		switch req.URL.Path {
		case "/ajax/profile/getImageWall":
			if req.URL.Query().Get("sinceid") == "0" {
				return 200, `{"ok":1,"data":{"album_list":[{"containerid":"2301_album1","pic_title":"一"}],"list":[{"pid":"wall"}],"since_id":"cursor1"}}`, ""
			}
			return 200, `{"ok":1,"data":{"list":[{"pid":"wall2"}],"since_id":"0"}}`, ""
		case "/ajax/profile/getAlbumDetail":
			if req.URL.Query().Get("containerid") != "2301_album1" {
				t.Fatalf("container: %s", req.URL)
			}
			if req.URL.Query().Get("since_id") == "0" {
				return 200, `{"ok":1,"data":{"list":[{"pid":"photo1"},{"mid":"123456"}],"since_id":"next"}}`, ""
			}
			return 200, `{"ok":1,"data":{"list":[{"pid":"photo2"}],"since_id":0}}`, ""
		case "/ajax/statuses/show":
			return 200, `{"idstr":"123456","pic_infos":{"x":{"original":{"url":"https://wx2.sinaimg.cn/large/from-post.jpg"}}}}`, ""
		default:
			t.Fatalf("unexpected endpoint %q", req.URL)
			return 500, "", ""
		}
	})
	got, err := discoverWeiboContent(context.Background(), getter, page)
	if err != nil || len(got.Items) != 5 || len(requests) != 5 {
		t.Fatalf("got %+v, requests=%v err=%v", got.Items, requests, err)
	}
	for _, req := range requests {
		if strings.Contains(req, "secret") {
			t.Fatalf("private root query forwarded: %q", req)
		}
	}
	for i, want := range []string{"photo1.jpg", "from-post.jpg", "photo2.jpg", "wall.jpg", "wall2.jpg"} {
		if !strings.HasSuffix(got.Items[i].URL, want) {
			t.Errorf("image %d: %+v", i, got.Items[i])
		}
	}
}

func TestWeiboAlbumPartialAndCancellation(t *testing.T) {
	for _, mode := range []string{"error", "repeat", "missing-cursor", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			page, _ := url.Parse("https://weibo.com/123/album")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := weiboFakeGetter(t, "weibo.com", func(ctx context.Context, req *http.Request) (int, string, string) {
				cursor := req.URL.Query().Get("sinceid")
				if cursor == "0" {
					if mode == "missing-cursor" {
						return 200, `{"data":{"album_list":[],"list":[{"pid":"one"}]}}`, ""
					}
					return 200, `{"data":{"album_list":[],"list":[{"pid":"one"}],"since_id":"next"}}`, ""
				}
				switch mode {
				case "error":
					return 403, "forbidden", ""
				case "repeat":
					return 200, `{"data":{"list":[{"pid":"two"}],"since_id":"next"}}`, ""
				case "cancel":
					cancel()
					return 200, `{}`, ""
				}
				return 500, "", ""
			})
			got, err := discoverWeiboContent(ctx, getter, page)
			if err == nil || len(got.Items) == 0 || !strings.HasSuffix(got.Items[0].URL, "/one.jpg") {
				t.Fatalf("partial=%+v err=%v", got.Items, err)
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel error = %v", err)
			}
		})
	}
}

func TestWeiboLegacyAlbumsJSONPAndPartial(t *testing.T) {
	page, _ := url.Parse("https://photo.weibo.com/123/albums/detail/album_id/456")
	getter := weiboFakeGetter(t, "photo.weibo.com", func(ctx context.Context, req *http.Request) (int, string, string) {
		switch req.URL.Path {
		case "/albums/get_all":
			return 200, `var callback = {"data":{"album_list":[{"album_id":"456","type":3,"count":{"photos":3}}]}};`, ""
		case "/photos/get_all":
			if req.URL.Query().Get("album_id") != "456" || req.URL.Query().Get("type") != "3" {
				t.Fatalf("wrong album: %v", req.URL)
			}
			if req.URL.Query().Get("page") == "1" {
				return 200, `cb({"data":{"photo_list":[{"pic_host":"https://wx1.sinaimg.cn","pic_name":"one.jpg"},{"pic_host":"https://127.0.0.1","pic_name":"bad.jpg"}]}});`, ""
			}
			return 200, `{"data":{"photo_list":[]}}`, ""
		default:
			t.Fatal("unexpected endpoint", req.URL)
			return 500, "", ""
		}
	})
	got, err := discoverWeiboContent(context.Background(), getter, page)
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != "https://wx1.sinaimg.cn/large/one.jpg" || !strings.Contains(err.Error(), "提前结束") {
		t.Fatalf("legacy result = %+v err=%v", got.Items, err)
	}
}

func TestWeiboResponseBoundedAndJSONP(t *testing.T) {
	for _, input := range []string{`alert(1);`, `a();evil({});`, `var x = {"a":1}; evil()`, `{"a":1} trailing`} {
		var out any
		if weiboDecode([]byte(input), &out) == nil {
			t.Errorf("accepted executable wrapper %q", input)
		}
	}
	page, _ := url.Parse("https://weibo.com/1/abc")
	getter := weiboFakeGetter(t, "weibo.com", func(context.Context, *http.Request) (int, string, string) {
		return 200, fmt.Sprintf(`{"idstr":"abc","text_raw":%q}`, strings.Repeat("x", maxPageBytes)), ""
	})
	if _, err := discoverWeiboContent(context.Background(), getter, page); err == nil || !strings.Contains(err.Error(), "8 MiB") {
		t.Fatalf("oversized response: %v", err)
	}
}

func TestWeiboRowLimitRetainsResults(t *testing.T) {
	page, _ := url.Parse("https://weibo.com/123/album")
	c := weiboCollector{ctx: context.Background(), source: page, seen: map[string]bool{}, rows: maxWeiboRows - 1}
	err := c.albumRows([]json.RawMessage{json.RawMessage(`{"pid":"one"}`), json.RawMessage(`{"pid":"two"}`)})
	if !errors.Is(err, errWeiboRowLimit) || len(c.result.Items) != 1 || !strings.HasSuffix(c.result.Items[0].URL, "/one.jpg") {
		t.Fatalf("limit result=%+v err=%v", c.result.Items, err)
	}
}

type weiboTestTransport func(*http.Request) (*http.Response, error)

func (f weiboTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWeiboRenderedCaptureSameOrigin(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html>placeholder</html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const target = "https://weibo.com/123/AbC123?synthetic=token"
	const document = `<html><title>微博</title><img src="https://wx1.sinaimg.cn/large/recommendation.jpg"></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(document)))
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
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://weibo.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	session.start, _ = url.Parse(target)
	requests := 0
	session.getter.client.Transport = weiboTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.Host != "weibo.com" || r.URL.Path != "/ajax/statuses/show" || r.URL.Query().Get("id") != "AbC123" || r.Header.Get("Cookie") != "synthetic_session=example" || r.Header.Get("X-XSRF-TOKEN") != "" {
			t.Errorf("unexpected synthetic request %s headers=%v", r.URL, r.Header)
		}
		body := `{"mblogid":"AbC123","text_raw":"当前帖子","pic_infos":{"x":{"original":{"url":"https://wx1.sinaimg.cn/large/current.jpg"}}}}`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || !got.IsPage || requests != 1 || got.Title != "当前帖子" || len(got.Items) != 1 || got.Items[0].URL != "https://wx1.sinaimg.cn/large/current.jpg" || got.Items[0].Headers["Referer"] != "https://weibo.com/" {
		t.Fatalf("rendered post=%+v requests=%d err=%v", got, requests, err)
	}
	select {
	case err := <-responses:
		t.Fatal(err)
	default:
	}
}
