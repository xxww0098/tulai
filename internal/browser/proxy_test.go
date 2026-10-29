package browser

import (
	"strings"
	"testing"
)

func TestProxyServerPrefersCustomThenEnv(t *testing.T) {
	env := func(key string) string {
		switch key {
		case "HTTPS_PROXY":
			return "http://10.0.0.8:8888"
		case "ALL_PROXY":
			return "socks5://10.0.0.9:1080"
		case "HTTP_PROXY":
			return "http://10.0.0.7:8080"
		default:
			return ""
		}
	}
	if got := ProxyServer("socks5://127.0.0.1:1080", env); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("custom = %q", got)
	}
	if got := ProxyServer("  http://127.0.0.1:7890/ ", env); got != "http://127.0.0.1:7890" {
		t.Fatalf("trimmed custom = %q", got)
	}
	if got := ProxyServer("ftp://127.0.0.1:21", env); got != "" {
		t.Fatalf("invalid custom should not fall through to env, got %q", got)
	}
	if got := ProxyServer("", env); got != "http://10.0.0.8:8888" {
		t.Fatalf("HTTPS_PROXY = %q", got)
	}
	httpsOff := func(key string) string {
		if key == "https_proxy" {
			return "socks5h://127.0.0.1:1080"
		}
		return ""
	}
	if got := ProxyServer("", httpsOff); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("lowercase socks5h = %q", got)
	}
	all := func(key string) string {
		if key == "ALL_PROXY" {
			return "127.0.0.1:7890"
		}
		return ""
	}
	if got := ProxyServer("", all); got != "http://127.0.0.1:7890" {
		t.Fatalf("host:port via ALL_PROXY = %q", got)
	}
	if got := ProxyServer("", func(string) string { return "" }); got != "" {
		t.Fatalf("no proxy = %q", got)
	}
	if _, ok := NormalizeProxy("http://127.0.0.1"); ok {
		t.Fatal("missing port should be rejected")
	}
}

func TestLaunchArgsIncludeProxyBeforeURL(t *testing.T) {
	userData := "/tmp/profile"
	login := "https://www.doubao.com/chat/"
	args := launchArgs(9333, userData, login, "", "http://127.0.0.1:7890", []string{"--disable-gpu"})
	if args[len(args)-1] != login {
		t.Fatalf("url = %q", args[len(args)-1])
	}
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "--proxy-server=http://127.0.0.1:7890") {
		t.Fatalf("missing proxy flag: %v", args)
	}
	proxyAt := strings.Index(joined, "--proxy-server=")
	extraAt := strings.Index(joined, "--disable-gpu")
	if proxyAt < 0 || extraAt < proxyAt {
		t.Fatalf("proxy should come before extra flags: %v", args)
	}
	plain := strings.Join(launchArgs(9333, userData, login, "", "", nil), "\n")
	if strings.Contains(plain, "--proxy-server") {
		t.Fatalf("empty proxy leaked: %s", plain)
	}
	socks := launchArgs(9333, userData, login, "/tmp/ext", "socks5://127.0.0.1:1080", nil)
	if socks[len(socks)-1] != login || !strings.Contains(strings.Join(socks, "\n"), "--proxy-server=socks5://127.0.0.1:1080") {
		t.Fatalf("socks args: %v", socks)
	}
	if !strings.Contains(strings.Join(socks, "\n"), "--load-extension=/tmp/ext") {
		t.Fatalf("proxy ate the extension flag: %v", socks)
	}
}
