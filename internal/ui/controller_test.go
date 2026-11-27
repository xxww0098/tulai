package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

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
	if !a.handleBrowserDrawerKey(BrowserKeyInput{Key: "a"}) || len(drawer.inputs) != 1 {
		t.Fatal("focused browser did not receive the typed character")
	}
	if in := <-drawer.inputs; in.Kind != "text" || in.Text != "a" {
		t.Fatalf("unexpected browser text input: %+v", in)
	}
	if !a.handleBrowserDrawerKey(BrowserKeyInput{Key: "Escape"}) || a.browserDrawer != nil || len(drawer.inputs) != 0 {
		t.Fatal("Escape should close the focused drawer without forwarding a key to the webpage")
	}
}

func TestBrowserDrawerMouseDragButtonsAndAdaptiveInterval(t *testing.T) {
	drawer := &browserDrawerState{
		row:    store.Browser{ID: 11},
		inputs: make(chan cdp.PageInput, 8),
	}
	drawer.held.Store(true)
	a := &App{browserDrawer: drawer}

	if got := a.drawerFrameInterval(drawer); got != drawerIdleInterval {
		t.Fatalf("idle frame interval = %v, want %v", got, drawerIdleInterval)
	}
	// Press with omitted buttons defaults to left-button bitmask (1)
	if !a.handleBrowserDrawerInput(BrowserInputDTO{Kind: "mousePressed", Button: "left", X: 100, Y: 200}) {
		t.Fatal("mousePressed was rejected")
	}
	if !a.handleBrowserDrawerInput(BrowserInputDTO{Kind: "mouseMoved", Buttons: 1, X: 180, Y: 200}) {
		t.Fatal("mouseMoved during drag was rejected")
	}
	if !a.handleBrowserDrawerInput(BrowserInputDTO{Kind: "mouseReleased", Button: "left", Buttons: 0, X: 180, Y: 200}) {
		t.Fatal("mouseReleased was rejected")
	}
	press := <-drawer.inputs
	move := <-drawer.inputs
	rel := <-drawer.inputs
	if press.Buttons != 1 || press.Button != "left" {
		t.Fatalf("press input = %+v, want buttons=1 left", press)
	}
	if move.Buttons != 1 || move.X != 180 {
		t.Fatalf("move input = %+v, want buttons=1 x=180", move)
	}
	if rel.Buttons != 0 || rel.Button != "left" {
		t.Fatalf("release input = %+v, want buttons=0 left", rel)
	}

	drawer.lastInput.Store(time.Now().UnixNano())
	if got := a.drawerFrameInterval(drawer); got != drawerFastInterval {
		t.Fatalf("active frame interval = %v, want %v", got, drawerFastInterval)
	}
}

func TestPlatformSharedExitHint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	q1, _ := st.AddBrowser("千问", "千问1", "")
	q2, _ := st.AddBrowser("千问", "千问2", "")
	st.UpdateBrowser(q1.ID, false, true)
	st.UpdateBrowser(q2.ID, false, true)

	a := NewApp(st, nil, nil, "")
	a.refreshBrowsers()
	br := a.buildBrowserState()
	if len(br.SharedExitHints) != 1 || !strings.Contains(br.SharedExitHints[0].Message, "2 个千问账号共用本机直连出口") {
		t.Fatalf("expected direct shared exit hint, got %+v", br.SharedExitHints)
	}

	_ = st.SetBrowserProxy(q1.ID, "http://10.0.0.1:8080")
	_ = st.SetBrowserProxy(q2.ID, "http://10.0.0.1:8080")
	a.refreshBrowsers()
	br = a.buildBrowserState()
	if len(br.SharedExitHints) != 1 || !strings.Contains(br.SharedExitHints[0].Message, "2 个千问账号共用同一代理出口") {
		t.Fatalf("expected same-proxy shared exit hint, got %+v", br.SharedExitHints)
	}

	_ = st.SetBrowserProxy(q2.ID, "http://10.0.0.2:8080")
	a.refreshBrowsers()
	br = a.buildBrowserState()
	if len(br.SharedExitHints) != 0 {
		t.Fatalf("distinct proxies should have empty SharedExitHints, got %+v", br.SharedExitHints)
	}
}

func TestDismissSharedExitHintsPersistsAndReappearsOnChange(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	d1, _ := st.AddBrowser("豆包", "豆包1", "")
	d2, _ := st.AddBrowser("豆包", "豆包2", "")
	q1, _ := st.AddBrowser("千问", "千问1", "")
	q2, _ := st.AddBrowser("千问", "千问2", "")
	for _, id := range []int64{d1.ID, d2.ID, q1.ID, q2.ID} {
		st.UpdateBrowser(id, false, true)
	}

	a := NewApp(st, nil, nil, "")
	a.refreshBrowsers()
	br := a.buildBrowserState()
	if len(br.SharedExitHints) != 2 {
		t.Fatalf("expected 2 shared exit hints initially, got %+v", br.SharedExitHints)
	}

	// Dismissing only 千问 hides 千问 while keeping 豆包 visible.
	a.dismissSharedExitHints([]string{"千问", "未知平台"})
	br = a.buildBrowserState()
	if len(br.SharedExitHints) != 1 || br.SharedExitHints[0].Platform != "豆包" {
		t.Fatalf("expected only 豆包 hint to remain, got %+v", br.SharedExitHints)
	}

	// Persists across app restart.
	restarted := NewApp(st, nil, nil, "")
	restarted.loadSettings()
	br = restarted.buildBrowserState()
	if len(br.SharedExitHints) != 1 || br.SharedExitHints[0].Platform != "豆包" {
		t.Fatalf("expected dismissed 千问 hint to persist across restart, got %+v", br.SharedExitHints)
	}

	// Changing the shared-exit condition for 千问 (adding a 3rd account) re-shows the updated hint.
	q3, _ := st.AddBrowser("千问", "千问3", "")
	st.UpdateBrowser(q3.ID, false, true)
	restarted.refreshBrowsers()
	br = restarted.buildBrowserState()
	if len(br.SharedExitHints) != 2 {
		t.Fatalf("expected updated 千问 hint to reappear when count changes, got %+v", br.SharedExitHints)
	}
}

func TestWashWorkbenchBatchesFreshSourcesAndIsolatesModerationRetries(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	account, err := st.AddBrowser("豆包", "测试", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(account.ID, false, true)
	engine := tasks.NewEngine(st, nil, nil, dir)
	defer engine.Shutdown()
	a := &App{tab: pageWash, statePage: pageWash, dailyCap: 50, dailyCapSet: true}
	for i := 1; i <= 23; i++ {
		a.sources = append(a.sources, sourceImage{Path: fmt.Sprintf("source-%02d.png", i)})
	}
	options := func() tasks.Options {
		return a.taskOptions([]string{"豆包"}, a.taskPromptList(), a.washPendingPaths())
	}
	var batches [][]string
	err = engine.RunObserved(t.Context(), options(), func(tasks.Task) {}, func(tasks.Progress) {}, func(_ context.Context, task *tasks.Task) error {
		batches = append(batches, slices.Clone(task.Images))
		return nil
	})
	want := slices.Collect(slices.Chunk(a.washPendingPaths(), 10))
	if err != nil || !slices.EqualFunc(batches, want, slices.Equal[[]string]) {
		t.Fatalf("dispatched %v, want 10 + 10 + 3 in input order, err=%v", batches, err)
	}
	rejection := "审核不通过"
	a.sources[2].Washed = true
	a.queue = []tasks.Task{
		{Images: []string{a.sources[0].Path}, Status: tasks.StatusFailed, Error: rejection},
		{Images: []string{a.sources[1].Path}, Status: tasks.StatusFailed, Error: rejection},
		{Images: []string{a.sources[1].Path}, Status: tasks.StatusFailed, Error: "出图超时"},
		{Images: []string{a.sources[2].Path}, Status: tasks.StatusFailed, Error: rejection},
	}
	if got := options().SoloImages; !slices.Equal(got, []string{a.sources[0].Path}) {
		t.Fatalf("individual retries = %v, want only %s", got, a.sources[0].Path)
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
	setSettingFieldBridge("concurrencyAuto", "true")
	setSettingFieldBridge("concurrencyMax", "4")
	setSettingFieldBridge("removeWatermark", "true")
	setSettingFieldBridge("proxyCustom", "true")
	setSettingFieldBridge("proxyURL", "http://127.0.0.1:7890")
	setSettingFieldBridge("dotMatrixEffect", "snowflakes")
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
	if !reloaded.concurrencyAuto || int(reloaded.concurrencyMax) != 4 || reloaded.taskConcurrency() != 4 {
		t.Fatalf("concurrency = (%v, %v, task=%d)", reloaded.concurrencyAuto, reloaded.concurrencyMax, reloaded.taskConcurrency())
	}
	if !reloaded.removeWatermark {
		t.Fatal("removeWatermark = false, want true")
	}
	if !reloaded.proxyCustom || reloaded.proxyURL != "http://127.0.0.1:7890" {
		t.Fatalf("proxy = (%v, %q)", reloaded.proxyCustom, reloaded.proxyURL)
	}
	if reloaded.resolvedDotMatrixEffect() != "snowflakes" {
		t.Fatalf("dotMatrixEffect = %q, want snowflakes", reloaded.resolvedDotMatrixEffect())
	}

	SetTestApp(reloaded)
	s := getSettingsStateBridge()
	if s.DailyCapDoubao != 15 || s.ConcurrencyMax != 4 || !s.RemoveWatermark || !s.ProxyCustom || s.ProxyURL != "http://127.0.0.1:7890" || s.DotMatrixEffect != "snowflakes" {
		t.Fatalf("settings DTO mismatch: %+v", s)
	}
	sh := getShellStateBridge()
	if sh.DotMatrixEffect != "snowflakes" {
		t.Fatalf("shell DTO dotMatrixEffect = %q, want snowflakes", sh.DotMatrixEffect)
	}
	setSettingFieldBridge("dotMatrixEffect", "invalid-pattern")
	if got := getSettingsStateBridge().DotMatrixEffect; got != "grain" {
		t.Fatalf("invalid dotMatrixEffect normalized to %q, want grain", got)
	}
}

func TestPerPlatformConcurrencyIndependentPersistenceAndTaskOptions(t *testing.T) {
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

	// Doubao OFF (max 3), Qwen ON (max 6)
	setSettingFieldBridge("concurrencyAutoDoubao", "false")
	setSettingFieldBridge("concurrencyMaxDoubao", "3")
	setSettingFieldBridge("concurrencyAutoQianwen", "true")
	setSettingFieldBridge("concurrencyMaxQianwen", "6")

	reloaded := NewApp(st, bm, eng, dir)
	if reloaded.concurrencyAutoFor("豆包") || reloaded.concurrencyMaxFor("豆包") != 3 || reloaded.taskConcurrencyFor("豆包") != 1 {
		t.Fatalf("doubao concurrency = (auto=%v, max=%d, task=%d)", reloaded.concurrencyAutoFor("豆包"), reloaded.concurrencyMaxFor("豆包"), reloaded.taskConcurrencyFor("豆包"))
	}
	if !reloaded.concurrencyAutoFor("千问") || reloaded.concurrencyMaxFor("千问") != 6 || reloaded.taskConcurrencyFor("千问") != 6 {
		t.Fatalf("qianwen concurrency = (auto=%v, max=%d, task=%d)", reloaded.concurrencyAutoFor("千问"), reloaded.concurrencyMaxFor("千问"), reloaded.taskConcurrencyFor("千问"))
	}

	if got := reloaded.taskOptions([]string{"豆包"}, []string{"猫"}, nil).Concurrency; got != 1 {
		t.Fatalf("doubao taskOptions Concurrency = %d, want 1", got)
	}
	if got := reloaded.taskOptions([]string{"千问"}, []string{"猫"}, nil).Concurrency; got != 6 {
		t.Fatalf("qianwen taskOptions Concurrency = %d, want 6", got)
	}

	// Flipping Qwen OFF does not affect Doubao ON
	SetTestApp(reloaded)
	setSettingFieldBridge("concurrencyAutoDoubao", "true")
	setSettingFieldBridge("concurrencyAutoQianwen", "false")
	reloaded2 := NewApp(st, bm, eng, dir)
	if !reloaded2.concurrencyAutoFor("豆包") || reloaded2.taskConcurrencyFor("豆包") != 3 {
		t.Fatalf("doubao after flip = (auto=%v, task=%d), want (true, 3)", reloaded2.concurrencyAutoFor("豆包"), reloaded2.taskConcurrencyFor("豆包"))
	}
	if reloaded2.concurrencyAutoFor("千问") || reloaded2.taskConcurrencyFor("千问") != 1 {
		t.Fatalf("qianwen after flip = (auto=%v, task=%d), want (false, 1)", reloaded2.concurrencyAutoFor("千问"), reloaded2.taskConcurrencyFor("千问"))
	}
}

func TestLiveConcurrencySettingUpdatesRunningEngine(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for i := range 3 {
		b, err := st.AddBrowser("豆包", fmt.Sprintf("豆包%d", i+1), "")
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetBrowserProxy(b.ID, fmt.Sprintf("http://10.0.0.%d:8080", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	bm := browser.NewManager()
	eng := tasks.NewEngine(st, bm, nil, dir)
	defer eng.Shutdown()

	a := NewApp(st, bm, eng, dir)
	SetTestApp(a)
	defer SetTestApp(nil)

	started := make(chan int64, 6)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- eng.RunObserved(context.Background(), tasks.Options{
			Platforms:   []string{"豆包"},
			Images:      []string{"1.png", "2.png", "3.png", "4.png"},
			Concurrency: 1,
		}, func(tasks.Task) {}, func(tasks.Progress) {}, func(ctx context.Context, task *tasks.Task) error {
			started <- task.BrowserID
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		close(release)
		t.Fatal("initial task did not start")
	}
	select {
	case extra := <-started:
		close(release)
		t.Fatalf("Concurrency=1 started extra task on account %d", extra)
	case <-time.After(100 * time.Millisecond):
	}

	// 运行中在 UI 设置开启自动并发并把豆包上限调到 3：应立刻唤醒引擎再起 2 个并发任务。
	setSettingFieldBridge("concurrencyAutoDoubao", "true")
	setSettingFieldBridge("concurrencyMaxDoubao", "3")
	for i := range 2 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatalf("live concurrency scale-up only started %d/2 additional tasks", i)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestDoubaoAndQianwenWorkspacesDoNotShareSourcesOrResults(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	bm := browser.NewManager()
	eng := tasks.NewEngine(st, bm, nil, dir)
	defer eng.Shutdown()

	validPNG := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
		0x0c, 0x49, 0x44, 0x41, 0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xdd, 0x8d, 0xb0, 0x00, 0x00, 0x00,
		0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
	doubaoSrc := filepath.Join(dir, "doubao_src.png")
	qwenSrc := filepath.Join(dir, "qwen_src.png")
	doubaoOut := filepath.Join(dir, "doubao_out.png")
	qwenOut := filepath.Join(dir, "qwen_out.png")
	for _, p := range []string{doubaoSrc, qwenSrc, doubaoOut, qwenOut} {
		if err := os.WriteFile(p, validPNG, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	a := NewApp(st, bm, eng, dir)
	SetTestApp(a)
	defer SetTestApp(nil)

	// Configure Doubao wash sources, prompt, and completed result.
	setPlatformSelectedBridge("豆包", true)
	setWorkbenchFieldBridge("mode", "wash")
	a.ingestSources([]string{doubaoSrc})
	setWorkbenchFieldBridge("prompts", "豆包专属洗图要求")
	a.queue = []tasks.Task{{
		Index:    1,
		Platform: "豆包",
		Prompt:   "豆包专属洗图要求",
		Images:   []string{doubaoSrc},
		Status:   tasks.StatusSuccess,
		Results:  []string{doubaoOut},
	}}
	a.applyWashQueue(a.queue)
	a.persistWorkspace(pageWash)

	// Switch to Qwen: must have clean independent workspace (no Doubao sources/results/prompts).
	setPlatformSelectedBridge("千问", true)
	wbQ := getWorkbenchStateBridge()
	if !wbQ.Qianwen || wbQ.SourcesTotal != 0 || wbQ.DoneCount != 0 || wbQ.TotalCount != 0 {
		t.Fatalf("qianwen inherited doubao sources/results: sources=%d done=%d total=%d", wbQ.SourcesTotal, wbQ.DoneCount, wbQ.TotalCount)
	}
	if wbQ.Prompts != DefaultWashPrompt {
		t.Fatalf("qianwen wash prompt = %q, want DefaultWashPrompt", wbQ.Prompts)
	}

	// Add independent Qwen wash source, prompt, and completed result, plus Qwen free-gen source.
	a.ingestSources([]string{qwenSrc})
	setWorkbenchFieldBridge("prompts", "千问专属洗图要求")
	a.queue = []tasks.Task{{
		Index:    1,
		Platform: "千问",
		Prompt:   "千问专属洗图要求",
		Images:   []string{qwenSrc},
		Status:   tasks.StatusSuccess,
		Results:  []string{qwenOut},
	}}
	a.applyWashQueue(a.queue)
	a.persistWorkspace(pageWash)

	setWorkbenchFieldBridge("mode", "free")
	a.ingestSources([]string{qwenSrc})
	if got := getWorkbenchStateBridge(); got.BatchMode != batchImage || got.SourcesTotal != 1 {
		t.Fatalf("qianwen free-gen source/batchMode unexpected: batch=%d sources=%d", got.BatchMode, got.SourcesTotal)
	}

	// Switch back to Doubao: free-gen must remain clean, wash must retain Doubao-only items.
	setPlatformSelectedBridge("豆包", true)
	if wbDFree := getWorkbenchStateBridge(); wbDFree.SourcesTotal != 0 || wbDFree.BatchMode != batchText {
		t.Fatalf("doubao free-gen polluted by qianwen: sources=%d batch=%d", wbDFree.SourcesTotal, wbDFree.BatchMode)
	}
	setWorkbenchFieldBridge("mode", "wash")
	wbD := getWorkbenchStateBridge()
	if wbD.SourcesTotal != 1 || wbD.Sources[0].Path != doubaoSrc || wbD.DoneCount != 1 || wbD.DoneTiles[0].Path != doubaoOut || wbD.Prompts != "豆包专属洗图要求" {
		t.Fatalf("doubao wash state altered: sources=%+v done=%+v prompt=%q", wbD.Sources, wbD.DoneTiles, wbD.Prompts)
	}

	// Clearing Doubao results must not touch Qwen results.
	clearWorkbenchResultsBridge()
	if wbDCleared := getWorkbenchStateBridge(); wbDCleared.DoneCount != 0 || wbDCleared.TotalCount != 0 {
		t.Fatalf("doubao results not cleared: done=%d total=%d", wbDCleared.DoneCount, wbDCleared.TotalCount)
	}

	setPlatformSelectedBridge("千问", true)
	wbQAfter := getWorkbenchStateBridge()
	if wbQAfter.SourcesTotal != 1 || wbQAfter.Sources[0].Path != qwenSrc || wbQAfter.DoneCount != 1 || wbQAfter.DoneTiles[0].Path != qwenOut || wbQAfter.Prompts != "千问专属洗图要求" {
		t.Fatalf("qianwen wash state mutated by doubao clear: sources=%+v done=%+v prompt=%q", wbQAfter.Sources, wbQAfter.DoneTiles, wbQAfter.Prompts)
	}

	// Persist & restart: both platforms reload their own independent SQLite sessions.
	a.saveSettings()
	reloaded := NewApp(st, bm, eng, dir)
	SetTestApp(reloaded)
	wbQReload := getWorkbenchStateBridge()
	if !wbQReload.Qianwen || wbQReload.SourcesTotal != 1 || wbQReload.Sources[0].Path != qwenSrc || wbQReload.DoneCount != 1 || wbQReload.DoneTiles[0].Path != qwenOut || wbQReload.Prompts != "千问专属洗图要求" {
		t.Fatalf("reloaded qianwen workspace mismatch: sources=%+v done=%+v prompt=%q", wbQReload.Sources, wbQReload.DoneTiles, wbQReload.Prompts)
	}
	setPlatformSelectedBridge("豆包", true)
	wbDReload := getWorkbenchStateBridge()
	if !wbDReload.Doubao || wbDReload.SourcesTotal != 1 || wbDReload.Sources[0].Path != doubaoSrc || wbDReload.DoneCount != 0 || wbDReload.Prompts != "豆包专属洗图要求" {
		t.Fatalf("reloaded doubao workspace mismatch: sources=%+v done=%+v prompt=%q", wbDReload.Sources, wbDReload.DoneTiles, wbDReload.Prompts)
	}
}

func TestTaskOptionsRemoveWatermarkSharedAcrossAllModes(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, tc := range []struct {
			name string
			page int
		}{
			{name: "wash", page: pageWash},
			{name: "free", page: pageHome},
			{name: "fission", page: pageFission},
		} {
			a := &App{tab: tc.page, statePage: tc.page, removeWatermark: enabled}
			opts := a.taskOptions([]string{"千问"}, []string{"测试"}, nil)
			if opts.RemoveWatermark != enabled {
				t.Fatalf("mode=%s enabled=%v: opts.RemoveWatermark = %v, want %v", tc.name, enabled, opts.RemoveWatermark, enabled)
			}
		}
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

	// Switch across the remaining providers; exactly one remains active.
	// Removed providers are ignored and do not clear the current choice.
	setPlatformSelectedBridge("ChatGPT", true)
	if !slices.Equal(a.selectedPlatforms(), []string{"千问"}) {
		t.Fatalf("removed provider altered selection: %v", a.selectedPlatforms())
	}
	for _, want := range []string{"豆包", "千问"} {
		setPlatformSelectedBridge(want, true)
		if got := a.selectedPlatforms(); !slices.Equal(got, []string{want}) {
			t.Fatalf("after selecting %q: selectedPlatforms = %v", want, got)
		}
		if got := a.activePlatforms(); !slices.Equal(got, []string{want}) {
			t.Fatalf("after selecting %q: activePlatforms = %v", want, got)
		}
		wb := getWorkbenchStateBridge()
		activeCount := 0
		for _, on := range []bool{wb.Doubao, wb.Qianwen} {
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

	bm := browser.NewManager()
	eng := tasks.NewEngine(st, bm, nil, dir)
	defer eng.Shutdown()

	a := NewApp(st, bm, eng, dir)
	SetTestApp(a)
	defer SetTestApp(nil)

	for i := range 30 {
		a.sources = append(a.sources, sourceImage{Path: filepath.Join(dir, fmt.Sprintf("img_%02d.png", i))})
	}

	// Workbench bridge caps preview Sources at 24 and skips the gallery while it is closed.
	wbClosed := getWorkbenchStateBridge()
	if len(wbClosed.Sources) != 24 || wbClosed.SourcesTotal != 30 {
		t.Fatalf("sources = %d total=%d, want 24 / 30", len(wbClosed.Sources), wbClosed.SourcesTotal)
	}
	if len(wbClosed.GalleryItems) != 0 {
		t.Fatalf("expected GalleryItems empty when gallery modal closed, got %d", len(wbClosed.GalleryItems))
	}

	setGalleryOpenBridge(true)
	wbOpen := getWorkbenchStateBridge()
	if len(wbOpen.GalleryItems) != 30 {
		t.Fatalf("GalleryItems = %d, want 30 when gallery open", len(wbOpen.GalleryItems))
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

func TestClearWashedWorkbenchSourcesBridge(t *testing.T) {
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

	a.tab = pageWash
	a.statePage = pageWash
	a.sources = make([]sourceImage, 8)
	for i := range a.sources {
		a.sources[i] = sourceImage{
			Path:   filepath.Join(dir, fmt.Sprintf("img_%02d.png", i)),
			Washed: i == 0 || i == 2 || i == 7,
		}
	}
	a.srcSel = 3
	a.gallerySelected = map[string]bool{
		a.sources[0].Path: true,
		a.sources[2].Path: true,
		a.sources[3].Path: true,
		a.sources[7].Path: true,
	}

	wbBefore := getWorkbenchStateBridge()
	if wbBefore.WashedCount != 3 || wbBefore.SourcesTotal != 8 {
		t.Fatalf("before clear: washed=%d total=%d, want 3 / 8", wbBefore.WashedCount, wbBefore.SourcesTotal)
	}

	// No-op while tasks are running.
	a.running = true
	clearWashedWorkbenchSourcesBridge()
	if len(a.sources) != 8 {
		t.Fatalf("clearWashedWorkbenchSourcesBridge modified sources while running: len=%d", len(a.sources))
	}

	a.running = false
	clearWashedWorkbenchSourcesBridge()
	if len(a.sources) != 5 {
		t.Fatalf("after clear: len(sources) = %d, want 5", len(a.sources))
	}
	for i, s := range a.sources {
		if s.Washed {
			t.Fatalf("remaining source[%d] unexpectedly marked washed: %+v", i, s)
		}
	}
	if a.srcSel != 1 {
		t.Fatalf("srcSel = %d, want 1 after removing earlier washed sources", a.srcSel)
	}
	if len(a.gallerySelected) != 1 || !a.gallerySelected[filepath.Join(dir, "img_03.png")] {
		t.Fatalf("gallerySelected = %+v, want only unwashed selected path", a.gallerySelected)
	}

	wbAfter := getWorkbenchStateBridge()
	if wbAfter.WashedCount != 0 || wbAfter.SourcesTotal != 5 {
		t.Fatalf("after clear: washed=%d total=%d, want 0 / 5", wbAfter.WashedCount, wbAfter.SourcesTotal)
	}

	sess, _, found, err := st.ActiveSession(store.PageWash)
	if err != nil || !found {
		t.Fatalf("expected active wash session persisted: found=%v err=%v", found, err)
	}
	if len(sess.Sources) != 5 {
		t.Fatalf("persisted session sources = %d, want 5", len(sess.Sources))
	}
}

func TestCoalescedRunWorkspaceUpdatesBatchSQLiteWritesAndTimerCancel(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	a := NewApp(st, nil, nil, dir)
	a.prompts = "洗图批处理"
	_, ch := a.subscribeEvents()

	for i := range 10 {
		idx := i + 1
		a.postUI(func() {
			a.applyWashTask(tasks.Task{Index: idx, Platform: "豆包", Status: tasks.StatusSuccess})
			a.scheduleRunWorkspaceUpdate(pageHome)
		})
		a.postUI(func() {
			a.progress = float64(idx) / 10.0
			a.progText = fmt.Sprintf("%d / 10", idx)
			a.scheduleRunWorkspaceUpdate(pageHome)
		})
	}
	a.flushUIUpdates()

	if a.workspaceWrites != 1 {
		t.Fatalf("expected 20 burst task/progress callbacks to coalesce into 1 SQLite write, got %d", a.workspaceWrites)
	}
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
		t.Fatalf("coalesced flush emitted %v, want single [workbench] without spurious 'all'", got)
	}

	// 验证定时器路径在 cancelRunWorkspaceUpdate 后不会触发过期写盘
	prevWindow := runPersistCoalesceWindow
	runPersistCoalesceWindow = 10 * time.Millisecond
	defer func() { runPersistCoalesceWindow = prevWindow }()

	a.scheduleRunWorkspaceUpdate(pageHome)
	a.ensureRunPersistTimer()
	a.cancelRunWorkspaceUpdate()
	time.Sleep(35 * time.Millisecond)
	a.flushUIUpdates()
	if a.workspaceWrites != 1 {
		t.Fatalf("canceled coalesce timer must not trigger extra write, got %d", a.workspaceWrites)
	}

	// 验证定时器自然到期时能落盘一次
	a.scheduleRunWorkspaceUpdate(pageHome)
	a.ensureRunPersistTimer()
	time.Sleep(35 * time.Millisecond)
	a.flushUIUpdates()
	if a.workspaceWrites != 2 {
		t.Fatalf("fired coalesce timer must persist once, got %d", a.workspaceWrites)
	}
}

func TestShellStateBridgeReportsActivePlatformAndReadyCounts(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	b1, err := st.AddBrowser("豆包", "d1", filepath.Join(dir, "d1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(b1.ID)
	b2, err := st.AddBrowser("千问", "q1", filepath.Join(dir, "q1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(b2.ID)
	if _, err := st.AddBrowser("千问", "q2-unlogged", filepath.Join(dir, "q2")); err != nil {
		t.Fatal(err)
	}
	b3, err := st.AddBrowser("千问国际", "qi1", filepath.Join(dir, "qi1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(b3.ID)
	b4, err := st.AddBrowser("Gemini", "g1", filepath.Join(dir, "g1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(b4.ID)

	eng := tasks.NewEngine(st, browser.NewManager(), nil, dir)
	defer eng.Shutdown()
	a := NewApp(st, nil, eng, dir)
	SetTestApp(a)
	defer SetTestApp(nil)

	sh := getShellStateBridge()
	if sh.Platform != "豆包" || len(sh.Platforms) != 4 {
		t.Fatalf("unexpected shell platform state: %+v", sh)
	}
	if sh.Platforms[0].ID != "doubao" || sh.Platforms[0].Ready != 1 || sh.Platforms[0].Total != 1 {
		t.Fatalf("unexpected doubao nav summary: %+v", sh.Platforms[0])
	}
	if sh.Platforms[1].ID != "qianwen" || sh.Platforms[1].Ready != 1 || sh.Platforms[1].Total != 2 {
		t.Fatalf("unexpected qianwen nav summary: %+v", sh.Platforms[1])
	}
	if sh.Platforms[2].ID != "qwen_intl" || sh.Platforms[2].Name != "千问国际" || sh.Platforms[2].Ready != 1 || sh.Platforms[2].Total != 1 {
		t.Fatalf("unexpected qwen_intl nav summary: %+v", sh.Platforms[2])
	}
	if sh.Platforms[3].ID != "gemini" || sh.Platforms[3].Name != "Gemini" || sh.Platforms[3].Ready != 1 || sh.Platforms[3].Total != 1 {
		t.Fatalf("unexpected gemini nav summary: %+v", sh.Platforms[3])
	}
	_, ch := a.subscribeEvents()
	setPlatformSelectedBridge("千问", true)
	sh2 := getShellStateBridge()
	if sh2.Platform != "千问" {
		t.Fatalf("shell Platform after switch = %q, want 千问", sh2.Platform)
	}
	setPlatformSelectedBridge("千问国际", true)
	sh3 := getShellStateBridge()
	if sh3.Platform != "千问国际" {
		t.Fatalf("shell Platform after switch = %q, want 千问国际", sh3.Platform)
	}
	wb := getWorkbenchStateBridge()
	if wb.Platform != "千问国际" || !wb.QwenIntl || wb.Qianwen || wb.Doubao || wb.Gemini {
		t.Fatalf("unexpected workbench state for 千问国际: platform=%q doubao=%v qianwen=%v qwenIntl=%v gemini=%v", wb.Platform, wb.Doubao, wb.Qianwen, wb.QwenIntl, wb.Gemini)
	}
	setPlatformSelectedBridge("Gemini", true)
	sh4 := getShellStateBridge()
	if sh4.Platform != "Gemini" {
		t.Fatalf("shell Platform after switch = %q, want Gemini", sh4.Platform)
	}
	wbGem := getWorkbenchStateBridge()
	if wbGem.Platform != "Gemini" || !wbGem.Gemini || wbGem.QwenIntl || wbGem.Qianwen || wbGem.Doubao {
		t.Fatalf("unexpected workbench state for Gemini: platform=%q doubao=%v qianwen=%v qwenIntl=%v gemini=%v", wbGem.Platform, wbGem.Doubao, wbGem.Qianwen, wbGem.QwenIntl, wbGem.Gemini)
	}
	var topics []string
	for {
		select {
		case topic := <-ch:
			topics = append(topics, topic)
		default:
			goto drained
		}
	}
drained:
	if !slices.Contains(topics, "shell") || !slices.Contains(topics, "workbench") {
		t.Fatalf("setPlatformSelectedBridge emitted %v, want workbench + shell", topics)
	}
}

func TestQwenIntlSettingsAndWorkspaceStayIsolatedFromDomesticQianwen(t *testing.T) {
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

	// Configure distinct caps and concurrency for 千问 vs 千问国际.
	setSettingFieldBridge("dailyCapQianwen", "25")
	setSettingFieldBridge("dailyCapQwenIntl", "80")
	setSettingFieldBridge("concurrencyAutoQianwen", "false")
	setSettingFieldBridge("concurrencyMaxQianwen", "2")
	setSettingFieldBridge("concurrencyAutoQwenIntl", "true")
	setSettingFieldBridge("concurrencyMaxQwenIntl", "6")

	cfg := getSettingsStateBridge()
	if cfg.DailyCapQianwen != 25 || cfg.DailyCapQwenIntl != 80 {
		t.Fatalf("daily caps = (qianwen=%d, qwenIntl=%d), want (25, 80)", cfg.DailyCapQianwen, cfg.DailyCapQwenIntl)
	}

	// Switch to 千问 and write prompt + output ratio, then switch to 千问国际 and verify independence.
	wbSvc := &WorkbenchService{app: a}
	setPlatformSelectedBridge("千问", true)
	setWorkbenchFieldBridge("prompts", "国内千问提示词")
	wbSvc.SetOutputSize(context.Background(), "", "ratio", "3:4")
	wbDomestic := getWorkbenchStateBridge()
	if wbDomestic.Platform != "千问" || wbDomestic.ConcurrencyAuto || wbDomestic.ConcurrencyMax != 2 {
		t.Fatalf("unexpected domestic qianwen workbench: %+v", wbDomestic)
	}

	setPlatformSelectedBridge("千问国际", true)
	wbIntl := getWorkbenchStateBridge()
	if wbIntl.Platform != "千问国际" || !wbIntl.QwenIntl || wbIntl.Qianwen {
		t.Fatalf("unexpected qwen intl workbench platform flags: %+v", wbIntl)
	}
	if wbIntl.Prompts == "国内千问提示词" {
		t.Fatalf("qwen intl workspace leaked domestic qianwen prompt: %q", wbIntl.Prompts)
	}
	if !wbIntl.ConcurrencyAuto || wbIntl.ConcurrencyMax != 6 {
		t.Fatalf("qwen intl concurrency = (auto=%v, max=%d), want (true, 6)", wbIntl.ConcurrencyAuto, wbIntl.ConcurrencyMax)
	}
	setWorkbenchFieldBridge("prompts", "国际站专属提示词")
	wbSvc.SetOutputSize(context.Background(), "", "ratio", "16:9")

	// Switching back to domestic 千问 must retain its own workspace & output ratio.
	setPlatformSelectedBridge("千问", true)
	wbBack := getWorkbenchStateBridge()
	if wbBack.Prompts != "国内千问提示词" || wbBack.OutputRatio != "3:4" {
		t.Fatalf("domestic qianwen workspace overwritten: prompts=%q ratio=%q", wbBack.Prompts, wbBack.OutputRatio)
	}
	optsDomestic := a.taskOptions([]string{"千问"}, nil, nil)
	optsIntl := a.taskOptions([]string{"千问国际"}, nil, nil)
	if optsDomestic.OutputSize.Ratio != "3:4" || optsIntl.OutputSize.Ratio != "16:9" {
		t.Fatalf("taskOptions OutputSize = (qianwen=%q, qwenIntl=%q), want (3:4, 16:9)", optsDomestic.OutputSize.Ratio, optsIntl.OutputSize.Ratio)
	}
	if optsIntl.DailyCaps["千问"] != 25 || optsIntl.DailyCaps["千问国际"] != 80 {
		t.Fatalf("taskOptions DailyCaps = %+v, want 千问=25 and 千问国际=80", optsIntl.DailyCaps)
	}
}

func TestGeminiSettingsWorkspaceAndRatiosStayIsolated(t *testing.T) {
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

	setSettingFieldBridge("dailyCapGemini", "65")
	setSettingFieldBridge("concurrencyAutoGemini", "true")
	setSettingFieldBridge("concurrencyMaxGemini", "5")

	cfg := getSettingsStateBridge()
	if cfg.DailyCapGemini != 65 || !cfg.ConcurrencyAutoGemini || cfg.ConcurrencyMaxGemini != 5 {
		t.Fatalf("unexpected gemini settings: cap=%d auto=%v max=%d", cfg.DailyCapGemini, cfg.ConcurrencyAutoGemini, cfg.ConcurrencyMaxGemini)
	}

	wbSvc := &WorkbenchService{app: a}
	setPlatformSelectedBridge("豆包", true)
	setWorkbenchFieldBridge("prompts", "豆包提示词")
	wbSvc.SetOutputSize(context.Background(), "", "ratio", "3:2")

	setPlatformSelectedBridge("Gemini", true)
	wbGem := getWorkbenchStateBridge()
	if wbGem.Platform != "Gemini" || !wbGem.Gemini || wbGem.Doubao || wbGem.Qianwen || wbGem.QwenIntl {
		t.Fatalf("unexpected gemini workbench flags: %+v", wbGem)
	}
	if wbGem.Prompts == "豆包提示词" {
		t.Fatalf("gemini workspace leaked doubao prompt: %q", wbGem.Prompts)
	}
	if !wbGem.ConcurrencyAuto || wbGem.ConcurrencyMax != 5 {
		t.Fatalf("gemini concurrency = (auto=%v, max=%d), want (true, 5)", wbGem.ConcurrencyAuto, wbGem.ConcurrencyMax)
	}
	// Gemini only exposes its 5 web-native aspect ratios (no 3:2 or 2:3).
	if len(wbGem.OutputRatios) != 5 || slices.Contains(wbGem.OutputRatios, "3:2") || slices.Contains(wbGem.OutputRatios, "2:3") {
		t.Fatalf("unexpected gemini OutputRatios: %v", wbGem.OutputRatios)
	}
	// Unsupported ratio 3:2 must be rejected for Gemini while supported 9:16 applies cleanly.
	wbSvc.SetOutputSize(context.Background(), "", "ratio", "3:2")
	if got := getWorkbenchStateBridge().OutputRatio; got != "" {
		t.Fatalf("gemini accepted unsupported ratio 3:2: got %q", got)
	}
	wbSvc.SetOutputSize(context.Background(), "", "ratio", "9:16")
	setWorkbenchFieldBridge("prompts", "Gemini专属提示词")

	wbGemAfter := getWorkbenchStateBridge()
	if wbGemAfter.OutputRatio != "9:16" || wbGemAfter.Prompts != "Gemini专属提示词" {
		t.Fatalf("unexpected gemini state after update: ratio=%q prompts=%q", wbGemAfter.OutputRatio, wbGemAfter.Prompts)
	}

	setPlatformSelectedBridge("豆包", true)
	wbDoubao := getWorkbenchStateBridge()
	if wbDoubao.Prompts != "豆包提示词" || wbDoubao.OutputRatio != "3:2" || len(wbDoubao.OutputRatios) != 7 {
		t.Fatalf("doubao workspace affected by gemini: prompts=%q ratio=%q ratios=%v", wbDoubao.Prompts, wbDoubao.OutputRatio, wbDoubao.OutputRatios)
	}

	optsGem := a.taskOptions([]string{"Gemini"}, nil, nil)
	if optsGem.OutputSize.Ratio != "9:16" || optsGem.DailyCaps["Gemini"] != 65 {
		t.Fatalf("gemini taskOptions = (ratio=%q, cap=%d), want (9:16, 65)", optsGem.OutputSize.Ratio, optsGem.DailyCaps["Gemini"])
	}
}

func TestLoadSourceFilesHeaderOnlyAndParallelHash(t *testing.T) {
	dir := t.TempDir()
	var inputs []string
	var wantPaths []string
	var wantHashes []string
	for i := 0; i < 12; i++ {
		p := filepath.Join(dir, fmt.Sprintf("img-%02d.png", i))
		raw := makeTestPNG(t, 16, 16, color.NRGBA{R: uint8(i * 19), G: 80, B: 140, A: 255})
		if err := os.WriteFile(p, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, p)
		wantPaths = append(wantPaths, p)
		sum := sha256.Sum256(raw)
		wantHashes = append(wantHashes, hex.EncodeToString(sum[:]))
	}
	bad := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(bad, []byte("not a real png"), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs = append(inputs, bad)

	fast := loadSourceFilesFor(inputs, false)
	if len(fast) != len(wantPaths) {
		t.Fatalf("header-only loaded %d files, want %d", len(fast), len(wantPaths))
	}
	for i, item := range fast {
		if item.Path != wantPaths[i] || item.Hash != "" {
			t.Fatalf("header-only[%d] = %+v, want path=%s empty hash", i, item, wantPaths[i])
		}
	}

	hashed := loadSourceFilesFor(inputs, true)
	if len(hashed) != len(wantPaths) {
		t.Fatalf("parallel-hash loaded %d files, want %d", len(hashed), len(wantPaths))
	}
	for i, item := range hashed {
		if item.Path != wantPaths[i] || item.Hash != wantHashes[i] {
			t.Fatalf("parallel-hash[%d] = (%s, %s), want (%s, %s)", i, item.Path, item.Hash, wantPaths[i], wantHashes[i])
		}
	}
}
