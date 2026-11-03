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

const tumblrSampleID = "701641653313748992"
const tumblrSampleURL = "https://www.tumblr.com/staff/701641653313748992"
const tumblrSampleBody = `<html><head><title>Tumblr</title><meta property="og:url" content="https://www.tumblr.com/staff/701641653313748992/example"><meta property="og:image" content="https://64.media.tumblr.com/example/s640x960/cover.jpg"></head><body>
<script id="___INITIAL_STATE___" type="application/json">{"PeeprRoute":{"initialTimeline":{"objects":[
{"idString":"701641653313748992","blogName":"staff","summary":"Post","content":[
{"type":"image","media":[{"url":"https://64.media.tumblr.com/post/s640/small.jpg","width":640,"height":480},{"url":"https://64.media.tumblr.com/post/s1280/large.jpg","width":1280,"height":960}]},
{"type":"image","media":{"url":"https://64.media.tumblr.com/post/original/second.png","width":900,"height":900}},
{"type":"video","media":[{"url":"https://64.media.tumblr.com/post/video/thumbnail.jpg","width":1280,"height":720}]}],
"trail":[{"content":[{"type":"image","media":[{"url":"https://64.media.tumblr.com/trail/original/third.jpg","width":700,"height":700}]}]}],
"photos":[{"original_size":{"url":"https://64.media.tumblr.com/legacy/original/fourth.jpg","width":1000,"height":700},"alt_sizes":[{"url":"https://64.media.tumblr.com/legacy/small/fourth.jpg","width":500,"height":350}]}]},
{"idString":"999999999999999999","blogName":"staff","content":[{"type":"image","media":[{"url":"https://64.media.tumblr.com/unrelated/recommendation.jpg","width":1000,"height":1000}]}]}]}}}</script>
<img src="https://64.media.tumblr.com/avatar/irrelevant.jpg"></body></html>`

func tumblrTestResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestTumblrPostURLs(t *testing.T) {
	for _, tc := range []struct{ raw, blog, id string }{
		{tumblrSampleURL, "staff", tumblrSampleID},
		{"https://www.tumblr.com/staff/701641653313748992/example", "staff", tumblrSampleID},
		{"https://staff.tumblr.com/post/701641653313748992/example", "staff", tumblrSampleID},
		{"https://tumblr.com/staff/701641653313748992", "staff", tumblrSampleID},
		{"https://staff.tumblr.com/post/701641653313748992/extra/more", "", ""},
		{"https://www.tumblr.com/staff", "", ""},
		{"https://www.tumblr.com/staff/likes", "", ""},
		{"https://staff.tumblr.com.evil.test/post/701641653313748992", "", ""},
		{"https://evil.staff.tumblr.com/post/701641653313748992", "", ""},
		{"https://user@staff.tumblr.com/post/701641653313748992", "", ""},
		{"https://staff.tumblr.com:8443/post/701641653313748992", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := tumblrPageRef(u)
		if got.blog != tc.blog || got.id != tc.id || isTumblrPost(u) != (tc.id != "") {
			t.Errorf("ref(%q)=%+v", tc.raw, got)
		}
	}
}

func TestTumblrMediaBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://64.media.tumblr.com.evil.test/post/a.jpg",
		"https://user@64.media.tumblr.com/post/a.jpg",
		"https://64.media.tumblr.com:8443/post/a.jpg",
		"http://64.media.tumblr.com/post/a.jpg",
		"https://evil.media.tumblr.com/post/a.jpg",
		"https://64.media.tumblr.com/post/../a.jpg",
		"https://127.0.0.1/post/a.jpg",
	} {
		if _, ok := tumblrMediaURL(raw); ok {
			t.Errorf("accepted %q", raw)
		}
	}
	if _, ok := tumblrMediaURL("https://64.media.tumblr.com/post/a.jpg?token=synthetic"); !ok {
		t.Fatal("valid media rejected")
	}
}

func TestTumblrImageSetAndTrail(t *testing.T) {
	root, _ := url.Parse(tumblrSampleURL + "?token=synthetic")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tumblrTestResponse(raw, tumblrSampleBody), nil
	})
	got, err := discoverTumblrPost(context.Background(), getter, root)
	if err != nil || got.Title != "Post" || len(got.Items) != 4 {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if !strings.Contains(got.Items[0].URL, "/large.jpg") || !strings.Contains(got.Items[1].URL, "/second.png") || !strings.Contains(got.Items[2].URL, "/third.jpg") || !strings.Contains(got.Items[3].URL, "/fourth.jpg") {
		t.Fatalf("wrong media order: %+v", got.Items)
	}
	for _, item := range got.Items {
		if strings.Contains(item.URL, "recommendation") || item.Headers["Referer"] != "https://www.tumblr.com/" {
			t.Fatalf("wrong item %+v", item)
		}
	}
}

func TestTumblrLegacyPhotosResponse(t *testing.T) {
	root, _ := url.Parse(tumblrSampleURL)
	body := `<script id="___INITIAL_STATE___" type="application/json">{"cache":{"response":{"posts":[{"id":701641653313748992,"blog_name":"staff","photos":[{"original_size":{"url":"https://64.media.tumblr.com/original/first.jpg","width":1280,"height":900},"alt_sizes":[{"url":"https://64.media.tumblr.com/small/first.jpg","width":540,"height":380}]}]}]}}}</script>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tumblrTestResponse(raw, body), nil
	})
	got, err := discoverTumblrPost(context.Background(), getter, root)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://64.media.tumblr.com/original/first.jpg" {
		t.Fatalf("legacy post=%+v err=%v", got, err)
	}
}

func TestTumblrIdentityFallbackAndPartial(t *testing.T) {
	root, _ := url.Parse(tumblrSampleURL)
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"different-id", strings.ReplaceAll(tumblrSampleBody, tumblrSampleID, "999999999999999999"), 0},
		{"different-blog", strings.ReplaceAll(strings.ReplaceAll(tumblrSampleBody, "/staff/", "/other/"), `"blogName":"staff"`, `"blogName":"other"`), 0},
		{"blocked-media", strings.Replace(tumblrSampleBody, "https://64.media.tumblr.com/post/original/second.png", "https://evil.test/second.png", 1), 3},
		{"og-only", `<meta property="og:url" content="https://www.tumblr.com/staff/701641653313748992"><meta property="og:image" content="https://64.media.tumblr.com/post/cover.jpg">`, 1},
		{"og-without-id", `<meta property="og:image" content="https://64.media.tumblr.com/post/cover.jpg">`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return tumblrTestResponse(raw, tc.body), nil
			})
			got, err := discoverTumblrPost(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestTumblrRejectsConflictingPostIdentity(t *testing.T) {
	root, _ := url.Parse(tumblrSampleURL)
	body := strings.Replace(tumblrSampleBody, `"idString":"701641653313748992"`, `"idString":"701641653313748992","id":"999999999999999999"`, 1)
	body = strings.Replace(body, `<meta property="og:image"`, `<meta property="og:image:unused"`, 1)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return tumblrTestResponse(raw, body), nil
	})
	got, err := discoverTumblrPost(context.Background(), getter, root)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("conflicting identity=%+v err=%v", got.Items, err)
	}
}

func TestTumblrRedirectAndCancellation(t *testing.T) {
	root, _ := url.Parse("https://staff.tumblr.com/post/" + tumblrSampleID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discoverTumblrPost(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := tumblrTestResponse(raw, tumblrSampleBody)
		resp.FinalURL = tumblrSampleURL
		return resp, nil
	})
	if got, err := discoverTumblrPost(context.Background(), getter, root); err != nil || len(got.Items) != 4 {
		t.Fatalf("valid redirect=%+v err=%v", got, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := tumblrTestResponse(raw, tumblrSampleBody)
		resp.FinalURL = "https://evil.test/staff/" + tumblrSampleID
		return resp, nil
	})
	if got, err := discoverTumblrPost(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("unsafe redirect=%+v err=%v", got, err)
	}
}

type tumblrRoundTripper func(*http.Request) (*http.Response, error)

func (fn tumblrRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestTumblrRenderedCapture(t *testing.T) {
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
	const target = tumblrSampleURL + "?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = tumblrRoundTripper(func(*http.Request) (*http.Response, error) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(tumblrSampleBody)))
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
	if err != nil || len(got.Items) != 4 || calls.Load() != 0 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestTumblrLivePostDownload(t *testing.T) {
	if os.Getenv("TULAI_TUMBLR_LIVE") != "1" {
		t.Skip("set TULAI_TUMBLR_LIVE=1 to verify a public Tumblr post")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, _ := url.Parse(tumblrSampleURL)
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, root.String(), 1)
	if err != nil || len(got.Items) < 20 {
		t.Fatalf("post images=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "tumblr"
	item.RequireImage = true
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
	t.Logf("public post images=%d first=%dx%d %s", len(got.Items), im.Width, im.Height, format)
}
