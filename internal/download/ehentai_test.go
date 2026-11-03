package download

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"golang.org/x/net/html"
)

const ehentaiTestGallery = "https://e-hentai.org/g/12345/0123456789/"
const ehentaiTestPhoto1 = "https://e-hentai.org/s/abcdef0123/12345-1"
const ehentaiTestPhoto2 = "https://e-hentai.org/s/abcdef0124/12345-2"
const ehentaiTestImage1 = "https://abc.hath.network/h/hash/keystamp=synthetic/image1.jpg"
const ehentaiTestImage2 = "https://abc.hath.network/h/hash/keystamp=synthetic/image2.png"

func ehentaiTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func ehentaiTestDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func ehentaiGalleryFixture(first, next bool) string {
	photo := ehentaiTestPhoto2
	if first {
		photo = ehentaiTestPhoto1
	}
	nav := ""
	if next {
		nav = `<table class="ptt"><tr><td><a href="?p=1">&gt;</a></td></tr></table>`
	}
	return `<html><body><table id="gdd"><tr><td class="gdt1">Length:</td><td class="gdt2">2 pages</td></tr></table>` +
		`<div id="gdt"><div><a href="` + photo + `"><img src="https://example.test/thumbnail.jpg"></a></div></div>` + nav +
		`<img id="img" src="https://example.test/unrelated.jpg"></body></html>`
}

func ehentaiPhotoFixture(image string) string {
	return `<html><body><div id="i3"><a href="#"><img id="img" src="` + image + `"></a></div><img src="https://example.test/unrelated.jpg"></body></html>`
}

func ehentaiGetter(t *testing.T, pages map[string]string, statuses map[string]int, requests *[]string) Getter {
	t.Helper()
	return GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		*requests = append(*requests, raw)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		if prepare != nil {
			prepare(req)
		}
		if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
			t.Fatalf("unexpected credentials: %v", req.Header)
		}
		status := statuses[raw]
		if status == 0 {
			status = http.StatusOK
		}
		return &Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(pages[raw]))}, nil
	})
}

func TestEhentaiRouteAndImages(t *testing.T) {
	for _, tc := range []struct {
		raw, kind, gid string
		page           int
		safe           bool
	}{
		{ehentaiTestGallery, "gallery", "12345", 0, true},
		{ehentaiTestGallery + "?p=1", "gallery", "12345", 1, true},
		{ehentaiTestGallery + "?nw=always", "gallery", "12345", 0, true},
		{ehentaiTestGallery + "?hc=1&nw=session", "gallery", "12345", 0, true},
		{ehentaiTestGallery + "?nw=unrecognized", "gallery", "12345", 0, false},
		{ehentaiTestGallery + "?p=1&p=2", "gallery", "12345", -1, false},
		{ehentaiTestPhoto1, "photo", "12345", 1, true},
		{ehentaiTestPhoto1 + "?nl=nz2-445629", "photo", "12345", 1, true},
		{"https://exhentai.org/g/12345/0123456789/", "", "", 0, false},
		{"https://e-hentai.org.evil.test/g/12345/0123456789/", "", "", 0, false},
		{"https://user@e-hentai.org/g/12345/0123456789/", "", "", 0, false},
		{"https://e-hentai.org:8443/g/12345/0123456789/", "", "", 0, false},
		{"https://e-hentai.org/s/abcdef0123/12345-0", "", "", 0, false},
	} {
		u := ehentaiTestURL(t, tc.raw)
		got := ehentaiRoute(u)
		if got.kind != tc.kind || got.gallery != tc.gid || got.page != tc.page || ehentaiSafeQuery(u) != tc.safe {
			t.Errorf("route %s: %+v safe=%v", tc.raw, got, ehentaiSafeQuery(u))
		}
	}
	page := ehentaiTestURL(t, ehentaiTestPhoto1)
	for _, tc := range []struct{ raw, want string }{
		{ehentaiTestImage1, ehentaiTestImage1},
		{"https://abc.hath.network/om/123/hash/x/1280/stamp/image.jpg", "https://abc.hath.network/om/123/hash/x/1280/stamp/image.jpg"},
		{"https://abc.hath.network/om/123/hash/x/0/stamp/image.jpg", ""},
		{"https://abc.hath.network:11075/h/hash/keystamp=synthetic/image.jpg", ""},
		{"https://abc.hath.network.evil.test/h/hash/keystamp=synthetic/image.jpg", ""},
		{"https://user@abc.hath.network/h/hash/keystamp=synthetic/image.jpg", ""},
		{"http://abc.hath.network/h/hash/keystamp=synthetic/image.jpg", ""},
		{"https://abc.hath.network/h/hash/../image.jpg", ""},
		{"https://abc.hath.network/h/hash/keystamp=synthetic/video.mp4", ""},
	} {
		if got := ehentaiImage(page, tc.raw); got != tc.want {
			t.Errorf("image %s: got %q want %q", tc.raw, got, tc.want)
		}
	}
}

func TestEhentaiPhotoSafety(t *testing.T) {
	page := ehentaiTestURL(t, ehentaiTestPhoto1)
	item, err := ehentaiPhoto(page, ehentaiTestDoc(t, ehentaiPhotoFixture(ehentaiTestImage1)))
	if err != nil || item.URL != ehentaiTestImage1 || !item.PublicOnly || !item.RequireImage || item.Metadata["ehentai_page"] != "1" || item.Headers["Referer"] != "https://e-hentai.org/" {
		t.Fatalf("item=%+v err=%v", item, err)
	}
	for _, body := range []string{
		`<img id="img" src="` + ehentaiTestImage1 + `">`,
		`<div id="i3" hidden><img id="img" src="` + ehentaiTestImage1 + `"></div>`,
		`<div id="i3"><img id="img" src="` + ehentaiTestImage1 + `"><img id="img" src="` + ehentaiTestImage2 + `"></div>`,
		`<div id="i3"><img id="img" src="https://evil.test/image.jpg"></div>`,
		`<div id="i3"><img id="img" src="https://abc.hath.network/om/123/hash/x/0/stamp/image.jpg"></div>`,
	} {
		if _, err := ehentaiPhoto(page, ehentaiTestDoc(t, body)); err == nil {
			t.Fatalf("accepted unsafe markup")
		}
	}
}

func TestEhentaiGalleryPagination(t *testing.T) {
	pages := map[string]string{ehentaiTestGallery: ehentaiGalleryFixture(true, true), ehentaiTestPhoto1: ehentaiPhotoFixture(ehentaiTestImage1), ehentaiTestGallery + "?p=1": ehentaiGalleryFixture(false, false), ehentaiTestPhoto2: ehentaiPhotoFixture(ehentaiTestImage2)}
	var requests []string
	got, err := discoverEhentai(context.Background(), ehentaiGetter(t, pages, nil, &requests), ehentaiTestURL(t, ehentaiTestGallery))
	if err == nil || !strings.Contains(err.Error(), "显示版本") || len(got.Items) != 2 || len(requests) != 4 || got.Items[0].Metadata["ehentai_page"] != "1" || got.Items[1].Metadata["ehentai_page"] != "2" {
		t.Fatalf("items=%+v requests=%v err=%v", got.Items, requests, err)
	}
	if got.Title != "E-Hentai 12345" {
		t.Fatalf("unexpected title %q", got.Title)
	}
}

func TestEhentaiStopsOnAccessLimitAndKeepsPartial(t *testing.T) {
	pages := map[string]string{ehentaiTestPhoto1: ehentaiPhotoFixture(ehentaiTestImage1)}
	body := `<div id="gdt"><a href="` + ehentaiTestPhoto1 + `"></a><a href="` + ehentaiTestPhoto2 + `"></a></div>`
	var requests []string
	got, err := ehentaiCollect(context.Background(), ehentaiGetter(t, pages, map[string]int{ehentaiTestPhoto2: 429}, &requests), ehentaiTestURL(t, ehentaiTestGallery), ehentaiTestDoc(t, body))
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != 429 || len(got.Items) != 1 || len(requests) != 2 {
		t.Fatalf("items=%d requests=%v err=%v", len(got.Items), requests, err)
	}
	requests = nil
	_, err = discoverEhentai(context.Background(), ehentaiGetter(t, nil, map[string]int{ehentaiTestGallery: 403}, &requests), ehentaiTestURL(t, ehentaiTestGallery))
	if !errors.As(err, &status) || status.StatusCode != 403 || len(requests) != 1 {
		t.Fatalf("requests=%v err=%v", requests, err)
	}
}

func TestEhentaiRejectsUnverifiedPages(t *testing.T) {
	page := ehentaiTestURL(t, ehentaiTestGallery)
	for _, body := range []string{
		`<h1>Content Warning</h1><div id="gdt"><a href="` + ehentaiTestPhoto1 + `"></a></div>`,
		`<input type="password"><div id="gdt"><a href="` + ehentaiTestPhoto1 + `"></a></div>`,
		`<div id="gdt"></div><div id="gdt"><a href="` + ehentaiTestPhoto1 + `"></a></div>`,
		`<div id="gdt"><a href="https://e-hentai.org/s/abcdef0123/99999-1"></a></div>`,
	} {
		var requests []string
		got, err := ehentaiCollect(context.Background(), ehentaiGetter(t, nil, nil, &requests), page, ehentaiTestDoc(t, body))
		if err == nil || len(got.Items) != 0 || len(requests) != 0 {
			t.Fatalf("items=%+v requests=%v err=%v", got.Items, requests, err)
		}
	}
	var requests []string
	_, err := discoverEhentai(context.Background(), ehentaiGetter(t, nil, nil, &requests), ehentaiTestURL(t, ehentaiTestGallery+"?unknown=1"))
	if err == nil || len(requests) != 0 {
		t.Fatalf("unverified params: requests=%v err=%v", requests, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = discoverEhentai(ctx, ehentaiGetter(t, nil, nil, &requests), page)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestEhentaiRejectsViewerRedirect(t *testing.T) {
	requests := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		requests++
		return &Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html"}}, FinalURL: "https://evil.test/other", Body: io.NopCloser(strings.NewReader(ehentaiPhotoFixture(ehentaiTestImage1)))}, nil
	})
	body := `<div id="gdt"><a href="` + ehentaiTestPhoto1 + `"></a></div>`
	got, err := ehentaiCollect(context.Background(), getter, ehentaiTestURL(t, ehentaiTestGallery), ehentaiTestDoc(t, body))
	if err == nil || len(got.Items) != 0 || requests != 1 {
		t.Fatalf("redirect: items=%d requests=%d err=%v", len(got.Items), requests, err)
	}
}

func TestEhentaiUserParametersAndLinkedBoundary(t *testing.T) {
	provided := ehentaiTestGallery + "?p=0&hc=1&nw=session"
	var requests []string
	_, err := discoverEhentai(context.Background(), ehentaiGetter(t, nil, map[string]int{provided: 403}, &requests), ehentaiTestURL(t, provided))
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != 403 || len(requests) != 1 || requests[0] != provided {
		t.Fatalf("provided query: requests=%v err=%v", requests, err)
	}
	requests = nil
	httpPage := strings.Replace(ehentaiTestGallery, "https://", "http://", 1)
	_, err = discoverEhentai(context.Background(), ehentaiGetter(t, nil, map[string]int{ehentaiTestGallery: 403}, &requests), ehentaiTestURL(t, httpPage))
	if !errors.As(err, &status) || status.StatusCode != 403 || len(requests) != 1 || requests[0] != ehentaiTestGallery {
		t.Fatalf("https upgrade: requests=%v err=%v", requests, err)
	}
	requests = nil
	body := strings.Replace(ehentaiGalleryFixture(true, true), `?p=1`, `?p=1&amp;nw=session`, 1)
	got, err := ehentaiCollect(context.Background(), ehentaiGetter(t, map[string]string{ehentaiTestPhoto1: ehentaiPhotoFixture(ehentaiTestImage1)}, nil, &requests), ehentaiTestURL(t, ehentaiTestGallery), ehentaiTestDoc(t, body))
	if err == nil || len(got.Items) != 1 || len(requests) != 1 || requests[0] != ehentaiTestPhoto1 {
		t.Fatalf("linked query: items=%d requests=%v err=%v", len(got.Items), requests, err)
	}
	body = strings.Replace(ehentaiGalleryFixture(true, true), `?p=1`, `?p=1&amp;hc=1`, 1)
	_, next, _, incomplete := ehentaiGalleryLinks(ehentaiTestURL(t, ehentaiTestGallery), ehentaiTestDoc(t, body))
	if incomplete || next == nil || next.Query().Get("hc") != "1" || next.Query().Get("p") != "1" {
		t.Fatalf("documented comment parameter: next=%v incomplete=%v", next, incomplete)
	}
}

type ehentaiRoundTrip func(*http.Request) (*http.Response, error)

func (f ehentaiRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEhentaiRenderedCapture(t *testing.T) {
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
	body := strings.Replace(ehentaiGalleryFixture(true, false), "2 pages", "1 page", 1)
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == ehentaiTestGallery {
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
	session.start = ehentaiTestURL(t, ehentaiTestGallery)
	if err := session.run(ctx, camoufox.Navigate(ehentaiTestGallery)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".e-hentai.org").WithURL("https://e-hentai.org/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	requests := 0
	session.getter.client.Transport = ehentaiRoundTrip(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests++
		mu.Unlock()
		switch r.URL.Host {
		case "e-hentai.org":
			if r.URL.String() != ehentaiTestPhoto1 || r.Header.Get("Cookie") != "synthetic_session=example" {
				t.Errorf("same-origin viewer request %s %v", r.URL, r.Header)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(ehentaiPhotoFixture(ehentaiTestImage1))), Request: r}, nil
		case "abc.hath.network":
			if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://e-hentai.org/" {
				t.Errorf("unsafe image request %v", r.Header)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic")), Request: r}, nil
		default:
			t.Errorf("unexpected host %s", r.URL.Host)
			return nil, errors.New("unexpected host")
		}
	})
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "显示版本") || len(got.Items) != 1 || session.PageURL() != ehentaiTestGallery {
		t.Fatalf("items=%+v page=%s err=%v", got.Items, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, func(r *http.Request) {
		for k, v := range got.Items[0].Headers {
			r.Header.Set(k, v)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	mu.Lock()
	count := requests
	mu.Unlock()
	if count != 2 {
		t.Fatalf("requests=%d", count)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept: %v", err)
	default:
	}
}
