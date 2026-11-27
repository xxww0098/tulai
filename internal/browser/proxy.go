package browser

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/xxww0098/tulai/internal/store"
)

// proxyEnvKeys 是「跟随系统」时读取的变量，按这个顺序。
// 同一设置用于 Fingerprint 安装与账号默认代理。
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

// CurrentProxyServer 是这一次启动浏览器或安装 Fingerprint 时用的代理。
func CurrentProxyServer() string {
	return ProxyServer(savedProxy(), os.Getenv)
}

func ResolveCurrentProxy() (string, error) {
	custom := savedProxy()
	proxy := ProxyServer(custom, os.Getenv)
	if proxy != "" {
		return proxy, nil
	}
	if custom != "" {
		return "", errors.New("全局代理地址无效")
	}
	for _, key := range proxyEnvKeys {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return "", errors.New("系统代理地址无效")
		}
	}
	return "", nil
}

// ProxyServer 把自定义代理或环境变量规范为受管浏览器可用的代理地址。
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
	scheme, host, port, user, ok := parseProxy(raw)
	if !ok {
		return "", false
	}
	addr := formatProxyAddr(host, port)
	if user != nil {
		addr = user.String() + "@" + addr
	}
	return scheme + "://" + addr, true
}

// NormalizeProxyKey returns a redacted canonical exit key (scheme, lowercased
// host:port, and hashed credentials) so exit comparisons never leak secrets.
func NormalizeProxyKey(raw string) string {
	scheme, host, port, user, ok := parseProxy(raw)
	if !ok {
		return ""
	}
	addr := formatProxyAddr(strings.ToLower(host), port)
	if tag := hashProxyAuth(user); tag != "" {
		addr = tag + "@" + addr
	}
	return scheme + "://" + addr
}

// SamePlatformSharedExit returns names of other enabled managed accounts on an
// overlapping platform (豆包/千问) that resolve to the same exit key.
func SamePlatformSharedExit(browsers []store.Browser, target store.Browser, globalProxy string) []string {
	if target.Disabled {
		return nil
	}
	targetPlatforms := managedPlatforms(target.Platform)
	if len(targetPlatforms) == 0 {
		return nil
	}
	targetKey, ok := effectiveExitKey(target.Proxy, globalProxy)
	if !ok {
		return nil
	}
	var shared []string
	for _, b := range browsers {
		if sameBrowser(b, target) || b.Disabled {
			continue
		}
		if !platformsOverlap(targetPlatforms, managedPlatforms(b.Platform)) {
			continue
		}
		key, ok := effectiveExitKey(b.Proxy, globalProxy)
		if !ok || key != targetKey {
			continue
		}
		shared = append(shared, browserIdentifier(b))
	}
	return shared
}

func parseProxy(raw string) (string, string, string, *url.Userinfo, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", nil, false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", "", "", nil, false
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "http", "socks5":
	case "socks5h":
		scheme = "socks5"
	default:
		return "", "", "", nil, false
	}
	host, port := u.Hostname(), u.Port()
	if host == "" || !validPort(port) {
		return "", "", "", nil, false
	}
	return scheme, host, port, u.User, true
}

func formatProxyAddr(host, port string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

func hashProxyAuth(user *url.Userinfo) string {
	if user == nil {
		return ""
	}
	username := user.Username()
	password, hasPassword := user.Password()
	if username == "" && !hasPassword {
		return ""
	}
	h := sha256.New()
	h.Write([]byte(username))
	h.Write([]byte{0})
	if hasPassword {
		h.Write([]byte{1})
		h.Write([]byte(password))
	}
	return "auth-" + hex.EncodeToString(h.Sum(nil)[:16])
}

func effectiveExitKey(accountProxy, globalProxy string) (string, bool) {
	if raw := strings.TrimSpace(accountProxy); raw != "" {
		key := NormalizeProxyKey(raw)
		return key, key != ""
	}
	if raw := strings.TrimSpace(globalProxy); raw != "" {
		key := NormalizeProxyKey(raw)
		return key, key != ""
	}
	return "", true
}

func managedPlatforms(raw string) map[string]struct{} {
	var out map[string]struct{}
	for _, part := range strings.Split(raw, ",") {
		canonical := ""
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "豆包":
			canonical = "豆包"
		case "千问":
			canonical = "千问"
		case "千问国际":
			canonical = "千问国际"
		case "gemini":
			canonical = "Gemini"
		case "即梦", "jimeng":
			canonical = "即梦"
		}
		if canonical == "" {
			continue
		}
		if out == nil {
			out = map[string]struct{}{}
		}
		out[canonical] = struct{}{}
	}
	return out
}

func platformsOverlap(a, b map[string]struct{}) bool {
	for k := range a {
		if _, ok := b[k]; ok {
			return true
		}
	}
	return false
}

func sameBrowser(a, b store.Browser) bool {
	if a.ID > 0 || b.ID > 0 {
		return a.ID == b.ID
	}
	return a.Name == b.Name && a.UserDataDir == b.UserDataDir
}

func browserIdentifier(b store.Browser) string {
	if name := strings.TrimSpace(b.Name); name != "" {
		return name
	}
	return fmt.Sprintf("account-%d", b.ID)
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
