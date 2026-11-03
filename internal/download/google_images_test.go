package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/xxww0098/tulai/internal/camoufox"
)

const googleImagesTestPage = "https://www.google.com/search?q=moon&tbm=isch&tbs=isz%3Al"

func googleImagesTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestGoogleImagesRoutesAndSearchIdentity(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{googleImagesTestPage, true},
		{"https://images.google.co.jp/search?q=moon&udm=2", true},
		{"https://www.google.com.hk/search?q=moon&tbm=isch", true},
		{"https://www.google.com/search?tbm=isch", true}, // recognized so an invalid query cannot fall through to generic image scanning
		{"https://www.google.com/search?q=moon&tbm=isch&tbm=other", false},
		{"https://www.google.com/search?q=moon&tbm=isch&udm=3", false},
		{"https://www.google.com/search?q=moon", false},
		{"https://www.google.com/imgres?q=moon&udm=2", false},
		{"https://google.example/search?q=moon&udm=2", false},
		{"https://www.google.com.evil.test/search?q=moon&udm=2", false},
		{"https://www.images.google.com/search?q=moon&udm=2", false},
		{"https://evil.google.com/search?q=moon&udm=2", false},
		{"https://user@google.com/search?q=moon&udm=2", false},
		{"https://www.google.com:8443/search?q=moon&udm=2", false},
	} {
		if got := isGoogleImageSearch(googleImagesTestURL(t, tc.raw)); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.raw, got, tc.want)
		}
	}
	start := googleImagesTestURL(t, googleImagesTestPage)
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://google.com/search?udm=2&q=moon&tbs=isz%3Al", true},
		{"https://www.google.com/search?udm=2&q=mars&tbs=isz%3Al", false},
		{"https://www.google.com/search?udm=2&q=moon&tbs=isz%3Am", false},
		{"https://www.google.com/search?udm=2&q=moon", false},
		{"https://www.google.com/search?udm=2&q=moon&q=mars&tbs=isz%3Al", false},
		{"https://www.google.co.jp/search?udm=2&q=moon&tbs=isz%3Al", false},
		{"http://www.google.com/search?udm=2&q=moon&tbs=isz%3Al", false},
	} {
		if got := googleImagesSameSearch(start, googleImagesTestURL(t, tc.raw)); got != tc.want {
			t.Errorf("same search %s: got %v want %v", tc.raw, got, tc.want)
		}
	}
	withoutFilter := googleImagesTestURL(t, "https://www.google.com/search?q=moon&udm=2")
	if googleImagesSameSearch(withoutFilter, start) {
		t.Fatal("added image filter changed the requested search")
	}
	if googleImagesSameSearch(withoutFilter, googleImagesTestURL(t, "https://www.google.com/search?q=moon&udm=2&lr=lang_en")) {
		t.Fatal("added language filter changed the requested search")
	}
}

func TestGoogleImagesCollectBoundRows(t *testing.T) {
	page := googleImagesTestURL(t, googleImagesTestPage+"&token=synthetic")
	row := googleImagesRow{Image: "https://cdn.example.test/image.jpg", Source: "https://publisher.example.test/page", ID: "thumb1", DocID: "doc1", Title: "Moon", Width: "1200", Height: "800"}
	snapshot := googleImagesSnapshot{Rows: []googleImagesRow{
		row,
		{Image: "https://cdn.example.test/second.png", Source: "https://publisher.example.test/two", ID: "thumb2", DocID: "doc2"},
		{Image: row.Image, Source: row.Source, ID: "thumb3", DocID: "doc1"},
		{Image: "http://127.0.0.1/secret.jpg", Source: row.Source, ID: "local", DocID: "doc4"},
		{Image: "https://encrypted-tbn0.gstatic.com/images?q=tbn:abc", Source: row.Source, ID: "thumbnail", DocID: "doc5"},
		{Image: "https://cdn.example.test/a.jpg", Source: row.Source, ID: "conflict", DocID: "doc6"},
		{Image: "https://cdn.example.test/b.jpg", Source: row.Source, ID: "conflict", DocID: "doc6"},
		{Image: "https://cdn.example.test/no-source.jpg", ID: "bad", DocID: "doc7"},
	}}
	got, err := googleImagesCollect(context.Background(), page, snapshot)
	if err == nil || !strings.Contains(err.Error(), "分页尚未验证") || len(got.Items) != 2 {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
	item := got.Items[0]
	if item.URL != row.Image || !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://www.google.com/" ||
		item.Metadata["google_query"] != "moon" || item.Metadata["google_source"] != row.Source || item.Metadata["google_width"] != "1200" {
		t.Fatalf("result metadata/referrer=%+v", item)
	}
	blocked, err := googleImagesCollect(context.Background(), page, googleImagesSnapshot{Blocked: true, Rows: []googleImagesRow{row}})
	if err == nil || len(blocked.Items) != 0 || !strings.Contains(err.Error(), "验证") {
		t.Fatalf("blocked=%+v err=%v", blocked.Items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := googleImagesCollect(ctx, page, snapshot); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if _, err := discoverGoogleImages(context.Background(), nil, page); err == nil || !strings.Contains(err.Error(), "安全重定向") {
		t.Fatalf("ordinary mode err=%v", err)
	}
}

func TestGoogleImagesCollectLimit(t *testing.T) {
	rows := make([]googleImagesRow, maxGoogleImages+1)
	for i := range rows {
		rows[i] = googleImagesRow{Image: fmt.Sprintf("https://cdn.example.test/%d.jpg", i), Source: "https://publisher.example.test/page", ID: fmt.Sprintf("thumb%d", i), DocID: "doc"}
	}
	got, err := googleImagesCollect(context.Background(), googleImagesTestURL(t, googleImagesTestPage), googleImagesSnapshot{Rows: rows})
	if err == nil || !strings.Contains(err.Error(), "上限") || len(got.Items) != maxGoogleImages {
		t.Fatalf("items=%d err=%v", len(got.Items), err)
	}
}

type googleImagesRoundTrip func(*http.Request) (*http.Response, error)

func (f googleImagesRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func googleImagesFixtureGetter(roundTrip googleImagesRoundTrip) *HTTPGetter {
	getter := NewHTTPGetter("")
	getter.client.Transport = roundTrip
	return getter
}

func googleImagesFixtureResponse(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Request: r, Body: io.NopCloser(strings.NewReader(body))}
}

func googleImagesStaticFixture() string {
	return `<html><body><form action="/search"><input name="q" value="moon"></form><div class="islrtb isv-r" data-docid="doc1" data-tbnid="thumb1" data-ou="https://cdn.example.test/one.jpg" data-ru="https://publisher.example.test/page" data-oh="800" data-ow="1200"><img src="https://encrypted-tbn0.gstatic.com/images?q=tbn:abc"></div>` +
		`<div class="islrtb isv-r" style="display:none" data-docid="doc2" data-tbnid="thumb2" data-ou="https://cdn.example.test/hidden.jpg" data-ru="https://publisher.example.test/page"><img src="https://example.test/thumb.jpg"></div>` +
		`<div class="islrtb isv-r" data-docid="doc3" data-tbnid="thumb3" data-ou="https://cdn.example.test/no-preview.jpg" data-ru="https://publisher.example.test/page"></div>` +
		`<div class="islrtb isv-r" data-docid="doc4" data-tbnid="thumb4" data-ou="https://cdn.example.test/hidden-preview.jpg" data-ru="https://publisher.example.test/page"><img style="opacity:0.0" src="https://example.test/thumb.jpg"></div>` +
		`<div class="islrtb isv-r" data-docid="doc5" data-tbnid="thumb5" data-ou="https://cdn.example.test/invalid-preview.jpg" data-ru="https://publisher.example.test/page"><img src="file:///tmp/secret.jpg"></div>` +
		`<div class="islrtb isv-r" data-docid="doc6" data-tbnid="thumb6" data-ou="https://cdn.example.test/zero-preview.jpg" data-ru="https://publisher.example.test/page"><img width="0" src="https://example.test/thumb.jpg"></div>` +
		`<div class="islrtb isv-r" data-docid="doc7" data-tbnid="thumb7" data-ou="https://cdn.example.test/lazy-preview.jpg" data-ru="https://publisher.example.test/page"><img data-src="https://example.test/thumb.jpg"></div>` +
		`<img src="https://cdn.example.test/logo.jpg"></body></html>`
}

func TestGoogleImagesStaticDiscovery(t *testing.T) {
	calls := 0
	getter := googleImagesFixtureGetter(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("credentials on public search: %v", r.Header)
		}
		return googleImagesFixtureResponse(r, http.StatusOK, googleImagesStaticFixture()), nil
	})
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(googleImagesTestURL(t, googleImagesTestPage), []*http.Cookie{{Name: "synthetic", Value: "example", Path: "/"}})
	getter.client.Jar = jar
	got, err := discoverGoogleImages(context.Background(), getter, googleImagesTestURL(t, googleImagesTestPage))
	if err == nil || !strings.Contains(err.Error(), "分页尚未验证") || calls != 1 || len(got.Items) != 1 || got.Items[0].URL != "https://cdn.example.test/one.jpg" {
		t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
	}
	if got.Items[0].Headers["Referer"] != "https://www.google.com/" || !got.Items[0].PublicOnly {
		t.Fatalf("unsafe result: %+v", got.Items[0])
	}
}

func TestGoogleImagesStaticStopsOnChallengeOrStatus(t *testing.T) {
	for _, tc := range []struct {
		name, body, message string
		status              int
	}{
		{"enablejs", `<noscript><a href="/httpservice/retry/enablejs">continue</a></noscript><img src="https://example.test/logo.png">` + googleImagesStaticFixture(), "JavaScript", http.StatusOK},
		{"captcha", googleImagesStaticFixture() + `<input name="captcha">`, "验证", http.StatusOK},
		{"hidden-captcha", googleImagesStaticFixture() + `<input name="captcha" type="hidden">`, "验证", http.StatusOK},
		{"captcha-token", googleImagesStaticFixture() + `<textarea name="g-recaptcha-response" style="display:none"></textarea>`, "验证", http.StatusOK},
		{"captcha-widget", googleImagesStaticFixture() + `<div class="g-recaptcha"></div>`, "验证", http.StatusOK},
		{"wrong-form-query", strings.Replace(googleImagesStaticFixture(), `value="moon"`, `value="mars"`, 1), "不一致", http.StatusOK},
		{"empty", `<html><body><img src="https://example.test/logo.png"></body></html>`, "尚未显示", http.StatusOK},
		{"forbidden", googleImagesStaticFixture(), "拒绝访问", http.StatusForbidden},
		{"rate-limit", googleImagesStaticFixture(), "限流", http.StatusTooManyRequests},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			getter := googleImagesFixtureGetter(func(r *http.Request) (*http.Response, error) {
				calls++
				return googleImagesFixtureResponse(r, tc.status, tc.body), nil
			})
			got, err := discoverGoogleImages(context.Background(), getter, googleImagesTestURL(t, googleImagesTestPage))
			if err == nil || !strings.Contains(err.Error(), tc.message) || len(got.Items) != 0 || calls != 1 {
				t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
			}
		})
	}
}

func TestGoogleImagesStaticPageLimit(t *testing.T) {
	body := "<html><body>" + strings.Repeat("x", maxPageBytes) + "</body></html>"
	getter := googleImagesFixtureGetter(func(r *http.Request) (*http.Response, error) {
		return googleImagesFixtureResponse(r, http.StatusOK, body), nil
	})
	got, err := discoverGoogleImages(context.Background(), getter, googleImagesTestURL(t, googleImagesTestPage))
	if err == nil || !strings.Contains(err.Error(), "8 MiB") || len(got.Items) != 0 {
		t.Fatalf("oversize items=%d err=%v", len(got.Items), err)
	}
}

func TestGoogleImagesStaticRedirectBoundary(t *testing.T) {
	start := googleImagesTestPage + "&token=synthetic"
	for _, tc := range []struct {
		name, target string
		accept       bool
	}{
		{"official-alias", "https://google.com/search?q=moon&udm=2&tbs=isz%3Al", true},
		{"other-site", "https://evil.test/search?q=moon&udm=2&tbs=isz%3Al", false},
		{"other-domain", "https://google.co.jp/search?q=moon&udm=2&tbs=isz%3Al", false},
		{"other-query", "https://google.com/search?q=mars&udm=2&tbs=isz%3Al", false},
		{"other-filter", "https://google.com/search?q=moon&udm=2&tbs=isz%3Am", false},
		{"challenge", "https://www.google.com/sorry/index", false},
		{"login", "https://accounts.google.com/ServiceLogin?continue=https%3A%2F%2Fwww.google.com%2F", false},
		{"downgrade", "http://www.google.com/search?q=moon&tbm=isch&tbs=isz%3Al", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			getter := googleImagesFixtureGetter(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					resp := googleImagesFixtureResponse(r, http.StatusFound, "")
					resp.Header.Set("Location", tc.target)
					return resp, nil
				}
				if !tc.accept {
					t.Errorf("issued request to rejected redirect %s", r.URL)
				}
				if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.google.com/" {
					t.Errorf("unsafe redirected headers: %v", r.Header)
				}
				return googleImagesFixtureResponse(r, http.StatusOK, googleImagesStaticFixture()), nil
			})
			got, err := discoverGoogleImages(context.Background(), getter, googleImagesTestURL(t, start))
			if tc.accept && (err == nil || !strings.Contains(err.Error(), "分页尚未验证") || len(got.Items) != 1 || calls != 2) {
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
	if _, err := discoverGoogleImages(context.Background(), unsafeGetter, googleImagesTestURL(t, start)); err == nil || called {
		t.Fatal("uncontrolled getter accepted")
	}
}

func TestGoogleImagesPublicJSResponse(t *testing.T) {
	file := os.Getenv("TULAI_GOOGLE_IMAGES_HTML_SAMPLE")
	if file == "" {
		t.Skip("set TULAI_GOOGLE_IMAGES_HTML_SAMPLE to a public Google response")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	getter := googleImagesFixtureGetter(func(r *http.Request) (*http.Response, error) {
		return googleImagesFixtureResponse(r, http.StatusOK, string(body)), nil
	})
	got, err := discoverGoogleImages(context.Background(), getter, googleImagesTestURL(t, "https://www.google.com/search?q=NASA+moon&tbm=isch"))
	if err == nil || !strings.Contains(err.Error(), "JavaScript") || len(got.Items) != 0 {
		t.Fatalf("public JS response: items=%+v err=%v", got.Items, err)
	}
}

func TestGoogleImagesPublicLive(t *testing.T) {
	if os.Getenv("TULAI_GOOGLE_IMAGES_LIVE") != "1" {
		t.Skip("set TULAI_GOOGLE_IMAGES_LIVE=1 to request a public Google search")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got, err := discoverGoogleImages(ctx, NewHTTPGetter(""), googleImagesTestURL(t, "https://www.google.com/search?q=NASA+moon&tbm=isch"))
	if len(got.Items) == 0 {
		if err == nil {
			t.Fatal("empty public response without explanation")
		}
		t.Skipf("public search not available for extraction: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "分页尚未验证") {
		t.Fatalf("public items=%d err=%v", len(got.Items), err)
	}
	t.Logf("public historic-format results: %d", len(got.Items))
}

func TestGoogleImagesRenderedCapture(t *testing.T) {
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
	page := googleImagesTestURL(t, googleImagesTestPage)
	session := &RenderedSession{process: process, tab: camoufox.PageContext(life, process, process.Page), closeTab: closeTab,
		closeAllocator: func() { process.Close() }, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: page}
	defer session.Close()
	thumbnail := `<img width="10" height="10" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==">`
	body := `<html><body><div class="islrtb isv-r" style="width:100px;height:100px" data-docid="doc1" data-tbnid="thumb1" data-ou="https://cdn.example.test/one.jpg" data-ru="https://publisher.example.test/page">` + thumbnail + `</div>` +
		`<div class="islrtb isv-r" style="display:none;width:100px;height:100px" data-docid="doc2" data-tbnid="thumb2" data-ou="https://cdn.example.test/hidden.jpg" data-ru="https://publisher.example.test/page">` + thumbnail + `</div>` +
		`<div class="islrtb isv-r" style="width:100px;height:100px" data-docid="doc3" data-tbnid="thumb3" data-ou="https://cdn.example.test/unloaded.jpg" data-ru="https://publisher.example.test/page"><img width="10" height="10" style="opacity:0" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw=="></div>` +
		`<img src="https://cdn.example.test/unrelated.jpg"></body></html>`
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == googleImagesTestPage {
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
	if err := session.run(ctx, fetch.Enable(), camoufox.Navigate(googleImagesTestPage)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".google.com").WithURL("https://www.google.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	calls := 0
	session.getter.client.Transport = googleImagesRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "cdn.example.test" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.google.com/" {
			t.Errorf("unsafe image request: %v", r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "分页尚未验证") || len(got.Items) != 1 || got.Items[0].URL != "https://cdn.example.test/one.jpg" || session.PageURL() != googleImagesTestPage {
		t.Fatalf("capture=%+v page=%s err=%v", got.Items, session.PageURL(), err)
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
		t.Fatalf("image requests=%d", calls)
	}
	session.start = googleImagesTestURL(t, "https://www.google.com/search?q=mars&tbm=isch&tbs=isz%3Al")
	if result, err := session.Capture(ctx); err == nil || len(result.Items) != 0 {
		t.Fatalf("different query accepted: %+v, %v", result.Items, err)
	}
	session.start = page
	if err := session.run(ctx, chromedp.Evaluate(`document.body.insertAdjacentHTML('beforeend', '<input name="captcha" style="width:100px;height:20px">')`, nil)); err != nil {
		t.Fatal(err)
	}
	if result, err := session.Capture(ctx); err == nil || !strings.Contains(err.Error(), "验证") || len(result.Items) != 0 {
		t.Fatalf("challenge accepted: %+v, %v", result.Items, err)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept: %v", err)
	default:
	}
}
