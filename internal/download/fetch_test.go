package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchBytesDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != DefaultUserAgent {
			t.Errorf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("png-bytes"))
	}))
	defer srv.Close()

	f := &Fetcher{Getter: NewHTTPGetter("")}
	data, err := f.FetchBytes(context.Background(), srv.URL+"/a.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "png-bytes" {
		t.Fatalf("data = %q", data)
	}
}

func TestFetchBytesRetriesServerErrors(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write([]byte("recovered"))
	}))
	defer srv.Close()

	f := &Fetcher{Getter: NewHTTPGetter(""), Retries: 1, RetryBase: time.Millisecond}
	data, err := f.FetchBytes(context.Background(), srv.URL+"/a.png", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "recovered" || hits.Load() != 2 {
		t.Fatalf("data=%q hits=%d", data, hits.Load())
	}
}

func TestFetchBytesDoesNotRetryNotFound(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	f := &Fetcher{Getter: NewHTTPGetter(""), Retries: 3, RetryBase: time.Millisecond}
	_, err := f.FetchBytes(context.Background(), srv.URL+"/missing.png", nil)
	var status *HTTPStatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusNotFound {
		t.Fatalf("err = %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("404 被重试了 %d 次", hits.Load())
	}
}

func TestFetchBytesRejectsOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2048")
		w.Write(make([]byte, 2048))
	}))
	defer srv.Close()

	f := &Fetcher{Getter: NewHTTPGetter(""), MaxBytes: 1024}
	_, err := f.FetchBytes(context.Background(), srv.URL+"/big.png", nil)
	var local *LocalError
	if !errors.As(err, &local) || !strings.Contains(err.Error(), "上限") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchBytesDetectsTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("short"))
	}))
	defer srv.Close()

	f := &Fetcher{Getter: NewHTTPGetter("")}
	_, err := f.FetchBytes(context.Background(), srv.URL+"/cut.png", nil)
	// 客户端读到 unexpected EOF 时按传输错误处理，和长度不符一样是可重试的。
	if err == nil || !ShouldRetry(err) {
		t.Fatalf("截断必须报成可重试错误: %v", err)
	}
}

func TestFetchBytesHonorsCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &Fetcher{Getter: NewHTTPGetter(""), Retries: 3}
	_, err := f.FetchBytes(ctx, srv.URL+"/a.png", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
