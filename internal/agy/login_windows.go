//go:build windows

package agy

import (
	"errors"
	"time"
)

// Login 在当前系统上不能启动。
type Login struct{}

var errLoginOS = errors.New("当前系统不能在这里完成 Antigravity 登录")

// StartLogin 在 Windows 上直接返回说明。
func StartLogin(string, string, string) (*Login, error) { return nil, errLoginOS }

func startLogin(string, string, string, time.Duration) (*Login, error) {
	return nil, errLoginOS
}

func (l *Login) URL() string { return "" }

func (l *Login) Submit(string) error { return errLoginOS }

func (l *Login) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (l *Login) Wait() error { return errLoginOS }

func (l *Login) Close() {}
