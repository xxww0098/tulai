package download

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPGetterHonorsProxyExclusions(t *testing.T) {
	t.Setenv("NO_PROXY", ".bypass.example")
	const proxy = "socks5://127.0.0.1:1080"
	chooseProxy := proxyFunc(proxy)
	for _, tc := range []struct {
		target string
		want   string
	}{
		{"http://localhost:9000/image.png", ""},
		{"http://127.0.0.1:9000/image.png", ""},
		{"http://[::1]:9000/image.png", ""},
		{"https://image.bypass.example/image.png", ""},
		{"http://image.example/image.png", proxy},
		{"https://image.example/image.png", proxy},
	} {
		req, err := http.NewRequest(http.MethodGet, tc.target, nil)
		if err != nil {
			t.Fatal(err)
		}
		proxyURL, err := chooseProxy(req)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		if proxyURL != nil {
			got = proxyURL.String()
		}
		if got != tc.want {
			t.Errorf("proxy for %s = %q, want %q", tc.target, got, tc.want)
		}
	}
}

func TestHTTPGetterRedirectDoesNotLeakCredentialsOrRefererQuery(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("跨站重定向泄露凭据: %v", r.Header)
		}
		if got := r.Header.Get("Referer"); got != "http://example.test/" {
			t.Errorf("跨站 Referer = %q", got)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer source.Close()
	resp, err := NewHTTPGetter("").Get(context.Background(), source.URL, func(r *http.Request) {
		r.Header.Set("Cookie", "session=secret")
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Referer", "http://example.test/private?token=secret")
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}
