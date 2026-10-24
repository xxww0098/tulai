// Package captcha 验证码自动求解：三后端（free 纯CV / custom 视觉大模型 / ikunocr 接码）
// + 页面注入脚本。对应原软件 CaptchaSolver 与 Captcha/ 目录能力。
package captcha

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Request 页面侧发来的求解请求（来自注入脚本桥调用）。
type Request struct {
	Type        string `json:"type"` // grid | slider
	GridBase64  string `json:"gridBase64"`
	TargetBase6 string `json:"targetBase64"`
	PromptText  string `json:"promptText"`
}

// Answer 求解结果，回传页面。
type Answer struct {
	Success    bool    `json:"success"`
	Indices    []int   `json:"indices,omitempty"` // 九宫格命中序号 1-9
	Ratio      float64 `json:"ratio,omitempty"`   // 滑块缺口比例 0-1
	Offset     int     `json:"offset,omitempty"`  // 滑块像素偏移（兜底）
	Confidence float64 `json:"confidence,omitempty"`
	Error      string  `json:"error,omitempty"`
}

// Solver 求解后端接口。
type Solver interface {
	Name() string
	Solve(ctx context.Context, req Request) Answer
}

// Config 求解配置（来自设置）。
type Config struct {
	Backend       string
	CustomURL     string
	CustomKey     string
	CustomModel   string
	CustomTimeout int
	IkunocrKey    string
	IkunocrTimeut int
	IkunocrTrack  string
}

// Build 按配置构造后端链：主后端 + custom 兜底（免费后端仅支持带目标图的拖拽）。
func Build(cfg Config) Solver {
	var primary Solver = NewCustomSolver(cfg)
	switch cfg.Backend {
	case "ikunocr":
		primary = NewIkunocrSolver(cfg)
	case "free":
		primary = NewFreeSolver(cfg)
	}
	return fallbackChain{primary: primary, secondary: NewCustomSolver(cfg)}
}

type fallbackChain struct {
	primary   Solver
	secondary Solver
}

func (f fallbackChain) Name() string { return f.primary.Name() }

func (f fallbackChain) Solve(ctx context.Context, req Request) Answer {
	ans := f.primary.Solve(ctx, req)
	if ans.Success {
		return ans
	}
	// 滑块与描述类九宫格 free 后端解不了，交给 custom 兜底（若配置了 key）
	if f.secondary != nil {
		if ans2 := f.secondary.Solve(ctx, req); ans2.Success {
			return ans2
		}
	}
	return ans
}

// parseJSONObject 从模型返回文本中提取 JSON 对象（兼容 markdown 代码块包裹）。
func parseJSONObject(s string) (map[string]any, bool) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		var lines []string
		in := false
		for _, line := range strings.Split(s, "\n") {
			if strings.HasPrefix(line, "```") {
				in = !in
				continue
			}
			if in {
				lines = append(lines, line)
			}
		}
		if len(lines) > 0 {
			s = strings.Join(lines, "\n")
		}
	}
	// 直接试
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) == nil {
		return m, true
	}
	// 提取第一个 {...}
	for from := 0; ; {
		i := strings.Index(s[from:], "{")
		if i < 0 {
			return nil, false
		}
		i += from
		for depth, j := 0, i; j < len(s); j++ {
			switch s[j] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					if json.Unmarshal([]byte(s[i:j+1]), &m) == nil {
						return m, true
					}
				}
			}
		}
		from = i + 1
	}
}

// answerFromModel 把模型 JSON 映射为 Answer（支持 indices/index 旧格式）。
func answerFromModel(m map[string]any) Answer {
	ans := Answer{}
	if v, ok := m["indices"]; ok {
		if arr, ok := v.([]any); ok {
			for _, it := range arr {
				if n, err := toInt(it); err == nil {
					ans.Indices = append(ans.Indices, n)
				}
			}
		}
	}
	if len(ans.Indices) == 0 {
		if n, err := toInt(m["index"]); err == nil && n > 0 {
			ans.Indices = []int{n}
		}
	}
	if n, err := toInt(m["offset"]); err == nil {
		ans.Offset = n
	}
	if f, err := toFloat(m["ratio"]); err == nil {
		ans.Ratio = f
	}
	if f, err := toFloat(m["confidence"]); err == nil {
		ans.Confidence = f
	}
	ans.Success = len(ans.Indices) > 0 || ans.Ratio > 0 || ans.Offset > 0
	return ans
}

func toInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(n))
	}
	return 0, fmt.Errorf("非数值")
}

func toFloat(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
	}
	return 0, fmt.Errorf("非数值")
}
