//go:build !windows

package cdp

import (
	"context"
	"os/exec"
)

func egoPlatformCommand(ctx context.Context, path, entry string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, path, "nodejs", "-e", entry), nil
}
