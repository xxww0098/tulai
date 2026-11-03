package download

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"image"
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
)

const dangdangPublicURL = "https://product.dangdang.com/28518072.html"
const dangdangFixture = `<html><head><title>Title</title></head><body>
<script>var prodSpuInfo = {"productId":"28518072","productName":"书名","template":"publish","describeMap":"1603:1","shopId":"0","categoryPath":"01.21.03.01.00.00"};</script>
<ul id="main-img-slider"><li><a data-imghref="//img3m2.ddimg.cn/33/26/28518072-1_u_1.jpg"></a></li></ul>
<ul class="list_c"><li><a title="红色"><img src="https://img3m2.ddimg.cn/33/26/28518072-2_b_1.jpg"></a></li></ul>
<img src="https://img3m2.ddimg.cn/33/26/recommendation.jpg"></body></html>`
const dangdangDetailFixture = `{"errMsg":"","data":{"html":"<div><a><img src=\"https://img3m2.ddimg.cn/33/26/recommendation.jpg\"></a><p><img data-original=\"http://img52.ddimg.cn/99999990598396082.jpg_.webp\"></p></div>"}}`

func dangdangTestResponse(raw, body, contentType string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDangdangURLs(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{dangdangPublicURL, "28518072"},
		{dangdangPublicURL + "?token=synthetic", "28518072"},
		{"https://product.dangdang.com/1.html", ""},
		{"https://product.dangdang.com/28518072.html/extra", ""},
		{"https://product.dangdang.com.evil.test/28518072.html", ""},
		{"https://user@product.dangdang.com/28518072.html", ""},
		{"https://product.dangdang.com:8443/28518072.html", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := dangdangProductID(u); got != tc.id || isDangdangProduct(u) != (tc.id != "") {
			t.Errorf("productID(%q)=%q", tc.raw, got)
		}
	}
}

func TestDangdangImageBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://img3m2.ddimg.cn.evil.test/33/26/a.jpg",
		"https://user@img3m2.ddimg.cn/33/26/a.jpg",
		"https://img3m2.ddimg.cn:8443/33/26/a.jpg",
		"https://127.0.0.1/33/26/a.jpg",
		"https://img3m2.ddimg.cn/33/../private.jpg",
	} {
		if _, ok := dangdangImageURL(raw, false); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	if got, ok := dangdangImageURL("http://img52.ddimg.cn/99999990598396082.jpg_.webp", false); !ok || got != "https://img52.ddimg.cn/99999990598396082.jpg" {
		t.Fatalf("description image=%q ok=%v", got, ok)
	}
	if got, ok := dangdangImageURL("//img3m2.ddimg.cn/33/26/28518072-2_b_1.jpg", true); !ok || !strings.Contains(got, "_u_") {
		t.Fatalf("sku image=%q ok=%v", got, ok)
	}
}

func dangdangCheckEndpoint(t *testing.T, raw string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "product.dangdang.com" || u.Path != "/index.php" {
		t.Fatalf("unsafe endpoint %q err=%v", raw, err)
	}
	q := u.Query()
	if len(q) != 6 || q.Get("r") != "callback/detail" || q.Get("productId") != "28518072" || q.Get("templateType") != "publish" || q.Get("describeMap") != "1603:1" || q.Get("shopId") != "0" || q.Get("categoryPath") != "01.21.03.01.00.00" {
		t.Fatalf("unexpected detail params %v", q)
	}
}

func TestDangdangProductImagesAndDetail(t *testing.T) {
	root, _ := url.Parse(dangdangPublicURL + "?token=synthetic")
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		if calls == 1 {
			return dangdangTestResponse(raw, dangdangFixture, "text/html"), nil
		}
		dangdangCheckEndpoint(t, raw)
		return dangdangTestResponse(raw, dangdangDetailFixture, "application/json"), nil
	})
	got, err := discoverDangdangProduct(context.Background(), getter, root)
	if err != nil || got.Title != "书名" || len(got.Items) != 3 || calls != 2 {
		t.Fatalf("images=%+v title=%q calls=%d err=%v", got.Items, got.Title, calls, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["dangdang_product_id"] != "28518072" || item.Headers["Referer"] != "https://product.dangdang.com/" || strings.Contains(item.URL, "recommendation") {
			t.Fatalf("unexpected image %+v", item)
		}
	}
	if got.Items[1].Metadata["variant"] != "红色" || !strings.Contains(got.Items[1].URL, "_u_") || got.Items[2].URL != "https://img52.ddimg.cn/99999990598396082.jpg" {
		t.Fatalf("unexpected sku/description %+v", got.Items)
	}
}

func TestDangdangIdentityAndPartial(t *testing.T) {
	root, _ := url.Parse(dangdangPublicURL)
	for _, tc := range []struct {
		name, body string
		failDetail bool
		count      int
	}{
		{"wrong-id", strings.Replace(dangdangFixture, `"productId":"28518072"`, `"productId":"28518073"`, 1), false, 0},
		{"unsafe-params", strings.Replace(dangdangFixture, `"shopId":"0"`, `"shopId":"0&evil=1"`, 1), false, 2},
		{"detail-failed", dangdangFixture, true, 2},
		{"script-literal", `<script>var x = 'var prodSpuInfo = {}';</script><ul id="main-img-slider"><a data-imghref="https://img3m2.ddimg.cn/33/26/wrong.jpg"></a></ul>`, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				if calls == 1 {
					return dangdangTestResponse(raw, tc.body, "text/html"), nil
				}
				if !tc.failDetail {
					t.Fatal("unexpected detail request")
				}
				return nil, errors.New("unavailable")
			})
			got, err := discoverDangdangProduct(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("images=%+v calls=%d err=%v", got.Items, calls, err)
			}
		})
	}
}

func TestDangdangDetailRedirectAndLimit(t *testing.T) {
	root, _ := url.Parse(dangdangPublicURL)
	for _, tc := range []string{"redirect", "large"} {
		t.Run(tc, func(t *testing.T) {
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				if calls == 1 {
					return dangdangTestResponse(raw, dangdangFixture, "text/html"), nil
				}
				dangdangCheckEndpoint(t, raw)
				if tc == "redirect" {
					resp := dangdangTestResponse(raw, dangdangDetailFixture, "application/json")
					resp.FinalURL = "https://evil.test/index.php"
					return resp, nil
				}
				return dangdangTestResponse(raw, strings.Repeat("x", maxPageBytes+1), "application/json"), nil
			})
			got, err := discoverDangdangProduct(context.Background(), getter, root)
			if err == nil || len(got.Items) != 2 {
				t.Fatalf("images=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestDangdangCancellation(t *testing.T) {
	root, _ := url.Parse(dangdangPublicURL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverDangdangProduct(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

type dangdangRoundTripper func(*http.Request) (*http.Response, error)

func (fn dangdangRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestDangdangRenderedCapture(t *testing.T) {
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
	const target = dangdangPublicURL + "?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = dangdangRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		dangdangCheckEndpoint(t, r.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(dangdangDetailFixture)), Request: r}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(dangdangFixture)))
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
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || calls.Load() != 1 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestDangdangLiveProductDownload(t *testing.T) {
	if os.Getenv("TULAI_DANGDANG_LIVE") != "1" {
		t.Skip("set TULAI_DANGDANG_LIVE=1 to verify a public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, dangdangPublicURL, 1)
	if err != nil || len(got.Items) < 7 {
		t.Fatalf("public product images=%d err=%v", len(got.Items), err)
	}
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.Metadata["image_kind"]]++
		if strings.Contains(item.URL, "30105969") {
			t.Fatalf("recommendation included: %s", item.URL)
		}
	}
	if counts["main"] < 6 || counts["description"] < 1 {
		t.Fatalf("missing image types: %v", counts)
	}
	item := got.Items[0]
	item.Filename = "product"
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("files=%v err=%v", entries, err)
	}
	f, err := os.Open(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, format, err := image.DecodeConfig(f)
	if err != nil || im.Width == 0 || im.Height == 0 {
		t.Fatalf("image=%+v format=%q err=%v", im, format, err)
	}
	t.Logf("public product images=%d categories=%v first=%dx%d %s", len(got.Items), counts, im.Width, im.Height, format)
}
