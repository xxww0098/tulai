package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const ymatouTestPage = "https://m.ymatou.com/shop/504443195/item?productId=p14592432"
const ymatouTestMain = "http://pic1.yangmatou.com/G03/shangou/M00/DC/3D/CgzUH2B6aQmAQt1zAAN8V2_3HUk413_1_1_n_w_o.jpg/webp800"
const ymatouTestDesc = "http://pic2.yangmatou.com/2022/12/13/23/09acdcf2-3838-45b8-9052-886e2ff76c29_75_98_o.jpg/webp800"

func ymatouTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func ymatouTestState() ymatouState {
	return ymatouState{Location: ymatouTestPage, ProductID: "p14592432", DetailID: "p14592432", DescriptionID: "p14592432",
		Title: "商品", MainReady: true, VariantReady: true, DescriptionReady: true,
		Main: []string{ymatouTestMain}, Variant: []ymatouVariant{{URL: ymatouTestDesc, Value: "蓝色"}},
		Description: []string{"https://pic1.yangmatou.com/G03/shangou/test/description.jpg/webp800"}}
}

func TestYmatouRoutes(t *testing.T) {
	for _, tc := range []struct{ raw, kind, id string }{
		{ymatouTestPage, "shop", "p14592432"},
		{"https://m.ymatou.com/item/p14592432", "legacy", "p14592432"},
		{"https://m.ymatou.com/shop/0/item?productId=p1", "", ""},
		{"https://m.ymatou.com/shop/504443195/item?productId=p0", "", ""},
		{"https://m.ymatou.com/shop/504443195/item?productId=p1&productId=p2", "", ""},
		{"https://m.ymatou.com/shop/504443195/item?productId=bad", "", ""},
		{"https://m.ymatou.com.evil.test/shop/504443195/item?productId=p1", "", ""},
		{"https://user@m.ymatou.com/shop/504443195/item?productId=p1", "", ""},
		{"https://m.ymatou.com:8443/shop/504443195/item?productId=p1", "", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			kind, id := ymatouRoute(ymatouTestURL(t, tc.raw))
			if kind != tc.kind || id != tc.id {
				t.Fatalf("route %q %q, want %q %q", kind, id, tc.kind, tc.id)
			}
		})
	}
}

func TestYmatouImageHostAndPath(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{ymatouTestMain, strings.Replace(ymatouTestMain, "http://", "https://", 1)},
		{ymatouTestDesc, strings.Replace(ymatouTestDesc, "http://", "https://", 1)},
		{"https://pic1.yangmatou.com/G03/shangou/image.png", "https://pic1.yangmatou.com/G03/shangou/image.png"},
		{"https://pic1.yangmatou.com.evil.test/a/image.jpg/webp800", ""},
		{"https://user@pic1.yangmatou.com/a/image.jpg", ""},
		{"https://pic1.yangmatou.com:444/a/image.jpg", ""},
		{"http://pic1.yangmatou.com:80/a/image.jpg", ""},
		{"https://pic1.yangmatou.com/a/../image.jpg", ""},
		{"https://pic1.yangmatou.com/a/image.jpg/../../other", ""},
		{"https://pic1.yangmatou.com/a/file.html/webp800", ""},
		{"https://pic1.yangmatou.com/a/file.jpg/webp0", ""},
	} {
		if got := ymatouImage(tc.raw); got != tc.want {
			t.Errorf("image %q got=%q want=%q", tc.raw, got, tc.want)
		}
	}
}

func TestYmatouCollectIdentityAndKinds(t *testing.T) {
	page := ymatouTestURL(t, ymatouTestPage)
	got, err := ymatouCollectState(context.Background(), page, ymatouTestState())
	if err != nil || len(got.Items) != 3 || got.Items[0].Metadata["image_kind"] != "main" ||
		got.Items[1].Metadata["image_kind"] != "sku" || got.Items[1].Metadata["sku_value"] != "蓝色" || got.Items[2].Metadata["image_kind"] != "description" {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Metadata["ymatou_product_id"] != "p14592432" || !strings.HasPrefix(item.URL, "https://") || item.Headers["Referer"] != "https://m.ymatou.com/" {
			t.Fatalf("item=%+v", item)
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*ymatouState)
	}{
		{"product changed", func(s *ymatouState) { s.ProductID = "p999" }},
		{"main changed", func(s *ymatouState) { s.DetailID = "p999" }},
		{"location changed", func(s *ymatouState) { s.Location = "https://m.ymatou.com/shop/504443195/item?productId=p999" }},
		{"origin changed", func(s *ymatouState) { s.Location = "https://other.test/shop/504443195/item?productId=p14592432" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := ymatouTestState()
			tc.edit(&state)
			got, err := ymatouCollectState(context.Background(), page, state)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("accepted mismatch: %+v %v", got, err)
			}
		})
	}
}

func TestYmatouPartialAndCancellation(t *testing.T) {
	page := ymatouTestURL(t, ymatouTestPage)
	for _, tc := range []struct {
		name string
		edit func(*ymatouState)
		want int
	}{
		{"description pending", func(s *ymatouState) { s.DescriptionReady = false }, 2},
		{"description changed", func(s *ymatouState) { s.DescriptionID = "p999" }, 2},
		{"sku pending", func(s *ymatouState) { s.VariantReady = false }, 3},
		{"bad host", func(s *ymatouState) { s.Main = append(s.Main, "https://evil.test/product.jpg") }, 3},
		{"truncated", func(s *ymatouState) { s.Truncated = true }, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := ymatouTestState()
			tc.edit(&state)
			got, err := ymatouCollectState(context.Background(), page, state)
			if err == nil || len(got.Items) != tc.want {
				t.Fatalf("partial=%+v err=%v", got.Items, err)
			}
		})
	}
	state := ymatouTestState()
	state.Variant = append(state.Variant, ymatouVariant{URL: state.Main[0]})
	got, err := ymatouCollectState(context.Background(), page, state)
	if err != nil || len(got.Items) != 3 {
		t.Fatalf("duplicate=%+v err=%v", got.Items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = ymatouCollectState(ctx, page, ymatouTestState())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestYmatouDirectMode(t *testing.T) {
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("direct mode must not fabricate signed API requests")
		return nil, nil
	})
	got, err := discoverYmatou(context.Background(), getter, ymatouTestURL(t, ymatouTestPage))
	if !got.IsPage || err == nil || !strings.Contains(err.Error(), "下载浏览器") {
		t.Fatalf("direct=%+v err=%v", got, err)
	}
}

func TestYmatouPublicBrowserSample(t *testing.T) {
	root := os.Getenv("TULAI_YMATOU_SAMPLES")
	if root == "" {
		t.Skip("set TULAI_YMATOU_SAMPLES to public anonymous browser captures")
	}
	read := func(name string, out any) {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatal(err)
		}
	}
	var details struct {
		ID, Title string
		Pics      []string
		Skus      []struct {
			Properties []struct{ OriginalPic, Value string }
		}
	}
	var description struct {
		ProductID  string
		ModuleList []struct {
			PicList          []string
			MixedContentList []struct{ Type, Value string }
		}
	}
	read("browser-detail.json", &details)
	read("browser-desc.json", &description)
	state := ymatouState{Location: ymatouTestPage, ProductID: details.ID, DetailID: details.ID, Title: details.Title,
		DescriptionID: description.ProductID, MainReady: true, VariantReady: true, DescriptionReady: true, Main: details.Pics}
	for _, sku := range details.Skus {
		for _, p := range sku.Properties {
			if p.OriginalPic != "" {
				state.Variant = append(state.Variant, ymatouVariant{URL: p.OriginalPic, Value: p.Value})
			}
		}
	}
	for _, m := range description.ModuleList {
		state.Description = append(state.Description, m.PicList...)
		for _, item := range m.MixedContentList {
			if item.Type == "img" {
				state.Description = append(state.Description, item.Value)
			}
		}
	}
	got, err := ymatouCollectState(context.Background(), ymatouTestURL(t, ymatouTestPage), state)
	if err != nil || len(got.Items) != 9 {
		t.Fatalf("real sample=%+v err=%v", got.Items, err)
	}
	if os.Getenv("TULAI_YMATOU_LIVE") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	for _, item := range []Item{got.Items[0], got.Items[len(got.Items)-1]} {
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatalf("public download: %v", err)
		}
		files, err := os.ReadDir(dir)
		if err != nil || len(files) != 1 {
			t.Fatalf("files=%v err=%v", files, err)
		}
		f, err := os.Open(filepath.Join(dir, files[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || cfg.Width == 0 || cfg.Height == 0 {
			t.Fatalf("decoded image: %v", err)
		}
	}
}

type ymatouRoundTrip func(*http.Request) (*http.Response, error)

func (f ymatouRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestYmatouRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Chrome integration")
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
	const body = `<html><body><div class="item"></div><script>
    document.querySelector('.item').__vue__={productId:'p14592432',
      productDetailInfo:{id:'p14592432',title:'商品',pics:['` + ymatouTestMain + `'],
        skus:[{properties:[{value:'蓝色',originalPic:'` + ymatouTestDesc + `'}]}]},
      detailInfo:{productId:'p14592432',moduleList:[{picList:['https://pic1.yangmatou.com/G03/shangou/test/detail.jpg/webp800']}]}};
    </script><img src="https://evil.test/recommended.jpg"></body></html>`
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == ymatouTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
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
	session.start = ymatouTestURL(t, ymatouTestPage)
	if err := session.run(ctx, camoufox.Navigate(ymatouTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".ymatou.com").WithURL("https://m.ymatou.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = ymatouRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "pic1.yangmatou.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://m.ymatou.com/" {
			t.Errorf("unsafe image request: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/webp"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || session.PageURL() != ymatouTestPage || got.Items[0].Metadata["ymatou_product_id"] != "p14592432" {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 1 {
		t.Fatalf("image calls=%d", calls)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("interception failed: %v", err)
	default:
	}
}

func TestYmatouPublicRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_YMATOU_BROWSER_LIVE") != "1" {
		t.Skip("set TULAI_YMATOU_BROWSER_LIVE=1 for isolated public browser verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), ymatouTestPage, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 9 || session.PageURL() != ymatouTestPage {
		t.Fatalf("public browser capture=%d page=%s err=%v", len(got.Items), session.PageURL(), err)
	}
	if os.Getenv("TULAI_YMATOU_BROWSER_DOWNLOAD") == "1" {
		dir := t.TempDir()
		if err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: dir}, got.Items[0], nil); err != nil {
			t.Fatalf("browser getter public image download: %v", err)
		}
		files, err := os.ReadDir(dir)
		if err != nil || len(files) != 1 {
			t.Fatalf("browser downloaded files=%v err=%v", files, err)
		}
	}
}

func TestYmatouDiscoverRoute(t *testing.T) {
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("must not fabricate API requests")
		return nil, nil
	})
	got, err := Discover(context.Background(), getter, ymatouTestPage, 1)
	if !got.IsPage || err == nil || !strings.Contains(err.Error(), "下载浏览器") {
		t.Fatalf("discover=%+v err=%v", got, err)
	}
}
