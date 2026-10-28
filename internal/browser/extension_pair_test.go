package browser

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
)

func TestCfTExtensionPairs(t *testing.T) {
	if testing.Short() {
		t.Skip("短测不下载、不启动 Chrome for Testing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	rt, err := EnsureCfT(ctx, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pairExtension(t, rt.Path, []string{"--headless=new", "--disable-gpu"}, "cft")
}

func TestEdgeExtensionPairs(t *testing.T) {
	if os.Getenv("TULAI_EDGE_E2E") != "1" {
		t.Skip("设置 TULAI_EDGE_E2E=1 才启动 Microsoft Edge")
	}
	edge, err := FindEdgePath()
	if err != nil {
		t.Fatal(err)
	}
	ensureXvfb(t)
	pairExtension(t, edge, nil, "edge-ext")
}

func pairExtension(t *testing.T, bin string, extra []string, label string) {
	t.Helper()
	extDir, err := extbundle.Install(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	token, err := extbridge.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bridgePort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	srv, err := extbridge.Listen(extbridge.Config{
		Addr:  fmt.Sprintf("127.0.0.1:%d", bridgePort),
		Token: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	pair, err := extbridge.StartPair(func() extbridge.PairInfo {
		return extbridge.PairInfo{Token: token, Port: strconv.Itoa(bridgePort)}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pair.Close() })

	profile := t.TempDir()
	port := freePort(t)
	cmd := exec.Command(bin, launchArgs(port, profile, "about:blank", extDir, extra)...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = exec.Command("pkill", "-f", profile).Run()
	})
	ws, err := WaitWSURL(port, 25*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := cdp.EnsureExtension(context.Background(), ws, extDir); err != nil {
		t.Fatalf("ensure extension: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	var qianwen, doubao bool
	var last string
	for time.Now().Before(deadline) {
		qianwen, doubao, last, _ = srv.Snapshot()
		if !pair.LastHit().IsZero() && (qianwen || doubao) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	ver := browserVersion(bin)
	line := fmt.Sprintf("%s binary=%s version=%s ext=%s pair=%v qianwen=%v doubao=%v helloErr=%s\n",
		label, bin, ver, extDir, !pair.LastHit().IsZero(), qianwen, doubao, last)
	t.Log(line)
	if dir := os.Getenv("TULAI_EDGE_EVIDENCE"); dir != "" {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(filepath.Join(dir, label+"-pair.txt"), []byte(line), 0o644)
	}
	if pair.LastHit().IsZero() || (!qianwen && !doubao) {
		t.Fatalf("extension did not pair: %s", line)
	}
	_ = bin
}
