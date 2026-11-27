package cdp

import (
	"slices"
	"testing"
	"time"
)

func TestPickPlatformTargetCoversAllProviders(t *testing.T) {
	targets := []sliderTarget{
		{ID: "t1", Type: "page", URL: "https://www.doubao.com/chat/"},
		{ID: "t2", Type: "page", URL: "https://www.qianwen.com/"},
		{ID: "t3", Type: "page", URL: "https://chat.qwen.ai/"},
		{ID: "t4", Type: "page", URL: "https://gemini.google.com/app"},
		{ID: "t5", Type: "page", URL: "https://jimeng.jianying.com/ai-tool/generate/?type=image"},
	}
	for _, tc := range []struct {
		platform string
		wantID   string
	}{
		{"豆包", "t1"},
		{"千问", "t2"},
		{"千问国际", "t3"},
		{"Gemini", "t4"},
		{"即梦", "t5"},
	} {
		got, ok := pickPlatformTarget(targets, tc.platform)
		if !ok || got != tc.wantID {
			t.Errorf("pickPlatformTarget(%s) = (%q, %v), want (%q, true)", tc.platform, got, ok, tc.wantID)
		}
	}
	if _, ok := pickPlatformTarget(targets, "未知"); ok {
		t.Fatal("unknown platform must not match any target")
	}
}

func TestNormalizeRawOutputCapsValidation(t *testing.T) {
	now := time.Unix(1795781000, 0)
	if _, err := normalizeRawOutputCaps("千问", rawOutputCapsProbe{OK: false, Error: "未找到千问比例入口"}, now); err == nil {
		t.Fatal("expected error for OK=false probe")
	}
	if _, err := normalizeRawOutputCaps("千问", rawOutputCapsProbe{OK: true, Ratios: []string{"1:1"}}, now); err == nil {
		t.Fatal("expected error for incomplete probe with only 1 ratio and no auto")
	}

	caps, err := normalizeRawOutputCaps("千问国际", rawOutputCapsProbe{
		OK:           true,
		SupportsAuto: true,
		AutoValue:    "auto",
		AutoLabel:    "Auto",
		Ratios:       []string{"1:1", "16:9", "4:5"},
		CurrentRatio: "auto",
	}, now)
	if err != nil {
		t.Fatalf("normalizeRawOutputCaps failed: %v", err)
	}
	if caps.Source != "live" || caps.SyncedAt != now.Unix() || !caps.SupportsAuto || caps.AutoValue != "auto" || caps.AutoLabel != "Auto" {
		t.Fatalf("unexpected normalized caps: %+v", caps)
	}
	if !slices.Equal(caps.Ratios, []string{"1:1", "16:9", "4:5"}) {
		t.Fatalf("unexpected ratios: %v", caps.Ratios)
	}
}
