package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
)

func TestChromeLoginLaunchLeavesVisiblePage(t *testing.T) {
	if testing.Short() {
		t.Skip("短测不启动浏览器")
	}
	chrome, err := FindChromePath()
	if err != nil {
		t.Skip(err)
	}
	runLoginLaunch(t, KindChrome, chrome, []string{"--headless=new", "--disable-gpu"}, false)
}

func TestEdgeLoginLaunchLeavesVisiblePage(t *testing.T) {
	if os.Getenv("TULAI_EDGE_E2E") != "1" {
		t.Skip("设置 TULAI_EDGE_E2E=1 才启动 Microsoft Edge")
	}
	edge, err := FindEdgePath()
	if err != nil {
		t.Fatal(err)
	}
	display := ensureXvfb(t)
	t.Logf("edge=%s display=%s", edge, display)
	runLoginLaunch(t, KindEdge, edge, nil, true)
}

func runLoginLaunch(t *testing.T, kind, bin string, extra []string, realURL bool) {
	t.Helper()
	ver := browserVersion(bin)
	t.Logf("browser %s version %s", kind, ver)
	evidence := os.Getenv("TULAI_EDGE_EVIDENCE")
	if evidence == "" {
		evidence = t.TempDir()
	}
	if err := os.MkdirAll(evidence, 0o755); err != nil {
		t.Fatal(err)
	}
	prefix := strings.ToLower(kind)
	var log bytes.Buffer
	fmt.Fprintf(&log, "browser=%s\nbinary=%s\nversion=%s\nflags_extra=%q\n", kind, bin, ver, extra)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><head><title>登录</title></head><body><h1>扫码登录</h1><p>%s</p></body></html>", r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	login := srv.URL + "/login"

	m := NewManager()
	m.extra = extra
	var stderr bytes.Buffer
	m.log = &stderr
	profile := t.TempDir()
	port := freePort(t)
	row := store.Browser{ID: 41, Name: "账号1", Platform: "豆包", Port: port, UserDataDir: profile}
	t.Cleanup(func() {
		m.Stop(row.ID)
		_ = exec.Command("pkill", "-f", profile).Run()
	})

	ws, err := m.Start(row, t.TempDir(), kind, login, false)
	if err != nil {
		fmt.Fprintf(&log, "first start error: %v\nstderr:\n%s\n", err, stderr.String())
		flushEvidence(t, evidence, prefix, &log, nil)
		if launchBlocked(stderr.String() + err.Error()) {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	if err := cdp.OpenURL(ws, login); err != nil {
		fmt.Fprintf(&log, "open error: %v\nstderr:\n%s\n", err, stderr.String())
		flushEvidence(t, evidence, prefix, &log, nil)
		t.Fatal(err)
	}
	visible := waitVisible(t, ws, login)
	pages := pageList(port)
	fmt.Fprintf(&log, "first visible=%s\npages=%s\ncmdline=%s\nstderr:\n%s\n", visible, pages, profileCmdline(profile), stderr.String())
	shot := screenshot(ws)
	if !strings.HasPrefix(visible, login) {
		flushEvidence(t, evidence, prefix, &log, shot)
		t.Fatalf("visible page = %q, want %s\npages=%s", visible, login, pages)
	}
	if strings.Contains(pages, "about:blank") {
		flushEvidence(t, evidence, prefix, &log, shot)
		t.Fatalf("about:blank still open: %s", pages)
	}
	assertNoAutomation(t, profile)

	pids := profilePIDs(profile)
	ws2, err := m.Start(row, t.TempDir(), kind, login, false)
	if err != nil {
		t.Fatalf("second start: %v", err)
	}
	if err := cdp.OpenURL(ws2, login); err != nil {
		t.Fatal(err)
	}
	visible2 := waitVisible(t, ws2, login)
	pages2 := pageList(port)
	pids2 := profilePIDs(profile)
	fmt.Fprintf(&log, "second visible=%s\npages=%s\npids=%v -> %v\n", visible2, pages2, pids, pids2)
	if !strings.HasPrefix(visible2, login) || strings.Contains(pages2, "about:blank") {
		flushEvidence(t, evidence, prefix, &log, screenshot(ws2))
		t.Fatalf("second launch visible=%q pages=%s", visible2, pages2)
	}
	if !sameIDs(pids, pids2) {
		flushEvidence(t, evidence, prefix, &log, shot)
		t.Fatalf("second launch changed processes %v -> %v", pids, pids2)
	}
	flushEvidence(t, evidence, prefix, &log, shot)

	if !realURL {
		return
	}
	tryRealURL(t, kind, extra, evidence, &log)
}

func tryRealURL(t *testing.T, kind string, extra []string, evidence string, log *bytes.Buffer) {
	t.Helper()
	const real = "https://www.doubao.com/chat/"
	m := NewManager()
	m.extra = extra
	var stderr bytes.Buffer
	m.log = &stderr
	profile := t.TempDir()
	port := freePort(t)
	row := store.Browser{ID: 42, Name: "账号2", Platform: "豆包", Port: port, UserDataDir: profile}
	t.Cleanup(func() {
		m.Stop(row.ID)
		_ = exec.Command("pkill", "-f", profile).Run()
	})
	ws, err := m.Start(row, t.TempDir(), kind, real, false)
	navErr := error(nil)
	if err == nil {
		navErr = cdp.OpenURL(ws, real)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	visible, visErr := "", error(nil)
	title, body := "", ""
	if ws != "" {
		deadline := time.Now().Add(8 * time.Second)
		for {
			visible, visErr = cdp.VisibleURL(ctx, ws)
			title, body, _ = cdp.PageSummary(ctx, ws)
			if visErr == nil && visible != "" && !strings.HasPrefix(visible, "about:") && (body != "" || !time.Now().Before(deadline)) {
				break
			}
			if !time.Now().Before(deadline) {
				break
			}
			time.Sleep(400 * time.Millisecond)
		}
	}
	fmt.Fprintf(log, "real url %s\nstart=%v\nnavigate=%v\nvisible=%q visErr=%v\ntitle=%q\nbody=%q\npages=%s\n",
		real, err, navErr, visible, visErr, title, strings.Join(strings.Fields(body), " "), pageList(port))
	shot := []byte(nil)
	if ws != "" {
		shot = screenshot(ws)
	}
	flushEvidence(t, evidence, strings.ToLower(kind)+"-doubao", log, shot)
	if visible == "about:blank" || strings.HasPrefix(visible, "about:blank") {
		t.Fatalf("real login url stayed on about:blank (start=%v navigate=%v)", err, navErr)
	}
	t.Logf("real url result visible=%q start=%v navigate=%v", visible, err, navErr)
}

func waitVisible(t *testing.T, ws, prefix string) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var last string
	var lastErr error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		last, lastErr = cdp.VisibleURL(ctx, ws)
		cancel()
		if lastErr == nil && strings.HasPrefix(last, prefix) {
			return last
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr != nil {
		t.Logf("visible url: %v", lastErr)
	}
	return last
}

func screenshot(ws string) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	png, err := cdp.CapturePNG(ctx, ws)
	if err != nil {
		return nil
	}
	return png
}

func pageList(port int) string {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return err.Error()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var pages []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if json.Unmarshal(body, &pages) != nil {
		return string(body)
	}
	var b strings.Builder
	for _, page := range pages {
		if page.Type == "page" {
			fmt.Fprintf(&b, "%s ", page.URL)
		}
	}
	return strings.TrimSpace(b.String())
}

func assertNoAutomation(t *testing.T, profile string) {
	t.Helper()
	raw := profileCmdline(profile)
	if strings.Contains(raw, "--enable-automation") {
		t.Fatalf("enable-automation in %s", raw)
	}
	for _, flag := range []string{"--no-first-run", "--no-default-browser-check", "--edge-skip-compat-layer-relaunch", "--user-data-dir=" + profile} {
		if !strings.Contains(raw, flag) {
			t.Fatalf("missing %s in %s", flag, raw)
		}
	}
}

func profileCmdline(profile string) string {
	var lines []string
	for _, pid := range profilePIDs(profile) {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		lines = append(lines, strings.ReplaceAll(string(raw), "\x00", " "))
	}
	return strings.Join(lines, "\n")
}

func profilePIDs(profile string) []int {
	out, err := exec.Command("ps", "-eo", "pid,args").Output()
	if err != nil {
		return nil
	}
	var ids []int
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, profile) || strings.Contains(line, "ps -eo") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		var pid int
		if _, err := fmt.Sscan(fields[0], &pid); err == nil && pid > 0 {
			ids = append(ids, pid)
		}
	}
	return ids
}

func sameIDs(a, b []int) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	seen := map[int]int{}
	for _, id := range a {
		seen[id]++
	}
	for _, id := range b {
		seen[id]--
		if seen[id] < 0 {
			return false
		}
	}
	return true
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func browserVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return strings.TrimSpace(err.Error() + " " + string(out))
	}
	return strings.TrimSpace(string(out))
}

func ensureXvfb(t *testing.T) string {
	t.Helper()
	if os.Getenv("DISPLAY") != "" {
		return os.Getenv("DISPLAY")
	}
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Fatal("需要 Xvfb 才能有头启动 Edge")
	}
	cmd := exec.Command("Xvfb", ":99", "-screen", "0", "1280x800x24", "-ac", "+extension", "GLX")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	_ = os.Setenv("DISPLAY", ":99")
	time.Sleep(300 * time.Millisecond)
	return ":99"
}

func flushEvidence(t *testing.T, dir, prefix string, log *bytes.Buffer, png []byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Log(err)
		return
	}
	path := filepath.Join(dir, prefix+"-result.txt")
	if err := os.WriteFile(path, log.Bytes(), 0o644); err != nil {
		t.Log(err)
	}
	if len(png) > 0 {
		if err := os.WriteFile(filepath.Join(dir, prefix+"-visible.png"), png, 0o644); err != nil {
			t.Log(err)
		}
	}
}
