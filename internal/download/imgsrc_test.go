package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"golang.org/x/net/html"
)

const imgsrcTestPhoto = "https://imgsrc.ru/alice/123.html"
const imgsrcTestTape0 = "https://imgsrc.ru/alice/tape-77-123-0.html"
const imgsrcTestTape24 = "https://imgsrc.ru/alice/tape-77-123-24.html"
const imgsrcTestImage1 = "https://b7.dd.icdn.ru/a/alice/3/imgsrc.ru_123abc.jpg"
const imgsrcTestImage2 = "https://b7.dd.icdn.ru/a/alice/4/imgsrc.ru_124abc.jpg"
const imgsrcTestImage3 = "https://b7.dd.icdn.ru/a/alice/5/imgsrc.ru_125abc.jpg"

func imgsrcPhotoFixture(tapeHref string) string {
	return `<html><head><title>Album / photo</title><link rel="canonical" href="` + imgsrcTestPhoto + `"></head><body><source id="big-pic-jpeg" type="image/jpeg" srcset="` + imgsrcTestImage1 + `"><a href="` + tapeHref + `">all images</a><source type="image/jpeg" srcset="https://s7.dd.icdn.ru/a/alice/3/recommendation.jpg"></body></html>`
}
func imgsrcTapeFixture(canonical string, entries map[string]string, links ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<html><head><link rel="canonical" href="%s"><title>Album</title></head><body>`, canonical)
	for id, image := range entries {
		fmt.Fprintf(&b, `<a href="#p%s"><picture><source type="image/jpeg" srcset="%s"><img class="big fts"></picture></a>`, id, image)
	}
	b.WriteString(`<picture><source type="image/jpeg" srcset="https://s7.dd.icdn.ru/a/alice/9/recommended.jpg"></picture>`)
	for _, u := range links {
		fmt.Fprintf(&b, `<a class="navdot" href="%s">next</a>`, u)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

func TestImgsrcTapeLastPictureLinks(t *testing.T) {
	for _, tc := range []struct {
		name, page, anchor string
	}{
		{"next tape", imgsrcTestTape0, "/alice/tape-77-123-24.html?pwd="},
		{"owner on final tape", imgsrcTestTape24, "/alice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, _ := url.Parse(tc.page)
			body := imgsrcTapeFixture(tc.page, nil)
			tail := `<a href="` + tc.anchor + `"><picture><source type="image/jpeg" srcset="` + imgsrcTestImage3 + `"><img class="big fts"></picture></a>`
			body = strings.Replace(body, "</body>", tail+"</body>", 1)
			doc, err := html.Parse(strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			got, err := imgsrcTape(context.Background(), doc, page)
			if err != nil || len(got.result.Items) != 1 || got.result.Items[0].URL != imgsrcTestImage3 {
				t.Fatalf("last image=%+v err=%v", got.result.Items, err)
			}
		})
	}
}

func TestImgsrcTapeLastPictureRejectsOtherAlbum(t *testing.T) {
	page, _ := url.Parse(imgsrcTestTape0)
	body := imgsrcTapeFixture(imgsrcTestTape0, map[string]string{"123": imgsrcTestImage1})
	tail := `<a href="/alice/tape-88-123-24.html"><picture><source type="image/jpeg" srcset="` + imgsrcTestImage3 + `"><img class="big fts"></picture></a>`
	body = strings.Replace(body, "</body>", tail+"</body>", 1)
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	got, err := imgsrcTape(context.Background(), doc, page)
	if err != nil || len(got.result.Items) != 1 || got.result.Items[0].URL != imgsrcTestImage1 {
		t.Fatalf("other-album image=%+v err=%v", got.result.Items, err)
	}
}

func TestImgsrcReportedCountMismatch(t *testing.T) {
	root, _ := url.Parse(imgsrcTestPhoto)
	photo := strings.Replace(imgsrcPhotoFixture(`/alice/tape-77-123-0.html`), "</body>", `<a href="/members/zip_manager.php?zip=alice/77.zip">ZIP 4 photo</a></body>`, 1)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw == imgsrcTestPhoto {
			return imgsrcResponse(raw, photo), nil
		}
		return imgsrcResponse(raw, imgsrcTapeFixture(imgsrcTestTape0, map[string]string{"123": imgsrcTestImage1, "124": imgsrcTestImage2})), nil
	})
	got, err := discoverImgsrc(context.Background(), getter, root)
	if err == nil || !strings.Contains(err.Error(), "数量") || len(got.Items) != 2 {
		t.Fatalf("partial items=%+v err=%v", got.Items, err)
	}
}
func imgsrcResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestImgsrcURLAndImageBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{imgsrcTestPhoto, true}, {"https://imgsrc.ru/alice/a77.html", true}, {imgsrcTestTape24, true},
		{"https://imgsrc.ru/alice/0.html", false}, {"https://imgsrc.ru/alice/a77.html/", false}, {"https://imgsrc.ru.evil.test/alice/123.html", false},
		{"https://x@imgsrc.ru/alice/123.html", false}, {"https://imgsrc.ru:444/alice/123.html", false}, {"https://imgsrc.ru/main/login.php", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isImgsrcPage(u); got != tc.want {
			t.Errorf("isImgsrcPage(%q)=%v", tc.raw, got)
		}
	}
	for _, raw := range []string{
		"http://b7.dd.icdn.ru/a/alice/3/imgsrc.ru_123abc.jpg", strings.Replace(imgsrcTestImage1, "b7.dd.icdn.ru", "b7.dd.icdn.ru.evil.test", 1),
		strings.Replace(imgsrcTestImage1, "b7.dd.icdn.ru", "user@b7.dd.icdn.ru", 1), strings.Replace(imgsrcTestImage1, "b7.dd.icdn.ru", "b7.dd.icdn.ru:444", 1),
		strings.Replace(imgsrcTestImage1, "/alice/", "/other/", 1), strings.Replace(imgsrcTestImage1, "_123abc", "_999abc", 1),
		strings.Replace(imgsrcTestImage1, "/3/", "/4/", 1), imgsrcTestImage1 + "?signed=token",
	} {
		if _, ok := imgsrcImage(raw, "alice", "123"); ok {
			t.Errorf("accepted %q", raw)
		}
	}
	if got, ok := imgsrcImage("//b7.dd.icdn.ru/a/alice/3/imgsrc.ru_123abc.jpg", "alice", "123"); !ok || got != imgsrcTestImage1 {
		t.Fatalf("image=%q ok=%v", got, ok)
	}
}

func TestImgsrcPublicPagination(t *testing.T) {
	root, _ := url.Parse(imgsrcTestPhoto)
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		switch raw {
		case imgsrcTestPhoto:
			return imgsrcResponse(raw, imgsrcPhotoFixture(`/alice/tape-77-123-0.html?pwd=`)), nil
		case imgsrcTestTape0:
			return imgsrcResponse(raw, imgsrcTapeFixture(imgsrcTestTape0, map[string]string{"123": imgsrcTestImage1, "124": imgsrcTestImage2}, `/alice/tape-77-123-24.html?pwd=`)), nil
		case imgsrcTestTape24:
			return imgsrcResponse(raw, imgsrcTapeFixture(imgsrcTestTape24, map[string]string{"125": imgsrcTestImage3}, `/alice/tape-77-123-0.html?pwd=`)), nil
		default:
			t.Fatalf("unexpected URL %s", raw)
			return nil, nil
		}
	})
	got, err := discoverImgsrc(context.Background(), getter, root)
	if err != nil || len(got.Items) != 3 || len(calls) != 3 {
		t.Fatalf("items=%+v calls=%v err=%v", got.Items, calls, err)
	}
	seen := map[string]bool{}
	for _, item := range got.Items {
		seen[item.Metadata["imgsrc_photo_id"]] = true
		if !item.PublicOnly || !item.RequireImage || item.Headers["Referer"] != "https://imgsrc.ru/" {
			t.Fatalf("item=%+v", item)
		}
	}
	if !seen["123"] || !seen["124"] || !seen["125"] {
		t.Fatalf("IDs %v", seen)
	}
}

func TestImgsrcAlbumRedirectIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, album string
		want        int
	}{
		{"same album", "77", 2},
		{"different album", "88", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := url.Parse("https://imgsrc.ru/alice/a" + tc.album + ".html")
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				if raw == root.String() {
					return imgsrcResponse(imgsrcTestPhoto, imgsrcPhotoFixture(`/alice/tape-77-123-0.html?pwd=`)), nil
				}
				if raw == imgsrcTestTape0 {
					return imgsrcResponse(raw, imgsrcTapeFixture(imgsrcTestTape0, map[string]string{"123": imgsrcTestImage1, "124": imgsrcTestImage2})), nil
				}
				t.Fatalf("unexpected request %q", raw)
				return nil, nil
			})
			got, err := discoverImgsrc(context.Background(), getter, root)
			if len(got.Items) != tc.want || ((err != nil) != (tc.want == 0)) {
				t.Fatalf("redirect items=%+v err=%v", got.Items, err)
			}
		})
	}
}

type imgsrcRoundTripper func(*http.Request) (*http.Response, error)

func (f imgsrcRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func imgsrcHTTPResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestImgsrcAuthorizedCredentialIsolation(t *testing.T) {
	root, _ := url.Parse(imgsrcTestPhoto + "?pwd=secret-value&token=secret-token")
	getter := NewHTTPGetter("")
	var calls int
	getter.client.Transport = imgsrcRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Hostname() != "imgsrc.ru" || r.URL.Query().Get("pwd") != "secret-value" || r.URL.Query().Get("token") != "secret-token" {
			t.Fatalf("credential was not restricted to site: host=%s path=%s", r.URL.Hostname(), r.URL.Path)
		}
		if strings.Contains(r.Header.Get("Referer"), "secret") {
			t.Fatal("credential leaked in referer")
		}
		switch r.URL.Path {
		case "/alice/123.html":
			return imgsrcHTTPResponse(r, 200, imgsrcPhotoFixture(`/alice/tape-77-123-0.html?pwd=`)), nil
		case "/alice/tape-77-123-0.html":
			return imgsrcHTTPResponse(r, 200, imgsrcTapeFixture(imgsrcTestTape0, map[string]string{"123": imgsrcTestImage1, "124": imgsrcTestImage2})), nil
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
			return nil, nil
		}
	})
	got, err := discoverImgsrc(context.Background(), getter, root)
	if err != nil || len(got.Items) != 2 || calls != 2 {
		t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "secret") {
		t.Fatal("credentials in discovered items")
	}
}

func TestImgsrcCredentialRedirectAndRestrictions(t *testing.T) {
	root, _ := url.Parse(imgsrcTestPhoto + "?pwd=secret-value")
	getter := NewHTTPGetter("")
	var evil atomic.Bool
	getter.client.Transport = imgsrcRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "evil.test" {
			evil.Store(true)
		}
		resp := imgsrcHTTPResponse(r, http.StatusFound, "")
		resp.Header.Set("Location", "https://evil.test/alice/123.html?pwd=secret-value")
		return resp, nil
	})
	got, err := discoverImgsrc(context.Background(), getter, root)
	if err == nil || evil.Load() || len(got.Items) != 0 || strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("items=%+v evil=%v err=%v", got.Items, evil.Load(), err)
	}
	mock := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("unsafe credentialed getter used")
		return nil, nil
	})
	if _, err := discoverImgsrc(context.Background(), mock, root); err == nil || !strings.Contains(err.Error(), "隔离") {
		t.Fatalf("unsafe getter err=%v", err)
	}
	page, _ := url.Parse(imgsrcTestPhoto + "?pwd=secret-value")
	other, _ := url.Parse("https://evil.test/alice/tape-77-123-0.html")
	if _, ok := imgsrcFollowURL(page, other); ok {
		t.Fatal("cross-origin link accepted")
	}
	for _, raw := range []string{"https://imgsrc.ru/alice/123.html?unknown=secret", "http://imgsrc.ru/alice/123.html?pwd=secret", "https://imgsrc.ru/alice/123.html?pwd=first&pwd=second"} {
		u, _ := url.Parse(raw)
		if imgsrcPublicQuery(u) {
			t.Errorf("unsafe query accepted %s", u.Hostname())
		}
	}
}

func TestImgsrcHiddenLoginFormDoesNotBlockVisiblePhoto(t *testing.T) {
	page, _ := url.Parse(imgsrcTestPhoto + "?pwd=already-authorized")
	body := strings.Replace(imgsrcPhotoFixture(`/alice/tape-77-123-0.html?pwd=`), "</body>", `<form style="display: none"><input type="password"></form></body>`, 1)
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	got, err := imgsrcRenderedPage(context.Background(), doc, page)
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != imgsrcTestImage1 {
		t.Fatalf("authorized visible photo=%+v err=%v", got.Items, err)
	}
}

func TestImgsrcIdentityAndPartialFailures(t *testing.T) {
	root, _ := url.Parse(imgsrcTestPhoto)
	for _, tc := range []struct {
		name, photo, tape string
		want              int
	}{
		{"locked", `<html><input type="password"></html>`, "", 0},
		{"wrong canonical", strings.Replace(imgsrcPhotoFixture(`/alice/tape-77-123-0.html`), imgsrcTestPhoto, "https://imgsrc.ru/other/123.html", 1), "", 0},
		{"mismatched image", imgsrcPhotoFixture(`/alice/tape-77-123-0.html`), imgsrcTapeFixture(imgsrcTestTape0, map[string]string{"124": imgsrcTestImage3}), 1},
		{"bad next response", imgsrcPhotoFixture(`/alice/tape-77-123-0.html`), `<html></html>`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				if raw == imgsrcTestPhoto {
					return imgsrcResponse(raw, tc.photo), nil
				}
				return imgsrcResponse(raw, tc.tape), nil
			})
			got, err := discoverImgsrc(context.Background(), getter, root)
			if err == nil || len(got.Items) != tc.want {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("cancelled request")
		return nil, nil
	})
	if _, err := discoverImgsrc(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestImgsrcRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Chrome")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	target := imgsrcTestPhoto + "?pwd=secret-value"
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(imgsrcPhotoFixture(`/alice/tape-77-123-0.html?pwd=`))))
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
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case err := <-responses:
		t.Fatalf("intercept: %v", err)
	default:
	}
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != imgsrcTestImage1 || strings.Contains(fmt.Sprint(got.Items), "secret-value") {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
}

func TestImgsrcPublicLive(t *testing.T) {
	if os.Getenv("TULAI_IMGSRC_LIVE") != "1" {
		t.Skip("set TULAI_IMGSRC_LIVE=1 for a public model ship album")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := Discover(ctx, getter, "https://imgsrc.ru/yurispol/26288940.html", 1)
	if err != nil || len(got.Items) < 100 {
		t.Fatalf("public album images=%d err=%v", len(got.Items), err)
	}
	dir := t.TempDir()
	item := got.Items[0]
	item.Filename = "ship-photo"
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("files=%v err=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil || info.Size() == 0 {
		t.Fatalf("image info=%v err=%v", info, err)
	}
	t.Logf("public album: %d images, first download %d bytes", len(got.Items), info.Size())
}
