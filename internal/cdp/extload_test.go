package cdp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// extFake 模拟一个刚启动的 Edge：组件扩展的 service worker 不回应附着，
// 页面桥要等 loadUnpacked 之后才出现，并且第一次读清单会失败。
type extFake struct {
	mu          sync.Mutex
	includeOurs bool
	loaded      bool
	readyAfter  int
	evals       int
}

func TestEnsureExtensionSkipsStuckComponentWorker(t *testing.T) {
	fake := &extFake{includeOurs: true}
	endpoint := startExtCDP(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := EnsureExtension(ctx, endpoint, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureExtensionWaitsForOwnServiceWorker(t *testing.T) {
	fake := &extFake{readyAfter: 1}
	endpoint := startExtCDP(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := EnsureExtension(ctx, endpoint, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	evals := fake.evals
	loaded := fake.loaded
	fake.mu.Unlock()
	if !loaded || evals < 2 {
		t.Fatalf("loaded=%v evals=%d, want to wait until our worker can answer", loaded, evals)
	}
}

func TestExtensionPairStateSkipsStuckComponentWorker(t *testing.T) {
	fake := &extFake{includeOurs: true}
	endpoint := startExtCDP(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	id, token, found, err := ExtensionPairState(ctx, endpoint)
	if err != nil || !found || id != "profile-a" || token != "tok" {
		t.Fatalf("id=%q token=%q found=%v err=%v", id, token, found, err)
	}
}

func startExtCDP(t *testing.T, fake *extFake) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			return
		}
		defer conn.Close()
		fake.serve(conn, rw.Reader)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return "ws://" + ln.Addr().String() + "/devtools/browser/test"
}

func (f *extFake) serve(conn net.Conn, r *bufio.Reader) {
	var reader io.Reader = conn
	if r != nil {
		reader = r
	}
	rw := struct {
		io.Reader
		io.Writer
	}{reader, conn}
	for {
		payload, err := wsutil.ReadClientText(rw)
		if err != nil {
			return
		}
		var req struct {
			ID      int64           `json:"id"`
			Method  string          `json:"method"`
			Session string          `json:"sessionId"`
			Params  json.RawMessage `json:"params"`
		}
		if json.Unmarshal(payload, &req) != nil || req.ID == 0 {
			continue
		}
		if req.Method == "Target.attachToTarget" {
			var p struct {
				TargetID string `json:"targetId"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if p.TargetID == "foreign" {
				continue
			}
		}
		raw, err := json.Marshal(map[string]any{"id": req.ID, "result": f.result(req.Method, req.Params)})
		if err != nil {
			return
		}
		if err := wsutil.WriteServerMessage(conn, ws.OpText, raw); err != nil {
			return
		}
	}
}

func (f *extFake) result(method string, params json.RawMessage) any {
	switch method {
	case "Target.getTargets":
		return map[string]any{"targetInfos": f.targetInfos()}
	case "Target.attachToTarget":
		return map[string]any{"sessionId": "sess-ours"}
	case "Runtime.evaluate":
		return f.eval(params)
	case "Extensions.loadUnpacked":
		f.mu.Lock()
		f.loaded = true
		f.mu.Unlock()
		return map[string]any{"id": "ours"}
	default:
		return map[string]any{}
	}
}

func (f *extFake) targetInfos() []map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := []map[string]string{{
		"targetId": "foreign",
		"type":     "service_worker",
		"url":      "chrome-extension://foreign/background.js",
	}}
	if f.includeOurs || f.loaded {
		list = append(list, map[string]string{
			"targetId": "ours",
			"type":     "service_worker",
			"url":      "chrome-extension://ours/background.js",
		})
	}
	return list
}

func (f *extFake) eval(params json.RawMessage) any {
	var p struct {
		Expression string `json:"expression"`
	}
	_ = json.Unmarshal(params, &p)
	f.mu.Lock()
	f.evals++
	n := f.evals
	readyAfter := f.readyAfter
	f.mu.Unlock()
	if n <= readyAfter {
		return map[string]any{
			"exceptionDetails": map[string]any{"text": "Uncaught"},
			"result":           map[string]any{"type": "undefined"},
		}
	}
	value := map[string]any{"name": extensionName}
	if strings.Contains(p.Expression, "profileId") {
		value["id"] = "profile-a"
		value["token"] = "tok"
	}
	return map[string]any{"result": map[string]any{"type": "object", "value": value}}
}
