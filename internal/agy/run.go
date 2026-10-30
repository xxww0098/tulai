package agy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Request 是一次出图。Prompt 已经是给代理的整段说明。
// Ref 非空时，Run 把参考图复制进临时工作目录，并把说明里的原路径换成复制后的绝对路径。
// agy 只允许读 cwd 里的文件，工作目录外的参考图会被拒绝。
type Request struct {
	Bin    string
	Home   string
	Model  string
	Prompt string
	Ref    string
}

type taggedLine struct {
	text   string
	stderr bool
}

// Run 在账号 HOME 里执行 agy，并收走生成的图片路径。
// 退出码即使是 0 也可能失败，要看状态和文件。
func Run(ctx context.Context, req Request) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	bin := strings.TrimSpace(req.Bin)
	if bin == "" {
		bin = DefaultBin()
	}
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("找不到 agy：%s", bin)
	}
	home := strings.TrimSpace(req.Home)
	if home == "" {
		return "", errors.New("没有 Antigravity 账号目录")
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = DefaultModel
	}
	if !LoggedIn(home) {
		return "", ErrNeedLogin
	}
	work, err := os.MkdirTemp("", "agy-run-")
	if err != nil {
		return "", fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(work)
	prompt, err := promptWithReference(work, req.Prompt, req.Ref)
	if err != nil {
		return "", err
	}

	started := time.Now()
	cmd := exec.CommandContext(ctx, bin, "-p", prompt, "--output-format", "stream-json", "--model", model)
	cmd.Dir = work
	cmd.Env = envWithHome(home)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("出图失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("出图失败: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("出图失败: %w", err)
	}
	lines := make(chan taggedLine, 64)
	var wg sync.WaitGroup
	scan := func(r io.Reader, isErr bool) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 2<<20)
		for sc.Scan() {
			lines <- taggedLine{text: sc.Text(), stderr: isErr}
		}
	}
	wg.Add(2)
	go scan(stdout, false)
	go scan(stderr, true)
	go func() {
		wg.Wait()
		close(lines)
	}()

	var state Stream
	for line := range lines {
		state.Feed(line.text, line.stderr, time.Now())
		if state.Auth && cmd.Process != nil {
			_ = cmd.Process.Kill()
			break
		}
	}
	_ = cmd.Wait()
	if state.Auth {
		ForgetToken(home)
		return "", ErrNeedLogin
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if state.Status == "SUCCESS" {
		path, err := CollectImage(home, state.ConversationID, state.Response, started)
		if err == nil {
			return path, nil
		}
		if quota, until := TranscriptQuota(home, started, time.Now()); quota {
			return "", &QuotaError{Until: until}
		}
		if state.Denied {
			return "", errDenied
		}
		return "", err
	}
	if !state.Quota {
		state.Quota, state.Until = TranscriptQuota(home, started, time.Now())
	}
	if state.Quota {
		return "", &QuotaError{Until: state.Until}
	}
	if path, err := CollectImage(home, state.ConversationID, state.Response, started); err == nil {
		return path, nil
	}
	if state.Denied {
		return "", errDenied
	}
	if state.Failed {
		return "", errors.New("出图失败")
	}
	return "", errNoImage
}

// promptWithReference 把参考图复制进工作目录。说明里若已写了原路径，就换成复制后的绝对路径。
func promptWithReference(work, prompt, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return prompt, nil
	}
	staged, err := stageReference(work, ref)
	if err != nil {
		return "", err
	}
	if strings.Contains(prompt, ref) {
		return strings.ReplaceAll(prompt, ref, staged), nil
	}
	return Instruction(prompt, staged), nil
}

func stageReference(work, ref string) (string, error) {
	info, err := os.Stat(ref)
	if err != nil {
		return "", fmt.Errorf("参考图不存在: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("参考图不是文件")
	}
	name := filepath.Base(ref)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		ext := filepath.Ext(ref)
		if ext == "" {
			ext = ".png"
		}
		name = "reference" + ext
	}
	dst := filepath.Join(work, name)
	if err := copyRegularFile(ref, dst); err != nil {
		return "", fmt.Errorf("复制参考图失败: %w", err)
	}
	abs, err := filepath.Abs(dst)
	if err != nil {
		return dst, nil
	}
	return abs, nil
}

func copyRegularFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	return err
}

func envWithHome(home string) []string {
	base := os.Environ()
	out := make([]string, 0, len(base)+1)
	for _, item := range base {
		if strings.HasPrefix(item, "HOME=") {
			continue
		}
		out = append(out, item)
	}
	out = append(out, "HOME="+home)
	return out
}
