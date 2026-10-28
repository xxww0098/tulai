package cdp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func egoPlatformCommand(ctx context.Context, path, entry string) (*exec.Cmd, error) {
	if ext := strings.ToLower(filepath.Ext(path)); ext != ".cmd" && ext != ".bat" {
		cmd := exec.CommandContext(ctx, path, "nodejs", "-e", entry)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		return cmd, nil
	}
	shell, err := exec.LookPath("cmd.exe")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, shell)
	cmd.Args = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CmdLine: egoBatchCommandLine(entry)}
	cmd.Env = append(os.Environ(), "TULAI_EGO_LAUNCHER="+path)
	cmd.WaitDelay = 3 * time.Second
	cmd.Cancel = func() error {
		killCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		kill := exec.CommandContext(killCtx, filepath.Join(filepath.Dir(shell), "taskkill.exe"), "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := kill.Run(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	return cmd, nil
}
