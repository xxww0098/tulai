// Package agy 跑 Antigravity 命令行出图，按账号切换并保存 OAuth 凭据。
package agy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	// Platform 是任务和账号表里的平台名。
	Platform = "Antigravity"
	// DefaultModel 是设置里没填模型时用的名字。
	DefaultModel = "gemini-3.8-flash"
	// LoginTimeout 是添加账号时等授权码的时间。
	LoginTimeout = 60 * time.Second
	// DefaultTimeout 是一单出图的默认时限。
	DefaultTimeout = 6 * time.Minute
)

// ErrNeedLogin 表示这个 HOME 还没登录，或者登录已经失效。
var ErrNeedLogin = errors.New("需登录")

// QuotaError 是额度或限流。Until 为空时等到本地次日 0 点。
type QuotaError struct {
	Until time.Time
}

func (e *QuotaError) Error() string { return "额度不足" }

// DefaultBin 是没填 agy 路径时用的位置。官方安装脚本的落点：
// macOS 和 Linux 是 ~/.local/bin/agy，Windows 是 %LOCALAPPDATA%\agy\bin\agy.exe。
func DefaultBin() string {
	return defaultBin(runtime.GOOS, os.UserHomeDir, os.Getenv)
}

func defaultBin(goos string, userHome func() (string, error), getenv func(string) string) string {
	if goos == "windows" {
		if dir := strings.TrimSpace(getenv("LOCALAPPDATA")); dir != "" {
			return filepath.Join(dir, "agy", "bin", "agy.exe")
		}
		if home, err := userHome(); err == nil && home != "" {
			return filepath.Join(home, "AppData", "Local", "agy", "bin", "agy.exe")
		}
		return filepath.Join("agy", "bin", "agy.exe")
	}
	home, err := userHome()
	if err != nil || home == "" {
		return filepath.Join(".local", "bin", "agy")
	}
	return filepath.Join(home, ".local", "bin", "agy")
}

// HomeRoot 是账号目录的根。设置为空时用数据库所在数据目录下的 agy-homes。
func HomeRoot(setting, dbPath string) string {
	if s := strings.TrimSpace(setting); s != "" {
		return s
	}
	return DirOfDB(dbPath)
}

// DirOfDB 从 database/config.db 往上找到数据目录，再进 agy-homes。
func DirOfDB(dbPath string) string {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return "agy-homes"
	}
	return filepath.Join(filepath.Dir(filepath.Dir(dbPath)), "agy-homes")
}

func cliDir(home string) string {
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

// TokenPath 是账号的 OAuth 快照，也是 CLI 的文件回退凭据。
func TokenPath(home string) string {
	return filepath.Join(cliDir(home), "antigravity-oauth-token")
}

// LogPath 是命令行日志，邮箱从这里解析。
func LogPath(home string) string {
	return filepath.Join(cliDir(home), "cli.log")
}

// BrainDir 是某次对话生成图片的目录。
func BrainDir(home, conversationID string) string {
	return filepath.Join(cliDir(home), "brain", conversationID)
}

// LoggedIn 要求存在可刷新的 OAuth 凭据，空文件不能代表登录成功。
func LoggedIn(home string) bool {
	data, err := os.ReadFile(TokenPath(home))
	if err != nil {
		return false
	}
	_, err = normalizeCredential(data)
	return err == nil
}

// ForgetToken 删掉失效的凭证文件，不读取内容。
func ForgetToken(home string) {
	_ = os.Remove(TokenPath(home))
}

var emailRe = regexp.MustCompile(`OAuth: authenticated successfully as (\S+@\S+)`)

// Email 取日志里最后一次登录成功的邮箱。没有就是空字符串。
func Email(home string) string {
	if token, err := os.ReadFile(TokenPath(home)); err == nil {
		if email := credentialEmail(token); email != "" {
			return email
		}
	}
	data, err := readTail(LogPath(home), 2<<20)
	if err != nil || len(data) == 0 {
		return ""
	}
	matches := emailRe.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return ""
	}
	return string(matches[len(matches)-1][1])
}

func readTail(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	off := int64(0)
	if size > max {
		off = size - max
	}
	buf := make([]byte, size-off)
	_, err = f.ReadAt(buf, off)
	if err != nil && len(buf) == 0 {
		return nil, err
	}
	return buf, nil
}

// NextAccountDir 在根目录下新建 acc1、acc2……返回目录和名字。
func NextAccountDir(root string) (dir, name string, err error) {
	if err = os.MkdirAll(root, 0o700); err != nil {
		return "", "", err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", "", err
	}
	used := map[string]bool{}
	for _, entry := range entries {
		used[entry.Name()] = true
	}
	for i := 1; i < 10000; i++ {
		name = fmt.Sprintf("acc%d", i)
		if used[name] {
			continue
		}
		dir = filepath.Join(root, name)
		if err = os.Mkdir(dir, 0o700); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", "", err
		}
		return dir, name, nil
	}
	return "", "", errors.New("账号目录已用尽")
}

// PauseMessage 是全部已登录账号都额度不足时，页头和日志用的同一句话。
func PauseMessage(until, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	loc := now.Location()
	y, m, d := now.In(loc).Date()
	midnight := time.Date(y, m, d+1, 0, 0, 0, 0, loc)
	if until.IsZero() || until.Sub(midnight).Abs() < time.Minute {
		return "全部账号额度不足，明天 00:00 自动恢复"
	}
	return fmt.Sprintf("全部账号额度不足，%s 自动恢复", until.In(loc).Format("2006-01-02 15:04"))
}
