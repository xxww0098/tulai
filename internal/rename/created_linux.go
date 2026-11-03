package rename

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

func creationTime(path string) (time.Time, error) {
	var stat unix.Statx_t
	if err := unix.Statx(unix.AT_FDCWD, path, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BTIME, &stat); err != nil {
		return time.Time{}, err
	}
	if stat.Mask&unix.STATX_BTIME == 0 {
		return time.Time{}, errors.New("文件系统不提供创建时间")
	}
	return time.Unix(stat.Btime.Sec, int64(stat.Btime.Nsec)), nil
}
