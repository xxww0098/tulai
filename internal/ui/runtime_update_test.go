package ui

import (
	"testing"

	"github.com/xxww0098/realbrowser/fingerprintkernel/engine"
)

func TestApplyBrowserRuntimeUpdateRequiresCurrentPreview(t *testing.T) {
	// A busy controller accepts the action without starting a real download.
	s := &BrowserService{app: &App{runtimeBusy: true}}
	if err := s.ApplyBrowserRuntimeUpdate(t.Context(), "unknown"); err == nil {
		t.Fatal("accepted an update without a preview")
	}
	s.checkedUpdate.Store(&checkedRuntimeUpdate{id: "reviewed", release: &engine.ReleaseInfo{TagName: "152"}})
	if err := s.ApplyBrowserRuntimeUpdate(t.Context(), "152"); err == nil {
		t.Fatal("accepted a version instead of its preview ID")
	}
	if err := s.ApplyBrowserRuntimeUpdate(t.Context(), "reviewed"); err != nil {
		t.Fatal(err)
	}
	s.checkedUpdate.Store(&checkedRuntimeUpdate{id: "replacement", release: &engine.ReleaseInfo{TagName: "152"}})
	if err := s.ApplyBrowserRuntimeUpdate(t.Context(), "reviewed"); err == nil {
		t.Fatal("accepted an obsolete preview with the same version")
	}
}
