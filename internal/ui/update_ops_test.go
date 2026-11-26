package ui

import (
	"context"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/update"
)

func TestApplyUpdateProgressPhasesAndStallDecay(t *testing.T) {
	app := NewApp(nil, nil, nil, t.TempDir())
	app.updateNotice = "0.1.41"

	t0 := time.Date(2026, 11, 26, 19, 0, 0, 0, time.UTC)

	app.applyUpdateProgressAt(t0, update.Progress{
		Phase: update.PhaseChecksums,
		Name:  "tulai.Setup.0.1.41.amd64.exe",
		Total: 100 << 20,
	})
	st := app.buildShellState().Update
	if !st.Downloading || st.Phase != "checksums" || st.Fraction >= 0 {
		t.Fatalf("checksums state = %+v", st)
	}

	app.applyUpdateProgressAt(t0.Add(200*time.Millisecond), update.Progress{
		Phase: update.PhaseProbing,
		Name:  "tulai.Setup.0.1.41.amd64.exe",
		Total: 100 << 20,
	})
	st = app.buildShellState().Update
	if st.Phase != "probing" || st.Fraction >= 0 || st.DoneBytes != 0 {
		t.Fatalf("probing state = %+v", st)
	}

	app.applyUpdateProgressAt(t0.Add(1*time.Second), update.Progress{
		Phase:       update.PhaseDownloading,
		Name:        "tulai.Setup.0.1.41.amd64.exe",
		Done:        25 << 20,
		Total:       100 << 20,
		BytesPerSec: float64(5 << 20),
		Source:      "edgeone.gh-proxy.org",
	})
	st = app.buildShellState().Update
	if st.Phase != "downloading" || st.Fraction < 0.24 || st.Fraction > 0.26 {
		t.Fatalf("downloading fraction = %+v", st)
	}
	if st.DoneBytes != 25<<20 || st.TotalBytes != 100<<20 || st.SpeedBps != 5<<20 || st.EtaSeconds != 15 || st.Source != "edgeone.gh-proxy.org" {
		t.Fatalf("downloading telemetry = %+v", st)
	}

	// 3 seconds of silence decays stale speed and ETA to zero.
	if !app.tickUpdateProgress(t0.Add(4 * time.Second)) {
		t.Fatal("expected stall decay to trigger")
	}
	st = app.buildShellState().Update
	if st.SpeedBps != 0 || st.EtaSeconds != 0 {
		t.Fatalf("expected decayed speed/eta = 0, got %+v", st)
	}

	app.applyUpdateProgressAt(t0.Add(5*time.Second), update.Progress{
		Phase:  update.PhaseVerifying,
		Name:   "tulai.Setup.0.1.41.amd64.exe",
		Done:   100 << 20,
		Total:  100 << 20,
		Source: "edgeone.gh-proxy.org",
	})
	st = app.buildShellState().Update
	if st.Phase != "verifying" || st.Fraction != 1 || st.SpeedBps != 0 || st.EtaSeconds != 0 {
		t.Fatalf("verifying state = %+v", st)
	}
}

func TestCancelUpdateResetsTransferAndCancelsContext(t *testing.T) {
	app := NewApp(nil, nil, nil, t.TempDir())
	app.StartActor()
	defer app.StopActor()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = app.call(context.Background(), func() {
		app.updateNotice = "0.1.41"
		app.updateDownloading = true
		app.updatePhase = "downloading"
		app.updateFraction = 0.5
		app.updateDone = 50 << 20
		app.updateTotal = 100 << 20
		app.updateSpeed = 4 << 20
		app.updateEta = 12
		app.updateSource = "ghfast.top"
		app.updateCancel = cancel
	})

	svc := &CoreService{app: app}
	svc.UpdateAction(context.Background(), "cancel")

	if ctx.Err() != context.Canceled {
		t.Fatalf("expected update download context to be canceled, err=%v", ctx.Err())
	}
	st := svc.GetShellState(context.Background()).Update
	if st.Downloading || st.Deferred || st.Phase != "" || st.Fraction != 0 || st.SpeedBps != 0 || st.Err != "" {
		t.Fatalf("unexpected state after cancel: %+v", st)
	}
	if st.Version != "0.1.41" {
		t.Fatalf("expected update version notice to remain after cancel, got %q", st.Version)
	}
}
