package download

import (
	"context"
	"io"
	"net/http"
)

// Response 是一次下载请求的结果。调用方读完 Body 后要关掉它。
type Response struct {
	StatusCode int
	Header     http.Header
	FinalURL   string
	Body       io.ReadCloser
}

// Getter 发起一次 GET。prepare 在请求发出前改它，用来加 Range 和调用方自己的头。
// 实现要处理重定向，并在跨站点时丢掉 Cookie 和 Authorization。
type Getter interface {
	Get(ctx context.Context, rawURL string, prepare func(*http.Request)) (*Response, error)
}

// GetterFunc 让一个函数充当 Getter。
type GetterFunc func(context.Context, string, func(*http.Request)) (*Response, error)

// Get 调用函数本身。
func (f GetterFunc) Get(ctx context.Context, rawURL string, prepare func(*http.Request)) (*Response, error) {
	return f(ctx, rawURL, prepare)
}
