package tasks

import (
	"errors"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

const jimengWashBatchMax = strategy.JimengMaxUploadImages

// errJimengBatchShrink 表示上传前实时核对即梦积分后发现余额少于本批张数，需缩减批次后重新派发。
var errJimengBatchShrink = errors.New("即梦剩余积分少于当前批次张数，需按可生图张数缩减重派")

type jimengCountStrategy interface {
	SetImageCount(int)
	ImageCount() int
}

// jimengCreditPlan 记录单次调用即梦前基于实时积分与实际模型/分辨率单价得出的发图决策。
type jimengCreditPlan struct {
	Known      bool   // 是否成功拿到有效余额（实时探测或未过期缓存）
	Live       bool   // 是否来自本次网页实时探测
	Remain     int    // 当前剩余积分
	Cost       int    // 本次目标模型与分辨率下的实际单张积分消耗
	Affordable int    // 按余额最多可生成张数 (Remain / Cost)
	Desired    int    // 本次任务原计划张数（洗图=本批图数 1..6，生图=6，裂变=1）
	Allowed    int    // 最终允许导入/生成的张数 (min(Desired, Affordable))
	Resolution string // 本次任务目标分辨率（如 "2K" / "4K"）
}

func (e *Engine) jimengTargetResolution() string {
	if e != nil {
		if res := strings.TrimSpace(e.opts.OutputSize.NativeResolution("即梦")); res != "" {
			return strings.ToUpper(res)
		}
	}
	return strings.ToUpper(cdp.JimengTargetResolution)
}

func (e *Engine) setJimengLiveCost(resolution string, cost int) {
	if e == nil || cost <= 0 {
		return
	}
	key := strings.ToLower(strings.TrimSpace(resolution))
	if key == "" {
		key = strings.ToLower(cdp.JimengTargetResolution)
	}
	e.mu.Lock()
	if e.jimengLiveCost == nil {
		e.jimengLiveCost = map[string]int{}
	}
	e.jimengLiveCost[key] = cost
	e.mu.Unlock()
}

func (e *Engine) jimengLiveCostFor(resolution string) int {
	if e == nil {
		return 0
	}
	key := strings.ToLower(strings.TrimSpace(resolution))
	if key == "" {
		key = strings.ToLower(cdp.JimengTargetResolution)
	}
	e.mu.Lock()
	cost := e.jimengLiveCost[key]
	e.mu.Unlock()
	return cost
}

// jimengCreditCostFor 返回即梦账号在当前输出分辨率下的单张积分消耗。
// 优先使用本轮在即梦网页实测到的目标分辨率实时单价，其次使用账号同步单价（2K 默认 1 分/张，4K 至少 2 分/张）。
func (e *Engine) jimengCreditCostFor(row store.Browser) int {
	targetRes := e.jimengTargetResolution()
	if live := e.jimengLiveCostFor(targetRes); live > 0 {
		return live
	}
	cost := max(row.CreditCost, cdp.JimengDefaultCreditCost)
	if strings.EqualFold(targetRes, "4k") && cost < 2 {
		cost = 2
	}
	return cost
}

// jimengResolvedUnitCost 结合本次实时探测的网页单价、本轮实测缓存与账号缓存，计算当前分辨率下的单张积分消耗。
func (e *Engine) jimengResolvedUnitCost(raw cdp.JimengStatus, row store.Browser, targetRes string) int {
	if raw.OK && cdp.JimengMatchesModelResolution(raw.Model, raw.Resolution, targetRes) {
		liveCost := raw.PageCreditCost
		if liveCost <= 0 && raw.CostModelMatched {
			liveCost = raw.CreditCost
		}
		if liveCost > 0 {
			e.setJimengLiveCost(targetRes, liveCost)
			return liveCost
		}
	}
	if cached := e.jimengLiveCostFor(targetRes); cached > 0 {
		return cached
	}
	cost := max(row.CreditCost, cdp.JimengDefaultCreditCost)
	if strings.EqualFold(targetRes, "4k") && cost < 2 {
		cost = 2
	}
	return cost
}

// jimengAffordableImages 返回即梦账号按已同步且未过期的剩余积分最多可生成的张数。
// 积分尚未同步或已过刷新时间时返回 known=false。
func (e *Engine) jimengAffordableImages(row store.Browser, now time.Time) (affordable int, known bool) {
	if !row.QuotaSynced() || row.QuotaExpired(now) || row.CreditsRemain < 0 {
		return 0, false
	}
	cost := e.jimengCreditCostFor(row)
	return row.CreditsRemain / max(cost, 1), true
}

// jimengBatchMaxFor 返回即梦账号当前单批最多可导入/生成的张数（满额 6 张；剩余积分不足时按可生图张数自动缩减）。
func (e *Engine) jimengBatchMaxFor(row store.Browser, ok bool, now time.Time) int {
	if !ok {
		return jimengWashBatchMax
	}
	affordable, known := e.jimengAffordableImages(row, now)
	if !known {
		return jimengWashBatchMax
	}
	if affordable <= 0 {
		return 0
	}
	return min(jimengWashBatchMax, affordable)
}

func (e *Engine) jimengDesiredCount(t *Task) int {
	switch {
	case e != nil && e.opts.Wash:
		n := 0
		if t != nil {
			n = max(len(t.batch), len(t.Images))
		}
		return min(max(n, 1), strategy.JimengMaxUploadImages)
	case e != nil && e.opts.Fission:
		return 1
	default:
		return strategy.JimengDefaultCount
	}
}

func (e *Engine) probeJimengStatusForTask(browserID int64, sess *cdp.Session) (cdp.JimengStatus, error) {
	if e != nil && e.jimengProbeFn != nil {
		return e.jimengProbeFn(browserID, sess)
	}
	if sess == nil {
		return cdp.JimengStatus{}, nil
	}
	raw, err := sess.ProbeJimengStatus()
	if err == nil && raw.OK && raw.HasCredits {
		return raw, nil
	}
	// 首次打开或刚重载页面时，顶栏积分徽章或接口可能晚到一拍，短暂重试一次后再决策。
	if sess.Ctx() != nil {
		select {
		case <-sess.Ctx().Done():
			return raw, err
		case <-time.After(300 * time.Millisecond):
		}
	}
	retryRaw, retryErr := sess.ProbeJimengStatus()
	if retryErr == nil && retryRaw.OK && (retryRaw.HasCredits || !raw.OK) {
		return retryRaw, nil
	}
	if err == nil && raw.OK {
		return raw, nil
	}
	return retryRaw, retryErr
}

func (e *Engine) persistJimengStatus(id int64, raw cdp.JimengStatus, now time.Time) (store.AccountQuota, bool) {
	quota, ok := cdp.NormalizeJimengStatus(raw, now)
	if !ok {
		return store.AccountQuota{}, false
	}
	if e != nil && e.store != nil && id > 0 {
		// 非默认分辨率（例如 4K）任务同步余额时，不覆盖账号已校准好的 2K 默认单价。
		if !raw.CostModelMatched {
			if prev, found := e.browserByID(id); found && prev.CreditCost > 0 {
				quota.CreditCost = prev.CreditCost
			}
		}
		if err := e.store.UpdateBrowserQuota(id, quota); err != nil {
			logging.Log.Debugf("保存即梦账号 %d 状态失败: %v", id, err)
		} else {
			e.notify()
		}
	}
	return quota, true
}

// analyzeJimengCreditsBeforeSend 在每次真正上传参考图或发送提示词调用即梦之前，
// 实时分析当前账号剩余积分与目标模型/分辨率单价，确保无论洗图、自由生图（文生图/图生图）还是裂变：
// 1. 余额不足 1 张时直接拦截并返回 ErrAccountQuota（触发换号或停跑），绝不上传/误发；
// 2. 洗图余额少于本批张数时返回 errJimengBatchShrink，按可生图张数缩减批次后立即重派；
// 3. 自由生图余额少于 6 张时自动按剩余可生图张数调整即梦网页生成数量（例如余 5 分则按 5 张发）；
// 4. 余额充足（或充值/刷新回满）时按原计划张数（最多 6 张）发送。
func (e *Engine) analyzeJimengCreditsBeforeSend(t *Task, strat strategy.Strategy, sess *cdp.Session) (jimengCreditPlan, error) {
	if e == nil || t == nil || t.submitted || t.Platform != "即梦" {
		return jimengCreditPlan{}, nil
	}
	now := time.Now()
	targetRes := e.jimengTargetResolution()
	desired := e.jimengDesiredCount(t)
	accountName := e.accountName(t.BrowserID)
	if accountName == "" {
		accountName = "即梦"
	}

	row, hasRow := e.browserByID(t.BrowserID)
	raw, probeErr := e.probeJimengStatusForTask(t.BrowserID, sess)

	var plan jimengCreditPlan
	plan.Desired = desired
	plan.Resolution = targetRes

	if probeErr == nil && raw.OK && raw.HasCredits {
		e.persistJimengStatus(t.BrowserID, raw, now)
		if updated, found := e.browserByID(t.BrowserID); found {
			row, hasRow = updated, true
		}
		cost := e.jimengResolvedUnitCost(raw, row, targetRes)
		remain := max(raw.CreditsRemain, 0)
		affordable := remain / max(cost, 1)
		plan.Known = true
		plan.Live = true
		plan.Remain = remain
		plan.Cost = cost
		plan.Affordable = affordable
		plan.Allowed = min(desired, affordable)
	} else {
		if probeErr != nil {
			logging.Log.Debugf("#%d 即梦「%s」发送前实时读取积分失败（%v），尝试按已缓存积分分析", t.Index, accountName, probeErr)
		} else {
			logging.Log.Debugf("#%d 即梦「%s」发送前实时探测未返回余额，尝试按已缓存积分分析", t.Index, accountName)
		}
		if hasRow {
			if affordable, known := e.jimengAffordableImages(row, now); known {
				cost := e.jimengCreditCostFor(row)
				plan.Known = true
				plan.Live = false
				plan.Remain = row.CreditsRemain
				plan.Cost = cost
				plan.Affordable = affordable
				plan.Allowed = min(desired, affordable)
			}
		}
	}

	if !plan.Known {
		logging.Log.Debugf("#%d 即梦「%s」未能读取剩余积分，沿用计划张数 %d 张发送", t.Index, accountName, desired)
		t.jimengCount = desired
		return plan, nil
	}

	if plan.Affordable <= 0 {
		logging.Log.Warning("#%d 即梦「%s」余额分析：剩余 %d 积分（%s · %d分/张），不足生成 1 张，跳过发送并切换账号",
			t.Index, accountName, plan.Remain, plan.Resolution, plan.Cost)
		return plan, strategy.ErrAccountQuota
	}

	if e.opts.Wash && desired > plan.Affordable {
		t.jimengShrinkCap = plan.Affordable
		t.jimengShrinkBrowser = t.BrowserID
		logging.Log.Info("#%d 即梦「%s」余额分析：剩余 %d 积分（%s · %d分/张，最多可发 %d 张），本批拟发 %d 张超出余额，自动缩减为 %d 张重派",
			t.Index, accountName, plan.Remain, plan.Resolution, plan.Cost, plan.Affordable, desired, plan.Allowed)
		return plan, errJimengBatchShrink
	}

	prevCount := desired
	if counter, ok := strat.(jimengCountStrategy); ok {
		prevCount = counter.ImageCount()
		counter.SetImageCount(plan.Allowed)
	}
	t.jimengCount = plan.Allowed

	if !e.opts.Wash && !e.opts.Fission && plan.Allowed < desired {
		logging.Log.Info("#%d 即梦「%s」余额分析：剩余 %d 积分（%s · %d分/张，最多可生 %d 张），不足 %d 张，本次自动按 %d 张发送",
			t.Index, accountName, plan.Remain, plan.Resolution, plan.Cost, plan.Affordable, desired, plan.Allowed)
	} else {
		logging.Log.Info("#%d 即梦「%s」余额分析：剩余 %d 积分（%s · %d分/张，最多可发 %d 张），足够本次发送 %d 张",
			t.Index, accountName, plan.Remain, plan.Resolution, plan.Cost, plan.Affordable, plan.Allowed)
	}

	// 若实时余额分析结果与当前工具栏选定张数不同（如余 5 分从 6 张降为 5 张，或积分刷新后从 5 张恢复为 6 张），
	// 立即同步网页端的生成数量控件，再继续上传参考图或发送提示词。
	if sess != nil && plan.Allowed != prevCount {
		if ensurer, ok := strat.(interface{ EnsureImageMode(*cdp.Session) error }); ok {
			if err := ensurer.EnsureImageMode(sess); err != nil {
				return plan, err
			}
		}
	}
	return plan, nil
}

// jimengAccountsStoppedReason 判断即梦所有非禁用/非人工接管账号是否均已积分耗尽或达今日上限，
// 避免最后一批把积分耗尽后调度器空等到次日零点。
func (e *Engine) jimengAccountsStoppedReason(rows []store.Browser, now time.Time, limit int) (logMsg, taskReason string, stop bool) {
	if len(rows) == 0 {
		return "", "", false
	}
	sawDailyCap := false
	sawCreditsExhausted := false
	for _, b := range rows {
		if b.Disabled || e.AccountManual(b.ID) {
			return "", "", false
		}
		if limit > 0 && b.UsedToday(now) >= limit {
			sawDailyCap = true
			continue
		}
		if b.QuotaExhausted(now) {
			sawCreditsExhausted = true
			continue
		}
		if b.Health == store.HealthExhausted && !b.Usable(now) {
			last := strings.TrimSpace(b.LastError)
			if last == "" || classifyFault(errors.New(last)) == faultQuota {
				sawCreditsExhausted = true
				continue
			}
		}
		return "", "", false
	}
	if sawCreditsExhausted && sawDailyCap {
		return "即梦所有账号积分已用尽或达今日上限，自动停止剩余任务", "账号积分已用尽或达今日上限，已停止", true
	}
	if sawCreditsExhausted {
		return "即梦所有账号积分已用尽，自动停止剩余任务", "账号积分已用尽，已停止", true
	}
	return "即梦所有账号今日已达上限，自动停止剩余任务", "今日已达上限，已停止", true
}
