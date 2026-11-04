package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

const temuSearchTestPage = "https://www.temu.com/search_result.html?search_key=lamp&filter=black"

func temuSearchTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTemuSearchRoutesAndIdentity(t *testing.T) {
	for _, tc := range []struct {
		raw          string
		search, home bool
	}{
		{temuSearchTestPage, true, false},
		{"https://temu.com/br/search_result.html?search_key=lamp", true, false},
		{"https://www.temu.com/search_result.html", true, false},
		{"https://www.temu.com/", false, true},
		{"https://temu.com/br/", false, true},
		{"https://www.temu.com/bgn_verification.html", false, false},
		{"https://www.temu.com.evil.test/search_result.html?search_key=lamp", false, false},
		{"https://user@www.temu.com/search_result.html?search_key=lamp", false, false},
		{"https://www.temu.com:8443/search_result.html?search_key=lamp", false, false},
	} {
		u := temuSearchTestURL(t, tc.raw)
		if isTemuSearch(u) != tc.search || isTemuHome(u) != tc.home {
			t.Errorf("route %s search=%v home=%v", tc.raw, isTemuSearch(u), isTemuHome(u))
		}
	}
	start := temuSearchTestURL(t, temuSearchTestPage)
	for _, tc := range []struct {
		raw  string
		same bool
	}{
		{"https://temu.com/search_result.html?filter=black&search_key=lamp&refer_page_id=synthetic", true},
		{"https://temu.com/search_result.html?filter=black&search_key=lamp&is_back=1", true},
		{"https://www.temu.com/search_result.html?filter=black&search_key=desk", false},
		{"https://www.temu.com/search_result.html?filter=red&search_key=lamp", false},
		{"https://www.temu.com/search_result.html?search_key=lamp", false},
		{"https://www.temu.com/search_result.html?filter=black&search_key=lamp&search_key=desk", false},
		{"http://www.temu.com/search_result.html?filter=black&search_key=lamp", false},
	} {
		if got := temuSearchSameSearch(start, temuSearchTestURL(t, tc.raw)); got != tc.same {
			t.Errorf("same search %s=%v", tc.raw, got)
		}
	}
	if _, err := discoverTemuSearch(context.Background(), GetterFunc(nil), start); err == nil || !strings.Contains(err.Error(), "下载浏览器") {
		t.Fatalf("ordinary mode err=%v", err)
	}
}

func TestTemuSearchCollectBoundRows(t *testing.T) {
	page := temuSearchTestURL(t, temuSearchTestPage)
	row := temuSearchRow{ID: temuTestID, Title: "Lamp", Image: "https://img.kwcdn.com/product/lamp.jpg?imageView2/2/w/800", Link: temuTestPage}
	snapshot := temuSearchSnapshot{URL: temuSearchTestPage, Title: "Search results", Rows: []temuSearchRow{
		row, row,
		{ID: "601099513224344", Title: "Wrong", Image: row.Image, Link: temuTestPage},
		{ID: "601099513224345", Title: "Bad host", Image: "https://evil.test/a.jpg", Link: "https://www.temu.com/item-g-601099513224345.html"},
		{ID: "601099513224346", Title: "Wrong link host", Image: row.Image, Link: "https://evil.temu.com/item-g-601099513224346.html"},
	}}
	got, err := temuSearchCollect(context.Background(), page, snapshot)
	if len(got.Items) != 1 || err == nil || !strings.Contains(err.Error(), "无法确认") {
		t.Fatalf("items=%+v err=%v", got.Items, err)
	}
	item := got.Items[0]
	if item.URL != "https://img.kwcdn.com/product/lamp.jpg" || len(item.AlternateURLs) != 1 || item.AlternateURLs[0] != row.Image ||
		!item.RequireImage || !item.PublicOnly || item.Metadata["album_id"] != row.ID || item.Metadata["album_title"] != "Lamp" ||
		item.Headers["Referer"] != "https://www.temu.com/" {
		t.Fatalf("item=%+v", item)
	}
	if got, err := temuSearchCollect(context.Background(), page, temuSearchSnapshot{URL: temuSearchTestPage, Rows: []temuSearchRow{row}}); err != nil || len(got.Items) != 1 {
		t.Fatalf("loaded search items=%+v err=%v", got.Items, err)
	}
	for _, test := range []struct {
		name string
		snap temuSearchSnapshot
	}{
		{"different search", temuSearchSnapshot{URL: "https://www.temu.com/search_result.html?search_key=other&filter=black", Rows: []temuSearchRow{row}}},
		{"challenge", temuSearchSnapshot{URL: temuSearchTestPage, Blocked: true, Rows: []temuSearchRow{row}}},
		{"empty", temuSearchSnapshot{URL: temuSearchTestPage}},
	} {
		if got, err := temuSearchCollect(context.Background(), page, test.snap); err == nil || len(got.Items) != 0 {
			t.Errorf("%s items=%+v err=%v", test.name, got.Items, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := temuSearchCollect(ctx, page, snapshot); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}

func TestTemuSearchLimit(t *testing.T) {
	rows := make([]temuSearchRow, maxTemuSearchCards+1)
	for i := range rows {
		id := fmt.Sprintf("%015d", 601099513224343+i)
		rows[i] = temuSearchRow{ID: id, Title: id, Link: "https://www.temu.com/item-g-" + id + ".html", Image: "https://img.kwcdn.com/product/" + id + ".jpg"}
	}
	got, err := temuSearchCollect(context.Background(), temuSearchTestURL(t, temuSearchTestPage), temuSearchSnapshot{URL: temuSearchTestPage, Rows: rows})
	if len(got.Items) != maxTemuSearchCards || err == nil || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("items=%d err=%v", len(got.Items), err)
	}
}

func TestTemuSearchRenderedCapture(t *testing.T) {
	dir := os.Getenv("TULAI_TEMU_TEST_RUNTIME")
	if dir == "" {
		t.Skip("set TULAI_TEMU_TEST_RUNTIME to an isolated Camoufox runtime copy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	geo := false
	process, err := camoufox.Start(ctx, dir, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true, GeoIP: &geo}, nil)
	if err != nil {
		t.Fatal(err)
	}
	life, closeTab := context.WithCancel(process.Context())
	start := temuSearchTestURL(t, "https://www.temu.com/")
	session := &RenderedSession{process: process, tab: camoufox.PageContext(life, process, process.Page), closeTab: closeTab,
		closeAllocator: func() { process.Close() }, releaseProfile: func() {}, getter: NewHTTPGetter(""), start: start}
	defer session.Close()
	const body = `<html><head><title>Search</title></head><body><div class="js-search-goodsList">
<div class="EKDT7a3v"><div id="goodName-601099513224343" title="Lamp"><a href="https://www.temu.com/item-g-601099513224343.html"><h2>Lamp</h2></a></div><div class="goods-img-external"><img aria-hidden="true" width="20" height="20" src="https://img.kwcdn.com/product/lamp.gif?imageView2/2/w/800"></div></div>
<div class="EKDT7a3v" style="display:none"><div id="goodName-601099513224344" title="Hidden"><a href="https://www.temu.com/item-g-601099513224344.html">Hidden</a></div><img class="goods-img-external" width="20" height="20" src="https://img.kwcdn.com/product/hidden.gif"></div>
<div class="EKDT7a3v"><div id="goodName-601099513224345" title="Mismatch"><a href="https://www.temu.com/item-g-601099513224346.html">Wrong</a></div><img class="goods-img-external" width="20" height="20" src="https://img.kwcdn.com/product/wrong.gif"></div>
<div class="EKDT7a3v"><div id="goodName-601099513224347" title="Unloaded"><a href="https://www.temu.com/item-g-601099513224347.html">Unloaded</a></div><img class="goods-img-external" width="20" height="20" src="https://img.kwcdn.com/product/missing.gif"></div>
<div class="EKDT7a3v"><div data-tooltip="goodName-601099513224348" data-tooltip-title="Modern lamp"><a href="https://www.temu.com/item-g-601099513224348.html">Modern lamp</a></div><img class="goods-img-external" width="20" height="20" src="https://img.kwcdn.com/product/modern.gif"></div>
<div class="EKDT7a3v"><div data-tooltip="goodName-601099513224349" data-tooltip-title="Lazy lamp"><a href="https://www.temu.com/item-g-601099513224349.html">Lazy lamp</a></div><img class="goods-img-external" width="20" height="20" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==" data-src="https://img.kwcdn.com/product/lazy.gif"></div>
</div><img src="https://img.kwcdn.com/product/not-result.gif"></body></html>`
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action chromedp.Action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == "https://www.temu.com/" {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(`<html><body>Temu home</body></html>`)))
			} else if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == "https://www.temu.com/bgn_verification.html" {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(`<html><body>Security Verification</body></html>`)))
			} else if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == temuSearchTestPage {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			} else if paused.ResourceType == network.ResourceTypeImage && strings.HasPrefix(paused.Request.URL, "https://img.kwcdn.com/product/") && !strings.Contains(paused.Request.URL, "missing.gif") {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "image/gif"}}).WithBody("R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==")
			}
			if err := camoufox.Run(session.tab, action); err != nil {
				select {
				case interceptErrors <- err:
				default:
				}
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable(), camoufox.Navigate("https://www.temu.com/"), camoufox.Navigate(temuSearchTestPage)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		var ready bool
		if err := session.run(ctx, chromedp.Evaluate(`(() => { const n = document.querySelector('.goods-img-external img'); return !!n && n.complete && n.naturalWidth > 0 })()`, &ready)); err != nil {
			t.Fatal(err)
		}
		if ready {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	got, err := session.Capture(ctx)
	if len(got.Items) != 3 || err == nil || !strings.Contains(err.Error(), "无法确认") || got.Items[0].Metadata["album_id"] != temuTestID || got.Items[1].Metadata["album_title"] != "Modern lamp" || got.Items[2].Metadata["album_title"] != "Lazy lamp" {
		t.Fatalf("capture=%+v err=%v", got.Items, err)
	}
	prior := temuSearchSnapshot{URL: temuSearchTestPage, Rows: []temuSearchRow{{
		ID: "601099513224350", Title: "Removed card", Link: "https://www.temu.com/item-g-601099513224350.html", Image: "https://img.kwcdn.com/product/removed.gif",
	}}}
	cache, err := json.Marshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.Evaluate(fmt.Sprintf(`(() => { const h = document.createElement('div'); h.dataset.tulaiDownloadHelper = ''; h.dataset.tulaiTemuSnapshot = %q; document.documentElement.append(h); })()`, string(cache)), nil)); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if len(got.Items) != 4 || err == nil || got.Items[0].Metadata["album_title"] != "Removed card" {
		t.Fatalf("removed card not retained: items=%+v err=%v", got.Items, err)
	}
	prior.URL = "https://www.temu.com/search_result.html?search_key=other&filter=black"
	cache, _ = json.Marshal(prior)
	if err := session.run(ctx, chromedp.Evaluate(fmt.Sprintf(`document.querySelector('[data-tulai-download-helper]').dataset.tulaiTemuSnapshot = %q`, string(cache)), nil)); err != nil {
		t.Fatal(err)
	}
	if got, err := session.Capture(ctx); err == nil || len(got.Items) != 0 || !strings.Contains(err.Error(), "条件已改变") {
		t.Fatalf("different cached search=%+v err=%v", got.Items, err)
	}
	if err := session.run(ctx, chromedp.Evaluate(`document.querySelector('[data-tulai-download-helper]').remove()`, nil)); err != nil {
		t.Fatal(err)
	}
	wrongStart := temuSearchTestURL(t, "https://www.temu.com/search_result.html?search_key=other&filter=black")
	session.start = wrongStart
	if got, err := session.Capture(ctx); err == nil || len(got.Items) != 0 {
		t.Fatalf("different search=%+v err=%v", got.Items, err)
	}
	session.start = start
	if err := session.run(ctx, chromedp.Evaluate(`document.body.insertAdjacentHTML('beforeend', '<div role="dialog" style="width:100px;height:40px">Security Verification</div>')`, nil)); err != nil {
		t.Fatal(err)
	}
	if got, err := session.Capture(ctx); err == nil || !strings.Contains(err.Error(), "验证") || len(got.Items) != 0 {
		t.Fatalf("challenge=%+v err=%v", got.Items, err)
	}
	if err := session.run(ctx, camoufox.Navigate("https://www.temu.com/bgn_verification.html")); err != nil {
		t.Fatal(err)
	}
	if got, err := session.Capture(ctx); err == nil || !strings.Contains(err.Error(), "验证") || len(got.Items) != 0 {
		t.Fatalf("verification page=%+v err=%v", got.Items, err)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept=%v", err)
	default:
	}
}
