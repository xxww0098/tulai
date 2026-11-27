package cdp

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestRegionBlockedURL(t *testing.T) {
	blocked := []string{
		"https://www.doubao.com/security/doubao-region-ban?source=1",
		"https://www.doubao.com/chat/security/doubao-region-ban",
		"https://www.qianwen.com/security/qianwen-region-ban",
		"https://www.tongyi.com/security/tongyi-region-ban",
		"https://chat.qwen.ai/security/qwen-region-ban",
		"https://gemini.google.com/unsupported-country",
		"https://jimeng.jianying.com/security/jimeng-region-ban",
	}
	for _, raw := range blocked {
		if !RegionBlockedURL(raw) {
			t.Fatalf("expected region ban: %s", raw)
		}
	}
	open := []string{
		"",
		"https://www.doubao.com/chat/",
		"https://www.tongyi.com/",
		"https://www.qianwen.com/",
		"https://gemini.google.com/app",
		"https://jimeng.jianying.com/ai-tool/generate/?type=image",
		"https://example.com/security/not-a-ban",
	}
	for _, raw := range open {
		if RegionBlockedURL(raw) {
			t.Fatalf("not a region ban: %s", raw)
		}
	}
}

func TestPickLoginTargetPrefersTheOpenSite(t *testing.T) {
	list := []sliderTarget{
		{ID: "blank", Type: "page", URL: "about:blank"},
		{ID: "other", Type: "page", URL: "https://example.com/"},
		{ID: "iframe", Type: "iframe", URL: "https://www.doubao.com/chat/"},
		{ID: "doubao", Type: "page", URL: "https://www.doubao.com/chat/"},
	}
	id, ok := pickLoginTarget(list)
	if !ok || id != "doubao" {
		t.Fatalf("pick = %q ok=%v", id, ok)
	}
	https, ok := pickLoginTarget([]sliderTarget{
		{ID: "blank", Type: "page", URL: "about:blank"},
		{ID: "site", Type: "page", URL: "https://www.tongyi.com/"},
	})
	if !ok || https != "site" {
		t.Fatalf("https pick = %q", https)
	}
	geminiTarget, ok := pickLoginTarget([]sliderTarget{
		{ID: "other", Type: "page", URL: "https://accounts.google.com/ServiceLogin"},
		{ID: "gemini", Type: "page", URL: "https://gemini.google.com/app"},
	})
	if !ok || geminiTarget != "gemini" {
		t.Fatalf("gemini pick = %q ok=%v", geminiTarget, ok)
	}
	googleAuth, ok := pickLoginTarget([]sliderTarget{
		{ID: "blank", Type: "page", URL: "about:blank"},
		{ID: "auth", Type: "page", URL: "https://accounts.google.com/v3/signin"},
	})
	if !ok || googleAuth != "auth" {
		t.Fatalf("google auth pick = %q ok=%v", googleAuth, ok)
	}
	jimengTarget, ok := pickLoginTarget([]sliderTarget{
		{ID: "other", Type: "page", URL: "https://example.com/"},
		{ID: "jimeng", Type: "page", URL: "https://jimeng.jianying.com/ai-tool/generate/?type=image"},
	})
	if !ok || jimengTarget != "jimeng" {
		t.Fatalf("jimeng pick = %q ok=%v", jimengTarget, ok)
	}
	if _, ok := pickLoginTarget(nil); ok {
		t.Fatal("empty target list should keep waiting")
	}
}

func TestLoginProbeUsesComposerAndScanLogin(t *testing.T) {
	for _, needle := range []string{"contenteditable", "扫码登录", "state:'in'", "state:'out'", "state:'unknown'", "state:'challenge'", "state:'region'", "/security/doubao-region-ban", "qianwen-region-ban", "受区域限制", "安全验证", "拖动滑块"} {
		if !strings.Contains(loginProbeJS, needle) {
			t.Fatalf("probe script missing %q", needle)
		}
	}
	for _, needle := range []string{"evidence:'generating'", "evidence:'login_dialog'", "signedIn()", "passwordField()", ".header-right-auth-button", "button.stop-button", ".user-menu-btn", "Continue with Google", "gem-open-account-menu", "sidenav-error-action-link", "button.send-button.stop", "accounts.google.com", "anyVisible(", "parseFloat(s.opacity) === 0"} {
		if !strings.Contains(loginProbeJS, needle) {
			t.Fatalf("probe script missing %q", needle)
		}
	}
	busy := strings.Index(loginProbeJS, "if (generating()) return {state:'unknown', evidence:'generating'}")
	toLogin := strings.Index(loginProbeJS, "if (toLoginButton()) return {state:'out', evidence:'to_login_button'}")
	button := strings.Index(loginProbeJS, "if (loginButton() && !signedIn()) return {state:'out', evidence:'login_button'}")
	loggedIn := strings.Index(loginProbeJS, "if (hasComposer || signedIn()) return {state:'in', evidence:'composer'}")
	if busy < 0 || toLogin < busy || button < toLogin || loggedIn < button {
		t.Fatal("generating must not count as logged out, and a login button still beats the composer")
	}
}

func TestNormalizeLoginState(t *testing.T) {
	encoded, err := json.Marshal(`{"state":"out"}`)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		`{"state":"in"}`:        "in",
		`{"state":"challenge"}`: "challenge",
		string(encoded):         "out",
		"in":                    "in",
		"challenge":             "challenge",
		"region":                "region",
		`{"state":"region"}`:    "region",
		"":                      "unknown",
		"nope":                  "unknown",
	}
	for raw, want := range cases {
		if got := NormalizeLoginState(raw); got != want {
			t.Errorf("NormalizeLoginState(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestSanitizeAccountUsername(t *testing.T) {
	cases := map[string]string{
		"xxww":                        "xxww",
		"  Qwen3464 \n\t ":            "Qwen3464",
		"小明\u200b同学":                  "小明 同学",
		"登录":                          "",
		"立即登录":                        "",
		"登录/注册":                       "",
		"退出登录":                        "",
		"豆包":                          "",
		"千问":                          "",
		"千问国际":                        "",
		"Qwen":                        "",
		"Gemini":                      "",
		"即梦":                          "",
		"即梦AI":                        "",
		"积分超市":                        "",
		"即刻登录":                        "",
		"即梦画师":                        "即梦画师",
		"Google":                      "",
		"Google Account":              "",
		"Google 账号":                   "",
		"Ultra":                       "",
		"alice@gmail.com":             "alice@gmail.com",
		"Alice Wang":                  "Alice Wang",
		"Sign in":                     "",
		"Free":                        "",
		"Upgrade":                     "",
		"Sign up":                     "",
		"Log out":                     "",
		"谢威":                          "谢威",
		"免费版":                         "",
		"  ":                          "",
		strings.Repeat("字", 49):       "",
		strings.Repeat("字", 48):       strings.Repeat("字", 48),
		"豆包用户8821":                    "豆包用户8821",
	}
	for raw, want := range cases {
		if got := SanitizeAccountUsername(raw); got != want {
			t.Errorf("SanitizeAccountUsername(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestAccountUsernameScriptAnchors(t *testing.T) {
	for _, needle := range []string{
		"data-sidebar-footer-account-menu",
		"chat_header_avatar_button",
		"window._ROUTER_DATA",
		"is_visitor_account",
		"#new-nav-tab-wrapper",
		"data-session-switch-target",
		"window._USER_",
		"fromQwenIntlDOM",
		".user-menu-btn-text",
		"fromGeminiDOM",
		"fromGeminiGlobal",
		"oPEP7c",
		"parseGoogleAccountLabel",
		"fromJimengAPI",
		"fromJimengDOM",
		"/passport/account/info/v2/",
		"data-main-frame-2026-avatar-action",
	} {
		if !strings.Contains(accountUsernameJS, needle) {
			t.Fatalf("accountUsernameJS missing %q", needle)
		}
	}
}

func TestLoginProbeJSGeminiEdgeCases(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available in PATH")
	}
	scriptJSON, err := json.Marshal(loginProbeJS)
	if err != nil {
		t.Fatal(err)
	}
	harness := `
const vm = require('vm');
const probeSrc = ` + string(scriptJSON) + `;

function makeEl(opts) {
  const o = Object.assign({ w: 40, h: 20, vis: 'visible', disp: 'block', opacity: '1', text: '', disabled: false, attrs: {} }, opts || {});
  return {
    innerText: o.text,
    disabled: o.disabled,
    getBoundingClientRect: () => ({ width: o.w, height: o.h }),
    getAttribute: (k) => (k in o.attrs ? o.attrs[k] : null),
    _style: { visibility: o.vis, display: o.disp, opacity: o.opacity },
  };
}

async function runCase(cfg) {
  const map = cfg.selectors || {};
  const ctx = {
    location: { href: cfg.href || 'https://gemini.google.com/app', hostname: cfg.hostname || 'gemini.google.com' },
    window: { WIZ_global_data: cfg.wiz || {} },
    getComputedStyle: (el) => el._style || { visibility: 'visible', display: 'block', opacity: '1' },
    document: {
      readyState: cfg.readyState || 'complete',
      body: { innerText: cfg.bodyText || '' },
      querySelector: (sel) => {
        const parts = String(sel || '').split(',').map((s) => s.trim());
        for (const [k, list] of Object.entries(map)) {
          if (parts.includes(k) && list.length > 0) return list[0];
        }
        return null;
      },
      querySelectorAll: (sel) => {
        const parts = String(sel || '').split(',').map((s) => s.trim());
        const out = [];
        for (const [k, list] of Object.entries(map)) {
          if (parts.includes(k)) out.push(...list);
        }
        return out;
      },
    },
  };
  return await vm.runInNewContext(probeSrc, ctx);
}

(async () => {
  const results = {
    signedInInteractiveWithProgressbarAndHistoryText: await runCase({
      readyState: 'interactive',
      wiz: { oPEP7c: 'kodram@gmail.com' },
      bodyText: '近期对话\n绘制中测试图\nGemini',
      selectors: {
        '[role="progressbar"]': [makeEl({ w: 200, h: 8, vis: 'visible', disp: 'block' })],
        '[contenteditable="true"]': [makeEl({ w: 400, h: 40 })],
      },
    }),
    signedInWithHiddenStopButton: await runCase({
      readyState: 'complete',
      wiz: { oPEP7c: 'kodram@gmail.com' },
      selectors: {
        'button.send-button.stop': [makeEl({ w: 32, h: 32, opacity: '0' })],
        '[contenteditable="true"]': [makeEl({ w: 400, h: 40 })],
      },
    }),
    signedInActivelyGenerating: await runCase({
      readyState: 'complete',
      wiz: { oPEP7c: 'kodram@gmail.com' },
      selectors: {
        'button.send-button.stop': [makeEl({ w: 32, h: 32, opacity: '1' })],
      },
    }),
    loggedOutComplete: await runCase({
      readyState: 'complete',
      selectors: {
        'button': [makeEl({ text: 'Sign in' })],
      },
    }),
    loggedOutStillLoading: await runCase({
      readyState: 'interactive',
      selectors: {
        'button': [makeEl({ text: 'Sign in' })],
      },
    }),
  };
  process.stdout.write(JSON.stringify(results));
})().catch((err) => { console.error(err); process.exit(1); });
`
	out, err := exec.Command(nodeBin, "-e", harness).CombinedOutput()
	if err != nil {
		t.Fatalf("node harness failed: %v\n%s", err, string(out))
	}
	var got map[string]struct {
		State    string `json:"state"`
		Evidence string `json:"evidence"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal node output: %v\n%s", err, string(out))
	}
	if got["signedInInteractiveWithProgressbarAndHistoryText"].State != "in" {
		t.Fatalf("signed-in interactive Gemini with progressbar/history text got %+v, want in", got["signedInInteractiveWithProgressbarAndHistoryText"])
	}
	if got["signedInWithHiddenStopButton"].State != "in" {
		t.Fatalf("signed-in Gemini with opacity:0 stop button got %+v, want in", got["signedInWithHiddenStopButton"])
	}
	if got["signedInActivelyGenerating"].State != "unknown" || got["signedInActivelyGenerating"].Evidence != "generating" {
		t.Fatalf("actively generating Gemini got %+v, want unknown/generating", got["signedInActivelyGenerating"])
	}
	if got["loggedOutComplete"].State != "out" {
		t.Fatalf("logged-out complete Gemini got %+v, want out", got["loggedOutComplete"])
	}
	if got["loggedOutStillLoading"].State != "unknown" {
		t.Fatalf("logged-out interactive Gemini got %+v, want unknown", got["loggedOutStillLoading"])
	}
}

func TestLoginProbeJSJimengEdgeCases(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available in PATH")
	}
	scriptJSON, err := json.Marshal(loginProbeJS)
	if err != nil {
		t.Fatal(err)
	}
	harness := `
const vm = require('vm');
const probeSrc = ` + string(scriptJSON) + `;

function makeEl(opts) {
  const o = Object.assign({ w: 40, h: 20, vis: 'visible', disp: 'block', opacity: '1', text: '', disabled: false, attrs: {}, hiddenAncestor: false }, opts || {});
  return {
    innerText: o.text,
    disabled: o.disabled,
    getBoundingClientRect: () => ({ width: o.w, height: o.h }),
    getAttribute: (k) => (k in o.attrs ? o.attrs[k] : null),
    closest: (sel) => (o.hiddenAncestor && sel.includes('aria-hidden') ? {} : null),
    querySelector: (sel) => (o.stopChild && sel.includes('stop') ? {} : null),
    _style: { visibility: o.vis, display: o.disp, opacity: o.opacity },
  };
}

async function runCase(cfg) {
  const map = cfg.selectors || {};
  const ctx = {
    location: { href: 'https://jimeng.jianying.com/ai-tool/generate/?type=image', hostname: 'jimeng.jianying.com' },
    window: {},
    getComputedStyle: (el) => el._style || { visibility: 'visible', display: 'block', opacity: '1' },
    document: {
      readyState: 'complete',
      body: { innerText: cfg.bodyText || '' },
      querySelector: (sel) => {
        for (const [k, list] of Object.entries(map)) {
          if (sel.includes(k) && list.length > 0) return list[0];
        }
        return null;
      },
      querySelectorAll: (sel) => {
        const out = [];
        for (const [k, list] of Object.entries(map)) {
          if (sel.includes(k)) out.push(...list);
        }
        return out;
      },
    },
  };
  return await vm.runInNewContext(probeSrc, ctx);
}

(async () => {
  const results = {
    signedInWithAvatarAndCredits: await runCase({
      selectors: {
        '[contenteditable="true"]': [
          makeEl({ w: 400, h: 40, hiddenAncestor: true }),
          makeEl({ w: 400, h: 40 }),
        ],
        'data-main-frame-2026-avatar-action': [makeEl({ w: 32, h: 32 })],
        'data-main-frame-2026-credit-display-anchor': [makeEl({ w: 60, h: 24, text: '94' })],
      },
    }),
    loggedOutWithComposerAndLoginBtn: await runCase({
      selectors: {
        '[contenteditable="true"]': [makeEl({ w: 400, h: 40 })],
        'login-button-wrapper-': [makeEl({ w: 64, h: 32, text: '登录' })],
      },
    }),
    activelyGenerating: await runCase({
      selectors: {
        'dreamina.generator.submit': [makeEl({ w: 36, h: 36, attrs: { 'aria-label': '停止生成' } })],
        '[contenteditable="true"]': [makeEl({ w: 400, h: 40 })],
      },
    }),
  };
  process.stdout.write(JSON.stringify(results));
})().catch((err) => { console.error(err); process.exit(1); });
`
	out, err := exec.Command(nodeBin, "-e", harness).CombinedOutput()
	if err != nil {
		t.Fatalf("node harness failed: %v\n%s", err, string(out))
	}
	var got map[string]struct {
		State    string `json:"state"`
		Evidence string `json:"evidence"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, string(out))
	}
	if got["signedInWithAvatarAndCredits"].State != "in" {
		t.Fatalf("signed-in jimeng got %+v, want in", got["signedInWithAvatarAndCredits"])
	}
	if got["loggedOutWithComposerAndLoginBtn"].State != "out" {
		t.Fatalf("logged-out jimeng got %+v, want out", got["loggedOutWithComposerAndLoginBtn"])
	}
	if got["activelyGenerating"].State != "unknown" || got["activelyGenerating"].Evidence != "generating" {
		t.Fatalf("generating jimeng got %+v, want unknown/generating", got["activelyGenerating"])
	}
}
