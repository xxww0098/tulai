package cdp

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Session) IsEgo() bool { return s.ego != nil }

func (s *Session) egoSDKRequest(method string, params, result any, timeout time.Duration) error {
	if !s.IsEgo() {
		return fmt.Errorf("当前会话不是 ego lite")
	}
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	return s.ego.request(ctx, method, params, result)
}

// EgoEvaluate 通过官方 page.evaluate 读取页面状态。
func (s *Session) EgoEvaluate(expression string, out any) error {
	return s.egoSDKRequest("sdkEvaluate", map[string]any{"expression": expression}, out, 30*time.Second)
}

func (s *Session) EgoSnapshot() (string, error) {
	var snapshot string
	err := s.egoSDKRequest("sdkSnapshot", nil, &snapshot, 30*time.Second)
	return snapshot, err
}

func (s *Session) EgoClick(selector string) error {
	return s.egoSDKRequest("sdkClick", map[string]any{"selector": selector, "timeout": 25000}, nil, 30*time.Second)
}

func (s *Session) EgoPress(key string) error {
	return s.egoSDKRequest("sdkPress", map[string]any{"key": key}, nil, 30*time.Second)
}

func (s *Session) EgoFill(selector, value string) error {
	return s.egoSDKRequest("sdkFill", map[string]any{"selector": selector, "value": value, "timeout": 25000}, nil, 30*time.Second)
}

// EgoUpload 只使用本次官方 FileChooser，不猜测页面文件输入框。
func (s *Session) EgoUpload(openSelectors []string, menuLabel string, files []string) error {
	if len(openSelectors) == 0 && strings.TrimSpace(menuLabel) == "" || len(files) == 0 {
		return fmt.Errorf("缺少上传入口或文件")
	}
	for _, selector := range openSelectors {
		if strings.TrimSpace(selector) == "" {
			return fmt.Errorf("上传入口为空")
		}
	}
	paths := make([]string, len(files))
	for i, file := range files {
		path, err := filepath.Abs(file)
		if err != nil {
			return err
		}
		paths[i] = path
	}
	return s.egoSDKRequest("sdkUpload", map[string]any{"selectors": openSelectors, "menuLabel": menuLabel, "files": paths, "timeout": 25000}, nil, 30*time.Second)
}

// EgoDownload 通过官方下载事件保存到私有临时目录，返回字节后清理。
func (s *Session) EgoDownload(selector string, timeout time.Duration) ([]byte, error) {
	if !s.IsEgo() {
		return nil, fmt.Errorf("当前会话不是 ego lite")
	}
	if timeout <= 0 || strings.TrimSpace(selector) == "" {
		return nil, fmt.Errorf("缺少下载入口或有效超时")
	}
	dir, err := os.MkdirTemp("", "tulai-ego-download-")
	if err != nil {
		return nil, err
	}
	defer func() {
		// 先回收仍可能 saveAs 的进程，避免取消后重新创建临时文件。
		if s.ctx.Err() != nil {
			s.ego.close()
		}
		_ = os.RemoveAll(dir)
	}()
	path, err := filepath.Abs(filepath.Join(dir, "result"))
	if err != nil {
		return nil, err
	}
	err = s.egoSDKRequest("sdkDownload", map[string]any{"selector": selector, "path": path, "timeout": max(int64(1), timeout.Milliseconds())}, nil, timeout)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxResourceBytes {
		return nil, fmt.Errorf("下载超过 64 MiB 上限")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxResourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResourceBytes {
		return nil, fmt.Errorf("下载超过 64 MiB 上限")
	}
	return data, nil
}
