package agy

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"os/exec"
	"strings"
)

type systemKeyring struct{}

var securityCommand = func(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "/usr/bin/security", args...)
}

func (systemKeyring) Read(ctx context.Context) ([]byte, error) {
	out, err := securityCommand(ctx, "find-generic-password", "-s", "gemini", "-a", "antigravity", "-w").Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 44 {
			return nil, nil
		}
		return nil, errors.New("读取 Antigravity 钥匙串失败")
	}
	value := strings.TrimSpace(string(out))
	if strings.HasPrefix(value, "go-keyring-base64:") {
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "go-keyring-base64:"))
		if err != nil {
			return nil, errCredential
		}
		return data, nil
	}
	return []byte(value), nil
}

func (systemKeyring) Write(ctx context.Context, data []byte) error {
	// stdin keeps the secret out of process arguments; -U preserves the item's access policy.
	cmd := securityCommand(ctx, "-i")
	cmd.Stdin = strings.NewReader("add-generic-password -U -s gemini -a antigravity -w go-keyring-base64:" + base64.StdEncoding.EncodeToString(data) + "\n")
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("写入 Antigravity 钥匙串失败")
	}
	// security's interactive mode can exit zero after a command fails.
	saved, err := (systemKeyring{}).Read(ctx)
	if err != nil || !bytes.Equal(saved, data) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("写入 Antigravity 钥匙串失败")
	}
	return nil
}
