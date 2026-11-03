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

const onexTestPhoto = "https://1x.com/photo/123"
const onexTestGallery = "https://gallery.1x.com/photos"
const onexTestImage1 = "https://1x.com/images/user/1234567890abcdef1234567890abcdef-hd4.jpg"
const onexTestImage2 = "https://1x.com/images/user/2234567890abcdef1234567890abcdef-hd2.jpg"

func onexPhotoFixture(id, image string) string {
	return `<html><head><meta property="og:url" content="/photo/` + id + `"><meta property="og:title" content="Test photo"></head><body><div id="imgcontainer-` + id + `"><img id="img-` + id + `" src="` + image + `"></div><div id="imgcontainer-999"><img id="img-999" src="https://1x.com/images/user/recommended.jpg"></div></body></html>`
}
func onexGalleryFixture() string {
	return `<html><head><title>Gallery</title></head><body><div id="photos-feed"><div id="imgcontainer-123"><img id="img-123" src="` + onexTestImage1 + `"></div><div id="imgcontainer-456"><img id="img-456" src="` + onexTestImage2 + `"></div><div id="imgcontainer-789"><img id="img-999" src="https://1x.com/images/user/mismatch.jpg"></div></div><div id="imgcontainer-777"><img id="img-777" src="https://1x.com/images/user/recommendation.jpg"></div></body></html>`
}
func onexResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestOnexURLBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw            string
		photo, gallery bool
	}{
		{onexTestPhoto, true, false}, {"https://gallery.1x.com/photo/1709089", true, false}, {"https://1x.com/photo/123/", true, false},
		{onexTestGallery, false, true}, {"https://1x.com/photos/latest/published", false, true},
		{"https://1x.com/photo/0", false, false}, {"https://1x.com/photo/123/other", false, false}, {"https://1x.com/gallery/feed", false, false},
		{"https://1x.com.evil.test/photo/123", false, false}, {"https://u@1x.com/photo/123", false, false}, {"https://1x.com:444/photo/123", false, false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if isOnexPhoto(u) != tc.photo || isOnexGallery(u) != tc.gallery {
			t.Errorf("url %q photo=%v gallery=%v", tc.raw, isOnexPhoto(u), isOnexGallery(u))
		}
	}
	for _, raw := range []string{
		"http://1x.com/images/user/x.jpg", "https://evil.test/images/user/x.jpg", "https://1x.com.evil.test/images/user/x.jpg",
		"https://u@1x.com/images/user/x.jpg", "https://1x.com:444/images/user/x.jpg", "https://1x.com/images/user/../x.jpg",
		"https://1x.com/images/profile/avatar.jpg", onexTestImage1 + "?secret=x", onexTestImage1 + "#fragment",
	} {
		if _, ok := onexImageURL(raw); ok {
			t.Errorf("accepted %q", raw)
		}
	}
}

func TestOnexPublicPhoto(t *testing.T) {
	root, _ := url.Parse(onexTestPhoto + "?tracking=sensitive")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return onexResponse(raw, onexPhotoFixture("123", onexTestImage1)), nil
	})
	got, err := Discover(context.Background(), getter, root.String(), 1)
	if err == nil || !strings.Contains(err.Error(), "展示图") || got.Title != "Test photo" || len(got.Items) != 1 || got.Items[0].URL != onexTestImage1 {
		t.Fatalf("items=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	item := got.Items[0]
	if !item.PublicOnly || !item.RequireImage || item.Metadata["onex_photo_id"] != "123" || item.Headers["Referer"] != "https://1x.com/" {
		t.Fatalf("item=%+v", item)
	}
}

func TestOnexPhotoIdentity(t *testing.T) {
	root, _ := url.Parse(onexTestPhoto)
	for _, tc := range []struct{ name, body string }{
		{"wrong og", onexPhotoFixture("999", onexTestImage1)},
		{"missing og", strings.Replace(onexPhotoFixture("123", onexTestImage1), `<meta property="og:url" content="/photo/123">`, "", 1)},
		{"wrong container", strings.Replace(onexPhotoFixture("123", onexTestImage1), `imgcontainer-123`, `imgcontainer-456`, 1)},
		{"untrusted source", onexPhotoFixture("123", "https://evil.test/images/user/x.jpg")},
		{"conflicting source", strings.Replace(onexPhotoFixture("123", onexTestImage1), `</div>`, `<img id="img-123" src="`+onexTestImage2+`"></div>`, 1)},
		{"login shell", `<html><head><meta property="og:url" content="/photo/123"></head><body>Log in</body></html>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return onexResponse(raw, tc.body), nil
			})
			got, err := discoverOnex(context.Background(), getter, root)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestOnexGalleryLoadedOnly(t *testing.T) {
	root, _ := url.Parse(onexTestGallery)
	var calls int
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		return onexResponse(raw, onexGalleryFixture()), nil
	})
	got, err := discoverOnex(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "当前加载") || len(got.Items) != 2 || calls != 1 {
		t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
	}
	if got.Items[0].URL != onexTestImage1 || got.Items[1].URL != onexTestImage2 || got.Items[1].Metadata["onex_photo_id"] != "456" {
		t.Fatalf("items=%+v", got.Items)
	}
}

func TestOnexRedirectAndCancel(t *testing.T) {
	root, _ := url.Parse(onexTestPhoto)
	for _, target := range []string{"https://1x.com/photo/456", "https://1x.com/login", "https://evil.test/photo/123"} {
		getter := GetterFunc(func(_ context.Context, _ string, _ func(*http.Request)) (*Response, error) {
			return onexResponse(target, onexPhotoFixture("123", onexTestImage1)), nil
		})
		if got, err := discoverOnex(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
			t.Fatalf("redirect=%s items=%+v err=%v", target, got.Items, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("cancelled request")
		return nil, nil
	})
	if _, err := discoverOnex(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestOnexRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Chrome")
	}
	for _, tc := range []struct {
		name, target, body string
		count              int
	}{
		{"photo", onexTestPhoto, onexPhotoFixture("123", onexTestImage1), 1}, {"gallery", onexTestGallery, onexGalleryFixture(), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
					if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == tc.target {
						action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(tc.body)))
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
			start, _ := url.Parse(tc.target)
			session.start = start
			if err := session.run(ctx, camoufox.Navigate(tc.target)); err != nil {
				t.Fatal(err)
			}
			got, err := session.Capture(ctx)
			select {
			case err := <-responses:
				t.Fatalf("intercept: %v", err)
			default:
			}
			if err == nil || len(got.Items) != tc.count || got.Items[0].URL != onexTestImage1 {
				t.Fatalf("capture=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestOnexPublicLive(t *testing.T) {
	if os.Getenv("TULAI_ONEX_LIVE") != "1" {
		t.Skip("set TULAI_ONEX_LIVE=1 for public photo tests")
	}
	for _, raw := range []string{"https://gallery.1x.com/photo/1709089", "https://1x.com/photo/57213"} {
		t.Run(raw, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			getter := NewHTTPGetter("")
			got, err := Discover(ctx, getter, raw, 1)
			if err == nil || !strings.Contains(err.Error(), "展示图") || len(got.Items) != 1 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
			item := got.Items[0]
			item.Filename = "onex-public"
			dir := t.TempDir()
			if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
				t.Fatalf("download: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("files=%v err=%v", entries, err)
			}
			info, err := entries[0].Info()
			if err != nil || info.Size() == 0 {
				t.Fatalf("info=%v err=%v", info, err)
			}
			t.Logf("visible photo %s downloaded %d bytes", item.Metadata["onex_photo_id"], info.Size())
		})
	}
}

func TestOnexGalleryPublicLive(t *testing.T) {
	if os.Getenv("TULAI_ONEX_LIVE") != "1" {
		t.Skip("set TULAI_ONEX_LIVE=1 for a public gallery in an isolated browser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), onexTestGallery, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.run(ctx, chromedp.Sleep(8*time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "当前加载") || len(got.Items) < 2 {
		t.Fatalf("gallery images=%d err=%v", len(got.Items), err)
	}
	dir := t.TempDir()
	item := got.Items[0]
	item.Filename = "onex-gallery"
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("gallery image download: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("gallery files=%v err=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil || info.Size() == 0 {
		t.Fatalf("gallery image info=%v err=%v", info, err)
	}
	t.Logf("loaded %d gallery previews and downloaded %d bytes", len(got.Items), info.Size())
}
