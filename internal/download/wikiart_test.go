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
)

const wikiArtTestAuthor = "https://www.wikiart.org/en/test-artist"
const wikiArtTestImage1 = "https://uploads5.wikiart.org/images/test-artist/painting-1.jpg"
const wikiArtTestImage2 = "https://uploads1.wikiart.org/images/test-artist/painting-2.jpg"
const wikiArtTestImage3 = "https://uploads0.wikiart.org/images/test-artist/painting-3.jpg"

func wikiArtTestFixture(canonical string) string {
	return `<html><head><link rel="canonical" href="` + canonical + `"></head><body>Author</body></html>`
}
func wikiArtTestAPI(page int) string {
	return fmt.Sprintf("%s/mode/all-paintings?json=2&layout=new&page=%d&resultType=masonry", wikiArtTestAuthor, page)
}
func wikiArtTestPaintings() []map[string]any {
	return []map[string]any{
		{"id": "a00000000000000000000001", "title": "First", "year": "1901", "artistName": "Test Artist", "artistUrl": "/en/test-artist", "paintingUrl": "/en/test-artist/painting-1", "image": wikiArtTestImage1},
		{"id": "a00000000000000000000002", "title": "Second", "year": 1902, "artistName": "Test Artist", "artistUrl": "/en/test-artist", "paintingUrl": "/en/test-artist/painting-2", "image": wikiArtTestImage2},
		{"id": "a00000000000000000000003", "title": "Third", "year": "1903", "artistName": "Test Artist", "artistUrl": "/en/test-artist", "paintingUrl": "/en/test-artist/painting-3", "image": wikiArtTestImage3},
	}
}
func wikiArtTestJSON(total, pageSize int, paintings any) string {
	b, _ := json.Marshal(map[string]any{"AllPaintingsCount": total, "PageSize": pageSize, "Paintings": paintings})
	return string(b)
}
func wikiArtResponse(raw, contentType, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestWikiArtURLs(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{wikiArtTestAuthor, true},
		{"https://www.wikiart.org/en/claude-monet/all-works", true},
		{"https://www.wikiart.org/en/claude-monet/all-works/text-list", true},
		{"https://www.wikiart.org/en/claude-monet/mode/all-paintings/19", true},
		{"https://www.wikiart.org/zh/wen-sen-te-fan-gao", true},
		{"https://www.wikiart.org/en/claude-monet/water-lilies-1906", false},
		{"https://www.wikiart.org/en/artists-by-field", false},
		{"https://www.wikiart.org.evil.test/en/claude-monet", false},
		{"https://person@www.wikiart.org/en/claude-monet", false},
		{"https://www.wikiart.org:444/en/claude-monet", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isWikiArtAuthor(u); got != tc.ok {
			t.Errorf("isWikiArtAuthor(%q)=%v", tc.raw, got)
		}
	}
	for _, raw := range []string{"http://uploads5.wikiart.org/images/artist/a.jpg", "https://uploads5.wikiart.org.evil.test/images/artist/a.jpg", "https://someone@uploads5.wikiart.org/images/artist/a.jpg", "https://uploads5.wikiart.org:444/images/artist/a.jpg", "https://uploads5.wikiart.org/images/artist/../a.jpg", "https://uploads5.wikiart.org/Content/logo.jpg"} {
		if _, ok := wikiArtImageURL(raw); ok {
			t.Errorf("accepted %q", raw)
		}
	}
	if image, ok := wikiArtImageURL(wikiArtTestImage1 + "#fragment"); !ok || image != wikiArtTestImage1 {
		t.Fatalf("image=%q ok=%v", image, ok)
	}
}

func TestWikiArtAuthorPages(t *testing.T) {
	root, _ := url.Parse(wikiArtTestAuthor + "?tracking=synthetic")
	paintings := wikiArtTestPaintings()
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		switch raw {
		case root.String():
			return wikiArtResponse(raw, "text/html", wikiArtTestFixture(wikiArtTestAuthor)), nil
		case wikiArtTestAPI(1), wikiArtTestAPI(2):
			r, _ := http.NewRequest(http.MethodGet, raw, nil)
			prepare(r)
			if r.Header.Get("Accept") != "application/json" {
				t.Fatal("missing JSON accept")
			}
			if raw == wikiArtTestAPI(1) {
				return wikiArtResponse(raw, "application/json", wikiArtTestJSON(3, 2, paintings[:2])), nil
			}
			return wikiArtResponse(raw, "application/json", wikiArtTestJSON(3, 2, paintings[2:])), nil
		default:
			t.Fatalf("unexpected request %q", raw)
			return nil, nil
		}
	})
	got, err := discoverWikiArt(context.Background(), getter, root)
	if err != nil || got.Title != "Test Artist" || len(got.Items) != 3 || len(calls) != 3 {
		t.Fatalf("items=%+v title=%q calls=%v err=%v", got.Items, got.Title, calls, err)
	}
	for i, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://www.wikiart.org/" || item.Metadata["wikiart_artist"] != "Test Artist" {
			t.Fatalf("item %d=%+v", i, item)
		}
	}
	if got.Items[0].URL != wikiArtTestImage1 || got.Items[1].Metadata["wikiart_year"] != "1902" || got.Items[2].URL != wikiArtTestImage3 {
		t.Fatalf("wrong items %+v", got.Items)
	}
}

func TestWikiArtDirectJSONEndpoint(t *testing.T) {
	root, _ := url.Parse(wikiArtTestAPI(1))
	p := wikiArtTestPaintings()
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		switch raw {
		case wikiArtTestAPI(1):
			return wikiArtResponse(raw, "application/json", wikiArtTestJSON(3, 2, p[:2])), nil
		case wikiArtTestAPI(2):
			return wikiArtResponse(raw, "application/json", wikiArtTestJSON(3, 2, p[2:])), nil
		default:
			t.Fatalf("unexpected request %q", raw)
			return nil, nil
		}
	})
	got, err := Discover(context.Background(), getter, root.String(), 1)
	if err != nil || len(got.Items) != 3 || len(calls) != 2 {
		t.Fatalf("items=%+v calls=%v err=%v", got.Items, calls, err)
	}
}

func TestWikiArtPartialAndBinding(t *testing.T) {
	root, _ := url.Parse(wikiArtTestAuthor)
	for _, tc := range []struct {
		name     string
		mutate   func([]map[string]any)
		expected int
	}{
		{"artist", func(p []map[string]any) { p[1]["artistUrl"] = "/en/another-artist" }, 1},
		{"painting", func(p []map[string]any) { p[1]["paintingUrl"] = "/en/another-artist/painting-2" }, 1},
		{"CDN", func(p []map[string]any) { p[1]["image"] = "https://evil.test/images/test-artist/p.jpg" }, 1},
		{"duplicate ID", func(p []map[string]any) { p[1]["id"] = p[0]["id"] }, 1},
		{"invalid id", func(p []map[string]any) { p[1]["id"] = "other" }, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paintings := wikiArtTestPaintings()
			tc.mutate(paintings)
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				if raw == root.String() {
					return wikiArtResponse(raw, "text/html", wikiArtTestFixture(root.String())), nil
				}
				return wikiArtResponse(raw, "application/json", wikiArtTestJSON(2, 2, paintings[:2])), nil
			})
			got, err := discoverWikiArt(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.expected || got.Items[0].URL != wikiArtTestImage1 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestWikiArtFailureAndRedirect(t *testing.T) {
	root, _ := url.Parse(wikiArtTestAuthor)
	p := wikiArtTestPaintings()
	for _, tc := range []struct {
		name, canonical, page2 string
		page2Final             string
	}{
		{"wrong page identity", "https://www.wikiart.org/en/another", "", ""},
		{"API failure", wikiArtTestAuthor, "not JSON", ""},
		{"early end", wikiArtTestAuthor, wikiArtTestJSON(3, 2, nil), ""},
		{"API redirect", wikiArtTestAuthor, wikiArtTestJSON(3, 2, p[2:]), "https://evil.test/page"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				switch raw {
				case root.String():
					return wikiArtResponse(raw, "text/html", wikiArtTestFixture(tc.canonical)), nil
				case wikiArtTestAPI(1):
					return wikiArtResponse(raw, "application/json", wikiArtTestJSON(3, 2, p[:2])), nil
				case wikiArtTestAPI(2):
					final := raw
					if tc.page2Final != "" {
						final = tc.page2Final
					}
					return wikiArtResponse(final, "application/json", tc.page2), nil
				default:
					t.Fatalf("unexpected request %s", raw)
					return nil, nil
				}
			})
			got, err := discoverWikiArt(context.Background(), getter, root)
			want := 2
			if tc.name == "wrong page identity" {
				want = 0
			}
			if err == nil || len(got.Items) != want {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
	filtered, _ := url.Parse(wikiArtTestAuthor + "/all-works#!#filterName:Genre_landscape")
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("filter should not fetch")
		return nil, nil
	})
	if got, err := discoverWikiArt(context.Background(), getter, filtered); err == nil || len(got.Items) != 0 {
		t.Fatalf("filter=%+v err=%v", got.Items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverWikiArt(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

type wikiArtRoundTripper func(*http.Request) (*http.Response, error)

func (f wikiArtRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWikiArtRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Chrome")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var calls atomic.Int32
	p := wikiArtTestPaintings()
	session.getter.client.Transport = wikiArtRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		var body string
		switch r.URL.String() {
		case wikiArtTestAPI(1):
			body = wikiArtTestJSON(3, 2, p[:2])
		case wikiArtTestAPI(2):
			body = wikiArtTestJSON(3, 2, p[2:])
		default:
			return nil, errors.New("unexpected request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == wikiArtTestAuthor {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(wikiArtTestFixture(wikiArtTestAuthor))))
			} else if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == wikiArtTestAPI(1) {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "application/json"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(wikiArtTestJSON(3, 2, p[:2]))))
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
	start, _ := url.Parse(wikiArtTestAuthor)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(wikiArtTestAuthor)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
	if err != nil || len(got.Items) != 3 || calls.Load() != 2 {
		t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	if err := session.run(ctx, camoufox.Navigate(wikiArtTestAPI(1))); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || calls.Load() != 4 {
		t.Fatalf("JSON capture items=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
}

func TestWikiArtPublicDownload(t *testing.T) {
	if os.Getenv("TULAI_WIKIART_LIVE") != "1" {
		t.Skip("set TULAI_WIKIART_LIVE=1 to check an author's public works")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, "https://www.wikiart.org/en/cassius-marcellus-coolidge", 1)
	if err != nil || len(got.Items) < 21 {
		t.Fatalf("public author: images=%d err=%v", len(got.Items), err)
	}
	apiURL := "https://www.wikiart.org/en/cassius-marcellus-coolidge/mode/all-paintings?json=2&layout=new&page=1&resultType=masonry"
	apiGot, err := Discover(ctx, getter, apiURL, 1)
	if err != nil || len(apiGot.Items) != len(got.Items) {
		t.Fatalf("public JSON entry: images=%d want=%d err=%v", len(apiGot.Items), len(got.Items), err)
	}
	dir := t.TempDir()
	item := got.Items[0]
	item.Filename = "wikiart"
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("public image download: %v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("download entries=%v err=%v", files, err)
	}
	info, err := files[0].Info()
	if err != nil || info.Size() == 0 {
		t.Fatalf("download info=%v err=%v", info, err)
	}
	t.Logf("public author: %s; collected %d images and downloaded %d bytes", got.Title, len(got.Items), info.Size())
}

func TestWikiArtPublicChinesePage(t *testing.T) {
	if os.Getenv("TULAI_WIKIART_LIVE") != "1" {
		t.Skip("set TULAI_WIKIART_LIVE=1 to check a Chinese author page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entry := "https://www.wikiart.org/zh/wen-sen-te-fan-gao/mode/all-paintings?json=2&layout=new&page=33&resultType=masonry"
	got, err := Discover(ctx, NewHTTPGetter(""), entry, 1)
	if err == nil || !strings.Contains(err.Error(), "从指定页开始") || len(got.Items) == 0 {
		t.Fatalf("Chinese later page: images=%d err=%v", len(got.Items), err)
	}
	t.Logf("Chinese later page: %d verified images; earlier pages are omitted", len(got.Items))
}
