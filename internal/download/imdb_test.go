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

const imdbTitleURL = "https://www.imdb.com/title/tt0111161/"
const imdbIndex = "https://www.imdb.com/title/tt0111161/mediaindex/"
const imdbFixture = `<html><head><title>The Shawshank Redemption - IMDb</title>
<script type="application/json" id="__NEXT_DATA__">{"props":{"pageProps":{"title":{"id":"tt0111161","images":{"edges":[
{"node":{"id":"rm101","url":"https://m.media-amazon.com/images/M/MV5Bfirst.jpg","caption":{"plainText":"First"}}},
{"node":{"id":"rm102","url":"https://m.media-amazon.com/images/M/MV5Bsecond.jpg"}}],"pageInfo":{"hasNextPage":false}}},
"other":{"id":"tt9999999","images":{"edges":[{"node":{"id":"rm999","url":"https://m.media-amazon.com/images/M/recommendation.jpg"}}]}}}}}</script></head></html>`

func imdbTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestIMDbURL(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		index   bool
	}{
		{imdbTitleURL, "tt0111161", false},
		{imdbIndex + "?refine=still_frame", "tt0111161", true},
		{"https://imdb.com/title/tt123456789012/mediaindex", "tt123456789012", true},
		{"https://www.imdb.com/title/tt0111161/mediaviewer/rm123/", "", false},
		{"https://www.imdb.com/name/nm0000151/mediaindex/", "", false},
		{"https://www.imdb.com.evil.test/title/tt0111161/", "", false},
		{"https://user@www.imdb.com/title/tt0111161/", "", false},
		{"https://www.imdb.com:8443/title/tt0111161/", "", false},
		{"https://www.imdb.com/title/tt1/", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := imdbPageRef(u)
		if got.id != tc.id || got.index != tc.index || isIMDbPage(u) != (tc.id != "") {
			t.Errorf("reference(%q)=%+v", tc.raw, got)
		}
	}
}

func TestIMDbImageBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://m.media-amazon.com.evil.test/images/M/image.jpg",
		"https://user@m.media-amazon.com/images/M/image.jpg",
		"https://m.media-amazon.com:8443/images/M/image.jpg",
		"http://m.media-amazon.com/images/M/image.jpg",
		"https://127.0.0.1/images/M/image.jpg",
		"https://m.media-amazon.com/images/M/../secret.jpg",
		"https://m.media-amazon.com/images/I/unrelated.jpg",
	} {
		if _, ok := imdbImageURL(raw); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	if _, ok := imdbImageURL("https://m.media-amazon.com/images/M/MV5Bfirst.jpg"); !ok {
		t.Fatal("valid image rejected")
	}
}

func TestIMDbTitleMediaIndex(t *testing.T) {
	root, _ := url.Parse(imdbTitleURL + "?tracking=synthetic")
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		if raw != imdbIndex {
			t.Fatalf("unexpected index URL %q", raw)
		}
		return imdbTestResponse(raw, imdbFixture), nil
	})
	got, err := discoverIMDb(context.Background(), getter, root)
	if err != nil || calls != 1 || got.Title != "The Shawshank Redemption" || len(got.Items) != 2 {
		t.Fatalf("items=%+v title=%q calls=%d err=%v", got.Items, got.Title, calls, err)
	}
	if got.Items[0].Metadata["caption"] != "First" {
		t.Fatalf("missing caption: %+v", got.Items[0])
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["imdb_title_id"] != "tt0111161" || item.Headers["Referer"] != "https://www.imdb.com/" || strings.Contains(item.URL, "recommendation") {
			t.Fatalf("unexpected image %+v", item)
		}
	}
}

func TestIMDbIdentityAndPartial(t *testing.T) {
	root, _ := url.Parse(imdbIndex)
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"other-title", strings.Replace(imdbFixture, `"id":"tt0111161"`, `"id":"tt2222222"`, 1), 0},
		{"has-next", strings.Replace(imdbFixture, `"hasNextPage":false`, `"hasNextPage":true`, 1), 2},
		{"unknown-next", strings.Replace(imdbFixture, `"hasNextPage":false`, `"unknown":false`, 1), 2},
		{"bad-host", strings.Replace(imdbFixture, `https://m.media-amazon.com/images/M/MV5Bsecond.jpg`, `https://evil.test/images/M/MV5Bsecond.jpg`, 1), 1},
		{"bad-id", strings.Replace(imdbFixture, `"id":"rm102"`, `"id":"tt2222222"`, 1), 1},
		{"empty", `<html><img src="https://m.media-amazon.com/images/M/recommendation.jpg"></html>`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return imdbTestResponse(raw, tc.body), nil
			})
			got, err := discoverIMDb(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("images=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestIMDbCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(imdbTitleURL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverIMDb(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := imdbTestResponse(raw, imdbFixture)
		resp.FinalURL = "https://www.imdb.com/title/tt9999999/mediaindex/"
		return resp, nil
	})
	if got, err := discoverIMDb(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect images=%+v err=%v", got.Items, err)
	}
}

type imdbRoundTripper func(*http.Request) (*http.Response, error)

func (fn imdbRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestIMDbRenderedCapture(t *testing.T) {
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
	const target = imdbIndex + "?fixture=synthetic"
	const root = imdbTitleURL + "?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = imdbRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != imdbIndex {
			return nil, errors.New("unexpected request or leaked query")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(imdbFixture)), Request: r}, nil
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && (paused.Request.URL == target || paused.Request.URL == root) {
				body := imdbFixture
				if paused.Request.URL == root {
					body = `<html><title>Title</title></html>`
				}
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
	if err != nil || len(got.Items) != 2 || calls.Load() != 0 {
		t.Fatalf("index capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	if err := session.run(ctx, camoufox.Navigate(root)); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || calls.Load() != 1 {
		t.Fatalf("title capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestIMDbLivePage(t *testing.T) {
	if os.Getenv("TULAI_IMDB_LIVE") != "1" {
		t.Skip("set TULAI_IMDB_LIVE=1 to verify public IMDb reachability")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), imdbTitleURL, 1)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("public page images=%+v err=%v", got.Items, err)
	}
	t.Logf("public IMDb page unavailable: %v", err)
}

func TestIMDbLiveImageCDN(t *testing.T) {
	if os.Getenv("TULAI_IMDB_LIVE") != "1" {
		t.Skip("set TULAI_IMDB_LIVE=1 to verify IMDb image CDN")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw := "https://m.media-amazon.com/images/M/MV5BM2QzMGVkNjUtN2Y4Yi00ODMwLTg3YzktYzUxYjJlNjFjNDY1XkEyXkFqcGc@._V1_.jpg"
	imageURL, ok := imdbImageURL(raw)
	if !ok {
		t.Fatal("public sample image rejected")
	}
	dir := t.TempDir()
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, Item{URL: imageURL, Filename: "image", RequireImage: true}, nil); err != nil {
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
	t.Logf("public image CDN: %dx%d %s", im.Width, im.Height, format)
}
