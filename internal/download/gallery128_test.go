package download

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/xxww0098/tulai/internal/camoufox"
	"golang.org/x/net/html"
)

func gallery128TestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func gallery128TestDoc(t *testing.T, raw string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func gallery128FixtureRoot(id string, total int) string {
	return fmt.Sprintf(`<html><body><input id="gallery_id" value="%s"><input id="load_id" value="synthetic"><input id="load_dir" value="001"><input id="load_pages" value="%d"><a href="/view/%s/1/">Read</a></body></html>`, id, total, id)
}

func gallery128FixtureReader(id string, page, total int) string {
	return fmt.Sprintf(`<html><body><input id="u_id" value="%s"><input id="pages" value="%d"><input id="image_dir" value="001"><input id="gallery_id" value="synthetic"><img id="gimg" class="preloader image_%d" src="https://m1.imhentai.xxx/001/synthetic/%d.jpg"></body></html>`, id, total, page, page)
}

func TestGallery128RoutesAndAssets(t *testing.T) {
	for _, tc := range []struct{ raw, id, kind string }{
		{"https://imhentai.xxx/gallery/123/", "123", "gallery"},
		{"http://imhentai.xxx/view/123/2/", "123", "view"},
		{"https://imhentai.xxx/view/123/0/", "", ""},
		{"https://imhentai.xxx/gallery/123/1/", "", ""},
		{"https://www.imhentai.xxx/gallery/123/", "", ""},
		{"https://imhentai.xxx.evil.test/gallery/123/", "", ""},
		{"https://user@imhentai.xxx/gallery/123/", "", ""},
		{"https://imhentai.xxx:444/gallery/123/", "", ""},
		{"https://imhentai.xxx/gallery%2f123/", "", ""},
	} {
		u := gallery128TestURL(t, tc.raw)
		ref := gallery128PageRef(u)
		if ref.id != tc.id || ref.kind != tc.kind || gallery128Key(u) != tc.id {
			t.Errorf("route %q = %+v", tc.raw, ref)
		}
	}
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://m1.imhentai.xxx/001/synthetic/1.jpg", true},
		{"https://m6.imhentai.xxx/001/synthetic/1.webp", true},
		{"https://m11.imhentai.xxx/001/synthetic/1.png", true},
		{"http://m1.imhentai.xxx/001/synthetic/1.jpg", false},
		{"https://user@m1.imhentai.xxx/001/synthetic/1.jpg", false},
		{"https://m1.imhentai.xxx:444/001/synthetic/1.jpg", false},
		{"https://m12.imhentai.xxx/001/synthetic/1.jpg", false},
		{"https://m1.imhentai.xxx.evil.test/001/synthetic/1.jpg", false},
		{"https://m1.imhentai.xxx/001/other/1.jpg", false},
		{"https://m1.imhentai.xxx/001/synthetic/%2e%2e/1.jpg", false},
		{"https://m1.imhentai.xxx/001/synthetic%2f1.jpg", false},
		{"https://m1.imhentai.xxx/001/synthetic/1.mp4", false},
	} {
		_, ok := gallery128Image(tc.raw, "001", "synthetic")
		if ok != tc.want {
			t.Errorf("asset %q accepted=%v", tc.raw, ok)
		}
	}
}

func TestGallery128IdentityAndLazyImage(t *testing.T) {
	page := gallery128TestURL(t, "https://imhentai.xxx/view/123/1/")
	ref := gallery128PageRef(page)
	valid := gallery128FixtureReader("123", 1, 3)
	state, err := gallery128Read(context.Background(), gallery128TestDoc(t, valid), ref)
	if err != nil || state.total != 3 || state.image != "https://m1.imhentai.xxx/001/synthetic/1.jpg" {
		t.Fatalf("reader=%+v err=%v", state, err)
	}
	lazy := strings.Replace(valid, `src="https://m1.imhentai.xxx/001/synthetic/1.jpg"`, `src="data:image/svg+xml,placeholder" data-src="https://m7.imhentai.xxx/001/synthetic/1.png"`, 1)
	if state, err := gallery128Read(context.Background(), gallery128TestDoc(t, lazy), ref); err != nil || !strings.Contains(state.image, "m7.imhentai.xxx") {
		t.Fatalf("lazy=%+v err=%v", state, err)
	}
	for name, changed := range map[string]string{
		"wrong gallery":      strings.Replace(valid, `id="u_id" value="123"`, `id="u_id" value="124"`, 1),
		"wrong page":         strings.Replace(valid, `image_1`, `image_2`, 1),
		"unknown pages":      strings.Replace(valid, `id="pages" value="3"`, `id="pages" value="0"`, 1),
		"duplicate identity": strings.Replace(valid, `</body>`, `<input id="u_id" value="123"></body>`, 1),
		"wrong folder":       strings.Replace(valid, `/001/synthetic/1.jpg`, `/001/other/1.jpg`, 1),
		"unknown host":       strings.Replace(valid, `m1.imhentai.xxx`, `evil.test`, 1),
		"ambiguous image":    strings.Replace(valid, `</body>`, `<img id="gimg" src="https://m1.imhentai.xxx/001/synthetic/2.jpg"></body>`, 1),
	} {
		if state, err := gallery128Read(context.Background(), gallery128TestDoc(t, changed), ref); err == nil || state.image != "" {
			t.Errorf("%s accepted: %+v %v", name, state, err)
		}
	}
	rootPage := gallery128TestURL(t, "https://imhentai.xxx/gallery/123/")
	root := gallery128FixtureRoot("123", 3)
	if _, err := gallery128ReadRoot(context.Background(), gallery128TestDoc(t, root), rootPage); err != nil {
		t.Fatal(err)
	}
	repeated := strings.Replace(root, `</body>`, `<input id="gallery_id" value="123"></body>`, 1)
	if _, err := gallery128ReadRoot(context.Background(), gallery128TestDoc(t, repeated), rootPage); err != nil {
		t.Fatalf("matching duplicate gallery id: %v", err)
	}
	conflicting := strings.Replace(root, `</body>`, `<input id="gallery_id" value="124"></body>`, 1)
	if _, err := gallery128ReadRoot(context.Background(), gallery128TestDoc(t, conflicting), rootPage); err == nil {
		t.Fatal("conflicting gallery ids accepted")
	}
	if _, err := gallery128ReadRoot(context.Background(), gallery128TestDoc(t, strings.ReplaceAll(root, `/view/123/1/`, `/view/124/1/`)), rootPage); err == nil {
		t.Fatal("unrelated reader accepted")
	}
}

func TestGallery128PaginationAndPartialFailure(t *testing.T) {
	root := gallery128TestURL(t, "https://imhentai.xxx/gallery/123/")
	var calls []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		var body string
		switch raw {
		case "https://imhentai.xxx/gallery/123/":
			body = gallery128FixtureRoot("123", 3)
		case "https://imhentai.xxx/view/123/1/":
			body = gallery128FixtureReader("123", 1, 3)
		case "https://imhentai.xxx/view/123/2/":
			body = gallery128FixtureReader("123", 2, 3)
		case "https://imhentai.xxx/view/123/3/":
			body = gallery128FixtureReader("123", 3, 3)
		default:
			t.Fatalf("unexpected request %q", raw)
		}
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got, err := discoverGallery128(context.Background(), getter, root)
	if err != nil || len(got.Items) != 3 || len(calls) != 4 {
		t.Fatalf("pages=%d calls=%v err=%v", len(got.Items), calls, err)
	}
	for i, item := range got.Items {
		if item.Metadata["gallery128_page"] != strconv.Itoa(i+1) || !item.RequireImage || !item.PublicOnly {
			t.Fatalf("item %d = %+v", i, item)
		}
	}
	view := gallery128TestURL(t, "https://imhentai.xxx/view/123/2/")
	calls = nil
	got, err = gallery128Collect(context.Background(), getter, view, gallery128TestDoc(t, gallery128FixtureReader("123", 2, 3)))
	if err != nil || len(got.Items) != 3 || len(calls) != 2 || got.Items[0].Metadata["gallery128_page"] != "1" || got.Items[1].Metadata["gallery128_page"] != "2" {
		t.Fatalf("reader pages=%d calls=%v err=%v", len(got.Items), calls, err)
	}
	for _, status := range []int{403, 429} {
		calls = nil
		blocked := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
			calls = append(calls, raw)
			return &Response{StatusCode: status, Header: http.Header{}, FinalURL: raw, Body: io.NopCloser(strings.NewReader("blocked"))}, nil
		})
		page := gallery128TestURL(t, "https://imhentai.xxx/view/123/1/")
		got, err := gallery128Collect(context.Background(), blocked, page, gallery128TestDoc(t, gallery128FixtureReader("123", 1, 3)))
		if err == nil || !strings.Contains(err.Error(), "不完整") || len(got.Items) != 1 || len(calls) != 1 {
			t.Fatalf("status %d pages=%d calls=%v err=%v", status, len(got.Items), calls, err)
		}
	}
	page := gallery128TestURL(t, "https://imhentai.xxx/view/123/1/")
	for name, changed := range map[string]string{
		"page count changed": gallery128FixtureReader("123", 2, 4),
		"album changed":      strings.ReplaceAll(gallery128FixtureReader("123", 2, 3), "synthetic", "different"),
		"duplicate image":    strings.Replace(gallery128FixtureReader("123", 2, 3), "/2.jpg", "/1.jpg", 1),
	} {
		calls = nil
		bad := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
			calls = append(calls, raw)
			return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(changed))}, nil
		})
		got, err := gallery128Collect(context.Background(), bad, page, gallery128TestDoc(t, gallery128FixtureReader("123", 1, 3)))
		if err == nil || !strings.Contains(err.Error(), "不完整") || len(got.Items) != 1 || len(calls) != 1 {
			t.Errorf("%s pages=%d calls=%d err=%v", name, len(got.Items), len(calls), err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelGetter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		cancel()
		return nil, context.Canceled
	})
	got, err = gallery128Collect(ctx, cancelGetter, page, gallery128TestDoc(t, gallery128FixtureReader("123", 1, 3)))
	if !errors.Is(err, context.Canceled) || len(got.Items) != 1 {
		t.Fatalf("cancel pages=%d err=%v", len(got.Items), err)
	}
	redirect := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: "https://evil.test/view/123/1/", Body: io.NopCloser(strings.NewReader(gallery128FixtureReader("123", 1, 3)))}, nil
	})
	if got, err := discoverGallery128(context.Background(), redirect, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect pages=%d err=%v", len(got.Items), err)
	}
}

func TestGallery128Limit(t *testing.T) {
	page := gallery128TestURL(t, "https://imhentai.xxx/view/123/1/")
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		ref := gallery128PageRef(gallery128TestURL(t, raw))
		body := gallery128FixtureReader("123", ref.page, gallery128MaxPages+1)
		return &Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	got, err := gallery128Collect(context.Background(), getter, page, gallery128TestDoc(t, gallery128FixtureReader("123", 1, gallery128MaxPages+1)))
	if err == nil || !strings.Contains(err.Error(), "上限") || len(got.Items) != gallery128MaxPages {
		t.Fatalf("limit pages=%d err=%v", len(got.Items), err)
	}
}

type gallery128RoundTrip func(*http.Request) (*http.Response, error)

func (fn gallery128RoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestGallery128RenderedCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_GALLERY128_BROWSER_RUNTIME")
	if runtime == "" {
		t.Skip("set TULAI_GALLERY128_BROWSER_RUNTIME to an isolated Camoufox runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	proc, err := camoufox.Start(ctx, runtime, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(proc.Context())
	tab := camoufox.PageContext(life, proc, proc.Page)
	start := gallery128TestURL(t, "https://imhentai.xxx/view/123/1/")
	getter := NewHTTPGetter("")
	getter.client.Transport = gallery128RoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://imhentai.xxx/view/123/2/" || !strings.Contains(r.Header.Get("Cookie"), "fixture=synthetic") {
			return nil, errors.New("unexpected request or missing synthetic same-origin cookie")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(gallery128FixtureReader("123", 2, 2))), Request: r}, nil
	})
	s := &RenderedSession{process: proc, tab: tab, closeTab: closeTab, closeAllocator: func() { proc.Close() }, releaseProfile: func() {}, getter: getter, start: start}
	defer s.Close()
	pages := map[string]string{
		start.String():                     gallery128FixtureReader("123", 1, 2),
		"https://imhentai.xxx/view/124/1/": gallery128FixtureReader("124", 1, 2),
		"https://imhentai.xxx/login/":      `<html><title>Login</title></html>`,
	}
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(s.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if body, ok := pages[paused.Request.URL]; ok && paused.ResourceType == network.ResourceTypeDocument {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{
					{Name: "Content-Type", Value: "text/html; charset=utf-8"},
					{Name: "Set-Cookie", Value: "fixture=synthetic; Path=/; Secure; SameSite=Lax"},
				}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if err := camoufox.Run(s.tab, action); err != nil {
				select {
				case interceptErrors <- err:
				default:
				}
			}
		}()
	})
	if err := s.run(ctx, fetch.Enable(), camoufox.Navigate(start.String())); err != nil {
		t.Fatal(err)
	}
	got, err := s.Capture(ctx)
	if err != nil || len(got.Items) != 2 || got.Items[1].Metadata["gallery128_page"] != "2" {
		t.Fatalf("capture pages=%d err=%v", len(got.Items), err)
	}
	for _, raw := range []string{"https://imhentai.xxx/view/124/1/", "https://imhentai.xxx/login/"} {
		if err := s.run(ctx, camoufox.Navigate(raw)); err != nil {
			t.Fatal(err)
		}
		if got, err := s.Capture(ctx); err == nil || len(got.Items) != 0 {
			t.Fatalf("redirect %s pages=%d err=%v", raw, len(got.Items), err)
		}
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("interception failed: %v", err)
	default:
	}
}

func TestGallery128HistoricalHTML(t *testing.T) {
	dir := os.Getenv("TULAI_GALLERY128_ARCHIVE")
	if dir == "" {
		t.Skip("set TULAI_GALLERY128_ARCHIVE to the archived HTML directory")
	}
	for _, tc := range []struct {
		file, raw, host string
		total           int
	}{
		{"view1000-1.html", "https://imhentai.xxx/view/1000/1/", "m1.imhentai.xxx", 10},
		{"view1000-2.html", "https://imhentai.xxx/view/1000/2/", "m1.imhentai.xxx", 10},
		{"view1000022-1.html", "https://imhentai.xxx/view/1000022/1/", "m7.imhentai.xxx", 8},
		{"view1000046-1.html", "https://imhentai.xxx/view/1000046/1/", "m7.imhentai.xxx", 74},
	} {
		file, err := os.Open(filepath.Join(dir, tc.file))
		if err != nil {
			t.Fatal(err)
		}
		reader, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		doc, err := html.Parse(io.LimitReader(reader, maxPageBytes+1))
		reader.Close()
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		ref := gallery128PageRef(gallery128TestURL(t, tc.raw))
		state, err := gallery128Read(context.Background(), doc, ref)
		if err != nil || state.total != tc.total || !strings.HasPrefix(state.image, "https://"+tc.host+"/") {
			t.Fatalf("archived reader %s total=%d correctHost=%v err=%v", tc.file, state.total, strings.HasPrefix(state.image, "https://"+tc.host+"/"), err)
		}
	}
	file, err := os.Open(filepath.Join(dir, "gallery1000.html"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	doc, err := html.Parse(io.LimitReader(reader, maxPageBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	root, err := gallery128ReadRoot(context.Background(), doc, gallery128TestURL(t, "https://imhentai.xxx/gallery/1000/"))
	if err != nil || root.total != 10 || root.dir != "001" || root.album == "" {
		t.Fatalf("archived gallery total=%d directory=%q albumPresent=%v err=%v", root.total, root.dir, root.album != "", err)
	}
}
