package agy

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
)

type systemKeyring struct{}

func (systemKeyring) Read(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "secret-tool", "lookup", "service", "gemini", "username", "antigravity")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 1 && len(out) == 0 && len(exit.Stderr) == 0 {
			return nil, nil
		}
		return nil, errors.New("读取 Antigravity 系统凭据失败，请检查 secret-tool 和 Secret Service")
	}
	return bytes.TrimSpace(out), nil
}

func (systemKeyring) Write(ctx context.Context, data []byte) error {
	cmd := exec.CommandContext(ctx, "secret-tool", "store", "--label=gemini", "service", "gemini", "username", "antigravity")
	cmd.Stdin = bytes.NewReader(data)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("写入 Antigravity 系统凭据失败，请检查 secret-tool 和 Secret Service")
	}
	return nil
}
