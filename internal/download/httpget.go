package download

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// DefaultUserAgent 是直接下载时带上的浏览器标识。结果图地址多为 CDN，空 UA 容易被拒。
const DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

// HTTPGetter 用 net/http 发下载请求。
//
// 代理与浏览器共用一条；跨站重定向时 net/http 会丢掉 Cookie 和 Authorization，
// 不会把登录信息带到别的站点。这里不设整体超时，大文件由 ctx 控制。
type HTTPGetter struct {
	client    *http.Client
	userAgent string
}

// NewHTTPGetter 按代理建一个下载器。proxy 空字符串表示直连；支持 http 和 socks5。
func NewHTTPGetter(proxy string) *HTTPGetter {
	transport := &http.Transport{
		Proxy:                 proxyFunc(proxy),
		DialContext:           (&net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &HTTPGetter{
		client: &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("重定向次数过多")
			}
			if len(via) > 0 && !sameOrigin(via[0].URL, req.URL) {
				req.Header.Del("Cookie")
				req.Header.Del("Authorization")
			}
			if ref, err := url.Parse(req.Header.Get("Referer")); err == nil && ref.Host != "" {
				if ref.Scheme == "https" && req.URL.Scheme == "http" {
					req.Header.Del("Referer")
				} else if !sameOrigin(ref, req.URL) {
					req.Header.Set("Referer", ref.Scheme+"://"+ref.Host+"/")
				}
			}
			return nil
		}},
		userAgent: DefaultUserAgent,
	}
}

func proxyFunc(raw string) func(*http.Request) (*url.URL, error) {
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return nil
	}
	return http.ProxyURL(parsed)
}

// Get 发出一次 GET。prepare 在发送前调用，调用方用它加 Range 和自己的头。
func (g *HTTPGetter) Get(ctx context.Context, rawURL string, prepare func(*http.Request)) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", g.userAgent)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Encoding", "identity")
	if prepare != nil {
		prepare(req)
	}
	client := g.client
	if public, _ := ctx.Value(publicDownloadKey{}).(bool); public {
		if _, err := publicImageURL(req.URL.String()); err != nil {
			return nil, err
		}
		copyClient := *g.client
		copyClient.Jar = nil
		checkRedirect := copyClient.CheckRedirect
		copyClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
			if _, err := publicImageURL(next.URL.String()); err != nil {
				return err
			}
			if checkRedirect != nil {
				return checkRedirect(next, via)
			}
			if len(via) >= 10 {
				return errors.New("重定向次数过多")
			}
			return nil
		}
		client = &copyClient
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		FinalURL:   resp.Request.URL.String(),
		Body:       resp.Body,
	}, nil
}

type publicDownloadKey struct{}

// shortcut: DNS follows the system network, which may use fake IPs; validate explicit addresses and redirects.
func publicImageURL(raw string) (*url.URL, error) {
	u, err := crawlURL(nil, raw)
	if err != nil || u.User != nil || (u.Port() != "" && !(u.Scheme == "https" && u.Port() == "443") && !(u.Scheme == "http" && u.Port() == "80")) {
		return nil, errors.New("图片地址无效")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".home.arpa") {
		return nil, errors.New("图片地址不是公网主机")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !isPublicDownloadIP(ip.Unmap()) {
			return nil, errors.New("图片地址不是公网主机")
		}
	} else if !strings.Contains(host, ".") || strings.Trim(host, "0123456789.") == "" {
		return nil, errors.New("图片地址不是公网主机")
	}
	return u, nil
}
