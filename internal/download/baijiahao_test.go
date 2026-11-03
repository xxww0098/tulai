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

const baijiahaoPublicArticle = "https://baijiahao.baidu.com/s?id=1835359983172550408"
const baijiahaoPublicArticle2 = "https://baijiahao.baidu.com/s?id=1831554376312950536"
const baijiahaoPublicHome = "https://author.baidu.com/home?app_id=1831517811395903"
const baijiahaoFixture = `<html><head><title>Article</title></head><body>
<img src="https://pics1.baidu.com/feed/avatar.jpg">
<div data-testid="article"><img src="https://pics1.baidu.com/feed/first.jpeg@f_auto?token=synthetic"><img src="https://pics2.baidu.com/feed/second.jpeg"></div>
<script>window.jsonData = {"bsData":{"superlanding":[{"itemType":"article","itemData":{"header":"文章标题","readsrc":{"link":"http://baijiahao.baidu.com/s?id=1835359983172550408"},"sections":[{"type":"img","link":"https://pics1.baidu.com/feed/first.jpeg@f_auto?token=synthetic"},{"type":"img","link":"https://pics2.baidu.com/feed/second.jpeg"}]}},{"itemType":"author","itemData":{"thumbnail":{"link":"https://pics1.baidu.com/feed/avatar.jpg"}}}]}};</script>
<img src="https://pics1.baidu.com/feed/recommendation.jpg"></body></html>`

func baijiahaoTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestBaijiahaoURLs(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{baijiahaoPublicArticle, true},
		{baijiahaoPublicArticle + "&wfr=spider", true},
		{baijiahaoPublicHome, true},
		{"https://author.baidu.com/home?uk=abcDEF_123", true},
		{baijiahaoPublicArticle + "&id=1835359983172550409", false},
		{"https://baijiahao.baidu.com/s?id=0", false},
		{"https://baijiahao.baidu.com.evil.test/s?id=1835359983172550408", false},
		{"https://user@baijiahao.baidu.com/s?id=1835359983172550408", false},
		{"https://baijiahao.baidu.com:8443/s?id=1835359983172550408", false},
		{"https://author.baidu.com/home", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if isBaijiahaoPage(u) != tc.ok {
			t.Errorf("isBaijiahaoPage(%q)=%v", tc.raw, isBaijiahaoPage(u))
		}
	}
}

func TestBaijiahaoImageBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://pics1.baidu.com.evil.test/feed/a.jpg",
		"https://user@pics1.baidu.com/feed/a.jpg",
		"https://pics1.baidu.com:8443/feed/a.jpg",
		"http://pics1.baidu.com/feed/a.jpg",
		"https://127.0.0.1/feed/a.jpg",
		"https://pics1.baidu.com/feed/../private.jpg",
		"https://pics1.baidu.com/avatar/a.jpg",
	} {
		if _, ok := baijiahaoImageURL(raw); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	if _, ok := baijiahaoImageURL("//pics1.baidu.com/feed/a.jpg"); !ok {
		t.Fatal("valid image rejected")
	}
}

func TestBaijiahaoArticleAndIdentity(t *testing.T) {
	root, _ := url.Parse(baijiahaoPublicArticle + "&token=synthetic")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return baijiahaoTestResponse(raw, baijiahaoFixture), nil
	})
	got, err := discoverBaijiahao(context.Background(), getter, root)
	if err != nil || got.Title != "文章标题" || len(got.Items) != 2 {
		t.Fatalf("images=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["baijiahao_article_id"] != "1835359983172550408" || item.Headers["Referer"] != "https://baijiahao.baidu.com/" || strings.Contains(item.URL, "avatar") || strings.Contains(item.URL, "recommendation") {
			t.Fatalf("unexpected image %+v", item)
		}
	}
	wrong := strings.Replace(baijiahaoFixture, `id=1835359983172550408`, `id=1835359983172550409`, 1)
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return baijiahaoTestResponse(raw, wrong), nil
	})
	if got, err := discoverBaijiahao(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("wrong identity images=%+v err=%v", got.Items, err)
	}
}

func TestBaijiahaoPartialAndHome(t *testing.T) {
	root, _ := url.Parse(baijiahaoPublicArticle)
	unsafe := strings.ReplaceAll(baijiahaoFixture, "https://pics2.baidu.com/feed/second.jpeg", "https://evil.test/feed/second.jpeg")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return baijiahaoTestResponse(raw, unsafe), nil
	})
	got, err := discoverBaijiahao(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 {
		t.Fatalf("partial images=%+v err=%v", got.Items, err)
	}
	home, _ := url.Parse(baijiahaoPublicHome)
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return baijiahaoTestResponse(raw, `<html><img src="https://pics1.baidu.com/feed/avatar.jpg"></html>`), nil
	})
	if got, err := discoverBaijiahao(context.Background(), getter, home); err == nil || len(got.Items) != 0 {
		t.Fatalf("home images=%+v err=%v", got.Items, err)
	}
}

func TestBaijiahaoCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(baijiahaoPublicArticle)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverBaijiahao(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := baijiahaoTestResponse(raw, baijiahaoFixture)
		resp.FinalURL = baijiahaoPublicArticle2
		return resp, nil
	})
	if got, err := discoverBaijiahao(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect images=%+v err=%v", got.Items, err)
	}
}

type baijiahaoRoundTripper func(*http.Request) (*http.Response, error)

func (fn baijiahaoRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestBaijiahaoRenderedCapture(t *testing.T) {
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
	const target = baijiahaoPublicArticle + "&token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = baijiahaoRoundTripper(func(*http.Request) (*http.Response, error) {
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(baijiahaoFixture)))
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

func TestBaijiahaoLiveArticleDownload(t *testing.T) {
	if os.Getenv("TULAI_BAIJIAHAO_LIVE") != "1" {
		t.Skip("set TULAI_BAIJIAHAO_LIVE=1 to verify public articles")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, baijiahaoPublicArticle, 1)
	if err != nil || len(got.Items) != 8 {
		t.Fatalf("public article images=%d err=%v", len(got.Items), err)
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

func TestBaijiahaoLiveSecondArticle(t *testing.T) {
	if os.Getenv("TULAI_BAIJIAHAO_LIVE") != "1" {
		t.Skip("set TULAI_BAIJIAHAO_LIVE=1 to verify public articles")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), baijiahaoPublicArticle2, 1)
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("public second article images=%d err=%v", len(got.Items), err)
	}
	t.Logf("second public article images=%d", len(got.Items))
}
