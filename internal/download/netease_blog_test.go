package download

import (
	"context"
	"encoding/base64"
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
	"golang.org/x/net/html"
)

const neteaseBlogTestPage = "https://blog.163.com/example/blog/static/123456/"
const neteaseBlogTestImage = "http://img317.ph.126.net/token/123.jpg"

func neteaseBlogFixture(id, body string) string {
	return `<html><head><title>文章</title><script>var post={blogUrl:'blog/static/` + id + `'};</script></head><body><img src="http://img317.ph.126.net/outside.jpg"><div class="nbw-blog-start"></div><div class="bct nbw-blog">` + body + `</div><div class="nbw-blog-end"></div><img src="http://img317.ph.126.net/after.jpg"></body></html>`
}
func neteaseBlogDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	n, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func neteaseBlogResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}
}

type neteaseBlogTestTransport func(*http.Request) (*http.Response, error)

func (f neteaseBlogTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNeteaseBlogReference(t *testing.T) {
	for _, tc := range []struct{ raw, owner, id string }{
		{neteaseBlogTestPage, "example", "123456"}, {"http://blog.163.com/a1001d@126/blog/static/168020008201132102955879/", "a1001d@126", "168020008201132102955879"},
		{"http://dmy1498.blog.163.com/blog/static/11065377920121076258510/", "dmy1498", "11065377920121076258510"},
		{"http://www.blog.163.com/example/blog/static/123456/", "example", "123456"},
		{"http://blog.163.com/example/blog/static/../123456/", "", ""}, {"https://blog.163.com/example/blog/static/abc", "", ""},
		{"https://blog.163.com/example/", "", ""}, {"https://blog.163.com/example/blog/static/123456%2Fother", "", ""},
		{"https://evil.test/example/blog/static/123456", "", ""}, {"https://blog.163.com.evil.test/example/blog/static/123456", "", ""},
		{"https://user@blog.163.com/example/blog/static/123456", "", ""}, {"https://blog.163.com:8443/example/blog/static/123456", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := neteaseBlogRef(u)
		if got.owner != tc.owner || got.id != tc.id {
			t.Errorf("%q ref=%+v", tc.raw, got)
		}
	}
}
func TestNeteaseBlogBodyBoundary(t *testing.T) {
	page, _ := url.Parse(neteaseBlogTestPage + "?token=synthetic-secret")
	body := neteaseBlogFixture("123456", `<img src="`+neteaseBlogTestImage+`"><img src="https://img.bimg.126.net/photo/token/234.jpg"><img src="https://third-party.example/other.jpg">`)
	got, err := neteaseBlogArticle(context.Background(), page, neteaseBlogDoc(t, body))
	if err == nil || len(got.Items) != 2 || got.Items[0].URL != neteaseBlogTestImage || got.Title != "文章" {
		t.Fatalf("items=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Metadata["netease_blog_post_id"] != "123456" || strings.Contains(fmt.Sprint(item.Headers), "synthetic-secret") {
			t.Fatalf("unsafe item %+v", item)
		}
	}
}
func TestNeteaseBlogValidation(t *testing.T) {
	page, _ := url.Parse(neteaseBlogTestPage)
	for _, raw := range []string{"https://evil.test/a.jpg", "https://img.ph.126.net.evil.test/a.jpg", "https://user@img.ph.126.net/a.jpg", "https://img.ph.126.net:8443/a.jpg", "https://img.ph.126.net/a/../b.jpg", "http://127.0.0.1/a.jpg"} {
		if image, ok := neteaseBlogImage(raw); ok {
			t.Errorf("unsafe image %q", image)
		}
	}
	good := `<img src="` + neteaseBlogTestImage + `">`
	for _, tc := range []struct{ name, body string }{
		{"wrong post", neteaseBlogFixture("999999", good)},
		{"missing end", strings.Replace(neteaseBlogFixture("123456", good), `<div class="nbw-blog-end"></div>`, ``, 1)},
		{"duplicate start", strings.Replace(neteaseBlogFixture("123456", good), `<div class="nbw-blog-start"></div>`, `<div class="nbw-blog-start"></div><div class="nbw-blog-start"></div>`, 1)},
		{"untrusted property in a string", strings.Replace(neteaseBlogFixture("123456", good), `var post={blogUrl:'blog/static/123456'};`, `var text="blogUrl:'blog/static/123456'";`, 1)},
		{"no body", `<html><title>已关停</title></html>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := neteaseBlogArticle(context.Background(), page, neteaseBlogDoc(t, tc.body))
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}
func TestNeteaseBlogRedirectCancellationAndStatus(t *testing.T) {
	page, _ := url.Parse(neteaseBlogTestPage)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("canceled request")
		return nil, nil
	})
	if _, err := discoverNeteaseBlog(ctx, getter, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		r := neteaseBlogResponse(raw, "Not Found")
		r.StatusCode = 404
		return r, nil
	})
	if got, err := discoverNeteaseBlog(context.Background(), getter, page); err == nil || !strings.Contains(err.Error(), "404") || len(got.Items) != 0 {
		t.Fatalf("status=%+v err=%v", got.Items, err)
	}
	client := NewHTTPGetter("")
	var external atomic.Bool
	client.client.Transport = neteaseBlogTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "blog.163.com" {
			external.Store(true)
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/path"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if got, err := discoverNeteaseBlog(context.Background(), client, page); err == nil || len(got.Items) != 0 || external.Load() {
		t.Fatalf("external redirect visited=%v items=%+v err=%v", external.Load(), got.Items, err)
	}
}
func TestNeteaseBlogArchivedArticles(t *testing.T) {
	dir := os.Getenv("TULAI_NETEASE_BLOG_ARCHIVE_DIR")
	if dir == "" {
		t.Skip("set TULAI_NETEASE_BLOG_ARCHIVE_DIR for historical public pages")
	}
	for _, sample := range []struct {
		stamp, owner, id string
		count            int
	}{
		{"20160721214018", "1024_yjj", "108703841201111333035944", 8},
		{"20170423001042", "2000angel_xixi", "113019439201132584021843", 12},
	} {
		b, err := os.ReadFile(filepath.Join(dir, "netease-blog-picture-"+sample.stamp+".html"))
		if err != nil {
			t.Fatal(err)
		}
		page, _ := url.Parse("http://blog.163.com/" + sample.owner + "/blog/static/" + sample.id)
		got, err := neteaseBlogArticle(context.Background(), page, neteaseBlogDoc(t, string(b)))
		if err == nil || len(got.Items) != sample.count {
			t.Fatalf("historical %s images=%d err=%v", sample.stamp, len(got.Items), err)
		}
		t.Logf("historical article %s: %d confirmed NetEase-hosted images; other hosts remain unverified", sample.stamp, len(got.Items))
	}
}
func TestNeteaseBlogPublicLive(t *testing.T) {
	if os.Getenv("TULAI_NETEASE_BLOG_LIVE") != "1" {
		t.Skip("set TULAI_NETEASE_BLOG_LIVE=1 to check old article")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := "https://blog.163.com/00_00_chen/blog/static/44429608201202111546445"
	got, err := Discover(ctx, NewHTTPGetter(""), root, 1)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("unexpected live result=%+v err=%v", got.Items, err)
	}
	t.Logf("current old article: no images, %v", err)
}
func TestNeteaseBlogRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for an isolated browser")
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
	body := neteaseBlogFixture("123456", `<img src="`+neteaseBlogTestImage+`"><img src="https://img.bimg.126.net/photo/token/234.jpg">`)
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == neteaseBlogTestPage {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if e := camoufox.Run(session.tab, action); e != nil {
				select {
				case responses <- e:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	start, _ := url.Parse(neteaseBlogTestPage)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(neteaseBlogTestPage)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case e := <-responses:
		t.Fatalf("intercept: %v", e)
	default:
	}
	if err == nil || len(got.Items) != 2 || got.Items[0].URL != neteaseBlogTestImage {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
}
