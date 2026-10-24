package captcha

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// IkunocrSolver ikunocr 接码平台后端（异步提交 + 轮询）。
type IkunocrSolver struct{ cfg Config }

// NewIkunocrSolver 构造器。
func NewIkunocrSolver(cfg Config) *IkunocrSolver { return &IkunocrSolver{cfg: cfg} }

// Name 后端名。
func (i *IkunocrSolver) Name() string { return "ikunocr" }

const ikunocrBase = "http://www.ikunocr.cn/api"

// Solve 提交识别任务并轮询结果。
func (i *IkunocrSolver) Solve(ctx context.Context, req Request) Answer {
	if i.cfg.IkunocrKey == "" {
		return Answer{Error: "未配置 ikunocr API Key"}
	}
	img, err := decodeBase64(req.GridBase64)
	if err != nil {
		return Answer{Error: "图片解码失败: " + err.Error()}
	}
	timeout := time.Duration(i.cfg.IkunocrTimeut) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 提交（type=1026 九宫格）
	form := url.Values{"key": {i.cfg.IkunocrKey}, "image": {base64Std(img)}, "type": {"1026"}}
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, ikunocrBase+"/yibu/create", strings.NewReader(form.Encode()))
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var create struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			TaskID string `json:"task_id"`
		} `json:"data"`
	}
	if err := doJSON(httpReq, &create); err != nil {
		return Answer{Error: "提交失败: " + err.Error()}
	}
	if create.Code != 0 || create.Data.TaskID == "" {
		return Answer{Error: "提交失败: " + create.Msg}
	}
	// 轮询
	deadline := time.Now().Add(timeout - 2*time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return Answer{Error: "超时"}
		case <-time.After(700 * time.Millisecond):
		}
		q := url.Values{"key": {i.cfg.IkunocrKey}, "task_id": {create.Data.TaskID}}
		get, _ := http.NewRequestWithContext(ctx, http.MethodGet, ikunocrBase+"/yibu/get?"+q.Encode(), nil)
		var result struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				Result any `json:"result"`
			} `json:"data"`
		}
		if err := doJSON(get, &result); err != nil {
			continue
		}
		switch result.Code {
		case 0:
			return parseIkunocrResult(result.Data.Result)
		case 1: // 处理中
			continue
		default:
			return Answer{Error: "识别失败: " + result.Msg}
		}
	}
	return Answer{Error: "轮询超时"}
}

// parseIkunocrResult 兼容 "1,4" / [1,4] / "1 4" / 首个数字等返回形态。
func parseIkunocrResult(v any) Answer {
	var indices []int
	switch r := v.(type) {
	case []any:
		for _, it := range r {
			if n, err := toInt(it); err == nil && n >= 1 && n <= 9 {
				indices = append(indices, n)
			}
		}
	case string:
		for _, f := range strings.FieldsFunc(r, func(c rune) bool { return c == ',' || c == ' ' || c == '，' }) {
			if n, err := toInt(f); err == nil && n >= 1 && n <= 9 {
				indices = append(indices, n)
			}
		}
	}
	if len(indices) == 0 {
		return Answer{Error: fmt.Sprintf("结果无法解析: %v", v)}
	}
	return Answer{Success: true, Indices: indices, Confidence: 0.8}
}

func doJSON(req *http.Request, out any) error {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(raw, out)
}

func base64Std(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
