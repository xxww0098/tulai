package strategy

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/xxww0098/tulai/internal/cdp"
)

// Doubao 豆包策略。
type Doubao struct{ base }

// Platform 平台名。
func (Doubao) Platform() string { return "豆包" }

// HomeURL 豆包图像生成入口。
func (Doubao) HomeURL() string { return "https://www.doubao.com/chat/" }

// FetchResultBytes 豆包覆盖：先要原图地址，再按页面 referer 和浏览器登录态去拉。
func (d Doubao) FetchResultBytes(s *cdp.Session, raw string) ([]byte, error) {
	tried := map[string]bool{}
	var last error
	for _, candidate := range []string{OriginalImageURL(raw), PreferUnwatermarkedURL(raw), raw} {
		if candidate == "" || tried[candidate] {
			continue
		}
		tried[candidate] = true
		body, err := d.base.FetchResultBytes(s, candidate)
		if err == nil {
			return body, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("缺少图片地址")
	}
	return nil, last
}

// OriginalImageURL 去掉 imagex 的 ~tplv 缩略图模板，留下对象原图地址。
// 没有模板时原样返回。签名跟模板绑在一起时，调用方还会再试页面上的原地址。
func OriginalImageURL(raw string) string {
	clean := PreferUnwatermarkedURL(raw)
	i := strings.Index(clean, "~tplv-")
	if i <= 0 {
		return clean
	}
	tail := ""
	if q := strings.IndexAny(clean[i:], "?#"); q >= 0 {
		tail = clean[i+q:]
	}
	return clean[:i] + tail
}

// PreferUnwatermarkedURL 去掉豆包 CDN 用来带水印的查询。
// 查询里出现 watermark 或 wm=（忽略大小写）时丢掉整段查询，没有这些参数时原样返回。
// 千问结果地址不用这组参数，因此不会被改写；这里不识别别的站点参数。
func PreferUnwatermarkedURL(raw string) string {
	if i := strings.Index(raw, "?"); i > 0 {
		lower := strings.ToLower(raw[i:])
		if strings.Contains(lower, "watermark") || strings.Contains(lower, "wm=") {
			return raw[:i]
		}
	}
	return raw
}

// Qianwen 千问策略。
type Qianwen struct{ base }

// Platform 平台名。
func (Qianwen) Platform() string { return "千问" }

// HomeURL 千问（通义）入口。
func (Qianwen) HomeURL() string { return "https://www.tongyi.com/" }

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ","); i >= 0 && strings.HasPrefix(s, "data:") {
		s = s[i+1:]
	}
	return base64.StdEncoding.DecodeString(s)
}
