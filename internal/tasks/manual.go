package tasks

import (
	"context"
	"fmt"
	"time"
)

// SetAccountDisabled 和派发使用同一把锁，让已接手的任务完成，之后不再派发。
func (e *Engine) SetAccountDisabled(id int64, disabled bool) error {
	if e == nil || e.store == nil {
		return fmt.Errorf("没有配置库，无法更改账号状态")
	}
	e.mu.Lock()
	err := e.store.SetBrowserDisabled(id, disabled)
	e.mu.Unlock()
	if err == nil {
		e.Wake()
	}
	return err
}

// SetAccountManual 暂停这个账号的新任务；当前任务等待验证时也可手动操作。
func (e *Engine) SetAccountManual(id int64, on bool) {
	if e == nil || id <= 0 {
		return
	}
	e.mu.Lock()
	if e.manual == nil {
		e.manual = map[int64]bool{}
	}
	if on {
		e.manual[id] = true
	} else {
		delete(e.manual, id)
	}
	e.mu.Unlock()
	e.Wake()
}

func (e *Engine) AccountManual(id int64) bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.manual[id]
}

// AccountAwaitingUser permits manual input while a task is paused for verification.
func (e *Engine) AccountAwaitingUser(id int64) bool {
	if e == nil || id <= 0 {
		return false
	}
	e.liveMu.Lock()
	defer e.liveMu.Unlock()
	return e.clock.verify[id]
}

// enterAccount 把接管检查和任务占用放在同一把锁里，避免点击接管与派发同时发生。
func (e *Engine) enterAccount(ctx context.Context, id int64) bool {
	if id <= 0 {
		return ctx.Err() == nil
	}
	for ctx.Err() == nil {
		e.mu.Lock()
		if e.store != nil {
			for _, b := range e.store.Browsers() {
				if b.ID == id && b.Disabled {
					e.mu.Unlock()
					return false
				}
			}
		}
		if !e.manual[id] {
			if e.active == nil {
				e.active = map[int64]bool{}
			}
			e.active[id] = true
			e.mu.Unlock()
			return true
		}
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
	return false
}
