// Package browser manages isolated Camoufox accounts.
package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

var ErrDebugPort = errors.New("Camoufox 账号窗口尚未就绪")

// ProfileDir keeps Firefox data separate from all former Chromium profiles.
func ProfileDir(dataRoot, backend, name string) string {
	name = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		name = "account"
	}
	return filepath.Join(dataRoot, KindCamoufox, name)
}

type Manager struct {
	lifecycle sync.RWMutex
	mu        sync.Mutex
	starts    sync.Map
	procs     map[int64]*camoufox.Process
	quiet     map[int64]bool
	addons    map[int64]string
	focusApp  func()
	configDir string
	headless  bool
}

func NewManager() *Manager { return &Manager{procs: map[int64]*camoufox.Process{}} }

func (m *Manager) Start(b store.Browser, dataRoot, backend, startURL string, background bool) (string, error) {
	return m.start(b, dataRoot, startURL, background, false, "")
}

// StartTask uses a headless process even if the account was left open for login.
func (m *Manager) StartTask(b store.Browser, dataRoot, backend, startURL string) (string, error) {
	return m.start(b, dataRoot, startURL, true, true, "")
}

func (m *Manager) StartTaskWithAddon(b store.Browser, dataRoot, startURL, addon string) (string, error) {
	return m.start(b, dataRoot, startURL, true, true, addon)
}

func (m *Manager) start(b store.Browser, dataRoot, startURL string, background, task bool, addon string) (string, error) {
	m.lifecycle.RLock()
	defer m.lifecycle.RUnlock()
	lock, _ := m.starts.LoadOrStore(b.ID, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	m.mu.Lock()
	running := m.procs[b.ID]
	quiet := m.quiet[b.ID]
	loadedAddon := m.addons[b.ID]
	m.mu.Unlock()
	if running.Alive() && (!task || quiet && loadedAddon == addon) {
		return camoufox.Endpoint(b.Port), nil
	}
	dir := accountProfilePath(dataRoot, KindCamoufox, b)
	if dir == "" {
		return "", errors.New("缺少账号资料目录")
	}
	configDir := m.configDir
	if configDir == "" {
		configDir = ConfigDir()
	}
	proxy := CurrentProxyServer()
	if b.Proxy != "" {
		var ok bool
		proxy, ok = NormalizeProxy(b.Proxy)
		if !ok {
			return "", errors.New("账号代理地址无效")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if running.Alive() {
		running.Close()
		m.mu.Lock()
		delete(m.procs, b.ID)
		delete(m.quiet, b.ID)
		delete(m.addons, b.ID)
		m.mu.Unlock()
	}
	headless := background || m.headless
	var addons []string
	if addon != "" {
		addons = []string{addon}
	}
	proc, err := camoufox.Start(ctx, configDir, camoufox.Options{Profile: dir, Proxy: proxy, Headless: headless, Addons: addons}, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(startURL) != "" && startURL != "about:blank" {
		if err := proc.Call(ctx, proc.Page, "Page.navigate", map[string]any{"url": startURL}, nil); err != nil {
			proc.Close()
			return "", fmt.Errorf("打开账号页面: %w", err)
		}
	}
	m.mu.Lock()
	m.procs[b.ID] = proc
	if m.quiet == nil {
		m.quiet = map[int64]bool{}
	}
	m.quiet[b.ID] = headless
	if m.addons == nil {
		m.addons = map[int64]string{}
	}
	m.addons[b.ID] = addon
	m.mu.Unlock()
	camoufox.Register(b.Port, proc)
	go func() { <-proc.Context().Done(); camoufox.Unregister(b.Port, proc) }()
	logging.Log.Info("账号[%s] 已使用 Camoufox 打开", b.Name)
	return camoufox.Endpoint(b.Port), nil
}

func (m *Manager) SetAppFocus(fn func()) { m.mu.Lock(); m.focusApp = fn; m.mu.Unlock() }

func (m *Manager) Stop(id int64) {
	m.stop(id, false)
}

// StopLogin leaves an account alone if a task has already switched it to the background.
func (m *Manager) StopLogin(id int64) bool {
	return m.stop(id, true)
}

func (m *Manager) stop(id int64, visibleOnly bool) bool {
	lock, _ := m.starts.LoadOrStore(id, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	m.mu.Lock()
	if visibleOnly && m.quiet[id] {
		m.mu.Unlock()
		return false
	}
	proc := m.procs[id]
	delete(m.procs, id)
	delete(m.quiet, id)
	delete(m.addons, id)
	m.mu.Unlock()
	proc.Close()
	return true
}

func (m *Manager) StopAll() {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.mu.Lock()
	ids := make([]int64, 0, len(m.procs))
	for id := range m.procs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

func (m *Manager) IsRunning(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.procs[id].Alive()
}
func (m *Manager) IsHeadless(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.procs[id].Alive() && m.quiet[id]
}
func (m *Manager) pid(id int64) int { m.mu.Lock(); defer m.mu.Unlock(); return m.procs[id].PID() }

// RemoveProfile only deletes the current account directory inside dataRoot.
func RemoveProfile(dataRoot, backend string, b store.Browser) error {
	if _, err := camoufox.Find(camoufox.Endpoint(b.Port)); err == nil {
		return errors.New("请先关闭该账号窗口")
	}
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
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolvedParent, parentErr := filepath.EvalSymlinks(filepath.Dir(clean))
	if os.IsNotExist(parentErr) {
		return nil
	}
	if rootErr != nil || parentErr != nil {
		return errors.Join(rootErr, parentErr)
	}
	parentRel, err := filepath.Rel(resolvedRoot, resolvedParent)
	if err != nil || parentRel == ".." || strings.HasPrefix(parentRel, ".."+string(filepath.Separator)) {
		return errors.New("账号目录不能通过符号链接指向数据目录之外")
	}

	var last error
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(clean); os.IsNotExist(err) {
			return nil
		}
		last = os.RemoveAll(clean)
		if _, err := os.Stat(clean); os.IsNotExist(err) {
			return nil
		}

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
	return ProfileDir(dataRoot, backend, fmt.Sprintf("account-%d", b.ID))
}
