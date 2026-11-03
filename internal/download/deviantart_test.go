package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	"golang.org/x/net/html"
)

const (
	deviantArtTestPage = "https://www.deviantart.com/artist/art/title-123456789"
	deviantArtTestCDN  = "https://images-wixmp-ed30a86b8c4ca887773594c2.wixmp.com"
)

type deviantArtRoundTrip func(*http.Request) (*http.Response, error)

func (fn deviantArtRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func deviantArtResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func deviantArtHTML(state string) string {
	literal, _ := json.Marshal(state)
	return `<html><title>fallback</title><meta property="og:url" content="` + deviantArtTestPage + `"><meta property="og:image" content="` + deviantArtTestCDN + `/f/artist/preview.jpg?token=preview"><script>window.__INITIAL_STATE__ = JSON.parse(` + string(literal) + `);</script></html>`
}

func deviantArtStateJSON(work, extended string) string {
	return `{"@@entities":{"deviation":{"123456789":` + work + `,"999999999":{"deviationId":999999999,"media":{"baseUri":"https://evil.test/recommended.jpg"}}},"deviationExtended":{"123456789":` + extended + `}}}`
}

const deviantArtWork = `{"deviationId":123456789,"type":"image","title":"作品","isDownloadable":false,"media":{"baseUri":"https://images-wixmp-ed30a86b8c4ca887773594c2.wixmp.com/f/artist/base.jpg","prettyName":"work_by_artist","token":["synthetic.token"],"types":[{"t":"preview","c":"/v1/fill/w_500,h_500/<prettyName>-pre.jpg"},{"t":"fullview","c":"/v1/fill/w_1024,h_1280/<prettyName>-fullview.jpg"}]}}`

func TestDeviantArtRoutesAndCDN(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{deviantArtTestPage, "123456789"},
		{"https://deviantart.com/Artist_Name/art/title-with-dash-123456789/?q=1", "123456789"},
		{"https://www.deviantart.com/artist/gallery/123456789", ""},
		{"https://www.deviantart.com/artist/art/title-000000", ""},
		{"https://www.deviantart.com.evil.test/artist/art/title-123456789", ""},
		{"https://user@www.deviantart.com/artist/art/title-123456789", ""},
		{"https://www.deviantart.com:8443/artist/art/title-123456789", ""},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := deviantArtID(u); got != tc.id {
			t.Errorf("route %s = %q", tc.raw, got)
		}
	}
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{deviantArtTestCDN + "/f/a/b.jpg?token=sig", true},
		{"http://images-wixmp-ed30a86b8c4ca887773594c2.wixmp.com/f/a/b.jpg", false},
		{"https://images-wixmp-ed30a86b8c4ca887773594c2.wixmp.com.evil.test/f/a/b.jpg", false},
		{"https://user@images-wixmp-ed30a86b8c4ca887773594c2.wixmp.com/f/a/b.jpg", false},
		{deviantArtTestCDN + "/f/a/../private", false},
		{deviantArtTestCDN + "/other/a.jpg", false},
	} {
		_, got := deviantArtCDNURL(tc.raw)
		if got != tc.want {
			t.Errorf("CDN %s = %t", tc.raw, got)
		}
	}
}

func TestDeviantArtCurrentWorkAndOriginal(t *testing.T) {
	page, _ := url.Parse(deviantArtTestPage + "?private=source")
	for _, tc := range []struct {
		name, work, extended, kind, suffix string
		alternate                          bool
	}{
		{"fullview", deviantArtWork, `{"download":{"url":"` + deviantArtTestCDN + `/f/artist/original.jpg?token=original"}}`, "fullview", "/f/artist/base.jpg/v1/fill/w_1024,h_1280/work_by_artist-fullview.jpg?token=synthetic.token", false},
		{"original", strings.Replace(deviantArtWork, `"isDownloadable":false`, `"isDownloadable":true`, 1), `{"download":{"url":"` + deviantArtTestCDN + `/f/artist/original.jpg?token=original"}}`, "original", "/f/artist/original.jpg?token=original", true},
		{"quota", strings.Replace(deviantArtWork, `"isDownloadable":false`, `"isDownloadable":true`, 1), `{"download":{"url":"` + deviantArtTestCDN + `/f/artist/original.jpg?token=original"},"downloadRestrictions":{"remaining":0}}`, "fullview", "/f/artist/base.jpg/v1/fill/w_1024,h_1280/work_by_artist-fullview.jpg?token=synthetic.token", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := deviantArtHTML(deviantArtStateJSON(tc.work, tc.extended))
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return deviantArtResponse(raw, body), nil
			})
			got, err := discoverDeviantArtWork(context.Background(), getter, page)
			if err != nil || got.Title != "作品" || len(got.Items) != 1 || got.Items[0].URL != deviantArtTestCDN+tc.suffix || got.Items[0].Metadata["image_kind"] != tc.kind || (len(got.Items[0].AlternateURLs) > 0) != tc.alternate || got.Items[0].Headers["Referer"] != "https://www.deviantart.com/" {
				t.Fatalf("work=%+v err=%v", got, err)
			}
		})
	}
}

func TestDeviantArtDiscoverDispatch(t *testing.T) {
	body := deviantArtHTML(deviantArtStateJSON(deviantArtWork, `{}`))
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return deviantArtResponse(raw, body), nil
	})
	got, err := Discover(context.Background(), getter, deviantArtTestPage, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["deviantart_id"] != "123456789" {
		t.Fatalf("discovery images=%d err=%v", len(got.Items), err)
	}
}

func TestDeviantArtSafetyAndPartial(t *testing.T) {
	page, _ := url.Parse(deviantArtTestPage)
	for _, mode := range []string{"blocked", "wrong-id", "missing-id", "bad-original", "preview", "multi", "invalid-state", "cancel", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				work, extended := deviantArtWork, `{}`
				switch mode {
				case "blocked":
					work = strings.Replace(work, `"type":"image"`, `"type":"image","isBlocked":true`, 1)
				case "wrong-id":
					work = strings.Replace(work, `"deviationId":123456789`, `"deviationId":999999999`, 1)
				case "bad-original":
					work = strings.Replace(work, `"isDownloadable":false`, `"isDownloadable":true`, 1)
					extended = `{"download":{"url":"https://evil.test/original.jpg"}}`
				case "preview":
					work = strings.Replace(work, `"t":"fullview"`, `"t":"missing"`, 1)
				case "multi":
					work = strings.Replace(work, `"type":"image"`, `"type":"image","isMultiMedia":true`, 1)
				case "cancel":
					cancel()
				}
				state := deviantArtStateJSON(work, extended)
				if mode == "missing-id" {
					state = strings.Replace(state, `"123456789":`, `"111111111":`, 1)
				}
				if mode == "invalid-state" {
					state = `{invalid`
				}
				resp := deviantArtResponse(raw, deviantArtHTML(state))
				if mode == "redirect" {
					resp.FinalURL = "https://evil.test/artist/art/title-123456789"
				}
				return resp, nil
			})
			got, err := discoverDeviantArtWork(ctx, getter, page)
			want := 0
			if mode == "bad-original" || mode == "preview" || mode == "multi" || mode == "invalid-state" {
				want = 1
			}
			if err == nil || len(got.Items) != want || (mode == "cancel" && !errors.Is(err, context.Canceled)) {
				t.Fatalf("mode=%s images=%d err=%v", mode, len(got.Items), err)
			}
		})
	}
}

func TestDeviantArtRenderedCapture(t *testing.T) {
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
	page := deviantArtHTML(deviantArtStateJSON(deviantArtWork, `{}`))
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == deviantArtTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(page)))
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
	session.start, _ = url.Parse(deviantArtTestPage)
	if err := session.run(ctx, camoufox.Navigate(deviantArtTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.deviantart.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	session.getter.client.Transport = deviantArtRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "images-wixmp-ed30a86b8c4ca887773594c2.wixmp.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.deviantart.com/" {
			t.Errorf("unsafe CDN request %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["image_kind"] != "fullview" || session.PageURL() != deviantArtTestPage {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	select {
	case err := <-responses:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestDeviantArtCapturedPublicWork(t *testing.T) {
	path := os.Getenv("TULAI_DEVIANTART_CAPTURE")
	if path == "" {
		t.Skip("set TULAI_DEVIANTART_CAPTURE to a captured public work HTML")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc, err := html.Parse(io.LimitReader(file, maxPageBytes+1))
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse("https://www.deviantart.com/guweiz/art/Lantern-745215143")
	got, err := deviantArtWorkPage(context.Background(), doc, page)
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["image_kind"] != "fullview" {
		t.Fatalf("public work images=%d err=%v", len(got.Items), err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if info, err := os.Stat(filepath.Join(dir, entries[0].Name())); err != nil || info.Size() == 0 {
		t.Fatalf("empty image: %v %v", info, err)
	}
	t.Logf("public work image downloaded")
}

func TestDeviantArtLiveWork(t *testing.T) {
	if os.Getenv("TULAI_DEVIANTART_LIVE") != "1" {
		t.Skip("set TULAI_DEVIANTART_LIVE=1 for a public work")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	page, _ := url.Parse("https://www.deviantart.com/guweiz/art/Lantern-745215143")
	getter := NewHTTPGetter("")
	got, err := discoverDeviantArtWork(ctx, getter, page)
	var status *HTTPStatusError
	if errors.As(err, &status) && status.StatusCode == 403 {
		t.Skipf("public work denied HTTP 403: %v", err)
	}
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("public work images=%d err=%v", len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "image"
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	t.Logf("public work image downloaded")
}
