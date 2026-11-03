package cdp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"
)

const maxResourceBytes = 64 << 20

// LoadResource 使用当前浏览器登录态读取资源，不受页面 fetch 的 CORS 限制。
func (s *Session) LoadResource(rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("资源地址必须是 HTTP 或 HTTPS")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 50*time.Second)
	defer cancel()
	var result struct{ Data string }
	if err := s.client.proc.Call(ctx, s.target, "resource", map[string]any{"url": rawURL}, &result); err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(result.Data)
	if len(data) > maxResourceBytes {
		return nil, fmt.Errorf("资源超过 64 MiB 上限")
	}
	return data, err
}
