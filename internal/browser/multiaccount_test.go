package browser

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
)

func TestEdgeLoadUnpacked(t *testing.T) {
	if os.Getenv("TULAI_EDGE_E2E") != "1" {
		t.Skip("设置 TULAI_EDGE_E2E=1 才启动 Microsoft Edge")
	}
	edge, err := FindEdgePath()
	if err != nil {
		t.Fatal(err)
	}
	ensureXvfb(t)
	extDir, err := extbundle.Install(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profile := t.TempDir()
	port := freePort(t)
	args := launchArgs(port, profile, "about:blank", "", nil)
	args = append(args[:len(args)-1], "--enable-unsafe-extension-debugging", args[len(args)-1])
	var stderr bytes.Buffer
	cmd := exec.Command(edge, args...)
	cmd.Stderr = &stderr
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
		t.Fatalf("%v\nstderr:\n%s", err, stderr.String())
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, _, found, err := cdp.ExtensionPairState(context.Background(), ws)
		if err != nil {
			t.Fatal(err)
		}
		if found {
			t.Fatal("Edge loaded the page bridge without --load-extension or Extensions.loadUnpacked")
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err := cdp.EnsureExtension(context.Background(), ws, extDir); err != nil {
		t.Fatalf("Extensions.loadUnpacked: %v", err)
	}
	if _, _, found, err := cdp.ExtensionPairState(context.Background(), ws); err != nil || !found {
		t.Fatalf("loadUnpacked did not install the page bridge: found=%v err=%v", found, err)
	}
}

func TestCfTMultiAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("短测不下载、不启动 Chrome for Testing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	rt, err := EnsureCfT(ctx, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
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

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "<html><head><title>%s</title></head><body><p>%s</p></body></html>", r.URL.Path, r.URL.Path)
	}))
	t.Cleanup(page.Close)

	type opened struct {
		profile string
		port    int
		url     string
		cmd     *exec.Cmd
	}
	var runs []opened
	for _, path := range []string{"/account-a", "/account-b"} {
		profile := t.TempDir()
		port := freePort(t)
		start := page.URL + path
		args := launchArgs(port, profile, start, extDir, []string{"--headless=new", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage"})
		cmd := exec.Command(rt.Path, args...)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		runs = append(runs, opened{profile: profile, port: port, url: start, cmd: cmd})
	}
	t.Cleanup(func() {
		for _, run := range runs {
			if run.cmd.Process != nil {
				_ = run.cmd.Process.Kill()
			}
			_ = exec.Command("pkill", "-f", run.profile).Run()
		}
	})

	ids := map[string]string{}
	for _, run := range runs {
		ws, err := WaitWSURL(run.port, 25*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		raw := profileCmdline(run.profile)
		if !strings.Contains(raw, "--load-extension="+extDir) || !strings.Contains(raw, "--user-data-dir="+run.profile) {
			t.Fatalf("CfT launch args missing extension or profile: %s", raw)
		}
		deadline := time.Now().Add(20 * time.Second)
		var profileID, gotToken string
		var found bool
		for time.Now().Before(deadline) {
			profileID, gotToken, found, err = cdp.ExtensionPairState(context.Background(), ws)
			if err == nil && found && profileID != "" && gotToken == token {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
		if !found || profileID == "" || gotToken != token {
			t.Fatalf("CfT --load-extension did not pair %s: found=%v id=%q token=%q err=%v", run.url, found, profileID, gotToken, err)
		}
		if prev, ok := ids[profileID]; ok {
			t.Fatalf("profile id %s collided between %s and %s", profileID, prev, run.profile)
		}
		ids[profileID] = run.profile
		visible := waitVisible(t, ws, run.url)
		if !strings.HasPrefix(visible, run.url) || strings.Contains(pageList(run.port), "about:blank") {
			t.Fatalf("CfT page %s visible=%q list=%s", run.url, visible, pageList(run.port))
		}
	}
	if len(ids) != 2 {
		t.Fatalf("profile ids = %v", ids)
	}
	deadline := time.Now().Add(15 * time.Second)
	seen := map[string]bool{}
	for time.Now().Before(deadline) && len(seen) < 2 {
		for _, sess := range srv.Sessions("") {
			if sess.Account() != "" {
				seen[sess.Account()] = true
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(seen) != 2 {
		t.Fatalf("bridge sessions = %v, want 2 profile ids", seen)
	}
}
