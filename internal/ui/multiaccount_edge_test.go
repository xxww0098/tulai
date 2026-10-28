package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	extbundle "github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/heal"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

// TestEdgeMultiAccount 用真实的 Microsoft Edge 进程检查多账号。
// 登录态用本地夹具页模拟：这个环境没有豆包 / 千问账号，也到不了能扫码的网络。
func TestEdgeMultiAccount(t *testing.T) {
	if os.Getenv("TULAI_EDGE_E2E") != "1" {
		t.Skip("设置 TULAI_EDGE_E2E=1 才启动 Microsoft Edge")
	}
	edge, err := browser.FindEdgePath()
	if err != nil {
		t.Fatal(err)
	}
	display := startEdgeDisplay(t)
	t.Logf("REAL edge=%s display=%s", edge, display)

	dataRoot := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	st.Set("browser_backend", browser.KindEdge)
	st.SetPlatforms([]string{"豆包", "千问"})

	bm := browser.NewManager()
	extDir, err := extbundle.Install(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bm.SetExtensionDir(extDir)
	var pathMu sync.Mutex
	var loadPath []string
	bm.SetExtensionReady(func(wsURL, dir string) error {
		_, _, found, _ := cdp.ExtensionPairState(context.Background(), wsURL)
		pathMu.Lock()
		if found {
			loadPath = append(loadPath, "command-line")
		} else {
			loadPath = append(loadPath, "loadUnpacked")
		}
		pathMu.Unlock()
		return cdp.EnsureExtension(context.Background(), wsURL, dir)
	})
	eng := tasks.NewEngine(st, bm, nil, dataRoot)
	a := &App{
		store: st, manager: bm, engine: eng, dataRoot: dataRoot,
		browserSel: -1, srcSel: -1,
	}
	a.loadSettings()
	if a.browserBackend != browser.KindEdge || extbridgeWeak(a.extToken) {
		t.Fatalf("backend=%s token=%q", a.browserBackend, a.extToken)
	}
	a.wireHealing()
	if err := eng.ServeLocal(); err != nil {
		t.Fatal(err)
	}
	eng.EnsureExtensionBridge(nil)
	closed := false
	t.Cleanup(func() {
		eng.CloseSessions()
		bm.StopAll()
		_ = exec.Command("pkill", "-f", dataRoot).Run()
		if !closed {
			_ = st.Close()
		}
	})

	for _, row := range []struct{ plat, name string }{
		{"豆包", "账号1"},
		{"千问", "账号2"},
		{"豆包", "账号3"},
	} {
		a.newBrowserPlat = row.plat
		a.newBrowserName = row.name
		a.addBrowser()
	}
	locals := localAccounts(st)
	if len(locals) != 3 {
		t.Fatalf("accounts = %+v", locals)
	}
	dirs := map[int64]string{}
	for _, row := range locals {
		dir := browser.ProfileDir(dataRoot, browser.KindEdge, row.Name)
		if _, ok := dirs[row.ID]; ok {
			t.Fatal("duplicate id")
		}
		dirs[row.ID] = dir
		for id, other := range dirs {
			if id != row.ID && other == dir {
				t.Fatalf("profile collision %s", dir)
			}
		}
	}

	type started struct {
		row store.Browser
		ws  string
		err error
	}
	startedRows := make([]started, len(locals))
	var wg sync.WaitGroup
	for i, row := range locals {
		wg.Add(1)
		go func(i int, row store.Browser) {
			defer wg.Done()
			home := accountHomeURL(row)
			ws, err := bm.Start(row, dataRoot, browser.KindEdge, home, false)
			if err == nil {
				err = cdp.OpenURL(ws, home)
			}
			startedRows[i] = started{row: row, ws: ws, err: err}
		}(i, row)
	}
	wg.Wait()

	mains := map[int64]int{}
	for _, run := range startedRows {
		if run.err != nil {
			t.Fatalf("start %s: %v", run.row.Name, run.err)
		}
		dir := dirs[run.row.ID]
		main := edgeMainPID(dir)
		if main == 0 {
			t.Fatalf("%s has no browser process", run.row.Name)
		}
		for id, pid := range mains {
			if pid == main {
				t.Fatalf("%s handed off to account %d", run.row.Name, id)
			}
		}
		mains[run.row.ID] = main
		cmd := edgeCmdline(dir)
		home := accountHomeURL(run.row)
		if !strings.Contains(cmd, "--user-data-dir="+dir) || !strings.Contains(cmd, home) {
			t.Fatalf("%s cmdline missing profile or login url: %s", run.row.Name, cmd)
		}
		if _, err := os.Stat(filepath.Join(dir, "Local State")); err != nil {
			t.Fatalf("%s profile was not created: %v", run.row.Name, err)
		}
	}
	first := startedRows[0]
	again, err := bm.Start(first.row, dataRoot, browser.KindEdge, accountHomeURL(first.row), false)
	if err != nil || again == "" {
		t.Fatalf("reuse start: %v", err)
	}
	if edgeMainPID(dirs[first.row.ID]) != mains[first.row.ID] {
		t.Fatal("reusing a profile started a second browser")
	}
	for _, run := range startedRows {
		visible := waitEdgeHost(t, run.ws, run.row.Platform)
		list := edgePages(run.row.Port)
		t.Logf("REAL %s %s visible=%s", run.row.Name, run.row.Platform, visible)
		if !loginHostOK(run.row.Platform, visible) || strings.Contains(list, "about:blank") {
			title, body, _ := cdp.PageSummary(context.Background(), run.ws)
			t.Fatalf("%s visible=%q title=%q body=%q list=%s", run.row.Name, visible, title, body, list)
		}
	}

	ids := map[string]string{}
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) && len(ids) < len(locals) {
		for _, run := range startedRows {
			profileID, token, found, err := cdp.ExtensionPairState(context.Background(), run.ws)
			if err != nil || !found || profileID == "" {
				continue
			}
			if token != a.extToken {
				t.Fatalf("%s paired with token %q, desktop token is %q", run.row.Name, token, a.extToken)
			}
			if prev, ok := ids[profileID]; ok && prev != run.row.Name {
				t.Fatalf("profile id %s collided: %s and %s", profileID, prev, run.row.Name)
			}
			ids[profileID] = run.row.Name
		}
		if len(ids) < len(locals) {
			time.Sleep(300 * time.Millisecond)
		}
	}
	if len(ids) != len(locals) {
		t.Fatalf("paired profiles = %v", ids)
	}
	pathMu.Lock()
	gotPath := append([]string(nil), loadPath...)
	pathMu.Unlock()
	t.Logf("REAL extension load path per profile: %v", gotPath)
	if len(gotPath) != len(locals) {
		t.Fatalf("extension hook ran %d times", len(gotPath))
	}
	extRows := map[string]int{}
	for _, row := range st.Browsers() {
		if row.ExtAccount != "" {
			extRows[row.ExtAccount]++
		}
	}
	for id, name := range ids {
		if extRows[id] < 1 {
			t.Fatalf("%s profile %s did not register an extension account: %+v", name, id, st.Browsers())
		}
	}

	fix := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>in</title><textarea style="width:240px;height:80px"></textarea>`)
	}))
	t.Cleanup(fix.Close)
	t.Log("SIMULATED login state: local pages with a composer or a 登录 button, probed by the real login script")
	byName := map[string]started{}
	for _, run := range startedRows {
		byName[run.row.Name] = run
		if err := cdp.OpenURL(run.ws, fix.URL+"/in"); err != nil {
			t.Fatal(err)
		}
		closePagesExcept(run.row.Port, fix.URL)
	}
	for _, run := range startedRows {
		if state := waitProbe(t, run.row.Port, "in"); state != "in" {
			t.Fatalf("%s probe=%s", run.row.Name, state)
		}
	}

	var used []int64
	err = eng.RunObserved(context.Background(), tasks.Options{
		Platforms: []string{"豆包", "千问"}, Prompts: []string{"猫", "海"}, Concurrency: 3,
	}, func(tasks.Task) {}, func(tasks.Progress) {}, func(_ context.Context, task *tasks.Task) error {
		used = append(used, task.BrowserID)
		return nil
	})
	if err != nil {
		t.Fatalf("distribute: %v queue=%+v", err, eng.View().Snapshot())
	}
	doubao, qianwen := map[int64]bool{}, map[int64]bool{}
	for _, id := range used {
		switch id {
		case byName["账号1"].row.ID, byName["账号3"].row.ID:
			doubao[id] = true
		case byName["账号2"].row.ID:
			qianwen[id] = true
		default:
			t.Fatalf("job ran on unexpected account %d (%v)", id, used)
		}
	}
	if len(doubao) != 2 || !qianwen[byName["账号2"].row.ID] {
		t.Fatalf("jobs were not spread across the usable accounts: %v", used)
	}
	t.Logf("SIMULATED job executor, REAL account assignment: %v", used)

	loggedOut := byName["账号3"]
	for _, run := range startedRows {
		if edgeMainPID(dirs[run.row.ID]) == 0 {
			t.Fatalf("%s browser exited before the logged-out check", run.row.Name)
		}
	}
	if err := setLoginFixture(loggedOut.row.Port, `<button style="width:80px;height:32px">登录</button>`); err != nil {
		t.Fatal(err)
	}
	if state := waitProbe(t, loggedOut.row.Port, "out"); state != "out" {
		t.Fatalf("logged-out probe=%s", state)
	}
	time.Sleep(heal.LoginMinGap + time.Second)
	var second []int64
	err = eng.RunObserved(context.Background(), tasks.Options{
		Platforms: []string{"豆包"}, Prompts: []string{"甲", "乙"}, Concurrency: 2,
	}, func(tasks.Task) {}, func(tasks.Progress) {}, func(_ context.Context, task *tasks.Task) error {
		second = append(second, task.BrowserID)
		return nil
	})
	if err != nil {
		t.Fatalf("skip logged-out: %v queue=%+v", err, eng.View().Snapshot())
	}
	for _, id := range second {
		if id != byName["账号1"].row.ID {
			t.Fatalf("logged-out account was still used: %v", second)
		}
	}
	health := browserByName(t, st, "账号3")
	if health.Usable(time.Now()) || !strings.Contains(health.LastError, "登录") || health.IsLoggedIn {
		t.Fatalf("logged-out account was not marked: %+v", health)
	}
	text, id, ok := eng.Attention()
	if !ok || id != loggedOut.row.ID || !strings.Contains(text, "账号3") {
		t.Fatalf("attention=%q id=%d ok=%v", text, id, ok)
	}
	visible := waitEdgeHost(t, loggedOut.ws, "豆包")
	if !loginHostOK("豆包", visible) {
		t.Fatalf("re-login focus landed on %s", visible)
	}

	a.prompts = "一只猫"
	a.tab = pageHome
	a.platformAuto = true
	tt := ui.NewTester(a.view, 1280, 900)
	assertVisible(t, tt, "去处理", 1280, 900)
	if err := tt.Click("去处理"); err != nil {
		t.Fatal(err)
	}
	if a.tab != pageBrowser {
		t.Fatalf("去处理 tab=%d", a.tab)
	}
	selected, ok := a.selectedBrowser()
	if !ok || selected.ID != loggedOut.row.ID {
		t.Fatalf("去处理 selected %+v", selected)
	}
	visible = waitEdgeHost(t, loggedOut.ws, "豆包")
	if !loginHostOK("豆包", visible) {
		t.Fatalf("去处理 left the window on %s", visible)
	}
	t.Logf("REAL re-login window: %s", visible)

	for _, row := range localAccounts(st) {
		st.UpdateBrowser(row.ID, false, false)
		st.UpdateBrowserHealth(row.ID, store.HealthExhausted, 0, "未登录")
	}
	a.tab = pageHome
	tt = ui.NewTester(a.view, 1280, 900)
	scrollIntoView(t, tt, "还没有可用账号。先去设置登录，再开始出图。", 1280, 900)
	t.Log("SIMULATED remaining accounts as logged-out in the store, the same mark the probe writes")

	keepID := ids
	dropped := byName["账号2"]
	a.refreshBrowsers()
	for i, row := range a.browserRows {
		if row.ID == dropped.row.ID {
			a.browserSel = i
		}
	}
	a.deleteBrowser()
	dropDir := dirs[dropped.row.ID]
	if _, err := os.Stat(dropDir); !os.IsNotExist(err) {
		entries, _ := os.ReadDir(filepath.Join(dataRoot, "Edge"))
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("removed profile still exists: %v dir=%s left=%v pid=%d rows=%+v", err, dropDir, names, edgeMainPID(dropDir), st.Browsers())
	}
	for _, row := range st.Browsers() {
		if row.ID == dropped.row.ID || row.ExtAccount == profileIDOf(keepID, dropped.row.Name) {
			t.Fatalf("deleted account or its pairing is still stored: %+v", row)
		}
	}
	for _, name := range []string{"账号1", "账号3"} {
		if _, err := os.Stat(dirs[byName[name].row.ID]); err != nil {
			t.Fatalf("%s profile was removed: %v", name, err)
		}
	}

	before := map[string]store.Browser{}
	for _, row := range localAccounts(st) {
		before[row.Name] = row
	}
	eng.CloseSessions()
	bm.StopAll()
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	closed = true
	restored, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { restored.Close() })
	got := map[string]store.Browser{}
	for _, row := range localAccounts(restored) {
		got[row.Name] = row
	}
	if _, ok := got["账号2"]; ok || got["账号1"].ID != before["账号1"].ID || got["账号3"].ID != before["账号3"].ID {
		t.Fatalf("restart restore = %+v want %+v", got, before)
	}
	if got["账号1"].Port != before["账号1"].Port || got["账号1"].Platform != "豆包" || got["账号3"].Platform != "豆包" {
		t.Fatalf("restored rows = %+v", got)
	}
	home := accountHomeURL(got["账号1"])
	ws, err := bm.Start(got["账号1"], dataRoot, browser.KindEdge, home, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := cdp.OpenURL(ws, home); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(edgeCmdline(dirs[got["账号1"].ID]), "--user-data-dir="+dirs[got["账号1"].ID]) {
		t.Fatal("restarted account used a different profile")
	}
	if !loginHostOK("豆包", waitEdgeHost(t, ws, "豆包")) {
		t.Fatal("restarted account did not open the login url")
	}
}

func extbridgeWeak(token string) bool {
	token = strings.TrimSpace(token)
	return token == "" || token == "dev-token"
}

func localAccounts(st *store.Store) []store.Browser {
	var out []store.Browser
	for _, row := range st.Browsers() {
		if row.ExtAccount == "" {
			out = append(out, row)
		}
	}
	return out
}

func browserByName(t *testing.T, st *store.Store, name string) store.Browser {
	t.Helper()
	for _, row := range st.Browsers() {
		if row.Name == name && row.ExtAccount == "" {
			return row
		}
	}
	t.Fatalf("missing %s", name)
	return store.Browser{}
}

func profileIDOf(ids map[string]string, name string) string {
	for id, got := range ids {
		if got == name {
			return id
		}
	}
	return ""
}

func setLoginFixture(port int, html string) error {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var pages []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return err
	}
	id := ""
	for _, page := range pages {
		if page.Type == "page" && page.ID != "" {
			id = page.ID
			break
		}
	}
	if id == "" {
		return fmt.Errorf("port %d has no page", port)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := cdp.DialSlider(ctx, fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	defer conn.Close()
	expr := fmt.Sprintf(`document.documentElement.innerHTML = %q`, html)
	return conn.EvalJSON(ctx, id, expr, nil)
}

func closePagesExcept(port int, keep string) {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var pages []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if json.NewDecoder(resp.Body).Decode(&pages) != nil {
		return
	}
	for _, page := range pages {
		if page.Type != "page" || page.ID == "" || strings.HasPrefix(page.URL, keep) {
			continue
		}
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/close/%s", port, page.ID))
		if err == nil {
			resp.Body.Close()
		}
	}
}

func waitProbe(t *testing.T, port int, want string) string {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	var state string
	var err error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		state, err = cdp.ProbeLogin(ctx, fmt.Sprintf("http://127.0.0.1:%d", port))
		cancel()
		if err == nil && state == want {
			return state
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		return err.Error()
	}
	return state
}

func waitEdgeHost(t *testing.T, ws, platform string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		last, _ = cdp.VisibleURL(ctx, ws)
		cancel()
		if loginHostOK(platform, last) {
			return last
		}
		time.Sleep(400 * time.Millisecond)
	}
	return last
}

func loginHostOK(platform, raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	switch platform {
	case "豆包":
		return host == "doubao.com" || strings.HasSuffix(host, ".doubao.com")
	case "千问":
		return host == "tongyi.com" || strings.HasSuffix(host, ".tongyi.com") ||
			host == "qianwen.com" || strings.HasSuffix(host, ".qianwen.com") ||
			host == "qwen.ai" || strings.HasSuffix(host, ".qwen.ai")
	default:
		return false
	}
}

func edgePages(port int) string {
	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", port))
	if err != nil {
		return err.Error()
	}
	defer resp.Body.Close()
	body := make([]byte, 0, 4096)
	buf := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buf)
		body = append(body, buf[:n]...)
		if err != nil {
			break
		}
	}
	return string(body)
}

func edgeCmdline(profile string) string {
	var lines []string
	for pid := range edgePIDs(profile) {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		lines = append(lines, strings.ReplaceAll(string(raw), "\x00", " "))
	}
	return strings.Join(lines, "\n")
}

func edgePIDs(profile string) map[int]bool {
	out, err := exec.Command("ps", "-eo", "pid,args").Output()
	if err != nil {
		return nil
	}
	ids := map[int]bool{}
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
			ids[pid] = true
		}
	}
	return ids
}

func edgeMainPID(profile string) int {
	for pid := range edgePIDs(profile) {
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}
		cmd := strings.ReplaceAll(string(raw), "\x00", " ")
		if strings.Contains(cmd, "--type=") || !strings.Contains(cmd, "--remote-debugging-port=") {
			continue
		}
		return pid
	}
	return 0
}

func startEdgeDisplay(t *testing.T) string {
	t.Helper()
	if display := os.Getenv("DISPLAY"); edgeDisplayAlive(display) {
		return display
	}
	if _, err := exec.LookPath("Xvfb"); err != nil {
		t.Fatal(err)
	}
	for n := 99; n < 110; n++ {
		display := fmt.Sprintf(":%d", n)
		if edgeDisplayAlive(display) {
			_ = os.Setenv("DISPLAY", display)
			return display
		}
		cmd := exec.Command("Xvfb", display, "-screen", "0", "1280x800x24", "-ac", "+extension", "GLX")
		if err := cmd.Start(); err != nil {
			continue
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) && !edgeDisplayAlive(display) {
			time.Sleep(50 * time.Millisecond)
		}
		if !edgeDisplayAlive(display) {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			continue
		}
		t.Cleanup(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
		_ = os.Setenv("DISPLAY", display)
		return display
	}
	t.Fatal("Xvfb 没有起来")
	return ""
}

func edgeDisplayAlive(display string) bool {
	display = strings.TrimSpace(display)
	if i := strings.LastIndex(display, ":"); i >= 0 {
		display = display[i+1:]
	}
	if i := strings.IndexByte(display, '.'); i >= 0 {
		display = display[:i]
	}
	if display == "" {
		return false
	}
	for _, r := range display {
		if r < '0' || r > '9' {
			return false
		}
	}
	conn, err := net.DialTimeout("unix", "/tmp/.X11-unix/X"+display, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
