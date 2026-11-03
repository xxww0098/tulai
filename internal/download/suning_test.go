package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
)

const suningPublicProduct = "https://product.suning.com/0000000000/12420560921.html"
const suningPublicVariants = "https://product.suning.com/0000000000/12448680801.html"
const suningSKU1 = "000000012423559757"
const suningSKU2 = "000000012423559756"
const suningFixture = `<html><head><title>Product</title><link rel="canonical" href="//product.suning.com/0000000000/12420560921.html"></head><body>
<input id="ga_itemDataBean_itemID" value="12420560921"><input id="curPartNumber" value="000000012420560921"><input id="ga_itemDataBean_description_name" value="商品名称">
<img src="https://imgservice.suning.cn/uimg1/b2c/image/avatar.jpg">
<div class="imgzoom-thumb-main"><img src-large="//imgservice.suning.cn/uimg1/b2c/image/main.jpg_800w_800h_4e"></div>
<ul><li class="clr-item" sku="000000012423559757"><a><img src=""></a></li><li class="clr-item" sku="000000012423559756"><a><img src=""></a></li></ul>
<div id="productDetail"><img src="//uimgproxy.suning.cn/uimg1/sop/commodity/banner.jpg"><img src2="//uimgproxy.suning.cn/uimg1/sop/commodity/detail.jpg"></div>
<img src="https://imgservice.suning.cn/uimg1/b2c/image/recommendation.jpg"></body></html>`

func suningTestResponse(raw string, status int, contentType, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func suningTestJSONP(codes []string, vendor string) string {
	type picture struct {
		Domain string `json:"domain"`
		Path   string `json:"pictureUrl"`
	}
	type entry struct {
		Code     string    `json:"cmmdtyCode"`
		Supplier string    `json:"supplierCode"`
		Pictures []picture `json:"mainPictureList"`
	}
	data := struct {
		List []entry `json:"mainPictureResponseList"`
	}{}
	for _, code := range codes {
		data.List = append(data.List, entry{Code: code, Supplier: vendor, Pictures: []picture{{Domain: "//imgservice.suning.cn", Path: "/uimg1/b2c/image/" + code + ".jpg"}}})
	}
	b, _ := json.Marshal(data)
	return "clusterPicCallBack(" + string(b) + ")"
}

func TestSuningProductURLs(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{suningPublicProduct, true},
		{"https://product.suning.com/0071269198/12124304712.html?x=1", true},
		{"https://product.suning.com/0000000000/000000012420560921.html", true},
		{"https://m.suning.com/product/0000000000/12420560921.html", false},
		{"https://product.suning.com/0000000000/0.html", false},
		{"https://product.suning.com/0000/12420560921.html", false},
		{"https://product.suning.com.evil.test/0000000000/12420560921.html", false},
		{"https://user@product.suning.com/0000000000/12420560921.html", false},
		{"https://product.suning.com:8443/0000000000/12420560921.html", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if isSuningProduct(u) != tc.ok {
			t.Errorf("isSuningProduct(%q)=%v", tc.raw, isSuningProduct(u))
		}
	}
}

func TestSuningImageBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://imgservice.suning.cn.evil.test/uimg1/b2c/image/a.jpg",
		"https://user@imgservice.suning.cn/uimg1/b2c/image/a.jpg",
		"https://imgservice.suning.cn:8443/uimg1/b2c/image/a.jpg",
		"http://imgservice.suning.cn/uimg1/b2c/image/a.jpg",
		"https://127.0.0.1/uimg1/b2c/image/a.jpg",
		"https://imgservice.suning.cn/uimg1/b2c/image/../a.jpg",
		"https://imgservice.suning.cn/private/a.jpg",
		"https://uimgproxy.suning.cn/uimg1/b2c/image/a.jpg",
	} {
		if _, ok := suningImageURL(raw, true); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	image, ok := suningImageURL("//imgservice.suning.cn/uimg1/b2c/image/a.jpg_60w_60h_4e", true)
	if !ok || image != "https://imgservice.suning.cn/uimg1/b2c/image/a.jpg" {
		t.Fatalf("original image=%q ok=%v", image, ok)
	}
}

func TestSuningProductAndVariants(t *testing.T) {
	root, _ := url.Parse(suningPublicProduct + "?token=synthetic")
	var calls int
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		if strings.HasPrefix(raw, "https://product.suning.com/pds-web/ajax/batchMainPicutures/") {
			if strings.Contains(raw, "token=synthetic") || !strings.Contains(raw, "0000000000-"+suningSKU1+"_0000000000-"+suningSKU2) {
				t.Fatalf("unsafe request %q", raw)
			}
			req, _ := http.NewRequest(http.MethodGet, raw, nil)
			prepare(req)
			if req.Header.Get("Referer") != "https://product.suning.com/" {
				t.Fatalf("referer=%q", req.Header.Get("Referer"))
			}
			return suningTestResponse(raw, http.StatusOK, "application/json", suningTestJSONP([]string{suningSKU1, suningSKU2}, "0000000000")), nil
		}
		return suningTestResponse(raw, http.StatusOK, "text/html", suningFixture), nil
	})
	got, err := discoverSuningProduct(context.Background(), getter, root)
	if err != nil || got.Title != "商品名称" || len(got.Items) != 4 || calls != 2 {
		t.Fatalf("items=%+v title=%q calls=%d err=%v", got.Items, got.Title, calls, err)
	}
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.Metadata["suning_section"]]++
		if !item.RequireImage || !item.PublicOnly || item.Metadata["suning_product_id"] != "12420560921" || item.Metadata["suning_vendor_id"] != "0000000000" || item.Headers["Referer"] != "https://product.suning.com/" || strings.Contains(item.URL, "banner") || strings.Contains(item.URL, "recommendation") {
			t.Fatalf("unexpected item %+v", item)
		}
	}
	if counts["主图"] != 1 || counts["规格图"] != 2 || counts["描述图"] != 1 {
		t.Fatalf("section counts=%+v", counts)
	}
}

func TestSuningPageIdentity(t *testing.T) {
	root, _ := url.Parse(suningPublicProduct)
	for _, body := range []string{
		strings.Replace(suningFixture, `value="12420560921"`, `value="12420560922"`, 1),
		strings.Replace(suningFixture, `/0000000000/12420560921.html`, `/0000000000/12420560922.html`, 1),
		strings.Replace(suningFixture, `/0000000000/12420560921.html`, `/0071269198/12420560921.html`, 1),
		strings.Replace(suningFixture, `<link rel="canonical" href="//product.suning.com/0000000000/12420560921.html">`, ``, 1),
	} {
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			if strings.Contains(raw, "/pds-web/") {
				t.Fatal("unexpected request after invalid identity")
			}
			return suningTestResponse(raw, http.StatusOK, "text/html", body), nil
		})
		if got, err := discoverSuningProduct(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
			t.Fatalf("invalid identity items=%+v err=%v", got.Items, err)
		}
	}
}

func TestSuningSKUBatchIdentityAndFormat(t *testing.T) {
	page, _ := url.Parse(suningPublicProduct)
	ref := suningProductRef(page)
	for _, body := range []string{
		suningTestJSONP([]string{suningSKU1}, "0071269198"),
		suningTestJSONP([]string{suningSKU2}, "0000000000"),
		`otherCallback({"mainPictureResponseList":[]})`,
		`clusterPicCallBack({"mainPictureResponseList":[]});alert(1)`,
		strings.Replace(suningTestJSONP([]string{suningSKU1}, "0000000000"), "imgservice.suning.cn", "evil.test", 1),
	} {
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			return suningTestResponse(raw, http.StatusOK, "application/json", body), nil
		})
		if images, err := suningSKUBatch(context.Background(), getter, page, ref, []string{suningSKU1}); err == nil || len(images) != 0 {
			t.Fatalf("invalid data images=%+v err=%v", images, err)
		}
	}
}

func TestSuningPartialBatchFailure(t *testing.T) {
	root, _ := url.Parse(suningPublicProduct)
	var codeList []string
	var nodes strings.Builder
	for i := 1; i <= 21; i++ {
		code := fmt.Sprintf("%018d", i)
		codeList = append(codeList, code)
		fmt.Fprintf(&nodes, `<li class="clr-item" sku="%s"><img src=""></li>`, code)
	}
	body := strings.Replace(suningFixture, `<li class="clr-item" sku="`+suningSKU1+`"><a><img src=""></a></li><li class="clr-item" sku="`+suningSKU2+`"><a><img src=""></a></li>`, nodes.String(), 1)
	batch := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if strings.Contains(raw, "/pds-web/") {
			batch++
			if batch == 1 {
				return suningTestResponse(raw, http.StatusOK, "application/json", suningTestJSONP(codeList[:20], "0000000000")), nil
			}
			return suningTestResponse(raw, http.StatusServiceUnavailable, "text/html", ""), nil
		}
		return suningTestResponse(raw, http.StatusOK, "text/html", body), nil
	})
	got, err := discoverSuningProduct(context.Background(), getter, root)
	if err == nil || len(got.Items) != 22 || batch != 2 {
		t.Fatalf("partial items=%d batches=%d err=%v", len(got.Items), batch, err)
	}
}

func TestSuningCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(suningPublicProduct)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverSuningProduct(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := suningTestResponse(raw, http.StatusOK, "text/html", suningFixture)
		resp.FinalURL = suningPublicVariants
		return resp, nil
	})
	if got, err := discoverSuningProduct(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect items=%+v err=%v", got.Items, err)
	}
}

type suningRoundTripper func(*http.Request) (*http.Response, error)

func (fn suningRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestSuningRenderedCapture(t *testing.T) {
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
	const target = suningPublicProduct + "?token=synthetic"
	var calls atomic.Int32
	session.getter.client.Transport = suningRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if !strings.HasPrefix(r.URL.String(), "https://product.suning.com/pds-web/ajax/batchMainPicutures/") || strings.Contains(r.URL.String(), "token=synthetic") {
			return nil, errors.New("unexpected request or query leak")
		}
		body := suningTestJSONP([]string{suningSKU1, suningSKU2}, "0000000000")
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
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
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(suningFixture)))
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
	if err != nil || len(got.Items) != 4 || calls.Load() != 1 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestSuningLiveProductsDownload(t *testing.T) {
	if os.Getenv("TULAI_SUNING_LIVE") != "1" {
		t.Skip("set TULAI_SUNING_LIVE=1 to verify public products")
	}
	for _, tc := range []struct {
		name     string
		raw      string
		sections []string
		minimum  int
	}{
		{"ordinary", suningPublicProduct, []string{"主图"}, 10},
		{"variants", suningPublicVariants, []string{"规格图", "描述图"}, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			getter := NewHTTPGetter("")
			got, err := Discover(ctx, getter, tc.raw, 1)
			if err != nil || len(got.Items) < tc.minimum {
				t.Fatalf("public product items=%d err=%v", len(got.Items), err)
			}
			counts := map[string]int{}
			for i := range got.Items {
				counts[got.Items[i].Metadata["suning_section"]]++
			}
			for _, section := range tc.sections {
				var chosen *Item
				for i := range got.Items {
					if got.Items[i].Metadata["suning_section"] == section {
						chosen = &got.Items[i]
						break
					}
				}
				if chosen == nil {
					t.Fatalf("no %s; counts=%+v", section, counts)
				}
				item := *chosen
				item.Filename = "product"
				dir := t.TempDir()
				if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
					t.Fatalf("download %s: %v", section, err)
				}
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 1 {
					t.Fatalf("files=%v err=%v", entries, err)
				}
				f, err := os.Open(filepath.Join(dir, entries[0].Name()))
				if err != nil {
					t.Fatal(err)
				}
				im, format, err := image.DecodeConfig(f)
				f.Close()
				if err != nil || im.Width < 2 || im.Height < 2 {
					t.Fatalf("image=%+v format=%q err=%v", im, format, err)
				}
				t.Logf("public product sections=%+v downloaded %s %dx%d %s", counts, section, im.Width, im.Height, format)
			}
		})
	}
}

func TestSuningRenderedLiveSKU(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" || os.Getenv("TULAI_SUNING_LIVE") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 and TULAI_SUNING_LIVE=1 for isolated Chrome and live SKU API")
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
	fixture := strings.ReplaceAll(suningFixture, "12420560921", "12448680801")
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == suningPublicVariants {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(fixture)))
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
	start, _ := url.Parse(suningPublicVariants)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(suningPublicVariants)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
	if err != nil && strings.Contains(err.Error(), "网页图片不能访问其他内网地址") {
		if len(got.Items) != 2 {
			t.Fatalf("DNS rejection lost main or detail images: %d", len(got.Items))
		}
		t.Skipf("live SKU endpoint blocked by rendered DNS boundary: %v", err)
	}
	if err != nil || len(got.Items) != 4 {
		t.Fatalf("rendered live SKU images=%d err=%v", len(got.Items), err)
	}
	t.Logf("rendered live SKU images=%d", len(got.Items))
}
