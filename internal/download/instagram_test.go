package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func instagramTestHTML(media map[string]any) string {
	value, _ := json.Marshal(map[string]any{"graphql": map[string]any{"shortcode_media": media}})
	return `<html><head><title>Post</title><script>window._sharedData=` + string(value) + `;</script></head><body><img src="https://scontent.cdninstagram.com/v/avatar.jpg"></body></html>`
}

func instagramTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestInstagramPostURLs(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{"https://www.instagram.com/p/AbcDe123_/", "AbcDe123_"},
		{"https://instagram.com/reel/AbcDe123_/", "AbcDe123_"},
		{"https://www.instagram.com/test.user/p/AbcDe123_/", "AbcDe123_"},
		{"https://m.instagram.com/p/AbcDe123_/?token=private", "AbcDe123_"},
		{"https://instagram.com/p/abc/", ""},
		{"https://instagram.com/stories/example/", ""},
		{"https://instagram.com/p/AbcDe123_/embed/", ""},
		{"https://instagram.com.evil.test/p/AbcDe123_/", ""},
		{"https://user@instagram.com/p/AbcDe123_/", ""},
		{"https://instagram.com:8443/p/AbcDe123_/", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := instagramPostID(u); got != tc.id || isInstagramPost(u) != (tc.id != "") {
			t.Errorf("post ID %q = %q", tc.raw, got)
		}
	}
}

func TestInstagramSingleAndCarousel(t *testing.T) {
	root, _ := url.Parse("https://www.instagram.com/p/AbcDe123_/?secret=private")
	media := map[string]any{"shortcode": "AbcDe123_", "__typename": "GraphImage", "is_video": false, "display_url": "https://scontent-abc1-1.cdninstagram.com/v/original.jpg?signature=synthetic"}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return instagramTestResponse(raw, instagramTestHTML(media)), nil
	})
	got, err := discoverInstagramPost(context.Background(), getter, root)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != media["display_url"] || got.Items[0].Headers["Referer"] != "https://www.instagram.com/" {
		t.Fatalf("single = %+v, %v", got.Items, err)
	}
	media = map[string]any{"shortcode": "AbcDe123_", "__typename": "GraphSidecar", "edge_sidecar_to_children": map[string]any{
		"count": 3, "page_info": map[string]any{"has_next_page": false}, "edges": []any{
			map[string]any{"node": map[string]any{"__typename": "GraphImage", "display_url": "https://scontent.cdninstagram.com/v/one.jpg"}},
			map[string]any{"node": map[string]any{"__typename": "GraphVideo", "display_url": "https://scontent.cdninstagram.com/v/video-cover.jpg"}},
			map[string]any{"node": map[string]any{"__typename": "GraphImage", "display_src": "https://instagram.fsjc1-3.fna.fbcdn.net/v/two.png"}},
		},
	}}
	got, err = discoverInstagramPost(context.Background(), getter, root)
	if err != nil || len(got.Items) != 2 || !strings.Contains(got.Items[0].URL, "/one.jpg") || !strings.Contains(got.Items[1].URL, "/two.png") {
		t.Fatalf("carousel = %+v, %v", got.Items, err)
	}
}

func TestInstagramBestCandidateAndPartialCarousel(t *testing.T) {
	root, _ := url.Parse("https://instagram.com/p/AbcDe123_/")
	candidate := map[string]any{"media_type": 1, "image_versions2": map[string]any{"candidates": []any{
		map[string]any{"url": "https://scontent.cdninstagram.com/v/small.jpg", "width": 320, "height": 320},
		map[string]any{"url": "https://scontent.cdninstagram.com/v/large.jpg", "width": 1080, "height": 1080},
	}}}
	media := map[string]any{"shortcode": "AbcDe123_", "__typename": "GraphSidecar", "edge_sidecar_to_children": map[string]any{"count": 2, "page_info": map[string]any{"has_next_page": true}, "edges": []any{map[string]any{"node": candidate}}}}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return instagramTestResponse(raw, instagramTestHTML(media)), nil
	})
	got, err := discoverInstagramPost(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 || !strings.Contains(got.Items[0].URL, "large.jpg") || !strings.Contains(err.Error(), "不完整") {
		t.Fatalf("partial carousel = %+v, %v", got.Items, err)
	}
}

func TestInstagramBoundPreviewAndWrongPost(t *testing.T) {
	root, _ := url.Parse("https://www.instagram.com/p/AbcDe123_/")
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"bound", `<meta property="og:url" content="https://www.instagram.com/p/AbcDe123_/"><meta property="og:image" content="https://scontent.cdninstagram.com/v/cover.jpg">`, 1},
		{"no canonical", `<meta property="og:image" content="https://scontent.cdninstagram.com/v/avatar.jpg">`, 0},
		{"wrong canonical", `<meta property="og:url" content="https://www.instagram.com/p/Other999/"><meta property="og:image" content="https://scontent.cdninstagram.com/v/avatar.jpg">`, 0},
		{"forged canonical", `<meta property="og:url" content="https://instagram.com.evil.test/p/AbcDe123_/"><meta property="og:image" content="https://scontent.cdninstagram.com/v/avatar.jpg">`, 0},
		{"wrong post", instagramTestHTML(map[string]any{"shortcode": "Other999", "__typename": "GraphImage", "display_url": "https://scontent.cdninstagram.com/v/other.jpg"}), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return instagramTestResponse(raw, tc.body), nil
			})
			got, err := discoverInstagramPost(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("preview = %+v, %v", got.Items, err)
			}
		})
	}
}

func TestInstagramImageSafetyAndUnknownMedia(t *testing.T) {
	root, _ := url.Parse("https://www.instagram.com/p/AbcDe123_/")
	for _, raw := range []string{
		"https://scontent.cdninstagram.com.evil.test/v/x.jpg", "https://user@scontent.cdninstagram.com/v/x.jpg",
		"https://scontent.cdninstagram.com:8443/v/x.jpg", "http://scontent.cdninstagram.com/v/x.jpg",
		"https://127.0.0.1/v/x.jpg", "https://evil.fbcdn.net/v/x.jpg", "https://scontent.cdninstagram.com/../v/x.jpg",
	} {
		if _, ok := instagramImageURL(root, raw); ok {
			t.Errorf("unsafe image accepted: %q", raw)
		}
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return instagramTestResponse(raw, instagramTestHTML(map[string]any{"shortcode": "AbcDe123_", "display_url": "https://scontent.cdninstagram.com/v/mystery.jpg"})), nil
	})
	got, err := discoverInstagramPost(context.Background(), getter, root)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("unknown media = %+v, %v", got.Items, err)
	}
}

func TestInstagramCancellationRedirectAndSize(t *testing.T) {
	root, _ := url.Parse("https://www.instagram.com/p/AbcDe123_/")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discoverInstagramPost(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel = %v", err)
	}
	for _, kind := range []string{"redirect", "login", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				resp := instagramTestResponse(raw, instagramTestHTML(map[string]any{"shortcode": "AbcDe123_", "__typename": "GraphImage", "display_url": "https://scontent.cdninstagram.com/v/photo.jpg"}))
				switch kind {
				case "redirect":
					resp.FinalURL = "https://evil.test/p/AbcDe123_/"
				case "login":
					resp.FinalURL = "https://www.instagram.com/accounts/login/"
				case "oversize":
					resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxPageBytes+1)))
				}
				return resp, nil
			})
			got, err := discoverInstagramPost(context.Background(), getter, root)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("unsafe response = %+v, %v", got.Items, err)
			}
		})
	}
}

func TestInstagramRenderedCapture(t *testing.T) {
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
	const target = "https://www.instagram.com/p/AbcDe123_/?token=synthetic"
	media := map[string]any{"shortcode": "AbcDe123_", "__typename": "GraphSidecar", "edge_sidecar_to_children": map[string]any{"count": 2, "edges": []any{
		map[string]any{"node": map[string]any{"__typename": "GraphImage", "display_url": "https://scontent.cdninstagram.com/v/one.jpg"}},
		map[string]any{"node": map[string]any{"__typename": "GraphImage", "display_url": "https://scontent.cdninstagram.com/v/two.jpg"}},
	}}}
	page := instagramTestHTML(media)
	var requests atomic.Int32
	session.getter.client.Transport = instagramTestTransport(func(*http.Request) (*http.Response, error) {
		requests.Add(1)
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
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.instagram.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || requests.Load() != 0 || strings.Contains(got.Items[0].URL, "avatar") {
		t.Fatalf("capture = %+v, requests %d, err %v", got.Items, requests.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

type instagramTestTransport func(*http.Request) (*http.Response, error)

func (f instagramTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
