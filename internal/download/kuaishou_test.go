package download

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"image"
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

const kuaishouTestID = "3xf9dczwg2n9sya"
const kuaishouTestPC = "https://www.kuaishou.com/short-video/" + kuaishouTestID
const kuaishouTestMobile = "https://m.gifshow.com/fw/photo/" + kuaishouTestID
const kuaishouTestState = `{"share":{"result":1,"photo":{"photoId":"5246130780353777480","share_info":"userId=3xtest&photoId=3xf9dczwg2n9sya","photoStatus":0,"photoType":"HORIZONTAL_ATLAS","caption":"测试图集"},"atlas":{"cdn":["p4.a.yximgs.com","p2.a.yximgs.com"],"list":["/ufile/atlas/sample_0.jpg","/ufile/atlas/sample_1.jpg","/ufile/atlas/sample_0.jpg"]}},"recommended":{"result":1,"photo":{"photoId":"123","share_info":"photoId=other"},"atlas":{"cdn":["p4.a.yximgs.com"],"list":["/ufile/atlas/recommended_0.jpg"]}}}`
const kuaishouTestHTML = `<html><head><title>快手</title><script>window.INIT_STATE = ` + kuaishouTestState + `;</script></head><body><img src="https://p4.a.yximgs.com/ufile/atlas/recommended_0.jpg"></body></html>`

type kuaishouRoundTrip func(*http.Request) (*http.Response, error)

func (f kuaishouRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func kuaishouResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestKuaishouRoutes(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{kuaishouTestPC, kuaishouTestID}, {kuaishouTestMobile, kuaishouTestID},
		{"http://m.gifshow.com/fw/photo/" + kuaishouTestID, kuaishouTestID},
		{"https://www.kuaishou.com/profile/3xtest", ""}, {"https://live.kuaishou.com/u/3xtest/" + kuaishouTestID, ""},
		{"https://m.gifshow.com/fw/photo/../private", ""}, {"https://m.gifshow.com/fw/photo/", ""},
		{"https://m.gifshow.com.evil.test/fw/photo/" + kuaishouTestID, ""},
		{"https://user@m.gifshow.com/fw/photo/" + kuaishouTestID, ""}, {"https://m.gifshow.com:8443/fw/photo/" + kuaishouTestID, ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := kuaishouPostID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
}

func TestKuaishouPCAndMobile(t *testing.T) {
	for _, raw := range []string{kuaishouTestPC + "?tracking=private", kuaishouTestMobile + "?tracking=private"} {
		page, _ := url.Parse(raw)
		getter := GetterFunc(func(_ context.Context, target string, _ func(*http.Request)) (*Response, error) {
			if target != kuaishouTestMobile {
				t.Errorf("share request = %s", target)
			}
			return kuaishouResponse(target, 200, kuaishouTestHTML), nil
		})
		got, err := discoverKuaishouPost(context.Background(), getter, page)
		if err == nil || !strings.Contains(err.Error(), "未确认原图") || got.Title != "测试图集" || len(got.Items) != 2 {
			t.Fatalf("product=%+v err=%v", got, err)
		}
		for _, item := range got.Items {
			if !item.RequireImage || item.Metadata["kuaishou_photo_id"] != kuaishouTestID || item.Metadata["image_kind"] != "atlas" || item.Headers["Referer"] != "https://m.gifshow.com/" || item.Headers["Cookie"] != "" || strings.Contains(item.URL, "recommended") {
				t.Errorf("item=%+v", item)
			}
		}
	}
}

func TestKuaishouPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(kuaishouTestMobile)
	for _, tc := range []struct {
		mode string
		want int
	}{
		{"wrong-id", 0}, {"missing-id", 0}, {"duplicate-id", 0}, {"challenge", 0}, {"hidden", 0},
		{"video", 0}, {"unverified-type", 0}, {"bad-host", 0}, {"bad-path", 1}, {"broken-image", 1}, {"empty-shell", 0}, {"cancel", 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := kuaishouTestHTML
			switch tc.mode {
			case "wrong-id":
				body = strings.Replace(body, "photoId="+kuaishouTestID, "photoId=wrong", 1)
			case "missing-id":
				body = strings.Replace(body, "photoId="+kuaishouTestID, "", 1)
			case "duplicate-id":
				body = strings.Replace(body, "photoId="+kuaishouTestID, "photoId="+kuaishouTestID+"&photoId="+kuaishouTestID, 1)
			case "challenge":
				body += `<div class="slider-bg-tip"></div>`
			case "hidden":
				body = strings.Replace(body, `"photoStatus":0`, `"photoStatus":1`, 1)
			case "video":
				body = strings.Replace(body, `"photoType":"HORIZONTAL_ATLAS"`, `"photoType":"VIDEO"`, 1)
			case "unverified-type":
				body = strings.Replace(body, `"photoType":"HORIZONTAL_ATLAS"`, `"photoType":"VERTICAL_ATLAS"`, 1)
			case "bad-host":
				body = strings.Replace(body, `"p4.a.yximgs.com","p2.a.yximgs.com"`, `"p4.a.yximgs.com.evil.test"`, 1)
			case "bad-path":
				body = strings.ReplaceAll(body, "/ufile/atlas/sample_0.jpg", "/ufile/atlas/../private.jpg")
			case "broken-image":
				body = strings.ReplaceAll(body, `"/ufile/atlas/sample_0.jpg"`, `123`)
			case "empty-shell":
				body = `<title>快手</title><img src="https://p4.a.yximgs.com/ufile/atlas/recommended_0.jpg">`
			case "cancel":
				cancel()
			}
			doc, err := html.Parse(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			got, err := kuaishouMobilePage(ctx, doc, page)
			if err == nil || len(got.Items) != tc.want || (tc.mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s count=%d err=%v", tc.mode, len(got.Items), err)
			}
			for _, item := range got.Items {
				if strings.Contains(item.URL, "evil.test") || strings.Contains(item.URL, "recommended") {
					t.Fatalf("unsafe=%+v", item)
				}
			}
		})
	}
}

func TestKuaishouRedirectsAndErrors(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade", "forbidden", "rate-limit"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			page, _ := url.Parse(kuaishouTestMobile)
			jar, _ := cookiejar.New(nil)
			jar.SetCookies(page, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			calls := 0
			getter.client.Transport = kuaishouRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("credentials sent to %s", r.URL.Host)
				}
				status, body := 200, kuaishouTestHTML
				headers := http.Header{"Content-Type": {"text/html"}}
				if calls == 1 {
					switch mode {
					case "forbidden":
						status, body = 403, ""
					case "rate-limit":
						status, body = 429, ""
					default:
						status, body = 302, ""
						target := kuaishouTestMobile + "?source=redirect"
						switch mode {
						case "wrong-id":
							target = "https://m.gifshow.com/fw/photo/other"
						case "cross-origin":
							target = "https://evil.test/fw/photo/" + kuaishouTestID
						case "downgrade":
							target = "http://m.gifshow.com/fw/photo/" + kuaishouTestID
						}
						headers.Set("Location", target)
					}
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := discoverKuaishouPost(context.Background(), getter, page)
			if mode == "success" {
				if err == nil || !strings.Contains(err.Error(), "未确认原图") || len(got.Items) != 2 || calls != 2 {
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

func TestKuaishouRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == kuaishouTestPC {
				body := `<html><body><img src="https://p4.a.yximgs.com/ufile/atlas/recommended_0.jpg"></body></html>`
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
	session.start, _ = url.Parse(kuaishouTestPC)
	if err := session.run(ctx, camoufox.Navigate(kuaishouTestPC)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".kuaishou.com").WithURL("https://www.kuaishou.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = kuaishouRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.kuaishou.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		switch r.URL.Host {
		case "m.gifshow.com":
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(kuaishouTestHTML)), Request: r}, nil
		case "p4.a.yximgs.com":
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
		default:
			t.Errorf("unexpected host %s", r.URL.Host)
			return nil, errors.New("unexpected host")
		}
	})
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "未确认原图") || len(got.Items) != 2 || got.Items[0].URL != "https://p4.a.yximgs.com/ufile/atlas/sample_0.jpg" || session.PageURL() != kuaishouTestPC {
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

func TestKuaishouSavedPublicPage(t *testing.T) {
	path := os.Getenv("TULAI_KUAISHOU_SAMPLE")
	if path == "" {
		t.Skip("set TULAI_KUAISHOU_SAMPLE to a saved public share page")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc, err := html.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse(kuaishouTestMobile)
	got, err := kuaishouMobilePage(context.Background(), doc, page)
	if err == nil || !strings.Contains(err.Error(), "未确认原图") || len(got.Items) != 4 {
		t.Fatalf("public sample items=%d err=%v", len(got.Items), err)
	}
	if os.Getenv("TULAI_KUAISHOU_DOWNLOAD") == "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		for _, index := range []int{0, len(got.Items) - 1} {
			item := got.Items[index]
			item.Filename = "image"
			dir := t.TempDir()
			if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
				t.Fatalf("public image %d download: %v", index, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 {
				t.Fatalf("image %d files=%v err=%v", index, entries, err)
			}
			if index == 0 {
				file, err := os.Open(filepath.Join(dir, entries[0].Name()))
				if err != nil {
					t.Fatal(err)
				}
				config, _, err := image.DecodeConfig(file)
				file.Close()
				if err != nil || config.Width != 1080 || config.Height != 1440 {
					t.Fatalf("page picture dimensions=%dx%d err=%v", config.Width, config.Height, err)
				}
			}
		}
	}
}

func TestKuaishouSavedPublicVideo(t *testing.T) {
	path := os.Getenv("TULAI_KUAISHOU_VIDEO_SAMPLE")
	if path == "" {
		t.Skip("set TULAI_KUAISHOU_VIDEO_SAMPLE to a saved public video page")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc, err := html.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse("https://m.gifshow.com/fw/photo/3x2cvn2u7fq8k8s")
	got, err := kuaishouMobilePage(context.Background(), doc, page)
	if err == nil || !strings.Contains(err.Error(), "视频") || len(got.Items) != 0 {
		t.Fatalf("video result items=%d err=%v", len(got.Items), err)
	}
}

func TestKuaishouCurrentPublicPage(t *testing.T) {
	if os.Getenv("TULAI_KUAISHOU_LIVE") != "1" {
		t.Skip("set TULAI_KUAISHOU_LIVE=1 for anonymous public share page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	page, _ := url.Parse(kuaishouTestPC)
	got, err := discoverKuaishouPost(ctx, NewHTTPGetter(""), page)
	if err == nil || !strings.Contains(err.Error(), "未确认原图") || len(got.Items) != 4 {
		t.Fatalf("current public product items=%d err=%v", len(got.Items), err)
	}
}
