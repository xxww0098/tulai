package cdp

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// OpenURL 打开或激活登录页面，控制连接退出后标签页仍由浏览器保留。
func OpenURL(wsURL, url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	b, err := chromedp.NewBrowser(ctx, wsURL)
	if err != nil {
		return err
	}
	defer func() {
		cancel()
		<-b.LostConnection
	}()
	ctx = cdp.WithExecutor(ctx, b)
	pages, err := target.GetTargets().Do(ctx)
	if err != nil {
		return err
	}
	for _, p := range pages {
		if p.Type == "page" && p.URL == url {
			return target.ActivateTarget(p.TargetID).Do(ctx)
		}
	}
	id, err := target.CreateTarget(url).Do(ctx)
	if err != nil {
		return err
	}
	return target.ActivateTarget(id).Do(ctx)
}
