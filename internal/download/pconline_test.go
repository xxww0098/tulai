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

const pconlineTestID = "3476333"
const pconlineTestSingle = "https://dp.pconline.com.cn/photo/3476333.html"
const pconlineTestGroup = "https://dp.pconline.com.cn/photo/list_3476333.html"
const pconlineTestImage1 = "https://img.pconline.com.cn/images/upload/upc/tx/photoblog/1502/01/c8/one_mthumb.jpg"
const pconlineTestImage2 = "https://img.pconline.com.cn/images/upload/upc/tx/photoblog/1502/01/c8/two_mthumb.jpg"
const pconlineTestGroupHTML = `<html><head><link rel="canonical" href="https://dp.pconline.com.cn/photo/list_3476333.html"></head><body><h1>测试作品</h1><span class="photo-gallery-count">1 / 2</span>
<div class="photo-group-modal"><div class="photo-group-modal-stage"><section class="photo-group-modal-slide" data-index="0"><img class="photo-group-modal-image" src="` + pconlineTestImage1 + `"></section><section class="photo-group-modal-slide" data-index="1"><img class="photo-group-modal-image" src="` + pconlineTestImage2 + `"></section></div></div>
<img class="photo-group-modal-image" src="https://img.pconline.com.cn/images/upload/upc/tx/photoblog/recommended_mthumb.jpg"><img src="https://img.pconline.com.cn/images/upload/upc/tx/photoblog/avatar_mthumb.jpg"></body></html>`
const pconlineTestSingleHTML = `<html><head><link rel="canonical" href="https://dp.pconline.com.cn/photo/3476333.html"></head><body><h1>测试作品</h1><span class="photo-gallery-count">1 / 2</span><section class="photo-gallery"><div class="photo-gallery-stage"><img class="photo-gallery-image" src="` + pconlineTestImage1 + `"></div></section></body></html>`

type pconlineRoundTrip func(*http.Request) (*http.Response, error)

func (f pconlineRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func pconlineResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestPconlineRoutesAndImages(t *testing.T) {
	for _, tc := range []struct {
		raw, id string
		group   bool
	}{
		{pconlineTestSingle, pconlineTestID, false}, {pconlineTestGroup, pconlineTestID, true},
		{"http://dp.pconline.com.cn/photo/5246367.html", "5246367", false},
		{"https://dp.pconline.com.cn/photo/list_0.html", "", false}, {"https://dp.pconline.com.cn/photo/3476333/", "", false},
		{"https://dp.pconline.com.cn.evil.test/photo/3476333.html", "", false},
		{"https://user@dp.pconline.com.cn/photo/3476333.html", "", false},
		{"https://dp.pconline.com.cn:8443/photo/3476333.html", "", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if id, group := pconlinePhotoID(u); id != tc.id || group != tc.group {
			t.Errorf("route %s = %q group=%t", tc.raw, id, group)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{pconlineTestImage1, pconlineTestImage1}, {pconlineTestImage1 + "#fragment", pconlineTestImage1},
		{strings.Replace(pconlineTestImage1, "_mthumb.jpg", ".jpg", 1), ""},
		{strings.Replace(pconlineTestImage1, "_mthumb.jpg", "_thumb.jpg", 1), ""},
		{strings.Replace(pconlineTestImage1, "img.pconline.com.cn", "img.pconline.com.cn.evil.test", 1), ""},
		{strings.Replace(pconlineTestImage1, "https://", "http://", 1), ""},
		{"https://user@img.pconline.com.cn/images/upload/upc/tx/photoblog/x_mthumb.jpg", ""},
		{"https://img.pconline.com.cn:8443/images/upload/upc/tx/photoblog/x_mthumb.jpg", ""},
		{"https://img.pconline.com.cn/images/upload/upc/tx/photoblog/a/%2e%2e/x_mthumb.jpg", ""},
	} {
		got, ok := pconlineImageURL(tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q ok=%t", tc.raw, got, ok)
		}
	}
}

func TestPconlineGroupAndFallback(t *testing.T) {
	page, _ := url.Parse(pconlineTestSingle + "?tracking=private")
	getter := GetterFunc(func(_ context.Context, target string, _ func(*http.Request)) (*Response, error) {
		if target != pconlineTestGroup {
			t.Errorf("group request=%s", target)
		}
		return pconlineResponse(target, 200, pconlineTestGroupHTML), nil
	})
	got, err := discoverPconlinePhoto(context.Background(), getter, page)
	if err == nil || !strings.Contains(err.Error(), "页面展示图") || got.Title != "测试作品" || len(got.Items) != 2 {
		t.Fatalf("group=%+v err=%v", got, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["pconline_photo_id"] != pconlineTestID || item.Metadata["image_kind"] != "display" || item.Headers["Referer"] != "https://dp.pconline.com.cn/" || item.Headers["Cookie"] != "" || strings.Contains(item.URL, "recommended") || strings.Contains(item.URL, "avatar") {
			t.Errorf("item=%+v", item)
		}
	}
	doc, err := html.Parse(strings.NewReader(pconlineTestSingleHTML))
	if err != nil {
		t.Fatal(err)
	}
	broken := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("network failure")
	})
	got, err = pconlineCollect(context.Background(), broken, page, doc)
	if err == nil || !strings.Contains(err.Error(), "整组作品读取失败") || len(got.Items) != 1 || got.Items[0].URL != pconlineTestImage1 {
		t.Fatalf("partial=%+v err=%v", got, err)
	}
}

func TestPconlinePartialAndSafety(t *testing.T) {
	page, _ := url.Parse(pconlineTestGroup)
	for _, tc := range []struct {
		mode string
		want int
	}{
		{"wrong-id", 0}, {"missing-id", 0}, {"duplicate-id", 0}, {"challenge", 0}, {"bad-host", 1},
		{"bad-index", 2}, {"missing-image", 1}, {"original", 1}, {"empty-shell", 0}, {"cancel", 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := pconlineTestGroupHTML
			switch tc.mode {
			case "wrong-id":
				body = strings.Replace(body, "list_3476333.html", "list_3476334.html", 1)
			case "missing-id":
				body = strings.Replace(body, `<link rel="canonical" href="https://dp.pconline.com.cn/photo/list_3476333.html">`, "", 1)
			case "duplicate-id":
				body = strings.Replace(body, "</head>", `<link rel="canonical" href="https://dp.pconline.com.cn/photo/list_3476334.html"></head>`, 1)
			case "challenge":
				body = strings.Replace(body, "</head>", "<title>安全验证</title></head>", 1)
			case "bad-host":
				body = strings.Replace(body, pconlineTestImage2, "https://evil.test/two_mthumb.jpg", 1)
			case "bad-index":
				body = strings.Replace(body, `data-index="1"`, `data-index="2"`, 1)
			case "missing-image":
				body = strings.Replace(body, `<img class="photo-group-modal-image" src="`+pconlineTestImage2+`">`, "", 1)
			case "original":
				body = strings.Replace(body, pconlineTestImage2, strings.Replace(pconlineTestImage2, "_mthumb.jpg", ".jpg", 1), 1)
			case "empty-shell":
				body = `<title>摄影部落</title><img src="` + pconlineTestImage1 + `">`
			case "cancel":
				cancel()
			}
			doc, err := html.Parse(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			got, err := pconlinePhotoPage(ctx, doc, page)
			if err == nil || len(got.Items) != tc.want || (tc.mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s count=%d err=%v", tc.mode, len(got.Items), err)
			}
			if (tc.mode == "bad-index" || tc.mode == "missing-image" || tc.mode == "original") && !strings.Contains(err.Error(), "不完整") {
				t.Fatalf("missing partial warning: %v", err)
			}
		})
	}
}

func TestPconlineRedirects(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade", "forbidden", "rate-limit"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			page, _ := url.Parse(pconlineTestGroup)
			jar, _ := cookiejar.New(nil)
			jar.SetCookies(page, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			calls := 0
			getter.client.Transport = pconlineRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("credentials sent to %s", r.URL.Host)
				}
				status, body := 200, pconlineTestGroupHTML
				headers := http.Header{"Content-Type": {"text/html"}}
				if calls == 1 {
					switch mode {
					case "forbidden":
						status, body = 403, ""
					case "rate-limit":
						status, body = 429, ""
					default:
						status, body = 302, ""
						target := pconlineTestGroup + "?source=redirect"
						switch mode {
						case "wrong-id":
							target = "https://dp.pconline.com.cn/photo/list_3476334.html"
						case "cross-origin":
							target = "https://evil.test/photo/list_3476333.html"
						case "downgrade":
							target = "http://dp.pconline.com.cn/photo/list_3476333.html"
						}
						headers.Set("Location", target)
					}
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := discoverPconlinePhoto(context.Background(), getter, page)
			if mode == "success" {
				if err == nil || len(got.Items) != 2 || calls != 2 {
					t.Fatalf("redirect=%+v calls=%d err=%v", got, calls, err)
				}
			} else if err == nil || calls != 1 || len(got.Items) != 0 {
				t.Fatalf("unsafe=%+v calls=%d err=%v", got, calls, err)
			} else if mode == "forbidden" && !strings.Contains(err.Error(), "登录或安全验证") {
				t.Fatalf("missing login message: %v", err)
			} else if mode == "rate-limit" && !strings.Contains(err.Error(), "限流") {
				t.Fatalf("missing rate-limit message: %v", err)
			}
		})
	}
}

func TestPconlineRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == pconlineTestSingle {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(pconlineTestSingleHTML)))
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
	session.start, _ = url.Parse(pconlineTestSingle)
	if err := session.run(ctx, camoufox.Navigate(pconlineTestSingle)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".pconline.com.cn").WithURL("https://dp.pconline.com.cn/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = pconlineRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		switch r.URL.Host {
		case "dp.pconline.com.cn":
			if r.URL.String() != pconlineTestGroup || !strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example") {
				t.Errorf("same-origin group request: %s %v", r.URL, r.Header)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(pconlineTestGroupHTML)), Request: r}, nil
		case "img.pconline.com.cn":
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://dp.pconline.com.cn/" {
				t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
		default:
			t.Errorf("unexpected host %s", r.URL.Host)
			return nil, errors.New("unexpected host")
		}
	})
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "页面展示图") || len(got.Items) != 2 || session.PageURL() != pconlineTestSingle {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestPconlineSavedPublicPages(t *testing.T) {
	root := os.Getenv("TULAI_PCONLINE_SAMPLES")
	if root == "" {
		t.Skip("set TULAI_PCONLINE_SAMPLES to the saved public pages directory")
	}
	for _, tc := range []struct {
		file, id string
		want     int
	}{{"page0.html", "3476333", 4}, {"page4.html", "5246367", 13}, {"page5.html", "5246293", 3}} {
		file, err := os.Open(filepath.Join(root, tc.file))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := html.Parse(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		got, err := pconlinePhotoPage(context.Background(), doc, pconlineGroupURL(tc.id))
		if err == nil || !strings.Contains(err.Error(), "页面展示图") || strings.Contains(err.Error(), "不完整") || len(got.Items) != tc.want {
			t.Fatalf("sample %s items=%d err=%v", tc.id, len(got.Items), err)
		}
	}
}

func TestPconlineCurrentPublic(t *testing.T) {
	if os.Getenv("TULAI_PCONLINE_LIVE") != "1" {
		t.Skip("set TULAI_PCONLINE_LIVE=1 for a public album and image download")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	page, _ := url.Parse(pconlineTestSingle)
	getter := NewHTTPGetter("")
	got, err := discoverPconlinePhoto(ctx, getter, page)
	if err == nil || !strings.Contains(err.Error(), "页面展示图") || len(got.Items) != 4 {
		t.Fatalf("public group items=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "display"
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("public display image download: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("files=%v err=%v", entries, err)
	}
}

func TestPconlineRenderedPublic(t *testing.T) {
	if os.Getenv("TULAI_PCONLINE_BROWSER_LIVE") != "1" {
		t.Skip("set TULAI_PCONLINE_BROWSER_LIVE=1 for an isolated public browser session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), pconlineTestSingle, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "页面展示图") || len(got.Items) != 4 || session.PageURL() != pconlineTestSingle {
		t.Fatalf("public capture items=%d page=%s err=%v", len(got.Items), session.PageURL(), err)
	}
	item := got.Items[0]
	item.Filename = "display"
	dir := t.TempDir()
	if err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		if !strings.Contains(err.Error(), "网页图片不能访问其他内网地址") {
			t.Fatalf("public browser image download: %v", err)
		}
		t.Log("browser image transfer blocked by the private/fake-IP DNS boundary; testing direct transfer separately")
		if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
			t.Fatalf("direct public image download: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("files=%v err=%v", entries, err)
	}
}
