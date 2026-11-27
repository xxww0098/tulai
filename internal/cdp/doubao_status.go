package cdp

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

// DoubaoStatus 保存从豆包网页（doubao.com）实时探测到的订阅套餐、付费额度余额、
// 窗口已用百分比、限额/频控状态与额度重置卡数量。
type DoubaoStatus struct {
	OK                    bool   `json:"ok"`
	HasActiveSubscription bool   `json:"hasActiveSubscription"`
	IsFreeUser            bool   `json:"isFreeUser"`
	SKUKey                string `json:"skuKey"`
	ProductName           string `json:"productName"`
	ShortName             string `json:"shortName"`
	MembershipDisplayName string `json:"membershipDisplayName"`
	PeriodType            int    `json:"periodType"`
	EndTime               int64  `json:"endTime"`        // 秒或毫秒时间戳
	NextDeductTime        int64  `json:"nextDeductTime"` // 秒或毫秒时间戳
	QuotaRemainingAmount  string `json:"quotaRemainingAmount"`
	QuotaTotalAmount      int    `json:"quotaTotalAmount"`
	WindowResetAt         int64  `json:"windowResetAt"` // 秒或毫秒时间戳
	HasUsagePercent       bool   `json:"hasUsagePercent"`
	UsedPercent           int    `json:"usedPercent"` // 0..100
	IsLimited             bool   `json:"isLimited"`
	RateLimited           bool   `json:"rateLimited"`
	RateLimitTime         int64  `json:"rateLimitTime"` // 秒或毫秒时间戳
	RateLimitTips         string `json:"rateLimitTips"`
	ResetCardCount        int    `json:"resetCardCount"`
}

// doubaoStatusProbeJS 在已登录的豆包（doubao.com）页面上下文内并发读取：
// 1. 订阅与额度摘要 (/alice/commerce/sale/subscription/quota/summary/)
// 2. 限额状态与重置卡列表 (/alice/commerce/marketing/card/list/)
// 3. 额度重置卡余额 (/alice/commerce/marketing/card/balance/)
// 4. 发送频控检查 (/im/message/send_rate_limit)
// 在非豆包域名上立即返回 {ok:false}，不发任何外部请求。
const doubaoStatusProbeJS = `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)doubao\.com$/.test(host)) {
		return { ok: false };
	}
	const q = 'version_code=20800&language=zh&device_platform=web&doubao_device_platform=web&aid=497858&region=CN&sys_region=CN&samantha_web=1&web_platform=browser&use-olympus-account=1';
	const postJSON = async (url, payload) => {
		const ctrl = typeof AbortController === 'function' ? new AbortController() : null;
		const timer = ctrl ? setTimeout(() => { try { ctrl.abort(); } catch (e) {} }, 2500) : null;
		try {
			const res = await fetch(url, {
				method: 'POST',
				credentials: 'include',
				headers: { 'content-type': 'application/json' },
				body: JSON.stringify(payload || {}),
				signal: ctrl ? ctrl.signal : undefined,
			});
			if (!res || !res.ok) return null;
			return await res.json();
		} catch (e) {
			return null;
		} finally {
			if (timer) clearTimeout(timer);
		}
	};

	const [summaryRes, cardListRes, cardBalRes, rateRes] = await Promise.all([
		postJSON('https://www.doubao.com/alice/commerce/sale/subscription/quota/summary/?' + q, { product_line: 'membership' }),
		postJSON('https://www.doubao.com/alice/commerce/marketing/card/list/?' + q, { status_in: [1, 2, 3, 4], with_limit_status: true }),
		postJSON('https://www.doubao.com/alice/commerce/marketing/card/balance/?' + q, {}),
		postJSON('https://www.doubao.com/im/message/send_rate_limit', {
			cmd: 2260,
			uplink_body: { check_message_send_rate_limit_uplink_body: {} },
			channel: 2,
			version: '1',
		}),
	]);

	const out = {
		ok: false,
		hasActiveSubscription: false,
		isFreeUser: true,
		skuKey: '',
		productName: '',
		shortName: '',
		membershipDisplayName: '',
		periodType: 0,
		endTime: 0,
		nextDeductTime: 0,
		quotaRemainingAmount: '',
		quotaTotalAmount: 0,
		windowResetAt: 0,
		hasUsagePercent: false,
		usedPercent: -1,
		isLimited: false,
		rateLimited: false,
		rateLimitTime: 0,
		rateLimitTips: '',
		resetCardCount: 0,
	};

	const sumData = summaryRes && summaryRes.code === 0 && summaryRes.data ? summaryRes.data : null;
	if (sumData) {
		out.ok = true;
		const sub = sumData.current_subscription || {};
		const disp = sub.display || {};
		const mem = sumData.member_info || {};
		out.hasActiveSubscription = Boolean(mem.hasActiveSubscription || mem.has_active_subscription || Number(sub.status || 0) === 1);
		out.isFreeUser = !out.hasActiveSubscription;
		out.skuKey = String(sub.sku_key || '').trim();
		out.productName = String(disp.product_name || '').trim();
		out.shortName = String(disp.short_name || '').trim();
		out.periodType = Number(sub.period_type || 0);
		out.endTime = Number(sub.end_time || 0);
		out.nextDeductTime = Number(sub.next_deduct_time || 0);
		if (sumData.quota_remaining_amount !== undefined && sumData.quota_remaining_amount !== null) {
			out.quotaRemainingAmount = String(sumData.quota_remaining_amount).trim();
		}
		const wls = sumData.window_limit_section || {};
		if (wls.usage_exhausted) out.isLimited = true;
		if (Array.isArray(wls.window_limit_groups)) {
			for (const grp of wls.window_limit_groups) {
				if (!grp || typeof grp !== 'object') continue;
				const pct = Number(grp.used_percent ?? grp.percent ?? -1);
				if (Number.isFinite(pct) && pct >= 0) {
					out.hasUsagePercent = true;
					out.usedPercent = Math.max(out.usedPercent, Math.min(100, Math.round(pct)));
				}
				const reset = Number(grp.reset_time || grp.next_reset_time || grp.expire_time || 0);
				if (reset > 0 && (out.windowResetAt === 0 || reset < out.windowResetAt)) {
					out.windowResetAt = reset;
				}
			}
		}
	}

	const listData = cardListRes && cardListRes.code === 0 && cardListRes.data ? cardListRes.data : null;
	if (listData && listData.limit_status && typeof listData.limit_status === 'object') {
		out.ok = true;
		const ls = listData.limit_status;
		if (typeof ls.is_free_user === 'boolean') {
			out.isFreeUser = ls.is_free_user;
			if (!ls.is_free_user) out.hasActiveSubscription = true;
		}
		if (ls.is_limited) out.isLimited = true;
		const pct = Number(ls.used_percent);
		if (Number.isFinite(pct) && pct >= 0) {
			out.hasUsagePercent = true;
			out.usedPercent = Math.max(out.usedPercent, Math.min(100, Math.round(pct)));
		}
	}

	const balData = cardBalRes && cardBalRes.code === 0 && cardBalRes.data ? cardBalRes.data : null;
	if (balData) {
		out.ok = true;
		out.resetCardCount = Math.max(0, Math.round(Number(balData.available_count || 0)));
	}

	const rlBody = rateRes && rateRes.downlink_body && rateRes.downlink_body.check_message_send_rate_limit_downlink_body;
	if (rlBody && typeof rlBody === 'object') {
		out.rateLimited = Boolean(rlBody.is_limit);
		out.rateLimitTime = Number(rlBody.limit_time || 0);
		out.rateLimitTips = String(rlBody.limit_tips || '').trim();
	}

	return out;
})()`

// NormalizeDoubaoStatus 将豆包网页探测结果归一为持久化用的 AccountQuota。
func NormalizeDoubaoStatus(raw DoubaoStatus, now time.Time) (store.AccountQuota, bool) {
	if !raw.OK {
		return store.AccountQuota{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}

	remainParsed, hasRemainNum := parseDoubaoQuotaInt(raw.QuotaRemainingAmount)
	isVIP := raw.HasActiveSubscription || (!raw.IsFreeUser && raw.SKUKey != "") || (hasRemainNum && remainParsed > 0)

	tier := "free"
	memberName := strings.TrimSpace(raw.ShortName)
	if isVIP {
		tier = "vip"
		if strings.TrimSpace(raw.MembershipDisplayName) != "" {
			memberName = strings.TrimSpace(raw.MembershipDisplayName)
		} else if memberName == "" || memberName == "免费体验" {
			memberName = doubaoSKUDisplayName(raw.SKUKey, raw.ProductName)
		}
	} else if memberName == "" {
		memberName = "免费体验"
	}

	// 免费豆包账号没有固定每日剩余张数（quota_remaining_amount="0" 为订阅余额），保持 -1 以区分付费订阅。
	remain := -1
	total := 0
	if isVIP {
		if hasRemainNum {
			remain = max(remainParsed, 0)
		}
		if raw.QuotaTotalAmount > 0 {
			total = raw.QuotaTotalAmount
		} else if skuTotal := doubaoSKUDefaultQuota(raw.SKUKey); skuTotal > 0 {
			total = max(skuTotal, remain)
		}
	}

	usagePct := -1
	if raw.HasUsagePercent && raw.UsedPercent >= 0 {
		usagePct = min(100, max(0, raw.UsedPercent))
	}

	limited := raw.IsLimited || raw.RateLimited
	tips := strings.TrimSpace(raw.RateLimitTips)
	if tips == "" && raw.IsLimited {
		tips = "当前账号已触发豆包额度限制"
	}

	resetAt, cycle := resolveDoubaoReset(raw, isVIP, limited, now)

	return store.AccountQuota{
		CreditsRemain:      remain,
		CreditsTotal:       total,
		CreditCost:         0,
		CreditsResetAt:     resetAt,
		CreditsCycle:       cycle,
		MemberTier:         tier,
		MemberName:         memberName,
		AccountConcurrency: 0,
		WatermarkFree:      false,
		UsagePercent:       usagePct,
		PlatformLimited:    limited,
		LimitTips:          tips,
		ResetCardCount:     max(raw.ResetCardCount, 0),
		SyncedAt:           now.Unix(),
	}, true
}

func parseDoubaoQuotaInt(raw string) (int, bool) {
	s := strings.TrimSpace(strings.ReplaceAll(raw, ",", ""))
	if s == "" {
		return 0, false
	}
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

func doubaoSKUDisplayName(sku, fallback string) string {
	lower := strings.ToLower(strings.TrimSpace(sku))
	switch {
	case strings.Contains(lower, "pro") || strings.Contains(lower, "adv"):
		return "高级订阅"
	case strings.Contains(lower, "plus") || strings.Contains(lower, "enh"):
		return "加强订阅"
	case strings.Contains(lower, "std"):
		return "标准订阅"
	}
	if fb := strings.TrimSpace(fallback); fb != "" && fb != "个人订阅" {
		return fb
	}
	return "豆包订阅"
}

func doubaoSKUDefaultQuota(sku string) int {
	lower := strings.ToLower(strings.TrimSpace(sku))
	if strings.Contains(lower, "doubao_personal_std") {
		return 9000
	}
	return 0
}

func resolveDoubaoReset(raw DoubaoStatus, isVIP, limited bool, now time.Time) (int64, string) {
	if raw.RateLimited && raw.RateLimitTime > 0 {
		ts := normalizeMillisOrSeconds(raw.RateLimitTime)
		if ts > now.Unix() {
			return ts, ""
		}
		if raw.RateLimitTime > 0 && raw.RateLimitTime < 86400*7 {
			return now.Unix() + raw.RateLimitTime, ""
		}
	}
	if raw.WindowResetAt > 0 {
		if ts := normalizeMillisOrSeconds(raw.WindowResetAt); ts > 0 {
			return ts, ""
		}
	}
	if isVIP {
		if ts := normalizeMillisOrSeconds(raw.NextDeductTime); ts > 0 {
			return ts, "monthly"
		}
		if ts := normalizeMillisOrSeconds(raw.EndTime); ts > 0 {
			return ts, "monthly"
		}
		return 0, "monthly"
	}
	_ = limited
	return 0, ""
}

func pickDoubaoTarget(list []sliderTarget) (string, bool) {
	for _, item := range list {
		if item.Type != "page" || item.ID == "" {
			continue
		}
		if strings.Contains(strings.ToLower(item.URL), "doubao.com") {
			return item.ID, true
		}
	}
	return "", false
}

// ProbeDoubaoStatus 附着到已经打开的豆包页面，读取实时订阅、额度百分比、限流与重置卡状态。
// 只断开调试连接，不关闭标签页。
func ProbeDoubaoStatus(ctx context.Context, endpoint string) (DoubaoStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return DoubaoStatus{}, err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return DoubaoStatus{}, err
	}
	id, ok := pickDoubaoTarget(targets)
	if !ok {
		return DoubaoStatus{}, nil
	}
	if RegionBlockedURL(targetURL(targets, id)) {
		return DoubaoStatus{}, nil
	}
	var result DoubaoStatus
	if err := conn.evalJSON(ctx, id, doubaoStatusProbeJS, &result, true, true); err != nil {
		return DoubaoStatus{}, err
	}
	return result, nil
}

// ProbeDoubaoStatus 在已有任务会话页面中读取实时豆包额度与限流状态，不新开标签页。
func (s *Session) ProbeDoubaoStatus() (DoubaoStatus, error) {
	if s == nil {
		return DoubaoStatus{}, nil
	}
	out, err := s.EvaluateAsync(doubaoStatusProbeJS)
	if err != nil {
		return DoubaoStatus{}, err
	}
	var result DoubaoStatus
	if err := ParseJSValue(out, &result); err != nil {
		return DoubaoStatus{}, err
	}
	return result, nil
}
