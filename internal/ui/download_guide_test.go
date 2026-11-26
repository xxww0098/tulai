package ui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/download"
	"github.com/xxww0098/tulai/internal/store"
)

type fakeDownloadBrowser struct {
	alive          bool
	focused        atomic.Int32
	openedURL      string
	bindOpenPageFn func(context.Context, string) error
	captureFn      func(context.Context) (download.Discovery, error)
}

func (f *fakeDownloadBrowser) Capture(ctx context.Context) (download.Discovery, error) {
	if f.captureFn != nil {
		return f.captureFn(ctx)
	}
	return download.Discovery{}, nil
}

func (f *fakeDownloadBrowser) BindCurrent(context.Context, string, string) error {
	return nil
}

func (f *fakeDownloadBrowser) BindOpenPage(ctx context.Context, rawURL string) error {
	if f.bindOpenPageFn != nil {
		return f.bindOpenPageFn(ctx, rawURL)
	}
	return nil
}

func (f *fakeDownloadBrowser) OpenTarget(_ context.Context, rawURL string) error {
	f.openedURL = rawURL
	return nil
}

func (f *fakeDownloadBrowser) Focus(context.Context) error {
	f.focused.Add(1)
	return nil
}

func (f *fakeDownloadBrowser) Getter() download.Getter { return nil }
func (f *fakeDownloadBrowser) Alive() bool             { return f.alive }
func (f *fakeDownloadBrowser) Close()                  { f.alive = false }

func TestDownloadGuideEntriesAreSingleSiteWithValidIcons(t *testing.T) {
	iconDir := filepath.Join("..", "..", "frontend", "src", "assets", "download-sites")
	entries, err := os.ReadDir(iconDir)
	if err != nil {
		t.Fatalf("read download-sites dir: %v", err)
	}
	diskIcons := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".png" && ext != ".svg" {
			t.Fatalf("unexpected non-icon file %q in download-sites", name)
		}
		domain := strings.TrimSuffix(name, ext)
		diskIcons[strings.ToLower(domain)] = name
	}

	referenced := make(map[string]bool)
	for _, group := range downloadGuideGroups {
		if len(group.entries) == 0 {
			t.Fatalf("guide group %q has no entries", group.title)
		}
		for _, entry := range group.entries {
			if strings.TrimSpace(entry.site) == "" {
				t.Fatalf("guide group %q contains blank site entry", group.title)
			}
			if entry.icons == "" {
				if entry.site != "图片直链、普通网页" {
					t.Fatalf("entry %q in %q is missing a site domain", entry.site, group.title)
				}
				continue
			}
			if strings.Contains(entry.site, "、") {
				t.Fatalf("entry %q in %q merges multiple sites into one row", entry.site, group.title)
			}
			domains := strings.Fields(entry.icons)
			if len(domains) != 1 {
				t.Fatalf("entry %q must specify exactly 1 domain, got %q", entry.site, entry.icons)
			}
			domain := strings.ToLower(domains[0])
			if _, ok := diskIcons[domain]; !ok {
				t.Fatalf("missing site icon asset for %q (%s)", entry.site, domain)
			}
			referenced[domain] = true
		}
	}

	for domain, file := range diskIcons {
		if !referenced[domain] {
			t.Errorf("orphan download site icon %q (%s) not referenced by downloadGuideGroups", file, domain)
		}
	}
}

func TestMatchingDownloadGuideSearch(t *testing.T) {
	tmallGroups := matchingDownloadGuide("tmall.com")
	if len(tmallGroups) != 1 || len(tmallGroups[0].entries) != 1 || tmallGroups[0].entries[0].site != "天猫" {
		t.Fatalf("tmall.com search = %+v, want only 天猫", tmallGroups)
	}

	taobaoGroups := matchingDownloadGuide("taobao.com")
	if len(taobaoGroups) != 1 || len(taobaoGroups[0].entries) != 1 || taobaoGroups[0].entries[0].site != "淘宝" {
		t.Fatalf("taobao.com search = %+v, want only 淘宝", taobaoGroups)
	}

	mokoGroups := matchingDownloadGuide("美空")
	if len(mokoGroups) != 1 || len(mokoGroups[0].entries) != 1 || mokoGroups[0].entries[0].icons != "moko.cc" {
		t.Fatalf("美空 search = %+v, want domain moko.cc", mokoGroups)
	}

	fpGroups := matchingDownloadGuide("指纹浏览器")
	if len(fpGroups) == 0 {
		t.Fatal("指纹浏览器 search returned no groups")
	}
}

func TestDownloadBrowserFocusAndFingerprintGuards(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("XDG_CONFIG_HOME", tmpHome)
	t.Setenv("APPDATA", tmpHome)

	st, err := store.Open(filepath.Join(tmpHome, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a := &App{store: st, manager: browser.NewManager()}
	a.loadSettings()
	a.BindHost(&stubDesktopHost{})

	svc := &DownloadService{app: a}
	ctx := context.Background()

	// Uninstalled Fingerprint state in isolated home
	state := svc.GetDownloadState(ctx)
	wantSupported := browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, browser.KindFingerprint)
	if state.BrowserSupported != wantSupported || state.BrowserReady || state.BrowserInstallBusy {
		t.Fatalf("unexpected browser runtime state: %+v", state)
	}

	// Attempting to launch Fingerprint download browser reports actionable error
	svc.OpenDownloadBrowser(ctx, "")
	if wantSupported {
		if !strings.Contains(a.dlBrowserErr, "尚未安装") || !strings.Contains(a.dlBrowserErr, "设置 → 浏览器与网络") {
			t.Fatalf("unexpected uninstalled browser error: %q", a.dlBrowserErr)
		}
	}

	// Installing Fingerprint updates both guard message and DTO state
	a.runtimeBusy = true
	svc.OpenDownloadBrowser(ctx, "")
	if wantSupported && !strings.Contains(a.dlBrowserErr, "正在安装") {
		t.Fatalf("expected installing hint, got %q", a.dlBrowserErr)
	}
	if !svc.GetDownloadState(ctx).BrowserInstallBusy {
		t.Fatal("expected BrowserInstallBusy = true")
	}
	a.runtimeBusy = false

	// Reject multi-URL submissions into Fingerprint browser
	svc.OpenDownloadBrowser(ctx, "https://a.example/1\nhttps://b.example/2")
	if a.dlBrowserErr != "指纹浏览器每次只能打开一个网址" {
		t.Fatalf("multi-URL OpenDownloadBrowser err = %q", a.dlBrowserErr)
	}

	// With active Fingerprint session and no URL, OpenDownloadBrowser focuses the window even if composer has stale multi-line URLs
	fake := &fakeDownloadBrowser{alive: true}
	a.dlBrowser = fake
	a.dlURLs = "https://stale.example/1\nhttps://stale.example/2"
	svc.OpenDownloadBrowser(ctx, "")
	deadline := time.Now().Add(2 * time.Second)
	for (fake.focused.Load() == 0 || a.dlBrowserBusy) && time.Now().Before(deadline) {
		a.flushUIUpdates()
		time.Sleep(5 * time.Millisecond)
	}
	a.flushUIUpdates()
	if fake.focused.Load() != 1 || a.dlBrowserBusy || a.dlBrowserErr != "" || fake.openedURL != "" {
		t.Fatalf("expected Focus() once with empty err and no navigation, got focused=%d busy=%v err=%q opened=%q", fake.focused.Load(), a.dlBrowserBusy, a.dlBrowserErr, fake.openedURL)
	}

	// Dismissing errors via SetDownloadField works for browser and composer errors
	a.dlBrowserErr = "测试错误"
	svc.SetDownloadField(ctx, "dismissBrowserErr", "1")
	if a.dlBrowserErr != "" {
		t.Fatalf("dismissBrowserErr did not clear error: %q", a.dlBrowserErr)
	}
}

func TestDownloadEmptyPageFailureSkipsPathAndAllowsBrowserRequeue(t *testing.T) {
	a, st := newDownloadApp(t)
	a.dlDir = t.TempDir()

	// 1) Failing page discovery with 0 items must not stamp a non-existent Local Path.
	task := newDownloadTask(1, "https://www.temu.com/search_result.html?search_key=test", a.dlDir)
	task.Rendered = true
	a.dlTasks = []*downloadTask{task}
	a.dlNextID = 1
	err := a.downloadPageImages(context.Background(), task, download.New(download.Options{Getter: download.NewHTTPGetter("")}), download.Job{Directory: a.dlDir}, download.Discovery{IsPage: true, Title: "Temu"}, "", 2, nil)
	if err == nil || task.Path != "" || !task.IsPage || task.Name != "Temu" {
		t.Fatalf("empty discovery should fail without non-existent path: err=%v task=%+v", err, task)
	}

	// 2) Duplicate URL submission in normal mode reports duplicate message instead of "先粘贴至少一个地址".
	a.dlBrowserMode = false
	a.enqueueDownloadComposer(task.URL)
	if !strings.Contains(a.dlComposerErr, "已有下载任务") {
		t.Fatalf("duplicate URL err = %q, want duplicate hint", a.dlComposerErr)
	}

	// 3) Loading persisted failed 0-image page record clears stale non-existent path.
	task.Phase = dlFailed
	task.Path = filepath.Join(a.dlDir, "Temu-nonexistent")
	a.saveDownloadRecords()
	reloaded := &App{store: st}
	reloaded.loadDownloadRecords()
	if len(reloaded.dlTasks) != 1 || reloaded.dlTasks[0].Path != "" {
		t.Fatalf("stale non-existent path not cleared on load: %+v", reloaded.dlTasks)
	}

	// 4) Clicking browser download button again on a failed rendered task requeues it instead of returning "无法加入下载任务".
	captured := make(chan struct{}, 1)
	fake := &fakeDownloadBrowser{
		alive: true,
		captureFn: func(context.Context) (download.Discovery, error) {
			select {
			case captured <- struct{}{}:
			default:
			}
			return download.Discovery{IsPage: true, Title: "Temu"}, nil
		},
	}
	a.dlBrowser = fake
	a.dlBrowserMode = true
	task.Phase = dlFailed
	task.Path = ""
	a.BindHost(&stubDesktopHost{})
	a.StartActor()
	defer a.StopActor()
	if err := a.downloadBrowserRequest(context.Background(), fake, task.URL, "12345678-1234-1234-1234-123456789abc"); err != nil {
		t.Fatalf("re-triggering failed rendered task from browser failed: %v", err)
	}
	select {
	case <-captured:
	case <-time.After(2 * time.Second):
		t.Fatal("requeued browser task did not start capture")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		done := false
		_ = a.call(context.Background(), func() {
			done = a.downloadsRunning == 0 && len(a.dlTasks) == 1
		})
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var count int
	_ = a.call(context.Background(), func() { count = len(a.dlTasks) })
	if count != 1 {
		t.Fatalf("expected existing task to be reused, got %d tasks", count)
	}
}

func TestDownloadBrowserCrashAndLifecycleAntiDeadlock(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlDir = t.TempDir()
	a.BindHost(&stubDesktopHost{})
	a.StartActor()
	defer a.StopActor()

	svc := &DownloadService{app: a}
	ctx := context.Background()

	// 1) Browser-required sites submitted with browserMode=false give clear guidance without creating a doomed task.
	svc.SetDownloadField(ctx, "browserMode", "false")
	svc.SetDownloadField(ctx, "composerOn", "true")
	svc.SubmitDownloadURLs(ctx, "https://www.temu.com/search_result.html?search_key=shoes")
	st := svc.GetDownloadState(ctx)
	if !strings.Contains(st.ComposerErr, "Temu 搜索") || !strings.Contains(st.ComposerErr, "托管指纹浏览器") || len(st.Tasks) != 0 {
		t.Fatalf("expected Temu search browser guidance, got err=%q tasks=%d", st.ComposerErr, len(st.Tasks))
	}
	svc.SubmitDownloadURLs(ctx, "https://www.xiaohongshu.com/explore/676a35670000000013002578")
	st = svc.GetDownloadState(ctx)
	if !strings.Contains(st.ComposerErr, "小红书") || !strings.Contains(st.ComposerErr, "托管指纹浏览器") || len(st.Tasks) != 0 {
		t.Fatalf("expected Xiaohongshu browser guidance, got err=%q tasks=%d", st.ComposerErr, len(st.Tasks))
	}

	// 2) Active browser download: Focus("") still works, while OpenTarget(newURL) is blocked.
	started := make(chan struct{}, 1)
	releaseCapture := make(chan struct{})
	fake := &fakeDownloadBrowser{
		alive: true,
		captureFn: func(c context.Context) (download.Discovery, error) {
			select {
			case started <- struct{}{}:
			default:
			}
			select {
			case <-c.Done():
				return download.Discovery{}, c.Err()
			case <-releaseCapture:
				return download.Discovery{IsPage: true, Title: "Done"}, nil
			}
		},
	}
	_ = a.call(ctx, func() {
		a.dlBrowser = fake
		a.dlBrowserURL = "https://www.temu.com/search_result.html?search_key=active"
		a.dlBrowserMode = true
		a.dlComposerOn = true
		a.dlURLs = a.dlBrowserURL
		a.submitDownloadComposer()
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("rendered capture did not start")
	}

	// Focusing active browser succeeds; navigating to another URL is blocked.
	svc.OpenDownloadBrowser(ctx, "")
	deadline := time.Now().Add(2 * time.Second)
	for (fake.focused.Load() == 0 || svc.GetDownloadState(ctx).BrowserBusy) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if fake.focused.Load() != 1 || svc.GetDownloadState(ctx).BrowserBusy {
		t.Fatalf("expected Focus() to succeed while browser download is active, got focused=%d busy=%v", fake.focused.Load(), svc.GetDownloadState(ctx).BrowserBusy)
	}
	svc.OpenDownloadBrowser(ctx, "https://www.temu.com/search_result.html?search_key=other")
	if errMsg := svc.GetDownloadState(ctx).BrowserErr; !strings.Contains(errMsg, "请等待当前指纹浏览器下载结束或暂停") {
		t.Fatalf("expected navigation blocked while browser busy, got %q", errMsg)
	}

	// 3) Unexpected browser exit cancels active rendered task, marks it dlFailed (not dlCanceled), and clears dlBrowserTask.
	_ = a.call(ctx, func() {
		fake.alive = false
		a.handleDownloadBrowserClosed(fake)
	})
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var done bool
		_ = a.call(ctx, func() {
			done = a.downloadsRunning == 0 && a.dlBrowserTask == nil && len(a.dlTasks) == 1 && a.dlTasks[0].Phase == dlFailed
		})
		if done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	var taskPhase dlPhase
	var taskErr string
	var inUse bool
	_ = a.call(ctx, func() {
		taskPhase = a.dlTasks[0].Phase
		taskErr = a.dlTasks[0].Err
		inUse = a.downloadBrowserInUse()
	})
	if taskPhase != dlFailed || !strings.Contains(taskErr, "指纹浏览器已关闭") || inUse {
		t.Fatalf("browser crash cleanup failed: phase=%v err=%q inUse=%v", taskPhase, taskErr, inUse)
	}
	close(releaseCapture)
}

func TestDownloadSubmitBrowserDraftDriftRequeueAndLifecycle(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlDir = t.TempDir()
	a.BindHost(&stubDesktopHost{})
	a.StartActor()
	defer a.StopActor()

	svc := &DownloadService{app: a}
	ctx := context.Background()

	// 1) submitDownloadBrowserURL reports error even if composer closed / draft changed mid-bind, and clears dlBrowserBusy.
	bindWait := make(chan struct{})
	fake := &fakeDownloadBrowser{
		alive: true,
		bindOpenPageFn: func(context.Context, string) error {
			<-bindWait
			return errors.New("下载浏览器中未找到该网页")
		},
	}
	_ = a.call(ctx, func() {
		a.dlBrowser = fake
		a.dlBrowserMode = true
		a.dlComposerOn = true
		a.dlURLs = "https://example.test/page"
		a.submitDownloadBrowserURL("https://example.test/page")
		// Simulate user closing composer while BindOpenPage is in flight.
		a.dlComposerOn = false
	})
	close(bindWait)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st := svc.GetDownloadState(ctx)
		if !st.BrowserBusy {
			if !strings.Contains(st.BrowserErr, "托管指纹浏览器中未找到该网页") {
				t.Fatalf("expected normalized browser error surfaced when composer closed, got %q", st.BrowserErr)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if svc.GetDownloadState(ctx).BrowserBusy {
		t.Fatal("dlBrowserBusy remained stuck true after BindOpenPage failed")
	}

	// 2) Normal-mode failed task can be upgraded to rendered via browser download button and via composer resubmit.
	requeued := make(chan struct{}, 2)
	fake.bindOpenPageFn = nil
	fake.captureFn = func(context.Context) (download.Discovery, error) {
		select {
		case requeued <- struct{}{}:
		default:
		}
		return download.Discovery{IsPage: true, Title: "Upgraded"}, nil
	}
	_ = a.call(ctx, func() {
		task := newDownloadTask(1, "https://example.test/needs-browser", a.dlDir)
		task.Phase = dlFailed
		task.Rendered = false
		task.Err = "failed"
		a.dlTasks = []*downloadTask{task}
		a.dlNextID = 1
	})
	if err := a.downloadBrowserRequest(ctx, fake, "https://example.test/needs-browser", "12345678-1234-1234-1234-123456789abc"); err != nil {
		t.Fatalf("downloadBrowserRequest on failed normal task failed: %v", err)
	}
	select {
	case <-requeued:
	case <-time.After(2 * time.Second):
		t.Fatal("upgraded browser task did not run")
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var idle bool
		_ = a.call(ctx, func() { idle = a.downloadsRunning == 0 && len(a.dlTasks) == 1 && a.dlTasks[0].Rendered })
		if idle {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 3) cancelTask moves failed task to trash; pruneDownloads & clearDownloadTrash clear dlSelectedID and emit toast.
	_ = a.call(ctx, func() {
		t1 := a.dlTasks[0]
		t1.Phase = dlFailed
		a.dlSelectedID = t1.ID
		a.cancelTask(t1)
		if t1.Phase != dlCanceled {
			t.Fatalf("cancelTask on failed task = %v, want dlCanceled", t1.Phase)
		}
		removed := a.clearDownloadTrash()
		if removed != 1 || a.dlSelectedID != 0 || !strings.Contains(a.toastText, "回收站已清空") {
			t.Fatalf("clearDownloadTrash unexpected: removed=%d selected=%d toast=%q", removed, a.dlSelectedID, a.toastText)
		}

		doneTask := newDownloadTask(2, "https://example.test/done.png", a.dlDir)
		doneTask.Phase = dlDone
		a.dlTasks = []*downloadTask{doneTask}
		a.dlSelectedID = doneTask.ID
		// resumeTask ignores non-paused tasks.
		a.resumeTask(doneTask)
		if doneTask.Phase != dlDone {
			t.Fatalf("resumeTask mutated dlDone task to %v", doneTask.Phase)
		}
		pruned := a.pruneDownloads()
		if pruned != 1 || a.dlSelectedID != 0 || !strings.Contains(a.toastText, "已清理") {
			t.Fatalf("pruneDownloads unexpected: pruned=%d selected=%d toast=%q", pruned, a.dlSelectedID, a.toastText)
		}
	})
}

func TestDownloadBookmarkFoldersCoverSupportedGuideSites(t *testing.T) {
	byName := make(map[string]string)
	for _, folder := range download.DownloadBookmarkFolders {
		for _, item := range folder.Items {
			byName[item.Name] = item.URL
		}
	}

	excluded := map[string]bool{
		"图片直链、普通网页": true,
		"国美":        true,
		"穷游相册":      true,
		"网易博客":      true,
		"美空 MOKO":   true,
		"四座网":       true,
	}
	for _, grp := range downloadGuideGroups {
		for _, entry := range grp.entries {
			if excluded[entry.site] || strings.Contains(entry.pages, "尚未提供对应的专用图片采集") {
				if _, present := byName[entry.site]; present {
					t.Fatalf("excluded/unsupported site %q should not be in bookmarks", entry.site)
				}
				continue
			}
			if _, ok := byName[entry.site]; !ok {
				t.Fatalf("supported guide site %q (%s) missing from DownloadBookmarkFolders", entry.site, entry.icons)
			}
		}
	}
}
