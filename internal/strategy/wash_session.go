package strategy

import (
	"encoding/json"
	"fmt"

	"github.com/xxww0098/tulai/extension"
	"github.com/xxww0098/tulai/internal/cdp"
)

// ReusableDoubaoChat returns the current conversation only when its last reply is complete.
func ReusableDoubaoChat(s *cdp.Session) string {
	if s == nil || s.IsEgo() {
		return ""
	}
	raw, err := s.Evaluate(`(() => {
		if (!/(^|\.)doubao\.com$/i.test(location.hostname) || !/^\/chat\/[^/]+/.test(location.pathname)) return '';
		if (!window.__tulaiLiveImages || !window.__tulaiPendingGeneration) return '';
		var stopping = Array.from(document.querySelectorAll('button,[role="button"]')).some(function (button) {
			var box = button.getBoundingClientRect();
			return box.width > 8 && box.height > 8 && [button.innerText, button.getAttribute('aria-label'), button.getAttribute('title')].some(function (label) {
				return /停止生成|停止响应|^停止$|\bStop generating\b/i.test(String(label || '').trim());
			});
		});
		var live = window.__tulaiLiveImages();
		return !stopping && live.done && !live.busy && live.images.length && !window.__tulaiPendingGeneration()
			? location.origin + location.pathname : '';
	})()`)
	if err != nil {
		return ""
	}
	var url string
	if cdp.ParseJSValue(raw, &url) != nil {
		return ""
	}
	return url
}

// PrepareDoubaoTurns reuses the extension's prompt-anchored reply detection.
func PrepareDoubaoTurns(s *cdp.Session) error {
	page := extbundle.DoubaoPageJS()
	if page == "" {
		return fmt.Errorf("豆包对话边界脚本不可用")
	}
	code := `(() => { if (!window.__tulaiMarkDoubaoTurn) return false; if (!window.__tulaiWashPage) {` + page + `
		window.__tulaiWashPage = window.TULAIPage; }
		return !!(window.__tulaiWashPage && window.__tulaiWashPage.mark && window.__tulaiWashPage.newAssistants && window.__tulaiWashPage.revealResults); })()`
	if !s.EvaluateBool(code) {
		return fmt.Errorf("豆包对话边界初始化失败")
	}
	return nil
}

func MarkDoubaoTurn(s *cdp.Session, prompt string) error {
	text, err := json.Marshal(prompt)
	if err != nil {
		return err
	}
	if !s.EvaluateBool(`!!(window.__tulaiMarkDoubaoTurn && window.__tulaiMarkDoubaoTurn(` + string(text) + `))`) {
		return fmt.Errorf("豆包对话边界标记失败")
	}
	return nil
}
