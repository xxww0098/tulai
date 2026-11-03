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
	"golang.org/x/net/html"
)

const goooodPublicArticle = "https://www.gooood.cn/hangzhou-hesheng-mansion-by-9m-design.htm"
const goooodPublicArticle2 = "https://www.gooood.cn/g-cube.htm"
const goooodPublicList = "https://www.gooood.cn/type/platform"

func goooodFixture(article string) string {
	return `<html><head><title>项目标题 | 谷德</title><link rel="canonical" href="https://www.gooood.cn/` + article + `.htm"><meta property="og:url" content="https://www.gooood.cn/` + article + `.htm"></head><body>
<img src="https://oss.gooood.cn/uploads/2026/03/avatar.jpg"><article id="main-post">
<img class="entry-hero-image" src="https://oss.gooood.cn/uploads/2026/03/` + article + `-cover-472x303.jpg">
<a class="article-image-link"><img src="https://oss.gooood.cn/uploads/2026/03/` + article + `-one-960x540.jpg"></a>
<a class="article-image-link article-image-link-align-center"><img src="https://oss.gooood.cn/uploads/2026/03/` + article + `-two.png"></a>
</article><img src="https://oss.gooood.cn/uploads/2026/03/recommendation.jpg"></body></html>`
}

func goooodTestResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestGoooodPageURLs(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{goooodPublicArticle, true},
		{"https://www.gooood.cn/en/article.htm", true},
		{goooodPublicList, true},
		{goooodPublicList + "/page/2", true},
		{"https://www.gooood.cn/category/series", true},
		{"https://www.gooood.cn/search?q=architecture&page=2", true},
		{"https://www.gooood.cn/search?q=architecture&q=other", false},
		{"https://www.gooood.cn/search?q=architecture&page=0", false},
		{"https://www.gooood.cn/search", false},
		{"https://www.gooood.cn/type/platform/page/0", false},
		{"https://www.gooood.cn.evil.test/a.htm", false},
		{"https://user@www.gooood.cn/a.htm", false},
		{"https://www.gooood.cn:8443/a.htm", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if isGoooodPage(u) != tc.ok {
			t.Errorf("isGoooodPage(%q)=%v", tc.raw, isGoooodPage(u))
		}
	}
}

func TestGoooodImageBoundaryAndOriginal(t *testing.T) {
	for _, raw := range []string{
		"https://oss.gooood.cn.evil.test/uploads/2026/a.jpg",
		"https://user@oss.gooood.cn/uploads/2026/a.jpg",
		"https://oss.gooood.cn:8443/uploads/2026/a.jpg",
		"http://oss.gooood.cn/uploads/2026/a.jpg",
		"https://127.0.0.1/uploads/2026/a.jpg",
		"https://oss.gooood.cn/uploads/2026/../a.jpg",
		"https://oss.gooood.cn/private/a.jpg",
	} {
		if _, _, ok := goooodImageURL(raw); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	original, alternate, ok := goooodImageURL("//oss.gooood.cn/uploads/2026/a-960x540.jpg")
	if !ok || original != "https://oss.gooood.cn/uploads/2026/a.jpg" || alternate != "https://oss.gooood.cn/uploads/2026/a-960x540.jpg" {
		t.Fatalf("original=%q alternate=%q ok=%v", original, alternate, ok)
	}
	original, alternate, ok = goooodImageURL("https://oss.gooood.cn/uploads/2026/a-960x540.jpg?signature=synthetic")
	if !ok || original != "https://oss.gooood.cn/uploads/2026/a-960x540.jpg?signature=synthetic" || alternate != "" {
		t.Fatalf("signed original=%q alternate=%q ok=%v", original, alternate, ok)
	}
}

func TestGoooodArticleIdentity(t *testing.T) {
	root, _ := url.Parse("https://www.gooood.cn/a.htm?token=synthetic")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return goooodTestResponse(raw, http.StatusOK, goooodFixture("a")), nil
	})
	got, err := discoverGooood(context.Background(), getter, root)
	if err != nil || got.Title != "项目标题" || len(got.Items) != 3 {
		t.Fatalf("images=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://www.gooood.cn/" || item.Metadata["gooood_article"] != "/a.htm" || strings.Contains(item.URL, "avatar") || strings.Contains(item.URL, "recommendation") {
			t.Fatalf("unexpected item %+v", item)
		}
	}
	if got.Items[1].URL != "https://oss.gooood.cn/uploads/2026/03/a-one.jpg" || len(got.Items[1].AlternateURLs) != 1 || got.Items[1].AlternateURLs[0] != "https://oss.gooood.cn/uploads/2026/03/a-one-960x540.jpg" {
		t.Fatalf("unexpected original fallback %+v", got.Items[1])
	}
	wrong := strings.Replace(goooodFixture("a"), `content="https://www.gooood.cn/a.htm"`, `content="https://www.gooood.cn/b.htm"`, 1)
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return goooodTestResponse(raw, http.StatusOK, wrong), nil
	})
	if got, err := discoverGooood(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("wrong identity images=%+v err=%v", got.Items, err)
	}
}

func TestGoooodListPagination(t *testing.T) {
	const list2 = "https://www.gooood.cn/type/platform/page/2"
	const first = `<html><title>列表 | 谷德</title><a class="article-cover-link" href="/a.htm"></a><nav class="pagination"><a aria-label="下一页" href="/type/platform/page/2">→</a></nav></html>`
	const second = `<html><a class="article-cover-link" href="/b.htm"></a><nav class="pagination"><span>2/2</span></nav></html>`
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		switch raw {
		case goooodPublicList:
			return goooodTestResponse(raw, http.StatusOK, first), nil
		case list2:
			return goooodTestResponse(raw, http.StatusOK, second), nil
		case "https://www.gooood.cn/a.htm":
			return goooodTestResponse(raw, http.StatusOK, goooodFixture("a")), nil
		case "https://www.gooood.cn/b.htm":
			return goooodTestResponse(raw, http.StatusOK, goooodFixture("b")), nil
		default:
			t.Fatalf("unexpected request %q", raw)
			return nil, nil
		}
	})
	root, _ := url.Parse(goooodPublicList)
	got, err := discoverGooood(context.Background(), getter, root)
	if err != nil || got.Title != "列表" || len(got.Items) != 6 || len(calls) != 4 {
		t.Fatalf("items=%+v title=%q calls=%+v err=%v", got.Items, got.Title, calls, err)
	}
}

func TestGoooodSearchPaginationAndPartial(t *testing.T) {
	const firstURL = "https://www.gooood.cn/search?q=architecture"
	const nextURL = "https://www.gooood.cn/search?page=2&q=architecture"
	const first = `<html><a class="result-image" href="/a.htm"></a><nav class="pagination"><a aria-label="下一页" href="/search?q=architecture&amp;page=2">→</a></nav></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		switch raw {
		case firstURL:
			return goooodTestResponse(raw, http.StatusOK, first), nil
		case "https://www.gooood.cn/a.htm":
			return goooodTestResponse(raw, http.StatusOK, goooodFixture("a")), nil
		case nextURL, "https://www.gooood.cn/search?q=architecture&page=2":
			return goooodTestResponse(raw, http.StatusServiceUnavailable, ""), nil
		default:
			t.Fatalf("unexpected request %q", raw)
			return nil, nil
		}
	})
	root, _ := url.Parse(firstURL)
	got, err := discoverGooood(context.Background(), getter, root)
	if err == nil || len(got.Items) != 3 {
		t.Fatalf("partial images=%+v err=%v", got.Items, err)
	}
	unsafe := strings.Replace(first, "/search?q=architecture&amp;page=2", "https://evil.test/search?q=architecture&amp;page=2", 1)
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == firstURL {
			return goooodTestResponse(raw, http.StatusOK, unsafe), nil
		}
		if raw == "https://www.gooood.cn/a.htm" {
			return goooodTestResponse(raw, http.StatusOK, goooodFixture("a")), nil
		}
		t.Fatalf("unsafe request %q", raw)
		return nil, nil
	})
	if got, err := discoverGooood(context.Background(), getter, root); err == nil || len(got.Items) != 3 {
		t.Fatalf("unsafe next images=%+v err=%v", got.Items, err)
	}
}

func TestGoooodCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse("https://www.gooood.cn/a.htm")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverGooood(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := goooodTestResponse(raw, http.StatusOK, goooodFixture("a"))
		resp.FinalURL = "https://www.gooood.cn/b.htm"
		return resp, nil
	})
	if got, err := discoverGooood(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect images=%+v err=%v", got.Items, err)
	}
}

type goooodRoundTripper func(*http.Request) (*http.Response, error)

func (fn goooodRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestGoooodRenderedCapture(t *testing.T) {
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
	const target = "https://www.gooood.cn/a.htm?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = goooodRoundTripper(func(*http.Request) (*http.Response, error) {
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(goooodFixture("a"))))
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
	if err != nil || len(got.Items) != 3 || calls.Load() != 0 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestGoooodLiveArticleDownload(t *testing.T) {
	if os.Getenv("TULAI_GOOOOD_LIVE") != "1" {
		t.Skip("set TULAI_GOOOOD_LIVE=1 to verify public articles")
	}
	for _, tc := range []struct {
		name    string
		raw     string
		minimum int
	}{
		{"first", goooodPublicArticle, 20},
		{"second", goooodPublicArticle2, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			getter := NewHTTPGetter("")
			got, err := Discover(ctx, getter, tc.raw, 1)
			if err != nil || len(got.Items) < tc.minimum {
				t.Fatalf("public article images=%d err=%v", len(got.Items), err)
			}
			item := got.Items[1]
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
			t.Logf("public article images=%d original=%dx%d %s", len(got.Items), im.Width, im.Height, format)
		})
	}
}

// The live list test uses two actual listing pages and one real article from
// each; other article requests and page three are deliberately bounded.
func TestGoooodLiveTwoListPages(t *testing.T) {
	if os.Getenv("TULAI_GOOOOD_LIVE") != "1" {
		t.Skip("set TULAI_GOOOOD_LIVE=1 to verify public pagination")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	const secondURL = goooodPublicList + "/page/2"
	const thirdURL = goooodPublicList + "/page/3"
	getter := NewHTTPGetter("")
	pages := map[string]string{}
	chosen := map[string]bool{}
	for _, raw := range []string{goooodPublicList, secondURL} {
		resp, err := getter.Get(ctx, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
		resp.Body.Close()
		if err != nil || len(body) > maxPageBytes || resp.StatusCode != http.StatusOK || resp.FinalURL != raw {
			t.Fatalf("list page %q status=%d bytes=%d err=%v", raw, resp.StatusCode, len(body), err)
		}
		pages[raw] = string(body)
		doc, err := html.Parse(strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		page, _ := url.Parse(raw)
		var first string
		count := 0
		walkHTML(doc, func(n *html.Node) {
			if n.Type != html.ElementNode || n.Data != "a" || !goooodClass(n, "article-cover-link") {
				return
			}
			u, err := crawlURL(page, attribute(n, "href"))
			if err == nil && goooodArticleID(u) != "" && sameOrigin(page, u) {
				count++
				if first == "" {
					first = u.String()
				}
			}
		})
		if first == "" || count < 1 {
			t.Fatalf("list page %q did not expose articles", raw)
		}
		chosen[first] = true
		t.Logf("public listing %s contains %d linked articles", page.Path, count)
	}
	var visitedSecond, visitedThird bool
	bounded := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		if body, ok := pages[raw]; ok {
			visitedSecond = visitedSecond || raw == secondURL
			return goooodTestResponse(raw, http.StatusOK, body), nil
		}
		if raw == thirdURL {
			visitedThird = true
			return goooodTestResponse(raw, http.StatusServiceUnavailable, ""), nil
		}
		if chosen[raw] {
			return getter.Get(ctx, raw, prepare)
		}
		return goooodTestResponse(raw, http.StatusNotFound, ""), nil
	})
	got, err := Discover(ctx, bounded, goooodPublicList, 1)
	if err == nil || !visitedSecond || !visitedThird || len(got.Items) < 2 {
		t.Fatalf("bounded crawl images=%d page2=%v page3=%v err=%v", len(got.Items), visitedSecond, visitedThird, err)
	}
	ids := map[string]bool{}
	for _, item := range got.Items {
		ids[item.Metadata["gooood_article"]] = true
	}
	if len(ids) != 2 {
		t.Fatalf("wanted one article from each page, got=%+v err=%v", ids, err)
	}
	t.Logf("bounded two-page traversal extracted %d images from %d live articles", len(got.Items), len(ids))
}

func TestGoooodRenderedListCapture(t *testing.T) {
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
	const first = `<html><title>List</title><a class="article-cover-link" href="/a.htm"></a><nav class="pagination"><a aria-label="下一页" href="/type/platform/page/2">→</a></nav></html>`
	const second = `<html><a class="article-cover-link" href="/b.htm"></a><nav class="pagination"><span>2/2</span></nav></html>`
	var calls atomic.Int32
	session.getter.client.Transport = goooodRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		var body string
		switch r.URL.String() {
		case "https://www.gooood.cn/a.htm":
			body = goooodFixture("a")
		case "https://www.gooood.cn/b.htm":
			body = goooodFixture("b")
		case goooodPublicList + "/page/2":
			body = second
		default:
			return nil, errors.New("unexpected HTTP request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == goooodPublicList {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(first)))
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
	start, _ := url.Parse(goooodPublicList)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(goooodPublicList)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 6 || calls.Load() != 3 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestGoooodMissingNextIsPartial(t *testing.T) {
	const body = `<html><a class="article-cover-link" href="/a.htm"></a><nav class="pagination" data-total-pages="2"></nav></html>`
	root, _ := url.Parse(goooodPublicList)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == goooodPublicList {
			return goooodTestResponse(raw, http.StatusOK, body), nil
		}
		if raw == "https://www.gooood.cn/a.htm" {
			return goooodTestResponse(raw, http.StatusOK, goooodFixture("a")), nil
		}
		t.Fatalf("unexpected request %s", raw)
		return nil, nil
	})
	if got, err := discoverGooood(context.Background(), getter, root); err == nil || len(got.Items) != 3 {
		t.Fatalf("missing next images=%d err=%v", len(got.Items), err)
	}
}

func TestGoooodRenderedLiveList(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" || os.Getenv("TULAI_GOOOOD_LIVE") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 and TULAI_GOOOOD_LIVE=1 for isolated Chrome and a live article")
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
	fixture := `<html><a class="article-cover-link" href="/hangzhou-hesheng-mansion-by-9m-design.htm"></a><nav class="pagination"></nav></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == goooodPublicList {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(fixture)))
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
	start, _ := url.Parse(goooodPublicList)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(goooodPublicList)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
	if err != nil && len(got.Items) == 0 && strings.Contains(err.Error(), "网页图片不能访问其他内网地址") {
		t.Skipf("live article blocked by rendered DNS boundary: %v", err)
	}
	if err != nil || len(got.Items) < 20 {
		t.Fatalf("rendered live list images=%d err=%v", len(got.Items), err)
	}
	t.Logf("rendered live list images=%d", len(got.Items))
}
