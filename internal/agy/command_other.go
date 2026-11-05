//go:build !windows

package agy

import "os/exec"

func configureCommand(*exec.Cmd) {}
