package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// loginProbeJS 跟页面桥的 loginWall、composer 的 hasComposer 用同一组信号。
// 页面上还有「登录」按钮或扫码层就是没登录。豆包未登录时聊天框也在，
// 但二维码要等点开右上角登录才出现，不能因为有输入框就当成已登录。
// 滑块、安全验证或 punish 页是 challenge，不算退出登录。
// 地区限制页是 region，不算退出登录。连接断开时不关用户的标签页。
const loginProbeJS = `(async function () {
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
    var keys = ['扫码登录', '手机号登录', '短信登录', '验证码登录', '密码登录', '手机登录', '邮箱登录', 'Log in to Qwen', 'Sign in to Qwen', 'Continue with Google', 'Continue with GitHub'];
    var nodes = document.querySelectorAll('[role="dialog"],[class*="dialog"],[class*="modal"],[class*="login"],[class*="sign"]');
    for (var i = 0; i < nodes.length; i++) {
      if (!visible(nodes[i])) continue;
      var text = String(nodes[i].innerText || '').slice(0, 300);
      for (var k = 0; k < keys.length; k++) if (text.indexOf(keys[k]) >= 0) return true;
    }
    return false;
  }
  function loginButton() {
    var nodes = document.querySelectorAll('button,[role="button"],a');
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      if (!visible(el)) continue;
      var t = String(el.innerText || '').trim();
      if (t === '登录' || t === '扫码登录' || t === '立即登录' || t === '登录/注册' || t === '注册' || /^log\s*in$/i.test(t) || /^sign\s*in$/i.test(t) || /^sign\s*up$/i.test(t)) return true;
    }
    return false;
  }
  function toLoginButton() {
    var nodes = document.querySelectorAll('[data-testid="to_login_button"],.header-right-auth-button');
    for (var i = 0; i < nodes.length; i++) if (visible(nodes[i])) return true;
    return false;
  }
  function passwordField() {
    var nodes = document.querySelectorAll('input[type="password"]');
    for (var i = 0; i < nodes.length; i++) if (visible(nodes[i])) return true;
    return false;
  }
  // 生成中、进度条、或文档还没画完，都不是「已退出」的证据。
  function generating() {
    if (document.readyState && document.readyState !== 'complete') return true;
    if (document.querySelector('[aria-busy="true"],[role="progressbar"],button.stop-button')) return true;
    var text = String((document.body && document.body.innerText) || '').slice(0, 2000);
    return /正在生成|生成中|绘制中|\bGenerating\b/.test(text);
  }
  function signedIn() {
    if (document.querySelector('[class*="avatar"],[class*="Avatar"],[data-testid*="avatar"],.user-menu-btn,.sidebar-user .user-profile-desktop,.user-img')) return true;
    var text = String((document.body && document.body.innerText) || '').slice(0, 2500);
    return text.indexOf('退出登录') >= 0 || text.indexOf('退出登陆') >= 0 || /\b(?:Log out|Sign out)\b/i.test(text);
  }
  function challenge() {
    var href = String(location.href || '');
    if (href.indexOf('_____tmd_____') >= 0 || href.indexOf('/punish') >= 0) return true;
    if (document.querySelector('#aliyunCaptcha-sliding-slider,[class*="baxia-dialog"],iframe[src*="punish"],iframe[src*="captcha"]')) return true;
    var body = String((document.body && document.body.innerText) || '').slice(0, 1200);
    return /安全验证|滑动验证|拖动滑块|请按住滑块|请完成下方验证|Please complete .* verification|Slide to verify|Security Verification/i.test(body);
  }
  var href = String(location.href || '');
  if (href.indexOf('/security/doubao-region-ban') >= 0 || href.indexOf('qianwen-region-ban') >= 0 || href.indexOf('tongyi-region-ban') >= 0 || href.indexOf('qwen-region-ban') >= 0) return {state:'region'};
  var host = String(location.hostname || '');
  if (/qianwen|tongyi|qwen/i.test(host)) {
    var top = String((document.body && document.body.innerText) || '').slice(0, 500);
    if (/受区域限制|当前地区暂不支持|所在地区暂不可用|服务在当前地区不可用|not available in your region|region is not supported|service is not available in your/i.test(top)) return {state:'region'};
  }
  if (challenge()) return {state:'challenge', evidence:'challenge'};
  if (generating()) return {state:'unknown', evidence:'generating'};
  var hasComposer = composer();
  // 退出必须有正面证据：登录层、密码框，或明确的登录按钮且页面上没有已登录痕迹。
  // 生成中途闪一下「登录」不能把正在出图的账号打成未登录。
  if (loginDialog() || passwordField()) return {state:'out', evidence:'login_dialog'};
  if (toLoginButton()) return {state:'out', evidence:'to_login_button'};
  if (loginButton() && !signedIn()) return {state:'out', evidence:'login_button'};
  if (hasComposer || signedIn()) return {state:'in', evidence:'composer'};
  var body = String((document.body && document.body.innerText) || '').slice(0, 800);
  if (/扫码登录|手机号登录|短信登录/.test(body)) return {state:'out', evidence:'login_text'};
  return {state:'unknown', evidence:'none'};
})()`

// accountUsernameJS 只读提取豆包或千问当前已登录账号的用户名，不点击页面也不发网络请求。
const accountUsernameJS = `(async function () {
  var INVALID = {
    '登录': 1, '立即登录': 1, '登录/注册': 1, '扫码登录': 1, '登录千问': 1, '登录豆包': 1,
    '退出登录': 1, '退出登陆': 1, '设置': 1, '个人中心': 1, '我的': 1, '账号': 1, '账户': 1,
    '游客': 1, '未登录': 1, '豆包': 1, '千问': 1, '通义千问': 1, '通义': 1, 'qwen': 1, 'doubao': 1,
    '免费版': 1, '升级': 1, '会员': 1, '开通会员': 1, '新对话': 1, '历史记录': 1, '更多': 1,
    '展开': 1, '收起': 1, '头像': 1, '用户头像': 1, 'avatar': 1,
    'sign in': 1, 'log in': 1, 'login': 1, 'sign up': 1, '注册': 1,
    'log out': 1, 'sign out': 1, 'guest': 1, 'settings': 1,
    'free': 1, 'plus': 1, 'pro': 1, 'upgrade': 1
  };
  function clean(raw) {
    var s = String(raw || '').replace(/[\u0000-\u001F\u007F\u200B-\u200D\uFEFF]/g, ' ').replace(/\s+/g, ' ').trim();
    if (!s || s.length > 48) return '';
    if (INVALID[s.toLowerCase()]) return '';
    return s;
  }
  function isHiddenIcon(el) {
    if (!el || !el.tagName) return false;
    var tag = el.tagName.toUpperCase();
    if (tag === 'SVG' || tag === 'STYLE' || tag === 'SCRIPT' || tag === 'NOSCRIPT') return true;
    return !!(el.getAttribute && el.getAttribute('aria-hidden') === 'true');
  }
  function extractFromContainer(root) {
    if (!root) return '';
    var nodes = root.querySelectorAll('span,p,div,strong,b,a');
    var fallbackShort = '';
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      if (isHiddenIcon(el)) continue;
      if (el.closest && el.closest('svg,[aria-hidden="true"]')) continue;
      var hasContainerChild = false;
      for (var c = 0; c < el.children.length; c++) {
        var ct = el.children[c].tagName.toUpperCase();
        if (ct !== 'SVG' && ct !== 'IMG' && ct !== 'BR') { hasContainerChild = true; break; }
      }
      if (hasContainerChild) continue;
      var raw = '';
      for (var k = 0; k < el.childNodes.length; k++) {
        if (el.childNodes[k].nodeType === 3) raw += el.childNodes[k].nodeValue || '';
      }
      var t = clean(raw);
      if (!t) continue;
      var cls = String(el.className || '');
      if (t.length === 1 && /rounded-full|avatar/i.test(cls)) {
        if (!fallbackShort) fallbackShort = t;
        continue;
      }
      return t;
    }
    if (fallbackShort) return fallbackShort;
    return clean(root.textContent);
  }
  function fromDoubaoDOM() {
    var menu = document.querySelector('[data-sidebar-footer-account-menu] button[aria-haspopup="menu"],[data-sidebar-footer-account-menu] button,[data-sidebar-footer-account-menu]');
    var n = extractFromContainer(menu);
    if (n) return n;
    var av = document.querySelector('[data-testid="chat_header_avatar_button"]');
    if (av) {
      var btn = (av.closest && av.closest('button,[role="button"],[data-sidebar-footer-account-menu]')) || av.parentElement;
      n = extractFromContainer(btn);
      if (n) return n;
      n = clean(av.getAttribute && (av.getAttribute('alt') || av.getAttribute('aria-label')));
      if (n) return n;
    }
    return '';
  }
  function fromDoubaoRouter() {
    try {
      var rd = window._ROUTER_DATA;
      if (!rd || !rd.loaderData || typeof rd.loaderData !== 'object') return '';
      var queue = [rd.loaderData];
      var seen = [];
      for (var depth = 0; depth < 3 && queue.length; depth++) {
        var next = [];
        for (var i = 0; i < queue.length; i++) {
          var obj = queue[i];
          if (!obj || typeof obj !== 'object' || seen.indexOf(obj) >= 0) continue;
          seen.push(obj);
          if (obj.accountInfo && obj.accountInfo.data && typeof obj.accountInfo.data === 'object') {
            var d = obj.accountInfo.data;
            if (d.is_visitor_account === true) continue;
            var n = clean(d.screen_name || d.name || d.nick_name || d.nickname);
            if (n) return n;
          }
          var keys = Object.keys(obj);
          for (var k = 0; k < keys.length && k < 20; k++) {
            var v = obj[keys[k]];
            if (v && typeof v === 'object') next.push(v);
          }
        }
        queue = next;
      }
    } catch (e) {}
    return '';
  }
  function fromQwenDOM() {
    var scopes = [
      document.querySelector('#new-nav-tab-wrapper'),
      document.querySelector('aside'),
      document.querySelector('[data-testid="qianwen-layout-left-panel"]')
    ];
    for (var s = 0; s < scopes.length; s++) {
      var scope = scopes[s];
      if (!scope) continue;
      var btns = scope.querySelectorAll('button[aria-haspopup="menu"],[role="button"][aria-haspopup="menu"]');
      for (var i = btns.length - 1; i >= 0; i--) {
        var b = btns[i];
        if (b.getAttribute('data-session-switch-target') === 'session-tools') continue;
        if (b.closest && b.closest('[data-session-id]')) continue;
        if (!b.querySelector('img')) continue;
        var n = extractFromContainer(b);
        if (n) return n;
      }
      for (var j = btns.length - 1; j >= 0; j--) {
        var b2 = btns[j];
        if (b2.getAttribute('data-session-switch-target') === 'session-tools') continue;
        if (b2.closest && b2.closest('[data-session-id]')) continue;
        if (!b2.querySelector('[class*="avatar"],[class*="Avatar"],[class*="rounded-full"]')) continue;
        var n2 = extractFromContainer(b2);
        if (n2) return n2;
      }
    }
    return '';
  }
  function fromQwenGlobal() {
    try {
      var u = window._USER_;
      if (u && typeof u === 'object') {
        var n = clean(u.showName || u.nickName || u.nickname || u.userName || u.name);
        if (n) return n;
      }
    } catch (e) {}
    return '';
  }
  function fromQwenIntlDOM() {
    var selectors = [
      '.user-menu-btn .user-menu-btn-text',
      '.user-menu-btn .user-menu-btn-text-pad',
      '.user-profile-desktop .user-menu-btn-text',
      '.user-profile-desktop .user-menu-btn-text-pad',
      '.sidebar-user .user-menu-btn'
    ];
    for (var i = 0; i < selectors.length; i++) {
      var el = document.querySelector(selectors[i]);
      if (!el) continue;
      var n = extractFromContainer(el);
      if (n) return n;
    }
    return '';
  }
  function fromGenericSidebar() {
    var btns = document.querySelectorAll('aside button[aria-haspopup="menu"],nav button[aria-haspopup="menu"],header button[aria-haspopup="menu"]');
    for (var i = btns.length - 1; i >= 0; i--) {
      var b = btns[i];
      if (b.closest && b.closest('[data-session-id]')) continue;
      if (!b.querySelector('img')) continue;
      var n = extractFromContainer(b);
      if (n) return n;
    }
    return '';
  }
  var host = String(location.hostname || '').toLowerCase();
  var username = '';
  if (/(^|\.)doubao\.com$/.test(host)) {
    username = fromDoubaoDOM() || fromDoubaoRouter() || fromGenericSidebar();
  } else if (/(^|\.)qwen\.ai$/.test(host)) {
    username = fromQwenIntlDOM() || fromQwenGlobal() || fromGenericSidebar();
  } else if (/(qianwen|tongyi|qwen)/i.test(host)) {
    username = fromQwenDOM() || fromQwenGlobal() || fromGenericSidebar();
  } else {
    username = fromDoubaoDOM() || fromQwenIntlDOM() || fromQwenDOM() || fromDoubaoRouter() || fromQwenGlobal() || fromGenericSidebar();
  }
  return { username: username || '' };
})()`

type loginProbeResult struct {
	State string `json:"state"`
}

type accountUsernameResult struct {
	Username string `json:"username"`
}

var invalidAccountUsernames = map[string]struct{}{
	"登录": {}, "立即登录": {}, "登录/注册": {}, "扫码登录": {}, "登录千问": {}, "登录豆包": {}, "注册": {},
	"退出登录": {}, "退出登陆": {}, "设置": {}, "个人中心": {}, "我的": {}, "账号": {}, "账户": {},
	"游客": {}, "未登录": {}, "豆包": {}, "千问": {}, "千问国际": {}, "通义千问": {}, "通义": {}, "qwen": {}, "doubao": {},
	"免费版": {}, "升级": {}, "会员": {}, "开通会员": {}, "新对话": {}, "历史记录": {}, "更多": {},
	"展开": {}, "收起": {}, "头像": {}, "用户头像": {}, "avatar": {},
	"sign in": {}, "log in": {}, "login": {}, "sign up": {}, "log out": {}, "sign out": {},
	"guest": {}, "settings": {}, "free": {}, "plus": {}, "pro": {}, "upgrade": {},
}

// SanitizeAccountUsername 清洗从网页提取的账号用户名，过滤控制字符、过长文本与常见按钮文案。
func SanitizeAccountUsername(raw string) string {
	var b strings.Builder
	b.Grow(len(raw))
	space := false
	for _, r := range raw {
		if unicode.IsControl(r) || r == '\u200b' || r == '\u200c' || r == '\u200d' || r == '\ufeff' || unicode.IsSpace(r) {
			if b.Len() > 0 && !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		b.WriteRune(r)
		space = false
	}
	s := strings.TrimSpace(b.String())
	if s == "" || utf8.RuneCountInString(s) > 48 {
		return ""
	}
	if _, bad := invalidAccountUsernames[strings.ToLower(s)]; bad {
		return ""
	}
	return s
}

// ProbeLogin 附着到已经打开的页面，读一次登录状态，然后只断开调试连接。
// endpoint 可以是浏览器的 WebSocket 地址，或 http://127.0.0.1:端口。
// RegionBlocked 是地区限制页。向导应提示去设置代理，而不是继续等扫码。
const RegionBlocked = "region"

// RegionBlockedURL 报告这个地址是不是地区限制页。
// 豆包是 /security/doubao-region-ban。千问没有公开的固定路径，平行的
// qianwen-region-ban、tongyi-region-ban、qwen-region-ban 一并认。
func RegionBlockedURL(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	if u == "" {
		return false
	}
	return strings.Contains(u, "/security/doubao-region-ban") ||
		strings.Contains(u, "qianwen-region-ban") ||
		strings.Contains(u, "tongyi-region-ban") ||
		strings.Contains(u, "qwen-region-ban")
}

// 返回 in、out、challenge、region 或 unknown。没有页面时返回 unknown，方便继续等。
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
	if RegionBlockedURL(targetURL(targets, id)) {
		return RegionBlocked, nil
	}
	var result loginProbeResult
	if err := conn.evalJSON(ctx, id, loginProbeJS, &result, true, true); err != nil {
		return "", err
	}
	return NormalizeLoginState(result.State), nil
}

// ProbeAccountUsername 附着到已经打开的平台页面，读取当前已登录账号的用户名。
// 未识别到用户名时返回空串和 nil，不关闭标签页。
func ProbeAccountUsername(ctx context.Context, endpoint string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
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
		return "", nil
	}
	if RegionBlockedURL(targetURL(targets, id)) {
		return "", nil
	}
	var result accountUsernameResult
	if err := conn.evalJSON(ctx, id, accountUsernameJS, &result, true, true); err != nil {
		return "", err
	}
	return SanitizeAccountUsername(result.Username), nil
}

func targetURL(list []sliderTarget, id string) string {
	for _, item := range list {
		if item.ID == id {
			return item.URL
		}
	}
	return ""
}

// NormalizeLoginState 只保留 in、out、challenge、region、unknown。
func NormalizeLoginState(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	var wrapped loginProbeResult
	if json.Unmarshal([]byte(raw), &wrapped) == nil && wrapped.State != "" {
		return NormalizeLoginState(wrapped.State)
	}
	var encoded string
	if json.Unmarshal([]byte(raw), &encoded) == nil && encoded != raw {
		return NormalizeLoginState(encoded)
	}
	switch raw {
	case "in", "out", "challenge", "unknown", RegionBlocked:
		return raw
	default:
		return "unknown"
	}
}

// LoginState 在已经打开的页面上读一次登录状态，不新开标签。
func (s *Session) LoginState() (string, error) {
	if s == nil {
		return "unknown", errors.New("没有页面")
	}
	out, err := s.EvaluateAsync(loginProbeJS)
	if err != nil {
		return "", err
	}
	return NormalizeLoginState(out), nil
}

// BringToFront 把豆包或千问那个标签拉到前面。没有站点页时激活当前页面。
// 只断开调试连接，不关闭标签。
func BringToFront(ctx context.Context, endpoint string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return err
	}
	id, ok := pickLoginTarget(targets)
	if !ok {
		return errors.New("没有可前置的页面")
	}
	_ = conn.moveWindow(ctx, id, true)
	return conn.Activate(ctx, id)
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
