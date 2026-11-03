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
	"golang.org/x/net/html"
)

const dewuTestPage = "https://fast.dewu.com/page/productDetail?spuId=12345"
const dewuTestState = `{"modelSequence":[],"detail":{"spuId":12345,"title":"测试商品","isShow":1},"image":{"spuImage":{"images":[{"url":"https://cdn.poizon.com/product/main.jpg"}]}},"imageAndTextMap":{"spuShowSpread":[{"type":"image","images":[{"url":"https://cdn.poizon.com/product/detail.jpg"}]}]},"saleProperties":{"list":[{"showValue":1,"value":"蓝色","levelImageUrl":"https://cdn.poizon.com/product/sku.jpg"},{"showValue":1,"value":"尺寸","levelImageUrl":""}]},"recommendations":[{"url":"https://cdn.poizon.com/product/recommended.jpg"}]}`
const dewuTestHTML = `<html><head><title>得物</title><script class="__modern_entry">__remixContext.r("routes/page.$pageName", "simpleDetail", ` + dewuTestState + `);</script></head><body><img src="https://cdn.poizon.com/product/recommended.jpg"></body></html>`

type dewuRoundTrip func(*http.Request) (*http.Response, error)

func (f dewuRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func dewuResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDewuRoutesAndImageURLs(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{dewuTestPage, "12345"}, {"http://fast.dewu.com/page/productDetail?spuId=12345", "12345"},
		{"https://www.dewu.com/product-detail.html?spuId=12345", "12345"},
		{"https://fast.dewu.com/page/productDetail?spuId=0", ""}, {"https://fast.dewu.com/page/productDetail?spuId=012345", ""},
		{"https://fast.dewu.com/page/productDetail?spuId=12345&spuId=12345", ""},
		{"https://fast.dewu.com/router/product/BrandDetailPage?spuId=12345", ""},
		{"https://fast.dewu.com.evil.test/page/productDetail?spuId=12345", ""},
		{"https://user@fast.dewu.com/page/productDetail?spuId=12345", ""},
		{"https://fast.dewu.com:8443/page/productDetail?spuId=12345", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := dewuProductID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(dewuTestPage)
	for _, tc := range []struct{ raw, want string }{
		{"//cdn.poizon.com/product/a.jpg?x=1", "https://cdn.poizon.com/product/a.jpg?x=1"},
		{"https://cdn.poizon.com/product/a.jpg#fragment", "https://cdn.poizon.com/product/a.jpg"},
		{"http://cdn.poizon.com/product/a.jpg", ""}, {"https://cdn.poizon.com.evil.test/a.jpg", ""},
		{"https://user@cdn.poizon.com/a.jpg", ""}, {"https://cdn.poizon.com:8443/a.jpg", ""},
		{"https://cdn.poizon.com/a/%2e%2e/private.jpg", ""}, {"data:image/png;base64,AA", ""},
	} {
		got, ok := dewuImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q ok=%t", tc.raw, got, ok)
		}
	}
}

func TestDewuProductAndDetails(t *testing.T) {
	page, _ := url.Parse(dewuTestPage + "&tracking=private")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return dewuResponse(raw, 200, dewuTestHTML), nil
	})
	got, err := discoverDewuProduct(context.Background(), getter, page)
	if err != nil || got.Title != "测试商品" || len(got.Items) != 3 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	for i, kind := range []string{"main", "description", "variant"} {
		item := got.Items[i]
		if !item.RequireImage || item.Metadata["dewu_spu_id"] != "12345" || item.Metadata["image_kind"] != kind || item.Headers["Referer"] != "https://fast.dewu.com/" || item.Headers["Cookie"] != "" || strings.Contains(item.URL, "recommended") {
			t.Errorf("item %d=%+v", i, item)
		}
	}
	if got.Items[2].Metadata["variant"] != "蓝色" {
		t.Fatalf("variant=%+v", got.Items[2])
	}
}

func TestDewuPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(dewuTestPage)
	for _, tc := range []struct {
		mode string
		want int
	}{
		{"wrong-id", 0}, {"missing-id", 0}, {"conflicting-id", 0}, {"challenge", 0}, {"hidden-product", 0},
		{"bad-host", 2}, {"broken-main-item", 3}, {"hidden-sku", 2}, {"missing-desc", 2}, {"empty-shell", 0}, {"cancel", 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := dewuTestHTML
			switch tc.mode {
			case "wrong-id":
				body = strings.Replace(body, `"spuId":12345`, `"spuId":12346`, 1)
			case "missing-id":
				body = strings.Replace(body, `"spuId":12345,`, "", 1)
			case "conflicting-id":
				body += `<script>__remixContext.r("routes/page.$pageName", "simpleDetail", ` + strings.Replace(dewuTestState, `"spuId":12345`, `"spuId":12346`, 1) + `);</script>`
			case "challenge":
				body += `<div id="dw-slide"></div>`
			case "hidden-product":
				body = strings.Replace(body, `"isShow":1`, `"isShow":0`, 1)
			case "bad-host":
				body = strings.Replace(body, "https://cdn.poizon.com/product/detail.jpg", "https://evil.test/detail.jpg", 1)
			case "broken-main-item":
				body = strings.Replace(body, `"images":[{"url":"https://cdn.poizon.com/product/main.jpg"}]`, `"images":[123,{"url":"https://cdn.poizon.com/product/main.jpg"}]`, 1)
			case "hidden-sku":
				body = strings.Replace(body, `"showValue":1,"value":"蓝色"`, `"showValue":0,"value":"蓝色"`, 1)
			case "missing-desc":
				body = strings.Replace(body, `"imageAndTextMap":{"spuShowSpread":[{"type":"image","images":[{"url":"https://cdn.poizon.com/product/detail.jpg"}]}]}`, `"imageAndTextMap":{}`, 1)
			case "empty-shell":
				body = `<title>得物</title><img src="https://cdn.poizon.com/product/recommended.jpg">`
			case "cancel":
				cancel()
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return dewuResponse(raw, 200, body), nil
			})
			got, err := discoverDewuProduct(ctx, getter, page)
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

func TestDewuRedirects(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			page, _ := url.Parse(dewuTestPage)
			jar, _ := cookiejar.New(nil)
			jar.SetCookies(page, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			calls := 0
			getter.client.Transport = dewuRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("credentials sent to %s", r.URL.Host)
				}
				status, body := 200, dewuTestHTML
				headers := http.Header{"Content-Type": {"text/html"}}
				if calls == 1 {
					status, body = 302, ""
					target := dewuTestPage + "&source=redirect"
					switch mode {
					case "wrong-id":
						target = "https://fast.dewu.com/page/productDetail?spuId=12346"
					case "cross-origin":
						target = "https://evil.test/page/productDetail?spuId=12345"
					case "downgrade":
						target = "http://fast.dewu.com/page/productDetail?spuId=12345"
					}
					headers.Set("Location", target)
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := discoverDewuProduct(context.Background(), getter, page)
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

func TestDewuAccessErrors(t *testing.T) {
	page, _ := url.Parse(dewuTestPage)
	for _, tc := range []struct {
		code int
		text string
	}{{http.StatusForbidden, "登录或安全验证"}, {http.StatusTooManyRequests, "限流"}} {
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			return dewuResponse(raw, tc.code, ""), nil
		})
		got, err := discoverDewuProduct(context.Background(), getter, page)
		var status *HTTPStatusError
		if err == nil || !strings.Contains(err.Error(), tc.text) || !errors.As(err, &status) || status.StatusCode != tc.code || len(got.Items) != 0 {
			t.Fatalf("HTTP %d: items=%d err=%v", tc.code, len(got.Items), err)
		}
	}
}

func TestDewuRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == dewuTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(dewuTestHTML)))
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
	session.start, _ = url.Parse(dewuTestPage)
	if err := session.run(ctx, camoufox.Navigate(dewuTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".dewu.com").WithURL("https://fast.dewu.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = dewuRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "cdn.poizon.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://fast.dewu.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://cdn.poizon.com/product/main.jpg" || session.PageURL() != dewuTestPage {
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

func TestDewuSavedPublicPage(t *testing.T) {
	path := os.Getenv("TULAI_DEWU_SAMPLE")
	if path == "" {
		t.Skip("set TULAI_DEWU_SAMPLE to a saved public product page")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc, err := html.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse("https://fast.dewu.com/page/productDetail?spuId=33425209")
	got, err := dewuProductPage(context.Background(), doc, page)
	if err != nil || len(got.Items) != 7 {
		t.Fatalf("public sample items=%d err=%v", len(got.Items), err)
	}
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.Metadata["image_kind"]]++
	}
	if counts["main"] != 6 || counts["description"] != 1 {
		t.Fatalf("public sample kinds=%v", counts)
	}
	var skuURL string
	walkHTML(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "script" {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			match := dewuStateStart.FindStringIndex(c.Data)
			if c.Type != html.TextNode || match == nil {
				continue
			}
			var state struct {
				Properties struct {
					List []struct {
						URL       string `json:"levelImageUrl"`
						ShowValue int    `json:"showValue"`
					} `json:"list"`
				} `json:"saleProperties"`
			}
			if json.NewDecoder(strings.NewReader(c.Data[match[1]:])).Decode(&state) != nil {
				continue
			}
			for _, prop := range state.Properties.List {
				if prop.ShowValue == 1 && prop.URL != "" {
					skuURL, _ = dewuImageURL(page, prop.URL)
					break
				}
			}
		}
	})
	var skuMatchesMain bool
	for _, item := range got.Items {
		if item.URL == skuURL && item.Metadata["image_kind"] == "main" {
			skuMatchesMain = true
		}
	}
	if !skuMatchesMain {
		t.Fatal("expected the visible variant image to duplicate a main image")
	}
	if os.Getenv("TULAI_DEWU_DOWNLOAD") == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		item := Item{URL: skuURL, Filename: "variant", RequireImage: true, Headers: map[string]string{"Referer": "https://fast.dewu.com/"}}
		dir := t.TempDir()
		if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatalf("public variant download: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("variant files=%v err=%v", entries, err)
		}
	}
}

func TestDewuCurrentPublicPage(t *testing.T) {
	if os.Getenv("TULAI_DEWU_LIVE") != "1" {
		t.Skip("set TULAI_DEWU_LIVE=1 for anonymous public page and image downloads")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	page, _ := url.Parse("https://fast.dewu.com/page/productDetail?spuId=33425209")
	getter := NewHTTPGetter("")
	got, err := discoverDewuProduct(ctx, getter, page)
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("current public product items=%d err=%v", len(got.Items), err)
	}
	downloaded := map[string]bool{}
	for _, item := range got.Items {
		kind := item.Metadata["image_kind"]
		if downloaded[kind] {
			continue
		}
		item.Filename = "image"
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatalf("%s download: %v", kind, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("%s files=%v err=%v", kind, entries, err)
		}
		downloaded[kind] = true
	}
	if !downloaded["main"] || !downloaded["description"] {
		t.Fatalf("downloaded kinds=%v", downloaded)
	}
	t.Logf("public product: %d unique images; downloaded kinds %v (variant duplicates a main image)", len(got.Items), downloaded)
}
