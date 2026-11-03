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
	"github.com/chromedp/chromedp"
)

const zcoolTestPageURL = "https://www.zcool.com.cn/work/ZMTIz.html"

type zcoolRoundTrip func(*http.Request) (*http.Response, error)

func (fn zcoolRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func zcoolResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestZcoolRoutesAndImageAddresses(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://www.zcool.com.cn/work/ZNzQzMzU1MjA%3D.html", true},
		{"http://zcool.com.cn/work/ZMTIz.html", true},
		{"https://user@www.zcool.com.cn/work/ZMTIz.html", false},
		{"https://www.zcool.com.cn:8443/work/ZMTIz.html", false},
		{"https://www.zcool.com.cn.evil.test/work/ZMTIz.html", false},
		{"https://www.zcool.com.cn/article/ZMTIz.html", false},
		{"https://www.zcool.com.cn/work/ZMTIz.html/extra", false},
		{"https://www.zcool.com.cn/work/ZYWJj.html", false},
		{"https://www.zcool.com.cn/work/ZMA==.html", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isZcoolWork(u); got != tc.want {
			t.Errorf("route %q = %t", tc.raw, got)
		}
	}
	page, _ := url.Parse(zcoolTestPageURL)
	for _, tc := range []struct{ raw, want string }{
		{"https://img.zcool.cn/community/a.jpg?k=sig&t=time&x-oss-process=resize%2Fw_500", "https://img.zcool.cn/community/a.jpg?k=sig&t=time&x-oss-process=resize%2Fw_500"},
		{"http://img.zcool.cn/community/a.jpg?token=x", "https://img.zcool.cn/community/a.jpg?token=x"},
		{"https://img.zcool.cn.evil.test/community/a.jpg", ""},
		{"https://user@img.zcool.cn/community/a.jpg", ""},
		{"https://img.zcool.cn:8443/community/a.jpg", ""},
		{"https://img.zcool.cn/community/../private.jpg", ""},
		{"https://img.zcool.cn/elsewhere/a.jpg", ""},
	} {
		got, ok := zcoolImageURL(page, tc.raw)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("image %q = %q, %t", tc.raw, got, ok)
		}
	}
}

func TestZcoolNextDataImages(t *testing.T) {
	root, _ := url.Parse(zcoolTestPageURL + "?private=do-not-forward")
	const page = `<html><title>fallback title</title><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"data":{"title":"作品标题","productImages":[{"urlBig":"https://img.zcool.cn/community/1.jpg?k=sig&t=time&x-oss-process=resize"},{"urlBig":"https://img.zcool.cn/community/2.jpg"},{"urlBig":"https://img.zcool.cn/community/2.jpg"}]},"related":{"urlBig":"https://img.zcool.cn/community/recommended.jpg"}}}}</script><div class="workShowBox"><img src="https://img.zcool.cn/community/small.jpg"></div><img src="https://img.zcool.cn/community/avatar.jpg"></html>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return zcoolResponse(raw, page), nil
	})
	got, err := discoverZcoolWork(context.Background(), getter, root)
	if err != nil || got.Title != "作品标题" || len(got.Items) != 2 || got.Items[0].URL != "https://img.zcool.cn/community/1.jpg?k=sig&t=time&x-oss-process=resize" || got.Items[1].URL != "https://img.zcool.cn/community/2.jpg" {
		t.Fatalf("work = %+v, err = %v", got, err)
	}
	for _, item := range got.Items {
		if item.Headers["Referer"] != "https://www.zcool.com.cn/" || item.Headers["Cookie"] != "" || !item.RequireImage || item.Metadata["zcool_work"] != "ZMTIz" {
			t.Fatalf("item = %+v", item)
		}
	}
}

func TestZcoolDiscoverDispatchAndNormalize(t *testing.T) {
	for _, raw := range []string{
		"http://zcool.com.cn/work/ZMTIz.html",
		"https://zcool.com.cn/work/ZMTIz.html",
	} {
		t.Run(raw, func(t *testing.T) {
			getter := NewHTTPGetter("")
			getter.client.Transport = zcoolRoundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != zcoolTestPageURL {
					t.Errorf("incorrect canonical URL: %s", r.URL)
				}
				// A generic page scan would wrongly return this unrelated image.
				body := `<html><textarea id="renderData">security check</textarea><img src="https://img.zcool.cn/community/recommended.jpg"></html>`
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			got, err := Discover(context.Background(), getter, raw, 1)
			if err == nil || !got.IsPage || len(got.Items) != 0 {
				t.Fatalf("discovery=%+v err=%v", got, err)
			}
		})
	}
}

func TestZcoolFallbackPartialAndRedirect(t *testing.T) {
	root, _ := url.Parse(zcoolTestPageURL)
	for _, mode := range []string{"fallback", "invalid-image", "invalid-data", "invalid-list", "empty", "other-origin", "other-work", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				body := `<html><title>work</title><div class="workShowBox"><img src="https://img.zcool.cn/community/a.jpg"></div><img src="https://img.zcool.cn/community/recommend.jpg"></html>`
				switch mode {
				case "invalid-image":
					body = strings.Replace(body, `</div>`, `<img src="https://evil.test/community/b.jpg"></div>`, 1)
				case "invalid-data":
					body = strings.Replace(body, `</title>`, `</title><script id="__NEXT_DATA__">invalid</script>`, 1)
				case "invalid-list":
					body = strings.Replace(body, `</title>`, `</title><script id="__NEXT_DATA__">{"props":{"pageProps":{"data":{"productImages":{}}}}}</script>`, 1)
				case "empty":
					body = `<html><textarea id="renderData">security check</textarea></html>`
				case "cancel":
					cancel()
				}
				resp := zcoolResponse(raw, body)
				if mode == "other-origin" {
					resp.FinalURL = "https://evil.test/work/ZMTIz.html"
				} else if mode == "other-work" {
					resp.FinalURL = "https://www.zcool.com.cn/work/ZMTI0.html"
				}
				return resp, nil
			})
			got, err := discoverZcoolWork(ctx, getter, root)
			want := 1
			if mode == "empty" || mode == "other-origin" || mode == "other-work" || mode == "cancel" {
				want = 0
			}
			if len(got.Items) != want || (mode == "fallback") != (err == nil) || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode %s: items=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestZcoolImageLimit(t *testing.T) {
	root, _ := url.Parse(zcoolTestPageURL)
	var list strings.Builder
	for i := 0; i < maxZcoolImages+1; i++ {
		if i > 0 {
			list.WriteByte(',')
		}
		fmt.Fprintf(&list, `{"urlBig":"https://img.zcool.cn/community/%d.jpg"}`, i)
	}
	body := `<script id="__NEXT_DATA__">{"props":{"pageProps":{"data":{"productImages":[` + list.String() + `]}}}}</script>`
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return zcoolResponse(raw, body), nil
	})
	got, err := discoverZcoolWork(context.Background(), getter, root)
	if err == nil || len(got.Items) != maxZcoolImages {
		t.Fatalf("images=%d err=%v", len(got.Items), err)
	}
}

func TestZcoolRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<html><title>synthetic start</title></html>`)
	}))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	const page = `<html><head><title>synthetic work</title><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"data":{"productImages":[{"urlBig":"https://img.zcool.cn/community/a.jpg?k=sig"},{"urlBig":"https://img.zcool.cn/community/b.jpg"}]},"related":{"urlBig":"https://img.zcool.cn/community/recommended.jpg"}}}}</script></head><body><img src="https://img.zcool.cn/community/avatar.jpg"></body></html>`
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == zcoolTestPageURL {
				action = fetch.FulfillRequest(paused.RequestID, 200).
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
	session.start, _ = url.Parse(zcoolTestPageURL)
	if err := session.run(ctx, camoufox.Navigate(zcoolTestPageURL)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.zcool.com.cn/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || session.PageURL() != zcoolTestPageURL || len(got.Items) != 2 || got.Items[0].URL != "https://img.zcool.cn/community/a.jpg?k=sig" || got.Items[1].URL != "https://img.zcool.cn/community/b.jpg" {
		t.Fatalf("captured=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	session.getter.client.Transport = zcoolRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "img.zcool.cn" || r.URL.RawQuery != "k=sig" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.zcool.com.cn/" {
			t.Errorf("unsafe CDN request: %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, func(r *http.Request) {
		r.Header.Set("Referer", got.Items[0].Headers["Referer"])
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case err := <-responses:
		t.Fatalf("intercept response failed: %v", err)
	default:
	}
}

func TestZcoolLiveWork(t *testing.T) {
	if os.Getenv("TULAI_ZCOOL_LIVE") != "1" {
		t.Skip("set TULAI_ZCOOL_LIVE=1 for a public Zcool work")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root, _ := url.Parse("https://www.zcool.com.cn/work/ZNzQzMzU1MjA=.html")
	getter := NewHTTPGetter("")
	challenge := false
	inspectingGetter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		resp, err := getter.Get(ctx, raw, prepare)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxPageBytes+1))
		resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		challenge = strings.Contains(string(body), `id="renderData"`)
		resp.Body = io.NopCloser(strings.NewReader(string(body)))
		return resp, nil
	})
	got, err := discoverZcoolWork(ctx, inspectingGetter, root)
	if err != nil && len(got.Items) == 0 && challenge {
		t.Skipf("public work blocked by site verification: %v", err)
	}
	if err != nil || len(got.Items) == 0 {
		t.Fatalf("public work images=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	err = New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil)
	entries, readErr := os.ReadDir(dir)
	if err != nil || readErr != nil || len(entries) != 1 {
		t.Fatalf("public work image err=%v entries=%v read=%v", err, entries, readErr)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("public image empty: %v %v", info, err)
	}
	t.Logf("public work images=%d", len(got.Items))
}
