//go:build windows

package browser

import "syscall"

func testProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func killTree(int) {}
