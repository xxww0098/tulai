package download

import (
	"context"
	"encoding/base64"
	"errors"
	"image"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/xxww0098/tulai/internal/camoufox"
	"golang.org/x/net/html"
)

func TestTwitterOriginalURLs(t *testing.T) {
	for _, tc := range []struct {
		source, raw, primary string
		alternatives         []string
	}{
		{"https://x.com/user/status/1", "https://pbs.twimg.com/media/abc?format=jpg&name=small", "https://pbs.twimg.com/media/abc?format=jpg&name=orig", []string{"https://pbs.twimg.com/media/abc?format=png&name=orig", "https://pbs.twimg.com/media/abc?format=jpg&name=small"}},
		{"https://twitter.com/user/status/1", "https://pbs.twimg.com/media/abc.png:large", "https://pbs.twimg.com/media/abc.png:orig", []string{"https://pbs.twimg.com/media/abc.png:large"}},
		{"https://twitter.com/user/status/1", "https://pbs.twimg.com/media/abc.jpg", "https://pbs.twimg.com/media/abc.jpg:orig", []string{"https://pbs.twimg.com/media/abc.jpg"}},
		{"https://x.com/user/status/1", "https://pbs.twimg.com/media/abc?format=webp&name=medium", "https://pbs.twimg.com/media/abc?format=webp&name=orig", []string{"https://pbs.twimg.com/media/abc?format=jpg&name=orig", "https://pbs.twimg.com/media/abc?format=png&name=orig", "https://pbs.twimg.com/media/abc?format=webp&name=medium"}},
		{"https://other.test/", "https://pbs.twimg.com/media/abc.png:large", "https://pbs.twimg.com/media/abc.png:large", nil},
		{"https://x.com/", "https://pbs.twimg.com.evil.test/media/abc.png:large", "https://pbs.twimg.com.evil.test/media/abc.png:large", nil},
		{"https://x.com/", "https://pbs.twimg.com/profile_images/abc.jpg", "https://pbs.twimg.com/profile_images/abc.jpg", nil},
		{"https://x.com/", "https://pbs.twimg.com/media/abc?format=jpg&name=small&signature=secret", "https://pbs.twimg.com/media/abc?format=jpg&name=small&signature=secret", nil},
	} {
		primary, alternatives := twitterOriginalURLs(tc.source, tc.raw)
		if primary != tc.primary || !reflect.DeepEqual(alternatives, tc.alternatives) {
			t.Errorf("twitterOriginalURLs(%q, %q) = %q, %v; want %q, %v", tc.source, tc.raw, primary, alternatives, tc.primary, tc.alternatives)
		}
	}
}

func TestTwitterDiscoveryPrefersOriginalAndDeduplicates(t *testing.T) {
	const page = "https://x.com/example/status/1"
	const preview = "https://pbs.twimg.com/media/abc?format=jpg&name=small"
	const original = "https://pbs.twimg.com/media/abc?format=jpg&name=orig"
	markup := `<meta property="og:url" content="` + page + `"><article><div><a href="/example/status/1/photo/1"></a><img src="` + preview + `"></div></article>` +
		`<article><div><a href="/example/status/1/photo/1"></a><img src="` + original + `"></div></article>`
	getter := twitterFixtureGetter(func(r *http.Request) (*http.Response, error) {
		return twitterTestResponse(r, http.StatusOK, markup), nil
	})
	u, _ := crawlURL(nil, page)
	found, err := discoverTwitterStatus(context.Background(), getter, u)
	if err != nil || len(found.Items) != 1 || found.Items[0].URL != original || len(found.Items[0].AlternateURLs) != 2 {
		t.Fatalf("static discovery = %+v, %v", found.Items, err)
	}
}

const twitterTestPage = "https://x.com/NASA/status/2041557036274475228"
const twitterTestMedia1 = "https://pbs.twimg.com/media/HFURRWmXYAEcq7c?format=webp&name=large"
const twitterTestMedia2 = "https://pbs.twimg.com/media/HFURR76XEAAFtWC?format=jpg&name=small"

func twitterTestResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Request: r, Body: io.NopCloser(strings.NewReader(body))}
}

func twitterFixtureGetter(roundTrip twitterRoundTrip) *HTTPGetter {
	getter := NewHTTPGetter("")
	getter.client.Transport = roundTrip
	return getter
}

func twitterTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func twitterTestDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func twitterTestFixture() string {
	return `<html><head><meta property="og:url" content="` + twitterTestPage + `"></head><body>` +
		`<article><div><a href="/NASA/status/2041557036274475228/photo/1"></a><img src="` + strings.ReplaceAll(twitterTestMedia1, "&", "&amp;") + `"></div>` +
		`<div><a href="/NASA/status/2041557036274475228/photo/2"></a><img src="` + strings.ReplaceAll(twitterTestMedia2, "&", "&amp;") + `"></div>` +
		`<div><a href="/someone/status/987/photo/1"></a><img src="https://pbs.twimg.com/media/quoted?format=jpg&amp;name=small"></div></article>` +
		`<article><div><a href="/other/status/456/photo/1"></a><img src="https://pbs.twimg.com/media/reply?format=jpg&amp;name=small"></div></article>` +
		`<img src="https://pbs.twimg.com/profile_images/avatar.jpg"></body></html>`
}

func TestTwitterStatusRoutes(t *testing.T) {
	for _, tc := range []struct {
		raw, author, id string
		photo           int
	}{
		{twitterTestPage, "nasa", "2041557036274475228", 0},
		{twitterTestPage + "/photo/2", "nasa", "2041557036274475228", 2},
		{"https://twitter.com/NASA/status/123?s=20", "nasa", "123", 0},
		{"https://mobile.twitter.com/NASA/status/123", "nasa", "123", 0},
		{"https://x.com/i/status/123", "", "123", 0},
		{"https://x.com/i/web/status/123", "", "123", 0},
		{"https://x.com/i/web/status/123/photo/1", "", "", 0},
		{"https://x.com/NASA/status/0", "", "", 0},
		{"https://x.com/NASA/status/123/photo/21", "", "", 0},
		{"https://x.com.evil.test/NASA/status/123", "", "", 0},
		{"https://user@x.com/NASA/status/123", "", "", 0},
		{"https://x.com:8443/NASA/status/123", "", "", 0},
	} {
		got := twitterStatus(twitterTestURL(t, tc.raw))
		if got.author != tc.author || got.id != tc.id || got.photo != tc.photo {
			t.Errorf("route %s = %+v", tc.raw, got)
		}
	}
	if !twitterSameStatus(twitterTestURL(t, "http://twitter.com/NASA/status/123"), twitterTestURL(t, "https://x.com/nasa/status/123")) {
		t.Fatal("safe upgrade or alias rejected")
	}
	if twitterSameStatus(twitterTestURL(t, "https://x.com/NASA/status/123"), twitterTestURL(t, "https://x.com/other/status/123")) {
		t.Fatal("accepted wrong author")
	}
	if twitterSameStatus(twitterTestURL(t, "https://x.com/NASA/status/123/photo/1"), twitterTestURL(t, "https://x.com/NASA/status/123/photo/2")) {
		t.Fatal("accepted wrong selected photo")
	}
	short := twitterTestURL(t, "https://twitter.com/i/web/status/123")
	final := twitterTestURL(t, "https://x.com/NASA/status/123")
	if !twitterSameStatus(short, final) || twitterSameStatus(final, short) || twitterSameStatus(short, twitterTestURL(t, "https://x.com/NASA/status/124")) {
		t.Fatal("invalid short URL identity check")
	}
	if twitterCapturePage(short, final) != final || twitterCapturePage(final, short) != nil {
		t.Fatal("short URL capture not bound to resolved author")
	}
}

func TestTwitterCollectIdentityAndBoundaries(t *testing.T) {
	page := twitterTestURL(t, twitterTestPage)
	got, err := twitterCollect(context.Background(), page, twitterTestDoc(t, twitterTestFixture()))
	if err != nil || len(got.Items) != 2 || got.Items[0].Metadata["twitter_photo_index"] != "1" || got.Items[1].Metadata["twitter_photo_index"] != "2" {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Metadata["twitter_author"] != "nasa" || item.Metadata["twitter_status_id"] != "2041557036274475228" || item.Headers["Referer"] != "https://x.com/" {
			t.Fatalf("unsafe item %+v", item)
		}
	}
	if !strings.Contains(got.Items[0].URL, "name=orig") || len(got.Items[0].AlternateURLs) != 3 {
		t.Fatalf("format fallbacks: %+v", got.Items[0])
	}
	selected, err := twitterCollect(context.Background(), twitterTestURL(t, twitterTestPage+"/photo/2"), twitterTestDoc(t, twitterTestFixture()))
	if err != nil || len(selected.Items) != 1 || selected.Items[0].Metadata["twitter_photo_index"] != "2" {
		t.Fatalf("selected=%+v err=%v", selected.Items, err)
	}
	legacy := twitterTestURL(t, "http://twitter.com/NASA/status/2041557036274475228?private=synthetic")
	legacyResult, err := twitterCollect(context.Background(), legacy, twitterTestDoc(t, twitterTestFixture()))
	if err != nil || len(legacyResult.Items) != 2 || legacyResult.Items[0].Headers["Referer"] != "https://twitter.com/" {
		t.Fatalf("legacy alias=%+v err=%v", legacyResult.Items, err)
	}
	getter := twitterFixtureGetter(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/photo/2") {
			resp := twitterTestResponse(r, http.StatusFound, "")
			resp.Header.Set("Location", twitterTestPage)
			return resp, nil
		}
		return twitterTestResponse(r, http.StatusOK, twitterTestFixture()), nil
	})
	selected, err = discoverTwitterStatus(context.Background(), getter, twitterTestURL(t, twitterTestPage+"/photo/2"))
	if err != nil || len(selected.Items) != 1 || selected.Items[0].Metadata["twitter_photo_index"] != "2" {
		t.Fatalf("selected after redirect=%+v err=%v", selected.Items, err)
	}
	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"wrong-canonical", strings.Replace(twitterTestFixture(), twitterTestPage, "https://x.com/other/status/2041557036274475228", 1), 0},
		{"login-wall", strings.Replace(twitterTestFixture(), `<body>`, `<body><div role="dialog"><input type="password"></div>`, 1), 0},
		{"no-article", `<a href="/NASA/status/2041557036274475228/photo/1"><img src="` + twitterTestMedia1 + `"></a>`, 0},
		{"wrong-author", strings.ReplaceAll(twitterTestFixture(), "/NASA/status/2041557036274475228/photo/", "/other/status/2041557036274475228/photo/"), 0},
		{"wrong-status", strings.ReplaceAll(twitterTestFixture(), "/NASA/status/2041557036274475228/photo/", "/NASA/status/999/photo/"), 0},
		{"invalid-cdn", strings.Replace(twitterTestFixture(), "pbs.twimg.com/media/HFURRWmXYAEcq7c", "evil.test/media/HFURRWmXYAEcq7c", 1), 1},
		{"conflict", strings.Replace(twitterTestFixture(), `</body>`, `<article><div><a href="/NASA/status/2041557036274475228/photo/1"></a><img src="https://pbs.twimg.com/media/conflict?format=jpg&amp;name=large"></div></article></body>`, 1), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := twitterCollect(context.Background(), page, twitterTestDoc(t, tc.body))
			if err == nil || len(got.Items) != tc.want {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = twitterCollect(ctx, page, twitterTestDoc(t, twitterTestFixture()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestTwitterShortURLRedirects(t *testing.T) {
	short := "https://twitter.com/i/web/status/2041557036274475228?private=synthetic"
	for _, tc := range []struct {
		name, start, location string
		accept                bool
	}{
		{"resolved-author", short, twitterTestPage, true},
		{"outside-host", short, "https://evil.test/NASA/status/2041557036274475228", false},
		{"wrong-id", short, "https://x.com/NASA/status/2041557036274475229", false},
		{"insecure", short, "http://x.com/NASA/status/2041557036274475228", false},
		{"known-author-changed", twitterTestPage, "https://x.com/other/status/2041557036274475228", false},
		{"known-author-lost", twitterTestPage, "https://x.com/i/status/2041557036274475228", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			getter := twitterFixtureGetter(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					resp := twitterTestResponse(r, http.StatusTemporaryRedirect, "")
					resp.Header.Set("Location", tc.location)
					return resp, nil
				}
				if !tc.accept {
					t.Errorf("issued request to rejected redirect %s", r.URL)
				}
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://twitter.com/" {
					t.Errorf("unsafe redirect headers: %v", r.Header)
				}
				return twitterTestResponse(r, http.StatusOK, twitterTestFixture()), nil
			})
			got, err := discoverTwitterStatus(context.Background(), getter, twitterTestURL(t, tc.start))
			if tc.accept && (err != nil || len(got.Items) != 2 || calls != 2) {
				t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
			}
			if !tc.accept && (err == nil || len(got.Items) != 0 || calls != 1) {
				t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
			}
		})
	}
	called := false
	unsafeGetter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		called = true
		return nil, nil
	})
	if _, err := discoverTwitterStatus(context.Background(), unsafeGetter, twitterTestURL(t, short)); err == nil || called {
		t.Fatal("uncontrolled getter accepted")
	}
	// A short URL may resolve an author once, but later hops cannot replace it.
	calls := 0
	getter := twitterFixtureGetter(func(r *http.Request) (*http.Response, error) {
		calls++
		resp := twitterTestResponse(r, http.StatusTemporaryRedirect, "")
		if calls == 1 {
			resp.Header.Set("Location", twitterTestPage)
		} else {
			resp.Header.Set("Location", "https://x.com/other/status/2041557036274475228")
		}
		return resp, nil
	})
	if _, err := discoverTwitterStatus(context.Background(), getter, twitterTestURL(t, short)); err == nil || calls != 2 {
		t.Fatalf("author changed after resolution: calls=%d err=%v", calls, err)
	}
}

func TestTwitterPublicHTMLSample(t *testing.T) {
	file := os.Getenv("TULAI_TWITTER_HTML_SAMPLE")
	if file == "" {
		t.Skip("set TULAI_TWITTER_HTML_SAMPLE to a public X post HTML sample")
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := html.Parse(io.LimitReader(f, maxPageBytes))
	if err != nil {
		t.Fatal(err)
	}
	got, err := twitterCollect(context.Background(), twitterTestURL(t, twitterTestPage), doc)
	if err != nil || len(got.Items) != 4 {
		t.Fatalf("public sample items=%d err=%v", len(got.Items), err)
	}
	for _, item := range got.Items {
		if item.Metadata["twitter_status_id"] != "2041557036274475228" || item.Metadata["twitter_author"] != "nasa" {
			t.Fatalf("wrong owner %+v", item.Metadata)
		}
	}
}

func TestTwitterPublicStatusAndDownload(t *testing.T) {
	if os.Getenv("TULAI_TWITTER_PUBLIC_LIVE") != "1" {
		t.Skip("set TULAI_TWITTER_PUBLIC_LIVE=1 for public X post and image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	got, err := discoverTwitterStatus(ctx, getter, twitterTestURL(t, twitterTestPage))
	if err != nil || len(got.Items) != 4 {
		t.Fatalf("public status items=%d err=%v", len(got.Items), err)
	}
	short := twitterTestURL(t, "https://x.com/i/web/status/2041557036274475228")
	resolved, err := discoverTwitterStatus(ctx, getter, short)
	if err != nil || len(resolved.Items) != 4 || resolved.Items[0].Metadata["twitter_author"] != "nasa" {
		t.Fatalf("resolved short status items=%d err=%v", len(resolved.Items), err)
	}
	dir := t.TempDir()
	if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, got.Items[0], nil); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	f, err := os.Open(filepath.Join(dir, files[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil || cfg.Width == 0 || cfg.Height == 0 {
		t.Fatalf("image decode: %v", err)
	}
	t.Logf("public image decoded as %s, %dx%d", format, cfg.Width, cfg.Height)
}

type twitterRoundTrip func(*http.Request) (*http.Response, error)

func (f twitterRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTwitterRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated browser integration")
	}
	dataDir := os.Getenv("TULAI_CAMOUFOX_TEST_DATA")
	if dataDir == "" {
		t.Skip("set TULAI_CAMOUFOX_TEST_DATA to a disposable copy of the Camoufox runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	process, err := camoufox.Start(ctx, dataDir, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(process.Context())
	getter := NewHTTPGetter("")
	getter.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := getter.client.Transport.(*http.Transport)
	transport.DialContext, transport.DisableKeepAlives = safeDownloadDial, true
	session := &RenderedSession{process: process, tab: camoufox.PageContext(life, process, process.Page), closeTab: closeTab,
		closeAllocator: func() { process.Close() }, releaseProfile: func() {}, getter: getter, start: twitterTestURL(t, twitterTestPage)}
	defer session.Close()
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == twitterTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(twitterTestFixture())))
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
	session.start = twitterTestURL(t, twitterTestPage)
	if err := session.run(ctx, camoufox.Navigate(twitterTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".x.com").WithURL("https://x.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = twitterRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "pbs.twimg.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://x.com/" {
			t.Errorf("unsafe CDN request: %v", r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || session.PageURL() != twitterTestPage {
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
	if calls != 1 {
		t.Fatalf("CDN calls=%d", calls)
	}
	// A legacy alias may redirect to the current host and drop /photo/N.
	// Capture must keep the requested index, without putting its query in the referrer.
	session.start = twitterTestURL(t, "https://twitter.com/NASA/status/2041557036274475228/photo/2?private=synthetic")
	selected, err := session.Capture(ctx)
	if err != nil || len(selected.Items) != 1 || selected.Items[0].Metadata["twitter_photo_index"] != "2" {
		t.Fatalf("selected=%+v err=%v", selected.Items, err)
	}
	resp, err = session.Getter().Get(ctx, selected.Items[0].URL, func(r *http.Request) {
		for k, v := range selected.Items[0].Headers {
			r.Header.Set(k, v)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 2 {
		t.Fatalf("CDN calls=%d", calls)
	}
	// Unresolved short URLs must be collected against the final author's URL.
	session.start = twitterTestURL(t, "https://x.com/i/status/2041557036274475228")
	resolved, err := session.Capture(ctx)
	if err != nil || len(resolved.Items) != 2 || resolved.Items[0].Metadata["twitter_author"] != "nasa" {
		t.Fatalf("short status items=%+v err=%v", resolved.Items, err)
	}
	if os.Getenv("TULAI_TWITTER_BROWSER_LIVE") == "1" {
		session.getter.client.Transport = transport
		dir := t.TempDir()
		err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: dir}, resolved.Items[0], nil)
		if err != nil {
			t.Logf("public CDN through browser getter unavailable: %v", err)
		} else {
			t.Log("public CDN download through browser getter succeeded")
		}
	}
	for _, wrong := range []string{
		"https://x.com/i/status/2041557036274475229",
		"https://x.com/other/status/2041557036274475228",
	} {
		session.start = twitterTestURL(t, wrong)
		if got, err := session.Capture(ctx); err == nil || len(got.Items) != 0 {
			t.Fatalf("capture accepted different post or author: %+v, %v", got.Items, err)
		}
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

const twitterProfileTestPage = "https://x.com/NASA"

func twitterProfileFixture() string {
	image1 := strings.ReplaceAll(twitterTestMedia1, "&", "&amp;")
	image2 := strings.ReplaceAll(twitterTestMedia2, "&", "&amp;")
	return `<html><head><meta property="og:url" content="` + twitterProfileTestPage + `"></head><body>` +
		`<article><div><a href="/NASA">NASA</a><a href="/NASA/status/101">time</a></div>` +
		`<div><img src="` + image1 + `"><a style="display:inline-block;width:10px;height:10px" href="/NASA/status/101/photo/1"></a></div>` +
		`<div><img src="` + image2 + `"><a style="display:inline-block;width:10px;height:10px" href="/NASA/status/101/photo/2"></a></div></article>` +
		`<article><div><a href="/NASA">NASA</a><a href="/NASA/status/102">time</a></div>` +
		`<div><img src="https://pbs.twimg.com/media/third?format=jpg&amp;name=small"><a style="display:inline-block;width:10px;height:10px" href="/NASA/status/102/photo/1"></a></div></article>` +
		`<article><div><a href="/NASA">NASA</a><a href="/NASA/status/103">time</a></div>` +
		`<div><img src="https://pbs.twimg.com/media/outer?format=jpg&amp;name=small"><a href="/NASA/status/103/photo/1"></a></div>` +
		`<div><a href="/other/status/999">quoted</a><img src="https://pbs.twimg.com/media/quote?format=jpg&amp;name=small"><a href="/other/status/999/photo/1"></a></div></article>` +
		`<article><div><a href="/other">other</a><a href="/other/status/104">time</a></div>` +
		`<div><img src="https://pbs.twimg.com/media/other?format=jpg&amp;name=small"><a href="/other/status/104/photo/1"></a></div></article>` +
		`<article><div><a href="/NASA">NASA</a><a href="/NASA/status/105">time</a></div>` +
		`<article><div><a href="/NASA">NASA</a><a href="/NASA/status/106">nested</a></div><div><img src="https://pbs.twimg.com/media/nested?format=jpg&amp;name=small"><a href="/NASA/status/106/photo/1"></a></div></article></article>` +
		`<img src="https://pbs.twimg.com/profile_images/avatar.jpg"></body></html>`
}

func TestTwitterProfileRoutes(t *testing.T) {
	for _, tc := range []struct {
		raw            string
		profile, media bool
	}{
		{twitterProfileTestPage, true, false},
		{"https://twitter.com/NASA/", true, false},
		{"https://x.com/NASA/media", false, true},
		{"https://mobile.twitter.com/NASA/media/", false, true},
		{"https://x.com/i/media", false, false},
		{"https://x.com/home", false, false},
		{"https://x.com/search/media", false, false},
		{"https://x.com/NASA/status/123", false, false},
		{"https://x.com.evil.test/NASA/media", false, false},
		{"https://user@x.com/NASA", false, false},
		{"https://x.com:8443/NASA/media", false, false},
	} {
		u := twitterTestURL(t, tc.raw)
		if isTwitterProfile(u) != tc.profile || isTwitterProfileMedia(u) != tc.media {
			t.Errorf("route %s: profile=%v media=%v", tc.raw, isTwitterProfile(u), isTwitterProfileMedia(u))
		}
	}
	if !twitterSameProfile(twitterTestURL(t, "http://twitter.com/NASA"), twitterTestURL(t, "https://x.com/nasa")) {
		t.Fatal("safe profile alias and HTTPS upgrade rejected")
	}
	for _, target := range []string{"https://x.com/other", "https://x.com/NASA/media", "http://x.com/NASA", "https://x.com.evil.test/NASA"} {
		if twitterSameProfile(twitterTestURL(t, twitterProfileTestPage), twitterTestURL(t, target)) {
			t.Errorf("accepted different profile %s", target)
		}
	}
	called := false
	getter := twitterFixtureGetter(func(r *http.Request) (*http.Response, error) {
		called = true
		return twitterTestResponse(r, http.StatusOK, ""), nil
	})
	if result, err := Discover(context.Background(), getter, "https://x.com/NASA/media", 1); err == nil || !result.IsPage || called {
		t.Fatalf("media route should not issue a request: called=%v result=%+v err=%v", called, result, err)
	}
}

func TestTwitterProfileCollect(t *testing.T) {
	page := twitterTestURL(t, twitterProfileTestPage+"?private=synthetic")
	got, err := twitterProfileCollect(context.Background(), page, twitterTestDoc(t, twitterProfileFixture()))
	if err == nil || !strings.Contains(err.Error(), "分页尚未验证") || len(got.Items) != 3 {
		t.Fatalf("profile items=%+v err=%v", got.Items, err)
	}
	for _, item := range got.Items {
		if item.Metadata["twitter_author"] != "nasa" || (item.Metadata["twitter_status_id"] != "101" && item.Metadata["twitter_status_id"] != "102") ||
			item.Headers["Referer"] != "https://x.com/" || !item.PublicOnly || !item.RequireImage {
			t.Fatalf("unbound or unsafe item %+v", item)
		}
	}
	if got.Items[0].Metadata["twitter_photo_index"] != "1" || got.Items[1].Metadata["twitter_photo_index"] != "2" || len(got.Items[0].AlternateURLs) != 3 {
		t.Fatalf("photo order or fallbacks: %+v", got.Items)
	}
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"transparent-article", strings.Replace(twitterProfileFixture(), `<article>`, `<article style="opacity:0.0">`, 1), 1},
		{"zero-width-article", strings.Replace(twitterProfileFixture(), `<article>`, `<article style="width:0px">`, 1), 1},
		{"zero-height-image", strings.Replace(twitterProfileFixture(), `<img src="`, `<img height="0" src="`, 1), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := twitterProfileCollect(context.Background(), page, twitterTestDoc(t, tc.body))
			if err == nil || len(got.Items) != tc.want {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
	for _, tc := range []struct {
		name, body string
	}{
		{"different-author", strings.Replace(twitterProfileFixture(), twitterProfileTestPage, "https://x.com/other", 1)},
		{"missing-canonical", strings.Replace(twitterProfileFixture(), `<meta property="og:url" content="`+twitterProfileTestPage+`">`, "", 1)},
		{"login", strings.Replace(twitterProfileFixture(), `<body>`, `<body><div role="dialog"><input type="password"></div>`, 1)},
		{"protected", strings.Replace(twitterProfileFixture(), `<body>`, `<body><svg data-testid="icon-lock" aria-label="Protected account"></svg>`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := twitterProfileCollect(context.Background(), page, twitterTestDoc(t, tc.body))
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("items=%+v err=%v", got.Items, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := twitterProfileCollect(ctx, page, twitterTestDoc(t, twitterProfileFixture())); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestTwitterProfileRedirectBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, destination string
		accept            bool
	}{
		{"alias", "https://twitter.com/NASA", true},
		{"other-author", "https://x.com/other", false},
		{"media", "https://x.com/NASA/media", false},
		{"login", "https://x.com/i/flow/login", false},
		{"external", "https://evil.test/NASA", false},
		{"downgrade", "http://x.com/NASA", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			getter := twitterFixtureGetter(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					resp := twitterTestResponse(r, http.StatusTemporaryRedirect, "")
					resp.Header.Set("Location", tc.destination)
					return resp, nil
				}
				if !tc.accept || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://x.com/" {
					t.Errorf("invalid redirect request: %s %v", r.URL, r.Header)
				}
				return twitterTestResponse(r, http.StatusOK, twitterProfileFixture()), nil
			})
			got, err := discoverTwitterProfile(context.Background(), getter, twitterTestURL(t, twitterProfileTestPage+"?private=synthetic"))
			if tc.accept && (err == nil || !strings.Contains(err.Error(), "分页尚未验证") || calls != 2 || len(got.Items) != 3) {
				t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
			}
			if !tc.accept && (err == nil || calls != 1 || len(got.Items) != 0) {
				t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
			}
		})
	}
}

func TestTwitterPublicProfileHTMLSample(t *testing.T) {
	file := os.Getenv("TULAI_TWITTER_PROFILE_HTML_SAMPLE")
	if file == "" {
		t.Skip("set TULAI_TWITTER_PROFILE_HTML_SAMPLE to a public X author page")
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := html.Parse(io.LimitReader(f, maxPageBytes))
	if err != nil {
		t.Fatal(err)
	}
	got, err := twitterProfileCollect(context.Background(), twitterTestURL(t, twitterProfileTestPage), doc)
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("hidden streamed profile must not yield images: items=%d err=%v", len(got.Items), err)
	}
}

func TestTwitterPublicProfileBrowser(t *testing.T) {
	if os.Getenv("TULAI_TWITTER_PROFILE_BROWSER_LIVE") != "1" || os.Getenv("TULAI_CAMOUFOX_TEST_DATA") == "" {
		t.Skip("set TULAI_TWITTER_PROFILE_BROWSER_LIVE=1 and TULAI_CAMOUFOX_TEST_DATA for an isolated public page")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	process, err := camoufox.Start(ctx, os.Getenv("TULAI_CAMOUFOX_TEST_DATA"), camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Skipf("isolated browser unavailable: %v", err)
	}
	life, closeTab := context.WithCancel(process.Context())
	session := &RenderedSession{process: process, tab: camoufox.PageContext(life, process, process.Page), closeTab: closeTab,
		closeAllocator: func() { process.Close() }, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: twitterTestURL(t, twitterProfileTestPage)}
	defer session.Close()
	if err := session.run(ctx, camoufox.Navigate(twitterProfileTestPage)); err != nil {
		t.Skipf("public profile navigation unavailable: %v", err)
	}
	if err := Wait(ctx, 2*time.Second); err != nil {
		t.Skipf("public profile did not load: %v", err)
	}
	got, err := session.Capture(ctx)
	if len(got.Items) == 0 {
		t.Skipf("public rendered profile has no confirmed photos: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "分页尚未验证") {
		t.Fatalf("public profile items=%d err=%v", len(got.Items), err)
	}
	for _, item := range got.Items {
		if item.Metadata["twitter_author"] != "nasa" || item.Metadata["twitter_status_id"] == "" || item.Metadata["twitter_photo_index"] == "" {
			t.Fatalf("unbound public profile item: %+v", item)
		}
	}
	t.Logf("public rendered profile: %d images", len(got.Items))
}

func TestTwitterProfileRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" || os.Getenv("TULAI_CAMOUFOX_TEST_DATA") == "" {
		t.Skip("set TULAI_RENDERED_E2E=1 and TULAI_CAMOUFOX_TEST_DATA to a disposable Camoufox runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	process, err := camoufox.Start(ctx, os.Getenv("TULAI_CAMOUFOX_TEST_DATA"), camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(process.Context())
	page := twitterTestURL(t, twitterProfileTestPage)
	session := &RenderedSession{process: process, tab: camoufox.PageContext(life, process, process.Page), closeTab: closeTab,
		closeAllocator: func() { process.Close() }, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: page}
	defer session.Close()
	hiddenCards := `<style>.fixture-hidden{display:none}.fixture-hidden-image{visibility:hidden}</style>` +
		`<article class="fixture-hidden"><div><a href="/NASA">NASA</a><a href="/NASA/status/107">time</a></div><div><img src="https://pbs.twimg.com/media/csshidden?format=jpg&amp;name=small"><a style="display:inline-block;width:10px;height:10px" href="/NASA/status/107/photo/1"></a></div></article>` +
		`<article><div><a href="/NASA">NASA</a><a href="/NASA/status/108">time</a></div><div><img class="fixture-hidden-image" src="https://pbs.twimg.com/media/csshiddenimage?format=jpg&amp;name=small"><a style="display:inline-block;width:10px;height:10px" href="/NASA/status/108/photo/1"></a></div></article>`
	body := strings.Replace(twitterProfileFixture(), `</body>`, hiddenCards+`</body>`, 1)
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == twitterProfileTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else if u, err := url.Parse(paused.Request.URL); err == nil && paused.ResourceType == network.ResourceTypeImage && u.Hostname() == "pbs.twimg.com" {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "image/gif"}}).WithBody("R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==")
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
	if err := session.run(ctx, fetch.Enable(), camoufox.Navigate(twitterProfileTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".x.com").WithURL("https://x.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = twitterRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "pbs.twimg.com" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://x.com/" {
			t.Errorf("unsafe CDN request: %v", r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "分页尚未验证") || len(got.Items) != 3 || session.PageURL() != twitterProfileTestPage {
		t.Fatalf("profile items=%+v page=%s err=%v", got.Items, session.PageURL(), err)
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
	if calls != 1 {
		t.Fatalf("CDN calls=%d", calls)
	}
	session.start = twitterTestURL(t, "https://x.com/other")
	if result, err := session.Capture(ctx); err == nil || len(result.Items) != 0 {
		t.Fatalf("different author accepted: %+v, %v", result.Items, err)
	}
	session.start = page
	if err := session.run(ctx, chromedp.Evaluate(`document.body.insertAdjacentHTML('beforeend', '<svg width="10" height="10" data-testid="icon-lock" aria-label="Protected account"></svg>')`, nil)); err != nil {
		t.Fatal(err)
	}
	if result, err := session.Capture(ctx); err == nil || len(result.Items) != 0 {
		t.Fatalf("protected account accepted: %+v, %v", result.Items, err)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept: %v", err)
	default:
	}
}

func TestTwitterOriginalLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_TWITTER_LIVE") != "1" {
		t.Skip("set TULAI_TWITTER_LIVE=1 to check a public X image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	primary, alternatives := twitterOriginalURLs("https://x.com/example/status/1", "https://pbs.twimg.com/media/CiMNh9rWEAAdM6Q?format=jpg&name=medium")
	dir := t.TempDir()
	err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, Item{URL: primary, AlternateURLs: alternatives, Filename: "image", RequireImage: true}, nil)
	data, readErr := os.ReadFile(filepath.Join(dir, "image.png"))
	if err != nil || readErr != nil || !strings.HasPrefix(string(data), "\x89PNG\r\n\x1a\n") {
		t.Fatalf("PNG original: download=%v, read=%v, bytes=%d", err, readErr, len(data))
	}
}
