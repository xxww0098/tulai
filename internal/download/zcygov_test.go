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
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const zcygovTestPage = "https://www.zcygov.cn/items/1399825342223769?skuId=1399825342219143"
const zcygovTestMain = "https://itemcdn.zcycdn.com/3001764231/afb402e2-f442-4ca3-88e6-9380efb5055f.png"
const zcygovTestSKU = "https://itemcdn.zcycdn.com/3001764231/7d90ee46-21f1-41d7-a6e5-717ec727364c.png"
const zcygovTestDesc = "https://sitecdn.zcycdn.com/1062CM/439900/10008214518/20226/954e42bd-5166-45e4-8b11-2332dc2abb79"

func zcygovTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func zcygovTestState() zcygovState {
	return zcygovState{Location: zcygovTestPage, ProductID: "1399825342223769", Title: "商品",
		MainReady: true, VariantReady: true, DescriptionReady: true, Main: []string{zcygovTestMain},
		Variant: []zcygovVariant{{URL: zcygovTestSKU, Value: "白色"}}, Description: []string{zcygovTestDesc}}
}

func TestZcygovRouteAndImage(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{zcygovTestPage, "1399825342223769"},
		{"https://mall.anhui.zcygov.cn/items/1453032354797322?searchType=1", "1453032354797322"},
		{"https://www.zcygov.cn/items/0", ""},
		{"https://www.zcygov.cn/items/123/extra", ""},
		{"https://www.zcygov.cn/items/%31%32%33", ""},
		{"https://www.zcygov.cn.evil.test/items/123", ""},
		{"https://user@www.zcygov.cn/items/123", ""},
		{"https://www.zcygov.cn:8443/items/123", ""},
	} {
		if got := zcygovID(zcygovTestURL(t, tc.raw)); got != tc.id {
			t.Errorf("route %s: got %q want %q", tc.raw, got, tc.id)
		}
	}
	page := zcygovTestURL(t, zcygovTestPage)
	for _, tc := range []struct{ raw, want string }{
		{zcygovTestMain, zcygovTestMain},
		{zcygovTestDesc, zcygovTestDesc},
		{zcygovTestDesc + "?x-oss-process=image/quality,Q_80", zcygovTestDesc + "?x-oss-process=image/quality,Q_80"},
		{"https://itemcdn.zcycdn.com.evil.test/3001764231/a.png", ""},
		{"https://user@itemcdn.zcycdn.com/3001764231/a.png", ""},
		{"https://itemcdn.zcycdn.com:8443/3001764231/a.png", ""},
		{"http://itemcdn.zcycdn.com/3001764231/a.png", ""},
		{"https://itemcdn.zcycdn.com/3001764231/../a.png", ""},
		{"https://itemcdn.zcycdn.com/3001764231/video.mp4", ""},
		{"https://sitecdn.zcycdn.com/zcy-front-other-upload/logo.png", ""},
		{"https://sitecdn.zcycdn.com/1062CM/439900/10008214518/20226/video.mp4", ""},
	} {
		if got := zcygovImage(page, tc.raw); got != tc.want {
			t.Errorf("image %s: got %q want %q", tc.raw, got, tc.want)
		}
	}
}

func TestZcygovCollectIdentityAndPartial(t *testing.T) {
	page := zcygovTestURL(t, zcygovTestPage)
	got, err := zcygovCollectState(context.Background(), page, zcygovTestState())
	if err != nil || len(got.Items) != 3 || got.Items[0].Metadata["image_kind"] != "main" || got.Items[1].Metadata["sku_value"] != "白色" || got.Items[2].Metadata["image_kind"] != "description" {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
	for _, item := range got.Items {
		if !item.PublicOnly || !item.RequireImage || item.Metadata["zcygov_product_id"] != "1399825342223769" || item.Headers["Referer"] != "https://www.zcygov.cn/" {
			t.Fatalf("unsafe item %+v", item)
		}
	}
	for _, tc := range []struct {
		name    string
		edit    func(*zcygovState)
		want    int
		partial bool
	}{
		{"wrong product", func(s *zcygovState) { s.ProductID = "123" }, 0, false},
		{"wrong page", func(s *zcygovState) { s.Location = "https://www.zcygov.cn/items/123" }, 0, false},
		{"different host", func(s *zcygovState) { s.Location = "https://mall.anhui.zcygov.cn/items/1399825342223769" }, 0, false},
		{"main not ready", func(s *zcygovState) { s.MainReady = false }, 2, true},
		{"sku not ready", func(s *zcygovState) { s.VariantReady = false }, 2, true},
		{"description not ready", func(s *zcygovState) { s.DescriptionReady = false }, 2, true},
		{"unknown CDN", func(s *zcygovState) { s.Description = append(s.Description, "https://evil.test/recommendation.jpg") }, 3, true},
		{"truncated", func(s *zcygovState) { s.Truncated = true }, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := zcygovTestState()
			tc.edit(&state)
			got, err := zcygovCollectState(context.Background(), page, state)
			if err == nil || len(got.Items) != tc.want || (tc.partial && !strings.Contains(err.Error(), "不完整")) {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
	state := zcygovTestState()
	state.Description = append(state.Description, zcygovTestMain)
	got, err = zcygovCollectState(context.Background(), page, state)
	if err != nil || len(got.Items) != 3 {
		t.Fatalf("duplicate items=%+v err=%v", got.Items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = zcygovCollectState(ctx, page, zcygovTestState())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestZcygovDirectMode(t *testing.T) {
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("must not query undocumented APIs")
		return nil, nil
	})
	got, err := discoverZcygov(context.Background(), getter, zcygovTestURL(t, zcygovTestPage))
	if !got.IsPage || err == nil || !strings.Contains(err.Error(), "浏览器") {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

// This opt-in test downloads two images observed on the public product page.
func TestZcygovPublicImages(t *testing.T) {
	if os.Getenv("TULAI_ZCYGOV_CDN_LIVE") != "1" {
		t.Skip("set TULAI_ZCYGOV_CDN_LIVE=1 to download public sample images")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := zcygovCollectState(ctx, zcygovTestURL(t, zcygovTestPage), zcygovTestState())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []Item{got.Items[0], got.Items[2]} {
		dir := t.TempDir()
		if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatal(err)
		}
		files, err := os.ReadDir(dir)
		if err != nil || len(files) != 1 {
			t.Fatalf("files=%v err=%v", files, err)
		}
		f, err := os.Open(filepath.Join(dir, files[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		cfg, _, decodeErr := image.DecodeConfig(f)
		f.Close()
		if decodeErr != nil || cfg.Width == 0 || cfg.Height == 0 {
			t.Fatalf("image decode: %v", decodeErr)
		}
	}
}

type zcygovRoundTrip func(*http.Request) (*http.Response, error)

func (f zcygovRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func zcygovFixture() string {
	return `<html><body><div id="product"><h1>商品</h1>` +
		`<div class="img-thumb-change"><img data-src="` + zcygovTestMain + `?x-oss-process=image/resize,l_124"></div>` +
		`<div class="sku"><span class="img-box-image"><img data-src="` + zcygovTestSKU + `?x-oss-process=image/resize,l_76"></span></div>` +
		`<div class="item-tab-detail-display"><img data-src="` + zcygovTestDesc + `"></div></div>` +
		`<img src="https://itemcdn.zcycdn.com/3001764231/recommendation.jpg"><script>` +
		`const main='` + zcygovTestMain + `',sku='` + zcygovTestSKU + `'; const item={item:{id:'1399825342223769',name:'商品'},imageInfos:[{url:main}],itemDetail:{detail:'介绍'}};` +
		`const store={state:{item:{item}}};document.querySelector('.img-thumb-change').__vue__={$store:store,imageInfos:item.imageInfos};` +
		`document.querySelector('.sku').__vue__={$store:store,groupedSkuAttrs:[{skuAttributes:[{image:sku,attrVal:'白色'}]}]};` +
		`document.querySelector('.item-tab-detail-display').__vue__={$store:store};</script></body></html>`
}

func TestZcygovRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Chrome integration")
	}
	for _, name := range []string{"complete", "wrong-product", "foreign-main", "video", "hidden-sku"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `<html></html>`) }))
			defer server.Close()
			session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			body := zcygovFixture()
			switch name {
			case "wrong-product":
				body = strings.Replace(body, `id:'1399825342223769'`, `id:'123'`, 1)
			case "foreign-main":
				body = strings.Replace(body, `data-src="`+zcygovTestMain, `data-src="https://itemcdn.zcycdn.com/3001764231/other.png`, 1)
			case "video":
				body = strings.Replace(body, `imageInfos:[{url:main}]`, `imageInfos:[{url:main,mp4Url:'video.mp4'}]`, 1)
			case "hidden-sku":
				body = strings.Replace(body, `class="sku"`, `class="sku" style="display:none"`, 1)
			}
			interceptErrors := make(chan error, 8)
			camoufox.ListenTarget(session.tab, func(event any) {
				paused, ok := event.(*fetch.EventRequestPaused)
				if !ok {
					return
				}
				go func() {
					var action camoufox.Action
					if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == zcygovTestPage {
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
			session.start = zcygovTestURL(t, zcygovTestPage)
			if err := session.run(ctx, camoufox.Navigate(zcygovTestPage)); err != nil {
				t.Fatal(err)
			}
			if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
				return network.SetCookie("synthetic_session", "example").WithDomain(".zcygov.cn").WithURL("https://www.zcygov.cn/").WithHTTPOnly(true).Do(ec)
			})); err != nil {
				t.Fatal(err)
			}
			calls := 0
			session.getter.client.Transport = zcygovRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "itemcdn.zcycdn.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.zcygov.cn/" {
					t.Errorf("unsafe request %s %v", r.URL, r.Header)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic")), Request: r}, nil
			})
			got, err := session.Capture(ctx)
			switch name {
			case "wrong-product":
				if err == nil || len(got.Items) != 0 {
					t.Fatalf("wrong identity: %+v err=%v", got.Items, err)
				}
			case "foreign-main", "video", "hidden-sku":
				if err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "不完整") {
					t.Fatalf("partial %s: %+v err=%v", name, got.Items, err)
				}
			default:
				if err != nil || len(got.Items) != 3 || session.PageURL() != zcygovTestPage {
					t.Fatalf("got=%+v page=%s err=%v", got.Items, session.PageURL(), err)
				}
				resp, err := session.Getter().Get(ctx, got.Items[0].URL, func(r *http.Request) {
					for k, v := range got.Items[0].Headers {
						r.Header.Set(k, v)
					}
				})
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if calls != 1 {
					t.Fatalf("CDN calls=%d", calls)
				}
			}
			select {
			case err := <-interceptErrors:
				t.Fatalf("interception: %v", err)
			default:
			}
		})
	}
}
