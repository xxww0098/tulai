package agy

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

const (
	// InstallPage 是官方下载页，里面有各系统的安装命令。
	InstallPage = "https://antigravity.google/download"
	// InstallDocs 是官方「开始使用」文档里命令行那一页。
	InstallDocs = "https://antigravity.google/docs/getting-started?tab=cli"
)

// InstallCommand 是官方给出的命令行安装命令。
// macOS 和 Linux 用 bash 脚本，Windows 用 PowerShell。脚本会把 agy 放到 DefaultBin。
func InstallCommand(goos string) string {
	if goos == "windows" {
		return "irm https://antigravity.google/cli/install.ps1 | iex"
	}
	return "curl -fsSL https://antigravity.google/cli/install.sh | bash"
}

// InstallShell 是 InstallCommand 要粘贴进去的终端名。
func InstallShell(goos string) string {
	if goos == "windows" {
		return "PowerShell"
	}
	return "终端"
}

// Locate 找出能用的 agy。bin 是设置里填的路径，空则按 DefaultBin，再按 PATH。
// 只看文件在不在，不执行它。找不到时 ok 为假，path 是最先检查的那个位置。
func Locate(bin string) (path string, ok bool) {
	return locate(bin, DefaultBin(), runtime.GOOS, exec.LookPath)
}

func locate(bin, fallback, goos string, lookPath func(string) (string, error)) (string, bool) {
	first := strings.TrimSpace(bin)
	if first == "" {
		first = fallback
	}
	if isFile(first) {
		return first, true
	}
	if first != fallback && isFile(fallback) {
		return fallback, true
	}
	name := "agy"
	if goos == "windows" {
		name = "agy.exe"
	}
	if found, err := lookPath(name); err == nil && isFile(found) {
		return found, true
	}
	return first, false
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info != nil && !info.IsDir()
}
