//go:build !darwin

package browser

import (
	"errors"
	"os"
	"os/exec"
)

func adoptOpenedBrowser(cmd *exec.Cmd, _ string) (*os.Process, error) {
	if cmd != nil {
		_ = cmd.Wait()
	}
	return nil, errors.New("open 启动只用于 macOS")
}

func watchOpened(_ int, done chan struct{}) {
	if done != nil {
		close(done)
	}
}
