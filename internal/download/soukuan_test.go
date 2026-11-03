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

const soukuanTestPage = "https://www.vvic.com/item/621a49842d8d110006f94e0a"
const soukuanTestImage = "https://img1.vvic.com/upload/1645889855816_777346.jpg"
const soukuanTestColor = "https://img1.vvic.com/upload/1645889859357_773982.jpg"
const soukuanTestDesc = "https://img1.vvic.com/upload/1645889920578_66705.jpg"

func soukuanTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func soukuanTestState() soukuanState {
	return soukuanState{Location: soukuanTestPage, ProductID: "621a49842d8d110006f94e0a", RouteID: "621a49842d8d110006f94e0a",
		Title: "商品", ProtectionKnown: true, MainReady: true, VariantReady: true, DescReady: true,
		Main: []string{soukuanTestImage}, Variant: []soukuanVariant{{URL: soukuanTestColor, Value: "白色"}}, Description: []string{soukuanTestDesc}}
}

func TestSoukuanRoutesAndImageHosts(t *testing.T) {
	for _, tc := range []struct{ raw, kind, id string }{
		{soukuanTestPage, "item", "621a49842d8d110006f94e0a"},
		{"https://www.vvic.com/item/13778639", "item", "13778639"},
		{"https://www.vvic.com/en/item/6610c52e21e14a0008db53f8", "unsupported", "6610c52e21e14a0008db53f8"},
		{"https://www.vvic.com/item/0", "", ""},
		{"https://www.vvic.com/item/not-an-id", "", ""},
		{"https://www.vvic.com/item/13778639/extra", "", ""},
		{"https://www.vvic.com.evil.test/item/13778639", "", ""},
		{"https://user@www.vvic.com/item/13778639", "", ""},
		{"https://www.vvic.com:444/item/13778639", "", ""},
	} {
		kind, id := soukuanRoute(soukuanTestURL(t, tc.raw))
		if kind != tc.kind || id != tc.id {
			t.Errorf("route %s = %s/%s want %s/%s", tc.raw, kind, id, tc.kind, tc.id)
		}
	}
	page := soukuanTestURL(t, soukuanTestPage)
	for _, tc := range []struct{ raw, want string }{
		{"//img1.vvic.com/upload/1645889855816_777346.jpg", soukuanTestImage},
		{"https://img.vvic.com/1552794991800_609185.jpg?x-oss-process=image/resize,w_400", "https://img.vvic.com/1552794991800_609185.jpg?x-oss-process=image/resize,w_400"},
		{"https://img.alicdn.com/imgextra/i2/1/example.jpg", "https://img.alicdn.com/imgextra/i2/1/example.jpg"},
		{"https://img1.vvic.com.evil.test/upload/123.jpg", ""},
		{"https://user@img1.vvic.com/upload/123.jpg", ""},
		{"https://img1.vvic.com:444/upload/123.jpg", ""},
		{"http://img1.vvic.com/upload/123.jpg", ""},
		{"https://img1.vvic.com/upload/../123.jpg", ""},
		{"https://img1.vvic.com/upload/video.mp4", ""},
		{"https://img.alicdn.com/other/123.jpg", ""},
	} {
		if got := soukuanImage(page, tc.raw); got != tc.want {
			t.Errorf("image %s got=%s want=%s", tc.raw, got, tc.want)
		}
	}
}

func TestSoukuanCollectIdentityAndProtection(t *testing.T) {
	page := soukuanTestURL(t, soukuanTestPage)
	got, err := soukuanCollectState(context.Background(), page, soukuanTestState())
	if err != nil || len(got.Items) != 3 || got.Items[0].Metadata["image_kind"] != "main" || got.Items[1].Metadata["sku_value"] != "白色" || got.Items[2].Metadata["image_kind"] != "description" {
		t.Fatalf("images=%+v err=%v", got.Items, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Metadata["soukuan_product_id"] != "621a49842d8d110006f94e0a" || item.Headers["Referer"] != "https://www.vvic.com/" {
			t.Fatalf("unsafe item %+v", item)
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*soukuanState)
	}{
		{"wrong item", func(s *soukuanState) { s.ProductID = "6032a0a5c472530001c53990" }},
		{"wrong route", func(s *soukuanState) { s.RouteID = "6032a0a5c472530001c53990" }},
		{"wrong page", func(s *soukuanState) { s.Location = "https://www.vvic.com/item/6032a0a5c472530001c53990" }},
		{"unknown protection", func(s *soukuanState) { s.ProtectionKnown = false }},
		{"protected", func(s *soukuanState) { s.Protected = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := soukuanTestState()
			tc.edit(&state)
			got, err := soukuanCollectState(context.Background(), page, state)
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("accepted unavailable or protected data: %+v %v", got, err)
			}
		})
	}
}

func TestSoukuanPartialAndCancellation(t *testing.T) {
	page := soukuanTestURL(t, soukuanTestPage)
	for _, tc := range []struct {
		name string
		edit func(*soukuanState)
		want int
	}{
		{"missing main", func(s *soukuanState) { s.MainIncomplete = true }, 3},
		{"main not ready", func(s *soukuanState) { s.MainReady = false }, 2},
		{"missing SKU", func(s *soukuanState) { s.SKUIncomplete = true }, 3},
		{"SKU not ready", func(s *soukuanState) { s.VariantReady = false }, 2},
		{"missing description", func(s *soukuanState) { s.DescReady = false }, 2},
		{"hidden description", func(s *soukuanState) { s.DescIncomplete = true }, 3},
		{"bad CDN", func(s *soukuanState) { s.Main = append(s.Main, "https://evil.test/a.jpg") }, 3},
		{"truncated", func(s *soukuanState) { s.Truncated = true }, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := soukuanTestState()
			tc.edit(&state)
			got, err := soukuanCollectState(context.Background(), page, state)
			if err == nil || len(got.Items) != tc.want {
				t.Fatalf("partial=%+v err=%v", got.Items, err)
			}
		})
	}
	state := soukuanTestState()
	state.Description = append(state.Description, soukuanTestImage)
	got, err := soukuanCollectState(context.Background(), page, state)
	if err != nil || len(got.Items) != 3 {
		t.Fatalf("duplicates=%+v err=%v", got.Items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = soukuanCollectState(ctx, page, soukuanTestState())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestSoukuanDirectMode(t *testing.T) {
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("must not request unverified APIs")
		return nil, nil
	})
	got, err := discoverSoukuan(context.Background(), getter, soukuanTestURL(t, soukuanTestPage))
	if !got.IsPage || err == nil || !strings.Contains(err.Error(), "下载浏览器") {
		t.Fatalf("discover=%+v err=%v", got, err)
	}
}

func TestSoukuanPublicHistoricalImage(t *testing.T) {
	if os.Getenv("TULAI_SOUKUAN_CDN_LIVE") != "1" {
		t.Skip("set TULAI_SOUKUAN_CDN_LIVE=1 for the independently published historical image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := soukuanCollectState(ctx, soukuanTestURL(t, soukuanTestPage), soukuanTestState())
	if err != nil || len(got.Items) != 3 {
		t.Fatalf("sample items=%d err=%v", len(got.Items), err)
	}
	dir := t.TempDir()
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, got.Items[0], nil); err != nil {
		t.Fatalf("historical public image: %v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	f, err := os.Open(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		t.Fatalf("image decode: %v", err)
	}
}

type soukuanRoundTrip func(*http.Request) (*http.Response, error)

func (f soukuanRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func soukuanFixture(protected bool) string {
	theft := "0"
	if protected {
		theft = "1"
	}
	return `<html><body><div id="productRoot"><h1 class="detail-title">商品</h1>` +
		`<div id="thumblist"><div class="owl-item active"><div class="tb-thumb-item"><img big="` + soukuanTestImage + `"></div></div></div>` +
		`<div id="skuInfo"><div class="value color-choice"><img big="` + soukuanTestColor + `" alt="白色"></div></div>` +
		`<div class="detail-desc"><img data-src="` + soukuanTestDesc + `"></div></div>` +
		`<img src="https://img1.vvic.com/upload/recommended.jpg"><script>` +
		`document.querySelector('#productRoot').__vue__={item:{vid:'621a49842d8d110006f94e0a',isTheft:` + theft +
		`,imgs:['` + soukuanTestImage + `'],color_pics:['` + soukuanTestColor + `'],item_desc:{desc:'商品描述'}},` +
		`$route:{params:{vid:'621a49842d8d110006f94e0a'}}};</script></body></html>`
}

func TestSoukuanRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Chrome integration")
	}
	for _, tc := range []struct {
		name      string
		protected bool
	}{
		{"visible", false},
		{"protected", true},
		{"visible-mask", false},
		{"hidden-mask", false},
		{"foreign-image", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `<html></html>`) }))
			defer server.Close()
			session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			body := soukuanFixture(tc.protected)
			switch tc.name {
			case "visible-mask":
				body = strings.Replace(body, `<div id="thumblist">`, `<div class="tb-pic-theft"></div><div id="thumblist">`, 1)
			case "hidden-mask":
				body = strings.Replace(body, `<div id="thumblist">`, `<div class="tb-pic-theft" style="display:none"></div><div id="thumblist">`, 1)
			case "foreign-image":
				body = strings.Replace(body, `big="`+soukuanTestImage+`"`, `big="https://img1.vvic.com/upload/other.jpg"`, 1)
			}
			interceptErrors := make(chan error, 8)
			camoufox.ListenTarget(session.tab, func(event any) {
				paused, ok := event.(*fetch.EventRequestPaused)
				if !ok {
					return
				}
				go func() {
					var action camoufox.Action
					if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == soukuanTestPage {
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
			session.start = soukuanTestURL(t, soukuanTestPage)
			if err := session.run(ctx, camoufox.Navigate(soukuanTestPage)); err != nil {
				t.Fatal(err)
			}
			if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
				return network.SetCookie("synthetic_session", "example").WithDomain(".vvic.com").WithURL("https://www.vvic.com/").WithHTTPOnly(true).Do(ec)
			})); err != nil {
				t.Fatal(err)
			}
			calls := 0
			session.getter.client.Transport = soukuanRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "img1.vvic.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.vvic.com/" {
					t.Errorf("unsafe CDN request %s %v", r.URL, r.Header)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic")), Request: r}, nil
			})
			got, err := session.Capture(ctx)
			if tc.protected || tc.name == "visible-mask" {
				if err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "保护") {
					t.Fatalf("protected capture=%+v err=%v", got, err)
				}
			} else if tc.name == "foreign-image" {
				if err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "不完整") {
					t.Fatalf("foreign image capture=%+v err=%v", got, err)
				}
			} else {
				if err != nil || len(got.Items) != 3 || session.PageURL() != soukuanTestPage {
					t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
				}
				resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if calls != 1 {
					t.Fatalf("CDN requests=%d", calls)
				}
			}
			select {
			case err := <-interceptErrors:
				t.Fatalf("interception failed: %v", err)
			default:
			}
		})
	}
}
