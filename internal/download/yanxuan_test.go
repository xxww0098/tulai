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
)

const yanxuanTestPage = "https://you.163.com/item/detail?id=4248855"

type yanxuanRoundTrip func(*http.Request) (*http.Response, error)

func (f yanxuanRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func yanxuanHTTPResponse(r *http.Request, code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

const yanxuanTestHTML = `<html><script>var JSON_DATA_FROMFTL = {"item":{"id":4248855,"name":"测试商品","skuSpecList":[{"skuSpecValueList":[{"picUrl":"https://yanxuan-item.nosdn.127.net/sku.png","value":"蓝色"}]}],"itemDetail":{"picUrl3":"https://yanxuan-item.nosdn.127.net/main3.jpg","detailHtml":"<p><img src=\"https://yanxuan-item.nosdn.127.net/detail1.jpg\"><img src=\"https://yanxuan.nosdn.127.net/detail2.jpg\"></p>","picUrl1":"https://yanxuan-item.nosdn.127.net/main1.jpg"}},"other":undefined};</script><img src="https://yanxuan-item.nosdn.127.net/recommended.jpg"></html>`

func TestYanxuanRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{yanxuanTestPage, "4248855"},
		{"http://you.163.com/item/detail?id=1076015&from=share", "1076015"},
		{"https://you.163.com/item/detail?id=4248855&id=1076015", ""},
		{"https://you.163.com/item/detail?id=00000", ""},
		{"https://you.163.com/item/detail?id=abc123", ""},
		{"https://you.163.com/item/detail/other?id=4248855", ""},
		{"https://you.163.com.evil.test/item/detail?id=4248855", ""},
		{"https://user@you.163.com/item/detail?id=4248855", ""},
		{"https://you.163.com:8443/item/detail?id=4248855", ""},
		{"https://m.you.163.com/item/detail?id=4248855", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := yanxuanProductID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	page, _ := url.Parse(yanxuanTestPage)
	for _, tc := range []struct{ raw, want string }{
		{"//yanxuan-item.nosdn.127.net/a.jpg?sig=keep", "https://yanxuan-item.nosdn.127.net/a.jpg?sig=keep"},
		{"http://yanxuan.nosdn.127.net/a.jpg", "https://yanxuan.nosdn.127.net/a.jpg"},
		{"https://yanxuan-item.nosdn.127.net.evil.test/a.jpg", ""},
		{"https://user@yanxuan-item.nosdn.127.net/a.jpg", ""},
		{"https://yanxuan-item.nosdn.127.net:8443/a.jpg", ""},
		{"https://yanxuan-item.nosdn.127.net/a/../private.jpg", ""},
		{"data:image/png;base64,AA", ""},
	} {
		got, ok := yanxuanImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q ok=%t", tc.raw, got, ok)
		}
	}
}

func TestYanxuanProductAndDetails(t *testing.T) {
	page, _ := url.Parse(yanxuanTestPage + "&private=source")
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(yanxuanTestHTML))}, nil
	})
	got, err := discoverYanxuanProduct(context.Background(), getter, page)
	if err != nil || got.Title != "测试商品" || len(got.Items) != 5 || got.Items[0].URL != "https://yanxuan-item.nosdn.127.net/main3.jpg" || got.Items[1].URL != "https://yanxuan-item.nosdn.127.net/main1.jpg" || got.Items[2].Metadata["variant"] != "蓝色" || got.Items[3].Metadata["image_kind"] != "description" {
		t.Fatalf("product=%+v err=%v", got, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["yanxuan_item_id"] != "4248855" || item.Headers["Referer"] != "https://you.163.com/" || item.Headers["Cookie"] != "" {
			t.Fatalf("item=%+v", item)
		}
	}
}

func TestYanxuanPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(yanxuanTestPage)
	for _, mode := range []string{"wrong-id", "invalid-image", "missing-sku", "missing-detail", "bad-state", "challenge", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := yanxuanTestHTML
			switch mode {
			case "wrong-id":
				body = strings.Replace(body, `"id":4248855`, `"id":1076015`, 1)
			case "invalid-image":
				body = strings.Replace(body, `https://yanxuan-item.nosdn.127.net/detail1.jpg`, `https://evil.test/a.jpg`, 1)
			case "missing-sku":
				body = strings.Replace(body, `"skuSpecList":`, `"otherSpecList":`, 1)
			case "missing-detail":
				body = strings.Replace(body, `"detailHtml":`, `"otherHtml":`, 1)
			case "bad-state":
				body = `<script>var JSON_DATA_FROMFTL = {"item":{broken}};</script><img src="https://yanxuan-item.nosdn.127.net/recommended.jpg">`
			case "challenge":
				body = `<div>Verification required</div><img src="https://yanxuan-item.nosdn.127.net/recommended.jpg">`
			case "cancel":
				cancel()
			}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			got, err := discoverYanxuanProduct(ctx, getter, page)
			want := 0
			switch mode {
			case "invalid-image":
				want = 4
			case "missing-sku":
				want = 4
			case "missing-detail":
				want = 3
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestYanxuanRedirects(t *testing.T) {
	other := "https://you.163.com/item/detail?id=4248855&from=redirect"
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			jar, _ := cookiejar.New(nil)
			u, _ := url.Parse(yanxuanTestPage)
			jar.SetCookies(u, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			var calls int
			getter.client.Transport = yanxuanRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected credentials to %s", r.URL.Host)
				}
				if r.URL.String() == yanxuanTestPage {
					resp := yanxuanHTTPResponse(r, 302, "")
					target := other
					switch mode {
					case "wrong-id":
						target = "https://you.163.com/item/detail?id=1076015"
					case "cross-origin":
						target = "https://evil.test/item/detail?id=4248855"
					case "downgrade":
						target = strings.Replace(other, "https:", "http:", 1)
					}
					resp.Header.Set("Location", target)
					return resp, nil
				}
				if r.URL.String() != other {
					t.Errorf("unexpected request %s", r.URL)
				}
				return yanxuanHTTPResponse(r, 200, yanxuanTestHTML), nil
			})
			page, _ := url.Parse(yanxuanTestPage)
			got, err := discoverYanxuanProduct(context.Background(), getter, page)
			if mode == "success" {
				if err != nil || len(got.Items) != 5 || calls != 2 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || calls != 1 || len(got.Items) != 0 {
				t.Fatalf("unsafe redirect=%+v calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func TestYanxuanDiscoverDispatch(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(yanxuanTestHTML))}, nil
	})
	got, err := Discover(context.Background(), getter, yanxuanTestPage, 1)
	if err != nil || len(got.Items) != 5 || got.Items[0].URL != "https://yanxuan-item.nosdn.127.net/main3.jpg" {
		t.Fatalf("discovery=%+v err=%v", got, err)
	}
}

func TestYanxuanRenderedCapture(t *testing.T) {
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
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == yanxuanTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(yanxuanTestHTML)))
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
	session.start, _ = url.Parse(yanxuanTestPage)
	if err := session.run(ctx, camoufox.Navigate(yanxuanTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".163.com").WithURL("https://you.163.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var calls int
	session.getter.client.Transport = yanxuanRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "yanxuan-item.nosdn.127.net" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://you.163.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 5 || got.Items[0].URL != "https://yanxuan-item.nosdn.127.net/main3.jpg" || got.Items[2].Metadata["image_kind"] != "sku" || session.PageURL() != yanxuanTestPage {
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
	case err := <-responses:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestYanxuanLiveProduct(t *testing.T) {
	if os.Getenv("TULAI_YANXUAN_LIVE") != "1" {
		t.Skip("set TULAI_YANXUAN_LIVE=1 for a public product")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, yanxuanTestPage, 1)
	if err != nil {
		t.Fatal(err)
	}
	var main, sku, detail *Item
	counts := make(map[string]int)
	for i := range got.Items {
		counts[got.Items[i].Metadata["image_kind"]]++
		switch got.Items[i].Metadata["image_kind"] {
		case "main":
			if main == nil {
				main = &got.Items[i]
			}
		case "sku":
			if sku == nil {
				sku = &got.Items[i]
			}
		case "description":
			if detail == nil {
				detail = &got.Items[i]
			}
		}
	}
	if main == nil || sku == nil || detail == nil {
		t.Fatalf("public product missing gallery, SKU or description: counts=%v", counts)
	}
	for i, source := range []*Item{main, sku, detail} {
		item := *source
		item.Filename = "image"
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatal(err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("image %d entries=%v err=%v", i, entries, err)
		}
		if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
			t.Fatalf("empty image %d: %v %v", i, info, err)
		}
	}
	t.Logf("public product counts=%v; gallery, SKU and description images downloaded", counts)
}
