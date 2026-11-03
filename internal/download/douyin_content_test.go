package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

const douyinContentTestID = "7630302640823912635"
const douyinContentTestPC = "https://www.douyin.com/note/" + douyinContentTestID
const douyinContentTestShare = "https://www.iesdouyin.com/share/note/" + douyinContentTestID + "/"
const douyinContentRouter = `{"loaderData":{"video_(id)/page":{"videoInfoRes":{"item_list":[{"aweme_id":"7630302640823912635","desc":"测试图文","status":{"private_status":0,"is_delete":false},"images":[{"url_list":["https://p3.douyinpic.com/image/thumb.jpg","https://p3-sign.douyinpic.com/image/full.jpg?signature=keep"]},{"url_list":["https://p3.douyinpic.com/image/two.jpg"]}],"author":{"avatar_thumb":{"url_list":["https://p3.douyinpic.com/image/avatar.jpg"]}}},{"aweme_id":"7630302640823912636","images":[{"url_list":["https://p3.douyinpic.com/image/recommended.jpg"]}]}]}}}}`
const douyinContentRouterHTML = `<html><script>window._ROUTER_DATA = ` + douyinContentRouter + `;</script><body><img src="https://p3.douyinpic.com/image/recommended.jpg"></body></html>`

func douyinContentPaceHTML(detail string) string {
	payload := `1:{"aweme":{"detail":` + detail + `}}`
	encoded, _ := json.Marshal(payload)
	return `<html><script>self.__pace_f.push([1,` + string(encoded) + `])</script><body><img src="https://p3.douyinpic.com/image/recommended.jpg"></body></html>`
}

type douyinContentRoundTrip func(*http.Request) (*http.Response, error)

func (f douyinContentRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func douyinContentResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDouyinContentRoutesAndImages(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{douyinContentTestPC, douyinContentTestID}, {douyinContentTestShare, douyinContentTestID},
		{"https://www.douyin.com/video/" + douyinContentTestID, douyinContentTestID},
		{"https://www.iesdouyin.com/share/video/" + douyinContentTestID + "/", douyinContentTestID},
		{"https://www.douyin.com/user/" + douyinContentTestID, ""}, {"https://www.douyin.com/note/123", ""},
		{"https://www.douyin.com.evil.test/note/" + douyinContentTestID, ""},
		{"https://user@www.douyin.com/note/" + douyinContentTestID, ""}, {"https://www.douyin.com:8443/note/" + douyinContentTestID, ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := douyinContentID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	for _, tc := range []struct{ raw, want string }{
		{"https://p3-sign.douyinpic.com/image/full.jpg?signature=keep", "https://p3-sign.douyinpic.com/image/full.jpg?signature=keep"},
		{"https://p3.douyinpic.com/image/full.jpg#x", "https://p3.douyinpic.com/image/full.jpg"},
		{"http://p3.douyinpic.com/image.jpg", ""}, {"https://p3.douyinpic.com.evil.test/image.jpg", ""},
		{"https://user@p3.douyinpic.com/image.jpg", ""}, {"https://p3.douyinpic.com:8443/image.jpg", ""},
		{"https://p3.douyinpic.com/a/%2e%2e/private.jpg", ""}, {"data:image/png;base64,AA", ""},
	} {
		got, ok := douyinContentImageURL(tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %s = %q ok=%t", tc.raw, got, ok)
		}
	}
}

func TestDouyinContentShareAndPace(t *testing.T) {
	page, _ := url.Parse(douyinContentTestShare + "?tracking=private")
	getter := GetterFunc(func(_ context.Context, target string, _ func(*http.Request)) (*Response, error) {
		if target != douyinContentTestShare {
			t.Errorf("unexpected URL %s", target)
		}
		return douyinContentResponse(target, 200, douyinContentRouterHTML), nil
	})
	got, err := discoverDouyinContent(context.Background(), getter, page)
	if err != nil || got.Title != "测试图文" || len(got.Items) != 2 || got.Items[0].URL != "https://p3-sign.douyinpic.com/image/full.jpg?signature=keep" {
		t.Fatalf("router product=%+v err=%v", got, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || item.Metadata["douyin_aweme_id"] != douyinContentTestID || item.Headers["Referer"] != "https://www.iesdouyin.com/" || item.Headers["Cookie"] != "" || strings.Contains(item.URL, "recommended") || strings.Contains(item.URL, "avatar") {
			t.Errorf("item=%+v", item)
		}
	}
	detail := `{"awemeId":"7630302640823912635","desc":"PC 图文","images":[{"urlList":["https://p3.douyinpic.com/image/pc.jpg"]}]}`
	doc, err := html.Parse(strings.NewReader(douyinContentPaceHTML(detail)))
	if err != nil {
		t.Fatal(err)
	}
	pc, _ := url.Parse(douyinContentTestPC)
	got, err = douyinContentPage(context.Background(), doc, pc)
	if err != nil || got.Title != "PC 图文" || len(got.Items) != 1 || got.Items[0].URL != "https://p3.douyinpic.com/image/pc.jpg" {
		t.Fatalf("pace product=%+v err=%v", got, err)
	}
}

func TestDouyinContentPaceChunks(t *testing.T) {
	payload := `1:{"aweme":{"detail":{"awemeId":"7630302640823912635","images":[{"urlList":["https://p3.douyinpic.com/image/pc.jpg"]}]}}}`
	first, _ := json.Marshal(payload[:len(payload)/2])
	second, _ := json.Marshal(payload[len(payload)/2:])
	body := `<script>self.__pace_f.push([0])</script><script>self.__pace_f.push([1,` + string(first) + `])</script><script>self.__pace_f.push([1,` + string(second) + `])</script>`
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse(douyinContentTestPC)
	got, err := douyinContentPage(context.Background(), doc, page)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://p3.douyinpic.com/image/pc.jpg" {
		t.Fatalf("split pace product=%+v err=%v", got, err)
	}
}

func TestDouyinContentPartialAndSafety(t *testing.T) {
	page, _ := url.Parse(douyinContentTestShare)
	for _, tc := range []struct {
		mode string
		want int
	}{
		{"wrong-id", 0}, {"missing-id", 0}, {"duplicate-match", 0}, {"challenge", 0}, {"login", 0}, {"private", 0}, {"video", 0},
		{"bad-host", 1}, {"broken-item", 2}, {"live-photo", 2}, {"empty-shell", 0}, {"cancel", 0},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			body := douyinContentRouterHTML
			switch tc.mode {
			case "wrong-id":
				body = strings.Replace(body, douyinContentTestID, "7630302640823912637", 1)
			case "missing-id":
				body = strings.Replace(body, `"aweme_id":"7630302640823912635",`, "", 1)
			case "duplicate-match":
				body = strings.Replace(body, "7630302640823912636", douyinContentTestID, 1)
			case "challenge":
				body += `<div class="captcha_verify_bar--title"></div>`
			case "login":
				body += `<div id="web-login-container"></div>`
			case "private":
				body = strings.Replace(body, `"private_status":0`, `"private_status":1`, 1)
			case "video":
				body = strings.Replace(body, `"images":[{"url_list":["https://p3.douyinpic.com/image/thumb.jpg","https://p3-sign.douyinpic.com/image/full.jpg?signature=keep"]},{"url_list":["https://p3.douyinpic.com/image/two.jpg"]}]`, `"images":[]`, 1)
			case "bad-host":
				body = strings.Replace(body, "https://p3-sign.douyinpic.com/image/full.jpg?signature=keep", "https://evil.test/image.jpg", 1)
			case "broken-item":
				body = strings.Replace(body, `"images":[{"url_list"`, `"images":[123,{"url_list"`, 1)
			case "live-photo":
				body = strings.Replace(body, `"images":[{"url_list"`, `"images":[{"video":{"playAddr":[]},"url_list"`, 1)
			case "empty-shell":
				body = `<title>抖音</title><img src="https://p3.douyinpic.com/image/recommended.jpg">`
			case "cancel":
				cancel()
			}
			doc, err := html.Parse(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			got, err := douyinContentPage(ctx, doc, page)
			if err == nil || len(got.Items) != tc.want || (tc.mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s count=%d err=%v", tc.mode, len(got.Items), err)
			}
			for _, item := range got.Items {
				if strings.Contains(item.URL, "evil.test") || strings.Contains(item.URL, "recommended") || strings.Contains(item.URL, "avatar") {
					t.Fatalf("unsafe=%+v", item)
				}
			}
		})
	}
}

func TestDouyinContentRedirects(t *testing.T) {
	for _, mode := range []string{"success", "wrong-id", "cross-origin", "downgrade", "forbidden", "rate-limit"} {
		t.Run(mode, func(t *testing.T) {
			getter := NewHTTPGetter("")
			page, _ := url.Parse(douyinContentTestShare)
			jar, _ := cookiejar.New(nil)
			jar.SetCookies(page, []*http.Cookie{{Name: "synthetic_session", Value: "secret"}})
			getter.client.Jar = jar
			calls := 0
			getter.client.Transport = douyinContentRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
					t.Errorf("credentials sent to %s", r.URL.Host)
				}
				status, body := 200, douyinContentRouterHTML
				headers := http.Header{"Content-Type": {"text/html"}}
				if calls == 1 {
					switch mode {
					case "forbidden":
						status, body = 403, ""
					case "rate-limit":
						status, body = 429, ""
					default:
						status, body = 302, ""
						target := douyinContentTestShare + "?source=redirect"
						switch mode {
						case "wrong-id":
							target = "https://www.iesdouyin.com/share/note/7630302640823912636/"
						case "cross-origin":
							target = "https://evil.test/share/note/" + douyinContentTestID
						case "downgrade":
							target = "http://www.iesdouyin.com/share/note/" + douyinContentTestID
						}
						headers.Set("Location", target)
					}
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := discoverDouyinContent(context.Background(), getter, page)
			if mode == "success" {
				if err != nil || len(got.Items) != 2 || calls != 2 {
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

func TestDouyinContentRenderedCapture(t *testing.T) {
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
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == douyinContentTestPC {
				body := douyinContentPaceHTML(`{"awemeId":"7630302640823912635","desc":"PC 图文","images":[{"urlList":["https://p3.douyinpic.com/image/pc.jpg"]}]}`)
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
	session.start, _ = url.Parse(douyinContentTestPC)
	if err := session.run(ctx, camoufox.Navigate(douyinContentTestPC)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".douyin.com").WithURL("https://www.douyin.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = douyinContentRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "p3.douyinpic.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.douyin.com/" {
			t.Errorf("cross-origin credentials/referrer: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != "https://p3.douyinpic.com/image/pc.jpg" || session.PageURL() != douyinContentTestPC {
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

func TestDouyinContentSavedEmptyPage(t *testing.T) {
	path := os.Getenv("TULAI_DOUYIN_CONTENT_SAMPLE")
	if path == "" {
		t.Skip("set TULAI_DOUYIN_CONTENT_SAMPLE to a saved public empty share page")
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
	page, _ := url.Parse(douyinContentTestShare)
	got, err := douyinContentPage(context.Background(), doc, page)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("empty public page items=%d err=%v", len(got.Items), err)
	}
}
