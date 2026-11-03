package tasks

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/xxww0098/tulai/internal/strategy"
)

// 页面里取不到结果图时，按设置走下载引擎，把字节取回来照常校验和落盘。
func TestSaveResultsFallsBackToDownloadEngine(t *testing.T) {
	payload := resultPNG(t, 9)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(payload)
	}))
	defer srv.Close()

	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir(), DownloadConcurrency: 2, DownloadRetries: 1}
	strat := &resultSequenceStrategy{}
	saved, err := e.saveResults(context.Background(), &Task{}, strat, nil, []strategy.Result{{URL: srv.URL + "/result.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 {
		t.Fatalf("saved = %v", saved)
	}
	got, err := os.ReadFile(saved[0])
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("落盘内容不符: %v", err)
	}
}

// 没有配置下载设置时不启用兜底，避免测试或直接调用引擎的地方意外联网。
func TestSaveResultsSkipsDownloadEngineWhenUnconfigured(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte("should not be requested"))
	}))
	defer srv.Close()

	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir()}
	strat := &resultSequenceStrategy{}
	saved, err := e.saveResults(context.Background(), &Task{}, strat, nil, []strategy.Result{{URL: srv.URL + "/result.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 0 || hits.Load() != 0 {
		t.Fatalf("未配置时不应下载: saved=%v hits=%d", saved, hits.Load())
	}
}
