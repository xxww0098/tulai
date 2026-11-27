package ui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

type accountQuotaView struct {
	UsageLabel         string
	UsagePct           int
	CreditsSynced      bool
	CreditsRemain      int
	CreditsTotal       int
	CreditCost         int
	CreditsResetAt     int64
	CreditsResetLabel  string
	CreditsCycle       string
	CreditsCycleLabel  string
	MemberTier         string
	MemberLabel        string
	ConcurrencyLimit   int
	ConcurrencyLabel   string
	WatermarkFree      bool
	UsagePercent       int
	PlatformLimited    bool
	LimitTips          string
	ResetCardCount     int
	CreditsSyncedAt    int64
	CreditsSyncedLabel string
}

func clampPercent(used, total int) int {
	if total <= 0 || used <= 0 {
		return 0
	}
	pct := int(math.Round(float64(used) * 100.0 / float64(total)))
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func buildAccountQuotaView(b store.Browser, dailyCap int, now time.Time) accountQuotaView {
	usedToday := b.UsedToday(now)
	view := accountQuotaView{
		CreditsRemain: -1,
		UsagePercent:  -1,
	}
	switch b.Platform {
	case "千问":
		return buildQwenQuotaView(b, usedToday, dailyCap, now)
	case "豆包":
		return buildDoubaoQuotaView(b, usedToday, dailyCap, now)
	case "Gemini":
		return buildGeminiQuotaView(b, usedToday, dailyCap, now)
	case "即梦":
		return buildJimengQuotaView(b, usedToday, dailyCap, now)
	default:
		if dailyCap > 0 {
			view.UsageLabel = fmt.Sprintf("%d/%d", usedToday, dailyCap)
			view.UsagePct = clampPercent(usedToday, dailyCap)
		}
		return view
	}
}

func buildDoubaoQuotaView(b store.Browser, usedToday, dailyCap int, now time.Time) accountQuotaView {
	view := accountQuotaView{
		CreditsRemain: -1,
		UsagePercent:  -1,
	}
	if !b.QuotaSynced() {
		if dailyCap > 0 {
			view.UsageLabel = fmt.Sprintf("%d/%d", usedToday, dailyCap)
			view.UsagePct = clampPercent(usedToday, dailyCap)
		}
		return view
	}

	view.CreditsSynced = true
	view.MemberTier = b.MemberTier
	view.MemberLabel = strings.TrimSpace(b.MemberName)
	if view.MemberLabel == "" {
		if b.MemberTier == "vip" {
			view.MemberLabel = "豆包订阅"
		} else {
			view.MemberLabel = "免费体验"
		}
	}
	view.UsagePercent = b.UsagePercent
	view.PlatformLimited = b.PlatformLimited
	view.LimitTips = strings.TrimSpace(b.LimitTips)
	view.ResetCardCount = max(b.ResetCardCount, 0)
	effectiveResetAt := effectiveQuotaResetAt(b.CreditsResetAt, b.CreditsSyncedAt)
	view.CreditsResetAt = effectiveResetAt
	view.CreditsCycle = b.CreditsCycle
	view.CreditsCycleLabel = formatCreditsCycleLabel(b.CreditsCycle)
	view.CreditsSyncedAt = b.CreditsSyncedAt
	view.CreditsSyncedLabel = formatSyncedAtLabel(b.CreditsSyncedAt, now)

	// 付费订阅（有具体剩余额度）
	if b.MemberTier == "vip" || b.CreditsRemain >= 0 {
		view.CreditsRemain = b.CreditsRemain
		view.CreditsTotal = b.CreditsTotal
		view.CreditsResetLabel = formatCreditsResetLabel(effectiveResetAt, b.CreditsCycle, now)
		if view.CreditsRemain >= 0 && view.CreditsTotal > 0 {
			usedCredits := max(0, view.CreditsTotal-view.CreditsRemain)
			view.UsagePct = clampPercent(usedCredits, view.CreditsTotal)
			if view.CreditsRemain == 0 {
				view.UsageLabel = fmt.Sprintf("0/%d 额度 · 已耗尽", view.CreditsTotal)
			} else {
				view.UsageLabel = fmt.Sprintf("余 %d/%d 额度", view.CreditsRemain, view.CreditsTotal)
			}
		} else if view.CreditsRemain >= 0 {
			if view.CreditsRemain == 0 {
				view.UsageLabel = "0 额度 · 已耗尽"
				view.UsagePct = 100
			} else {
				view.UsageLabel = fmt.Sprintf("余 %d 额度", view.CreditsRemain)
			}
		}
		if view.PlatformLimited {
			view.UsagePct = 100
		} else if view.UsagePercent >= 0 && view.UsagePercent > view.UsagePct {
			view.UsagePct = view.UsagePercent
		}
		return view
	}

	// 豆包免费版（动态限额池，无固定剩余张数，但有窗口已用百分比、限流状态与重置卡）
	if view.PlatformLimited {
		view.UsagePct = 100
		view.UsageLabel = "已触发限额"
		if view.CreditsResetAt > now.Unix() {
			view.CreditsResetLabel = formatDoubaoResumeLabel(view.CreditsResetAt, now)
		}
		return view
	}
	if view.CreditsResetAt > now.Unix() {
		view.CreditsResetLabel = formatCreditsResetLabel(view.CreditsResetAt, view.CreditsCycle, now)
	}
	if view.UsagePercent > 0 {
		view.UsagePct = view.UsagePercent
		view.UsageLabel = fmt.Sprintf("窗口已用 %d%%", view.UsagePercent)
	} else if dailyCap > 0 {
		view.UsagePct = clampPercent(usedToday, dailyCap)
		view.UsageLabel = fmt.Sprintf("额度正常 · 今日 %d/%d", usedToday, dailyCap)
	} else {
		view.UsageLabel = "额度正常"
	}
	return view
}

func buildGeminiQuotaView(b store.Browser, usedToday, dailyCap int, now time.Time) accountQuotaView {
	view := accountQuotaView{
		CreditsRemain: -1,
		UsagePercent:  -1,
	}
	if !b.QuotaSynced() {
		if dailyCap > 0 {
			view.UsageLabel = fmt.Sprintf("今日 %d/%d · 待同步额度", usedToday, dailyCap)
			view.UsagePct = clampPercent(usedToday, dailyCap)
		} else {
			view.UsageLabel = fmt.Sprintf("今日 %d 次 · 待同步额度", usedToday)
		}
		return view
	}

	view.CreditsSynced = true
	view.CreditsRemain = b.CreditsRemain
	view.CreditsTotal = b.CreditsTotal
	view.CreditCost = b.CreditCost
	effectiveResetAt := effectiveQuotaResetAt(b.CreditsResetAt, b.CreditsSyncedAt)
	view.CreditsResetAt = effectiveResetAt
	view.CreditsCycle = b.CreditsCycle
	view.CreditsCycleLabel = formatCreditsCycleLabel(b.CreditsCycle)
	view.CreditsResetLabel = formatCreditsResetLabel(effectiveResetAt, b.CreditsCycle, now)
	view.MemberTier = b.MemberTier
	view.MemberLabel = strings.TrimSpace(b.MemberName)
	if view.MemberLabel == "" {
		switch strings.ToLower(strings.TrimSpace(b.MemberTier)) {
		case "ultra":
			view.MemberLabel = "AI Ultra"
		case "pro":
			view.MemberLabel = "AI Pro"
		case "plus":
			view.MemberLabel = "AI Plus"
		default:
			view.MemberLabel = "免费版"
		}
	}
	view.CreditsSyncedAt = b.CreditsSyncedAt
	view.CreditsSyncedLabel = formatSyncedAtLabel(b.CreditsSyncedAt, now)

	switch {
	case (b.CreditsCycle == cdp.GeminiQuotaCycleFiveHour || b.CreditsCycle == cdp.GeminiQuotaCycleWeekly) && b.CreditsRemain >= 0:
		usedPct := 100 - b.CreditsRemain
		if usedPct < 0 {
			usedPct = 0
		}
		if usedPct > 100 {
			usedPct = 100
		}
		view.UsagePct = usedPct
		if b.CreditsRemain == 0 {
			view.UsageLabel = "剩余 0% · 已耗尽"
		} else {
			view.UsageLabel = fmt.Sprintf("余 %d%%", b.CreditsRemain)
		}
	case b.CreditsCycle == cdp.GeminiQuotaCycleAICredits && b.CreditsRemain >= 0:
		if b.CreditsRemain == 0 {
			view.UsagePct = 100
			view.UsageLabel = "0 AI积分 · 已耗尽"
		} else {
			view.UsageLabel = fmt.Sprintf("余 %d AI积分", b.CreditsRemain)
		}
	case b.CreditsRemain >= 0 && b.CreditsTotal > 0:
		used := max(0, b.CreditsTotal-b.CreditsRemain)
		view.UsagePct = clampPercent(used, b.CreditsTotal)
		if b.CreditsRemain == 0 {
			view.UsageLabel = fmt.Sprintf("0/%d · 已耗尽", b.CreditsTotal)
		} else {
			view.UsageLabel = fmt.Sprintf("余 %d/%d", b.CreditsRemain, b.CreditsTotal)
		}
	case dailyCap > 0:
		view.UsageLabel = fmt.Sprintf("今日 %d/%d", usedToday, dailyCap)
		view.UsagePct = clampPercent(usedToday, dailyCap)
	default:
		view.UsageLabel = view.MemberLabel
	}
	return view
}

func formatDoubaoResumeLabel(resetAt int64, now time.Time) string {
	loc := now.Location()
	if loc == nil {
		loc = time.Local
	}
	t := time.Unix(resetAt, 0).In(loc)
	ny, nm, nd := now.In(loc).Date()
	ty, tm, td := t.Date()
	if ny == ty && nm == tm && nd == td {
		return fmt.Sprintf("预计 %s 恢复", t.Format("15:04"))
	}
	return fmt.Sprintf("预计 %s 恢复", t.Format("01-02 15:04"))
}

func buildQwenQuotaView(b store.Browser, usedToday, dailyCap int, now time.Time) accountQuotaView {
	view := accountQuotaView{
		CreditsRemain: -1,
		UsagePercent:  -1,
		CreditCost:    b.CreditCost,
	}
	if view.CreditCost <= 0 {
		view.CreditCost = cdp.QwenDefaultCreditCost
	}

	if !b.QuotaSynced() {
		view.ConcurrencyLabel = "免费突发≤6(建议2) · 会员不限"
		if dailyCap > 0 {
			view.UsageLabel = fmt.Sprintf("今日 %d/%d · 待同步积分", usedToday, dailyCap)
			view.UsagePct = clampPercent(usedToday, dailyCap)
		} else {
			view.UsageLabel = fmt.Sprintf("今日 %d 次 · 待同步积分", usedToday)
		}
		return view
	}

	view.CreditsSynced = true
	view.CreditsRemain = b.CreditsRemain
	view.CreditsTotal = b.CreditsTotal
	effectiveResetAt := effectiveQuotaResetAt(b.CreditsResetAt, b.CreditsSyncedAt)
	view.CreditsResetAt = effectiveResetAt
	view.CreditsCycle = b.CreditsCycle
	view.CreditsCycleLabel = formatCreditsCycleLabel(b.CreditsCycle)
	view.CreditsResetLabel = formatCreditsResetLabel(effectiveResetAt, b.CreditsCycle, now)
	view.MemberTier = b.MemberTier
	view.MemberLabel = strings.TrimSpace(b.MemberName)
	if view.MemberLabel == "" {
		if b.MemberTier == "vip" {
			view.MemberLabel = "创作会员"
		} else {
			view.MemberLabel = "免费套餐"
		}
	}
	view.ConcurrencyLimit = b.AccountConcurrency
	view.ConcurrencyLabel = formatQwenConcurrencyLabel(b.AccountConcurrency, b.MemberTier)
	view.WatermarkFree = b.WatermarkFree
	view.CreditsSyncedAt = b.CreditsSyncedAt
	view.CreditsSyncedLabel = formatSyncedAtLabel(b.CreditsSyncedAt, now)

	switch {
	case view.CreditsRemain >= 0 && view.CreditsTotal > 0:
		usedCredits := max(0, view.CreditsTotal-view.CreditsRemain)
		view.UsagePct = clampPercent(usedCredits, view.CreditsTotal)
		if view.CreditsRemain == 0 {
			view.UsageLabel = fmt.Sprintf("0/%d 积分 · 已耗尽", view.CreditsTotal)
		} else if view.CreditCost > 1 && view.CreditsRemain < view.CreditCost {
			view.UsageLabel = fmt.Sprintf("余 %d/%d 积分 · 不足1张", view.CreditsRemain, view.CreditsTotal)
			view.UsagePct = 100
		} else {
			view.UsageLabel = fmt.Sprintf("余 %d/%d 积分", view.CreditsRemain, view.CreditsTotal)
		}
	case view.CreditsRemain >= 0:
		if view.CreditsRemain == 0 {
			view.UsageLabel = "0 积分 · 已耗尽"
			view.UsagePct = 100
		} else if view.CreditCost > 1 && view.CreditsRemain < view.CreditCost {
			view.UsageLabel = fmt.Sprintf("余 %d 积分 · 不足1张", view.CreditsRemain)
			view.UsagePct = 100
		} else {
			view.UsageLabel = fmt.Sprintf("余 %d 积分", view.CreditsRemain)
		}
	case dailyCap > 0:
		view.UsageLabel = fmt.Sprintf("今日 %d/%d", usedToday, dailyCap)
		view.UsagePct = clampPercent(usedToday, dailyCap)
	}
	return view
}

func buildJimengQuotaView(b store.Browser, usedToday, dailyCap int, now time.Time) accountQuotaView {
	view := accountQuotaView{
		CreditsRemain: -1,
		UsagePercent:  -1,
		CreditCost:    b.CreditCost,
	}
	if view.CreditCost <= 0 {
		view.CreditCost = cdp.JimengDefaultCreditCost
	}
	if !b.QuotaSynced() {
		if dailyCap > 0 {
			view.UsageLabel = fmt.Sprintf("今日 %d/%d · 待同步积分", usedToday, dailyCap)
			view.UsagePct = clampPercent(usedToday, dailyCap)
		} else {
			view.UsageLabel = fmt.Sprintf("今日 %d 次 · 待同步积分", usedToday)
		}
		return view
	}

	view.CreditsSynced = true
	view.CreditsRemain = b.CreditsRemain
	view.CreditsTotal = b.CreditsTotal
	effectiveResetAt := effectiveQuotaResetAt(b.CreditsResetAt, b.CreditsSyncedAt)
	view.CreditsResetAt = effectiveResetAt
	view.CreditsCycle = b.CreditsCycle
	view.CreditsCycleLabel = formatCreditsCycleLabel(b.CreditsCycle)
	view.CreditsResetLabel = formatCreditsResetLabel(effectiveResetAt, b.CreditsCycle, now)
	view.MemberTier = b.MemberTier
	view.MemberLabel = strings.TrimSpace(b.MemberName)
	if view.MemberLabel == "" {
		if b.MemberTier == "vip" {
			view.MemberLabel = "即梦会员"
		} else {
			view.MemberLabel = "免费版"
		}
	}
	view.WatermarkFree = b.WatermarkFree
	view.CreditsSyncedAt = b.CreditsSyncedAt
	view.CreditsSyncedLabel = formatSyncedAtLabel(b.CreditsSyncedAt, now)

	switch {
	case view.CreditsRemain >= 0 && view.CreditsTotal > 0:
		usedCredits := max(0, view.CreditsTotal-view.CreditsRemain)
		view.UsagePct = clampPercent(usedCredits, view.CreditsTotal)
		if view.CreditsRemain == 0 {
			view.UsageLabel = fmt.Sprintf("0/%d 积分 · 已耗尽", view.CreditsTotal)
		} else if view.CreditCost > 1 && view.CreditsRemain < view.CreditCost {
			view.UsageLabel = fmt.Sprintf("余 %d/%d 积分 · 不足1张", view.CreditsRemain, view.CreditsTotal)
			view.UsagePct = 100
		} else {
			view.UsageLabel = fmt.Sprintf("余 %d/%d 积分", view.CreditsRemain, view.CreditsTotal)
		}
	case view.CreditsRemain >= 0:
		if view.CreditsRemain == 0 {
			view.UsageLabel = "0 积分 · 已耗尽"
			view.UsagePct = 100
		} else if view.CreditCost > 1 && view.CreditsRemain < view.CreditCost {
			view.UsageLabel = fmt.Sprintf("余 %d 积分 · 不足1张", view.CreditsRemain)
			view.UsagePct = 100
		} else {
			view.UsageLabel = fmt.Sprintf("余 %d 积分", view.CreditsRemain)
		}
	case dailyCap > 0:
		view.UsageLabel = fmt.Sprintf("今日 %d/%d", usedToday, dailyCap)
		view.UsagePct = clampPercent(usedToday, dailyCap)
	default:
		view.UsageLabel = view.MemberLabel
	}
	return view
}

func formatCreditsCycleLabel(cycle string) string {
	switch strings.TrimSpace(cycle) {
	case "daily":
		return "每日刷新"
	case "7d":
		return "7日周期"
	case "monthly":
		return "会员月度"
	case "yearly":
		return "会员年度"
	case cdp.GeminiQuotaCycleFiveHour:
		return "5小时窗口"
	case cdp.GeminiQuotaCycleWeekly:
		return "每周上限"
	case cdp.GeminiQuotaCycleAICredits:
		return "AI 积分"
	case cdp.GeminiQuotaCycleWindow:
		return "周期窗口"
	default:
		return ""
	}
}

// effectiveQuotaResetAt 过滤掉在最近一次同步时就已经过期的历史刷新时间，
// 避免刚同步完成后界面仍提示「已到期 · 待同步刷新」。
func effectiveQuotaResetAt(resetAt, syncedAt int64) int64 {
	if resetAt > 0 && syncedAt >= resetAt {
		return 0
	}
	return resetAt
}

func formatCreditsResetLabel(resetAt int64, cycle string, now time.Time) string {
	trimmedCycle := strings.TrimSpace(cycle)
	if resetAt <= 0 {
		switch trimmedCycle {
		case "daily":
			return "每日 00:00 刷新"
		case "7d":
			return "7日周期到期"
		case "monthly":
			return "按月刷新"
		case "yearly":
			return "按年刷新"
		case cdp.GeminiQuotaCycleFiveHour:
			return "5小时滚动刷新"
		case cdp.GeminiQuotaCycleWeekly:
			return "每周滚动刷新"
		default:
			return ""
		}
	}
	loc := now.Location()
	if loc == nil {
		loc = time.Local
	}
	t := time.Unix(resetAt, 0).In(loc)
	if !now.Before(t) {
		return "已到期 · 待同步刷新"
	}
	suffix := "到期"
	if trimmedCycle == cdp.GeminiQuotaCycleFiveHour || trimmedCycle == cdp.GeminiQuotaCycleWeekly {
		suffix = "刷新"
	}
	ny, nm, nd := now.In(loc).Date()
	ty, tm, td := t.Date()
	if ny == ty && nm == tm && nd == td {
		if t.Hour() == 23 && t.Minute() >= 59 {
			return "今日 23:59 刷新"
		}
		return fmt.Sprintf("今日 %s %s", t.Format("15:04"), suffix)
	}
	tomorrow := now.In(loc).AddDate(0, 0, 1)
	if tomorrow.Year() == ty && tomorrow.Month() == tm && tomorrow.Day() == td {
		return fmt.Sprintf("明日 %s %s", t.Format("15:04"), suffix)
	}
	return fmt.Sprintf("%s %s", t.Format("01-02 15:04"), suffix)
}

func formatQwenConcurrencyLabel(limit int, memberTier string) string {
	if limit < 0 || strings.EqualFold(strings.TrimSpace(memberTier), "vip") {
		return "不限并发"
	}
	if limit > 0 {
		return fmt.Sprintf("单号≤%d(建议2)", limit)
	}
	return "单号≤6(建议2)"
}

func formatSyncedAtLabel(syncedAt int64, now time.Time) string {
	if syncedAt <= 0 {
		return ""
	}
	loc := now.Location()
	if loc == nil {
		loc = time.Local
	}
	t := time.Unix(syncedAt, 0).In(loc)
	diff := now.Sub(t)
	if diff >= 0 && diff < time.Minute {
		return "刚刚同步"
	}
	ny, nm, nd := now.In(loc).Date()
	ty, tm, td := t.Date()
	if ny == ty && nm == tm && nd == td {
		return fmt.Sprintf("%s 同步", t.Format("15:04"))
	}
	return fmt.Sprintf("%s 同步", t.Format("01-02 15:04"))
}

func (a *App) probeQwenStatus(ctx context.Context, endpoint string) (cdp.QwenStatus, error) {
	a.mu.Lock()
	fn := a.qwenStatusProbe
	a.mu.Unlock()
	if fn != nil {
		return fn(ctx, endpoint)
	}
	return cdp.ProbeQwenStatus(ctx, endpoint)
}

func (a *App) probeDoubaoStatus(ctx context.Context, endpoint string) (cdp.DoubaoStatus, error) {
	a.mu.Lock()
	fn := a.doubaoStatusProbe
	a.mu.Unlock()
	if fn != nil {
		return fn(ctx, endpoint)
	}
	return cdp.ProbeDoubaoStatus(ctx, endpoint)
}

func (a *App) probeGeminiStatus(ctx context.Context, endpoint string) (cdp.GeminiStatus, error) {
	a.mu.Lock()
	fn := a.geminiStatusProbe
	a.mu.Unlock()
	if fn != nil {
		return fn(ctx, endpoint)
	}
	return cdp.ProbeGeminiStatus(ctx, endpoint)
}

func (a *App) probeJimengStatus(ctx context.Context, endpoint string) (cdp.JimengStatus, error) {
	a.mu.Lock()
	fn := a.jimengStatusProbe
	a.mu.Unlock()
	if fn != nil {
		return fn(ctx, endpoint)
	}
	return cdp.ProbeJimengStatus(ctx, endpoint)
}

func (a *App) probeJimengTargetModelCost(ctx context.Context, endpoint string) (int, error) {
	a.mu.Lock()
	hasCustomProbe := a.jimengStatusProbe != nil
	a.mu.Unlock()
	if hasCustomProbe {
		return 0, nil
	}
	return cdp.ProbeJimengTargetModelCost(ctx, endpoint)
}

func (a *App) applyQwenStatus(id int64, raw cdp.QwenStatus) (store.AccountQuota, bool) {
	if a == nil || id <= 0 || a.store == nil {
		return store.AccountQuota{}, false
	}
	quota, ok := cdp.NormalizeQwenStatus(raw, time.Now())
	if !ok {
		return store.AccountQuota{}, false
	}
	if err := a.store.UpdateBrowserQuota(id, quota); err != nil {
		logging.Log.Warning("保存千问账号状态失败: %v", err)
		return store.AccountQuota{}, false
	}
	if cur, ok := a.browserByID(id); ok && (!cur.IsLoggedIn || accountNeedsLogin(cur)) {
		a.confirmLogin(id, true)
	}
	a.refreshBrowsers()
	a.emit("browsers", "workbench", "shell")
	return quota, true
}

func (a *App) applyDoubaoStatus(id int64, raw cdp.DoubaoStatus) (store.AccountQuota, bool) {
	if a == nil || id <= 0 || a.store == nil {
		return store.AccountQuota{}, false
	}
	quota, ok := cdp.NormalizeDoubaoStatus(raw, time.Now())
	if !ok {
		return store.AccountQuota{}, false
	}
	if err := a.store.UpdateBrowserQuota(id, quota); err != nil {
		logging.Log.Warning("保存豆包账号状态失败: %v", err)
		return store.AccountQuota{}, false
	}
	if cur, ok := a.browserByID(id); ok && (!cur.IsLoggedIn || accountNeedsLogin(cur)) {
		a.confirmLogin(id, true)
	}
	a.refreshBrowsers()
	a.emit("browsers", "workbench", "shell")
	return quota, true
}

func (a *App) applyGeminiStatus(id int64, raw cdp.GeminiStatus) (store.AccountQuota, bool) {
	if a == nil || id <= 0 || a.store == nil {
		return store.AccountQuota{}, false
	}
	quota, ok := cdp.NormalizeGeminiStatus(raw, time.Now())
	if !ok {
		return store.AccountQuota{}, false
	}
	if candidate := cdp.SanitizeAccountUsername(raw.Username); candidate != "" {
		a.applyAutoAccountName(id, candidate)
	} else if candidate := cdp.SanitizeAccountUsername(raw.Email); candidate != "" {
		a.applyAutoAccountName(id, candidate)
	}
	if err := a.store.UpdateBrowserQuota(id, quota); err != nil {
		logging.Log.Warning("保存 Gemini 账号状态失败: %v", err)
		return store.AccountQuota{}, false
	}
	if cur, ok := a.browserByID(id); ok && (!cur.IsLoggedIn || accountNeedsLogin(cur)) {
		a.confirmLogin(id, true)
	}
	a.refreshBrowsers()
	a.emit("browsers", "workbench", "shell")
	return quota, true
}

func (a *App) applyJimengStatus(id int64, raw cdp.JimengStatus) (store.AccountQuota, bool) {
	if a == nil || id <= 0 || a.store == nil {
		return store.AccountQuota{}, false
	}
	quota, ok := cdp.NormalizeJimengStatus(raw, time.Now())
	if !ok {
		return store.AccountQuota{}, false
	}
	if candidate := cdp.SanitizeAccountUsername(raw.Username); candidate != "" {
		a.applyAutoAccountName(id, candidate)
	}
	if err := a.store.UpdateBrowserQuota(id, quota); err != nil {
		logging.Log.Warning("保存即梦账号状态失败: %v", err)
		return store.AccountQuota{}, false
	}
	if cur, ok := a.browserByID(id); ok && (!cur.IsLoggedIn || accountNeedsLogin(cur)) {
		a.confirmLogin(id, true)
	}
	a.refreshBrowsers()
	a.emit("browsers", "workbench", "shell")
	return quota, true
}

// probeRunningAccountStatuses 异步刷新当前已在运行中的豆包、千问、Gemini 与即梦账号状态（不额外启动已关闭的浏览器）。
func (a *App) probeRunningAccountStatuses() {
	if a == nil || a.store == nil || a.manager == nil || a.running {
		return
	}
	var targets []store.Browser
	staleCaps := map[string]bool{}
	now := time.Now()
	for _, b := range a.browserRows {
		if store.OutputCapsSupportedPlatform(b.Platform) && a.manager.IsRunning(b.ID) {
			targets = append(targets, b)
			if a.outputCapsStale(b.Platform, now) {
				staleCaps[b.Platform] = true
			}
		}
	}
	if len(targets) == 0 {
		return
	}
	if !a.qwenLiveProbing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.qwenLiveProbing.Store(false)
		probedCaps := map[string]bool{}
		for _, row := range targets {
			if a.isQuitting() {
				return
			}
			ep := loginEndpoint(row)
			if ep == "" {
				continue
			}
			timeout := 6 * time.Second
			if row.Platform == "Gemini" || row.Platform == "即梦" {
				timeout = 8 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			switch row.Platform {
			case "千问":
				st, err := a.probeQwenStatus(ctx, ep)
				cancel()
				if err == nil && st.OK {
					id := row.ID
					a.postUI(func() { a.applyQwenStatus(id, st) })
				}
			case "豆包":
				st, err := a.probeDoubaoStatus(ctx, ep)
				cancel()
				if err == nil && st.OK {
					id := row.ID
					a.postUI(func() { a.applyDoubaoStatus(id, st) })
				}
			case "Gemini":
				st, err := a.probeGeminiStatus(ctx, ep)
				cancel()
				if err == nil && st.OK {
					id := row.ID
					a.postUI(func() { a.applyGeminiStatus(id, st) })
				}
			case "即梦":
				st, err := a.probeJimengStatus(ctx, ep)
				cancel()
				if err == nil && st.OK {
					id := row.ID
					a.postUI(func() { a.applyJimengStatus(id, st) })
				}
			default:
				cancel()
			}
			if staleCaps[row.Platform] && !probedCaps[row.Platform] && !a.isQuitting() {
				probedCaps[row.Platform] = true
				a.piggybackOutputCaps(context.Background(), row.Platform, ep)
			}
		}
	}()
}

// syncQwenStatus 批量或单个同步千问账号的实时积分、刷新时间、会员与并发上限。
func (a *App) syncQwenStatus(ids []int64) error {
	return a.syncAccountStatus(ids, "千问")
}

// syncGeminiStatus 批量或单个同步 Gemini 账号的实时剩余额度、刷新时间、套餐与用户名。
func (a *App) syncGeminiStatus(ids []int64) error {
	return a.syncAccountStatus(ids, "Gemini")
}

// syncJimengStatus 批量或单个同步即梦账号的实时积分、会员与用户名。
func (a *App) syncJimengStatus(ids []int64) error {
	return a.syncAccountStatus(ids, "即梦")
}

// syncAccountStatus 批量或单个同步豆包/千问/Gemini/即梦账号的实时额度、套餐与限流状态。
// platformFilter 为空时支持豆包、千问、Gemini 与即梦全部账号；ids 为空时同步该范围内的全部账号。
func (a *App) syncAccountStatus(ids []int64, platformFilter string) error {
	if a.running {
		return errors.New("任务运行中，任务完成后会自动刷新账号状态")
	}
	if a.syncQwenCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "syncing_status") {
		return errors.New("正在同步账号状态，请等待完成或先停止")
	}
	if a.syncNamesCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "syncing") {
		return errors.New("正在同步账号用户名，请等待完成后再同步状态")
	}
	if a.clearHistoryCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "clearing") {
		return errors.New("正在清理聊天历史，请等待完成后再同步状态")
	}
	if a.probeImageCancel != nil || (a.browserBatch != nil && a.browserBatch.opKind() == "probing_image") {
		return errors.New("正在检测豆包生图能力，请等待完成后再同步状态")
	}

	supportsPlatform := func(plat string) bool {
		if platformFilter != "" {
			return plat == platformFilter
		}
		return plat == "豆包" || plat == "千问" || plat == "Gemini" || plat == "即梦"
	}

	var requested []store.Browser
	seen := map[int64]bool{}
	if len(ids) > 0 {
		for _, id := range ids {
			if id <= 0 || seen[id] {
				continue
			}
			if row, ok := a.browserByID(id); ok && supportsPlatform(row.Platform) {
				seen[id] = true
				requested = append(requested, row)
			}
		}
	} else {
		for _, row := range a.browserRows {
			if supportsPlatform(row.Platform) && !seen[row.ID] {
				seen[row.ID] = true
				requested = append(requested, row)
			}
		}
	}
	if len(requested) == 0 {
		if platformFilter != "" {
			return fmt.Errorf("当前没有可同步状态的%s账号", platformFilter)
		}
		return errors.New("当前没有可同步状态的豆包、千问、Gemini 或即梦账号")
	}
	var targets []store.Browser
	for _, row := range requested {
		if a.clearHistoryAllowed(row) {
			targets = append(targets, row)
		}
	}
	if len(targets) == 0 {
		return errors.New("所选账号当前正在操作中，请稍后再试")
	}
	for _, row := range targets {
		hasStub := (row.Platform == "千问" && a.syncQwenStatusFn != nil) ||
			(row.Platform == "豆包" && a.syncDoubaoStatusFn != nil) ||
			(row.Platform == "Gemini" && a.syncGeminiStatusFn != nil) ||
			(row.Platform == "即梦" && a.syncJimengStatusFn != nil)
		if !hasStub && a.manager == nil {
			return errors.New("没有受管浏览器管理器，无法同步账号状态")
		}
	}
	if a.browserPending == nil {
		a.browserPending = map[int64]browserOperation{}
	}

	groups := map[string][]store.Browser{}
	var order []string
	for _, row := range targets {
		if _, ok := groups[row.Platform]; !ok {
			order = append(order, row.Platform)
		}
		groups[row.Platform] = append(groups[row.Platform], row)
	}
	statusLabel := platformFilter
	if statusLabel == "" {
		if len(order) == 1 {
			statusLabel = order[0]
		} else {
			statusLabel = "账号"
		}
	}

	batch := &browserBatchRun{
		kind:        "syncing_status",
		statusLabel: statusLabel,
		total:       len(targets),
		skipped:     len(requested) - len(targets),
	}
	for _, row := range targets {
		a.browserPending[row.ID] = browserOperation{kind: "syncing_status", batch: batch}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	a.browserBatch = batch
	a.syncQwenCancel = cancel
	a.emit("browsers", "workbench")

	go func() {
		defer func() {
			cancel()
			a.postUI(func() {
				if a.browserBatch == batch {
					a.browserBatch = nil
				}
				a.syncQwenCancel = nil
				for _, row := range targets {
					if op, ok := a.browserPending[row.ID]; ok && op.batch == batch {
						delete(a.browserPending, row.ID)
					}
				}
				a.refreshBrowsers()
				a.emit("browsers", "workbench", "shell")
			})
		}()

		var totalUpdated atomic.Int64
		var totalFailed atomic.Int64
		var firstErrMu sync.Mutex
		var firstErrMsg string
		recordFailure := func(err error) {
			if err == nil || errors.Is(err, context.Canceled) {
				return
			}
			totalFailed.Add(1)
			firstErrMu.Lock()
			if firstErrMsg == "" {
				firstErrMsg = err.Error()
			}
			firstErrMu.Unlock()
		}
		var wg sync.WaitGroup
		for _, plat := range order {
			list := groups[plat]
			wg.Add(1)
			go func(rows []store.Browser) {
				defer wg.Done()
				for _, row := range rows {
					if runCtx.Err() != nil {
						a.postUI(func() {
							a.finishBrowserOperation(row.ID, context.Canceled)
							a.emit("browsers", "workbench")
						})
						continue
					}
					label := fmt.Sprintf("%s/%s", row.Platform, accountLabel(row))
					a.postUI(func() {
						batch.current = label
						a.emit("browsers", "workbench")
					})

					var quota store.AccountQuota
					var applied bool
					var err error
					switch row.Platform {
					case "千问":
						var raw cdp.QwenStatus
						var scrapedName string
						raw, scrapedName, err = a.syncOneQwenStatus(runCtx, row)
						if err == nil && !raw.OK {
							err = errors.New("未能从千问页面读取到积分或会员状态")
						}
						if err == nil {
							a.syncUI(func() {
								if scrapedName != "" {
									a.applySyncedAccountName(row.ID, scrapedName)
								}
								quota, applied = a.applyQwenStatus(row.ID, raw)
								if applied {
									batch.updated++
								}
								a.finishBrowserOperation(row.ID, nil)
								a.emit("browsers", "workbench", "shell")
							})
							if applied {
								totalUpdated.Add(1)
								logging.Log.Success("账号[%s] 状态已同步：剩余 %d/%d 积分（%s）", label, quota.CreditsRemain, quota.CreditsTotal, quota.MemberName)
							}
							continue
						}
					case "豆包":
						var raw cdp.DoubaoStatus
						var scrapedName string
						raw, scrapedName, err = a.syncOneDoubaoStatus(runCtx, row)
						if err == nil && !raw.OK {
							err = errors.New("未能从豆包页面读取到订阅或额度状态")
						}
						if err == nil {
							a.syncUI(func() {
								if scrapedName != "" {
									a.applySyncedAccountName(row.ID, scrapedName)
								}
								quota, applied = a.applyDoubaoStatus(row.ID, raw)
								if applied {
									batch.updated++
								}
								a.finishBrowserOperation(row.ID, nil)
								a.emit("browsers", "workbench", "shell")
							})
							if applied {
								totalUpdated.Add(1)
								if quota.CreditsRemain >= 0 {
									logging.Log.Success("账号[%s] 状态已同步：%s · 剩余 %d 额度", label, quota.MemberName, quota.CreditsRemain)
								} else if quota.PlatformLimited {
									logging.Log.Warning("账号[%s] 状态已同步：%s · 已触发限额", label, quota.MemberName)
								} else {
									logging.Log.Success("账号[%s] 状态已同步：%s · 窗口已用 %d%%", label, quota.MemberName, max(quota.UsagePercent, 0))
								}
							}
							continue
						}
					case "Gemini":
						var raw cdp.GeminiStatus
						var scrapedName string
						raw, scrapedName, err = a.syncOneGeminiStatus(runCtx, row)
						if err == nil && !raw.OK {
							err = errors.New("未能从 Gemini 页面读取到剩余额度或订阅状态")
						}
						if err == nil {
							a.syncUI(func() {
								if scrapedName != "" {
									a.applySyncedAccountName(row.ID, scrapedName)
								}
								quota, applied = a.applyGeminiStatus(row.ID, raw)
								if applied {
									batch.updated++
								}
								a.finishBrowserOperation(row.ID, nil)
								a.emit("browsers", "workbench", "shell")
							})
							if applied {
								totalUpdated.Add(1)
								if quota.CreditsRemain >= 0 && (quota.CreditsCycle == cdp.GeminiQuotaCycleFiveHour || quota.CreditsCycle == cdp.GeminiQuotaCycleWeekly) {
									logging.Log.Success("账号[%s] 状态已同步：%s · 剩余 %d%%", label, quota.MemberName, quota.CreditsRemain)
								} else if quota.CreditsRemain >= 0 {
									logging.Log.Success("账号[%s] 状态已同步：%s · 剩余 %d", label, quota.MemberName, quota.CreditsRemain)
								} else {
									logging.Log.Success("账号[%s] 状态已同步：%s", label, quota.MemberName)
								}
							}
							continue
						}
					case "即梦":
						var raw cdp.JimengStatus
						var scrapedName string
						raw, scrapedName, err = a.syncOneJimengStatus(runCtx, row)
						if err == nil && !raw.OK {
							err = errors.New("未能从即梦页面读取到积分或账号状态")
						}
						if err == nil {
							a.syncUI(func() {
								if scrapedName != "" {
									a.applySyncedAccountName(row.ID, scrapedName)
								}
								quota, applied = a.applyJimengStatus(row.ID, raw)
								if applied {
									batch.updated++
								}
								a.finishBrowserOperation(row.ID, nil)
								a.emit("browsers", "workbench", "shell")
							})
							if applied {
								totalUpdated.Add(1)
								if quota.CreditsRemain >= 0 {
									logging.Log.Success("账号[%s] 状态已同步：剩余 %d 积分（%s · %d分/张）", label, quota.CreditsRemain, quota.MemberName, max(quota.CreditCost, 1))
								} else {
									logging.Log.Success("账号[%s] 状态已同步：%s", label, quota.MemberName)
								}
							}
							continue
						}
					}
					if errors.Is(err, context.Canceled) {
						logging.Log.Info("账号[%s] 状态同步已停止", label)
					} else if err != nil {
						recordFailure(err)
						logging.Log.Warning("账号[%s] 同步状态失败：%v", label, err)
					}
					a.postUI(func() {
						a.finishBrowserOperation(row.ID, err)
						a.emit("browsers", "workbench")
					})
				}
			}(list)
		}
		wg.Wait()
		if runCtx.Err() == nil {
			updated := totalUpdated.Load()
			failed := totalFailed.Load()
			logging.Log.Info("%s状态同步结束：共处理 %d 个账号，已更新 %d 个", statusLabel, len(targets), updated)
			firstErrMu.Lock()
			errMsg := firstErrMsg
			firstErrMu.Unlock()
			a.postUI(func() {
				switch {
				case failed > 0 && updated == 0:
					if failed == 1 && errMsg != "" {
						a.pushToast("error", fmt.Sprintf("%s状态同步失败：%s", statusLabel, errMsg))
					} else if errMsg != "" {
						a.pushToast("error", fmt.Sprintf("%s状态同步失败（%d 个）：%s", statusLabel, failed, errMsg))
					} else {
						a.pushToast("error", fmt.Sprintf("%s状态同步失败（%d 个）", statusLabel, failed))
					}
				case failed > 0 && updated > 0:
					a.pushToast("warning", fmt.Sprintf("%s状态同步完成：已更新 %d/%d 个，失败 %d 个", statusLabel, updated, len(targets), failed))
				case updated > 0:
					if len(targets) == 1 {
						a.pushToast("success", fmt.Sprintf("%s状态已同步", statusLabel))
					} else {
						a.pushToast("success", fmt.Sprintf("已同步 %d 个%s账号状态", updated, statusLabel))
					}
				}
			})
		}
	}()
	return nil
}

func (a *App) stopSyncQwenStatus() {
	if a.syncQwenCancel != nil {
		a.syncQwenCancel()
		logging.Log.Info("已请求停止同步账号状态")
	}
}

func (a *App) stopSyncGeminiStatus() {
	a.stopSyncQwenStatus()
}

func (a *App) stopSyncJimengStatus() {
	a.stopSyncQwenStatus()
}

func (a *App) syncOneQwenStatus(ctx context.Context, b store.Browser) (cdp.QwenStatus, string, error) {
	if a.syncQwenStatusFn != nil {
		st, err := a.syncQwenStatusFn(ctx, b)
		return st, "", err
	}
	ws, _, cleanup, err := a.openStatusProbeBrowser(ctx, b)
	if err != nil {
		return cdp.QwenStatus{}, "", err
	}
	defer cleanup()
	st, scraped, err := a.pollQwenStatus(ctx, ws)
	if err == nil && st.OK {
		a.piggybackOutputCaps(ctx, "千问", ws)
	}
	return st, scraped, err
}

func (a *App) syncOneDoubaoStatus(ctx context.Context, b store.Browser) (cdp.DoubaoStatus, string, error) {
	if a.syncDoubaoStatusFn != nil {
		st, err := a.syncDoubaoStatusFn(ctx, b)
		return st, "", err
	}
	ws, _, cleanup, err := a.openStatusProbeBrowser(ctx, b)
	if err != nil {
		return cdp.DoubaoStatus{}, "", err
	}
	defer cleanup()
	st, scraped, err := a.pollDoubaoStatus(ctx, ws)
	if err == nil && st.OK {
		a.piggybackOutputCaps(ctx, "豆包", ws)
	}
	return st, scraped, err
}

func (a *App) syncOneGeminiStatus(ctx context.Context, b store.Browser) (cdp.GeminiStatus, string, error) {
	if a.syncGeminiStatusFn != nil {
		st, err := a.syncGeminiStatusFn(ctx, b)
		name := cdp.SanitizeAccountUsername(st.Username)
		if name == "" {
			name = cdp.SanitizeAccountUsername(st.Email)
		}
		return st, name, err
	}
	ws, _, cleanup, err := a.openStatusProbeBrowser(ctx, b)
	if err != nil {
		return cdp.GeminiStatus{}, "", err
	}
	defer cleanup()
	st, scraped, err := a.pollGeminiStatus(ctx, ws)
	if err == nil && st.OK {
		a.piggybackOutputCaps(ctx, "Gemini", ws)
	}
	return st, scraped, err
}

func (a *App) syncOneJimengStatus(ctx context.Context, b store.Browser) (cdp.JimengStatus, string, error) {
	if a.syncJimengStatusFn != nil {
		st, err := a.syncJimengStatusFn(ctx, b)
		name := cdp.SanitizeAccountUsername(st.Username)
		return st, name, err
	}
	ws, wasRunning, cleanup, err := a.openStatusProbeBrowser(ctx, b)
	if err != nil {
		return cdp.JimengStatus{}, "", err
	}
	defer cleanup()
	st, scraped, err := a.pollJimengStatus(ctx, ws)
	// 若是后台临时拉起的同步浏览器，且网页尚未停留在目标生图模型（Seedream 4.7 · 2K），
	// 自动切换到目标模型读取网页实际显示的每张积分单价，避免停留在页面默认 Seedream 5.5 导致单价失准。
	if err == nil && st.OK && !wasRunning && (!st.CostModelMatched || st.CreditCost <= 0) {
		costCtx, costCancel := context.WithTimeout(ctx, 6*time.Second)
		if cost, cErr := a.probeJimengTargetModelCost(costCtx, ws); cErr == nil && cost > 0 {
			st.CreditCost = cost
			st.Model = cdp.JimengTargetModel
			st.Resolution = strings.ToUpper(cdp.JimengTargetResolution)
			st.CostModelMatched = true
		}
		costCancel()
	}
	if err == nil && st.OK {
		a.piggybackOutputCaps(ctx, "即梦", ws)
	}
	return st, scraped, err
}

func (a *App) openStatusProbeBrowser(ctx context.Context, b store.Browser) (string, bool, func(), error) {
	plat := strategy.Factory(b.Platform)
	if plat == nil {
		return "", false, func() {}, fmt.Errorf("不支持的平台：%s", b.Platform)
	}
	if a.manager == nil {
		return "", false, func() {}, errors.New("受管浏览器管理器未初始化")
	}
	a.pushProxy()
	wasRunning := a.manager.IsRunning(b.ID)
	ws, err := a.manager.StartView(ctx, b, a.dataRoot, plat.HomeURL())
	if err != nil {
		return "", false, func() {}, fmt.Errorf("打开内置浏览器失败: %w", err)
	}
	cleanup := func() {}
	if !wasRunning {
		cleanup = func() {
			a.manager.Stop(b.ID)
			if a.store != nil {
				a.store.SetRunning(b.ID, a.manager.IsRunning(b.ID))
			}
		}
	}
	openCtx, openCancel := context.WithTimeout(ctx, 20*time.Second)
	err = cdp.OpenURLBackground(openCtx, ws, plat.HomeURL())
	openCancel()
	if err != nil {
		cleanup()
		return "", false, func() {}, fmt.Errorf("打开%s页面失败: %w", b.Platform, err)
	}
	return ws, wasRunning, cleanup, nil
}

func (a *App) pollQwenStatus(ctx context.Context, endpoint string) (cdp.QwenStatus, string, error) {
	deadline := time.Now().Add(20 * time.Second)
	outCount := 0
	for {
		if ctx.Err() != nil {
			return cdp.QwenStatus{}, "", ctx.Err()
		}
		loginCtx, loginCancel := context.WithTimeout(ctx, min(6*time.Second, max(time.Second, time.Until(deadline))))
		state, err := cdp.ProbeLogin(loginCtx, endpoint)
		loginCancel()
		if err == nil {
			state = cdp.NormalizeLoginState(state)
			switch state {
			case "out":
				outCount++
				if outCount >= 2 {
					return cdp.QwenStatus{}, "", errors.New("账号未登录，请先打开登录")
				}
			case cdp.RegionBlocked:
				return cdp.QwenStatus{}, "", errors.New("当前地区受限，请检查代理设置")
			case "challenge":
				return cdp.QwenStatus{}, "", errors.New("触发安全验证，请先打开账号处理验证")
			case "in":
				outCount = 0
				statusCtx, statusCancel := context.WithTimeout(ctx, min(8*time.Second, max(time.Second, time.Until(deadline))))
				st, qErr := a.probeQwenStatus(statusCtx, endpoint)
				if qErr == nil && st.OK {
					scraped := ""
					nameCtx, nameCancel := context.WithTimeout(ctx, 4*time.Second)
					if name, nErr := cdp.ProbeAccountUsername(nameCtx, endpoint); nErr == nil {
						scraped = cdp.SanitizeAccountUsername(name)
					}
					nameCancel()
					statusCancel()
					return st, scraped, nil
				}
				statusCancel()
				goto waitNext
			}
		}
	waitNext:
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return cdp.QwenStatus{}, "", errors.New("读取千问状态超时，请稍后重试")
		}
		timer := time.NewTimer(min(400*time.Millisecond, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return cdp.QwenStatus{}, "", ctx.Err()
		case <-timer.C:
		}
	}
}

func (a *App) pollDoubaoStatus(ctx context.Context, endpoint string) (cdp.DoubaoStatus, string, error) {
	deadline := time.Now().Add(20 * time.Second)
	outCount := 0
	for {
		if ctx.Err() != nil {
			return cdp.DoubaoStatus{}, "", ctx.Err()
		}
		loginCtx, loginCancel := context.WithTimeout(ctx, min(6*time.Second, max(time.Second, time.Until(deadline))))
		state, err := cdp.ProbeLogin(loginCtx, endpoint)
		loginCancel()
		if err == nil {
			state = cdp.NormalizeLoginState(state)
			switch state {
			case "out":
				outCount++
				if outCount >= 2 {
					return cdp.DoubaoStatus{}, "", errors.New("账号未登录，请先打开登录")
				}
			case cdp.RegionBlocked:
				return cdp.DoubaoStatus{}, "", errors.New("当前地区受限，请检查代理设置")
			case "challenge":
				return cdp.DoubaoStatus{}, "", errors.New("触发安全验证，请先打开账号处理验证")
			case "in":
				outCount = 0
				statusCtx, statusCancel := context.WithTimeout(ctx, min(8*time.Second, max(time.Second, time.Until(deadline))))
				st, dErr := a.probeDoubaoStatus(statusCtx, endpoint)
				if dErr == nil && st.OK {
					scraped := ""
					nameCtx, nameCancel := context.WithTimeout(ctx, 4*time.Second)
					if name, nErr := cdp.ProbeAccountUsername(nameCtx, endpoint); nErr == nil {
						scraped = cdp.SanitizeAccountUsername(name)
					}
					nameCancel()
					statusCancel()
					return st, scraped, nil
				}
				statusCancel()
				goto waitNext
			}
		}
	waitNext:
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return cdp.DoubaoStatus{}, "", errors.New("读取豆包状态超时，请稍后重试")
		}
		timer := time.NewTimer(min(400*time.Millisecond, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return cdp.DoubaoStatus{}, "", ctx.Err()
		case <-timer.C:
		}
	}
}

func geminiStatusHasIdentityOrMeter(st cdp.GeminiStatus) bool {
	if !st.OK {
		return false
	}
	return st.HasFiveHour || st.HasWeekly || st.HasCredits ||
		strings.TrimSpace(st.Username) != "" || strings.TrimSpace(st.Email) != ""
}

func (a *App) pollGeminiStatus(ctx context.Context, endpoint string) (cdp.GeminiStatus, string, error) {
	startedAt := time.Now()
	deadline := startedAt.Add(30 * time.Second)
	outCount := 0
	var lastUnknownProbe time.Time
	lastState := "unknown"
	var lastErr error

	tryProbeGemini := func(requireIdentity bool) (cdp.GeminiStatus, string, bool) {
		rem := time.Until(deadline)
		if rem <= 0 {
			return cdp.GeminiStatus{}, "", false
		}
		statusCtx, statusCancel := context.WithTimeout(ctx, min(12*time.Second, rem))
		st, gErr := a.probeGeminiStatus(statusCtx, endpoint)
		statusCancel()
		if gErr != nil {
			lastErr = gErr
			return cdp.GeminiStatus{}, "", false
		}
		if !st.OK {
			return cdp.GeminiStatus{}, "", false
		}
		if requireIdentity && !geminiStatusHasIdentityOrMeter(st) {
			return cdp.GeminiStatus{}, "", false
		}
		scraped := cdp.SanitizeAccountUsername(st.Username)
		if scraped == "" {
			nameCtx, nameCancel := context.WithTimeout(ctx, 4*time.Second)
			if name, nErr := cdp.ProbeAccountUsername(nameCtx, endpoint); nErr == nil {
				scraped = cdp.SanitizeAccountUsername(name)
			}
			nameCancel()
		}
		if scraped == "" {
			scraped = cdp.SanitizeAccountUsername(st.Email)
		}
		if scraped != "" {
			st.Username = scraped
		}
		return st, scraped, true
	}

	for {
		if ctx.Err() != nil {
			return cdp.GeminiStatus{}, "", ctx.Err()
		}
		loginCtx, loginCancel := context.WithTimeout(ctx, min(6*time.Second, max(time.Second, time.Until(deadline))))
		state, err := cdp.ProbeLogin(loginCtx, endpoint)
		loginCancel()
		if err != nil {
			lastErr = err
		} else {
			state = cdp.NormalizeLoginState(state)
			lastState = state
			switch state {
			case "out":
				outCount++
				if outCount >= 2 {
					return cdp.GeminiStatus{}, "", errors.New("账号未登录，请先打开登录")
				}
			case cdp.RegionBlocked:
				return cdp.GeminiStatus{}, "", errors.New("当前地区受限，请检查代理设置")
			case "challenge":
				return cdp.GeminiStatus{}, "", errors.New("触发安全验证，请先打开账号处理验证")
			case "in":
				outCount = 0
				if st, scraped, ok := tryProbeGemini(false); ok {
					return st, scraped, nil
				}
				goto waitNext
			case "unknown":
				// 某些页面在已登录但存在常驻加载指示器时 ProbeLogin 可能持续返回 unknown；
				// 兜底直接探测官方同源接口，且仅在返回额度池或账号身份时采信，避免误判未登录页。
				if time.Since(startedAt) >= 2*time.Second && time.Since(lastUnknownProbe) >= 2*time.Second {
					lastUnknownProbe = time.Now()
					if st, scraped, ok := tryProbeGemini(true); ok {
						return st, scraped, nil
					}
				}
			}
		}
	waitNext:
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if lastErr != nil {
				logging.Log.Warning("Gemini 状态探测超时（最后登录态=%s，最后错误=%v）", lastState, lastErr)
			} else {
				logging.Log.Warning("Gemini 状态探测超时（最后登录态=%s）", lastState)
			}
			return cdp.GeminiStatus{}, "", errors.New("读取 Gemini 状态超时，请稍后重试")
		}
		timer := time.NewTimer(min(400*time.Millisecond, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return cdp.GeminiStatus{}, "", ctx.Err()
		case <-timer.C:
		}
	}
}

func (a *App) pollJimengStatus(ctx context.Context, endpoint string) (cdp.JimengStatus, string, error) {
	startedAt := time.Now()
	deadline := startedAt.Add(25 * time.Second)
	outCount := 0
	var lastUnknownProbe time.Time
	for {
		if ctx.Err() != nil {
			return cdp.JimengStatus{}, "", ctx.Err()
		}
		loginCtx, loginCancel := context.WithTimeout(ctx, min(6*time.Second, max(time.Second, time.Until(deadline))))
		state, err := cdp.ProbeLogin(loginCtx, endpoint)
		loginCancel()
		if err == nil {
			state = cdp.NormalizeLoginState(state)
			switch state {
			case "out":
				outCount++
				if outCount >= 2 {
					return cdp.JimengStatus{}, "", errors.New("账号未登录，请先打开登录")
				}
			case cdp.RegionBlocked:
				return cdp.JimengStatus{}, "", errors.New("当前地区受限，请检查代理设置")
			case "challenge":
				return cdp.JimengStatus{}, "", errors.New("触发安全验证，请先打开账号处理验证")
			case "in":
				outCount = 0
				statusCtx, statusCancel := context.WithTimeout(ctx, min(8*time.Second, max(time.Second, time.Until(deadline))))
				st, jErr := a.probeJimengStatus(statusCtx, endpoint)
				statusCancel()
				if jErr == nil && st.OK {
					scraped := cdp.SanitizeAccountUsername(st.Username)
					if scraped == "" {
						nameCtx, nameCancel := context.WithTimeout(ctx, 4*time.Second)
						if name, nErr := cdp.ProbeAccountUsername(nameCtx, endpoint); nErr == nil {
							scraped = cdp.SanitizeAccountUsername(name)
						}
						nameCancel()
					}
					if scraped != "" {
						st.Username = scraped
					}
					return st, scraped, nil
				}
				goto waitNext
			case "unknown":
				if time.Since(startedAt) >= 2*time.Second && time.Since(lastUnknownProbe) >= 2*time.Second {
					lastUnknownProbe = time.Now()
					statusCtx, statusCancel := context.WithTimeout(ctx, min(8*time.Second, max(time.Second, time.Until(deadline))))
					st, jErr := a.probeJimengStatus(statusCtx, endpoint)
					statusCancel()
					if jErr == nil && st.OK && (st.HasCredits || strings.TrimSpace(st.Username) != "") {
						scraped := cdp.SanitizeAccountUsername(st.Username)
						if scraped != "" {
							st.Username = scraped
						}
						return st, scraped, nil
					}
				}
			}
		}
	waitNext:
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return cdp.JimengStatus{}, "", errors.New("读取即梦状态超时，请稍后重试")
		}
		timer := time.NewTimer(min(400*time.Millisecond, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return cdp.JimengStatus{}, "", ctx.Err()
		case <-timer.C:
		}
	}
}
