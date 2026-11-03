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
	"golang.org/x/net/html"
)

const etsyTestURL = "https://www.etsy.com/listing/87673424/personalized-word-pillow-case-letter"

type etsyRoundTrip func(*http.Request) (*http.Response, error)

func (f etsyRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func etsyResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestEtsyRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{etsyTestURL, "87673424"},
		{"https://www.etsy.com/in-en/listing/87673424/slug?x=1", "87673424"},
		{"https://shopname.etsy.com/listing/87673424/slug", "87673424"},
		{"http://etsy.com/listing/87673424", "87673424"},
		{"https://www.etsy.com/listing/00000000/slug", ""},
		{"https://www.etsy.com/listing/87673424/slug/other", ""},
		{"https://www.etsy.com.evil.test/listing/87673424/slug", ""},
		{"https://user@www.etsy.com/listing/87673424/slug", ""},
		{"https://www.etsy.com:8443/listing/87673424/slug", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := etsyListingID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(etsyTestURL)
	for _, tc := range []struct{ raw, want string }{
		{"//i.etsystatic.com/6088772/r/il/a/il_fullxfull.a.jpg?token=1", "https://i.etsystatic.com/6088772/r/il/a/il_fullxfull.a.jpg?token=1"},
		{"http://i.etsystatic.com/6088772/r/il/a.jpg", "https://i.etsystatic.com/6088772/r/il/a.jpg"},
		{"https://i.etsystatic.com.evil.test/6088772/a.jpg", ""},
		{"https://user@i.etsystatic.com/6088772/a.jpg", ""},
		{"https://i.etsystatic.com:8443/6088772/a.jpg", ""},
		{"https://i.etsystatic.com/6088772/../a.jpg", ""},
		{"data:image/png;base64,AA", ""},
	} {
		got, ok := etsyImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q %t", tc.raw, got, ok)
		}
	}
}

func TestEtsyListingGallery(t *testing.T) {
	page, _ := url.Parse(etsyTestURL + "?private=source")
	body := `<html><title>fallback</title><meta property="og:title" content="Sample listing"><meta property="og:url" content="https://www.etsy.com/in-en/listing/87673424/slug"><div class="listing-page-image-carousel-component"><ul class="carousel-pane-list"><li><img class="carousel-image" data-src-zoom-image="https://i.etsystatic.com/1/r/il/a/il_fullxfull.a.jpg?sig=keep" src="https://i.etsystatic.com/1/r/il/a/il_794xN.a.jpg"></li><li><img class="carousel-image" alt="Blue option" data-src-zoom-image="https://i.etsystatic.com/1/r/il/b/il_fullxfull.b.jpg"></li><li><img class="carousel-image" data-src-zoom-image="https://i.etsystatic.com/1/r/il/b/il_fullxfull.b.jpg"></li></ul></div><img class="carousel-image" data-src-zoom-image="https://i.etsystatic.com/recommended.jpg"></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return etsyResponse(raw, body), nil
	})
	got, err := discoverEtsyProduct(context.Background(), getter, page)
	if err != nil || got.Title != "Sample listing" || len(got.Items) != 2 || got.Items[0].URL != "https://i.etsystatic.com/1/r/il/a/il_fullxfull.a.jpg?sig=keep" || got.Items[1].URL != "https://i.etsystatic.com/1/r/il/b/il_fullxfull.b.jpg" {
		t.Fatalf("listing=%+v err=%v", got, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["etsy_listing_id"] != "87673424" || item.Headers["Referer"] != "https://www.etsy.com/" || item.Headers["Cookie"] != "" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestEtsyPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(etsyTestURL)
	for _, mode := range []string{"preview", "invalid", "wrong-id", "wrong-origin", "redirect-id", "cancel", "challenge"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				body := `<div class="listing-page-image-carousel-component"><img class="carousel-image" data-src-zoom-image="https://i.etsystatic.com/a/full.jpg"></div>`
				switch mode {
				case "preview":
					body = strings.Replace(body, `</div>`, `<img class="carousel-image" data-src-zoom-image="https://evil.test/full.jpg" src="https://i.etsystatic.com/a/thumb.jpg"></div>`, 1)
				case "invalid":
					body = strings.Replace(body, `</div>`, `<img class="carousel-image" data-src-zoom-image="https://evil.test/full.jpg"></div>`, 1)
				case "wrong-id":
					body += `<meta property="og:url" content="https://www.etsy.com/listing/99999999/other">`
				case "cancel":
					cancel()
				case "challenge":
					body = `<div class="captcha__human">Verify</div><img src="https://i.etsystatic.com/recommendation.jpg">`
				}
				resp := etsyResponse(raw, body)
				if mode == "wrong-origin" {
					resp.FinalURL = "https://evil.test/listing/87673424/slug"
				} else if mode == "redirect-id" {
					resp.FinalURL = "https://www.etsy.com/listing/99999999/other"
				}
				return resp, nil
			})
			got, err := discoverEtsyProduct(ctx, getter, page)
			want := 0
			if mode == "preview" {
				want = 2
			} else if mode == "invalid" {
				want = 1
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestEtsyDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return etsyResponse(raw, `<ul class="carousel-pane-list"><li><img class="carousel-image" data-src-zoom-image="https://i.etsystatic.com/1/full.jpg"></li></ul><img src="https://i.etsystatic.com/recommended.jpg">`), nil
	})
	got, err := Discover(context.Background(), getter, etsyTestURL, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://i.etsystatic.com/1/full.jpg" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestEtsyRenderedCapture(t *testing.T) {
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
	const page = `<html><div class="listing-page-image-carousel-component"><img class="carousel-image" data-src-zoom-image="https://i.etsystatic.com/1/full.jpg"><img class="carousel-image" data-src-zoom-image="https://i.etsystatic.com/2/full.jpg"></div><img class="carousel-image" data-src-zoom-image="https://i.etsystatic.com/recommended.jpg"></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == etsyTestURL {
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
	session.start, _ = url.Parse(etsyTestURL)
	if err := session.run(ctx, camoufox.Navigate(etsyTestURL)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.etsy.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = etsyRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "i.etsystatic.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.etsy.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://i.etsystatic.com/1/full.jpg" || session.PageURL() != etsyTestURL {
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

func TestEtsyCapturedPublicListing(t *testing.T) {
	path := os.Getenv("TULAI_ETSY_CAPTURE")
	if path == "" {
		t.Skip("set TULAI_ETSY_CAPTURE to a captured public listing HTML")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc, err := html.Parse(io.LimitReader(file, maxPageBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse("https://www.etsy.com/in-en/listing/87673424/personalized-word-pillow-case-letter")
	got, err := etsyProductPage(context.Background(), doc, page)
	if err != nil || len(got.Items) != 9 || got.Items[0].URL != "https://i.etsystatic.com/6088772/r/il/719b4b/1631899982/il_fullxfull.1631899982_2iay.jpg" {
		t.Fatalf("public listing images=%d err=%v", len(got.Items), err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("empty image: %v %v", info, err)
	}
	t.Logf("public listing images=%d; first image downloaded", len(got.Items))
}

func TestEtsyLiveListing(t *testing.T) {
	if os.Getenv("TULAI_ETSY_LIVE") != "1" {
		t.Skip("set TULAI_ETSY_LIVE=1 for a public listing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := url.Parse("https://www.etsy.com/listing/643372894/handmade-personalized-pottery-mug-custom")
	got, err := discoverEtsyProduct(ctx, NewHTTPGetter(""), page)
	var status *HTTPStatusError
	if errors.As(err, &status) && status.StatusCode == 403 {
		t.Skipf("public listing denied HTTP 403: %v", err)
	}
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("public listing images=%d err=%v", len(got.Items), err)
	}
	t.Logf("public listing images=%d", len(got.Items))
}
