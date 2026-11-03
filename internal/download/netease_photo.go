package download

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var neteasePhotoSubdomain = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// The original pp.163.com photography service is no longer verifiable. Handle
// its links explicitly instead of interpreting a replacement page as an album.
func isNeteasePhotoPage(u *url.URL) bool {
	if u == nil || u.User != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host != "pp.163.com" {
		if !strings.HasSuffix(host, ".pp.163.com") || !neteasePhotoSubdomain.MatchString(strings.TrimSuffix(host, ".pp.163.com")) {
			return false
		}
	}
	return (u.Scheme == "http" && (u.Port() == "" || u.Port() == "80")) ||
		(u.Scheme == "https" && (u.Port() == "" || u.Port() == "443"))
}

func discoverNeteasePhoto(ctx context.Context, _ Getter, page *url.URL) (Discovery, error) {
	result := Discovery{IsPage: true}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !isNeteasePhotoPage(page) {
		return result, errors.New("网易摄影页面地址无效")
	}
	return result, errors.New("网易摄影当前不可访问，专用采集未实现")
}
