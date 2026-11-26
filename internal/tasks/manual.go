package tasks

import (
	"context"
	"fmt"
)

func (e *Engine) signalManualLocked() {
	if e.manualWake != nil {
		close(e.manualWake)
		e.manualWake = nil
	}
}

// SetAccountDisabled 和派发使用同一把锁，让已接手的任务完成，之后不再派发。
func (e *Engine) SetAccountDisabled(id int64, disabled bool) error {
	if e == nil || e.store == nil {
		return fmt.Errorf("没有配置库，无法更改账号状态")
	}
	e.mu.Lock()
	err := e.store.SetBrowserDisabled(id, disabled)
	if err == nil {
		e.signalManualLocked()
	}
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
	e.signalManualLocked()
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
// 人工接管期间通过条件通道即时唤醒，避免 100ms 轮询和反复全表查询。
func (e *Engine) enterAccount(ctx context.Context, id int64) bool {
	if id <= 0 {
		return ctx.Err() == nil
	}
	for ctx.Err() == nil {
		e.mu.Lock()
		if e.store != nil && e.store.IsBrowserDisabled(id) {
			e.mu.Unlock()
			return false
		}
		if !e.manual[id] {
			if e.active == nil {
				e.active = map[int64]bool{}
			}
			e.active[id] = true
			e.mu.Unlock()
			return true
		}
		if e.manualWake == nil {
			e.manualWake = make(chan struct{})
		}
		waitCh := e.manualWake
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-waitCh:
		}
	}
	return false
}
