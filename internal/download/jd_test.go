package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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

func jdTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

const jdTestPage = `<html><head><title>商品页</title><script>
var pageConfig = {product:{skuid:12345, desc:'ignored'}};
</script></head><body>
<div class="spec-items"><img src="//img12.360buyimg.com/n4/s114x114_jfs/t1/11/main.jpg" data-url="jfs/t1/11/main.jpg"></div>
<div data-sku="variant"><img src="//img11.360buyimg.com/n9/s28x28_jfs/t1/22/sku.jpg"></div>
<div id="J-detail-content"><img data-lazyload="//img13.360buyimg.com/cms/jfs/t1/33/detail.jpg">
<div id="zbViewWeChatMiniImages" value="/sku/jfs/t1/44/mobile.jpg"></div></div>
<img src="https://img10.360buyimg.com/jfs/t1/999/recommend.jpg">
</body></html>`

const jdTestDetail = `<html><body><div id="IntroductionModule">
<style>.picture{background-image:url(//img30.360buyimg.com/sku/jfs/t1/55/background.jpg.dpg)}</style>
<img data-src="//img10.360buyimg.com/cms/jfs/t1/66/subpage.png">
</div><img src="//img10.360buyimg.com/cms/jfs/t1/999/recommend.jpg"></body></html>`

func TestJDProductURLs(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		mobile  bool
	}{
		{"https://item.jd.com/12345.html", "12345", false},
		{"https://item.jd.com/product/MkK-iglOTfFDo3ScfhqyOw.html", "MkK-iglOTfFDo3ScfhqyOw", false},
		{"https://item.m.jd.com/product/100012043978.html", "100012043978", true},
		{"https://i-item.jd.com/12345.html", "12345", false},
		{"https://item.jd.com/product/bad!.html", "", false},
		{"https://item.jd.com.evil.test/12345.html", "", false},
		{"https://user@item.jd.com/12345.html", "", false},
		{"https://item.jd.com:8443/12345.html", "", false},
		{"https://item.jd.com/category/12345.html", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		ref := jdProductRef(u)
		if ref.id != tc.id || ref.mobile != tc.mobile || isJDProduct(u) != (tc.id != "") {
			t.Errorf("ref(%q) = %+v", tc.raw, ref)
		}
	}
}

func TestJDDesktopProductAndDetail(t *testing.T) {
	root, _ := url.Parse("https://item.jd.com/product/exampleToken.html?token=private")
	var requested string
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Hostname() != "in.m.jd.com" {
			return jdTestResponse(raw, jdTestPage), nil
		}
		request, _ := http.NewRequest("GET", raw, nil)
		prepare(request)
		if request.Header.Get("Referer") != "https://item.jd.com/" {
			t.Fatalf("detail referer %q", request.Header.Get("Referer"))
		}
		requested = u.Path
		return jdTestResponse(raw, jdTestDetail), nil
	})
	got, err := discoverJDProduct(context.Background(), getter, root)
	if err != nil || requested != "/product/graphext/12345.html" || len(got.Items) != 6 {
		t.Fatalf("product = %+v, detail %q, err %v", got.Items, requested, err)
	}
	for _, item := range got.Items {
		if strings.Contains(item.URL, "recommend") || strings.Contains(item.URL, ".dpg") || item.Headers["Referer"] != "https://item.jd.com/" {
			t.Fatalf("unexpected image %+v", item)
		}
	}
	if got.Items[0].URL != "https://img10.360buyimg.com/n5/s1440x1440_jfs/t1/11/main.jpg" || !strings.Contains(got.Items[1].URL, "/t1/22/sku.jpg") || !strings.Contains(got.Items[4].URL, "/t1/55/background.jpg") {
		t.Fatalf("unexpected originals %+v", got.Items)
	}
}

func TestJDMobileImagesAndNumericSKU(t *testing.T) {
	root, _ := url.Parse("https://item.m.jd.com/product/12345.html")
	body := `<html><script>window._itemOnly = ({"item":{"skuId":"12345","skuName":"手机商品","image":["jfs/t1/11/main.jpg"],"newColorSize":[{"imagePath":"jfs/t1/22/sku.jpg"}]}});</script></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Hostname() == "in.m.jd.com" {
			return jdTestResponse(raw, jdTestDetail), nil
		}
		return jdTestResponse(raw, body), nil
	})
	got, err := discoverJDProduct(context.Background(), getter, root)
	if err != nil || len(got.Items) != 4 || got.Title != "手机商品" || !strings.Contains(got.Items[1].URL, "/22/sku.jpg") {
		t.Fatalf("mobile = %+v, err %v", got.Items, err)
	}
	body = `<div class="spec-items"><img src="//img12.360buyimg.com/n4/s114x114_jfs/t1/11/main.jpg"></div>`
	got, err = discoverJDProduct(context.Background(), getter, root)
	if err != nil || len(got.Items) != 3 {
		t.Fatalf("numeric URL fallback = %+v, err %v", got.Items, err)
	}
}

func TestJDSecurityAndPartialResults(t *testing.T) {
	root, _ := url.Parse("https://item.jd.com/12345.html")
	for _, hostile := range []string{
		"https://img10.360buyimg.com.evil.test/n0/jfs/t1/x.jpg", "https://user@img10.360buyimg.com/n0/jfs/t1/x.jpg",
		"https://img10.360buyimg.com:8443/n0/jfs/t1/x.jpg", "http://img10.360buyimg.com/n0/jfs/t1/x.jpg",
		"https://127.0.0.1/n0/jfs/t1/x.jpg", "jfs/../../private.jpg",
		"https://img10.360buyimg.com/../jfs/t1/x.jpg",
	} {
		t.Run(hostile, func(t *testing.T) {
			if _, _, ok := jdImageURL(root, hostile, true); ok {
				t.Fatalf("unsafe URL accepted: %s", hostile)
			}
		})
	}
	for _, kind := range []string{"detail 403", "redirect", "empty detail", "wrong sku", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Hostname() != "in.m.jd.com" {
					body := jdTestPage
					if kind == "wrong sku" {
						body = strings.Replace(body, "skuid:12345", "skuid:99999", 1)
					}
					return jdTestResponse(raw, body), nil
				}
				resp := jdTestResponse(raw, jdTestDetail)
				switch kind {
				case "detail 403":
					resp.StatusCode = http.StatusForbidden
				case "redirect":
					resp.FinalURL = "https://evil.test/product/graphext/12345.html"
				case "empty detail":
					resp = jdTestResponse(raw, "<html>not a product</html>")
				case "cancel":
					cancel()
					return nil, context.Canceled
				}
				return resp, nil
			})
			got, err := discoverJDProduct(ctx, getter, root)
			if err == nil || len(got.Items) < 4 {
				t.Fatalf("partial = %+v, err %v", got.Items, err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel = %v", err)
			}
		})
	}
}

func TestJDCancellationAndRootRedirect(t *testing.T) {
	root, _ := url.Parse("https://item.jd.com/12345.html")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discoverJDProduct(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := jdTestResponse(raw, jdTestPage)
		resp.FinalURL = "https://evil.test/12345.html"
		return resp, nil
	})
	if _, err := discoverJDProduct(context.Background(), getter, root); err == nil {
		t.Fatal("external root redirect accepted")
	}
}

func TestJDRenderedCapture(t *testing.T) {
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
	const target = "https://item.jd.com/product/exampleToken.html?token=synthetic"
	var detailCalls atomic.Int32
	session.getter.client.Transport = jdTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "in.m.jd.com" || r.URL.Path != "/product/graphext/12345.html" || r.Header.Get("Cookie") != "" {
			return nil, errors.New("unexpected detail request or cookie leak")
		}
		detailCalls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, Request: r, Body: io.NopCloser(strings.NewReader(jdTestDetail))}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(jdTestPage)))
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
		return network.SetCookie("synthetic_session", "example").WithURL("https://item.jd.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 6 || detailCalls.Load() != 1 {
		t.Fatalf("capture = %+v, detail calls %d, err %v", got.Items, detailCalls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

type jdTestTransport func(*http.Request) (*http.Response, error)

func (f jdTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJDLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_JD_LIVE") != "1" {
		t.Skip("set TULAI_JD_LIVE=1 to check public JD products")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	for _, tc := range []struct {
		name, raw string
		minimum   int
	}{
		{"desktop", "https://item.jd.com/product/MkK-iglOTfFDo3ScfhqyOw.html", 12},
		{"mobile", "https://item.m.jd.com/product/100012043978.html", 10},
	} {
		got, err := Discover(ctx, getter, tc.raw, 1)
		if err != nil || len(got.Items) < tc.minimum {
			t.Fatalf("%s: got %d images, err %v", tc.name, len(got.Items), err)
		}
		if tc.name == "desktop" {
			item := got.Items[0]
			item.RequireImage = true
			if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: t.TempDir()}, item, nil); err != nil {
				t.Fatal(fmt.Errorf("download public JD image: %w", err))
			}
		}
		t.Logf("%s public product: %d images", tc.name, len(got.Items))
	}
}
