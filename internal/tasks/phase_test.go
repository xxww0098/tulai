package tasks

import (
	"sync/atomic"
	"testing"
)

func TestSetTaskPhasePropagatesToBatchMembersAndNotifies(t *testing.T) {
	items := []Task{
		{Index: 1, Platform: "豆包", Status: StatusRunning, Images: []string{"a.png"}},
		{Index: 2, Platform: "豆包", Status: StatusRunning, Images: []string{"b.png"}},
	}
	e := &Engine{
		view: NewQueueView(items),
	}
	var notified atomic.Int32
	e.SetHealNotify(func() {
		notified.Add(1)
	})

	batched := washBatchOf(items[0], items)
	e.setTaskPhase(&batched, PhaseUploading)
	if notified.Load() != 1 {
		t.Fatalf("notify count = %d, want 1", notified.Load())
	}
	for i, item := range e.view.Snapshot() {
		if item.Phase != PhaseUploading {
			t.Fatalf("item[%d].Phase = %q, want %q", i, item.Phase, PhaseUploading)
		}
	}

	// Same phase is a no-op
	e.setTaskPhase(&batched, PhaseUploading)
	if notified.Load() != 1 {
		t.Fatalf("duplicate phase should not notify: got %d", notified.Load())
	}

	e.setTaskPhase(&batched, PhaseGenerating)
	if notified.Load() != 2 {
		t.Fatalf("notify count = %d, want 2", notified.Load())
	}
	for i, item := range e.view.Snapshot() {
		if item.Phase != PhaseGenerating {
			t.Fatalf("item[%d].Phase = %q, want %q", i, item.Phase, PhaseGenerating)
		}
	}

	e.setTaskPhase(&batched, PhaseDownloading)
	if notified.Load() != 3 {
		t.Fatalf("notify count = %d, want 3", notified.Load())
	}
	for i, item := range e.view.Snapshot() {
		if item.Phase != PhaseDownloading {
			t.Fatalf("item[%d].Phase = %q, want %q", i, item.Phase, PhaseDownloading)
		}
	}
}
