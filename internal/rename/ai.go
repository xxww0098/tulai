package rename

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Suggest asks an OpenAI-compatible model for one new name per input name.
func Suggest(ctx context.Context, endpoint, key, model, instruction string, names []string) ([]string, error) {
	if strings.TrimSpace(endpoint) == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(model) == "" {
		return nil, errors.New("请先配置模型 API 地址、API Key 和模型名称")
	}
	if strings.TrimSpace(instruction) == "" || len(names) == 0 {
		return nil, errors.New("请先输入重命名要求并添加文件")
	}
	namesJSON, err := json.Marshal(names)
	if err != nil {
		return nil, errors.New("文件名编码失败")
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "根据用户要求批量重命名文件。只返回 JSON 字符串数组，按输入顺序对应每个文件，数量必须一致。每项只能是文件名，保留扩展名，除非用户明确要求修改。"},
			{"role": "user", "content": "重命名要求：" + instruction + "\n文件名：" + string(namesJSON)},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("模型请求编码失败")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.Host == "" {
		return nil, errors.New("模型 API 地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{CheckRedirect: func(next *http.Request, via []*http.Request) error {
		if next.URL.Scheme != via[0].URL.Scheme || next.URL.Host != via[0].URL.Host {
			return errors.New("模型 API 不允许跨站重定向")
		}
		if len(via) >= 10 {
			return errors.New("模型 API 重定向次数过多")
		}
		return nil
	}}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("模型请求失败，请检查地址和网络")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("模型 API 返回状态 %d", resp.StatusCode)
	}
	const maxResponse = 1 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(raw) > maxResponse {
		return nil, errors.New("模型响应读取失败或超过大小限制")
	}
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || len(result.Choices) == 0 {
		return nil, errors.New("模型响应格式无效")
	}
	content := strings.TrimSpace(result.Choices[0].Message.Content)
	if strings.HasPrefix(content, "```") {
		firstLine, rest, ok := strings.Cut(content, "\n")
		if !ok || (firstLine != "```" && !strings.EqualFold(firstLine, "```json")) || !strings.HasSuffix(rest, "```") {
			return nil, errors.New("模型没有返回有效的文件名数组")
		}
		content = strings.TrimSpace(strings.TrimSuffix(rest, "```"))
	}
	var suggestions []string
	if err := json.Unmarshal([]byte(content), &suggestions); err != nil || len(suggestions) != len(names) {
		return nil, errors.New("模型返回的文件名数组无效或数量不匹配")
	}
	for _, name := range suggestions {
		if strings.TrimSpace(name) == "" {
			return nil, errors.New("模型返回了空文件名")
		}
	}
	return suggestions, nil
}
