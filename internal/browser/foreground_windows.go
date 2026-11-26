//go:build windows

package browser

import "golang.org/x/sys/windows"

var lockSetForegroundWindow = windows.NewLazySystemDLL("user32.dll").NewProc("LockSetForegroundWindow")

// setForegroundLock 调 LockSetForegroundWindow：上锁后其他进程（含刚启动的 Fingerprint）不能把窗口抢到前台。
// 只有前台进程能上锁；tulai 不在前台时调用失败，这时新窗口本来也抢不到前台。
// 用户按 Alt 或点别的窗口时系统会自动解锁。
func setForegroundLock(on bool) {
	code := uintptr(2) // LSFW_UNLOCK
	if on {
		code = 1 // LSFW_LOCK
	}
	if lockSetForegroundWindow.Find() != nil {
		return
	}
	_, _, _ = lockSetForegroundWindow.Call(code)
}
