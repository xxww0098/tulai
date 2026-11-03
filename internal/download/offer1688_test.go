package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
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
	"golang.org/x/net/html"
	"golang.org/x/text/encoding/simplifiedchinese"
)

func Test1688OfferURL(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
	}{
		{"https://detail.1688.com/offer/123.html?x=1", "123"},
		{"https://m.1688.com/offer/123.html", "123"},
		{"https://detail.m.1688.com/page/index.htm?offerId=123", "123"},
		{"https://detail.1688.com/buyer/offerdetail/123.html", "123"},
		{"https://detail.1688.com/offer/not-a-number.html", ""},
		{"https://detail.1688.com.evil.test/offer/123.html", ""},
		{"https://user@detail.1688.com/offer/123.html", ""},
		{"https://detail.1688.com:8443/offer/123.html", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := offer1688ID(u); got != tc.want || is1688Offer(u) != (tc.want != "") {
			t.Errorf("offer URL %q = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func Test1688OfferImagesAndDescription(t *testing.T) {
	const root = "https://detail.1688.com/offer/123.html?private=secret"
	const detail = "https://itemcdn.tmall.com/1688offer/description?sign=keep"
	var calls []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		prepare(req)
		calls = append(calls, raw)
		var body string
		switch raw {
		case root:
			body = `<title>测试商品</title>
<div id="J_Detail_ImageSlides"><img src="https://cbu01.alicdn.com/small.jpg" swipe-lazy-src="//cbu01.alicdn.com/main.jpg"></div>
<div class="detail-gallery-turn-wrapper-main"><img src="https://cbu01.alicdn.com/gallery.jpg"></div>
<img src="https://cbu01.alicdn.com/not-product.jpg"><span data-tfs-url="` + detail + `"></span>
<script>var main = {"offerImgList":["https://cbu01.alicdn.com/main.jpg",{"originalImageURI":"https://cbu01.alicdn.com/second.jpg"}],"skuProps":[{"value":[{"imageUrl":"https://cbu01.alicdn.com/sku.jpg"},{"imageUrl":null}]}]};
var iDetailData = {sku:{skuProps:[{"imageUrl":"https://cbu01.alicdn.com/variant.jpg"}]}, detailUrl:"https://itemcdn.tmall.com/1688offer/ignored"};</script>`
		case detail:
			if req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Referer") != "https://detail.1688.com/" {
				t.Fatalf("description headers = %v", req.Header)
			}
			body = `var desc='<div><img src="https://cbu01.alicdn.com/description.jpg"><img src="https://cbu01.alicdn.com/main.jpg"></div>';`
		default:
			t.Fatalf("unexpected request %q", raw)
		}
		return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	u, _ := url.Parse(root)
	got, err := discover1688Offer(context.Background(), getter, u)
	if err != nil || !got.IsPage || got.Title != "测试商品" || len(calls) != 2 || len(got.Items) != 6 {
		t.Fatalf("offer = %+v, calls=%v, err=%v", got, calls, err)
	}
	want := []string{"main.jpg", "gallery.jpg", "second.jpg", "sku.jpg", "variant.jpg", "description.jpg"}
	for i, item := range got.Items {
		if item.URL != "https://cbu01.alicdn.com/"+want[i] || strings.Contains(item.Headers["Referer"], "secret") || strings.Contains(item.Headers["Referer"], "sign=keep") {
			t.Errorf("item %d = %+v", i, item)
		}
	}
}

func Test1688OfferKeepsPartialImagesOnDescriptionFailure(t *testing.T) {
	for _, mode := range []string{"http", "foreign", "redirect", "invalid", "transfer"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				if calls == 1 {
					detail := "https://itemcdn.tmall.com/1688offer/details?sign=private"
					if mode == "foreign" {
						detail = "http://127.0.0.1/private"
					}
					body := fmt.Sprintf(`<div id="J_Detail_ImageSlides"><img src="https://cbu01.alicdn.com/keep.jpg"></div><script>var iDetailData = {"detailUrl":%q};</script>`, detail)
					return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
				}
				if mode == "transfer" {
					return nil, &url.Error{Op: "Get", URL: raw, Err: errors.New("network down")}
				}
				final, status, body := raw, 200, `<img src="https://cbu01.alicdn.com/desc.jpg">`
				if mode == "http" {
					status = 503
				} else if mode == "redirect" {
					final = "https://evil.test/private"
				} else if mode == "invalid" {
					body = "executeMe()"
				}
				return &Response{StatusCode: status, FinalURL: final, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			u, _ := url.Parse("https://detail.1688.com/offer/123.html")
			got, err := discover1688Offer(context.Background(), getter, u)
			if err == nil || strings.Contains(err.Error(), "sign=private") || len(got.Items) != 1 || !strings.HasSuffix(got.Items[0].URL, "/keep.jpg") || (mode == "foreign" && calls != 1) {
				t.Fatalf("partial result=%+v, calls=%d, err=%v", got.Items, calls, err)
			}
		})
	}
}

func Test1688OfferDataParsingAndCancellation(t *testing.T) {
	for _, s := range []string{`{"x":"}","a":{"b":1}} trailing`, `{foo:'}', /* } */ bar:{x:1}}`} {
		if _, ok := offer1688Object(s); !ok {
			t.Fatalf("valid object not recognized: %q", s)
		}
	}
	for _, s := range []string{`{"x":`, `{x:'unterminated}`} {
		if _, ok := offer1688Object(s); ok {
			t.Fatalf("invalid object accepted: %q", s)
		}
	}
	for _, tc := range []struct{ raw, want string }{{`"x\\ny"`, "x\\ny"}, {`'a\'b'`, "a'b"}, {`'<img src="x">'`, `<img src="x">`}} {
		got, _, ok := offer1688String(tc.raw)
		if !ok || got != tc.want {
			t.Errorf("string %q = %q, %v; want %q", tc.raw, got, ok, tc.want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("canceled request reached network")
		return nil, nil
	})
	u, _ := url.Parse("https://detail.1688.com/offer/123.html")
	if _, err := discover1688Offer(ctx, getter, u); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
}

func Test1688OfferIgnoresFieldsInsideStringsAndRejectsExpressions(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<script>
var fake = '{"offerImgList":["https://cbu01.alicdn.com/recommendation.jpg"]}';
// var iDetailData = {"imageUrl":"https://cbu01.alicdn.com/comment.jpg"};
var iDetailData = {message:'"imageUrl":"https://cbu01.alicdn.com/embedded.jpg"',
  sku:{"imageUrl":"https://cbu01.alicdn.com/real.jpg"},
  detailUrl:"https://itemcdn.tmall.com/1688offer/path" + externalValue};
</script>`))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse("https://detail.1688.com/offer/123.html")
	got, detail, err := offer1688Page(doc, page)
	if len(got.Items) != 1 || got.Items[0].URL != "https://cbu01.alicdn.com/real.jpg" || detail != "" || err == nil {
		t.Fatalf("result=%+v detail=%q err=%v", got.Items, detail, err)
	}
}

func Test1688OfferDescriptionGBK(t *testing.T) {
	source, _ := url.Parse("https://dsc.taobaocdn.com/a.desc%7Cvar%5Edesc%3Blang%5Egbk")
	body, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(`var desc='<div>商品描述<img src="https://cbu01.alicdn.com/good.jpg"></div>';`))
	if err != nil {
		t.Fatal(err)
	}
	result := Discovery{IsPage: true}
	if err := offer1688AddDescription(&result, source, body); err != nil || len(result.Items) != 1 || result.Items[0].URL != "https://cbu01.alicdn.com/good.jpg" {
		t.Fatalf("GBK description = %+v, %v", result.Items, err)
	}
}

type offer1688RoundTripper func(*http.Request) (*http.Response, error)

func (f offer1688RoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func Test1688OfferRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "<html><title>placeholder</title></html>")
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	const target = "https://detail.1688.com/offer/123.html?private=synthetic"
	const detail = "https://itemcdn.tmall.com/1688offer/description?sign=synthetic"
	const page = `<html><head><title>测试商品</title></head><body>
<div id="J_Detail_ImageSlides"><img src="https://cbu01.alicdn.com/main.jpg"></div>
<div data-tfs-url="` + detail + `"></div><img src="https://unrelated.invalid/recommendation.jpg"></body></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(page)))
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
	var requests int
	session.getter.client.Transport = offer1688RoundTripper(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.URL.String() != detail || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://detail.1688.com/" {
			t.Errorf("description request: %s headers=%v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Request: r,
			Body: io.NopCloser(strings.NewReader(`var desc='<img src="https://cbu01.alicdn.com/description.jpg">';`))}, nil
	})
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || !got.IsPage || got.Title != "测试商品" || len(got.Items) != 2 || requests != 1 ||
		got.Items[0].URL != "https://cbu01.alicdn.com/main.jpg" || got.Items[1].URL != "https://cbu01.alicdn.com/description.jpg" {
		t.Fatalf("captured offer = %+v, requests=%d, err=%v", got, requests, err)
	}
	if session.PageURL() != target {
		t.Fatalf("captured page URL = %q", session.PageURL())
	}
	for _, item := range got.Items {
		if ref := item.Headers["Referer"]; strings.Contains(ref, "synthetic") {
			t.Fatalf("captured image referer leaked query: %q", ref)
		}
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}
