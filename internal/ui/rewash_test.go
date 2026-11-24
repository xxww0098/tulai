package ui

import (
	"fmt"
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

func createEmptyTestFile(p string) error {
	_, err := EnsureOutputDir(filepath.Dir(p))
	if err != nil {
		return err
	}
	return os.WriteFile(p, []byte("png"), 0o644)
}
