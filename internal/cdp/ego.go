package cdp

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed ego_bridge.js
var egoBridgeJS string

//go:embed ego_sdk.js
var egoSDKJS string

var egoInit = make(chan struct{}, 1)

type egoResponse struct {
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   string          `json:"error"`
	Event   string          `json:"event"`
	Payload string          `json:"payload"`
	Fatal   bool            `json:"fatal"`
}

type egoTransport struct {
	mu          sync.Mutex
	writeMu     sync.Mutex
	conn        net.Conn
	cmd         *exec.Cmd
	next        uint64
	pending     map[uint64]chan egoResponse
	done        chan struct{}
	processDone chan struct{}
	err         error
	onEvent     func(egoResponse)
	onClose     context.CancelFunc
}

func egoExecutable() (string, error) {
	names := []string{"ego-browser"}
	if runtime.GOOS == "windows" {
		names = []string{"ego-browser.exe", "ego-browser"}
	}
	var lookupErr error
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		} else {
			lookupErr = err
		}
	}
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		for _, name := range names {
			if path, err := exec.LookPath(filepath.Join(home, ".local", "bin", name)); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("未找到 ego-browser，请先打开 ego lite 完成初始化: %w", lookupErr)
}

func startEgoTransport(ctx context.Context, onEvent func(egoResponse), onClose context.CancelFunc) (*egoTransport, error) {
	path, err := egoExecutable()
	if err != nil {
		return nil, err
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	stopAccept := context.AfterFunc(ctx, func() { listener.Close() })
	defer stopAccept()
	if err := listener.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		return nil, err
	}
	token := rand.Text()
	config, _ := json.Marshal(map[string]any{"port": listener.Addr().(*net.TCPAddr).Port, "token": token})
	t := &egoTransport{pending: map[uint64]chan egoResponse{}, done: make(chan struct{}), processDone: make(chan struct{}), onEvent: onEvent, onClose: onClose}
	cmd, cleanup, err := egoCommand(ctx, path, "const bridgeConfig = "+string(config)+";\n"+egoSDKJS+"\n"+egoBridgeJS)
	if err != nil {
		return nil, err
	}
	t.cmd = cmd
	var output bytes.Buffer
	t.cmd.Stdout, t.cmd.Stderr = &output, &output
	if err := t.cmd.Start(); err != nil {
		cleanup()
		return nil, err
	}
	go func() {
		err := t.cmd.Wait()
		cleanup()
		if err == nil {
			err = errors.New("ego lite 控制进程已退出")
		} else {
			err = fmt.Errorf("ego lite 控制进程退出: %w: %s", err, strings.TrimSpace(output.String()))
		}
		t.fail(err)
		listener.Close()
		close(t.processDone)
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			t.fail(fmt.Errorf("连接 ego lite 控制桥失败: %w", err))
			t.close()
			return nil, t.failure()
		}
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		decoder := json.NewDecoder(conn)
		var auth struct {
			Token string `json:"token"`
		}
		if err := decoder.Decode(&auth); err != nil || auth.Token != token {
			conn.Close()
			continue
		}
		conn.SetReadDeadline(time.Time{})
		t.mu.Lock()
		if t.err != nil {
			t.mu.Unlock()
			conn.Close()
			return nil, t.failure()
		}
		t.conn = conn
		t.mu.Unlock()
		go t.read(decoder)
		return t, nil
	}
}

func (t *egoTransport) failure() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

func (t *egoTransport) fail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.err != nil {
		return
	}
	t.err = err
	if t.onClose != nil {
		t.onClose()
	}
	if t.conn != nil {
		t.conn.Close()
	}
	close(t.done)
}

func (t *egoTransport) close() {
	t.fail(errors.New("ego lite 控制会话已关闭"))
	if t.cmd != nil {
		select {
		case <-t.processDone:
			return
		default:
		}
		t.cmd.Cancel()
		<-t.processDone
	}
}

func (t *egoTransport) read(decoder *json.Decoder) {
	for {
		var response egoResponse
		if err := decoder.Decode(&response); err != nil {
			t.fail(fmt.Errorf("ego lite 控制连接中断: %w", err))
			return
		}
		if response.Event != "" {
			if response.Event == "error" {
				t.fail(errors.New(response.Error))
				return
			}
			if t.onEvent != nil {
				t.onEvent(response)
			}
			continue
		}
		t.mu.Lock()
		responseCh := t.pending[response.ID]
		t.mu.Unlock()
		if responseCh != nil {
			responseCh <- response
		}
	}
}

func (t *egoTransport) request(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t.mu.Lock()
	if t.err != nil {
		err := t.err
		t.mu.Unlock()
		return err
	}
	t.next++
	id := t.next
	responseCh := make(chan egoResponse, 1)
	t.pending[id] = responseCh
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}()
	t.writeMu.Lock()
	deadline, _ := ctx.Deadline()
	t.conn.SetWriteDeadline(deadline)
	err := json.NewEncoder(t.conn).Encode(map[string]any{"id": id, "method": method, "params": params})
	t.writeMu.Unlock()
	if err != nil {
		t.fail(err)
		return err
	}
	select {
	case response := <-responseCh:
		if response.Error != "" {
			if response.Fatal {
				t.fail(errors.New(response.Error))
			}
			return errors.New(response.Error)
		}
		if result != nil {
			return json.Unmarshal(response.Result, result)
		}
		return nil
	case <-ctx.Done():
		err := fmt.Errorf("ego lite 控制超时或已取消，会话已断开: %w", ctx.Err())
		t.fail(err)
		return err
	case <-t.done:
		return t.failure()
	}
}

// Execute 实现 CDP Executor，使已有 Runtime/Page 协议操作复用 ego lite 页面。
func (t *egoTransport) Execute(ctx context.Context, method string, params, result any) error {
	return t.request(ctx, "cdp", map[string]any{"method": method, "params": params}, result)
}

func egoPageHosts(rawURL string) ([]string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("无效的 ego lite 页面地址: %q", rawURL)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	port := u.Port()
	if u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443" {
		port = ""
	}
	hosts := []string{host}
	if net.ParseIP(host) == nil {
		hosts = append(hosts, "www."+host)
		if host == "tongyi.com" {
			hosts = append(hosts, "qianwen.com", "www.qianwen.com")
		} else if host == "qianwen.com" {
			hosts = append(hosts, "tongyi.com", "www.tongyi.com")
		}
	}
	for i, host := range hosts {
		if port != "" {
			hosts[i] = net.JoinHostPort(host, port)
		} else if strings.Contains(host, ":") {
			hosts[i] = "[" + host + "]"
		}
	}
	return hosts, nil
}

// NewEgoSession 连接软件的持久 ego lite 空间，复用同域名页面及其登录态。
func NewEgoSession(parent context.Context, url string, onLog func(string)) (*Session, error) {
	ctx, cancel := context.WithCancel(parent)
	s := &Session{ctx: ctx, cancel: cancel, hooks: map[string]func(string) string{}, onLog: onLog}
	hosts, err := egoPageHosts(url)
	if err != nil {
		cancel()
		return nil, errors.Join(err, context.Canceled)
	}
	transport, err := startEgoTransport(ctx, func(event egoResponse) {
		if event.Event == "binding" {
			go s.dispatchBridge(event.Payload)
		} else if event.Event == "log" && onLog != nil {
			onLog(event.Payload)
		}
	}, cancel)
	if err != nil {
		cancel()
		return nil, errors.Join(err, context.Canceled)
	}
	s.ego = transport
	initCtx, initCancel := context.WithTimeout(ctx, 75*time.Second)
	defer initCancel()
	select {
	case egoInit <- struct{}{}:
		err = transport.request(initCtx, "init", map[string]any{"url": url, "hosts": hosts}, nil)
		<-egoInit
	case <-initCtx.Done():
		err = initCtx.Err()
	}
	if err != nil {
		s.Close()
		return nil, errors.Join(err, context.Canceled)
	}
	if err := s.InjectBase(); err != nil {
		s.Close()
		return nil, errors.Join(err, context.Canceled)
	}
	return s, nil
}

// HandOff 显式交还 Ego 空间供用户操作，保留页面与会话；Chrome 无需交还。
func (s *Session) HandOff() error {
	if s.ego == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	return s.ego.request(ctx, "handoff", nil, nil)
}

// OpenEgoURL 激活软件自己的平台页面，断开控制后保留页面供用户登录。
func OpenEgoURL(url string) error {
	openCtx, openCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer openCancel()
	s, err := NewEgoSession(openCtx, url, nil)
	if err != nil {
		return err
	}
	defer s.Close()
	return s.HandOff()
}
