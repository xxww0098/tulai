package tasks

import (
	"os/exec"
	"runtime"
	"strings"
)

// sendSystemNotification 发一条系统通知。没有通知程序或失败时静默返回，出图不等它。
func sendSystemNotification(title, body string) {
	name, args := systemNotificationCommand(title, body)
	if name == "" {
		return
	}
	_ = exec.Command(name, args...).Start()
}

// systemNotificationCommand 返回当前系统的通知命令；测试检查参数，不真正弹出通知。
func systemNotificationCommand(title, body string) (string, []string) {
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
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func quotePS(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
