//go:build darwin

package browser

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// adoptOpenedBrowser 等 open 退出，再按用户目录认领真正的 Chrome 进程。
// open 自己马上就退出，不能拿它的 pid 去停浏览器。
func adoptOpenedBrowser(cmd *exec.Cmd, userData string) (*os.Process, error) {
	waitErr := cmd.Wait()
	deadline := time.Now().Add(8 * time.Second)
	for {
		out, err := exec.Command("ps", "-axww", "-o", "pid=,command=").Output()
		if err == nil {
			if pid := pidFromPS(string(out), userData); pid > 0 {
				return os.FindProcess(pid)
			}
		}
		if !time.Now().Before(deadline) {
			if waitErr != nil {
				return nil, fmt.Errorf("后台启动浏览器失败: %w", waitErr)
			}
			return nil, errors.New("后台浏览器没有起来")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// watchOpened 等到这个不是我们直接拉起的进程退出，再关掉 done。
func watchOpened(pid int, done chan struct{}) {
	if done == nil {
		return
	}
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		if !processAlive(pid) {
			close(done)
			return
		}
	}
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
