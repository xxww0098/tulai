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
	"github.com/chromedp/chromedp"
)

const shopeeShop = "314649750"
const shopeeItemID = "7065872218"
const shopeeBase = "https://shopee.sg/product/314649750/7065872218"
const shopeeImage = "https://down-sg.img.susercontent.com/file/ac2094cb365c432d815215bb9a8ec5f5"
const shopeeHTML = `<html><head><meta property="og:url" content="https://shopee.sg/product/314649750/7065872218"><meta property="og:image" content="` + shopeeImage + `"><meta property="og:title" content="Product"></head><body><img src="https://down-sg.img.susercontent.com/file/ffffffffffffffffffffffffffffffff"></body></html>`

func shopeeTestResponse(raw, body, contentType string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestShopeeProductURLs(t *testing.T) {
	for _, tc := range []struct{ raw, region, shop, item string }{
		{shopeeBase, "sg", shopeeShop, shopeeItemID},
		{"https://shopee.sg/Some-Product-i.314649750.7065872218?token=private", "sg", shopeeShop, shopeeItemID},
		{"https://shopee.sg/shop/314649750/item/7065872218", "sg", shopeeShop, shopeeItemID},
		{"https://www.shopee.com.my/product/12/34", "my", "12", "34"},
		{"https://shopee.co.th/product/12/34", "th", "12", "34"},
		{"https://shopee.co.id/product/12/34", "id", "12", "34"},
		{"https://shopee.vn/product/12/34", "vn", "12", "34"},
		{"https://shopee.ph/product/12/34", "ph", "12", "34"},
		{"https://shopee.tw/product/12/34", "tw", "12", "34"},
		{"https://shopee.com.br/product/12/34", "br", "12", "34"},
		{"https://shopee.sg/product/12/34/other", "", "", ""},
		{"https://shopee.sg/search?shop_id=12&item_id=34", "", "", ""},
		{"https://shopee.sg.evil.test/product/12/34", "", "", ""},
		{"https://user@shopee.sg/product/12/34", "", "", ""},
		{"https://shopee.sg:8443/product/12/34", "", "", ""},
		{"https://shopee.sg/product/0/34", "", "", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		got := shopeePageRef(u)
		if got.region != tc.region || got.shop != tc.shop || got.item != tc.item || isShopeeProduct(u) != (tc.item != "") {
			t.Errorf("ref(%q)=%+v", tc.raw, got)
		}
	}
}

func TestShopeeImageBoundary(t *testing.T) {
	ref := shopeeRef{region: "sg", shop: shopeeShop, item: shopeeItemID}
	for _, raw := range []string{
		"https://down-sg.img.susercontent.com.evil.test/file/ac2094cb365c432d815215bb9a8ec5f5",
		"https://down-my.img.susercontent.com/file/ac2094cb365c432d815215bb9a8ec5f5",
		"https://user@down-sg.img.susercontent.com/file/ac2094cb365c432d815215bb9a8ec5f5",
		"https://down-sg.img.susercontent.com:8443/file/ac2094cb365c432d815215bb9a8ec5f5",
		"http://down-sg.img.susercontent.com/file/ac2094cb365c432d815215bb9a8ec5f5",
		"https://down-sg.img.susercontent.com/file/../private",
		"https://127.0.0.1/file/ac2094cb365c432d815215bb9a8ec5f5",
	} {
		if _, ok := shopeeMediaURL(ref, raw); ok {
			t.Errorf("accepted unsafe media %q", raw)
		}
	}
	if got, ok := shopeeMediaURL(ref, "ac2094cb365c432d815215bb9a8ec5f5"); !ok || got != shopeeImage {
		t.Fatalf("id URL %q %v", got, ok)
	}
}

func TestShopeeStructuredDataBoundToShopAndItem(t *testing.T) {
	root, _ := url.Parse(shopeeBase)
	body := `<script type="application/json">{"data":{"item":{"item_id":7065872218,"shop_id":314649750,"name":"Correct"},"product_images":{"cover_image":"ac2094cb365c432d815215bb9a8ec5f5","images":["bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"],"long_images":["cccccccccccccccccccccccccccccccc"],"first_tier_variations":[{"image":"dddddddddddddddddddddddddddddddd"}],"gallery_contents":[{"image":{"url":"https://down-sg.img.susercontent.com/file/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}}]}},"recommendations":[{"item":{"item_id":3,"shop_id":4},"product_images":{"images":["ffffffffffffffffffffffffffffffff"]}}]}</script>`
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		return shopeeTestResponse(raw, body, "text/html"), nil
	})
	got, err := discoverShopeeProduct(context.Background(), getter, root)
	if err != nil || calls != 1 || got.Title != "Correct" || len(got.Items) != 5 {
		t.Fatalf("got=%+v calls=%d err=%v", got, calls, err)
	}
	for _, item := range got.Items {
		if strings.Contains(item.URL, "ffffffff") || item.Headers["Referer"] != "https://shopee.sg/" {
			t.Fatalf("unsafe item %+v", item)
		}
	}
}

func TestShopeeAPIAndPartialFailure(t *testing.T) {
	root, _ := url.Parse(shopeeBase + "?secret=synthetic")
	for _, tc := range []struct {
		name    string
		status  int
		data    string
		count   int
		wantErr bool
	}{
		{"api", 200, `{"data":{"item":{"item_id":7065872218,"shop_id":314649750},"product_images":{"images":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"],"first_tier_variations":[{"image":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]}}}`, 3, false},
		{"wrong-item", 200, `{"data":{"item":{"item_id":7777777777,"shop_id":314649750},"product_images":{"images":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}}}`, 1, true},
		{"wrong-shop", 200, `{"data":{"item":{"item_id":7065872218,"shop_id":999999999},"product_images":{"images":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}}}`, 1, true},
		{"blocked", 403, `{"error":90309999}`, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
				calls++
				u, err := url.Parse(raw)
				if err != nil {
					t.Fatal(err)
				}
				if u.Path == "/product/314649750/7065872218" {
					return shopeeTestResponse(raw, shopeeHTML, "text/html"), nil
				}
				if u.Host != "shopee.sg" || u.Path != "/api/v4/pdp/get_pc" || u.Query().Get("shop_id") != shopeeShop || u.Query().Get("item_id") != shopeeItemID || strings.Contains(raw, "synthetic") {
					t.Fatalf("bad API URL %q", raw)
				}
				req, _ := http.NewRequest("GET", raw, nil)
				prepare(req)
				if req.Header.Get("Accept") != "application/json" {
					t.Fatal("missing JSON accept")
				}
				resp := shopeeTestResponse(raw, tc.data, "application/json")
				resp.StatusCode = tc.status
				return resp, nil
			})
			got, err := discoverShopeeProduct(context.Background(), getter, root)
			if calls != 2 || len(got.Items) != tc.count || (err != nil) != tc.wantErr {
				t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
			}
		})
	}
}

func TestShopeeRejectsUnboundCoverAndRedirect(t *testing.T) {
	root, _ := url.Parse(shopeeBase)
	for _, body := range []string{
		strings.Replace(shopeeHTML, "7065872218", "7777777777", 1),
		strings.Replace(shopeeHTML, shopeeImage, "https://down-sg.img.susercontent.com.evil.test/file/ac2094cb365c432d815215bb9a8ec5f5", 1),
		`<img src="` + shopeeImage + `">`,
	} {
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			u, _ := url.Parse(raw)
			if u.Path == "/api/v4/pdp/get_pc" {
				resp := shopeeTestResponse(raw, "{}", "application/json")
				resp.StatusCode = 403
				return resp, nil
			}
			return shopeeTestResponse(raw, body, "text/html"), nil
		})
		got, err := discoverShopeeProduct(context.Background(), getter, root)
		if err == nil || len(got.Items) != 0 {
			t.Fatalf("unbound items=%+v err=%v", got.Items, err)
		}
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := shopeeTestResponse(raw, shopeeHTML, "text/html")
		resp.FinalURL = "https://evil.test/product/314649750/7065872218"
		return resp, nil
	})
	if got, err := discoverShopeeProduct(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect=%+v err=%v", got, err)
	}
}

func TestShopeeUnrelatedDataAndUnsafeAPI(t *testing.T) {
	root, _ := url.Parse(shopeeBase)
	for _, mode := range []string{"unrelated", "redirect", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, _ := url.Parse(raw)
				if u.Path != "/api/v4/pdp/get_pc" {
					if mode == "unrelated" {
						return shopeeTestResponse(raw, `<script type="application/json">{"item":{"shop_id":9,"item_id":8,"images":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}}</script>`, "text/html"), nil
					}
					return shopeeTestResponse(raw, shopeeHTML, "text/html"), nil
				}
				resp := shopeeTestResponse(raw, `{}`, "application/json")
				switch mode {
				case "unrelated":
					resp.StatusCode = http.StatusForbidden
				case "redirect":
					resp.FinalURL = "https://evil.test/api/v4/pdp/get_pc"
				case "oversize":
					resp.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxPageBytes+1)))
				}
				return resp, nil
			})
			got, err := discoverShopeeProduct(context.Background(), getter, root)
			want := 1
			if mode == "unrelated" {
				want = 0
			}
			if err == nil || len(got.Items) != want {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestShopeeCancellation(t *testing.T) {
	root, _ := url.Parse(shopeeBase)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("request after cancellation")
		return nil, nil
	})
	if _, err := discoverShopeeProduct(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

type shopeeRoundTripper func(*http.Request) (*http.Response, error)

func (fn shopeeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestShopeeRenderedCapture(t *testing.T) {
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
	const target = shopeeBase + "?token=synthetic"
	var calls, cookies, mediaCalls, mediaCookies atomic.Int32
	session.getter.client.Transport = shopeeRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "down-sg.img.susercontent.com" && r.URL.Path == "/file/ac2094cb365c432d815215bb9a8ec5f5" {
			mediaCalls.Add(1)
			if len(r.Cookies()) != 0 {
				mediaCookies.Add(1)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
		}
		calls.Add(1)
		if r.URL.Host != "shopee.sg" || r.URL.Path != "/api/v4/pdp/get_pc" {
			return nil, errors.New("unexpected HTTP request")
		}
		for _, c := range r.Cookies() {
			if c.Name == "synthetic_session" && c.Value == "example" {
				cookies.Add(1)
			}
		}
		return &http.Response{StatusCode: 403, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader("{}")), Request: r}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(shopeeHTML)))
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
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://shopee.sg/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != shopeeImage || calls.Load() != 1 || cookies.Load() != 1 {
		t.Fatalf("capture=%+v calls=%d cookies=%d err=%v", got.Items, calls.Load(), cookies.Load(), err)
	}
	media, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatalf("media request: %v", err)
	}
	media.Body.Close()
	if mediaCalls.Load() != 1 || mediaCookies.Load() != 0 {
		t.Fatalf("cross-origin media calls=%d cookies=%d", mediaCalls.Load(), mediaCookies.Load())
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestShopeeLiveProductAndCDN(t *testing.T) {
	if os.Getenv("TULAI_SHOPEE_LIVE") != "1" {
		t.Skip("set TULAI_SHOPEE_LIVE=1 to check a public Shopee product and image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	root, _ := url.Parse(shopeeBase)
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, root.String(), 1)
	if err == nil && len(got.Items) == 0 {
		t.Fatal("empty product without error")
	}
	t.Logf("product items=%d err=%v", len(got.Items), err)
	item := Item{URL: shopeeImage, Filename: "product.jpg", RequireImage: true, Headers: map[string]string{"Referer": "https://shopee.sg/"}}
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("download public CDN image: %v", err)
	}
	f, err := os.Open(filepath.Join(dir, item.Filename))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	im, format, err := image.DecodeConfig(f)
	if err != nil || im.Width == 0 || im.Height == 0 {
		t.Fatalf("image=%+v format=%q err=%v", im, format, err)
	}
	t.Logf("public product image decoded=%dx%d format=%s", im.Width, im.Height, format)
}
