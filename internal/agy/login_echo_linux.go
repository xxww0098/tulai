//go:build linux

package agy

import (
	"os"

	"golang.org/x/sys/unix"
)

func silenceEcho(f *os.File) {
	if f == nil {
		return
	}
	term, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
	if err != nil {
		return
	}
	term.Lflag &^= unix.ECHO | unix.ECHOE | unix.ECHOK | unix.ECHONL
	_ = unix.IoctlSetTermios(int(f.Fd()), unix.TCSETS, term)
}
