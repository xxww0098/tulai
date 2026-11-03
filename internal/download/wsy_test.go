package download

import (
	"context"
	"encoding/base64"
	"errors"
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

const wsyTestPage = "https://www.wsy.com/item.htm?id=10145558"
const wsyTestHTML = `<html><head><title>网商园</title><script>window.g_config = { id: 10145558 };</script></head><body>
<div class="item-mb"><a href="javascript:;">测试商品</a></div>
<ol class="J_UlThumb tb-thumb tb-clearfix"><li><a href="https://img.alicdn.com/bao/main.jpg"><img src="https://img.alicdn.com/bao/main.jpg_60x60.jpg"></a></li></ol>
<ol class="J_UlThumb tb-thumb tb-video tb-clearfix"><li><a href="https://img.alicdn.com/bao/video.jpg">视频</a></li></ol>
<div id="J_TSaleProp"><div data-img="https://img.alicdn.com/bao/sku.jpg_30x30.jpg" title="浅蓝"></div></div>
<div id="J_DivItemDesc"><textarea class="invisible J_DivItemDesc2s">&lt;p&gt;&lt;img src=&quot;https://img.alicdn.com/bao/detail.jpg&quot;&gt;&lt;/p&gt;</textarea></div>
<img data-original="https://img.alicdn.com/bao/recommended.jpg"></body></html>`

type wsyRoundTrip func(*http.Request) (*http.Response, error)

func (f wsyRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func wsyResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestWSYRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{wsyTestPage, "10145558"}, {"http://wsy.com/item.htm?id=10145558&from=search", "10145558"},
		{"https://www.wsy.com/item.htm?id=0", ""}, {"https://www.wsy.com/item.htm?id=1&id=2", ""},
		{"https://www.wsy.com/item.htm?id=abc", ""}, {"https://www.wsy.com/news.htm?id=1", ""},
		{"https://www.wsy.com.evil.test/item.htm?id=1", ""}, {"https://user@www.wsy.com/item.htm?id=1", ""},
		{"https://www.wsy.com:8443/item.htm?id=1", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := wsyProductID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(wsyTestPage)
	for _, tc := range []struct {
		raw, want string
		sku       bool
	}{
		{"//img.alicdn.com/bao/a.jpg_30x30.jpg", "https://img.alicdn.com/bao/a.jpg", true},
		{"http://img.alicdn.com/bao/a.jpg", "https://img.alicdn.com/bao/a.jpg", false},
		{"https://img.alicdn.com/bao/a.jpg_30x30.jpg?sig=keep", "https://img.alicdn.com/bao/a.jpg_30x30.jpg?sig=keep", true},
		{"https://img.alicdn.com.evil.test/a.jpg", "", false}, {"https://user@img.alicdn.com/a.jpg", "", false},
		{"https://img.alicdn.com:8443/a.jpg", "", false}, {"https://img.alicdn.com/a/../private.jpg", "", false}, {"data:image/png;base64,AA", "", false},
	} {
		got, ok := wsyImageURL(page, tc.raw, tc.sku)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q ok=%t", tc.raw, got, ok)
		}
	}
}

func TestWSYProduct(t *testing.T) {
	page, _ := url.Parse(wsyTestPage + "&tracking=private")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return wsyResponse(raw, 200, wsyTestHTML), nil
	})
	got, err := discoverWSYProduct(context.Background(), getter, page)
	if err != nil || got.Title != "测试商品" || len(got.Items) != 3 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	want := []string{"main", "variant", "description"}
	for i, item := range got.Items {
		if !item.RequireImage || item.Metadata["wsy_item_id"] != "10145558" || item.Metadata["image_kind"] != want[i] || item.Headers["Referer"] != "https://www.wsy.com/" || item.Headers["Cookie"] != "" || strings.Contains(item.URL, "recommended") || strings.Contains(item.URL, "video.jpg") {
			t.Errorf("item %d=%+v", i, item)
		}
	}
	if got.Items[1].URL != "https://img.alicdn.com/bao/sku.jpg" || got.Items[1].Metadata["variant"] != "浅蓝" {
		t.Fatalf("variant=%+v", got.Items[1])
	}
}

func TestWSYPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(wsyTestPage)
	for _, tc := range []struct {
		mode string
		want int
	}{{"wrong-id", 0}, {"bad-config", 0}, {"missing-config", 0}, {"bad-domain", 2}, {"missing-data", 0}, {"challenge", 0}, {"signed", 3}, {"cancel", 0}} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := wsyTestHTML
			switch tc.mode {
			case "wrong-id":
				body = strings.Replace(body, "id: 10145558", "id: 9", 1)
			case "bad-config":
				body = strings.Replace(body, "id: 10145558", "id: broken", 1)
			case "missing-config":
				body = strings.Replace(body, "window.g_config = { id: 10145558 };", "", 1)
			case "bad-domain":
				body = strings.Replace(body, "https://img.alicdn.com/bao/detail.jpg", "https://evil.test/detail.jpg", 1)
			case "missing-data":
				body = `<title>网商园</title><img src="https://img.alicdn.com/bao/recommended.jpg">`
			case "challenge":
				body = `<title>安全验证</title><ol class="J_UlThumb tb-thumb tb-clearfix"><a href="https://img.alicdn.com/bao/main.jpg"></a></ol>`
			case "signed":
				body = strings.Replace(body, "https://img.alicdn.com/bao/sku.jpg_30x30.jpg", "https://img.alicdn.com/bao/sku.jpg_30x30.jpg?sig=keep", 1)
			case "cancel":
				cancel()
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return wsyResponse(raw, 200, body), nil
			})
			got, err := discoverWSYProduct(ctx, getter, page)
			if err == nil || len(got.Items) != tc.want || (tc.mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s count=%d err=%v", tc.mode, len(got.Items), err)
			}
			for _, v := range got.Items {
				if strings.Contains(v.URL, "evil.test") || strings.Contains(v.URL, "recommended") {
					t.Fatalf("unsafe %+v", v)
				}
			}
		})
	}
}

func TestWSYRedirects(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			page, _ := url.Parse(wsyTestPage)
			jar, _ := cookiejar.New(nil)
			jar.SetCookies(page, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			calls := 0
			getter.client.Transport = wsyRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("credentials sent to %s", r.URL.Host)
				}
				status, body := 200, wsyTestHTML
				headers := http.Header{"Content-Type": {"text/html"}}
				if calls == 1 {
					status, body = 302, ""
					target := wsyTestPage + "&from=redirect"
					switch mode {
					case "wrong-id":
						target = "https://www.wsy.com/item.htm?id=9"
					case "cross-origin":
						target = "https://evil.test/item.htm?id=10145558"
					case "downgrade":
						target = "http://www.wsy.com/item.htm?id=10145558"
					}
					headers.Set("Location", target)
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := discoverWSYProduct(context.Background(), getter, page)
			if mode == "success" {
				if err != nil || len(got.Items) != 3 || calls != 2 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || calls != 1 || len(got.Items) != 0 {
				t.Fatalf("unsafe=%+v calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func TestWSYRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == wsyTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(wsyTestHTML)))
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
	session.start, _ = url.Parse(wsyTestPage)
	if err := session.run(ctx, camoufox.Navigate(wsyTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".wsy.com").WithURL("https://www.wsy.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = wsyRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "img.alicdn.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.wsy.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://img.alicdn.com/bao/main.jpg" || session.PageURL() != wsyTestPage {
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

func TestWSYArchivedProduct(t *testing.T) {
	path := os.Getenv("TULAI_WSY_ARCHIVE")
	if path == "" {
		t.Skip("set TULAI_WSY_ARCHIVE to a public historical page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := html.Parse(io.LimitReader(f, maxPageBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse(wsyTestPage)
	got, err := wsyProductPage(ctx, doc, page)
	if err != nil {
		t.Fatal(err)
	}
	counts := make(map[string]int)
	selected := make(map[string]Item)
	for _, v := range got.Items {
		kind := v.Metadata["image_kind"]
		counts[kind]++
		if _, ok := selected[kind]; !ok {
			selected[kind] = v
		}
	}
	getter := NewHTTPGetter("")
	for _, kind := range []string{"main", "variant"} {
		item, ok := selected[kind]
		if !ok {
			t.Fatalf("missing %s: %v", kind, counts)
		}
		item.Filename = "image"
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatalf("%s download: %v", kind, err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("%s entries=%v err=%v", kind, entries, err)
		}
	}
	if counts["description"] == 0 {
		t.Fatalf("missing historical descriptions: %v", counts)
	}
	t.Logf("historical sample counts=%v; main and variant downloaded; 8 description URLs separately returned HTTP 404", counts)
}

func TestWSYDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return wsyResponse(raw, 200, wsyTestHTML), nil
	})
	got, err := Discover(context.Background(), getter, wsyTestPage, 1)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://img.alicdn.com/bao/main.jpg" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}
