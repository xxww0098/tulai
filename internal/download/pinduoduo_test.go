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

func pinduoduoTestResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestPinduoduoProductRoutes(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{"https://mobile.yangkeduo.com/goods.html?goods_id=123456", "123456"},
		{"http://mobile.yangkeduo.com/goods.html?goods_id=123456&other=value", "123456"},
		{"https://mobile.yangkeduo.com/goods.html?ps=shortcode", ""},
		{"https://mobile.yangkeduo.com/goods.html?goods_id=0", ""},
		{"https://mobile.yangkeduo.com/goods.html?goods_id=123&goods_id=456", ""},
		{"https://mobile.yangkeduo.com/goods.html?goods_id=abc", ""},
		{"https://mobile.yangkeduo.com.evil.test/goods.html?goods_id=123", ""},
		{"https://user@mobile.yangkeduo.com/goods.html?goods_id=123", ""},
		{"https://mobile.yangkeduo.com:8443/goods.html?goods_id=123", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := pinduoduoProductID(u); got != tc.id {
			t.Errorf("product %q = %q, want %q", tc.raw, got, tc.id)
		}
	}
}

func TestPinduoduoImageHosts(t *testing.T) {
	page, _ := url.Parse("https://mobile.yangkeduo.com/goods.html?goods_id=123")
	for _, tc := range []struct{ raw, want string }{
		{"https://img.pddpic.com/mms-material-img/a.jpg?token=kept", "https://img.pddpic.com/mms-material-img/a.jpg?token=kept"},
		{"http://t00img.yangkeduo.com/goods/images/a.jpg", "https://t00img.yangkeduo.com/goods/images/a.jpg"},
		{"https://omsproductionimg.yangkeduo.com/images/a.jpg", "https://omsproductionimg.yangkeduo.com/images/a.jpg"},
		{"https://img.pddpic.com.evil.test/a.jpg", ""},
		{"https://user@img.pddpic.com/a.jpg", ""},
		{"https://img.pddpic.com:8443/a.jpg", ""},
		{"https://mobile.yangkeduo.com/private", ""},
		{"https://img.pddpic.com/../../private", ""},
	} {
		got, ok := pinduoduoImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q %t want %q", tc.raw, got, ok, tc.want)
		}
	}
}

func TestPinduoduoMainSKUAndDetail(t *testing.T) {
	root, _ := url.Parse("https://mobile.yangkeduo.com/goods.html?goods_id=123&private=source")
	body := `<html><script>window.rawData={"store":{"initDataObj":{"goodsId":123,"goodsName":"示例商品","viewImageData":["https://img.pddpic.com/main.jpg?sign=kept"],"skus":[{"goodsId":123,"thumbUrl":"https://t00img.yangkeduo.com/sku.jpg","specs":[{"spec_key":"颜色","spec_value":"红色"}]}],"detailGallery":[{"url":"https://img.pddpic.com/detail.jpg","width":800}],"goods":{"goods_id":"123","banner":[{"url":"https://img.pddpic.com/banner.jpg"}]}},"recommendations":[{"url":"https://img.pddpic.com/recommendation.jpg"}]}};</script><img src="https://img.pddpic.com/other.jpg"></html>`
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
		prepare(req)
		if req.URL.Host != "mobile.yangkeduo.com" || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected request %s headers=%v", req.URL, req.Header)
		}
		return pinduoduoTestResponse(raw, 200, body), nil
	})
	got, err := discoverPinduoduoProduct(context.Background(), getter, root)
	if err != nil || got.Title != "示例商品" || len(got.Items) != 4 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.Metadata["image_kind"]]++
		if item.Metadata["pinduoduo_goods_id"] != "123" || item.Headers["Referer"] != "https://mobile.yangkeduo.com/" || item.Headers["Cookie"] != "" || !item.RequireImage || strings.Contains(item.URL, "recommendation") || strings.Contains(item.URL, "other.jpg") {
			t.Fatalf("unexpected item %+v", item)
		}
	}
	if counts["main"] != 2 || counts["sku"] != 1 || counts["detail"] != 1 || got.Items[1].Metadata["variant"] != "颜色[红色]" {
		t.Fatalf("counts=%v items=%+v", counts, got.Items)
	}
}

func TestPinduoduoPartialAndSafety(t *testing.T) {
	root, _ := url.Parse("https://mobile.yangkeduo.com/goods.html?goods_id=123")
	for _, mode := range []string{"missing-detail", "invalid-image", "wrong-id", "wrong-sku-id", "login", "challenge", "redirect", "cancel", "expression"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				init := `{"goodsId":123,"viewImageData":["https://img.pddpic.com/main.jpg"],"skus":[{"goodsId":123,"thumbUrl":"https://img.pddpic.com/sku.jpg"}],"detailGallery":[{"url":"https://img.pddpic.com/detail.jpg"}]}`
				switch mode {
				case "missing-detail":
					init = `{"goodsId":123,"viewImageData":["https://img.pddpic.com/main.jpg"],"skus":[]}`
				case "invalid-image":
					init = `{"goodsId":123,"viewImageData":["https://img.pddpic.com/main.jpg"],"skus":[],"detailGallery":[{"url":"https://evil.test/detail.jpg"}]}`
				case "wrong-id":
					init = strings.Replace(init, `"goodsId":123`, `"goodsId":999`, 1)
				case "wrong-sku-id":
					init = strings.Replace(init, `"goodsId":123,"thumbUrl"`, `"goodsId":999,"thumbUrl"`, 1)
				case "login":
					init = `{"needLogin":true}`
				case "cancel":
					cancel()
				}
				body := `<html><script>window.rawData={"store":{"initDataObj":` + init + `}};</script></html>`
				if mode == "challenge" {
					body = `<html><title>登录/验证</title></html>`
				}
				if mode == "expression" {
					body = `<html><script>window.rawData={"store":{"initDataObj":` + init + `}}.map(foo);</script></html>`
				}
				r := pinduoduoTestResponse(raw, 200, body)
				if mode == "redirect" {
					r.FinalURL = "https://attacker.test/goods.html?goods_id=123"
				}
				return r, nil
			})
			got, err := discoverPinduoduoProduct(ctx, getter, root)
			want := 0
			if mode == "missing-detail" || mode == "invalid-image" {
				want = 1
			}
			if mode == "wrong-sku-id" {
				want = 2
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s items=%d err=%v", mode, len(got.Items), err)
			}
			if mode == "login" && !strings.Contains(err.Error(), "登录") {
				t.Fatalf("login error=%v", err)
			}
		})
	}
}

func TestPinduoduoImageLimit(t *testing.T) {
	root, _ := url.Parse("https://mobile.yangkeduo.com/goods.html?goods_id=123")
	var images []string
	for i := 0; i < maxPinduoduoImages+1; i++ {
		images = append(images, fmt.Sprintf(`"https://img.pddpic.com/%d.jpg"`, i))
	}
	body := `<html><script>window.rawData={"store":{"initDataObj":{"goodsId":123,"viewImageData":[` + strings.Join(images, ",") + `],"skus":[],"detailGallery":[]}}};</script></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return pinduoduoTestResponse(raw, 200, body), nil
	})
	got, err := discoverPinduoduoProduct(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "10000") || len(got.Items) != maxPinduoduoImages {
		t.Fatalf("limit=%d err=%v", len(got.Items), err)
	}
}

func TestPinduoduoRenderedCapture(t *testing.T) {
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
	const target = "https://mobile.yangkeduo.com/goods.html?goods_id=123"
	const page = `<html><head><title>product</title></head><body><script>window.rawData={"store":{"initDataObj":{"goodsId":123,"goodsName":"测试商品","viewImageData":["https://img.pddpic.com/main.jpg"],"skus":[{"goodsId":123,"thumbUrl":"https://img.pddpic.com/sku.jpg"}],"detailGallery":[{"url":"https://img.pddpic.com/detail.jpg"}]}}};</script><img src="https://img.pddpic.com/recommended.jpg"></body></html>`
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
	if err != nil || session.PageURL() != target || got.Title != "测试商品" || len(got.Items) != 3 || got.Items[0].URL != "https://img.pddpic.com/main.jpg" || got.Items[1].URL != "https://img.pddpic.com/sku.jpg" || got.Items[2].URL != "https://img.pddpic.com/detail.jpg" {
		t.Fatalf("captured=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestPinduoduoLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_PINDUODUO_LIVE") != "1" {
		t.Skip("set TULAI_PINDUODUO_LIVE=1 for a public product probe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, _ := url.Parse("https://mobile.yangkeduo.com/goods.html?goods_id=590445866953")
	got, err := discoverPinduoduoProduct(ctx, NewHTTPGetter(""), root)
	if err != nil && len(got.Items) == 0 {
		t.Skipf("public product unavailable or requires login: %v", err)
	}
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("public product images=%d err=%v", len(got.Items), err)
	}
}
