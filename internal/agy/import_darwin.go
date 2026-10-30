//go:build darwin

package agy

import (
	"context"
	"io"
	"os/exec"
	"time"
)

// keychainHasToken 看当前用户钥匙串里有没有 Antigravity 令牌。
// agy 用 zalando/go-keyring：macOS 经 /usr/bin/security 访问钥匙串，Linux 用 Secret Service，
// 没有钥匙串时才退回文件 antigravity-oauth-token。钥匙串按用户而不是按 HOME，
// 所以同一台 Mac 上只能用这一个钥匙串账号。这里只看退出码，不用 -w/-g，也不读输出。
func keychainHasToken() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", keychainProbeArgs()...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}
