//go:build !windows

package update

import "os/exec"

// StartDetached starts helper and does not wait for it.
func StartDetached(path string, args ...string) error {
	cmd := exec.Command(path, args...)
	return cmd.Start()
}
