// 每个来源站点的并发上限和共享冷却。
//
// 一个限制器给所有任务共用，避免一个大任务把同一个站点打满。
// 收到 429 或 Retry-After 时，整个站点一起冷却，不只是当前这一个请求。
package download

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"
)

type hostBudget struct {
	active       int
	blockedUntil time.Time
	waiters      []chan struct{}
}

// HostLimiter 限制每个站点同时进行的下载数。
type HostLimiter struct {
	mu    sync.Mutex
	limit int
	hosts map[string]*hostBudget
}

// NewHostLimiter 按每个站点的并发上限建一个限制器。小于 1 按 2 算。
func NewHostLimiter(limit int) *HostLimiter {
	if limit <= 0 {
		limit = 2
	}
	return &HostLimiter{limit: limit, hosts: map[string]*hostBudget{}}
}

// SetLimit 改每个站点的并发上限，范围是 1 到 16，并唤醒正在等待的请求。
func (h *HostLimiter) SetLimit(limit int) int {
	if limit < 1 {
		limit = 1
	}
	if limit > 16 {
		limit = 16
	}
	h.mu.Lock()
	h.limit = limit
	for _, budget := range h.hosts {
		h.notify(budget)
	}
	h.mu.Unlock()
	return limit
}

// Acquire 等到 rawURL 所属站点有一个空位，返回一个可重复调用的释放函数。
func (h *HostLimiter) Acquire(ctx context.Context, rawURL string) (func(), error) {
	host := canonicalHost(rawURL)
	if host == "" {
		return nil, &LocalError{Err: errInvalidHost(rawURL)}
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		h.mu.Lock()
		budget := h.hosts[host]
		if budget == nil {
			budget = &hostBudget{}
			h.hosts[host] = budget
		}
		cooldown := time.Until(budget.blockedUntil)
		if budget.active < h.limit && cooldown <= 0 {
			budget.active++
			h.mu.Unlock()
			var once sync.Once
			return func() { once.Do(func() { h.release(host) }) }, nil
		}
		ch := make(chan struct{})
		budget.waiters = append(budget.waiters, ch)
		h.mu.Unlock()
		if err := h.wait(ctx, ch, cooldown); err != nil {
			return nil, err
		}
	}
}

func (h *HostLimiter) wait(ctx context.Context, ch chan struct{}, cooldown time.Duration) error {
	var timer <-chan time.Time
	if cooldown > 0 {
		t := time.NewTimer(cooldown)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	case <-timer:
		return nil
	}
}

func (h *HostLimiter) release(host string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	budget := h.hosts[host]
	if budget == nil {
		return
	}
	budget.active--
	h.notify(budget)
}

// Cooldown 让 rawURL 所属站点在 delay 内不再开始新的下载。
func (h *HostLimiter) Cooldown(rawURL string, delay time.Duration) {
	if delay <= 0 {
		return
	}
	host := canonicalHost(rawURL)
	if host == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	budget := h.hosts[host]
	if budget == nil {
		budget = &hostBudget{}
		h.hosts[host] = budget
	}
	until := time.Now().Add(delay)
	if until.After(budget.blockedUntil) {
		budget.blockedUntil = until
		// 新的冷却让旧的等待失效，等待方重新排队。
		h.notify(budget)
	}
}

func (h *HostLimiter) notify(budget *hostBudget) {
	waiters := budget.waiters
	budget.waiters = nil
	for _, ch := range waiters {
		close(ch)
	}
}

// canonicalHost 把地址归一成小写的主机名，用来把同一个站点归到一个名额下。
func canonicalHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	return host
}

type hostError string

func (e hostError) Error() string { return string(e) }

func errInvalidHost(rawURL string) error {
	return hostError("地址没有有效的主机名: " + rawURL)
}
