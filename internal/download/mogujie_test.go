package download

import (
	"context"
	"encoding/base64"
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

const mogujieTestID = "16qwaji"
const mogujieTestPage = "https://shop.mogu.com/detail/" + mogujieTestID
const mogujieTestHTML = `<html><head><title>测试商品</title><script>
var detailInfo = {
 itemid: "16qwaji",
 attribute: [{"itemId":"16qwaji","imageOrg":"https://s20.mogucdn.com/b7/sku.jpg","style":"白色"}],
 main: {"topImages":["http://s18.mogucdn.com/b7/main.jpg_468x468.jpg"]}
};
</script></head><body>
<div id="J_SmallImgs"><img src="https://s18.mogucdn.com/b7/main.jpg_468x468.jpg"></div>
<div id="J_GoodsSku"><div title="白色"><div><img src="https://s20.mogucdn.com/b7/sku.jpg_100x100.jpg"></div></div></div>
<div id="J_ModuleGraphic"><img src="data:image/gif;base64,AA" data-original="https://s16.mogucdn.com/b7/detail.jpg"></div>
<img src="https://s18.mogucdn.com/b7/recommended.jpg"></body></html>`

type mogujieRoundTrip func(*http.Request) (*http.Response, error)

func (f mogujieRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func mogujieResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestMogujieRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{mogujieTestPage, mogujieTestID},
		{"http://shop.mogujie.com/detail/1mv3ps2?tracking=1", "1mv3ps2"},
		{"https://shop.mogu.com/detail/16qwaji/", ""},
		{"https://shop.mogu.com/detail/16q%2fwaji", ""},
		{"https://shop.mogu.com/detail/../../../secret", ""},
		{"https://shop.mogu.com.evil.test/detail/16qwaji", ""},
		{"https://user@shop.mogu.com/detail/16qwaji", ""},
		{"https://shop.mogu.com:8443/detail/16qwaji", ""},
		{"https://api.mogu.com/h5/http.detail.api/1/", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := mogujieProductID(u); got != tc.id {
			t.Errorf("product %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(mogujieTestPage)
	for _, tc := range []struct{ raw, want string }{
		{"//s18.mogucdn.com/b7/main.jpg_468x468.jpg", "https://s18.mogucdn.com/b7/main.jpg"},
		{"http://s20.mogucdn.com/b7/sku.jpg", "https://s20.mogucdn.com/b7/sku.jpg"},
		{"https://s20.mogucdn.com/b7/sku.jpg_100x100.jpg?sig=keep", "https://s20.mogucdn.com/b7/sku.jpg_100x100.jpg?sig=keep"},
		{"https://s20.mogucdn.com.evil.test/b7/sku.jpg", ""},
		{"https://user@s20.mogucdn.com/b7/sku.jpg", ""},
		{"https://s20.mogucdn.com:8443/b7/sku.jpg", ""},
		{"https://s20.mogucdn.com/b7/../private.jpg", ""},
		{"data:image/png;base64,AA", ""},
	} {
		got, ok := mogujieImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q ok=%t", tc.raw, got, ok)
		}
	}
}

func TestMogujieProductAndDetails(t *testing.T) {
	page, _ := url.Parse(mogujieTestPage + "?tracking=private")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return mogujieResponse(raw, 200, mogujieTestHTML), nil
	})
	got, err := discoverMogujieProduct(context.Background(), getter, page)
	if err != nil || got.Title != "测试商品" || len(got.Items) != 3 {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	want := []string{"main", "variant", "description"}
	for i, item := range got.Items {
		if !item.RequireImage || item.Metadata["mogujie_item_id"] != mogujieTestID || item.Metadata["image_kind"] != want[i] || item.Headers["Referer"] != "https://shop.mogu.com/" || item.Headers["Cookie"] != "" || strings.Contains(item.URL, "recommended") {
			t.Errorf("image %d=%+v", i, item)
		}
	}
	if got.Items[0].URL != "https://s18.mogucdn.com/b7/main.jpg" || got.Items[1].Metadata["variant"] != "白色" {
		t.Fatalf("original or variant missing: %+v", got.Items)
	}
}

func TestMogujiePartialAndSafety(t *testing.T) {
	page, _ := url.Parse(mogujieTestPage)
	for _, tc := range []struct {
		mode string
		want int
	}{{"wrong-id", 0}, {"invalid-host", 2}, {"bad-sku", 3}, {"missing-data", 0}, {"challenge", 0}, {"signed-thumb", 3}, {"cancel", 0}} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := mogujieTestHTML
			switch tc.mode {
			case "wrong-id":
				body = strings.Replace(body, `itemid: "16qwaji"`, `itemid: "other1"`, 1)
			case "invalid-host":
				body = strings.Replace(body, `https://s16.mogucdn.com/b7/detail.jpg`, `https://evil.test/detail.jpg`, 1)
			case "bad-sku":
				body = strings.Replace(body, `attribute: [{"itemId":"16qwaji","imageOrg":"https://s20.mogucdn.com/b7/sku.jpg","style":"白色"}]`, `attribute: false`, 1)
			case "missing-data":
				body = `<title>蘑菇街</title><img src="https://s18.mogucdn.com/b7/recommended.jpg">`
			case "challenge":
				body = `<title>安全验证</title><div id="J_SmallImgs"><img src="https://s18.mogucdn.com/b7/main.jpg"></div>`
			case "signed-thumb":
				body = strings.Replace(body, `http://s18.mogucdn.com/b7/main.jpg_468x468.jpg`, `http://s18.mogucdn.com/b7/main.jpg_468x468.jpg?sig=keep`, 1)
				body = strings.Replace(body, `https://s18.mogucdn.com/b7/main.jpg_468x468.jpg`, `https://s18.mogucdn.com/b7/main.jpg_468x468.jpg?sig=keep`, 1)
			case "cancel":
				cancel()
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return mogujieResponse(raw, 200, body), nil
			})
			got, err := discoverMogujieProduct(ctx, getter, page)
			if err == nil || len(got.Items) != tc.want || (tc.mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s count=%d err=%v", tc.mode, len(got.Items), err)
			}
			for _, item := range got.Items {
				if strings.Contains(item.URL, "evil.test") || strings.Contains(item.URL, "recommended") {
					t.Fatalf("unsafe result %+v", item)
				}
			}
		})
	}
}

func TestMogujieHistoricalDetail(t *testing.T) {
	page, _ := url.Parse("https://shop.mogujie.com/detail/16qwaji")
	doc, err := html.Parse(strings.NewReader(`<title>旧商品</title><script>var detailInfo = {
itemid: "16qwaji",
attribute: [{"itemId":"16qwaji","imageOrg":"http://s20.mogucdn.com/sku.jpg","style":"白"}],
main: {"topImages":["http://s18.mogucdn.com/main.jpg_468x468.jpg"]}
};</script><div id="J_ModuleDetail"><div class="img-box"><img src="http://www.mogujie.com/img/transparent.gif" d-src="http://s16.mogucdn.com/detail.jpg"></div><img src="http://s18.mogucdn.com/recommend.jpg"></div>`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := mogujieProductPage(context.Background(), doc, page)
	if err != nil || len(got.Items) != 3 || got.Items[2].URL != "https://s16.mogucdn.com/detail.jpg" {
		t.Fatalf("historical=%+v err=%v", got, err)
	}
}

func TestMogujieRedirects(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			page, _ := url.Parse(mogujieTestPage)
			jar, _ := cookiejar.New(nil)
			jar.SetCookies(page, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			calls := 0
			getter.client.Transport = mogujieRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("credentials sent to %s", r.URL.Host)
				}
				status, body := 200, mogujieTestHTML
				header := http.Header{"Content-Type": {"text/html"}}
				if calls == 1 {
					status, body = 302, ""
					target := mogujieTestPage + "?source=redirect"
					switch mode {
					case "wrong-id":
						target = "https://shop.mogu.com/detail/other1"
					case "cross-origin":
						target = "https://evil.test/detail/16qwaji"
					case "downgrade":
						target = "http://shop.mogu.com/detail/16qwaji"
					}
					header.Set("Location", target)
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := discoverMogujieProduct(context.Background(), getter, page)
			if mode == "success" {
				if err != nil || len(got.Items) != 3 || calls != 2 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || calls != 1 || len(got.Items) != 0 {
				t.Fatalf("unsafe redirect=%+v calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func TestMogujieRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == mogujieTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(mogujieTestHTML)))
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
	session.start, _ = url.Parse(mogujieTestPage)
	if err := session.run(ctx, camoufox.Navigate(mogujieTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".mogu.com").WithURL("https://shop.mogu.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = mogujieRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "s18.mogucdn.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://shop.mogu.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://s18.mogucdn.com/b7/main.jpg" || session.PageURL() != mogujieTestPage {
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

func TestMogujieArchivedProduct(t *testing.T) {
	if os.Getenv("TULAI_MOGUJIE_ARCHIVE") != "1" {
		t.Skip("set TULAI_MOGUJIE_ARCHIVE=1 for public historical sample")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	const archive = "https://raw.githubusercontent.com/WilsonWangTHU/shell_tools/master/test/Clothing/Tshirt/16qwaji/page.html"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archive, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("historical page HTTP %d", response.StatusCode)
	}
	doc, err := html.Parse(io.LimitReader(response.Body, maxPageBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse("https://shop.mogujie.com/detail/16qwaji")
	got, err := mogujieProductPage(ctx, doc, page)
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
	getter := NewHTTPGetter("")
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
	t.Logf("historical public product counts=%v; main, variant and description downloaded", counts)
}

func TestMogujieCurrentPublicPage(t *testing.T) {
	if os.Getenv("TULAI_MOGUJIE_LIVE") != "1" {
		t.Skip("set TULAI_MOGUJIE_LIVE=1 for current public page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	page, _ := url.Parse("https://shop.mogu.com/detail/1mqewws")
	got, err := discoverMogujieProduct(ctx, NewHTTPGetter(""), page)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("current page changed: images=%d err=%v", len(got.Items), err)
	}
	t.Logf("current public page returned no product data: %v", err)
}

func TestMogujieDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return mogujieResponse(raw, 200, mogujieTestHTML), nil
	})
	got, err := Discover(context.Background(), getter, mogujieTestPage, 1)
	if err != nil || len(got.Items) != 3 || got.Items[0].URL != "https://s18.mogucdn.com/b7/main.jpg" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestMogujieEmptyState(t *testing.T) {
	page, _ := url.Parse(mogujieTestPage)
	doc, err := html.Parse(strings.NewReader(fmt.Sprintf(`<title>商品</title><script>var detailInfo = {
itemid: %q,
attribute: [],
main: {"topImages":[]}
};</script>`, mogujieTestID)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := mogujieProductPage(context.Background(), doc, page)
	if err == nil || len(got.Items) != 0 || strings.Contains(err.Error(), "数据无法完整解析") {
		t.Fatalf("empty=%+v err=%v", got, err)
	}
}
