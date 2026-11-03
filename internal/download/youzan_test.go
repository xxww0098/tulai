package download

import (
	"context"
	"encoding/base64"
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
	youzanTestAlias = "26u1hw5egcc35"
	youzanTestH5    = "https://h5.youzan.com/v2/goods/26u1hw5egcc35"
	youzanTestJump  = "https://h5.youzan.com/wscgoods/detail/26u1hw5egcc35"
	youzanTestM     = "https://shop192169.m.youzan.com/wscgoods/detail/26u1hw5egcc35"
	youzanTestDesk  = "https://detail.youzan.com/show/goods?alias=26u1hw5egcc35"
)

type youzanRoundTrip func(*http.Request) (*http.Response, error)

func (f youzanRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func youzanHTTPResponse(r *http.Request, code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

const youzanMobileHTML = `<html><script>window._global = {"alias":"26u1hw5egcc35","goodsData":{"goods":{"alias":"26u1hw5egcc35","title":"测试商品","pictures":[{"url":"https://img01.yzcdn.cn/upload_files/main.png"}]},"skuInfo":{"props":[{"v":[{"name":"红色","imgUrl":"https://img.yzcdn.cn/upload_files/main.png"},{"name":"蓝色","imgUrl":"https://img.yzcdn.cn/upload_files/sku.jpg"}]}]}}};</script></html>`

const youzanDesktopHTML = `<html><h1 class="goods-title">测试商品</h1><meta property="og:url" content="https://detail.youzan.com/show/goods?alias=26u1hw5egcc35"><div class="swiper-pagination-list"><img src="https://img01.yzcdn.cn/upload_files/main.png!100x100.jpg"></div><div class="goods-detail js-goods-detail"><img src="https://img.yzcdn.cn/upload_files/main.png!730x0.jpg"><img src="https://img.yzcdn.cn/upload_files/detail.png!730x0.jpg"></div><div class="hot-goods"><img src="https://img.yzcdn.cn/upload_files/recommended.jpg"></div></html>`

func TestYouzanRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, alias string }{
		{youzanTestH5, youzanTestAlias},
		{youzanTestJump, youzanTestAlias},
		{youzanTestM, youzanTestAlias},
		{youzanTestDesk, youzanTestAlias},
		{"https://h5.youzan.com/v2/showcase/goods?alias=26u1hw5egcc35", youzanTestAlias},
		{"https://tuicashier.youzan.com/pay/wscgoods_order?alias=26u1hw5egcc35", youzanTestAlias},
		{"https://detail.youzan.com/show/goods?alias=26u1hw5egcc35&alias=other123", ""},
		{"https://detail.youzan.com/show/goods?alias=", ""},
		{"https://h5.youzan.com/v2/goods/abc/def", ""},
		{"https://detail.youzan.com.evil.test/show/goods?alias=26u1hw5egcc35", ""},
		{"https://user@detail.youzan.com/show/goods?alias=26u1hw5egcc35", ""},
		{"https://detail.youzan.com:8443/show/goods?alias=26u1hw5egcc35", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := youzanProductID(u); got != tc.alias {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(youzanTestDesk)
	for _, tc := range []struct{ raw, want, full string }{
		{"//img01.yzcdn.cn/upload_files/a.png!100x100.jpg", "https://img01.yzcdn.cn/upload_files/a.png!100x100.jpg", "https://img01.yzcdn.cn/upload_files/a.png"},
		{"https://img.yzcdn.cn/upload_files/a.png!730x0.jpg", "https://img.yzcdn.cn/upload_files/a.png!730x0.jpg", "https://img.yzcdn.cn/upload_files/a.png"},
		{"https://img.yzcdn.cn/upload_files/a.png!middle.jpg?sig=keep", "https://img.yzcdn.cn/upload_files/a.png!middle.jpg?sig=keep", "https://img.yzcdn.cn/upload_files/a.png!middle.jpg?sig=keep"},
		{"https://img.yzcdn.cn.evil.test/upload_files/a.jpg", "", ""},
		{"https://user@img.yzcdn.cn/upload_files/a.jpg", "", ""},
		{"https://img.yzcdn.cn:8443/upload_files/a.jpg", "", ""},
		{"https://img.yzcdn.cn/upload_files/../a.jpg", "", ""},
		{"data:image/png;base64,AA", "", ""},
	} {
		got, ok := youzanImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") || youzanFullImage(got) != tc.full {
			t.Errorf("image %s = %q full=%q ok=%t", tc.raw, got, youzanFullImage(got), ok)
		}
	}
}

func TestYouzanDesktopProduct(t *testing.T) {
	page, _ := url.Parse(youzanTestDesk)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(youzanDesktopHTML))}, nil
	})
	got, err := discoverYouzanProduct(context.Background(), getter, page)
	if err == nil || !strings.Contains(err.Error(), "规格") || got.Title != "测试商品" || len(got.Items) != 2 || got.Items[0].URL != "https://img01.yzcdn.cn/upload_files/main.png" || got.Items[1].URL != "https://img.yzcdn.cn/upload_files/detail.png" || got.Items[1].Metadata["image_kind"] != "description" {
		t.Fatalf("desktop=%+v err=%v", got, err)
	}
	if len(got.Items[0].AlternateURLs) != 1 || got.Items[0].AlternateURLs[0] != "https://img01.yzcdn.cn/upload_files/main.png!100x100.jpg" {
		t.Fatalf("fallback=%+v", got.Items[0])
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["youzan_alias"] != youzanTestAlias || item.Headers["Referer"] != "https://detail.youzan.com/" || item.Headers["Cookie"] != "" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestYouzanMobileProductAndRedirects(t *testing.T) {
	for _, mode := range []string{"success", "detail-redirect", "wrong-detail", "bad-product-redirect", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := NewHTTPGetter("")
			jar, _ := cookiejar.New(nil)
			for _, raw := range []string{youzanTestH5, youzanTestM, youzanTestDesk} {
				u, _ := url.Parse(raw)
				jar.SetCookies(u, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			}
			getter.client.Jar = jar
			var calls []string
			getter.client.Transport = youzanRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls = append(calls, r.URL.String())
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected credentials sent to %s", r.URL.Host)
				}
				switch r.URL.String() {
				case youzanTestH5:
					resp := youzanHTTPResponse(r, 302, "")
					resp.Header.Set("Location", youzanTestJump)
					if mode == "bad-product-redirect" {
						resp.Header.Set("Location", "https://evil.test/wscgoods/detail/26u1hw5egcc35")
					}
					return resp, nil
				case youzanTestJump:
					resp := youzanHTTPResponse(r, 301, "")
					resp.Header.Set("Location", youzanTestM)
					return resp, nil
				case youzanTestM:
					if mode == "cancel" {
						cancel()
					}
					return youzanHTTPResponse(r, 200, youzanMobileHTML), nil
				case youzanTestDesk:
					if mode == "detail-redirect" {
						resp := youzanHTTPResponse(r, 302, "")
						resp.Header.Set("Location", "https://evil.test/steal")
						return resp, nil
					}
					body := youzanDesktopHTML
					if mode == "wrong-detail" {
						body = strings.Replace(body, "alias=26u1hw5egcc35", "alias=other123", 1)
					}
					return youzanHTTPResponse(r, 200, body), nil
				default:
					t.Errorf("unexpected request %s", r.URL)
					return youzanHTTPResponse(r, 500, ""), nil
				}
			})
			page, _ := url.Parse(youzanTestH5)
			got, err := discoverYouzanProduct(ctx, getter, page)
			switch mode {
			case "success":
				if err != nil || len(got.Items) != 3 || got.Title != "测试商品" || got.Items[0].Metadata["image_kind"] != "main" || got.Items[1].Metadata["image_kind"] != "sku" || got.Items[1].Metadata["variant"] != "蓝色" || got.Items[2].Metadata["image_kind"] != "description" {
					t.Fatalf("mobile=%+v calls=%v err=%v", got, calls, err)
				}
			case "detail-redirect", "wrong-detail":
				if err == nil || len(got.Items) != 2 || len(calls) != 4 {
					t.Fatalf("partial=%+v calls=%v err=%v", got, calls, err)
				}
			case "bad-product-redirect":
				if err == nil || len(got.Items) != 0 || len(calls) != 1 {
					t.Fatalf("unsafe redirect=%+v calls=%v err=%v", got, calls, err)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || len(calls) != 3 {
					t.Fatalf("cancel=%+v calls=%v err=%v", got, calls, err)
				}
			}
		})
	}
}

func TestYouzanDesktopSKU(t *testing.T) {
	for _, mode := range []string{"success", "redirect", "wrong-alias", "missing-sku"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			jar, _ := cookiejar.New(nil)
			for _, raw := range []string{youzanTestDesk, youzanTestH5} {
				u, _ := url.Parse(raw)
				jar.SetCookies(u, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			}
			getter.client.Jar = jar
			var calls int
			getter.client.Transport = youzanRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected credentials sent to %s", r.URL.Host)
				}
				switch r.URL.String() {
				case youzanTestDesk:
					return youzanHTTPResponse(r, 200, youzanDesktopHTML), nil
				case youzanTestH5:
					if mode == "redirect" {
						resp := youzanHTTPResponse(r, 302, "")
						resp.Header.Set("Location", "https://evil.test/steal")
						return resp, nil
					}
					body := youzanMobileHTML
					if mode == "wrong-alias" {
						body = strings.ReplaceAll(body, youzanTestAlias, "other123")
					} else if mode == "missing-sku" {
						body = `<script>window._global = {"alias":"26u1hw5egcc35","goodsData":{"goods":{"alias":"26u1hw5egcc35"}}};</script>`
					}
					return youzanHTTPResponse(r, 200, body), nil
				default:
					t.Errorf("unexpected request %s", r.URL)
					return youzanHTTPResponse(r, 500, ""), nil
				}
			})
			page, _ := url.Parse(youzanTestDesk)
			got, err := discoverYouzanProduct(context.Background(), getter, page)
			if mode == "success" {
				if err != nil || len(got.Items) != 3 || got.Items[2].Metadata["image_kind"] != "sku" {
					t.Fatalf("desktop SKU=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "规格") || len(got.Items) != 2 || calls != 2 {
				t.Fatalf("desktop partial=%+v calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func TestYouzanSafetyAndPartial(t *testing.T) {
	page, _ := url.Parse(youzanTestDesk)
	for _, mode := range []string{"wrong-id", "unsafe-image", "no-gallery", "bad-state"} {
		t.Run(mode, func(t *testing.T) {
			body := youzanDesktopHTML
			switch mode {
			case "wrong-id":
				body = strings.Replace(body, "alias=26u1hw5egcc35", "alias=other123", 1)
			case "unsafe-image":
				body = strings.Replace(body, `</div><div class="hot-goods">`, `<img src="https://evil.test/a.jpg"></div><div class="hot-goods">`, 1)
			case "no-gallery":
				body = `<div>Verification required</div><img src="https://img.yzcdn.cn/upload_files/recommended.jpg">`
			case "bad-state":
				body += `<script>window._global = {broken};</script>`
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			got, err := discoverYouzanProduct(context.Background(), getter, page)
			want := 0
			if mode == "unsafe-image" || mode == "bad-state" {
				want = 2
			}
			if err == nil || len(got.Items) != want {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestYouzanDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(youzanDesktopHTML))}, nil
	})
	got, err := Discover(context.Background(), getter, youzanTestDesk, 1)
	if err == nil || !strings.Contains(err.Error(), "规格") || len(got.Items) != 2 || got.Items[0].URL != "https://img01.yzcdn.cn/upload_files/main.png" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestYouzanRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == youzanTestM {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(youzanMobileHTML)))
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
	session.start, _ = url.Parse(youzanTestM)
	if err := session.run(ctx, camoufox.Navigate(youzanTestM)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://shop192169.m.youzan.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = youzanRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("cross-origin credentials sent to %s", r.URL.Host)
		}
		if r.URL.String() == youzanTestDesk {
			return youzanHTTPResponse(r, 200, youzanDesktopHTML), nil
		}
		if r.URL.Host != "img01.yzcdn.cn" {
			t.Errorf("unexpected host %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://img01.yzcdn.cn/upload_files/main.png" || got.Items[2].Metadata["image_kind"] != "description" || session.PageURL() != youzanTestM {
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

func TestYouzanLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_YOUZAN_LIVE") != "1" {
		t.Skip("set TULAI_YOUZAN_LIVE=1 for a public product")
	}
	for _, raw := range []string{youzanTestDesk, youzanTestH5} {
		t.Run(raw, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			getter := NewHTTPGetter("")
			got, err := Discover(ctx, getter, raw, 1)
			if err != nil {
				t.Fatal(err)
			}
			var main, detail *Item
			for i := range got.Items {
				if got.Items[i].Metadata["image_kind"] == "main" && main == nil {
					main = &got.Items[i]
				}
				if got.Items[i].Metadata["image_kind"] == "description" && detail == nil {
					detail = &got.Items[i]
				}
			}
			if main == nil || detail == nil {
				t.Fatalf("public product missing gallery or description: images=%d", len(got.Items))
			}
			for i, source := range []*Item{main, detail} {
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
			t.Logf("public product images=%d; gallery and description images downloaded", len(got.Items))
		})
	}
}
