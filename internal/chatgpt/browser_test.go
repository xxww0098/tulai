package chatgpt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/camoufoxtest"
	"github.com/xxww0098/tulai/internal/cdp"
)

func TestBrowserTransportStreamsAndCancelsWithinManagedCamoufox(t *testing.T) {
	if testing.Short() {
		t.Skip("隔离 Camoufox 页面传输 E2E")
	}
	want := []byte(strings.Repeat("data: 中文生成流\n\n", 10000))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write(want[:40000])
			w.(http.Flusher).Flush()
			w.Write(want[40000:])
		case "/upload":
			data, err := io.ReadAll(r.Body)
			if err != nil {
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write(data)
		case "/slow":
			w.Write([]byte("first"))
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
		default:
			fmt.Fprint(w, `<!doctype html><html><title>ChatGPT transport fixture</title><body>local fixture</body></html>`)
		}
	}))
	defer server.Close()
	_, endpoint := camoufoxtest.Start(t, "about:blank")
	control, err := cdp.NewClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	page, err := control.NewSession(server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()
	// Map all protocol URLs to this local fixture. No request reaches ChatGPT or storage.
	if _, err := page.Evaluate(`(() => {
 const nativeFetch = fetch;
 window.fetch = (url, options) => nativeFetch(new URL(url).pathname, options);
})()`); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: browserTransport{page: page}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := New(ctx, page); !errors.Is(err, ErrNeedLogin) {
		t.Fatalf("non-ChatGPT page was accepted as an account: %v", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://chatgpt.com/stream", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("stream bytes=%d want=%d error=%v", len(got), len(want), err)
	}
	binary := append([]byte{0, 255, 128, 1}, want...)
	req, _ = http.NewRequestWithContext(ctx, http.MethodPut, "https://fixture.blob.core.windows.net/upload", bytes.NewReader(binary))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatal("binary upload changed bytes", err)
	}
	slowCtx, stop := context.WithCancel(ctx)
	req, _ = http.NewRequestWithContext(slowCtx, http.MethodGet, "https://chatgpt.com/slow", nil)
	resp, err = client.Do(req)
	if err != nil {
		stop()
		t.Fatal(err)
	}
	prefix := make([]byte, 5)
	if _, err := io.ReadFull(resp.Body, prefix); err != nil || string(prefix) != "first" {
		stop()
		resp.Body.Close()
		t.Fatal("initial chunk missing", err)
	}
	stop()
	if _, err := resp.Body.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		resp.Body.Close()
		t.Fatalf("cancellation=%v", err)
	}
	resp.Body.Close()
	if got, err := page.Evaluate(`document.title`); err != nil || !strings.Contains(got, "transport fixture") {
		t.Fatal("cancellation closed the account page", err)
	}
	if count := page.EvaluateInt(`Object.keys(window.__tulaiChatGPTRequests || {}).length`); count != 0 {
		t.Fatalf("retained %d page requests", count)
	}
}
