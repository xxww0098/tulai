// 任务级的总带宽限制。一个任务共用一个限制器，配置的速率是全部并发的总和，不是每个连接一份。
package download

import (
	"context"
	"sync"
	"time"
)

// BandwidthLimiter 按预约发放字节额度。调用方每次最多读 readSize 个字节，避免一次读出一大段。
type BandwidthLimiter struct {
	mu             sync.Mutex
	bytesPerSecond float64
	next           time.Time
}

// NewBandwidthLimiter 按每秒字节数建一个限制器。
func NewBandwidthLimiter(bytesPerSecond int64) *BandwidthLimiter {
	return &BandwidthLimiter{bytesPerSecond: float64(bytesPerSecond)}
}

// WaitN 预约 bytes 个字节，等到它们的时间片到来才返回。
func (b *BandwidthLimiter) WaitN(ctx context.Context, bytes int) error {
	if bytes <= 0 {
		return nil
	}
	b.mu.Lock()
	duration := time.Duration(float64(bytes) / b.bytesPerSecond * float64(time.Second))
	now := time.Now()
	start := b.next
	if now.After(start) {
		start = now
	}
	b.next = start.Add(duration)
	b.mu.Unlock()
	return Wait(ctx, start.Sub(now))
}

// ReadSize 把一次读取限制在最多 100 毫秒的量，并且不超过 16KiB。
func (b *BandwidthLimiter) ReadSize(maximum int) int {
	if maximum <= 0 {
		return maximum
	}
	size := int(b.bytesPerSecond / 10)
	if size < 1 {
		size = 1
	}
	if size > 16*1024 {
		size = 16 * 1024
	}
	if size > maximum {
		size = maximum
	}
	return size
}
