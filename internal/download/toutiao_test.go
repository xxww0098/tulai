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

const toutiaoPublicArticle = "https://www.toutiao.com/article/7624569776911729188/"
const toutiaoPublicArticle2 = "https://www.toutiao.com/article/7229609497856426548/"
const toutiaoFixture = `<html><head><title>测试文章-今日头条</title><link rel="canonical" href="https://www.toutiao.com/article/7624569776911729188/"></head><body>
<img src="https://p3-sign.toutiaoimg.com/avatar.jpg">
<div class="show-monitor"><p><img src="https://p3-sign.toutiaoimg.com/tos-cn-i-example/first~tplv.jpeg?token=synthetic"></p><img src="//p26.toutiaoimg.com/origin/second.jpg"><img src="//p26.toutiaoimg.com/origin/second.jpg"></div>
<img src="https://p3-sign.toutiaoimg.com/recommendation.jpg"></body></html>`

func toutiaoTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestToutiaoArticleURLs(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{toutiaoPublicArticle, true},
		{"https://toutiao.com/article/7624569776911729188?x=1", true},
		{"https://m.toutiao.com/article/7624569776911729188/", true},
		{toutiaoPublicArticle + "extra", false},
		{"https://www.toutiao.com/article/0/", false},
		{"https://www.toutiao.com/article/abc/", false},
		{"https://www.toutiao.com/w/1747556931189775/", false},
		{"https://www.toutiao.com/group/7624569776911729188/", false},
		{"https://www.toutiao.com.evil.test/article/7624569776911729188/", false},
		{"https://user@www.toutiao.com/article/7624569776911729188/", false},
		{"https://www.toutiao.com:8443/article/7624569776911729188/", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if isToutiaoArticle(u) != tc.ok {
			t.Errorf("isToutiaoArticle(%q)=%v", tc.raw, isToutiaoArticle(u))
		}
	}
}

func TestToutiaoImageBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://p3-sign.toutiaoimg.com.evil.test/origin/a.jpg",
		"https://p3-sign.toutiaoimg.com@evil.test/origin/a.jpg",
		"https://user@p3-sign.toutiaoimg.com/origin/a.jpg",
		"https://p3-sign.toutiaoimg.com:8443/origin/a.jpg",
		"http://p3-sign.toutiaoimg.com/origin/a.jpg",
		"https://127.0.0.1/origin/a.jpg",
		"https://p3-sign.toutiaoimg.com/origin/../private.jpg",
		"https://p3-sign.toutiaoimg.com/origin//a.jpg",
		"data:image/png;base64,AAAA",
	} {
		if _, ok := toutiaoImageURL(raw); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	for _, raw := range []string{"//p26.toutiaoimg.com/origin/a.jpg", "https://p11-sign.toutiaoimg.com/origin/a.jpg"} {
		if _, ok := toutiaoImageURL(raw); !ok {
			t.Errorf("valid image rejected %q", raw)
		}
	}
}

func TestToutiaoArticleAndIdentity(t *testing.T) {
	root, _ := url.Parse(toutiaoPublicArticle + "?token=synthetic")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return toutiaoTestResponse(raw, toutiaoFixture), nil
	})
	got, err := discoverToutiaoArticle(context.Background(), getter, root)
	if err != nil || got.Title != "测试文章" || len(got.Items) != 2 {
		t.Fatalf("images=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Metadata["toutiao_article_id"] != "7624569776911729188" || item.Headers["Referer"] != "https://www.toutiao.com/" || strings.Contains(item.URL, "avatar") || strings.Contains(item.URL, "recommendation") {
			t.Fatalf("unexpected image %+v", item)
		}
	}
	wrong := strings.Replace(toutiaoFixture, toutiaoPublicArticle, toutiaoPublicArticle2, 1)
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return toutiaoTestResponse(raw, wrong), nil
	})
	if got, err := discoverToutiaoArticle(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("wrong identity images=%+v err=%v", got.Items, err)
	}
}

func TestToutiaoPartialAndBlocked(t *testing.T) {
	root, _ := url.Parse(toutiaoPublicArticle)
	unsafe := strings.ReplaceAll(toutiaoFixture, "//p26.toutiaoimg.com/origin/second.jpg", "https://evil.test/origin/second.jpg")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return toutiaoTestResponse(raw, unsafe), nil
	})
	got, err := discoverToutiaoArticle(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 {
		t.Fatalf("partial images=%+v err=%v", got.Items, err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return toutiaoTestResponse(raw, `<html><body></body><script>var challenge=true;</script><img src="https://p3-sign.toutiaoimg.com/avatar.jpg"></html>`), nil
	})
	if got, err := discoverToutiaoArticle(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("empty page images=%+v err=%v", got.Items, err)
	}
}

func TestToutiaoCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(toutiaoPublicArticle)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverToutiaoArticle(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := toutiaoTestResponse(raw, toutiaoFixture)
		resp.FinalURL = toutiaoPublicArticle2
		return resp, nil
	})
	if got, err := discoverToutiaoArticle(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect images=%+v err=%v", got.Items, err)
	}
}

type toutiaoRoundTripper func(*http.Request) (*http.Response, error)

func (fn toutiaoRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestToutiaoRenderedCapture(t *testing.T) {
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
	const target = toutiaoPublicArticle + "?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = toutiaoRoundTripper(func(*http.Request) (*http.Response, error) {
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(toutiaoFixture)))
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
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestToutiaoLiveArticleDownload(t *testing.T) {
	if os.Getenv("TULAI_TOUTIAO_LIVE") != "1" {
		t.Skip("set TULAI_TOUTIAO_LIVE=1 to verify a public article")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, toutiaoPublicArticle, 1)
	if err != nil {
		if len(got.Items) != 0 {
			t.Fatalf("public article partial images=%d err=%v", len(got.Items), err)
		}
		t.Skipf("public article did not provide readable image content: %v", err)
	}
	if len(got.Items) == 0 {
		t.Fatal("public article yielded no images")
	}
	item := got.Items[0]
	item.Filename = "article"
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
	t.Logf("public article images=%d first=%dx%d %s", len(got.Items), im.Width, im.Height, format)
}
