//go:build !windows

package browser

import "syscall"

func testProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func killTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
