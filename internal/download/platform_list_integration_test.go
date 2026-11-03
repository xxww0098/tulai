package download

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/xxww0098/tulai/internal/camoufox"
)

func TestRenderedPageRedirectGuard(t *testing.T) {
	page, _ := url.Parse("https://source.example/")
	getter := NewHTTPGetter("")
	getter.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	calls := 0
	getter.client.Transport = ebayRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": {"https://other.example/private"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})
	s := &RenderedSession{tab: context.Background(), start: page, pageURL: page, getter: getter}
	ctx := context.WithValue(context.Background(), renderedRedirectGuardKey{}, func(u *url.URL) bool {
		return u.Scheme == "https" && u.Host == "www.ebay.com" && u.Path == "/sch/i.html" && u.RawQuery == "_nkw=camera"
	})
	if response, err := s.Get(ctx, "https://other.example/private", nil); err == nil || response != nil || calls != 0 {
		t.Fatalf("guard accepted initial request: response=%v err=%v calls=%d", response, err, calls)
	}
	if response, err := s.Get(ctx, "https://www.ebay.com/sch/i.html?_nkw=camera", nil); err == nil || response != nil || calls != 1 {
		t.Fatalf("guard accepted redirect: response=%v err=%v calls=%d", response, err, calls)
	}
}

func TestMarketplaceSearchDiscoverRoutes(t *testing.T) {
	for _, tc := range []struct{ name, raw, body, id string }{
		{"amazon", "https://www.amazon.com/s?k=coffee", amazonSearchFixture, "B012345678"},
		{"ebay", ebaySearchTestURL, ebaySearchFixture("", ebaySearchCard("123456789012", "Camera", "https://i.ebayimg.com/images/g/A/s-l500.jpg")), "123456789012"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getter := NewHTTPGetter("")
			getter.client.Transport = ebayRoundTrip(func(r *http.Request) (*http.Response, error) {
				return ebaySearchHTTPResponse(r, http.StatusOK, tc.body), nil
			})
			found, err := Discover(context.Background(), getter, tc.raw, 1)
			if !found.IsPage || len(found.Items) != 1 || found.Items[0].Metadata["album_id"] != tc.id {
				t.Fatalf("routed result=%+v err=%v", found, err)
			}
		})
	}
}

func TestMarketplaceSearchTemuRequiresBrowser(t *testing.T) {
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("TEMU search should request the browser rather than fetch an empty shell")
		return nil, nil
	})
	found, err := Discover(context.Background(), getter, temuSearchTestPage, 1)
	if !found.IsPage || err == nil || !strings.Contains(err.Error(), "下载浏览器") {
		t.Fatalf("TEMU search route: result=%+v err=%v", found, err)
	}
}

func TestAmazonSearchCompleteListSucceeds(t *testing.T) {
	const body = `<html><div class="s-main-slot"><div data-component-type="s-search-result" data-asin="B012345678"><a href="/dp/B012345678"><img class="s-image" src="https://m.media-amazon.com/images/I/a._AC_UL320_.jpg" alt="Cup"></a></div></div><span class="s-pagination-next s-pagination-disabled">next</span></html>`
	getter := NewHTTPGetter("")
	getter.client.Transport = ebayRoundTrip(func(r *http.Request) (*http.Response, error) {
		return ebaySearchHTTPResponse(r, http.StatusOK, body), nil
	})
	found, err := Discover(context.Background(), getter, "https://www.amazon.com/s?k=coffee", 1)
	if err != nil || len(found.Items) != 1 {
		t.Fatalf("complete search: result=%+v err=%v", found, err)
	}
}

func TestMarketplaceSearchRenderedRoutes(t *testing.T) {
	dataDir := os.Getenv("TULAI_CAMOUFOX_TEST_DATA")
	if os.Getenv("TULAI_RENDERED_E2E") != "1" || dataDir == "" {
		t.Skip("set TULAI_RENDERED_E2E=1 and TULAI_CAMOUFOX_TEST_DATA to a disposable runtime")
	}
	amazonFirst := strings.Replace(amazonSearchFixture, `<span class="s-pagination-next s-pagination-disabled">next</span>`, `<a class="s-pagination-next" href="/s?k=coffee&amp;page=2">next</a>`, 1)
	amazonNext := strings.ReplaceAll(amazonSearchFixture, "B012345678", "B012345677")
	for _, tc := range []struct{ name, raw, changed, body, nextBody, id, nextID, pageKey string }{
		{"amazon", "https://www.amazon.com/s?k=coffee", "https://www.amazon.com/s?k=other", amazonFirst, amazonNext, "B012345678", "B012345677", "page"},
		{"ebay", ebaySearchTestURL, "https://www.ebay.com/sch/i.html?_nkw=other&_sacat=625", ebaySearchFixture(`<a class="pagination__next" href="/sch/i.html?_nkw=camera&amp;_sacat=625&amp;_pgn=2">next</a>`, ebaySearchCard("123456789012", "Camera", "https://i.ebayimg.com/images/g/A/s-l500.jpg")), ebaySearchFixture("", ebaySearchCard("123456789013", "Lens", "https://i.ebayimg.com/images/g/B/s-l500.jpg")), "123456789012", "123456789013", "_pgn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			process, err := camoufox.Start(ctx, dataDir, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
			if err != nil {
				t.Fatal(err)
			}
			life, closeTab := context.WithCancel(process.Context())
			page, _ := url.Parse(tc.raw)
			session := &RenderedSession{process: process, tab: camoufox.PageContext(life, process, process.Page), closeTab: closeTab,
				closeAllocator: func() { process.Close() }, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: page}
			defer session.Close()
			session.getter.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			calls := 0
			session.getter.client.Transport = ebayRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Query().Get(tc.pageKey) != "2" || r.URL.Host != page.Host {
					t.Errorf("unexpected pagination request: %v", r.URL)
				}
				return ebaySearchHTTPResponse(r, http.StatusOK, tc.nextBody), nil
			})
			interceptErrors := make(chan error, 8)
			camoufox.ListenTarget(session.tab, func(event any) {
				paused, ok := event.(*fetch.EventRequestPaused)
				if !ok {
					return
				}
				go func() {
					var action camoufox.Action
					if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == tc.raw {
						action = fetch.FulfillRequest(paused.RequestID, 200).
							WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
							WithBody(base64.StdEncoding.EncodeToString([]byte(tc.body)))
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
			if err := session.run(ctx, fetch.Enable(), camoufox.Navigate(tc.raw)); err != nil {
				t.Fatal(err)
			}
			found, err := session.Capture(ctx)
			if !found.IsPage || len(found.Items) != 2 || found.Items[0].Metadata["album_id"] != tc.id || found.Items[1].Metadata["album_id"] != tc.nextID || calls != 1 {
				t.Fatalf("captured result=%+v calls=%d err=%v", found, calls, err)
			}
			session.start, _ = url.Parse(tc.changed)
			if found, err := session.Capture(ctx); err == nil || len(found.Items) != 0 {
				t.Fatalf("changed search accepted: result=%+v err=%v", found, err)
			}
			select {
			case err := <-interceptErrors:
				t.Fatalf("intercept: %v", err)
			default:
			}
		})
	}
}
