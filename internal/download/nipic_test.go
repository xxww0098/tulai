package download

import (
	"context"
	"encoding/base64"
	"errors"
	"github.com/xxww0098/tulai/internal/camoufox"
	"image"
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
	"github.com/chromedp/chromedp"

	"golang.org/x/net/html"
)

const nipicTestPostURL = "https://www.nipic.com/show/56851093.html"
const nipicTestPreview = "https://pic.nximg.cn/file/20261029/1703163_145100649108_2.jpg"
const huituTestPostURL = "https://www.huitu.com/photo/show/20260120/224443498223.html"
const huituTestCode = "20260120224443498223"
const huituTestPreview = "https://pic.huitu.com/res/20260120/1449698_20260120224443498223_1.jpg"

func nipicTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func nipicTestDoc(t *testing.T, body string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func nipicTestPost() string {
	return `<link rel="canonical" href="` + nipicTestPostURL + `"><meta property="og:image" content="` + nipicTestPreview + `"><div id="J_detailMain"><h1>作品</h1><img id="J_worksImg" class="works-img main-image" src="` + nipicTestPreview + `" alt="作品"></div><input id="worksId" value="56851093"><img src="https://pic.nximg.cn/file/recommendation.jpg">`
}

func huituTestPost() string {
	return `<div class="m-img-wrap"><img class="m-img" src="` + huituTestPreview + `" alt="雪"></div><div class="pic-info-box"><label>编号：` + huituTestCode + `</label></div>`
}

func TestNipicRoutes(t *testing.T) {
	for _, tc := range []struct{ raw, kind, id string }{
		{nipicTestPostURL, "nipic", "56851093"},
		{huituTestPostURL, "huitu", huituTestCode},
		{"https://soso.nipic.com/?q=%E9%9B%AA&page=2", "search", ""},
		{"https://www.huitu.com/design/show/20260920/131757028200.html", "unsupported", ""},
		{"https://www.huitu.com/photo/vcg/detail/123.html", "unsupported", ""},
		{"https://soso.nipic.com/q_%E9%9B%AA_g_0.html", "unsupported", ""},
		{"https://www.nipic.com/show/0.html", "", ""},
		{"https://www.nipic.com/show/56851093.html/extra", "", ""},
		{"https://www.nipic.com.evil.test/show/56851093.html", "", ""},
		{"https://user@www.nipic.com/show/56851093.html", "", ""},
		{"https://www.huitu.com:8443/photo/show/20260120/224443498223.html", "", ""},
		{"https://soso.nipic.com/?q=a&q=b", "", ""},
		{"https://soso.nipic.com/?q=a&page=1&page=2", "", ""},
		{"https://soso.nipic.com/?q=a&page=0", "", ""},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			kind, id := nipicRoute(nipicTestURL(t, tc.raw))
			if kind != tc.kind || id != tc.id {
				t.Fatalf("route=%s/%s want=%s/%s", kind, id, tc.kind, tc.id)
			}
		})
	}
}

func TestNipicPostIdentityAndPreview(t *testing.T) {
	for _, tc := range []struct{ name, raw, body, want, id string }{
		{"nipic", nipicTestPostURL, nipicTestPost(), nipicTestPreview, "56851093"},
		{"huitu", huituTestPostURL, huituTestPost(), huituTestPreview, huituTestCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nipicPost(context.Background(), nipicTestURL(t, tc.raw), nipicTestDoc(t, tc.body))
			if !errors.Is(err, errNipicPreview) || len(got.Items) != 1 || got.Items[0].URL != tc.want ||
				got.Items[0].Metadata["nipic_work_id"] != tc.id || got.Items[0].Metadata["image_kind"] != "preview" || !got.Items[0].RequireImage || !got.Items[0].PublicOnly {
				t.Fatalf("post=%+v err=%v", got, err)
			}
		})
	}
}

func TestNipicRejectsUnboundPreview(t *testing.T) {
	for _, tc := range []struct{ name, raw, body string }{
		{"nipic wrong id", nipicTestPostURL, strings.Replace(nipicTestPost(), `value="56851093"`, `value="999"`, 1)},
		{"nipic wrong canonical", nipicTestPostURL, strings.Replace(nipicTestPost(), `href="`+nipicTestPostURL+`"`, `href="https://www.nipic.com/show/999.html"`, 1)},
		{"nipic image mismatch", nipicTestPostURL, strings.Replace(nipicTestPost(), `content="`+nipicTestPreview+`"`, `content="https://pic.nximg.cn/file/other.jpg"`, 1)},
		{"nipic fake host", nipicTestPostURL, strings.ReplaceAll(nipicTestPost(), "pic.nximg.cn", "pic.nximg.cn.evil.test")},
		{"nipic absent image", nipicTestPostURL, strings.Replace(nipicTestPost(), `id="J_worksImg"`, `id="recommendation"`, 1)},
		{"huitu wrong id", huituTestPostURL, strings.Replace(huituTestPost(), "编号："+huituTestCode, "编号：20260120999999999999", 1)},
		{"huitu wrong image", huituTestPostURL, strings.Replace(huituTestPost(), huituTestCode+"_1.jpg", "20260120999999999999_1.jpg", 1)},
		{"huitu original guessed", huituTestPostURL, strings.Replace(huituTestPost(), "/res/", "/pic/", 1)},
		{"huitu fake host", huituTestPostURL, strings.Replace(huituTestPost(), "pic.huitu.com", "pic.huitu.com.evil.test", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := nipicPost(context.Background(), nipicTestURL(t, tc.raw), nipicTestDoc(t, tc.body))
			if err == nil || len(got.Items) != 0 {
				t.Fatalf("accepted %s: %+v %v", tc.name, got.Items, err)
			}
		})
	}
}

func TestNipicSearchMixedPagesAndPartial(t *testing.T) {
	root := nipicTestURL(t, "https://soso.nipic.com/?q=snow")
	first := `<ul id="img-list-outer"><li class="new-search-works-item"><a class="search-works-thumb" href="` + nipicTestPostURL + `"></a></li>` +
		`<li class="new-search-works-item"><a class="search-works-thumb" href="` + huituTestPostURL + `"></a></li>` +
		`<li class="new-search-works-item"><a class="search-works-thumb" href="https://www.huitu.com/design/show/1/2.html"></a></li></ul>` +
		`<a href="https://www.nipic.com/show/999.html">recommendation</a>` +
		`<div class="common-page-box"><a href="/?q=snow&amp;page=2" title="下一页"></a></div>`
	var calls []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		calls = append(calls, raw)
		switch raw {
		case nipicTestPostURL:
			return tiebaTestResponse(raw, 200, nipicTestPost()), nil
		case huituTestPostURL:
			return tiebaTestResponse(raw, 200, huituTestPost()), nil
		case "https://soso.nipic.com/?q=snow&page=2":
			return tiebaTestResponse(raw, 200, `<ul id="img-list-outer"><li class="new-search-works-item"><a class="search-works-thumb" href="https://www.nipic.com/show/999.html"></a></li></ul>`), nil
		case "https://www.nipic.com/show/999.html":
			return tiebaTestResponse(raw, 403, ""), nil
		default:
			t.Fatalf("unexpected URL %s", raw)
			return nil, nil
		}
	})
	got, err := nipicCollect(context.Background(), getter, root, nipicTestDoc(t, first))
	if len(got.Items) != 2 || err == nil || !errors.Is(err, errNipicPreview) || !strings.Contains(err.Error(), "1 个搜索结果") || !strings.Contains(err.Error(), "1 篇作品") || len(calls) != 4 {
		t.Fatalf("items=%+v calls=%v err=%v", got.Items, calls, err)
	}
}

func TestNipicSearchPageFiltersAndBounds(t *testing.T) {
	root := nipicTestURL(t, "https://soso.nipic.com/?q=snow&page=1")
	body := `<ul id="img-list-outer"><li class="new-search-works-item"><a class="search-works-thumb" href="` + nipicTestPostURL + `"></a></li></ul>` +
		`<div class="common-page-box"><a href="/?q=other&amp;page=2">2</a></div>`
	list, err := nipicExtractSearch(root, nipicTestDoc(t, body))
	if err == nil || len(list.posts) != 1 || list.next != nil {
		t.Fatalf("changed search accepted: %+v %v", list, err)
	}
	list, err = nipicExtractSearch(root, nipicTestDoc(t, body+`<ul id="img-list-outer"></ul>`))
	if err == nil || len(list.posts) != 0 {
		t.Fatalf("duplicate search grids accepted: %+v %v", list, err)
	}
}

func TestNipicCancellationAndRedirect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := nipicPost(ctx, nipicTestURL(t, nipicTestPostURL), nipicTestDoc(t, nipicTestPost()))
	if len(got.Items) != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %+v %v", got, err)
	}
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		resp := tiebaTestResponse(raw, 200, nipicTestPost())
		resp.FinalURL = "https://www.nipic.com/show/999.html"
		return resp, nil
	})
	got, err = discoverNipic(context.Background(), getter, nipicTestURL(t, nipicTestPostURL))
	if err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect to another work accepted: %+v %v", got, err)
	}
}

func TestNipicPublicSamples(t *testing.T) {
	root := os.Getenv("TULAI_NIPIC_SAMPLES")
	if root == "" {
		t.Skip("set TULAI_NIPIC_SAMPLES to saved public pages")
	}
	read := func(file string) *html.Node {
		f, err := os.Open(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		doc, err := html.Parse(f)
		if err != nil {
			t.Fatal(err)
		}
		return doc
	}
	for _, tc := range []struct{ file, raw, want string }{
		{"post.html", nipicTestPostURL, nipicTestPreview},
		{"post2.html", "https://www.nipic.com/show/56590287.html", "https://pic.nximg.cn/file/20260911/32620414_182909088106_2.jpg"},
		{"huitu.html", huituTestPostURL, huituTestPreview},
	} {
		got, err := nipicPost(context.Background(), nipicTestURL(t, tc.raw), read(tc.file))
		if !errors.Is(err, errNipicPreview) || len(got.Items) != 1 || got.Items[0].URL != tc.want {
			t.Fatalf("public %s: %+v %v", tc.file, got, err)
		}
	}
	for _, tc := range []struct{ file, raw, next string }{
		{"search.html", "https://soso.nipic.com/?q=%E9%9B%AA", "2"},
		{"search2.html", "https://soso.nipic.com/?q=%E9%9B%AA&page=2", "3"},
	} {
		list, err := nipicExtractSearch(nipicTestURL(t, tc.raw), read(tc.file))
		if err != nil || len(list.posts) == 0 || list.next == nil || list.next.Query().Get("page") != tc.next {
			t.Fatalf("public %s: posts=%d unsupported=%d next=%v err=%v", tc.file, len(list.posts), list.unsupported, list.next, err)
		}
		t.Logf("public %s: posts=%d unsupported=%d next=%s", tc.file, len(list.posts), list.unsupported, tc.next)
	}
}

func TestNipicPublicPreviewDownloads(t *testing.T) {
	if os.Getenv("TULAI_NIPIC_LIVE") != "1" {
		t.Skip("set TULAI_NIPIC_LIVE=1 for public preview downloads")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	getter := NewHTTPGetter("")
	for _, raw := range []string{nipicTestPostURL, huituTestPostURL} {
		got, err := discoverNipic(ctx, getter, nipicTestURL(t, raw))
		if !errors.Is(err, errNipicPreview) || len(got.Items) != 1 {
			t.Fatalf("live page %s: %+v %v", raw, got, err)
		}
		dir := t.TempDir()
		if err := New(Options{Getter: getter}).Download(ctx, Job{Directory: dir}, got.Items[0], nil); err != nil {
			t.Fatalf("live preview %s: %v", raw, err)
		}
		files, err := os.ReadDir(dir)
		if err != nil || len(files) != 1 {
			t.Fatalf("files=%v err=%v", files, err)
		}
		f, err := os.Open(filepath.Join(dir, files[0].Name()))
		if err != nil {
			t.Fatal(err)
		}
		info, format, err := image.DecodeConfig(f)
		f.Close()
		if err != nil || info.Width == 0 || info.Height == 0 {
			t.Fatalf("preview decode: %v", err)
		}
		t.Logf("%s preview %s %dx%d", raw, format, info.Width, info.Height)
	}
}

type nipicRoundTrip func(*http.Request) (*http.Response, error)

func (f nipicRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func nipicTestBrowser(t *testing.T, ctx context.Context, target, body string) (*RenderedSession, chan error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `<html></html>`) }))
	t.Cleanup(server.Close)
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == target {
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
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	session.start = nipicTestURL(t, target)
	if err := session.run(ctx, camoufox.Navigate(target)); err != nil {
		t.Fatal(err)
	}
	if err := session.run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		return network.SetCookie("synthetic_session", "example").WithDomain(".nipic.com").WithURL("https://www.nipic.com/").WithHTTPOnly(true).Do(ec)
	})); err != nil {
		t.Fatal(err)
	}
	return session, interceptErrors
}

func TestNipicRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	session, interceptErrors := nipicTestBrowser(t, ctx, nipicTestPostURL, nipicTestPost())
	calls := 0
	session.getter.client.Transport = nipicRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != nipicTestPreview || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.nipic.com/" {
			t.Errorf("unsafe preview request %s %v", r.URL, r.Header)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/jpeg"}}, Body: io.NopCloser(strings.NewReader("synthetic image")), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if !errors.Is(err, errNipicPreview) || len(got.Items) != 1 || got.Items[0].URL != nipicTestPreview || session.PageURL() != nipicTestPostURL {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	resp, err := session.Getter().Get(ctx, got.Items[0].URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if calls != 1 {
		t.Fatalf("preview requests=%d", calls)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestNipicRenderedSearch(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	const target = "https://soso.nipic.com/?q=snow"
	body := `<ul id="img-list-outer"><li class="new-search-works-item"><a class="search-works-thumb" href="` + nipicTestPostURL + `"></a></li>` +
		`<li class="new-search-works-item"><a class="search-works-thumb" href="` + huituTestPostURL + `"></a></li>` +
		`<li class="new-search-works-item"><a class="search-works-thumb" href="https://www.huitu.com/design/show/1/2.html"></a></li></ul>`
	session, interceptErrors := nipicTestBrowser(t, ctx, target, body)
	calls := 0
	session.getter.client.Transport = nipicRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("cross-origin search leaked credentials: %s %v", r.URL, r.Header)
		}
		var result string
		switch r.URL.String() {
		case nipicTestPostURL:
			result = nipicTestPost()
		case huituTestPostURL:
			result = huituTestPost()
		default:
			t.Errorf("unexpected work %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(result)), Request: r}, nil
	})
	got, err := session.Capture(ctx)
	if !errors.Is(err, errNipicPreview) || !strings.Contains(err.Error(), "页面类型尚未支持") || len(got.Items) != 2 || calls != 2 || session.PageURL() != target {
		t.Fatalf("capture=%+v page=%s calls=%d err=%v", got, session.PageURL(), calls, err)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestNipicDiscoverRoute(t *testing.T) {
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if raw != nipicTestPostURL {
			t.Fatalf("unexpected request %s", raw)
		}
		return tiebaTestResponse(raw, 200, nipicTestPost()), nil
	})
	got, err := Discover(context.Background(), getter, nipicTestPostURL, 1)
	if !errors.Is(err, errNipicPreview) || len(got.Items) != 1 || got.Items[0].URL != nipicTestPreview {
		t.Fatalf("discover=%+v err=%v", got, err)
	}
}
