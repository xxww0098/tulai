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

const zolPublicAlbum = "https://detail.zol.com.cn/1309/1308403/pic.shtml"
const zolPublicViewer = "https://detail.zol.com.cn/picture_index_2038/index20378406_15561_p1308403.shtml"
const zolPhoneAlbum = "https://detail.zol.com.cn/1428/1427369/pic.shtml"
const zolAlbumFixture = `<html><head><title>Album</title></head><body>
<a class="imgwrap" href="/picture_index_2038/index20378406_15561_p1308403.shtml?tracking=synthetic"><img src="https://2a.zol-img.com.cn/product/203_280x210/406/ceM3tPKM9Wuv2.jpg" alt="主图"></a>
<a class="imgwrap" href="https://evil.test/picture_index_2038/index20378406_15561_p1308403.shtml"><img src="https://2a.zol-img.com.cn/product/203_280x210/407/not-ours.jpg"></a>
<a class="imgwrap" href="/picture_index_2038/index20378406_15561_p1308404.shtml"><img src="https://2a.zol-img.com.cn/product/203_280x210/408/other.jpg"></a>
</body></html>`
const zolViewerFixture = `<html><head><title>Viewer</title></head><body><script>
var picList = [
{"proId":1308403,"picId":20378406,"name":"官方图","picSrc":"https://2a.zol-img.com.cn/product/203_100x75/406/ceM3tPKM9Wuv2.jpg"},
{"proId":1308403,"picId":20378407,"name":"细节图","picSrc":"https://2b.zol-img.com.cn/product/203_100x75/407/detail.jpg"},
{"proId":1308403,"picId":20378407,"name":"其他类别","picSrc":"https://2b.zol-img.com.cn/product/203_100x75/407/detail.jpg"}
];
</script></body></html>`

func zolTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestZOLURLs(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		viewer  bool
	}{
		{zolPublicAlbum, "1308403", false},
		{zolPublicViewer, "1308403", true},
		{zolPhoneAlbum, "1427369", false},
		{"https://detail.zol.com.cn/1428/1427369/pic_15549_1.shtml", "", false},
		{"https://detail.zol.com.cn/notebook/index1308403.shtml", "", false},
		{"https://detail.zol.com.cn.evil.test/1309/1308403/pic.shtml", "", false},
		{"https://user@detail.zol.com.cn/1309/1308403/pic.shtml", "", false},
		{"https://detail.zol.com.cn:8443/1309/1308403/pic.shtml", "", false},
		{"https://detail.zol.com.cn/1309/1/pic.shtml", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := zolPageRef(u)
		if got.id != tc.id || got.viewer != tc.viewer || isZOLPage(u) != (tc.id != "") {
			t.Errorf("ref(%q)=%+v", tc.raw, got)
		}
	}
}

func TestZOLOriginalImageBoundary(t *testing.T) {
	valid := "https://2a.zol-img.com.cn/product/203_100x75/406/ceM3tPKM9Wuv2.jpg"
	if got, ok := zolOriginalImage(valid); !ok || got != "https://2a.zol-img.com.cn/product/203/406/ceM3tPKM9Wuv2.jpg" {
		t.Fatalf("original=%q ok=%v", got, ok)
	}
	for _, raw := range []string{
		"https://2a.zol-img.com.cn.evil.test/product/203_100x75/406/a.jpg",
		"https://user@2a.zol-img.com.cn/product/203_100x75/406/a.jpg",
		"https://2a.zol-img.com.cn:8443/product/203_100x75/406/a.jpg",
		"http://2a.zol-img.com.cn/product/203_100x75/406/a.jpg",
		"https://127.0.0.1/product/203_100x75/406/a.jpg",
		"https://2a.zol-img.com.cn/product/203_100x75/../secret.jpg",
		"https://2a.zol-img.com.cn/avatar/203_100x75/406/a.jpg",
	} {
		if _, ok := zolOriginalImage(raw); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
}

func TestZOLAlbumAndOriginalImages(t *testing.T) {
	root, _ := url.Parse(zolPublicAlbum + "?token=synthetic")
	var requests []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		requests = append(requests, raw)
		switch raw {
		case root.String():
			return zolTestResponse(raw, zolAlbumFixture), nil
		case zolPublicViewer:
			return zolTestResponse(raw, zolViewerFixture), nil
		default:
			t.Fatalf("unexpected request %q", raw)
			return nil, nil
		}
	})
	got, err := discoverZOL(context.Background(), getter, root)
	if err != nil || len(got.Items) != 2 || len(requests) != 2 {
		t.Fatalf("images=%+v requests=%v err=%v", got.Items, requests, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["zol_product_id"] != "1308403" || item.Metadata["zol_picture_id"] == "" || item.Headers["Referer"] != "https://detail.zol.com.cn/" || strings.Contains(item.URL, "_100x75") {
			t.Fatalf("unexpected image %+v", item)
		}
	}
}

func TestZOLPartialAndIdentity(t *testing.T) {
	root, _ := url.Parse(zolPublicAlbum)
	t.Run("viewer-failed", func(t *testing.T) {
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			if raw == zolPublicAlbum {
				return zolTestResponse(raw, zolAlbumFixture), nil
			}
			return nil, errors.New("unavailable")
		})
		got, err := discoverZOL(context.Background(), getter, root)
		if err == nil || len(got.Items) != 1 || got.Items[0].URL != "https://2a.zol-img.com.cn/product/203/406/ceM3tPKM9Wuv2.jpg" {
			t.Fatalf("images=%+v err=%v", got.Items, err)
		}
	})
	t.Run("mixed-products", func(t *testing.T) {
		body := strings.Replace(zolViewerFixture, `"proId":1308403`, `"proId":1308404`, 1)
		viewer, _ := url.Parse(zolPublicViewer)
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			return zolTestResponse(raw, body), nil
		})
		got, err := discoverZOL(context.Background(), getter, viewer)
		if err == nil || len(got.Items) != 1 || got.Items[0].Metadata["zol_picture_id"] != "20378407" {
			t.Fatalf("images=%+v err=%v", got.Items, err)
		}
	})
	t.Run("untrusted-js-string", func(t *testing.T) {
		viewer, _ := url.Parse(zolPublicViewer)
		body := `<script>var unrelated = 'var picList = []';</script>`
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			return zolTestResponse(raw, body), nil
		})
		if got, err := discoverZOL(context.Background(), getter, viewer); err == nil || len(got.Items) != 0 {
			t.Fatalf("images=%+v err=%v", got.Items, err)
		}
	})
}

func TestZOLCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(zolPublicAlbum)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverZOL(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := zolTestResponse(raw, zolAlbumFixture)
		resp.FinalURL = "https://detail.zol.com.cn/1309/1308404/pic.shtml"
		return resp, nil
	})
	if got, err := discoverZOL(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect images=%+v err=%v", got.Items, err)
	}
}

type zolRoundTripper func(*http.Request) (*http.Response, error)

func (fn zolRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestZOLRenderedCapture(t *testing.T) {
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
	const target = zolPublicAlbum + "?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = zolRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.String() != zolPublicViewer {
			return nil, errors.New("unexpected request or leaked query")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(zolViewerFixture)), Request: r}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(zolAlbumFixture)))
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
	if err != nil || len(got.Items) != 2 || calls.Load() != 1 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestZOLLiveProductDownload(t *testing.T) {
	if os.Getenv("TULAI_ZOL_LIVE") != "1" {
		t.Skip("set TULAI_ZOL_LIVE=1 to verify public products")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, zolPublicAlbum, 1)
	if err != nil || len(got.Items) != 16 {
		t.Fatalf("public album images=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "product"
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
	t.Logf("public album images=%d first=%dx%d %s", len(got.Items), im.Width, im.Height, format)
}

func TestZOLLivePhoneAlbum(t *testing.T) {
	if os.Getenv("TULAI_ZOL_LIVE") != "1" {
		t.Skip("set TULAI_ZOL_LIVE=1 to verify public products")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), zolPhoneAlbum, 1)
	if err != nil || len(got.Items) != 66 {
		t.Fatalf("public phone album images=%d err=%v", len(got.Items), err)
	}
	t.Logf("public phone album images=%d after deduplication", len(got.Items))
}

func TestZOLLiveLargeAlbum(t *testing.T) {
	if os.Getenv("TULAI_ZOL_LIVE") != "1" {
		t.Skip("set TULAI_ZOL_LIVE=1 to verify public products")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), "https://detail.zol.com.cn/347/346888/pic.shtml", 1)
	if err != nil || len(got.Items) != 311 {
		t.Fatalf("public large album images=%d err=%v", len(got.Items), err)
	}
	t.Logf("public large album images=%d after deduplication", len(got.Items))
}
