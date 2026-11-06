package browser

import (
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/store"
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

func TestAccountRejectsInvalidGlobalProxy(t *testing.T) {
	previous := savedProxy()
	t.Cleanup(func() { SetProxy(previous) })
	SetProxy("ftp://127.0.0.1:21")
	m := NewManager()
	m.configDir = t.TempDir()
	b := store.Browser{ID: 1, Name: "account", UserDataDir: t.TempDir()}
	if _, err := m.Start(b, t.TempDir(), KindCamoufox, "", true); err == nil || !strings.Contains(err.Error(), "全局代理地址无效") {
		t.Fatalf("invalid global proxy should prevent startup: %v", err)
	}
	b.Proxy = "http://127.0.0.1:7890"
	if _, err := m.Start(b, t.TempDir(), KindCamoufox, "", true); err == nil || !strings.Contains(err.Error(), "Camoufox 尚未准备好") {
		t.Fatalf("valid account proxy should override invalid global proxy: %v", err)
	}
}

func TestResolveCurrentProxyRejectsInvalidSystemProxy(t *testing.T) {
	previous := savedProxy()
	t.Cleanup(func() { SetProxy(previous) })
	SetProxy("")
	for _, key := range proxyEnvKeys {
		t.Setenv(key, "")
	}
	t.Setenv("HTTPS_PROXY", "invalid")
	if _, err := ResolveCurrentProxy(); err == nil || !strings.Contains(err.Error(), "系统代理地址无效") {
		t.Fatalf("invalid system proxy should prevent direct connection: %v", err)
	}
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:1080")
	if got, err := ResolveCurrentProxy(); err != nil || got != "socks5://127.0.0.1:1080" {
		t.Fatalf("valid fallback proxy: %q %v", got, err)
	}
}
