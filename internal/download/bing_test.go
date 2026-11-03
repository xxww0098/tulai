package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/xxww0098/tulai/internal/camoufox"
	xhtml "golang.org/x/net/html"
)

func bingTestResult(image, source string) string {
	data, _ := json.Marshal(map[string]string{"murl": image, "purl": source})
	return `<a class="iusc" m="` + html.EscapeString(string(data)) + `"></a>`
}

func bingTestResponse(raw string, status int, body string) *Response {
	return &Response{StatusCode: status, FinalURL: raw, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestBingSearchPaginationAndPrivacy(t *testing.T) {
	var calls []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		prepare(req)
		if req.URL.Host != "www.bing.com" || req.Header.Get("Cookie") != "" {
			t.Fatalf("search request host/cookie = %s / %q", req.URL.Host, req.Header.Get("Cookie"))
		}
		calls = append(calls, req.URL.Path)
		if req.URL.Path == "/images/search" {
			return bingTestResponse(raw, 200, `<title>Bing cats</title>`+
				bingTestResult("https://img.test/one.jpg", "https://src.test/photo?secret=one")+
				bingTestResult("http://img.test/two.jpg", "https://src.test/private?secret=two")+
				`<div data-nextUrl="/images/async?q=cat&amp;first=3"></div>`), nil
		}
		if req.URL.Query().Get("qft") != "blue" || req.Header.Get("Referer") != "https://www.bing.com/images/search?q=cat&qft=blue&secret=private" {
			t.Fatalf("pagination query/header = %s / %s", req.URL.RawQuery, req.Header.Get("Referer"))
		}
		return bingTestResponse(raw, 200, bingTestResult("https://img.test/one.jpg", "")+bingTestResult("https://img.test/three.jpg", "")), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.bing.com/images/search?q=cat&qft=blue&secret=private", 1)
	if err != nil || !got.IsPage || got.Title != "Bing cats" || len(got.Items) != 3 || strings.Join(calls, ",") != "/images/search,/images/async" {
		t.Fatalf("result = %+v, calls = %v, err = %v", got, calls, err)
	}
	if ref := got.Items[0].Headers["Referer"]; ref != "https://src.test/" {
		t.Fatalf("cross-origin referer = %q", ref)
	}
	if ref := got.Items[1].Headers["Referer"]; ref != "" {
		t.Fatalf("HTTPS to HTTP referer = %q", ref)
	}
	if ref := got.Items[2].Headers["Referer"]; ref != "https://www.bing.com/" {
		t.Fatalf("fallback referer = %q", ref)
	}
	for _, item := range got.Items {
		if item.Headers["Cookie"] != "" || strings.Contains(item.Headers["Referer"], "secret=") {
			t.Fatalf("credentials leaked: %+v", item)
		}
	}
}

const bingTestConfig = `<script>_w["DgConfig"]={"aurl":"/images/async?q=%QUERY%\u0026async=content\u0026first=%FIRST%\u0026count=%COUNT%\u0026fixed=site-value","ac":2};</script>`

func TestBingSearchAdvertisedAsyncPagination(t *testing.T) {
	var positions []string
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		u, _ := url.Parse(raw)
		positions = append(positions, u.Query().Get("first"))
		if u.Query().Get("qft") != "+filterui:photo" {
			t.Fatalf("lost filter: %s", u.RawQuery)
		}
		if u.Path == "/images/search" {
			return bingTestResponse(raw, 200, bingTestConfig+bingTestResult("https://img.test/1", "")+bingTestResult("https://img.test/2", "")), nil
		}
		if u.Path != "/images/async" || u.Query().Get("count") != "2" || u.Query().Get("async") != "content" || u.Query().Get("fixed") != "site-value" {
			t.Fatalf("async query = %s", raw)
		}
		if u.Query().Get("first") == "3" {
			return bingTestResponse(raw, 200, bingTestResult("https://img.test/2", "")+bingTestResult("https://img.test/3", "")), nil
		}
		return bingTestResponse(raw, 200, `<div class="dgControl"></div>`), nil
	})
	got, err := Discover(context.Background(), getter, "https://www.bing.com/images/search?q=cat&qft=%2Bfilterui%3Aphoto", 1)
	if err != nil || len(got.Items) != 3 || strings.Join(positions, ",") != ",3,5" {
		t.Fatalf("items = %+v, positions = %v, err = %v", got.Items, positions, err)
	}
}

func TestBingSearchFailuresKeepPartialResults(t *testing.T) {
	for _, mode := range []string{"repeat", "http", "missing", "external", "cycle", "cancel", "limit"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				calls++
				if calls > 1 && mode == "http" {
					return bingTestResponse(raw, 503, "unavailable"), nil
				}
				if calls > 1 && mode == "missing" {
					return bingTestResponse(raw, 200, `<a class="iusc" m="{}"></a>`), nil
				}
				if calls == 1 && mode == "cancel" {
					cancel()
				}
				image := "https://img.test/" + strconv.Itoa(calls)
				if mode == "repeat" {
					image = "https://img.test/same"
				}
				body := bingTestResult(image, "")
				if mode == "external" {
					body += `<div data-nexturl="https://evil.test/images/async?secret=1"></div>`
				} else if mode == "cycle" {
					body += `<div data-nexturl="/images/search?q=cat"></div>`
				} else {
					body += bingTestConfig
				}
				return bingTestResponse(raw, 200, body), nil
			})
			got, err := Discover(ctx, getter, "https://www.bing.com/images/search?q=cat", 1)
			if err == nil || len(got.Items) == 0 {
				t.Fatalf("expected partial result and error: items=%d err=%v", len(got.Items), err)
			}
			switch mode {
			case "http":
				var status *HTTPStatusError
				if !errors.As(err, &status) || status.StatusCode != 503 || calls != 2 {
					t.Fatalf("status error = %v, calls = %d", err, calls)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || calls != 1 {
					t.Fatalf("cancel error = %v, calls = %d", err, calls)
				}
			case "limit":
				if calls != maxCrawlPages || len(got.Items) != maxCrawlPages || !strings.Contains(err.Error(), "上限") {
					t.Fatalf("limit calls=%d items=%d err=%v", calls, len(got.Items), err)
				}
			case "external", "cycle":
				if calls != 1 {
					t.Fatalf("unexpected pagination request: %d", calls)
				}
			}
		})
	}
}

func TestBingSearchRecognizesOnlySearchPages(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"https://www.bing.com/images/search?q=cat", true},
		{"https://cn.bing.com/images/search?q=cat&qft=photo", true},
		{"https://bing.com/images/search?first=1&q=cat", true},
		{"https://www.bing.com/images/search?view=detailV2&q=cat", false},
		{"https://evilbing.com/images/search?q=cat", false},
		{"https://www.bing.com.evil.test/images/search?q=cat", false},
		{"https://user@www.bing.com/images/search?q=cat", false},
		{"https://www.bing.com:8443/images/search?q=cat", false},
		{"https://www.bing.com/images/search?q=", false},
		{"https://www.bing.com/images/async?q=cat", false},
	} {
		u, _ := url.Parse(tc.raw)
		if got := isBingImageSearch(u); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.raw, got, tc.want)
		}
	}
}

func TestDiscoverGenericLinkElementsAndDepth(t *testing.T) {
	var requests []string
	var externalCalls int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalCalls++
		fmt.Fprint(w, `<img src="/external.jpg">`)
	}))
	defer external.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, `<html><head>
			<link rel="next" href="/linked"><link rel="alternate" type="text/html" href="/alternate"><link rel="prev" href="/previous">
			<link rel="stylesheet" href="/missing.css"><link rel="preconnect" href="/preconnect">
			<link rel="preload" as="script" href="/missing.js"><link rel="alternate" type="application/rss+xml" href="/feed">
			<link rel="alternate stylesheet" href="/alternate.css"><link href="/direct-link.png">
			<link rel="preload" as="image" href="/image-without-extension"></head><body>
			<a href="/anchor">anchor</a><a data-href="/custom">custom</a>
			<map><area href="/area"><area href="/direct-area.jpg" data-preview="/direct-custom.gif"></map>
			<iframe src="/iframe"></iframe><iframe src="%s/foreign"></iframe></body></html>`, external.URL)
		case "/frame-root":
			fmt.Fprint(w, `<html><frameset><frame src="/frame"></frameset></html>`)
		default:
			fmt.Fprintf(w, `<img src="%s.jpg">`, r.URL.Path)
		}
	}))
	defer srv.Close()

	first, err := Discover(context.Background(), NewHTTPGetter(""), srv.URL+"/", 1)
	if err != nil || len(first.Items) != 4 || len(requests) != 1 {
		t.Fatalf("depth 1 items=%+v requests=%v err=%v", first.Items, requests, err)
	}
	requests = nil
	second, err := Discover(context.Background(), NewHTTPGetter(""), srv.URL+"/", 2)
	if err != nil || len(second.Items) != 11 || len(requests) != 8 || externalCalls != 0 {
		t.Fatalf("depth 2 items=%+v requests=%v external=%d err=%v", second.Items, requests, externalCalls, err)
	}
	requests = nil
	frame, err := Discover(context.Background(), NewHTTPGetter(""), srv.URL+"/frame-root", 2)
	if err != nil || len(frame.Items) != 1 || frame.Items[0].URL != srv.URL+"/frame.jpg" {
		t.Fatalf("frame items=%+v requests=%v err=%v", frame.Items, requests, err)
	}
}

func TestBingLiveProbe(t *testing.T) {
	if os.Getenv("TULAI_BING_LIVE") != "1" {
		t.Skip("set TULAI_BING_LIVE=1 to verify the current Bing search endpoint")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	base := NewHTTPGetter("")
	calls := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		if calls == 3 {
			cancel()
			return nil, ctx.Err()
		}
		return base.Get(ctx, raw, prepare)
	})
	got, err := Discover(ctx, getter, "https://www.bing.com/images/search?q=landscape", 1)
	if !errors.Is(err, context.Canceled) || calls != 3 || len(got.Items) <= 35 {
		t.Fatalf("live search: %d requests, %d results, %v", calls, len(got.Items), err)
	}
	t.Logf("Bing search and one async page returned %d unique original image URLs", len(got.Items))
}

const bingDetailTestID = "E77AAAC2BBE0417E703ECA19441AD83A6D1CCE79"
const bingDetailTestThumb = "OIP.i_G3VuXE6mzxECEewWLPcAHaFi"

func bingDetailTestURL() string {
	q := url.Values{"view": {"detailV2"}, "id": {bingDetailTestID}, "thid": {bingDetailTestThumb}, "q": {"landscape"}, "qft": {"filterui:imagesize-large"}, "filters": {"size-large"}, "mediaurl": {"https://evil.example/forged.jpg"}}
	return "https://www.bing.com/images/search?" + q.Encode()
}
func bingDetailTestDocument(id, thumb string) string {
	b, _ := json.Marshal(map[string]string{"id": id, "thumbId": thumb})
	return `<html><div id="b_idpdata" data-firstimg="` + html.EscapeString(string(b)) + `"></div></html>`
}
func bingDetailTestResult(id, thumb, image, source, query string) string {
	b, _ := json.Marshal(map[string]string{"mid": id, "murl": image, "purl": source})
	q := url.Values{"view": {"detailV2"}, "id": {id}, "thid": {thumb}, "q": {query}}
	return `<a class="iusc" href="/images/search?` + html.EscapeString(q.Encode()) + `" m="` + html.EscapeString(string(b)) + `"></a>`
}
func bingDetailTestHTML(t *testing.T, raw string) *xhtml.Node {
	t.Helper()
	n, err := xhtml.Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func TestBingDetailAddressAndRedirect(t *testing.T) {
	root, _ := url.Parse(bingDetailTestURL())
	if !isBingImageDetail(root) || bingDetailKey(root) != bingDetailTestID {
		t.Fatalf("detail not recognized")
	}
	for _, raw := range []string{
		"https://evil.bing.com/images/search?view=detailV2",
		"https://user@www.bing.com/images/search?view=detailV2",
		"https://www.bing.com:8443/images/search?view=detailV2",
		"https://www.bing.com/images/search?view=detailV2&view=detailV2",
	} {
		u, _ := url.Parse(raw)
		if isBingImageDetail(u) {
			t.Errorf("unsafe detail recognized %s", raw)
		}
	}
	other := *root
	q := other.Query()
	q.Set("q", "different")
	other.RawQuery = q.Encode()
	if bingDetailSameRequest(root, &other) {
		t.Error("changed query accepted")
	}
	q = other.Query()
	q.Set("q", "landscape")
	q.Set("thid", "OIP.other")
	other.RawQuery = q.Encode()
	if bingDetailSameRequest(root, &other) {
		t.Error("changed thumbnail accepted")
	}
	other = *root
	q = other.Query()
	q.Set("qft", "filterui:imagesize-small")
	other.RawQuery = q.Encode()
	if bingDetailSameRequest(root, &other) {
		t.Error("changed search filter accepted")
	}
	other = *root
	other.Scheme = "http"
	if bingDetailSameRequest(root, &other) {
		t.Error("HTTPS downgrade accepted")
	}
	noQuery := *root
	q = noQuery.Query()
	q.Del("q")
	noQuery.RawQuery = q.Encode()
	if _, err := bingDetailTargetOf(&noQuery); err == nil {
		t.Error("missing query accepted")
	}
}
func TestBingDetailCrossVerificationAndPrivacy(t *testing.T) {
	root := bingDetailTestURL()
	calls := 0
	getter := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
		prepare(req)
		if req.URL.Host != "www.bing.com" || req.Header.Get("Cookie") != "" {
			t.Fatalf("unsafe request")
		}
		if req.URL.Query().Get("view") != "" {
			return bingTestResponse(raw, 200, bingDetailTestDocument(bingDetailTestID, bingDetailTestThumb)), nil
		}
		q := req.URL.Query()
		if q.Get("q") != "landscape" || q.Get("qft") != "filterui:imagesize-large" || q.Get("filters") != "size-large" || q.Has("mediaurl") || q.Has("id") {
			t.Fatalf("search filters=%v", q)
		}
		return bingTestResponse(raw, 200, bingDetailTestResult("1111111111111111111111111111111111111111", "OIP.other", "https://other.example/x.jpg", "", "landscape")+bingDetailTestResult(bingDetailTestID, bingDetailTestThumb, "https://images.example.com/original.jpg", "https://images.example.com/article?token=synthetic-secret", "landscape")), nil
	})
	got, err := Discover(context.Background(), getter, root, 1)
	if err != nil || calls != 2 || len(got.Items) != 1 || got.Items[0].URL != "https://images.example.com/original.jpg" {
		t.Fatalf("items=%+v calls=%d err=%v", got.Items, calls, err)
	}
	item := got.Items[0]
	if !item.PublicOnly || !item.RequireImage || item.Metadata["bing_image_id"] != bingDetailTestID || item.Headers["Referer"] != "https://images.example.com/" {
		t.Fatalf("unsafe item %+v", item)
	}
}
func TestBingDetailPageAndResultRejection(t *testing.T) {
	page, _ := url.Parse(bingDetailTestURL())
	target, err := bingDetailTargetOf(page)
	if err != nil {
		t.Fatal(err)
	}
	asyncData, _ := json.Marshal(map[string]any{"web": map[string]any{"0": map[string]string{"mid": bingDetailTestID, "thId": bingDetailTestThumb}}})
	asyncDOM := `<span json-data="` + html.EscapeString(url.QueryEscape(string(asyncData))) + `"></span>`
	if err := bingDetailPageCheck(bingDetailTestHTML(t, asyncDOM), target); err != nil {
		t.Fatalf("reference async data rejected: %v", err)
	}
	for _, bad := range []string{
		bingDetailTestDocument("1111111111111111111111111111111111111111", bingDetailTestThumb),
		bingDetailTestDocument(bingDetailTestID, "OIP.other"),
		`<meta property="og:image" content="https://images.example.com/avatar.jpg">`,
		bingDetailTestDocument(bingDetailTestID, bingDetailTestThumb) + `<div>verify you are human</div>`,
	} {
		if err := bingDetailPageCheck(bingDetailTestHTML(t, bad), target); err == nil {
			t.Error("unverified page accepted")
		}
	}
	for name, result := range map[string]string{
		"different mid":    bingDetailTestResult("1111111111111111111111111111111111111111", bingDetailTestThumb, "https://images.example.com/a.jpg", "", "landscape"),
		"wrong thumbnail":  bingDetailTestResult(bingDetailTestID, "OIP.other", "https://images.example.com/a.jpg", "", "landscape"),
		"private image":    bingDetailTestResult(bingDetailTestID, bingDetailTestThumb, "http://127.0.0.1/private.jpg", "", "landscape"),
		"different query":  bingDetailTestResult(bingDetailTestID, bingDetailTestThumb, "https://images.example.com/a.jpg", "", "different"),
		"conflicting URLs": bingDetailTestResult(bingDetailTestID, bingDetailTestThumb, "https://images.example.com/a.jpg", "", "landscape") + bingDetailTestResult(bingDetailTestID, bingDetailTestThumb, "https://images.example.com/b.jpg", "", "landscape"),
	} {
		t.Run(name, func(t *testing.T) {
			getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
				return bingTestResponse(raw, 200, result), nil
			})
			got, err := bingDetailCollect(context.Background(), getter, page, bingDetailTestHTML(t, bingDetailTestDocument(bingDetailTestID, bingDetailTestThumb)))
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("unverified result=%+v err=%v", got.Items, err)
			}
		})
	}
}
func TestBingDetailPaginationAndCancellation(t *testing.T) {
	page, _ := url.Parse(bingDetailTestURL())
	calls := 0
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls++
		u, _ := url.Parse(raw)
		if u.Query().Get("qft") != "filterui:imagesize-large" || u.Query().Get("filters") != "size-large" {
			t.Fatal("pagination dropped filters")
		}
		if calls == 1 {
			return bingTestResponse(raw, 200, bingDetailTestResult("1111111111111111111111111111111111111111", "OIP.other", "https://images.example.com/other.jpg", "", "landscape")+`<div data-nexturl="/images/async?q=landscape&amp;first=2"></div>`), nil
		}
		return bingTestResponse(raw, 200, bingDetailTestResult(bingDetailTestID, bingDetailTestThumb, "https://images.example.com/a.jpg", "", "landscape")), nil
	})
	doc := bingDetailTestHTML(t, bingDetailTestDocument(bingDetailTestID, bingDetailTestThumb))
	got, err := bingDetailCollect(context.Background(), getter, page, doc)
	if err != nil || len(got.Items) != 1 || calls != 2 {
		t.Fatalf("pagination items=%d calls=%d err=%v", len(got.Items), calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bingDetailCollect(ctx, getter, page, doc); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
func TestBingDetailHistoricalReplay(t *testing.T) {
	dir := os.Getenv("TULAI_BING_DETAIL_FIXTURES")
	if dir == "" {
		t.Skip("set TULAI_BING_DETAIL_FIXTURES to saved public responses")
	}
	var rows []struct {
		Href  string `json:"href"`
		Image string `json:"murl"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "search-rows.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	search, err := os.ReadFile(filepath.Join(dir, "search.html"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		detail, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("detail%d.html", i)))
		if err != nil {
			t.Fatal(err)
		}
		root := "https://www.bing.com" + rows[i].Href
		getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
			u, _ := url.Parse(raw)
			if u.Query().Has("view") {
				return bingTestResponse(raw, 200, string(detail)), nil
			}
			return bingTestResponse(raw, 200, string(search)), nil
		})
		got, err := Discover(context.Background(), getter, root, 1)
		if err != nil || len(got.Items) != 1 || got.Items[0].URL != rows[i].Image {
			t.Fatalf("public replay %d items=%d err=%v", i, len(got.Items), err)
		}
	}
}
func TestBingDetailLive(t *testing.T) {
	if os.Getenv("TULAI_BING_DETAIL_LIVE") != "1" {
		t.Skip("set TULAI_BING_DETAIL_LIVE=1 to verify a public detail")
	}
	dir := os.Getenv("TULAI_BING_DETAIL_FIXTURES")
	var rows []struct {
		Href  string `json:"href"`
		Image string `json:"murl"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "search-rows.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got, err := Discover(ctx, NewHTTPGetter(""), "https://www.bing.com"+rows[0].Href, 1)
	if err != nil || len(got.Items) != 1 || got.Items[0].URL != rows[0].Image {
		t.Fatalf("live detail items=%d err=%v", len(got.Items), err)
	}
	t.Log("public detail ID matched the original image in current Bing search results")
	if os.Getenv("TULAI_BING_DETAIL_DOWNLOAD") == "1" {
		dir := t.TempDir()
		if err := New(Options{Getter: NewHTTPGetter("")}).Download(ctx, Job{Directory: dir}, got.Items[0], nil); err != nil {
			t.Fatalf("public image download: %v", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("saved entries=%d err=%v", len(entries), err)
		}
		info, err := entries[0].Info()
		if err != nil || info.Size() == 0 {
			t.Fatalf("saved image err=%v", err)
		}
		t.Logf("public original downloaded %d bytes", info.Size())
	}
}

type bingDetailTestTransport func(*http.Request) (*http.Response, error)

func (f bingDetailTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestBingDetailCamoufoxCapture(t *testing.T) {
	runtime := os.Getenv("TULAI_BING_DETAIL_CAMOUFOX_ROOT")
	if runtime == "" {
		t.Skip("set TULAI_BING_DETAIL_CAMOUFOX_ROOT to a copied temporary runtime")
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
	var sameCookie, foreignCookie atomic.Bool
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	getter.client.Transport = bingDetailTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "www.bing.com" {
			sameCookie.Store(strings.Contains(r.Header.Get("Cookie"), "synthetic_session=example"))
			body := bingDetailTestResult(bingDetailTestID, bingDetailTestThumb, "https://images.example.com/a.png", "", "landscape")
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		}
		foreignCookie.Store(r.Header.Get("Cookie") != "")
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(strings.NewReader(string(png))), Request: r}, nil
	})
	root := bingDetailTestURL()
	page, _ := url.Parse(root)
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
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(bingDetailTestDocument(bingDetailTestID, bingDetailTestThumb))))
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
		return network.SetCookie("synthetic_session", "example").WithURL("https://www.bing.com/").Do(c)
	})); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err != nil || len(got.Items) != 1 || !sameCookie.Load() {
		t.Fatalf("capture items=%d same-origin=%v err=%v", len(got.Items), sameCookie.Load(), err)
	}
	if err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: t.TempDir()}, got.Items[0], nil); err != nil || foreignCookie.Load() {
		t.Fatalf("cross-site download err=%v cookie=%v", err, foreignCookie.Load())
	}
}
