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
