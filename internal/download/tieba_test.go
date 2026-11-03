package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func tiebaTestResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestTiebaThreadURL(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://tieba.baidu.com/p/123", true},
		{"https://c.tieba.baidu.com/p/123/?pn=2", true},
		{"http://tieba.baidu.com:80/p/123", true},
		{"https://tieba.baidu.com/p/123/more", false},
		{"https://tieba.baidu.com/p/abc", false},
		{"https://tieba.baidu.com/f?kw=test", false},
		{"https://tieba.baidu.com.evil.test/p/123", false},
		{"https://user@tieba.baidu.com/p/123", false},
		{"https://tieba.baidu.com:8443/p/123", false},
	} {
		u, _ := url.Parse(tc.raw)
		if got := isTiebaThread(u); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.raw, got, tc.want)
		}
	}
}

func TestTiebaOriginalURL(t *testing.T) {
	const page = "https://tieba.baidu.com/p/123"
	for _, tc := range []struct{ source, raw, want string }{
		{page, "https://imgsa.baidu.com/forum/w%3D580/sign=abc/original.jpg?q=1", "https://imgsa.baidu.com/forum/pic/item/original.jpg?q=1"},
		{page, "https://tiebapic.baidu.com/forum/pic/item/image.gif", "https://tiebapic.baidu.com/forum/pic/item/image.gif"},
		{page, "https://imgsa.baidu.com/forum/a/b/photo.png", "https://imgsa.baidu.com/forum/pic/item/photo.png"},
		{page, "https://other.test/forum/a/b/photo.png", "https://other.test/forum/a/b/photo.png"},
		{page, "https://imgsa.baidu.com/forum/photo.png", "https://imgsa.baidu.com/forum/photo.png"},
		{page, "https://imgsa.baidu.com.evil.test/forum/a/b/photo.png", "https://imgsa.baidu.com.evil.test/forum/a/b/photo.png"},
		{"https://example.test/p/123", "https://imgsa.baidu.com/forum/a/b/photo.png", "https://imgsa.baidu.com/forum/a/b/photo.png"},
	} {
		if got := tiebaOriginalURL(tc.source, tc.raw); got != tc.want {
			t.Errorf("original(%q, %q) = %q, want %q", tc.source, tc.raw, got, tc.want)
		}
	}
}

func TestTiebaThreadPaginationOriginalAndPrivacy(t *testing.T) {
	var requests []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		prepare(req)
		requests = append(requests, req.URL.String())
		if req.URL.Host != "tieba.baidu.com" || req.URL.Query().Get("see_lz") != "1" || req.Header.Get("Cookie") != "" {
			t.Fatalf("unexpected request: %s, cookies=%q", raw, req.Header.Get("Cookie"))
		}
		if req.URL.Query().Get("pn") == "2" {
			return tiebaTestResponse(raw, 200, `<div class="l_post"><img class="BDE_Image" src="https://imgsa.baidu.com/forum/w%3D580/sign=abc/a.jpg?q=1"><img class="BDE_Image" src="https://imgsa.baidu.com/forum/x/y/b.png"><img class="d_content_img" src="https://imgsa.baidu.com/forum/x/y/legacy.gif"></div>`), nil
		}
		return tiebaTestResponse(raw, 200, `<title>测试帖子</title><div class="l_post">
		<img class="BDE_Image" src="https://imgsa.baidu.com/forum/w%3D580/sign=abc/a.jpg?q=1">
		<img class="BDE_Image" src="https://other.test/photo.jpg"><img class="logo" src="/logo.png"></div>
		<a href="/p/123?pn=2&amp;see_lz=1">下一页</a><a href="/p/123?pn=2&amp;see_lz=0">全部楼层</a>
		<a href="/p/456?pn=9">其他帖子</a><a href="https://evil.test/p/123?pn=9">外部</a>`), nil
	})
	root, _ := url.Parse("https://tieba.baidu.com/p/123?see_lz=1&private=secret")
	got, err := discoverTiebaThread(context.Background(), getter, root)
	if err != nil || !got.IsPage || got.Title != "测试帖子" || len(requests) != 2 || len(got.Items) != 4 {
		t.Fatalf("items=%+v requests=%v err=%v", got.Items, requests, err)
	}
	if got.Items[0].URL != "https://imgsa.baidu.com/forum/pic/item/a.jpg?q=1" || got.Items[2].URL != "https://imgsa.baidu.com/forum/pic/item/b.png" || got.Items[3].URL != "https://imgsa.baidu.com/forum/x/y/legacy.gif" {
		t.Fatalf("original images = %+v", got.Items)
	}
	for _, item := range got.Items {
		if ref := item.Headers["Referer"]; ref != "https://tieba.baidu.com/" {
			t.Fatalf("cross-origin referer leaks query: %q", ref)
		}
	}
	if !strings.Contains(requests[1], "private=secret") || !strings.Contains(requests[1], "pn=2") {
		t.Fatalf("pagination lost input parameters: %v", requests)
	}
}

func TestTiebaThreadPartialFailures(t *testing.T) {
	for _, mode := range []string{"403", "redirect", "wrong_page", "cancel", "limit", "unrecognized"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				if calls == 2 {
					switch mode {
					case "403":
						return tiebaTestResponse(raw, 403, "challenge"), nil
					case "redirect":
						return tiebaTestResponse("https://evil.test/p/123?pn=2", 200, "redirect"), nil
					case "wrong_page":
						return tiebaTestResponse("https://tieba.baidu.com/p/123?pn=1", 200, `<div class="l_post"></div>`), nil
					case "unrecognized":
						return tiebaTestResponse(raw, 200, `<title>百度安全验证</title>`), nil
					}
				}
				if calls == 1 && mode == "cancel" {
					cancel()
				}
				body := fmt.Sprintf(`<div class="l_post"><img class="BDE_Image" src="https://imgsa.baidu.com/forum/w/sign/%d.jpg"></div><a href="?pn=%d">下一页</a>`, calls, calls+1)
				return tiebaTestResponse(raw, 200, body), nil
			})
			root, _ := url.Parse("https://tieba.baidu.com/p/123")
			got, err := discoverTiebaThread(ctx, getter, root)
			if err == nil || len(got.Items) == 0 {
				t.Fatalf("expected partial result: items=%d, err=%v", len(got.Items), err)
			}
			if mode == "cancel" && (!errors.Is(err, context.Canceled) || calls != 1) {
				t.Fatalf("cancel: calls=%d, err=%v", calls, err)
			}
			if mode == "403" {
				var status *HTTPStatusError
				if !errors.As(err, &status) || status.StatusCode != 403 || !strings.Contains(err.Error(), "验证") {
					t.Fatalf("status error = %v", err)
				}
			}
			if mode == "limit" && (calls != maxCrawlPages || len(got.Items) != maxCrawlPages || !strings.Contains(err.Error(), strconv.Itoa(maxCrawlPages))) {
				t.Fatalf("limit: calls=%d, items=%d, err=%v", calls, len(got.Items), err)
			}
		})
	}
}
