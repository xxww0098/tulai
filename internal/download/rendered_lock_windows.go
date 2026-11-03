package download

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

func acquireRenderedProfile(profileDir string) (func(), error) {
	f, err := renderedLockFile(profileDir)
	if err != nil {
		return nil, err
	}
	handle := windows.Handle(f.Fd())
	var overlapped windows.Overlapped
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped); err != nil {
		_ = f.Close()
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING) {
			return nil, errors.New("下载浏览器目录正在使用")
		}
		return nil, fmt.Errorf("锁定下载浏览器目录: %w", err)
	}
	return func() {
		_ = windows.UnlockFileEx(handle, 0, 1, 0, &overlapped)
		_ = f.Close()
	}, nil
}
