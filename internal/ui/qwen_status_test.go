package ui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestBuildAccountQuotaViewFormatsQwenAndDoubao(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 27, 12, 15, 0, 0, loc)

	// 未同步的千问账号
	unsynced := buildAccountQuotaView(store.Browser{
		Platform: "千问",
		DayKey:   now.Format("2006-01-02"),
		DayCount: 2,
	}, 100, now)
	if unsynced.CreditsSynced || unsynced.CreditsRemain != -1 || !strings.Contains(unsynced.UsageLabel, "待同步积分") {
		t.Fatalf("unsynced qwen view unexpected: %+v", unsynced)
	}

	// 已同步的每日刷新免费千问账号
	dailyReset := time.Date(2026, 11, 27, 23, 59, 59, 0, loc).Unix()
	daily := buildAccountQuotaView(store.Browser{
		Platform:           "千问",
		CreditsRemain:      31,
		CreditsTotal:       40,
		CreditCost:         1,
		CreditsResetAt:     dailyReset,
		CreditsCycle:       "daily",
		MemberTier:         "free",
		MemberName:         "免费套餐",
		AccountConcurrency: cdp.QwenFreeConcurrencyCap,
		CreditsSyncedAt:    now.Unix(),
	}, 100, now)
	if !daily.CreditsSynced || daily.CreditsRemain != 31 || daily.CreditsTotal != 40 || daily.UsagePct != 23 {
		t.Fatalf("daily qwen view unexpected: %+v", daily)
	}
	if daily.UsageLabel != "余 31/40 积分" || daily.CreditsResetLabel != "今日 23:59 刷新" || daily.CreditsCycleLabel != "每日刷新" {
		t.Fatalf("daily labels unexpected: %+v", daily)
	}
	if daily.ConcurrencyLabel != "单号≤6(建议2)" || daily.CreditsSyncedLabel != "刚刚同步" {
		t.Fatalf("daily concurrency/synced unexpected: %+v", daily)
	}

	// 已用尽的 7 日周期千问账号
	sevenDayReset := time.Date(2026, 12, 3, 3, 51, 0, 0, loc).Unix()
	exhausted := buildAccountQuotaView(store.Browser{
		Platform:           "千问",
		CreditsRemain:      0,
		CreditsTotal:       40,
		CreditCost:         1,
		CreditsResetAt:     sevenDayReset,
		CreditsCycle:       "7d",
		MemberTier:         "free",
		MemberName:         "免费套餐",
		AccountConcurrency: cdp.QwenFreeConcurrencyCap,
		CreditsSyncedAt:    now.Add(-20 * time.Minute).Unix(),
	}, 100, now)
	if exhausted.UsagePct != 100 || exhausted.UsageLabel != "0/40 积分 · 已耗尽" || exhausted.CreditsResetLabel != "12-03 03:51 到期" || exhausted.CreditsCycleLabel != "7日周期" {
		t.Fatalf("exhausted 7d view unexpected: %+v", exhausted)
	}

	// VIP 会员千问账号
	vip := buildAccountQuotaView(store.Browser{
		Platform:           "千问",
		CreditsRemain:      760,
		CreditsTotal:       800,
		CreditCost:         1,
		CreditsCycle:       "monthly",
		MemberTier:         "vip",
		MemberName:         "标准会员",
		AccountConcurrency: cdp.QwenVIPConcurrencyUnlimited,
		WatermarkFree:      true,
		CreditsSyncedAt:    now.Unix(),
	}, 100, now)
	if vip.ConcurrencyLabel != "不限并发" || !vip.WatermarkFree || vip.MemberLabel != "标准会员" || vip.CreditsCycleLabel != "会员月度" {
		t.Fatalf("vip view unexpected: %+v", vip)
	}

	// 豆包账号保持原有简洁计数
	doubao := buildAccountQuotaView(store.Browser{
		Platform: "豆包",
		DayKey:   now.Format("2006-01-02"),
		DayCount: 3,
	}, 50, now)
	if doubao.CreditsSynced || doubao.CreditsRemain != -1 || doubao.UsageLabel != "3/50" || doubao.UsagePct != 6 {
		t.Fatalf("doubao view unexpected: %+v", doubao)
	}

	// 未同步的 Gemini 账号
	gemUnsynced := buildAccountQuotaView(store.Browser{
		Platform: "Gemini",
		DayKey:   now.Format("2006-01-02"),
		DayCount: 2,
	}, 100, now)
	if gemUnsynced.CreditsSynced || gemUnsynced.CreditsRemain != -1 || !strings.Contains(gemUnsynced.UsageLabel, "待同步额度") {
		t.Fatalf("unsynced gemini view unexpected: %+v", gemUnsynced)
	}

	// 已同步的 5 小时滚动窗口 Gemini AI Pro 账号
	fiveReset := time.Date(2026, 11, 27, 17, 30, 0, 0, loc).Unix()
	gemPro := buildAccountQuotaView(store.Browser{
		Platform:        "Gemini",
		CreditsRemain:   85,
		CreditsTotal:    100,
		CreditsResetAt:  fiveReset,
		CreditsCycle:    cdp.GeminiQuotaCycleFiveHour,
		MemberTier:      "pro",
		MemberName:      "AI Pro",
		CreditsSyncedAt: now.Unix(),
	}, 100, now)
	if !gemPro.CreditsSynced || gemPro.CreditsRemain != 85 || gemPro.UsagePct != 15 || gemPro.UsageLabel != "余 85%" {
		t.Fatalf("gemini pro view unexpected: %+v", gemPro)
	}
	if gemPro.CreditsCycleLabel != "5小时窗口" || gemPro.CreditsResetLabel != "今日 17:30 刷新" || gemPro.MemberLabel != "AI Pro" {
		t.Fatalf("gemini pro labels unexpected: %+v", gemPro)
	}

	// 已耗尽的每周上限 Gemini 账号
	gemExhausted := buildAccountQuotaView(store.Browser{
		Platform:        "Gemini",
		CreditsRemain:   0,
		CreditsTotal:    100,
		CreditsResetAt:  sevenDayReset,
		CreditsCycle:    cdp.GeminiQuotaCycleWeekly,
		MemberTier:      "ultra",
		MemberName:      "AI Ultra",
		CreditsSyncedAt: now.Add(-10 * time.Minute).Unix(),
	}, 100, now)
	if gemExhausted.UsagePct != 100 || gemExhausted.UsageLabel != "剩余 0% · 已耗尽" || gemExhausted.CreditsCycleLabel != "每周上限" || gemExhausted.CreditsResetLabel != "12-03 03:51 刷新" {
		t.Fatalf("gemini exhausted view unexpected: %+v", gemExhausted)
	}

	// 已到期且未在到期后重新同步的 Gemini 5 小时窗口应提示「已到期 · 待同步刷新」
	staleReset := now.Add(-1 * time.Hour).Unix()
	gemStale := buildAccountQuotaView(store.Browser{
		Platform:        "Gemini",
		CreditsRemain:   99,
		CreditsTotal:    100,
		CreditsResetAt:  staleReset,
		CreditsCycle:    cdp.GeminiQuotaCycleFiveHour,
		MemberTier:      "ultra",
		MemberName:      "AI Ultra",
		CreditsSyncedAt: now.Add(-2 * time.Hour).Unix(),
	}, 100, now)
	if gemStale.CreditsResetLabel != "已到期 · 待同步刷新" {
		t.Fatalf("expected stale reset label, got %+v", gemStale)
	}

	// 若同步时间已晚于服务端返回的旧刷新时间，不再显示「已到期 · 待同步刷新」
	gemFreshPast := buildAccountQuotaView(store.Browser{
		Platform:        "Gemini",
		CreditsRemain:   99,
		CreditsTotal:    100,
		CreditsResetAt:  staleReset,
		CreditsCycle:    cdp.GeminiQuotaCycleFiveHour,
		MemberTier:      "ultra",
		MemberName:      "AI Ultra",
		CreditsSyncedAt: now.Unix(),
	}, 100, now)
	if gemFreshPast.CreditsResetAt != 0 || gemFreshPast.CreditsResetLabel != "5小时滚动刷新" {
		t.Fatalf("expected freshly synced past reset fallback to rolling label, got %+v", gemFreshPast)
	}

	// 未同步的即梦账号
	jmUnsynced := buildAccountQuotaView(store.Browser{
		Platform: "即梦",
		DayKey:   now.Format("2006-01-02"),
		DayCount: 2,
	}, 100, now)
	if jmUnsynced.CreditsSynced || jmUnsynced.CreditsRemain != -1 || !strings.Contains(jmUnsynced.UsageLabel, "待同步积分") {
		t.Fatalf("unsynced jimeng view unexpected: %+v", jmUnsynced)
	}

	// 已同步的即梦账号（94 积分）
	jmSynced := buildAccountQuotaView(store.Browser{
		Platform:        "即梦",
		CreditsRemain:   94,
		CreditsTotal:    100,
		CreditCost:      1,
		CreditsResetAt:  dailyReset,
		CreditsCycle:    "daily",
		MemberTier:      "free",
		MemberName:      "免费版",
		CreditsSyncedAt: now.Unix(),
	}, 100, now)
	if !jmSynced.CreditsSynced || jmSynced.CreditsRemain != 94 || jmSynced.CreditsTotal != 100 || jmSynced.UsagePct != 6 || jmSynced.UsageLabel != "余 94/100 积分" {
		t.Fatalf("synced jimeng view unexpected: %+v", jmSynced)
	}
	if jmSynced.MemberLabel != "免费版" || jmSynced.CreditsCycleLabel != "每日刷新" {
		t.Fatalf("synced jimeng labels unexpected: %+v", jmSynced)
	}
}

func TestSyncGeminiStatusFailurePushesErrorToast(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	g1, err := st.AddBrowser("Gemini", "Kodram Dritken", filepath.Join(root, "g1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(g1.ID)

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	coreSvc := &CoreService{app: app}
	browserSvc := &BrowserService{app: app}
	ctx := context.Background()

	_ = app.call(ctx, func() {
		app.syncGeminiStatusFn = func(_ context.Context, _ store.Browser) (cdp.GeminiStatus, error) {
			return cdp.GeminiStatus{}, context.DeadlineExceeded
		}
	})

	if err := browserSvc.SyncGeminiStatus(ctx, []int64{g1.ID}); err != nil {
		t.Fatalf("SyncGeminiStatus failed to start: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bs := browserSvc.GetBrowserState(ctx)
		if !bs.BatchActive {
			sh := coreSvc.GetShellState(ctx)
			if sh.ToastLevel != "error" || !strings.Contains(sh.ToastText, "Gemini状态同步失败") {
				t.Fatalf("expected error toast on sync failure, got level=%q text=%q", sh.ToastLevel, sh.ToastText)
			}
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("SyncGeminiStatus failure batch did not finish in time")
}

func TestGeminiStatusHasIdentityOrMeter(t *testing.T) {
	if geminiStatusHasIdentityOrMeter(cdp.GeminiStatus{OK: false, HasFiveHour: true}) {
		t.Fatal("non-ok status must not qualify")
	}
	if geminiStatusHasIdentityOrMeter(cdp.GeminiStatus{OK: true, SubscriptionTier: "FREE"}) {
		t.Fatal("tier-only status without meter or identity must not bypass unknown login state")
	}
	if !geminiStatusHasIdentityOrMeter(cdp.GeminiStatus{OK: true, HasFiveHour: true}) {
		t.Fatal("metered status should qualify")
	}
	if !geminiStatusHasIdentityOrMeter(cdp.GeminiStatus{OK: true, Email: "alice@gmail.com"}) {
		t.Fatal("identified status should qualify")
	}
}

func TestSyncGeminiStatusUpdatesUsernameAndQuotaDTOs(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	g1, err := st.AddBrowser("Gemini", "Gemini1", filepath.Join(root, "g1"))
	if err != nil {
		t.Fatal(err)
	}
	// 故意保持未确认登录状态，验证同步成功后自动确认登录并使左侧栏就绪计数变为 1。
	now := time.Now()
	resetAt := now.Add(4 * time.Hour)
	st.UpdateBrowserHealth(g1.ID, store.HealthExhausted, resetAt.Unix(), "reached your image generation limit")

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	coreSvc := &CoreService{app: app}
	browserSvc := &BrowserService{app: app}
	wbSvc := &WorkbenchService{app: app}
	settingsSvc := &SettingsService{app: app}
	ctx := context.Background()

	settingsSvc.SetSettingField(ctx, "concurrencyAutoGemini", "true")
	settingsSvc.SetSettingField(ctx, "concurrencyMaxGemini", "5")

	_ = app.call(ctx, func() {
		app.syncGeminiStatusFn = func(_ context.Context, b store.Browser) (cdp.GeminiStatus, error) {
			return cdp.GeminiStatus{
				OK:               true,
				Source:           "graphql",
				Username:         "Alice Wang",
				Email:            "alice@gmail.com",
				SubscriptionTier: "TIER1",
				HasFiveHour:      true,
				FiveHourPctUsed:  15,
				FiveHourResetAt:  resetAt.UnixMilli(),
			}, nil
		}
	})

	if err := browserSvc.SyncGeminiStatus(ctx, nil); err != nil {
		t.Fatalf("SyncGeminiStatus failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bs := browserSvc.GetBrowserState(ctx)
		if !bs.BatchActive {
			if !bs.GeminiConcurrencyAuto || bs.GeminiConcurrencyMax != 5 {
				t.Fatalf("expected gemini concurrency (true, 5), got (%v, %d)", bs.GeminiConcurrencyAuto, bs.GeminiConcurrencyMax)
			}
			var gRow *BrowserRowDTO
			for i := range bs.Rows {
				if bs.Rows[i].ID == g1.ID {
					gRow = &bs.Rows[i]
				}
			}
			if gRow == nil || gRow.Name != "Alice Wang" || !gRow.IsLoggedIn {
				t.Fatalf("expected default Gemini name synced to Alice Wang and confirmed logged in, got %+v", gRow)
			}
			shell := coreSvc.GetShellState(ctx)
			geminiReady := -1
			for _, p := range shell.Platforms {
				if p.Name == "Gemini" {
					geminiReady = p.Ready
				}
			}
			if geminiReady != 1 {
				t.Fatalf("expected sidebar Gemini ready count = 1 after sync, got %d (platforms=%+v)", geminiReady, shell.Platforms)
			}
			if !gRow.CreditsSynced || gRow.CreditsRemain != 85 || gRow.CreditsTotal != 100 || gRow.CreditsCycle != "5h" || gRow.MemberLabel != "AI Pro" || gRow.UsageLabel != "余 85%" {
				t.Fatalf("gemini browser row quota mismatch: %+v", gRow)
			}
			if (gRow.Health != "就绪" && gRow.Health != "运行中") || gRow.HealthTone != "success" || gRow.LastError != "" {
				t.Fatalf("expected exhausted gemini account recovered on fresh quota, got health=%q tone=%q err=%q", gRow.Health, gRow.HealthTone, gRow.LastError)
			}

			wbSvc.SetPlatformSelected(ctx, "Gemini", true)
			wb := wbSvc.GetWorkbenchState(ctx)
			var gLane *AccountLaneDTO
			for i := range wb.Lanes {
				if wb.Lanes[i].ID == g1.ID {
					gLane = &wb.Lanes[i]
				}
			}
			if gLane == nil || gLane.Title != "Alice Wang" || !gLane.CreditsSynced || gLane.CreditsRemain != 85 || gLane.UsageLabel != "余 85%" {
				t.Fatalf("gemini workbench lane mismatch: %+v", gLane)
			}
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("SyncGeminiStatus batch did not finish in time")
}

func TestSyncQwenStatusUpdatesBrowserAndWorkbenchDTOs(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	d1, err := st.AddBrowser("豆包", "豆包1", filepath.Join(root, "d1"))
	if err != nil {
		t.Fatal(err)
	}
	q1, err := st.AddBrowser("千问", "千问1", filepath.Join(root, "q1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(d1.ID)
	st.ConfirmLogin(q1.ID)

	now := time.Now().Truncate(time.Second)
	resetAt := now.Add(6 * time.Hour)
	st.UpdateBrowserHealth(q1.ID, store.HealthExhausted, resetAt.Unix(), "积分不足，请购买积分或开通创作会员")

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	browserSvc := &BrowserService{app: app}
	wbSvc := &WorkbenchService{app: app}
	settingsSvc := &SettingsService{app: app}
	ctx := context.Background()

	settingsSvc.SetSettingField(ctx, "concurrencyAutoQianwen", "true")
	settingsSvc.SetSettingField(ctx, "concurrencyMaxQianwen", "4")

	// 任务运行中不允许手动同步
	_ = app.call(ctx, func() { app.running = true })
	if err := browserSvc.SyncQwenStatus(ctx, []int64{q1.ID}); err == nil {
		t.Fatal("expected error while tasks are running")
	}
	_ = app.call(ctx, func() { app.running = false })

	_ = app.call(ctx, func() {
		app.syncQwenStatusFn = func(_ context.Context, b store.Browser) (cdp.QwenStatus, error) {
			return cdp.QwenStatus{
				OK:              true,
				HasCredits:      true,
				CreditsRemain:   31,
				CreditCost:      1,
				GrantAmount:     40,
				GrantReceiveAt:  now.Add(-2 * time.Hour).UnixMilli(),
				GrantExpiredAt:  resetAt.UnixMilli(),
				GrantExpiredStr: resetAt.Format("2006-01-02 15:04:05"),
				MemberType:      "free",
				MemberName:      "免费套餐",
			}, nil
		}
	})

	if err := browserSvc.SyncQwenStatus(ctx, nil); err != nil {
		t.Fatalf("SyncQwenStatus failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bs := browserSvc.GetBrowserState(ctx)
		if !bs.BatchActive {
			if !bs.QianwenConcurrencyAuto || bs.QianwenConcurrencyMax != 4 {
				t.Fatalf("expected qianwen concurrency (true, 4), got (%v, %d)", bs.QianwenConcurrencyAuto, bs.QianwenConcurrencyMax)
			}
			var qRow *BrowserRowDTO
			for i := range bs.Rows {
				if bs.Rows[i].ID == q1.ID {
					qRow = &bs.Rows[i]
				}
			}
			if qRow == nil || !qRow.CreditsSynced || qRow.CreditsRemain != 31 || qRow.CreditsTotal != 40 || qRow.MemberLabel != "免费套餐" || qRow.ConcurrencyLimit != 6 {
				t.Fatalf("browser row quota mismatch: %+v", qRow)
			}
			if (qRow.Health != "就绪" && qRow.Health != "运行中") || qRow.HealthTone != "success" || qRow.LastError != "" {
				t.Fatalf("expected exhausted qwen account recovered on fresh credits, got health=%q tone=%q err=%q", qRow.Health, qRow.HealthTone, qRow.LastError)
			}

			wbSvc.SetPlatformSelected(ctx, "千问", true)
			wb := wbSvc.GetWorkbenchState(ctx)
			var qLane *AccountLaneDTO
			for i := range wb.Lanes {
				if wb.Lanes[i].ID == q1.ID {
					qLane = &wb.Lanes[i]
				}
			}
			if qLane == nil || !qLane.CreditsSynced || qLane.CreditsRemain != 31 || qLane.CreditsTotal != 40 || qLane.ConcurrencyLabel != "单号≤6(建议2)" {
				t.Fatalf("workbench lane quota mismatch: %+v", qLane)
			}
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("SyncQwenStatus batch did not finish in time")
}

func TestBuildAccountQuotaViewFormatsDoubaoAndGemini(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 27, 13, 30, 0, 0, loc)

	// 已同步免费版豆包（正常，窗口 0%）
	freeOk := buildAccountQuotaView(store.Browser{
		Platform:        "豆包",
		DayKey:          now.Format("2006-01-02"),
		DayCount:        3,
		CreditsRemain:   -1,
		MemberTier:      "free",
		MemberName:      "免费体验",
		UsagePercent:    0,
		PlatformLimited: false,
		ResetCardCount:  1,
		CreditsSyncedAt: now.Unix(),
	}, 50, now)
	if !freeOk.CreditsSynced || freeOk.CreditsRemain != -1 || freeOk.UsagePercent != 0 || freeOk.ResetCardCount != 1 || freeOk.MemberLabel != "免费体验" || freeOk.UsageLabel != "额度正常 · 今日 3/50" {
		t.Fatalf("doubao free healthy view unexpected: %+v", freeOk)
	}

	// 已同步免费版豆包（窗口 45%）
	freeWindow := buildAccountQuotaView(store.Browser{
		Platform:        "豆包",
		CreditsRemain:   -1,
		MemberTier:      "free",
		MemberName:      "免费体验",
		UsagePercent:    45,
		CreditsSyncedAt: now.Unix(),
	}, 0, now)
	if freeWindow.UsagePct != 45 || freeWindow.UsageLabel != "窗口已用 45%" {
		t.Fatalf("doubao window view unexpected: %+v", freeWindow)
	}

	// 已同步免费版豆包（已触发限额）
	resumeAt := now.Add(30 * time.Minute).Unix()
	freeLimited := buildAccountQuotaView(store.Browser{
		Platform:        "豆包",
		CreditsRemain:   -1,
		CreditsResetAt:  resumeAt,
		MemberTier:      "free",
		MemberName:      "免费体验",
		UsagePercent:    100,
		PlatformLimited: true,
		LimitTips:       "今日额度已达上限",
		CreditsSyncedAt: now.Unix(),
	}, 0, now)
	if !freeLimited.PlatformLimited || freeLimited.UsagePct != 100 || freeLimited.UsageLabel != "已触发限额" || freeLimited.CreditsResetLabel != "预计 14:00 恢复" {
		t.Fatalf("doubao limited view unexpected: %+v", freeLimited)
	}

	// 已同步付费订阅豆包
	paidReset := time.Date(2026, 12, 27, 23, 59, 0, 0, loc).Unix()
	paid := buildAccountQuotaView(store.Browser{
		Platform:        "豆包",
		CreditsRemain:   8420,
		CreditsTotal:    9000,
		CreditsResetAt:  paidReset,
		CreditsCycle:    "monthly",
		MemberTier:      "vip",
		MemberName:      "标准订阅",
		UsagePercent:    6,
		CreditsSyncedAt: now.Unix(),
	}, 0, now)
	if paid.MemberLabel != "标准订阅" || paid.UsageLabel != "余 8420/9000 额度" || paid.UsagePct != 6 || paid.CreditsResetLabel != "12-27 23:59 到期" {
		t.Fatalf("doubao paid view unexpected: %+v", paid)
	}

	// 已同步 Gemini 5 小时窗口
	geminiReset := time.Date(2026, 11, 27, 17, 30, 0, 0, loc).Unix()
	gem := buildAccountQuotaView(store.Browser{
		Platform:        "Gemini",
		CreditsRemain:   85,
		CreditsTotal:    100,
		CreditsResetAt:  geminiReset,
		CreditsCycle:    cdp.GeminiQuotaCycleFiveHour,
		MemberTier:      "pro",
		MemberName:      "AI Pro",
		CreditsSyncedAt: now.Unix(),
	}, 100, now)
	if !gem.CreditsSynced || gem.UsagePct != 15 || gem.UsageLabel != "余 85%" || gem.CreditsCycleLabel != "5小时窗口" || gem.CreditsResetLabel != "今日 17:30 刷新" || gem.MemberLabel != "AI Pro" {
		t.Fatalf("gemini view unexpected: %+v", gem)
	}
}

func TestSyncAccountStatusUpdatesDoubaoBrowserAndWorkbenchDTOs(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	d1, err := st.AddBrowser("豆包", "豆包1", filepath.Join(root, "d1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(d1.ID)

	now := time.Now()
	until := now.Add(15 * time.Minute).Unix()
	st.UpdateBrowserHealth(d1.ID, store.HealthCooling, until, "请求过于频繁")
	st.MarkBrowserPlatformLimited(d1.ID, "请求过于频繁", until, now)

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	browserSvc := &BrowserService{app: app}
	wbSvc := &WorkbenchService{app: app}
	settingsSvc := &SettingsService{app: app}
	ctx := context.Background()

	settingsSvc.SetSettingField(ctx, "concurrencyAutoDoubao", "true")
	settingsSvc.SetSettingField(ctx, "concurrencyMaxDoubao", "5")

	_ = app.call(ctx, func() {
		app.syncDoubaoStatusFn = func(_ context.Context, b store.Browser) (cdp.DoubaoStatus, error) {
			return cdp.DoubaoStatus{
				OK:                    true,
				HasActiveSubscription: false,
				ShortName:             "免费体验",
				HasUsagePercent:       true,
				IsFreeUser:            true,
				IsLimited:             false,
				UsedPercent:           12,
				ResetCardCount:        2,
			}, nil
		}
	})

	if err := browserSvc.SyncAccountStatus(ctx, []int64{d1.ID}); err != nil {
		t.Fatalf("SyncAccountStatus failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bs := browserSvc.GetBrowserState(ctx)
		if !bs.BatchActive {
			if !bs.DoubaoConcurrencyAuto || bs.DoubaoConcurrencyMax != 5 {
				t.Fatalf("expected doubao concurrency (true, 5), got (%v, %d)", bs.DoubaoConcurrencyAuto, bs.DoubaoConcurrencyMax)
			}
			var dRow *BrowserRowDTO
			for i := range bs.Rows {
				if bs.Rows[i].ID == d1.ID {
					dRow = &bs.Rows[i]
				}
			}
			if dRow == nil || !dRow.CreditsSynced || dRow.CreditsRemain != -1 || dRow.UsagePercent != 12 || dRow.PlatformLimited || dRow.ResetCardCount != 2 || dRow.MemberLabel != "免费体验" {
				t.Fatalf("doubao browser row mismatch: %+v", dRow)
			}
			if (dRow.Health != "就绪" && dRow.Health != "运行中") || dRow.HealthTone != "success" || dRow.LastError != "" {
				t.Fatalf("expected cooling doubao account auto-recovered on healthy sync, got health=%q tone=%q err=%q", dRow.Health, dRow.HealthTone, dRow.LastError)
			}

			wbSvc.SetPlatformSelected(ctx, "豆包", true)
			wb := wbSvc.GetWorkbenchState(ctx)
			var dLane *AccountLaneDTO
			for i := range wb.Lanes {
				if wb.Lanes[i].ID == d1.ID {
					dLane = &wb.Lanes[i]
				}
			}
			if dLane == nil || !dLane.CreditsSynced || dLane.UsagePercent != 12 || dLane.PlatformLimited || dLane.ResetCardCount != 2 || dLane.MemberLabel != "免费体验" {
				t.Fatalf("doubao workbench lane mismatch: %+v", dLane)
			}
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("SyncAccountStatus batch did not finish in time")
}

func TestExhaustedQuotaReflectsInBrowserHealthSidebarReadyAndWorkbenchLane(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	q1, err := st.AddBrowser("千问", "千问正常", filepath.Join(root, "q1"))
	if err != nil {
		t.Fatal(err)
	}
	q2, err := st.AddBrowser("千问", "千问耗尽", filepath.Join(root, "q2"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(q1.ID)
	st.ConfirmLogin(q2.ID)
	st.UpdateBrowser(q1.ID, false, true)
	st.UpdateBrowser(q2.ID, false, true)

	now := time.Now()
	resetAt := now.Add(48 * time.Hour).Unix()
	if err := st.UpdateBrowserQuota(q1.ID, store.AccountQuota{
		CreditsRemain:  31,
		CreditsTotal:   40,
		CreditCost:     1,
		CreditsResetAt: resetAt,
		CreditsCycle:   "daily",
		MemberTier:     "free",
		MemberName:     "免费套餐",
		SyncedAt:       now.Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateBrowserQuota(q2.ID, store.AccountQuota{
		CreditsRemain:  0,
		CreditsTotal:   40,
		CreditCost:     1,
		CreditsResetAt: resetAt,
		CreditsCycle:   "7d",
		MemberTier:     "free",
		MemberName:     "免费套餐",
		SyncedAt:       now.Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	coreSvc := &CoreService{app: app}
	browserSvc := &BrowserService{app: app}
	wbSvc := &WorkbenchService{app: app}
	ctx := context.Background()

	bs := browserSvc.GetBrowserState(ctx)
	var row1, row2 *BrowserRowDTO
	for i := range bs.Rows {
		if bs.Rows[i].ID == q1.ID {
			row1 = &bs.Rows[i]
		} else if bs.Rows[i].ID == q2.ID {
			row2 = &bs.Rows[i]
		}
	}
	if row1 == nil || row1.Health != "就绪" || row1.HealthTone != "success" {
		t.Fatalf("expected q1 ready, got %+v", row1)
	}
	if row2 == nil || row2.Health != "已用尽" || row2.HealthTone != "danger" {
		t.Fatalf("expected q2 exhausted/danger, got %+v", row2)
	}

	shell := coreSvc.GetShellState(ctx)
	for _, p := range shell.Platforms {
		if p.Name == "千问" && p.Ready != 1 {
			t.Fatalf("expected sidebar 千问 ready=1 (excluding exhausted q2), got %d", p.Ready)
		}
	}

	wbSvc.SetPlatformSelected(ctx, "千问", true)
	wb := wbSvc.GetWorkbenchState(ctx)
	for _, ln := range wb.Lanes {
		if ln.ID == q2.ID && ln.State != "额度不足" {
			t.Fatalf("expected exhausted q2 workbench lane state=额度不足, got %+v", ln)
		}
	}
}

func TestSyncJimengStatusUpdatesCreditsAndAutoRenames(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	j1, err := st.AddBrowser("即梦", "即梦1", filepath.Join(root, "j1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(j1.ID)

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	browserSvc := &BrowserService{app: app}
	wbSvc := &WorkbenchService{app: app}
	settingsSvc := &SettingsService{app: app}
	ctx := context.Background()

	settingsSvc.SetSettingField(ctx, "concurrencyAutoJimeng", "true")
	settingsSvc.SetSettingField(ctx, "concurrencyMaxJimeng", "4")
	settingsSvc.SetSettingField(ctx, "dailyCapJimeng", "120")

	_ = app.call(ctx, func() {
		app.syncJimengStatusFn = func(_ context.Context, _ store.Browser) (cdp.JimengStatus, error) {
			return cdp.JimengStatus{
				OK:              true,
				Source:          "user_credit",
				Username:        "即梦设计师",
				HasCredits:      true,
				CreditsRemain:   94,
				GiftCredits:     60,
				PurchaseCredits: 34,
				CreditCost:      1,
				MemberType:      "free",
				MemberName:      "免费版",
			}, nil
		}
	})

	if err := browserSvc.SyncJimengStatus(ctx, []int64{j1.ID}); err != nil {
		t.Fatalf("SyncJimengStatus failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bs := browserSvc.GetBrowserState(ctx)
		if !bs.BatchActive {
			if !bs.JimengConcurrencyAuto || bs.JimengConcurrencyMax != 4 {
				t.Fatalf("expected jimeng concurrency (true, 4), got (%v, %d)", bs.JimengConcurrencyAuto, bs.JimengConcurrencyMax)
			}
			var jRow *BrowserRowDTO
			for i := range bs.Rows {
				if bs.Rows[i].ID == j1.ID {
					jRow = &bs.Rows[i]
				}
			}
			if jRow == nil || jRow.Name != "即梦设计师" || !jRow.CreditsSynced || jRow.CreditsRemain != 94 || jRow.CreditCost != 1 {
				t.Fatalf("jimeng browser row mismatch: %+v", jRow)
			}
			ss := settingsSvc.GetSettingsState(ctx)
			if ss.DailyCapJimeng != 120 || !ss.ConcurrencyAutoJimeng || ss.ConcurrencyMaxJimeng != 4 {
				t.Fatalf("jimeng settings mismatch: %+v", ss)
			}

			wbSvc.SetPlatformSelected(ctx, "即梦", true)
			wb := wbSvc.GetWorkbenchState(ctx)
			if wb.Platform != "即梦" || !wb.Jimeng {
				t.Fatalf("jimeng workbench mismatch: %+v", wb)
			}
			var jLane *AccountLaneDTO
			for i := range wb.Lanes {
				if wb.Lanes[i].ID == j1.ID {
					jLane = &wb.Lanes[i]
				}
			}
			if jLane == nil || jLane.Title != "即梦设计师" || !jLane.CreditsSynced || jLane.CreditsRemain != 94 {
				t.Fatalf("jimeng workbench lane mismatch: %+v", jLane)
			}
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("SyncJimengStatus batch did not finish in time")
}

func TestJimengTargetConfigMatchesStrategyAndOverwritesStaleCost(t *testing.T) {
	if cdp.JimengTargetMode != strategy.JimengDefaultMode ||
		cdp.JimengTargetModel != strategy.JimengDefaultModel ||
		!strings.EqualFold(cdp.JimengTargetResolution, strategy.JimengDefaultResolution) {
		t.Fatalf("cdp target (%s / %s / %s) diverged from strategy defaults (%s / %s / %s)",
			cdp.JimengTargetMode, cdp.JimengTargetModel, cdp.JimengTargetResolution,
			strategy.JimengDefaultMode, strategy.JimengDefaultModel, strategy.JimengDefaultResolution)
	}

	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	j1, err := st.AddBrowser("即梦", "即梦旧单价", filepath.Join(root, "j1"))
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(j1.ID)
	// 模拟历史错误同步写入的 Seedream 5.5 单价（4分/张）
	if err := st.UpdateBrowserQuota(j1.ID, store.AccountQuota{
		CreditsRemain: 94,
		CreditsTotal:  94,
		CreditCost:    4,
		CreditsCycle:  "daily",
		MemberTier:    "free",
		MemberName:    "免费版",
		SyncedAt:      time.Now().Add(-time.Hour).Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	browserSvc := &BrowserService{app: app}
	ctx := context.Background()

	// 即使同步页仍停留在网页默认 Seedream 5.5（4/张），归一化也必须把单价纠正为 Seedream 4.7 的 1分/张。
	_ = app.call(ctx, func() {
		app.syncJimengStatusFn = func(_ context.Context, _ store.Browser) (cdp.JimengStatus, error) {
			return cdp.JimengStatus{
				OK:            true,
				Source:        "user_credit",
				HasCredits:    true,
				CreditsRemain: 94,
				Model:         "Seedream 5.5",
				Resolution:    "1.5K",
				CreditCost:    4,
			}, nil
		}
	})

	if err := browserSvc.SyncJimengStatus(ctx, []int64{j1.ID}); err != nil {
		t.Fatalf("SyncJimengStatus failed: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		bs := browserSvc.GetBrowserState(ctx)
		if !bs.BatchActive {
			for i := range bs.Rows {
				if bs.Rows[i].ID == j1.ID {
					if bs.Rows[i].CreditCost != 1 {
						t.Fatalf("expected stale creditCost=4 to be corrected to 1, got %d", bs.Rows[i].CreditCost)
					}
					return
				}
			}
			t.Fatal("jimeng row not found")
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("SyncJimengStatus batch did not finish in time")
}
