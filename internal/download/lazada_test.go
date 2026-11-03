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

const lazadaSG = "https://www.lazada.sg/products/pdp-i111650098-s23209659764.html"
const lazadaPH = "https://www.lazada.com.ph/products/pdp-i141603072-s161229104.html"
const lazadaFixture = `<html><head><title>Product</title></head><body><script>
var __moduleData__ = {"data":{"root":{"fields":{
"primaryKey":{"itemId":"111650098"},"product":{"title":"Item","desc":"<img src=\"https://ph-live-01.slatic.net/p/dddddddddddddddddddddddddddddddd.jpg\"><img src=\"https://evil.test/p/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee.jpg\">"},
"skuGalleries":{"0":[{"type":"img","src":"//laz-img-sg.alicdn.com/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png"},{"type":"video","src":"//laz-img-sg.alicdn.com/p/ffffffffffffffffffffffffffffffff.mp4"}],"123":[{"type":"img","src":"//laz-img-sg.alicdn.com/p/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.jpg"},{"type":"img","src":"//ph-test-11.slatic.net/original/dddddddddddddddddddddddddddddddd.jpg"}]},
"productOption":{"options":[{"image":"//laz-img-sg.alicdn.com/p/cccccccccccccccccccccccccccccccc.jpg"}],"skuBase":{"properties":[{"values":[{"image":"//laz-img-sg.alicdn.com/p/cccccccccccccccccccccccccccccccc.jpg"}]}],"skus":[{"itemId":"111650098","skuId":"123"}]}},
"skuInfos":{"other":{"itemId":"999999999","image":"//laz-img-sg.alicdn.com/p/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee.jpg"}}
}}}};</script><img src="https://laz-img-sg.alicdn.com/p/ffffffffffffffffffffffffffffffff.png"></body></html>`

func lazadaTestResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestLazadaURLs(t *testing.T) {
	for _, tc := range []struct{ raw, region, id string }{
		{lazadaSG, "sg", "111650098"},
		{"https://www.lazada.sg/products/example-i111650098.html?token=private", "sg", "111650098"},
		{"https://www.lazada.com.ph/products/pdp-i141603072-s161229104.html", "ph", "141603072"},
		{"https://www.lazada.com.my/products/example-i12345.html", "my", "12345"},
		{"https://www.lazada.co.th/products/example-i12345.html", "th", "12345"},
		{"https://www.lazada.co.id/products/example-i12345.html", "id", "12345"},
		{"https://www.lazada.vn/products/example-i12345.html", "vn", "12345"},
		{"https://www.lazada.sg/products/pdp-i111650098.html/other", "", ""},
		{"https://www.lazada.sg/products/unknown.html?itemId=111650098", "", ""},
		{"https://www.lazada.sg.evil.test/products/pdp-i111650098.html", "", ""},
		{"https://user@www.lazada.sg/products/pdp-i111650098.html", "", ""},
		{"https://www.lazada.sg:8443/products/pdp-i111650098.html", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := lazadaPageRef(u)
		if got.region != tc.region || got.id != tc.id || isLazadaProduct(u) != (tc.id != "") {
			t.Errorf("ref(%q)=%+v", tc.raw, got)
		}
	}
}

func TestLazadaImageBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://laz-img-sg.alicdn.com.evil.test/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg",
		"https://user@laz-img-sg.alicdn.com/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg",
		"https://laz-img-sg.alicdn.com:8443/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg",
		"http://laz-img-sg.alicdn.com/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg",
		"https://127.0.0.1/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg",
		"https://laz-img-sg.alicdn.com/p/../private",
		"https://laz-img-sg.alicdn.com/avatar/a.png",
	} {
		if _, _, ok := lazadaImageURL(raw); ok {
			t.Errorf("accepted unsafe image %q", raw)
		}
	}
	if _, _, ok := lazadaImageURL("//laz-img-sg.alicdn.com/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"); !ok {
		t.Fatal("valid image rejected")
	}
}

func TestLazadaProductImagesAndIdentity(t *testing.T) {
	root, _ := url.Parse(lazadaSG + "?secret=synthetic")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return lazadaTestResponse(raw, lazadaFixture), nil
	})
	got, err := discoverLazadaProduct(context.Background(), getter, root)
	if err != nil || got.Title != "Item" || len(got.Items) != 4 {
		t.Fatalf("items=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || strings.Contains(item.URL, "eeeeeeee") || strings.Contains(item.URL, "ffffffff") || item.Headers["Referer"] != "https://www.lazada.sg/" {
			t.Fatalf("unexpected image %+v", item)
		}
	}
	wrong := strings.Replace(lazadaFixture, `"itemId":"111650098"`, `"itemId":"999999999"`, 1)
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return lazadaTestResponse(raw, wrong), nil
	})
	if got, err := discoverLazadaProduct(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("wrong identity=%+v err=%v", got, err)
	}
}

func TestLazadaStructuredFallbackAndPartial(t *testing.T) {
	root, _ := url.Parse(lazadaSG)
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"product-ld", `<script type="application/ld+json">{"@type":"Product","url":"https://www.lazada.sg/products/example-i111650098.html","image":["//laz-img-sg.alicdn.com/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"]}</script>`, 1},
		{"wrong-ld", `<script type="application/ld+json">{"@type":"Product","url":"https://www.lazada.sg/products/example-i999999999.html","image":["//laz-img-sg.alicdn.com/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg"]}</script>`, 0},
		{"og", `<meta property="og:url" content="https://www.lazada.sg/products/example-i111650098.html"><meta property="og:image" content="https://img.lazcdn.com/g/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg">`, 1},
		{"unbound-og", `<meta property="og:image" content="https://img.lazcdn.com/g/p/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg">`, 0},
		{"deferred-desc", strings.Replace(lazadaFixture, `"primaryKey":{"itemId":"111650098"}`, `"pageUrl":"https://example.test/pc?pageId=111650098","primaryKey":{"itemId":"111650098"}`, 1), 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return lazadaTestResponse(raw, tc.body), nil
			})
			got, err := discoverLazadaProduct(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestLazadaCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(lazadaSG)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discoverLazadaProduct(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := lazadaTestResponse(raw, lazadaFixture)
		resp.FinalURL = "https://evil.test/products/pdp-i111650098.html"
		return resp, nil
	})
	if got, err := discoverLazadaProduct(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect=%+v err=%v", got, err)
	}
}

type lazadaRoundTripper func(*http.Request) (*http.Response, error)

func (fn lazadaRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestLazadaRenderedCapture(t *testing.T) {
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
	const target = lazadaSG + "?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = lazadaRoundTripper(func(*http.Request) (*http.Response, error) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(lazadaFixture)))
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
	if err != nil || len(got.Items) != 4 || calls.Load() != 0 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestLazadaLiveProductDownload(t *testing.T) {
	if os.Getenv("TULAI_LAZADA_LIVE") != "1" {
		t.Skip("set TULAI_LAZADA_LIVE=1 to verify a public Lazada product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, lazadaSG, 1)
	if err != nil || len(got.Items) < 9 {
		t.Fatalf("product images=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "product"
	item.RequireImage = true
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
	t.Logf("public product images=%d first=%dx%d %s", len(got.Items), im.Width, im.Height, format)
}

func TestLazadaLiveDescriptionImages(t *testing.T) {
	if os.Getenv("TULAI_LAZADA_LIVE") != "1" {
		t.Skip("set TULAI_LAZADA_LIVE=1 to verify public description images")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), lazadaPH, 1)
	if err != nil || len(got.Items) < 7 {
		t.Fatalf("product images=%d err=%v", len(got.Items), err)
	}
	found := false
	for _, item := range got.Items {
		if strings.Contains(item.URL, "e73354eb7505d93180bc699abc6279d5") {
			found = true
		}
	}
	if !found {
		t.Fatal("description image was not collected")
	}
	t.Logf("public product images=%d including description", len(got.Items))
}
