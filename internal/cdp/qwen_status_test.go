package cdp

import (
	"testing"
	"time"
)

func TestNormalizeQwenStatusDailyAnd7DayAndVIP(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, loc)

	t.Run("daily free account", func(t *testing.T) {
		recv := time.Date(2026, 11, 27, 11, 37, 0, 0, loc)
		exp := time.Date(2026, 11, 27, 23, 59, 59, 0, loc)
		q, ok := NormalizeQwenStatus(QwenStatus{
			OK:              true,
			HasCredits:      true,
			CreditsRemain:   31,
			SignInCredits:   31,
			CreditCost:      1,
			GrantAmount:     40,
			GrantReceiveAt:  recv.UnixMilli(),
			GrantExpiredAt:  exp.UnixMilli(),
			GrantExpiredStr: "2026-11-27 23:59:59",
			MemberType:      "free",
			MemberName:      "免费套餐",
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditsRemain != 31 || q.CreditsTotal != 40 || q.CreditCost != 1 {
			t.Fatalf("credits mismatch: %+v", q)
		}
		if q.CreditsCycle != "daily" || q.CreditsResetAt != exp.Unix() {
			t.Fatalf("cycle/reset mismatch: cycle=%q reset=%d want=%d", q.CreditsCycle, q.CreditsResetAt, exp.Unix())
		}
		if q.MemberTier != "free" || q.MemberName != "免费套餐" || q.AccountConcurrency != QwenFreeConcurrencyCap || q.WatermarkFree {
			t.Fatalf("member/concurrency mismatch: %+v", q)
		}
	})

	t.Run("7-day cohort inferred from prior expiry", func(t *testing.T) {
		prevGrant := time.Date(2026, 10, 29, 22, 45, 0, 0, loc)
		prevExp := time.Date(2026, 11, 5, 22, 45, 0, 0, loc)
		latestGrant := time.Date(2026, 11, 26, 3, 51, 0, 0, loc)
		q, ok := NormalizeQwenStatus(QwenStatus{
			OK:             true,
			HasCredits:     true,
			CreditsRemain:  0,
			CreditCost:     1,
			GrantAmount:    40,
			GrantReceiveAt: latestGrant.UnixMilli(),
			PrevGrantAt:    prevGrant.UnixMilli(),
			PrevExpiredAt:  prevExp.UnixMilli(),
			MemberType:     "free",
			MemberName:     "免费套餐",
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		wantReset := latestGrant.Add(7 * 24 * time.Hour).Unix()
		if q.CreditsRemain != 0 || q.CreditsTotal != 40 || q.CreditsCycle != "7d" || q.CreditsResetAt != wantReset {
			t.Fatalf("7d inference mismatch: got %+v wantReset=%d", q, wantReset)
		}
	})

	t.Run("vip account unlocks unlimited concurrency and watermark removal", func(t *testing.T) {
		exp := time.Date(2026, 12, 27, 23, 59, 59, 0, loc)
		q, ok := NormalizeQwenStatus(QwenStatus{
			OK:               true,
			HasCredits:       true,
			CreditsRemain:    760,
			VIPCredits:       760,
			CreditCost:       1,
			MemberType:       "standard_vip",
			SubscribeStatus:  1,
			VIPActive:        true,
			VIPName:          "标准会员",
			VIPMonthlyCredit: 800,
			MemberExpireAt:   exp.UnixMilli(),
			WatermarkFree:    true,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.MemberTier != "vip" || q.MemberName != "标准会员" || q.CreditsTotal != 800 || q.AccountConcurrency != QwenVIPConcurrencyUnlimited || !q.WatermarkFree {
			t.Fatalf("vip normalization mismatch: %+v", q)
		}
		if q.CreditsCycle != "monthly" || q.CreditsResetAt != exp.Unix() {
			t.Fatalf("vip cycle/reset mismatch: %+v", q)
		}
	})

	t.Run("non-ok status ignored", func(t *testing.T) {
		if _, ok := NormalizeQwenStatus(QwenStatus{OK: false}, now); ok {
			t.Fatal("expected ok=false")
		}
	})
}
