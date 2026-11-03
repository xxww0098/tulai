package download

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestWeiboOriginalURL(t *testing.T) {
	for _, tc := range []struct {
		name, source, image, want string
	}{
		{"normal", "https://weibo.com/post", "https://wx1.sinaimg.cn/orj360/abc.jpg?x=1", "https://wx1.sinaimg.cn/large/abc.jpg?x=1"},
		{"subdomain and exact image domain", "https://m.weibo.cn/post", "https://sinaimg.cn/mw690/a.jpg?x=a%2Fb", "https://sinaimg.cn/large/a.jpg?x=a%2Fb"},
		{"escaped path", "https://WEIBO.COM/post", "https://wx1.sinaimg.cn/mw690/a%2Fb.jpg?x=a%2Fb", "https://wx1.sinaimg.cn/large/a%2Fb.jpg?x=a%2Fb"},
		{"not weibo", "https://example.test/post", "https://wx1.sinaimg.cn/orj360/abc.jpg", "https://wx1.sinaimg.cn/orj360/abc.jpg"},
		{"fake source suffix", "https://weibo.com.evil.test/post", "https://wx1.sinaimg.cn/orj360/abc.jpg", "https://wx1.sinaimg.cn/orj360/abc.jpg"},
		{"fake source userinfo", "https://weibo.com@evil.test/post", "https://wx1.sinaimg.cn/orj360/abc.jpg", "https://wx1.sinaimg.cn/orj360/abc.jpg"},
		{"fake image suffix", "https://weibo.com/post", "https://wx1.sinaimg.cn.evil.test/orj360/abc.jpg", "https://wx1.sinaimg.cn.evil.test/orj360/abc.jpg"},
		{"fake image userinfo", "https://weibo.com/post", "https://sinaimg.cn@evil.test/orj360/abc.jpg", "https://sinaimg.cn@evil.test/orj360/abc.jpg"},
		{"single segment", "https://weibo.com/post", "https://wx1.sinaimg.cn/abc.jpg", "https://wx1.sinaimg.cn/abc.jpg"},
		{"empty second segment", "https://weibo.com/post", "https://wx1.sinaimg.cn/orj360/", "https://wx1.sinaimg.cn/orj360/"},
		{"double slash", "https://weibo.com/post", "https://wx1.sinaimg.cn/orj360//abc.jpg", "https://wx1.sinaimg.cn/orj360//abc.jpg"},
		{"empty first segment", "https://weibo.com/post", "https://wx1.sinaimg.cn//orj360/abc.jpg", "https://wx1.sinaimg.cn//orj360/abc.jpg"},
		{"invalid escape", "https://weibo.com/post", "https://wx1.sinaimg.cn/orj360/%ZZ", "https://wx1.sinaimg.cn/orj360/%ZZ"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := weiboOriginalURL(tc.source, tc.image); got != tc.want {
				t.Fatalf("weiboOriginalURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiscoverNormalizesWeiboBeforeDeduplication(t *testing.T) {
	page := `<html><img src="https://wx1.sinaimg.cn/orj360/a.jpg?x=1">
		<img src="https://wx1.sinaimg.cn/mw690/a.jpg?x=1">
		<a href="https://wx1.sinaimg.cn/large/a.jpg?x=1">original</a>
		<img src="https://wx1.sinaimg.cn/orj360/a.jpg?x=2">
		<img src="https://wx1.sinaimg.cn.evil.test/orj360/a.jpg">
		<img src="https://wx1.sinaimg.cn/single.jpg"></html>`
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(page))}, nil
	})
	got, err := Discover(context.Background(), getter, "https://weibo.com/post?token=private", 1)
	if err != nil || len(got.Items) != 4 {
		t.Fatalf("weibo result = %+v, err = %v", got.Items, err)
	}
	want := []string{
		"https://wx1.sinaimg.cn/large/a.jpg?x=1",
		"https://wx1.sinaimg.cn/large/a.jpg?x=2",
		"https://wx1.sinaimg.cn.evil.test/orj360/a.jpg",
		"https://wx1.sinaimg.cn/single.jpg",
	}
	for i, item := range got.Items {
		if item.URL != want[i] || item.Headers["Referer"] != "https://weibo.com/" {
			t.Errorf("item %d = %+v, want %q", i, item, want[i])
		}
	}
	other, err := Discover(context.Background(), getter, "https://example.test/post", 1)
	if err != nil || len(other.Items) != 6 || other.Items[0].URL != "https://wx1.sinaimg.cn/orj360/a.jpg?x=1" {
		t.Fatalf("non-weibo result = %+v, err = %v", other.Items, err)
	}
}
