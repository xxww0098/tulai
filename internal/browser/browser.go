// Package browser manages the application's isolated browser accounts.
package browser

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

var ErrDebugPort = errors.New("账号窗口尚未就绪")

// ProfileDir gives each managed browser its own account directory tree.
func ProfileDir(dataRoot, backend, name string) string {
	name = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		name = "account"
	}
	if backend == KindChromeCDP {
		return filepath.Join(dataRoot, "Chrome-CDP", name)
	}
	return filepath.Join(dataRoot, KindCamoufox, name)
}

func AccountEndpoint(b store.Browser) string {
	if b.Engine == KindChromeCDP {
		return camoufox.ChromeEndpoint(b.Port)
	}
	return camoufox.Endpoint(b.Port)
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
	store     *store.Store
	headless  bool
	// taskVisible 让任务浏览器显示窗口（设置「生图时显示浏览器窗口」）。
	taskVisible bool
}

func NewManager() *Manager { return NewManagerWithConfig("") }

func NewManagerWithConfig(configDir string) *Manager {
	return &Manager{procs: map[int64]*camoufox.Process{}, configDir: configDir}
}

func (m *Manager) runtimeConfigDir() string {
	if m.configDir != "" {
		return m.configDir
	}
	return ConfigDir()
}

func (m *Manager) CheckLauncher(ctx context.Context, kind string) error {
	if kind == KindChromeCDP {
		_, err := Resolve(runtime.GOOS, kind, m.runtimeConfigDir())
		return err
	}
	return camoufox.CheckLauncher(ctx, m.runtimeConfigDir())
}

// SetTaskVisible 切换任务浏览器是否显示窗口；已开的浏览器形态不符时，下一次任务会重开。
func (m *Manager) SetTaskVisible(on bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.taskVisible = on
	m.mu.Unlock()
}

// TaskVisible 是当前任务浏览器是否显示窗口。
func (m *Manager) TaskVisible() bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.taskVisible
}

// LoadedAddon 是正在运行的浏览器装着的页面桥目录；没运行或没装时为空。
func (m *Manager) LoadedAddon(id int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.procs[id].Alive() {
		return ""
	}
	return m.addons[id]
}

// SetStore attaches the account store used for same-platform shared-exit warnings.
func (m *Manager) SetStore(st *store.Store) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.store = st
	m.mu.Unlock()
}

func (m *Manager) Start(b store.Browser, dataRoot, backend, startURL string, background bool) (string, error) {
	return m.start(context.Background(), b, dataRoot, startURL, background, false, "")
}

// StartViewWithAddon opens a background view with the bound page-bridge addon
// preinstalled, so a task requesting the same addon reuses this browser.
func (m *Manager) StartViewWithAddon(b store.Browser, dataRoot, startURL, addon string) (string, error) {
	return m.StartView(context.Background(), b, dataRoot, startURL, addon)
}

// StartView opens or reuses the account's background browser for the live drawer.
func (m *Manager) StartView(ctx context.Context, b store.Browser, dataRoot, startURL, addon string) (string, error) {
	return m.start(ctx, b, dataRoot, startURL, true, false, addon)
}

// StartTask uses a headless process unless task windows are enabled, even if the
// account was left open for login.
func (m *Manager) StartTask(ctx context.Context, b store.Browser, dataRoot, backend, startURL string) (string, error) {
	return m.start(ctx, b, dataRoot, startURL, true, true, "")
}

func (m *Manager) StartTaskWithAddon(ctx context.Context, b store.Browser, dataRoot, startURL, addon string) (string, error) {
	return m.start(ctx, b, dataRoot, startURL, true, true, addon)
}

func (m *Manager) start(ctx context.Context, b store.Browser, dataRoot, startURL string, background, task bool, addon string) (string, error) {
	kind := CanonicalBackend(b.Engine)
	if kind == KindChromeCDP && addon != "" {
		return "", errors.New("Chrome CDP 账号使用内置 CDP 页面控制")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	m.lifecycle.RLock()
	defer m.lifecycle.RUnlock()
	lock, _ := m.starts.LoadOrStore(b.ID, make(chan struct{}, 1))
	gate := lock.(chan struct{})
	select {
	case gate <- struct{}{}:
		defer func() { <-gate }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	m.mu.Lock()
	running := m.procs[b.ID]
	quiet := m.quiet[b.ID]
	loadedAddon := m.addons[b.ID]
	st := m.store
	headless := background || m.headless
	if task {
		headless = !m.taskVisible || m.headless
	}
	m.mu.Unlock()
	reuse := quiet == headless && loadedAddon == addon
	if !task {
		reuse = addon == "" || loadedAddon == "" || loadedAddon == addon
	}
	if running.Alive() && reuse {
		if task {
			logging.Log.Info("账号[%s] 复用已打开的浏览器", b.Name)
		}
		return AccountEndpoint(b), nil
	}
	dir := accountProfilePath(dataRoot, kind, b)
	if dir == "" {
		return "", errors.New("缺少账号资料目录")
	}
	if kind == KindChromeCDP {
		if err := checkChromeProfile(dataRoot, dir, true); err != nil {
			return "", err
		}
	}
	configDir := m.runtimeConfigDir()
	var proxy, globalProxy string
	if b.Proxy != "" {
		var ok bool
		proxy, ok = NormalizeProxy(b.Proxy)
		if !ok {
			return "", errors.New("账号代理地址无效")
		}
		globalProxy = CurrentProxyServer()
	} else {
		var err error
		proxy, err = ResolveCurrentProxy()
		if err != nil {
			return "", err
		}
		globalProxy = proxy
	}
	warnSharedExit(st, b, proxy, globalProxy)
	if running.Alive() {
		running.Close()
		m.mu.Lock()
		delete(m.procs, b.ID)
		delete(m.quiet, b.ID)
		delete(m.addons, b.ID)
		m.mu.Unlock()
	}
	var addons []string
	if addon != "" {
		addons = []string{addon}
	}
	roots := collisionRoots(dataRoot, dir, configDir, st)
	if task && !headless {
		// 显示窗口生图时，多个账号窗口一起弹出也不能抢走用户正在用的窗口。
		defer holdForeground()()
	}
	proc, err := camoufox.Start(ctx, configDir, camoufox.Options{
		ChromeCDP:      kind == KindChromeCDP,
		Profile:        dir,
		Proxy:          proxy,
		Headless:       headless,
		Addons:         addons,
		CollisionRoots: roots,
	}, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(startURL) != "" && startURL != "about:blank" {
		// 只等导航提交：绑定模式由扩展轮询页面就绪，CDP 模式随后会再导航一次。
		if err := proc.Call(ctx, proc.Page, "Page.navigate", map[string]any{"url": startURL, "wait": "commit"}, nil); err != nil {
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
	logging.Log.Info("账号[%s] 已使用 %s 打开", b.Name, kind)
	return AccountEndpoint(b), nil
}

func (m *Manager) SetAppFocus(fn func()) { m.mu.Lock(); m.focusApp = fn; m.mu.Unlock() }

func (m *Manager) Stop(id int64) {
	m.stop(id, false, nil)
}

// StopLogin leaves an account alone if a task has already switched it to the background.
func (m *Manager) StopLogin(id int64) bool {
	return m.stop(id, true, nil)
}

// StopWithAddon checks the loaded bridge under the account lock. An empty name matches a plain browser.
func (m *Manager) StopWithAddon(id int64, addon string) bool {
	return m.stop(id, false, &addon)
}

func (m *Manager) stop(id int64, visibleOnly bool, addon *string) bool {
	lock, _ := m.starts.LoadOrStore(id, make(chan struct{}, 1))
	gate := lock.(chan struct{})
	gate <- struct{}{}
	defer func() { <-gate }()
	m.mu.Lock()
	if (visibleOnly && m.quiet[id]) || (addon != nil && m.addons[id] != *addon) {
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
	if _, err := camoufox.Find(AccountEndpoint(b)); err == nil {
		return errors.New("请先关闭该账号窗口")
	}
	dir := accountProfilePath(dataRoot, CanonicalBackend(b.Engine), b)
	if b.Engine == KindChromeCDP {
		if err := checkChromeProfile(dataRoot, dir, false); err != nil {
			return err
		}
	}
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

// A Chrome account always owns one immediate child of the Chrome-CDP directory.
// Reject paths redirected to existing personal or Firefox profiles.
func checkChromeProfile(dataRoot, dir string, create bool) error {
	if strings.TrimSpace(dataRoot) == "" {
		return errors.New("缺少 Chrome CDP 资料根目录")
	}
	base := filepath.Join(dataRoot, "Chrome-CDP")
	rel, err := filepath.Rel(base, dir)
	if err != nil || !filepath.IsLocal(rel) || rel == "." || filepath.Base(rel) != rel {
		return errors.New("Chrome CDP 账号资料必须位于专用目录")
	}
	for _, path := range []string{base, dir} {
		st, err := os.Lstat(path)
		if os.IsNotExist(err) {
			if create {
				if err := os.MkdirAll(path, 0o700); err != nil {
					return err
				}
			}
			continue
		}
		if err != nil {
			return err
		}
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("Chrome CDP 资料路径必须是独立目录")
		}
	}
	return nil
}

func warnSharedExit(st *store.Store, target store.Browser, effectiveProxy, globalProxy string) {
	if st == nil {
		return
	}
	shared := SamePlatformSharedExit(st.Browsers(), target, globalProxy)
	if len(shared) == 0 {
		return
	}
	exitLabel := NormalizeProxyKey(effectiveProxy)
	if exitLabel == "" {
		exitLabel = "direct"
	}
	logging.Log.Warning("账号[%s] 与同平台账号 %s 共用同一出口(%s)，可能触发平台关联风控", target.Name, strings.Join(shared, "、"), exitLabel)
	log.Printf("warning: account %q shares exit (%s) with same-platform accounts: %s", target.Name, exitLabel, strings.Join(shared, ", "))
}

func collisionRoots(dataRoot, profileDir, configDir string, st *store.Store) []string {
	var candidates []string
	if profileDir = strings.TrimSpace(profileDir); profileDir != "" {
		candidates = append(candidates, filepath.Dir(profileDir))
	}
	if dataRoot = strings.TrimSpace(dataRoot); dataRoot != "" {
		candidates = append(candidates, filepath.Join(dataRoot, KindCamoufox))
		cleanData := filepath.Clean(dataRoot)
		if filepath.Base(cleanData) == "BrowserData" {
			candidates = append(candidates, filepath.Join(filepath.Dir(cleanData), "DownloadBrowser"))
		}
	}
	if configDir = strings.TrimSpace(configDir); configDir != "" {
		candidates = append(candidates, filepath.Join(configDir, "DownloadBrowser"))
	}
	if st != nil {
		if dbFile := strings.TrimSpace(st.File()); dbFile != "" && filepath.Base(filepath.Dir(dbFile)) == "database" {
			candidates = append(candidates, filepath.Join(filepath.Dir(filepath.Dir(dbFile)), "DownloadBrowser"))
		}
	}
	seen := make(map[string]struct{}, len(candidates))
	out := make([]string, 0, len(candidates))
	for _, raw := range candidates {
		abs, err := filepath.Abs(raw)
		if err != nil {
			abs = filepath.Clean(raw)
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out
}
