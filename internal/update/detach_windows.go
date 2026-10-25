//go:build windows

package update

import (
	"os/exec"
	"syscall"
)

// StartDetached starts helper outside this process so it can replace the executable.
func StartDetached(path string, args ...string) error {
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008, // DETACHED_PROCESS
		HideWindow:    true,
	}
	return cmd.Start()
}
