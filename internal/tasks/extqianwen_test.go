package tasks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"math/rand/v2"
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

	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestReloadExtensionBridgeUsesSavedToken(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingExtPort, port)
	st.Set(extbridge.SettingExtToken, "token-a")
	e := NewEngine(st, nil, nil, t.TempDir())
	t.Cleanup(func() { e.CloseSessions() })
	if err := e.ReloadExtensionBridge(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.extensionServer(); err != nil {
		t.Fatal(err)
	}
	if ack := helloBridge(t, "127.0.0.1:"+port, "token-a"); ack.Type != extbridge.TypeAck {
		t.Fatalf("first hello: %+v", ack)
	}
	st.Set(extbridge.SettingExtToken, "token-b")
	if err := e.ReloadExtensionBridge(); err != nil {
		t.Fatal(err)
	}
	if ack := helloBridge(t, "127.0.0.1:"+port, "token-a"); ack.Type != extbridge.TypeError {
		t.Fatalf("old token still accepted: %+v", ack)
	}
	if ack := helloBridge(t, "127.0.0.1:"+port, "token-b"); ack.Type != extbridge.TypeAck {
		t.Fatalf("new token: %+v", ack)
	}
}

func helloBridge(t *testing.T, addr, token string) extbridge.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, br, _, err := ws.Dial(ctx, "ws://"+addr+"/")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	raw, _ := json.Marshal(map[string]string{"token": token, "version": "0.3.6"})
	hello := extbridge.Envelope{V: extbridge.Version, ID: "hello-reload", Type: extbridge.TypeHello, TS: time.Now().UnixMilli(), Platform: extbridge.PlatformQianwen, Payload: raw}
	if err := writeClient(conn, hello); err != nil {
		t.Fatal(err)
	}
	reader := io.Reader(conn)
	if br != nil {
		reader = br
	}
	ack, err := readServer(conn, reader)
	if err != nil {
		t.Fatal(err)
	}
	return ack
}

func TestUseExtensionForQianwenAndDoubao(t *testing.T) {
	st := openTaskStore(t)
	e := NewEngine(st, nil, nil, t.TempDir())
	if !e.useBoundExtension("千问") || !e.useBoundExtension("豆包") {
		t.Fatal("default backend must use the bound Firefox bridge")
	}
	st.Set(extbridge.SettingBrowserBackend, "extension")
	if !e.useExtension("千问") || !e.useExtension("豆包") {
		t.Fatal("qianwen and doubao should use the extension")
	}
	st.Set(extbridge.SettingBrowserBackend, "ego")
	if e.useExtension("千问") || e.useExtension("豆包") {
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

func TestExtensionTextOnlySkipsUpload(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	var mu sync.Mutex
	uploads, sends := 0, 0
	go serveFakeQianwen("127.0.0.1:"+port, "dev-token", func(env extbridge.Envelope) extbridge.Envelope {
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
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	e := NewEngine(st, nil, nil, t.TempDir())
	defer e.CloseSessions()
	err := e.Run(context.Background(), Options{
		Platforms:   []string{"千问"},
		Prompts:     []string{"画一只猫", "画一条鱼"},
		Concurrency: 1,
		OutputPath:  t.TempDir(),
	}, func(Task) {}, func(Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	snap := e.View().Snapshot()
	if len(snap) != 2 || snap[0].Prompt != "画一只猫" || snap[1].Prompt != "画一条鱼" {
		t.Fatalf("队列: %+v", snap)
	}
	for _, task := range snap {
		if len(task.Images) != 0 || task.Status != StatusSuccess {
			t.Fatalf("任务: %+v", task)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if uploads != 0 || sends != 2 {
		t.Fatalf("uploads=%d sends=%d", uploads, sends)
	}
}

func TestQianwenCaptchaDoesNotDragOrRetrySend(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writeSolidPNG(t, src, 2, 2, color.NRGBA{R: 20, A: 255})
	var mu sync.Mutex
	sends := 0
	go serveFakeQianwen("127.0.0.1:"+port, "dev-token", func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdSend:
			mu.Lock()
			sends++
			mu.Unlock()
			return ackOK(env, map[string]any{
				"ok": false, "code": extbridge.ECaptchaHuman, "message": "Please drag the slider to verify",
				"captchaBlocked": true, "kind": "slider", "baxia": true,
			})
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
		RetryCount:        1,
		WaitResult:        true,
		WaitResultSeconds: 30,
		OutputPath:        filepath.Join(dir, "out"),
	}, func(task Task) { last = task }, func(Progress) {})
	if last.Status != StatusNeedsUser {
		t.Fatalf("status=%s err=%s", last.Status, last.Error)
	}
	mu.Lock()
	defer mu.Unlock()
	if sends != 1 {
		t.Fatalf("send calls=%d", sends)
	}
}

func TestDoubaoCaptchaDoesNotUseQianwenDrag(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writeSolidPNG(t, src, 2, 2, color.NRGBA{B: 20, A: 255})
	go serveFakeExt("127.0.0.1:"+port, "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{
				"ok": false, "code": extbridge.ECaptchaHuman, "message": "滑块",
				"captchaBlocked": true, "kind": "slider",
			})
		case extbridge.CmdCaptchaAssist, extbridge.CmdUpload, extbridge.CmdSetPrompt, extbridge.CmdSend:
			return ackOK(env, map[string]any{"ok": true, "captchaBlocked": false})
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	var last Task
	_ = e.Run(context.Background(), Options{
		Platforms:         []string{"豆包"},
		Prompts:           []string{"水彩风格"},
		Images:            []string{src},
		Concurrency:       1,
		RetryCount:        1,
		WaitResult:        true,
		WaitResultSeconds: 5,
		OutputPath:        dir,
	}, func(task Task) { last = task }, func(Progress) {})
	if last.Status != StatusNeedsUser {
		t.Fatalf("status=%s err=%s", last.Status, last.Error)
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

func TestExtensionDoubaoHappyPath(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writeSolidPNG(t, src, 2, 2, color.NRGBA{R: 20, G: 40, A: 255})
	resultPNG := solidPNG(t, 2, 2, color.NRGBA{B: 90, A: 255})

	var mu sync.Mutex
	var uploads, sends, waits, downloads int
	var sawPlatform string
	go serveFakeExt("127.0.0.1:"+port, "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		mu.Lock()
		sawPlatform = env.Platform
		mu.Unlock()
		name, _ := env.Name()
		switch name {
		case extbridge.CmdUpload:
			mu.Lock()
			uploads++
			mu.Unlock()
			return ackOK(env, map[string]any{"ok": true})
		case extbridge.CmdSetPrompt, extbridge.CmdCaptchaAssist:
			extra := map[string]any{"ok": true, "captchaBlocked": false}
			return ackOK(env, extra)
		case extbridge.CmdSend:
			mu.Lock()
			sends++
			mu.Unlock()
			return ackOK(env, map[string]any{"ok": true})
		case extbridge.CmdWaitResult:
			mu.Lock()
			waits++
			mu.Unlock()
			return ackOK(env, map[string]any{"ok": true, "images": []map[string]string{{"url": "https://cdn.example/doubao.png"}}})
		case extbridge.CmdDownload:
			mu.Lock()
			downloads++
			mu.Unlock()
			return chunkAck(env, resultPNG, len(resultPNG))
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})

	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	var last Task
	err := e.Run(context.Background(), Options{
		Platforms:         []string{"豆包"},
		Prompts:           []string{"水彩风格"},
		Images:            []string{src},
		Concurrency:       1,
		RetryCount:        1,
		WaitResult:        true,
		WaitResultSeconds: 30,
		OutputPath:        filepath.Join(dir, "out"),
	}, func(task Task) { last = task }, func(Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	if last.Status != StatusSuccess || len(last.Results) != 1 {
		t.Fatalf("status=%s err=%s saved=%v", last.Status, last.Error, last.Results)
	}
	mu.Lock()
	defer mu.Unlock()
	if sawPlatform != extbridge.PlatformDoubao || uploads != 1 || sends != 1 || waits != 1 || downloads != 1 {
		t.Fatalf("platform=%s uploads=%d sends=%d waits=%d downloads=%d", sawPlatform, uploads, sends, waits, downloads)
	}
}

func TestExtensionDoubaoNativeRatio(t *testing.T) {
	for _, tc := range []struct {
		name    string
		applied bool
		broken  bool
	}{
		{"applied", true, false},
		{"not applied", false, false},
		{"old extension", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := openTaskStore(t)
			port := freePort(t)
			st.Set(extbridge.SettingBrowserBackend, "extension")
			st.Set("extBridgePort", port)
			st.Set("extBridgeToken", "dev-token")
			dir := t.TempDir()
			data := outputMarkerPNG(t, color.NRGBA{R: 220, A: 255})

			var mu sync.Mutex
			ratios := []string{}
			prompts := []string{}
			go serveFakeExt("127.0.0.1:"+port, "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
				name, _ := env.Name()
				switch name {
				case extbridge.CmdSetRatio:
					var payload struct {
						Ratio string `json:"ratio"`
					}
					_ = json.Unmarshal(env.Payload, &payload)
					mu.Lock()
					ratios = append(ratios, payload.Ratio)
					mu.Unlock()
					if tc.broken {
						return ackFail(env, extbridge.EProtocol, "unknown command")
					}
					return ackOK(env, map[string]any{"ok": true, "applied": tc.applied})
				case extbridge.CmdSetPrompt:
					var payload struct {
						Prompt string `json:"prompt"`
					}
					_ = json.Unmarshal(env.Payload, &payload)
					mu.Lock()
					prompts = append(prompts, payload.Prompt)
					mu.Unlock()
					return ackOK(env, nil)
				case extbridge.CmdCaptchaAssist, extbridge.CmdSend:
					return ackOK(env, nil)
				case extbridge.CmdWaitResult:
					return ackOK(env, map[string]any{"images": []map[string]string{{"url": "https://cdn.example/native.png"}}})
				case extbridge.CmdDownload:
					return chunkAck(env, data, len(data))
				default:
					return ackFail(env, extbridge.EProtocol, name)
				}
			})

			e := NewEngine(st, nil, nil, dir)
			defer e.CloseSessions()
			var last Task
			err := e.Run(context.Background(), Options{
				Platforms: []string{"豆包"}, Prompts: []string{"商品图"}, OutputPath: filepath.Join(dir, "out"),
				WaitResult: true, WaitResultSeconds: 30, Concurrency: 1,
				OutputSize: OutputSize{Ratio: "4:3"},
			}, func(task Task) { last = task }, func(Progress) {})
			if err != nil || last.Status != StatusSuccess || len(last.Results) != 1 {
				t.Fatalf("task=%+v err=%v", last, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(ratios) != 1 || ratios[0] != "4:3" {
				t.Fatalf("setRatio calls: %v", ratios)
			}
			if len(prompts) == 0 {
				t.Fatal("bridge did not receive a prompt")
			}
			if tc.applied {
				if prompts[0] != "商品图" {
					t.Fatalf("native ratio applied, prompt must stay clean: %q", prompts[0])
				}
			} else if !strings.Contains(prompts[0], "4:3") || !strings.Contains(prompts[0], "商品图") {
				t.Fatalf("native ratio missing, prompt must carry ratio line: %q", prompts[0])
			}
		})
	}
}

func TestExtensionWashSavesResultsForEachSource(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	dir := t.TempDir()
	sources := []string{filepath.Join(dir, "甲.png"), filepath.Join(dir, "乙.png"), filepath.Join(dir, "丙.png")}
	inputs := [][]byte{resultPNG(t, 10), resultPNG(t, 11), resultPNG(t, 12)}
	for i, path := range sources {
		if err := os.WriteFile(path, inputs[i], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 甲和丙得到同一张图；这张图也恰好与乙的源图相同。
	outputs := map[string][]byte{"1": inputs[1], "2": resultPNG(t, 13), "3": inputs[1]}
	var mu sync.Mutex
	uploads := map[string][]byte{}
	names := map[string]string{}
	go serveFakeExt("127.0.0.1:"+port, "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdUpload:
			var payload struct {
				FileName   string `json:"fileName"`
				DataBase64 string `json:"dataBase64"`
			}
			_ = json.Unmarshal(env.Payload, &payload)
			data, _ := base64.StdEncoding.DecodeString(payload.DataBase64)
			mu.Lock()
			uploads[env.TaskID] = append(uploads[env.TaskID], data...)
			names[env.TaskID] = payload.FileName
			mu.Unlock()
			return ackOK(env, nil)
		case extbridge.CmdSetPrompt, extbridge.CmdCaptchaAssist, extbridge.CmdSend:
			return ackOK(env, nil)
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"images": []map[string]string{{"url": "https://cdn.example/" + env.TaskID + ".png"}}})
		case extbridge.CmdDownload:
			return chunkAck(env, outputs[env.TaskID], 0)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	err := e.Run(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: sources, Wash: true,
		Concurrency: 1, WaitResult: true, WaitResultSeconds: 30, OutputPath: filepath.Join(dir, "out"),
	}, func(Task) {}, func(Progress) {})
	if err != nil {
		t.Fatal(err)
	}
	queue := e.View().Snapshot()
	if len(queue) != len(sources) {
		t.Fatalf("queue=%+v", queue)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, task := range queue {
		id := strconv.Itoa(i + 1)
		if task.Status != StatusSuccess || len(task.Images) != 1 || task.Images[0] != sources[i] || len(task.Results) != 1 {
			t.Fatalf("task %s does not match its source: %+v", id, task)
		}
		wantName := strings.TrimSuffix(filepath.Base(sources[i]), ".png") + "_01.png"
		if filepath.Base(task.Results[0]) != wantName {
			t.Fatalf("task %s result name = %s, want %s", id, task.Results[0], wantName)
		}
		if data, err := os.ReadFile(task.Results[0]); err != nil || !bytes.Equal(data, outputs[id]) {
			t.Fatalf("task %s saved the wrong result: %v", id, err)
		}
		if names[id] != filepath.Base(sources[i]) || !bytes.Equal(uploads[id], inputs[i]) {
			t.Fatalf("task %s uploaded a different source: name=%s bytes=%d", id, names[id], len(uploads[id]))
		}
	}
}

func TestExtensionWashRejectsSourceEcho(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	dir := t.TempDir()
	source := filepath.Join(dir, "源图.png")
	data := resultPNG(t, 14)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	go serveFakeExt("127.0.0.1:"+port, "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdUpload, extbridge.CmdSetPrompt, extbridge.CmdCaptchaAssist, extbridge.CmdSend:
			return ackOK(env, nil)
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"images": []map[string]string{{"url": "https://cdn.example/echo.png"}}})
		case extbridge.CmdDownload:
			return chunkAck(env, data, 0)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	err := e.Run(context.Background(), Options{
		Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: []string{source}, Wash: true,
		Concurrency: 1, WaitResult: true, WaitResultSeconds: 30, OutputPath: filepath.Join(dir, "out"),
	}, func(Task) {}, func(Progress) {})
	queue := e.View().Snapshot()
	if err != nil || len(queue) != 1 || queue[0].Status != StatusFailed || len(queue[0].Results) != 0 || !strings.Contains(queue[0].Error, "未保存到有效结果") {
		t.Fatalf("source echo must fail instead of reporting success: err=%v queue=%+v", err, queue)
	}
}

func TestExtensionRetryAcceptsItsAlreadySavedResult(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	st.Set("extBridgeToken", "dev-token")
	dir := t.TempDir()
	source := filepath.Join(dir, "源图.png")
	if err := os.WriteFile(source, resultPNG(t, 15), 0o600); err != nil {
		t.Fatal(err)
	}
	result := resultPNG(t, 16)
	var mu sync.Mutex
	var sends, waits int
	go serveFakeExt("127.0.0.1:"+port, "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdUpload, extbridge.CmdSetPrompt, extbridge.CmdCaptchaAssist:
			return ackOK(env, nil)
		case extbridge.CmdSend:
			mu.Lock()
			sends++
			mu.Unlock()
			return ackOK(env, nil)
		case extbridge.CmdWaitResult:
			mu.Lock()
			waits++
			mu.Unlock()
			return ackOK(env, map[string]any{"images": []map[string]string{{"url": "https://cdn.example/result.png"}}})
		case extbridge.CmdDownload:
			return chunkAck(env, result, 0)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	e.opts = Options{Wash: true, WaitResult: true, WaitResultSeconds: 30, OutputPath: filepath.Join(dir, "out")}
	task := &Task{Index: 1, Platform: "豆包", Prompt: "洗图", Images: []string{source}}
	if err := e.attemptDoubaoExtension(context.Background(), task); err != nil || len(task.Results) != 1 {
		t.Fatalf("first attempt: err=%v task=%+v", err, task)
	}
	if err := e.attemptDoubaoExtension(context.Background(), task); err != nil || len(task.Results) != 1 || !task.matchedSavedResult {
		t.Fatalf("retry must recognize the saved result: err=%v task=%+v", err, task)
	}
	mu.Lock()
	defer mu.Unlock()
	if sends != 1 || waits != 2 {
		t.Fatalf("retry must only wait again: sends=%d waits=%d", sends, waits)
	}
}

func TestExtensionDoubaoLoginStopsBatch(t *testing.T) {
	st := openTaskStore(t)
	port := freePort(t)
	st.Set(extbridge.SettingBrowserBackend, "extension")
	st.Set("extBridgePort", port)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	writeSolidPNG(t, src, 2, 2, color.NRGBA{R: 9, A: 255})
	var mu sync.Mutex
	uploads := 0
	go serveFakeExt("127.0.0.1:"+port, "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		if name == extbridge.CmdUpload {
			mu.Lock()
			uploads++
			mu.Unlock()
			return ackFail(env, extbridge.ENotLoggedIn, "页面没有输入框")
		}
		return ackOK(env, map[string]any{"ok": true, "captchaBlocked": false})
	})
	e := NewEngine(st, nil, nil, dir)
	defer e.CloseSessions()
	var last Task
	_ = e.Run(context.Background(), Options{
		Platforms:   []string{"豆包"},
		Prompts:     []string{"水彩风格"},
		Images:      []string{src},
		Concurrency: 1,
		RetryCount:  2,
		OutputPath:  dir,
	}, func(task Task) { last = task }, func(Progress) {})
	if last.Status != StatusFailed || !strings.Contains(last.Error, "豆包页面未登录") {
		t.Fatalf("status=%s err=%s", last.Status, last.Error)
	}
	mu.Lock()
	defer mu.Unlock()
	if uploads != 1 {
		t.Fatalf("upload retries=%d", uploads)
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
	// 8 字节一片。solidPNG 为了过生成图尺寸门槛被放大后，分片次数会把 5 秒上下文吃满，
	// -race 下稳定撞上 E_TIMEOUT。这里只要能拼回字节，用请求的小图。
	pngBytes := tinyPNG(t, 2, 2, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
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

func TestExtDownloadRejectsSVGPlaceholder(t *testing.T) {
	svg := []byte("<svg xmlns='http://www.w3.org/2000/svg' version='1.1' width='1536' height='1536'/>")
	if len(svg) != 82 {
		t.Fatalf("svg fixture is %d bytes", len(svg))
	}
	if images.IsValidImageBytes(svg) {
		t.Fatal("empty svg shell must not pass image magic")
	}
	srv, err := extbridge.Listen(extbridge.Config{Addr: "127.0.0.1:0", Token: "dev-token"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go serveFakeExt(srv.Addr(), "dev-token", extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdCaptchaAssist, extbridge.CmdSetPrompt, extbridge.CmdSend:
			return ackOK(env, map[string]any{"ok": true})
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"ok": true, "images": []map[string]string{{"url": "https://cdn.example/shell.svg"}}})
		case extbridge.CmdDownload:
			return chunkAck(env, svg, len(svg))
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
	out, err := RunDoubaoExtension(ctx, sess, QianwenJob{
		TaskID:            "live",
		IdempotencyPrefix: "live",
		Prompt:            "水彩风格",
		Wait:              time.Second,
	})
	if err == nil || len(out.Results) != 0 {
		t.Fatalf("err=%v results=%d", err, len(out.Results))
	}
	if extbridge.CodeOf(err) != extbridge.EDownloadFail || !strings.Contains(err.Error(), "PNG") {
		t.Fatal(err)
	}
}

func TestExtensionDownloadPrefersDoubaoRawAndFallsBack(t *testing.T) {
	raw := resultPNG(t, 31)
	marked := watermarkResultPNG(t, "豆包")
	repaired := repairedPNG(t, marked, "豆包")
	const rawURL = "https://p3.byteimg.com/a.jpeg~tplv-x-image_raw.png?signature=raw"
	const displayURL = "https://p3.byteimg.com/a.jpeg~tplv-x-cgen_dwm.png?signature=display"
	for _, tc := range []struct {
		name  string
		raw   []byte
		want  []byte
		calls string
	}{
		{"raw", raw, raw, rawURL},
		{"invalid raw", []byte("not an image"), repaired, rawURL + "," + displayURL},
		{"truncated raw", raw[:len(raw)-1000], repaired, rawURL + "," + displayURL},
		{"unavailable raw", nil, repaired, rawURL + "," + displayURL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var urls []string
			sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
				name, _ := env.Name()
				switch name {
				case extbridge.CmdCaptchaAssist, extbridge.CmdSetPrompt, extbridge.CmdSend:
					return ackOK(env, map[string]any{"ok": true})
				case extbridge.CmdWaitResult:
					return ackOK(env, map[string]any{"ok": true, "images": []map[string]string{{"url": displayURL, "rawUrl": rawURL}}})
				case extbridge.CmdDownload:
					var request struct {
						URL string `json:"url"`
					}
					_ = json.Unmarshal(env.Payload, &request)
					mu.Lock()
					urls = append(urls, request.URL)
					mu.Unlock()
					if request.URL == rawURL {
						if tc.raw == nil {
							return ackFail(env, extbridge.EDownloadFail, "raw unavailable")
						}
						return chunkAck(env, tc.raw, len(tc.raw))
					}
					return chunkAck(env, marked, len(marked))
				default:
					return ackFail(env, extbridge.EProtocol, name)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			out, err := RunDoubaoExtension(ctx, sess, QianwenJob{TaskID: "raw", IdempotencyPrefix: "raw", Prompt: "水彩", Wait: time.Second})
			if err != nil || len(out.Results) != 1 || !bytes.Equal(out.Results[0].Data, tc.want) || !out.Results[0].WatermarkRemoved {
				t.Fatalf("扩展结果不符: err=%v, count=%d", err, len(out.Results))
			}
			mu.Lock()
			got := strings.Join(urls, ",")
			mu.Unlock()
			if got != tc.calls {
				t.Fatalf("下载地址 = %s, want %s", got, tc.calls)
			}
		})
	}
}

func TestExtensionDownloadStripsDoubaoWatermarkAndRepairs(t *testing.T) {
	pageURL := "https://p9-flow-imagex-sign.byteimg.com/ocean-cloud-tos/a.png?x-expires=1&watermark=1&wm=2"
	cleanURL := "https://p9-flow-imagex-sign.byteimg.com/ocean-cloud-tos/a.png"
	watermarked := watermarkResultPNG(t, "豆包")
	want := repairedPNG(t, watermarked, "豆包")
	var mu sync.Mutex
	var urls []string
	sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdCaptchaAssist, extbridge.CmdSetPrompt, extbridge.CmdSend:
			return ackOK(env, map[string]any{"ok": true})
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"ok": true, "images": []map[string]string{{"url": pageURL}}})
		case extbridge.CmdDownload:
			var body struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(env.Payload, &body)
			mu.Lock()
			urls = append(urls, body.URL)
			mu.Unlock()
			if strings.Contains(body.URL, "?") {
				return ackFail(env, extbridge.EDownloadFail, "watermark url rejected")
			}
			return chunkAck(env, watermarked, len(watermarked))
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := RunDoubaoExtension(ctx, sess, QianwenJob{
		TaskID:            "live",
		IdempotencyPrefix: "live",
		Prompt:            "水彩",
		Wait:              time.Second,
	})
	if err != nil || len(out.Results) != 1 {
		t.Fatalf("err=%v results=%d", err, len(out.Results))
	}
	mu.Lock()
	gotURLs := append([]string(nil), urls...)
	mu.Unlock()
	if len(gotURLs) != 1 || gotURLs[0] != cleanURL {
		t.Fatalf("download urls=%v, want only %s", gotURLs, cleanURL)
	}
	result := out.Results[0]
	if result.URL != cleanURL || !result.WatermarkRemoved || !bytes.Equal(result.Data, want) || bytes.Equal(result.Data, watermarked) {
		t.Fatalf("url=%s removed=%v sameAsWatermarked=%v", result.URL, result.WatermarkRemoved, bytes.Equal(result.Data, watermarked))
	}
	for _, enabled := range []bool{false, true} {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputPath: t.TempDir(), RemoveWatermark: enabled}
		start := len(logging.Log.Entries())
		saved, err := e.saveResults(context.Background(), &Task{Platform: "豆包", Images: []string{"source.png"}}, nil, nil, out.Results)
		day := e.opts.ResultDir(time.Now())
		if err != nil || len(saved) != 1 || saved[0] != filepath.Join(day, "文生图_01.png") {
			t.Fatalf("enabled=%t saved=%v err=%v", enabled, saved, err)
		}
		file, err := os.ReadFile(saved[0])
		if err != nil || !bytes.Equal(file, want) {
			t.Fatalf("enabled=%t 落盘应是去水印 PNG: %v", enabled, err)
		}
		files, err := os.ReadDir(day)
		if err != nil || len(files) != 1 {
			t.Fatalf("enabled=%t 不应另存第二份: %v %v", enabled, files, err)
		}
		for _, entry := range logging.Log.Entries()[start:] {
			if strings.Contains(entry.Text, "未匹配") || strings.Contains(entry.Text, "正在计算去水印") {
				t.Fatalf("enabled=%t 扩展结果不应再算一遍: %s", enabled, entry.Text)
			}
		}
	}
}

func TestExtensionDownloadQianwenSharesRepairWithoutNewStrip(t *testing.T) {
	pageURL := "https://img.alicdn.com/out.png?x-oss-process=image/resize,w_1024"
	watermarked := watermarkResultPNG(t, "千问")
	want := repairedPNG(t, watermarked, "千问")
	var mu sync.Mutex
	var urls []string
	sess := dialFakeExt(t, extbridge.PlatformQianwen, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdCaptchaAssist, extbridge.CmdSetPrompt, extbridge.CmdSend:
			return ackOK(env, map[string]any{"ok": true})
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"ok": true, "images": []map[string]string{{"url": pageURL}}})
		case extbridge.CmdDownload:
			var body struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(env.Payload, &body)
			mu.Lock()
			urls = append(urls, body.URL)
			mu.Unlock()
			return chunkAck(env, watermarked, len(watermarked))
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := RunQianwenExtension(ctx, sess, QianwenJob{
		TaskID:            "live",
		IdempotencyPrefix: "live",
		Prompt:            "水彩",
		Wait:              time.Second,
	})
	if err != nil || len(out.Results) != 1 {
		t.Fatalf("err=%v results=%d", err, len(out.Results))
	}
	mu.Lock()
	gotURLs := append([]string(nil), urls...)
	mu.Unlock()
	if len(gotURLs) != 1 || gotURLs[0] != pageURL {
		t.Fatalf("qianwen url without watermark params must stay %s, got %v", pageURL, gotURLs)
	}
	result := out.Results[0]
	if result.URL != pageURL || !result.WatermarkRemoved || !bytes.Equal(result.Data, want) {
		t.Fatalf("url=%s removed=%v", result.URL, result.WatermarkRemoved)
	}
	if strategy.PreferUnwatermarkedURL(pageURL) != pageURL {
		t.Fatal("qianwen oss process must not be treated as a watermark query")
	}
}

func TestExtensionDownloadFallsBackToOriginalURL(t *testing.T) {
	pageURL := "https://p9-flow-imagex-sign.byteimg.com/ocean-cloud-tos/a.png?x-expires=1&wm=1"
	cleanURL := "https://p9-flow-imagex-sign.byteimg.com/ocean-cloud-tos/a.png"
	small := solidPNG(t, 2, 2, color.NRGBA{R: 8, G: 9, B: 10, A: 255})
	var mu sync.Mutex
	var urls []string
	sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdCaptchaAssist, extbridge.CmdSetPrompt, extbridge.CmdSend:
			return ackOK(env, map[string]any{"ok": true})
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"ok": true, "images": []map[string]string{{"url": pageURL}}})
		case extbridge.CmdDownload:
			var body struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(env.Payload, &body)
			mu.Lock()
			urls = append(urls, body.URL)
			mu.Unlock()
			if !strings.Contains(body.URL, "wm=") {
				return ackFail(env, extbridge.EDownloadFail, "clean url rejected")
			}
			return chunkAck(env, small, len(small))
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := RunDoubaoExtension(ctx, sess, QianwenJob{
		TaskID:            "live",
		IdempotencyPrefix: "live",
		Prompt:            "水彩",
		Wait:              time.Second,
	})
	if err != nil || len(out.Results) != 1 {
		t.Fatalf("err=%v results=%d", err, len(out.Results))
	}
	mu.Lock()
	gotURLs := append([]string(nil), urls...)
	mu.Unlock()
	if len(gotURLs) != 2 || gotURLs[0] != cleanURL || gotURLs[1] != pageURL {
		t.Fatalf("urls=%v", gotURLs)
	}
	if out.Results[0].URL != pageURL || out.Results[0].WatermarkRemoved || !bytes.Equal(out.Results[0].Data, small) {
		t.Fatalf("url=%s removed=%v", out.Results[0].URL, out.Results[0].WatermarkRemoved)
	}
}

func TestDewatermarkExtImageKeepsGIF(t *testing.T) {
	var animation bytes.Buffer
	frame := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{1, 1}}); err != nil {
		t.Fatal(err)
	}
	out, removed, err := dewatermarkExtImage(context.Background(), animation.Bytes(), "豆包")
	if err != nil || removed || !bytes.Equal(out, animation.Bytes()) {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
}

func dialFakeExt(t *testing.T, platform string, fn func(extbridge.Envelope) extbridge.Envelope) *extbridge.Session {
	t.Helper()
	srv, err := extbridge.Listen(extbridge.Config{Addr: "127.0.0.1:0", Token: "dev-token"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go serveFakeExt(srv.Addr(), "dev-token", platform, fn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := srv.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return sess
}

func repairedPNG(t *testing.T, data []byte, platform string) []byte {
	t.Helper()
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	repaired, changed, err := images.RemoveWatermark(context.Background(), decoded, platform)
	if err != nil || !changed {
		t.Fatalf("fixture must match %s watermark: changed=%v err=%v", platform, changed, err)
	}
	out, err := images.EncodePNG(repaired)
	if err != nil {
		t.Fatal(err)
	}
	return out
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

func tinyPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
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

func solidPNG(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	if w < 128 {
		w = 128
	}
	if h < 128 {
		h = 128
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rnd := rand.New(rand.NewPCG(uint64(c.R)+1, uint64(w)))
	for i := range img.Pix {
		img.Pix[i] = byte(rnd.Uint32())
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
	serveFakeExt(addr, token, extbridge.PlatformQianwen, fn)
}

func serveFakeExt(addr, token, platform string, fn func(extbridge.Envelope) extbridge.Envelope) {
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
	hello := extbridge.Envelope{V: extbridge.Version, ID: "hello-test", Type: extbridge.TypeHello, TS: time.Now().UnixMilli(), Platform: platform, Payload: raw}
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

func TestExtensionHelloSavesOnlyLoggedInAccounts(t *testing.T) {
	for _, tc := range []struct{ protocol, label string }{
		{extbridge.PlatformQianwen, "千问"},
		{extbridge.PlatformDoubao, "豆包"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			st := openTaskStore(t)
			st.Set(extbridge.SettingBrowserBackend, "extension")
			port := freePort(t)
			st.Set(extbridge.SettingExtPort, port)
			st.Set(extbridge.SettingExtToken, "token-a")
			e := NewEngine(st, nil, nil, t.TempDir())
			t.Cleanup(func() { e.CloseSessions() })
			if _, err := e.extensionServer(); err != nil {
				t.Fatal(err)
			}
			addr := "127.0.0.1:" + port
			closeHello := func(account string, loggedIn *bool) {
				t.Helper()
				conn := dialExtensionHello(t, addr, "token-a", tc.protocol, account, loggedIn)
				conn.Close()
			}
			closeHello("profile-x", nil)
			time.Sleep(80 * time.Millisecond)
			if rows := st.Browsers(); len(rows) != 0 {
				t.Fatalf("connect without login saved %+v", rows)
			}
			loggedOut := false
			if _, err := st.UpsertExtensionAccount(tc.label, "profile-x", "扩展"); err != nil {
				t.Fatal(err)
			}
			closeHello("profile-x", &loggedOut)
			if rows := waitBrowserCount(st, 0, time.Second); len(rows) != 0 {
				t.Fatalf("explicit logout kept %+v", rows)
			}
			loggedIn := true
			closeHello("profile-x", &loggedIn)
			rows := waitBrowserCount(st, 1, time.Second)
			if len(rows) != 1 || rows[0].Platform != tc.label || !rows[0].IsLoggedIn || !rows[0].Usable(time.Now()) || rows[0].ExtAccount != "profile-x" {
				t.Fatalf("logged-in hello = %+v", rows)
			}
			conn := dialExtensionHello(t, addr, "token-a", tc.protocol, "profile-y", nil)
			defer conn.Close()
			time.Sleep(80 * time.Millisecond)
			if rows := st.Browsers(); len(rows) != 1 {
				t.Fatalf("second connect saved %+v", rows)
			}
			raw, _ := json.Marshal(map[string]any{"name": "login", "loggedIn": true, "account": "profile-y"})
			if err := writeClient(conn, extbridge.Envelope{
				V: extbridge.Version, ID: "login-y", Type: extbridge.TypeEvent,
				TS: time.Now().UnixMilli(), Platform: tc.protocol, Payload: raw,
			}); err != nil {
				t.Fatal(err)
			}
			rows = waitBrowserCount(st, 2, time.Second)
			if len(rows) != 2 {
				t.Fatalf("login event = %+v", rows)
			}
			var sawY bool
			for _, row := range rows {
				if row.ExtAccount == "profile-y" {
					sawY = row.Platform == tc.label && row.IsLoggedIn && row.Usable(time.Now())
				}
			}
			if !sawY {
				t.Fatalf("profile-y = %+v", rows)
			}
		})
	}
}

func TestExtensionLoginIsOnlyRecordedInExtensionMode(t *testing.T) {
	for _, tc := range []struct{ protocol, label string }{
		{extbridge.PlatformQianwen, "千问"},
		{extbridge.PlatformDoubao, "豆包"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			st := openTaskStore(t)
			e := NewEngine(st, nil, nil, t.TempDir())
			raw, _ := json.Marshal(map[string]any{"name": "login", "loggedIn": true, "account": "profile-x"})
			login := extbridge.Envelope{
				V: extbridge.Version, ID: "login-x", Type: extbridge.TypeEvent,
				TS: time.Now().UnixMilli(), Platform: tc.protocol, Payload: raw,
			}

			e.noteExtensionLogin(nil, login)
			if rows := st.Browsers(); len(rows) != 0 {
				t.Fatalf("ego mode recorded an extension account: %+v", rows)
			}

			st.Set(extbridge.SettingBrowserBackend, "extension")
			e.noteExtensionLogin(nil, login)
			rows := st.Browsers()
			if len(rows) != 1 || rows[0].Platform != tc.label || rows[0].ExtAccount != "profile-x" || !rows[0].IsLoggedIn {
				t.Fatalf("extension mode did not record the login: %+v", rows)
			}
		})
	}
}

func TestDropInactiveExtensionAccounts(t *testing.T) {
	st := openTaskStore(t)
	local, err := st.AddBrowser("豆包", "豆包1", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(local.ID, false, true)
	for _, platform := range []string{"豆包", "千问"} {
		if _, err := st.SaveLoggedInExtension(platform, "profile-x", ""); err != nil {
			t.Fatal(err)
		}
	}
	e := NewEngine(st, nil, nil, t.TempDir())

	st.Set(extbridge.SettingBrowserBackend, "extension")
	e.DropInactiveExtensionAccounts()
	if rows := st.Browsers(); len(rows) != 3 {
		t.Fatalf("extension mode lost accounts: %+v", rows)
	}

	st.Set(extbridge.SettingBrowserBackend, "ego")
	e.DropInactiveExtensionAccounts()
	rows := st.Browsers()
	if len(rows) != 1 || rows[0].ID != local.ID || rows[0].ExtAccount != "" {
		t.Fatalf("the local account should remain alone: %+v", rows)
	}
}

func dialExtensionHello(t *testing.T, addr, token, platform, account string, loggedIn *bool) net.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	conn, br, _, err := ws.Dial(ctx, "ws://"+addr+"/")
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"token": token, "version": "0.3.10", "account": account}
	if loggedIn != nil {
		payload["loggedIn"] = *loggedIn
	}
	raw, _ := json.Marshal(payload)
	hello := extbridge.Envelope{
		V: extbridge.Version, ID: "hello-" + account, Type: extbridge.TypeHello,
		TS: time.Now().UnixMilli(), Platform: platform, Payload: raw,
	}
	if err := writeClient(conn, hello); err != nil {
		conn.Close()
		t.Fatal(err)
	}
	reader := io.Reader(conn)
	if br != nil {
		reader = br
	}
	ack, err := readServer(conn, reader)
	if err != nil || ack.Type != extbridge.TypeAck {
		conn.Close()
		t.Fatalf("ack = %+v err=%v", ack, err)
	}
	return conn
}

func waitBrowserCount(st *store.Store, n int, wait time.Duration) []store.Browser {
	deadline := time.Now().Add(wait)
	rows := st.Browsers()
	for len(rows) != n && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
		rows = st.Browsers()
	}
	return rows
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
