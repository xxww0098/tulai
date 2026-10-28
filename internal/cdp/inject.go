package cdp

import (
	_ "embed"
	"strings"
)

//go:embed base.js
var baseJS string

// BaseJS 返回基础页面运行时脚本。
func BaseJS() string { return baseJS }

// InjectBase 在当前文档执行基础脚本（新文档注入由 SetNavigateInjection 处理）。
func (s *Session) InjectBase() error {
	_, err := s.Evaluate(baseJS)
	return err
}

// EnsureBase 确保基础脚本就绪（幂等）。
func (s *Session) EnsureBase() {
	if !s.EvaluateBool("window.__tulaiBaseInstalled === 3") {
		_ = s.InjectBase()
	}
}

// AppendNavigateScript 把脚本追加到导航注入链（去重）。
func (s *Session) AppendNavigateScript(js string) {
	s.mu.Lock()
	if !contains(s.newDoc, js) {
		s.newDoc = append(s.newDoc, js)
	}
	scripts := append([]string{baseJS}, s.newDoc...)
	s.mu.Unlock()
	s.SetNavigateInjection(strings.Join(scripts, "\n;\n"))
	// 当前文档也立即执行一次
	_, _ = s.Evaluate(js)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
