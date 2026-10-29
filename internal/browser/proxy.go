package browser

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

// proxyEnvKeys 是「跟随系统」时读取的变量，按这个顺序。
// Chrome 在 Linux 上不读这些变量，所以要写进 --proxy-server。
var proxyEnvKeys = []string{
	"HTTPS_PROXY", "https_proxy",
	"ALL_PROXY", "all_proxy",
	"HTTP_PROXY", "http_proxy",
}

var (
	proxyMu     sync.Mutex
	proxyCustom string
)

// SetProxy 记下设置里的自定义代理。空字符串表示跟随系统。
func SetProxy(custom string) {
	proxyMu.Lock()
	proxyCustom = strings.TrimSpace(custom)
	proxyMu.Unlock()
}

func savedProxy() string {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	return proxyCustom
}

// CurrentProxyServer 是这一次启动浏览器或下载内置浏览器时用的代理。
func CurrentProxyServer() string {
	return ProxyServer(savedProxy(), os.Getenv)
}

// ProxyServer 把自定义代理或环境变量收成 Chrome --proxy-server 的值。
// custom 非空时只用它；写错了就返回空，不再改走环境变量。
// custom 为空时依次看 HTTPS_PROXY、ALL_PROXY、HTTP_PROXY，以及对应的小写名字。
func ProxyServer(custom string, env func(string) string) string {
	if env == nil {
		env = func(string) string { return "" }
	}
	if s := strings.TrimSpace(custom); s != "" {
		if norm, ok := NormalizeProxy(s); ok {
			return norm
		}
		return ""
	}
	for _, key := range proxyEnvKeys {
		if norm, ok := NormalizeProxy(env(key)); ok {
			return norm
		}
	}
	return ""
}

// NormalizeProxy 接受 http://主机:端口 或 socks5://主机:端口。
// 没写协议时按 http。socks5h 收成 socks5。
func NormalizeProxy(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "socks5":
	case "socks5h":
		scheme = "socks5"
	default:
		return "", false
	}
	host, port := u.Hostname(), u.Port()
	if host == "" || !validPort(port) {
		return "", false
	}
	addr := host + ":" + port
	if strings.Contains(host, ":") {
		addr = "[" + host + "]:" + port
	}
	if u.User != nil {
		addr = u.User.String() + "@" + addr
	}
	return scheme + "://" + addr, true
}

func validPort(port string) bool {
	if port == "" || len(port) > 5 {
		return false
	}
	n := 0
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	return n >= 1 && n <= 65535
}

// downloadProxy 让内置浏览器的下载走和 Chrome 相同的代理。
func downloadProxy(*http.Request) (*url.URL, error) {
	raw := CurrentProxyServer()
	if raw == "" {
		return nil, nil
	}
	return url.Parse(raw)
}
