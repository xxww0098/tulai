package download

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xxww0098/tulai/internal/camoufox"
	"io"
	"net/http"
	"net/http/httptest"
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
	"golang.org/x/net/html"
)

const dianpingTestPage = "https://www.dianping.com/shop/Ab123/photos"

func dianpingFixture(shop string, page int, ids []int, next string, urls []string) string {
	var records struct {
		Images []map[string]any `json:"img"`
	}
	var entries strings.Builder
	for i, id := range ids {
		records.Images = append(records.Images, map[string]any{"picId": id, "full": urls[i]})
		fmt.Fprintf(&entries, `<li class="J_list"><div class="img"><a class="J_entry" href="/photos/%d"><img src="/thumb"></a></div><div class="name"><a class="J_entry" href="/photos/%d" title="第 %d 张">第 %d 张</a></div></li>`, id, id, id, id)
	}
	data, _ := json.Marshal(records)
	return fmt.Sprintf(`<html><head><title>门店图片</title></head><body><div class="page-gallery"><ul>%s</ul></div>%s<script>DP.data({shopID: %q}); DP.data({'page': %d, 'records': '%s'});</script></body></html>`, entries.String(), next, shop, page, data)
}
func dianpingTestResponse(raw, body string) *Response {
	return &Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, FinalURL: raw, Body: io.NopCloser(strings.NewReader(body))}
}
func dianpingDoc(t *testing.T, source string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestDianpingPageReferences(t *testing.T) {
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{dianpingTestPage, true},
		{"https://www.dianping.com/shop/Ab123", true},
		{"https://m.dianping.com/shopshare/Ab123", true},
		{"https://www.dianping.com/shop/Ab123/photos/tag-%E8%8F%9C", true},
		{"https://www.dianping.com/shop/Ab123/officialphotos", true},
		{"https://www.dianping.com/shop/Ab123/review_all", false},
		{"https://www.dianping.com/shop/Ab123/photos/../../other", false},
		{"https://www.dianping.com/shop/Ab123%2Fother/photos", false},
		{"https://evil.test/shop/Ab123/photos", false},
		{"https://dianping.com.evil.test/shop/Ab123/photos", false},
		{"https://user@www.dianping.com/shop/Ab123/photos", false},
		{"https://www.dianping.com:8443/shop/Ab123/photos", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isDianpingPage(u); got != tc.ok {
			t.Errorf("%s matched=%v", tc.raw, got)
		}
	}
	for _, raw := range []string{"https://www.dianping.com/shop/Ab123", "https://m.dianping.com/shopshare/Ab123"} {
		u, _ := url.Parse(raw)
		if got := dianpingPhotoPage(u).String(); got != dianpingTestPage {
			t.Errorf("normalized=%s", got)
		}
	}
}

func TestDianpingPaginationAndIdentity(t *testing.T) {
	image1 := "https://qcloud.dpfile.com/pc/image1.jpg"
	image2 := "https://img.meituan.net/merchant/image2.jpg"
	first := dianpingFixture("Ab123", 1, []int{111}, `<a class="NextPage" href="?pg=2" data-pg="2">下一页</a>`, []string{image1})
	second := dianpingFixture("Ab123", 2, []int{222}, "", []string{image2})
	var requests []string
	getter := GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		requests = append(requests, raw)
		switch raw {
		case dianpingTestPage:
			return dianpingTestResponse(raw, first), nil
		case dianpingTestPage + "?pg=2":
			return dianpingTestResponse(raw, second), nil
		default:
			t.Fatalf("unexpected request %s", raw)
			return nil, nil
		}
	})
	root, _ := url.Parse(dianpingTestPage)
	got, err := discoverDianping(context.Background(), getter, root)
	if err != nil || len(requests) != 2 || len(got.Items) != 2 || got.Items[0].URL != image1 || got.Items[1].URL != image2 {
		t.Fatalf("requests=%v items=%+v err=%v", requests, got.Items, err)
	}
	for _, item := range got.Items {
		if !item.PublicOnly || !item.RequireImage || item.Metadata["dianping_shop_id"] != "Ab123" || item.Headers["Referer"] != "https://www.dianping.com/" {
			t.Fatalf("unsafe item %+v", item)
		}
	}
	if got.Items[0].Metadata["dianping_title"] != "第 111 张" {
		t.Fatalf("title=%q", got.Items[0].Metadata["dianping_title"])
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		if strings.Contains(raw, "pg=2") {
			return nil, errors.New("unavailable")
		}
		return dianpingTestResponse(raw, first), nil
	})
	got, err = discoverDianping(context.Background(), getter, root)
	if err == nil || len(got.Items) != 1 || got.Items[0].URL != image1 {
		t.Fatalf("partial=%+v err=%v", got.Items, err)
	}
}

func TestDianpingImageAndNavigationBoundaries(t *testing.T) {
	good := "https://qcloud.dpfile.com/pc/a.jpg"
	for _, raw := range []string{"https://qcloud.dpfile.com.evil.test/pc/a.jpg", "http://localhost/a.jpg", "https://user@qcloud.dpfile.com/pc/a.jpg", "https://qcloud.dpfile.com:8443/pc/a.jpg", "https://qcloud.dpfile.com/pc/a.jpg?token=secret", "https://qcloud.dpfile.com/pc/../a.jpg"} {
		if _, ok := dianpingImage(raw); ok {
			t.Errorf("unsafe image accepted %q", raw)
		}
	}
	page, _ := url.Parse(dianpingTestPage)
	for _, tc := range []struct {
		name, body string
		count      int
	}{
		{"wrong shop", dianpingFixture("Other", 1, []int{111}, "", []string{good}), 0},
		{"wrong page", dianpingFixture("Ab123", 2, []int{111}, "", []string{good}), 0},
		{"wrong photo", strings.Replace(dianpingFixture("Ab123", 1, []int{111}, "", []string{good}), `/photos/111`, `/photos/222`, -1), 0},
		{"untrusted image", dianpingFixture("Ab123", 1, []int{111, 222}, "", []string{good, "https://evil.test/a.jpg"}), 1},
		{"cross-origin next", dianpingFixture("Ab123", 1, []int{111}, `<a class="NextPage" href="https://evil.test/photos?pg=2" data-pg="2">next</a>`, []string{good}), 1},
		{"different album next", dianpingFixture("Ab123", 1, []int{111}, `<a class="NextPage" href="/shop/Other/photos?pg=2" data-pg="2">next</a>`, []string{good}), 1},
		{"skipped page", dianpingFixture("Ab123", 1, []int{111}, `<a class="NextPage" href="?pg=3" data-pg="3">next</a>`, []string{good}), 1},
		{"unexpected parameters", dianpingFixture("Ab123", 1, []int{111}, `<a class="NextPage" href="?pg=2&amp;token=secret" data-pg="2">next</a>`, []string{good}), 1},
		{"verification page", `<html><title>安全验证</title></html>`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := dianpingParse(context.Background(), page, dianpingDoc(t, tc.body))
			if err == nil || len(parsed.result.Items) != tc.count || parsed.next != nil {
				t.Fatalf("items=%+v next=%v err=%v", parsed.result.Items, parsed.next, err)
			}
		})
	}
}

func TestDianpingCancellationAndRedirect(t *testing.T) {
	root, _ := url.Parse(dianpingTestPage)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("canceled request sent")
		return nil, nil
	})
	if _, err := discoverDianping(ctx, getter, root); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	getter = GetterFunc(func(_ context.Context, raw string, _ func(*http.Request)) (*Response, error) {
		r := dianpingTestResponse(raw, `<html></html>`)
		r.FinalURL = "https://verify.meituan.com/v2/app/general_page"
		return r, nil
	})
	if got, err := discoverDianping(context.Background(), getter, root); err == nil || len(got.Items) != 0 {
		t.Fatalf("redirect items=%v err=%v", got.Items, err)
	}
}

// This optional check reads the directly acquired public page without any
// network or credentials; the fixture remains outside the repository.
func TestDianpingSavedPublicPage(t *testing.T) {
	file := os.Getenv("TULAI_DIANPING_FIXTURE")
	if file == "" {
		t.Skip("set TULAI_DIANPING_FIXTURE to the saved public page")
	}
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	doc := dianpingDoc(t, string(b))
	page, _ := url.Parse("https://www.dianping.com/shop/l5bXe7nGzIs26SHp/photos")
	got, err := dianpingParse(context.Background(), page, doc)
	if err != nil || len(got.result.Items) == 0 || got.next == nil {
		t.Fatalf("public=%d next=%v err=%v", len(got.result.Items), got.next, err)
	}
	t.Logf("saved public first page: %d images; next page: %s", len(got.result.Items), got.next.Path)
}

func TestDianpingPublicLive(t *testing.T) {
	if os.Getenv("TULAI_DIANPING_LIVE") != "1" {
		t.Skip("set TULAI_DIANPING_LIVE=1 to check a public gallery")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	root := "https://www.dianping.com/shop/k8NVchwjOR532uOe/photos"
	client := NewHTTPGetter("")
	calls := 0
	limited := GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*Response, error) {
		calls++
		if calls > 2 {
			return nil, errors.New("live test page limit")
		}
		return client.Get(ctx, raw, prepare)
	})
	got, err := Discover(ctx, limited, root, 1)
	if len(got.Items) == 0 {
		t.Skipf("public page unavailable, validation or access restriction: %v", err)
	}
	if calls != 3 || len(got.Items) < 16 || err == nil {
		t.Fatalf("pages=%d items=%d err=%v", calls, len(got.Items), err)
	}
	item := got.Items[0]
	item.Filename = "dianping-public"
	dir := t.TempDir()
	if e := New(Options{Getter: client}).Download(ctx, Job{Directory: dir}, item, nil); e != nil {
		t.Fatalf("found %d images, but image download failed: %v", len(got.Items), e)
	}
	entries, e := os.ReadDir(dir)
	if e != nil || len(entries) != 1 {
		t.Fatalf("download entries=%v err=%v", entries, e)
	}
	info, e := os.Stat(filepath.Join(dir, entries[0].Name()))
	if e != nil || info.Size() == 0 {
		t.Fatalf("download size err=%v", e)
	}
	t.Logf("two public pages: %d images; downloaded %d bytes; intentionally stopped before page 3", len(got.Items), info.Size())
}

type dianpingTestTransport func(*http.Request) (*http.Response, error)

func (f dianpingTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDianpingRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for an isolated browser")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "<html></html>") }))
	defer server.Close()
	session, err := OpenRendered(ctx, filepath.Join(t.TempDir(), "profile"), server.URL, "", true)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	image1 := "https://qcloud.dpfile.com/pc/image1.jpg"
	image2 := "https://img.meituan.net/merchant/image2.jpg"
	first := dianpingFixture("Ab123", 1, []int{111}, `<a class="NextPage" href="?pg=2" data-pg="2">下一页</a>`, []string{image1})
	second := dianpingFixture("Ab123", 2, []int{222}, "", []string{image2})
	var pageRequests atomic.Int32
	var syntheticCookie atomic.Bool
	session.getter.client.Transport = dianpingTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != dianpingTestPage+"?pg=2" {
			return nil, errors.New("unexpected browser getter destination")
		}
		pageRequests.Add(1)
		cookie, err := r.Cookie("synthetic_dianping")
		if err == nil && cookie.Value == "authorized" {
			syntheticCookie.Store(true)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(second)), Request: r}, nil
	})
	responses := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && paused.Request.URL == dianpingTestPage {
				action = fetch.FulfillRequest(paused.RequestID, http.StatusOK).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(first)))
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
	start, _ := url.Parse(dianpingTestPage)
	session.start = start
	if err := session.run(ctx, camoufox.Navigate(dianpingTestPage), chromedp.Evaluate(`document.cookie="synthetic_dianping=authorized; Path=/; Secure"`, nil)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	select {
	case e := <-responses:
		t.Fatalf("intercept: %v", e)
	default:
	}
	if err != nil || len(got.Items) != 2 || got.Items[0].URL != image1 || got.Items[1].URL != image2 || pageRequests.Load() != 1 || !syntheticCookie.Load() {
		t.Fatalf("capture items=%+v getter calls=%d synthetic cookie forwarded=%v err=%v", got.Items, pageRequests.Load(), syntheticCookie.Load(), err)
	}
}
