package download

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// script 按顺序返回预先排好的响应，用来控制服务器的行为。
type script struct {
	mu    sync.Mutex
	steps []http.HandlerFunc
	hits  atomic.Int32
}

func (s *script) Get(_ context.Context, rawURL string, prepare func(*http.Request)) (*Response, error) {
	s.mu.Lock()
	if len(s.steps) == 0 {
		s.mu.Unlock()
		return nil, errors.New("没有更多响应")
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	s.mu.Unlock()
	s.hits.Add(1)

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if prepare != nil {
		prepare(req)
	}
	rec := &record{}
	step(rec, req)
	return rec.response(), nil
}

// record 收集一次响应，避免测试依赖真实的网络。
type record struct {
	code   int
	header http.Header
	body   string
}

func (r *record) Header() http.Header {
	if r.header == nil {
		r.header = http.Header{}
	}
	return r.header
}

func (r *record) Write(b []byte) (int, error) { r.body += string(b); return len(b), nil }

func (r *record) WriteHeader(code int) { r.code = code }

func (r *record) response() *Response {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return &Response{StatusCode: r.code, Header: r.Header(), Body: io.NopCloser(strings.NewReader(r.body))}
}

func ok(body, etag string, total int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Length", itoa(len(body)))
		w.Write([]byte(body))
		_ = total
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestDownloadWritesFile(t *testing.T) {
	srv := &script{steps: []http.HandlerFunc{ok("hello image", `"v1"`, 0)}}
	dir := t.TempDir()
	dl := New(Options{Getter: srv})
	err := dl.Download(context.Background(), Job{Directory: dir}, Item{URL: "https://cdn.example/a.jpg", Extension: "jpg"}, nil)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a.jpg"))
	if err != nil {
		t.Fatalf("读取结果: %v", err)
	}
	if string(got) != "hello image" {
		t.Fatalf("内容是 %q", got)
	}
}

func TestDownloadResumesPartial(t *testing.T) {
	const full = "0123456789abcdefghij"
	dir := t.TempDir()
	dest := filepath.Join(dir, "pic.bin")
	if err := os.WriteFile(dest+".part", []byte(full[:10]), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePartialMeta(dest+".part", PartialMeta{URL: "https://cdn.example/pic.bin", ETag: `"v1"`, Total: int64(len(full))}); err != nil {
		t.Fatal(err)
	}

	var gotRange string
	srv := &script{steps: []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) {
			gotRange = r.Header.Get("Range")
			if r.Header.Get("If-Range") != `"v1"` {
				t.Errorf("If-Range = %q", r.Header.Get("If-Range"))
			}
			w.Header().Set("Content-Range", "bytes 10-19/20")
			w.Header().Set("ETag", `"v1"`)
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte(full[10:]))
		},
	}}
	dl := New(Options{Getter: srv})
	if err := dl.Download(context.Background(), Job{Directory: dir}, Item{URL: "https://cdn.example/pic.bin"}, nil); err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	if gotRange != "bytes=10-" {
		t.Fatalf("Range = %q", gotRange)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != full {
		t.Fatalf("续传结果是 %q", got)
	}
	if _, err := os.Stat(dest + ".part"); !os.IsNotExist(err) {
		t.Fatal("半成品没有被提交")
	}
}

func TestDownloadRestartsWhenContentChanged(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "pic.bin")
	if err := os.WriteFile(dest+".part", []byte("stale-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePartialMeta(dest+".part", PartialMeta{URL: "https://cdn.example/pic.bin", ETag: `"old"`, Total: 20}); err != nil {
		t.Fatal(err)
	}

	srv := &script{steps: []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) {
			// 校验值变了，这份 206 不能续上。
			w.Header().Set("Content-Range", "bytes 10-19/20")
			w.Header().Set("ETag", `"new"`)
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("0123456789"))
		},
		func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Range") != "" {
				t.Errorf("重下仍带了 Range: %q", r.Header.Get("Range"))
			}
			w.Header().Set("ETag", `"new"`)
			w.Write([]byte("fresh"))
		},
	}}
	dl := New(Options{Getter: srv})
	if err := dl.Download(context.Background(), Job{Directory: dir}, Item{URL: "https://cdn.example/pic.bin"}, nil); err != nil {
		t.Fatalf("重下失败: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "fresh" {
		t.Fatalf("结果是 %q，残片被拼了进去", got)
	}
}

func TestDownloadRetriesRetryableStatus(t *testing.T) {
	srv := &script{steps: []http.HandlerFunc{
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		},
		ok("recovered", `"v"`, 0),
	}}
	dir := t.TempDir()
	dl := New(Options{Getter: srv, Retries: 1, RetryBase: time.Millisecond})
	err := dl.Download(context.Background(), Job{Directory: dir}, Item{URL: "https://cdn.example/a.jpg", Extension: "jpg"}, nil)
	if err != nil {
		t.Fatalf("重试后仍失败: %v", err)
	}
	if srv.hits.Load() != 2 {
		t.Fatalf("请求了 %d 次", srv.hits.Load())
	}
}

func TestDownloadDoesNotRetryNotFound(t *testing.T) {
	srv := &script{steps: []http.HandlerFunc{
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		ok("should not be fetched", "", 0),
	}}
	dl := New(Options{Getter: srv, Retries: 3, RetryBase: time.Millisecond})
	err := dl.Download(context.Background(), Job{Directory: t.TempDir()}, Item{URL: "https://cdn.example/missing.jpg"}, nil)
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusNotFound {
		t.Fatalf("得到 %v", err)
	}
	if srv.hits.Load() != 1 {
		t.Fatalf("404 被重试了 %d 次", srv.hits.Load())
	}
}

func TestDownloadSkipsExistingAndArchive(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.jpg"), []byte("already"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := &script{}
	dl := New(Options{Getter: srv})
	err := dl.Download(context.Background(), Job{Directory: dir}, Item{URL: "https://cdn.example/keep.jpg"}, nil)
	if !errors.Is(err, ErrSkipped) {
		t.Fatalf("得到 %v", err)
	}
	if srv.hits.Load() != 0 {
		t.Fatal("已存在的文件仍发起了请求")
	}

	archive := NewMemoryArchive()
	srv = &script{steps: []http.HandlerFunc{ok("one", "", 0)}}
	dl = New(Options{Getter: srv, Archive: archive})
	item := Item{URL: "https://cdn.example/once.jpg", Extension: "jpg", ArchiveKey: "k"}
	if err := dl.Download(context.Background(), Job{Directory: t.TempDir()}, item, nil); err != nil {
		t.Fatal(err)
	}
	err = dl.Download(context.Background(), Job{Directory: t.TempDir()}, item, nil)
	if !errors.Is(err, ErrSkipped) {
		t.Fatalf("第二次得到 %v", err)
	}
	if srv.hits.Load() != 1 {
		t.Fatalf("归档没有挡住第二次下载: %d 次请求", srv.hits.Load())
	}
}

func TestDownloadUsesTemplate(t *testing.T) {
	srv := &script{steps: []http.HandlerFunc{ok("pixels", `"v"`, 0)}}
	dir := t.TempDir()
	dl := New(Options{Getter: srv, Template: BraceTemplate("{site}/{id}")})
	item := Item{
		URL:       "https://cdn.example/raw",
		Extension: "png",
		Metadata:  map[string]string{"site": "doubao", "id": "42"},
	}
	if err := dl.Download(context.Background(), Job{Directory: dir}, item, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "doubao", "42.png")); err != nil {
		t.Fatalf("模板路径没有生成: %v", err)
	}
}

func TestDownloadRejectsEscapingName(t *testing.T) {
	srv := &script{steps: []http.HandlerFunc{ok("x", "", 0)}}
	dl := New(Options{Getter: srv})
	err := dl.Download(context.Background(), Job{Directory: t.TempDir()}, Item{URL: "https://cdn.example/x", Filename: "../escape.txt"}, nil)
	var local *LocalError
	if !errors.As(err, &local) {
		t.Fatalf("得到 %v", err)
	}
	if srv.hits.Load() != 0 {
		t.Fatal("非法文件名仍发起了请求")
	}
}

func TestParseContentRange(t *testing.T) {
	span, err := parseContentRange("bytes 10-19/20")
	if err != nil || span.Start != 10 || span.End != 19 || span.Total != 20 {
		t.Fatalf("解析结果 %+v, %v", span, err)
	}
	if _, err := parseContentRange("bytes 10-19/*"); err == nil {
		t.Fatal("未知总长度应被拒绝")
	}
	if _, err := parseContentRange("bytes 10-9/20"); err == nil {
		t.Fatal("前后不一致的范围应被拒绝")
	}
}

func TestHostLimiterCapsConcurrency(t *testing.T) {
	limiter := NewHostLimiter(1)
	ctx := context.Background()
	release, err := limiter.Acquire(ctx, "https://cdn.example/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		second, err := limiter.Acquire(ctx, "https://cdn.example/b.jpg")
		if err != nil {
			t.Error(err)
		}
		second()
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("第二个请求在名额释放前就拿到了")
	case <-time.After(50 * time.Millisecond):
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("释放名额后第二个请求没有继续")
	}
}

func TestBandwidthLimiterSpacesReads(t *testing.T) {
	limiter := NewBandwidthLimiter(1000)
	start := time.Now()
	if err := limiter.WaitN(context.Background(), 1000); err != nil {
		t.Fatal(err)
	}
	if err := limiter.WaitN(context.Background(), 1000); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("两段 1000 字节在 %s 内都通过了，限速没有生效", elapsed)
	}
}

func TestLoadPartialDiscardsForeignFragment(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, "a.part")
	if err := os.WriteFile(part, []byte("fragment"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WritePartialMeta(part, PartialMeta{URL: "https://other.example/a", ETag: `"v"`, Total: 100}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPartial(part, "https://cdn.example/a")
	if err != nil || loaded != nil {
		t.Fatalf("不同地址的残片应被丢弃: %+v, %v", loaded, err)
	}
	if _, statErr := os.Stat(part); !os.IsNotExist(statErr) {
		t.Fatal("残片文件还在")
	}
}
