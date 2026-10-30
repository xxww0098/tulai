//go:build !windows

package agy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

// Login 是一次在伪终端里的授权。授权码只写进终端，不留在结构体里。
type Login struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	ptmx     *os.File
	home     string
	dir      string
	url      string
	err      error
	finished bool
	done     chan struct{}
}

// StartLogin 用和出图相同的命令拉起登录提示。
func StartLogin(bin, home, model string) (*Login, error) {
	return startLogin(bin, home, model, LoginTimeout)
}

func startLogin(bin, home, model string, timeout time.Duration) (*Login, error) {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		bin = DefaultBin()
	}
	if _, err := os.Stat(bin); err != nil {
		return nil, fmt.Errorf("找不到 agy：%s", bin)
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return nil, errors.New("没有 Antigravity 账号目录")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}
	if timeout <= 0 {
		timeout = LoginTimeout
	}
	work, err := os.MkdirTemp("", "agy-login-")
	if err != nil {
		return nil, fmt.Errorf("创建临时目录失败: %w", err)
	}
	cmd := exec.Command(bin, "-p", Instruction("登录", ""), "--output-format", "stream-json", "--model", model)
	cmd.Dir = work
	cmd.Env = envWithHome(home)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		_ = os.RemoveAll(work)
		return nil, fmt.Errorf("无法启动登录: %w", err)
	}
	silenceEcho(ptmx)
	l := &Login{
		cmd:  cmd,
		ptmx: ptmx,
		home: home,
		dir:  work,
		done: make(chan struct{}),
	}
	go l.read()
	go l.watch(timeout)
	return l, nil
}

func (l *Login) read() {
	sc := bufio.NewScanner(l.ptmx)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	sawPhrase := false
	for sc.Scan() {
		line := sc.Text()
		if url := AuthURL(line); url != "" {
			l.mu.Lock()
			if l.url == "" {
				l.url = url
			}
			l.mu.Unlock()
		}
		if strings.Contains(line, "authenticated successfully") {
			sawPhrase = true
		}
		if sawPhrase || LoggedIn(l.home) {
			if l.waitToken(2 * time.Second) {
				l.finish(nil)
				return
			}
		}
	}
	if LoggedIn(l.home) || (sawPhrase && l.waitToken(2*time.Second)) {
		l.finish(nil)
		return
	}
	l.finish(errors.New("登录未完成，请点重试"))
}

func (l *Login) waitToken(limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for {
		if LoggedIn(l.home) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func (l *Login) watch(timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-l.done:
	case <-timer.C:
		l.finish(errors.New("登录超时，请点重试"))
	}
}

// URL 是目前看到的授权链接。
func (l *Login) URL() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.url
}

// Submit 把授权码写进终端并补一个换行。调用方不要再把这段码写进日志。
func (l *Login) Submit(code string) error {
	if l == nil || l.ptmx == nil {
		return errors.New("登录还没开始，请点重试")
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return errors.New("请先粘贴授权码")
	}
	l.mu.Lock()
	ptmx := l.ptmx
	finished := l.finished
	l.mu.Unlock()
	if finished || ptmx == nil {
		return errors.New("登录还没开始，请点重试")
	}
	_, err := io.WriteString(ptmx, code+"\n")
	if err != nil {
		return errors.New("提交失败，请重试")
	}
	return nil
}

// Done 在登录结束时关闭。
func (l *Login) Done() <-chan struct{} {
	if l == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return l.done
}

// Wait 等到登录结束。
func (l *Login) Wait() error {
	if l == nil {
		return errors.New("登录还没开始，请点重试")
	}
	<-l.done
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// Close 结束这次登录。
func (l *Login) Close() {
	if l == nil {
		return
	}
	l.finish(errors.New("已取消登录"))
}

func (l *Login) finish(err error) {
	l.mu.Lock()
	if l.finished {
		l.mu.Unlock()
		return
	}
	l.finished = true
	l.err = err
	ptmx := l.ptmx
	cmd := l.cmd
	dir := l.dir
	l.ptmx = nil
	l.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if ptmx != nil {
		_ = ptmx.Close()
	}
	if cmd != nil {
		_ = cmd.Wait()
	}
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	close(l.done)
}
