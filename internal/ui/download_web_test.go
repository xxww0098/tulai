package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xxww0098/tulai/internal/download"
)

func TestDownloadComposerDownloadsPageImages(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gallery":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<title>相册</title><img src="/a/photo.png"><img data-src="/b/photo.png"><a href="/detail">详情</a>`)
		case "/detail":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<img src="/asset">`)
		default:
			want := server.URL + "/gallery"
			if r.URL.Path == "/asset" {
				want = server.URL + "/detail"
			}
			if got := r.Header.Get("Referer"); got != want {
				t.Errorf("图片 Referer = %q, want %q", got, want)
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(imageData.Bytes())
		}
	}))
	defer server.Close()

	a, st := newDownloadApp(t)
	a.dlDir, a.dlURLs, a.dlDepth = t.TempDir(), server.URL+"/gallery", 2
	a.submitDownloadComposer()
	if len(a.dlTasks) != 1 || a.dlTasks[0].Phase != dlDone {
		t.Fatalf("网页任务 = %+v", a.dlTasks)
	}
	task := a.dlTasks[0]
	if !task.IsPage || task.ImagesDone != 3 || task.ImagesTotal != 3 || task.Name != "相册" {
		t.Fatalf("网页任务进度 = %+v", task)
	}
	entries, err := os.ReadDir(task.Path)
	if err != nil || len(entries) != 3 {
		t.Fatalf("下载目录 = %v, %v", entries, err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(task.Path, entry.Name()))
		if err != nil || !bytes.Equal(data, imageData.Bytes()) || !strings.HasSuffix(entry.Name(), ".png") {
			t.Fatalf("图片 %s 不正确: %v", entry.Name(), err)
		}
	}
	restored := &App{store: st}
	restored.loadDownloadRecords()
	if len(restored.dlTasks) != 1 || restored.dlTasks[0].Depth != 2 || restored.dlTasks[0].Path != task.Path || restored.dlTasks[0].ImagesTotal != 3 {
		t.Fatalf("网页任务未恢复: %+v", restored.dlTasks)
	}
}

func TestDownloadDepthSettingPersists(t *testing.T) {
	a, st := newDownloadApp(t)
	if a.dlDepth != 1 {
		t.Fatalf("默认扫描深度 = %v", a.dlDepth)
	}
	a.dlDepth = 32767
	a.saveSettings()
	restored := &App{store: st}
	restored.loadSettings()
	if restored.dlDepth != 32767 {
		t.Fatalf("恢复的扫描深度 = %v", restored.dlDepth)
	}
}

func TestDownloadComposerUsesFirstResponseForOneTimeLink(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) != 1 {
			http.Error(w, "expired", http.StatusGone)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("single-use image"))
	}))
	defer server.Close()
	a, _ := newDownloadApp(t)
	a.dlDir, a.dlURLs = t.TempDir(), server.URL+"/one-time"
	a.submitDownloadComposer()
	if len(a.dlTasks) != 1 || a.dlTasks[0].Phase != dlDone || requests.Load() != 1 {
		t.Fatalf("一次性链接: tasks=%+v requests=%d", a.dlTasks, requests.Load())
	}
	data, err := os.ReadFile(a.dlTasks[0].Path)
	if err != nil || string(data) != "single-use image" {
		t.Fatalf("下载内容 = %q, %v", data, err)
	}
}

func TestDownloadComposerRetriesInitialRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("image"))
	}))
	defer server.Close()
	a, _ := newDownloadApp(t)
	a.dlDir, a.dlURLs = t.TempDir(), server.URL+"/retry"
	a.submitDownloadComposer()
	if len(a.dlTasks) != 1 || a.dlTasks[0].Phase != dlDone || requests.Load() != 2 {
		t.Fatalf("重试任务 = %+v, 请求次数 = %d", a.dlTasks, requests.Load())
	}
}

func TestDownloadComposerKeepsDifferentImagesWithTheSameName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer server.Close()
	a, st := newDownloadApp(t)
	a.dlDir = t.TempDir()
	a.dlURLs = server.URL + "/a/photo.png\n" + server.URL + "/b/photo.png"
	a.submitDownloadComposer()
	if len(a.dlTasks) != 2 || a.dlTasks[0].Phase != dlDone || a.dlTasks[1].Phase != dlDone || a.dlTasks[0].Path == a.dlTasks[1].Path {
		t.Fatalf("同名任务 = %+v", a.dlTasks)
	}
	for i, want := range []string{"/a/photo.png", "/b/photo.png"} {
		data, err := os.ReadFile(a.dlTasks[i].Path)
		if err != nil || string(data) != want {
			t.Fatalf("文件 %d = %q, %v", i, data, err)
		}
	}
	restored := &App{store: st}
	restored.loadDownloadRecords()
	if len(restored.dlTasks) != 2 || restored.dlTasks[1].TargetName == "" {
		t.Fatalf("同名任务没有恢复目标名: %+v", restored.dlTasks)
	}
}

func TestDownloadComposerResumesDirectLink(t *testing.T) {
	payload := []byte("partially downloaded image")
	var ranges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"same"`)
		w.Header().Set("Content-Type", "image/png")
		if r.Header.Get("Range") != "" {
			ranges.Add(1)
			if r.Header.Get("Range") != "bytes=8-" || r.Header.Get("If-Range") != `"same"` {
				t.Errorf("续传请求头 = %v", r.Header)
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 8-%d/%d", len(payload)-1, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[8:])
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	a, _ := newDownloadApp(t)
	a.dlDir, a.dlURLs = t.TempDir(), server.URL+"/resume.png"
	partial := filepath.Join(a.dlDir, "resume.png.part")
	if err := os.WriteFile(partial, payload[:8], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := download.WritePartialMeta(partial, download.PartialMeta{URL: a.dlURLs, ETag: `"same"`, Total: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	a.submitDownloadComposer()
	if len(a.dlTasks) != 1 || a.dlTasks[0].Phase != dlDone || ranges.Load() != 1 {
		t.Fatalf("续传任务 = %+v, Range 次数 %d", a.dlTasks, ranges.Load())
	}
	data, err := os.ReadFile(a.dlTasks[0].Path)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("续传内容 = %q, %v", data, err)
	}
}

func TestDownloadPageReportsInvalidImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/" {
			fmt.Fprint(w, `<img src="/bad.jpg">`)
		} else {
			fmt.Fprint(w, "<html>登录后查看</html>")
		}
	}))
	defer server.Close()
	a, _ := newDownloadApp(t)
	a.dlDir, a.dlURLs = t.TempDir(), server.URL
	a.submitDownloadComposer()
	if len(a.dlTasks) != 1 || a.dlTasks[0].Phase != dlFailed || !strings.Contains(a.dlTasks[0].Err, "不是图片") {
		t.Fatalf("无效图片应报告失败: %+v", a.dlTasks)
	}
	entries, err := os.ReadDir(a.dlTasks[0].Path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("错误网页不应保存为图片: %v, %v", entries, err)
	}
}
