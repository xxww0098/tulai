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
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
)

func amazonTestResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestAmazonProductRoutes(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{"https://www.amazon.com/dp/B0B5RJHYFD", "B0B5RJHYFD"},
		{"https://www.amazon.com/slug/dp/b0b5rjhyfd/ref=abc", "B0B5RJHYFD"},
		{"https://amazon.co.uk/gp/product/B0B5RJHYFD?tag=synthetic", "B0B5RJHYFD"},
		{"https://www.amazon.co.jp/dp/B0B5RJHYFD", "B0B5RJHYFD"},
		{"https://www.amazon.com.evil.test/dp/B0B5RJHYFD", ""},
		{"https://www.m.amazon.com/dp/B0B5RJHYFD", ""},
		{"https://user@www.amazon.com/dp/B0B5RJHYFD", ""},
		{"https://www.amazon.com:8443/dp/B0B5RJHYFD", ""},
		{"https://www.amazon.com/dp/not-an-asin", ""},
		{"https://www.amazon.com/s?k=product", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := amazonProductID(u); got != tc.id {
			t.Errorf("route %q = %q want %q", tc.raw, got, tc.id)
		}
	}
}

func TestAmazonImageDomainRestrictions(t *testing.T) {
	page, _ := url.Parse("https://www.amazon.com/dp/B0B5RJHYFD")
	for _, tc := range []struct{ raw, want string }{
		{"https://m.media-amazon.com/images/I/a.jpg?sign=ok", "https://m.media-amazon.com/images/I/a.jpg?sign=ok"},
		{"http://images-na.ssl-images-amazon.com/images/I/b.jpg", "https://images-na.ssl-images-amazon.com/images/I/b.jpg"},
		{"https://m.media-amazon.com.evil.test/images/I/a.jpg", ""},
		{"https://user@m.media-amazon.com/images/I/a.jpg", ""},
		{"https://m.media-amazon.com:8443/images/I/a.jpg", ""},
		{"https://m.media-amazon.com/redirect?url=https://evil.test", ""},
		{"https://m.media-amazon.com/images/../../private.jpg", ""},
	} {
		got, ok := amazonImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %q = %q, %t; want %q", tc.raw, got, ok, tc.want)
		}
	}
}

func TestAmazonProductImagesAndVariants(t *testing.T) {
	root, _ := url.Parse("https://www.amazon.com/dp/B0B5RJHYFD?tag=private")
	const html = `<html><head><title>fallback</title></head><body><input id="ASIN" value="B0B5RJHYFD"><span id="productTitle"> Product name </span>
<div id="imageBlock_feature_div"><script>var x={'colorImages':{'initial':A.$.parseJSON('[{"hiRes":"https://m.media-amazon.com/images/I/main.jpg?keep=1","large":"https://m.media-amazon.com/images/I/small.jpg","variant":"MAIN"},{"hiRes":null,"large":"https://m.media-amazon.com/images/I/second.jpg","variant":"PT01"}]')}};</script>
<img id="landingImage" data-old-hires="https://m.media-amazon.com/images/I/main.jpg?keep=1"></div>
<div id="mediaBlockVariations_feature_div"><script>var x=jQuery.parseJSON('{"colorImages":{"red":[{"hiRes":"https://m.media-amazon.com/images/I/red.jpg","large":"https://m.media-amazon.com/images/I/redsmall.jpg"}],"blue":[{"hiRes":"https://m.media-amazon.com/images/I/blue.jpg"}]}}');</script></div>
<div id="aplus_feature_div"><img data-src="https://m.media-amazon.com/images/S/detail.jpg" src="https://evil.test/placeholder"><div style="background-image:url('https://images-na.ssl-images-amazon.com/images/S/bg.jpg')"></div><div id="comparison-table-container-0"><img src="https://m.media-amazon.com/images/I/wrong-product.jpg"></div></div>
<div id="recommendations"><img src="https://m.media-amazon.com/images/I/recommend.jpg"><script>var x={"colorImages":{"fake":[{"hiRes":"https://m.media-amazon.com/images/I/wrong.jpg"}]}};</script></div></body></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return amazonTestResponse(raw, 200, html), nil
	})
	got, err := discoverAmazonProduct(context.Background(), getter, root)
	if err != nil || got.Title != "Product name" || len(got.Items) != 6 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.Metadata["image_kind"]]++
		if item.Metadata["amazon_asin"] != "B0B5RJHYFD" || item.Headers["Referer"] != "https://www.amazon.com/" || item.Headers["Cookie"] != "" || !item.RequireImage || strings.Contains(item.URL, "recommend") || strings.Contains(item.URL, "wrong") {
			t.Fatalf("unexpected item: %+v", item)
		}
	}
	if counts["main"] != 2 || counts["sku"] != 2 || counts["detail"] != 2 {
		t.Fatalf("image counts %v", counts)
	}
}

func TestAmazonPartialAndUnsafe(t *testing.T) {
	root, _ := url.Parse("https://www.amazon.com/dp/B0B5RJHYFD")
	for _, mode := range []string{"invalid-image", "no-detail", "bad-asin", "challenge", "wrong-redirect", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				body := `<html><input id="ASIN" value="B0B5RJHYFD"><div id="imageBlock_feature_div"><img id="landingImage" data-old-hires="https://m.media-amazon.com/images/I/main.jpg"></div><div id="aplus_feature_div"><img src="https://m.media-amazon.com/images/S/detail.jpg"></div></html>`
				switch mode {
				case "invalid-image":
					body = strings.Replace(body, `https://m.media-amazon.com/images/S/detail.jpg`, `https://evil.test/detail.jpg`, 1)
				case "no-detail":
					body = strings.Replace(body, `id="aplus_feature_div"`, `id="ignored"`, 1)
				case "bad-asin":
					body = strings.Replace(body, `value="B0B5RJHYFD"`, `value="B012345678"`, 1)
				case "challenge":
					body = `<html><title>Robot Check</title><form action="/errors/validateCaptcha"></form></html>`
				case "cancel":
					cancel()
				}
				r := amazonTestResponse(raw, 200, body)
				if mode == "wrong-redirect" {
					r.FinalURL = "https://www.amazon.com/dp/B012345678"
				}
				return r, nil
			})
			got, err := discoverAmazonProduct(ctx, getter, root)
			want := 0
			if mode == "invalid-image" || mode == "no-detail" {
				want = 1
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s got=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestAmazonUnknownColorExpressionReportsPartial(t *testing.T) {
	root, _ := url.Parse("https://www.amazon.com/dp/B0B5RJHYFD")
	body := `<html><div id="imageBlock_feature_div"><script>var x={'colorImages':{'initial':computeImages()}};</script><img id="landingImage" src="https://m.media-amazon.com/images/I/main.jpg"></div><div id="aplus_feature_div"><img src="https://m.media-amazon.com/images/S/detail.jpg"></div></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return amazonTestResponse(raw, 200, body), nil
	})
	got, err := discoverAmazonProduct(context.Background(), getter, root)
	if err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "规格图片") {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestAmazonParentASINMatchesChildPage(t *testing.T) {
	root, _ := url.Parse("https://www.amazon.com/dp/B0FNN4R5H6")
	for _, tc := range []struct {
		name, parent, container string
		want                    bool
	}{
		{"valid-parent", "B0FNN4R5H6", "mediaBlockVariations_feature_div", true},
		{"wrong-parent", "B012345678", "mediaBlockVariations_feature_div", false},
		{"unrelated-script", "B0FNN4R5H6", "recommendations", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`<html><input id="ASIN" value="B0CGVYWQC8"><div id="%s"><script>var p={"parentAsin":%q};</script></div><img id="landingImage" src="https://m.media-amazon.com/images/I/main.jpg"><div id="aplus_feature_div"><img src="https://m.media-amazon.com/images/S/detail.jpg"></div></html>`, tc.container, tc.parent)
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return amazonTestResponse(raw, 200, body), nil
			})
			got, err := discoverAmazonProduct(context.Background(), getter, root)
			if tc.want && (err != nil || len(got.Items) != 2) {
				t.Fatalf("parent got=%+v err=%v", got, err)
			}
			if !tc.want && (err == nil || len(got.Items) != 0) {
				t.Fatalf("unrelated parent accepted: %+v err=%v", got, err)
			}
		})
	}
}

func TestAmazonRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<html><title>synthetic start</title></html>`)
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const target = "https://www.amazon.com/dp/B0B5RJHYFD"
	const page = `<html><head><title>product</title></head><body><input id="ASIN" value="B0B5RJHYFD">
<div id="imageBlock_feature_div"><img id="landingImage" data-old-hires="https://m.media-amazon.com/images/I/main.jpg"></div>
<div id="aplus_feature_div"><img src="https://m.media-amazon.com/images/S/detail.jpg"><div id="comparison-table-container-0"><img src="https://m.media-amazon.com/images/I/other.jpg"></div></div>
<div id="recommendations"><img src="https://m.media-amazon.com/images/I/recommended.jpg"></div></body></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, 200).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(page)))
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
	if err != nil || session.PageURL() != target || len(got.Items) != 2 || got.Items[0].URL != "https://m.media-amazon.com/images/I/main.jpg" || got.Items[1].URL != "https://m.media-amazon.com/images/S/detail.jpg" {
		t.Fatalf("captured=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestAmazonLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_AMAZON_LIVE") != "1" {
		t.Skip("set TULAI_AMAZON_LIVE=1 for public Amazon product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	root, _ := url.Parse("https://www.amazon.com/dp/B0B5RJHYFD")
	got, err := discoverAmazonProduct(ctx, getter, root)
	if err != nil || len(got.Items) < 3 {
		t.Fatalf("public product images=%d err=%v", len(got.Items), err)
	}
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.Metadata["image_kind"]]++
	}
	if counts["main"] == 0 || counts["detail"] == 0 {
		t.Fatalf("public product categories=%v", counts)
	}
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	err = New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil)
	entries, readErr := os.ReadDir(dir)
	if err != nil || readErr != nil || len(entries) != 1 {
		t.Fatalf("public image err=%v entries=%v read=%v categories=%v", err, entries, readErr, counts)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("public image empty: %v %v", info, err)
	}
	t.Log(fmt.Sprintf("public product images=%d categories=%v", len(got.Items), counts))
}

func TestAmazonLiveVariants(t *testing.T) {
	if os.Getenv("TULAI_AMAZON_LIVE") != "1" {
		t.Skip("set TULAI_AMAZON_LIVE=1 for public Amazon product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root, _ := url.Parse("https://www.amazon.com/dp/B0FNN4R5H6")
	got, err := discoverAmazonProduct(ctx, NewHTTPGetter(""), root)
	counts := map[string]int{}
	colors := map[string]bool{}
	for _, item := range got.Items {
		counts[item.Metadata["image_kind"]]++
		if item.Metadata["image_kind"] == "sku" {
			colors[item.Metadata["variant"]] = true
		}
	}
	if err != nil || counts["main"] == 0 || len(colors) < 2 {
		t.Fatalf("public variants images=%d categories=%v colors=%d err=%v", len(got.Items), counts, len(colors), err)
	}
	t.Logf("public variants images=%d categories=%v colors=%d", len(got.Items), counts, len(colors))
}
