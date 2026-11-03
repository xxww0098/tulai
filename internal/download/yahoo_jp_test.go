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

const yahooJPTestURL = "https://store.shopping.yahoo.co.jp/favras/fn02131.html"

type yahooJPRoundTrip func(*http.Request) (*http.Response, error)

func (f yahooJPRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func yahooJPResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestYahooJPRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, shop, code string }{
		{yahooJPTestURL, "favras", "fn02131"},
		{"http://store.shopping.yahoo.co.jp/real-co/co-263049.html?x=1", "real-co", "co-263049"},
		{"https://store.shopping.yahoo.co.jp/artemis-webshop-2/rwnd.html", "artemis-webshop-2", "rwnd"},
		{"https://store.shopping.yahoo.co.jp/favras/fn02131.html/other", "", ""},
		{"https://store.shopping.yahoo.co.jp/favras/search.html", "", ""},
		{"https://store.shopping.yahoo.co.jp/favras/search.html/", "", ""},
		{"https://store.shopping.yahoo.co.jp.evil.test/favras/fn02131.html", "", ""},
		{"https://user@store.shopping.yahoo.co.jp/favras/fn02131.html", "", ""},
		{"https://store.shopping.yahoo.co.jp:8443/favras/fn02131.html", "", ""},
		{"https://auctions.yahoo.co.jp/jp/auction/12345678", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		shop, code := yahooJPProductID(u)
		if shop != tc.shop || code != tc.code {
			t.Errorf("route %s = %q %q", tc.raw, shop, code)
		}
	}
	page, _ := url.Parse(yahooJPTestURL)
	for _, tc := range []struct{ raw, want, large string }{
		{"//item-shopping.c.yimg.jp/i/n/favras_fn02131", "https://item-shopping.c.yimg.jp/i/n/favras_fn02131", "https://item-shopping.c.yimg.jp/i/f/favras_fn02131"},
		{"https://item-shopping.c.yimg.jp/i/n/favras_fn02131?sig=keep", "https://item-shopping.c.yimg.jp/i/n/favras_fn02131?sig=keep", "https://item-shopping.c.yimg.jp/i/n/favras_fn02131?sig=keep"},
		{"https://shopping.c.yimg.jp/lib/favras/detail.jpg", "https://shopping.c.yimg.jp/lib/favras/detail.jpg", "https://shopping.c.yimg.jp/lib/favras/detail.jpg"},
		{"https://shopping.geocities.jp/favras/item/detail.jpg", "https://shopping.geocities.jp/favras/item/detail.jpg", "https://shopping.geocities.jp/favras/item/detail.jpg"},
		{"https://shopping.c.yimg.jp/lib/other/detail.jpg", "", ""},
		{"https://item-shopping.c.yimg.jp.evil.test/i/n/favras_fn02131", "", ""},
		{"https://user@item-shopping.c.yimg.jp/i/n/favras_fn02131", "", ""},
		{"https://item-shopping.c.yimg.jp:8443/i/n/favras_fn02131", "", ""},
		{"https://item-shopping.c.yimg.jp/i/n/../private", "", ""},
	} {
		got, ok := yahooJPImageURL(page, "favras", tc.raw)
		if got != tc.want || ok != (tc.want != "") || yahooJPLargeImage(got) != tc.large {
			t.Errorf("image %s = %q large=%q ok=%t", tc.raw, got, yahooJPLargeImage(got), ok)
		}
	}
}

func TestYahooJPProductAndDescription(t *testing.T) {
	page, _ := url.Parse(yahooJPTestURL + "?private=source")
	body := `<html><meta property="og:title" content="Sample product"><meta property="og:url" content="https://store.shopping.yahoo.co.jp/favras/fn02131.html"><div id="itmbasic"><ul class="splide__list"><li><img src="data:image/svg+xml,placeholder"><img src="https://item-shopping.c.yimg.jp/i/n/favras_fn02131"></li><li><noscript><img src="https://item-shopping.c.yimg.jp/i/n/favras_fn02131_1"></noscript></li></ul></div><div id="anchor-itemInfo"><script type="text/template" data-testid="freeSpaceData"><div><img src="https://shopping.c.yimg.jp/lib/favras/detail.jpg"><a href="https://store.shopping.yahoo.co.jp/favras/other.html"><img src="https://shopping.c.yimg.jp/lib/favras/recommended.jpg"></a></div></script></div><div id="storeFooter"><script type="text/template" data-testid="freeSpaceData"><img src="https://shopping.c.yimg.jp/lib/favras/footer.jpg"></script></div><img src="https://item-shopping.c.yimg.jp/i/n/other_recommendation"></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return yahooJPResponse(raw, body), nil
	})
	got, err := discoverYahooJPProduct(context.Background(), getter, page)
	if err != nil || got.Title != "Sample product" || len(got.Items) != 3 || got.Items[0].URL != "https://item-shopping.c.yimg.jp/i/f/favras_fn02131" || got.Items[1].URL != "https://item-shopping.c.yimg.jp/i/f/favras_fn02131_1" || got.Items[2].URL != "https://shopping.c.yimg.jp/lib/favras/detail.jpg" || got.Items[2].Metadata["image_kind"] != "description" {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	if len(got.Items[0].AlternateURLs) != 1 || got.Items[0].AlternateURLs[0] != "https://item-shopping.c.yimg.jp/i/n/favras_fn02131" {
		t.Fatalf("fallback=%+v", got.Items[0])
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["yahoo_jp_shop"] != "favras" || item.Metadata["yahoo_jp_item"] != "fn02131" || item.Headers["Referer"] != "https://store.shopping.yahoo.co.jp/" || item.Headers["Cookie"] != "" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestYahooJPPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(yahooJPTestURL)
	for _, mode := range []string{"invalid", "wrong-id", "wrong-origin", "redirect-id", "cancel", "challenge"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				body := `<div id="itmbasic"><ul class="splide__list"><li><img src="https://item-shopping.c.yimg.jp/i/n/favras_fn02131"></li></ul></div>`
				switch mode {
				case "invalid":
					body += `<div id="caption"><img src="https://evil.test/track.jpg"></div>`
				case "wrong-id":
					body += `<meta property="og:url" content="https://store.shopping.yahoo.co.jp/favras/other.html">`
				case "cancel":
					cancel()
				case "challenge":
					body = `<div>Verify</div><img src="https://item-shopping.c.yimg.jp/i/n/other_product">`
				}
				resp := yahooJPResponse(raw, body)
				if mode == "wrong-origin" {
					resp.FinalURL = "https://evil.test/favras/fn02131.html"
				} else if mode == "redirect-id" {
					resp.FinalURL = "https://store.shopping.yahoo.co.jp/favras/other.html"
				}
				return resp, nil
			})
			got, err := discoverYahooJPProduct(ctx, getter, page)
			want := 0
			if mode == "invalid" {
				want = 1
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestYahooJPLegacyProduct(t *testing.T) {
	page, _ := url.Parse(yahooJPTestURL)
	body := `<img class="BasicImage ImageGallery_image" src="https://item-shopping.c.yimg.jp/i/l/favras_fn02131"><div class="CentItemAdditional"><img src="https://shopping.c.yimg.jp/lib/favras/detail.jpg"></div>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return yahooJPResponse(raw, body), nil
	})
	got, err := discoverYahooJPProduct(context.Background(), getter, page)
	if err != nil || len(got.Items) != 2 || got.Items[0].Metadata["image_kind"] != "main" || got.Items[1].Metadata["image_kind"] != "description" {
		t.Fatalf("legacy=%+v err=%v", got, err)
	}
}

func TestYahooJPDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return yahooJPResponse(raw, `<div id="itmbasic"><ul class="splide__list"><li><img src="https://item-shopping.c.yimg.jp/i/n/favras_fn02131"></li></ul></div><img src="https://item-shopping.c.yimg.jp/i/n/other_recommendation">`), nil
	})
	got, err := Discover(context.Background(), getter, yahooJPTestURL, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://item-shopping.c.yimg.jp/i/f/favras_fn02131" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestYahooJPRenderedCapture(t *testing.T) {
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
	const page = `<html><div id="itmbasic"><ul class="splide__list"><li><img src="https://item-shopping.c.yimg.jp/i/n/favras_fn02131"></li></ul></div><div id="anchor-itemInfo"><script type="text/template" data-testid="freeSpaceData"><img src="https://shopping.c.yimg.jp/lib/favras/detail.jpg"></script></div><img src="https://item-shopping.c.yimg.jp/i/n/other_recommendation"></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == yahooJPTestURL {
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
	session.start, _ = url.Parse(yahooJPTestURL)
	if err := session.run(ctx, camoufox.Navigate(yahooJPTestURL)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://store.shopping.yahoo.co.jp/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = yahooJPRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "item-shopping.c.yimg.jp" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://store.shopping.yahoo.co.jp/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://item-shopping.c.yimg.jp/i/f/favras_fn02131" || got.Items[1].Metadata["image_kind"] != "description" || session.PageURL() != yahooJPTestURL {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, func(r *http.Request) { r.Header.Set("Referer", got.Items[0].Headers["Referer"]) })
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestYahooJPLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_YAHOO_JP_LIVE") != "1" {
		t.Skip("set TULAI_YAHOO_JP_LIVE=1 for a public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, yahooJPTestURL, 1)
	if err != nil {
		t.Fatal(err)
	}
	var main, detail *Item
	for i := range got.Items {
		switch got.Items[i].Metadata["image_kind"] {
		case "main":
			if main == nil {
				main = &got.Items[i]
			}
		case "description":
			if detail == nil {
				detail = &got.Items[i]
			}
		}
	}
	if main == nil || detail == nil {
		t.Fatalf("public product missing gallery or description: images=%d", len(got.Items))
	}
	for i, source := range []*Item{main, detail} {
		item := *source
		item.Filename = "image"
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("image %d entries=%v err=%v", i, entries, err)
		}
		if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
			t.Fatalf("empty image %d: %v %v", i, info, err)
		}
	}
	t.Logf("public product images=%d; gallery and description images downloaded", len(got.Items))
}
