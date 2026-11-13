// Package camoufox controls the official Camoufox launcher through a private pipe.
package camoufox

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
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	Profile    string   `json:"profile"`
	Proxy      string   `json:"proxy,omitempty"`
	Headless   bool     `json:"headless"`
	Executable string   `json:"executable,omitempty"`
	Addons     []string `json:"addons,omitempty"`
	// Local integration fixtures use a proxy with no public exit and a self-signed certificate.
	GeoIP             *bool `json:"geoip,omitempty"`
	IgnoreHTTPSErrors bool  `json:"ignoreHTTPSErrors,omitempty"`
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
	cmd       *exec.Cmd
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

// Start uses an already installed, version-pinned runtime. It never searches for a system browser.
func Start(ctx context.Context, dataDir string, options Options, stderr io.Writer) (*Process, error) {
	installMu.Lock()
	runtime, ok := Installed(dataDir)
	var driverErr error
	if ok {
		driverErr = ensureNodeRuntimeSupport(runtime.Dir)
		if driverErr == nil {
			driverErr = writeDriver(runtime.Dir)
		}
	}
	installMu.Unlock()
	if !ok {
		return nil, errors.New("Camoufox 尚未准备好，请先在账号页安装 Camoufox")
	}
	if driverErr != nil {
		return nil, driverErr
	}
	if options.Profile == "" {
		return nil, errors.New("缺少 Camoufox 账号资料目录")
	}
	profile, err := filepath.Abs(options.Profile)
	if err != nil {
		return nil, err
	}
	options.Profile = profile
	options.Executable = runtime.Executable
	cmd := exec.Command(runtime.Node, filepath.Join(runtime.Dir, "bridge.mjs"))
	cmd.Dir = runtime.Dir
	cmd.Env = runtimeEnv("", runtime.Dir)
	cmd.Stderr = stderr
	configureCommand(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	life, cancel := context.WithCancel(context.Background())
	p := &Process{cmd: cmd, in: in, ctx: life, cancel: cancel, done: make(chan struct{}), pending: map[int64]chan reply{}, listeners: map[int64]func(Event){}}
	go func() {
		p.read(out)
		_ = cmd.Wait()
		cancel()
		close(p.done)
	}()
	var result struct {
		TargetID string `json:"targetId"`
	}
	if err := p.Call(ctx, "", "start", options, &result); err != nil {
		p.Close()
		return nil, fmt.Errorf("启动 Camoufox: %w", err)
	}
	p.Page = result.TargetID
	return p, nil
}

func (p *Process) Context() context.Context { return p.ctx }
func (p *Process) Alive() bool              { return p != nil && p.ctx.Err() == nil }
func (p *Process) PID() int {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p *Process) Call(ctx context.Context, target, method string, params, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.Alive() {
		return errors.New("Camoufox 已关闭")
	}
	id := p.next.Add(1)
	ch := make(chan reply, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.pending, id); p.mu.Unlock() }()
	timeout := 60 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	if err := p.write(map[string]any{"id": id, "target": target, "method": method, "params": params, "timeout": max(1, timeout.Milliseconds())}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		_ = p.write(map[string]any{"id": id, "method": "cancel"})
		return ctx.Err()
	case <-p.ctx.Done():
		return errors.New("Camoufox 已关闭")
	case result := <-ch:
		if result.Error != "" {
			return fmt.Errorf("%s: %s", method, result.Error)
		}
		if out != nil && len(result.Result) > 0 {
			return json.Unmarshal(result.Result, out)
		}
		return nil
	}
}

func (p *Process) write(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err = p.in.Write(append(data, '\n'))
	return err
}

func (p *Process) read(out io.Reader) {
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 64<<10), 96<<20)
	for scanner.Scan() {
		var message reply
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			continue
		}
		p.mu.Lock()
		ch := p.pending[message.ID]
		var listeners []func(Event)
		if message.Event.Event != "" {
			for _, listener := range p.listeners {
				listeners = append(listeners, listener)
			}
		}
		p.mu.Unlock()
		if ch != nil {
			select {
			case ch <- message:
			default:
			}
		}
		for _, listener := range listeners {
			listener(message.Event)
		}
	}
	p.cancel()
}

// Subscribe callbacks must return promptly; page calls belong in a separate goroutine.
func (p *Process) Subscribe(listener func(Event)) func() {
	id := p.next.Add(1)
	p.mu.Lock()
	p.listeners[id] = listener
	p.mu.Unlock()
	return func() { p.mu.Lock(); delete(p.listeners, id); p.mu.Unlock() }
}

// Close lets Firefox flush cookies before the private launcher process exits.
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
			killCommand(p.cmd)
			<-p.done
		}
	})
}

var registry sync.Map

func Endpoint(port int) string              { return "camoufox://" + strconv.Itoa(port) }
func Register(port int, process *Process)   { registry.Store(Endpoint(port), process) }
func Unregister(port int, process *Process) { registry.CompareAndDelete(Endpoint(port), process) }

func Find(endpoint string) (*Process, error) {
	value, ok := registry.Load(strings.TrimSpace(endpoint))
	if !ok {
		return nil, errors.New("Camoufox 账号窗口尚未打开")
	}
	p := value.(*Process)
	if !p.Alive() {
		return nil, errors.New("Camoufox 账号窗口已关闭")
	}
	return p, nil
}

func runtimeEnv(proxy, dir string) []string {
	inherited := os.Environ()
	env := make([]string, 0, len(inherited)+6)
	for _, item := range inherited {
		key, _, _ := strings.Cut(item, "=")
		key = strings.ToUpper(key)
		if strings.HasPrefix(key, "CAMOU_CONFIG_") || strings.HasPrefix(key, "CAMOU_PREFS_") || strings.HasPrefix(key, "NAPI_RS_") || key == "FONTCONFIG_FILE" || key == "TULAI_CAMOUFOX_MIRROR" {
			continue
		}
		env = append(env, item)
	}
	if proxy != "" {
		env = append(env, "HTTPS_PROXY="+proxy, "HTTP_PROXY="+proxy, "npm_config_proxy="+proxy, "npm_config_https_proxy="+proxy)
	}
	return append(env, "NODE_USE_ENV_PROXY=1", "NO_COLOR=1", "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1",
		"TULAI_CAMOUFOX_CACHE="+filepath.Join(filepath.Dir(dir), "cache", runtime.GOOS+"-"+runtime.GOARCH))
}
