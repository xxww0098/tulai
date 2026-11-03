package browser

import "time"

// 出图冷启动后，窗口管理器可能晚一拍才把 Camoufox 映到 tulai 上面。
// 端口就绪后再补这几下，把 tulai 自己的窗口拉回来。
var appFocusWaits = []time.Duration{
	300 * time.Millisecond,
	800 * time.Millisecond,
	1600 * time.Millisecond,
}

// KeepAppInFront 立刻把 tulai 放到前面，并在冷启动窗口真正出现后再补几次。
// 没有接上界面时什么都不做。登录路径不要调用。
func (m *Manager) KeepAppInFront() {
	if m == nil || m.focusFn() == nil {
		return
	}
	m.refocusApp()
	waits := append([]time.Duration(nil), appFocusWaits...)
	go func() {
		for _, wait := range waits {
			time.Sleep(wait)
			m.refocusApp()
		}
	}()
}

func (m *Manager) refocusApp() {
	fn := m.focusFn()
	if fn != nil {
		fn()
	}
}

func (m *Manager) focusFn() func() {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.focusApp
}
