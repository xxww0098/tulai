//go:build !windows

package browser

// shortcut: 只做了 Windows；其他系统启动显示窗口的任务浏览器时可能抢前台。
func setForegroundLock(bool) {}
