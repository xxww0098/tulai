//go:build windows

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// StartDetached starts helper outside this process so it can replace the executable.
func StartDetached(path string, args ...string) error {
	cmd := exec.Command(resolveHelperPath(path), args...)
	if dir := os.TempDir(); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			cmd.Dir = dir
		}
	}
	// Windows PowerShell 5.1 (ConsoleHost) exits immediately if started with
	// DETACHED_PROCESS because it has no console; CREATE_NO_WINDOW hides its
	// console without stripping its console subsystem environment, while GUI
	// children started with Start-Process still open visibly.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow,
		HideWindow:    true,
	}
	return cmd.Start()
}

func resolveHelperPath(path string) string {
	if !strings.EqualFold(strings.TrimSpace(path), "powershell.exe") {
		return path
	}
	root := strings.TrimSpace(os.Getenv("SystemRoot"))
	if root == "" {
		root = strings.TrimSpace(os.Getenv("WINDIR"))
	}
	if root == "" {
		return path
	}
	candidate := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return path
}
