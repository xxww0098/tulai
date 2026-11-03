package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestRunBatchDownloadsEveryItem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("bytes for " + r.URL.Path))
	}))
	defer srv.Close()

	dir := t.TempDir()
	items := []Item{
		{URL: srv.URL + "/a.bin"},
		{URL: srv.URL + "/b.bin"},
		{URL: srv.URL + "/c.bin"},
	}
	var lastDone, lastTotal int
	results := RunBatch(context.Background(), New(Options{Getter: NewHTTPGetter("")}),
		Job{Directory: dir}, items, 3, func(p BatchProgress) {
			if p.Total != len(items) || p.Done < 0 || p.Done > p.Total {
				t.Errorf("progress = %+v", p)
			}
			lastDone, lastTotal = p.Done, p.Total
		})
	if len(results) != len(items) {
		t.Fatalf("results = %d", len(results))
	}
	for i, r := range results {
		if r.Err != nil || r.Skipped {
			t.Fatalf("item %d: %+v", i, r)
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.Base(items[i].URL)))
		if err != nil || string(data) != "bytes for "+mustPath(items[i].URL) {
			t.Fatalf("item %d file: %q, %v", i, data, err)
		}
	}
	if lastDone != len(items) || lastTotal != len(items) {
		t.Fatalf("final progress = %d/%d", lastDone, lastTotal)
	}
}

func TestRunBatchCancellationMarksUnprocessed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	items := []Item{{URL: "https://cdn.example/a.bin"}, {URL: "https://cdn.example/b.bin"}}
	results := RunBatch(ctx, New(Options{Getter: NewHTTPGetter(""), Retries: 3}),
		Job{Directory: t.TempDir()}, items, 2, nil)
	if len(results) != len(items) {
		t.Fatalf("results = %d", len(results))
	}
	for _, r := range results {
		if !errors.Is(r.Err, context.Canceled) {
			t.Fatalf("result = %+v", r)
		}
	}
}

// mustPath 取地址的路径部分，测试服务端按它回写内容。
func mustPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Path
}
