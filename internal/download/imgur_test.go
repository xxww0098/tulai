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
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
)

func TestImgurPageURL(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		album   bool
	}{
		{"https://imgur.com/69OdJuh", "69OdJuh", false},
		{"https://imgur.com/a/lCVNc", "lCVNc", true},
		{"https://www.imgur.com/gallery/my-gallery-kx48l8e", "kx48l8e", true},
		{"https://m.imgur.com/a/lCVNc?token=private", "lCVNc", true},
		{"https://i.imgur.com/69OdJuh.png", "", false},
		{"https://imgur.com.evil.test/a/lCVNc", "", false},
		{"https://user@imgur.com/a/lCVNc", "", false},
		{"https://imgur.com:8443/a/lCVNc", "", false},
		{"https://imgur.com/a/abc", "", false},
		{"https://imgur.com/login", "", false},
		{"https://imgur.com/gallery", "", false},
		{"https://imgur.com/random", "", false},
		{"https://imgur.com/topics", "", false},
		{"https://imgur.com/a/lCVNc/other", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		id, album := imgurPageID(u)
		if id != tc.id || album != tc.album || isImgurPage(u) != (tc.id != "") {
			t.Errorf("imgurPageID(%q) = %q,%v, want %q,%v", tc.raw, id, album, tc.id, tc.album)
		}
	}
}

func imgurTestHTML(post imgurPost) string {
	payload, _ := json.Marshal(post)
	quoted, _ := json.Marshal(string(payload))
	value := strings.ReplaceAll(string(quoted), "'", `\'`)
	return `<title>fallback</title><script>window.postDataJSON=` + value + `;</script>`
}

func imgurTestGetter(body string) Getter {
	return GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
}

func TestImgurPostImages(t *testing.T) {
	count := 3
	post := imgurPost{ID: "lCVNc", Title: "artist's gallery", ImageCount: &count, Media: []imgurMedia{
		{URL: "https://i.imgur.com/one1234.png", Type: "image", MIMEType: "image/png"},
		{URL: "https://i.imgur.com/two1234.jpg", Type: "image", MIMEType: "image/jpeg"},
		{URL: "https://i.imgur.com/movie12.mp4", Type: "video", MIMEType: "video/mp4"},
	}}
	root, _ := url.Parse("https://imgur.com/a/lCVNc?token=private")
	got, err := discoverImgurPage(context.Background(), imgurTestGetter(imgurTestHTML(post)), root)
	if err != nil || !got.IsPage || got.Title != post.Title || len(got.Items) != 2 || got.Items[0].URL != post.Media[0].URL || got.Items[1].URL != post.Media[1].URL {
		t.Fatalf("post = %+v, err = %v", got, err)
	}
	for _, item := range got.Items {
		if item.Headers["Referer"] != "https://imgur.com/" {
			t.Errorf("referer = %q", item.Headers["Referer"])
		}
	}
}

func TestImgurPartialAndUnsafeMedia(t *testing.T) {
	count := 7
	post := imgurPost{ID: "lCVNc", ImageCount: &count, Media: []imgurMedia{
		{URL: "https://i.imgur.com/ok12345.png", MIMEType: "image/png"},
		{URL: "https://i.imgur.com/ok12345.png", MIMEType: "image/png"},
		{URL: "http://127.0.0.1/private.jpg", MIMEType: "image/jpeg"},
		{URL: "https://i.imgur.com.evil.test/x.jpg", MIMEType: "image/jpeg"},
		{URL: "https://user@i.imgur.com/x.jpg", MIMEType: "image/jpeg"},
		{URL: "https://i.imgur.com:8443/x.jpg", MIMEType: "image/jpeg"},
	}}
	root, _ := url.Parse("https://imgur.com/a/lCVNc")
	got, err := discoverImgurPage(context.Background(), imgurTestGetter(imgurTestHTML(post)), root)
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != post.Media[0].URL || !strings.Contains(err.Error(), "不完整") || !strings.Contains(err.Error(), "无效") {
		t.Fatalf("partial post = %+v, err = %v", got, err)
	}
}

func TestImgurCoverAndLegacy(t *testing.T) {
	for _, tc := range []struct {
		name, root, html string
		count            int
		partial          bool
	}{
		{"single", "https://imgur.com/69OdJuh", `<meta property="og:image" content="https://i.imgur.com/69OdJuh.png?fb">`, 1, false},
		{"wrong single", "https://imgur.com/69OdJuh", `<meta property="og:image" content="https://i.imgur.com/other12.png">`, 0, true},
		{"album cover", "https://imgur.com/a/lCVNc", `<meta property="og:image" content="https://i.imgur.com/69OdJuh.png?fb">`, 1, true},
		{"unsafe cover", "https://imgur.com/a/lCVNc", `<meta property="og:image" content="https://i.imgur.com.evil.test/image.png">`, 0, true},
		{"legacy", "https://imgur.com/a/lCVNc", `<script>widgetFactory.mergeConfig('gallery', {"id":"lCVNc","image_count":2,"images":[{"hash":"one1234","ext":".jpg"},{"hash":"two1234","ext":".png"}]});</script>`, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := url.Parse(tc.root)
			got, err := discoverImgurPage(context.Background(), imgurTestGetter(tc.html), root)
			if len(got.Items) != tc.count || (err != nil) != tc.partial {
				t.Fatalf("result=%+v, err=%v", got.Items, err)
			}
		})
	}
}

func TestImgurRejectsWrongPostAndKeepsPartial(t *testing.T) {
	count := 1
	post := imgurPost{ID: "other12", ImageCount: &count, Media: []imgurMedia{{URL: "https://i.imgur.com/other12.png", MIMEType: "image/png"}}}
	root, _ := url.Parse("https://imgur.com/a/lCVNc")
	got, err := discoverImgurPage(context.Background(), imgurTestGetter(imgurTestHTML(post)), root)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "当前帖子") {
		t.Fatalf("wrong post = %+v, %v", got.Items, err)
	}
	post.ID, post.ImageCount = "lCVNc", nil
	got, err = discoverImgurPage(context.Background(), imgurTestGetter(imgurTestHTML(post)), root)
	if err == nil || len(got.Items) != 1 || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("unknown media count = %+v, %v", got.Items, err)
	}
}

func TestImgurCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse("https://imgur.com/a/lCVNc")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("canceled request reached network")
		return nil, nil
	})
	if _, err := discoverImgurPage(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
	getter = GetterFunc(func(_ context.Context, _ string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: "https://evil.test/a/lCVNc", Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("<html></html>"))}, nil
	})
	if _, err := discoverImgurPage(context.Background(), getter, root); err == nil {
		t.Fatal("external redirect accepted")
	}
}

func TestImgurRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html><title>placeholder</title></html>")
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	const target = "https://imgur.com/a/lCVNc?token=synthetic"
	count := 2
	post := imgurPost{ID: "lCVNc", Title: "当前相册", ImageCount: &count, Media: []imgurMedia{
		{URL: "https://i.imgur.com/one1234.png", MIMEType: "image/png"},
		{URL: "https://i.imgur.com/two1234.jpg", MIMEType: "image/jpeg"},
	}}
	page := `<html><head>` + imgurTestHTML(post) + `</head><body><img src="https://unrelated.invalid/recommendation.jpg"></body></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(page)))
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
	if err != nil || !got.IsPage || got.Title != post.Title || len(got.Items) != 2 ||
		got.Items[0].URL != post.Media[0].URL || got.Items[1].URL != post.Media[1].URL {
		t.Fatalf("captured album = %+v, err = %v", got, err)
	}
	if session.PageURL() != target {
		t.Fatalf("captured page URL = %q", session.PageURL())
	}
	for _, item := range got.Items {
		if ref := item.Headers["Referer"]; ref != "https://imgur.com/" {
			t.Fatalf("image referer = %q", ref)
		}
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestImgurLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_IMGUR_LIVE") != "1" {
		t.Skip("set TULAI_IMGUR_LIVE=1 to check a public Imgur album")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, _ := url.Parse("https://imgur.com/a/lCVNc")
	getter := NewHTTPGetter("")
	got, err := discoverImgurPage(ctx, getter, root)
	if err != nil || len(got.Items) < 10 {
		t.Fatalf("live album: %d images, %v", len(got.Items), err)
	}
	singleURL, _ := url.Parse("https://imgur.com/69OdJuh")
	single, err := discoverImgurPage(ctx, getter, singleURL)
	if err != nil || len(single.Items) != 1 || !strings.HasPrefix(single.Items[0].URL, "https://i.imgur.com/69OdJuh.png") {
		t.Fatalf("live single image: %+v, %v", single.Items, err)
	}
	item := got.Items[0]
	item.RequireImage = true
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: t.TempDir()}, item, nil); err != nil {
		t.Fatal(fmt.Errorf("download public image: %w", err))
	}
	t.Logf("public album: %d images, single image, and one verified download", len(got.Items))
}
