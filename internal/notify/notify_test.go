package notify

import (
	"runtime"
	"strings"
	"testing"
)

func TestCommandNamesTheVerifier(t *testing.T) {
	name, args := Command("需要人工验证", "千问2 出图遇到验证码，请在浏览器窗口里完成")
	switch runtime.GOOS {
	case "linux":
		if name != "notify-send" || len(args) != 2 || args[0] != "需要人工验证" || !strings.Contains(args[1], "千问2") {
			t.Fatalf("notify-send: %s %q", name, args)
		}
	case "darwin":
		if name != "osascript" || len(args) != 2 || !strings.Contains(args[1], "需要人工验证") || !strings.Contains(args[1], "千问2") {
			t.Fatalf("osascript: %s %q", name, args)
		}
	case "windows":
		if name != "powershell" || len(args) != 4 || !strings.Contains(args[3], "需要人工验证") || !strings.Contains(args[3], "千问2") {
			t.Fatalf("powershell: %s %q", name, args)
		}
	default:
		if name != "" {
			t.Fatalf("unsupported OS should not invent a command: %s", name)
		}
	}
}
