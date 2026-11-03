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

const douyinProductPublicURL = "https://haohuo.jinritemai.com/ecommerce/trade/detail/index.html?id=3453769915167842926"
const douyinProductFixture = `<html><body>
<span class="title-info__text">商品名称</span>
<div class="head-figure__media-view__content" style="background-image: url(&quot;//p9-aio.ecombdimg.com/obj/temai/main&quot;)"></div>
<div class="head-figure__media-view__content" style="background-image: url('https://p3-aio.ecombdimg.com/obj/temai/second')"></div>
<div class="product-big-img-list"><div><img src="https://p6-aio.ecombdimg.com/obj/ecom-shop-material/detail"></div><img src="https://p6-aio.ecombdimg.com/obj/ecom-shop-material/detail"><img src="https://evil.example/obj/temai/wrong"></div>
<img src="https://p9-aio.ecombdimg.com/obj/temai/recommendation">
</body></html>`

func TestDouyinProductURL(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{douyinProductPublicURL, true},
		{douyinProductPublicURL + "&product_id=3453769915167842926&token=synthetic", true},
		{douyinProductPublicURL + "&product_id=3453769915167842927", false},
		{douyinProductPublicURL + "&id=3453769915167842927", false},
		{"https://haohuo.jinritemai.com/views/product/item2?id=3453769915167842926", false},
		{"https://haohuo.jinritemai.com.evil.test/ecommerce/trade/detail/index.html?id=3453769915167842926", false},
		{"https://user@haohuo.jinritemai.com/ecommerce/trade/detail/index.html?id=3453769915167842926", false},
		{"https://haohuo.jinritemai.com:8443/ecommerce/trade/detail/index.html?id=3453769915167842926", false},
		{"https://haohuo.jinritemai.com/ecommerce/trade/detail/index.html?id=0", false},
		{"https://haohuo.jinritemai.com/ecommerce/trade/detail/index.html?id=123%ZZ", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			if tc.ok {
				t.Fatalf("parse %q: %v", tc.raw, err)
			}
			continue
		}
		if isDouyinProduct(u) != tc.ok {
			t.Errorf("isDouyinProduct(%q)=%v", tc.raw, isDouyinProduct(u))
		}
	}
}

func TestDouyinProductImageBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://p9-aio.ecombdimg.com.evil.test/obj/temai/a",
		"https://user@p9-aio.ecombdimg.com/obj/temai/a",
		"https://p9-aio.ecombdimg.com:8443/obj/temai/a",
		"http://p9-aio.ecombdimg.com/obj/temai/a",
		"https://127.0.0.1/obj/temai/a",
		"https://p9-aio.ecombdimg.com/obj/../private",
		"https://p9-aio.ecombdimg.com/avatar/a",
	} {
		if _, ok := douyinProductImageURL(raw); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	if _, ok := douyinProductImageURL("//p9-aio.ecombdimg.com/obj/temai/a"); !ok {
		t.Fatal("valid image rejected")
	}
}

func douyinProductTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDouyinProductDOMAndPartial(t *testing.T) {
	root, _ := url.Parse(douyinProductPublicURL + "&token=synthetic")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return douyinProductTestResponse(raw, douyinProductFixture), nil
	})
	got, err := discoverDouyinProduct(context.Background(), getter, root)
	if err == nil || got.Title != "商品名称" || len(got.Items) != 3 {
		t.Fatalf("images=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["douyin_product_id"] != "3453769915167842926" || item.Headers["Referer"] != "https://haohuo.jinritemai.com/" || strings.Contains(item.URL, "recommendation") || strings.Contains(item.URL, "evil") {
			t.Fatalf("unexpected item %+v", item)
		}
	}
}

func TestDouyinProductSecurityAndEmptyPage(t *testing.T) {
	root, _ := url.Parse(douyinProductPublicURL)
	for _, tc := range []struct{ name, body string }{
		{"risk", `<div class="err-reason-text">检测到当前环境存在安全风险</div>` + douyinProductFixture},
		{"login", `<div class="login-page"></div>` + douyinProductFixture},
		{"empty", `<title>商品详情</title><img src="https://p9-aio.ecombdimg.com/obj/temai/recommendation">`},
		{"meta", `<meta property="og:image" content="https://p9-aio.ecombdimg.com/obj/temai/recommendation">`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return douyinProductTestResponse(raw, tc.body), nil
			})
			got, err := discoverDouyinProduct(context.Background(), getter, root)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("images=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestDouyinProductCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(douyinProductPublicURL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverDouyinProduct(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		response := douyinProductTestResponse(raw, douyinProductFixture)
		response.FinalURL = "https://haohuo.jinritemai.com/ecommerce/trade/detail/index.html?id=3453769915167842927"
		return response, nil
	})
	if got, err := discoverDouyinProduct(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect images=%+v err=%v", got.Items, err)
	}
}

type douyinProductRoundTripper func(*http.Request) (*http.Response, error)

func (fn douyinProductRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestDouyinProductRenderedCapture(t *testing.T) {
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
	const target = douyinProductPublicURL + "&token=synthetic"
	const blockedTarget = target + "&fixture=verification"
	var calls atomic.Int32
	session.getter.client.Transport = douyinProductRoundTripper(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected HTTP request")
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && (paused.Request.URL == target || paused.Request.URL == blockedTarget) {
				body := douyinProductFixture
				if paused.Request.URL == blockedTarget {
					body = `<div class="err-reason-text">检测到当前环境存在安全风险</div>` + body
				}
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
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
	if err == nil || len(got.Items) != 3 || calls.Load() != 0 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	if err := session.run(ctx, camoufox.Navigate(blockedTarget)); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "安全验证") || len(got.Items) != 0 || calls.Load() != 0 {
		t.Fatalf("verification capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestDouyinProductLiveShell(t *testing.T) {
	if os.Getenv("TULAI_DOUYIN_PRODUCT_LIVE") != "1" {
		t.Skip("set TULAI_DOUYIN_PRODUCT_LIVE=1 to verify a public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), douyinProductPublicURL, 1)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("public static page images=%+v err=%v", got.Items, err)
	}
	t.Logf("public product page did not expose its gallery to an anonymous HTTP request: %v", err)
}

func TestDouyinProductLiveImageCDN(t *testing.T) {
	if os.Getenv("TULAI_DOUYIN_PRODUCT_LIVE") != "1" {
		t.Skip("set TULAI_DOUYIN_PRODUCT_LIVE=1 to verify the public image CDN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw := "https://p9-aio.ecombdimg.com/obj/temai/1122d7e59c000e993781346b2ba925a1www800-800"
	imageURL, ok := douyinProductImageURL(raw)
	if !ok {
		t.Fatal("known public image rejected")
	}
	dir := t.TempDir()
	item := Item{URL: imageURL, Filename: "product", RequireImage: true}
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
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
	t.Logf("reference CDN image: %dx%d %s", im.Width, im.Height, format)
}
