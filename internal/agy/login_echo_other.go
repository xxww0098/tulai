//go:build !windows && !linux

package agy

import "os"

func silenceEcho(*os.File) {}
