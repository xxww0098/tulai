package download

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPublicSearchDownloadBlocksPrivateTargets(t *testing.T) {
	for _, raw := range []string{"http://127.1/image.png", "http://2130706433/image.png", "http://localhost/image.png"} {
		if _, err := publicImageURL(raw); err == nil {
			t.Fatalf("accepted local-looking search URL %s", raw)
		}
	}
	img := filterPNG(t, 1, 1)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(img)
	}))
	defer server.Close()
	getter := NewHTTPGetter("")
	dl := New(Options{Getter: getter})
	job := Job{Directory: t.TempDir()}
	item := Item{URL: server.URL + "/image.png", Filename: "search.png", RequireImage: true, PublicOnly: true}
	err := dl.Download(context.Background(), job, item, nil)
	if err == nil || requests.Load() != 0 {
		t.Fatalf("public-only err=%v, requests=%d", err, requests.Load())
	}
	item.PublicOnly = false
	item.Filename = "user-specified.png"
	if err := dl.Download(context.Background(), job, item, nil); err != nil || requests.Load() != 1 {
		t.Fatalf("direct local download err=%v, requests=%d", err, requests.Load())
	}
	var proxyRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests.Add(1)
		if r.URL.Path == "/redirect.png" {
			http.Redirect(w, r, "http://127.0.0.1/image.png", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(img)
	}))
	defer proxy.Close()
	item.PublicOnly, item.URL, item.Filename = true, "http://example.com/image.png", "proxy.png"
	proxyDL := New(Options{Getter: NewHTTPGetter(proxy.URL)})
	if err := proxyDL.Download(context.Background(), job, item, nil); err != nil || proxyRequests.Load() != 1 {
		t.Fatalf("public-only proxy err=%v, requests=%d", err, proxyRequests.Load())
	}
	item.URL, item.Filename = "http://example.com/redirect.png", "redirect.png"
	err = proxyDL.Download(context.Background(), job, item, nil)
	if err == nil || !strings.Contains(err.Error(), "公网") || proxyRequests.Load() != 2 {
		t.Fatalf("private redirect err=%v, requests=%d", err, proxyRequests.Load())
	}
}
