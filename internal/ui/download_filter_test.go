package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestDownloadImageFilterIsVisibleAndPersists(t *testing.T) {
	a, st := newDownloadApp(t)
	a.tab = pageSettings
	a.settingsTab = settingsDownload
	tt := ui.NewTester(a.view, 760, 1600)
	for _, label := range []string{"网页图片筛选", "文件大小 (KB)", "图片宽度 (px)", "图片高度 (px)"} {
		scrollIntoView(t, tt, label, 760, 1600)
		assertVisible(t, tt, label, 760, 1600)
	}
	a.dlMinKB, a.dlMaxKB = 20, 500
	a.dlMinWidth, a.dlMaxWidth = 300, 2000
	a.dlMinHeight, a.dlMaxHeight = 200, 1000
	a.saveSettings()
	restored := &App{store: st}
	restored.loadSettings()
	if got, want := restored.downloadImageFilter(), a.downloadImageFilter(); got != want {
		t.Fatalf("筛选设置未恢复: %+v, want %+v", got, want)
	}
}

func TestDownloadPageFiltersImagesAndRestoresTask(t *testing.T) {
	encode := func(width, height int) []byte {
		var buf bytes.Buffer
		if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	small, large := encode(2, 2), encode(12, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gallery" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<title>相册</title><img src="/small.png"><img src="/large.png">`)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		if r.URL.Path == "/small.png" {
			_, _ = w.Write(small)
		} else {
			_, _ = w.Write(large)
		}
	}))
	defer server.Close()
	a, st := newDownloadApp(t)
	a.dlDir, a.dlURLs = t.TempDir(), server.URL+"/gallery"
	a.dlMinWidth, a.dlMaxWidth, a.dlMinHeight = 10, 15, 5
	a.submitDownloadComposer()
	if len(a.dlTasks) != 1 || a.dlTasks[0].Phase != dlDone || a.dlTasks[0].ImagesFiltered != 1 {
		t.Fatalf("筛选任务 = %+v", a.dlTasks)
	}
	entries, err := os.ReadDir(a.dlTasks[0].Path)
	if err != nil || len(entries) != 1 {
		t.Fatalf("筛选后的图片 = %v, %v", entries, err)
	}
	restored := &App{store: st}
	restored.loadDownloadRecords()
	if len(restored.dlTasks) != 1 || restored.dlTasks[0].ImageFilter != a.dlTasks[0].ImageFilter || restored.dlTasks[0].ImagesFiltered != 1 {
		t.Fatalf("筛选任务没有恢复: %+v", restored.dlTasks)
	}
}

func TestDownloadRejectsInvalidFilterBeforeQueueing(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlURLs = "https://example.test/image.jpg"
	a.dlMinWidth, a.dlMaxWidth = 100, 50
	a.submitDownloadComposer()
	if len(a.dlTasks) != 0 || a.dlComposerErr == "" {
		t.Fatalf("无效范围不应入队: tasks=%v error=%q", a.dlTasks, a.dlComposerErr)
	}
}

func TestDownloadDirectImageReportsFiltered(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data.Bytes())
	}))
	defer server.Close()
	a, _ := newDownloadApp(t)
	a.dlDir, a.dlURLs, a.dlMinWidth = t.TempDir(), server.URL+"/small.png", 10
	a.submitDownloadComposer()
	if len(a.dlTasks) != 1 || a.dlTasks[0].Phase != dlDone || a.dlTasks[0].ImagesFiltered != 1 || a.dlTasks[0].Path != "" {
		t.Fatalf("过滤后的直链任务 = %+v", a.dlTasks)
	}
	entries, err := os.ReadDir(a.dlDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("过滤后的目录 = %v, %v", entries, err)
	}
}
