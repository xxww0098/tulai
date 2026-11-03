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
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"golang.org/x/net/html"
)

const gomeTestPage = `https://item.gome.com.cn/9140070138-1130581852.html`
const gomeTestHTML = `<html><head><title>商品</title><script>var prdInfo={prdId:"9140070138",sku:"1130581852",htmlHref:"//desc.gome.com.cn/html/prodhtml/goodsDesc/descHtml/201911/cat10000070/1130581852.html"};</script></head><body>
<div id="gm-main-info"><div class="pic-small"><img rpic="//gfs17.gomein.net.cn/main_800_pc.jpg" alt="主图"></div><div id="salesProperty"><div class="prdcol prd-img-item"><img gome-src="//gfs17.gomein.net.cn/blue_60.jpg" alt="蓝"></div></div></div>
<img rpic="//gfs17.gomein.net.cn/recommend.jpg"></body></html>`
const gomeTestDetail = `<html><body><img gome-src="//gfs3.gomein.net.cn/detail.jpg"></body></html>`

type gomeRoundTrip func(*http.Request) (*http.Response, error)

func (f gomeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func gomeTestGetter(t *testing.T, page, detail string) Getter {
	t.Helper()
	return GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		body := page
		if strings.HasPrefix(raw, "https://desc.gome.com.cn/") {
			body = detail
		} else if raw != gomeTestPage {
			t.Fatalf("unexpected request %s", raw)
		}
		return &Response{StatusCode: 200, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
}

func TestGomeDiscoveryAndCategories(t *testing.T) {
	got, err := Discover(context.Background(), gomeTestGetter(t, gomeTestHTML, gomeTestDetail), gomeTestPage, 1)
	if err != nil || len(got.Items) != 3 {
		t.Fatalf("images=%d err=%v", len(got.Items), err)
	}
	for i, want := range []string{"main", "sku", "detail"} {
		item := got.Items[i]
		if item.Metadata["image_kind"] != want || item.Metadata["gome_sku_id"] != "1130581852" || !item.RequireImage || !item.PublicOnly || item.Headers["Referer"] != "https://item.gome.com.cn/" {
			t.Errorf("item %d: %+v", i, item)
		}
	}
	if got.Items[1].URL != "https://gfs17.gomein.net.cn/blue.jpg" || len(got.Items[1].AlternateURLs) != 1 || got.Items[1].AlternateURLs[0] != "https://gfs17.gomein.net.cn/blue_60.jpg" {
		t.Fatalf("SKU original/fallback: %+v", got.Items[1])
	}
}

func TestGomeIdentityAndPartial(t *testing.T) {
	page, _ := url.Parse(gomeTestPage)
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"wrong identity", strings.Replace(gomeTestHTML, `sku:"1130581852"`, `sku:"1"`, 1), 0},
		{"wrong detail", strings.Replace(gomeTestHTML, "//desc.gome.com.cn/", "//evil.test/", 1), 2},
		{"missing detail", strings.Replace(gomeTestHTML, `htmlHref:`, `missing:`, 1), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			got, err := gomeCollect(context.Background(), gomeTestGetter(t, tc.body, gomeTestDetail), page, doc)
			if err == nil || len(got.Items) != tc.count {
				t.Fatalf("images=%d err=%v", len(got.Items), err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverGome(ctx, gomeTestGetter(t, gomeTestHTML, gomeTestDetail), page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

func TestGomeArchivedPublicProduct(t *testing.T) {
	file := os.Getenv("TULAI_GOME_ARCHIVE")
	if file == "" {
		t.Skip("set TULAI_GOME_ARCHIVE to a saved public product page")
	}
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	doc, err := html.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse(gomeTestPage)
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		return nil, errors.New("description unavailable")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got, err := gomeCollect(ctx, getter, page, doc)
	if err == nil || len(got.Items) != 11 {
		t.Fatalf("archived images=%d err=%v", len(got.Items), err)
	}
	for _, idx := range []int{0, 6} {
		item := got.Items[idx]
		item.Filename = "image"
		if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: t.TempDir()}, item, nil); err != nil {
			t.Fatalf("archived image %d: %v", idx, err)
		}
	}
}

func TestGomeRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated browser integration")
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
	interceptErrors := make(chan error, 1)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			action := camoufox.Action(fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient))
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == gomeTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(gomeTestHTML)))
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
	session.start, _ = url.Parse(gomeTestPage)
	if err := session.run(ctx, camoufox.Navigate(gomeTestPage)); err != nil {
		t.Fatal(err)
	}
	session.getter.client.Transport = gomeRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "desc.gome.com.cn" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected description request %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(gomeTestDetail)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 3 || session.PageURL() != gomeTestPage {
		t.Fatalf("capture images=%d page=%s err=%v", len(got.Items), session.PageURL(), err)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}
