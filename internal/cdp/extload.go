package cdp

import (
	"context"
	"errors"
	"strings"
	"time"
)

const extensionName = "tulai 页面桥"

// EnsureExtension 确认这个 profile 里已经有页面桥。
// --load-extension 被品牌版 Chrome / Edge 忽略时，再用 Extensions.loadUnpacked。
// 调用方需要带上 --enable-unsafe-extension-debugging，否则后一条会失败。
func EnsureExtension(ctx context.Context, endpoint, dir string) error {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	loaded, err := extensionLoaded(ctx, endpoint)
	if err != nil {
		return err
	}
	if loaded {
		return nil
	}
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	var out struct {
		ID string `json:"id"`
	}
	if err := conn.call(ctx, "", "Extensions.loadUnpacked", map[string]any{"path": dir}, &out); err != nil {
		return err
	}
	if out.ID == "" {
		return errors.New("扩展没有装上")
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := extensionLoaded(ctx, endpoint); ok {
			return nil
		}
		if err := sleepCtx(ctx, 200*time.Millisecond); err != nil {
			return err
		}
	}
	return errors.New("扩展已加载但没有看到页面桥")
}

func extensionLoaded(ctx context.Context, endpoint string) (bool, error) {
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return false, err
	}
	for _, item := range targets {
		if item.ID == "" || !strings.HasPrefix(item.URL, "chrome-extension://") {
			continue
		}
		if item.Type != "service_worker" && item.Type != "background_page" {
			continue
		}
		var snap struct {
			Name string `json:"name"`
		}
		err := conn.EvalJSON(ctx, item.ID, `({name:(chrome.runtime.getManifest()||{}).name||""})`, &snap)
		if err == nil && snap.Name == extensionName {
			return true, nil
		}
	}
	return false, nil
}
