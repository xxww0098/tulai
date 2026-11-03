package download

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"image"
	"image/color"
	"image/png"
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

func animePicturesTestDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func animePicturesTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func animePicturesTestPost(id, link string) string {
	return `<html><head><title>Picture ` + id + `</title></head><body><h1>Anime picture №` + id + `</h1><a href="` + link + `">download</a></body></html>`
}

func TestAnimePicturesRoute(t *testing.T) {
	for _, tc := range []struct {
		raw, kind string
	}{
		{"https://anime-pictures.net/posts/834153?lang=en", "post"},
		{"https://anime-pictures.net/posts?page=0&search_tag=cat", "search"},
		{"https://anime-pictures.net/posts?search_tag=", "search"},
		{"http://anime-pictures.net/posts/1", "post"},
		{"https://anime-pictures.net/posts", ""},
		{"https://anime-pictures.net/posts/0", ""},
		{"https://anime-pictures.net/posts/12/other", ""},
		{"https://anime-pictures.net/pictures/view_post/12", ""},
		{"https://anime-pictures.net/posts?search_tag=a&search_tag=b", ""},
		{"https://anime-pictures.net/posts?search_tag=a&page=1&page=2", ""},
		{"https://anime-pictures.net/posts?search_tag=a&page=-1", ""},
		{"https://anime-pictures.net:444/posts/1", ""},
		{"https://anime-pictures.net.evil.test/posts/1", ""},
		{"https://user@anime-pictures.net/posts/1", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			kind, _ := animePicturesRoute(animePicturesTestURL(t, tc.raw))
			if kind != tc.kind {
				t.Fatalf("got %q, want %q", kind, tc.kind)
			}
		})
	}
}

func TestAnimePicturesOnlyExplicitOwnedDownloads(t *testing.T) {
	page := animePicturesTestURL(t, "https://anime-pictures.net/posts/834153?lang=en")
	valid := "/pictures/download_image/834153-1309x2000-%28title%29.png"
	fixture := `<html><head><title>Picture</title></head><body><h1>Anime picture №834153</h1>` +
		`<a href="/pictures/download_image/111-other.jpg">recommendation</a>` +
		`<a href="` + valid + `">download</a><img src="https://other.test/tracking.jpg"></body></html>`
	got, err := animePicturesPost(context.Background(), page, animePicturesTestDoc(t, fixture))
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://anime-pictures.net"+valid ||
		got.Items[0].Metadata["animepictures_post_id"] != "834153" || !got.Items[0].RequireImage || !got.Items[0].PublicOnly {
		t.Fatalf("post=%+v, err=%v", got.Items, err)
	}
	for _, tc := range []struct{ name, doc string }{
		{"wrong heading", animePicturesTestPost("111", valid)},
		{"missing heading", `<a href="` + valid + `">download</a>`},
		{"other image", animePicturesTestPost("834153", "/pictures/download_image/8341530-other.jpg")},
		{"foreign host", animePicturesTestPost("834153", "https://anime-pictures.net.evil.test/pictures/download_image/834153-a.jpg")},
		{"credentials", animePicturesTestPost("834153", "https://user@anime-pictures.net/pictures/download_image/834153-a.jpg")},
		{"encoded slash", animePicturesTestPost("834153", "/pictures/download_image/834153-a%2Fother.jpg")},
		{"preview only", animePicturesTestPost("834153", "/pictures/get_image/834153-a.jpg")},
		{"hidden", `<h1>Anime picture №834153</h1><div style="DISPLAY: none !important"><a href="` + valid + `">download</a></div>`},
		{"aria hidden", `<h1>Anime picture №834153</h1><div aria-hidden="true"><a href="` + valid + `">download</a></div>`},
		{"ambiguous", animePicturesTestPost("834153", valid) + `<a href="/pictures/download_image/834153-other.jpg">another</a>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := animePicturesPost(context.Background(), page, animePicturesTestDoc(t, tc.doc))
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("accepted %s: %+v %v", tc.name, got.Items, err)
			}
		})
	}
	api := "https://api.anime-pictures.net/pictures/download_image/834153-1309x2000.png"
	if actual := animePicturesDownload(page, api, "834153"); actual != api {
		t.Fatalf("recorded API host/path rejected: %s", actual)
	}
}

func TestAnimePicturesSearchPaginationAndPartialFailures(t *testing.T) {
	page := animePicturesTestURL(t, "https://anime-pictures.net/posts?lang=en&page=0&search_tag=cat")
	first := `<div class="central-block"><div><a href="/posts/123?by_tag=1"><img src="thumbnail"></a></div>` +
		`<div class="last-stars"><a href="/posts/777"><img src="thumbnail"></a></div></div>` +
		`<a href="/posts/999"><img src="thumbnail"></a>` +
		`<a href="/posts?lang=en&amp;ldate=0&amp;order_by=date&amp;page=1&amp;search_tag=cat">&gt;</a>`
	var calls []string
	getter := GetterFunc(func(ctx context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		u := animePicturesTestURL(t, raw)
		kind, id := animePicturesRoute(u)
		if kind == "post" {
			if id == "456" {
				return tiebaTestResponse(raw, 403, ""), nil
			}
			if id != "123" {
				t.Fatalf("unexpected post: %s", id)
			}
			return tiebaTestResponse(raw, 200, animePicturesTestPost(id, "/pictures/download_image/"+id+"-sample.jpg")), nil
		}
		if kind == "search" && u.Query().Get("page") == "1" {
			return tiebaTestResponse(raw, 200, `<div class="images-block"><div><a href="/posts/456"><img src="thumbnail"></a></div></div>`), nil
		}
		return nil, fmt.Errorf("unexpected URL %s", raw)
	})
	got, err := animePicturesCollect(context.Background(), getter, page, animePicturesTestDoc(t, first))
	if err == nil || !strings.Contains(err.Error(), "1 篇") || len(got.Items) != 1 || got.Items[0].Metadata["animepictures_post_id"] != "123" || len(calls) != 3 {
		t.Fatalf("posts=%+v calls=%v err=%v", got.Items, calls, err)
	}
}

func TestAnimePicturesSearchRejectsChangedFilter(t *testing.T) {
	page := animePicturesTestURL(t, "https://anime-pictures.net/posts?search_tag=cat&page=0")
	body := `<div class="images-block"><div><a href="/posts/123"><img src="thumbnail"></a></div></div>` +
		`<a href="/posts?search_tag=dog&amp;page=1">&gt;</a>`
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		return tiebaTestResponse(raw, 200, animePicturesTestPost("123", "/pictures/download_image/123-a.jpg")), nil
	})
	got, err := animePicturesCollect(context.Background(), getter, page, animePicturesTestDoc(t, body))
	if err == nil || len(got.Items) != 1 || calls != 1 {
		t.Fatalf("posts=%+v calls=%d err=%v", got.Items, calls, err)
	}
}

func TestAnimePicturesCancellationAndDenied(t *testing.T) {
	page := animePicturesTestURL(t, "https://anime-pictures.net/posts/123")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc := animePicturesTestDoc(t, animePicturesTestPost("123", "/pictures/download_image/123-a.jpg"))
	_, err := animePicturesPost(ctx, page, doc)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tiebaTestResponse(raw, 403, ""), nil
	})
	got, err := discoverAnimePictures(context.Background(), getter, page)
	var status *HTTPStatusError
	if len(got.Items) != 0 || !errors.As(err, &status) || status.StatusCode != 403 {
		t.Fatalf("403 should remain visible: %+v %v", got, err)
	}
}

func TestAnimePicturesSyntheticDownload(t *testing.T) {
	var data bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	page := animePicturesTestURL(t, "https://anime-pictures.net/posts/123")
	got, err := animePicturesPost(context.Background(), page, animePicturesTestDoc(t, animePicturesTestPost("123", "/pictures/download_image/123-sample.png")))
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("post=%+v err=%v", got, err)
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw != "https://anime-pictures.net/pictures/download_image/123-sample.png" {
			t.Fatalf("unexpected image URL: %s", raw)
		}
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(data.Bytes()))}, nil
	})
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(context.Background(), Job{Directory: dir}, got.Items[0], nil); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	f, err := os.Open(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := png.Decode(f); err != nil {
		t.Fatalf("downloaded image: %v", err)
	}
}

type animePicturesRoundTrip func(*http.Request) (*http.Response, error)

func (f animePicturesRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func animePicturesTestBrowser(t *testing.T, ctx context.Context, target, body string) (*RenderedSession, chan error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `<html></html>`) }))
	t.Cleanup(server.Close)
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
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
	session.start = animePicturesTestURL(t, target)
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".anime-pictures.net").WithURL("https://anime-pictures.net/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	return session, interceptErrors
}

func TestAnimePicturesRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const target = "https://anime-pictures.net/posts/123"
	body := animePicturesTestPost("123", "/pictures/download_image/123-sample.png") + `<img src="https://evil.test/recommended.png">`
	session, interceptErrors := animePicturesTestBrowser(t, ctx, target, body)
	calls := 0
	session.getter.client.Transport = animePicturesRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host == "anime-pictures.net" {
			if !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") {
				t.Errorf("same-origin request lost its synthetic session")
			}
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://api.anime-pictures.net/pictures/download_image/123-sample.png"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		if r.URL.Host != "api.anime-pictures.net" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://anime-pictures.net/" {
			t.Errorf("cross-origin request carries credentials or unexpected referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || session.PageURL() != target || got.Items[0].URL != "https://anime-pictures.net/pictures/download_image/123-sample.png" {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 2 {
		t.Fatalf("redirect calls=%d", calls)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestAnimePicturesRenderedSearch(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const target = "https://anime-pictures.net/posts?search_tag=cat&page=0"
	body := `<div class="images-block"><div><a href="/posts/123"><img src=""></a></div></div>` + `<a href="/posts?search_tag=cat&amp;page=1">&gt;</a>`
	session, interceptErrors := animePicturesTestBrowser(t, ctx, target, body)
	calls := 0
	session.getter.client.Transport = animePicturesRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "anime-pictures.net" || !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") {
			t.Errorf("unexpected site request: %s %v", r.URL, r.Header)
		}
		var body string
		switch r.URL.Path {
		case "/posts/123":
			body = animePicturesTestPost("123", "/pictures/download_image/123-image.jpg")
		case "/posts":
			if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("search_tag") != "cat" {
				t.Errorf("search filters changed: %s", r.URL)
			}
			body = `<div class="central-block"><div><a href="/posts/456"><img src=""></a></div></div>`
		case "/posts/456":
			body = animePicturesTestPost("456", "/pictures/download_image/456-image.jpg")
		default:
			t.Errorf("unexpected path %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || calls != 3 || session.PageURL() != target ||
		got.Items[0].Metadata["animepictures_post_id"] != "123" || got.Items[1].Metadata["animepictures_post_id"] != "456" {
		t.Fatalf("capture=%+v page=%s calls=%d err=%v", got, session.PageURL(), calls, err)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestAnimePicturesDiscoverRoute(t *testing.T) {
	const target = "https://anime-pictures.net/posts/123"
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw != target {
			t.Fatalf("unexpected request %s", raw)
		}
		body := animePicturesTestPost("123", "/pictures/download_image/123-image.jpg") + `<img src="https://evil.test/recommended.jpg">`
		return tiebaTestResponse(raw, 200, body), nil
	})
	got, err := Discover(context.Background(), getter, target, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["animepictures_post_id"] != "123" {
		t.Fatalf("discover=%+v err=%v", got, err)
	}
}
