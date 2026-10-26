package extbridge

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gobwas/ws"
)

const (
	defaultHelloTimeout = 10 * time.Second
	defaultIdleTimeout  = 45 * time.Second
	defaultCacheLimit   = 64
)

// Config 是桥的监听参数。Addr 为空时使用 DefaultAddr。Token 必填。
type Config struct {
	Addr             string
	Token            string
	IdempotencyLimit int
	HelloTimeout     time.Duration
	IdleTimeout      time.Duration
	// Heartbeat 为 0 时每 15s 发一次协议 ping。负数表示不主动心跳，读超时仍然生效。
	Heartbeat time.Duration
	// OnEvent 收到与在途 RPC 无关的 event。可以为空。
	OnEvent func(*Session, Envelope)
	// OnChange 在连上、断开或 hello 失败后异步调用。可以为空。
	OnChange func()
	// OnHello 在会话登记成功后调用。可以为空。
	OnHello func(*Session)
}

// Server 只接受 127.0.0.1 或 ::1 上的 WebSocket。
type Server struct {
	cfg       Config
	httpSrv   *http.Server
	ln        net.Listener
	addr      string
	arrived   mailbox
	quit      chan struct{}
	closeOnce sync.Once

	mu           sync.Mutex
	closed       bool
	lastHelloErr string
	sessions     map[string]*Session
	byPlat       map[string][]*Session
}

// Session 是一个已通过 hello 的扩展连接。
type Session struct {
	id          string
	platform    string
	account     string
	accountName string
	conn        net.Conn
	reader      io.Reader
	srv         *Server
	idle        time.Duration
	onEvent     func(*Session, Envelope)

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]*waiter
	cache   *idempCache
	done    chan struct{}
	once    sync.Once
}

type waiter struct {
	accept map[string]struct{}
	fn     func(Envelope)
	fail   func(error)
}

// Listen 在回环地址上启动桥。
func Listen(cfg Config) (*Server, error) {
	if cfg.Token == "" {
		return nil, errors.New("extbridge: 必须配置 token")
	}
	addr, err := loopbackAddr(cfg.Addr)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if cfg.IdempotencyLimit <= 0 {
		cfg.IdempotencyLimit = defaultCacheLimit
	}
	if cfg.HelloTimeout <= 0 {
		cfg.HelloTimeout = defaultHelloTimeout
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = defaultIdleTimeout
	}
	if cfg.Heartbeat == 0 {
		cfg.Heartbeat = 15 * time.Second
	}
	s := &Server{
		cfg:      cfg,
		ln:       ln,
		addr:     ln.Addr().String(),
		quit:     make(chan struct{}),
		sessions: map[string]*Session{},
		byPlat:   map[string][]*Session{},
		arrived:  mailbox{notify: make(chan struct{}, 1)},
	}
	s.httpSrv = &http.Server{
		Handler:           http.HandlerFunc(s.serveHTTP),
		ReadHeaderTimeout: cfg.HelloTimeout,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() { _ = s.httpSrv.Serve(ln) }()
	return s, nil
}

// Addr 返回实际监听地址。
func (s *Server) Addr() string { return s.addr }

// Close 关闭监听和全部会话。
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		snap := make([]*Session, 0, len(s.sessions))
		for _, sess := range s.sessions {
			snap = append(snap, sess)
		}
		s.mu.Unlock()
		close(s.quit)
		for _, sess := range snap {
			sess.shutdown(io.EOF)
		}
		err = s.httpSrv.Close()
	})
	return err
}

// Sessions 返回某平台的会话副本。platform 为空时返回全部。
func (s *Server) Sessions(platform string) []*Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	if platform == "" {
		out := make([]*Session, 0, len(s.sessions))
		for _, sess := range s.sessions {
			out = append(out, sess)
		}
		return out
	}
	src := s.byPlat[platform]
	out := make([]*Session, len(src))
	copy(out, src)
	return out
}

// Session 返回该平台最近一次仍活着的 hello。已经断开的不算。
func (s *Server) Session(platform string) (*Session, error) {
	all := s.Sessions(platform)
	var live []*Session
	for _, sess := range all {
		select {
		case <-sess.Done():
		default:
			live = append(live, sess)
		}
	}
	if len(live) == 0 {
		return nil, &ProtoError{Code: ENotConnected, Message: "扩展未连接。请确认 tulai 已打开，扩展弹出页的端口和令牌与「浏览器」页一致。"}
	}
	return live[len(live)-1], nil
}

// Snapshot 给界面用：两个平台是否连着，以及最近一次 hello 失败原因。
func (s *Server) Snapshot() (qianwen, doubao bool, lastErr, addr string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	addr = s.addr
	lastErr = s.lastHelloErr
	qianwen = platformLive(s.byPlat[PlatformQianwen])
	doubao = platformLive(s.byPlat[PlatformDoubao])
	return
}

func platformLive(list []*Session) bool {
	for _, sess := range list {
		select {
		case <-sess.done:
		default:
			return true
		}
	}
	return false
}

// SessionByAccount 返回该平台这个配置文件上仍活着的最近一条连接。
func (s *Server) SessionByAccount(platform, account string) *Session {
	account = strings.TrimSpace(account)
	if s == nil || account == "" {
		return nil
	}
	var found *Session
	for _, sess := range s.Sessions(platform) {
		if sess.Account() == account && sess.Alive() {
			found = sess
		}
	}
	return found
}

// Next 等待下一次成功的 hello。
func (s *Server) Next(ctx context.Context) (*Session, error) {
	for {
		if sess, ok := s.arrived.pop(); ok {
			return sess, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.quit:
			return nil, net.ErrClosed
		case <-s.arrived.notify:
		}
	}
}

// ID 是 hello 成功后分配的会话标识。
func (s *Session) ID() string { return s.id }

// Platform 是扩展在 hello 里声明的平台。
func (s *Session) Platform() string { return s.platform }

// Account 是 hello 里可选的配置文件标识。旧扩展为空。
func (s *Session) Account() string {
	if s == nil {
		return ""
	}
	return s.account
}

// AccountName 是扩展上报的账号备注，可以为空。
func (s *Session) AccountName() string {
	if s == nil {
		return ""
	}
	return s.accountName
}

// Alive 在连接还没关掉时为真。
func (s *Session) Alive() bool {
	if s == nil {
		return false
	}
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

// Done 在连接结束时关闭。
func (s *Session) Done() <-chan struct{} { return s.done }

// Close 断开这个扩展连接。
func (s *Session) Close() { s.shutdown(io.EOF) }

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if !remoteLoopback(r.RemoteAddr) {
		http.Error(w, "loopback only", http.StatusForbidden)
		return
	}
	conn, rw, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return
	}
	go s.handshake(conn, rw.Reader)
}

func (s *Server) handshake(conn net.Conn, reader *bufio.Reader) {
	fail := func(id, code, message string) {
		s.noteHello(message)
		_ = writeEnvelope(conn, ws.StateServerSide, errorEnvelope(id, code, message))
		_ = conn.Close()
	}
	_ = conn.SetReadDeadline(time.Now().Add(s.cfg.HelloTimeout))
	payload, err := readData(reader, nil, ws.StateServerSide, MaxMessageBytes)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		return
	}
	env, err := Decode(payload)
	if err != nil {
		fail("", EProtocol, err.Error())
		return
	}
	if env.Type != TypeHello {
		fail(env.ID, EAuth, "需要先 hello。在扩展弹出页点「保存并重连」。")
		return
	}
	token, _ := helloToken(env)
	if !tokenOK(token, s.cfg.Token) {
		fail(env.ID, EAuth, fmt.Sprintf("令牌不一致（tulai 正在监听 %s）。打开「浏览器」页，把令牌和端口填进扩展弹出页，点「保存并重连」。不用重启 tulai。", s.addr))
		return
	}
	account, accountName := HelloAccount(env)
	sess := &Session{
		id:          newID(),
		platform:    env.Platform,
		account:     account,
		accountName: accountName,
		conn:        conn,
		reader:      reader,
		srv:         s,
		idle:        s.cfg.IdleTimeout,
		onEvent:     s.cfg.OnEvent,
		pending:     map[string]*waiter{},
		cache:       newIdempCache(s.cfg.IdempotencyLimit),
		done:        make(chan struct{}),
	}
	// 先读，再写出 helloAck，最后才对外发布会话。
	// 这样调用方拿到会话时，扩展已经先收到 ack，随后的 ping 不会插到 ack 前面。
	go sess.readLoop()
	ack := Envelope{
		V:         Version,
		ID:        env.ID,
		Type:      TypeAck,
		TS:        nowMS(),
		Platform:  sess.platform,
		SessionID: sess.id,
		Payload:   mustRaw(map[string]any{"name": CmdHelloAck, "ok": true}),
	}
	if err := sess.writeEnvelope(ack); err != nil {
		sess.shutdown(err)
		return
	}
	if !s.register(sess) {
		sess.shutdown(io.EOF)
		return
	}
	if s.cfg.OnHello != nil {
		s.cfg.OnHello(sess)
	}
	go sess.heartbeat()
}

func (s *Server) register(sess *Session) bool {
	s.mu.Lock()
	select {
	case <-sess.done:
		s.mu.Unlock()
		return false
	default:
	}
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.sessions[sess.id] = sess
	s.byPlat[sess.platform] = append(s.byPlat[sess.platform], sess)
	s.lastHelloErr = ""
	s.mu.Unlock()
	s.arrived.push(sess)
	s.notify()
	return true
}

func (s *Server) unregister(sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.sessions[sess.id]
	if !ok || cur != sess {
		return
	}
	delete(s.sessions, sess.id)
	list := s.byPlat[sess.platform]
	kept := list[:0]
	for _, item := range list {
		if item != sess {
			kept = append(kept, item)
		}
	}
	if len(kept) == 0 {
		delete(s.byPlat, sess.platform)
	} else {
		s.byPlat[sess.platform] = kept
	}
}

func (s *Session) readLoop() {
	err := s.readUntilClose()
	s.shutdown(err)
}

func (s *Session) readUntilClose() error {
	for {
		if s.idle > 0 {
			_ = s.conn.SetReadDeadline(time.Now().Add(s.idle))
		}
		payload, err := readData(s.reader, func(op ws.OpCode, p []byte) error {
			return s.writeOp(op, p)
		}, ws.StateServerSide, MaxMessageBytes)
		if err != nil {
			return err
		}
		s.handle(payload)
	}
}

func (s *Session) handle(payload []byte) {
	env, err := Decode(payload)
	if err != nil {
		_ = s.writeEnvelope(errorEnvelope("", EProtocol, err.Error()))
		return
	}
	if env.Type == TypeHello {
		_ = s.writeEnvelope(errorEnvelope(env.ID, EProtocol, "会话已建立"))
		return
	}
	s.deliver(env)
}

func (s *Session) deliver(env Envelope) {
	s.mu.Lock()
	w := s.pending[env.ID]
	if w != nil {
		if _, ok := w.accept[env.Type]; ok {
			delete(s.pending, env.ID)
		} else {
			w = nil
		}
	}
	s.mu.Unlock()
	if w != nil {
		w.fn(env)
		return
	}
	if env.Type == TypePing {
		_ = s.writeEnvelope(Envelope{
			V: Version, ID: env.ID, Type: TypePong, TS: nowMS(),
			Platform: s.platform, SessionID: s.id,
		})
		return
	}
	if env.Type == TypeEvent && s.onEvent != nil {
		s.onEvent(s, env)
	}
}

func (s *Session) registerWait(id string, w *waiter) {
	s.mu.Lock()
	s.pending[id] = w
	s.mu.Unlock()
}

func (s *Session) unregister(id string) {
	if id == "" {
		return
	}
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *Session) shutdown(err error) {
	pe := classifyConnErr(err)
	s.once.Do(func() {
		s.mu.Lock()
		waiters := s.pending
		s.pending = map[string]*waiter{}
		s.mu.Unlock()
		for _, w := range waiters {
			w.fail(pe)
		}
		close(s.done)
		_ = s.conn.Close()
		s.srv.unregister(s)
		s.srv.notify()
	})
}

func (s *Session) heartbeat() {
	interval := s.srv.cfg.Heartbeat
	if interval < 0 {
		return
	}
	if interval == 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), interval)
			err := s.Ping(ctx)
			cancel()
			if err == nil {
				continue
			}
			select {
			case <-s.done:
				return
			default:
			}
			s.shutdown(&ProtoError{Code: ESWSleep, Message: "扩展心跳没有回应，连接已断开。扩展会自动重连，不用重启 tulai。"})
			return
		}
	}
}

func (s *Server) noteHello(message string) {
	s.mu.Lock()
	s.lastHelloErr = message
	s.mu.Unlock()
	s.notify()
}

func (s *Server) notify() {
	s.mu.Lock()
	fn := s.cfg.OnChange
	s.mu.Unlock()
	if fn == nil {
		return
	}
	go fn()
}

func (s *Session) writeEnvelope(env Envelope) error {
	raw, err := Encode(env)
	if err != nil {
		return err
	}
	return s.writeOp(ws.OpText, raw)
}

func (s *Session) writeOp(op ws.OpCode, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	err := writeOp(s.conn, ws.StateServerSide, op, payload)
	_ = s.conn.SetWriteDeadline(time.Time{})
	return err
}

func errorEnvelope(id, code, message string) Envelope {
	if id == "" {
		id = newID()
	}
	return Envelope{
		V: Version, ID: id, Type: TypeError, TS: nowMS(),
		Payload: mustRaw(map[string]any{"code": code, "message": message}),
	}
}

func writeEnvelope(w io.Writer, side ws.State, env Envelope) error {
	raw, err := Encode(env)
	if err != nil {
		return err
	}
	return writeOp(w, side, opText, raw)
}

const opText = ws.OpText

func mustRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func tokenOK(got, want string) bool {
	g := sha256.Sum256([]byte(got))
	w := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(g[:], w[:]) == 1
}

func loopbackAddr(addr string) (string, error) {
	if addr == "" {
		addr = DefaultAddr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("extbridge 地址无效: %w", err)
	}
	if host != "127.0.0.1" && host != "::1" {
		return "", fmt.Errorf("extbridge 只允许监听 127.0.0.1 或 ::1，拒绝 %q", host)
	}
	if port == "" {
		return "", errors.New("extbridge 缺少端口")
	}
	return net.JoinHostPort(host, port), nil
}

func remoteLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func classifyConnErr(err error) *ProtoError {
	var already *ProtoError
	if errors.As(err, &already) && already != nil {
		return already
	}
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return &ProtoError{Code: ENotConnected, Message: "扩展连接已关闭"}
	}
	var ne net.Error
	if errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
		return &ProtoError{Code: ESWSleep, Message: "扩展心跳超时，服务工作者可能已休眠"}
	}
	return &ProtoError{Code: ENotConnected, Message: "扩展连接中断: " + err.Error()}
}

// mailbox 按到达顺序交出新会话，不丢弃。
type mailbox struct {
	mu     sync.Mutex
	queue  []*Session
	notify chan struct{}
}

func (m *mailbox) push(sess *Session) {
	m.mu.Lock()
	m.queue = append(m.queue, sess)
	m.mu.Unlock()
	select {
	case m.notify <- struct{}{}:
	default:
	}
}

func (m *mailbox) pop() (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queue) == 0 {
		return nil, false
	}
	sess := m.queue[0]
	m.queue = m.queue[1:]
	return sess, true
}
