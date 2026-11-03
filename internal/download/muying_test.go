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
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"golang.org/x/net/html"
)

const muyingTestProduct = "https://wybbej.mytx.cn/ProductDetails.aspx?productId=123"
const muyingTestCategory = "https://wybbej.mytx.cn/SubCategory.aspx?categoryId=abc&pageindex=2"

func muyingProductFixture(main, detail string) string {
	return `<html><head><meta name="description" content="测试商品"></head><body><a class="cloud-zoom-gallery" href="` + main + `"><img src="/ignored-thumb.jpg"></a><div class="product_detail"><img src="` + detail + `"><div class="quanbupji"><img src="/review.jpg"></div><img src="/after-reviews.jpg"></div><img src="/recommendation.jpg"></body></html>`
}
func muyingCategoryFixture() string {
	return `<html><head><title>分类页</title></head><body><div class="category_pro_list"><a href="/ProductDetails.aspx?productId=123">1</a><a href="/ProductDetails.aspx?productId=456">2</a><a href="/ProductDetails.aspx?productId=123">重复</a><a href="https://evil.test/ProductDetails.aspx?productId=9">外站</a></div><a href="/SubCategory.aspx?categoryId=abc&amp;pageindex=3">下一页</a></body></html>`
}
func muyingDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func muyingResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}
}

type muyingTestTransport func(*http.Request) (*http.Response, error)

func (f muyingTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMuyingReference(t *testing.T) {
	for _, tc := range []struct {
		raw, kind, id string
		page          int
	}{
		{muyingTestProduct, "product", "123", 1}, {muyingTestCategory, "category", "abc", 2},
		{"http://wybbej.mytx.cn/ProductDetails.aspx?productId=abc-def", "product", "abc-def", 1},
		{"https://wybbej.mytx.cn/ProductDetails.aspx?productId=1&productId=2", "", "", 0},
		{"https://wybbej.mytx.cn/ProductDetails.aspx?productId=", "", "", 0},
		{"https://wybbej.mytx.cn/ProductDetails.aspx?categoryId=123", "", "", 0},
		{"https://wybbej.mytx.cn/SubCategory.aspx?categoryId=1&pageindex=0", "", "", 0},
		{"https://wybbej.mytx.cn/SubCategory.aspx?categoryId=1&pageindex=2&pageindex=3", "", "", 0},
		{"https://evil.test/ProductDetails.aspx?productId=123", "", "", 0},
		{"https://wybbej.mytx.cn.evil.test/ProductDetails.aspx?productId=123", "", "", 0},
		{"https://user@wybbej.mytx.cn/ProductDetails.aspx?productId=123", "", "", 0},
		{"https://wybbej.mytx.cn:8443/ProductDetails.aspx?productId=123", "", "", 0},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := muyingPageRef(u)
		if got.kind != tc.kind || got.id != tc.id || got.page != tc.page {
			t.Errorf("%q ref=%+v", tc.raw, got)
		}
	}
}

func TestMuyingProductSelectors(t *testing.T) {
	page, _ := url.Parse(muyingTestProduct + "&token=secret")
	body := muyingProductFixture("/images/main.jpg?format=1", "/images/detail.png")
	got, err := muyingProduct(context.Background(), page, muyingDoc(t, body))
	if err == nil || len(got.Items) != 2 || got.Items[0].URL != "https://wybbej.mytx.cn/images/main.jpg?format=1" || got.Items[1].URL != "https://wybbej.mytx.cn/images/detail.png" || got.Title != "测试商品" {
		t.Fatalf("items=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Metadata["muying_product_id"] != "123" || strings.Contains(fmt.Sprint(item.Headers), "secret") {
			t.Fatalf("unsafe item %+v", item)
		}
	}
}

func TestMuyingImageRestrictionsAndIdentity(t *testing.T) {
	page, _ := url.Parse(muyingTestProduct)
	for _, raw := range []string{"https://evil.test/a.jpg", "https://wybbej.mytx.cn.evil.test/a.jpg", "https://user@wybbej.mytx.cn/a.jpg", "https://wybbej.mytx.cn:8443/a.jpg", "http://wybbej.mytx.cn/a.jpg", "/a.php", "/dir/../a.jpg"} {
		if got, ok := muyingImage(page, raw); ok {
			t.Errorf("unsafe image accepted %q", got)
		}
	}
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"external main", muyingProductFixture("https://evil.test/main.jpg", "/ok.png"), 1},
		{"unsupported detail", muyingProductFixture("/ok.jpg", "https://evil.test/image.png"), 1},
		{"no identified images", `<html><body><img src="/recommendation.jpg"></body></html>`, 0},
		{"reviews only", `<div class="product_detail"><div class="quanbupji"><img src="/review.jpg"></div></div>`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := muyingProduct(context.Background(), page, muyingDoc(t, tc.body))
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestMuyingCategoryPartialAndNoPagination(t *testing.T) {
	root, _ := url.Parse(muyingTestCategory)
	var requests []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		requests = append(requests, raw)
		switch raw {
		case muyingTestCategory:
			return muyingResponse(raw, muyingCategoryFixture()), nil
		case muyingTestProduct:
			return muyingResponse(raw, muyingProductFixture("/images/main.jpg", "/images/detail.png")), nil
		case "https://wybbej.mytx.cn/ProductDetails.aspx?productId=456":
			return nil, errors.New("unavailable")
		default:
			t.Fatalf("unexpected request: %q", raw)
			return nil, nil
		}
	})
	got, err := discoverMuying(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "部分商品") || len(got.Items) != 2 || len(requests) != 3 {
		t.Fatalf("items=%+v requests=%v err=%v", got.Items, requests, err)
	}
}

func TestMuyingRedirectAndCancellation(t *testing.T) {
	root, _ := url.Parse(muyingTestProduct)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("canceled request sent")
		return nil, nil
	})
	if _, err := discoverMuying(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		r := muyingResponse(raw, muyingProductFixture("/a.jpg", "/b.png"))
		r.FinalURL = "https://wybbej.mytx.cn/ProductDetails.aspx?productId=456"
		return r, nil
	})
	if got, err := discoverMuying(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect=%+v err=%v", got.Items, err)
	}
	client := NewHTTPGetter("")
	var external atomic.Bool
	client.client.Transport = muyingTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "wybbej.mytx.cn" {
			external.Store(true)
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/path"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if got, err := discoverMuying(context.Background(), client, root); err == nil || len(got.Items) != 0 || external.Load() {
		t.Fatalf("external redirect visited=%v items=%+v err=%v", external.Load(), got.Items, err)
	}
}

func TestMuyingRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for an isolated browser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	body := muyingProductFixture("/images/main.jpg", "/images/detail.png")
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == muyingTestProduct {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if e := camoufox.Run(session.tab, action); e != nil {
				select {
				case responses <- e:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	start, _ := url.Parse(muyingTestProduct)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(muyingTestProduct)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case e := <-responses:
		t.Fatalf("intercept: %v", e)
	default:
	}
	if err == nil || len(got.Items) != 2 || got.Items[0].Metadata["muying_image_kind"] != "main" {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
}
