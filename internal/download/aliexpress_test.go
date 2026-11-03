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
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const aliTestDescURL = "https://aeproductsourcesite.alicdn.com/product/description/pc/v2/en_US/desc.htm?productId=12345&token=synthetic"
const aliTestPage = `<html><head><title>Test product</title><script>var product={descriptionUrl:"` + aliTestDescURL + `"};</script></head><body>
<div class="slider--img--a"><img src="//ae01.alicdn.com/kf/main.jpg_220x220.jpg_.avif"></div>
<div class="sku--wrap--b"><img src="//ae-pic-a1.aliexpress-media.com/kf/sku.png_100x100.png"></div>
<div class="description--product-description--c"><img src="//ae01.alicdn.com/kf/detail.jpg"></div>
<img src="//ae01.alicdn.com/kf/recommend.jpg">
</body></html>`
const aliTestDetail = `<html><body><img src="//ae01.alicdn.com/kf/extra.jpg"><img src="https://unrelated.invalid/not-product.jpg"></body></html>`

func aliTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestAliExpressProductURLs(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{"https://www.aliexpress.com/item/12345.html", "12345"},
		{"https://es.aliexpress.com/item/32756021879.html", "32756021879"},
		{"https://m.es.aliexpress.com/item/12345.html", "12345"},
		{"https://www.aliexpress.us/item/12345.html", "12345"},
		{"https://aliexpress.com/i/12345.html", "12345"},
		{"https://aliexpress.com/item/x.html", ""},
		{"https://aliexpress.com/item/12345.html/extra", ""},
		{"https://www.aliexpress.com.evil.test/item/12345.html", ""},
		{"https://user@www.aliexpress.com/item/12345.html", ""},
		{"https://www.aliexpress.com:8443/item/12345.html", ""},
		{"https://ae01.alicdn.com/kf/12345.jpg", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := aliExpressProductID(u); got != tc.id || isAliExpressProduct(u) != (tc.id != "") {
			t.Errorf("product ID %q = %q", tc.raw, got)
		}
	}
}

func TestAliExpressDOMAndDescription(t *testing.T) {
	root, _ := url.Parse("https://www.aliexpress.com/item/12345.html?token=private")
	var detailCalls int
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Hostname() != "aeproductsourcesite.alicdn.com" {
			return aliTestResponse(raw, aliTestPage), nil
		}
		r, _ := http.NewRequest("GET", raw, nil)
		prepare(r)
		if r.Header.Get("Referer") != "https://www.aliexpress.com/" || u.Query().Get("productId") != "12345" {
			t.Fatal("unexpected description request")
		}
		detailCalls++
		return aliTestResponse(raw, `<img src="//ae01.alicdn.com/kf/extra.jpg">`), nil
	})
	got, err := discoverAliExpressProduct(context.Background(), getter, root)
	if err != nil || detailCalls != 1 || len(got.Items) != 4 {
		t.Fatalf("product = %+v, calls %d, err %v", got.Items, detailCalls, err)
	}
	if got.Items[0].URL != "https://ae01.alicdn.com/kf/main.jpg" || got.Items[1].URL != "https://ae-pic-a1.aliexpress-media.com/kf/sku.png" || !strings.Contains(got.Items[3].URL, "extra.jpg") {
		t.Fatalf("originals = %+v", got.Items)
	}
	for _, item := range got.Items {
		if item.Headers["Referer"] != "https://www.aliexpress.com/" || strings.Contains(item.URL, "recommend") {
			t.Fatalf("unexpected product image %+v", item)
		}
	}
}

func TestAliExpressStructuredProduct(t *testing.T) {
	root, _ := url.Parse("https://www.aliexpress.com/item/12345.html")
	body := `<script>window.runParams = {data:{"actionModule":{"productId":12345},"titleModule":{"subject":"Structured"},"imageModule":{"imagePathList":["//ae01.alicdn.com/kf/main.jpg"]},"skuModule":{"productSKUPropertyList":[{"skuPropertyValues":[{"skuPropertyImagePath":"//ae01.alicdn.com/kf/sku.jpg"}]}]},"descriptionModule":{"descriptionUrl":"` + aliTestDescURL + `"}},csrfToken:"synthetic"};</script>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Hostname() == "aeproductsourcesite.alicdn.com" {
			return aliTestResponse(raw, `<img src="//ae01.alicdn.com/kf/extra.jpg">`), nil
		}
		return aliTestResponse(raw, body), nil
	})
	got, err := discoverAliExpressProduct(context.Background(), getter, root)
	if err != nil || len(got.Items) != 3 || got.Title != "Structured" {
		t.Fatalf("structured = %+v, %v", got.Items, err)
	}
	body = strings.Replace(body, `"productId":12345`, `"productId":99999`, 1)
	got, err = discoverAliExpressProduct(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 {
		t.Fatalf("wrong product = %+v, %v", got.Items, err)
	}
}

func TestAliExpressSafeURLsAndPartialErrors(t *testing.T) {
	root, _ := url.Parse("https://www.aliexpress.com/item/12345.html")
	for _, raw := range []string{
		"https://ae01.alicdn.com.evil.test/kf/x.jpg", "https://user@ae01.alicdn.com/kf/x.jpg",
		"https://ae01.alicdn.com:8443/kf/x.jpg", "http://ae01.alicdn.com/kf/x.jpg",
		"https://127.0.0.1/kf/x.jpg", "https://ae01.alicdn.com/../kf/x.jpg",
	} {
		if _, _, ok := aliImageURL(root, raw); ok {
			t.Errorf("unsafe image accepted: %s", raw)
		}
	}
	for _, raw := range []string{
		"https://pdp.aliexpress-media.com.evil.test/product/description/pc/v2/en_US/desc.htm?productId=12345",
		"https://user@pdp.aliexpress-media.com/product/description/pc/v2/en_US/desc.htm?productId=12345",
		"https://pdp.aliexpress-media.com:8443/product/description/pc/v2/en_US/desc.htm?productId=12345",
		"https://pdp.aliexpress-media.com/product/description/pc/v2/en_US/desc.htm?productId=99999",
		"https://pdp.aliexpress-media.com/product/description/pc/v2/en_US/desc.htm?productId=12345&productId=99999",
		"https://pdp.aliexpress-media.com/private/desc.htm?productId=12345",
	} {
		if aliDescriptionURL(root, raw, "12345") != nil {
			t.Errorf("unsafe detail accepted: %s", raw)
		}
	}
	for _, kind := range []string{"403", "redirect", "wrong product", "oversize", "cancel", "empty"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Hostname() != "aeproductsourcesite.alicdn.com" {
					return aliTestResponse(raw, aliTestPage), nil
				}
				resp := aliTestResponse(raw, `<img src="//ae01.alicdn.com/kf/extra.jpg">`)
				switch kind {
				case "403":
					resp.StatusCode = http.StatusForbidden
				case "redirect":
					resp.FinalURL = "https://evil.test/desc.htm?token=secret"
				case "wrong product":
					resp.FinalURL = strings.Replace(raw, "12345", "99999", 1)
				case "oversize":
					resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxPageBytes+1)))
				case "cancel":
					cancel()
					return nil, context.Canceled
				case "empty":
					resp = aliTestResponse(raw, "<html></html>")
				}
				return resp, nil
			})
			got, err := discoverAliExpressProduct(ctx, getter, root)
			if kind == "empty" {
				// The product already contains its own detail image.
				if err != nil || len(got.Items) != 3 {
					t.Fatalf("empty extra detail = %+v, %v", got.Items, err)
				}
				return
			}
			if err == nil || len(got.Items) != 3 || strings.Contains(err.Error(), "synthetic") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("partial = %+v, %v", got.Items, err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel = %v", err)
			}
		})
	}
}

func TestAliExpressChallengeAndRedirect(t *testing.T) {
	root, _ := url.Parse("https://www.aliexpress.com/item/12345.html")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return aliTestResponse(raw, `<script>location.href="/_____tmd_____/punish";</script>`), nil
	})
	got, err := discoverAliExpressProduct(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "验证") || len(got.Items) != 0 {
		t.Fatalf("challenge = %+v, %v", got.Items, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := aliTestResponse(raw, aliTestPage)
		resp.FinalURL = "https://evil.test/item/12345.html"
		return resp, nil
	})
	if _, err := discoverAliExpressProduct(context.Background(), getter, root); err == nil {
		t.Fatal("external redirect accepted")
	}
}

func TestAliExpressRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const target = "https://www.aliexpress.com/item/12345.html?token=synthetic"
	var detailCalls atomic.Int32
	session.getter.client.Transport = aliTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "aeproductsourcesite.alicdn.com" || r.URL.Query().Get("productId") != "12345" || r.Header.Get("Cookie") != "" {
			return nil, errors.New("unexpected detail request or cookie leak")
		}
		detailCalls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, Request: r, Body: io.NopCloser(strings.NewReader(`<img src="//ae01.alicdn.com/kf/extra.jpg">`))}, nil
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(aliTestPage)))
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
	start, _ := url.Parse(target)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.aliexpress.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 4 || detailCalls.Load() != 1 {
		t.Fatalf("capture = %+v, detail calls %d, err %v", got.Items, detailCalls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

type aliTestTransport func(*http.Request) (*http.Response, error)

func (f aliTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
