package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

const aicaigouTestID = "26e7a502eb87cf44a21a20dd23d026f9"
const aicaigouTestPage = "https://b2b.baidu.com/land?id=" + aicaigouTestID + "10"
const aicaigouTestShop = "https://b2b.baidu.com/shop?xzhid=52423526&tpath=prod"
const aicaigouTestHTML = `<html><title>商品 - 百度爱采购</title><script>window.pageData={}; window.data = {"item":{"id":"26e7a502eb87cf44a21a20dd23d026f9","xzhid":52423526,"fullName":"测试商品","picList":["https://b2b-material.cdn.bcebos.com/main1.jpg","https://ss2.baidu.com/6ONYsj/it/u=1?w=750"],"skuList":[{"image":"https://b2b-material.cdn.bcebos.com/sku.jpg"},{"image":"https://b2b-material.cdn.bcebos.com/main1.jpg"}],"detail_v2":"<p><img src=\"https://t11.baidu.com/it/u=2?w=750&amp;h=999\"></p>"},"recommendList":[{"picUrl":"https://b2b-material.cdn.bcebos.com/recommended.jpg"}]};</script><img src="https://b2b-material.cdn.bcebos.com/recommended.jpg"></html>`

type aicaigouRoundTrip func(*http.Request) (*http.Response, error)

func (f aicaigouRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func aicaigouResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestAicaigouRoutes(t *testing.T) {
	for _, tc := range []struct{ raw, product, shop string }{
		{aicaigouTestPage, aicaigouTestID, ""},
		{"https://b2b.baidu.com/land/" + aicaigouTestID, aicaigouTestID, ""},
		{"http://b2b.baidu.com/land?id=" + aicaigouTestID + "10", aicaigouTestID, ""},
		{"https://b2b.baidu.com/land?id=" + aicaigouTestID, "", ""},
		{"https://b2b.baidu.com/land?id=" + aicaigouTestID + "10&id=" + aicaigouTestID + "10", "", ""},
		{"https://b2b.baidu.com/land/" + aicaigouTestID + "?id=other", "", ""},
		{"https://user@b2b.baidu.com/land/" + aicaigouTestID, "", ""},
		{"https://b2b.baidu.com.evil.test/land/" + aicaigouTestID, "", ""},
		{"https://b2b.baidu.com:444/land/" + aicaigouTestID, "", ""},
		{aicaigouTestShop, "", "52423526"},
		{"https://b2b.baidu.com/shop/52423526", "", "52423526"},
		{"https://b2b.baidu.com/shop?xzhid=52423526&tpath=index", "", "52423526"},
		{"https://b2b.baidu.com/shop?xzhid=52423526&xzhid=2", "", ""},
		{"https://b2b.baidu.com/shop/52423526?xzhid=2", "", ""},
		{"https://b2b.baidu.com/shop?xzhid=52423526&tpath=other", "", ""},
		{"https://b2b.baidu.com/shop?xzhid=000", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := aicaigouProductID(u); got != tc.product {
			t.Errorf("product %s = %q", tc.raw, got)
		}
		if got := aicaigouShopID(u); got != tc.shop {
			t.Errorf("shop %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(aicaigouTestPage)
	for _, tc := range []struct{ raw, want string }{
		{"//b2b-material.cdn.bcebos.com/main.jpg?sig=keep", "https://b2b-material.cdn.bcebos.com/main.jpg?sig=keep"},
		{"http://t11.baidu.com/it/u=1", "https://t11.baidu.com/it/u=1"},
		{"https://bj.bcebos.com/b2b-openapi-attachment/main.png?authorization=example", "https://bj.bcebos.com/b2b-openapi-attachment/main.png?authorization=example"},
		{"https://bj.bcebos.com/other-bucket/main.png", ""},
		{"http://b2b-openapi-attachment.cdn.bcebos.com/a.jpg", "https://b2b-openapi-attachment.cdn.bcebos.com/a.jpg"},
		{"https://b2b-material.cdn.bcebos.com.evil.test/a.jpg", ""},
		{"https://user@ss2.baidu.com/a.jpg", ""},
		{"https://ss2.baidu.com:8443/a.jpg", ""},
		{"https://ss2.baidu.com/a/../private.jpg", ""},
		{"data:image/png;base64,AA", ""},
	} {
		got, ok := aicaigouImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q ok=%t", tc.raw, got, ok)
		}
	}
}

func TestAicaigouProductAndDetails(t *testing.T) {
	page, _ := url.Parse(aicaigouTestPage + "&tracking=private")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return aicaigouResponse(raw, 200, aicaigouTestHTML), nil
	})
	got, err := Discover(context.Background(), getter, page.String(), 1)
	if err != nil || got.Title != "测试商品" || len(got.Items) != 4 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	want := []string{"main", "main", "variant", "description"}
	for i, item := range got.Items {
		if !item.RequireImage || item.Metadata["aicaigou_product_id"] != aicaigouTestID || item.Metadata["aicaigou_shop_id"] != "52423526" || item.Metadata["image_kind"] != want[i] || item.Headers["Referer"] != "https://b2b.baidu.com/" || item.Headers["Cookie"] != "" || strings.Contains(item.URL, "recommended") {
			t.Errorf("image %d = %+v", i, item)
		}
	}
	if got.Items[3].URL != "https://t11.baidu.com/it/u=2?w=750&h=999" {
		t.Fatalf("HTML entity decoding: %s", got.Items[3].URL)
	}
}

func TestAicaigouPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(aicaigouTestPage)
	for _, tc := range []struct {
		mode string
		want int
	}{{"wrong-id", 0}, {"invalid-image", 3}, {"bad-sku", 3}, {"challenge", 0}, {"dom-fallback", 2}, {"cancel", 0}} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := aicaigouTestHTML
			switch tc.mode {
			case "wrong-id":
				body = strings.Replace(body, aicaigouTestID, strings.Repeat("a", 32), 1)
			case "invalid-image":
				body = strings.Replace(body, "https://b2b-material.cdn.bcebos.com/sku.jpg", "https://evil.test/sku.jpg", 1)
			case "bad-sku":
				body = strings.Replace(body, `"skuList":[{"image":"https://b2b-material.cdn.bcebos.com/sku.jpg"},{"image":"https://b2b-material.cdn.bcebos.com/main1.jpg"}]`, `"skuList":false`, 1)
			case "challenge":
				body = `<title>百度安全验证</title><img src="https://b2b-material.cdn.bcebos.com/recommended.jpg">`
			case "dom-fallback":
				body = `<div class="img-big pcui-img"><img src="https://b2b-material.cdn.bcebos.com/main.jpg"></div><div class="rich-text"><img src="https://t11.baidu.com/it/u=2"></div><img src="https://b2b-material.cdn.bcebos.com/recommended.jpg">`
			case "cancel":
				cancel()
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return aicaigouResponse(raw, 200, body), nil
			})
			got, err := discoverAicaigouProduct(ctx, getter, page)
			if err == nil || len(got.Items) != tc.want || (tc.mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s count=%d err=%v", tc.mode, len(got.Items), err)
			}
			for _, item := range got.Items {
				if strings.Contains(item.URL, "recommended") || strings.Contains(item.URL, "evil.test") {
					t.Fatalf("unsafe result %+v", item)
				}
			}
		})
	}
}

func TestAicaigouRedirects(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			jar, _ := cookiejar.New(nil)
			page, _ := url.Parse(aicaigouTestPage)
			jar.SetCookies(page, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			calls := 0
			getter.client.Transport = aicaigouRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("credentials sent to %s", r.URL.Host)
				}
				body := aicaigouTestHTML
				status := 200
				header := http.Header{"Content-Type": {"text/html"}}
				if calls == 1 {
					status, body = 302, ""
					target := "https://b2b.baidu.com/land/" + aicaigouTestID
					switch mode {
					case "wrong-id":
						target = "https://b2b.baidu.com/land/" + strings.Repeat("a", 32)
					case "cross-origin":
						target = "https://evil.test/land/" + aicaigouTestID
					case "downgrade":
						target = "http://b2b.baidu.com/land/" + aicaigouTestID
					}
					header.Set("Location", target)
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := discoverAicaigouProduct(context.Background(), getter, page)
			if mode == "success" {
				if err != nil || len(got.Items) != 4 || calls != 2 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || calls != 1 || len(got.Items) != 0 {
				t.Fatalf("unsafe redirect=%+v calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func aicaigouFixtureProduct(index int) map[string]any {
	id := fmt.Sprintf("%032x", index)
	return map[string]any{"id": id, "xzhid": 52423526, "jUrl": "https://b2b.baidu.com/land/" + id}
}

func aicaigouJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestAicaigouShopPagination(t *testing.T) {
	for _, mode := range []string{"success", "api-error", "wrong-seller", "product-403", "repeated-page"} {
		t.Run(mode, func(t *testing.T) {
			var first []map[string]any
			for i := 1; i <= 30; i++ {
				first = append(first, aicaigouFixtureProduct(i))
			}
			if mode == "wrong-seller" {
				first[9]["xzhid"] = 1
			}
			page, _ := url.Parse(aicaigouTestShop)
			productCalls, pageCalls := 0, 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, err := url.Parse(raw)
				if err != nil || u.Host != "b2b.baidu.com" || u.Scheme != "https" {
					t.Errorf("unsafe request %s", raw)
					return nil, errors.New("unsafe request")
				}
				switch {
				case u.Path == "/shop":
					state := map[string]any{"xzhId": 52423526, "entName": "测试店铺", "dispNum": 31, "size": 30, "productList": first, "recommendList": []any{aicaigouFixtureProduct(999)}}
					return aicaigouResponse(raw, 200, `<script>window.data=`+aicaigouJSON(state)+`;</script>`), nil
				case u.Path == "/land/entProdAjax":
					pageCalls++
					if u.Query().Get("xzhid") != "52423526" || u.Query().Get("p") != "2" || u.Query().Get("s") != "30" {
						t.Errorf("unexpected pagination: %s", raw)
					}
					if mode == "api-error" {
						return aicaigouResponse(raw, 429, ""), nil
					}
					last := aicaigouFixtureProduct(31)
					if mode == "repeated-page" {
						last = aicaigouFixtureProduct(1)
					}
					return aicaigouResponse(raw, 200, aicaigouJSON(map[string]any{"status": 0, "data": map[string]any{"dispNum": 31, "productList": []any{last}}})), nil
				case strings.HasPrefix(u.Path, "/land/"):
					productCalls++
					if mode == "product-403" && productCalls == 2 {
						return aicaigouResponse(raw, 403, ""), nil
					}
					id := strings.TrimPrefix(u.Path, "/land/")
					title := "商品"
					if id == fmt.Sprintf("%032x", 31) {
						title = ""
					}
					item := map[string]any{"id": id, "xzhid": 52423526, "fullName": title, "picList": []string{"https://b2b-material.cdn.bcebos.com/shared.jpg"}, "skuList": []any{}, "detail_v2": ""}
					return aicaigouResponse(raw, 200, `<script>window.data=`+aicaigouJSON(map[string]any{"item": item})+`;</script>`), nil
				default:
					t.Errorf("unexpected request %s", raw)
					return nil, errors.New("unexpected request")
				}
			})
			got, err := Discover(context.Background(), getter, page.String(), 1)
			want, wantPages := 31, 1
			switch mode {
			case "api-error":
				want = 30
			case "wrong-seller":
				want = 30
			case "product-403":
				want, wantPages = 1, 0
			case "repeated-page":
				want = 30
			}
			if len(got.Items) != want || pageCalls != wantPages || (mode == "success") != (err == nil) {
				t.Fatalf("mode=%s count=%d productcalls=%d pagecalls=%d err=%v", mode, len(got.Items), productCalls, pageCalls, err)
			}
			if got.Title != "测试店铺" {
				t.Fatalf("title=%q", got.Title)
			}
			for _, item := range got.Items {
				wantTitle := "商品"
				if item.Metadata["album_id"] == fmt.Sprintf("%032x", 31) {
					wantTitle = "商品 " + item.Metadata["album_id"]
				}
				if item.Metadata["aicaigou_shop_id"] != "52423526" || item.Metadata["album_id"] != item.Metadata["aicaigou_product_id"] || item.Metadata["album_title"] != wantTitle || strings.Contains(item.URL, "000000000000000000000000000003e7") {
					t.Fatalf("unexpected shop item %+v", item)
				}
			}
		})
	}
}

func TestAicaigouShopRejectsRedirectAndChallenge(t *testing.T) {
	page, _ := url.Parse(aicaigouTestShop)
	for _, tc := range []struct{ mode, body, final string }{
		{"challenge", `<title>百度安全验证</title>`, ""},
		{"wrong-shop", `<script>window.data={"xzhId":1,"dispNum":0,"size":30,"productList":[]};</script>`, ""},
		{"cross-origin", `<script>window.data={"xzhId":52423526,"dispNum":0,"size":30,"productList":[]};</script>`, "https://evil.test/"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				r := aicaigouResponse(raw, 200, tc.body)
				if tc.final != "" {
					r.FinalURL = tc.final
				}
				return r, nil
			})
			got, err := discoverAicaigouShop(context.Background(), getter, page)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("shop=%+v err=%v", got, err)
			}
		})
	}
}

func TestAicaigouRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `<html></html>`) }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == aicaigouTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(aicaigouTestHTML)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case interceptErrors <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	session.start, _ = url.Parse(aicaigouTestPage)
	if err := session.run(ctx, camoufox.Navigate(aicaigouTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".baidu.com").WithURL("https://b2b.baidu.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = aicaigouRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "b2b-material.cdn.bcebos.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://b2b.baidu.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 4 || got.Items[0].URL != "https://b2b-material.cdn.bcebos.com/main1.jpg" || session.PageURL() != aicaigouTestPage {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestAicaigouLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_AICAIGOU_LIVE") != "1" {
		t.Skip("set TULAI_AICAIGOU_LIVE=1 for public product verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	page, _ := url.Parse("https://b2b.baidu.com/land?id=a80b4c964e564124011845857070199b10")
	got, err := discoverAicaigouProduct(ctx, getter, page)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	selected := make(map[string]Item)
	for _, item := range got.Items {
		kind := item.Metadata["image_kind"]
		counts[kind]++
		if _, ok := selected[kind]; !ok {
			selected[kind] = item
		}
	}
	for _, kind := range []string{"main", "variant", "description"} {
		item, ok := selected[kind]
		if !ok {
			t.Fatalf("missing %s: %v", kind, counts)
		}
		item.Filename = "image"
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("%s entries=%v err=%v", kind, entries, err)
		}
		if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
			t.Fatalf("%s empty: %v %v", kind, info, err)
		}
	}
	t.Logf("public product counts=%v; main, variant, description downloaded", counts)
}

func TestAicaigouLiveShopPages(t *testing.T) {
	if os.Getenv("TULAI_AICAIGOU_LIVE") != "1" {
		t.Skip("set TULAI_AICAIGOU_LIVE=1 for public shop verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	page := &url.URL{Scheme: "https", Host: "b2b.baidu.com", Path: "/shop", RawQuery: url.Values{"xzhid": {"52423526"}, "tpath": {"prod"}}.Encode()}
	first, err := aicaigouShopPage(ctx, getter, page, "52423526")
	if err != nil {
		t.Fatal(err)
	}
	second, err := aicaigouShopAPI(ctx, getter, page, "52423526", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Products) == 0 || len(second.Products) == 0 || first.Products[0].ID == second.Products[0].ID {
		t.Fatalf("pages first=%d second=%d", len(first.Products), len(second.Products))
	}
	product, err := crawlURL(page, first.Products[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := discoverAicaigouProduct(ctx, getter, product)
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("shop product count=%d err=%v", len(got.Items), err)
	}
	if got.Items[0].Metadata["aicaigou_shop_id"] != "52423526" {
		t.Fatal("shop/product ownership mismatch")
	}
	item := got.Items[0]
	item.Filename = "shop-product"
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("shop product image download: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("shop product image entries=%v err=%v", entries, err)
	}
	t.Logf("public shop total=%d page1=%d page2=%d first product=%d images", *first.Total, len(first.Products), len(second.Products), len(got.Items))
}

func TestAicaigouDOMOnlyFallback(t *testing.T) {
	page, _ := url.Parse(aicaigouTestPage)
	doc, err := html.Parse(strings.NewReader(`<div class="img-big pcui-img"><img src="https://ss2.baidu.com/main.jpg"></div><div class="rich-text"><img src="https://t10.baidu.com/detail.jpg"></div><img src="https://ss2.baidu.com/recommend.jpg">`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := aicaigouProductPage(context.Background(), doc, page)
	if err == nil || len(got.Items) != 2 {
		t.Fatalf("fallback=%+v err=%v", got, err)
	}
}

func TestAicaigouShopRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `<html></html>`) }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == aicaigouTestShop {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(`<title>店铺</title>`)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case interceptErrors <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	session.start, _ = url.Parse(aicaigouTestShop)
	if err := session.run(ctx, camoufox.Navigate(aicaigouTestShop)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".baidu.com").WithURL("https://b2b.baidu.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var sameOriginCalls, imageCalls int
	session.getter.client.Transport = aicaigouRoundTrip(func(r *http.Request) (*http.Response, error) {
		status, body := 200, ""
		switch {
		case r.URL.Host == "b2b.baidu.com" && r.URL.Path == "/shop":
			sameOriginCalls++
			if !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") {
				t.Error("synthetic session missing from same-origin shop request")
			}
			listing := map[string]any{"xzhId": 52423526, "entName": "测试店铺", "dispNum": 1, "size": 30, "productList": []any{map[string]any{"id": aicaigouTestID, "xzhid": 52423526, "jUrl": "https://b2b.baidu.com/land/" + aicaigouTestID}}}
			body = `<script>window.data=` + aicaigouJSON(listing) + `;</script>`
		case r.URL.Host == "b2b.baidu.com" && r.URL.Path == "/land/"+aicaigouTestID:
			sameOriginCalls++
			if !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") {
				t.Error("synthetic session missing from same-origin product request")
			}
			body = aicaigouTestHTML
		case r.URL.Host == "b2b-material.cdn.bcebos.com":
			imageCalls++
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://b2b.baidu.com/" {
				t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
			}
			body = "synthetic image"
		default:
			t.Errorf("unexpected browser getter request %s", r.URL)
			status = 500
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 4 || session.PageURL() != aicaigouTestShop || sameOriginCalls != 2 {
		t.Fatalf("capture=%+v page=%s same-origin calls=%d err=%v", got, session.PageURL(), sameOriginCalls, err)
	}
	for _, item := range got.Items {
		if item.Metadata["album_id"] != aicaigouTestID || item.Metadata["album_title"] != "测试商品" {
			t.Fatalf("grouping=%+v", item.Metadata)
		}
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if imageCalls != 1 {
		t.Fatalf("image calls=%d", imageCalls)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}
