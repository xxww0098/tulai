package rename

import (
	"time"

	"golang.org/x/sys/unix"
)

func creationTime(path string) (time.Time, error) {
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return time.Time{}, err
	}
	return time.Unix(stat.Btim.Sec, stat.Btim.Nsec), nil
}
