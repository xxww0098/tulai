// Package browser 负责查找本机 Chrome、按 profile 启动独立调试实例并维护映射。
package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"xbs/internal/logging"
	"xbs/internal/store"
)

// ErrChromeNotFound 本机找不到 Chrome。
var ErrChromeNotFound = errors.New("未找到 Google Chrome，请先安装")

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

// Manager 管理浏览器进程。
type Manager struct {
	procs map[int64]*os.Process
}

// NewManager 创建管理器。
func NewManager() *Manager { return &Manager{procs: map[int64]*os.Process{}} }

// Start 按 profile 启动 Chrome 调试实例并等待 CDP 端口就绪，返回浏览器 WS 地址。
func (m *Manager) Start(b store.Browser, dataRoot string, minimize bool) (string, error) {
	if p, ok := m.procs[b.ID]; ok {
		if ws, err := WaitWSURL(b.Port, 3*time.Second); err == nil {
			return ws, nil
		}
		p.Kill()
		delete(m.procs, b.ID)
	}
	chrome, err := FindChromePath()
	if err != nil {
		return "", err
	}
	userData := b.UserDataDir
	if userData == "" {
		userData = dataRoot + string(os.PathSeparator) + b.Name
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
	cmd := exec.Command(chrome, args...)
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("启动 Chrome 失败: %w", err)
	}
	m.procs[b.ID] = cmd.Process
	logging.Log.Info("浏览器[%s] 已启动 (端口 %d)", b.Name, b.Port)
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
