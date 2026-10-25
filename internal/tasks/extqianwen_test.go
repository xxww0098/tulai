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
	"xbs/internal/images"
	"xbs/internal/logging"
	"xbs/internal/store"
	"xbs/internal/strategy"
)

func TestUseExtensionForQianwenAndDoubao(t *testing.T) {
	st := openTaskStore(t)
	e := NewEngine(st, nil, nil, t.TempDir())
	if e.useExtension("千问") || e.useExtension("豆包") {
		t.Fatal("default backend must stay on ego")
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
		NamingRule:        "原文件名",
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
		if err != nil || len(saved) != 1 || filepath.Base(saved[0]) != "source.png" {
			t.Fatalf("enabled=%t saved=%v err=%v", enabled, saved, err)
		}
		file, err := os.ReadFile(saved[0])
		if err != nil || !bytes.Equal(file, want) {
			t.Fatalf("enabled=%t 落盘应是去水印 PNG: %v", enabled, err)
		}
		files, err := os.ReadDir(e.opts.OutputPath)
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
