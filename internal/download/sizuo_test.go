package download

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
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
	"github.com/chromedp/chromedp"
	"github.com/xxww0098/tulai/internal/camoufox"
	"golang.org/x/net/html"
)

func sizuoTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
func sizuoTestDoc(t *testing.T, raw string) *html.Node {
	t.Helper()
	n, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func sizuoTestResponse(raw string, status int, contentType, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body))}
}

type sizuoTestTransport func(*http.Request) (*http.Response, error)

func (f sizuoTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const sizuoTestPage = "http://www.sizuo.com/shijing/2356.html"
const sizuoTestJS = `PrevArr=new Array(['12','neighbor','/uploads/allimg/old/other.jpg']);CurGallery=new Array('2356','','title','2356.html',['/uploads/allimg/141103/1-1411031J424.jpg','/uploads/allimg/141103/1-1411031J428.jpg'],'author');NextArr=new Array(['/uploads/allimg/old/next.jpg']);iGocwebPics=1;`

func TestSizuoArticleAndEndpoint(t *testing.T) {
	for _, tc := range []struct{ raw, id string }{
		{sizuoTestPage, "2356"}, {"https://www.sizuo.com/shufang/2813.html?token=synthetic", "2813"},
		{"https://www.sizuo.com/jiazhuang/123.html", "123"}, {"https://sizuo.com/shijing/2356.html", ""},
		{"https://www.sizuo.com.evil.test/shijing/2356.html", ""}, {"https://user@www.sizuo.com/shijing/2356.html", ""},
		{"https://www.sizuo.com:8443/shijing/2356.html", ""}, {"https://www.sizuo.com/shijing/23%2F56.html", ""},
		{"https://www.sizuo.com/shijing/../2356.html", ""}, {"https://www.sizuo.com/shijing/0.html", ""},
	} {
		if got := sizuoArticleID(sizuoTestURL(t, tc.raw)); got != tc.id {
			t.Errorf("%s ID=%q want %q", tc.raw, got, tc.id)
		}
	}
	page := sizuoTestURL(t, sizuoTestPage)
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"/getimgs.php?aid=2356", true}, {"/getimgs.php?aid=2813", false}, {"/getimgs.php?aid=2356&aid=2356", false},
		{"/getimgs.php?aid=2356&token=secret", false}, {"//evil.test/getimgs.php?aid=2356", false},
		{"http://user@www.sizuo.com/getimgs.php?aid=2356", false}, {"/getimgs.php?aid=2356#fragment", false},
	} {
		_, ok := sizuoEndpoint(page, tc.raw)
		if ok != tc.ok {
			t.Errorf("endpoint %s got %v", tc.raw, ok)
		}
	}
	for _, raw := range []string{"/uploads/allimg/../secret.jpg", "/uploads/allimg/%2e%2e/secret.jpg", "/uploads/allimg/a%2Fb.jpg", "/uploads/allimg/a.jpg?token=x", "//evil.test/uploads/allimg/a.jpg", "/uploads/allimg/a.svg"} {
		if _, ok := sizuoAsset(page, raw); ok {
			t.Errorf("unsafe asset %q", raw)
		}
	}
}
func TestSizuoCurrentImages(t *testing.T) {
	images, ok := sizuoCurrentImages(sizuoTestJS, "2356")
	if !ok || len(images) != 2 || strings.Contains(strings.Join(images, ","), "old/") {
		t.Fatalf("current images=%v ok=%v", images, ok)
	}
	pic := `PicArr=new Array(['7','1','other','7.html','/uploads/allimg/1/other.jpg'],['2356','1','wanted','2356.html','/uploads/allimg/2/current.jpg']);CurPos=1;`
	images, ok = sizuoCurrentImages(pic, "2356")
	if !ok || len(images) != 1 || images[0] != "/uploads/allimg/2/current.jpg" {
		t.Fatalf("PicArr images=%v ok=%v", images, ok)
	}
	for _, raw := range []string{
		strings.Replace(sizuoTestJS, "CurGallery=new Array('2356'", "CurGallery=new Array('999'", 1),
		strings.Replace(pic, "CurPos=1", "CurPos=0", 1),
		`CurGallery=executeUntrustedCode();`,
		`var decoy="CurGallery=new Array('2356','','','2356.html',['/uploads/allimg/x/a.jpg'])";`,
		`ignore=1/*; CurGallery=new Array('2356','','','2356.html',['/uploads/allimg/x/a.jpg']); */;`,
		sizuoTestJS + `CurGallery=new Array('2356','','','2356.html',['/uploads/allimg/x/duplicate.jpg']);`,
	} {
		if got, ok := sizuoCurrentImages(raw, "2356"); ok {
			t.Errorf("unexpected accepted data %v", got)
		}
	}
	escaped := `CurGallery=new Array('2356','','a\\'b','2356.html',['/uploads/allimg/x/a.jpg']);`
	// The backslash in the source represents an escaped quote, not executable code.
	escaped = strings.ReplaceAll(escaped, `\\'`, `\'`)
	if got, ok := sizuoCurrentImages(escaped, "2356"); !ok || len(got) != 1 {
		t.Fatalf("escaped literal failed %v %v", got, ok)
	}
}
func TestSizuoSyntheticCollect(t *testing.T) {
	page := sizuoTestURL(t, sizuoTestPage+"?token=synthetic-secret")
	doc := sizuoTestDoc(t, `<title>Test</title><script src="/getimgs.php?aid=2356"></script><img src="/uploads/allimg/old/recommended.jpg">`)
	getter := GetterFunc(func(_ context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequest("GET", raw, nil)
		prepare(req)
		if raw != "http://www.sizuo.com/getimgs.php?aid=2356" || strings.Contains(req.Header.Get("Referer"), "secret") {
			return nil, errors.New("incorrect request")
		}
		return sizuoTestResponse(raw, 200, "text/html", sizuoTestJS), nil
	})
	got, err := sizuoCollect(context.Background(), getter, page, doc)
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("items=%d err=%v", len(got.Items), err)
	}
	for _, item := range got.Items {
		if !item.RequireImage || !item.PublicOnly || item.Metadata["sizuo_article_id"] != "2356" || strings.Contains(item.URL, "old/") {
			t.Errorf("unexpected item %+v", item)
		}
	}
}
func TestSizuoRedirectAndCancel(t *testing.T) {
	getter := NewHTTPGetter("")
	var foreign atomic.Bool
	getter.client.Transport = sizuoTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "www.sizuo.com" {
			foreign.Store(true)
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://evil.test/shijing/2356.html"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if got, err := discoverSizuo(context.Background(), getter, sizuoTestURL(t, sizuoTestPage)); err == nil || foreign.Load() || len(got.Items) != 0 {
		t.Fatalf("unsafe redirect %v %v", foreign.Load(), err)
	}
	endpoint := sizuoTestURL(t, "http://www.sizuo.com/getimgs.php?aid=2356")
	getter.client.Transport = sizuoTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() != "www.sizuo.com" || r.URL.RawQuery != "aid=2356" {
			foreign.Store(true)
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"http://www.sizuo.com/getimgs.php?aid=9999"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	})
	if _, err := sizuoEndpointGetter(getter, endpoint).Get(context.Background(), endpoint.String(), nil); err == nil || foreign.Load() {
		t.Fatalf("unsafe endpoint redirect %v %v", foreign.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverSizuo(ctx, getter, sizuoTestURL(t, sizuoTestPage)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
func TestSizuoResponseLimit(t *testing.T) {
	page := sizuoTestURL(t, sizuoTestPage)
	doc := sizuoTestDoc(t, `<script src="/getimgs.php?aid=2356"></script>`)
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		return sizuoTestResponse(raw, 200, "text/html", strings.Repeat("x", (2<<20)+1)), nil
	})
	got, err := sizuoCollect(context.Background(), getter, page, doc)
	if err == nil || !strings.Contains(err.Error(), "上限") || len(got.Items) != 0 {
		t.Fatalf("oversized response: %d items, %v", len(got.Items), err)
	}
}
func TestSizuoHistoricalReplay(t *testing.T) {
	dir := os.Getenv("TULAI_SIZUO_FIXTURES")
	if dir == "" {
		t.Skip("set TULAI_SIZUO_FIXTURES to archived public page/response files")
	}
	for _, tc := range []struct {
		id, section string
		count       int
	}{{"2356", "shijing", 15}, {"2813", "shufang", 1}} {
		pageBytes, err := os.ReadFile(filepath.Join(dir, "sizuo-"+tc.id+".html"))
		if err != nil {
			t.Fatal(err)
		}
		jsBytes, err := os.ReadFile(filepath.Join(dir, "sizuo-"+tc.id+".js"))
		if err != nil {
			t.Fatal(err)
		}
		page := "http://www.sizuo.com/" + tc.section + "/" + tc.id + ".html"
		endpoint := "http://www.sizuo.com/getimgs.php?aid=" + tc.id
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			switch raw {
			case page:
				return sizuoTestResponse(raw, 200, "text/html; charset=utf-8", string(pageBytes)), nil
			case endpoint:
				return sizuoTestResponse(raw, 200, "text/html; charset=utf-8", string(jsBytes)), nil
			default:
				return nil, fmt.Errorf("unexpected request")
			}
		})
		got, err := Discover(context.Background(), getter, page, 1)
		if err != nil || len(got.Items) != tc.count {
			t.Fatalf("article %s images=%d want=%d err=%v", tc.id, len(got.Items), tc.count, err)
		}
		t.Logf("historical article %s: %d current-work images", tc.id, len(got.Items))
	}
}
func TestSizuoLive(t *testing.T) {
	if os.Getenv("TULAI_SIZUO_LIVE") != "1" {
		t.Skip("set TULAI_SIZUO_LIVE=1 for one public historical URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), sizuoTestPage, 1)
	if err == nil || len(got.Items) != 0 {
		t.Skipf("site state changed: %d images err=%v", len(got.Items), err)
	}
	t.Logf("current site: %v", err)
}
func TestSizuoCamoufoxCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_SIZUO_CAMOUFOX_ROOT")
	if runtime == "" {
		t.Skip("set TULAI_SIZUO_CAMOUFOX_ROOT to a copied temporary runtime")
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
	var usedCookie atomic.Bool
	getter.client.Transport = sizuoTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://www.sizuo.com/getimgs.php?aid=2356" {
			return nil, errors.New("unexpected synthetic request")
		}
		usedCookie.Store(strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(sizuoTestJS)), Request: r}, nil
	})
	root := "https://www.sizuo.com/shijing/2356.html"
	page := sizuoTestURL(t, root)
	session := &RenderedSession{process: process, tab: tab, closeTab: closeTab, closeAllocator: process.Close, releaseProfile: func() {}, getter: getter, start: page}
	defer session.Close()
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == root {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(`<title>Test</title><script src="/getimgs.php?aid=2356"></script>`)))
			} else {
				action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			}
			_ = camoufox.Run(session.tab, action)
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, camoufox.Navigate(root)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(c context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.sizuo.com/").Do(c)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 2 || !usedCookie.Load() {
		t.Fatalf("capture items=%d authorized=%v err=%v", len(got.Items), usedCookie.Load(), err)
	}
}
