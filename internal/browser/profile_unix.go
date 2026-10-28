//go:build !windows

package browser

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// releaseProfile 结束仍占用这个用户数据目录的浏览器进程。
func releaseProfile(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return
	}
	self := os.Getpid()
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || pid == self {
			continue
		}
		raw, err := os.ReadFile("/proc/" + entry.Name() + "/cmdline")
		if err != nil || !cmdlineUsesProfile(raw, dir) {
			continue
		}
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func cmdlineUsesProfile(raw []byte, dir string) bool {
	needle := "--user-data-dir=" + dir
	for _, part := range strings.Split(string(raw), "\x00") {
		if part == needle {
			return true
		}
	}
	return false
}
