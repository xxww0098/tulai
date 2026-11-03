package download

import (
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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/xxww0098/tulai/internal/camoufox"
	"golang.org/x/net/html"
)

func gallery121TestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func gallery121TestDoc(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
func gallery121TestResponse(raw string, code int, source string) *Response {
	return &Response{StatusCode: code, FinalURL: raw, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(source))}
}
func gallery121Fixture(site, id, internalID string, index, pages int) string {
	if site == "asm" {
		return fmt.Sprintf(`<html><head><link rel="canonical" href="https://asmhentai.com/gallery/%s/%d/"></head><body><a href="/g/%s/">back</a><input id="pages" value="%d"><input id="gallery_id" value="%s"><img id="fimg" class="lazy image_%d" data-src="https://images.asmhentai.com/5/%s/%d.jpg"><img data-src="https://evil.test/recommended.jpg"></body></html>`, id, index, id, pages, internalID, index, internalID, index)
	}
	return fmt.Sprintf(`<html><body><a href="/gallery/%s/">back</a><input id="pages" value="%d"><input id="gallery_id" value="%s"><img id="gimg" class="lazy image_%d" data-src="https://i3.hentaifox.com/5/%s/%d.webp"><img data-src="https://evil.test/recommended.jpg"></body></html>`, id, pages, internalID, index, internalID, index)
}

type gallery121TestTransport func(*http.Request) (*http.Response, error)

func (f gallery121TestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGallery121Addresses(t *testing.T) {
	for _, tc := range []struct{ raw, key string }{
		{"https://asmhentai.com/g/42/", "asm:42"}, {"https://asmhentai.com/gallery/42/2/", "asm:42"},
		{"https://hentaifox.com/gallery/42/", "fox:42"}, {"https://hentaifox.com/g/42/2/", "fox:42"},
		{"https://asmhentai.com/g/42/1/", ""}, {"https://asmhentai.com/gallery/42/", ""},
		{"https://hentaifox.com/gallery/42/1/", ""}, {"https://hentaifox.com/g/42/", ""},
		{"https://asmhentai.com.evil.test/g/42/", ""}, {"https://user@hentaifox.com/gallery/42/", ""},
		{"https://hentaifox.com:8443/gallery/42/", ""}, {"https://hentaifox.com/g/42%2F2/", ""},
		{"https://hentaifox.com/g/0/1/", ""}, {"https://hentaifox.com/g/42/0/", ""},
	} {
		if got := gallery121Key(gallery121TestURL(t, tc.raw)); got != tc.key {
			t.Errorf("%s key=%q want=%q", tc.raw, got, tc.key)
		}
	}
}
func TestGallery121ReaderIdentity(t *testing.T) {
	for _, site := range []string{"asm", "fox"} {
		t.Run(site, func(t *testing.T) {
			pageRaw, internal := "https://asmhentai.com/gallery/42/1/", "42"
			if site == "fox" {
				pageRaw, internal = "https://hentaifox.com/g/42/1/", "77"
			}
			page := gallery121TestURL(t, pageRaw)
			good := gallery121Fixture(site, "42", internal, 1, 3)
			if got, err := gallery121ParseReader(page, gallery121TestDoc(t, good)); err != nil || got.pages != 3 || got.internalID != internal {
				t.Fatalf("reader=%+v err=%v", got, err)
			}
			mutations := []string{
				strings.Replace(good, `value="3"`, `value="0"`, 1),
				strings.Replace(good, `image_1`, `image_2`, 1),
				strings.Replace(good, `/42/">back`, `/43/">back`, 1),
				strings.Replace(good, `/`+internal+`/1.`, `/`+internal+`/2.`, 1),
				strings.Replace(good, `/`+internal+`/1.`, `/999/1.`, 1),
				strings.Replace(good, `https://`, `https://attacker@`, 1),
			}
			if site == "fox" {
				mutations = append(mutations, strings.Replace(good, `i3.hentaifox.com`, `i4.hentaifox.com`, 1))
			}
			for i, source := range mutations {
				if got, err := gallery121ParseReader(page, gallery121TestDoc(t, source)); err == nil || got.image != "" {
					t.Errorf("mutation %d returned reader %+v err=%v", i, got, err)
				}
			}
		})
	}
}
func TestGallery121PartialPages(t *testing.T) {
	for _, site := range []string{"asm", "fox"} {
		t.Run(site, func(t *testing.T) {
			root, link, internal := "https://asmhentai.com/g/42/?token=synthetic-secret", "/gallery/42/1/", "42"
			if site == "fox" {
				root, link, internal = "https://hentaifox.com/gallery/42/?token=synthetic-secret", "/g/42/1/", "77"
			}
			page := gallery121TestURL(t, root)
			var mu sync.Mutex
			seen := map[int]int{}
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				u, err := url.Parse(raw)
				if err != nil {
					return nil, err
				}
				if u.RawQuery != "" {
					return nil, errors.New("source query leaked")
				}
				info, ok := gallery121AddressOf(u)
				if !ok || !info.reader || info.site != site || info.id != "42" {
					return nil, errors.New("unexpected URL")
				}
				mu.Lock()
				seen[info.page]++
				mu.Unlock()
				if info.page == 2 {
					return gallery121TestResponse(raw, 404, ""), nil
				}
				return gallery121TestResponse(raw, 200, gallery121Fixture(site, "42", internal, info.page, 3)), nil
			})
			doc := gallery121TestDoc(t, `<a href="`+link+`">first</a><img data-src="https://evil.test/a.jpg">`)
			got, err := gallery121Collect(context.Background(), getter, page, doc)
			if err == nil || len(got.Items) != 2 || !strings.Contains(err.Error(), "1 页未能确认") {
				t.Fatalf("items=%d err=%v", len(got.Items), err)
			}
			for i, item := range got.Items {
				want := []string{"1", "3"}[i]
				if item.Metadata["gallery_page"] != want || !item.PublicOnly || !item.RequireImage || strings.Contains(fmt.Sprint(item.Headers), "secret") {
					t.Errorf("unsafe item: %+v", item)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			if len(seen) != 3 || seen[1] != 1 || seen[2] != 1 || seen[3] != 1 {
				t.Errorf("requests=%v", seen)
			}
		})
	}
}

func TestGallery121StopsAfterForbiddenOrRateLimit(t *testing.T) {
	for _, code := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			page := gallery121TestURL(t, "https://asmhentai.com/gallery/42/1/")
			var calls atomic.Int32
			ready := make(chan struct{})
			getter := GetterFunc(func(ctx context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				if calls.Add(1) == 4 {
					close(ready)
				}
				select {
				case <-ready:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				u, _ := url.Parse(raw)
				info, _ := gallery121AddressOf(u)
				if info.page == 2 {
					return gallery121TestResponse(raw, code, ""), nil
				}
				select {
				case <-time.After(100 * time.Millisecond):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
				return gallery121TestResponse(raw, 200, gallery121Fixture("asm", "42", "42", info.page, 40)), nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got, err := gallery121Collect(ctx, getter, page, gallery121TestDoc(t, gallery121Fixture("asm", "42", "42", 1, 40)))
			if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", code)) || !strings.Contains(err.Error(), "停止后续分页") || calls.Load() != 4 || len(got.Items) != 4 {
				t.Fatalf("status=%d started=%d retained=%d err=%v", code, calls.Load(), len(got.Items), err)
			}
		})
	}
}
func TestGallery121RedirectCancellationAndLimit(t *testing.T) {
	page := gallery121TestURL(t, "https://asmhentai.com/gallery/42/1/")
	getter := NewHTTPGetter("")
	var foreign atomic.Bool
	getter.client.Transport = gallery121TestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "asmhentai.com" {
			foreign.Store(true)
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/gallery/42/1/"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, _, err := gallery121Fetch(context.Background(), getter, page); err == nil || foreign.Load() {
		t.Fatalf("redirect err=%v foreign=%v", err, foreign.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelGetter := GetterFunc(func(ctx context.Context, _ string, _ func(*http.Request)) (*Response, error) {
		cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	})
	got, err := gallery121Collect(ctx, cancelGetter, page, gallery121TestDoc(t, gallery121Fixture("asm", "42", "42", 1, 3)))
	if !errors.Is(err, context.Canceled) || len(got.Items) != 1 {
		t.Fatalf("cancel items=%d err=%v", len(got.Items), err)
	}
	limitGetter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		i, _ := gallery121AddressOf(u)
		return gallery121TestResponse(raw, 200, gallery121Fixture("asm", "42", "42", i.page, 105)), nil
	})
	got, err = gallery121Collect(context.Background(), limitGetter, page, gallery121TestDoc(t, gallery121Fixture("asm", "42", "42", 1, 105)))
	if err == nil || len(got.Items) != 100 || !strings.Contains(err.Error(), "总页数 105") {
		t.Fatalf("limit items=%d err=%v", len(got.Items), err)
	}
}
func TestGallery121SavedPublicPages(t *testing.T) {
	dir := os.Getenv("TULAI_GALLERY121_FIXTURES")
	if dir == "" {
		t.Skip("set TULAI_GALLERY121_FIXTURES to locally saved public HTML")
	}
	for _, tc := range []struct {
		file, raw string
		pages     int
	}{
		{"asm-page1.html", "https://asmhentai.com/gallery/693932/1/", 67},
		{"asm-page2.html", "https://asmhentai.com/gallery/693932/2/", 67},
		{"asm-sample2.html", "https://asmhentai.com/gallery/693933/1/", 3},
		{"fox-page1.html", "https://hentaifox.com/g/175538/1/", 38},
		{"fox-page2.html", "https://hentaifox.com/g/175538/2/", 38},
		{"fox-sample2.html", "https://hentaifox.com/g/175446/1/", 62},
		{"fox-i2.html", "https://hentaifox.com/g/137512/1/", 39},
		{"fox-i-5.html", "https://hentaifox.com/g/137512/5/", 39},
		{"fox-i-6.html", "https://hentaifox.com/g/137512/6/", 39},
	} {
		b, err := os.ReadFile(filepath.Join(dir, tc.file))
		if err != nil {
			t.Fatal(err)
		}
		got, err := gallery121ParseReader(gallery121TestURL(t, tc.raw), gallery121TestDoc(t, string(b)))
		if err != nil || got.pages != tc.pages || got.image == "" {
			t.Errorf("%s pages=%d err=%v", tc.file, got.pages, err)
		}
	}
}
func TestGallery121Live(t *testing.T) {
	if os.Getenv("TULAI_GALLERY121_LIVE") != "1" {
		t.Skip("set TULAI_GALLERY121_LIVE=1 for public metadata")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, tc := range []struct {
		root string
		want int
	}{
		{"https://asmhentai.com/g/693933/", 3}, {"https://hentaifox.com/gallery/175538/", 38},
	} {
		got, err := Discover(ctx, NewHTTPGetter(""), tc.root, 1)
		if err != nil || len(got.Items) != tc.want {
			t.Errorf("public discovery returned %d pages, expected %d: %v", len(got.Items), tc.want, err)
			continue
		}
		for i, item := range got.Items {
			if item.Metadata["gallery_page"] != strconv.Itoa(i+1) {
				t.Errorf("wrong page order")
			}
		}
		t.Logf("public discovery returned %d page image addresses", len(got.Items))
	}
}
func TestGallery121LiveMixedHosts(t *testing.T) {
	if os.Getenv("TULAI_GALLERY121_LIVE_MIXED") != "1" {
		t.Skip("set TULAI_GALLERY121_LIVE_MIXED=1 for public metadata")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), "https://hentaifox.com/gallery/137512/", 1)
	if err != nil || len(got.Items) != 39 {
		t.Fatalf("mixed-host public discovery returned %d pages: %v", len(got.Items), err)
	}
	hosts := map[string]bool{}
	for _, item := range got.Items {
		u, err := url.Parse(item.URL)
		if err != nil {
			t.Fatal(err)
		}
		hosts[u.Hostname()] = true
	}
	if !hosts["i.hentaifox.com"] || !hosts["i2.hentaifox.com"] {
		t.Fatalf("expected two observed image hosts, got %v", hosts)
	}
	t.Logf("public discovery returned %d page addresses across %d confirmed image hosts", len(got.Items), len(hosts))
}
func TestGallery121CamoufoxCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_GALLERY121_CAMOUFOX_ROOT")
	if runtime == "" {
		t.Skip("set TULAI_GALLERY121_CAMOUFOX_ROOT to a copied temporary runtime")
	}
	for _, site := range []string{"asm", "fox"} {
		t.Run(site, func(t *testing.T) { gallery121TestCapture(t, runtime, site) })
	}
}
func gallery121TestCapture(t *testing.T, runtime, site string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	process, err := camoufox.Start(ctx, runtime, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(process.Context())
	tab := camoufox.PageContext(life, process, process.Page)
	getter := NewHTTPGetter("")
	getter.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	root, first, internal := "https://asmhentai.com/g/42/", "/gallery/42/1/", "42"
	if site == "fox" {
		root, first, internal = "https://hentaifox.com/gallery/42/", "/g/42/1/", "77"
	}
	getter.client.Transport = gallery121TestTransport(func(r *http.Request) (*http.Response, error) {
		info, ok := gallery121AddressOf(r.URL)
		if !ok || info.site != site || !info.reader || info.id != "42" {
			return nil, errors.New("unexpected synthetic request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(gallery121Fixture(site, "42", internal, info.page, 3))), Request: r}, nil
	})
	page := gallery121TestURL(t, root)
	session := &RenderedSession{process: process, tab: tab, closeTab: closeTab, closeAllocator: process.Close, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == root {
				body := `<html><body><a href="` + first + `">first</a></body></html>`
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			_ = camoufox.Run(session.tab, action)
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(root)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 {
		t.Fatalf("capture items=%d err=%v", len(got.Items), err)
	}
}
