// Package browser 负责查找本机 Chrome、Edge 或 Chromium，必要时使用已下载的
// Chrome for Testing，并按 profile 启动独立调试实例。
package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"xbs/internal/logging"
	"xbs/internal/store"
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

// Manager 管理浏览器进程。
type Manager struct {
	procs map[int64]*os.Process
}

// NewManager 创建管理器。
func NewManager() *Manager { return &Manager{procs: map[int64]*os.Process{}} }

// Start 按 profile 启动调试实例并等待 CDP 端口就绪，返回浏览器 WS 地址。
// backend 接受 Chrome、Edge、Builtin。选中的浏览器不在时按系统顺序回落，仍使用独立的 --user-data-dir。
func (m *Manager) Start(b store.Browser, dataRoot, backend string, minimize bool) (string, error) {
	requested := CanonicalBackend(backend)
	if p, ok := m.procs[b.ID]; ok {
		if ws, err := WaitWSURL(b.Port, 3*time.Second); err == nil {
			return ws, nil
		}
		p.Kill()
		delete(m.procs, b.ID)
	}
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
	cmd := exec.Command(rt.Path, debugArgs(b.Port, userData)...)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动%s失败: %w", ChoiceLabel(rt.Kind), err)
	}
	m.procs[b.ID] = cmd.Process
	logging.Log.Info("浏览器[%s] 已启动（%s，端口 %d）", b.Name, ChoiceLabel(rt.Kind), b.Port)
	ws, err := WaitWSURL(b.Port, 20*time.Second)
	if err != nil {
		return "", err
	}
	return ws, nil
}

// debugArgs 是现有调试参数，另加 --disable-infobars。不带 --enable-automation，避免 navigator.webdriver 被打开。
func debugArgs(port int, userData string) []string {
	return []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--user-data-dir=" + userData,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate",
		"--disable-infobars",
		"--remote-allow-origins=*",
		"about:blank",
	}
}

// Stop 停止指定浏览器进程。
func (m *Manager) Stop(id int64) {
	if p, ok := m.procs[id]; ok {
		p.Kill()
		delete(m.procs, id)
	}
}

// StopAll 停止全部浏览器。
func (m *Manager) StopAll() {
	for id, p := range m.procs {
		p.Kill()
		delete(m.procs, id)
	}
}

// IsRunning 判断浏览器进程是否仍在运行。
func (m *Manager) IsRunning(id int64) bool {
	_, ok := m.procs[id]
	return ok
}

// WaitWSURL 轮询 /json/version 直到 CDP 就绪，返回 browser 级 WebSocket 地址。
func WaitWSURL(port int, timeout time.Duration) (string, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	client := &http.Client{Timeout: 2 * time.Second}
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
			lastErr = err
		} else {
			lastErr = err
		}
		time.Sleep(300 * time.Millisecond)
	}
	return "", fmt.Errorf("CDP 端口 %d 未就绪: %v", port, lastErr)
}
