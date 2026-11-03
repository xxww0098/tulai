package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

const flickrTestKey = "0123456789abcdef0123456789abcdef"

func flickrTestPhoto(id string, sizes map[string]string) map[string]any {
	data := map[string]any{}
	for key, value := range sizes {
		data[key] = map[string]any{"data": map[string]any{"url": value}}
	}
	return map[string]any{"id": id, "title": "test photo", "sizes": map[string]any{"data": data}}
}

func flickrTestHTML(album bool, id string, photos []map[string]any, total int, end, key bool) string {
	entries := make([]map[string]any, 0, len(photos))
	for _, photo := range photos {
		entries = append(entries, map[string]any{"data": photo})
	}
	main := map[string]any{}
	if album {
		set := map[string]any{"id": id, "title": "test album", "photoPageList": map[string]any{"data": map[string]any{"_data": entries, "totalItems": total, "fetchedEnd": end}}}
		main["set-models"] = []any{map[string]any{"data": set}}
	} else {
		main["photo-models"] = entries
	}
	body, _ := json.Marshal(map[string]any{"main": main})
	keyScript := ""
	if key {
		keyScript = `YUI_config.flickr.api.site_key = "` + flickrTestKey + `";`
	}
	return `<html><head><title>fallback</title><script>` + keyScript + ` var app={modelExport:` + string(body) + `};</script></head><body><img src="https://unrelated.invalid/recommendation.jpg"></body></html>`
}

func flickrTestResponse(raw, body, contentType string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func flickrTestAPI(albumID string, page, pages, total int, photos ...map[string]any) string {
	body, _ := json.Marshal(map[string]any{"stat": "ok", "photoset": map[string]any{"id": albumID, "page": fmt.Sprint(page), "pages": pages, "total": total, "photo": photos}})
	return string(body)
}

func TestFlickrPageURLs(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		album   bool
	}{
		{"https://www.flickr.com/photos/name/155761353/", "155761353", false},
		{"https://flickr.com/photos/12037949754@N01/155761353/in/album-123", "155761353", false},
		{"https://www.flickr.com/photos/name/155761353/sizes/o/", "155761353", false},
		{"https://www.flickr.com/photos/name/albums/123/", "123", true},
		{"https://m.flickr.com/photos/name/sets/123/", "123", true},
		{"https://www.flickr.com/photos/name/albums/", "", false},
		{"https://www.flickr.com/photos/name/123/comments", "", false},
		{"https://flickr.com.evil.test/photos/name/123", "", false},
		{"https://user@flickr.com/photos/name/123", "", false},
		{"https://flickr.com:8443/photos/name/123", "", false},
		{"https://live.staticflickr.com/1/123_abc.jpg", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := flickrRef(u)
		if got.id != tc.id || got.album != tc.album || isFlickrPage(u) != (tc.id != "") {
			t.Errorf("ref(%q) = %+v", tc.raw, got)
		}
	}
}

func TestFlickrSingleUsesProvidedOriginal(t *testing.T) {
	root, _ := url.Parse("https://www.flickr.com/photos/name/155761353/?token=synthetic")
	photo := flickrTestPhoto("155761353", map[string]string{
		"o": "//live.staticflickr.com/73/155761353_original_o.png", "k": "//live.staticflickr.com/73/155761353_large_k.jpg",
	})
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return flickrTestResponse(raw, flickrTestHTML(false, "155761353", []map[string]any{photo}, 1, true, true), "text/html"), nil
	})
	got, err := discoverFlickr(context.Background(), getter, root)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://live.staticflickr.com/73/155761353_original_o.png" || got.Items[0].Headers["Referer"] != "https://www.flickr.com/" {
		t.Fatalf("single = %+v, err = %v", got, err)
	}
}

func TestFlickrAlbumPaginationAndVideos(t *testing.T) {
	root, _ := url.Parse("https://www.flickr.com/photos/name/albums/123/?token=synthetic")
	photos := []map[string]any{
		flickrTestPhoto("11", map[string]string{"l": "//live.staticflickr.com/73/11_small_b.jpg"}),
		flickrTestPhoto("12", map[string]string{"l": "//live.staticflickr.com/73/12_small_b.jpg"}),
		flickrTestPhoto("13", map[string]string{"l": "//live.staticflickr.com/73/13_video_b.jpg"}),
		flickrTestPhoto("99", map[string]string{"l": "//live.staticflickr.com/73/99_stale_b.jpg"}),
		flickrTestPhoto("14", nil),
	}
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		if u.Path != "/services/rest/" {
			return flickrTestResponse(raw, flickrTestHTML(true, "123", photos, 4, false, true), "text/html"), nil
		}
		r, _ := http.NewRequest("GET", raw, nil)
		prepare(r)
		if u.Hostname() != "www.flickr.com" || u.Query().Get("api_key") != flickrTestKey || u.Query().Get("per_page") != "50" || r.Header.Get("Referer") != "https://www.flickr.com/" {
			t.Fatalf("API request headers/path are wrong")
		}
		calls = append(calls, u.Query().Get("page"))
		if len(calls) == 1 {
			return flickrTestResponse(raw, flickrTestAPI("123", 1, 2, 4,
				map[string]any{"id": "11", "media": "photo", "url_o": "https://live.staticflickr.com/73/11_original_o.jpg"},
				map[string]any{"id": "12", "media": "photo", "url_o": "https://live.staticflickr.com/73/12_original_o.jpg"}), "application/json"), nil
		}
		return flickrTestResponse(raw, flickrTestAPI("123", 2, 2, 4,
			map[string]any{"id": "13", "media": "video"},
			map[string]any{"id": "14", "media": "photo", "url_l": "https://farm1.staticflickr.com/73/14_large_b.jpg"}), "application/json"), nil
	})
	got, err := discoverFlickr(context.Background(), getter, root)
	if err != nil || strings.Join(calls, ",") != "1,2" || len(got.Items) != 3 || !strings.Contains(got.Items[0].URL, "11_original_o") || !strings.Contains(got.Items[1].URL, "12_original_o") || !strings.Contains(got.Items[2].URL, "14_large_b") {
		t.Fatalf("album = %+v, pages=%v, err=%v", got.Items, calls, err)
	}
}

func TestFlickrPartialAndUnsafeURLs(t *testing.T) {
	root, _ := url.Parse("https://www.flickr.com/photos/name/albums/123/")
	for _, hostile := range []string{
		"https://live.staticflickr.com.evil.test/73/11_x_o.jpg", "https://user@live.staticflickr.com/73/11_x_o.jpg",
		"https://live.staticflickr.com:8443/73/11_x_o.jpg", "https://127.0.0.1/73/11_x_o.jpg",
		"https://live.staticflickr.com/73/99_wrong_o.jpg", "http://live.staticflickr.com/73/11_x_o.jpg",
	} {
		t.Run(hostile, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Path != "/services/rest/" {
					return flickrTestResponse(raw, flickrTestHTML(true, "123", nil, 1, false, true), "text/html"), nil
				}
				return flickrTestResponse(raw, flickrTestAPI("123", 1, 1, 1, map[string]any{"id": "11", "url_o": hostile, "url_l": "https://live.staticflickr.com/73/11_safe_b.jpg"}), "application/json"), nil
			})
			got, err := discoverFlickr(context.Background(), getter, root)
			if err == nil || len(got.Items) != 1 || !strings.Contains(got.Items[0].URL, "11_safe_b") {
				t.Fatalf("unsafe fallback = %+v, %v", got.Items, err)
			}
		})
	}
}

func TestFlickrCancellationRedirectAndIncomplete(t *testing.T) {
	root, _ := url.Parse("https://www.flickr.com/photos/name/albums/123/")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discoverFlickr(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := flickrTestResponse(raw, "<html></html>", "text/html")
		resp.FinalURL = "https://evil.test/photos/name/albums/123/"
		return resp, nil
	})
	if _, err := discoverFlickr(context.Background(), getter, root); err == nil {
		t.Fatal("external redirect accepted")
	}
	photo := flickrTestPhoto("11", map[string]string{"l": "//live.staticflickr.com/73/11_safe_b.jpg"})
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return flickrTestResponse(raw, flickrTestHTML(true, "123", []map[string]any{photo}, 4, false, false), "text/html"), nil
	})
	got, err := discoverFlickr(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("incomplete = %+v, %v", got.Items, err)
	}
}

func TestFlickrAPIBoundaries(t *testing.T) {
	root, _ := url.Parse("https://www.flickr.com/photos/name/albums/123/")
	photo := flickrTestPhoto("11", map[string]string{"l": "//live.staticflickr.com/73/11_safe_b.jpg"})
	for _, kind := range []string{"redirect", "wrongpage", "toolarge", "length", "status", "network", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Path != "/services/rest/" {
					return flickrTestResponse(raw, flickrTestHTML(true, "123", []map[string]any{photo}, 2, false, true), "text/html"), nil
				}
				resp := flickrTestResponse(raw, flickrTestAPI("123", 1, 1, 1, map[string]any{"id": "11", "url_l": "https://live.staticflickr.com/73/11_safe_b.jpg"}), "application/json")
				switch kind {
				case "redirect":
					resp.FinalURL = "https://evil.test/services/rest/"
				case "wrongpage":
					u.RawQuery = strings.Replace(u.RawQuery, "page=1", "page=2", 1)
					resp.FinalURL = u.String()
				case "toolarge":
					resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxPageBytes+1)))
				case "length":
					resp.Header.Set("Content-Length", "999")
				case "status":
					resp.StatusCode = 403
				case "network":
					return nil, fmt.Errorf("request failed: %s", raw)
				case "cancel":
					cancel()
					return nil, context.Canceled
				}
				return resp, nil
			})
			got, err := discoverFlickr(ctx, getter, root)
			if err == nil || len(got.Items) != 1 || strings.Contains(err.Error(), flickrTestKey) {
				t.Fatalf("boundary result = %+v, err = %v", got.Items, err)
			}
			if kind == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel = %v", err)
			}
		})
	}
}

func TestFlickrRenderedCapture(t *testing.T) {
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
	const target = "https://www.flickr.com/photos/name/albums/123/?token=synthetic"
	photo := flickrTestPhoto("11", map[string]string{"l": "//live.staticflickr.com/73/11_safe_b.jpg"})
	page := flickrTestHTML(true, "123", []map[string]any{photo}, 2, false, true)
	var apiCalls atomic.Int32
	session.getter.client.Transport = flickrTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.flickr.com" || r.URL.Path != "/services/rest/" || r.URL.Query().Get("api_key") != flickrTestKey || !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") {
			return nil, errors.New("unexpected HTTP request or missing same-origin session")
		}
		apiCalls.Add(1)
		body := flickrTestAPI("123", 1, 1, 2,
			map[string]any{"id": "11", "url_o": "https://live.staticflickr.com/73/11_orig_o.jpg"},
			map[string]any{"id": "12", "url_l": "https://live.staticflickr.com/73/12_safe_b.jpg"})
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Request: r, Body: io.NopCloser(strings.NewReader(body))}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(page)))
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
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.flickr.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || apiCalls.Load() != 1 || !strings.Contains(got.Items[0].URL, "11_orig_o") || !strings.Contains(got.Items[1].URL, "12_safe_b") {
		t.Fatalf("capture = %+v, API calls %d, err %v", got.Items, apiCalls.Load(), err)
	}
	for _, item := range got.Items {
		if item.Headers["Referer"] != "https://www.flickr.com/" {
			t.Fatalf("referer = %q", item.Headers["Referer"])
		}
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

type flickrTestTransport func(*http.Request) (*http.Response, error)

func (f flickrTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFlickrLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_FLICKR_LIVE") != "1" {
		t.Skip("set TULAI_FLICKR_LIVE=1 for a public Flickr photo and album")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	single, _ := url.Parse("https://www.flickr.com/photos/12037949754@N01/155761353/")
	got, err := discoverFlickr(ctx, getter, single)
	if err != nil || len(got.Items) != 1 || !strings.Contains(got.Items[0].URL, "/155761353_") {
		t.Fatalf("live single = %+v, %v", got.Items, err)
	}
	album, _ := url.Parse("https://www.flickr.com/photos/nasacommons/albums/72157651060963685/")
	got, err = discoverFlickr(ctx, getter, album)
	if err != nil || len(got.Items) != 63 {
		t.Fatalf("live album = %d items, %v", len(got.Items), err)
	}
	t.Logf("public photo and %d album images across two API pages", len(got.Items))
}
