package cdp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"time"

	protocol "github.com/chromedp/cdproto/cdp"
	cdpio "github.com/chromedp/cdproto/io"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
)

const maxResourceBytes = 64 << 20

// LoadResource 使用当前浏览器登录态读取资源，不受页面 fetch 的 CORS 限制。
func (s *Session) LoadResource(rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("资源地址必须是 HTTP 或 HTTPS")
	}
	var data []byte
	err = s.exec(func(ctx context.Context) error {
		frame, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		resource, err := network.LoadNetworkResource(rawURL, &network.LoadNetworkResourceOptions{IncludeCredentials: true}).WithFrameID(frame.Frame.ID).Do(ctx)
		if err != nil {
			return err
		}
		if resource == nil {
			return fmt.Errorf("浏览器未返回资源")
		}
		if resource.Stream != "" {
			defer func() {
				closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
				defer cancel()
				_ = cdpio.Close(resource.Stream).Do(closeCtx)
			}()
		}
		if resource.HTTPStatusCode >= 400 {
			return fmt.Errorf("HTTP %.0f", resource.HTTPStatusCode)
		}
		if !resource.Success || resource.Stream == "" {
			return fmt.Errorf("浏览器资源读取失败: %s (%.0f)", resource.NetErrorName, resource.NetError)
		}
		data, err = readResourceStream(ctx, resource.Stream, maxResourceBytes)
		return err
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

func readResourceStream(ctx context.Context, stream cdpio.StreamHandle, limit int) ([]byte, error) {
	var data []byte
	for {
		var chunk cdpio.ReadReturns
		// 此版本生成的 IO.Read.Do 丢弃 base64Encoded，直接读取完整协议响应。
		if err := protocol.Execute(ctx, cdpio.CommandRead, cdpio.Read(stream).WithSize(256<<10), &chunk); err != nil {
			return nil, err
		}
		part := []byte(chunk.Data)
		if chunk.Base64encoded {
			var err error
			part, err = base64.StdEncoding.DecodeString(chunk.Data)
			if err != nil {
				return nil, fmt.Errorf("资源流编码无效: %w", err)
			}
		}
		if len(part) > limit-len(data) {
			return nil, fmt.Errorf("资源超过 %d 字节上限", limit)
		}
		data = append(data, part...)
		if chunk.EOF {
			return data, nil
		}
	}
}
