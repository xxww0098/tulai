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

const mtimeTestData = `{"code":0,"data":{"imageTypes":[{"type":-1,"imageCount":2}],"images":[{"id":11,"imageOrigin":"https://img31.mtime.cn/pi/a.jpg","imageTypeUrlName":"posters","imageTypeDes":"海报"},{"id":12,"imageOrigin":"https://img21.mtime.cn/pi/b.jpg","imageTypeUrlName":"stills","imageSubtypeUrlName":"officialstills","imageTypeDes":"剧照"}]}}`

type mtimeRoundTrip func(*http.Request) (*http.Response, error)

func (f mtimeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mtimeTestGetter(t *testing.T, body string) Getter {
	t.Helper()
	return GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		if prepare != nil {
			prepare(r)
		}
		if r.URL.Host != "front-gateway.mtime.com" || r.Header.Get("Accept") != "application/json" || r.Header.Get("Cookie") != "" {
			t.Fatalf("unexpected API request %s %v", raw, r.Header)
		}
		return &Response{StatusCode: 200, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
}

func TestMtimeRoutes(t *testing.T) {
	for _, tc := range []struct {
		raw, kind, id, category, image string
	}{
		{"https://movie.mtime.com/12408/posters_and_images/", "movie", "12408", "", ""},
		{"http://movie.mtime.com/12408/posters_and_images/11/", "movie", "12408", "", "11"},
		{"https://movie.mtime.com/12408/posters_and_images/stills/hot.html", "movie", "12408", "stills", ""},
		{"https://people.mtime.com/1287527/images/", "person", "1287527", "", ""},
		{"https://movie.mtime.com.evil.test/12408/posters_and_images/", "", "", "", ""},
		{"https://movie.mtime.com/../12408/posters_and_images/", "", "", "", ""},
		{"https://movie.mtime.com/12408/posters_and_images/evil/extra/", "", "", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		ref := mtimePageRef(u)
		if ref.kind != tc.kind || ref.id != tc.id || ref.category != tc.category || ref.imageID != tc.image || isMtimePage(u) != (tc.kind != "") {
			t.Errorf("%s: %+v", tc.raw, ref)
		}
	}
}

func TestMtimeDiscoveryAndFilters(t *testing.T) {
	getter := mtimeTestGetter(t, mtimeTestData)
	for _, tc := range []struct {
		raw, id string
		count   int
	}{
		{"https://movie.mtime.com/12408/posters_and_images/", "", 2},
		{"https://movie.mtime.com/12408/posters_and_images/11/", "11", 1},
		{"https://movie.mtime.com/12408/posters_and_images/officialstills/", "12", 1},
	} {
		got, err := Discover(context.Background(), getter, tc.raw, 1)
		if err != nil || len(got.Items) != tc.count || !got.IsPage {
			t.Fatalf("%s: count=%d err=%v", tc.raw, len(got.Items), err)
		}
		if tc.id != "" && got.Items[0].Metadata["mtime_image_id"] != tc.id {
			t.Fatalf("%s: wrong image %+v", tc.raw, got.Items[0])
		}
		for _, item := range got.Items {
			if !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://movie.mtime.com/" || item.Metadata["mtime_id"] != "12408" {
				t.Errorf("unsafe or incomplete item: %+v", item)
			}
		}
	}
}

func TestMtimePartialAndInvalidData(t *testing.T) {
	page, _ := url.Parse("https://movie.mtime.com/12408/posters_and_images/")
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"unsafe image", strings.Replace(mtimeTestData, "https://img21.mtime.cn/pi/b.jpg", "https://evil.test/b.jpg", 1), 1},
		{"missing image", strings.Replace(mtimeTestData, `"imageCount":2`, `"imageCount":3`, 1), 2},
		{"api error", `{"code":1,"data":null}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := discoverMtime(context.Background(), mtimeTestGetter(t, tc.body), page)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("count=%d err=%v", len(got.Items), err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverMtime(ctx, mtimeTestGetter(t, mtimeTestData), page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestMtimeCurrentPublic(t *testing.T) {
	if os.Getenv("TULAI_MTIME_LIVE") != "1" {
		t.Skip("set TULAI_MTIME_LIVE=1 for public API and image download")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	for _, raw := range []string{"https://movie.mtime.com/12408/posters_and_images/", "https://people.mtime.com/1287527/images/"} {
		got, err := Discover(ctx, getter, raw, 1)
		if err != nil || len(got.Items) == 0 {
			t.Fatalf("%s count=%d err=%v", raw, len(got.Items), err)
		}
		t.Logf("%s: %d images", raw, len(got.Items))
		item := got.Items[0]
		item.Filename = "image"
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: t.TempDir()}, item, nil); err != nil {
			t.Fatalf("%s image download: %v", raw, err)
		}
	}
}

func TestMtimeRenderedCapture(t *testing.T) {
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
	page := "https://movie.mtime.com/12408/posters_and_images/"
	interceptErrors := make(chan error, 1)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			action := chromedp.Action(fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient))
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == page {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(`<html><body>movie</body></html>`)))
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case interceptErrors <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	session.start, _ = url.Parse(page)
	if err := session.run(ctx, camoufox.Navigate(page)); err != nil {
		t.Fatal(err)
	}
	session.getter.client.Transport = mtimeRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "front-gateway.mtime.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected API request: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(mtimeTestData)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || session.PageURL() != page {
		t.Fatalf("capture count=%d page=%s err=%v", len(got.Items), session.PageURL(), err)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}
