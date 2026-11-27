package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
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

func TestCanReuseUpdateAssetValidation(t *testing.T) {
	dir := t.TempDir()
	assetPath := filepath.Join(dir, "tulai.Setup.0.1.42.amd64.exe")
	payload := []byte("verified-installer-bytes")
	if err := os.WriteFile(assetPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	wantSHA := hex.EncodeToString(sum[:])

	if !canReuseUpdateAsset(assetPath, "0.1.42", "0.1.42", update.ModeWindowsSetup, update.ModeWindowsSetup, wantSHA) {
		t.Fatal("expected valid matching cached asset to be reusable")
	}
	if canReuseUpdateAsset(assetPath, "0.1.41", "0.1.42", update.ModeWindowsSetup, update.ModeWindowsSetup, wantSHA) {
		t.Fatal("reused cached asset from an older version")
	}
	if canReuseUpdateAsset(assetPath, "0.1.42", "0.1.42", update.ModeWindowsExe, update.ModeWindowsSetup, wantSHA) {
		t.Fatal("reused cached asset from a different install mode")
	}
	if canReuseUpdateAsset(assetPath, "0.1.42", "0.1.42", update.ModeWindowsSetup, update.ModeWindowsSetup, "") {
		t.Fatal("reused cached asset without a verified SHA-256")
	}
	if canReuseUpdateAsset(filepath.Join(dir, "missing.exe"), "0.1.42", "0.1.42", update.ModeWindowsSetup, update.ModeWindowsSetup, wantSHA) {
		t.Fatal("reused missing cached asset")
	}
	if canReuseUpdateAsset(dir, "0.1.42", "0.1.42", update.ModeWindowsSetup, update.ModeWindowsSetup, wantSHA) {
		t.Fatal("reused directory as cached asset")
	}
	if err := os.WriteFile(assetPath, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if canReuseUpdateAsset(assetPath, "0.1.42", "0.1.42", update.ModeWindowsSetup, update.ModeWindowsSetup, wantSHA) {
		t.Fatal("reused cached asset after file content changed")
	}
}

func TestUpdateQuittingAndDoubleClickGuards(t *testing.T) {
	app := NewApp(nil, nil, nil, t.TempDir())
	app.StartActor()
	defer app.StopActor()

	// Once updateQuitting is set, cancel/check/install/deferred-install must be no-ops.
	_ = app.call(context.Background(), func() {
		app.updateNotice = "0.1.42"
		app.updateDownloading = true
		app.updateQuitting = true
		app.updatePending = true
		app.updateDeferred = true
		app.updatePhase = "installing"
		app.updateProgressText = "正在退出并安装 v0.1.42..."
	})

	svc := &CoreService{app: app}
	svc.UpdateAction(context.Background(), "cancel")
	svc.UpdateAction(context.Background(), "check")
	svc.UpdateAction(context.Background(), "install")
	_ = app.call(context.Background(), func() {
		app.maybeInstallPending()
	})

	st := svc.GetShellState(context.Background()).Update
	if !st.Downloading || st.Phase != "installing" {
		t.Fatalf("expected quitting update state to remain locked, got %+v", st)
	}
	if app.updateChecks != 0 {
		t.Fatalf("checkForUpdates ran while updateQuitting=true (checks=%d)", app.updateChecks)
	}

	// Verify clearCachedUpdateAsset clears all cached metadata.
	_ = app.call(context.Background(), func() {
		app.updateAsset = "/tmp/old.exe"
		app.updateAssetVersion = "0.1.41"
		app.updateAssetMode = update.ModeWindowsSetup
		app.updateAssetSHA256 = "abc"
		app.clearCachedUpdateAsset()
	})
	if app.updateAsset != "" || app.updateAssetVersion != "" || app.updateAssetMode != "" || app.updateAssetSHA256 != "" {
		t.Fatalf("clearCachedUpdateAsset left stale fields: path=%q ver=%q mode=%q sha=%q",
			app.updateAsset, app.updateAssetVersion, app.updateAssetMode, app.updateAssetSHA256)
	}
}

type updateTestHost struct {
	stubDesktopHost
	mu           sync.Mutex
	confirmReply func(title, message, confirmLabel string) bool
	confirmCalls int
	quitCalls    int
	quitCh       chan struct{}
	restartCalls int
	restartCh    chan struct{}
}

func (h *updateTestHost) ConfirmAlert(title, message, confirmLabel string) bool {
	h.mu.Lock()
	h.confirmCalls++
	fn := h.confirmReply
	h.mu.Unlock()
	if fn != nil {
		return fn(title, message, confirmLabel)
	}
	return true
}

func (h *updateTestHost) Quit() {
	h.mu.Lock()
	h.quitCalls++
	ch := h.quitCh
	h.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (h *updateTestHost) Restart() {
	h.mu.Lock()
	h.restartCalls++
	ch := h.restartCh
	h.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func stubUpdateHooks(t *testing.T) {
	t.Helper()
	origVer := packagedVersionOverride
	origExe := updateRunningExecutable
	origDetect := updateDetectModeFunc
	origInstall := updateInstallFunc
	origStart := updateStartDetachedFunc
	origSync := updateSyncWinVerFunc
	origClient := updateNewClientFunc
	origGOOS := updateGOOS
	origGOARCH := updateGOARCH
	t.Cleanup(func() {
		packagedVersionOverride = origVer
		updateRunningExecutable = origExe
		updateDetectModeFunc = origDetect
		updateInstallFunc = origInstall
		updateStartDetachedFunc = origStart
		updateSyncWinVerFunc = origSync
		updateNewClientFunc = origClient
		updateGOOS = origGOOS
		updateGOARCH = origGOARCH
	})
}

func newFakeReleaseServer(t *testing.T, tag, assetName string, payload []byte) (*httptest.Server, update.Release, string, *atomic.Int32) {
	t.Helper()
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])
	sumsText := sumHex + "  " + assetName + "\n"
	sumsSum := sha256.Sum256([]byte(sumsText))
	var assetDownloads atomic.Int32

	var srv *httptest.Server
	var rel update.Release
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/xxww0098/tulai/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rel)
		case "/sums":
			_, _ = w.Write([]byte(sumsText))
		case "/asset":
			assetDownloads.Add(1)
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	rel = update.Release{
		Tag:  tag,
		Name: "tulai " + strings.TrimPrefix(tag, "v"),
		Body: "release notes for " + tag,
		Assets: []update.Asset{
			{
				Name:               "SHA256SUMS",
				State:              "uploaded",
				BrowserDownloadURL: srv.URL + "/sums",
				Size:               int64(len(sumsText)),
				Digest:             "sha256:" + hex.EncodeToString(sumsSum[:]),
			},
			{
				Name:               assetName,
				State:              "uploaded",
				BrowserDownloadURL: srv.URL + "/asset",
				Size:               int64(len(payload)),
				Digest:             "sha256:" + sumHex,
			},
		},
	}
	return srv, rel, sumHex, &assetDownloads
}

func TestDownloadAndInstallEndToEndReuseAndSingleHelper(t *testing.T) {
	stubUpdateHooks(t)

	dlDir := t.TempDir()
	srv, rel, _, downloads := newFakeReleaseServer(t, "v0.1.43", "tulai.Setup.0.1.43.amd64.exe", []byte("setup-binary-v0.1.43"))

	packagedVersionOverride = "0.1.42"
	updateGOOS = func() string { return "windows" }
	updateGOARCH = func() string { return "amd64" }
	updateRunningExecutable = func() (string, error) {
		return `C:\Users\alice\AppData\Local\Programs\tulai\tulai.exe`, nil
	}
	updateNewClientFunc = func(version string) *update.Client {
		c := update.NewClient(version)
		c.API = srv.URL
		c.DownloadDir = dlDir
		return c
	}

	var installCalls, detachCalls, syncWinCalls atomic.Int32
	var installedOpts update.Options
	updateInstallFunc = func(opts update.Options) (update.Outcome, error) {
		installCalls.Add(1)
		installedOpts = opts
		return update.Outcome{
			InstalledPath: opts.AssetPath,
			Quit:          true,
			Helper:        "powershell.exe",
			HelperArgs:    []string{"-File", "replace.ps1", "-ExpectedVersion", opts.Version},
		}, nil
	}
	updateStartDetachedFunc = func(path string, args ...string) error {
		detachCalls.Add(1)
		return nil
	}
	updateSyncWinVerFunc = func(executable, version string) error {
		syncWinCalls.Add(1)
		return nil
	}

	app := NewApp(nil, nil, nil, t.TempDir())
	host := &updateTestHost{quitCh: make(chan struct{}, 2)}
	app.BindHost(host)
	app.StartActor()
	defer app.StopActor()

	// Seed discovered release, mirror=off so test uses local server directly.
	_ = app.call(context.Background(), func() {
		app.updateMirror = "off"
		app.updateRel = rel
		app.updateNotice = "0.1.43"
	})

	// 1) First attempt: user cancels the setup confirmation dialog.
	// Download must complete and cache the verified asset without installing or quitting.
	host.confirmReply = func(string, string, string) bool { return false }
	app.downloadAndInstall()

	if got := downloads.Load(); got != 1 {
		t.Fatalf("downloads after first attempt = %d, want 1", got)
	}
	if installCalls.Load() != 0 || detachCalls.Load() != 0 || host.quitCalls != 0 {
		t.Fatalf("canceled setup should not install/quit: install=%d detach=%d quit=%d",
			installCalls.Load(), detachCalls.Load(), host.quitCalls)
	}
	var cachedPath string
	_ = app.call(context.Background(), func() {
		cachedPath = app.updateAsset
		if app.updateQuitting || app.updateDownloading {
			t.Fatalf("expected clean state after confirm cancel: quitting=%v downloading=%v",
				app.updateQuitting, app.updateDownloading)
		}
	})
	if cachedPath == "" {
		t.Fatal("expected verified asset to remain cached after confirm cancel")
	}

	// 2) Second attempt: double-click applyUpdate() while confirming.
	// Must reuse cached asset (0 extra downloads) and spawn only ONE helper + Quit.
	confirmGate := make(chan struct{})
	host.confirmReply = func(string, string, string) bool {
		<-confirmGate
		return true
	}
	svc := &CoreService{app: app}
	svc.UpdateAction(context.Background(), "install")
	svc.UpdateAction(context.Background(), "install") // double-click must be ignored
	close(confirmGate)

	select {
	case <-host.quitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for host.Quit()")
	}

	if got := downloads.Load(); got != 1 {
		t.Fatalf("expected cached asset reuse without re-download, got %d downloads", got)
	}
	if installCalls.Load() != 1 || detachCalls.Load() != 1 {
		t.Fatalf("expected exactly 1 install & 1 detach, got install=%d detach=%d",
			installCalls.Load(), detachCalls.Load())
	}
	if syncWinCalls.Load() != 0 {
		t.Fatalf("SyncWindowsInstalledVersion must not run before detached helper (got %d calls)", syncWinCalls.Load())
	}
	if installedOpts.Mode != update.ModeWindowsSetup || installedOpts.Version != "0.1.43" {
		t.Fatalf("unexpected install options: %+v", installedOpts)
	}
	_ = app.call(context.Background(), func() {
		if !app.updateQuitting {
			t.Fatal("expected updateQuitting=true after detached helper launch")
		}
	})
}

func TestDownloadAndInstallReuploadedReleaseInvalidatesCacheAndDetachErrorUnlocks(t *testing.T) {
	stubUpdateHooks(t)

	dlDir := t.TempDir()
	srvV1, relV1, shaV1, _ := newFakeReleaseServer(t, "v0.1.43", "tulai-0.1.43-windows-amd64.exe", []byte("portable-build-1"))
	srvV2, relV2, shaV2, downloadsV2 := newFakeReleaseServer(t, "v0.1.43", "tulai-0.1.43-windows-amd64.exe", []byte("portable-build-2-clobbered"))
	_ = srvV1

	// Write cached file matching relV1 on disk.
	stalePath := filepath.Join(dlDir, "tulai-0.1.43-windows-amd64.exe")
	if err := os.WriteFile(stalePath, []byte("portable-build-1"), 0o644); err != nil {
		t.Fatal(err)
	}

	packagedVersionOverride = "0.1.42"
	updateGOOS = func() string { return "windows" }
	updateGOARCH = func() string { return "amd64" }
	updateRunningExecutable = func() (string, error) {
		return `D:\Portable\tulai-0.1.42-windows-amd64.exe`, nil
	}
	updateNewClientFunc = func(version string) *update.Client {
		c := update.NewClient(version)
		c.API = srvV2.URL
		c.DownloadDir = dlDir
		return c
	}

	var failDetach atomic.Bool
	failDetach.Store(true)
	updateInstallFunc = func(opts update.Options) (update.Outcome, error) {
		return update.Outcome{
			InstalledPath: opts.Executable,
			Quit:          true,
			Helper:        "powershell.exe",
			HelperArgs:    []string{"-File", "replace.ps1"},
		}, nil
	}
	updateStartDetachedFunc = func(path string, args ...string) error {
		if failDetach.Load() {
			return errors.New("simulated helper launch error")
		}
		return nil
	}

	app := NewApp(nil, nil, nil, t.TempDir())
	host := &updateTestHost{quitCh: make(chan struct{}, 2)}
	app.BindHost(host)
	app.StartActor()
	defer app.StopActor()

	// Seed stale cached asset from relV1 while updateRel is now the re-uploaded relV2.
	_ = app.call(context.Background(), func() {
		app.updateMirror = "off"
		app.updateRel = relV2
		app.updateNotice = "0.1.43"
		app.updateAsset = stalePath
		app.updateAssetVersion = "0.1.43"
		app.updateAssetMode = update.ModeWindowsExe
		app.updateAssetSHA256 = shaV1
	})
	_ = relV1

	// 1) Must detect digest mismatch against relV2, re-download relV2, fail StartDetached,
	// and clear updateQuitting so the user can retry.
	app.downloadAndInstall()
	// Flush async failUpdate callback on actor.
	_ = app.call(context.Background(), func() {})

	if got := downloadsV2.Load(); got != 1 {
		t.Fatalf("expected stale cache to be invalidated and relV2 downloaded once, got %d", got)
	}
	_ = app.call(context.Background(), func() {
		if app.updateQuitting || app.updateDownloading {
			t.Fatalf("detach failure must unlock update state: quitting=%v downloading=%v",
				app.updateQuitting, app.updateDownloading)
		}
		if app.updateAssetSHA256 != shaV2 {
			t.Fatalf("expected cached sha256 to refresh to %s, got %s", shaV2, app.updateAssetSHA256)
		}
		if !strings.Contains(app.updateErr, "simulated helper launch error") {
			t.Fatalf("expected error text, got %q", app.updateErr)
		}
	})

	// 2) Retry now succeeds and reuses the freshly verified relV2 download.
	failDetach.Store(false)
	app.downloadAndInstall()
	if got := downloadsV2.Load(); got != 1 {
		t.Fatalf("retry should reuse freshly downloaded relV2 asset, got %d downloads", got)
	}
	if host.quitCalls != 1 {
		t.Fatalf("expected 1 Quit call on retry success, got %d", host.quitCalls)
	}
}

func TestSilentCheckSkipsWhileDownloadingOrQuitting(t *testing.T) {
	stubUpdateHooks(t)

	var clientCalled atomic.Int32
	packagedVersionOverride = "0.1.42"
	updateRunningExecutable = func() (string, error) { return "/Applications/tulai.app/Contents/MacOS/tulai", nil }
	updateNewClientFunc = func(version string) *update.Client {
		clientCalled.Add(1)
		return update.NewClient(version)
	}

	app := NewApp(nil, nil, nil, t.TempDir())
	app.StartActor()
	defer app.StopActor()

	_ = app.call(context.Background(), func() {
		app.autoCheck = true
		app.updateDownloading = true
	})
	app.silentCheck()

	_ = app.call(context.Background(), func() {
		app.updateDownloading = false
		app.updateQuitting = true
	})
	app.silentCheck()

	if clientCalled.Load() != 0 {
		t.Fatalf("silentCheck ran during download/quit (client calls=%d)", clientCalled.Load())
	}
}

func TestCancelDuringDownloadConfirmAbortsInstallAndQuit(t *testing.T) {
	stubUpdateHooks(t)

	dlDir := t.TempDir()
	srv, rel, _, _ := newFakeReleaseServer(t, "v0.1.43", "tulai.Setup.0.1.43.amd64.exe", []byte("setup-binary-v0.1.43"))

	packagedVersionOverride = "0.1.42"
	updateGOOS = func() string { return "windows" }
	updateGOARCH = func() string { return "amd64" }
	updateRunningExecutable = func() (string, error) {
		return `C:\Users\alice\AppData\Local\Programs\tulai\tulai.exe`, nil
	}
	updateNewClientFunc = func(version string) *update.Client {
		c := update.NewClient(version)
		c.API = srv.URL
		c.DownloadDir = dlDir
		return c
	}

	var installCalls, detachCalls atomic.Int32
	updateInstallFunc = func(opts update.Options) (update.Outcome, error) {
		installCalls.Add(1)
		return update.Outcome{Quit: true, Helper: "powershell.exe"}, nil
	}
	updateStartDetachedFunc = func(path string, args ...string) error {
		detachCalls.Add(1)
		return nil
	}

	app := NewApp(nil, nil, nil, t.TempDir())
	host := &updateTestHost{}
	// Simulate cancel arriving while confirm dialog is shown, before install commits.
	host.confirmReply = func(string, string, string) bool {
		_ = app.call(context.Background(), func() {
			app.cancelUpdate()
		})
		return true
	}
	app.BindHost(host)
	app.StartActor()
	defer app.StopActor()

	_ = app.call(context.Background(), func() {
		app.updateMirror = "off"
		app.updateRel = rel
		app.updateNotice = "0.1.43"
	})

	app.downloadAndInstall()

	if installCalls.Load() != 0 || detachCalls.Load() != 0 || host.quitCalls != 0 {
		t.Fatalf("expected cancel before commit to abort install/detach/quit: install=%d detach=%d quit=%d",
			installCalls.Load(), detachCalls.Load(), host.quitCalls)
	}
	_ = app.call(context.Background(), func() {
		if app.updateQuitting || app.updateDownloading {
			t.Fatalf("expected clean state after cancel, got quitting=%v downloading=%v",
				app.updateQuitting, app.updateDownloading)
		}
	})
}

func TestDismissAndSkipClearDeferredPendingState(t *testing.T) {
	stubUpdateHooks(t)

	var installCalls, clientCalls atomic.Int32
	packagedVersionOverride = "0.1.42"
	updateGOOS = func() string { return "windows" }
	updateGOARCH = func() string { return "amd64" }
	updateRunningExecutable = func() (string, error) {
		return `C:\Users\alice\AppData\Local\Programs\tulai\tulai.exe`, nil
	}
	updateNewClientFunc = func(version string) *update.Client {
		clientCalls.Add(1)
		return update.NewClient(version)
	}
	updateInstallFunc = func(opts update.Options) (update.Outcome, error) {
		installCalls.Add(1)
		return update.Outcome{Quit: true}, nil
	}

	for _, action := range []string{"dismiss", "skip"} {
		t.Run(action, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "config.db")
			st, err := store.Open(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })

			app := NewApp(st, nil, nil, t.TempDir())
			app.StartActor()
			defer app.StopActor()

			// Seed a deferred update waiting for active tasks to finish.
			_ = app.call(context.Background(), func() {
				app.running = true
				app.updateNotice = "0.1.43"
				app.updateRel = update.Release{Tag: "v0.1.43"}
				app.updatePending = true
				app.updateDeferred = true
				app.updatePhase = "deferred"
				app.updateFraction = -1
				app.updateProgressText = update.DeferMessage
				app.updateAsset = "/tmp/cached.exe"
				app.updateAssetVersion = "0.1.43"
				app.updateAssetMode = update.ModeWindowsSetup
				app.updateAssetSHA256 = "deadbeef"
			})

			svc := &CoreService{app: app}
			svc.UpdateAction(context.Background(), action)

			dto := svc.GetShellState(context.Background()).Update
			if dto.Visible || dto.Version != "" || dto.Deferred || dto.Phase != "" || dto.ProgressText != "" || dto.Fraction != 0 {
				t.Fatalf("%s left stale update DTO state: %+v", action, dto)
			}
			_ = app.call(context.Background(), func() {
				if app.updatePending || app.updateDeferred || app.updateAsset != "" {
					t.Fatalf("%s left pending/deferred/cache set: pending=%v deferred=%v asset=%q",
						action, app.updatePending, app.updateDeferred, app.updateAsset)
				}
				// Tasks finish now; maybeInstallPending must be a no-op.
				app.running = false
				app.maybeInstallPending()
				if app.updateDownloading {
					t.Fatalf("%s allowed maybeInstallPending to start downloadAndInstall", action)
				}
			})
			if action == "skip" && st.Get(update.KeySkipped) != "0.1.43" {
				t.Fatalf("skip did not persist skipped version, got %q", st.Get(update.KeySkipped))
			}
			if installCalls.Load() != 0 || clientCalls.Load() != 0 {
				t.Fatalf("%s triggered install after task finish: installs=%d clients=%d",
					action, installCalls.Load(), clientCalls.Load())
			}
		})
	}
}

func TestDownloadAndInstallUsesDesktopHostConcurrentlyWithBindHost(t *testing.T) {
	for _, tc := range []struct {
		name     string
		relaunch bool
	}{
		{name: "quit", relaunch: false},
		{name: "relaunch", relaunch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubUpdateHooks(t)

			dlDir := t.TempDir()
			assetPath := filepath.Join(dlDir, "tulai-0.1.43-linux-amd64.tar.gz")
			payload := []byte("verified-update-bytes")
			if err := os.WriteFile(assetPath, payload, 0o644); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(payload)
			sumHex := hex.EncodeToString(sum[:])

			packagedVersionOverride = "0.1.42"
			updateGOOS = func() string { return "linux" }
			updateGOARCH = func() string { return "amd64" }
			updateRunningExecutable = func() (string, error) {
				return "/home/alice/.local/tulai.app/tulai", nil
			}
			updateSyncWinVerFunc = func(executable, version string) error { return nil }
			updateStartDetachedFunc = func(path string, args ...string) error { return nil }
			updateInstallFunc = func(opts update.Options) (update.Outcome, error) {
				if tc.relaunch {
					return update.Outcome{InstalledPath: opts.Executable, Relaunch: true}, nil
				}
				return update.Outcome{InstalledPath: opts.Executable, Quit: true, Helper: "/bin/sh"}, nil
			}

			app := NewApp(nil, nil, nil, t.TempDir())
			hostA := &updateTestHost{
				quitCh:    make(chan struct{}, 2),
				restartCh: make(chan struct{}, 2),
			}
			hostB := &updateTestHost{
				quitCh:    make(chan struct{}, 2),
				restartCh: make(chan struct{}, 2),
			}
			app.BindHost(hostA)
			app.StartActor()
			defer app.StopActor()

			_ = app.call(context.Background(), func() {
				app.updateNotice = "0.1.43"
				app.updateRel = update.Release{Tag: "v0.1.43"}
				app.updateAsset = assetPath
				app.updateAssetVersion = "0.1.43"
				app.updateAssetMode = update.ModeLinuxUser
				app.updateAssetSHA256 = sumHex
			})

			// Hammer BindHost concurrently while downloadAndInstall runs to verify
			// there is no data race under -race on reading/writing app.host.
			stopBind := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; ; i++ {
					select {
					case <-stopBind:
						return
					default:
						if i%2 == 0 {
							app.BindHost(hostB)
						} else {
							app.BindHost(hostA)
						}
					}
				}
			}()

			app.downloadAndInstall()
			close(stopBind)
			wg.Wait()

			hostA.mu.Lock()
			qA, rA := hostA.quitCalls, hostA.restartCalls
			hostA.mu.Unlock()
			hostB.mu.Lock()
			qB, rB := hostB.quitCalls, hostB.restartCalls
			hostB.mu.Unlock()

			if tc.relaunch {
				if rA+rB != 1 || qA+qB != 0 {
					t.Fatalf("expected 1 Restart and 0 Quit, got restart=%d quit=%d", rA+rB, qA+qB)
				}
			} else {
				if qA+qB != 1 || rA+rB != 0 {
					t.Fatalf("expected 1 Quit and 0 Restart, got quit=%d restart=%d", qA+qB, rA+rB)
				}
			}
		})
	}
}

func TestCancelThenImmediateReinstallDoesNotCorruptSecondAttempt(t *testing.T) {
	stubUpdateHooks(t)

	dlDir := t.TempDir()
	payload := []byte("portable-binary-v0.1.43")
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])
	assetName := "tulai-0.1.43-windows-amd64.exe"
	sumsText := sumHex + "  " + assetName + "\n"
	sumsSum := sha256.Sum256([]byte(sumsText))

	req1Started := make(chan struct{}, 1)
	req2Started := make(chan struct{}, 1)
	releaseReq1 := make(chan struct{})
	releaseReq2 := make(chan struct{})
	var assetReqCount atomic.Int32

	var srv *httptest.Server
	var rel update.Release
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sums":
			_, _ = w.Write([]byte(sumsText))
		case "/asset":
			n := assetReqCount.Add(1)
			if n == 1 {
				select {
				case req1Started <- struct{}{}:
				default:
				}
				// Hold request 1 open briefly until test releases it so attempt 2 is already active.
				<-releaseReq1
				_, _ = w.Write(payload)
				return
			}
			select {
			case req2Started <- struct{}{}:
			default:
			}
			<-releaseReq2
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		select {
		case <-releaseReq1:
		default:
			close(releaseReq1)
		}
		select {
		case <-releaseReq2:
		default:
			close(releaseReq2)
		}
		srv.Close()
	})

	rel = update.Release{
		Tag:  "v0.1.43",
		Name: "tulai 0.1.43",
		Assets: []update.Asset{
			{
				Name:               "SHA256SUMS",
				State:              "uploaded",
				BrowserDownloadURL: srv.URL + "/sums",
				Size:               int64(len(sumsText)),
				Digest:             "sha256:" + hex.EncodeToString(sumsSum[:]),
			},
			{
				Name:               assetName,
				State:              "uploaded",
				BrowserDownloadURL: srv.URL + "/asset",
				Size:               int64(len(payload)),
				Digest:             "sha256:" + sumHex,
			},
		},
	}

	packagedVersionOverride = "0.1.42"
	updateGOOS = func() string { return "windows" }
	updateGOARCH = func() string { return "amd64" }
	updateRunningExecutable = func() (string, error) {
		return `D:\Portable\tulai.exe`, nil
	}
	updateNewClientFunc = func(version string) *update.Client {
		c := update.NewClient(version)
		c.API = srv.URL
		c.DownloadDir = dlDir
		return c
	}

	var installCalls, detachCalls atomic.Int32
	updateInstallFunc = func(opts update.Options) (update.Outcome, error) {
		installCalls.Add(1)
		return update.Outcome{InstalledPath: opts.Executable, Quit: true, Helper: "powershell.exe"}, nil
	}
	updateStartDetachedFunc = func(path string, args ...string) error {
		detachCalls.Add(1)
		return nil
	}

	app := NewApp(nil, nil, nil, t.TempDir())
	host := &updateTestHost{quitCh: make(chan struct{}, 4)}
	app.BindHost(host)
	app.StartActor()
	defer app.StopActor()

	_ = app.call(context.Background(), func() {
		app.updateMirror = "off"
		app.updateRel = rel
		app.updateNotice = "0.1.43"
	})

	svc := &CoreService{app: app}
	// Attempt 1 starts downloading.
	svc.UpdateAction(context.Background(), "install")
	select {
	case <-req1Started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for attempt 1 download")
	}

	// User cancels attempt 1 and immediately clicks install again (attempt 2).
	svc.UpdateAction(context.Background(), "cancel")
	svc.UpdateAction(context.Background(), "install")
	select {
	case <-req2Started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for attempt 2 download")
	}

	// Now let attempt 1 finish unwinding from client.Download while attempt 2 is active.
	close(releaseReq1)
	time.Sleep(150 * time.Millisecond)

	// Attempt 1 unwinding must NOT clobber attempt 2's downloading/cancel/err state or install early.
	_ = app.call(context.Background(), func() {
		if !app.updateDownloading {
			t.Fatalf("stale attempt 1 cleared updateDownloading while attempt 2 is active (err=%q)", app.updateErr)
		}
		if app.updateErr != "" {
			t.Fatalf("stale attempt 1 set updateErr=%q while attempt 2 is active", app.updateErr)
		}
		if app.updateCancel == nil {
			t.Fatal("stale attempt 1 cleared attempt 2's updateCancel")
		}
	})
	if installCalls.Load() != 0 || detachCalls.Load() != 0 {
		t.Fatalf("stale attempt 1 triggered install/detach: install=%d detach=%d", installCalls.Load(), detachCalls.Load())
	}

	// Let attempt 2 finish; exactly one install + detach + quit must occur.
	close(releaseReq2)
	select {
	case <-host.quitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for attempt 2 Quit")
	}
	if installCalls.Load() != 1 || detachCalls.Load() != 1 {
		t.Fatalf("expected exactly 1 install & 1 detach from attempt 2, got install=%d detach=%d",
			installCalls.Load(), detachCalls.Load())
	}
}

func TestSkipWhileDownloadingCancelsInsteadOfInstalling(t *testing.T) {
	stubUpdateHooks(t)

	dlDir := t.TempDir()
	payload := []byte("portable-binary-v0.1.43")
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])
	assetName := "tulai-0.1.43-windows-amd64.exe"
	sumsText := sumHex + "  " + assetName + "\n"
	sumsSum := sha256.Sum256([]byte(sumsText))

	reqStarted := make(chan struct{}, 1)
	releaseReq := make(chan struct{})

	var srv *httptest.Server
	var rel update.Release
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sums":
			_, _ = w.Write([]byte(sumsText))
		case "/asset":
			select {
			case reqStarted <- struct{}{}:
			default:
			}
			<-releaseReq
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		select {
		case <-releaseReq:
		default:
			close(releaseReq)
		}
		srv.Close()
	})

	rel = update.Release{
		Tag:  "v0.1.43",
		Name: "tulai 0.1.43",
		Assets: []update.Asset{
			{
				Name:               "SHA256SUMS",
				State:              "uploaded",
				BrowserDownloadURL: srv.URL + "/sums",
				Size:               int64(len(sumsText)),
				Digest:             "sha256:" + hex.EncodeToString(sumsSum[:]),
			},
			{
				Name:               assetName,
				State:              "uploaded",
				BrowserDownloadURL: srv.URL + "/asset",
				Size:               int64(len(payload)),
				Digest:             "sha256:" + sumHex,
			},
		},
	}

	packagedVersionOverride = "0.1.42"
	updateGOOS = func() string { return "windows" }
	updateGOARCH = func() string { return "amd64" }
	updateRunningExecutable = func() (string, error) {
		return `D:\Portable\tulai.exe`, nil
	}
	updateNewClientFunc = func(version string) *update.Client {
		c := update.NewClient(version)
		c.API = srv.URL
		c.DownloadDir = dlDir
		return c
	}

	var installCalls atomic.Int32
	updateInstallFunc = func(opts update.Options) (update.Outcome, error) {
		installCalls.Add(1)
		return update.Outcome{Quit: true, Helper: "powershell.exe"}, nil
	}
	updateStartDetachedFunc = func(path string, args ...string) error { return nil }

	dbPath := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	app := NewApp(st, nil, nil, t.TempDir())
	host := &updateTestHost{quitCh: make(chan struct{}, 2)}
	app.BindHost(host)
	app.StartActor()
	defer app.StopActor()

	_ = app.call(context.Background(), func() {
		app.updateMirror = "off"
		app.updateRel = rel
		app.updateNotice = "0.1.43"
	})

	svc := &CoreService{app: app}
	svc.UpdateAction(context.Background(), "install")
	select {
	case <-reqStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for download start")
	}

	// User skips this version while download is in flight.
	svc.UpdateAction(context.Background(), "skip")
	close(releaseReq)
	time.Sleep(150 * time.Millisecond)

	if st.Get(update.KeySkipped) != "0.1.43" {
		t.Fatalf("expected skipped version 0.1.43, got %q", st.Get(update.KeySkipped))
	}
	host.mu.Lock()
	quits := host.quitCalls
	host.mu.Unlock()
	if installCalls.Load() != 0 || quits != 0 {
		t.Fatalf("skip during download still installed/quit: install=%d quit=%d", installCalls.Load(), quits)
	}
	dto := svc.GetShellState(context.Background()).Update
	if dto.Visible || dto.Downloading || dto.Version != "" {
		t.Fatalf("expected update state cleared after skip, got %+v", dto)
	}
}

func TestUpToDateCheckClearsDeferredPendingState(t *testing.T) {
	stubUpdateHooks(t)

	srv, _, _, _ := newFakeReleaseServer(t, "v0.1.42", "tulai-0.1.42-windows-amd64.exe", []byte("same"))
	packagedVersionOverride = "0.1.42"
	updateGOOS = func() string { return "windows" }
	updateGOARCH = func() string { return "amd64" }
	updateRunningExecutable = func() (string, error) {
		return `D:\Portable\tulai.exe`, nil
	}
	updateNewClientFunc = func(version string) *update.Client {
		c := update.NewClient(version)
		c.API = srv.URL
		return c
	}

	app := NewApp(nil, nil, nil, t.TempDir())
	app.BindHost(&updateTestHost{})
	app.StartActor()
	defer app.StopActor()

	_ = app.call(context.Background(), func() {
		app.running = true
		app.updateMirror = "off"
		app.updateNotice = "0.1.43"
		app.updateRel = update.Release{Tag: "v0.1.43"}
		app.updatePending = true
		app.updateDeferred = true
		app.updatePhase = "deferred"
		app.updateProgressText = update.DeferMessage
	})

	app.runUpdateCheck(true)
	// Flush async onUI callback.
	_ = app.call(context.Background(), func() {})

	_ = app.call(context.Background(), func() {
		if app.updatePending || app.updateDeferred || app.updatePhase != "" || app.updateNotice != "" {
			t.Fatalf("up-to-date check left stale deferred state: notice=%q pending=%v deferred=%v phase=%q",
				app.updateNotice, app.updatePending, app.updateDeferred, app.updatePhase)
		}
	})
}

func TestDeferredInstallCancelDoesNotResurrectPending(t *testing.T) {
	stubUpdateHooks(t)

	app := NewApp(nil, nil, nil, t.TempDir())
	app.BindHost(&updateTestHost{})
	app.StartActor()
	defer app.StopActor()

	svc := &CoreService{app: app}
	for i := 0; i < 20; i++ {
		_ = app.call(context.Background(), func() {
			app.running = true
			app.updateNotice = "0.1.43"
			app.updateRel = update.Release{Tag: "v0.1.43"}
			app.updatePending = false
			app.updateDeferred = false
			app.updateDownloading = false
		})
		// Trigger deferred install and immediately cancel on the next actor turn.
		svc.UpdateAction(context.Background(), "install")
		svc.UpdateAction(context.Background(), "cancel")
		time.Sleep(10 * time.Millisecond)

		_ = app.call(context.Background(), func() {
			if app.updatePending || app.updateDeferred || app.updateDownloading {
				t.Fatalf("iter %d: cancel after deferred install left stale state: pending=%v deferred=%v downloading=%v",
					i, app.updatePending, app.updateDeferred, app.updateDownloading)
			}
		})
	}
}
