package cdp

import (
	"encoding/json"
	"strings"
	"testing"
)

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
	for _, needle := range []string{"contenteditable", "扫码登录", "state:'in'", "state:'out'", "state:'unknown'", "state:'challenge'", "安全验证", "拖动滑块"} {
		if !strings.Contains(loginProbeJS, needle) {
			t.Fatalf("probe script missing %q", needle)
		}
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
		"":                      "unknown",
		"nope":                  "unknown",
	}
	for raw, want := range cases {
		if got := NormalizeLoginState(raw); got != want {
			t.Errorf("NormalizeLoginState(%q) = %q, want %q", raw, got, want)
		}
	}
}
