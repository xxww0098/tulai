package download

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// DefaultMaxFetchBytes 是只取字节时单个结果的上限，和结果图校验的上限一致。
const DefaultMaxFetchBytes = 64 << 20

// Fetcher 把重试、站点并发和限速收在一起，给只想要整段字节的调用方用。
type Fetcher struct {
	Getter    Getter
	Host      *HostLimiter
	Bandwidth *BandwidthLimiter
	Retries   int
	RetryBase time.Duration
	MaxBytes  int64
}

// FetchBytes 下载一个地址的全部内容。
func (f *Fetcher) FetchBytes(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	if f.Getter == nil {
		return nil, &LocalError{Err: errors.New("没有配置下载客户端")}
	}
	limit := f.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxFetchBytes
	}
	var last error
	for attempt := 0; attempt <= f.Retries; attempt++ {
		data, err := f.once(ctx, rawURL, headers, limit)
		if err == nil {
			return data, nil
		}
		last = err
		if attempt == f.Retries || ctx.Err() != nil || !ShouldRetry(err) {
			return nil, err
		}
		delay := RetryDelay(err, attempt, f.RetryBase)
		var status *HTTPStatusError
		if errors.As(err, &status) && f.Host != nil {
			target := status.URL
			if target == "" {
				target = rawURL
			}
			f.Host.Cooldown(target, delay)
		}
		if waitErr := Wait(ctx, delay); waitErr != nil {
			return nil, waitErr
		}
	}
	return nil, last
}

func (f *Fetcher) once(ctx context.Context, rawURL string, headers map[string]string, limit int64) ([]byte, error) {
	release := func() {}
	if f.Host != nil {
		var err error
		release, err = f.Host.Acquire(ctx, rawURL)
		if err != nil {
			return nil, err
		}
	}
	defer release()

	resp, err := f.Getter.Get(ctx, rawURL, func(r *http.Request) {
		for k, v := range headers {
			r.Header.Set(k, v)
		}
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &TransferError{Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPStatusError{
			URL:        resp.FinalURL,
			StatusCode: resp.StatusCode,
			RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		}
	}
	if length := contentLength(resp); length > limit {
		return nil, &LocalError{Err: errors.New("结果图超过大小上限")}
	}

	var buf bytes.Buffer
	chunk := make([]byte, 32*1024)
	var written int64
	for {
		readSize := len(chunk)
		if f.Bandwidth != nil {
			readSize = f.Bandwidth.ReadSize(readSize)
			if err := f.Bandwidth.WaitN(ctx, readSize); err != nil {
				return nil, err
			}
		}
		n, readErr := resp.Body.Read(chunk[:readSize])
		if n > 0 {
			written += int64(n)
			if written > limit {
				return nil, &LocalError{Err: errors.New("结果图超过大小上限")}
			}
			buf.Write(chunk[:n])
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &TransferError{Err: readErr}
		}
	}
	if length := contentLength(resp); length >= 0 && written != length {
		return nil, &IntegrityError{Err: errors.New("下载长度与 Content-Length 不符")}
	}
	return buf.Bytes(), nil
}
