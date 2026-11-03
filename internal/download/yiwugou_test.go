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
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/html"
)

func yiwugouTestDoc(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func yiwugouTestGetter() Getter {
	return GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("unexpected request")
	})
}

func yiwugouTestPage(id string) string {
	return `<title>fallback</title><script>window.__INITIAL_STATE__={"c":{"productId":` + id + `,"detail":{"id":` + id + `,"productDetailVO":{"id":` + id + `,"title":"Product","picture":"http://ywgimg.yiwugo.com/product/store/normal/` + id + `/cover.jpg?x-oss-process=image/resize,m_lfit,h_750,w_750","detaill":"<img src='//cbu01.alicdn.com/img/ibank/detail.jpg?__r__=123'>"},"sdiProductsPicList":[{"goodsid":` + id + `,"picture":"//cbu01.alicdn.com/img/ibank/main.jpg"}],"productPropertySkuBoList":[{"productId":` + id + `,"picture":"//cbu01.alicdn.com/img/ibank/main.jpg","propertyValues":"Blue"}],"productPropertyDimensionTwoList":[{"productId":` + id + `,"picture":"//img1.yiwugo.com/i004/2024/sku.jpg","specificationName":"Red"}]}}};</script>`
}

func TestYiwugouRoutesAndImages(t *testing.T) {
	for _, raw := range []string{
		"https://www.yiwugo.com/product/detail/123.html", "https://yiwugo.com/product/detail/123.html?from=shop",
		"https://www.yiwugou.com/product/detail/123.html", "https://wap.yiwugo.com/product/detail/123.html",
	} {
		page, _ := url.Parse(raw)
		if got := yiwugouProductID(page); got != "123" || !isYiwugouProduct(page) {
			t.Errorf("product %q = %q", raw, got)
		}
	}
	for _, raw := range []string{
		"https://user@www.yiwugo.com/product/detail/123.html", "https://www.yiwugo.com:444/product/detail/123.html",
		"https://www.yiwugo.com.evil.invalid/product/detail/123.html", "https://127.0.0.1/product/detail/123.html",
		"https://www.yiwugo.com/product/detail/0.html", "https://www.yiwugo.com/product/detail/%31%32%33.html",
		"https://www.yiwugo.com/product/detail/123.html/other", "https://www.yiwugo.com/product/detail/abc.html",
	} {
		page, _ := url.Parse(raw)
		if isYiwugouProduct(page) {
			t.Errorf("unsafe product %q", raw)
		}
	}
	page, _ := url.Parse("https://www.yiwugo.com/product/detail/123.html")
	for _, tc := range []struct {
		raw, expected string
		kept          bool
	}{
		{"http://ywgimg.yiwugo.com/product/store/normal/123/a.jpg?x-oss-process=image/resize,m_lfit,h_750,w_750", "https://ywgimg.yiwugo.com/product/store/normal/123/a.jpg", false},
		{"//cbu01.alicdn.com/img/ibank/a.jpg?__r__=123", "https://cbu01.alicdn.com/img/ibank/a.jpg", false},
		{"http://img1.yiwugo.com/i004/a.jpg@1o", "https://img1.yiwugo.com/i004/a.jpg@1o", false},
		{"https://cbu01.alicdn.com/img/ibank/a.jpg?token=secret&x-oss-process=image/resize,w_50", "https://cbu01.alicdn.com/img/ibank/a.jpg?token=secret&x-oss-process=image/resize,w_50", true},
		{"https://cbu01.alicdn.com:444/img/ibank/a.jpg", "", false},
		{"https://user@cbu01.alicdn.com/img/ibank/a.jpg", "", false},
		{"https://cbu01.alicdn.com.evil.invalid/img/ibank/a.jpg", "", false},
		{"https://img1.yiwugo.com/other/a.jpg", "", false},
		{"https://cbu01.alicdn.com/other/a.jpg", "", false},
		{"https://ywgimg.yiwugo.com/other/a.jpg", "", false},
		{"https://127.0.0.1/a.jpg", "", false},
		{"https://cbu01.alicdn.com/img/ibank/%2fsecret.jpg", "", false},
		{"https://cbu01.alicdn.com/img/ibank/../secret.jpg", "", false},
	} {
		got, ok, kept := yiwugouImageURL(page, tc.raw)
		if got != tc.expected || ok != (tc.expected != "") || kept != tc.kept {
			t.Errorf("image %q = %q, %v, %v", tc.raw, got, ok, kept)
		}
	}
}

func TestYiwugouProductMainSKUAndDetail(t *testing.T) {
	page, _ := url.Parse("https://www.yiwugo.com/product/detail/123.html?private=1")
	source := yiwugouTestPage("123") + `<div class="product-detail-page"><div class="picture-viewer"><div class="swiper-wrapper"><img data-src="//cbu01.alicdn.com/img/ibank/main.jpg" src="//static.yiwugo.com/yiwugou/ywgimages/yiwugo_default.jpg"></div></div><div class="sales product-item"><img src="//img1.yiwugo.com/i004/2024/extra.jpg"></div><div class="tab-detail"><img src="//cbu01.alicdn.com/img/ibank/detail.jpg?__r__=456"></div></div><div class="swiper-wrapper"><img src="//cbu01.alicdn.com/img/ibank/unrelated.jpg"></div>`
	got, err := yiwugouCollect(context.Background(), yiwugouTestGetter(), page, yiwugouTestDoc(t, source))
	if err != nil || got.Title != "Product" || len(got.Items) != 5 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	var shared bool
	for _, item := range got.Items {
		if item.Metadata["yiwugou_product_id"] != "123" || item.Headers["Referer"] != "https://www.yiwugo.com/" || !item.RequireImage {
			t.Errorf("item=%+v", item)
		}
		if strings.HasSuffix(item.URL, "/main.jpg") {
			shared = item.Metadata["yiwugou_image_kinds"] == "main,sku" && item.Metadata["yiwugou_sku_title"] == "Blue"
		}
		if strings.Contains(item.URL, "unrelated") || strings.Contains(item.URL, "yiwugo_default") {
			t.Errorf("unrelated image=%s", item.URL)
		}
	}
	if !shared {
		t.Error("shared main/SKU image lost its SKU role")
	}
}

func TestYiwugouPartialAndIdentity(t *testing.T) {
	page, _ := url.Parse("https://www.yiwugo.com/product/detail/123.html")
	for _, source := range []string{
		strings.Replace(yiwugouTestPage("123"), `"goodsid":123`, `"goodsid":999`, 1),
		strings.Replace(yiwugouTestPage("123"), `"picture":"//cbu01.alicdn.com/img/ibank/main.jpg"`, `"picture":123`, 1),
		strings.Replace(yiwugouTestPage("123"), `"detaill":"<img src='//cbu01.alicdn.com/img/ibank/detail.jpg?__r__=123'>"`, `"detaill":{}`, 1),
	} {
		got, err := yiwugouCollect(context.Background(), yiwugouTestGetter(), page, yiwugouTestDoc(t, source))
		if err == nil || len(got.Items) == 0 || !strings.Contains(err.Error(), "不完整") {
			t.Errorf("partial=%+v err=%v", got, err)
		}
	}
	for _, source := range []string{
		strings.Replace(yiwugouTestPage("123"), `"productId":123`, `"productId":999`, 1),
		strings.Replace(yiwugouTestPage("123"), `"id":123`, `"id":999`, 1),
		`<div class="product-detail-page"><div class="tab-detail"><img src="//cbu01.alicdn.com/img/ibank/a.jpg"></div></div>`,
	} {
		got, err := yiwugouCollect(context.Background(), yiwugouTestGetter(), page, yiwugouTestDoc(t, source))
		if err == nil || len(got.Items) != 0 {
			t.Errorf("identity=%+v err=%v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := yiwugouCollect(ctx, yiwugouTestGetter(), page, yiwugouTestDoc(t, yiwugouTestPage("123"))); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled=%v", err)
	}
}

func TestYiwugouDiscoveryRedirect(t *testing.T) {
	page, _ := url.Parse("https://www.yiwugou.com/product/detail/123.html")
	for _, tc := range []struct {
		final string
		ok    bool
	}{
		{"https://www.yiwugo.com/product/detail/123.html", true},
		{"https://www.yiwugo.com/product/detail/124.html", false},
		{"https://evil.invalid/product/detail/123.html", false},
		{"http://www.yiwugo.com/product/detail/123.html", false},
	} {
		getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
			return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: tc.final, Body: io.NopCloser(strings.NewReader(yiwugouTestPage("123")))}, nil
		})
		got, err := discoverYiwugouProduct(context.Background(), getter, page)
		if tc.ok && (err != nil || len(got.Items) == 0) {
			t.Errorf("valid redirect=%+v err=%v", got, err)
		}
		if !tc.ok && (err == nil || len(got.Items) != 0) {
			t.Errorf("invalid redirect=%+v err=%v", got, err)
		}
	}
}

func TestYiwugouImageLimit(t *testing.T) {
	page, _ := url.Parse("https://www.yiwugo.com/product/detail/123.html")
	var photos strings.Builder
	photos.WriteByte('[')
	for i := 0; i <= maxYiwugouImages; i++ {
		if i != 0 {
			photos.WriteByte(',')
		}
		photos.WriteString(`{"goodsid":123,"picture":"//cbu01.alicdn.com/img/ibank/` + strconv.Itoa(i) + `.jpg"}`)
	}
	photos.WriteByte(']')
	original := `[{"goodsid":123,"picture":"//cbu01.alicdn.com/img/ibank/main.jpg"}]`
	source := strings.Replace(yiwugouTestPage("123"), original, photos.String(), 1)
	got, err := yiwugouCollect(context.Background(), yiwugouTestGetter(), page, yiwugouTestDoc(t, source))
	if err == nil || len(got.Items) != maxYiwugouImages || !strings.Contains(err.Error(), "10000") {
		t.Fatalf("image limit=%d err=%v", len(got.Items), err)
	}
}

func TestYiwugouRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	png := filterPNG(t, 1, 1)
	var images, badRequests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "ywgimg.yiwugo.com" || r.Host == "cbu01.alicdn.com" || r.Host == "img1.yiwugo.com" {
			images.Add(1)
			if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "https://www.yiwugo.com/" {
				badRequests.Add(1)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		if r.Host != "www.yiwugo.com" {
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
		case "/product/detail/123.html", "/product/detail/124.html":
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/", Secure: true})
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/product/detail/"), ".html")
			body := strings.Replace(yiwugouTestPage(id), "/cover.jpg?x-oss-process=image/resize,m_lfit,h_750,w_750", "/cover.png", 1)
			_, _ = io.WriteString(w, body)
		case "/login":
			_, _ = io.WriteString(w, `<img data-src="//img1.yiwugo.com/i004/2024/login.png">`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tab, closeTab, closeAllocator := fixtureBrowser(t, server)
	page, _ := url.Parse("https://www.yiwugo.com/product/detail/123.html")
	getter := NewHTTPGetter("")
	transport := getter.client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec -- synthetic local TLS server
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		switch addr {
		case "www.yiwugo.com:443", "ywgimg.yiwugo.com:443", "cbu01.alicdn.com:443", "img1.yiwugo.com:443":
		default:
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
	if err != nil || len(got.Items) != 4 || got.Items[0].Metadata["yiwugou_product_id"] != "123" {
		t.Fatalf("first product=%+v err=%v", got, err)
	}
	if err := New(Options{Getter: session}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil {
		t.Fatalf("download image: %v", err)
	}
	if images.Load() != 1 || badRequests.Load() != 0 {
		t.Fatalf("images=%d bad=%d", images.Load(), badRequests.Load())
	}
	if err := session.run(ctx, camoufox.Navigate("https://www.yiwugo.com/product/detail/124.html")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err != nil || len(got.Items) != 4 || got.Items[0].Metadata["yiwugou_product_id"] != "124" {
		t.Fatalf("current product=%+v err=%v", got, err)
	}
	if err := session.run(ctx, camoufox.Navigate("https://www.yiwugo.com/login")); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || len(got.Items) != 0 || badRequests.Load() != 0 {
		t.Fatalf("login=%+v bad=%d err=%v", got, badRequests.Load(), err)
	}
}
