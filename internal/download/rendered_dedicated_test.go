package download

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/xxww0098/tulai/internal/camoufox"
)

func TestDedicatedBrowserOpensBlankAndKeepsSession(t *testing.T) {
	runtimeDir := os.Getenv("TULAI_CAMOUFOX_TEST_DATA")
	if runtimeDir == "" {
		t.Skip("set TULAI_CAMOUFOX_TEST_DATA to an isolated Camoufox runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
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

	session, err := openRendered(ctx, filepath.Join(t.TempDir(), "profile"), "", "", true, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
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
	if err := session.run(ctx, camoufox.Navigate(targetURL+"/login")); err != nil {
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
	if err := session.run(ctx, camoufox.Navigate(other.URL)); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Capture(ctx); err == nil {
		t.Fatal("离开目标站点后不应采集")
	}
}

func TestDedicatedFingerprintBrowserKeepsLoginAndDownloads(t *testing.T) {
	runtimeDir := os.Getenv("TULAI_CAMOUFOX_TEST_DATA")
	if runtimeDir == "" {
		t.Skip("set TULAI_CAMOUFOX_TEST_DATA to an isolated Camoufox runtime")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
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
	accountProfile := filepath.Join(root, "BrowserData", "Camoufox", "account-1")
	first, err := openRendered(ctx, profile, server.URL+"/login", "", true, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
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
	if _, err := os.Stat(filepath.Join(profile, ".tulai-identity.json")); err != nil {
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

	account, err := openRendered(ctx, accountProfile, server.URL+"/gallery", "", true, runtimeDir)
	if err != nil {
		t.Fatal(err)
	}
	defer account.Close()
	var separate string
	if err := account.run(ctx, chromedp.Evaluate(`document.cookie + '|' + (localStorage.getItem('download-test') || '')`, &separate)); err != nil {
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

	reopened, err := openRendered(ctx, profile, server.URL+"/gallery", "", true, runtimeDir)
	if err != nil {
		t.Fatalf("重新打开下载环境: %v", err)
	}
	defer reopened.Close()
	var reopenedFingerprint, savedState string
	if err := reopened.run(ctx, chromedp.Evaluate(fingerprintScript, &reopenedFingerprint),
		chromedp.Evaluate(`document.cookie + '|' + (localStorage.getItem('download-test') || '')`, &savedState)); err != nil {
		t.Fatal(err)
	}
	if reopenedFingerprint != fingerprint || !strings.Contains(savedState, "download_session=synthetic") || !strings.HasSuffix(savedState, "|saved") {
		t.Fatalf("重启后指纹或登录状态发生变化: fingerprint match=%v state=%q", reopenedFingerprint == fingerprint, savedState)
	}
	if err := reopened.run(ctx, chromedp.ActionFunc(func(ec context.Context) error { return cdppage.Close().Do(ec) })); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reopened.Done():
	case <-ctx.Done():
		t.Fatal("手动关闭页面后没有通知会话结束")
	}
	if reopened.Alive() {
		t.Fatal("手动关闭页面后会话仍显示运行中")
	}
}
