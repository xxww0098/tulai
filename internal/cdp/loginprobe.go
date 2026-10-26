package cdp

import (
	"context"
	"strings"
	"time"
)

// loginProbeJS 跟页面桥的 loginWall、composer 的 hasComposer 用同一组信号：
// 看得见输入框，并且没有扫码登录层，才算已登录。连接断开时不关用户的标签页。
const loginProbeJS = `(function () {
  function visible(el) {
    if (!el) return false;
    var r = el.getBoundingClientRect();
    if (r.width < 5 || r.height < 5) return false;
    var s = getComputedStyle(el);
    if (s.visibility === 'hidden' || s.display === 'none') return false;
    if (el.disabled || (el.getAttribute && el.getAttribute('aria-disabled') === 'true')) return false;
    return true;
  }
  function composer() {
    var nodes = document.querySelectorAll('textarea,[contenteditable="true"],input[type="text"]');
    for (var i = nodes.length - 1; i >= 0; i--) if (visible(nodes[i])) return true;
    return false;
  }
  function loginDialog() {
    var keys = ['扫码登录', '手机号登录', '短信登录', '验证码登录', '密码登录', '手机登录'];
    var nodes = document.querySelectorAll('[role="dialog"],[class*="dialog"],[class*="modal"],[class*="login"],[class*="sign"]');
    for (var i = 0; i < nodes.length; i++) {
      if (!visible(nodes[i])) continue;
      var text = String(nodes[i].innerText || '').slice(0, 300);
      for (var k = 0; k < keys.length; k++) if (text.indexOf(keys[k]) >= 0) return true;
    }
    return false;
  }
  function loginButton() {
    var nodes = document.querySelectorAll('button,[role="button"],a,input[type="password"]');
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      if (!visible(el)) continue;
      if (el.type === 'password') return true;
      var t = String(el.innerText || '').trim();
      if (t === '登录' || t === '扫码登录' || t === '立即登录') return true;
    }
    return false;
  }
  var hasComposer = composer();
  if (hasComposer && !loginDialog()) return {state:'in'};
  if (loginDialog() || loginButton()) return {state:'out'};
  var body = String((document.body && document.body.innerText) || '').slice(0, 800);
  if (!hasComposer && /扫码登录|手机号登录|短信登录/.test(body)) return {state:'out'};
  return {state:'unknown'};
})()`

type loginProbeResult struct {
	State string `json:"state"`
}

// ProbeLogin 附着到已经打开的页面，读一次登录状态，然后只断开调试连接。
// endpoint 可以是浏览器的 WebSocket 地址，或 http://127.0.0.1:端口。
// 返回 in、out 或 unknown。没有页面时返回 unknown，方便继续等。
func ProbeLogin(ctx context.Context, endpoint string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return "", err
	}
	id, ok := pickLoginTarget(targets)
	if !ok {
		return "unknown", nil
	}
	var result loginProbeResult
	if err := conn.EvalJSON(ctx, id, loginProbeJS, &result); err != nil {
		return "", err
	}
	switch result.State {
	case "in", "out", "unknown":
		return result.State, nil
	default:
		return "unknown", nil
	}
}

func pickLoginTarget(list []sliderTarget) (string, bool) {
	var https, page string
	for _, item := range list {
		if item.Type != "page" || item.ID == "" {
			continue
		}
		url := strings.ToLower(item.URL)
		if strings.Contains(url, "doubao.com") || strings.Contains(url, "qianwen.com") || strings.Contains(url, "tongyi.com") || strings.Contains(url, "qwen.ai") {
			return item.ID, true
		}
		if page == "" {
			page = item.ID
		}
		if https == "" && (strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")) {
			https = item.ID
		}
	}
	if https != "" {
		return https, true
	}
	if page != "" {
		return page, true
	}
	return "", false
}
