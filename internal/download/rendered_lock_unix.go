//go:build !windows

package download

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func acquireRenderedProfile(profileDir string) (func(), error) {
	f, err := renderedLockFile(profileDir)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errors.New("下载浏览器目录正在使用")
		}
		return nil, fmt.Errorf("锁定下载浏览器目录: %w", err)
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}
