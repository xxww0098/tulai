package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"golang.org/x/net/html"
)

func TestAlibabaProductURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.alibaba.com/product-detail/Water-Bottle_1601199193769.html?spm=private": "1601199193769",
		"https://m.alibaba.com/product-detail/1601199193769.html":                            "1601199193769",
		"https://www.alibaba.com/product-detail/_1601199193769.html":                         "1601199193769",
		"https://m.alibaba.com/product/1600050690926/detail.html":                            "1600050690926",
		"https://www.alibaba.com/product/1600050690926/detail.html":                          "",
		"https://user@www.alibaba.com/product-detail/x_1601199193769.html":                   "",
		"https://www.alibaba.com:444/product-detail/x_1601199193769.html":                    "",
		"https://www.alibaba.com.evil.test/product-detail/x_1601199193769.html":              "",
		"https://www.alibaba.com/product-detail/x_00000.html":                                "",
		"https://www.alibaba.com/product-detail/x_abc.html":                                  "",
		"https://www.alibaba.com/product-detail/x%2Fy_1601199193769.html":                    "",
		"https://www.alibaba.com/store/product-detail/x_1601199193769.html":                  "",
	} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := alibabaProductID(u); got != want {
			t.Errorf("alibabaProductID(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestAlibabaCollectProductImages(t *testing.T) {
	page, _ := url.Parse("https://www.alibaba.com/product-detail/test_1601199193769.html?secret=private")
	const source = `<title>瓶子</title>
<div class="id-overflow-hidden id-h-full main"><img src="//s.alicdn.com/kf/main.jpg_640x640.jpg?secret=resize"></div>
<img src="//s.alicdn.com/kf/recommendation.jpg">
<div id="J-rich-text-description"><img data-src="//sc04.alicdn.com/kf/detail.jpg"></div>
<script>window.test={"globalData":{"product":{"sku":{"skuAttrs":[{"values":[{"name":"blue","originImage":"//s.alicdn.com/kf/blue.jpg"},{"name":"red","originImage":"//s.alicdn.com/kf/red.jpg"}]}]}}}};</script>`
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request while parsing the product")
		return nil, nil
	})
	result, err := alibabaCollect(context.Background(), getter, page, doc)
	want := []string{"https://s.alicdn.com/kf/main.jpg", "https://sc04.alicdn.com/kf/detail.jpg", "https://s.alicdn.com/kf/blue.jpg", "https://s.alicdn.com/kf/red.jpg"}
	if err == nil || !strings.Contains(err.Error(), "详情接口数据") || result.Title != "瓶子" || len(result.Items) != len(want) {
		t.Fatalf("product = %+v, err=%v", result, err)
	}
	for i, item := range result.Items {
		if item.URL != want[i] || item.Headers["Referer"] != "https://www.alibaba.com/" {
			t.Errorf("image %d = %+v; want %s and origin-only Referer", i, item, want[i])
		}
	}
}

func TestAlibabaInlineDescriptionAndDeduplication(t *testing.T) {
	page, _ := url.Parse("https://www.alibaba.com/product-detail/test_1601199193769.html")
	source := `<div class="id-overflow-hidden id-h-full"><img src="https://s.alicdn.com/a.jpg_600x600.jpg"><img src="https://s.alicdn.com/a.jpg"></div>
<script>var product={"globalData":{"sku":null},"productHtmlDescription":"<img src=\"//s.alicdn.com/detail.jpg\">"};
var unrelated='"productHtmlDescription":"<img src=\\"//s.alicdn.com/not-real.jpg\\">"';</script>`
	doc, _ := html.Parse(strings.NewReader(source))
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("unexpected")
	})
	result, err := alibabaCollect(context.Background(), getter, page, doc)
	if err != nil || len(result.Items) != 2 || result.Items[1].URL != "https://s.alicdn.com/detail.jpg" {
		t.Fatalf("inline description = %+v, err=%v", result.Items, err)
	}
}

func TestAlibabaPartialUnsafeAndMalformed(t *testing.T) {
	page, _ := url.Parse("https://www.alibaba.com/product-detail/test_1601199193769.html")
	const source = `<div class="id-overflow-hidden id-h-full">
<img src="https://s.alicdn.com/main.jpg"><img src="http://127.0.0.1/private">
<img src="https://user:pass@s.alicdn.com/private.jpg"><img src="https://s.alicdn.com:444/private.jpg">
<img src="https://s.alicdn.com.evil.test/private.jpg"><img src="https://s.alicdn.com/a%2fb.jpg"></div>
<script>window.d={"globalData":{"sku":{"skuAttrs":[{"values":[{"originImage":42},{"originImage":"//s.alicdn.com/valid.jpg"}]}]}}};
location.href="//bixi.alicdn.com/punish/_____tmd_____";</script>`
	doc, _ := html.Parse(strings.NewReader(source))
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("unexpected")
	})
	result, err := alibabaCollect(context.Background(), getter, page, doc)
	if err == nil || len(result.Items) != 2 || result.Items[0].URL != "https://s.alicdn.com/main.jpg" || result.Items[1].URL != "https://s.alicdn.com/valid.jpg" {
		t.Fatalf("partial images = %+v, err=%v", result.Items, err)
	}
	for _, message := range []string{"安全验证", "详情图", "地址无效", "无法解析"} {
		if !strings.Contains(err.Error(), message) {
			t.Errorf("partial error missing %q: %v", message, err)
		}
	}
}

func TestAlibabaImageLimitKeepsPartialResult(t *testing.T) {
	page, _ := url.Parse("https://www.alibaba.com/product-detail/test_1601199193769.html")
	var source strings.Builder
	source.WriteString(`<div class="id-overflow-hidden id-h-full">`)
	for i := 0; i < 10001; i++ {
		fmt.Fprintf(&source, `<img src="https://s.alicdn.com/%d.jpg">`, i)
	}
	source.WriteString(`</div><div id="J-rich-text-description"><img src="//s.alicdn.com/detail.jpg"></div><script>var d={"globalData":{"sku":null}};</script>`)
	doc, _ := html.Parse(strings.NewReader(source.String()))
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("unexpected")
	})
	result, err := alibabaCollect(context.Background(), getter, page, doc)
	if len(result.Items) != 10000 || err == nil || !strings.Contains(err.Error(), "采集上限") {
		t.Fatalf("image limit = %d images, err=%v", len(result.Items), err)
	}
}

func TestAlibabaDiscoveryIdentityAndCancellation(t *testing.T) {
	root, _ := url.Parse("https://www.alibaba.com/product-detail/test_1601199193769.html")
	const source = `<div class="id-overflow-hidden id-h-full"><img src="//s.alicdn.com/main.jpg"></div>`
	for _, tc := range []struct {
		name  string
		final string
		want  int
	}{
		{"same product", "https://www.alibaba.com/product-detail/new_1601199193769.html", 1},
		{"other product", "https://www.alibaba.com/product-detail/new_1601199193770.html", 0},
		{"login redirect", "https://login.alibaba.com/", 0},
		{"downgrade", "http://www.alibaba.com/product-detail/new_1601199193769.html", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				return &Response{StatusCode: 200, FinalURL: tc.final, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(source))}, nil
			})
			result, err := discoverAlibabaProduct(context.Background(), getter, root)
			if err == nil || len(result.Items) != tc.want {
				t.Fatalf("discovery = %+v, err=%v", result.Items, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc, _ := html.Parse(strings.NewReader(source))
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("unexpected")
	})
	if _, err := alibabaCollect(ctx, getter, root, doc); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v", err)
	}
}

func TestAlibabaMobileRedirectPreservesID(t *testing.T) {
	root, _ := url.Parse("https://m.alibaba.com/product/1600050690926/detail.html")
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{
			StatusCode: 200, FinalURL: "https://www.alibaba.com/product-detail/detail_1600050690926.html",
			Header: http.Header{"Content-Type": {"text/html"}},
			Body:   io.NopCloser(strings.NewReader(`<div class="id-overflow-hidden id-h-full"><img src="//s.alicdn.com/main.jpg"></div>`)),
		}, nil
	})
	result, err := discoverAlibabaProduct(context.Background(), getter, root)
	if err == nil || len(result.Items) != 1 || result.Items[0].URL != "https://s.alicdn.com/main.jpg" {
		t.Fatalf("mobile redirect = %+v, err=%v", result.Items, err)
	}
}

func TestAlibabaLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_ALIBABA_LIVE") != "1" {
		t.Skip("set TULAI_ALIBABA_LIVE=1 to probe an anonymous public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := url.Parse("https://www.alibaba.com/product-detail/Custom-Logo-750ml-Aluminium-Sports-Water_1601199193769.html")
	result, err := discoverAlibabaProduct(ctx, NewHTTPGetter(""), page)
	if len(result.Items) == 0 && err == nil {
		t.Fatal("public page returned no images without reporting incomplete results")
	}
	t.Logf("public product: %d images, err=%v", len(result.Items), err)
}

func TestAlibabaLiveOriginalDownload(t *testing.T) {
	if os.Getenv("TULAI_ALIBABA_LIVE") != "1" {
		t.Skip("set TULAI_ALIBABA_LIVE=1 to download a public Alibaba CDN image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := url.Parse("https://www.alibaba.com/product-detail/Source-Factory-Support-Custom-Self-Inflatable_1600599938366.html")
	// This thumbnail and product ID are published together on Alibaba's public Ready to Ship page.
	doc, _ := html.Parse(strings.NewReader(`<div class="id-overflow-hidden id-h-full"><img src="https://s.alicdn.com/@sc04/kf/Hfcee233cda8543f4983968015676d098H.jpg_220x220.jpg"></div>`))
	getter := NewHTTPGetter("")
	result, err := alibabaCollect(ctx, getter, page, doc)
	if len(result.Items) != 1 || err == nil {
		t.Fatalf("synthetic page result = %+v, err=%v", result.Items, err)
	}
	item := result.Items[0]
	item.Filename, item.RequireImage = "original.jpg", true
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(dir, "original.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	config, format, err := image.DecodeConfig(file)
	if err != nil || format != "jpeg" || config.Width <= 220 || config.Height <= 220 {
		t.Fatalf("downloaded image = %+v %q, err=%v", config, format, err)
	}
	t.Logf("public original: %dx%d %s", config.Width, config.Height, format)
}

func TestAlibabaCaptureProduct(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html><title>placeholder</title></html>")
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const target = "https://www.alibaba.com/product-detail/test_1601199193769.html"
	const document = `<html><head><title>当前商品</title></head><body><div class="id-overflow-hidden id-h-full"><img src="https://s.alicdn.com/main.jpg_60x60.jpg"></div><div id="J-rich-text-description"><img src="https://s.alicdn.com/detail.jpg"></div><img src="https://s.alicdn.com/recommendation.jpg"><script>var x={"globalData":{"sku":{"skuAttrs":[{"values":[{"originImage":"//s.alicdn.com/sku.jpg"}]}]}}};</script></body></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(document)))
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
	session.start, _ = url.Parse(target)
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "详情接口数据") || got.Title != "当前商品" || len(got.Items) != 3 || got.Items[0].URL != "https://s.alicdn.com/main.jpg" || got.Items[1].URL != "https://s.alicdn.com/detail.jpg" || got.Items[2].URL != "https://s.alicdn.com/sku.jpg" {
		t.Fatalf("captured product = %+v, err = %v", got, err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestAlibabaCaptureRedirectIdentity(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "<html><title>placeholder</title></html>")
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const original = "https://www.alibaba.com/product-detail/test_1601199193769.html"
	redirects := map[string]string{
		"https://m.alibaba.com/product/1601199193769/detail.html": original,
		original + "?case=other":                                  "https://www.alibaba.com/product-detail/other_1601199193770.html",
		original + "?case=verify":                                 "https://www.alibaba.com/_____tmd_____/verify",
	}
	const document = `<html><body><div class="id-overflow-hidden id-h-full"><img src="https://s.alicdn.com/main.jpg"></div><div id="J-rich-text-description"><img src="https://s.alicdn.com/detail.jpg"></div><script>var x={"globalData":{"sku":null}};</script></body></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument {
				body := document
				if strings.Contains(paused.Request.URL, "other_1601199193770.html") {
					body = strings.ReplaceAll(body, "main.jpg", "other-main.jpg")
				}
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
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
	for _, tc := range []struct {
		name, source, wantMain string
		wantImages             int
	}{
		{"mobile same product", "https://m.alibaba.com/product/1601199193769/detail.html", "https://s.alicdn.com/main.jpg", 2},
		{"other current product", original + "?case=other", "https://s.alicdn.com/other-main.jpg", 2},
		{"verification", original + "?case=verify", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session.start, _ = url.Parse(tc.source)
			// Present the landing page directly: guarded Firefox routes reject automatic redirects.
			if err := session.run(ctx, camoufox.Navigate(redirects[tc.source])); err != nil {
				t.Fatal(err)
			}
			got, err := session.Capture(ctx)
			if len(got.Items) != tc.wantImages {
				t.Fatalf("captured = %+v, err=%v", got.Items, err)
			}
			if tc.wantImages > 0 && got.Items[0].URL != tc.wantMain {
				t.Fatalf("captured current product = %+v", got.Items)
			}
			if tc.wantImages == 0 && (err == nil || !strings.Contains(err.Error(), "商品页面已跳转")) {
				t.Fatalf("redirect error = %v", err)
			}
			if tc.wantImages != 0 && (err == nil || !strings.Contains(err.Error(), "详情接口数据")) {
				t.Fatalf("same product partial error = %v", err)
			}
		})
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}
