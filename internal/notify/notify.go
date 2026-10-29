// Package notify 发一条系统通知。失败就丢掉，出图不等它。
package notify

import (
	"os/exec"
	"runtime"
	"strings"
)

// Send 通知用户去完成验证码。没有通知程序时静默返回。
func Send(title, body string) {
	name, args := Command(title, body)
	if name == "" {
		return
	}
	_ = exec.Command(name, args...).Start()
}

// Command 是当前系统的通知命令。测试检查参数，不真正弹出通知。
func Command(title, body string) (string, []string) {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		title = "tulai"
	}
	switch runtime.GOOS {
	case "linux":
		return "notify-send", []string{title, body}
	case "darwin":
		return "osascript", []string{"-e", "display notification " + quoteApple(body) + " with title " + quoteApple(title)}
	case "windows":
		script := "[System.Reflection.Assembly]::LoadWithPartialName('System.Windows.Forms') | Out-Null; " +
			"$n = New-Object System.Windows.Forms.NotifyIcon; " +
			"$n.Icon = [System.Drawing.SystemIcons]::Information; $n.Visible = $true; " +
			"$n.ShowBalloonTip(8000, " + quotePS(title) + ", " + quotePS(body) + ", [System.Windows.Forms.ToolTipIcon]::Warning)"
		return "powershell", []string{"-NoProfile", "-NonInteractive", "-Command", script}
	default:
		return "", nil
	}
}

func quoteApple(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

func quotePS(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
