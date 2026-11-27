package store

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestDefaultProviderOutputCapsMatchEachWebpage(t *testing.T) {
	doubao := DefaultProviderOutputCaps("豆包")
	if !doubao.SupportsAuto || doubao.AutoValue != "自动" || doubao.AutoLabel != "自动" || len(doubao.Ratios) != 7 || len(doubao.Resolutions) != 0 {
		t.Fatalf("unexpected 豆包 default caps: %+v", doubao)
	}

	qianwen := DefaultProviderOutputCaps("千问")
	if qianwen.SupportsAuto || qianwen.DefaultRatio != "3:4" || len(qianwen.Ratios) != 7 || len(qianwen.Resolutions) != 0 {
		t.Fatalf("unexpected 千问 default caps: %+v", qianwen)
	}

	qwenIntl := DefaultProviderOutputCaps("千问国际")
	if qwenIntl.SupportsAuto || qwenIntl.DefaultRatio != "16:9" || len(qwenIntl.Ratios) != 7 || len(qwenIntl.Resolutions) != 0 {
		t.Fatalf("unexpected 千问国际 default caps: %+v", qwenIntl)
	}

	gemini := DefaultProviderOutputCaps("Gemini")
	if !gemini.SupportsAuto || gemini.AutoLabel != "默认比例" || len(gemini.Ratios) != 5 || slices.Contains(gemini.Ratios, "3:2") {
		t.Fatalf("unexpected Gemini default caps: %+v", gemini)
	}

	jimeng := DefaultProviderOutputCaps("即梦")
	if !jimeng.SupportsAuto || jimeng.AutoValue != "智能" || jimeng.AutoLabel != "智能比例" || !slices.Contains(jimeng.Ratios, "21:9") || len(jimeng.Ratios) != 8 || len(jimeng.Resolutions) != 3 || jimeng.DefaultResolution != "2K" {
		t.Fatalf("unexpected 即梦 default caps: %+v", jimeng)
	}
}

func TestSaveAndLoadProviderOutputCapsNormalization(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Unsynced platform falls back to its built-in default caps.
	if _, ok := st.GetOutputCaps("即梦"); ok {
		t.Fatal("unsynced 即梦 should not have cached caps")
	}
	if eff := st.EffectiveOutputCaps("即梦"); eff.Source != "default" || eff.AutoLabel != "智能比例" {
		t.Fatalf("unexpected unsynced effective caps: %+v", eff)
	}

	// Reject broken / empty probes without overwriting cache.
	if err := st.SaveOutputCaps("豆包", ProviderOutputCaps{Ratios: []string{"1:1"}}); err == nil {
		t.Fatal("expected single-ratio probe without auto to be rejected")
	}
	if err := st.SaveOutputCaps("未知平台", ProviderOutputCaps{Ratios: []string{"1:1", "4:3"}}); err == nil {
		t.Fatal("expected unsupported platform to be rejected")
	}

	// Save live probed caps with custom webpage ratios & resolutions (full-width colon + duplicate + lowercase resolution).
	err = st.SaveOutputCaps("即梦", ProviderOutputCaps{
		SyncedAt:          1795780000,
		Model:             "Seedream 5.0",
		SupportsAuto:      true,
		AutoValue:         "智能",
		AutoLabel:         "智能比例",
		Ratios:            []string{"21：9", "16:9", "16:9", "4:5", "invalid", "1:1"},
		Resolutions:       []string{"2k", "4k", "2K"},
		DefaultResolution: "4k",
		CurrentRatio:      "4:5",
		CurrentResolution: "4k",
	})
	if err != nil {
		t.Fatalf("SaveOutputCaps failed: %v", err)
	}

	got, ok := st.GetOutputCaps("即梦")
	if !ok {
		t.Fatal("expected cached 即梦 caps after save")
	}
	if got.Source != "live" || got.SyncedAt != 1795780000 || got.Model != "Seedream 5.0" {
		t.Fatalf("unexpected live caps metadata: %+v", got)
	}
	wantRatios := []string{"21:9", "16:9", "4:5", "1:1"}
	if !slices.Equal(got.Ratios, wantRatios) {
		t.Fatalf("ratios = %v, want %v", got.Ratios, wantRatios)
	}
	wantRes := []string{"2K", "4K"}
	if !slices.Equal(got.Resolutions, wantRes) || got.DefaultResolution != "4K" || got.CurrentResolution != "4K" {
		t.Fatalf("resolutions = %v default=%q current=%q, want %v / 4K", got.Resolutions, got.DefaultResolution, got.CurrentResolution, wantRes)
	}
}
