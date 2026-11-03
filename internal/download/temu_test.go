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

const temuTestID = "601099513224343"
const temuTestPage = "https://www.temu.com/example-g-" + temuTestID + ".html"
const temuTestHTML = `<html><head><title>TEMU</title><meta property="og:url" content="https://www.temu.com/example-g-601099513224343.html"><meta property="og:title" content="测试商品"></head><body>
<div class="_2ftcdy74 gallery"><img data-src="https://img.kwcdn.com/product/main.jpeg?imageView2/2/w/800"></div>
<div class="_2bzGqXzH"><img alt="蓝色" src="https://img.kwcdn.com/product/sku.png"></div>
<div class="_2jCYETGt"><img data-src="https://img-eu.kwcdn.com/product/detail.jpg"></div>
<img data-src="https://img.kwcdn.com/product/recommended.jpg"><div class="_2ftcdy74-recommended-other"><img data-src="https://img.kwcdn.com/product/recommended-2.jpg"></div></body></html>`

type temuRoundTrip func(*http.Request) (*http.Response, error)

func (f temuRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func temuResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestTemuRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{temuTestPage, temuTestID}, {"https://www.temu.com/-g-601099513224343.html", temuTestID}, {"http://temu.com/uk/test-g-601099513224343.html", temuTestID},
		{"https://www.temu.com/goods.html?goods_id=601099513224343", ""}, {"https://www.temu.com/example-g-123.html", ""},
		{"https://www.temu.com/example-g-601099513224343.html?goods_id=1", ""}, {"https://www.temu.com/example-g-601099513224343.html?goods_id=601099513224343&goods_id=601099513224343", ""},
		{"https://www.temu.com.evil.test/example-g-601099513224343.html", ""}, {"https://user@www.temu.com/example-g-601099513224343.html", ""}, {"https://www.temu.com:8443/example-g-601099513224343.html", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := temuProductID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(temuTestPage)
	for _, tc := range []struct{ raw, want, alt string }{
		{"//img.kwcdn.com/product/a.jpg?imageView2/2/w/800", "https://img.kwcdn.com/product/a.jpg", "https://img.kwcdn.com/product/a.jpg?imageView2/2/w/800"},
		{"http://img-eu.kwcdn.com/product/a.jpg", "https://img-eu.kwcdn.com/product/a.jpg", ""},
		{"https://img.kwcdn.com/product/a.jpg?imageView2/2/w/800&signature=keep", "https://img.kwcdn.com/product/a.jpg?imageView2/2/w/800&signature=keep", ""},
		{"https://img.kwcdn.com.evil.test/a.jpg", "", ""}, {"https://user@img.kwcdn.com/a.jpg", "", ""}, {"https://img.kwcdn.com:8443/a.jpg", "", ""}, {"https://img.kwcdn.com/a/../private.jpg", "", ""}, {"data:image/png;base64,AA", "", ""},
	} {
		got, alt, ok := temuImageURL(page, tc.raw)
		if got != tc.want || alt != tc.alt || ok != (tc.want != "") {
			t.Errorf("image %s = %q alt=%q ok=%t", tc.raw, got, alt, ok)
		}
	}
}

func TestTemuProductAndDetails(t *testing.T) {
	page, _ := url.Parse(temuTestPage + "?tracking=private")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return temuResponse(raw, 200, temuTestHTML), nil
	})
	got, err := discoverTemuProduct(context.Background(), getter, page)
	if err != nil || got.Title != "测试商品" || len(got.Items) != 3 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	want := []string{"main", "variant", "description"}
	for i, item := range got.Items {
		if !item.RequireImage || item.Metadata["temu_goods_id"] != temuTestID || item.Metadata["image_kind"] != want[i] || item.Headers["Referer"] != "https://www.temu.com/" || item.Headers["Cookie"] != "" || strings.Contains(item.URL, "recommended") {
			t.Errorf("item %d=%+v", i, item)
		}
	}
	if got.Items[0].URL != "https://img.kwcdn.com/product/main.jpeg" || len(got.Items[0].AlternateURLs) != 1 || got.Items[1].Metadata["variant"] != "蓝色" {
		t.Fatalf("thumbnail/variant=%+v", got.Items)
	}
}

func TestTemuPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(temuTestPage)
	for _, tc := range []struct {
		mode string
		want int
	}{{"wrong-id", 0}, {"missing-id", 0}, {"conflicting-id", 0}, {"bad-host", 2}, {"missing-sku", 2}, {"empty-shell", 0}, {"cancel", 0}} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := temuTestHTML
			switch tc.mode {
			case "wrong-id":
				body = strings.Replace(body, `content="https://www.temu.com/example-g-601099513224343.html"`, `content="https://www.temu.com/other-g-601099513224344.html"`, 1)
			case "missing-id":
				body = strings.Replace(body, `<meta property="og:url" content="https://www.temu.com/example-g-601099513224343.html">`, "", 1)
			case "conflicting-id":
				body = strings.Replace(body, `<meta property="og:title"`, `<meta property="og:url" content="https://www.temu.com/other-g-601099513224344.html"><meta property="og:title"`, 1)
			case "bad-host":
				body = strings.Replace(body, "https://img-eu.kwcdn.com/product/detail.jpg", "https://evil.test/detail.jpg", 1)
			case "missing-sku":
				body = strings.Replace(body, `<div class="_2bzGqXzH"><img alt="蓝色" src="https://img.kwcdn.com/product/sku.png"></div>`, "", 1)
			case "empty-shell":
				body = `<title>TEMU</title><meta property="og:url" content="https://www.temu.com/example-g-601099513224343.html"><img src="https://img.kwcdn.com/product/recommended.jpg">`
			case "cancel":
				cancel()
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return temuResponse(raw, 200, body), nil
			})
			got, err := discoverTemuProduct(ctx, getter, page)
			if err == nil || len(got.Items) != tc.want || (tc.mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s count=%d err=%v", tc.mode, len(got.Items), err)
			}
			for _, item := range got.Items {
				if strings.Contains(item.URL, "evil.test") || strings.Contains(item.URL, "recommended") {
					t.Fatalf("unsafe=%+v", item)
				}
			}
		})
	}
}

func TestTemuRedirects(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			page, _ := url.Parse(temuTestPage)
			jar, _ := cookiejar.New(nil)
			jar.SetCookies(page, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			calls := 0
			getter.client.Transport = temuRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("credentials sent to %s", r.URL.Host)
				}
				status, body := 200, temuTestHTML
				headers := http.Header{"Content-Type": {"text/html"}}
				if calls == 1 {
					status, body = 302, ""
					target := temuTestPage + "?source=redirect"
					switch mode {
					case "wrong-id":
						target = "https://www.temu.com/other-g-601099513224344.html"
					case "cross-origin":
						target = "https://evil.test/example-g-601099513224343.html"
					case "downgrade":
						target = "http://www.temu.com/example-g-601099513224343.html"
					}
					headers.Set("Location", target)
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := discoverTemuProduct(context.Background(), getter, page)
			if mode == "success" {
				if err != nil || len(got.Items) != 3 || calls != 2 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || calls != 1 || len(got.Items) != 0 {
				t.Fatalf("unsafe=%+v calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func TestTemuRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == temuTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(temuTestHTML)))
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
	session.start, _ = url.Parse(temuTestPage)
	if err := session.run(ctx, camoufox.Navigate(temuTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".temu.com").WithURL("https://www.temu.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = temuRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "img.kwcdn.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.temu.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://img.kwcdn.com/product/main.jpeg" || session.PageURL() != temuTestPage {
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

func TestTemuPublicExampleImages(t *testing.T) {
	path := os.Getenv("TULAI_TEMU_EXAMPLE")
	if path == "" {
		t.Skip("set TULAI_TEMU_EXAMPLE to a public product-data example")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var sample struct {
		Result struct {
			Goods struct {
				ID      json.Number `json:"goods_id"`
				Gallery []struct {
					Type int    `json:"type"`
					URL  string `json:"url"`
				} `json:"gallery"`
			} `json:"goods"`
			SKU []struct {
				Image string `json:"spec_show_image_url"`
			} `json:"sku"`
			Detail struct {
				Floors []struct {
					Items []struct {
						URL   string          `json:"url"`
						Video json.RawMessage `json:"video"`
					} `json:"items"`
				} `json:"floor_list"`
			} `json:"product_detail"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &sample); err != nil {
		t.Fatal(err)
	}
	id := sample.Result.Goods.ID.String()
	var gallery, variant, detail string
	for _, v := range sample.Result.Goods.Gallery {
		if v.Type == 1 && v.URL != "" {
			gallery = v.URL
			break
		}
	}
	for _, v := range sample.Result.SKU {
		if v.Image != "" {
			variant = v.Image
			break
		}
	}
	for _, f := range sample.Result.Detail.Floors {
		for _, v := range f.Items {
			if v.URL != "" && len(v.Video) == 0 {
				detail = v.URL
				break
			}
		}
		if detail != "" {
			break
		}
	}
	if gallery == "" || variant == "" || detail == "" {
		t.Fatal("public sample missing image fields")
	}
	raw := "https://www.temu.com/example-g-" + id + ".html"
	page, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`<meta property="og:url" content="%s"><div class="_2ftcdy74"><img data-src="%s"></div><div class="_2bzGqXzH"><img alt="sample" src="%s"></div><div class="_2jCYETGt"><img data-src="%s"></div>`, raw, gallery, variant, detail)
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	got, err := temuProductPage(ctx, doc, page)
	if err != nil || len(got.Items) != 3 {
		t.Fatalf("synthetic DOM from public sample=%+v err=%v", got, err)
	}
	getter := NewHTTPGetter("")
	for _, item := range got.Items {
		item.Filename = "image"
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatalf("%s download: %v", item.Metadata["image_kind"], err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("entries=%v err=%v", entries, err)
		}
	}
}

func TestTemuCurrentPublicPage(t *testing.T) {
	if os.Getenv("TULAI_TEMU_LIVE") != "1" {
		t.Skip("set TULAI_TEMU_LIVE=1 for current public page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	page, _ := url.Parse("https://www.temu.com/-g-601099513224343.html")
	got, err := discoverTemuProduct(ctx, NewHTTPGetter(""), page)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("current page changed: images=%d err=%v", len(got.Items), err)
	}
}

func TestTemuThumbnailFallback(t *testing.T) {
	page, _ := url.Parse(temuTestPage)
	doc, err := html.Parse(strings.NewReader(temuTestHTML))
	if err != nil {
		t.Fatal(err)
	}
	found, err := temuProductPage(context.Background(), doc, page)
	if err != nil || len(found.Items) != 3 {
		t.Fatalf("product=%+v err=%v", found, err)
	}
	item := found.Items[0]
	if len(item.AlternateURLs) != 1 {
		t.Fatal("missing same-origin thumbnail fallback")
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+XW8kAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		if raw == item.URL {
			return &Response{StatusCode: 404, FinalURL: raw, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("not found"))}, nil
		}
		if raw == item.AlternateURLs[0] {
			return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader(string(png)))}, nil
		}
		return nil, errors.New("unexpected URL")
	})
	item.Filename = "image"
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(context.Background(), Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != item.URL || calls[1] != item.AlternateURLs[0] {
		t.Fatalf("fallback calls=%v", calls)
	}
	if info, err := os.Stat(filepath.Join(dir, "image.png")); err != nil || info.Size() == 0 {
		t.Fatalf("download=%v err=%v", info, err)
	}
}

func TestTemuDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return temuResponse(raw, 200, temuTestHTML), nil
	})
	got, err := Discover(context.Background(), getter, temuTestPage, 1)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://img.kwcdn.com/product/main.jpeg" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}
