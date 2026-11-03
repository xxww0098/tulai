package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/xxww0098/tulai/internal/camoufox"
	"golang.org/x/net/html"
)

const dribbbleTestPage = "https://dribbble.com/shots/471756-Sasquatch"
const dribbbleTestOriginal = "https://cdn.dribbble.com/userupload/18957607/file/original-ea3d065010bbcfee7c5fce0af6badfa5.png"
const dribbbleTestThumb = "https://cdn.dribbble.com/userupload/18957721/file/original-230d890d45e3400bde84bfba221ced5a.png?resize=400x300"

func dribbbleFixture(canonical, preload, body string) string {
	return `<html><head><title>作品</title><link rel="canonical" href="` + canonical + `"><meta property="og:url" content="` + canonical + `"><meta property="og:title" content="Sasquatch"><meta property="og:image:secure_url" content="` + dribbbleTestThumb + `">` + preload + `</head><body><img src="https://evil.test/avatar.jpg"><div class="js-media-container shot-media-container" data-shot-id="471756">` + body + `</div><img src="` + dribbbleTestThumb + `"></body></html>`
}
func dribbbleTestDoc(t *testing.T, s string) *html.Node {
	t.Helper()
	n, err := html.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func dribbbleTestURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func dribbbleResponse(raw, body string) *Response {
	return &Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}
}

type dribbbleTestTransport func(*http.Request) (*http.Response, error)

func (f dribbbleTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDribbbleShotReferences(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{dribbbleTestPage, "471756"}, {"https://www.dribbble.com/shots/471756-Sasquatch/", "471756"}, {"http://dribbble.com/shots/471756", "471756"},
		{"https://dribbble.com/shots/471756-%E4%BD%9C%E5%93%81", "471756"}, {"https://dribbble.com/shots/0-invalid", ""}, {"https://dribbble.com/shots/471756-", ""},
		{"https://dribbble.com/shots/471756-Sasquatch/comments", ""}, {"https://dribbble.com/shots/471756%2Fother", ""},
		{"https://dribbble.com.evil.test/shots/471756-Sasquatch", ""}, {"https://user@dribbble.com/shots/471756", ""}, {"https://dribbble.com:8443/shots/471756", ""},
	} {
		u := dribbbleTestURL(t, tc.raw)
		if id := dribbbleShotID(u); id != tc.id {
			t.Errorf("%s id=%q", tc.raw, id)
		}
	}
}
func TestDribbblePreloadMustMatchMedia(t *testing.T) {
	page := dribbbleTestURL(t, dribbbleTestPage+"?token=synthetic-secret")
	preload := `<link rel="preload" as="image" href="` + dribbbleTestOriginal + `">`
	img := `<img class="v-img content-block border-radius-8" src="` + dribbbleTestOriginal + `?resize=400x&amp;vertical=center">`
	got, err := dribbbleShot(context.Background(), page, dribbbleTestDoc(t, dribbbleFixture(dribbbleTestPage, preload, img)))
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != dribbbleTestOriginal || got.Title != "Sasquatch" {
		t.Fatalf("items=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	item := got.Items[0]
	if !item.PublicOnly || !item.RequireImage || item.Metadata["dribbble_shot_id"] != "471756" || item.Headers["Accept"] != "*/*" || strings.Contains(fmt.Sprint(item.Headers), "secret") {
		t.Fatalf("unsafe item %+v", item)
	}
	other := `<link rel="preload" as="image" href="https://cdn.dribbble.com/userupload/18957721/file/original-230d890d45e3400bde84bfba221ced5a.png">`
	got, err = dribbbleShot(context.Background(), page, dribbbleTestDoc(t, dribbbleFixture(dribbbleTestPage, other, img)))
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != dribbbleTestOriginal+"?resize=400x&vertical=center" {
		t.Fatalf("unmatched preload items=%+v err=%v", got.Items, err)
	}
}
func TestDribbbleVerifiedSiblingMedia(t *testing.T) {
	page := dribbbleTestURL(t, dribbbleTestPage)
	preload := `<link rel="preload" as="image" href="` + dribbbleTestOriginal + `">`
	img := `<img class="v-img content-block" src="` + dribbbleTestOriginal + `?resize=400x">`
	doc := `<html><head><link rel="canonical" href="` + dribbbleTestPage + `">` + preload + `</head><body>` +
		`<div class="shot-content-container"><section data-screenshot_id="471756"><div class="js-media-container" data-shot-id="471756"></div></section>` +
		`<div id="ssr-app" class="shot-page-container">` + img + `</div></div>` +
		`<div class="recommendations">` + img + `</div></body></html>`
	got, err := dribbbleShot(context.Background(), page, dribbbleTestDoc(t, doc))
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != dribbbleTestOriginal {
		t.Fatalf("verified sibling items=%+v err=%v", got.Items, err)
	}
	doc = strings.Replace(doc, `data-screenshot_id="471756"`, `data-screenshot_id="123"`, 1)
	got, err = dribbbleShot(context.Background(), page, dribbbleTestDoc(t, doc))
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("different section items=%+v err=%v", got.Items, err)
	}
}
func TestDribbbleIdentityAndAssetRestrictions(t *testing.T) {
	page := dribbbleTestURL(t, dribbbleTestPage)
	for _, raw := range []string{"http://cdn.dribbble.com/userupload/1/file/a.png", "https://evil.test/userupload/1/file/a.png", "https://cdn.dribbble.com.evil.test/userupload/1/file/a.png", "https://user@cdn.dribbble.com/userupload/1/file/a.png", "https://cdn.dribbble.com:8443/userupload/1/file/a.png", "https://cdn.dribbble.com/userupload/1/file/a.png?token=secret", "https://cdn.dribbble.com/userupload/1/file/a.png?resize=2x2&resize=3x3", "https://cdn.dribbble.com/userupload/1/file/a.svg"} {
		if got, ok := dribbbleImage(raw); ok {
			t.Errorf("unsafe asset %q", got)
		}
	}
	good := `<img class="v-img content-block" src="` + dribbbleTestOriginal + `?resize=400x">`
	for _, tc := range []struct{ name, body string }{
		{"different canonical", dribbbleFixture("https://dribbble.com/shots/123-Other", "", good)},
		{"different container", strings.Replace(dribbbleFixture(dribbbleTestPage, "", good), `data-shot-id="471756"`, `data-shot-id="123"`, 1)},
		{"unmatched media", dribbbleFixture(dribbbleTestPage, "", `<img class="v-img content-block" src="https://evil.test/a.jpg">`)},
		{"OG image only", dribbbleFixture(dribbbleTestPage, "", ``)},
		{"hidden image", dribbbleFixture(dribbbleTestPage, "", `<div hidden>`+good+`</div>`)},
		{"verification page", `<html><title>安全验证</title></html>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := dribbbleShot(context.Background(), page, dribbbleTestDoc(t, tc.body))
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
}
func TestDribbbleRedirectCancellationAndStatus(t *testing.T) {
	page := dribbbleTestURL(t, dribbbleTestPage)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("canceled request")
		return nil, nil
	})
	if _, err := discoverDribbble(ctx, getter, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		r := dribbbleResponse(raw, "")
		r.StatusCode = 202
		return r, nil
	})
	if got, err := discoverDribbble(context.Background(), getter, page); err == nil || !strings.Contains(err.Error(), "202") || len(got.Items) != 0 {
		t.Fatalf("status=%+v err=%v", got.Items, err)
	}
	client := NewHTTPGetter("")
	var external atomic.Bool
	client.client.Transport = dribbbleTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "dribbble.com" {
			external.Store(true)
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/shots/471756-Sasquatch"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if got, err := discoverDribbble(context.Background(), client, page); err == nil || len(got.Items) != 0 || external.Load() {
		t.Fatalf("external visited=%v items=%+v err=%v", external.Load(), got.Items, err)
	}
}
func TestDribbbleArchivedShot(t *testing.T) {
	file := os.Getenv("TULAI_DRIBBBLE_SAMPLE")
	if file == "" {
		t.Skip("set TULAI_DRIBBBLE_SAMPLE to an archived public shot")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw != dribbbleTestPage {
			t.Fatalf("unexpected historical request %q", raw)
		}
		return dribbbleResponse(raw, string(b)), nil
	})
	got, err := Discover(context.Background(), getter, dribbbleTestPage, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != dribbbleTestOriginal {
		t.Fatalf("historical items=%+v err=%v", got.Items, err)
	}
	t.Logf("historical shot: matched media image to explicit original preload")
}
func TestDribbblePublicImageLive(t *testing.T) {
	if os.Getenv("TULAI_DRIBBBLE_IMAGE_LIVE") != "1" {
		t.Skip("set TULAI_DRIBBBLE_IMAGE_LIVE=1 for one historically public image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	preload := `<link rel="preload" as="image" href="` + dribbbleTestOriginal + `">`
	img := `<img class="v-img content-block" src="` + dribbbleTestOriginal + `?resize=400x">`
	page := dribbbleTestURL(t, dribbbleTestPage)
	got, err := dribbbleShot(ctx, page, dribbbleTestDoc(t, dribbbleFixture(dribbbleTestPage, preload, img)))
	if err != nil || len(got.Items) != 1 {
		t.Fatalf("discovery: %+v err=%v", got.Items, err)
	}
	item := got.Items[0]
	item.Filename = "dribbble-public"
	dir := t.TempDir()
	if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil || info.Size() == 0 {
		t.Fatalf("info err=%v", err)
	}
	f, err := os.Open(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	config, format, err := image.DecodeConfig(f)
	if err != nil || format != "png" || config.Width != 400 || config.Height != 300 {
		t.Fatalf("downloaded image format=%s size=%dx%d err=%v", format, config.Width, config.Height, err)
	}
	t.Logf("historically public explicit preload downloaded %d bytes, %s %dx%d", info.Size(), format, config.Width, config.Height)
}
func TestDribbblePublicPageLive(t *testing.T) {
	if os.Getenv("TULAI_DRIBBBLE_PAGE_LIVE") != "1" {
		t.Skip("set TULAI_DRIBBBLE_PAGE_LIVE=1 to check the public page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), dribbbleTestPage, 1)
	if err == nil || len(got.Items) != 0 {
		t.Skipf("site state changed: %d items err=%v", len(got.Items), err)
	}
	t.Logf("current page: %v", err)
}
func TestDribbbleCamoufoxCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_DRIBBBLE_CAMOUFOX_ROOT")
	if runtime == "" {
		t.Skip("set TULAI_DRIBBBLE_CAMOUFOX_ROOT to a copied temporary runtime")
	}
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
	getter.client.Transport.(*http.Transport).DialContext = safeDownloadDial
	page := dribbbleTestURL(t, dribbbleTestPage)
	session := &RenderedSession{process: process, tab: tab, closeTab: closeTab, closeAllocator: process.Close, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	preload := `<link rel="preload" as="image" href="` + dribbbleTestOriginal + `">`
	img := `<img class="v-img content-block" src="` + dribbbleTestOriginal + `?resize=400x">`
	body := dribbbleFixture(dribbbleTestPage, preload, img)
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == dribbbleTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			if e := camoufox.Run(session.tab, action); e != nil {
				select {
				case responses <- e:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(dribbbleTestPage)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case e := <-responses:
		t.Fatalf("intercept: %v", e)
	default:
	}
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != dribbbleTestOriginal {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
	if os.Getenv("TULAI_DRIBBBLE_SESSION_DOWNLOAD") == "1" {
		err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil)
		if err != nil {
			t.Logf("browser-session image download unavailable: %v", err)
		} else {
			t.Log("browser-session image download succeeded")
		}
	}
}
