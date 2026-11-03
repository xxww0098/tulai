// 下载错误分类和重试等待。
//
// 传输错误和完整性错误可以重试：最终文件还没被改动。
// 本地错误是磁盘或配置问题，再发一次请求也解决不了，所以不重试。
// HTTP 状态错误只在限流和服务器故障时重试，并优先用服务器给的等待时间。
package download

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TransferError 是网络读取或请求失败。重试是安全的。
type TransferError struct{ Err error }

func (e *TransferError) Error() string { return "下载传输失败: " + e.Err.Error() }
func (e *TransferError) Unwrap() error { return e.Err }

// IntegrityError 是收到的内容和声明的范围或长度不符。目标文件没有被改过，重试是安全的。
type IntegrityError struct{ Err error }

func (e *IntegrityError) Error() string { return "下载内容不符: " + e.Err.Error() }
func (e *IntegrityError) Unwrap() error { return e.Err }

// LocalError 是本地文件系统或配置的永久性失败。它不能再触发一次网络下载。
type LocalError struct{ Err error }

func (e *LocalError) Error() string { return "下载本地失败: " + e.Err.Error() }
func (e *LocalError) Unwrap() error { return e.Err }

// HTTPStatusError 保留响应状态码和服务器要求的冷却时间。
type HTTPStatusError struct {
	URL        string
	StatusCode int
	RetryAfter time.Duration
}

func (e *HTTPStatusError) Error() string {
	return "下载被服务器拒绝: HTTP " + strconv.Itoa(e.StatusCode)
}

// Retryable 判断这个状态码是限流或故障，而不是一个确定的拒绝。
func (e *HTTPStatusError) Retryable() bool {
	switch e.StatusCode {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	}
	return e.StatusCode >= 500
}

// ErrSkipped 表示这个条目被故意跳过：归档里已有它的稳定键，或已有文件按策略保留。
var ErrSkipped = errors.New("下载已跳过: 文件已存在")

// ErrFiltered 表示图片不符合大小或尺寸条件，未保存。
var ErrFiltered = errors.New("图片不符合筛选条件")

// ShouldRetry 判断这次失败是否值得再试一次。取消不是失败，不重试。
func ShouldRetry(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var status *HTTPStatusError
	if errors.As(err, &status) {
		return status.Retryable()
	}
	var transfer *TransferError
	if errors.As(err, &transfer) {
		return true
	}
	var integrity *IntegrityError
	return errors.As(err, &integrity)
}

const maxAttemptShift = 6

// RetryDelay 优先用服务器给的等待时间，否则用带全抖动的指数退避。
// 全抖动避免一批任务在同一时刻一起重试。
func RetryDelay(err error, attempt int, base time.Duration) time.Duration {
	var status *HTTPStatusError
	if errors.As(err, &status) && status.RetryAfter > 0 {
		return status.RetryAfter
	}
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	if attempt > maxAttemptShift {
		attempt = maxAttemptShift
	}
	delay := base << attempt
	if delay <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(delay))) + time.Nanosecond
}

// ParseRetryAfter 解析 Retry-After，支持秒数和 HTTP 日期两种写法。
func ParseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

// Wait 在 ctx 取消时立即返回，否则等到 delay 结束。
func Wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
