package cdp

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// UploadFiles 先等待本次文件选择器，再执行入口 JS，避免猜测旧的文件输入框。
func (s *Session) UploadFiles(openJS string, files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("没有待上传文件")
	}
	paths := make([]string, len(files))
	for i, file := range files {
		path, err := filepath.Abs(file)
		if err != nil {
			return err
		}
		paths[i] = path
	}
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Second)
	defer cancel()
	if s.ego != nil {
		return s.ego.request(ctx, "chooseUpload", map[string]any{"openJS": openJS, "files": paths, "timeout": 25000}, nil)
	}
	opened := make(chan *page.EventFileChooserOpened, 1)
	chromedp.ListenTarget(ctx, func(event any) {
		if chooser, ok := event.(*page.EventFileChooserOpened); ok {
			select {
			case opened <- chooser:
			default:
			}
		}
	})
	return chromedp.Run(ctx, chromedp.ActionFunc(func(ec context.Context) error {
		if err := page.SetInterceptFileChooserDialog(true).Do(ec); err != nil {
			return err
		}
		defer func() {
			cleanup, stop := context.WithTimeout(context.WithoutCancel(ec), 3*time.Second)
			defer stop()
			_ = page.SetInterceptFileChooserDialog(false).Do(cleanup)
		}()
		_, exception, err := runtime.Evaluate(openJS).WithAwaitPromise(true).WithUserGesture(true).Do(ec)
		if err != nil {
			return err
		}
		if exception != nil {
			return fmt.Errorf("打开上传入口失败: %s", exception.Text)
		}
		select {
		case chooser := <-opened:
			if chooser.BackendNodeID == 0 {
				return fmt.Errorf("文件选择器未提供文件输入节点")
			}
			return dom.SetFileInputFiles(paths).WithBackendNodeID(chooser.BackendNodeID).Do(ec)
		case <-ec.Done():
			return fmt.Errorf("等待文件选择器: %w", ec.Err())
		}
	}))
}
