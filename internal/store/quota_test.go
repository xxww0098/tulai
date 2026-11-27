package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateBrowserQuotaPersistenceAndRecovery(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	b, err := st.AddBrowser("千问", "千问1", filepath.Join(t.TempDir(), "p1"))
	if err != nil {
		t.Fatal(err)
	}
	if b.QuotaSynced() || b.CreditsRemain != -1 {
		t.Fatalf("new browser should start unsynced with remain=-1, got synced=%v remain=%d", b.QuotaSynced(), b.CreditsRemain)
	}

	now := time.Now().Truncate(time.Second)
	resetAt := now.Add(12 * time.Hour).Unix()

	// 模拟因积分不足进入 exhausted 状态
	st.UpdateBrowserHealth(b.ID, HealthExhausted, resetAt, "积分不足，请购买积分或开通创作会员")
	st.MarkBrowserCreditsExhausted(b.ID, now)

	rows := st.Browsers()
	if len(rows) != 1 || !rows[0].QuotaSynced() || rows[0].CreditsRemain != 0 || rows[0].Health != HealthExhausted {
		t.Fatalf("after MarkBrowserCreditsExhausted got %+v", rows)
	}

	// 同步到新的可用积分后，应写入全部字段并自动解除额度挂起
	q := AccountQuota{
		CreditsRemain:      31,
		CreditsTotal:       40,
		CreditCost:         1,
		CreditsResetAt:     resetAt,
		CreditsCycle:       "daily",
		MemberTier:         "free",
		MemberName:         "免费套餐",
		AccountConcurrency: 6,
		WatermarkFree:      false,
		SyncedAt:           now.Add(time.Minute).Unix(),
	}
	if err := st.UpdateBrowserQuota(b.ID, q); err != nil {
		t.Fatalf("UpdateBrowserQuota: %v", err)
	}

	rows = st.Browsers()
	if len(rows) != 1 {
		t.Fatalf("expected 1 browser, got %d", len(rows))
	}
	got := rows[0]
	if !got.QuotaSynced() || got.CreditsRemain != 31 || got.CreditsTotal != 40 || got.CreditCost != 1 {
		t.Fatalf("credits mismatch: %+v", got)
	}
	if got.CreditsResetAt != resetAt || got.CreditsCycle != "daily" || got.MemberTier != "free" || got.MemberName != "免费套餐" {
		t.Fatalf("cycle/member mismatch: %+v", got)
	}
	if got.AccountConcurrency != 6 || got.WatermarkFree {
		t.Fatalf("concurrency/watermark mismatch: %+v", got)
	}
	if got.Health != HealthReady || got.CoolUntil != 0 || got.LastError != "" {
		t.Fatalf("expected quota exhausted state to auto-recover when credits > 0, got health=%q cool=%d err=%q", got.Health, got.CoolUntil, got.LastError)
	}
	if got.QuotaExpired(now) {
		t.Fatal("quota should not be expired before resetAt")
	}
	if !got.QuotaExpired(time.Unix(resetAt+1, 0)) {
		t.Fatal("quota should be expired after resetAt")
	}
}

func TestMigrateHealsSyncedAccountLoginFlag(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.AddBrowser("Gemini", "Kodram Dritken", filepath.Join(t.TempDir(), "g1"))
	if err != nil {
		t.Fatal(err)
	}
	// 模拟历史行：已同步实时额度，但 is_logged_in 仍为 0
	if err := st.UpdateBrowserQuota(b.ID, AccountQuota{
		CreditsRemain: 99,
		CreditsTotal:  100,
		CreditsCycle:  "5h",
		MemberTier:    "ultra",
		MemberName:    "AI Ultra",
		SyncedAt:      time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE browsers SET is_logged_in=0 WHERE id=?`, b.ID); err != nil {
		t.Fatal(err)
	}
	st.Close()

	reopened, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows := reopened.Browsers()
	if len(rows) != 1 || !rows[0].IsLoggedIn {
		t.Fatalf("expected synced account to heal is_logged_in=true on startup, got %+v", rows)
	}
}

func TestDoubaoQuotaPersistenceLimitedAndAutoRecovery(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	b, err := st.AddBrowser("豆包", "豆包1", filepath.Join(t.TempDir(), "d1"))
	if err != nil {
		t.Fatal(err)
	}
	if b.QuotaSynced() || b.CreditsRemain != -1 || b.UsagePercent != -1 {
		t.Fatalf("new doubao browser should start unsynced, got synced=%v remain=%d usagePct=%d", b.QuotaSynced(), b.CreditsRemain, b.UsagePercent)
	}

	now := time.Now()
	until := now.Add(15 * time.Minute).Unix()

	// 模拟豆包限流进入冷却并标记平台限额
	st.UpdateBrowserHealth(b.ID, HealthCooling, until, "当前访问人数过多，请稍后再试")
	st.MarkBrowserPlatformLimited(b.ID, "当前访问人数过多，请稍后再试", until, now)

	rows := st.Browsers()
	if len(rows) != 1 || !rows[0].QuotaSynced() || !rows[0].PlatformLimited || rows[0].UsagePercent != 100 || rows[0].LimitTips != "当前访问人数过多，请稍后再试" || rows[0].Health != HealthCooling {
		t.Fatalf("after MarkBrowserPlatformLimited got %+v", rows)
	}

	// 同步到恢复正常的免费版豆包状态后，应持久化全部字段并自动解除限流冷却
	q := AccountQuota{
		CreditsRemain:   -1,
		CreditsTotal:    0,
		MemberTier:      "free",
		MemberName:      "免费体验",
		UsagePercent:    18,
		PlatformLimited: false,
		LimitTips:       "",
		ResetCardCount:  2,
		SyncedAt:        now.Add(2 * time.Minute).Unix(),
	}
	if err := st.UpdateBrowserQuota(b.ID, q); err != nil {
		t.Fatalf("UpdateBrowserQuota doubao: %v", err)
	}

	rows = st.Browsers()
	if len(rows) != 1 {
		t.Fatalf("expected 1 browser, got %d", len(rows))
	}
	got := rows[0]
	if !got.QuotaSynced() || got.CreditsRemain != -1 || got.UsagePercent != 18 || got.PlatformLimited || got.LimitTips != "" || got.ResetCardCount != 2 || got.MemberName != "免费体验" {
		t.Fatalf("doubao quota mismatch: %+v", got)
	}
	if got.Health != HealthReady || got.CoolUntil != 0 || got.LastError != "" {
		t.Fatalf("expected limited doubao account to auto-recover when healthy, got health=%q cool=%d err=%q", got.Health, got.CoolUntil, got.LastError)
	}

	// 账号被标记为服务端生图能力降级后，豆包公开额度接口仍返回 0% / 未限额，绝不能被健康额度同步误清空。
	degradedUntil := now.Add(6 * time.Hour).Unix()
	const degradedMsg = "账号生图能力受限（已降级纯文本）（flow.agent.shell）"
	st.UpdateBrowserHealth(b.ID, HealthExhausted, degradedUntil, degradedMsg)
	if err := st.UpdateBrowserQuota(b.ID, AccountQuota{
		CreditsRemain:   -1,
		MemberTier:      "free",
		MemberName:      "免费体验",
		UsagePercent:    0,
		PlatformLimited: false,
		SyncedAt:        now.Add(5 * time.Minute).Unix(),
	}); err != nil {
		t.Fatalf("UpdateBrowserQuota on degraded doubao: %v", err)
	}
	got = st.Browsers()[0]
	if got.Health != HealthExhausted || got.CoolUntil != degradedUntil || got.LastError != degradedMsg {
		t.Fatalf("healthy quota sync must not clear degraded health: got health=%q cool=%d err=%q", got.Health, got.CoolUntil, got.LastError)
	}
}

func TestSyncedZeroCreditsMarksAccountUnusableAndExhausted(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	b, err := st.AddBrowser("千问", "千问耗尽号", filepath.Join(t.TempDir(), "q_zero"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	resetAt := now.Add(5 * 24 * time.Hour).Unix()

	if err := st.UpdateBrowserQuota(b.ID, AccountQuota{
		CreditsRemain:  0,
		CreditsTotal:   40,
		CreditCost:     1,
		CreditsResetAt: resetAt,
		CreditsCycle:   "7d",
		MemberTier:     "free",
		MemberName:     "免费套餐",
		SyncedAt:       now.Unix(),
	}); err != nil {
		t.Fatalf("UpdateBrowserQuota: %v", err)
	}

	rows := st.Browsers()
	if len(rows) != 1 {
		t.Fatalf("expected 1 browser, got %d", len(rows))
	}
	got := rows[0]
	if got.Health != HealthExhausted || got.CoolUntil != resetAt {
		t.Fatalf("expected HealthExhausted with coolUntil=%d, got health=%q cool=%d", resetAt, got.Health, got.CoolUntil)
	}
	if !got.QuotaExhausted(now) || got.Usable(now) {
		t.Fatalf("expected QuotaExhausted=true and Usable=false at %v", now)
	}
	afterReset := time.Unix(resetAt, 0)
	if got.QuotaExhausted(afterReset) || !got.Usable(afterReset) {
		t.Fatalf("expected quota to recover after resetAt %v", afterReset)
	}
}

func TestSyncedCreditsBelowSingleImageCostMarksExhausted(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	b, err := st.AddBrowser("即梦", "即梦不足1张", filepath.Join(t.TempDir(), "jm_low"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	resetAt := now.Add(10 * time.Hour).Unix()

	if err := st.UpdateBrowserQuota(b.ID, AccountQuota{
		CreditsRemain:  1,
		CreditsTotal:   60,
		CreditCost:     2,
		CreditsResetAt: resetAt,
		CreditsCycle:   "daily",
		MemberTier:     "free",
		MemberName:     "免费版",
		SyncedAt:       now.Unix(),
	}); err != nil {
		t.Fatalf("UpdateBrowserQuota: %v", err)
	}
	got := st.Browsers()[0]
	if got.Health != HealthExhausted || !got.QuotaExhausted(now) || got.Usable(now) {
		t.Fatalf("expected remain=1 with cost=2 to be exhausted/unusable, got %+v", got)
	}

	// 积分恢复到 >= cost（如 5 分，2 分/张）后自动恢复可用。
	if err := st.UpdateBrowserQuota(b.ID, AccountQuota{
		CreditsRemain:  5,
		CreditsTotal:   60,
		CreditCost:     2,
		CreditsResetAt: resetAt,
		CreditsCycle:   "daily",
		MemberTier:     "free",
		MemberName:     "免费版",
		SyncedAt:       now.Add(time.Minute).Unix(),
	}); err != nil {
		t.Fatalf("UpdateBrowserQuota recover: %v", err)
	}
	recovered := st.Browsers()[0]
	if recovered.Health != HealthReady || recovered.QuotaExhausted(now) || !recovered.Usable(now) {
		t.Fatalf("expected remain=5 with cost=2 to recover ready/usable, got %+v", recovered)
	}
}
