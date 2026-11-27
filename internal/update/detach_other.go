//go:build !windows

package update

import (
	"os"
	"os/exec"
	"syscall"
)

// StartDetached starts helper in a detached session and does not wait for it.
func StartDetached(path string, args ...string) error {
	cmd := exec.Command(path, args...)
	if dir := os.TempDir(); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			cmd.Dir = dir
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	return nil
}
