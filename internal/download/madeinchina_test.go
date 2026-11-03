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
	"golang.org/x/net/html"
)

const (
	madeInChinaTestID   = "cRspPQFKfvVh"
	madeInChinaTestPage = "https://example.en.made-in-china.com/product/cRspPQFKfvVh/China-Sample.html"
)

type madeInChinaRoundTrip func(*http.Request) (*http.Response, error)

func (f madeInChinaRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func madeInChinaHTTPResponse(r *http.Request, code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

const madeInChinaTestHTML = `<html><title>fallback</title><meta property="og:url" content="https://example.en.made-in-china.com/product/cRspPQFKfvVh/China-Sample.html"><h1 class="sr-proMainInfo-baseInfoH1 J-baseInfo-name">测试商品</h1><div class="sr-proMainInfo-slide"><div class="swiper-wrapper"><div class="swiper-slide" fsrc="//image.made-in-china.com/full-a.webp"><figure><img src="//image.made-in-china.com/preview-a.webp"></figure></div><div class="swiper-slide" fsrc="https://image.made-in-china.com/full-b.webp?sig=keep"></div><div class="swiper-slide J-play-video"><img src="https://image.made-in-china.com/video-poster.webp"></div></div></div><section class="sr-layout-content detail-desc"><img data-original="https://image.made-in-china.com/detail-a.webp" src="https://www.micstatic.com/transparent.png"><img data-original="https://image.made-in-china.com/detail-b.webp"></section><div class="swiper-wrapper"><div fsrc="https://image.made-in-china.com/recommended.webp"></div></div></html>`

func TestMadeInChinaRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{madeInChinaTestPage, madeInChinaTestID},
		{"http://cnzhuoying.en.made-in-china.com/product/cRspPQFKfvVh/China-Product.html?x=1", madeInChinaTestID},
		{"https://www.made-in-china.com/product/Example-Product-cRspPQFKfvVh.html", madeInChinaTestID},
		{"https://www.made-in-china.com/products-search/Example.html", ""},
		{"https://example.en.made-in-china.com/product/short/China-Product.html", ""},
		{"https://example.en.made-in-china.com/product/cRspPQFKfvVh/China-Product.html/other", ""},
		{"https://example.en.made-in-china.com.evil.test/product/cRspPQFKfvVh/China-Product.html", ""},
		{"https://user@example.en.made-in-china.com/product/cRspPQFKfvVh/China-Product.html", ""},
		{"https://example.en.made-in-china.com:8443/product/cRspPQFKfvVh/China-Product.html", ""},
		{"https://captcha.made-in-china.com/verification.html", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := madeInChinaProductID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(madeInChinaTestPage)
	for _, tc := range []struct{ raw, want string }{
		{"//image.made-in-china.com/a/full.webp?sig=keep", "https://image.made-in-china.com/a/full.webp?sig=keep"},
		{"http://image.made-in-china.com/a/full.jpg", "https://image.made-in-china.com/a/full.jpg"},
		{"https://image.made-in-china.com.evil.test/a/full.jpg", ""},
		{"https://user@image.made-in-china.com/a/full.jpg", ""},
		{"https://image.made-in-china.com:8443/a/full.jpg", ""},
		{"https://image.made-in-china.com/a/../private.jpg", ""},
		{"data:image/png;base64,AA", ""},
	} {
		got, ok := madeInChinaImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q ok=%t", tc.raw, got, ok)
		}
	}
}

func TestMadeInChinaProductAndDetail(t *testing.T) {
	page, _ := url.Parse(madeInChinaTestPage + "?private=source")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(madeInChinaTestHTML))}, nil
	})
	got, err := discoverMadeInChinaProduct(context.Background(), getter, page)
	if err != nil || got.Title != "测试商品" || len(got.Items) != 4 || got.Items[0].URL != "https://image.made-in-china.com/full-a.webp" || got.Items[1].URL != "https://image.made-in-china.com/full-b.webp?sig=keep" || got.Items[2].Metadata["image_kind"] != "description" {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	if len(got.Items[0].AlternateURLs) != 1 || got.Items[0].AlternateURLs[0] != "https://image.made-in-china.com/preview-a.webp" {
		t.Fatalf("fallback=%+v", got.Items[0])
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["madeinchina_product_id"] != madeInChinaTestID || item.Headers["Referer"] != "https://example.en.made-in-china.com/" || item.Headers["Cookie"] != "" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestMadeInChinaPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(madeInChinaTestPage)
	for _, mode := range []string{"wrong-id", "invalid-image", "preview", "no-detail", "challenge", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := madeInChinaTestHTML
			switch mode {
			case "wrong-id":
				body = strings.Replace(body, "/product/cRspPQFKfvVh/China-Sample.html\"", "/product/OtherProductID/China-Sample.html\"", 1)
			case "invalid-image":
				body = strings.Replace(body, `</section>`, `<img data-original="https://evil.test/a.jpg"></section>`, 1)
			case "preview":
				body = strings.Replace(body, `</section>`, `<img src="https://image.made-in-china.com/preview.jpg"></section>`, 1)
			case "no-detail":
				body = strings.Replace(body, `class="sr-layout-content detail-desc"`, `class="other"`, 1)
			case "challenge":
				body = `<div>Verification required</div><img src="https://image.made-in-china.com/recommended.webp">`
			case "cancel":
				cancel()
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			got, err := discoverMadeInChinaProduct(ctx, getter, page)
			want := 0
			switch mode {
			case "invalid-image":
				want = 4
			case "preview":
				want = 5
			case "no-detail":
				want = 2
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestMadeInChinaRedirects(t *testing.T) {
	other := "https://second.en.made-in-china.com/product/cRspPQFKfvVh/China-Sample.html"
	for _, mode := range []string{"success", "challenge", "wrong-id", "downgrade"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			jar, _ := cookiejar.New(nil)
			for _, raw := range []string{madeInChinaTestPage, other} {
				u, _ := url.Parse(raw)
				jar.SetCookies(u, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			}
			getter.client.Jar = jar
			var calls int
			getter.client.Transport = madeInChinaRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected credentials to %s", r.URL.Host)
				}
				if r.URL.String() == madeInChinaTestPage {
					resp := madeInChinaHTTPResponse(r, 302, "")
					target := other
					switch mode {
					case "challenge":
						target = "https://captcha.made-in-china.com/verification.html"
					case "wrong-id":
						target = "https://second.en.made-in-china.com/product/OtherProductID/China-Sample.html"
					case "downgrade":
						target = strings.Replace(other, "https:", "http:", 1)
					}
					resp.Header.Set("Location", target)
					return resp, nil
				}
				if r.URL.String() != other {
					t.Errorf("unexpected request %s", r.URL)
				}
				return madeInChinaHTTPResponse(r, 200, madeInChinaTestHTML), nil
			})
			page, _ := url.Parse(madeInChinaTestPage)
			got, err := discoverMadeInChinaProduct(context.Background(), getter, page)
			if mode == "success" {
				if err != nil || len(got.Items) != 4 || calls != 2 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || calls != 1 || len(got.Items) != 0 || (mode == "challenge" && !errors.Is(err, errMadeInChinaChallenge)) {
				t.Fatalf("unsafe redirect=%+v calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func TestMadeInChinaDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(madeInChinaTestHTML))}, nil
	})
	got, err := Discover(context.Background(), getter, madeInChinaTestPage, 1)
	if err != nil || len(got.Items) != 4 || got.Items[0].URL != "https://image.made-in-china.com/full-a.webp" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestMadeInChinaRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == madeInChinaTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(madeInChinaTestHTML)))
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
	session.start, _ = url.Parse(madeInChinaTestPage)
	if err := session.run(ctx, camoufox.Navigate(madeInChinaTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".made-in-china.com").WithURL("https://example.en.made-in-china.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = madeInChinaRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "image.made-in-china.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://example.en.made-in-china.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/webp"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 4 || got.Items[0].URL != "https://image.made-in-china.com/full-a.webp" || got.Items[2].Metadata["image_kind"] != "description" || session.PageURL() != madeInChinaTestPage {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestMadeInChinaCapturedProduct(t *testing.T) {
	path := os.Getenv("TULAI_MADEINCHINA_CAPTURE")
	if path == "" {
		t.Skip("set TULAI_MADEINCHINA_CAPTURE to a public product HTML")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc, err := html.Parse(io.LimitReader(file, maxPageBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse("https://cnzhuoying.en.made-in-china.com/product/cRspPQFKfvVh/China-Wholesale-Chinese-Home-Furniture-for-Modern-Office-Bedroom-Living-Room-and-Kitchen.html")
	got, err := madeInChinaProductPage(context.Background(), doc, page)
	if err != nil || len(got.Items) != 16 {
		t.Fatalf("public product images=%d err=%v", len(got.Items), err)
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
		t.Fatal("public product missing gallery or description")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for i, source := range []*Item{main, detail} {
		item := *source
		item.Filename = "image"
		dir := t.TempDir()
		if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
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
	t.Logf("captured public product images=%d; gallery and description images downloaded", len(got.Items))
}

func TestMadeInChinaLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_MADEINCHINA_LIVE") != "1" {
		t.Skip("set TULAI_MADEINCHINA_LIVE=1 for a public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const product = "https://cnzhuoying.en.made-in-china.com/product/cRspPQFKfvVh/China-Wholesale-Chinese-Home-Furniture-for-Modern-Office-Bedroom-Living-Room-and-Kitchen.html"
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, product, 1)
	if errors.Is(err, errMadeInChinaChallenge) {
		t.Skipf("public product requires verification: %v", err)
	}
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
}
