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
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

type ebayRoundTrip func(*http.Request) (*http.Response, error)

func (fn ebayRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

const ebayTestURL = "https://www.ebay.com/itm/123456789012"

func ebayResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestEbayRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{ebayTestURL, "123456789012"},
		{"https://www.ebay.co.uk/itm/product-name/123456789012?var=1", "123456789012"},
		{"https://cafr.ebay.ca/itm/123456789012", "123456789012"},
		{"https://www.ebay.com/itm/000000000000", ""},
		{"https://www.ebay.com/itm/123456789012/other", ""},
		{"https://www.ebay.com.evil.test/itm/123456789012", ""},
		{"https://user@www.ebay.com/itm/123456789012", ""},
		{"https://www.ebay.com:8443/itm/123456789012", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := ebayItemID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(ebayTestURL)
	for _, tc := range []struct{ raw, want string }{
		{"//i.ebayimg.com/images/g/A/s-l500.jpg?k=1", "https://i.ebayimg.com/images/g/A/s-l500.jpg?k=1"},
		{"https://i.ebayimg.com/thumbs/images/g/A/s-l500.jpg", "https://i.ebayimg.com/thumbs/images/g/A/s-l500.jpg"},
		{"https://i.ebayimg.com.evil.test/images/g/A/s-l500.jpg", ""},
		{"https://user@i.ebayimg.com/images/g/A/s-l500.jpg", ""},
		{"https://i.ebayimg.com:8443/images/g/A/s-l500.jpg", ""},
		{"https://i.ebayimg.com/images/../private", ""},
		{"https://i.ebayimg.com/other/a.jpg", ""},
	} {
		got, ok := ebayImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q %t", tc.raw, got, ok)
		}
	}
}

func TestEbayProductAndDescription(t *testing.T) {
	page, _ := url.Parse(ebayTestURL + "?private=source")
	body := `<html><title>fallback</title><h1 class="x-item-title__mainTitle">商品</h1><div class="ux-image-carousel-container"><div class="ux-image-carousel-item active"><img data-zoom-src="https://i.ebayimg.com/images/g/A/s-l1600.webp" src="https://i.ebayimg.com/thumbs/images/g/A/s-l500.jpg"></div><div class="ux-image-carousel-item"><img data-zoom-src="https://evil.test/b.jpg" data-src="https://i.ebayimg.com/images/g/B/s-l500.jpg?token=1"></div></div><iframe id="desc_ifr" src="https://itm.ebaydesc.com/itmdesc/123456789012?x=1"></iframe><img src="https://i.ebayimg.com/images/g/recommended/s-l1600.jpg"></html>`
	var calls int
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		u, _ := url.Parse(raw)
		if u.Host == "www.ebay.com" {
			return ebayResponse(raw, body), nil
		}
		req, _ := http.NewRequest("GET", raw, nil)
		prepare(req)
		if u.Host != "itm.ebaydesc.com" || u.Path != "/itmdesc/123456789012" || req.Header.Get("Referer") != "https://www.ebay.com/" || req.Header.Get("Cookie") != "" {
			t.Fatalf("unsafe detail request %s %v", u, req.Header)
		}
		return ebayResponse(raw, `<html><img src="https://i.ebayimg.com/images/g/C/s-l500.jpg"></html>`), nil
	})
	got, err := discoverEbayProduct(context.Background(), getter, page)
	if err != nil || calls != 2 || got.Title != "商品" || len(got.Items) != 3 || got.Items[0].URL != "https://i.ebayimg.com/images/g/A/s-l1600.webp" || got.Items[1].URL != "https://i.ebayimg.com/images/g/B/s-l1600.jpg?token=1" || got.Items[2].Metadata["image_kind"] != "detail" {
		t.Fatalf("product=%+v calls=%d err=%v", got, calls, err)
	}
	if len(got.Items[1].AlternateURLs) != 1 || got.Items[1].AlternateURLs[0] != "https://i.ebayimg.com/images/g/B/s-l500.jpg?token=1" {
		t.Fatalf("fallback=%+v", got.Items[1])
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Headers["Referer"] != "https://www.ebay.com/" || item.Headers["Cookie"] != "" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestEbayDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return ebayResponse(raw, `<div class="ux-image-carousel-container"><div class="ux-image-carousel-item"><img src="https://i.ebayimg.com/images/g/A/s-l1600.jpg"></div></div><img src="https://i.ebayimg.com/images/g/recommended/s-l1600.jpg">`), nil
	})
	got, err := Discover(context.Background(), getter, ebayTestURL, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://i.ebayimg.com/images/g/A/s-l1600.jpg" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestEbayPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(ebayTestURL)
	for _, mode := range []string{"variants", "invalid-image", "bad-desc-host", "bad-desc-id", "desc-fail", "desc-redirect", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Host == "www.ebay.com" {
					body := `<div class="ux-image-carousel-container"><div class="ux-image-carousel-item"><img src="https://i.ebayimg.com/images/g/A/s-l500.jpg"></div></div><iframe id="desc_ifr" src="https://itm.ebaydesc.com/itmdesc/123456789012"></iframe>`
					switch mode {
					case "variants":
						body += `<div class="x-msku"><select></select></div>`
					case "invalid-image":
						body = strings.Replace(body, `</div></div>`, `<img src="https://evil.test/a.jpg"></div></div>`, 1)
					case "bad-desc-host":
						body = strings.Replace(body, "itm.ebaydesc.com", "evil.test", 1)
					case "bad-desc-id":
						body = strings.Replace(body, "/itmdesc/123456789012", "/itmdesc/999999999999", 1)
					case "cancel":
						cancel()
					}
					return ebayResponse(raw, body), nil
				}
				if mode == "desc-fail" {
					return nil, errors.New("description unavailable")
				}
				resp := ebayResponse(raw, `<div>no images</div>`)
				if mode == "desc-redirect" {
					resp.FinalURL = "https://evil.test/itmdesc/123456789012"
				}
				return resp, nil
			})
			got, err := discoverEbayProduct(ctx, getter, page)
			want := 1
			if mode == "cancel" {
				want = 0
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestEbayRenderedCapture(t *testing.T) {
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
	const page = `<html><div class="ux-image-carousel-container"><div class="ux-image-carousel-item"><img data-zoom-src="https://i.ebayimg.com/images/g/A/s-l1600.jpg"></div></div><iframe id="desc_ifr" src="https://itm.ebaydesc.com/itmdesc/123456789012"></iframe><img src="https://i.ebayimg.com/images/g/recommended/s-l1600.jpg"></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == ebayTestURL {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(page)))
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
	session.start, _ = url.Parse(ebayTestURL)
	if err := session.run(ctx, camoufox.Navigate(ebayTestURL)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.ebay.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = ebayRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.ebay.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		body, ct := "synthetic image", "image/jpeg"
		if r.URL.Host == "itm.ebaydesc.com" {
			body, ct = `<img src="https://i.ebayimg.com/images/g/B/s-l500.jpg">`, "text/html"
		} else if r.URL.Host != "i.ebayimg.com" {
			t.Errorf("unexpected host %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {ct}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://i.ebayimg.com/images/g/A/s-l1600.jpg" || got.Items[1].Metadata["image_kind"] != "detail" {
		t.Fatalf("capture=%+v err=%v", got, err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestEbayLiveDescription(t *testing.T) {
	if os.Getenv("TULAI_EBAY_LIVE") != "1" {
		t.Skip("set TULAI_EBAY_LIVE=1 for public eBay description")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := url.Parse("https://www.ebay.com/itm/124872065058")
	desc, _ := url.Parse("https://itm.ebaydesc.com/itmdesc/124872065058")
	getter := NewHTTPGetter("")
	var items []Item
	err := ebayDescription(ctx, getter, page, desc, "124872065058", func(raw, kind string, _ bool) {
		if image, ok := ebayImageURL(page, raw); ok {
			items = append(items, Item{URL: image, RequireImage: true, Filename: "image"})
		}
	})
	if err != nil || len(items) == 0 {
		t.Fatalf("description images=%d err=%v", len(items), err)
	}
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, items[0], nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("empty image: %v %v", info, err)
	}
	t.Logf("public seller description images=%d", len(items))
}

func TestEbayLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_EBAY_LIVE") != "1" {
		t.Skip("set TULAI_EBAY_LIVE=1 for public eBay product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := url.Parse("https://www.ebay.com/itm/254200737718")
	got, err := discoverEbayProduct(ctx, NewHTTPGetter(""), page)
	var status *HTTPStatusError
	if errors.As(err, &status) && status.StatusCode == 403 {
		t.Skipf("public product denied HTTP 403: %v", err)
	}
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("product images=%d err=%v", len(got.Items), err)
	}
	t.Logf("public product images=%d", len(got.Items))
}
