// Package fingerprint manages the dedicated fingerprint-chromium runtime through private pipes.
package fingerprint

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fpprocess "github.com/xxww0098/realbrowser/fingerprintkernel/process"
	fpprofile "github.com/xxww0098/realbrowser/fingerprintkernel/profile"
)

var ansiEscapeRe = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

func stripANSI(s string) string {
	return ansiEscapeRe.ReplaceAllString(s, "")
}

const legacyIdentityFile = ".tulai-fingerprint.json"

type Options struct {
	Profile           string                     `json:"profile"`
	Proxy             string                     `json:"proxy,omitempty"`
	DirectNetwork     bool                       `json:"directNetwork,omitempty"`
	Headless          bool                       `json:"headless"`
	Executable        string                     `json:"executable,omitempty"`
	Addons            []string                   `json:"addons,omitempty"`
	CollisionRoots    []string                   `json:"collisionRoots,omitempty"`
	IgnoreHTTPSErrors bool                       `json:"ignoreHTTPSErrors,omitempty"`
	Timezone          string                     `json:"timezone,omitempty"`
	Lang              string                     `json:"lang,omitempty"`
	WindowWidth       int                        `json:"windowWidth,omitempty"`
	WindowHeight      int                        `json:"windowHeight,omitempty"`
	ColorScheme       fpprofile.ColorSchemeMode  `json:"colorScheme,omitempty"`
	Identity          *fpprofile.FingerprintSpec `json:"identity,omitempty"`
}

type Event struct {
	Event  string          `json:"event"`
	Target string          `json:"target"`
	Data   json.RawMessage `json:"data"`
}

type reply struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  string          `json:"error"`
	Event
}

type Process struct {
	child     *fpprocess.Process
	in        io.WriteCloser
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	writeMu   sync.Mutex
	mu        sync.Mutex
	pending   map[int64]chan reply
	listeners map[int64]func(Event)
	next      atomic.Int64
	closeOnce sync.Once
	Page      string
}

func applyProfileIdentity(ctx context.Context, profile string, roots []string, options *Options) error {
	prefs, err := fpprofile.EnsureProfile(ctx, profile, fpprofile.EnsureOptions{
		CollisionRoots:     roots,
		LegacyIdentityFile: legacyIdentityFile,
	})
	if err != nil {
		return fmt.Errorf("读取指纹配置: %w", err)
	}
	if options.Timezone == "" {
		options.Timezone = prefs.Timezone
	}
	if options.Lang == "" {
		options.Lang = prefs.Lang
	}
	options.WindowWidth, options.WindowHeight, options.ColorScheme = prefs.WindowWidth, prefs.WindowHeight, prefs.ColorScheme
	options.Identity = &prefs
	return nil
}

// Start opens the active managed Fingerprint runtime for this profile.
func Start(ctx context.Context, dataDir string, options Options, stderr io.Writer) (*Process, error) {
	options.Proxy = strings.TrimSpace(options.Proxy)
	if options.DirectNetwork && options.Proxy != "" {
		return nil, errors.New("浏览器直连模式不能同时指定代理")
	}
	installMu.Lock()
	runtime, ok := InstalledFingerprint(dataDir)
	var driverErr error
	if ok {
		driverErr = writeDriverFiles(runtime.Dir, bridgeScripts)
	}
	installMu.Unlock()
	if !ok {
		return nil, errors.New("Fingerprint 尚未准备好，请先在「设置 → 浏览器与网络」安装 Fingerprint")
	}
	if driverErr != nil {
		return nil, driverErr
	}
	if options.Profile == "" {
		return nil, errors.New("缺少浏览器账号资料目录")
	}
	profile, err := filepath.Abs(options.Profile)
	if err != nil {
		return nil, err
	}
	roots, err := absUniquePaths(options.CollisionRoots)
	if err != nil {
		return nil, err
	}
	if err := applyProfileIdentity(ctx, profile, roots, &options); err != nil {
		return nil, err
	}
	options.Profile = profile
	options.CollisionRoots = roots
	options.Executable = runtime.Executable
	args := []string{filepath.Join(runtime.Dir, "bridge.mjs")}
	cmd := exec.Command(runtime.Node, args...)
	cmd.Dir = runtime.Dir
	cmd.Env = browserRuntimeEnv(runtime.Dir, options.DirectNetwork)
	cmd.Stderr = stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, writer, err := os.Pipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	cmd.Stdout = writer
	child, err := fpprocess.StartProcess(cmd, fpprocess.PrivateDriver)
	_ = writer.Close()
	if err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	life, cancel := context.WithCancel(context.Background())
	p := &Process{child: child, in: in, ctx: life, cancel: cancel, done: make(chan struct{}), pending: map[int64]chan reply{}, listeners: map[int64]func(Event){}}
	go func() {
		p.read(out)
		_ = out.Close()
		child.Wait()
		cancel()
		close(p.done)
	}()
	var result struct {
		TargetID string `json:"targetId"`
	}
	if err := p.Call(ctx, "", "start", options, &result); err != nil {
		p.Close()
		return nil, fmt.Errorf("启动 Fingerprint: %w", err)
	}
	p.Page = result.TargetID
	return p, nil
}

func (p *Process) read(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 128<<20)
	for scanner.Scan() {
		var msg reply
		if json.Unmarshal(scanner.Bytes(), &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			p.mu.Lock()
			ch := p.pending[msg.ID]
			delete(p.pending, msg.ID)
			p.mu.Unlock()
			if ch != nil {
				ch <- msg
			}
			continue
		}
		if msg.Event.Event == "" {
			continue
		}
		p.mu.Lock()
		listeners := make([]func(Event), 0, len(p.listeners))
		for _, fn := range p.listeners {
			listeners = append(listeners, fn)
		}
		p.mu.Unlock()
		for _, fn := range listeners {
			fn(msg.Event)
		}
	}
	p.mu.Lock()
	pending := p.pending
	p.pending = map[int64]chan reply{}
	p.mu.Unlock()
	for _, ch := range pending {
		ch <- reply{Error: "browser driver closed"}
	}
}

func (p *Process) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err = p.in.Write(data)
	return err
}

func (p *Process) Call(ctx context.Context, target, method string, params, out any) error {
	if p == nil {
		return errors.New("账号浏览器尚未启动")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := p.next.Add(1)
	ch := make(chan reply, 1)
	p.mu.Lock()
	if p.ctx.Err() != nil {
		p.mu.Unlock()
		return errors.New("账号浏览器已关闭")
	}
	p.pending[id] = ch
	p.mu.Unlock()
	timeout := int64(60000)
	if deadline, ok := ctx.Deadline(); ok {
		timeout = max(1, time.Until(deadline).Milliseconds())
	}
	if err := p.write(map[string]any{"id": id, "target": target, "method": method, "params": params, "timeout": timeout}); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return err
	}
	select {
	case msg := <-ch:
		if msg.Error != "" {
			return errors.New(stripANSI(msg.Error))
		}
		if out != nil && len(msg.Result) > 0 {
			return json.Unmarshal(msg.Result, out)
		}
		return nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		_ = p.write(map[string]any{"id": id, "method": "cancel"})
		return ctx.Err()
	case <-p.ctx.Done():
		return errors.New("账号浏览器已关闭")
	}
}

func (p *Process) Context() context.Context { return p.ctx }
func (p *Process) Alive() bool              { return p != nil && p.ctx.Err() == nil }
func (p *Process) PID() int {
	if p == nil || p.child == nil {
		return 0
	}
	return p.child.PID()
}

// Subscribe callbacks must return promptly; page calls belong in a separate goroutine.
func (p *Process) Subscribe(listener func(Event)) func() {
	id := p.next.Add(1)
	p.mu.Lock()
	p.listeners[id] = listener
	p.mu.Unlock()
	return func() { p.mu.Lock(); delete(p.listeners, id); p.mu.Unlock() }
}

// Close lets the browser flush its profile before the private launcher exits.
func (p *Process) Close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		_ = p.Call(ctx, "", "Browser.close", nil, nil)
		cancel()
		_ = p.in.Close()
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			_ = p.child.Kill()
			<-p.done
		}
	})
}

var registry sync.Map

func FingerprintEndpoint(port int) string { return "fingerprint://" + strconv.Itoa(port) }
func Endpoint(port int) string            { return FingerprintEndpoint(port) }
func (p *Process) endpoint(port int) string {
	return FingerprintEndpoint(port)
}
func Register(port int, process *Process) { registry.Store(process.endpoint(port), process) }
func Unregister(port int, process *Process) {
	registry.CompareAndDelete(process.endpoint(port), process)
}

func Find(endpoint string) (*Process, error) {
	value, ok := registry.Load(strings.TrimSpace(endpoint))
	if !ok {
		return nil, errors.New("账号窗口尚未打开")
	}
	p := value.(*Process)
	if !p.Alive() {
		return nil, errors.New("账号窗口已关闭")
	}
	return p, nil
}

func browserRuntimeEnv(dir string, direct bool) []string {
	env := runtimeEnv("", dir)
	if !direct {
		return env
	}
	return slices.DeleteFunc(env, func(item string) bool {
		key, _, _ := strings.Cut(item, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NPM_CONFIG_PROXY", "NPM_CONFIG_HTTP_PROXY", "NPM_CONFIG_HTTPS_PROXY":
			return true
		default:
			return false
		}
	})
}

func runtimeEnv(proxy, dir string) []string {
	inherited := os.Environ()
	env := make([]string, 0, len(inherited)+6)
	for _, item := range inherited {
		key, _, _ := strings.Cut(item, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "NAPI_RS_") || key == "FONTCONFIG_FILE" || key == "TULAI_FINGERPRINT_MIRROR" || key == "TULAI_FINGERPRINT_MIRRORS" || key == "TULAI_FINGERPRINT_DIRECT_FIRST" || key == "TULAI_FINGERPRINT_ARCHIVE" || key == "TULAI_FINGERPRINT_URL" || key == "TULAI_FINGERPRINT_DEST" {
			continue
		}
		env = append(env, item)
	}
	if proxy != "" {
		env = append(env, "HTTPS_PROXY="+proxy, "HTTP_PROXY="+proxy, "npm_config_proxy="+proxy, "npm_config_https_proxy="+proxy)
	}
	return append(env, "NODE_USE_ENV_PROXY=1", "NO_COLOR=1", "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1")
}

// absUniquePaths resolves roots against the host process cwd before bridge.mjs
// runs with cmd.Dir set to the runtime directory.
func absUniquePaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, raw := range paths {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		abs, err := filepath.Abs(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out, nil
}
