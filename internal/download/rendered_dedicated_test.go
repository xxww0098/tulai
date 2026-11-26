package download

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/xxww0098/tulai/internal/fingerprint"
)

func TestRenderedSiteAccessRecovery(t *testing.T) {
	runtimeDir := dedicatedFixtureData(t)
	session, err := openDedicatedFixture(t, runtimeDir, filepath.Join(t.TempDir(), "profile"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()

	zhihu := "https://zhuanlan.zhihu.com/p/123"
	behance := "https://www.behance.net/gallery/123/Work"
	pages := map[string]string{
		zhihu:   `<div class="Post-RichText"><img data-original="https://pic1.zhimg.com/article.png"></div>`,
		behance: behanceTestHTML("project", behanceTestProject("123")),
	}
	picture := filterPNG(t, 13, 9)
	session.getter.client.Transport = flickrTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Cookie") != "" || (req.URL.String() != "https://pic1.zhimg.com/article.png" &&
			req.URL.String() != "https://mir-s3-cdn-cf.behance.net/project_modules/source/one.jpg") {
			return nil, errors.New("unexpected transfer after page capture")
		}
		return renderedFixturePNGResponse(req, picture), nil
	})
	var status atomic.Int32
	var acceptAfterRefresh atomic.Bool
	fingerprint.ListenTarget(session.tab, func(event any) {
		paused, ok := event.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		go func() {
			var action fingerprint.Action = fetch.FailRequest(paused.RequestID, network.ErrorReasonBlockedByClient)
			if body, ok := pages[paused.Request.URL]; ok && paused.ResourceType == network.ResourceTypeDocument {
				code := status.Load()
				if code == http.StatusForbidden && acceptAfterRefresh.CompareAndSwap(true, false) {
					status.Store(http.StatusOK)
					body = `<meta http-equiv="refresh" content="1">`
				}
				action = fetch.FulfillRequest(paused.RequestID, int64(code)).
					WithResponseHeaders([]*fetch.HeaderEntry{{Name: "Content-Type", Value: "text/html; charset=utf-8"}}).
					WithBody(base64.StdEncoding.EncodeToString([]byte(body)))
			}
			if err := fingerprint.Run(session.tab, action); err != nil && session.tab.Err() == nil {
				t.Logf("fixture interception: %v", err)
			}
		}()
	})
	if err := session.run(ctx, fetch.Enable()); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{zhihu, behance} {
		status.Store(http.StatusForbidden)
		if err := session.OpenTarget(ctx, target); err != nil {
			t.Fatal(err)
		}
		blocked, err := session.Capture(ctx)
		var accessError *HTTPStatusError
		if !errors.As(err, &accessError) || accessError.StatusCode != http.StatusForbidden || len(blocked.Items) != 0 {
			t.Fatalf("%s: 403 capture returned %d images and %v", target, len(blocked.Items), err)
		}
		acceptAfterRefresh.Store(true)
		if err := session.OpenTarget(ctx, target); err != nil {
			t.Fatal(err)
		}
		found, err := session.Capture(ctx)
		if err != nil || len(found.Items) == 0 {
			t.Fatalf("%s: authorized page returned %d images and %v", target, len(found.Items), err)
		}
		assertSavedRenderedPNG(t, ctx, session, found.Items[0], picture)
	}
}

func TestDedicatedBrowserBindsNavigatedTab(t *testing.T) {
	runtimeDir := dedicatedFixtureData(t)
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/image.png" {
			cookie, err := r.Cookie("download_session")
			if err != nil || cookie.Value != "fixture" {
				http.Error(w, "sign in", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageData.Bytes())
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "download_session", Value: "fixture", Path: "/", HttpOnly: true})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<title>Home</title>`)
	}))
	defer server.Close()

	session, err := openDedicatedFixture(t, runtimeDir, filepath.Join(t.TempDir(), "profile"), server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	tab, closeTab, err := fingerprint.NewPage(ctx, session.tab, false)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTab()
	const route = "/search/pins/?q=Wallpaper%20aesthetic"
	if err := runRendered(ctx, tab, fingerprint.Navigate(server.URL+"/"), chromedp.Evaluate(
		`history.pushState(null, "", "`+route+`"); document.title = "Search results"; document.body.innerHTML = '<img src="/image.png">';`, nil)); err != nil {
		t.Fatal(err)
	}
	target := server.URL + route
	if err := session.BindOpenPage(ctx, server.URL+"/search/pins/?q=another"); err == nil {
		t.Fatal("不应绑定到不同搜索")
	}
	if err := session.BindCurrent(ctx, target, ""); err == nil {
		t.Fatal("下载助手请求必须携带有效的标记")
	}
	if err := session.BindOpenPage(ctx, target); err != nil {
		t.Fatal(err)
	}
	found, err := session.Capture(ctx)
	if err != nil || found.Title != "Search results" || len(found.Items) != 1 || found.Items[0].URL != server.URL+"/image.png" {
		t.Fatalf("手动跳转的标签页采集失败或被刷新: %+v, %v", found, err)
	}
	item := found.Items[0]
	item.Filename = "image.png"
	dir := t.TempDir()
	if err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("绑定后无法使用浏览器登录态下载: %v", err)
	}
	if saved, err := os.ReadFile(filepath.Join(dir, item.Filename)); err != nil || !bytes.Equal(saved, imageData.Bytes()) {
		t.Fatalf("保存的图片不匹配: %v", err)
	}
	if err := runRendered(ctx, tab, chromedp.Evaluate(`history.pushState(null, "", "?q=another")`, nil)); err != nil {
		t.Fatal(err)
	}
	if found, err := session.Capture(ctx); err == nil || len(found.Items) != 0 {
		t.Fatalf("标签页切换到不同搜索后仍被采集: %+v, %v", found, err)
	}
}

func TestDedicatedBrowserOpensBlankAndKeepsSession(t *testing.T) {
	runtimeDir := dedicatedFixtureData(t)
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/image.png" {
			cookie, err := r.Cookie("session")
			if err != nil || cookie.Value != "synthetic" {
				http.Error(w, "sign in", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageData.Bytes())
			return
		}
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "synthetic", Path: "/"})
		}
		_, _ = io.WriteString(w, `<title>Gallery</title><img src="/image.png">`)
	}))
	defer server.Close()
	targetURL := strings.Replace(server.URL, "127.0.0.1", "localhost", 1)

	session, err := openDedicatedFixture(t, runtimeDir, filepath.Join(t.TempDir(), "profile"), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if !session.Alive() {
		t.Fatal("空白专用浏览器未运行")
	}
	if _, err := session.Capture(ctx); err == nil {
		t.Fatal("未指定目标网址时不应采集")
	}
	for _, target := range []string{"file:///tmp/private", "https://user:pass@example.test"} {
		if err := session.OpenTarget(ctx, target); err == nil {
			t.Fatalf("无效目标被接受: %s", target)
		}
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := session.OpenTarget(canceled, targetURL); err == nil {
		t.Fatal("取消的目标导航未返回错误")
	}
	if err := session.run(ctx, fingerprint.Navigate(targetURL+"/login")); err != nil {
		t.Fatal(err)
	}
	if err := session.OpenTarget(ctx, targetURL+"/gallery"); err != nil {
		t.Fatal(err)
	}
	var cookie string
	if err := session.run(ctx, chromedp.Evaluate(`document.cookie`, &cookie)); err != nil || !strings.Contains(cookie, "session=synthetic") {
		t.Fatalf("打开目标网址后登录态丢失: %q, %v", cookie, err)
	}
	page, err := session.Capture(ctx)
	if err != nil || len(page.Items) != 1 || page.Items[0].URL != targetURL+"/image.png" {
		t.Fatalf("采集目标网页: %+v %v", page.Items, err)
	}
	item := page.Items[0]
	item.Filename = "image.png"
	dir := t.TempDir()
	if err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: dir}, item, nil); err != nil {
		t.Fatalf("使用登录态下载图片: %v", err)
	}
	if saved, err := os.ReadFile(filepath.Join(dir, "image.png")); err != nil || !bytes.Equal(saved, imageData.Bytes()) {
		t.Fatalf("图片保存不正确: %v", err)
	}
	if err := session.OpenTarget(ctx, targetURL+"/other"); err != nil {
		t.Fatalf("在同一浏览器切换目标: %v", err)
	}
	if _, err := session.Getter().Get(ctx, targetURL+"/image.png", nil); err == nil {
		t.Fatal("切换目标后不应使用旧页面的下载权限")
	}
	if err := session.run(ctx, chromedp.Evaluate(`document.cookie`, &cookie)); err != nil || !strings.Contains(cookie, "session=synthetic") {
		t.Fatalf("切换目标后登录态丢失: %q, %v", cookie, err)
	}
	if page, err := session.Capture(ctx); err != nil || len(page.Items) != 1 {
		t.Fatalf("切换目标后无法采集: %+v %v", page.Items, err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "other") }))
	defer other.Close()
	if err := session.run(ctx, fingerprint.Navigate(other.URL)); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Capture(ctx); err == nil {
		t.Fatal("离开目标站点后不应采集")
	}
}

func TestDedicatedBrowserDownloadsImageURL(t *testing.T) {
	runtimeDir := dedicatedFixtureData(t)
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 24, 16))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/asset" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageData.Bytes())
	}))
	defer server.Close()

	session, err := openDedicatedFixture(t, runtimeDir, t.TempDir(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	target := server.URL + "/asset"
	if err := session.OpenTarget(ctx, target); err != nil {
		t.Fatal(err)
	}
	found, err := session.Capture(ctx)
	if err != nil || len(found.Items) != 1 || found.Items[0].URL != target {
		t.Fatalf("direct image capture: %+v, %v", found.Items, err)
	}
	destination := t.TempDir()
	item := found.Items[0]
	item.Filename = "download.png"
	if err := New(Options{Getter: session.Getter()}).Download(ctx, Job{Directory: destination}, item, nil); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(destination, item.Filename))
	if err != nil || !bytes.Equal(saved, imageData.Bytes()) {
		t.Fatalf("direct image download differs from source: %v", err)
	}
}

func TestDedicatedFingerprintBrowserKeepsLoginAndDownloads(t *testing.T) {
	runtimeDir := dedicatedFixtureData(t)
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 3, 3))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/image.png" {
			cookie, err := r.Cookie("download_session")
			if err != nil || cookie.Value != "synthetic" {
				http.Error(w, "sign in", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageData.Bytes())
			return
		}
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "download_session", Value: "synthetic", Path: "/", MaxAge: 3600})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<title>下载测试</title><img src="/image.png">`)
	}))
	defer server.Close()

	root := t.TempDir()
	profile := filepath.Join(root, "config", "DownloadBrowser")
	accountProfile := filepath.Join(root, "BrowserData", "Fingerprint", "account-1")
	first, err := openDedicatedFixture(t, runtimeDir, profile, server.URL+"/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	if !first.Alive() {
		t.Fatal("新打开的专用浏览器未运行")
	}
	if second, err := openRendered(ctx, profile, server.URL+"/gallery", "", true, runtimeDir); err == nil {
		second.Close()
		t.Fatal("同一下载资料目录被重复打开")
	}
	var fingerprint string
	const fingerprintScript = `JSON.stringify({ua:navigator.userAgent, platform:navigator.platform, cores:navigator.hardwareConcurrency, width:screen.width, height:screen.height})`
	if err := first.run(ctx, chromedp.Evaluate(fingerprintScript, &fingerprint), chromedp.Evaluate(`localStorage.setItem('download-test','saved')`, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(profile, ".tulai-fingerprint.json")); err != nil {
		t.Fatalf("专用指纹未保存在资料目录: %v", err)
	}
	page, err := first.Capture(ctx)
	if err != nil || len(page.Items) != 1 || page.Items[0].URL != server.URL+"/image.png" {
		t.Fatalf("采集页面图片: %+v %v", page.Items, err)
	}
	item := page.Items[0]
	item.Filename = "image.png"
	destination := t.TempDir()
	if err := New(Options{Getter: first.Getter()}).Download(ctx, Job{Directory: destination}, item, nil); err != nil {
		t.Fatalf("使用专用浏览器登录态下载图片: %v", err)
	}
	if saved, err := os.ReadFile(filepath.Join(destination, "image.png")); err != nil || !bytes.Equal(saved, imageData.Bytes()) {
		t.Fatalf("图片保存不正确: %v", err)
	}

	cancel()
	account, err := openDedicatedFixture(t, runtimeDir, accountProfile, server.URL+"/gallery", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer account.Close()
	accountCtx, cancelAccount := context.WithTimeout(t.Context(), time.Minute)
	defer cancelAccount()
	var separate string
	if err := account.run(accountCtx, chromedp.Evaluate(`document.cookie + '|' + (localStorage.getItem('download-test') || '')`, &separate)); err != nil {
		t.Fatal(err)
	}
	if separate != "|" {
		t.Fatalf("账号环境读取了下载环境的登录状态: %q", separate)
	}
	first.Close()
	if first.Alive() || !account.Alive() {
		t.Fatal("关闭下载环境影响了账号环境，或未正确关闭")
	}
	select {
	case <-first.Done():
	default:
		t.Fatal("关闭后未通知会话结束")
	}

	reopened, err := openDedicatedFixture(t, runtimeDir, profile, "", nil)
	if err != nil {
		t.Fatalf("重新打开下载环境: %v", err)
	}
	defer reopened.Close()
	reopenCtx, cancelReopen := context.WithTimeout(t.Context(), time.Minute)
	defer cancelReopen()
	if err := reopened.OpenTarget(reopenCtx, server.URL+"/gallery"); err != nil {
		t.Fatalf("重开专用环境后打开网页: %v", err)
	}
	var reopenedFingerprint, savedState string
	if err := reopened.run(reopenCtx, chromedp.Evaluate(fingerprintScript, &reopenedFingerprint),
		chromedp.Evaluate(`document.cookie + '|' + (localStorage.getItem('download-test') || '')`, &savedState)); err != nil {
		t.Fatal(err)
	}
	if reopenedFingerprint != fingerprint || !strings.Contains(savedState, "download_session=synthetic") || !strings.HasSuffix(savedState, "|saved") {
		t.Fatalf("重启后指纹或登录状态发生变化: fingerprint match=%v state=%q", reopenedFingerprint == fingerprint, savedState)
	}
	if err := reopened.run(reopenCtx, chromedp.ActionFunc(func(ec context.Context) error { return cdppage.Close().Do(ec) })); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reopened.Done():
	case <-reopenCtx.Done():
		t.Fatal("手动关闭页面后没有通知会话结束")
	}
	if reopened.Alive() {
		t.Fatal("手动关闭页面后会话仍显示运行中")
	}
}
