// Package browser 负责查找本机 Chrome、Edge 或 Chromium，必要时使用已下载的
// Chrome for Testing，并按 profile 启动独立调试实例。
package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

// ErrChromeNotFound 本机找不到 Chrome。
var ErrChromeNotFound = errors.New("未找到 Google Chrome，请先安装")

// ErrEdgeNotFound 本机找不到 Edge。
var ErrEdgeNotFound = errors.New("未找到 Microsoft Edge，请先安装")

// FindChromePath 在常见安装位置查找 Chrome，找不到再找 Chromium。
func FindChromePath() (string, error) {
	loc := hostLookup(runtime.GOOS, runtime.GOARCH)
	if path, err := loc.find(chromeLocations(loc.goos, loc.home, loc.env)); err == nil {
		return path, nil
	}
	if path, err := loc.find(chromiumLocations(loc.goos, loc.home, loc.env)); err == nil {
		return path, nil
	}
	return "", ErrChromeNotFound
}

// FindEdgePath 在常见安装位置查找 Edge 可执行文件。
func FindEdgePath() (string, error) {
	home, _ := os.UserHomeDir()
	look, files := edgeLocations(runtime.GOOS, home, os.Getenv)
	return findOnDisk(look, files, ErrEdgeNotFound)
}

// ProfileDir 是实例的用户数据目录。Chrome 与 Chromium 沿用 BrowserData/<名称>；
// Edge 使用 BrowserData/Edge/<名称>；内置浏览器使用 BrowserData/Builtin/<名称>。
func ProfileDir(dataRoot, backend, name string) string {
	switch pageBackend(backend) {
	case KindEdge:
		return filepath.Join(dataRoot, "Edge", name)
	case KindBuiltin:
		return filepath.Join(dataRoot, "Builtin", name)
	default:
		return filepath.Join(dataRoot, name)
	}
}

func edgeLocations(goos, home string, env func(string) string) (look, files []string) {
	switch goos {
	case "darwin":
		files = []string{"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"}
		if home != "" {
			files = append(files, osJoin(goos, home, "Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"))
		}
	case "windows":
		for _, key := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
			base := env(key)
			if base == "" {
				continue
			}
			files = append(files, osJoin(goos, base, `Microsoft\Edge\Application\msedge.exe`))
		}
	default:
		look = []string{"microsoft-edge", "microsoft-edge-stable", "microsoft-edge-beta", "microsoft-edge-dev"}
		files = []string{
			"/usr/bin/microsoft-edge",
			"/usr/bin/microsoft-edge-stable",
			"/opt/microsoft/msedge/msedge",
			"/opt/microsoft/msedge/microsoft-edge",
		}
	}
	return look, files
}

func findOnDisk(look, files []string, notFound error) (string, error) {
	for _, name := range look {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	for _, p := range files {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", notFound
}

func osJoin(goos string, elem ...string) string {
	sep := "/"
	if goos == "windows" {
		sep = `\`
	}
	return strings.Join(elem, sep)
}

func pageBackend(backend string) string {
	return CanonicalBackend(backend)
}

// ErrDebugPort 表示浏览器进程已经拉起，命令行上的登录页还在，但调试端口没连上。
// 调用方应留着窗口让用户在网页里登录，不要当成「没打开浏览器」。
var ErrDebugPort = errors.New("调试端口未就绪")

type runningProc struct {
	proc *os.Process
	done chan struct{}
}

func (r *runningProc) alive() bool {
	if r == nil || r.done == nil {
		return false
	}
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

// Manager 管理浏览器进程。
type Manager struct {
	mu    sync.Mutex
	procs map[int64]*runningProc
	// extra 插在启动地址前面。正式路径留空。测试可加无头参数。
	extra []string
	// extDir 是页面桥扩展目录。非空时启动参数带上 --load-extension。
	extDir string
	// onExt 在调试端口就绪后补装扩展。品牌版浏览器会忽略 --load-extension。
	onExt func(wsURL, dir string) error
	// focusApp 在出图浏览器冷启动之后把 tulai 自己的窗口重新放到前面。
	// 登录启动不调用。界面用 MyGo 的 Window.Focus 接上。
	focusApp func()
	// log 非空时收浏览器的标准错误，便于对照首次运行页。
	log io.Writer
}

// NewManager 创建管理器。
func NewManager() *Manager { return &Manager{procs: map[int64]*runningProc{}} }

// Start 按 profile 启动调试实例并等待 CDP 端口就绪，返回浏览器 WS 地址。
// startURL 写在命令行最后，窗口先打开这一页；调试连不上时进程照样留着。
// 同一 profile 的调试端口已经在听时，不再起第二个进程（Edge/Chrome 会把新进程交回旧实例，并再开一个窗口）。
// backend 接受 Chrome、Edge、Builtin。选中的浏览器不在时按系统顺序回落，仍使用独立的 --user-data-dir。
// 每次启动都重新 Resolve，不沿用进程刚起来时看到的安装路径。
// background 为真是出图：窗口留在屏幕外，启动后把 tulai 拉回前面。登录传 false，窗口可以到前面。
func (m *Manager) Start(b store.Browser, dataRoot, backend, startURL string, background bool) (string, error) {
	if strings.TrimSpace(startURL) == "" {
		startURL = "about:blank"
	}
	if ws, err := tryWS(b.Port, 300*time.Millisecond); err == nil {
		logging.Log.Info("浏览器[%s] 已在运行，使用现有窗口（端口 %d）", b.Name, b.Port)
		return ws, nil
	}
	m.mu.Lock()
	running := m.procs[b.ID]
	m.mu.Unlock()
	if running.alive() {
		ws, err := WaitWSURL(b.Port, 15*time.Second)
		if err != nil {
			noteDebugPort(b.Name, startURL)
			return "", fmt.Errorf("%w：%v", ErrDebugPort, err)
		}
		return ws, nil
	}
	requested := CanonicalBackend(backend)
	rt, err := Resolve(runtime.GOOS, requested, ConfigDir())
	if err != nil {
		return "", err
	}
	if pageBackend(rt.Kind) != requested {
		logging.Log.Info("没有可用的%s，改用%s", ChoiceLabel(requested), ChoiceLabel(rt.Kind))
	}
	userData := b.UserDataDir
	if userData == "" {
		userData = ProfileDir(dataRoot, rt.Kind, b.Name)
	}
	if err := os.MkdirAll(userData, 0o755); err != nil {
		return "", err
	}
	markFirstRunDone(userData)
	m.mu.Lock()
	extra := append([]string(nil), m.extra...)
	extDir := m.extDir
	stderr := m.log
	m.mu.Unlock()
	args := launchArgs(b.Port, userData, startURL, extDir, CurrentProxyServer(), extra)
	cmd, quietOpen := launchCmd(runtime.GOOS, rt.Path, args, background)
	cmd.Stdout, cmd.Stderr = nil, stderr
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动%s失败: %w", ChoiceLabel(rt.Kind), err)
	}
	done := make(chan struct{})
	proc := cmd.Process
	if quietOpen {
		adopted, adoptErr := adoptOpenedBrowser(cmd, userData)
		if adoptErr != nil {
			return "", fmt.Errorf("启动%s失败: %w", ChoiceLabel(rt.Kind), adoptErr)
		}
		proc = adopted
		go watchOpened(proc.Pid, done)
	} else {
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
	}
	m.mu.Lock()
	if old := m.procs[b.ID]; old.alive() {
		old.proc.Kill()
	}
	m.procs[b.ID] = &runningProc{proc: proc, done: done}
	m.mu.Unlock()
	if quietOpen {
		logging.Log.Info("浏览器[%s] 已在后台启动（%s，端口 %d）", b.Name, ChoiceLabel(rt.Kind), b.Port)
	} else {
		logging.Log.Info("浏览器[%s] 已启动（%s，端口 %d）", b.Name, ChoiceLabel(rt.Kind), b.Port)
	}
	if background {
		// 窗口可能比调试端口更早出现。先把 tulai 拉回来，端口就绪后再补一次。
		m.refocusApp()
	}
	ws, err := WaitWSURL(b.Port, 20*time.Second)
	if background {
		m.KeepAppInFront()
	}
	if err != nil {
		noteDebugPort(b.Name, startURL)
		return "", fmt.Errorf("%w：%v", ErrDebugPort, err)
	}
	m.mu.Lock()
	onExt := m.onExt
	m.mu.Unlock()
	if extDir != "" && onExt != nil {
		if extErr := onExt(ws, extDir); extErr != nil {
			logging.Log.Warning("浏览器[%s] 页面桥扩展没有自动装上：%v", b.Name, extErr)
		}
	}
	return ws, nil
}

// SetExtensionDir 让之后启动的 profile 自动带上页面桥扩展。
func (m *Manager) SetExtensionDir(dir string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.extDir = dir
	m.mu.Unlock()
}

// SetAppFocus 在出图浏览器冷启动之后调用，把 tulai 自己的窗口重新放到前面。
// 登录启动不会调用它。fn 应走 MyGo 的 Window.Focus。
func (m *Manager) SetAppFocus(fn func()) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.focusApp = fn
	m.mu.Unlock()
}

// SetExtensionReady 在调试端口连上后调用。用来在 --load-extension 被忽略时补装扩展。
func (m *Manager) SetExtensionReady(fn func(wsURL, dir string) error) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.onExt = fn
	m.mu.Unlock()
}

func noteDebugPort(name, startURL string) {
	if strings.TrimSpace(startURL) == "" || strings.HasPrefix(strings.TrimSpace(startURL), "about:blank") {
		logging.Log.Warning("浏览器[%s] 已启动，但调试端口还没连上", name)
		return
	}
	logging.Log.Warning("浏览器[%s] 已打开登录页，但调试端口还没连上", name)
}

// launchArgs 是调试参数。startURL 必须放在最后，作为窗口的起始页。
// 不带 --enable-automation，避免 navigator.webdriver 被打开。
// --edge-skip-compat-layer-relaunch 让 Edge 不要在兼容层里再启动一次并把地址丢掉。
// proxy 非空时加上 --proxy-server。Chrome 在 Linux 上不会自己读 HTTPS_PROXY。
func launchArgs(port int, userData, startURL, extDir, proxy string, extra []string) []string {
	if strings.TrimSpace(startURL) == "" {
		startURL = "about:blank"
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir=" + userData,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-features=Translate,msImplicitSignin,msEdgeFirstRunExperience,EdgeSignInPromo",
		"--disable-infobars",
		"--hide-crash-restore-bubble",
		"--disable-session-crashed-bubble",
		"--no-service-autorun",
		"--edge-skip-compat-layer-relaunch",
		"--remote-allow-origins=*",
		// 窗口留在屏幕内。负坐标会被页面读成 screenX，风控会当成自动化。
		// 出图冷启动后再把 tulai 自己的窗口拉回前面，不靠把浏览器扔到屏幕外。
		"--window-position=80,48",
		"--window-size=1280,900",
	}
	if dir := strings.TrimSpace(extDir); dir != "" {
		// 品牌版 Chrome 137+ 和当前 Edge 会忽略 --load-extension。
		// 调试端口连上后会再试 Extensions.loadUnpacked，那条需要这个开关。
		args = append(args,
			"--load-extension="+dir,
			"--enable-unsafe-extension-debugging",
		)
	}
	if p := strings.TrimSpace(proxy); p != "" {
		args = append(args, "--proxy-server="+p)
	}
	args = append(args, extra...)
	args = append(args, startURL)
	return args
}

// debugArgs 给仍直接拼命令行的测试用。
func debugArgs(port int, userData, startURL string) []string {
	return launchArgs(port, userData, startURL, "", "", nil)
}

// RemoveProfile 删掉这个账号的用户数据目录。
// 只删除 dataRoot 下面的目录。扩展账号没有自己的目录时什么都不做。
// 浏览器退出时还会往目录里写文件，所以先停掉残留进程，再多删几次。
func RemoveProfile(dataRoot, backend string, b store.Browser) error {
	dir := accountProfilePath(dataRoot, backend, b)
	root := filepath.Clean(strings.TrimSpace(dataRoot))
	if dir == "" || root == "" || root == "." {
		return nil
	}
	clean := filepath.Clean(dir)
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
		return nil
	}
	releaseProfile(clean)
	var last error
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(clean); os.IsNotExist(err) {
			return nil
		}
		last = os.RemoveAll(clean)
		if _, err := os.Stat(clean); os.IsNotExist(err) {
			return nil
		}
		releaseProfile(clean)
		time.Sleep(150 * time.Millisecond)
	}
	if last == nil {
		return fmt.Errorf("配置目录还在")
	}
	return last
}

func accountProfilePath(dataRoot, backend string, b store.Browser) string {
	if dir := strings.TrimSpace(b.UserDataDir); dir != "" {
		return dir
	}
	if strings.TrimSpace(b.ExtAccount) != "" || strings.TrimSpace(b.Name) == "" {
		return ""
	}
	return ProfileDir(dataRoot, backend, b.Name)
}

// markFirstRunDone 写下 Chromium/Edge 的首次运行哨兵。
// --no-first-run 挡不住 Edge 的欢迎页和同步提示时，这个空文件能让它跳过。
func markFirstRunDone(userData string) {
	path := filepath.Join(userData, "First Run")
	if _, err := os.Stat(path); err == nil {
		return
	}
	_ = os.WriteFile(path, nil, 0o644)
}

// OpenExtensionsPage 用日常浏览器打开扩展管理页，不带 tulai 的独立 profile。
func OpenExtensionsPage(kind string) error {
	var bin, page string
	var err error
	if CanonicalBackend(kind) == KindEdge {
		bin, err = FindEdgePath()
		page = "edge://extensions"
	} else {
		bin, err = FindChromePath()
		page = "chrome://extensions"
	}
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, page)
	return cmd.Start()
}

// OpenFolder 在系统文件管理器里打开扩展目录。
func OpenFolder(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("没有扩展目录")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// Stop 停止指定浏览器进程，并等到它退出。配置目录要等进程结束后再删。
func (m *Manager) Stop(id int64) {
	m.mu.Lock()
	r := m.procs[id]
	delete(m.procs, id)
	m.mu.Unlock()
	waitProc(r)
}

// StopAll 停止全部浏览器。
func (m *Manager) StopAll() {
	m.mu.Lock()
	all := m.procs
	m.procs = map[int64]*runningProc{}
	m.mu.Unlock()
	for _, r := range all {
		waitProc(r)
	}
}

func waitProc(r *runningProc) {
	if r == nil || r.proc == nil {
		return
	}
	_ = r.proc.Kill()
	if r.done == nil {
		return
	}
	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
	}
}

// IsRunning 判断我们拉起的浏览器进程是否还在。
func (m *Manager) IsRunning(id int64) bool {
	m.mu.Lock()
	r := m.procs[id]
	m.mu.Unlock()
	return r.alive()
}

// pid 返回已跟踪进程的 pid。测试用来确认第二次启动没有换进程。
func (m *Manager) pid(id int64) int {
	m.mu.Lock()
	r := m.procs[id]
	m.mu.Unlock()
	if r == nil || r.proc == nil || !r.alive() {
		return 0
	}
	return r.proc.Pid
}

// WaitWSURL 轮询 /json/version 直到 CDP 就绪，返回 browser 级 WebSocket 地址。
func WaitWSURL(port int, timeout time.Duration) (string, error) {
	if port <= 0 {
		return "", fmt.Errorf("CDP 端口 %d 未就绪: 没有端口", port)
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	perRequest := 500 * time.Millisecond
	if timeout < perRequest {
		perRequest = timeout
	}
	client := &http.Client{Timeout: perRequest}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			var v struct {
				WS string `json:"webSocketDebuggerUrl"`
			}
			err = json.NewDecoder(resp.Body).Decode(&v)
			resp.Body.Close()
			if err == nil && v.WS != "" {
				return v.WS, nil
			}
			if err != nil {
				lastErr = err
			}
		} else {
			lastErr = err
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			break
		}
		pause := 200 * time.Millisecond
		if remain < pause {
			pause = remain
		}
		time.Sleep(pause)
	}
	return "", fmt.Errorf("CDP 端口 %d 未就绪: %v", port, lastErr)
}

// tryWS 只探一次。端口没开时马上返回，避免第二次启动前白等。
func tryWS(port int, timeout time.Duration) (string, error) {
	if port <= 0 {
		return "", errors.New("没有调试端口")
	}
	return WaitWSURL(port, timeout)
}
