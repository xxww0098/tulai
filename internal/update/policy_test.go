package update

import (
	"path/filepath"
	"testing"
	"time"

	"xbs/internal/store"
)

func TestShouldOfferAndSkipPersists(t *testing.T) {
	offer, err := ShouldOffer("0.1.2", "0.1.2", "")
	if err != nil || offer {
		t.Fatalf("same version offered: %v %v", offer, err)
	}
	offer, err = ShouldOffer("0.1.2", "0.2.0", "")
	if err != nil || !offer {
		t.Fatalf("newer version hidden: %v %v", offer, err)
	}
	offer, err = ShouldOffer("0.1.2", "0.2.0", "0.2.0")
	if err != nil || offer {
		t.Fatalf("skipped version was offered: %v %v", offer, err)
	}
	offer, err = ShouldOffer("0.1.2", "0.2.1", "0.2.0")
	if err != nil || !offer {
		t.Fatalf("version after the skip was hidden: %v %v", offer, err)
	}

	path := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !AutoOn(st.Get(KeyAutoCheck)) || st.Get(KeyAutoCheck) != "1" {
		t.Fatalf("auto check default: %q", st.Get(KeyAutoCheck))
	}
	if err := RememberSkip(st, "v0.2.0"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if Skipped(st) != "0.2.0" {
		t.Fatalf("skip did not persist: %q", Skipped(st))
	}
	offer, err = ShouldOffer("0.1.2", Skipped(st), Skipped(st))
	if err != nil || offer {
		t.Fatalf("reopened skip was offered: %v %v", offer, err)
	}
}

func TestNextStepDefersWhileRunning(t *testing.T) {
	if got := NextStep(true, true, false); got != StepDefer {
		t.Fatalf("running download = %s", got)
	}
	if got := NextStep(true, true, true); got != StepDefer {
		t.Fatalf("running install = %s", got)
	}
	if got := NextStep(false, true, false); got != StepDownload {
		t.Fatalf("idle download = %s", got)
	}
	if got := NextStep(false, true, true); got != StepInstall {
		t.Fatalf("idle install = %s", got)
	}
	if got := NextStep(false, false, true); got != StepIdle {
		t.Fatalf("not pending = %s", got)
	}
	if DeferMessage == "" {
		t.Fatal("missing defer message")
	}
}

func TestMirrorSettingAndCheckDue(t *testing.T) {
	if got := MirrorSetting("", DefaultMirrors); len(got) != len(DefaultMirrors) {
		t.Fatalf("defaults: %v", got)
	}
	if got := MirrorSetting("off", DefaultMirrors); got != nil {
		t.Fatalf("off: %v", got)
	}
	got := MirrorSetting("https://example.test/proxy/", DefaultMirrors)
	if len(got) != 1+len(DefaultMirrors) || got[0] != "https://example.test/proxy/" {
		t.Fatalf("custom: %v", got)
	}
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	if !CheckDue(time.Time{}, now, CheckEvery) {
		t.Fatal("first check was not due")
	}
	if CheckDue(now.Add(-time.Hour), now, CheckEvery) {
		t.Fatal("check was due after one hour")
	}
	if !CheckDue(now.Add(-CheckEvery), now, CheckEvery) {
		t.Fatal("check was not due after six hours")
	}
}

func TestShortNotes(t *testing.T) {
	got := ShortNotes("第一行\n\n第二行说明", 4)
	if got != "第一行…" && got != "第一…" {
		// 4 runes cuts inside the first line.
		if len([]rune(got)) > 5 {
			t.Fatalf("notes too long: %q", got)
		}
	}
	full := ShortNotes("# tulai 0.2.0\n\n- 修复下载", 180)
	if full == "" || len([]rune(full)) > 180 {
		t.Fatalf("notes: %q", full)
	}
}
