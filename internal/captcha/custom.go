package captcha

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CustomSolver 视觉大模型后端（OpenAI 兼容 chat/completions）。
type CustomSolver struct{ cfg Config }

// NewCustomSolver 构造器。
func NewCustomSolver(cfg Config) *CustomSolver { return &CustomSolver{cfg: cfg} }

// Name 后端名。
func (c *CustomSolver) Name() string { return "custom" }

// Solve 调视觉模型识别。
func (c *CustomSolver) Solve(ctx context.Context, req Request) Answer {
	if c.cfg.CustomURL == "" {
		return Answer{Error: "未配置视觉模型 API 地址"}
	}
	if c.cfg.CustomKey == "" {
		return Answer{Error: "未配置视觉模型 API Key"}
	}
	timeout := time.Duration(c.cfg.CustomTimeout) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var prompt string
	var imageB64 string
	if req.Type == "slider" {
		imageB64 = req.GridBase64
		prompt = "这是一张滑块验证码的截图。上方背景图右侧有一个不规则缺口，下方左侧是滑块按钮。" +
			"请计算缺口左边缘到背景图左边界的距离占背景图总宽度的比例。\n" +
			`只返回JSON：{"ratio": 0到1的小数, "confidence": 0到1的小数}`
	} else {
		imageB64 = req.GridBase64
		if req.PromptText != "" {
			prompt = "在以下9张图中寻找\n" + req.PromptText +
				"\n9张图序号1-9：第一行1/2/3，第二行4/5/6，第三行7/8/9。" +
				`请告诉我符合的图片序号。只返回JSON：{"indices": [序号列表], "confidence": 小数}`
		} else {
			prompt = "这是一张验证码截图。顶部有一句中文提示文字（请从图中直接阅读），" +
				"中间是3×3九宫格（序号1-9：第一行1/2/3，第二行4/5/6，第三行7/8/9）。\n" +
				`找出所有符合提示的图标序号。只返回JSON：{"indices": [序号列表], "confidence": 小数}`
		}
	}

	payload := map[string]any{
		"model": c.cfg.CustomModel,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": prompt},
				{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + imageB64}},
			},
		}},
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.CustomURL, bytes.NewReader(body))
	if err != nil {
		return Answer{Error: err.Error()}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.cfg.CustomKey)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return Answer{Error: "API 请求失败: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Answer{Error: fmt.Sprintf("API 返回 %d: %s", resp.StatusCode, truncStr(string(raw), 200))}
	}
	var data struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &data); err != nil || len(data.Choices) == 0 {
		return Answer{Error: "API 返回格式异常: " + truncStr(string(raw), 200)}
	}
	m, ok := parseJSONObject(data.Choices[0].Message.Content)
	if !ok {
		return Answer{Error: "模型未返回 JSON: " + truncStr(data.Choices[0].Message.Content, 200)}
	}
	ans := answerFromModel(m)
	if !ans.Success {
		ans.Error = "模型返回无有效答案"
	}
	return ans
}

func truncStr(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func decodeBase64(s string) ([]byte, error) {
	if i := strings.Index(s, ","); i >= 0 && strings.HasPrefix(s, "data:") {
		s = s[i+1:]
	}
	return base64.StdEncoding.DecodeString(s)
}

func encodeB64ForTest(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
