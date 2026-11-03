package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/xxww0098/tulai/internal/camoufox"
	"golang.org/x/net/html"
)

const mujiTestID = "4549738975236"
const mujiTestPage = "https://www.muji.com.cn/cn/store/goods/" + mujiTestID

func mujiTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func mujiTestDoc(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func mujiTestResponse(raw string, status int, contentType, body string) *Response {
	return &Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}
}
func mujiSyntheticJPEG(raw string) *Response {
	return mujiTestResponse(raw, 200, "image/jpeg", "\xff\xd8\xffsynthetic-jpeg")
}

type mujiTestTransport func(*http.Request) (*http.Response, error)

func (f mujiTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMujiProductID(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{mujiTestPage, mujiTestID}, {"http://www.muji.com.cn/cn/store/goods/123-4.5+6", "123-4.5+6"},
		{mujiTestPage + "?synthetic=secret", mujiTestID}, {"https://www.muji.com.cn:443/cn/store/goods/123", "123"},
		{"https://muji.com.cn/cn/store/goods/123", ""}, {"https://www.muji.com.cn.evil.test/cn/store/goods/123", ""},
		{"https://user@www.muji.com.cn/cn/store/goods/123", ""}, {"https://www.muji.com.cn:8443/cn/store/goods/123", ""},
		{"https://www.muji.com.cn/cn/store/commodity/123", ""}, {"https://www.muji.com.cn/cn/store/goods/123/", ""},
		{"https://www.muji.com.cn/cn/store/goods/123%2F4", ""}, {"https://www.muji.com.cn/cn/store/goods/../x", ""},
		{"https://www.muji.com.cn/cn/store/goods/abc", ""},
	} {
		if got := mujiProductID(mujiTestURL(t, tc.raw)); got != tc.want {
			t.Errorf("%q ID=%q want %q", tc.raw, got, tc.want)
		}
	}
}

func TestMujiDiscoveryPartialAndOrder(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
		if err != nil {
			return nil, err
		}
		if prepare != nil {
			prepare(req)
		}
		mu.Lock()
		seen[raw]++
		mu.Unlock()
		if raw == mujiTestPage {
			return mujiTestResponse(raw, 200, "text/html", "<html><title>无印良品</title></html>"), nil
		}
		if req.Header.Get("Range") != "bytes=0-2" || req.Header.Get("Accept") != "image/jpeg" || req.Header.Get("Cookie") != "" || ctx.Value(publicDownloadKey{}) != true {
			return nil, errors.New("unsafe probe headers")
		}
		switch raw {
		case mujiImageURL(mujiTestID, 1), mujiImageURL(mujiTestID, 2):
			return mujiSyntheticJPEG(raw), nil
		case mujiImageURL(mujiTestID, 3):
			return mujiTestResponse(raw, 403, "text/html", "forbidden"), nil
		default:
			return mujiTestResponse(raw, 404, "text/html", "not found"), nil
		}
	})
	page := mujiTestURL(t, mujiTestPage)
	got, err := discoverMuji(context.Background(), getter, page)
	if err == nil || !strings.Contains(err.Error(), "18 个地址未能确认") || !strings.Contains(err.Error(), "1 个返回 HTTP 403") || len(got.Items) != 2 {
		t.Fatalf("got %d items err=%v", len(got.Items), err)
	}
	for i, item := range got.Items {
		if item.URL != mujiImageURL(mujiTestID, i+1) || !item.PublicOnly || !item.RequireImage || item.Metadata["muji_product_id"] != mujiTestID || item.Headers["Accept"] != "image/jpeg" {
			t.Errorf("item %d: %+v", i, item)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 21 {
		t.Fatalf("requested %d distinct addresses, want page + 20 images", len(seen))
	}
	for raw, count := range seen {
		if count != 1 {
			t.Errorf("request %q counted %d times", raw, count)
		}
	}
}

func TestMujiProbeValidation(t *testing.T) {
	raw := mujiImageURL(mujiTestID, 1)
	for _, tc := range []struct {
		name string
		resp *Response
		ok   bool
	}{
		{"JPEG", mujiSyntheticJPEG(raw), true},
		{"HTML claiming JPEG", mujiTestResponse(raw, 200, "image/jpeg", "<html>"), false},
		{"image without JPEG MIME", mujiTestResponse(raw, 200, "text/html", "\xff\xd8\xff"), false},
		{"forbidden is unresolved", mujiTestResponse(raw, 403, "text/html", "forbidden"), false},
		{"redirected image", func() *Response { r := mujiSyntheticJPEG(raw); r.FinalURL = "https://evil.test/1.jpg"; return r }(), false},
		{"partial response wrong offset", func() *Response {
			r := mujiSyntheticJPEG(raw)
			r.StatusCode = 206
			r.Header.Set("Content-Range", "bytes 3-5/10")
			return r
		}(), false},
		{"partial response from zero", func() *Response {
			r := mujiSyntheticJPEG(raw)
			r.StatusCode = 206
			r.Header.Set("Content-Range", "bytes 0-2/10")
			return r
		}(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) { return tc.resp, nil })
			if got := mujiProbe(context.Background(), getter, raw); got.ok != tc.ok {
				t.Fatalf("probe=%+v want %v", got, tc.ok)
			}
		})
	}
}

func TestMujiRedirectCancellationAndVerification(t *testing.T) {
	client := NewHTTPGetter("")
	var external atomic.Bool
	client.client.Transport = mujiTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "img.muji.com.cn" {
			external.Store(true)
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/a.jpg"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if got := mujiProbe(context.Background(), mujiImageGetter(client), mujiImageURL(mujiTestID, 1)); got.ok || external.Load() {
		t.Fatalf("redirect probe=%+v external=%v", got, external.Load())
	}
	page := mujiTestURL(t, mujiTestPage)
	calls := atomic.Int32{}
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		calls.Add(1)
		return nil, errors.New("must not request")
	})
	if got, err := mujiCollect(context.Background(), getter, page, mujiTestDoc(t, `<title>请登录</title>`)); err == nil || len(got.Items) != 0 || calls.Load() != 0 {
		t.Fatalf("login page items=%d calls=%d err=%v", len(got.Items), calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	getter = GetterFunc(func(ctx context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == mujiImageURL(mujiTestID, 1) {
			cancel()
			return mujiSyntheticJPEG(raw), nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	got, err := mujiCollect(ctx, getter, page, mujiTestDoc(t, `<title>MUJI</title>`))
	if !errors.Is(err, context.Canceled) || len(got.Items) != 1 || got.Items[0].URL != mujiImageURL(mujiTestID, 1) {
		t.Fatalf("cancel items=%+v err=%v", got.Items, err)
	}
}

func TestMujiTwentyImagesRemainLimited(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return mujiSyntheticJPEG(raw), nil
	})
	got, err := mujiCollect(context.Background(), getter, mujiTestURL(t, mujiTestPage), mujiTestDoc(t, `<title>MUJI</title>`))
	if err == nil || !strings.Contains(err.Error(), "400 像素") || len(got.Items) != 20 {
		t.Fatalf("20 images=%d err=%v", len(got.Items), err)
	}
}

func TestMujiLive(t *testing.T) {
	if os.Getenv("TULAI_MUJI_LIVE") != "1" {
		t.Skip("set TULAI_MUJI_LIVE=1 for the public historical product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, mujiTestPage, 1)
	if len(got.Items) == 0 {
		t.Fatalf("real product returned no verified images: %v", err)
	}
	if err == nil {
		t.Fatal("expected limited 400px / possible partial warning")
	}
	for _, item := range got.Items {
		if item.URL != mujiImageURL(mujiTestID, 1) && item.URL != mujiImageURL(mujiTestID, 2) {
			t.Logf("site has an additional available image: %s", item.URL)
		}
	}
	t.Logf("public discovery: %d images, limitation: %v", len(got.Items), err)
	for _, item := range got.Items {
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("saved entries=%v err=%v", entries, err)
		}
		f, err := os.Open(filepath.Join(dir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		cfg, format, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || format != "jpeg" || cfg.Width == 0 || cfg.Height == 0 {
			t.Fatalf("download image format=%s size=%dx%d err=%v", format, cfg.Width, cfg.Height, err)
		}
		t.Logf("saved %s as JPEG %dx%d", item.Metadata["muji_sequence"], cfg.Width, cfg.Height)
	}
}

func TestMujiCamoufoxCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_MUJI_CAMOUFOX_ROOT")
	if runtime == "" {
		t.Skip("set TULAI_MUJI_CAMOUFOX_ROOT to a copied temporary runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	process, err := camoufox.Start(ctx, runtime, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(process.Context())
	tab := camoufox.PageContext(life, process, process.Page)
	getter := NewHTTPGetter("")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(mujiTestURL(t, "https://img.muji.com.cn"), []*http.Cookie{{Name: "synthetic", Value: "must-not-leak"}})
	getter.client.Jar = jar
	var leaked atomic.Bool
	getter.client.Transport = mujiTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Cookie") != "" || r.URL.Hostname() != "img.muji.com.cn" {
			leaked.Store(true)
		}
		if r.URL.String() == mujiImageURL(mujiTestID, 1) || r.URL.String() == mujiImageURL(mujiTestID, 2) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("\xff\xd8\xffjpeg")), Request: r}, nil
		}
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("forbidden")), Request: r}, nil
	})
	page := mujiTestURL(t, mujiTestPage)
	session := &RenderedSession{process: process, tab: tab, closeTab: closeTab, closeAllocator: process.Close, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == mujiTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(`<html><head><title>MUJI</title></head><body>商品</body></html>`)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			_ = camoufox.Run(session.tab, action)
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(mujiTestPage)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || len(got.Items) != 2 || got.Items[0].URL != mujiImageURL(mujiTestID, 1) || leaked.Load() {
		t.Fatalf("capture %d items cookie leaked=%v err=%v", len(got.Items), leaked.Load(), err)
	}
	t.Logf("browser capture: %d images with expected partial warning, synthetic cookie not sent", len(got.Items))
}

func Example_mujiImageURL() {
	fmt.Println(mujiImageURL("4549738975236", 1))
	// Output: https://img.muji.com.cn/img/item/4549738975236_01_400.jpg
}
