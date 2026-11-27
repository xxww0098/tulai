package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func setupRewashTestApp(t *testing.T, count int) (*App, *store.Store, []string, []string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	bm := browser.NewManager()
	eng := tasks.NewEngine(st, bm, nil, dir)
	t.Cleanup(eng.Shutdown)

	srcs := make([]string, count)
	outs := make([]string, count)
	var sources []store.SessionSource
	var sessionTasks []store.SessionTask
	for i := range count {
		srcs[i] = filepath.Join(dir, fmt.Sprintf("src_%02d.png", i+1))
		outs[i] = filepath.Join(dir, fmt.Sprintf("out_%02d.png", i+1))
		if err := createEmptyTestFile(srcs[i]); err != nil {
			t.Fatal(err)
		}
		if err := createEmptyTestFile(outs[i]); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, store.SessionSource{Path: srcs[i], Washed: true, Output: outs[i]})
		sessionTasks = append(sessionTasks, store.SessionTask{
			Ord:      i + 1,
			Platform: "豆包",
			Source:   srcs[i],
			Status:   string(tasks.StatusSuccess),
			Outputs:  outs[i],
		})
	}
	if _, err := st.SaveActive(store.Session{
		Page:     store.PageWash,
		Prompts:  "重绘为纯画",
		Progress: 1,
		Sources:  sources,
	}, sessionTasks); err != nil {
		t.Fatal(err)
	}

	a := NewApp(st, bm, eng, dir)
	SetTestApp(a)
	t.Cleanup(func() { SetTestApp(nil) })
	return a, st, srcs, outs
}

func TestRequeueDoneTasksAndUndo(t *testing.T) {
	a, st, srcs, _ := setupRewashTestApp(t, 3)

	wb := getWorkbenchStateBridge()
	if wb.DoneCount != 3 || wb.PendingCount != 0 || wb.WashedCount != 3 || wb.CanUndoRewash {
		t.Fatalf("unexpected initial workbench state: %+v", wb)
	}
	if !wb.DoneTiles[0].CanRewash || wb.DoneTiles[0].RewashCount != 0 {
		t.Fatalf("expected CanRewash=true, RewashCount=0, got %+v", wb.DoneTiles[0])
	}

	// Requeue tasks 1 (with duplicate) and 3
	requeueDoneTasksBridge([]int{1, 1, 3})

	wb = getWorkbenchStateBridge()
	if wb.DoneCount != 1 || wb.PendingCount != 2 || wb.WashedCount != 1 {
		t.Fatalf("after requeue counts: done=%d pending=%d washed=%d", wb.DoneCount, wb.PendingCount, wb.WashedCount)
	}
	if !wb.CanUndoRewash || wb.RewashUndoCount != 2 {
		t.Fatalf("expected undo available for 2 items, got can=%v count=%d", wb.CanUndoRewash, wb.RewashUndoCount)
	}
	if wb.PendingTiles[0].TaskIndex != 1 || wb.PendingTiles[0].RewashCount != 1 ||
		wb.PendingTiles[1].TaskIndex != 3 || wb.PendingTiles[1].RewashCount != 1 {
		t.Fatalf("unexpected pending tiles after requeue: %+v", wb.PendingTiles)
	}

	pendingPaths := a.washPendingPaths()
	if len(pendingPaths) != 2 || pendingPaths[0] != srcs[0] || pendingPaths[1] != srcs[2] {
		t.Fatalf("washPendingPaths = %v, want [%s %s]", pendingPaths, srcs[0], srcs[2])
	}

	// Persisted session reflects re-queued tasks
	sess, items, ok, err := st.ActiveSession(store.PageWash)
	if err != nil || !ok || len(items) != 3 {
		t.Fatalf("persisted session invalid: ok=%v err=%v items=%d", ok, err, len(items))
	}
	if items[0].Status != string(tasks.StatusPending) || items[1].Status != string(tasks.StatusSuccess) || items[2].Status != string(tasks.StatusPending) {
		t.Fatalf("persisted task statuses mismatch: %+v", items)
	}
	if sess.Sources[0].Washed || !sess.Sources[1].Washed || sess.Sources[2].Washed {
		t.Fatalf("persisted source washed flags mismatch: %+v", sess.Sources)
	}

	// Undo restores exact pre-requeue state
	undoRequeueTasksBridge()
	wb = getWorkbenchStateBridge()
	if wb.DoneCount != 3 || wb.PendingCount != 0 || wb.WashedCount != 3 || wb.CanUndoRewash {
		t.Fatalf("after undo counts: done=%d pending=%d washed=%d canUndo=%v", wb.DoneCount, wb.PendingCount, wb.WashedCount, wb.CanUndoRewash)
	}
	if wb.DoneTiles[0].RewashCount != 0 || wb.DoneTiles[2].RewashCount != 0 {
		t.Fatalf("expected RewashCount reset after undo, got %+v", wb.DoneTiles)
	}
}

func TestRequeueAfterClearWashedRestoresSource(t *testing.T) {
	a, _, srcs, _ := setupRewashTestApp(t, 2)

	clearWashedWorkbenchSourcesBridge()
	wb := getWorkbenchStateBridge()
	if wb.SourcesTotal != 0 {
		t.Fatalf("expected 0 sources after clearWashed, got %d", wb.SourcesTotal)
	}

	requeueDoneTasksBridge([]int{1})
	wb = getWorkbenchStateBridge()
	if wb.SourcesTotal != 1 || wb.PendingCount != 1 || wb.DoneCount != 1 {
		t.Fatalf("expected cleared source #1 restored for rewash: sources=%d pending=%d done=%d", wb.SourcesTotal, wb.PendingCount, wb.DoneCount)
	}
	if got := a.washPendingPaths(); len(got) != 1 || got[0] != srcs[0] {
		t.Fatalf("washPendingPaths after restore = %v, want [%s]", got, srcs[0])
	}
}

func TestRequeueIgnoredWhileRunning(t *testing.T) {
	a, _, _, _ := setupRewashTestApp(t, 2)
	a.running = true

	requeueDoneTasksBridge([]int{1})
	wb := getWorkbenchStateBridge()
	if wb.DoneCount != 2 || wb.PendingCount != 0 || wb.CanUndoRewash {
		t.Fatalf("expected requeue to be ignored while running, got done=%d pending=%d undo=%v", wb.DoneCount, wb.PendingCount, wb.CanUndoRewash)
	}
}

func TestClearWorkbenchResultsClearsQueuePreservesFilesAndPersists(t *testing.T) {
	a, st, srcs, outs := setupRewashTestApp(t, 3)

	// Requeue one task and open lightbox preview on a result file to test full cleanup.
	requeueDoneTasksBridge([]int{1})
	a.previewOn = true
	a.previewPath = outs[1]

	clearWorkbenchResultsBridge()

	wb := getWorkbenchStateBridge()
	if wb.DoneCount != 0 || wb.PendingCount != 0 || wb.TotalCount != 0 {
		t.Fatalf("expected empty queue after clear, got done=%d pending=%d total=%d", wb.DoneCount, wb.PendingCount, wb.TotalCount)
	}
	if wb.Progress != 0 || wb.ProgText != "" {
		t.Fatalf("expected progress reset after clear, got progress=%v progText=%q", wb.Progress, wb.ProgText)
	}
	if wb.CanUndoRewash || wb.RewashUndoCount != 0 {
		t.Fatalf("expected rewash undo cleared, got can=%v count=%d", wb.CanUndoRewash, wb.RewashUndoCount)
	}
	if wb.PreviewOn || wb.PreviewPath != "" {
		t.Fatalf("expected preview closed for cleared result, got on=%v path=%q", wb.PreviewOn, wb.PreviewPath)
	}
	if wb.SourcesTotal != 3 {
		t.Fatalf("expected sources preserved after clearing results, got %d", wb.SourcesTotal)
	}
	if a.rewashCounts[filepath.Clean(srcs[0])] != 0 {
		t.Fatalf("expected rewash count cleared for %s, got %d", srcs[0], a.rewashCounts[filepath.Clean(srcs[0])])
	}
	for _, out := range outs {
		if _, err := os.Stat(out); err != nil {
			t.Fatalf("expected local output file preserved (%s): %v", out, err)
		}
	}

	sess, items, ok, err := st.ActiveSession(store.PageWash)
	if err != nil || !ok {
		t.Fatalf("expected persisted wash session, ok=%v err=%v", ok, err)
	}
	if len(items) != 0 || sess.Progress != 0 || sess.ProgText != "" {
		t.Fatalf("expected cleared tasks persisted, got items=%d progress=%v progText=%q", len(items), sess.Progress, sess.ProgText)
	}
	if len(sess.Sources) != 3 {
		t.Fatalf("expected persisted sources preserved, got %d", len(sess.Sources))
	}

	// Reopening app from same SQLite store stays cleared.
	reopened := NewApp(st, a.manager, a.engine, a.dataRoot)
	SetTestApp(reopened)
	wbReopened := getWorkbenchStateBridge()
	if wbReopened.DoneCount != 0 || wbReopened.TotalCount != 0 {
		t.Fatalf("expected reopened app to stay cleared, got done=%d total=%d", wbReopened.DoneCount, wbReopened.TotalCount)
	}
}

func TestClearWorkbenchResultsIgnoredWhileRunning(t *testing.T) {
	a, _, _, _ := setupRewashTestApp(t, 2)
	a.running = true

	clearWorkbenchResultsBridge()

	wb := getWorkbenchStateBridge()
	if wb.DoneCount != 2 || wb.TotalCount != 2 {
		t.Fatalf("expected clear results ignored while running, got done=%d total=%d", wb.DoneCount, wb.TotalCount)
	}
}

func TestClearWorkbenchResultsOnlyAffectsActivePage(t *testing.T) {
	a, _, _, _ := setupRewashTestApp(t, 2)

	// Populate parked free-generation page with a completed task.
	setWorkbenchFieldBridge("mode", "free")
	a.queue = []tasks.Task{
		{Index: 1, Platform: "豆包", Prompt: "cat", Status: tasks.StatusSuccess, Results: []string{"/tmp/cat.png"}},
	}

	// Switch back to wash and clear wash results only.
	setWorkbenchFieldBridge("mode", "wash")
	clearWorkbenchResultsBridge()

	if wbWash := getWorkbenchStateBridge(); wbWash.TotalCount != 0 || wbWash.DoneCount != 0 {
		t.Fatalf("expected wash results cleared, got total=%d done=%d", wbWash.TotalCount, wbWash.DoneCount)
	}

	setWorkbenchFieldBridge("mode", "free")
	if wbFree := getWorkbenchStateBridge(); wbFree.TotalCount != 1 || wbFree.DoneCount != 1 {
		t.Fatalf("expected parked free-gen results untouched, got total=%d done=%d", wbFree.TotalCount, wbFree.DoneCount)
	}
}

func TestCanRewashAndRequeueRestrictedToWashPage(t *testing.T) {
	a, _, _, outs := setupRewashTestApp(t, 1)

	for _, mode := range []string{"free", "fission"} {
		t.Run(mode, func(t *testing.T) {
			setWorkbenchFieldBridge("mode", mode)
			a.sources = []sourceImage{{Path: "/tmp/mother.png"}}
			a.queue = []tasks.Task{
				{Index: 1, Platform: "豆包", Prompt: "测试", Images: []string{"/tmp/mother.png"}, Status: tasks.StatusSuccess, Results: []string{outs[0]}},
			}
			wb := getWorkbenchStateBridge()
			if len(wb.DoneTiles) != 1 || wb.DoneTiles[0].CanRewash {
				t.Fatalf("mode=%s: CanRewash must be false, got %+v", mode, wb.DoneTiles)
			}
			requeueDoneTasksBridge([]int{1})
			wbAfter := getWorkbenchStateBridge()
			if wbAfter.DoneCount != 1 || wbAfter.PendingCount != 0 || wbAfter.CanUndoRewash {
				t.Fatalf("mode=%s: RequeueDoneTasks must be ignored, got done=%d pending=%d undo=%v", mode, wbAfter.DoneCount, wbAfter.PendingCount, wbAfter.CanUndoRewash)
			}
			if len(a.sources) != 1 || a.sources[0].Path != "/tmp/mother.png" {
				t.Fatalf("mode=%s: RequeueDoneTasks polluted sources: %+v", mode, a.sources)
			}
		})
	}

	// UndoRequeueTasks must sync tab -> statePage before guarding pageWash.
	setWorkbenchFieldBridge("mode", "wash")
	requeueDoneTasksBridge([]int{1})
	a.tab = pageFission // Simulate tab switch before syncPage
	undoRequeueTasksBridge()
	if a.statePage != pageFission {
		t.Fatalf("expected UndoRequeueTasks to sync to pageFission, got %d", a.statePage)
	}
	setWorkbenchFieldBridge("mode", "wash")
	if wb := getWorkbenchStateBridge(); !wb.CanUndoRewash || wb.PendingCount != 1 {
		t.Fatalf("UndoRequeueTasks invoked from fission tab must not mutate wash queue: %+v", wb)
	}
}

func createEmptyTestFile(p string) error {
	_, err := EnsureOutputDir(filepath.Dir(p))
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte("png"), 0o644)
}

func TestDeleteWorkbenchResultRemoveAndTrash(t *testing.T) {
	a, st, srcs, outs := setupRewashTestApp(t, 2)
	host := &stubDesktopHost{}
	a.BindHost(host)
	svc := &WorkbenchService{app: a}

	// Add a second output image to task 1 to test single-image removal within multi-output tasks
	extraOut := filepath.Join(filepath.Dir(outs[0]), "out_01_b.png")
	if err := createEmptyTestFile(extraOut); err != nil {
		t.Fatal(err)
	}
	a.queue[0].Results = []string{outs[0], extraOut}
	a.previewOn = true
	a.previewPath = outs[0]

	// 1. Remove from list only (trashFile=false): keeps file on disk, removes only matching result
	if err := svc.DeleteWorkbenchResult(context.Background(), 1, outs[0], false); err != nil {
		t.Fatalf("DeleteWorkbenchResult(remove only) failed: %v", err)
	}
	if len(host.trashed) != 0 {
		t.Fatalf("expected no trash calls when trashFile=false, got %v", host.trashed)
	}
	if _, err := os.Stat(outs[0]); err != nil {
		t.Fatalf("expected local file preserved when trashFile=false: %v", err)
	}
	if a.previewOn || a.previewPath != "" {
		t.Fatalf("expected preview closed when previewed tile removed")
	}
	wb := getWorkbenchStateBridge()
	if wb.DoneCount != 2 || len(a.queue) != 2 || len(a.queue[0].Results) != 1 || a.queue[0].Results[0] != extraOut {
		t.Fatalf("expected only first output removed from task 1, got done=%d queue=%+v", wb.DoneCount, a.queue)
	}

	// 2. Delete with trashFile=true on task 2: calls TrashPath, removes task, resets source Washed
	if err := svc.DeleteWorkbenchResult(context.Background(), 2, outs[1], true); err != nil {
		t.Fatalf("DeleteWorkbenchResult(trash) failed: %v", err)
	}
	if len(host.trashed) != 1 || host.trashed[0] != outs[1] {
		t.Fatalf("expected TrashPath called with %q, got %v", outs[1], host.trashed)
	}
	wb = getWorkbenchStateBridge()
	if wb.DoneCount != 1 || len(a.queue) != 1 {
		t.Fatalf("expected task 2 removed, got done=%d queue=%d", wb.DoneCount, len(a.queue))
	}
	if a.sources[1].Path != srcs[1] || a.sources[1].Washed || a.sources[1].WashOutput != "" {
		t.Fatalf("expected washed state reset for trashed wash output, got %+v", a.sources[1])
	}

	// Verify persisted session reflects the deletions
	_, savedTasks, ok, err := st.ActiveSession(store.PageWash)
	if err != nil || !ok {
		t.Fatalf("expected active session persisted, ok=%v err=%v", ok, err)
	}
	if len(savedTasks) != 1 || savedTasks[0].Outputs != extraOut {
		t.Fatalf("expected persisted session updated, got %+v", savedTasks)
	}

	// 3. Refuses arbitrary path not belonging to a.queue
	outside := filepath.Join(t.TempDir(), "secret.png")
	if err := createEmptyTestFile(outside); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteWorkbenchResult(context.Background(), 999, outside, true); err != nil {
		t.Fatalf("unexpected error on unmatched target: %v", err)
	}
	if len(host.trashed) != 1 {
		t.Fatalf("must not trash unverified path outside queue, trashed=%v", host.trashed)
	}

	// 4. Trash failure keeps task intact in queue
	host.trashErr = errors.New("recycle bin unavailable")
	if err := svc.DeleteWorkbenchResult(context.Background(), 1, extraOut, true); err == nil {
		t.Fatalf("expected trash error propagated")
	}
	if len(a.queue) != 1 {
		t.Fatalf("queue must stay intact when TrashPath fails")
	}
	host.trashErr = nil

	// 5. Ignored while running
	a.running = true
	if err := svc.DeleteWorkbenchResult(context.Background(), 1, extraOut, false); err != nil {
		t.Fatalf("unexpected error while running: %v", err)
	}
	if len(a.queue) != 1 {
		t.Fatalf("must ignore delete while running")
	}
}

func TestCoreCopyImageAndEditOpenPath(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(2, 2, color.RGBA{R: 20, G: 120, B: 240, A: 255})

	pngPath := filepath.Join(dir, "sample.png")
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pngPath, pngBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	jpgPath := filepath.Join(dir, "sample.jpg")
	var jpgBuf bytes.Buffer
	if err := jpeg.Encode(&jpgBuf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jpgPath, jpgBuf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &App{}
	host := &stubDesktopHost{}
	a.BindHost(host)

	coreSvc := &CoreService{app: a}
	if err := coreSvc.CopyImage(context.Background(), pngPath); err != nil {
		t.Fatalf("CopyImage(png) failed: %v", err)
	}
	if !bytes.Equal(host.clipboardImage, pngBuf.Bytes()) {
		t.Fatalf("expected PNG bytes passed directly to clipboard")
	}

	host.clipboardImage = nil
	if err := coreSvc.CopyImage(context.Background(), jpgPath); err != nil {
		t.Fatalf("CopyImage(jpg) failed: %v", err)
	}
	if !bytes.HasPrefix(host.clipboardImage, pngHeaderMagic) {
		t.Fatalf("expected JPEG converted to PNG bytes for clipboard, got %d bytes", len(host.clipboardImage))
	}

	// EditOpenPath appends and selects newly added image even when filmstrip is non-empty
	editSvc := &ImageEditService{app: a}
	editSvc.EditAddPaths(context.Background(), []string{pngPath})
	if a.edit.selected != 0 || len(a.edit.files) != 1 {
		t.Fatalf("unexpected initial filmstrip state: selected=%d len=%d", a.edit.selected, len(a.edit.files))
	}
	editSvc.EditOpenPath(context.Background(), jpgPath)
	if len(a.edit.files) != 2 || a.edit.selected != 1 {
		t.Fatalf("expected EditOpenPath to append and select jpgPath, got selected=%d len=%d", a.edit.selected, len(a.edit.files))
	}
	// Re-opening existing image selects it without duplicating
	editSvc.EditOpenPath(context.Background(), pngPath)
	if len(a.edit.files) != 2 || a.edit.selected != 0 {
		t.Fatalf("expected EditOpenPath to select existing pngPath without dupes, got selected=%d len=%d", a.edit.selected, len(a.edit.files))
	}
}

func TestCoreOpenExternalValidation(t *testing.T) {
	a := &App{}
	host := &stubDesktopHost{}
	a.BindHost(host)

	coreSvc := &CoreService{app: a}
	const repoURL = "https://github.com/xxww0098/tulai"
	if err := coreSvc.OpenExternal(context.Background(), repoURL); err != nil {
		t.Fatalf("OpenExternal(https) failed: %v", err)
	}
	if len(host.openedURLs) != 1 || host.openedURLs[0] != repoURL {
		t.Fatalf("expected %q recorded, got %v", repoURL, host.openedURLs)
	}

	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "https://"} {
		if err := coreSvc.OpenExternal(context.Background(), bad); err == nil {
			t.Fatalf("expected OpenExternal(%q) to fail", bad)
		}
	}
}
