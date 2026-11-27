package cdp

import (
	"encoding/json"
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
	for _, needle := range []string{"evidence:'generating'", "evidence:'login_dialog'", "signedIn()", "passwordField()", ".header-right-auth-button", "button.stop-button", ".user-menu-btn", "Continue with Google", "gem-open-account-menu", "sidenav-error-action-link", "button.send-button.stop", "accounts.google.com"} {
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
	} {
		if !strings.Contains(accountUsernameJS, needle) {
			t.Fatalf("accountUsernameJS missing %q", needle)
		}
	}
}
