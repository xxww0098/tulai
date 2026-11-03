package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

const dhgatePublicURL = "https://www.dhgate.com/product/for-tiktok-high-quality-designer-sneakers/1018787914.html"

func dhgateFixture(itemID, nestedID string) string {
	value := map[string]any{
		"itemCode": itemID,
		"prodItem": map[string]any{"itemCode": nestedID, "productName": "Product"},
		"prodImg":  map[string]any{"oriImgList": []string{"https://img4.dhresource.com/webp/m/0x0/f3/albu/main.jpg"}},
		"prodSku": map[string]any{"itemAttrList": []any{map[string]any{"itemAttrvalList": []any{
			map[string]any{"attrValName": "Red", "pic0x0Url": "https://img4.dhresource.com/0x0/f3/albu/red.jpg"},
		}}}},
	}
	data, _ := json.Marshal([]any{"$", "div", nil, map[string]any{"children": value}})
	payload := "a:" + string(data) + "\n"
	// Split a single Flight record across script chunks, as the site may do.
	half := len(payload) / 2
	a, _ := json.Marshal([]any{1, payload[:half]})
	b, _ := json.Marshal([]any{1, payload[half:]})
	return `<html><head><title>Page</title></head><body>` +
		`<script>self.__next_f.push(` + string(a) + `)</script><script>self.__next_f.push(` + string(b) + `)</script>` +
		`<div id="descWrap"><div class="product-details"><img src="//www.dhresource.com/webp/m/0x0/f2/albu/detail.jpg"></div>` +
		`<div class="review"><img src="https://img4.dhresource.com/0x0/f3/albu/review.jpg"></div></div>` +
		`<img src="https://img4.dhresource.com/0x0/f3/albu/recommendation.jpg"></body></html>`
}

func dhgateTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDHgateURL(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{dhgatePublicURL, "1018787914"},
		{"https://www.dhgate.com/store/product/example/1018787914.html", "1018787914"},
		{"https://de.dhgate.com/product/example/1018787914.html", "1018787914"},
		{"https://www.dhgate.com/product/example/1018787914.html?token=synthetic", "1018787914"},
		{"https://www.dhgate.com/product/example/1018787914.html/extra", ""},
		{"https://evil.dhgate.com/product/example/1018787914.html", ""},
		{"https://www.dhgate.com.evil.test/product/example/1018787914.html", ""},
		{"https://user@www.dhgate.com/product/example/1018787914.html", ""},
		{"https://www.dhgate.com:8443/product/example/1018787914.html", ""},
		{"https://www.dhgate.com/product/example/1.html", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := dhgateProductID(u); got != tc.id || isDHgateProduct(u) != (tc.id != "") {
			t.Errorf("productID(%q)=%q", tc.raw, got)
		}
	}
}

func TestDHgateImageBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://img4.dhresource.com.evil.test/0x0/f3/albu/a.jpg",
		"https://user@img4.dhresource.com/0x0/f3/albu/a.jpg",
		"https://img4.dhresource.com:8443/0x0/f3/albu/a.jpg",
		"http://img4.dhresource.com/0x0/f3/albu/a.jpg",
		"https://127.0.0.1/0x0/f3/albu/a.jpg",
		"https://img4.dhresource.com/0x0/../private",
		"https://img4.dhresource.com/avatar/a.jpg",
	} {
		if _, ok := dhgateImageURL(raw); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	if _, ok := dhgateImageURL("//img4.dhresource.com/0x0/f3/albu/a.jpg"); !ok {
		t.Fatal("valid image rejected")
	}
}

func TestDHgateFlightImagesAndIdentity(t *testing.T) {
	root, _ := url.Parse(dhgatePublicURL + "?token=synthetic")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return dhgateTestResponse(raw, dhgateFixture("1018787914", "1018787914")), nil
	})
	got, err := discoverDHgateProduct(context.Background(), getter, root)
	if err != nil || got.Title != "Product" || len(got.Items) != 3 {
		t.Fatalf("images=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["dhgate_item_code"] != "1018787914" || item.Headers["Referer"] != "https://www.dhgate.com/" || strings.Contains(item.URL, "review") || strings.Contains(item.URL, "recommendation") {
			t.Fatalf("unexpected image %+v", item)
		}
	}
	if got.Items[1].Metadata["variant"] != "Red" {
		t.Fatalf("missing SKU label: %+v", got.Items[1])
	}
	for _, ids := range [][2]string{{"1018787915", "1018787914"}, {"1018787914", "1018787915"}} {
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			return dhgateTestResponse(raw, dhgateFixture(ids[0], ids[1])), nil
		})
		if got, err := discoverDHgateProduct(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
			t.Fatalf("wrong identity images=%+v err=%v", got.Items, err)
		}
	}
}

func TestDHgatePartialAndFallback(t *testing.T) {
	root, _ := url.Parse(dhgatePublicURL)
	fixture := dhgateFixture("1018787914", "1018787914")
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"missing-description", strings.Replace(fixture, `id="descWrap"`, `id="other"`, 1), 2},
		{"og-product", `<meta property="og:type" content="product"><meta property="og:url" content="` + dhgatePublicURL + `"><meta property="og:image" content="https://img4.dhresource.com/0x0/f3/albu/main.jpg">`, 1},
		{"og-other", `<meta property="og:type" content="product"><meta property="og:url" content="https://www.dhgate.com/product/example/1018787915.html"><meta property="og:image" content="https://img4.dhresource.com/0x0/f3/albu/main.jpg">`, 0},
		{"no-binding", `<img src="https://img4.dhresource.com/0x0/f3/albu/recommendation.jpg">`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return dhgateTestResponse(raw, tc.body), nil
			})
			got, err := discoverDHgateProduct(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("images=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestDHgateCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(dhgatePublicURL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverDHgateProduct(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := dhgateTestResponse(raw, dhgateFixture("1018787914", "1018787914"))
		resp.FinalURL = "https://www.dhgate.com/product/example/1018787915.html"
		return resp, nil
	})
	if got, err := discoverDHgateProduct(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect images=%+v err=%v", got.Items, err)
	}
}

type dhgateRoundTripper func(*http.Request) (*http.Response, error)

func (fn dhgateRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestDHgateRenderedCapture(t *testing.T) {
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
	const target = dhgatePublicURL + "?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = dhgateRoundTripper(func(*http.Request) (*http.Response, error) {
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
				body := dhgateFixture("1018787914", "1018787914")
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
	if err != nil || len(got.Items) != 3 || calls.Load() != 0 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestDHgateLiveProductDownload(t *testing.T) {
	if os.Getenv("TULAI_DHGATE_LIVE") != "1" {
		t.Skip("set TULAI_DHGATE_LIVE=1 to verify the public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, dhgatePublicURL, 1)
	if err != nil && len(got.Items) == 0 {
		t.Skipf("public product did not expose its data: %v", err)
	}
	dhgateAssertPublicImages(t, ctx, getter, got, err)
}

func TestDHgateCapturedProduct(t *testing.T) {
	file := os.Getenv("TULAI_DHGATE_CAPTURED")
	if file == "" {
		t.Skip("set TULAI_DHGATE_CAPTURED to an anonymous public page response")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return dhgateTestResponse(raw, string(body)), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got, err := Discover(ctx, getter, dhgatePublicURL, 1)
	dhgateAssertPublicImages(t, ctx, NewHTTPGetter(""), got, err)
}

func dhgateAssertPublicImages(t *testing.T, ctx context.Context, getter Getter, got Discovery, err error) {
	t.Helper()
	if err != nil || len(got.Items) < 100 {
		t.Fatalf("public product images=%d err=%v", len(got.Items), err)
	}
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.Metadata["image_kind"]]++
	}
	if counts["main"] < 7 || counts["sku"] < 90 || counts["description"] < 3 {
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
