package cdp

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeGeminiStatusWindowsAndCreditsAndPlans(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, loc)

	t.Run("5-hour window primary for AI Pro", func(t *testing.T) {
		fiveReset := now.Add(4*time.Hour + 30*time.Minute)
		weekReset := now.Add(5 * 24 * time.Hour)
		q, ok := NormalizeGeminiStatus(GeminiStatus{
			OK:               true,
			Source:           "graphql",
			Username:         "Alice Wang",
			Email:            "alice@gmail.com",
			SubscriptionTier: "TIER1",
			HasFiveHour:      true,
			FiveHourPctUsed:  15.0,
			FiveHourResetAt:  fiveReset.UnixMilli(),
			HasWeekly:        true,
			WeeklyPctUsed:    8.0,
			WeeklyResetAt:    weekReset.UnixMilli(),
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditsRemain != 85 || q.CreditsTotal != 100 || q.CreditsCycle != GeminiQuotaCycleFiveHour {
			t.Fatalf("5h quota mismatch: %+v", q)
		}
		if q.CreditsResetAt != fiveReset.Unix() {
			t.Fatalf("resetAt=%d want=%d", q.CreditsResetAt, fiveReset.Unix())
		}
		if q.MemberTier != "pro" || q.MemberName != "AI Pro" {
			t.Fatalf("plan mismatch: %+v", q)
		}
	})

	t.Run("weekly bottleneck wins when tighter than 5-hour window", func(t *testing.T) {
		fiveReset := now.Add(3 * time.Hour)
		weekReset := now.Add(3 * 24 * time.Hour)
		q, ok := NormalizeGeminiStatus(GeminiStatus{
			OK:               true,
			SubscriptionTier: "TIER2",
			HasFiveHour:      true,
			FiveHourPctUsed:  20.0,
			FiveHourResetAt:  fiveReset.UnixMilli(),
			HasWeekly:        true,
			WeeklyPctUsed:    95.0,
			WeeklyResetAt:    weekReset.UnixMilli(),
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditsRemain != 5 || q.CreditsTotal != 100 || q.CreditsCycle != GeminiQuotaCycleWeekly || q.CreditsResetAt != weekReset.Unix() {
			t.Fatalf("weekly bottleneck mismatch: %+v", q)
		}
		if q.MemberTier != "ultra" || q.MemberName != "AI Ultra" {
			t.Fatalf("ultra plan mismatch: %+v", q)
		}
	})

	t.Run("exhausted window falls back to active overage AI credits", func(t *testing.T) {
		fiveReset := now.Add(2 * time.Hour)
		q, ok := NormalizeGeminiStatus(GeminiStatus{
			OK:                  true,
			SubscriptionTier:    "TIER_NEON",
			HasFiveHour:         true,
			FiveHourPctUsed:     100,
			FiveHourResetAt:     fiveReset.UnixMilli(),
			HasCredits:          true,
			RemainingCredits:    1250,
			UseOverageAiCredits: true,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditsRemain != 1250 || q.CreditsTotal != 0 || q.CreditsCycle != GeminiQuotaCycleAICredits {
			t.Fatalf("overage ai credits mismatch: %+v", q)
		}
		if q.MemberTier != "ultra" || q.MemberName != "AI Ultra" {
			t.Fatalf("tier neon plan mismatch: %+v", q)
		}
	})

	t.Run("exhausted window without overage reports 0 percent", func(t *testing.T) {
		fiveReset := now.Add(2 * time.Hour)
		q, ok := NormalizeGeminiStatus(GeminiStatus{
			OK:               true,
			SubscriptionTier: "TIER_HELIUM",
			HasFiveHour:      true,
			FiveHourPctUsed:  100,
			FiveHourResetAt:  fiveReset.UnixMilli(),
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditsRemain != 0 || q.CreditsTotal != 100 || q.CreditsCycle != GeminiQuotaCycleFiveHour {
			t.Fatalf("exhausted mismatch: %+v", q)
		}
		if q.MemberTier != "plus" || q.MemberName != "AI Plus" {
			t.Fatalf("plus plan mismatch: %+v", q)
		}
	})

	t.Run("free plan without meter pools still syncs tier", func(t *testing.T) {
		q, ok := NormalizeGeminiStatus(GeminiStatus{
			OK:               true,
			SubscriptionTier: "FREE",
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditsRemain != -1 || q.CreditsTotal != 0 || q.MemberTier != "free" || q.MemberName != "免费版" || q.SyncedAt != now.Unix() {
			t.Fatalf("free plan-only mismatch: %+v", q)
		}
	})

	t.Run("non-ok status ignored", func(t *testing.T) {
		if _, ok := NormalizeGeminiStatus(GeminiStatus{OK: false}, now); ok {
			t.Fatal("expected ok=false")
		}
	})
}

func TestPickGeminiTargetAndScriptAnchors(t *testing.T) {
	id, ok := pickGeminiTarget([]sliderTarget{
		{ID: "blank", Type: "page", URL: "about:blank"},
		{ID: "iframe", Type: "iframe", URL: "https://gemini.google.com/app"},
		{ID: "gem", Type: "page", URL: "https://gemini.google.com/app?pli=1"},
	})
	if !ok || id != "gem" {
		t.Fatalf("pickGeminiTarget = (%q, %v), want (gem, true)", id, ok)
	}
	for _, needle := range []string{
		"gemini\\.google\\.com",
		"2/oee4e7iJB+YlSRDtGuxpv36rfWVddgii370z2JvCuVM=",
		"GXU_FIVE_HOURLY",
		"GXU_WEEKLY",
		"OVERAGE_CREDITS",
		"windowRefreshesAt",
		"jSf9Qc",
		"qpEbW",
		"oPEP7c",
	} {
		if !strings.Contains(geminiStatusProbeJS, needle) {
			t.Fatalf("geminiStatusProbeJS missing %q", needle)
		}
	}
	for _, forbidden := range []string{"innerHTML", "outerHTML"} {
		if strings.Contains(geminiStatusProbeJS, forbidden) {
			t.Fatalf("geminiStatusProbeJS must not use %q (Trusted Types CSP)", forbidden)
		}
	}
}
