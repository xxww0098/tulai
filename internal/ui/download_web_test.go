package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

type recordingDownloadHost struct {
	stubDesktopHost
	recMu      sync.Mutex
	pickedDirs []string
	opened     []string
	revealed   []string
}

func (h *recordingDownloadHost) PickDirectories(context.Context, string, bool) ([]string, error) {
	h.recMu.Lock()
	defer h.recMu.Unlock()
	return append([]string(nil), h.pickedDirs...), nil
}

func (h *recordingDownloadHost) OpenPath(path string) error {
	h.recMu.Lock()
	h.opened = append(h.opened, path)
	h.recMu.Unlock()
	return nil
}

func (h *recordingDownloadHost) RevealPath(path string) error {
	h.recMu.Lock()
	h.revealed = append(h.revealed, path)
	h.recMu.Unlock()
	return nil
}

func TestDownloadFilterValidationAndSettingsPersistence(t *testing.T) {
	a, st := newDownloadApp(t)
	host := &recordingDownloadHost{}
	a.BindHost(host)
	a.StartActor()
	defer a.StopActor()

	svc := &DownloadService{app: a}
	ctx := context.Background()

	dir := t.TempDir()
	host.pickedDirs = []string{dir}
	if err := svc.DownloadPickDir(ctx); err != nil {
		t.Fatalf("DownloadPickDir: %v", err)
	}
	svc.SetDownloadField(ctx, "depth", "3")
	svc.SetDownloadField(ctx, "minKb", "500")
	svc.SetDownloadField(ctx, "maxKb", "100")
	svc.SetDownloadField(ctx, "minW", "200")
	svc.SetDownloadField(ctx, "maxW", "800")
	svc.SetDownloadField(ctx, "minH", "150")
	svc.SetDownloadField(ctx, "maxH", "900")
	svc.SetDownloadField(ctx, "browserMode", "true")

	// Reload into a fresh App to confirm settings persisted immediately.
	restored := &App{store: st}
	restored.loadSettings()
	if restored.dlDir != dir || restored.dlDepth != 3 || restored.dlMinKB != 500 || restored.dlMaxKB != 100 ||
		restored.dlMinWidth != 200 || restored.dlMaxWidth != 800 || restored.dlMinHeight != 150 || restored.dlMaxHeight != 900 || !restored.dlBrowserMode {
		t.Fatalf("settings did not persist immediately: dir=%q depth=%v kb=%v..%v w=%v..%v h=%v..%v browserMode=%v",
			restored.dlDir, restored.dlDepth, restored.dlMinKB, restored.dlMaxKB,
			restored.dlMinWidth, restored.dlMaxWidth, restored.dlMinHeight, restored.dlMaxHeight, restored.dlBrowserMode)
	}

	// submitDownloadComposer must reject invalid min > max filter before enqueueing.
	svc.SetDownloadField(ctx, "browserMode", "false")
	svc.SetDownloadField(ctx, "composerOn", "true")
	svc.SubmitDownloadURLs(ctx, "https://example.test/image.png")
	got := svc.GetDownloadState(ctx)
	if !got.ComposerOn || !strings.Contains(got.ComposerErr, "最小值不能大于最大值") || len(got.Tasks) != 0 {
		t.Fatalf("expected invalid filter rejection, got composerOn=%v err=%q tasks=%d", got.ComposerOn, got.ComposerErr, len(got.Tasks))
	}

	// Fixing maxKb clears stale composerErr.
	svc.SetDownloadField(ctx, "maxKb", "1000")
	if errText := svc.GetDownloadState(ctx).ComposerErr; errText != "" {
		t.Fatalf("expected composerErr cleared after fixing filter, got %q", errText)
	}
}

func TestDownloadPageBatchTracksRealTimeBytesAndSpeed(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	// Pad PNG trailing bytes (or use repeated large PNG payload) so each image is ~32 KiB.
	raw := imageData.Bytes()
	payload := append(append([]byte(nil), raw...), bytes.Repeat([]byte{0x00}, 32*1024)...)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gallery" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<title>速率测试相册</title><img src="/1.png"><img src="/2.png"><img src="/3.png"><img src="/4.png">`)
			return
		}
		time.Sleep(60 * time.Millisecond)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	a, _ := newDownloadApp(t)
	a.dlDir = t.TempDir()
	a.downloadConc = 1 // serialize image downloads so duration > 240ms and speed samples fire reliably
	a.BindHost(&recordingDownloadHost{})
	a.StartActor()
	defer a.StopActor()

	svc := &DownloadService{app: a}
	ctx := context.Background()
	svc.SubmitDownloadURLs(ctx, server.URL+"/gallery")

	var sawLiveSpeed bool
	var sawLiveWritten bool
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state := svc.GetDownloadState(ctx)
		if state.TotalSpeed > 0 {
			sawLiveSpeed = true
		}
		if len(state.Tasks) == 1 && state.Tasks[0].Written > 0 && state.Tasks[0].Phase == "下载中" {
			sawLiveWritten = true
		}
		if state.CountDone == 1 {
			break
		}
		time.Sleep(15 * time.Millisecond)
	}
	svc.SetDownloadFilter(ctx, dlFilterDone)
	state := svc.GetDownloadState(ctx)
	if len(state.Tasks) != 1 || state.Tasks[0].Phase != "已完成" {
		t.Fatalf("expected completed page task, got countDone=%d tasks=%+v", state.CountDone, state.Tasks)
	}
	task := state.Tasks[0]
	if !sawLiveSpeed || !sawLiveWritten {
		t.Fatalf("expected real-time batch speed and written bytes during page download: sawSpeed=%v sawWritten=%v task=%+v", sawLiveSpeed, sawLiveWritten, task)
	}
	wantBytes := int64(4 * len(payload))
	if task.Written != wantBytes || task.Total != wantBytes || task.Speed != 0 {
		t.Fatalf("unexpected final page bytes/speed: written=%d total=%d speed=%d want=%d", task.Written, task.Total, task.Speed, wantBytes)
	}
}

func TestImportDownloadSheetIPCAndErrors(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.BindHost(&recordingDownloadHost{})
	a.StartActor()
	defer a.StopActor()

	svc := &DownloadService{app: a}
	ctx := context.Background()
	dir := t.TempDir()

	// Unsupported / missing / directory paths return clear errors.
	if err := svc.ImportDownloadSheet(ctx, filepath.Join(dir, "legacy.xls")); err == nil || !strings.Contains(err.Error(), ".xls") {
		t.Fatalf("expected .xls error, got %v", err)
	}
	if err := svc.ImportDownloadSheet(ctx, filepath.Join(dir, "missing.csv")); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("expected missing file error, got %v", err)
	}
	if err := svc.ImportDownloadSheet(ctx, dir); err == nil {
		t.Fatal("expected error for directory path")
	}
	if err := svc.ImportDownloadSheet(ctx, filepath.Join(dir, "photo.png")); err == nil || !strings.Contains(err.Error(), "CSV 或 XLSX") {
		t.Fatalf("expected extension error, got %v", err)
	}

	// Valid CSV opens import preview dialog and closes composer.
	csvPath := filepath.Join(dir, "urls.csv")
	if err := os.WriteFile(csvPath, []byte("名称,图片链接\n商品A,https://example.test/a.jpg\n商品B,https://example.test/b.jpg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.SetDownloadField(ctx, "composerOn", "true")
	if err := svc.ImportDownloadSheet(ctx, csvPath); err != nil {
		t.Fatalf("ImportDownloadSheet valid CSV failed: %v", err)
	}
	st := svc.GetDownloadState(ctx)
	if !st.Import.Open || st.Import.FileName != "urls.csv" || st.Import.ImageCount != 2 || st.ComposerOn {
		t.Fatalf("unexpected import state: %+v composerOn=%v", st.Import, st.ComposerOn)
	}
}

func TestDownloadRowOpenRevealSafetyAndGlobalCounters(t *testing.T) {
	a, _ := newDownloadApp(t)
	host := &recordingDownloadHost{}
	a.BindHost(host)
	a.StartActor()
	defer a.StopActor()

	svc := &DownloadService{app: a}
	ctx := context.Background()

	existingDir := t.TempDir()
	existingFile := filepath.Join(existingDir, "ok.png")
	if err := os.WriteFile(existingFile, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	missingDir := filepath.Join(t.TempDir(), "deleted-dir")

	_ = a.call(ctx, func() {
		t1 := newDownloadTask(1, "https://example.test/1.png", existingDir)
		t1.Phase = dlActive
		t1.Speed = 12345
		t2 := newDownloadTask(2, "https://example.test/2.png", existingDir)
		t2.Phase = dlQueued
		t3 := newDownloadTask(3, "https://example.test/3.png", existingDir)
		t3.Phase = dlPaused
		t4 := newDownloadTask(4, "https://example.test/4.png", existingDir)
		t4.Phase = dlFailed
		t4.Err = "network error"
		t5 := newDownloadTask(5, "https://example.test/5.png", existingDir)
		t5.Phase = dlDone
		t5.Path = existingFile
		t6 := newDownloadTask(6, "https://example.test/6.png", existingDir)
		t6.Phase = dlDone
		t6.Path = filepath.Join(existingDir, "gone.png")
		t7 := newDownloadTask(7, "https://example.test/7.png", missingDir)
		t7.Phase = dlDone
		t7.Path = filepath.Join(missingDir, "gone.png")
		a.dlDir = missingDir
		a.dlTasks = []*downloadTask{t1, t2, t3, t4, t5, t6, t7}
		a.dlNextID = 7
	})

	// Switching filter to Done or searching must keep global counters and TotalSpeed intact.
	svc.SetDownloadFilter(ctx, dlFilterDone)
	svc.SetDownloadField(ctx, "search", "nonexistent-keyword")
	st := svc.GetDownloadState(ctx)
	if st.TotalSpeed != 12345 || st.ActiveCount != 1 || st.QueuedCount != 1 || st.PausedCount != 1 || st.FailedCount != 1 || len(st.Tasks) != 0 {
		t.Fatalf("global counters broken under filter/search: %+v", st)
	}

	// Open existing file opens file directly.
	if err := svc.DownloadRowAction(ctx, 5, "open"); err != nil {
		t.Fatalf("open existing file failed: %v", err)
	}
	// Reveal missing file with existing dir falls back to dir and emits toast.
	if err := svc.DownloadRowAction(ctx, 6, "reveal"); err != nil {
		t.Fatalf("reveal fallback dir failed: %v", err)
	}
	// Open missing file with missing dir & fallback dir returns clear error.
	if err := svc.DownloadRowAction(ctx, 7, "open"); err == nil || !strings.Contains(err.Error(), "均不存在") {
		t.Fatalf("expected missing path error, got %v", err)
	}

	host.recMu.Lock()
	opened := append([]string(nil), host.opened...)
	revealed := append([]string(nil), host.revealed...)
	host.recMu.Unlock()
	if len(opened) != 1 || opened[0] != existingFile {
		t.Fatalf("opened = %v, want [%s]", opened, existingFile)
	}
	if len(revealed) != 1 || revealed[0] != existingDir {
		t.Fatalf("revealed = %v, want [%s]", revealed, existingDir)
	}
	var toast string
	_ = a.call(ctx, func() { toast = a.toastText })
	if !strings.Contains(toast, "已打开保存目录") {
		t.Fatalf("expected fallback toast, got %q", toast)
	}

	// DownloadOpenDir returns error when target is a regular file (cannot MkdirAll).
	svc.SetDownloadField(ctx, "dir", existingFile)
	if err := svc.DownloadOpenDir(ctx); err == nil || !strings.Contains(err.Error(), "无法打开下载目录") {
		t.Fatalf("expected DownloadOpenDir error on regular file path, got %v", err)
	}
}
