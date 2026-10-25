package extbridge

import (
	"context"
	"encoding/json"
	"sync"
)

var (
	acceptRPC  = map[string]struct{}{TypeAck: {}, TypeEvent: {}, TypeError: {}}
	acceptPong = map[string]struct{}{TypePong: {}}
)

// Call 发送一条命令并等待相同 id 的 ack、event 或 error。
// 相同 idempotencyKey 的已完成结果会直接返回，不再发给扩展。
// 进行中的相同键会并到同一次等待。上下文超时返回 E_TIMEOUT，不写入缓存。
func (s *Session) Call(ctx context.Context, env Envelope) (Envelope, error) {
	env.Type = TypeCmd
	return s.roundTrip(ctx, env, acceptRPC)
}

// Ping 发送协议 ping 并等待 pong。
func (s *Session) Ping(ctx context.Context) error {
	_, err := s.roundTrip(ctx, Envelope{Type: TypePing}, acceptPong)
	return err
}

func (s *Session) roundTrip(ctx context.Context, env Envelope, accept map[string]struct{}) (Envelope, error) {
	select {
	case <-s.done:
		return Envelope{}, &ProtoError{Code: ENotConnected, Message: "扩展连接已关闭"}
	default:
	}
	if err := ctx.Err(); err != nil {
		return Envelope{}, timeoutErr(err)
	}
	env, err := s.prepare(env)
	if err != nil {
		return Envelope{}, err
	}
	var slot *rpcSlot
	key := env.IdempotencyKey
	if key != "" {
		cached, done, joined, leader := s.cache.join(key, env.ID)
		if done {
			return cached, terminalError(cached)
		}
		slot = joined
		if !leader {
			return s.waitSlot(ctx, slot, key, "")
		}
	} else {
		slot = newRPCSlot()
	}
	s.registerWait(env.ID, &waiter{
		accept: accept,
		fn: func(got Envelope) {
			if key != "" {
				s.cache.complete(key, got)
				return
			}
			slot.fulfill(got, nil)
		},
		fail: func(cause error) {
			if key != "" {
				s.cache.fail(key, cause)
				return
			}
			slot.fulfill(Envelope{}, cause)
		},
	})
	if err := s.writeEnvelope(env); err != nil {
		s.unregister(env.ID)
		if key != "" {
			s.cache.fail(key, err)
		} else {
			slot.fulfill(Envelope{}, err)
		}
		return Envelope{}, err
	}
	return s.waitSlot(ctx, slot, key, env.ID)
}

func (s *Session) prepare(env Envelope) (Envelope, error) {
	if env.V == 0 {
		env.V = Version
	}
	if env.Type == "" {
		env.Type = TypeCmd
	}
	if env.ID == "" {
		env.ID = newID()
	}
	if env.TS == 0 {
		env.TS = nowMS()
	}
	env.Platform = s.platform
	env.SessionID = s.id
	if err := env.Validate(); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

func (s *Session) waitSlot(ctx context.Context, slot *rpcSlot, key, id string) (Envelope, error) {
	select {
	case <-slot.ready:
		return slot.result()
	case <-ctx.Done():
		select {
		case <-slot.ready:
			return slot.result()
		default:
		}
		s.dropIfLast(key, id)
		select {
		case <-slot.ready:
			return slot.result()
		default:
			return Envelope{}, timeoutErr(ctx.Err())
		}
	case <-s.done:
		select {
		case <-slot.ready:
			return slot.result()
		default:
			return Envelope{}, &ProtoError{Code: ENotConnected, Message: "扩展连接已关闭"}
		}
	}
}

func (s *Session) dropIfLast(key, id string) {
	if key == "" {
		s.unregister(id)
		return
	}
	if dropID, drop := s.cache.leave(key); drop {
		s.unregister(dropID)
	}
}

func timeoutErr(err error) error {
	message := "等待扩展响应超时"
	if err != nil {
		message = "等待扩展响应超时: " + err.Error()
	}
	return &ProtoError{Code: ETimeout, Message: message}
}

type rpcSlot struct {
	ready chan struct{}
	env   Envelope
	err   error
	once  sync.Once
}

func newRPCSlot() *rpcSlot {
	return &rpcSlot{ready: make(chan struct{})}
}

func (sl *rpcSlot) fulfill(env Envelope, err error) {
	sl.once.Do(func() {
		sl.env = env
		sl.err = err
		close(sl.ready)
	})
}

func (sl *rpcSlot) result() (Envelope, error) {
	if sl.err != nil {
		return sl.env, sl.err
	}
	return sl.env, terminalError(sl.env)
}

type cacheEntry struct {
	id      string
	waiters int
	done    bool
	env     Envelope
	slot    *rpcSlot
}

type idempCache struct {
	mu    sync.Mutex
	limit int
	order []string
	items map[string]*cacheEntry
}

func newIdempCache(limit int) *idempCache {
	if limit <= 0 {
		limit = defaultCacheLimit
	}
	return &idempCache{limit: limit, items: map[string]*cacheEntry{}}
}

// join 登记一个幂等键。done 为真时直接复用上次结果。leader 负责真正发送。
func (c *idempCache) join(key, id string) (env Envelope, done bool, slot *rpcSlot, leader bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[key]; ok {
		if e.done {
			return e.env, true, nil, false
		}
		e.waiters++
		return Envelope{}, false, e.slot, false
	}
	sl := newRPCSlot()
	c.items[key] = &cacheEntry{id: id, waiters: 1, slot: sl}
	return Envelope{}, false, sl, true
}

func (c *idempCache) complete(key string, env Envelope) {
	c.mu.Lock()
	e := c.items[key]
	if e == nil || e.done {
		c.mu.Unlock()
		return
	}
	sl := e.slot
	// 滑块可能在这次应答之后被 CDP 拖掉。记住 E_CAPTCHA_HUMAN 会让同一次发送无法再确认。
	if captchaHumanAck(env) {
		delete(c.items, key)
		c.mu.Unlock()
		if sl != nil {
			sl.fulfill(env, nil)
		}
		return
	}
	e.done = true
	e.env = env
	e.slot = nil
	c.remember(key)
	c.mu.Unlock()
	if sl != nil {
		sl.fulfill(env, nil)
	}
}

func captchaHumanAck(env Envelope) bool {
	if env.Type != TypeAck || len(env.Payload) == 0 {
		return false
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(env.Payload, &body); err != nil {
		return false
	}
	return body.Code == ECaptchaHuman
}

func (c *idempCache) fail(key string, err error) {
	c.mu.Lock()
	e := c.items[key]
	if e == nil || e.done {
		c.mu.Unlock()
		return
	}
	sl := e.slot
	delete(c.items, key)
	c.mu.Unlock()
	if sl != nil {
		sl.fulfill(Envelope{}, err)
	}
}

// leave 减少等待者。最后一个离开且结果尚未回来时，删掉进行中的记录并返回命令 id。
func (c *idempCache) leave(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.items[key]
	if e == nil || e.done {
		return "", false
	}
	e.waiters--
	if e.waiters > 0 {
		return "", false
	}
	id := e.id
	delete(c.items, key)
	return id, true
}

func (c *idempCache) remember(key string) {
	c.order = append(c.order, key)
	for len(c.order) > c.limit {
		old := c.order[0]
		c.order = c.order[1:]
		if e := c.items[old]; e != nil && e.done {
			delete(c.items, old)
		}
	}
}
