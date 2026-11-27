// Package browser manages the application's isolated Fingerprint browser accounts.
package browser

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xxww0098/realbrowser/browserkernel"

	"github.com/xxww0098/tulai/internal/fingerprint"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

var ErrDebugPort = errors.New("账号窗口尚未就绪")

// ProfileDir gives each managed Fingerprint account its own directory.
func ProfileDir(dataRoot, _, name string) string {
	name = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		name = "account"
	}
	return filepath.Join(dataRoot, "Fingerprint", name)
}

func AccountEndpoint(b store.Browser) string {
	return fingerprint.FingerprintEndpoint(b.Port)
}

type accountProcess struct {
	*fingerprint.Process
	headless bool
}

type Manager struct {
	lifecycle sync.RWMutex
	mu        sync.Mutex
	starts    sync.Map
	instances browserkernel.Registry[int64, accountProcess]
	configDir string
	store     *store.Store
	headless  bool
	// taskVisible 让任务浏览器显示窗口（设置「生图时显示浏览器窗口」）。
	taskVisible bool
	// onExit 在浏览器自行退出时通知界面。主动停止或换进程不触发。
	onExit func(int64)
}

func NewManager() *Manager { return NewManagerWithConfig("") }

func NewManagerWithConfig(configDir string) *Manager {
	return &Manager{configDir: configDir}
}

func (m *Manager) runtimeConfigDir() string {
	if m.configDir != "" {
		return m.configDir
	}
	return ConfigDir()
}

func (m *Manager) CheckLauncher(ctx context.Context, _ string) error {
	return fingerprint.CheckLauncher(ctx, m.runtimeConfigDir())
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

// SetStore attaches the account store used for same-platform shared-exit warnings.
func (m *Manager) SetStore(st *store.Store) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.store = st
	m.mu.Unlock()
}

func (m *Manager) Start(b store.Browser, dataRoot, _, startURL string, background bool) (string, error) {
	return m.start(context.Background(), b, dataRoot, startURL, background, false)
}

// StartView opens or reuses the account's background browser for the live drawer.
func (m *Manager) StartView(ctx context.Context, b store.Browser, dataRoot, startURL string) (string, error) {
	return m.start(ctx, b, dataRoot, startURL, true, false)
}

// StartTask uses a headless process unless task windows are enabled, even if the
// account was left open for login.
func (m *Manager) StartTask(ctx context.Context, b store.Browser, dataRoot, _, startURL string) (string, error) {
	return m.start(ctx, b, dataRoot, startURL, true, true)
}

func (m *Manager) start(ctx context.Context, b store.Browser, dataRoot, startURL string, background, task bool) (string, error) {
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
	running, _ := m.instances.Get(b.ID)
	m.mu.Lock()
	st := m.store
	headless := background || m.headless
	if task {
		headless = !m.taskVisible || m.headless
	}
	m.mu.Unlock()
	reuse := running.headless == headless
	if !task {
		reuse = true
	}
	if running.Alive() && reuse {
		if task {
			logging.Log.Info("账号[%s] 复用已打开的浏览器", b.Name)
		}
		return AccountEndpoint(b), nil
	}
	dir := accountProfilePath(dataRoot, KindFingerprint, b)
	if dir == "" {
		return "", errors.New("缺少账号资料目录")
	}
	if err := checkManagedProfile(dataRoot, "Fingerprint", KindFingerprint, dir, true); err != nil {
		return "", err
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
		// 先摘掉记录再关进程：这是换形态重开，不是用户关窗口，不该通知界面。
		m.instances.RemoveIf(b.ID, nil)
		running.Close()
	}
	roots := collisionRoots(dataRoot, dir, configDir, st)
	if task && !headless {
		// 显示窗口生图时，多个账号窗口一起弹出也不能抢走用户正在用的窗口。
		defer holdForeground()()
	}
	proc, err := fingerprint.Start(ctx, configDir, fingerprint.Options{
		Profile:        dir,
		Proxy:          proxy,
		Headless:       headless,
		CollisionRoots: roots,
	}, nil)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(startURL) != "" && startURL != "about:blank" {
		if err := proc.Call(ctx, proc.Page, "Page.navigate", map[string]any{"url": startURL, "wait": "commit"}, nil); err != nil {
			proc.Close()
			return "", fmt.Errorf("打开账号页面: %w", err)
		}
	}
	m.instances.Set(b.ID, accountProcess{Process: proc, headless: headless})
	fingerprint.Register(b.Port, proc)
	go m.watchExit(b.ID, b.Port, proc)
	logging.Log.Info("账号[%s] 已使用 %s 打开", b.Name, KindFingerprint)
	return AccountEndpoint(b), nil
}

// SetExitHandler registers the callback for a managed browser that exits on its
// own, such as when the user closes the window. Stop and a replaced process keep
// the account quiet, so the interface only hears about exits it did not cause.
func (m *Manager) SetExitHandler(fn func(id int64)) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.onExit = fn
	m.mu.Unlock()
}

// watchExit forgets a browser as soon as it exits and reports the exit, so a
// window the user closed stops showing as running. Stop and a replaced process
// already dropped the record, so they stay quiet.
func (m *Manager) watchExit(id int64, port int, proc *fingerprint.Process) {
	<-proc.Context().Done()
	fingerprint.Unregister(port, proc)
	_, owner := m.instances.RemoveIf(id, func(e accountProcess) bool { return e.Process == proc })
	m.mu.Lock()
	handler := m.onExit
	m.mu.Unlock()
	if owner && handler != nil {
		handler(id)
	}
}

func (m *Manager) Stop(id int64) {
	m.stop(id, false)
}

// StopLogin leaves an account alone if a task has already switched it to the background.
func (m *Manager) StopLogin(id int64) bool {
	return m.stop(id, true)
}

func (m *Manager) stop(id int64, visibleOnly bool) bool {
	lock, _ := m.starts.LoadOrStore(id, make(chan struct{}, 1))
	gate := lock.(chan struct{})
	gate <- struct{}{}
	defer func() { <-gate }()
	entry, removed := m.instances.RemoveIf(id, func(e accountProcess) bool { return !visibleOnly || !e.headless })
	if !removed {
		return !entry.headless
	}
	entry.Close()
	return true
}

func (m *Manager) StopAll() {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	var wg sync.WaitGroup
	for id := range m.instances.Snapshot() {
		wg.Go(func() {
			m.Stop(id)
		})
	}
	wg.Wait()
}

func (m *Manager) IsRunning(id int64) bool {
	entry, _ := m.instances.Get(id)
	return entry.Alive()
}

func (m *Manager) IsHeadless(id int64) bool {
	entry, _ := m.instances.Get(id)
	return entry.Alive() && entry.headless
}

// RunningVisibleIDs returns account IDs that currently have an alive visible browser window.
func (m *Manager) RunningVisibleIDs() []int64 {
	if m == nil {
		return nil
	}
	var ids []int64
	for id, proc := range m.instances.Snapshot() {
		if proc.Alive() && !proc.headless {
			ids = append(ids, id)
		}
	}
	return ids
}

// ProbeVisible sends a lightweight ping to a visible managed browser so closed
// windows (including macOS zero-window Chromium instances) are detected promptly.
func (m *Manager) ProbeVisible(ctx context.Context, id int64) bool {
	if m == nil {
		return false
	}
	proc, _ := m.instances.Get(id)
	if !proc.Alive() {
		return false
	}
	if proc.headless {
		return true
	}
	var res struct {
		Alive *bool `json:"alive"`
	}
	if err := proc.Call(ctx, "", "ping", nil, &res); err != nil {
		return proc.Alive()
	}
	if res.Alive != nil && !*res.Alive {
		return false
	}
	return proc.Alive()
}

func (m *Manager) pid(id int64) int { entry, _ := m.instances.Get(id); return entry.PID() }

// RemoveProfile only deletes the current account directory inside dataRoot.
func RemoveProfile(dataRoot, _ string, b store.Browser) error {
	if _, err := fingerprint.Find(AccountEndpoint(b)); err == nil {
		return errors.New("请先关闭该账号窗口")
	}
	dir := accountProfilePath(dataRoot, KindFingerprint, b)
	root := filepath.Clean(strings.TrimSpace(dataRoot))
	if dir == "" || root == "" || root == "." {
		return nil
	}
	clean := filepath.Clean(dir)
	rel, err := filepath.Rel(root, clean)
	if err != nil || rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
		return nil
	}
	if st, err := os.Lstat(clean); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return errors.New(KindFingerprint + " 资料路径必须是独立目录")
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

// AccountProfileDir resolves one application's managed account for preference I/O.
func AccountProfileDir(dataRoot string, b store.Browser, create bool) (string, error) {
	dir := accountProfilePath(dataRoot, KindFingerprint, b)
	return dir, checkManagedProfile(dataRoot, "Fingerprint", KindFingerprint, dir, create)
}

func accountProfilePath(dataRoot, backend string, b store.Browser) string {
	if dir := strings.TrimSpace(b.UserDataDir); dir != "" {
		return dir
	}
	if strings.TrimSpace(b.Name) == "" {
		return ""
	}
	return ProfileDir(dataRoot, backend, fmt.Sprintf("account-%d", b.ID))
}

// A managed Fingerprint account always owns one immediate child of its dedicated
// directory. Reject paths redirected to existing personal profiles.
func checkManagedProfile(dataRoot, subdir, label, dir string, create bool) error {
	if strings.TrimSpace(dataRoot) == "" {
		return errors.New("缺少 " + label + " 资料根目录")
	}
	base := filepath.Join(dataRoot, subdir)
	rel, err := filepath.Rel(base, dir)
	if err != nil || !filepath.IsLocal(rel) || rel == "." || filepath.Base(rel) != rel {
		return errors.New(label + " 账号资料必须位于专用目录")
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
			return errors.New(label + " 资料路径必须是独立目录")
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
		candidates = append(candidates, filepath.Join(dataRoot, "Fingerprint"))
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
