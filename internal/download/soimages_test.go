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
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
)

const soImagesSearch = "https://image.so.com/i?q=%E7%8C%AB&color=blue&token=synthetic"
const soImagesFixture = `<html><head><title>Search</title></head><body><script type="text/data" id="initData">{"end":false,"lastindex":2,"ps":2,"pc":2,"sid":"abc123","ras":6,"list":[{"id":"first","title":"猫","img":"https://images.example.com/cat1.jpg","link":"https://source.example.org/post?secret=synthetic"},{"id":"second","img":"https://images.example.com/cat2.jpg","link":"https://source.example.org/post"}]}</script><img src="https://images.example.com/recommendation.jpg"></body></html>`
const soImagesPageTwo = `jQuery130_1({"end":true,"lastindex":3,"ps":3,"pc":1,"list":[{"id":"third","title":"猫咪","img":"https://images.example.com/cat3.jpg","link":"https://source.example.org/"},{"id":"duplicate","img":"https://images.example.com/cat2.jpg"}]});`

func soImagesTestResponse(raw, body, contentType string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestSOImagesSearchURL(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{soImagesSearch, true},
		{"https://image.so.com/i?q=cat", true},
		{"https://image.so.com/i?q=cat&tab=chacha", false},
		{"https://image.so.com/i?q=cat&q=dog", false},
		{"https://image.so.com/i?q=", false},
		{"https://image.so.com/view?q=cat", false},
		{"https://image.so.com.evil.test/i?q=cat", false},
		{"https://user@image.so.com/i?q=cat", false},
		{"https://image.so.com:8443/i?q=cat", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if isSOImageSearch(u) != tc.ok {
			t.Errorf("isSOImageSearch(%q)=%v", tc.raw, isSOImageSearch(u))
		}
	}
}

func TestSOImagesResultBoundary(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/cat.jpg", "http://10.0.0.1/cat.jpg", "http://169.254.169.254/cat.jpg", "http://[::1]/cat.jpg",
		"http://localhost/cat.jpg", "http://camera.local/cat.jpg", "http://printer.internal/cat.jpg",
		"https://user@example.com/cat.jpg", "https://example.com:8443/cat.jpg", "javascript:alert(1)",
	} {
		if _, err := publicImageURL(raw); err == nil {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	if _, err := publicImageURL("https://images.example.com/cat.jpg"); err != nil {
		t.Fatalf("public image rejected: %v", err)
	}
}

func soImagesCheckEndpoint(t *testing.T, raw string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "image.so.com" || u.Path != "/j" {
		t.Fatalf("bad endpoint %q err=%v", raw, err)
	}
	q := u.Query()
	if q.Get("q") != "猫" || q.Get("color") != "blue" || q.Get("sn") != "2" || q.Get("ps") != "2" || q.Get("pc") != "2" || q.Get("callback") != "jQuery130_1" || q.Get("sid") != "abc123" || q.Has("token") {
		t.Fatalf("bad pagination query %v", q)
	}
}

func TestSOImagesPagination(t *testing.T) {
	root, _ := url.Parse(soImagesSearch)
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		if calls == 1 {
			return soImagesTestResponse(raw, soImagesFixture, "text/html"), nil
		}
		soImagesCheckEndpoint(t, raw)
		req, _ := http.NewRequest("GET", raw, nil)
		prepare(req)
		if req.Header.Get("Referer") != "https://image.so.com/" || req.Header.Get("Cookie") != "" {
			t.Fatalf("unexpected request headers %v", req.Header)
		}
		return soImagesTestResponse(raw, soImagesPageTwo, "application/javascript"), nil
	})
	got, err := discoverSOImages(context.Background(), getter, root)
	if err != nil || calls != 2 || len(got.Items) != 3 || got.Title != "猫 - 360 图片" {
		t.Fatalf("images=%+v calls=%d title=%q err=%v", got.Items, calls, got.Title, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || strings.Contains(item.URL, "recommendation") || strings.Contains(item.Headers["Referer"], "secret") || item.Headers["Cookie"] != "" {
			t.Fatalf("unexpected item %+v", item)
		}
	}
	if got.Items[0].Headers["Referer"] != "https://source.example.org/" {
		t.Fatalf("unexpected referer %+v", got.Items[0])
	}
}

func TestSOImagesPartialAndBlocked(t *testing.T) {
	root, _ := url.Parse(soImagesSearch)
	for _, tc := range []struct {
		name, response string
		fail           bool
		count          int
	}{
		{"failed-page", "", true, 2},
		{"bad-jsonp", `jQuery130_1({"end":true,"list":[]});alert(1)`, false, 2},
		{"unsafe-result", `jQuery130_1({"end":true,"list":[{"img":"http://127.0.0.1/x.jpg"}]});`, false, 2},
		{"malformed-result", `jQuery130_1({"end":true,"list":["invalid",{"id":123,"img":"https://images.example.com/cat3.jpg"}]});`, false, 3},
		{"duplicate-page", `jQuery130_1({"end":false,"lastindex":2,"list":[{"img":"https://images.example.com/cat1.jpg"}]});`, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				if calls == 1 {
					return soImagesTestResponse(raw, soImagesFixture, "text/html"), nil
				}
				if tc.fail {
					return nil, errors.New("unavailable")
				}
				return soImagesTestResponse(raw, tc.response, "application/javascript"), nil
			})
			got, err := discoverSOImages(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("images=%+v calls=%d err=%v", got.Items, calls, err)
			}
		})
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return soImagesTestResponse(raw, `<html><img src="https://images.example.com/recommendation.jpg"></html>`, "text/html"), nil
	})
	if got, err := discoverSOImages(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("blocked images=%+v err=%v", got.Items, err)
	}
}

func TestSOImagesRedirectAndLimit(t *testing.T) {
	root, _ := url.Parse(soImagesSearch)
	for _, tc := range []string{"redirect", "large"} {
		t.Run(tc, func(t *testing.T) {
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				if calls == 1 {
					return soImagesTestResponse(raw, soImagesFixture, "text/html"), nil
				}
				if tc == "redirect" {
					resp := soImagesTestResponse(raw, soImagesPageTwo, "application/javascript")
					resp.FinalURL = "https://evil.test/j"
					return resp, nil
				}
				return soImagesTestResponse(raw, strings.Repeat("x", maxPageBytes+1), "application/javascript"), nil
			})
			got, err := discoverSOImages(context.Background(), getter, root)
			if err == nil || len(got.Items) != 2 {
				t.Fatalf("images=%+v err=%v", got.Items, err)
			}
		})
	}
}

func TestSOImagesCancellation(t *testing.T) {
	root, _ := url.Parse(soImagesSearch)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unexpected request after cancellation")
		return nil, nil
	})
	if _, err := discoverSOImages(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

type soImagesRoundTripper func(*http.Request) (*http.Response, error)

func (fn soImagesRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestSOImagesRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var calls atomic.Int32
	session.getter.client.Transport = soImagesRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		soImagesCheckEndpoint(t, r.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/javascript"}}, Body: io.NopCloser(strings.NewReader(soImagesPageTwo)), Request: r}, nil
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == soImagesSearch {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(soImagesFixture)))
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
	start, _ := url.Parse(soImagesSearch)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(soImagesSearch)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || calls.Load() != 1 {
		t.Fatalf("capture=%+v calls=%d err=%v", got.Items, calls.Load(), err)
	}
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestSOImagesLiveSearch(t *testing.T) {
	if os.Getenv("TULAI_SOIMAGES_LIVE") != "1" {
		t.Skip("set TULAI_SOIMAGES_LIVE=1 to verify public search and one page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client := NewHTTPGetter("")
	calls := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		if calls > 2 {
			return nil, context.Canceled
		}
		return client.Get(ctx, raw, prepare)
	})
	got, err := Discover(ctx, getter, "https://image.so.com/i?q=%E7%8C%AB", 1)
	if !errors.Is(err, context.Canceled) || calls != 3 || len(got.Items) < 200 {
		t.Fatalf("public search images=%d calls=%d err=%v", len(got.Items), calls, err)
	}
	var downloaded bool
	attempts := 0
	for _, candidate := range got.Items {
		if !strings.HasPrefix(candidate.URL, "https://") {
			continue
		}
		attempts++
		if attempts > 4 {
			break
		}
		item := candidate
		item.Filename = "image"
		dir := t.TempDir()
		requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := New(Options{Getter: client}).Download(requestCtx, Job{Directory: dir}, item, nil)
		cancel()
		if err != nil {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("files=%v err=%v", entries, err)
		}
		f, err := os.Open(filepath.Join(dir, entries[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		im, format, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || im.Width == 0 || im.Height == 0 {
			t.Fatalf("image=%+v format=%q err=%v", im, format, err)
		}
		t.Logf("public search images=%d from two pages; first downloaded=%dx%d %s", len(got.Items), im.Width, im.Height, format)
		downloaded = true
		break
	}
	if !downloaded {
		t.Fatal("none of the public search images could be downloaded")
	}
}
