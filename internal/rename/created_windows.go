package rename

import (
	"errors"
	"os"
	"syscall"
	"time"
)

func creationTime(path string) (time.Time, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return time.Time{}, err
	}
	stat, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return time.Time{}, errors.New("文件系统不提供创建时间")
	}
	return time.Unix(0, stat.CreationTime.Nanoseconds()), nil
}
