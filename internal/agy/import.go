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

const (
	// VerifyTimeout 是导入后试跑 agy 的时限。
	VerifyTimeout = 90 * time.Second
	// MsgNotFound 是本机 HOME 里没有已登录的 CLI。
	MsgNotFound = "本机没有找到已登录的 Antigravity CLI"
	// MsgKeychain 是令牌在钥匙串里、目录里没有凭证文件。
	MsgKeychain = "这个账号存在钥匙串里，可直接导入为一个账号（同一台 Mac 的钥匙串账号只能用一个）"
	// MsgAlready 是源目录在账号根里，或邮箱已经导入过。
	MsgAlready = "这个账号已经导入过"
	// MsgVerifying 是导入后正在试跑。
	MsgVerifying = "正在验证…"
	// MsgImported 是验证通过。
	MsgImported = "导入成功"
	// MsgNoToken 是选中的目录里没有凭证文件。
	MsgNoToken = "没有找到登录凭证"

	verifyPrompt = "reply with just: ok"
)

// ErrAlreadyImported 表示这个账号已经在账号根里，或者邮箱重复。
var ErrAlreadyImported = errors.New(MsgAlready)

// Detection 是本机 HOME 上的 Antigravity CLI。
type Detection struct {
	Home     string
	Email    string
	HasToken bool
	Keychain bool
	Hint     string
}

// CanImport 表示可以直接导入，不必再选文件夹。
func (d Detection) CanImport() bool {
	return strings.TrimSpace(d.Home) != "" && (d.HasToken || d.Keychain)
}

// DetectLocal 看当前用户的 HOME。
func DetectLocal() Detection {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return Detection{Hint: MsgNotFound}
	}
	return DetectAt(home)
}

// DetectAt 看指定 HOME 里有没有已登录的 CLI。不读取凭证内容。
func DetectAt(home string) Detection {
	home = strings.TrimSpace(home)
	if home == "" {
		return Detection{Hint: MsgNotFound}
	}
	if LoggedIn(home) {
		email := Email(home)
		hint := "已检测到本机登录"
		if email != "" {
			hint = "已检测到 " + email
		}
		return Detection{Home: home, Email: email, HasToken: true, Hint: hint}
	}
	if keychainHasToken() {
		return Detection{Home: home, Email: Email(home), Keychain: true, Hint: MsgKeychain}
	}
	return Detection{Hint: MsgNotFound}
}

// keychainProbeArgs 只按服务名查找，不能带出会泄漏口令的参数。
func keychainProbeArgs() []string {
	return []string{"find-generic-password", "-s", "antigravity"}
}

// NormalizeHome 接受三种目录：用户 HOME、.gemini，或 antigravity-cli 本身。
// 返回对应的 HOME。凭证文件必须存在。
func NormalizeHome(path string) (string, error) {
	path, err := absClean(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", errors.New("请选择文件夹")
	}
	var home string
	switch {
	case dirExists(cliDir(path)):
		home = path
	case filepath.Base(path) == ".gemini" && dirExists(filepath.Join(path, "antigravity-cli")):
		home = filepath.Dir(path)
	case filepath.Base(path) == "antigravity-cli" && filepath.Base(filepath.Dir(path)) == ".gemini":
		home = filepath.Dir(filepath.Dir(path))
	default:
		return "", errors.New("没有找到 Antigravity CLI 目录")
	}
	home, err = absClean(home)
	if err != nil {
		return "", err
	}
	if !LoggedIn(home) {
		return "", errors.New(MsgNoToken)
	}
	return home, nil
}

// WithinAccountRoot 表示 path 就是账号根，或在账号根里面。
func WithinAccountRoot(path, root string) bool {
	path, err1 := absClean(path)
	root, err2 := absClean(root)
	if err1 != nil || err2 != nil || path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// EmailUsed 比较已有账号的邮箱。空邮箱不算重复。
func EmailUsed(email string, existing []string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}
	for _, item := range existing {
		if strings.EqualFold(strings.TrimSpace(item), email) {
			return true
		}
	}
	return false
}

// ImportRequest 是一次从本机 CLI 目录导入。
type ImportRequest struct {
	SourceHome        string
	AccountRoot       string
	ExistingEmails    []string
	Bin               string
	Model             string
	Timeout           time.Duration
	AllowMissingToken bool
}

// ImportResult 是新建的账号目录。
type ImportResult struct {
	Home  string
	Name  string
	Email string
}

// Import 新建 accN，只复制登录需要的文件，再试跑。失败就删掉新建的目录。
func Import(ctx context.Context, req ImportRequest) (ImportResult, error) {
	src, err := absClean(req.SourceHome)
	if err != nil {
		return ImportResult{}, errors.New("没有可导入的账号")
	}
	root := strings.TrimSpace(req.AccountRoot)
	if root == "" {
		return ImportResult{}, errors.New("没有账号目录")
	}
	root, err = absClean(root)
	if err != nil {
		return ImportResult{}, errors.New("没有账号目录")
	}
	if WithinAccountRoot(src, root) {
		return ImportResult{}, ErrAlreadyImported
	}
	email := Email(src)
	if EmailUsed(email, req.ExistingEmails) {
		return ImportResult{}, ErrAlreadyImported
	}
	if !LoggedIn(src) && !req.AllowMissingToken {
		return ImportResult{}, errors.New(MsgNoToken)
	}
	bin := strings.TrimSpace(req.Bin)
	if bin == "" {
		bin = DefaultBin()
	}
	if _, err := os.Stat(bin); err != nil {
		return ImportResult{}, fmt.Errorf("找不到 agy：%s", bin)
	}
	dir, name, err := NextAccountDir(root)
	if err != nil {
		return ImportResult{}, err
	}
	if err := copyAccountFiles(src, dir); err != nil {
		_ = os.RemoveAll(dir)
		return ImportResult{}, err
	}
	if err := verify(ctx, bin, dir, req.Model, req.Timeout); err != nil {
		_ = os.RemoveAll(dir)
		return ImportResult{}, err
	}
	return ImportResult{Home: dir, Name: name, Email: email}, nil
}

// accountFile 是允许复制的 antigravity-cli 文件。凭证单独收成 0600。
var accountFiles = []struct {
	name string
	mode os.FileMode
}{
	{"antigravity-oauth-token", 0o600},
	{"installation_id", 0o644},
	{"jetski_state.pbtxt", 0o644},
}

// copyAccountFiles 只复制凭证、installation_id、jetski_state.pbtxt 和 .gemini/config。
// 不复制 brain、conversations、日志和缓存。复制凭证时只做字节拷贝，不把内容读出来。
func copyAccountFiles(srcHome, dstHome string) error {
	srcCLI := cliDir(srcHome)
	dstCLI := cliDir(dstHome)
	if err := os.MkdirAll(dstCLI, 0o700); err != nil {
		return fmt.Errorf("复制账号文件失败: %w", err)
	}
	for _, file := range accountFiles {
		src := filepath.Join(srcCLI, file.name)
		info, err := os.Stat(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("复制账号文件失败: %w", err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := copyFileExact(src, filepath.Join(dstCLI, file.name), file.mode); err != nil {
			return fmt.Errorf("复制账号文件失败: %w", err)
		}
	}
	return copyDirFiles(filepath.Join(srcHome, ".gemini", "config"), filepath.Join(dstHome, ".gemini", "config"))
}

func copyDirFiles(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("复制账号文件失败: %w", err)
	}
	if !info.IsDir() {
		return nil
	}
	err = filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := dst
		if rel != "." {
			target = filepath.Join(dst, rel)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		meta, err := d.Info()
		if err != nil {
			return err
		}
		if !meta.Mode().IsRegular() {
			return nil
		}
		return copyFileExact(path, target, 0o644)
	})
	if err != nil {
		return fmt.Errorf("复制账号文件失败: %w", err)
	}
	return nil
}

// copyFileExact 按字节复制普通文件，并设成指定权限。
func copyFileExact(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("不是普通文件")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func verify(ctx context.Context, bin, home, model string, timeout time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = VerifyTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	model = strings.TrimSpace(model)
	if model == "" {
		model = DefaultModel
	}
	work, err := os.MkdirTemp("", "agy-verify-")
	if err != nil {
		return fmt.Errorf("创建临时目录失败: %w", err)
	}
	defer os.RemoveAll(work)
	cmd := exec.CommandContext(ctx, bin, "-p", verifyPrompt, "--output-format", "stream-json", "--model", model)
	cmd.Dir = work
	cmd.Env = envWithHome(home)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return errors.New("验证没有通过")
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return errors.New("验证没有通过")
	}
	if err := cmd.Start(); err != nil {
		return errors.New("验证没有通过")
	}
	lines := make(chan taggedLine, 64)
	var wg sync.WaitGroup
	scan := func(r io.Reader, isErr bool) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 1<<20)
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
		}
	}
	_ = cmd.Wait()
	if state.Status == "SUCCESS" {
		return nil
	}
	if state.Auth {
		return errors.New("登录已失效")
	}
	if state.Quota {
		return &QuotaError{Until: state.Until}
	}
	if ctx.Err() != nil {
		return errors.New("验证超时")
	}
	return errors.New("验证没有通过")
}

func absClean(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("请选择文件夹")
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
