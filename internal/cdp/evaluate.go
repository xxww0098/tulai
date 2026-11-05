package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// EvaluateContext runs a short expression without closing the account page on cancellation.
// Long-lived page requests must provide their own AbortController.
func (s *Session) EvaluateContext(ctx context.Context, expression string, out any) error {
	if s == nil || s.client == nil || s.client.proc == nil {
		return errors.New("没有可控制的 Camoufox 页面")
	}
	work, cancel := context.WithTimeout(ctx, 60*time.Second)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	var value struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception json.RawMessage `json:"exceptionDetails"`
	}
	if err := s.client.proc.Call(work, s.target, "Runtime.evaluate", map[string]any{
		"expression": expression, "awaitPromise": true, "returnByValue": true, "readOnly": true,
	}, &value); err != nil {
		return err
	}
	if len(value.Exception) > 0 && string(value.Exception) != "null" {
		return errors.New("页面脚本执行失败，请查看账号浏览器")
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(value.Result.Value, out)
}
