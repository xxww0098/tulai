package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var errNoStore = errors.New("没有配置库")

// RunningExecutable is the file an in-place update should replace.
// Inside an AppImage this is the APPIMAGE path, not the mounted binary.
func RunningExecutable() (string, error) {
	if app := strings.TrimSpace(os.Getenv("APPIMAGE")); app != "" {
		return app, nil
	}
	return os.Executable()
}

// MarkStarted writes the stamp the restart helper waits for.
// Call it only after the process image is actually running.
// An AppImage's stamp is the APPIMAGE path, which is the file that was replaced.
func MarkStarted(executable string) {
	for _, path := range startedPaths(executable) {
		_ = os.WriteFile(path+".started", []byte("ok\n"), 0o644)
	}
}

// CommitPrevious drops the backup kept for rollback. A missing backup is fine.
func CommitPrevious(executable string) {
	for _, path := range startedPaths(executable) {
		if dir, ok := installDir(path); ok {
			_ = os.RemoveAll(dir + ".previous")
		}
		_ = os.Remove(path + ".previous")
		_ = os.Remove(path + ".started")
	}
}

func startedPaths(executable string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		out = append(out, path)
	}
	add(executable)
	add(os.Getenv("APPIMAGE"))
	return out
}

// ConfirmStart runs `version` and requires a non-empty stdout.
// The real binary handles that command before any window opens.
func ConfirmStart(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("没有可执行文件")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return fmt.Errorf("新版本没有启动: %w", err)
		}
		return fmt.Errorf("新版本没有启动: %w (%s)", err, text)
	}
	if strings.TrimSpace(string(out)) == "" {
		return errors.New("新版本没有输出版本号")
	}
	return nil
}

func confirmAndKeep(path string) error {
	if err := ConfirmStart(path); err != nil {
		if rb := rollbackInstalled(path); rb != nil {
			return fmt.Errorf("新版本无法启动，回滚失败: %v（%w）", rb, err)
		}
		return fmt.Errorf("新版本无法启动，已恢复上一版: %w", err)
	}
	return nil
}

func rollbackInstalled(installed string) error {
	if dir, ok := installDir(installed); ok {
		return RollbackDir(dir)
	}
	return RollbackFile(installed)
}

func installDir(installed string) (string, bool) {
	if installed == "" {
		return "", false
	}
	if bundle, ok := MacBundle(installed); ok {
		return bundle, true
	}
	dir := filepath.Dir(installed)
	if filepath.Base(dir) == appName+".app" || strings.EqualFold(filepath.Ext(dir), ".app") {
		return dir, true
	}
	return "", false
}

// RollbackDir puts dest.previous back. With no backup, dest is removed.
func RollbackDir(dest string) error {
	backup := dest + ".previous"
	if _, err := os.Stat(backup); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.RemoveAll(dest)
		}
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return os.Rename(backup, dest)
}

// RollbackFile puts dest.previous back over dest.
func RollbackFile(dest string) error {
	backup := dest + ".previous"
	if _, err := os.Stat(backup); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return os.Remove(dest)
		}
		return err
	}
	_ = os.Remove(dest)
	return os.Rename(backup, dest)
}

func replaceFile(dest, src string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".new"
	if err := copyFile(src, tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	backup := dest + ".previous"
	_ = os.Remove(backup)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, backup); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("无法移开现有程序: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = os.Rename(backup, dest)
		}
		os.Remove(tmp)
		return err
	}
	return nil
}

// Open asks the system to show a downloaded package.
func Open(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("没有可打开的文件")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// RestartScript waits for this process to exit, starts the new binary, and
// puts the backup back if that process dies before it writes the stamp.
func RestartScript() string {
	return `#!/bin/sh
set -eu
pid="$1"
target="$2"
backup="$3"
stamp="$4"
rounds="${5:-20}"
unit="${6:-file}"
dest="${7:-$target}"
while kill -0 "$pid" 2>/dev/null; do
  sleep 0.2
done
rm -f "$stamp"
STAMP="$stamp" "$target" >/dev/null 2>&1 &
child=$!
i=0
while [ "$i" -lt "$rounds" ]; do
  if [ -f "$stamp" ]; then
    rm -rf "$backup"
    rm -f "$stamp"
    exit 0
  fi
  if ! kill -0 "$child" 2>/dev/null; then
    break
  fi
  i=$((i + 1))
  sleep 0.2
done
if kill -0 "$child" 2>/dev/null; then
  rm -rf "$backup"
  rm -f "$stamp"
  exit 0
fi
if [ ! -e "$backup" ]; then
  exit 1
fi
if [ "$unit" = "dir" ]; then
  rm -rf "$dest"
  mv "$backup" "$dest"
else
  rm -f "$dest"
  mv "$backup" "$dest"
fi
chmod +x "$target" 2>/dev/null || true
"$target" >/dev/null 2>&1 &
exit 1
`
}

func writeRestartScript() (string, error) {
	dir, err := os.MkdirTemp("", "tulai-restart-")
	if err != nil {
		return "", err
	}
	script := filepath.Join(dir, "restart.sh")
	if err := os.WriteFile(script, []byte(RestartScript()), 0o755); err != nil {
		return "", err
	}
	return script, nil
}

func restartPlan(installed string) (target, backup, dest, unit string) {
	target = installed
	if dir, ok := installDir(installed); ok {
		return installed, dir + ".previous", dir, "dir"
	}
	return installed, installed + ".previous", installed, "file"
}

func restartArgs(script string, pid int, installed string) []string {
	target, backup, dest, unit := restartPlan(installed)
	if pid == 0 {
		pid = os.Getpid()
	}
	return []string{script, strconv.Itoa(pid), target, backup, target + ".started", "20", unit, dest}
}
