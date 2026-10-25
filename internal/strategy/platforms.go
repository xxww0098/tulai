package strategy

import (
	"encoding/base64"
	"strings"

	"xbs/internal/cdp"
)

// Doubao 豆包策略。
type Doubao struct{ base }

// Platform 平台名。
func (Doubao) Platform() string { return "豆包" }

// HomeURL 豆包图像生成入口。
func (Doubao) HomeURL() string { return "https://www.doubao.com/chat/" }

// FetchResultBytes 豆包覆盖：优先网络层截获的原图（保存免水印版本）。
func (d Doubao) FetchResultBytes(s *cdp.Session, url string) ([]byte, error) {
	// 豆包 CDN 支持按参数取无水印原图：去掉水印相关查询参数。
	clean := PreferUnwatermarkedURL(url)
	b, err := d.base.FetchResultBytes(s, clean)
	if err != nil {
		// 原参数版本兜底
		return d.base.FetchResultBytes(s, url)
	}
	return b, nil
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
