package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/cookiejar"
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
	weidianTestID   = "7399591875"
	weidianTestRoot = "https://weidian.com/item.html?itemID=7399591875"
	weidianTestShop = "https://shop1831323721.v.weidian.com/item.html?itemID=7399591875"
)

type weidianRoundTrip func(*http.Request) (*http.Response, error)

func (f weidianRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func weidianHTTPResponse(r *http.Request, code int, body, contentType string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

const weidianTestHTML = `<html><script data-obj='{"status":{"code":0},"result":{"default_model":{"item_info":{"item_id":"7399591875","item_name":"测试商品","imgs":["https://si.geilicdn.com/main.jpg"]},"sku_properties":{"attr_list":[{"attr_values":[{"attr_value":"红色","img":"https://si.geilicdn.com/sku-red.jpg"}]}],"sku":{"12":{"title":"蓝色","img":"https://si.geilicdn.com/sku-blue.jpg"}}}}}}'></script><img class="item-img" data-src="https://si.geilicdn.com/main.jpg.webp?w=100&h=100&cp=1"><div class="d-content"><img class="d-image" data-src="https://si.geilicdn.com/detail.png.webp?w=300&h=400"></div><img src="https://si.geilicdn.com/recommended.jpg"></html>`

const weidianTestDetail = `{"status":{"code":0},"result":{"item_detail":{"desc_content":[{"type":2,"url":"https://si.geilicdn.com/detail.png"},{"type":1,"text":"介绍"}]}}}`

func TestWeidianRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{weidianTestRoot, weidianTestID},
		{weidianTestShop, weidianTestID},
		{"http://weidian.com/item.html?itemId=7399591875", weidianTestID},
		{"https://weidian.com/item.html?itemID=7399591875&itemId=7399591875", ""},
		{"https://weidian.com/item.html?itemID=&itemId=7399591875", ""},
		{"https://weidian.com/item.html?itemID=0000000000", ""},
		{"https://weidian.com/item.html?itemID=123x", ""},
		{"https://weidian.com.evil.test/item.html?itemID=7399591875", ""},
		{"https://user@weidian.com/item.html?itemID=7399591875", ""},
		{"https://weidian.com:8443/item.html?itemID=7399591875", ""},
		{"https://h5.weidian.com/m/abnormal/404.html?itemID=7399591875", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := weidianProductID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(weidianTestShop)
	for _, tc := range []struct{ raw, want, full string }{
		{"//si.geilicdn.com/a.jpg.webp?w=100&h=100&cp=1", "https://si.geilicdn.com/a.jpg.webp?w=100&h=100&cp=1", "https://si.geilicdn.com/a.jpg"},
		{"https://si.geilicdn.com/a.png.webp", "https://si.geilicdn.com/a.png.webp", "https://si.geilicdn.com/a.png"},
		{"https://si.geilicdn.com/a.jpg.webp?token=keep", "https://si.geilicdn.com/a.jpg.webp?token=keep", "https://si.geilicdn.com/a.jpg.webp?token=keep"},
		{"https://si.geilicdn.com/a.jpg?w=100&sig=x", "https://si.geilicdn.com/a.jpg?w=100&sig=x", "https://si.geilicdn.com/a.jpg?w=100&sig=x"},
		{"https://si.geilicdn.com.evil.test/a.jpg", "", ""},
		{"https://user@si.geilicdn.com/a.jpg", "", ""},
		{"https://si.geilicdn.com:8443/a.jpg", "", ""},
		{"https://si.geilicdn.com/path/../a.jpg", "", ""},
		{"data:image/png;base64,AA", "", ""},
	} {
		got, ok := weidianImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") || weidianFullImage(got) != tc.full {
			t.Errorf("image %s = %q full=%q ok=%t", tc.raw, got, weidianFullImage(got), ok)
		}
	}
}

func TestWeidianProductAndDetails(t *testing.T) {
	for _, mode := range []string{"success", "detail-redirect", "detail-status", "detail-malformed", "bad-redirect", "wrong-id", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := NewHTTPGetter("")
			jar, _ := cookiejar.New(nil)
			for _, raw := range []string{weidianTestRoot, weidianTestShop, "https://thor.weidian.com/"} {
				u, _ := url.Parse(raw)
				jar.SetCookies(u, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			}
			getter.client.Jar = jar
			var calls int
			getter.client.Transport = weidianRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected credentials sent to %s", r.URL.Host)
				}
				switch r.URL.Host {
				case "weidian.com":
					resp := weidianHTTPResponse(r, 302, "", "text/html")
					resp.Header.Set("Location", weidianTestShop)
					if mode == "bad-redirect" {
						resp.Header.Set("Location", "https://evil.test/item.html?itemID=7399591875")
					}
					return resp, nil
				case "shop1831323721.v.weidian.com":
					body := weidianTestHTML
					if mode == "wrong-id" {
						body = strings.Replace(body, `"item_id":"7399591875"`, `"item_id":"1111111111"`, 1)
					} else if mode == "cancel" {
						cancel()
					}
					return weidianHTTPResponse(r, 200, body, "text/html"), nil
				case "thor.weidian.com":
					if r.URL.Scheme != "https" || r.URL.Path != "/detail/getDetailDesc/1.0" || len(r.URL.Query()) != 1 {
						t.Errorf("unexpected detail endpoint %s", r.URL)
					}
					var param struct {
						ID        string `json:"vItemId"`
						Translate bool   `json:"needTranslate"`
					}
					if json.Unmarshal([]byte(r.URL.Query().Get("param")), &param) != nil || param.ID != weidianTestID || !param.Translate {
						t.Errorf("unexpected detail parameters %+v", param)
					}
					if mode == "detail-redirect" {
						resp := weidianHTTPResponse(r, 302, "", "text/html")
						resp.Header.Set("Location", "https://evil.test/steal")
						return resp, nil
					}
					if mode == "detail-status" {
						return weidianHTTPResponse(r, 403, "denied", "text/html"), nil
					}
					if mode == "detail-malformed" {
						return weidianHTTPResponse(r, 200, `{}`, "application/json"), nil
					}
					return weidianHTTPResponse(r, 200, weidianTestDetail, "application/json"), nil
				default:
					t.Errorf("unexpected request %s", r.URL)
					return weidianHTTPResponse(r, 500, "", "text/html"), nil
				}
			})
			page, _ := url.Parse(weidianTestRoot)
			got, err := discoverWeidianProduct(ctx, getter, page)
			switch mode {
			case "success":
				if err != nil || calls != 3 || got.Title != "测试商品" || len(got.Items) != 4 || got.Items[0].Metadata["image_kind"] != "main" || got.Items[1].Metadata["variant"] != "红色" || got.Items[2].Metadata["variant"] != "蓝色" || got.Items[3].Metadata["image_kind"] != "description" {
					t.Fatalf("product=%+v calls=%d err=%v", got, calls, err)
				}
				if len(got.Items[3].AlternateURLs) != 1 || got.Items[3].AlternateURLs[0] != "https://si.geilicdn.com/detail.png.webp?w=300&h=400" {
					t.Fatalf("fallback=%+v", got.Items[3])
				}
			case "detail-redirect", "detail-status", "detail-malformed":
				if err == nil || !strings.Contains(err.Error(), "详情") || len(got.Items) != 4 || calls != 3 {
					t.Fatalf("partial=%+v calls=%d err=%v", got, calls, err)
				}
			case "wrong-id":
				if err == nil || len(got.Items) != 0 || calls != 2 {
					t.Fatalf("mismatch=%+v calls=%d err=%v", got, calls, err)
				}
			case "bad-redirect":
				if err == nil || len(got.Items) != 0 || calls != 1 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || calls != 2 {
					t.Fatalf("cancel=%+v calls=%d err=%v", got, calls, err)
				}
			}
		})
	}
}

func TestWeidianPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(weidianTestShop)
	for _, mode := range []string{"missing-sku", "unsafe-image", "bad-json", "no-state"} {
		t.Run(mode, func(t *testing.T) {
			body := weidianTestHTML
			switch mode {
			case "missing-sku":
				body = `<script data-obj='{"status":{"code":0},"result":{"default_model":{"item_info":{"item_id":"7399591875","imgs":["https://si.geilicdn.com/main.jpg"]}}}}'></script>`
			case "unsafe-image":
				body = strings.Replace(body, `</div><img src=`, `<img class="d-image" data-src="https://evil.test/a.jpg"></div><img src=`, 1)
			case "bad-json":
				body = `<script data-obj='{"status":'></script><img class="item-img" data-src="https://si.geilicdn.com/main.jpg">`
			case "no-state":
				body = `<img class="item-img" data-src="https://si.geilicdn.com/main.jpg">`
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			got, err := discoverWeidianProduct(context.Background(), getter, page)
			if err == nil || len(got.Items) == 0 {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestWeidianDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(weidianTestHTML))}, nil
	})
	got, err := Discover(context.Background(), getter, weidianTestShop, 1)
	if err == nil || !strings.Contains(err.Error(), "详情") || len(got.Items) != 4 || got.Items[0].URL != "https://si.geilicdn.com/main.jpg" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestWeidianRenderedCapture(t *testing.T) {
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
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == weidianTestShop {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(weidianTestHTML)))
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
	session.start, _ = url.Parse(weidianTestShop)
	if err := session.run(ctx, camoufox.Navigate(weidianTestShop)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".weidian.com").WithURL("https://shop1831323721.v.weidian.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = weidianRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("cross-origin credentials sent to %s", r.URL.Host)
		}
		if r.URL.Host == "thor.weidian.com" && r.URL.Path == "/detail/getDetailDesc/1.0" {
			return weidianHTTPResponse(r, 200, weidianTestDetail, "application/json"), nil
		}
		if r.URL.Host != "si.geilicdn.com" {
			t.Errorf("unexpected host %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 4 || got.Items[0].URL != "https://si.geilicdn.com/main.jpg" || got.Items[3].Metadata["image_kind"] != "description" || session.PageURL() != weidianTestShop {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestWeidianLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_WEIDIAN_LIVE") != "1" {
		t.Skip("set TULAI_WEIDIAN_LIVE=1 for a public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, weidianTestRoot, 1)
	if err != nil {
		t.Fatal(err)
	}
	var main, sku, detail *Item
	for i := range got.Items {
		switch got.Items[i].Metadata["image_kind"] {
		case "main":
			if main == nil {
				main = &got.Items[i]
			}
		case "sku":
			if sku == nil {
				sku = &got.Items[i]
			}
		case "description":
			if detail == nil {
				detail = &got.Items[i]
			}
		}
	}
	if main == nil || sku == nil || detail == nil {
		t.Fatalf("public product missing gallery, SKU or description: images=%d", len(got.Items))
	}
	for i, source := range []*Item{main, sku, detail} {
		item := *source
		item.Filename = "image"
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("image %d entries=%v err=%v", i, entries, err)
		}
		if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
			t.Fatalf("empty image %d: %v %v", i, info, err)
		}
	}
	t.Logf("public product images=%d; gallery, SKU and description images downloaded", len(got.Items))
}
