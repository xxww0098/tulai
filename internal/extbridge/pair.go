package extbridge

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// PairPort 是扩展自动对接用的固定回环端口。桥本身的端口仍由配置决定，并写在响应里。
const PairPort = "18764"

// PairPath 是发现令牌的路径。
const PairPath = "/tulai-pair"

// PairInfo 是扩展启动时来取的令牌和桥端口。
type PairInfo struct {
	Token string
	Port  string
}

// PairServer 只在 127.0.0.1 上回答令牌。扩展拿不到时仍可在弹出页手动填写。
type PairServer struct {
	info func() PairInfo

	mu      sync.Mutex
	lastHit time.Time
	ln      net.Listener
	srv     *http.Server
}

// StartPair 监听固定端口。info 每次请求现读，重新生成令牌后扩展下一轮就能拿到。
func StartPair(info func() PairInfo) (*PairServer, error) {
	if info == nil {
		return nil, errors.New("extbridge: 缺少对接信息")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", PairPort))
	if err != nil {
		return nil, err
	}
	p := &PairServer{info: info, ln: ln}
	p.srv = &http.Server{
		Handler:           http.HandlerFunc(p.serveHTTP),
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

// Addr 返回实际监听地址。
func (p *PairServer) Addr() string {
	if p == nil || p.ln == nil {
		return ""
	}
	return p.ln.Addr().String()
}

// LastHit 是扩展最近一次成功取走令牌的时间。零值表示还没有取过。
func (p *PairServer) LastHit() time.Time {
	if p == nil {
		return time.Time{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastHit
}

// Close 关掉发现端口。
func (p *PairServer) Close() error {
	if p == nil || p.srv == nil {
		return nil
	}
	return p.srv.Close()
}

func (p *PairServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != PairPath {
		http.NotFound(w, r)
		return
	}
	if !remoteLoopback(r.RemoteAddr) {
		http.Error(w, "loopback only", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	info := p.info()
	token := NormalizeExtToken(info.Token)
	port := NormalizeExtPort(info.Port)
	if WeakToken(token) {
		http.Error(w, "token", http.StatusServiceUnavailable)
		return
	}
	p.mu.Lock()
	p.lastHit = time.Now()
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{
		"product": "tulai",
		"token":   token,
		"port":    port,
	})
}
