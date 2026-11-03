package download

import (
	"context"
	"encoding/base64"
	"errors"
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
	"golang.org/x/net/html"
)

func TestTaobaoItemURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://item.taobao.com/item.htm?id=123&skuId=1":    "123",
		"https://detail.tmall.com/item.htm?id=123":           "123",
		"https://detail.tmall.hk/item_o.htm?id=123":          "123",
		"https://new.m.taobao.com/detail.htm?id=123":         "123",
		"https://h5.m.taobao.com/awp/core/detail.htm?id=123": "123",
		"https://world.taobao.com/item/123.htm":              "123",
		"https://item.taobao.com/item.htm?id=123&id=999":     "",
		"https://item.taobao.com/item.htm?id=0":              "",
		"https://item.taobao.com/item.htm?id=abc":            "",
		"https://user@item.taobao.com/item.htm?id=123":       "",
		"https://item.taobao.com:444/item.htm?id=123":        "",
		"https://item.taobao.com.evil.test/item.htm?id=123":  "",
		"https://example.test/item.htm?id=123":               "",
		"https://item.taobao.com/search.htm?id=123":          "",
	} {
		u, _ := url.Parse(raw)
		if got := taobaoItemID(u); got != want {
			t.Errorf("taobaoItemID(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestTaobaoCollectMainSKUAndDescription(t *testing.T) {
	page, _ := url.Parse("https://item.taobao.com/item.htm?id=123&token=private")
	body := `<title>商品</title><ul id="J_UlThumb"><li><img src="//img.alicdn.com/a.jpg_60x60q90.jpg"></li></ul>
<div class="SkuContent--valueItem--abc" style="background-image:url('//img.alicdn.com/sku.png_50x50.jpg')"></div>
<div class="descV8-container"><img data-src="//img.alicdn.com/detail.jpg"></div>
<img src="https://img.alicdn.com/recommend.jpg"><script>
var data={"propertyPics":{"blue":"//img.alicdn.com/sku2.jpg_50x50.jpg"},"skuBase":{"props":[{"values":[{"image":"//img.alicdn.com/sku3.jpg"}]}]}};
</script>`
	doc, _ := html.Parse(strings.NewReader(body))
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		t.Fatalf("unexpected request: %s", raw)
		return nil, errors.New("unexpected request")
	})
	got, err := taobaoCollect(context.Background(), getter, page, doc)
	if err != nil || got.Title != "商品" || len(got.Items) != 5 {
		t.Fatalf("item result = %+v, err = %v", got, err)
	}
	want := []string{"https://img.alicdn.com/a.jpg", "https://img.alicdn.com/sku.png", "https://img.alicdn.com/detail.jpg", "https://img.alicdn.com/sku2.jpg", "https://img.alicdn.com/sku3.jpg"}
	for i, item := range got.Items {
		if item.URL != want[i] || item.Headers["Referer"] != "https://item.taobao.com/" {
			t.Errorf("image %d = %+v, want %s", i, item, want[i])
		}
	}
}

func TestTaobaoDescriptionRequestAndPartial(t *testing.T) {
	for _, mode := range []string{"success", "request-failure", "unsafe-url", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			desc := "https://desc.alicdn.com/i1/123/test.desc"
			if mode == "unsafe-url" {
				desc = "http://127.0.0.1/private"
			}
			body := `<title>商品</title><ul id="J_UlThumb"><img src="//img.alicdn.com/a.jpg"></ul><script>var d={"httpsDescUrl":"` + desc + `"};</script>`
			doc, _ := html.Parse(strings.NewReader(body))
			page, _ := url.Parse("https://detail.tmall.com/item.htm?id=123&token=private")
			requests := 0
			getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				requests++
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
				req.Header.Set("Cookie", "session=private")
				req.Header.Set("Authorization", "private")
				prepare(req)
				if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Referer") != "https://detail.tmall.com/" {
					t.Fatalf("description headers: %v", req.Header)
				}
				if mode == "cancel" {
					cancel()
					return nil, ctx.Err()
				}
				status := 200
				if mode == "request-failure" {
					status = 503
				}
				return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`var desc='<img src="//img.alicdn.com/detail.jpg">';`))}, nil
			})
			got, err := taobaoCollect(ctx, getter, page, doc)
			if mode == "success" {
				if err != nil || requests != 1 || len(got.Items) != 2 || got.Items[1].URL != "https://img.alicdn.com/detail.jpg" {
					t.Fatalf("success: items=%+v requests=%d err=%v", got.Items, requests, err)
				}
				return
			}
			if err == nil || len(got.Items) != 1 || got.Items[0].URL != "https://img.alicdn.com/a.jpg" {
				t.Fatalf("partial: items=%+v requests=%d err=%v", got.Items, requests, err)
			}
			if mode == "unsafe-url" && requests != 0 {
				t.Fatalf("private description was requested")
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel = %v", err)
			}
		})
	}
}

func TestTaobaoRejectsUnsafeImagesAndLoginPage(t *testing.T) {
	page, _ := url.Parse("https://item.taobao.com/item.htm?id=123")
	body := `<ul id="J_UlThumb"><img src="https://img.alicdn.com/good.jpg"><img src="http://127.0.0.1/private"><img src="https://user:secret@img.alicdn.com/a.jpg"><img src="https://img.alicdn.com:444/a.jpg"><img src="https://img.alicdn.com.evil.test/a.jpg"></ul><div class="descV8-container"><img src="https://img.alicdn.com/detail.jpg"></div>`
	doc, _ := html.Parse(strings.NewReader(body))
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("unexpected")
	})
	got, err := taobaoCollect(context.Background(), getter, page, doc)
	if err == nil || len(got.Items) != 2 || got.Items[0].URL != "https://img.alicdn.com/good.jpg" {
		t.Fatalf("unsafe image result = %+v, err=%v", got.Items, err)
	}
	login, _ := html.Parse(strings.NewReader(`<script>location.href="https://login.taobao.com/"</script>`))
	got, err = taobaoCollect(context.Background(), getter, page, login)
	if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "登录") {
		t.Fatalf("login result = %+v, err=%v", got.Items, err)
	}
}

func TestTaobaoLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_TAOBAO_LIVE") != "1" {
		t.Skip("set TULAI_TAOBAO_LIVE=1 to probe a public item")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := url.Parse("https://item.taobao.com/item.htm?id=917048988868")
	got, err := discoverTaobaoItem(ctx, NewHTTPGetter(""), page)
	if len(got.Items) == 0 && (err == nil || !strings.Contains(err.Error(), "登录")) {
		t.Fatalf("public item returned no images and no login indication: %+v %v", got, err)
	}
	t.Logf("public item: %d images, err=%v", len(got.Items), err)
}

func TestTaobaoCaptureCurrentItemOnly(t *testing.T) {
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
	const target = "https://item.taobao.com/item.htm?id=123"
	const document = `<html><head><title>当前商品</title></head><body><ul id="J_UlThumb"><img src="https://img.alicdn.com/main.jpg_60x60q90.jpg"></ul><div class="SkuContent--valueItem--x" style="background-image:url('https://img.alicdn.com/sku.jpg')"></div><div class="descV8-container"><img src="https://img.alicdn.com/detail.jpg"></div><img src="https://img.alicdn.com/recommendation.jpg"></body></html>`
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
	if err != nil || got.Title != "当前商品" || len(got.Items) != 3 || got.Items[0].URL != "https://img.alicdn.com/main.jpg" || got.Items[1].URL != "https://img.alicdn.com/sku.jpg" || got.Items[2].URL != "https://img.alicdn.com/detail.jpg" {
		t.Fatalf("captured item = %+v, err = %v", got, err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}
