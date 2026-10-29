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
	for _, needle := range []string{"evidence:'generating'", "evidence:'login_dialog'", "signedIn()", "passwordField()"} {
		if !strings.Contains(loginProbeJS, needle) {
			t.Fatalf("probe script missing %q", needle)
		}
	}
	busy := strings.Index(loginProbeJS, "if (generating()) return {state:'unknown', evidence:'generating'}")
	button := strings.Index(loginProbeJS, "if (loginButton() && !signedIn()) return {state:'out', evidence:'login_button'}")
	loggedIn := strings.Index(loginProbeJS, "if (hasComposer || signedIn()) return {state:'in', evidence:'composer'}")
	if busy < 0 || button < busy || loggedIn < button {
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
