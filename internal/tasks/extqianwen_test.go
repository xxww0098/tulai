package tasks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"

	"xbs/internal/extbridge"
	"xbs/internal/store"
)

func TestUseExtensionOnlyForQianwen(t *testing.T) {
	st := openTaskStore(t)
	e := NewEngine(st, nil, nil, t.TempDir())
	if e.useExtension("千问") || e.useExtension("豆包") {
		t.Fatal("default backend must stay on ego")
	}
	st.Set(extbridge.SettingBrowserBackend, "extension")
	if !e.useExtension("千问") {
		t.Fatal("qianwen should use the extension")
	}
	if e.useExtension("豆包") {
		t.Fatal("doubao stays on ego/chrome")
	}
	st.Set(extbridge.SettingBrowserBackend, "ego")
	if e.useExtension("千问") {
		t.Fatal("ego must not select the extension")
	}
}

func TestExtensionQianwenHappyPathRetriesWaitWithoutResend(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writeSolidPNG(t, src, 2, 2, color.NRGBA{R: 200, A: 255})
	resultPNG := solidPNG(t, 3, 3, color.NRGBA{G: 180, A: 255})

	var mu sync.Mutex
	var uploads, sends, waits, downloads int
	addr := "127.0.0.1:" + port
	go serveFakeQianwen(addr, "dev-token", func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdUpload:
			mu.Lock()
			uploads++
			mu.Unlock()
			return ackOK(env, map[string]any{"ok": true})
		case extbridge.CmdSetPrompt, extbridge.CmdCaptchaAssist:
			extra := map[string]any{"ok": true}
			if name == extbridge.CmdCaptchaAssist {
				extra["captchaBlocked"] = false
			}
			return ackOK(env, extra)
		case extbridge.CmdSend:
			mu.Lock()
			sends++
			mu.Unlock()
			return ackOK(env, map[string]any{"ok": true})
		case extbridge.CmdWaitResult:
			mu.Lock()
			waits++
			n := waits
			mu.Unlock()
			if n == 1 {
				return ackFail(env, extbridge.ETimeout, "等待生成结果超时")
			}
			return ackOK(env, map[string]any{"ok": true, "images": []map[string]string{{"url": "https://cdn.example/out.png"}}})
		case extbridge.CmdDownload:
			mu.Lock()
			downloads++
			mu.Unlock()
			return chunkAck(env, resultPNG, 8)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})

	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	var last Task
	err := e.Run(context.Background(), Options{
		Platforms:         []string{"千问"},
		Prompts:           []string{"水彩风格"},
		Images:            []string{src},
		Concurrency:       1,
		RetryCount:        1,
		WaitResult:        true,
		WaitResultSeconds: 30,
		OutputPath:        filepath.Join(dir, "out"),
		NamingRule:        "原文件名",
	}, func(task Task) { last = task }, func(Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	if last.Status != StatusSuccess {
		t.Fatalf("status=%s err=%s", last.Status, last.Error)
	}
	if len(last.Results) != 1 {
		t.Fatalf("saved=%v", last.Results)
	}
	mu.Lock()
	defer mu.Unlock()
	if uploads != 1 || sends != 1 || waits != 2 || downloads < 2 {
		t.Fatalf("uploads=%d sends=%d waits=%d downloads=%d", uploads, sends, waits, downloads)
	}
}

func TestExtensionSendUncertainDoesNotRetrySend(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writeSolidPNG(t, src, 2, 2, color.NRGBA{B: 200, A: 255})

	var mu sync.Mutex
	sends := 0
	go serveFakeQianwen("127.0.0.1:"+port, "dev-token", func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdSend:
			mu.Lock()
			sends++
			mu.Unlock()
			return ackFail(env, extbridge.ESendUncertain, "平台未接受消息")
		case extbridge.CmdCaptchaAssist, extbridge.CmdUpload, extbridge.CmdSetPrompt:
			return ackOK(env, map[string]any{"ok": true, "captchaBlocked": false})
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})

	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	var last Task
	_ = e.Run(context.Background(), Options{
		Platforms:         []string{"千问"},
		Prompts:           []string{"水彩风格"},
		Images:            []string{src},
		Concurrency:       1,
		RetryCount:        2,
		WaitResult:        true,
		WaitResultSeconds: 5,
		OutputPath:        dir,
	}, func(task Task) { last = task }, func(Progress) {})
	if last.Status != StatusFailed {
		t.Fatalf("status=%s err=%s", last.Status, last.Error)
	}
	if !strings.Contains(last.Error, "发送状态未确认") {
		t.Fatalf("error=%s", last.Error)
	}
	mu.Lock()
	defer mu.Unlock()
	if sends != 1 {
		t.Fatalf("send calls=%d", sends)
	}
}

func TestExtensionUploadSplitsChunks(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	dir := t.TempDir()
	src := filepath.Join(dir, "big.png")
	raw := make([]byte, extChunkRaw+8)
	raw[0] = 1
	if err := os.WriteFile(src, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	uploads := 0
	var sawFinish bool
	go serveFakeQianwen("127.0.0.1:"+port, "dev-token", func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		if name == extbridge.CmdUpload {
			var body struct {
				Finish bool `json:"finish"`
				Offset int  `json:"offset"`
			}
			_ = json.Unmarshal(env.Payload, &body)
			mu.Lock()
			uploads++
			if body.Finish {
				sawFinish = body.Offset == extChunkRaw
			}
			mu.Unlock()
			return ackOK(env, map[string]any{"ok": true})
		}
		if name == extbridge.CmdSetPrompt {
			return ackFail(env, extbridge.ESelectorMiss, "停在填词")
		}
		return ackOK(env, map[string]any{"ok": true, "captchaBlocked": false})
	})
	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	var last Task
	_ = e.Run(context.Background(), Options{
		Platforms:   []string{"千问"},
		Prompts:     []string{"x"},
		Images:      []string{src},
		Concurrency: 1,
		OutputPath:  dir,
	}, func(task Task) { last = task }, func(Progress) {})
	if last.Status != StatusFailed {
		t.Fatalf("status=%s err=%s", last.Status, last.Error)
	}
	mu.Lock()
	defer mu.Unlock()
	if uploads != 2 || !sawFinish {
		t.Fatalf("uploads=%d finish=%v", uploads, sawFinish)
	}
}

func TestRunQianwenDownloadReassembly(t *testing.T) {
	pngBytes := solidPNG(t, 2, 2, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	srv, err := extbridge.Listen(extbridge.Config{Addr: "127.0.0.1:0", Token: "dev-token"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go serveFakeQianwen(srv.Addr(), "dev-token", func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdCaptchaAssist, extbridge.CmdSetPrompt, extbridge.CmdSend:
			return ackOK(env, map[string]any{"ok": true})
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"ok": true, "images": []map[string]string{{"url": "https://cdn.example/a.png"}}})
		case extbridge.CmdDownload:
			return chunkAck(env, pngBytes, 8)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := srv.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunQianwenExtension(ctx, sess, QianwenJob{
		TaskID:            "1",
		IdempotencyPrefix: "batch",
		Prompt:            "水彩",
		Wait:              time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Submitted || len(out.Results) != 1 || string(out.Results[0].Data) != string(pngBytes) {
		t.Fatalf("submitted=%v bytes=%d", out.Submitted, len(out.Results))
	}
	if out.Results[0].Src != "extension" {
		t.Fatal(out.Results[0].Src)
	}
}

func openTaskStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return strconv.Itoa(port)
}

func writeSolidPNG(t *testing.T, path string, w, h int, c color.NRGBA) {
	t.Helper()
	if err := os.WriteFile(path, solidPNG(t, w, h, c), 0o644); err != nil {
		t.Fatal(err)
	}
}

func solidPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func ackOK(req extbridge.Envelope, fields map[string]any) extbridge.Envelope {
	name, _ := req.Name()
	if fields == nil {
		fields = map[string]any{}
	}
	fields["name"] = name
	if _, ok := fields["ok"]; !ok {
		fields["ok"] = true
	}
	raw, _ := json.Marshal(fields)
	return extbridge.Envelope{
		V: extbridge.Version, ID: req.ID, Type: extbridge.TypeAck, TS: time.Now().UnixMilli(),
		Platform: req.Platform, SessionID: req.SessionID, TaskID: req.TaskID, IdempotencyKey: req.IdempotencyKey,
		Payload: raw,
	}
}

func ackFail(req extbridge.Envelope, code, message string) extbridge.Envelope {
	return ackOK(req, map[string]any{"ok": false, "code": code, "message": message})
}

func chunkAck(req extbridge.Envelope, data []byte, n int) extbridge.Envelope {
	var body struct {
		Offset int `json:"offset"`
	}
	_ = json.Unmarshal(req.Payload, &body)
	if n <= 0 {
		n = len(data)
	}
	end := body.Offset + n
	if end > len(data) {
		end = len(data)
	}
	if body.Offset > len(data) {
		body.Offset = len(data)
	}
	part := data[body.Offset:end]
	return ackOK(req, map[string]any{
		"ok":         true,
		"dataBase64": base64.StdEncoding.EncodeToString(part),
		"offset":     body.Offset,
		"total":      len(data),
		"done":       end >= len(data),
	})
}

func serveFakeQianwen(addr, token string, fn func(extbridge.Envelope) extbridge.Envelope) {
	var conn net.Conn
	var br *bufio.Reader
	var err error
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		conn, br, _, err = ws.Dial(ctx, "ws://"+addr+"/")
		cancel()
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		return
	}
	defer conn.Close()
	raw, _ := json.Marshal(map[string]string{"token": token, "version": "0.2.0"})
	hello := extbridge.Envelope{V: extbridge.Version, ID: "hello-test", Type: extbridge.TypeHello, TS: time.Now().UnixMilli(), Platform: extbridge.PlatformQianwen, Payload: raw}
	reader := io.Reader(conn)
	if br != nil {
		reader = br
	}
	if err := writeClient(conn, hello); err != nil {
		return
	}
	if _, err := readServer(conn, reader); err != nil {
		return
	}
	for {
		env, err := readServer(conn, reader)
		if err != nil {
			return
		}
		if env.Type == extbridge.TypePing {
			_ = writeClient(conn, extbridge.Envelope{V: extbridge.Version, ID: env.ID, Type: extbridge.TypePong, TS: time.Now().UnixMilli()})
			continue
		}
		if env.Type != extbridge.TypeCmd {
			continue
		}
		if err := writeClient(conn, fn(env)); err != nil {
			return
		}
	}
}

func writeClient(conn net.Conn, env extbridge.Envelope) error {
	raw, err := extbridge.Encode(env)
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return wsutil.WriteClientMessage(conn, ws.OpText, raw)
}

func readServer(conn net.Conn, reader io.Reader) (extbridge.Envelope, error) {
	if reader == nil {
		reader = conn
	}
	for {
		_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
		header, err := ws.ReadHeader(reader)
		if err != nil {
			return extbridge.Envelope{}, err
		}
		payload := make([]byte, header.Length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return extbridge.Envelope{}, err
		}
		if header.Masked {
			ws.Cipher(payload, header.Mask, 0)
		}
		switch header.OpCode {
		case ws.OpPing:
			_ = wsutil.WriteClientMessage(conn, ws.OpPong, payload)
		case ws.OpPong, ws.OpContinuation:
		case ws.OpClose:
			return extbridge.Envelope{}, io.EOF
		case ws.OpText, ws.OpBinary:
			return extbridge.Decode(payload)
		}
	}
}
