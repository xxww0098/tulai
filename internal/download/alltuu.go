package download

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var alltuuAlbumPath = regexp.MustCompile(`^/album/[0-9]{1,24}/?$`)

func isAlltuuAlbum(u *url.URL) bool {
	if u == nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") ||
		(u.Port() != "" && !(u.Scheme == "http" && u.Port() == "80") && !(u.Scheme == "https" && u.Port() == "443")) {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return (host == "alltuu.com" || host == "www.alltuu.com" || host == "m.alltuu.com") && alltuuAlbumPath.MatchString(u.Path)
}

func alltuuUnsupported() error {
	return errors.New("喔图相册目前无法验证查看和下载权限，暂不支持采集")
}
