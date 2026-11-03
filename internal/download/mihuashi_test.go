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

const mihuashiTestPage = "https://www.mihuashi.com/profiles/821980"
const mihuashiTestOtherPage = "https://www.mihuashi.com/profiles/821981"
const mihuashiTestHTML = `<html><head><title>服务器错误 - 米画师</title></head><body><img src="https://image-assets.mihuashi.com/misc/header-bar-logo.png"><img src="https://js-assets.mihuashi.com/error-500.png"></body></html>`

func TestMihuashiProtectedRoutes(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{mihuashiTestPage, true}, {"http://mihuashi.com/profiles/7600/", true},
		{"https://www.mihuashi.com/profiles/画师Alice", true}, {"https://www.mihuashi.com/profiles/Artist_1", true},
		{"https://www.mihuashi.com/artworks/25322299", true}, {"https://www.mihuashi.com/users/1234", true},
		{"https://www.mihuashi.com/profiles/0", false}, {"https://www.mihuashi.com/profiles/../7600", false},
		{"https://www.mihuashi.com/users/username", false}, {"https://www.mihuashi.com/artworks", false},
		{"https://www.mihuashi.com.evil.test/profiles/7600", false},
		{"https://user@www.mihuashi.com/profiles/7600", false}, {"https://www.mihuashi.com:8443/profiles/7600", false},
	} {
		u, err := url.Parse(tc.raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := isMihuashiPage(u); got != tc.want {
			t.Errorf("route %s = %t", tc.raw, got)
		}
	}
}

func TestMihuashiDoesNotScan(t *testing.T) {
	page, _ := url.Parse(mihuashiTestPage)
	getter := GetterFunc(func(context.Context, string, func(*http.Request)) (*Response, error) {
		t.Fatal("protected route must not scan or access the signed API")
		return nil, nil
	})
	got, err := discoverMihuashi(context.Background(), getter, page)
	if err == nil || !strings.Contains(err.Error(), "暂无法确认") || !got.IsPage || len(got.Items) != 0 {
		t.Fatalf("protected result=%+v err=%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discoverMihuashi(ctx, getter, page); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
	for _, tc := range []struct{ body, want string }{
		{mihuashiTestHTML, "服务器错误"},
		{`<title>安全验证 - 米画师</title><img src="https://image-assets.mihuashi.com/logo.png">`, "安全验证"},
		{`<title>登录 - 米画师</title><img src="https://image-assets.mihuashi.com/logo.png">`, "登录"},
		{`<title>作品详情 - 米画师</title><img src="https://image-assets.mihuashi.com/logo.png">`, "暂无法确认"},
	} {
		doc, err := html.Parse(strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		got, err := mihuashiPage(context.Background(), doc, page)
		if err == nil || !strings.Contains(err.Error(), tc.want) || len(got.Items) != 0 {
			t.Fatalf("page result=%+v err=%v", got, err)
		}
	}
}

func TestMihuashiRenderedCapture(t *testing.T) {
	if os.Getenv("TULAI_RENDERED_E2E") != "1" {
		t.Skip("set TULAI_RENDERED_E2E=1 for isolated Camoufox integration")
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
	interceptErrors := make(chan error, 8)
	camoufox.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action camoufox.Action
			if paused.ResourceType == network.ResourceTypeDocument && (paused.Request.URL == mihuashiTestPage || paused.Request.URL == mihuashiTestOtherPage) {
				action = fetch.FulfillRequest(paused.RequestID, 200).WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).WithBody(base64.StdEncoding.EncodeToString([]byte(mihuashiTestHTML)))
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
	session.start, _ = url.Parse(mihuashiTestPage)
	if err := session.run(ctx, camoufox.Navigate(mihuashiTestPage)); err != nil {
		t.Fatal(err)
	}
	got, err := session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "服务器错误") || len(got.Items) != 0 || session.PageURL() != mihuashiTestPage {
		t.Fatalf("capture=%+v page=%s err=%v", got, session.PageURL(), err)
	}
	if err := session.run(ctx, camoufox.Navigate(mihuashiTestOtherPage)); err != nil {
		t.Fatal(err)
	}
	got, err = session.Capture(ctx)
	if err == nil || !strings.Contains(err.Error(), "已跳转") || len(got.Items) != 0 {
		t.Fatalf("other author capture=%+v err=%v", got, err)
	}
	select {
	case err := <-interceptErrors:
		t.Fatalf("intercept failed: %v", err)
	default:
	}
}

func TestMihuashiSavedPublicError(t *testing.T) {
	path := os.Getenv("TULAI_MIHUASHI_SAMPLE")
	if path == "" {
		t.Skip("set TULAI_MIHUASHI_SAMPLE to a saved public error page")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc, err := html.Parse(file)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := url.Parse(mihuashiTestPage)
	got, err := mihuashiPage(context.Background(), doc, page)
	if err == nil || !strings.Contains(err.Error(), "服务器错误") || len(got.Items) != 0 {
		t.Fatalf("public error page items=%d err=%v", len(got.Items), err)
	}
}

func TestMihuashiAPIIdentityAndSources(t *testing.T) {
	page, _ := url.Parse("https://www.mihuashi.com/profiles/821980")
	for _, tc := range []struct {
		raw, route, id, kind, owner string
		page                        int
	}{
		{"https://www.mihuashi.com/api/v1/users/821980?by=name", "profiles", "821980", "user", "821980", 0},
		{"https://www.mihuashi.com/api/v1/users/%E7%94%BB%E5%B8%88Alice?by=name", "profiles", "画师Alice", "user", "画师Alice", 0},
		{"https://www.mihuashi.com/api/v1/users/821980?by=id", "profiles", "821980", "", "", 0},
		{"https://www.mihuashi.com/api/v1/users/123?by=id", "users", "123", "user", "123", 0},
		{"https://www.mihuashi.com/api/v1/users/123?by=name", "users", "123", "", "", 0},
		{"https://www.mihuashi.com/api/v1/users/123/artworks?page=2&per=12", "profiles", "821980", "list", "123", 2},
		{"https://www.mihuashi.com/api/v1/users/123/artworks?page=2&filter_tag_id=44", "profiles", "821980", "", "", 0},
		{"https://www.mihuashi.com/api/v1/users/123/artworks?page=2&page=3", "profiles", "821980", "", "", 0},
		{"https://www.mihuashi.com/api/v1/users/123/artworks", "profiles", "821980", "", "", 0},
		{"https://evil@www.mihuashi.com/api/v1/users/821980?by=name", "profiles", "821980", "", "", 0},
		{"https://www.mihuashi.com.evil.test/api/v1/users/821980?by=name", "profiles", "821980", "", "", 0},
		{"https://www.mihuashi.com:444/api/v1/users/821980?by=name", "profiles", "821980", "", "", 0},
		{"https://www.mihuashi.com/api/v1/artworks/44", "artworks", "44", "work", "44", 0},
		{"https://www.mihuashi.com/api/v1/artworks/45", "artworks", "44", "", "", 0},
	} {
		kind, owner, pageNum := mihuashiAPI(tc.raw, page, tc.route, tc.id)
		if kind != tc.kind || owner != tc.owner || pageNum != tc.page {
			t.Errorf("request %s = %s %s %d", tc.raw, kind, owner, pageNum)
		}
	}
	for _, tc := range []struct {
		raw string
		ok  bool
	}{
		{"https://image-assets.mihuashi.com/permanent/123%7C-2022/05/image.jpg", true},
		{"https://image-assets.mihuashi.com/pfop/permanent/123/image.png?key=synthetic", true},
		{"https://image-assets.mihuashi.com/misc/header-bar-logo.png", false},
		{"http://image-assets.mihuashi.com/permanent/123/image.jpg", false},
		{"https://user@image-assets.mihuashi.com/permanent/123/image.jpg", false},
		{"https://image-assets.mihuashi.com:8443/permanent/123/image.jpg", false},
		{"https://image-assets.mihuashi.com.evil.test/permanent/123/image.jpg", false},
		{"https://image-assets.mihuashi.com/permanent/%2e%2e/image.jpg", false},
		{"https://image-assets.mihuashi.com/permanent/123%2fimage.jpg", false},
	} {
		_, ok := mihuashiAsset(tc.raw)
		if ok != tc.ok {
			t.Errorf("asset %s = %v", tc.raw, ok)
		}
	}
	for _, tc := range []struct{ body, route, id, want string }{
		{`{"user":{"id":123,"name":"821980"}}`, "profiles", "821980", "123"},
		{`{"user":{"id":123,"name":"画师Alice"}}`, "profiles", "画师Alice", "123"},
		{`{"user":{"id":123,"name":"821981"}}`, "profiles", "821980", ""},
		{`{"user":{"id":123,"name":"alice"}}`, "users", "123", "123"},
		{`{"user":{"id":124,"name":"alice"}}`, "users", "123", ""},
	} {
		got, err := mihuashiUser([]byte(tc.body), tc.route, tc.id)
		if got != tc.want || ((err == nil) != (tc.want != "")) {
			t.Errorf("identity = %q, %v", got, err)
		}
	}
}

func TestMihuashiWorksAndPagination(t *testing.T) {
	work := `{"artwork":{"id":42,"artwork_type":"image","url":"https://image-assets.mihuashi.com/permanent/123/image.jpg"}}`
	got, err := mihuashiWork([]byte(work), "42")
	if err != nil || len(got.Items) != 1 || got.Items[0].Metadata["mihuashi_artwork_id"] != "42" || !got.Items[0].PublicOnly || !got.Items[0].RequireImage {
		t.Fatalf("work=%+v err=%v", got, err)
	}
	for _, body := range []string{
		work,
		strings.Replace(work, `"id":42`, `"id":43`, 1),
		strings.Replace(work, `image-assets.mihuashi.com`, `attacker.invalid`, 1),
		strings.Replace(work, `"image"`, `"live2d"`, 1),
		`{"error":"signature invalid"}`,
	} {
		want := "42"
		if body == work {
			want = "43"
		}
		if result, err := mihuashiWork([]byte(body), want); err == nil || len(result.Items) != 0 {
			t.Errorf("invalid work accepted: %+v %v", result, err)
		}
	}
	body := `{"total_pages":2,"artworks":[{"id":42,"artwork_type":"image","url":"https://image-assets.mihuashi.com/permanent/123/image.jpg"},{"id":43,"artwork_type":"live2d","url":"https://image-assets.mihuashi.com/permanent/123/live.jpg"},{"id":"invalid","url":"https://image-assets.mihuashi.com/permanent/123/invalid.jpg"}]}`
	items, total, partial, err := mihuashiList(context.Background(), []byte(body), 1)
	if err != nil || !partial || total != 2 || len(items) != 1 || items[0].Metadata["mihuashi_artwork_id"] != "42" {
		t.Fatalf("list=%+v total=%d partial=%v err=%v", items, total, partial, err)
	}
	items, total, partial, err = mihuashiList(context.Background(), []byte(body), 3)
	if err != nil || !partial || total != -1 || len(items) != 1 {
		t.Fatalf("inconsistent paging lost results: items=%d total=%d partial=%v err=%v", len(items), total, partial, err)
	}
	missingTotal := strings.Replace(body, `"total_pages":2,`, "", 1)
	items, total, partial, err = mihuashiList(context.Background(), []byte(missingTotal), 1)
	if err != nil || !partial || total != -1 || len(items) != 1 {
		t.Fatalf("missing total lost results: items=%d total=%d partial=%v err=%v", len(items), total, partial, err)
	}
	if _, _, _, err := mihuashiList(context.Background(), make([]byte, mihuashiMaxBody+1), 1); err == nil {
		t.Fatal("oversized response accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := mihuashiList(ctx, []byte(body), 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel=%v", err)
	}
}
