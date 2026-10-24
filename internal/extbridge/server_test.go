package extbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

func TestListenRejectsNonLoopbackAndEmptyToken(t *testing.T) {
	if _, err := Listen(Config{Addr: "0.0.0.0:18765", Token: "t"}); err == nil {
		t.Fatal("accepted 0.0.0.0")
	}
	if _, err := Listen(Config{Addr: "localhost:18765", Token: "t"}); err == nil {
		t.Fatal("accepted localhost name")
	}
	if _, err := Listen(Config{Addr: "127.0.0.1:0"}); err == nil {
		t.Fatal("accepted empty token")
	}
}

func TestHelloPingPongAndStubAck(t *testing.T) {
	srv := startServer(t, Config{})
	if srv.Addr() == "" || !hasLoopback(srv.Addr()) {
		t.Fatal(srv.Addr())
	}
	p := dialPeer(t, srv.Addr())
	ack := p.hello(t, "test-token", PlatformQianwen)
	if ack.Type != TypeAck || ack.SessionID == "" {
		t.Fatalf("helloAck: %+v", ack)
	}
	name, _ := ack.Name()
	if name != CmdHelloAck {
		t.Fatalf("ack name: %s", name)
	}
	sess := mustSession(t, srv, PlatformQianwen)
	if sess.ID() != ack.SessionID || sess.Platform() != PlatformQianwen {
		t.Fatalf("registry: %s %s", sess.ID(), sess.Platform())
	}

	p.writeEnv(t, Envelope{V: Version, ID: "ping-from-ext", Type: TypePing, TS: nowMS(), Platform: PlatformQianwen})
	pong := p.readEnv(t)
	if pong.Type != TypePong || pong.ID != "ping-from-ext" {
		t.Fatalf("pong: %+v", pong)
	}
	_ = wsutil.WriteClientMessage(p.conn, ws.OpPing, []byte("hi"))
	_ = p.conn.SetReadDeadline(time.Now().Add(time.Second))
	header, err := ws.ReadHeader(p.reader())
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, header.Length)
	if _, err := io.ReadFull(p.reader(), body); err != nil {
		t.Fatal(err)
	}
	if header.Masked {
		ws.Cipher(body, header.Mask, 0)
	}
	if header.OpCode != ws.OpPong || string(body) != "hi" {
		t.Fatalf("ws pong: op=%v body=%q", header.OpCode, body)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	stop := p.serve(func(env Envelope) error {
		if env.Type == TypePing {
			return p.writeEnv(nil, Envelope{V: Version, ID: env.ID, Type: TypePong, TS: nowMS()})
		}
		if env.Type == TypeCmd {
			return p.writeEnv(nil, ackFor(env, false, ESelectorMiss, "stub"))
		}
		return nil
	})
	defer stop()
	if err := sess.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	cmd, err := Command(CmdUpload, map[string]any{"paths": []string{"a.png"}})
	if err != nil {
		t.Fatal(err)
	}
	cmd.TaskID = "task-1"
	got, err := sess.Call(ctx, cmd)
	if CodeOf(err) != ESelectorMiss {
		t.Fatalf("stub: %v payload=%s", err, got.Payload)
	}
	if got.Type != TypeAck || got.TaskID != "task-1" || got.ID == "" {
		t.Fatalf("stub envelope: %+v", got)
	}
}

func TestAuthAndProtocolReject(t *testing.T) {
	srv := startServer(t, Config{})
	p := dialPeer(t, srv.Addr())
	p.writeEnv(t, Envelope{V: Version, ID: "early", Type: TypePing, TS: 1})
	env, err := p.readEnvErr()
	if err != nil || env.Type != TypeError || CodeOf(terminalError(env)) != EAuth {
		t.Fatalf("early message: %+v %v", env, err)
	}
	if _, err := p.readEnvErr(); !closedPeer(err) {
		t.Fatalf("early message left the connection open: %v", err)
	}

	p = dialPeer(t, srv.Addr())
	raw, _ := json.Marshal(map[string]string{"token": "nope"})
	p.writeEnv(t, Envelope{V: Version, ID: "bad", Type: TypeHello, TS: 1, Platform: PlatformQianwen, Payload: raw})
	env, err = p.readEnvErr()
	if err != nil || CodeOf(terminalError(env)) != EAuth {
		t.Fatalf("bad token: %+v %v", env, err)
	}
	if _, err := p.readEnvErr(); !closedPeer(err) {
		t.Fatalf("connection stayed open after E_AUTH: %v", err)
	}
	if sessions := srv.Sessions(PlatformQianwen); len(sessions) != 0 {
		t.Fatalf("rejected hello was registered: %d", len(sessions))
	}

	p = dialPeer(t, srv.Addr())
	p.hello(t, "test-token", PlatformQianwen)
	if err := writeOp(p.conn, ws.StateClientSide, ws.OpText, []byte("{")); err != nil {
		t.Fatal(err)
	}
	env, err = p.readEnvErr()
	if err != nil || CodeOf(terminalError(env)) != EProtocol {
		t.Fatalf("bad json: %+v %v", env, err)
	}
	p.writeEnv(t, Envelope{V: Version, ID: "still-up", Type: TypePing, TS: 1})
	pong, err := p.readEnvErr()
	if err != nil || pong.Type != TypePong {
		t.Fatalf("session died after protocol error: %+v %v", pong, err)
	}
}

func TestCallTimeoutDoesNotCacheIdempotency(t *testing.T) {
	srv := startServer(t, Config{})
	p := dialPeer(t, srv.Addr())
	p.hello(t, "test-token", PlatformQianwen)
	sess := mustSession(t, srv, PlatformQianwen)
	cmds := make(chan Envelope, 4)
	stop := p.serve(func(env Envelope) error {
		if env.Type == TypeCmd {
			cmds <- env
		}
		return nil
	})
	defer stop()
	cmd, err := Command(CmdSend, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd.IdempotencyKey = "send-1"
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		_, err = sess.Call(ctx, cmd)
		cancel()
		if CodeOf(err) != ETimeout {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-cmds:
		case <-time.After(time.Second):
			t.Fatalf("timeout cached or dropped send %d", i)
		}
	}
}

func TestIdempotencyDedupesInFlight(t *testing.T) {
	srv := startServer(t, Config{IdempotencyLimit: 2})
	p := dialPeer(t, srv.Addr())
	p.hello(t, "test-token", PlatformQianwen)
	sess := mustSession(t, srv, PlatformQianwen)
	cmds := make(chan Envelope, 8)
	stop := p.serve(func(env Envelope) error {
		if env.Type != TypeCmd {
			return nil
		}
		cmds <- env
		return nil
	})
	defer stop()

	cmd, err := Command(CmdDownload, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd.IdempotencyKey = "dl"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type callResult struct {
		env Envelope
		err error
	}
	first := make(chan callResult, 1)
	second := make(chan callResult, 1)
	go func() {
		env, err := sess.Call(ctx, cmd)
		first <- callResult{env, err}
	}()
	got := <-cmds
	go func() {
		env, err := sess.Call(ctx, cmd)
		second <- callResult{env, err}
	}()
	select {
	case extra := <-cmds:
		t.Fatalf("in-flight duplicate was sent: %s", extra.ID)
	case <-time.After(150 * time.Millisecond):
	}
	if err := p.writeEnv(nil, ackFor(got, false, ESelectorMiss, "stub")); err != nil {
		t.Fatal(err)
	}
	a := <-first
	b := <-second
	if CodeOf(a.err) != ESelectorMiss || CodeOf(b.err) != ESelectorMiss || a.env.ID != got.ID || b.env.ID != got.ID {
		t.Fatalf("duplicate results: %+v %+v", a, b)
	}
	thirdCtx, thirdCancel := context.WithTimeout(context.Background(), time.Second)
	defer thirdCancel()
	third, err := sess.Call(thirdCtx, cmd)
	if CodeOf(err) != ESelectorMiss || third.ID != got.ID {
		t.Fatalf("cache miss: %+v %v", third, err)
	}
	time.Sleep(50 * time.Millisecond)
	select {
	case extra := <-cmds:
		t.Fatalf("cached key was sent again: %s", extra.ID)
	default:
	}
}

func TestIdempotencyEvictsOldest(t *testing.T) {
	srv := startServer(t, Config{IdempotencyLimit: 2})
	p := dialPeer(t, srv.Addr())
	p.hello(t, "test-token", PlatformQianwen)
	sess := mustSession(t, srv, PlatformQianwen)
	var n atomic.Int32
	stop := p.serve(func(env Envelope) error {
		if env.Type != TypeCmd {
			return nil
		}
		n.Add(1)
		return p.writeEnv(nil, ackFor(env, true, "", ""))
	})
	defer stop()
	for _, key := range []string{"a", "b", "b", "c", "a"} {
		callCtx, callCancel := context.WithTimeout(context.Background(), time.Second)
		env, err := Command(CmdStatus, nil)
		if err != nil {
			callCancel()
			t.Fatal(err)
		}
		env.IdempotencyKey = key
		if _, err := sess.Call(callCtx, env); err != nil {
			callCancel()
			t.Fatalf("key %s: %v", key, err)
		}
		callCancel()
	}
	if n.Load() != 4 {
		t.Fatalf("eviction sends=%d, want 4", n.Load())
	}
}

func TestEventCompletesCallAndCloseWakesWaiter(t *testing.T) {
	srv := startServer(t, Config{})
	p := dialPeer(t, srv.Addr())
	p.hello(t, "test-token", PlatformQianwen)
	sess := mustSession(t, srv, PlatformQianwen)
	var once sync.Once
	entered := make(chan struct{})
	stop := p.serve(func(env Envelope) error {
		if env.Type != TypeCmd {
			return nil
		}
		raw, _ := json.Marshal(map[string]any{"name": CmdWaitResult, "stage": "stub"})
		if err := p.writeEnv(nil, Envelope{
			V: Version, ID: env.ID, Type: TypeEvent, TS: nowMS(),
			Platform: env.Platform, SessionID: env.SessionID, Payload: raw,
		}); err != nil {
			return err
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd, _ := Command(CmdWaitResult, nil)
	got, err := sess.Call(ctx, cmd)
	stop()
	if err != nil || got.Type != TypeEvent {
		t.Fatalf("event: %+v %v", got, err)
	}

	p = dialPeer(t, srv.Addr())
	p.hello(t, "test-token", PlatformDoubao)
	doubao := mustSession(t, srv, PlatformDoubao)
	stop = p.serve(func(env Envelope) error {
		if env.Type == TypeCmd {
			once.Do(func() { close(entered) })
			_ = p.conn.Close()
		}
		return io.EOF
	})
	defer stop()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	upload, _ := Command(CmdUpload, nil)
	_, err = doubao.Call(ctx, upload)
	<-entered
	if CodeOf(err) != ENotConnected {
		t.Fatalf("close: %v", err)
	}
}

func TestIdleTimeoutReportsServiceWorkerSleep(t *testing.T) {
	srv := startServer(t, Config{IdleTimeout: 400 * time.Millisecond})
	p := dialPeer(t, srv.Addr())
	p.hello(t, "test-token", PlatformQianwen)
	sess := mustSession(t, srv, PlatformQianwen)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd, _ := Command(CmdStatus, nil)
	_, err := sess.Call(ctx, cmd)
	if CodeOf(err) != ESWSleep {
		t.Fatalf("idle: %v", err)
	}
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("session stayed up after idle timeout")
	}
}

func TestUnmaskedClientFrameDropsSession(t *testing.T) {
	srv := startServer(t, Config{})
	p := dialPeer(t, srv.Addr())
	p.hello(t, "test-token", PlatformQianwen)
	sess := mustSession(t, srv, PlatformQianwen)
	if err := wsutil.WriteServerMessage(p.conn, ws.OpText, []byte(`{"v":1}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sess.Done():
	case <-time.After(time.Second):
		t.Fatal("unmasked frame kept the session")
	}
}

func startServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	if cfg.Token == "" {
		cfg.Token = "test-token"
	}
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:0"
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = time.Minute
	}
	if cfg.HelloTimeout == 0 {
		cfg.HelloTimeout = 2 * time.Second
	}
	srv, err := Listen(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func mustSession(t *testing.T, srv *Server, platform string) *Session {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		sess, err := srv.Session(platform)
		if err == nil {
			return sess
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no %s session", platform)
	return nil
}

func hasLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type peer struct {
	conn    net.Conn
	br      *bufio.Reader
	writeMu sync.Mutex
}

func dialPeer(t *testing.T, addr string) *peer {
	t.Helper()
	var last error
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		conn, br, _, err := ws.Dial(ctx, "ws://"+addr+"/")
		cancel()
		if err == nil {
			p := &peer{conn: conn, br: br}
			t.Cleanup(func() { conn.Close() })
			return p
		}
		last = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(last)
	return nil
}

func (p *peer) hello(t *testing.T, token, platform string) Envelope {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"token": token, "version": "0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	p.writeEnv(t, Envelope{V: Version, ID: newID(), Type: TypeHello, TS: nowMS(), Platform: platform, Payload: raw})
	return p.readEnv(t)
}

func (p *peer) writeEnv(t *testing.T, env Envelope) error {
	if t != nil {
		t.Helper()
	}
	err := p.writeEnvErr(env)
	if err != nil && t != nil {
		t.Fatal(err)
	}
	return err
}

func closedPeer(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)
}

func (p *peer) writeEnvErr(env Envelope) error {
	raw, err := Encode(env)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return writeOp(p.conn, ws.StateClientSide, ws.OpText, raw)
}

func (p *peer) readEnv(t *testing.T) Envelope {
	t.Helper()
	env, err := p.readEnvErr()
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func (p *peer) readEnvErr() (Envelope, error) {
	_ = p.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	payload, err := readData(p.reader(), func(op ws.OpCode, body []byte) error {
		p.writeMu.Lock()
		defer p.writeMu.Unlock()
		return writeOp(p.conn, ws.StateClientSide, op, body)
	}, ws.StateClientSide, MaxMessageBytes)
	if err != nil {
		return Envelope{}, err
	}
	return Decode(payload)
}

func (p *peer) reader() io.Reader {
	if p.br != nil {
		return p.br
	}
	return p.conn
}

func (p *peer) serve(fn func(Envelope) error) func() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			env, err := p.readEnvErr()
			if err != nil {
				return
			}
			if err := fn(env); err != nil {
				return
			}
		}
	}()
	return func() {
		_ = p.conn.Close()
		<-done
	}
}

func ackFor(req Envelope, ok bool, code, message string) Envelope {
	name, _ := req.Name()
	body := map[string]any{"name": name, "ok": ok}
	if code != "" {
		body["code"] = code
	}
	if message != "" {
		body["message"] = message
	}
	raw, _ := json.Marshal(body)
	return Envelope{
		V: Version, ID: req.ID, Type: TypeAck, TS: nowMS(),
		Platform: req.Platform, SessionID: req.SessionID,
		TaskID: req.TaskID, IdempotencyKey: req.IdempotencyKey,
		Payload: raw,
	}
}
