package download

import (
	"net/url"
	"strings"
)

func weiboOriginalURL(source, raw string) string {
	page, err := crawlURL(nil, source)
	if err != nil || (!domainOrSubdomain(page.Hostname(), "weibo.com") && !domainOrSubdomain(page.Hostname(), "weibo.cn")) {
		return raw
	}
	image, err := crawlURL(nil, raw)
	if err != nil || !domainOrSubdomain(image.Hostname(), "sinaimg.cn") {
		return raw
	}
	path := image.EscapedPath()
	if !strings.HasPrefix(path, "/") {
		return raw
	}
	rest := path[1:]
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 || slash+1 >= len(rest) || rest[slash+1] == '/' {
		return raw
	}
	escaped := "/large" + rest[slash:]
	unescaped, err := url.PathUnescape(escaped)
	if err != nil {
		return raw
	}
	image.Path, image.RawPath = unescaped, escaped
	return image.String()
}

func domainOrSubdomain(host, domain string) bool {
	host = strings.ToLower(host)
	return host == domain || strings.HasSuffix(host, "."+domain)
}
