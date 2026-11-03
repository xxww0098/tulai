package cdp

import (
	"context"
	"fmt"
	"path/filepath"
	"time"
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
	return s.client.proc.Call(ctx, s.target, "chooseUpload", map[string]any{"openJS": openJS, "files": paths}, nil)
}
