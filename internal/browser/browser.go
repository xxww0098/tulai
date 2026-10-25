// Package browser 负责查找本机 Chrome 或 Edge、按 profile 启动独立调试实例并维护映射。
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

// FindChromePath 在常见安装位置查找 Chrome 可执行文件。
func FindChromePath() (string, error) {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			os.ExpandEnv("$HOME/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		}
	case "windows":
		candidates = []string{} // Windows 环境变量路径在下方展开处理
	default:
		candidates = []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"}
		if p, err := exec.LookPath(candidates[0]); err == nil {
			return p, nil
		}
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	// Windows 的环境变量展开手工做一遍（ExpandEnv 不识别 $env 形式）
	if runtime.GOOS == "windows" {
		for _, name := range []string{"PROGRAMFILES", "PROGRAMFILES(X86)", "LOCALAPPDATA"} {
			base := os.Getenv(name)
			if base == "" {
				continue
			}
			for _, sub := range []string{
				`Google\Chrome\Application\chrome.exe`,
				`Google\Chrome Beta\Application\chrome.exe`,
			} {
				p := base + `\` + sub
				if _, err := os.Stat(p); err == nil {
					return p, nil
				}
			}
		}
	}
	return "", ErrChromeNotFound
}

// FindEdgePath 在常见安装位置查找 Edge 可执行文件。
func FindEdgePath() (string, error) {
	home, _ := os.UserHomeDir()
	look, files := edgeLocations(runtime.GOOS, home, os.Getenv)
	return findOnDisk(look, files, ErrEdgeNotFound)
}

// ProfileDir 是实例的用户数据目录。Chrome 沿用 BrowserData/<名称>；Edge 使用 BrowserData/Edge/<名称>，避免两套登录态写进同一个 profile。
func ProfileDir(dataRoot, backend, name string) string {
	if backend == "Edge" {
		return filepath.Join(dataRoot, "Edge", name)
	}
	return filepath.Join(dataRoot, name)
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
	if backend == "Edge" {
		return "Edge"
	}
	return "Chrome"
}

// Manager 管理浏览器进程。
type Manager struct {
	procs map[int64]*os.Process
}

// NewManager 创建管理器。
func NewManager() *Manager { return &Manager{procs: map[int64]*os.Process{}} }

// Start 按 profile 启动 Chrome 或 Edge 调试实例并等待 CDP 端口就绪，返回浏览器 WS 地址。
// backend 只接受 Chrome 或 Edge，其它值按 Chrome 启动。
func (m *Manager) Start(b store.Browser, dataRoot, backend string, minimize bool) (string, error) {
	backend = pageBackend(backend)
	if p, ok := m.procs[b.ID]; ok {
		if ws, err := WaitWSURL(b.Port, 3*time.Second); err == nil {
			return ws, nil
		}
		p.Kill()
		delete(m.procs, b.ID)
	}
	bin, err := FindChromePath()
	if backend == "Edge" {
		bin, err = FindEdgePath()
	}
	if err != nil {
		return "", err
	}
	userData := b.UserDataDir
	if userData == "" {
		userData = ProfileDir(dataRoot, backend, b.Name)
	}
	if err := os.MkdirAll(userData, 0o755); err != nil {
		return "", err
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", b.Port),
		"--user-data-dir=" + userData,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate",
		"--remote-allow-origins=*",
		"about:blank",
	}
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动 %s 失败: %w", backend, err)
	}
	m.procs[b.ID] = cmd.Process
	logging.Log.Info("浏览器[%s] 已启动 (%s, 端口 %d)", b.Name, backend, b.Port)
	ws, err := WaitWSURL(b.Port, 20*time.Second)
	if err != nil {
		return "", err
	}
	return ws, nil
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
