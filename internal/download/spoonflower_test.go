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

const spoonflowerPublicDesign = "https://www.spoonflower.com/en/fabric/10112497-large-scale-alien-abduction-toile-de-jouy-pattern-in-blue-by-somecallmebeth"
const spoonflowerPublicDesign2 = "https://www.spoonflower.com/en/fabric/22005480"
const spoonflowerPublicShop = "https://www.spoonflower.com/en/shop?on=fabric"
const spoonflowerFixture = `{"data":{"id":10112497,"name":"Design name","for_sale":true,"thumbnail_url":"https://garden.spoonflower.com/c/10112497/i/s/test/10112497.jpg"}}`
const spoonflowerShopFixture = `<html><head><title>Shop</title></head><body>
<a href="/en/fabric/10112497-pattern"><img src="https://img.spoonflower.com/c/10112497/p/f/m/abc/10112497.png"></a>
<a href="/en/wallpaper/22005480"><img src="https://img.spoonflower.com/c/22005480/p/f/m/def/22005480.png"></a>
<a href="/en/fabric/10112497-pattern"><img src="https://img.spoonflower.com/c/10112497/p/f/s/abc/10112497.png"></a>
<a href="/en/fabric/22005481"><img src="https://img.spoonflower.com/c/22005482/p/f/m/def/22005482.png"></a>
<a href="https://evil.test/en/fabric/123"><img src="https://img.spoonflower.com/c/123/p/f/m/def/123.png"></a>
<img src="https://img.spoonflower.com/c/123/p/f/m/avatar/123.png"></body></html>`

func spoonflowerResponse(raw string, status int, contentType, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestSpoonflowerURLs(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{spoonflowerPublicDesign, true},
		{spoonflowerPublicDesign2, true},
		{"https://www.spoonflower.com/en/wallpaper/10112497-pattern?sku=x", true},
		{spoonflowerPublicShop, true},
		{"https://www.spoonflower.com/profiles/brodesigns", true},
		{"https://www.spoonflower.com/en/fabric/0", false},
		{"https://www.spoonflower.com/en/fabric/123/extra", false},
		{"https://www.spoonflower.com/en/fabric-shop", false},
		{"https://www.spoonflower.com.evil.test/en/fabric/10112497", false},
		{"https://user@www.spoonflower.com/en/fabric/10112497", false},
		{"https://www.spoonflower.com:8443/en/fabric/10112497", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if isSpoonflowerPage(u) != tc.ok {
			t.Errorf("isSpoonflowerPage(%q)=%v", tc.raw, isSpoonflowerPage(u))
		}
	}
}

func TestSpoonflowerImageBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://garden.spoonflower.com.evil.test/c/10112497/i/s/test/a.jpg",
		"https://user@garden.spoonflower.com/c/10112497/i/s/test/a.jpg",
		"https://garden.spoonflower.com:8443/c/10112497/i/s/test/a.jpg",
		"http://garden.spoonflower.com/c/10112497/i/s/test/a.jpg",
		"https://garden.spoonflower.com/c/999/i/s/test/a.jpg",
		"https://garden.spoonflower.com/c/10112497/i/s/../a.jpg",
		"https://127.0.0.1/c/10112497/i/s/test/a.jpg",
	} {
		if _, ok := spoonflowerImageURL(raw, "garden.spoonflower.com", "10112497", "i"); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	if _, ok := spoonflowerImageURL("//garden.spoonflower.com/c/10112497/i/s/test/a.jpg", "garden.spoonflower.com", "10112497", "i"); !ok {
		t.Fatal("valid image rejected")
	}
}

func TestSpoonflowerDesignAndIdentity(t *testing.T) {
	root, _ := url.Parse(spoonflowerPublicDesign + "?token=synthetic")
	var calls int
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		req.Header.Set("Cookie", "synthetic")
		req.Header.Set("Authorization", "synthetic")
		prepare(req)
		if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Referer") != "" || strings.Contains(raw, "token=synthetic") {
			t.Fatalf("credentials leaked: %s %+v", raw, req.Header)
		}
		switch raw {
		case spoonflowerAPI + "10112497":
			return spoonflowerResponse(raw, http.StatusOK, "application/json", spoonflowerFixture), nil
		case spoonflowerLegacyThumbnail("10112497"):
			if req.Header.Get("Range") != "bytes=0-511" {
				t.Fatal("missing bounded thumbnail probe")
			}
			return spoonflowerResponse(raw, http.StatusPartialContent, "image/jpeg", "\xff\xd8\xff\xe0abc"), nil
		default:
			t.Fatalf("unexpected request %q", raw)
			return nil, nil
		}
	})
	got, err := discoverSpoonflower(context.Background(), getter, root)
	if err != nil || got.Title != "Design name" || len(got.Items) != 1 || calls != 2 {
		t.Fatalf("images=%+v title=%q calls=%d err=%v", got.Items, got.Title, calls, err)
	}
	item := got.Items[0]
	if item.URL != spoonflowerLegacyThumbnail("10112497") || !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://www.spoonflower.com/" || item.Metadata["spoonflower_design_id"] != "10112497" {
		t.Fatalf("unexpected item %+v", item)
	}
	wrong := strings.Replace(spoonflowerFixture, `"id":10112497`, `"id":10112498`, 1)
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw != spoonflowerAPI+"10112497" {
			t.Fatalf("unexpected probe after invalid identity: %s", raw)
		}
		return spoonflowerResponse(raw, http.StatusOK, "application/json", wrong), nil
	})
	if got, err := discoverSpoonflower(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("wrong identity images=%+v err=%v", got.Items, err)
	}
}

func TestSpoonflowerPreviewFallbackAndPrivate(t *testing.T) {
	root, _ := url.Parse(spoonflowerPublicDesign)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == spoonflowerAPI+"10112497" {
			return spoonflowerResponse(raw, http.StatusOK, "application/json", spoonflowerFixture), nil
		}
		return spoonflowerResponse(raw, http.StatusForbidden, "text/html", "blocked"), nil
	})
	got, err := discoverSpoonflower(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != "https://garden.spoonflower.com/c/10112497/i/s/test/10112497.jpg" {
		t.Fatalf("fallback images=%+v err=%v", got.Items, err)
	}
	private := strings.Replace(spoonflowerFixture, `"for_sale":true`, `"for_sale":false`, 1)
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw != spoonflowerAPI+"10112497" {
			t.Fatal("unexpected private thumbnail request")
		}
		return spoonflowerResponse(raw, http.StatusOK, "application/json", private), nil
	})
	if got, err := discoverSpoonflower(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("private images=%+v err=%v", got.Items, err)
	}
}

func TestSpoonflowerShopPartial(t *testing.T) {
	root, _ := url.Parse(spoonflowerPublicShop)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return spoonflowerResponse(raw, http.StatusOK, "text/html", spoonflowerShopFixture), nil
	})
	got, err := discoverSpoonflower(context.Background(), getter, root)
	if err == nil || got.Title != "Shop" || len(got.Items) != 2 {
		t.Fatalf("shop images=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	if got.Items[0].Metadata["spoonflower_design_id"] != "10112497" || got.Items[1].Metadata["spoonflower_design_id"] != "22005480" {
		t.Fatalf("wrong shop IDs: %+v", got.Items)
	}
}

func TestSpoonflowerCancellationRedirectAndLimits(t *testing.T) {
	root, _ := url.Parse(spoonflowerPublicDesign)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverSpoonflower(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := spoonflowerResponse(raw, http.StatusOK, "application/json", spoonflowerFixture)
		resp.FinalURL = "https://evil.test/"
		return resp, nil
	})
	if got, err := discoverSpoonflower(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect images=%+v err=%v", got.Items, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return spoonflowerResponse(raw, http.StatusOK, "application/json", strings.Repeat(" ", maxPageBytes+1)), nil
	})
	if got, err := discoverSpoonflower(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("oversize images=%+v err=%v", got.Items, err)
	}
}

type spoonflowerRoundTripper func(*http.Request) (*http.Response, error)

func (fn spoonflowerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestSpoonflowerRenderedCapture(t *testing.T) {
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
	const target = spoonflowerPublicDesign + "?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = spoonflowerRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || strings.Contains(r.URL.String(), "token=synthetic") {
			return nil, errors.New("cross-origin credential leak")
		}
		body, status, content := spoonflowerFixture, http.StatusOK, "application/json"
		if r.URL.String() == spoonflowerLegacyThumbnail("10112497") {
			body, status, content = "\xff\xd8\xff\xe0abc", http.StatusPartialContent, "image/jpeg"
		} else if r.URL.String() != spoonflowerAPI+"10112497" {
			return nil, errors.New("unexpected HTTP request")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {content}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(`<html><title>Design</title></html>`)))
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
	if err != nil || len(got.Items) != 1 || calls.Load() != 2 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestSpoonflowerLiveDesignDownload(t *testing.T) {
	if os.Getenv("TULAI_SPOONFLOWER_LIVE") != "1" {
		t.Skip("set TULAI_SPOONFLOWER_LIVE=1 to verify public designs")
	}
	for _, raw := range []string{spoonflowerPublicDesign, spoonflowerPublicDesign2} {
		t.Run(raw, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			getter := NewHTTPGetter("")
			got, err := Discover(ctx, getter, raw, 1)
			if len(got.Items) != 1 {
				t.Fatalf("public design images=%d err=%v", len(got.Items), err)
			}
			if err != nil {
				t.Logf("public preview fallback: %v", err)
			}
			item := got.Items[0]
			item.Filename = "design"
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
			t.Logf("public design first=%dx%d %s", im.Width, im.Height, format)
		})
	}
}

func TestSpoonflowerRenderedShopCapture(t *testing.T) {
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
	var calls atomic.Int32
	session.getter.client.Transport = spoonflowerRoundTripper(func(*http.Request) (*http.Response, error) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == spoonflowerPublicShop {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(spoonflowerShopFixture)))
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
	start, _ := url.Parse(spoonflowerPublicShop)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(spoonflowerPublicShop)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || len(got.Items) != 2 || calls.Load() != 0 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestSpoonflowerPreviewAndShopRedirect(t *testing.T) {
	root, _ := url.Parse(spoonflowerPublicDesign)
	invalid := strings.ReplaceAll(spoonflowerFixture, "garden.spoonflower.com/c/10112497/", "evil.test/c/10112497/")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == spoonflowerAPI+"10112497" {
			return spoonflowerResponse(raw, http.StatusOK, "application/json", invalid), nil
		}
		return spoonflowerResponse(raw, http.StatusForbidden, "text/html", "blocked"), nil
	})
	if got, err := discoverSpoonflower(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("untrusted preview images=%+v err=%v", got.Items, err)
	}
	shop, _ := url.Parse(spoonflowerPublicShop)
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := spoonflowerResponse(raw, http.StatusOK, "text/html", spoonflowerShopFixture)
		resp.FinalURL = "https://www.spoonflower.com/en/shop?on=wallpaper"
		return resp, nil
	})
	if got, err := discoverSpoonflower(context.Background(), getter, shop); err == nil || len(got.Items) != 0 {
		t.Fatalf("shop redirect images=%+v err=%v", got.Items, err)
	}
}

func TestSpoonflowerRenderedLiveAPI(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" || os.Getenv("TULAI_SPOONFLOWER_LIVE") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 and TULAI_SPOONFLOWER_LIVE=1 for isolated Chrome and live API")
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
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == spoonflowerPublicDesign {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(`<html><title>Design</title></html>`)))
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
	start, _ := url.Parse(spoonflowerPublicDesign)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(spoonflowerPublicDesign)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
	if err != nil && len(got.Items) == 0 {
		if strings.Contains(err.Error(), "网页图片不能访问其他内网地址") {
			t.Skipf("live API blocked by the rendered DNS boundary: %v", err)
		}
		t.Fatalf("rendered live API failed: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("rendered live API images=%d err=%v", len(got.Items), err)
	}
	t.Logf("rendered live API images=%d, err=%v", len(got.Items), err)
}
