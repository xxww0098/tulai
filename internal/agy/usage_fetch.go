package agy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	// UsageTimeout 是读额度的上限。命令本来立刻返回，超时就当失败。
	UsageTimeout = 20 * time.Second
)

var (
	errUsageData     = errors.New("额度数据无法识别")
	errUsageGemini   = errors.New("没有 Gemini 额度")
	errUsageTimeout  = errors.New("超时")
	errUsageBin      = errors.New("找不到 agy")
	errUsageEmpty    = errors.New("agy 没有返回额度")
	errUsageCanceled = errors.New("context canceled")
)

// FetchUsage 在账号 HOME 里执行 agy -p /usage。不读凭证内容，也不往标准输入写任何东西。
// 输出里一出现 Authentication required 就杀掉进程：它会停在那里等授权码。
func FetchUsage(ctx context.Context, bin, home string) (GeminiUsage, error) {
	return fetchUsage(ctx, bin, home, UsageTimeout)
}

func fetchUsage(ctx context.Context, bin, home string, timeout time.Duration) (GeminiUsage, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	bin = strings.TrimSpace(bin)
	if bin == "" {
		bin = DefaultBin()
	}
	if _, err := os.Stat(bin); err != nil {
		return GeminiUsage{}, errUsageBin
	}
	home = strings.TrimSpace(home)
	if home == "" {
		return GeminiUsage{}, errors.New("没有 Antigravity 账号目录")
	}
	if timeout <= 0 {
		timeout = UsageTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	work, err := os.MkdirTemp("", "agy-usage-")
	if err != nil {
		return GeminiUsage{}, errUsageEmpty
	}
	defer os.RemoveAll(work)
	cmd := exec.CommandContext(ctx, bin, "-p", "/usage", "--output-format", "json")
	cmd.Dir = work
	cmd.Env = envWithHome(home)
	// nil 标准输入接到空设备，不会把授权码或别的内容送进去。
	cmd.Stdin = nil
	out, err := captureUsage(ctx, cmd)
	if bytes.Contains(out, []byte("Authentication required")) {
		return GeminiUsage{}, ErrNeedLogin
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return GeminiUsage{}, errUsageTimeout
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return GeminiUsage{}, errUsageCanceled
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return GeminiUsage{}, errUsageEmpty
	}
	return ParseUsage(out)
}

// captureUsage 边读边看登录提示。一出现就杀掉进程，不等它读授权码。
func captureUsage(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, errUsageEmpty
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, errUsageEmpty
	}
	if err := cmd.Start(); err != nil {
		return nil, errUsageEmpty
	}
	var buf bytes.Buffer
	var mu sync.Mutex
	auth := make(chan struct{}, 1)
	note := func(p []byte) {
		if len(p) == 0 {
			return
		}
		mu.Lock()
		if buf.Len() < 2<<20 {
			buf.Write(p)
		}
		hit := bytes.Contains(buf.Bytes(), []byte("Authentication required"))
		mu.Unlock()
		if !hit {
			return
		}
		select {
		case auth <- struct{}{}:
		default:
		}
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	var wg sync.WaitGroup
	read := func(r io.Reader) {
		defer wg.Done()
		tmp := make([]byte, 4096)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, tmp[:n])
				note(chunk)
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go read(stdout)
	go read(stderr)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-auth:
	case <-done:
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	}
	_ = cmd.Wait()
	<-done
	mu.Lock()
	defer mu.Unlock()
	return append([]byte(nil), buf.Bytes()...), nil
}
