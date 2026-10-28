package browser

import (
	"bytes"
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
)

func TestLaunchPinnedCfT(t *testing.T) {
	if testing.Short() {
		t.Skip("短测不下载、不启动 Chrome for Testing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	dir := t.TempDir()
	rt, err := EnsureCfT(ctx, dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	verOut, verErr := exec.CommandContext(ctx, rt.Path, "--version").CombinedOutput()
	if verErr != nil {
		if launchBlocked(string(verOut) + verErr.Error()) {
			t.Skip("当前环境无法启动 Chrome for Testing: " + strings.TrimSpace(string(verOut)))
		}
		t.Fatalf("--version: %v %s", verErr, verOut)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	userData := t.TempDir()
	cmd := exec.Command(rt.Path, debugArgs(port, userData)...)
	cmd.SysProcAttr = testProcAttr()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.Process != nil {
			killTree(cmd.Process.Pid)
			_ = cmd.Process.Kill()
		}
	}()
	ws, err := WaitWSURL(port, 30*time.Second)
	if err != nil {
		msg := stderr.String()
		if launchBlocked(msg) {
			t.Skip("当前环境无法启动 Chrome for Testing: " + strings.TrimSpace(msg))
		}
		t.Fatalf("%v %s", err, msg)
	}
	client, err := cdp.NewClient(ws)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := client.NewSession("about:blank", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	webdriver, err := sess.Evaluate("navigator.webdriver")
	if err != nil {
		t.Fatal(err)
	}
	if webdriver != "false" {
		t.Fatalf("navigator.webdriver = %s", webdriver)
	}
}

func launchBlocked(msg string) bool {
	msg = strings.ToLower(msg)
	for _, needle := range []string{
		"shared librar",
		"cannot open display",
		"missing x server",
		"ozone",
		"platform failed to initialize",
		"no usable sandbox",
		"failed to move to new namespace",
		"zygote",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}
