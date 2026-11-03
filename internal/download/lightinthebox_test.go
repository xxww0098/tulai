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

const lightInTheBoxTestID = "9390922"
const lightInTheBoxTestPage = "https://www.lightinthebox.com/en/p/example_p9390922.html"
const lightInTheBoxPublicImage = "https://litb-cgis.rightinthebox.com/images/170x226/202211/bps/product/inc/gtmpye1668508778991.jpg"
const lightInTheBoxTestHTML = `<html><head><title>Example</title></head><body><h1>Test product #9390922</h1>
<div class="viewport"><img alt="Test product" data-normal="` + lightInTheBoxPublicImage + `"></div>
<div class="attr-item-container"><span data-content="Blue"><img src="https://litb-cgis.rightinthebox.com/images/170x170/202211/bps/product/inc/variant.jpg"></span></div>
<div class="new-desc-photo"><img src="https://litb-cgis.rightinthebox.com/images/640x640/202211/bps/product/inc/detail.jpg"></div>
<div class="viewport"><img alt="Recommended product" data-normal="https://litb-cgis.rightinthebox.com/images/640x640/202211/bps/product/inc/recommendation.jpg"></div>
<img src="https://litb-cgis.rightinthebox.com/images/640x640/202211/bps/product/inc/unrelated.jpg"></body></html>`

type lightInTheBoxRoundTrip func(*http.Request) (*http.Response, error)

func (f lightInTheBoxRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func lightInTheBoxResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestLightInTheBoxRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{lightInTheBoxTestPage, lightInTheBoxTestID}, {"http://lightinthebox.com/p/example_p9390922.html", lightInTheBoxTestID},
		{"https://www.lightinthebox.com/en/p/example_p0.html", ""}, {"https://www.lightinthebox.com/en/p/example.html", ""},
		{"https://www.lightinthebox.com/c/example_9390922.html", ""}, {"https://www.lightinthebox.com.evil.test/en/p/example_p9390922.html", ""},
		{"https://user@www.lightinthebox.com/en/p/example_p9390922.html", ""}, {"https://www.lightinthebox.com:8443/en/p/example_p9390922.html", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := lightInTheBoxProductID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(lightInTheBoxTestPage)
	for _, tc := range []struct {
		raw, want string
		sku       bool
	}{
		{lightInTheBoxPublicImage, lightInTheBoxPublicImage, false},
		{"https://litb-cgis.rightinthebox.com/images/170x170/a.jpg", "https://litb-cgis.rightinthebox.com/images/640x640/a.jpg", true},
		{"https://litb-cgis.rightinthebox.com/images/170x170/a.jpg?sig=keep", "https://litb-cgis.rightinthebox.com/images/170x170/a.jpg?sig=keep", true},
		{"https://litb-cgis.rightinthebox.com/images/170x226/a.jpg", "https://litb-cgis.rightinthebox.com/images/170x226/a.jpg", true},
		{"https://litb-cgis.rightinthebox.com.evil.test/images/a.jpg", "", false},
		{"https://user@litb-cgis.rightinthebox.com/images/a.jpg", "", false},
		{"https://litb-cgis.rightinthebox.com:8443/images/a.jpg", "", false},
		{"https://litb-cgis.rightinthebox.com/images/a/%2e%2e/private.jpg", "", false},
		{"http://litb-cgis.rightinthebox.com/images/a.jpg", "", false},
	} {
		got, ok := lightInTheBoxImageURL(page, tc.raw, tc.sku)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q ok=%t", tc.raw, got, ok)
		}
	}
}

func TestLightInTheBoxProduct(t *testing.T) {
	page, _ := url.Parse(lightInTheBoxTestPage + "?tracking=private")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return lightInTheBoxResponse(raw, 200, lightInTheBoxTestHTML), nil
	})
	got, err := discoverLightInTheBoxProduct(context.Background(), getter, page)
	if err == nil || !strings.Contains(err.Error(), "详情接口尚未验证") || got.Title != "Test product" || len(got.Items) != 3 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	for i, kind := range []string{"main", "variant", "description"} {
		item := got.Items[i]
		if !item.RequireImage || item.Metadata["lightinthebox_product_id"] != lightInTheBoxTestID || item.Metadata["image_kind"] != kind || item.Headers["Referer"] != "https://www.lightinthebox.com/" || item.Headers["Cookie"] != "" || strings.Contains(item.URL, "recommendation") || strings.Contains(item.URL, "unrelated") {
			t.Errorf("item=%+v", item)
		}
	}
	if !strings.Contains(got.Items[1].URL, "/640x640/") || got.Items[1].Metadata["variant"] != "Blue" || len(got.Items[1].AlternateURLs) != 1 || !strings.Contains(got.Items[1].AlternateURLs[0], "/170x170/") {
		t.Fatalf("variant=%+v", got.Items[1])
	}
}

func TestLightInTheBoxPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(lightInTheBoxTestPage)
	for _, tc := range []struct {
		mode string
		want int
	}{
		{"wrong-id", 0}, {"missing-id", 0}, {"ambiguous-id", 0}, {"challenge", 0}, {"bad-host", 2}, {"wrong-alt", 2}, {"missing-sku", 2}, {"empty-shell", 0}, {"cancel", 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := lightInTheBoxTestHTML
			switch tc.mode {
			case "wrong-id":
				body = strings.Replace(body, "#9390922", "#9390923", 1)
			case "missing-id":
				body = strings.Replace(body, "#9390922", "", 1)
			case "ambiguous-id":
				body += `<h1>Other product #9390923</h1>`
			case "challenge":
				body = strings.Replace(body, "<title>Example</title>", "<title>Security Check</title>", 1)
			case "bad-host":
				body = strings.Replace(body, "https://litb-cgis.rightinthebox.com/images/640x640/202211/bps/product/inc/detail.jpg", "https://evil.test/detail.jpg", 1)
			case "wrong-alt":
				body = strings.Replace(body, `alt="Test product"`, `alt="Other product"`, 1)
			case "missing-sku":
				body = strings.Replace(body, `attr-item-container`, `unrelated-container`, 1)
			case "empty-shell":
				body = `<title>LightInTheBox</title><img src="https://litb-cgis.rightinthebox.com/images/a.jpg">`
			case "cancel":
				cancel()
			}
			doc, err := html.Parse(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			got, err := lightInTheBoxProductPage(ctx, doc, page)
			if err == nil || len(got.Items) != tc.want || (tc.mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s count=%d err=%v", tc.mode, len(got.Items), err)
			}
			for _, item := range got.Items {
				if strings.Contains(item.URL, "evil.test") || strings.Contains(item.URL, "recommendation") || strings.Contains(item.URL, "unrelated") {
					t.Fatalf("unsafe=%+v", item)
				}
			}
		})
	}
}

func TestLightInTheBoxRedirects(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade", "forbidden", "rate-limit"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			page, _ := url.Parse(lightInTheBoxTestPage)
			jar, _ := cookiejar.New(nil)
			jar.SetCookies(page, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			calls := 0
			getter.client.Transport = lightInTheBoxRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("credentials sent to %s", r.URL.Host)
				}
				status, body := 200, lightInTheBoxTestHTML
				headers := http.Header{"Content-Type": {"text/html"}}
				if calls == 1 {
					switch mode {
					case "forbidden":
						status, body = 403, ""
					case "rate-limit":
						status, body = 429, ""
					default:
						status, body = 302, ""
						target := lightInTheBoxTestPage + "?source=redirect"
						switch mode {
						case "wrong-id":
							target = "https://www.lightinthebox.com/en/p/other_p9390923.html"
						case "cross-origin":
							target = "https://evil.test/en/p/example_p9390922.html"
						case "downgrade":
							target = "http://www.lightinthebox.com/en/p/example_p9390922.html"
						}
						headers.Set("Location", target)
					}
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := discoverLightInTheBoxProduct(context.Background(), getter, page)
			if mode == "success" {
				if err == nil || len(got.Items) != 3 || calls != 2 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || calls != 1 || len(got.Items) != 0 {
				t.Fatalf("unsafe=%+v calls=%d err=%v", got, calls, err)
			} else if mode == "forbidden" && !strings.Contains(err.Error(), "登录或安全验证") {
				t.Fatalf("missing login message: %v", err)
			} else if mode == "rate-limit" && !strings.Contains(err.Error(), "限流") {
				t.Fatalf("missing rate-limit message: %v", err)
			}
		})
	}
}

func TestLightInTheBoxRenderedCapture(t *testing.T) {
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
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == lightInTheBoxTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(lightInTheBoxTestHTML)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case interceptErrors <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	session.start, _ = url.Parse(lightInTheBoxTestPage)
	if err := session.run(ctx, camoufox.Navigate(lightInTheBoxTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".lightinthebox.com").WithURL("https://www.lightinthebox.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = lightInTheBoxRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "litb-cgis.rightinthebox.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.lightinthebox.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err == nil || len(got.Items) != 3 || got.Items[0].URL != lightInTheBoxPublicImage || session.PageURL() != lightInTheBoxTestPage {
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
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestLightInTheBoxPublicImage(t *testing.T) {
	if os.Getenv("TULAI_LIGHTINTHEBOX_PUBLIC_IMAGE") != "1" {
		t.Skip("set TULAI_LIGHTINTHEBOX_PUBLIC_IMAGE=1 for published thumbnail download")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	page, _ := url.Parse(lightInTheBoxTestPage)
	doc, err := html.Parse(strings.NewReader(lightInTheBoxTestHTML))
	if err != nil {
		t.Fatal(err)
	}
	got, err := lightInTheBoxProductPage(ctx, doc, page)
	if err == nil || len(got.Items) == 0 || got.Items[0].URL != lightInTheBoxPublicImage {
		t.Fatalf("synthetic product items=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "thumbnail"
	dir := t.TempDir()
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("published thumbnail download: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("files=%v err=%v", entries, err)
	}
}
