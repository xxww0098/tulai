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
	ozonTestID   = "1940224127"
	ozonTestPage = "https://www.ozon.ru/product/sample-product-1940224127/"
)

type ozonRoundTrip func(*http.Request) (*http.Response, error)

func (f ozonRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func ozonHTTPResponse(r *http.Request, code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

const ozonTestHTML = `<html><title>fallback</title><meta property="og:url" content="https://www.ozon.ru/product/sample-product-1940224127/"><div data-widget="webProductHeading"><h1>测试商品</h1></div><div data-widget="webGallery"><img src="https://ir.ozone.ru/s3/multimedia-1-b/wc50/main.jpg"><img src="https://cdn1.ozone.ru/s3/multimedia-n/wc50/second.jpg"><img src="https://ir.ozone.ru/s3/multimedia-1-b/wc50/main.jpg"></div><div id="state-webAspects-123" data-state='{"aspects":[{"variants":[{"data":{"picture":"https://ir.ozone.ru/s3/multimedia-1-b/c100/wc140/sku.jpg","searchableText":"蓝色"}}]}]}'></div><img src="https://ir.ozone.ru/s3/multimedia-1-b/wc50/recommended.jpg"><img src="https://cdn1.ozonusercontent.com/s3/fs-my-account-avatar/wc50/avatar.jpg"></html>`

func TestOzonRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{ozonTestPage, ozonTestID},
		{"https://ozon.ru/product/1940224127/", ozonTestID},
		{"http://www.ozon.ru/product/sample-1940224127?x=1", ozonTestID},
		{"https://www.ozon.ru/product/0000000/", ""},
		{"https://www.ozon.ru/product/sample-123/", ""},
		{"https://www.ozon.ru/product/sample-1940224127/reviews/", ""},
		{"https://www.ozon.ru.evil.test/product/1940224127/", ""},
		{"https://user@www.ozon.ru/product/1940224127/", ""},
		{"https://www.ozon.ru:8443/product/1940224127/", ""},
		{"https://www.ozon.by/product/1940224127/", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := ozonProductID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(ozonTestPage)
	for _, tc := range []struct{ raw, want, full string }{
		{"//ir.ozone.ru/s3/multimedia-1-b/wc50/a.jpg", "https://ir.ozone.ru/s3/multimedia-1-b/wc50/a.jpg", "https://ir.ozone.ru/s3/multimedia-1-b/a.jpg"},
		{"https://cdn1.ozone.ru/s3/multimedia-n/c100/wc140/a.jpg.webp", "https://cdn1.ozone.ru/s3/multimedia-n/c100/wc140/a.jpg.webp", "https://cdn1.ozone.ru/s3/multimedia-n/a.jpg"},
		{"https://ir.ozone.ru/s3/multimedia-1-b/wc50/a.jpg?sig=keep", "https://ir.ozone.ru/s3/multimedia-1-b/wc50/a.jpg?sig=keep", "https://ir.ozone.ru/s3/multimedia-1-b/wc50/a.jpg?sig=keep"},
		{"https://cdn1.ozone.ru/s3/fs-my-account-avatar/wc50/avatar.jpg", "", ""},
		{"https://ir.ozone.ru.evil.test/s3/multimedia-n/wc50/a.jpg", "", ""},
		{"https://user@ir.ozone.ru/s3/multimedia-n/wc50/a.jpg", "", ""},
		{"https://ir.ozone.ru:8443/s3/multimedia-n/wc50/a.jpg", "", ""},
		{"https://ir.ozone.ru/s3/multimedia-n/../a.jpg", "", ""},
		{"data:image/png;base64,AA", "", ""},
	} {
		got, ok := ozonImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") || ozonFullImage(got) != tc.full {
			t.Errorf("image %s = %q full=%q ok=%t", tc.raw, got, ozonFullImage(got), ok)
		}
	}
}

func TestOzonGalleryAndSKU(t *testing.T) {
	page, _ := url.Parse(ozonTestPage + "?private=source")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(ozonTestHTML))}, nil
	})
	got, err := discoverOzonProduct(context.Background(), getter, page)
	if err != nil || got.Title != "测试商品" || len(got.Items) != 3 || got.Items[0].URL != "https://ir.ozone.ru/s3/multimedia-1-b/main.jpg" || got.Items[1].URL != "https://cdn1.ozone.ru/s3/multimedia-n/second.jpg" || got.Items[2].URL != "https://ir.ozone.ru/s3/multimedia-1-b/sku.jpg" || got.Items[2].Metadata["variant"] != "蓝色" {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	if len(got.Items[0].AlternateURLs) != 1 || got.Items[0].AlternateURLs[0] != "https://ir.ozone.ru/s3/multimedia-1-b/wc50/main.jpg" {
		t.Fatalf("fallback=%+v", got.Items[0])
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["ozon_product_id"] != ozonTestID || item.Headers["Referer"] != "https://www.ozon.ru/" || item.Headers["Cookie"] != "" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestOzonPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(ozonTestPage)
	for _, mode := range []string{"wrong-id", "invalid-image", "missing-sku", "bad-sku", "rich-content", "challenge", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := ozonTestHTML
			switch mode {
			case "wrong-id":
				body = strings.Replace(body, "/product/sample-product-1940224127/\"", "/product/other-1234567890/\"", 1)
			case "invalid-image":
				body = strings.Replace(body, `</div><div id="state-webAspects`, `<img src="https://evil.test/wc50/a.jpg"></div><div id="state-webAspects`, 1)
			case "missing-sku":
				body = strings.Replace(body, `id="state-webAspects-123"`, `id="not-product-state"`, 1)
			case "bad-sku":
				body = strings.Replace(body, `{"aspects":[`, `{"invalid":[`, 1)
			case "rich-content":
				body += `<div data-widget="webRichContent"></div>`
			case "challenge":
				body = `<div>Verification required</div><img src="https://ir.ozone.ru/s3/multimedia-1-b/wc50/recommended.jpg">`
			case "cancel":
				cancel()
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			got, err := discoverOzonProduct(ctx, getter, page)
			want := 0
			switch mode {
			case "invalid-image", "rich-content":
				want = 3
			case "missing-sku", "bad-sku":
				want = 2
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestOzonRedirects(t *testing.T) {
	other := "https://ozon.ru/product/1940224127/"
	for _, mode := range []string{"success", "loop", "wrong-id", "downgrade"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			jar, _ := cookiejar.New(nil)
			for _, raw := range []string{ozonTestPage, other} {
				u, _ := url.Parse(raw)
				jar.SetCookies(u, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			}
			getter.client.Jar = jar
			var calls int
			getter.client.Transport = ozonRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected credentials to %s", r.URL.Host)
				}
				if r.URL.String() == ozonTestPage {
					resp := ozonHTTPResponse(r, 307, "")
					target := other
					switch mode {
					case "loop":
						target = ozonTestPage + "?retry=1"
					case "wrong-id":
						target = "https://ozon.ru/product/1234567890/"
					case "downgrade":
						target = strings.Replace(other, "https:", "http:", 1)
					}
					resp.Header.Set("Location", target)
					return resp, nil
				}
				if r.URL.String() != other {
					t.Errorf("unexpected request %s", r.URL)
				}
				return ozonHTTPResponse(r, 200, ozonTestHTML), nil
			})
			page, _ := url.Parse(ozonTestPage)
			got, err := discoverOzonProduct(context.Background(), getter, page)
			if mode == "success" {
				if err != nil || len(got.Items) != 3 || calls != 2 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || calls != 1 || len(got.Items) != 0 || (mode == "loop" && !errors.Is(err, errOzonRedirect)) {
				t.Fatalf("unsafe redirect=%+v calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func TestOzonDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(ozonTestHTML))}, nil
	})
	got, err := Discover(context.Background(), getter, ozonTestPage, 1)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://ir.ozone.ru/s3/multimedia-1-b/main.jpg" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestOzonRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == ozonTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(ozonTestHTML)))
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
	session.start, _ = url.Parse(ozonTestPage)
	if err := session.run(ctx, camoufox.Navigate(ozonTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".ozon.ru").WithURL("https://www.ozon.ru/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = ozonRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "ir.ozone.ru" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.ozon.ru/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://ir.ozone.ru/s3/multimedia-1-b/main.jpg" || got.Items[2].Metadata["image_kind"] != "sku" || session.PageURL() != ozonTestPage {
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

func TestOzonPublicCDNImage(t *testing.T) {
	if os.Getenv("TULAI_OZON_CDN") != "1" {
		t.Skip("set TULAI_OZON_CDN=1 for a public Ozon CDN image")
	}
	doc, err := html.Parse(strings.NewReader(`<div data-widget="webGallery"><img src="https://ir.ozone.ru/s3/multimedia-1-b/wc50/6912448463.jpg"></div><div id="state-webAspects-test" data-state='{"aspects":[]}'></div>`))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse(ozonTestPage)
	got, err := ozonProductPage(context.Background(), doc, page)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://ir.ozone.ru/s3/multimedia-1-b/6912448463.jpg" {
		t.Fatalf("image=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("empty image: %v %v", info, err)
	}
	t.Logf("public CDN image downloaded")
}

func TestOzonLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_OZON_LIVE") != "1" {
		t.Skip("set TULAI_OZON_LIVE=1 for a public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), "https://www.ozon.ru/product/1940224127/", 1)
	if errors.Is(err, errOzonRedirect) {
		t.Skipf("public product entered a redirect loop: %v", err)
	}
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("public product images=%d err=%v", len(got.Items), err)
	}
	t.Logf("public product images=%d", len(got.Items))
}
