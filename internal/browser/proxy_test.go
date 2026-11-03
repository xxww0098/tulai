package browser

import (
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
