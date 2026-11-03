package ui

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/download"
)

type fakeDownloadBrowser struct {
	result download.Discovery
	getter download.Getter
	used   bool
	closed bool
}

func (f *fakeDownloadBrowser) Capture(context.Context) (download.Discovery, error) {
	f.used = true
	return f.result, nil
}
func (f *fakeDownloadBrowser) Getter() download.Getter { return f.getter }
func (f *fakeDownloadBrowser) Alive() bool             { return !f.closed }
func (f *fakeDownloadBrowser) Close()                  { f.closed = true }

func TestDownloadUsesOpenedBrowserForRenderedPage(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/image.png" {
			t.Errorf("浏览器模式不应静态请求页面: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data.Bytes())
	}))
	defer server.Close()
	a, st := newDownloadApp(t)
	page := server.URL + "/gallery"
	fake := &fakeDownloadBrowser{
		result: download.Discovery{IsPage: true, Title: "登录后的相册", Items: []download.Item{{URL: server.URL + "/image.png"}}},
		getter: download.NewHTTPGetter(""),
	}
	a.dlDir, a.dlURLs, a.dlBrowserMode = t.TempDir(), page, true
	a.dlBrowser, a.dlBrowserURL = fake, page
	a.submitDownloadComposer()
	if !fake.used || len(a.dlTasks) != 1 || a.dlTasks[0].Phase != dlDone || !a.dlTasks[0].Rendered {
		t.Fatalf("浏览器任务 = %+v, capture=%v", a.dlTasks, fake.used)
	}
	entries, err := os.ReadDir(a.dlTasks[0].Path)
	if err != nil || len(entries) != 1 {
		t.Fatalf("浏览器任务输出 = %v, %v", entries, err)
	}
	saved, err := os.ReadFile(filepath.Join(a.dlTasks[0].Path, entries[0].Name()))
	if err != nil || !bytes.Equal(saved, data.Bytes()) {
		t.Fatalf("浏览器任务保存内容错误: %v", err)
	}
	restored := &App{store: st}
	restored.loadDownloadRecords()
	if len(restored.dlTasks) != 1 || !restored.dlTasks[0].Rendered {
		t.Fatalf("浏览器任务类型未恢复: %+v", restored.dlTasks)
	}
}

func TestDownloadBrowserModeRequiresMatchingPage(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlDir, a.dlURLs, a.dlBrowserMode = t.TempDir(), "https://example.test/gallery", true
	a.submitDownloadComposer()
	if len(a.dlTasks) != 0 || !strings.Contains(a.dlComposerErr, "打开") {
		t.Fatalf("未打开浏览器时不应入队: %+v, %q", a.dlTasks, a.dlComposerErr)
	}
	a.dlBrowser = &fakeDownloadBrowser{}
	a.dlBrowserURL = "https://other.test/gallery"
	a.submitDownloadComposer()
	if len(a.dlTasks) != 0 || !strings.Contains(a.dlComposerErr, "打开") {
		t.Fatalf("网址不匹配时不应入队: %+v, %q", a.dlTasks, a.dlComposerErr)
	}
}

func TestDownloadBrowserModeControlsAreVisible(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlComposerOn, a.dlBrowserMode, a.dlURLs = true, true, "https://example.test/gallery"
	tt := ui.NewTester(a.view, 1024, 700)
	for _, label := range []string{"使用浏览器登录并抓取动态页面", "打开下载浏览器", "浏览器模式采集当前页面", "立即下载"} {
		assertVisible(t, tt, label, 1024, 700)
	}
}

func TestResumeBrowserTaskRequiresReopeningPage(t *testing.T) {
	a, _ := newDownloadApp(t)
	task := newDownloadTask(1, "https://example.test/gallery", t.TempDir())
	task.Rendered, task.Phase = true, dlPaused
	a.dlTasks = []*downloadTask{task}
	a.resumeTask(task)
	if task.Phase != dlFailed || !strings.Contains(task.Err, "下载浏览器") {
		t.Fatalf("未重新打开浏览器的任务 = %+v", task)
	}
}

func TestDownloadBrowserCannotReplaceSessionWhileTaskQueued(t *testing.T) {
	a, _ := newDownloadApp(t)
	old := &fakeDownloadBrowser{}
	a.dlBrowser, a.dlBrowserURL = old, "https://example.test/first"
	a.dlURLs = "https://example.test/second"
	task := newDownloadTask(1, a.dlBrowserURL, t.TempDir())
	task.Rendered, task.Phase = true, dlQueued
	a.dlTasks = []*downloadTask{task}
	a.openDownloadBrowser()
	if a.dlBrowser != old || a.dlBrowserBusy || !strings.Contains(a.dlBrowserErr, "暂停") {
		t.Fatalf("排队中的浏览器任务不应丢失会话: browser=%v, busy=%v, err=%q", a.dlBrowser, a.dlBrowserBusy, a.dlBrowserErr)
	}
}

func TestDownloadDedicatedEnvironmentOpensComposer(t *testing.T) {
	a, st := newDownloadApp(t)
	a.tab, a.detectedNote, a.camoufoxStatus = pageBrowser, "已检测", "Camoufox 已就绪"
	a.dlURLs = "https://example.test/gallery"
	tt := ui.NewTester(a.view, 1024, 700)
	for _, label := range []string{"网页图片下载专用环境", "使用下载环境", "未打开"} {
		assertVisible(t, tt, label, 1024, 700)
	}
	b := &fakeDownloadBrowser{}
	a.dlBrowser = b
	tt.Frame()
	assertVisible(t, tt, "已打开", 1024, 700)
	b.Close()
	tt.Frame()
	assertVisible(t, tt, "未打开", 1024, 700)
	if err := tt.Click("使用下载环境"); err != nil {
		t.Fatal(err)
	}
	if a.tab != pageDownload || !a.dlComposerOn || !a.dlBrowserMode || a.dlURLs != "https://example.test/gallery" {
		t.Fatalf("未打开下载浏览器任务: tab=%v composer=%v mode=%v url=%q", a.tab, a.dlComposerOn, a.dlBrowserMode, a.dlURLs)
	}
	assertVisible(t, tt, "打开下载浏览器", 1024, 700)
	if rows := st.Browsers(); len(rows) != 0 {
		t.Fatalf("下载专用环境不应加入生图账号: %+v", rows)
	}
}

func TestDownloadBrowserClosedSessionIsNotQueued(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlDir, a.dlURLs, a.dlBrowserMode = t.TempDir(), "https://example.test/gallery", true
	a.dlBrowser, a.dlBrowserURL = &fakeDownloadBrowser{closed: true}, a.dlURLs
	a.submitDownloadComposer()
	if len(a.dlTasks) != 0 || !strings.Contains(a.dlComposerErr, "打开") {
		t.Fatalf("已关闭的下载浏览器不应入队: %+v %q", a.dlTasks, a.dlComposerErr)
	}
}

func TestDownloadBrowserProxyDoesNotCloseExisting(t *testing.T) {
	a, _ := newDownloadApp(t)
	browser.SetProxy("http://127.0.0.1:7890")
	t.Cleanup(func() { browser.SetProxy("") })
	old := &fakeDownloadBrowser{}
	a.dlBrowser, a.dlBrowserURL = old, "https://example.test/first"
	a.dlURLs = "https://example.test/second"
	a.openDownloadBrowser()
	if a.dlBrowser != old || !old.Alive() || !strings.Contains(a.dlBrowserErr, "代理") {
		t.Fatalf("代理不可用时不应关闭已有浏览器: browser=%v alive=%v err=%q", a.dlBrowser, old.Alive(), a.dlBrowserErr)
	}
}
