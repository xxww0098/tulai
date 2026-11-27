package cdp

import (
	"testing"
	"time"
)

func TestNormalizeDoubaoStatusFreePaidAndLimited(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 27, 13, 30, 0, 0, loc)

	t.Run("free healthy account keeps CreditsRemain=-1 and tracks usagePercent", func(t *testing.T) {
		q, ok := NormalizeDoubaoStatus(DoubaoStatus{
			OK:                    true,
			HasActiveSubscription: false,
			IsFreeUser:            true,
			ShortName:             "免费体验",
			QuotaRemainingAmount:  "0",
			HasUsagePercent:       true,
			UsedPercent:           25,
			ResetCardCount:        1,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.MemberTier != "free" || q.MemberName != "免费体验" || q.CreditsRemain != -1 || q.UsagePercent != 25 || q.PlatformLimited || q.ResetCardCount != 1 {
			t.Fatalf("unexpected free doubao quota: %+v", q)
		}
	})

	t.Run("free rate-limited account marks PlatformLimited and reset timestamp", func(t *testing.T) {
		resumeAt := now.Add(20 * time.Minute).Unix()
		q, ok := NormalizeDoubaoStatus(DoubaoStatus{
			OK:              true,
			IsFreeUser:      true,
			HasUsagePercent: true,
			UsedPercent:     100,
			IsLimited:       true,
			RateLimited:     true,
			RateLimitTime:   resumeAt,
			RateLimitTips:   "发送过于频繁，请稍后再试",
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if !q.PlatformLimited || q.UsagePercent != 100 || q.CreditsResetAt != resumeAt || q.LimitTips != "发送过于频繁，请稍后再试" {
			t.Fatalf("unexpected limited doubao quota: %+v", q)
		}
	})

	t.Run("paid subscription parses remaining quota, total and renewal time", func(t *testing.T) {
		nextDeduct := time.Date(2026, 12, 27, 0, 0, 0, 0, loc).Unix()
		q, ok := NormalizeDoubaoStatus(DoubaoStatus{
			OK:                    true,
			HasActiveSubscription: true,
			IsFreeUser:            false,
			SKUKey:                "doubao_personal_std",
			ProductName:           "个人订阅",
			QuotaRemainingAmount:  "8420",
			NextDeductTime:        nextDeduct,
			HasUsagePercent:       true,
			UsedPercent:           6,
			ResetCardCount:        2,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.MemberTier != "vip" || q.MemberName != "标准订阅" || q.CreditsRemain != 8420 || q.CreditsTotal != 9000 {
			t.Fatalf("unexpected paid doubao quota: %+v", q)
		}
		if q.CreditsCycle != "monthly" || q.CreditsResetAt != nextDeduct || q.ResetCardCount != 2 {
			t.Fatalf("unexpected paid doubao reset/cards: %+v", q)
		}
	})
}
