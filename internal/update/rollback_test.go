package update

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInstallRollsBackWhenBinaryFails(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "app.tar.gz")
	writeTar(t, archive, map[string]string{
		"tulai": "#!/bin/sh\nexit 1\n",
	})
	home := filepath.Join(dir, "home")
	exe := filepath.Join(home, ".local", "tulai.app", "tulai")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Install(Options{
		Mode:       ModeLinuxUser,
		Executable: exe,
		AssetPath:  archive,
		Version:    "0.2.0",
		Home:       home,
		DataHome:   filepath.Join(home, ".local", "share"),
	})
	if err == nil || !strings.Contains(err.Error(), "已恢复上一版") {
		t.Fatalf("err = %v", err)
	}
	body, err := os.ReadFile(exe)
	if err != nil || string(body) != "old-binary" {
		t.Fatalf("rolled back body %q err %v", body, err)
	}
}

func TestReplaceFileRollsBack(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "tulai.AppImage")
	if err := os.WriteFile(dest, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "next")
	if err := os.WriteFile(src, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Install(Options{
		Mode:       ModeLinuxBinary,
		Executable: dest,
		AssetPath:  src,
		Version:    "0.2.0",
	})
	if err == nil || !strings.Contains(err.Error(), "已恢复上一版") {
		t.Fatalf("err = %v", err)
	}
	body, err := os.ReadFile(dest)
	if err != nil || !strings.Contains(string(body), "old") {
		t.Fatalf("body %q err %v", body, err)
	}
}

func TestRestartScriptRestoresFailedStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell watcher")
	}
	root := t.TempDir()
	app := filepath.Join(root, "tulai.app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(app, "tulai")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	backup := app + ".previous"
	if err := os.MkdirAll(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "tulai"), []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "watch.sh")
	if err := os.WriteFile(script, []byte(RestartScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	sleep := exec.Command("sleep", "0.2")
	if err := sleep.Start(); err != nil {
		t.Fatal(err)
	}
	pid := sleep.Process.Pid
	_ = sleep.Wait()
	stamp := target + ".started"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/bin/sh", script, strconv.Itoa(pid), target, backup, stamp, "3", "dir", app).CombinedOutput()
	body, readErr := os.ReadFile(target)
	if readErr != nil || !strings.Contains(string(body), "old") {
		t.Fatalf("body %q read %v script %v\n%s", body, readErr, err, out)
	}
	if _, statErr := os.Stat(backup); !os.IsNotExist(statErr) {
		t.Fatal("backup still present after restore")
	}
}

func TestRestartScriptKeepsRunningBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell watcher")
	}
	root := t.TempDir()
	target := filepath.Join(root, "tulai")
	if err := os.WriteFile(target, []byte("#!/bin/sh\necho ok > \"$STAMP\"\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	backup := target + ".previous"
	if err := os.WriteFile(backup, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "watch.sh")
	if err := os.WriteFile(script, []byte(RestartScript()), 0o755); err != nil {
		t.Fatal(err)
	}
	sleep := exec.Command("sleep", "0.1")
	if err := sleep.Start(); err != nil {
		t.Fatal(err)
	}
	pid := sleep.Process.Pid
	_ = sleep.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "/bin/sh", script, strconv.Itoa(pid), target, backup, target+".started", "5", "file", target).CombinedOutput(); err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("backup kept after a successful start")
	}
	body, err := os.ReadFile(target)
	if err != nil || !strings.Contains(string(body), "STAMP") {
		t.Fatalf("new binary was replaced: %q %v", body, err)
	}
}

func TestCommitPreviousCleansVersionedLeftovers(t *testing.T) {
	// Include brackets to ensure cleanup does not rely on fragile globbing.
	dir := filepath.Join(t.TempDir(), "app [1]")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "tulai.exe")
	keep := []string{
		exe,
		filepath.Join(dir, "other.exe.previous"),
		filepath.Join(dir, "other.exe.previous.99"),
	}
	drop := []string{
		exe + ".previous",
		exe + ".previous.1234",
		exe + ".failed.5678",
		exe + ".new",
		exe + ".started",
	}
	for _, p := range append(keep, drop...) {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	CommitPrevious(exe)

	for _, p := range drop {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("leftover %s was not removed (err=%v)", p, err)
		}
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to remain: %v", p, err)
		}
	}
}

func TestCommitPreviousCleansBundlePreviousAndStagedNew(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS bundle layout test")
	}
	root := t.TempDir()
	bundle := filepath.Join(root, "tulai.app")
	exe := filepath.Join(bundle, "Contents", "MacOS", "tulai")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevBundle := bundle + ".previous"
	stagedBundle := bundle + ".new"
	unrelatedBundle := filepath.Join(root, "other.app.new")
	for _, d := range []string{prevBundle, stagedBundle, unrelatedBundle} {
		if err := os.MkdirAll(filepath.Join(d, "Contents", "MacOS"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "Contents", "MacOS", "tulai"), []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(exe+".started", []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	CommitPrevious(exe)

	for _, removed := range []string{prevBundle, stagedBundle, exe + ".started"} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be removed (err=%v)", removed, err)
		}
	}
	for _, kept := range []string{exe, unrelatedBundle} {
		if _, err := os.Stat(kept); err != nil {
			t.Fatalf("expected %s to remain: %v", kept, err)
		}
	}
}

func TestRestartScriptIgnoresHUPAndCapsWait(t *testing.T) {
	script := RestartScript()
	for _, needle := range []string{
		"trap '' HUP", "wait_i=0", `"$wait_i" -lt 600`, "cleanup_self", "open_target",
		"target_running", "start_direct_and_confirm", `-n "$dest"`, "</dev/null",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("RestartScript missing %q:\n%s", needle, script)
		}
	}
	if runtime.GOOS != "windows" {
		path := filepath.Join(t.TempDir(), "restart.sh")
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("/bin/sh", "-n", path).CombinedOutput(); err != nil {
			t.Fatalf("RestartScript syntax error: %v\n%s", err, out)
		}
	}
}

func TestConfirmStartVersionRejectsOldSemver(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell binary test")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "tulai")

	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 0.1.41\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmStartVersion(bin, "0.1.42"); err == nil || !strings.Contains(err.Error(), "新版本不匹配") {
		t.Fatalf("expected version mismatch error, got %v", err)
	}
	if err := ConfirmStartVersion(bin, "v0.1.41"); err != nil {
		t.Fatalf("matching version rejected: %v", err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho updated\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ConfirmStartVersion(bin, "0.1.42"); err != nil {
		t.Fatalf("non-semver test stub rejected: %v", err)
	}
}

func TestInstallLinuxBinaryRollsBackOnWrongVersion(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell binary test")
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "tulai.AppImage")
	if err := os.WriteFile(dest, []byte("#!/bin/sh\necho 0.1.41\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "wrong-ver")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho 0.1.40\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Install(Options{
		Mode:       ModeLinuxBinary,
		Executable: dest,
		AssetPath:  src,
		Version:    "0.1.42",
	})
	if err == nil || !strings.Contains(err.Error(), "新版本不匹配") || !strings.Contains(err.Error(), "已恢复上一版") {
		t.Fatalf("err = %v", err)
	}
	body, err := os.ReadFile(dest)
	if err != nil || !strings.Contains(string(body), "0.1.41") {
		t.Fatalf("expected rollback to 0.1.41, got %q err=%v", body, err)
	}
}

func TestRestartScriptStagedMacOpenSwapAndRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell watcher")
	}

	t.Run("success swaps staged bundle and cleans restart dir", func(t *testing.T) {
		root := t.TempDir()
		dest := filepath.Join(root, "tulai.app")
		staged := dest + ".new"
		backup := dest + ".previous"
		target := filepath.Join(dest, "Contents", "MacOS", "tulai")
		stamp := target + ".started"
		ranLog := filepath.Join(root, "ran.txt")

		for _, d := range []string{filepath.Join(dest, "Contents", "MacOS"), filepath.Join(staged, "Contents", "MacOS")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(target, []byte("#!/bin/sh\necho old >> "+shellQuote(ranLog)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		stagedBin := filepath.Join(staged, "Contents", "MacOS", "tulai")
		newScript := "#!/bin/sh\necho new >> " + shellQuote(ranLog) + "\necho ok > " + shellQuote(stamp) + "\n"
		if err := os.WriteFile(stagedBin, []byte(newScript), 0o755); err != nil {
			t.Fatal(err)
		}

		// Mock /usr/bin/open that requires -n and invokes Contents/MacOS/tulai inside the bundle.
		openLog := filepath.Join(root, "open.args")
		mockOpen := filepath.Join(root, "mock-open")
		mockOpenScript := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuote(openLog) + "\nif [ \"$1\" != \"-n\" ]; then exit 2; fi\n\"$2/Contents/MacOS/tulai\" &\n"
		if err := os.WriteFile(mockOpen, []byte(mockOpenScript), 0o755); err != nil {
			t.Fatal(err)
		}

		scriptDir := filepath.Join(root, "tulai-restart-test")
		if err := os.MkdirAll(scriptDir, 0o755); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(scriptDir, "restart.sh")
		if err := os.WriteFile(script, []byte(RestartScript()), 0o755); err != nil {
			t.Fatal(err)
		}

		sleep := exec.Command("sleep", "0.1")
		if err := sleep.Start(); err != nil {
			t.Fatal(err)
		}
		pid := sleep.Process.Pid
		_ = sleep.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args := restartArgsStaged(script, pid, target, staged, "open", 15)
		cmd := exec.CommandContext(ctx, "/bin/sh", args...)
		cmd.Env = append(os.Environ(), "OPEN_CMD="+mockOpen)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("staged restart failed: %v\n%s", err, out)
		}

		openArgs, err := os.ReadFile(openLog)
		if err != nil || !strings.HasPrefix(strings.TrimSpace(string(openArgs)), "-n ") {
			t.Fatalf("expected open to be called with -n, got %q err=%v", openArgs, err)
		}
		got, err := os.ReadFile(ranLog)
		if err != nil || strings.TrimSpace(string(got)) != "new" {
			t.Fatalf("expected new bundle to run, got %q err=%v", got, err)
		}
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Fatal("backup was not cleaned after successful staged swap")
		}
		if _, err := os.Stat(staged); !os.IsNotExist(err) {
			t.Fatal("staged bundle still exists")
		}
		if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
			t.Fatal("tulai-restart-* temp dir was not cleaned up")
		}
	})

	t.Run("crash rolls back staged bundle and reopens old version", func(t *testing.T) {
		root := t.TempDir()
		dest := filepath.Join(root, "tulai.app")
		staged := dest + ".new"
		backup := dest + ".previous"
		target := filepath.Join(dest, "Contents", "MacOS", "tulai")
		ranLog := filepath.Join(root, "ran.txt")

		for _, d := range []string{filepath.Join(dest, "Contents", "MacOS"), filepath.Join(staged, "Contents", "MacOS")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(target, []byte("#!/bin/sh\necho old >> "+shellQuote(ranLog)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		stagedBin := filepath.Join(staged, "Contents", "MacOS", "tulai")
		if err := os.WriteFile(stagedBin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		mockOpen := filepath.Join(root, "mock-open")
		if err := os.WriteFile(mockOpen, []byte("#!/bin/sh\nif [ \"$1\" != \"-n\" ]; then exit 2; fi\n\"$2/Contents/MacOS/tulai\" &\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		scriptDir := filepath.Join(root, "tulai-restart-crash")
		if err := os.MkdirAll(scriptDir, 0o755); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(scriptDir, "restart.sh")
		if err := os.WriteFile(script, []byte(RestartScript()), 0o755); err != nil {
			t.Fatal(err)
		}

		sleep := exec.Command("sleep", "0.1")
		if err := sleep.Start(); err != nil {
			t.Fatal(err)
		}
		pid := sleep.Process.Pid
		_ = sleep.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args := restartArgsStaged(script, pid, target, staged, "open", 3)
		cmd := exec.CommandContext(ctx, "/bin/sh", args...)
		cmd.Env = append(os.Environ(), "OPEN_CMD="+mockOpen)
		_ = cmd.Run()

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if body, err := os.ReadFile(ranLog); err == nil && strings.Contains(string(body), "old") {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		body, readErr := os.ReadFile(target)
		if readErr != nil || !strings.Contains(string(body), "old") {
			t.Fatalf("expected rolled back old bundle, got %q err=%v", body, readErr)
		}
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Fatal("backup still present after rollback")
		}
		if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
			t.Fatal("tulai-restart-* temp dir was not cleaned up on rollback")
		}
	})

	t.Run("open failure falls back to direct target launch without rollback", func(t *testing.T) {
		root := t.TempDir()
		dest := filepath.Join(root, "tulai.app")
		staged := dest + ".new"
		backup := dest + ".previous"
		target := filepath.Join(dest, "Contents", "MacOS", "tulai")
		stamp := target + ".started"
		ranLog := filepath.Join(root, "ran.txt")

		for _, d := range []string{filepath.Join(dest, "Contents", "MacOS"), filepath.Join(staged, "Contents", "MacOS")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(target, []byte("#!/bin/sh\necho old >> "+shellQuote(ranLog)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		stagedBin := filepath.Join(staged, "Contents", "MacOS", "tulai")
		newScript := "#!/bin/sh\necho new-direct >> " + shellQuote(ranLog) + "\necho ok > " + shellQuote(stamp) + "\n"
		if err := os.WriteFile(stagedBin, []byte(newScript), 0o755); err != nil {
			t.Fatal(err)
		}
		// Mock /usr/bin/open that always fails (non-zero exit).
		mockOpen := filepath.Join(root, "mock-open-fail")
		if err := os.WriteFile(mockOpen, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		scriptDir := filepath.Join(root, "tulai-restart-openfail")
		if err := os.MkdirAll(scriptDir, 0o755); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(scriptDir, "restart.sh")
		if err := os.WriteFile(script, []byte(RestartScript()), 0o755); err != nil {
			t.Fatal(err)
		}

		sleep := exec.Command("sleep", "0.1")
		if err := sleep.Start(); err != nil {
			t.Fatal(err)
		}
		pid := sleep.Process.Pid
		_ = sleep.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args := restartArgsStaged(script, pid, target, staged, "open", 15)
		cmd := exec.CommandContext(ctx, "/bin/sh", args...)
		cmd.Env = append(os.Environ(), "OPEN_CMD="+mockOpen)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("direct fallback after open failure should succeed: %v\n%s", err, out)
		}

		got, err := os.ReadFile(ranLog)
		if err != nil || strings.TrimSpace(string(got)) != "new-direct" {
			t.Fatalf("expected direct launch of new binary, got %q err=%v", got, err)
		}
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Fatal("backup was not cleaned after direct fallback launch")
		}
		if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
			t.Fatal("tulai-restart-* temp dir was not cleaned up")
		}
	})

	t.Run("open failure plus direct crash rolls back to old bundle", func(t *testing.T) {
		root := t.TempDir()
		dest := filepath.Join(root, "tulai.app")
		staged := dest + ".new"
		backup := dest + ".previous"
		target := filepath.Join(dest, "Contents", "MacOS", "tulai")
		ranLog := filepath.Join(root, "ran.txt")

		for _, d := range []string{filepath.Join(dest, "Contents", "MacOS"), filepath.Join(staged, "Contents", "MacOS")} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(target, []byte("#!/bin/sh\necho old-after-openfail >> "+shellQuote(ranLog)+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		stagedBin := filepath.Join(staged, "Contents", "MacOS", "tulai")
		if err := os.WriteFile(stagedBin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		mockOpen := filepath.Join(root, "mock-open-fail")
		if err := os.WriteFile(mockOpen, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}

		scriptDir := filepath.Join(root, "tulai-restart-openfail-crash")
		if err := os.MkdirAll(scriptDir, 0o755); err != nil {
			t.Fatal(err)
		}
		script := filepath.Join(scriptDir, "restart.sh")
		if err := os.WriteFile(script, []byte(RestartScript()), 0o755); err != nil {
			t.Fatal(err)
		}

		sleep := exec.Command("sleep", "0.1")
		if err := sleep.Start(); err != nil {
			t.Fatal(err)
		}
		pid := sleep.Process.Pid
		_ = sleep.Wait()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		args := restartArgsStaged(script, pid, target, staged, "open", 3)
		cmd := exec.CommandContext(ctx, "/bin/sh", args...)
		cmd.Env = append(os.Environ(), "OPEN_CMD="+mockOpen)
		_ = cmd.Run()

		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if body, err := os.ReadFile(ranLog); err == nil && strings.Contains(string(body), "old-after-openfail") {
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
		body, readErr := os.ReadFile(target)
		if readErr != nil || !strings.Contains(string(body), "old-after-openfail") {
			t.Fatalf("expected rolled back old bundle, got %q err=%v", body, readErr)
		}
		if _, err := os.Stat(backup); !os.IsNotExist(err) {
			t.Fatal("backup still present after rollback")
		}
		if _, err := os.Stat(scriptDir); !os.IsNotExist(err) {
			t.Fatal("tulai-restart-* temp dir was not cleaned up")
		}
	})

	t.Run("stamp written before post-loop check does not roll back in open or direct mode", func(t *testing.T) {
		t.Run("open", func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "tulai.app")
			staged := dest + ".new"
			backup := dest + ".previous"
			target := filepath.Join(dest, "Contents", "MacOS", "tulai")
			stamp := target + ".started"

			for _, d := range []string{filepath.Join(dest, "Contents", "MacOS"), filepath.Join(staged, "Contents", "MacOS")} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(target, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			stagedBin := filepath.Join(staged, "Contents", "MacOS", "tulai")
			if err := os.WriteFile(stagedBin, []byte("#!/bin/sh\necho new\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			// Simulate the stamp appearing right as the poll loop exits (rounds=0 skips the while body
			// so only the post-loop check runs; prior script checked only ps and falsely rolled back).
			mockOpen := filepath.Join(root, "mock-open")
			mockOpenScript := "#!/bin/sh\necho ok > " + shellQuote(stamp) + "\nexit 0\n"
			if err := os.WriteFile(mockOpen, []byte(mockOpenScript), 0o755); err != nil {
				t.Fatal(err)
			}

			scriptDir := filepath.Join(root, "tulai-restart-lastround-open")
			if err := os.MkdirAll(scriptDir, 0o755); err != nil {
				t.Fatal(err)
			}
			script := filepath.Join(scriptDir, "restart.sh")
			if err := os.WriteFile(script, []byte(RestartScript()), 0o755); err != nil {
				t.Fatal(err)
			}

			sleep := exec.Command("sleep", "0.05")
			if err := sleep.Start(); err != nil {
				t.Fatal(err)
			}
			pid := sleep.Process.Pid
			_ = sleep.Wait()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			args := []string{script, strconv.Itoa(pid), target, backup, stamp, "0", "dir", dest, staged, "open"}
			cmd := exec.CommandContext(ctx, "/bin/sh", args...)
			cmd.Env = append(os.Environ(), "OPEN_CMD="+mockOpen)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("open mode falsely rolled back when stamp existed at post-loop check: %v\n%s", err, out)
			}
			body, err := os.ReadFile(target)
			if err != nil || strings.Contains(string(body), "old") {
				t.Fatalf("open mode rolled back to old binary: %q err=%v", body, err)
			}
			if _, err := os.Stat(backup); !os.IsNotExist(err) {
				t.Fatal("open mode left backup behind after confirmed start")
			}
		})

		t.Run("direct", func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "tulai.app")
			staged := dest + ".new"
			backup := dest + ".previous"
			target := filepath.Join(dest, "Contents", "MacOS", "tulai")
			stamp := target + ".started"

			for _, d := range []string{filepath.Join(dest, "Contents", "MacOS"), filepath.Join(staged, "Contents", "MacOS")} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(target, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			// With rounds=2 (0.4s budget), write stamp at ~0.22s (during the final 0.2s sleep)
			// and exit immediately so only the post-loop [ -f "$stamp" ] check confirms startup.
			stagedBin := filepath.Join(staged, "Contents", "MacOS", "tulai")
			newScript := "#!/bin/sh\nsleep 0.22\necho ok > " + shellQuote(stamp) + "\nexit 0\n"
			if err := os.WriteFile(stagedBin, []byte(newScript), 0o755); err != nil {
				t.Fatal(err)
			}

			scriptDir := filepath.Join(root, "tulai-restart-lastround-direct")
			if err := os.MkdirAll(scriptDir, 0o755); err != nil {
				t.Fatal(err)
			}
			script := filepath.Join(scriptDir, "restart.sh")
			if err := os.WriteFile(script, []byte(RestartScript()), 0o755); err != nil {
				t.Fatal(err)
			}

			sleep := exec.Command("sleep", "0.05")
			if err := sleep.Start(); err != nil {
				t.Fatal(err)
			}
			pid := sleep.Process.Pid
			_ = sleep.Wait()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			args := restartArgsStaged(script, pid, target, staged, "direct", 2)
			cmd := exec.CommandContext(ctx, "/bin/sh", args...)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("direct mode falsely rolled back when stamp appeared during final round: %v\n%s", err, out)
			}
			body, err := os.ReadFile(target)
			if err != nil || strings.Contains(string(body), "old") {
				t.Fatalf("direct mode rolled back to old binary: %q err=%v", body, err)
			}
			if _, err := os.Stat(backup); !os.IsNotExist(err) {
				t.Fatal("direct mode left backup behind after confirmed start")
			}
		})
	})
}
