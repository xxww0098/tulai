package download

import (
	"context"
	"crypto/tls"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func vipTestDoc(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func vipTestGetter() Getter {
	return GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("unexpected request")
	})
}

func TestVIPRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, brand, goods string }{
		{"https://detail.vip.com/detail-1710613581-6920995882726037325.html?from=shop", "1710613581", "6920995882726037325"},
		{"https://m.vip.com/product-3495916-2165850533.html", "3495916", "2165850533"},
		{"http://detail.vip.com:80/detail-12-34.html", "12", "34"},
	} {
		page, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := vipProductOf(page); got.brand != tc.brand || got.goods != tc.goods || !isVIPProduct(page) {
			t.Errorf("product %q = %+v", tc.raw, got)
		}
	}
	for _, raw := range []string{
		"https://user@detail.vip.com/detail-12-34.html", "https://detail.vip.com:444/detail-12-34.html",
		"https://detail.vip.com.evil.example/detail-12-34.html", "https://127.0.0.1/detail-12-34.html",
		"https://detail.vip.com/detail-00-34.html", "https://detail.vip.com/detail-12-x.html",
		"https://m.vip.com/product-12-34.html/extra", "https://m.vip.com/product-%31%32-34.html",
		"https://m.vip.com/v3/index.html#/vipshop/productDetail/pages/productDetail/productDetail?brandId=12&goodsId=34",
	} {
		page, _ := url.Parse(raw)
		if isVIPProduct(page) {
			t.Errorf("accepted product %q", raw)
		}
	}
	page, _ := url.Parse("https://detail.vip.com/detail-12-34.html?secret=not-forwarded")
	for _, tc := range []struct {
		raw, expected string
		original      bool
	}{
		{"//a.vpimg2.com/upload/name_750x750_75.jpg", "https://a.vpimg2.com/upload/name.jpg", true},
		{"http://a.vpimg1.com/upload/under_score/name_750x750_75.jpg", "https://a.vpimg1.com/upload/under_score/name.jpg", true},
		{"https://a.vpimg3.com/upload/my_name.jpg", "https://a.vpimg3.com/upload/my_name.jpg", true},
		{"//a.appsimg.com/upload/merchandise/a_420_531.jpg", "https://a.appsimg.com/upload/merchandise/a.jpg", true},
		{"//h2.appsimg.com/a.appsimg.com/upload/merchandise/a_420_531.jpg", "https://h2.appsimg.com/a.appsimg.com/upload/merchandise/a.jpg", true},
		{"https://a.vpimg4.com/upload/name_750x750_75.jpg?sig=1", "https://a.vpimg4.com/upload/name_750x750_75.jpg?sig=1", true},
		{"https://a.vpimg4.com/upload/name_750x750_75.jpg", "https://a.vpimg4.com/upload/name_750x750_75.jpg", false},
		{"https://a.vpimg4.com:444/a.jpg", "", true},
		{"https://user@a.vpimg4.com/a.jpg", "", true},
		{"https://a.vpimg4.com.evil.example/a.jpg", "", true},
		{"https://127.0.0.1/a.jpg", "", true},
		{"https://a.vpimg4.com/a/%2fsecret.jpg", "", true},
		{"https://a.vpimg4.com/a/../secret.jpg", "", true},
		{"https://h2.appsimg.com/evil.invalid/upload/merchandise/a.jpg", "", true},
		{"https://h2.appsimg.com/a.appsimg.com/upload/wxadmin/a.jpg", "", true},
		{"https://a.appsimg.com/upload/other/a.jpg", "", true},
	} {
		got, ok := vipImageURL(page, tc.raw, tc.original)
		if got != tc.expected || ok != (tc.expected != "") {
			t.Errorf("image %q = %q %v", tc.raw, got, ok)
		}
	}
}

func TestVIPCollectOriginalSKUAndDescription(t *testing.T) {
	page, _ := url.Parse("https://detail.vip.com/detail-12-34.html?tracking=private")
	doc := vipTestDoc(t, `<title>fallback</title><h1 class="pib-title-detail" title="Product name"></h1>
<div class="pic-sliderwrap"><div class="pic-slider-items J-picSlider-items"><img data-original="//a.vpimg2.com/main_750x750_75.jpg" src="//shop.vipstatic.com/img/share/blank.png"></div><img data-original="//a.vpimg2.com/main_750x750_75.jpg"></div>
<div class="color-list"><img alt="blue" data-original="//a.vpimg3.com/sku_750x750_75.jpg"></div>
<div class="dc-img-detail"><img data-original="//a.vpimg4.com/desc1.jpg"><img src="//a.vpimg4.com/desc2.png"></div>
<img data-original="//a.vpimg2.com/unrelated.jpg"><video><source src="//a.vpimg2.com/movie.mp4"></video>`)
	got, err := vipCollect(context.Background(), vipTestGetter(), page, doc)
	if err != nil || got.Title != "Product name" || len(got.Items) != 4 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	want := []struct{ source, kind string }{
		{"https://a.vpimg2.com/main.jpg", "main"},
		{"https://a.vpimg3.com/sku.jpg", "sku"},
		{"https://a.vpimg4.com/desc1.jpg", "detail"},
		{"https://a.vpimg4.com/desc2.png", "detail"},
	}
	for i, expected := range want {
		item := got.Items[i]
		if item.URL != expected.source || item.Metadata["image_kind"] != expected.kind || item.Metadata["vip_goods_id"] != "34" || item.Headers["Referer"] != "https://detail.vip.com/" || !item.RequireImage {
			t.Errorf("item %d = %+v", i, item)
		}
	}
	if got.Items[1].Metadata["title"] != "blue" {
		t.Errorf("SKU title = %q", got.Items[1].Metadata["title"])
	}
}

func TestVIPPartialAndCancellation(t *testing.T) {
	page, _ := url.Parse("https://detail.vip.com/detail-12-34.html")
	doc := vipTestDoc(t, `<div class="pic-sliderwrap"><img data-original="//a.vpimg2.com/signed_750x750_75.jpg?token=1"><img data-original="https://evil.invalid/injected.jpg"></div>`)
	got, err := vipCollect(context.Background(), vipTestGetter(), page, doc)
	if err == nil || len(got.Items) != 1 || !strings.Contains(err.Error(), "详情") || !strings.Contains(err.Error(), "不受支持") || !strings.Contains(err.Error(), "未改写") {
		t.Fatalf("partial=%+v err=%v", got, err)
	}
	template := vipTestDoc(t, `<title>唯品会</title><script type="text/html"><div class="pic-sliderwrap"><img data-original="{{item.s_img}}"></div></script>`)
	got, err = vipCollect(context.Background(), vipTestGetter(), page, template)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "尚未加载") {
		t.Fatalf("template=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := vipCollect(ctx, vipTestGetter(), page, doc); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled err=%v", err)
	}
}

func TestVIPDiscoverMobileConversionAndRedirect(t *testing.T) {
	page, _ := url.Parse("https://m.vip.com/product-12-34.html?tracking=1")
	calls := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		if raw != "https://detail.vip.com/detail-12-34.html" {
			t.Fatalf("unexpected target %q", raw)
		}
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(`<div class="pic-sliderwrap"><img data-original="//a.vpimg2.com/main.jpg"></div><div class="dc-img-detail"><img src="//a.vpimg3.com/desc.jpg"></div>`))}, nil
	})
	got, err := discoverVIPProduct(context.Background(), getter, page)
	if err != nil || len(got.Items) != 2 || calls != 1 {
		t.Fatalf("mobile=%+v calls=%d err=%v", got, calls, err)
	}
	for _, target := range []string{"https://detail.vip.com/detail-12-35.html", "https://passport.vip.com/login", "http://detail.vip.com/detail-12-34.html"} {
		getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
			return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: target, Body: io.NopCloser(strings.NewReader(`<div class="pic-sliderwrap"><img data-original="//a.vpimg2.com/wrong.jpg"></div>`))}, nil
		})
		got, err := discoverVIPProduct(context.Background(), getter, page)
		if err == nil || len(got.Items) != 0 {
			t.Errorf("redirect %q product=%+v err=%v", target, got, err)
		}
	}
}

func TestVIPRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	png := filterPNG(t, 1, 1)
	var imageRequests, badRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "a.vpimg2.com" {
			imageRequests.Add(1)
			if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "https://detail.vip.com/" {
				badRequests.Add(1)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		if r.Host != "detail.vip.com" {
			badRequests.Add(1)
			http.Error(w, "unexpected host", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/favicon.ico" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/detail-12-34.html":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/", Secure: true})
			_, _ = io.WriteString(w, `<title>first</title><div class="pic-sliderwrap"><img data-original="//a.vpimg2.com/first.png"></div><div class="dc-img-detail"><img data-original="//a.vpimg2.com/description.png"></div>`)
		case "/detail-12-35.html":
			_, _ = io.WriteString(w, `<title>second</title><div class="pic-sliderwrap"><img data-original="//a.vpimg2.com/second.png"></div><div class="dc-img-detail"><img data-original="//a.vpimg2.com/desc2.png"></div>`)
		case "/login":
			_, _ = io.WriteString(w, `<title>Login</title><img src="//a.vpimg2.com/login.png">`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://detail.vip.com/detail-12-34.html")
	getter := NewHTTPGetter("")
	transport := getter.client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec -- synthetic local TLS server
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if addr != "detail.vip.com:443" && addr != "a.vpimg2.com:443" {
			badRequests.Add(1)
			return nil, errors.New("blocked test destination")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	getter.client.Transport = transport
	session := &RenderedSession{tab: tab, closeTab: closeTab, closeAllocator: closeAllocator, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := camoufox.Run(tab); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(page.String())); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != "https://a.vpimg2.com/first.png" {
		t.Fatalf("capture=%+v err=%v", got, err)
	}
	if err := New(Options{Getter: session}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("download image: %v", err)
	}
	if imageRequests.Load() != 1 || badRequests.Load() != 0 {
		t.Fatalf("image requests=%d bad=%d", imageRequests.Load(), badRequests.Load())
	}
	if err := session.run(ctx, camoufox.Navigate("https://detail.vip.com/detail-12-35.html")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[0].Metadata["vip_goods_id"] != "35" {
		t.Fatalf("current product=%+v err=%v", got, err)
	}
	if err := session.run(ctx, camoufox.Navigate("https://detail.vip.com/login")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || len(got.Items) != 0 || badRequests.Load() != 0 {
		t.Fatalf("login=%+v bad=%d err=%v", got, badRequests.Load(), err)
	}
}
