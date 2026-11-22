package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestFocusedBrowserDrawerKeyboard(t *testing.T) {
	drawer := &browserDrawerState{inputs: make(chan cdp.PageInput, 4)}
	drawer.held.Store(true)
	a := &App{browserDrawer: drawer}
	if !a.handleBrowserDrawerKey(core.KeyEvent{Key: "a"}) || len(drawer.inputs) != 1 {
		t.Fatal("focused browser did not receive the typed character")
	}
	if in := <-drawer.inputs; in.Kind != "text" || in.Text != "a" {
		t.Fatalf("unexpected browser text input: %+v", in)
	}
	if !a.handleBrowserDrawerKey(core.KeyEvent{Key: "Escape"}) || a.browserDrawer != nil || len(drawer.inputs) != 0 {
		t.Fatal("Escape should close the focused drawer without forwarding a key to the webpage")
	}
}

func TestControllerSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	bm := browser.NewManager()
	eng := tasks.NewEngine(st, bm, nil, dir)
	defer eng.Shutdown()

	a := NewApp(st, bm, eng, dir)
	SetTestApp(a)
	defer SetTestApp(nil)

	setSettingFieldBridge("outputPath", filepath.Join(dir, "out"))
	setSettingFieldBridge("numPrefix", "batch-")
	setSettingFieldBridge("dailyCapDoubao", "15")
	setSettingFieldBridge("proxyCustom", "true")
	setSettingFieldBridge("proxyURL", "http://127.0.0.1:7890")
	setSettingFieldBridge("tab", "2")
	saveSettingsNowBridge()

	reloaded := NewApp(st, bm, eng, dir)
	if reloaded.outputPath != filepath.Join(dir, "out") {
		t.Fatalf("outputPath = %q", reloaded.outputPath)
	}
	if reloaded.numPrefix != "batch-" {
		t.Fatalf("numPrefix = %q", reloaded.numPrefix)
	}
	if reloaded.shownDailyCap() != 15 {
		t.Fatalf("dailyCap = %d", reloaded.shownDailyCap())
	}
	if !reloaded.proxyCustom || reloaded.proxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("proxy = (%v, %q)", reloaded.proxyCustom, reloaded.proxyURL)
	}

	SetTestApp(reloaded)
	s := getSettingsStateBridge()
	if s.DailyCapDoubao != 15 || !s.ProxyCustom || s.ProxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("settings DTO mismatch: %+v", s)
	}
}

func TestControllerWorkspaceAndPromptsBridge(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	eng := tasks.NewEngine(st, browser.NewManager(), nil, dir)
	defer eng.Shutdown()

	a := NewApp(st, nil, eng, dir)
	SetTestApp(a)
	defer SetTestApp(nil)

	if a.tab != pageWash {
		t.Fatalf("initial tab = %d, want pageWash (%d)", a.tab, pageWash)
	}
	if wbInit := getWorkbenchStateBridge(); wbInit.Mode != "wash" {
		t.Fatalf("initial workbench mode = %q, want wash", wbInit.Mode)
	}

	setWorkbenchFieldBridge("mode", "free")
	setWorkbenchFieldBridge("prompts", "赛博朋克夜景")
	wb := getWorkbenchStateBridge()
	if wb.Mode != "free" || wb.Prompts != "赛博朋克夜景" {
		t.Fatalf("free workbench state: %+v", wb)
	}

	setWorkbenchFieldBridge("mode", "wash")
	wbWash := getWorkbenchStateBridge()
	if wbWash.Mode != "wash" || wbWash.Prompts == "" {
		t.Fatalf("wash workbench state: %+v", wbWash)
	}

	setWorkbenchFieldBridge("mode", "fission")
	wbFis := getWorkbenchStateBridge()
	if wbFis.Mode != "fission" || wbFis.Prompts != DefaultFissionPrompt {
		t.Fatalf("fission workbench state: %+v", wbFis)
	}

	createNewPromptBridge("商品精修", "洗图", "高清晰白底棚拍")
	ps := getPromptsStateBridge()
	found := false
	for _, it := range ps.Items {
		if it.Name == "商品精修" && !it.Builtin {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created prompt missing: %+v", ps.Items)
	}
}

func newDownloadApp(t *testing.T) (*App, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := &App{store: st, manager: browser.NewManager()}
	a.loadSettings()
	return a, st
}

func TestControllerPlatformSingleSelect(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Seed legacy multi-platform record and platform_auto=1
	st.SetPlatforms([]string{"千问", "豆包", "ChatGPT"})
	st.SetInt("platform_auto", 1)

	eng := tasks.NewEngine(st, browser.NewManager(), nil, dir)
	defer eng.Shutdown()

	a := NewApp(st, nil, eng, dir)
	SetTestApp(a)
	defer SetTestApp(nil)

	if !slices.Equal(a.selectedPlatforms(), []string{"千问"}) {
		t.Fatalf("selectedPlatforms on load = %v, want [千问]", a.selectedPlatforms())
	}
	if !slices.Equal(a.activePlatforms(), []string{"千问"}) || a.platformAuto {
		t.Fatalf("activePlatforms = %v, platformAuto = %v", a.activePlatforms(), a.platformAuto)
	}

	// Switch across all providers; exactly one remains active
	for _, want := range []string{"ChatGPT", agy.Platform, "豆包", "千问"} {
		setPlatformSelectedBridge(want, true)
		if got := a.selectedPlatforms(); !slices.Equal(got, []string{want}) {
			t.Fatalf("after selecting %q: selectedPlatforms = %v", want, got)
		}
		if got := a.activePlatforms(); !slices.Equal(got, []string{want}) {
			t.Fatalf("after selecting %q: activePlatforms = %v", want, got)
		}
		wb := getWorkbenchStateBridge()
		activeCount := 0
		for _, on := range []bool{wb.Doubao, wb.Qianwen, wb.Agy, wb.ChatGPT} {
			if on {
				activeCount++
			}
		}
		if activeCount != 1 {
			t.Fatalf("after selecting %q: activeCount = %d (%+v)", want, activeCount, wb)
		}
	}

	// Passing on=false must never clear all providers
	setPlatformSelectedBridge("千问", false)
	if !slices.Equal(a.selectedPlatforms(), []string{"千问"}) {
		t.Fatalf("passing on=false cleared provider: %v", a.selectedPlatforms())
	}

	// Unknown provider must be ignored without clearing active selection
	setPlatformSelectedBridge("UnknownPlatform", true)
	if !slices.Equal(a.selectedPlatforms(), []string{"千问"}) {
		t.Fatalf("unknown provider altered selection: %v", a.selectedPlatforms())
	}

	// Persists across reload as a single provider
	reloaded := NewApp(st, nil, eng, dir)
	if !slices.Equal(reloaded.selectedPlatforms(), []string{"千问"}) {
		t.Fatalf("reloaded selectedPlatforms = %v, want [千问]", reloaded.selectedPlatforms())
	}
}

func TestBridgeLazySerializationAndFlushCoalescing(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	histID, err := st.SaveActive(store.Session{Page: store.PageWash, Prompts: "测试历史"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ArchiveSession(histID); err != nil {
		t.Fatal(err)
	}

	bm := browser.NewManager()
	eng := tasks.NewEngine(st, bm, nil, dir)
	defer eng.Shutdown()

	a := NewApp(st, bm, eng, dir)
	SetTestApp(a)
	defer SetTestApp(nil)

	for i := range 10 {
		a.sources = append(a.sources, sourceImage{Path: filepath.Join(dir, fmt.Sprintf("img_%02d.png", i))})
	}

	// Workbench bridge caps preview Sources at 6, skips history query and gallery when closed
	wbClosed := getWorkbenchStateBridge()
	if len(wbClosed.Sources) != 6 || wbClosed.SourcesTotal != 10 {
		t.Fatalf("sources = %d total=%d, want 6 / 10", len(wbClosed.Sources), wbClosed.SourcesTotal)
	}
	if len(wbClosed.Sessions) != 0 {
		t.Fatalf("expected Sessions empty when history dialog closed, got %d", len(wbClosed.Sessions))
	}
	if len(wbClosed.GalleryItems) != 0 {
		t.Fatalf("expected GalleryItems empty when gallery modal closed, got %d", len(wbClosed.GalleryItems))
	}

	setHistoryOpenBridge(true)
	setGalleryOpenBridge(true)
	wbOpen := getWorkbenchStateBridge()
	if len(wbOpen.Sessions) == 0 {
		t.Fatal("expected Sessions populated when history dialog open")
	}
	if len(wbOpen.GalleryItems) != 10 {
		t.Fatalf("GalleryItems = %d, want 10 when gallery open", len(wbOpen.GalleryItems))
	}

	// Logs bridge skips filtering/entry allocation while closed
	logging.Log.Info("bridge lazy log check")
	logsClosed := getLogsStateBridge()
	if logsClosed.Open || len(logsClosed.Entries) != 0 || logsClosed.Total == 0 {
		t.Fatalf("closed logs bridge unexpected: %+v", logsClosed)
	}
	setLogOpenBridge(true)
	logsOpen := getLogsStateBridge()
	if !logsOpen.Open || len(logsOpen.Entries) == 0 {
		t.Fatalf("open logs bridge unexpected: %+v", logsOpen)
	}

	// flushUIUpdates avoids emitting "all" if a queued callback already emitted a topic
	_, ch := a.subscribeEvents()
	a.postUI(func() {
		a.emit("workbench")
	})
	a.flushUIUpdates()
	var got []string
	for {
		select {
		case topic := <-ch:
			got = append(got, topic)
		default:
			goto drained
		}
	}
drained:
	if !slices.Equal(got, []string{"workbench"}) {
		t.Fatalf("flushUIUpdates emitted %v, want [workbench] without redundant 'all'", got)
	}

	// Bulk enable/disable scopes strictly to the requested platform
	b1, err := st.AddBrowser("豆包", "d1", filepath.Join(dir, "d1"))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := st.AddBrowser("千问", "q1", filepath.Join(dir, "q1"))
	if err != nil {
		t.Fatal(err)
	}
	a.refreshBrowsers()
	setPlatformAccountsDisabledBridge("豆包", true)
	for _, r := range st.Browsers() {
		if r.ID == b1.ID && !r.Disabled {
			t.Fatal("expected 豆包 account to be disabled")
		}
		if r.ID == b2.ID && r.Disabled {
			t.Fatal("expected 千问 account to remain enabled")
		}
	}
	setPlatformAccountsDisabledBridge("豆包", false)
	for _, r := range st.Browsers() {
		if r.ID == b1.ID && r.Disabled {
			t.Fatal("expected 豆包 account to be re-enabled")
		}
	}
}
