package download

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNeteasePhotoRoutes(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"http://pp.163.com/", true},
		{"https://PP.163.COM:443/maolvdui666/pp/12413164.html", true},
		{"http://pp.163.com:80/square", true},
		{"http://dou1980824.pp.163.com/", true},
		{"https://Artist-One.PP.163.COM/", true},
		{"https://pp.163.com:80/square", false},
		{"https://pp.163.com:8443/square", false},
		{"https://user@pp.163.com/", false},
		{"https://pp.163.com.evil.test/", false},
		{"https://photo.163.com/", false},
		{"https://user.pp.163.com/", true},
		{"https://a.b.pp.163.com/", false},
		{"https://-user.pp.163.com/", false},
		{"https://user-.pp.163.com/", false},
		{"https://user_name.pp.163.com/", false},
		{"https://user.pp.163.com.evil.test/", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isNeteasePhotoPage(u); got != tc.want {
			t.Errorf("route %q = %v", tc.raw, got)
		}
	}
	if isNeteasePhotoPage(nil) {
		t.Fatal("nil URL accepted")
	}
}

func TestNeteasePhotoUnavailable(t *testing.T) {
	page, _ := url.Parse("http://pp.163.com/maolvdui666/pp/12413164.html")
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unavailable route must not issue requests")
		return nil, nil
	})
	got, err := discoverNeteasePhoto(context.Background(), getter, page)
	if err == nil || !strings.Contains(err.Error(), "专用采集未实现") || !got.IsPage || len(got.Items) != 0 {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverNeteasePhoto(ctx, getter, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	other, _ := url.Parse("https://photo.163.com/")
	if _, err := discoverNeteasePhoto(context.Background(), getter, other); err == nil || !strings.Contains(err.Error(), "地址无效") {
		t.Fatalf("other site err=%v", err)
	}
}
