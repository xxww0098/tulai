package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/xxww0098/tulai/internal/extbridge"
)

// Unlike serveFakeExt, this peer can answer independent download commands out of order,
// as the Firefox extension does while multiple fetches are in flight.
func dialConcurrentWashExt(t *testing.T, handle func(extbridge.Envelope) extbridge.Envelope) *extbridge.Session {
	t.Helper()
	srv, err := extbridge.Listen(extbridge.Config{Addr: "127.0.0.1:0", Token: "wash-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, reader, _, err := ws.Dial(ctx, "ws://"+srv.Addr()+"/")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	raw, _ := json.Marshal(map[string]string{"token": "wash-test", "version": "0.2.0"})
	hello := extbridge.Envelope{V: extbridge.Version, ID: "hello-wash", Type: extbridge.TypeHello, TS: time.Now().UnixMilli(), Platform: extbridge.PlatformDoubao, Payload: raw}
	if err := writeClient(conn, hello); err != nil {
		t.Fatal(err)
	}
	var stream io.Reader = conn
	if reader != nil {
		stream = reader
	}
	if _, err := readServer(conn, stream); err != nil {
		t.Fatal(err)
	}
	sess, err := srv.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		var writeMu sync.Mutex
		for {
			env, err := readServer(conn, stream)
			if err != nil {
				return
			}
			if env.Type != extbridge.TypeCmd {
				continue
			}
			go func() {
				reply := handle(env)
				writeMu.Lock()
				defer writeMu.Unlock()
				_ = writeClient(conn, reply)
			}()
		}
	}()
	return sess
}

func TestDoubaoWashDownloadsKeepResultOrder(t *testing.T) {
	const count = 6
	var active, peak atomic.Int32
	images := make([]map[string]any, count)
	inputs := make([]string, count)
	data := make(map[string][]byte, count)
	for i := range count {
		url := fmt.Sprintf("https://cdn.example/raw-%d.png", i)
		images[i] = map[string]any{"url": url, "rawUrl": url, "sourceIndex": count - i}
		inputs[i] = fmt.Sprintf("source-%d.png", i)
		data[url] = resultPNG(t, byte(i+1))
	}
	sess := dialConcurrentWashExt(t, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		switch name {
		case extbridge.CmdCaptchaAssist:
			return ackOK(env, nil)
		case extbridge.CmdWaitResult:
			return ackOK(env, map[string]any{"images": images})
		case extbridge.CmdDownload:
			var request struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(env.Payload, &request)
			n := active.Add(1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			// Deliberately complete later URLs first to exercise response ordering.
			time.Sleep(150*time.Millisecond + time.Duration(count-int(request.URL[len(request.URL)-5]-'0'))*10*time.Millisecond)
			active.Add(-1)
			return chunkAck(env, data[request.URL], 0)
		default:
			return ackFail(env, extbridge.EProtocol, name)
		}
	})
	started := time.Now()
	out, err := RunDoubaoExtension(context.Background(), sess, QianwenJob{TaskID: "wash", Images: inputs, Prompt: "洗图", Wait: time.Second, SkipSubmit: true, VerifyAttachments: true})
	if err != nil || len(out.Results) != count || len(out.sourceIndices) != count {
		t.Fatalf("results=%d indices=%v err=%v", len(out.Results), out.sourceIndices, err)
	}
	for i, item := range out.Results {
		url := fmt.Sprintf("https://cdn.example/raw-%d.png", i)
		if !bytes.Equal(item.Data, data[url]) || out.sourceIndices[i] != count-i {
			t.Fatalf("result %d lost its source association", i)
		}
	}
	if n := peak.Load(); n < 2 || n > 3 {
		t.Fatalf("download concurrency = %d, want 2 or 3", n)
	}
	t.Logf("six downloads: %v, peak concurrent requests: %d", time.Since(started), peak.Load())
}

func TestDoubaoWashDownloadFailureKeepsOtherAssociations(t *testing.T) {
	const count = 4
	items := make([]extImage, count)
	data := make([][]byte, count)
	for i := range items {
		items[i] = extImage{
			URL:         fmt.Sprintf("https://cdn.example/view-%d.png", i),
			RawURL:      fmt.Sprintf("https://cdn.example/raw-%d.png", i),
			SourceIndex: i + 1,
		}
		data[i] = resultPNG(t, byte(i+21))
	}
	sess := dialConcurrentWashExt(t, func(env extbridge.Envelope) extbridge.Envelope {
		var request struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(env.Payload, &request)
		for i, item := range items {
			if request.URL == item.RawURL || request.URL == item.URL {
				if i == 2 || (i == 1 && request.URL == item.RawURL) {
					return ackFail(env, extbridge.EDownloadFail, "fixture unavailable")
				}
				return chunkAck(env, data[i], 0)
			}
		}
		return ackFail(env, extbridge.EDownloadFail, "unexpected URL")
	})
	results, indices, err := downloadExtensionResults(context.Background(), sess, "wash", "豆包", items, 3)
	if err == nil || len(results) != 3 || len(indices) != 3 {
		t.Fatalf("results=%d indices=%v err=%v", len(results), indices, err)
	}
	for i, sourceIndex := range []int{1, 2, 4} {
		if indices[i] != sourceIndex || !bytes.Equal(results[i].Data, data[sourceIndex-1]) {
			t.Fatalf("partial result %d lost its source association", i)
		}
	}
}

func TestDoubaoWashDownloadCancellationStopsWaiting(t *testing.T) {
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	defer close(release)
	sess := dialConcurrentWashExt(t, func(env extbridge.Envelope) extbridge.Envelope {
		started <- struct{}{}
		<-release
		return ackFail(env, extbridge.EDownloadFail, "cancelled fixture")
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, _, err := downloadExtensionResults(ctx, sess, "wash", "豆包", []extImage{{URL: "https://cdn.example/1.png"}, {URL: "https://cdn.example/2.png"}}, 3)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("download did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled download: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("download did not stop after cancellation")
	}
}
