package download

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/xxww0098/tulai/internal/camoufox"
)

const xiaomiYoupinTestPage = "https://www.xiaomiyoupin.com/detail?gid=185130"
const xiaomiYoupinTestAPI = "https://www.xiaomiyoupin.com/api/gateway/detail"
const xiaomiYoupinMain = "https://img.youpin.mi-img.com/shop-fe/main.jpeg"
const xiaomiYoupinSKU = "https://img.youpin.mi-img.com/shop-fe/sku.png"
const xiaomiYoupinDesc = "https://img.youpin.mi-img.com/shopmain/detail.jpeg?w=1080&h=1200"

func xiaomiYoupinFixture(gid, main, sku, desc string) string {
	return fmt.Sprintf(`{"code":0,"data":{"gid":%s,"goods":{"shareInfo":{"poster":{"title":"商品"}},"carouselMapList":[{"type":1,"url":"https://video.example/v.mp4"},{"type":0,"url":%q}],"productInfo":{"7":{"gid":%s,"pid":7,"name":"白色","img":%q}},"goodsInfo":{"introExt":[{"title":"详情","imagesArray":[%q]}]}}}}`, gid, main, gid, sku, desc)
}
func xiaomiYoupinTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestXiaomiYoupinReferenceAndStatic(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{xiaomiYoupinTestPage, "185130"}, {"https://m.xiaomiyoupin.com/detail?gid=185130", "185130"}, {"http://www.xiaomiyoupin.com/detail?gid=1", "1"},
		{"https://www.xiaomiyoupin.com/detail?gid=0", ""}, {"https://www.xiaomiyoupin.com/detail?gid=1&gid=2", ""},
		{"https://www.xiaomiyoupin.com/detail?gid=abc", ""}, {"https://www.xiaomiyoupin.com/detail/other?gid=1", ""},
		{"https://www.xiaomiyoupin.com.evil.test/detail?gid=1", ""}, {"https://user@www.xiaomiyoupin.com/detail?gid=1", ""},
		{"https://www.xiaomiyoupin.com:8443/detail?gid=1", ""},
	} {
		u := xiaomiYoupinTestURL(t, tc.raw)
		if got := xiaomiYoupinID(u); got != tc.id {
			t.Errorf("%s id=%q", tc.raw, got)
		}
	}
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("static mode sent a request")
		return nil, nil
	})
	got, err := discoverXiaomiYoupin(context.Background(), getter, xiaomiYoupinTestURL(t, xiaomiYoupinTestPage))
	if err == nil || !strings.Contains(err.Error(), "下载浏览器") || len(got.Items) != 0 || !got.IsPage {
		t.Fatalf("static=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverXiaomiYoupin(ctx, getter, xiaomiYoupinTestURL(t, xiaomiYoupinTestPage)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	if err := xiaomiYoupinPageState(xiaomiYoupinTestPage, "https://www.xiaomiyoupin.com/detail?gid=185131", "商品"); err == nil {
		t.Fatal("changed product accepted")
	}
	if err := xiaomiYoupinPageState(xiaomiYoupinTestPage, xiaomiYoupinTestPage, "安全验证"); err == nil {
		t.Fatal("verification page accepted")
	}
}
func TestXiaomiYoupinImageFields(t *testing.T) {
	page := xiaomiYoupinTestURL(t, xiaomiYoupinTestPage+"&token=synthetic-secret")
	body := xiaomiYoupinFixture("185130", xiaomiYoupinMain, xiaomiYoupinSKU, xiaomiYoupinDesc)
	got, err := xiaomiYoupinDetail(context.Background(), page, []byte(body))
	if err != nil || len(got.Items) != 3 || got.Title != "商品" {
		t.Fatalf("items=%+v title=%q err=%v", got.Items, got.Title, err)
	}
	for i, kind := range []string{"main", "sku", "detail"} {
		item := got.Items[i]
		if !item.PublicOnly || !item.RequireImage || item.Metadata["xiaomiyoupin_gid"] != "185130" || item.Metadata["xiaomiyoupin_kind"] != kind || strings.Contains(fmt.Sprint(item.Headers), "secret") {
			t.Fatalf("item %d=%+v", i, item)
		}
	}
	if got.Items[1].Metadata["xiaomiyoupin_label"] != "白色" {
		t.Fatal("SKU label missing")
	}
	mismatch := strings.Replace(body, `"gid":185130`, `"gid":185131`, 1)
	if got, err := xiaomiYoupinDetail(context.Background(), page, []byte(mismatch)); err == nil || len(got.Items) != 0 {
		t.Fatalf("wrong gid=%+v err=%v", got.Items, err)
	}
	invalid := xiaomiYoupinFixture("185130", xiaomiYoupinMain, "https://evil.test/sku.png", xiaomiYoupinDesc)
	if got, err := xiaomiYoupinDetail(context.Background(), page, []byte(invalid)); err == nil || len(got.Items) != 2 {
		t.Fatalf("partial=%+v err=%v", got.Items, err)
	}
	badSKU := strings.Replace(body, `"pid":7`, `"pid":8`, 1)
	if got, err := xiaomiYoupinDetail(context.Background(), page, []byte(badSKU)); err == nil || len(got.Items) != 2 {
		t.Fatalf("SKU mismatch=%+v err=%v", got.Items, err)
	}
	badSection := strings.Replace(body, `"productInfo":{"7":`, `"productInfo":{"broken":false,"7":`, 1)
	badSection = strings.Replace(badSection, `"imagesArray":[`, `"imagesArray":[42,`, 1)
	if got, err := xiaomiYoupinDetail(context.Background(), page, []byte(badSection)); err == nil || len(got.Items) != 3 {
		t.Fatalf("malformed entries should retain valid images: %+v err=%v", got.Items, err)
	}
	for _, body := range []string{`{"code":1,"data":{}}`, `{"data":{"gid":185130}}`, `{`, strings.Repeat("x", xiaomiYoupinMaxBody+1)} {
		if got, err := xiaomiYoupinDetail(context.Background(), page, []byte(body)); err == nil || len(got.Items) != 0 {
			t.Fatalf("bad response length=%d items=%d err=%v", len(body), len(got.Items), err)
		}
	}
}
func TestXiaomiYoupinAssetAndAPIRestrictions(t *testing.T) {
	page := xiaomiYoupinTestURL(t, xiaomiYoupinTestPage)
	for _, raw := range []string{"http://img.youpin.mi-img.com/shop-fe/a.jpg", "https://evil.test/shop-fe/a.jpg", "https://img.youpin.mi-img.com.evil.test/shop-fe/a.jpg", "https://user@img.youpin.mi-img.com/shop-fe/a.jpg", "https://img.youpin.mi-img.com:8443/shop-fe/a.jpg", "https://img.youpin.mi-img.com/shop-fe/../a.jpg", "https://img.youpin.mi-img.com/shop-fe/a.jpg?token=secret", "https://img.youpin.mi-img.com/shopmain/a.jpg?w=12&w=13", "https://img.youpin.mi-img.com/other/a.jpg", "https://img.youpin.mi-img.com/shopmain/a.svg"} {
		if got, ok := xiaomiYoupinAsset(raw); ok {
			t.Errorf("unsafe asset %q", got)
		}
	}
	for _, tc := range []struct {
		raw string
		ok  bool
	}{{xiaomiYoupinTestAPI, true}, {"https://www.xiaomiyoupin.com/api/gateway/detail?token=secret", false}, {"https://evil.test/api/gateway/detail", false}, {"http://www.xiaomiyoupin.com/api/gateway/detail", false}, {"https://user@www.xiaomiyoupin.com/api/gateway/detail", false}} {
		if got := xiaomiYoupinAPI(tc.raw, page); got != tc.ok {
			t.Errorf("API %s=%v", tc.raw, got)
		}
	}
}
func TestXiaomiYoupinNetworkObservation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	page := xiaomiYoupinTestURL(t, xiaomiYoupinTestPage)
	o := newXiaomiYoupinObserver(page, ctx.Done())
	request := func(id network.RequestID, method, raw string) {
		// Camoufox's request event omits Type; the response supplies it.
		o.event(&network.EventRequestWillBeSent{RequestID: id, Request: &network.Request{URL: raw, Method: method}})
	}
	response := func(id network.RequestID, raw string, status int64, sw bool) {
		o.event(&network.EventResponseReceived{RequestID: id, Type: network.ResourceTypeXHR, Response: &network.Response{URL: raw, Status: status, FromServiceWorker: sw}})
	}
	request("wrong-method", "GET", xiaomiYoupinTestAPI)
	response("wrong-method", xiaomiYoupinTestAPI, 200, false)
	o.event(&network.EventLoadingFinished{RequestID: "wrong-method"})
	request("wrong-host", "POST", "https://evil.test/api/gateway/detail")
	response("wrong-host", xiaomiYoupinTestAPI, 200, false)
	o.event(&network.EventLoadingFinished{RequestID: "wrong-host"})
	select {
	case e := <-o.events:
		t.Fatalf("untrusted request accepted %+v", e)
	default:
	}
	request("good", "POST", xiaomiYoupinTestAPI)
	response("good", xiaomiYoupinTestAPI, 200, false)
	o.event(&network.EventLoadingFinished{RequestID: "good"})
	select {
	case e := <-o.events:
		if e.failed || e.requestID != "good" {
			t.Fatalf("good=%+v", e)
		}
	default:
		t.Fatal("missing response")
	}
	request("service-worker", "POST", xiaomiYoupinTestAPI)
	response("service-worker", xiaomiYoupinTestAPI, 200, true)
	select {
	case e := <-o.events:
		if !e.failed {
			t.Fatalf("service worker accepted %+v", e)
		}
	default:
		t.Fatal("missing failed response")
	}
	request("redirect", "POST", xiaomiYoupinTestAPI)
	o.event(&network.EventRequestWillBeSent{RequestID: "redirect", RedirectResponse: &network.Response{URL: xiaomiYoupinTestAPI, Status: 302}})
	select {
	case e := <-o.events:
		if !e.failed {
			t.Fatalf("redirect accepted %+v", e)
		}
	default:
		t.Fatal("redirect not rejected")
	}
}
func TestXiaomiYoupinSavedPublicResponse(t *testing.T) {
	file := os.Getenv("TULAI_XIAOMI_YOUPIN_SAMPLE")
	if file == "" {
		t.Skip("set TULAI_XIAOMI_YOUPIN_SAMPLE to an anonymous public response")
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	got, err := xiaomiYoupinDetail(context.Background(), xiaomiYoupinTestURL(t, xiaomiYoupinTestPage), body)
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.Metadata["xiaomiyoupin_kind"]]++
	}
	if err != nil || len(got.Items) != 70 || counts["main"] != 6 || counts["sku"] != 4 || counts["detail"] != 60 {
		t.Fatalf("items=%d counts=%v err=%v", len(got.Items), counts, err)
	}
	t.Logf("anonymous response: %d main, %d SKU, %d detail", counts["main"], counts["sku"], counts["detail"])
}

// Optional real browser check uses a copied runtime and an empty temporary
// profile. The site, not the test, initiates the gateway request.
func TestXiaomiYoupinPublicCamoufoxCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_XIAOMI_YOUPIN_CAMOUFOX_ROOT")
	if runtime == "" {
		t.Skip("set TULAI_XIAOMI_YOUPIN_CAMOUFOX_ROOT to a copied temporary runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	process, err := camoufox.Start(ctx, runtime, camoufox.Options{Profile: filepath.Join(t.TempDir(), "profile"), Headless: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var eventMu sync.Mutex
	observed := map[string]int{}
	stopObserve := process.Subscribe(func(e camoufox.Event) {
		if e.Event != "Network.requestWillBeSent" && e.Event != "Network.responseReceived" {
			return
		}
		var data struct {
			Type     string                       `json:"type"`
			Request  struct{ URL, Method string } `json:"request"`
			Response struct {
				URL    string
				Status int
			} `json:"response"`
		}
		if json.Unmarshal(e.Data, &data) != nil {
			return
		}
		raw, verb, status := data.Request.URL, data.Request.Method, 0
		if e.Event == "Network.responseReceived" {
			raw, status = data.Response.URL, data.Response.Status
		}
		u, parseErr := url.Parse(raw)
		if parseErr != nil || u.Hostname() != "www.xiaomiyoupin.com" {
			return
		}
		name := "other"
		if u.Path == "/api/gateway/detail" {
			name = "gateway"
		}
		if u.Path == "/detail" {
			name = "document"
		}
		key := fmt.Sprintf("%s %s %s %d %s", e.Event, name, verb, status, data.Type)
		eventMu.Lock()
		observed[key]++
		eventMu.Unlock()
	})
	defer stopObserve()
	life, closeTab := context.WithCancel(process.Context())
	tab := camoufox.PageContext(life, process, process.Page)
	getter := NewHTTPGetter("")
	getter.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	getter.client.Transport.(*http.Transport).DialContext = safeDownloadDial
	page := xiaomiYoupinTestURL(t, xiaomiYoupinTestPage)
	session := &RenderedSession{process: process, tab: tab, closeTab: closeTab, closeAllocator: process.Close, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	if err := session.run(ctx, camoufox.Navigate(xiaomiYoupinTestPage)); err != nil {
		t.Fatal(err)
	}
	var title string
	for i := 0; i < 30; i++ {
		if err := session.run(ctx, chromedp.Evaluate(`document.title`, &title)); err != nil {
			t.Fatal(err)
		}
		if title != "" && title != "小米有品" {
			break
		}
		if err := Wait(ctx, 500*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	got, err := session.Capture(ctx)
	counts := map[string]int{}
	for _, item := range got.Items {
		counts[item.Metadata["xiaomiyoupin_kind"]]++
	}
	if err != nil || len(got.Items) != 70 || counts["main"] != 6 || counts["sku"] != 4 || counts["detail"] != 60 {
		eventMu.Lock()
		diagnostics := fmt.Sprint(observed)
		eventMu.Unlock()
		t.Fatalf("public browser capture=%d counts=%v title=%q events=%s err=%v", len(got.Items), counts, title, diagnostics, err)
	}
	t.Logf("anonymous browser capture: %d main, %d SKU, %d detail", counts["main"], counts["sku"], counts["detail"])
	if os.Getenv("TULAI_XIAOMI_YOUPIN_DOWNLOAD") == "1" {
		plain := NewHTTPGetter("")
		dir := t.TempDir()
		for _, kind := range []string{"main", "sku", "detail"} {
			for _, item := range got.Items {
				if item.Metadata["xiaomiyoupin_kind"] != kind {
					continue
				}
				item.Filename = "xiaomi-" + kind
				if e := New(Options{Getter: plain}).Download(ctx, Job{Directory: dir}, item, nil); e != nil {
					t.Fatalf("download %s: %v", kind, e)
				}
				break
			}
		}
		entries, e := os.ReadDir(dir)
		if e != nil || len(entries) != 3 {
			t.Fatalf("download count=%d err=%v", len(entries), e)
		}
		t.Logf("downloaded one image from each category")
	}
	if os.Getenv("TULAI_XIAOMI_YOUPIN_SESSION_DOWNLOAD") == "1" {
		dir := t.TempDir()
		for _, kind := range []string{"main", "sku", "detail"} {
			for _, item := range got.Items {
				if item.Metadata["xiaomiyoupin_kind"] != kind {
					continue
				}
				item.Filename = "xiaomi-session-" + kind
				if e := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: dir}, item, nil); e != nil {
					t.Fatalf("session getter download %s: %v", kind, e)
				}
				break
			}
		}
		t.Logf("session getter downloaded one image from each category")
	}
}
