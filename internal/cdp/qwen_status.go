package cdp

import (
	"context"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

const (
	// QwenFreeConcurrencyCap 是国内千问免费账号实测服务端单号突发并发上限（超过将触发风控或对话上限）。
	QwenFreeConcurrencyCap = 6
	// QwenVIPConcurrencyUnlimited 表示千问创作会员「不限并发」。
	QwenVIPConcurrencyUnlimited = -1
	// QwenFreeDefaultCredits 是国内千问免费账号单周期默认获赠创作积分。
	QwenFreeDefaultCredits = 40
	// QwenDefaultCreditCost 是千问 Qwen-Image 2.0 单张生图默认积分消耗。
	QwenDefaultCreditCost = 1
)

// QwenStatus 保存从国内千问网页实时探测到的积分、刷新时间、会员等级与去水印配置。
type QwenStatus struct {
	OK               bool   `json:"ok"`
	CreditsRemain    int    `json:"creditsRemain"`
	HasCredits       bool   `json:"hasCredits"`
	SignInCredits    int    `json:"signInCredits"`
	PurchaseCredits  int    `json:"purchaseCredits"`
	GiftCredits      int    `json:"giftCredits"`
	VIPCredits       int    `json:"vipCredits"`
	ExpiredIn7Day    int    `json:"expiredIn7Day"`
	CreditCost       int    `json:"creditCost"`
	Insufficient     bool   `json:"insufficient"`
	CostText         string `json:"costText"`
	GrantAmount      int    `json:"grantAmount"`
	GrantReceiveAt   int64  `json:"grantReceiveAt"`   // 毫秒时间戳
	GrantExpiredAt   int64  `json:"grantExpiredAt"`   // 毫秒时间戳
	GrantExpiredStr  string `json:"grantExpiredStr"`  // 如 "2026-11-27 23:59:59"
	PrevGrantAt      int64  `json:"prevGrantAt"`      // 毫秒时间戳（用于推断 7 日周期）
	PrevExpiredAt    int64  `json:"prevExpiredAt"`    // 毫秒时间戳（上一笔积分到期时间）
	MemberType       string `json:"memberType"`       // "free" | 会员类型
	MemberName       string `json:"memberName"`       // "免费套餐" | "标准会员" 等
	SubscribeStatus  int    `json:"subscribeStatus"`
	MemberExpireAt   int64  `json:"memberExpireAt"`   // 毫秒时间戳
	VIPActive        bool   `json:"vipActive"`
	VIPName          string `json:"vipName"`
	VIPMonthlyCredit int    `json:"vipMonthlyCredit"`
	WatermarkFree    bool   `json:"watermarkFree"`
}

// qwenStatusProbeJS 在已登录的国内千问（qianwen.com / tongyi.com）页面上下文内并发读取
// 积分余额、积分明细（刷新/到期时间）、会员等级、单张积分消耗与去水印配置。
// 在非国内千问域名上立即返回 {ok:false}，不发任何外部请求。
const qwenStatusProbeJS = `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)(qianwen|tongyi)\.com$/.test(host)) {
		return { ok: false };
	}
	const q = 'biz_id=ai_qwen&chat_client=h5&device=pc&fr=pc&pr=qwen';
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
	const fetchDetailList = async () => {
		const bodies = [
			{},
			{ pageNum: 1, pageSize: 30 },
			{ page: 1, size: 30 },
		];
		for (const body of bodies) {
			const resp = await postJSON('https://ai-studio-create.qianwen.com/api/web/credit/detail?' + q, body);
			const list = resp && resp.data && Array.isArray(resp.data.list) ? resp.data.list : null;
			if (list && list.length > 0) return list;
		}
		return [];
	};
	const [infoRes, totalRes, detailList, levelRes, vipRes, calcRes, wmRes] = await Promise.all([
		postJSON('https://ai-studio-create.qianwen.com/api/web/credit/info?' + q, {}),
		postJSON('https://ai-studio-create.qianwen.com/api/web/credit/total?' + q, {}),
		fetchDetailList(),
		postJSON('https://member.qianwen.com/api/member/level/query?' + q, {}),
		postJSON('https://ai-studio-create.qianwen.com/api/web/vip/using?' + q, {}),
		postJSON('https://zd.qianwen.com/api/web/task/credit/calculate?' + q, {
			rootModel: 'qwen2',
			model: 'qwen2',
			genMode: 'gen_image',
			attachments: [],
		}),
		postJSON('https://ai-studio-create.qianwen.com/api/web/v1/user/watermark/config/get?' + q, {}),
	]);
	const out = {
		ok: false,
		creditsRemain: -1,
		hasCredits: false,
		signInCredits: 0,
		purchaseCredits: 0,
		giftCredits: 0,
		vipCredits: 0,
		expiredIn7Day: 0,
		creditCost: 0,
		insufficient: false,
		costText: '',
		grantAmount: 0,
		grantReceiveAt: 0,
		grantExpiredAt: 0,
		grantExpiredStr: '',
		prevGrantAt: 0,
		prevExpiredAt: 0,
		memberType: '',
		memberName: '',
		subscribeStatus: 0,
		memberExpireAt: 0,
		vipActive: false,
		vipName: '',
		vipMonthlyCredit: 0,
		watermarkFree: false,
	};
	const infoData = infoRes && infoRes.success && infoRes.data ? infoRes.data : null;
	const totalData = totalRes && totalRes.success && totalRes.data ? totalRes.data : null;
	if (infoData && typeof infoData.totalAmount === 'number') {
		out.ok = true;
		out.hasCredits = true;
		out.creditsRemain = Math.max(0, Math.round(infoData.totalAmount));
		out.signInCredits = Math.max(0, Math.round(Number(infoData.signIn || 0)));
		out.purchaseCredits = Math.max(0, Math.round(Number(infoData.purchase || 0)));
		out.giftCredits = Math.max(0, Math.round(Number(infoData.gift || 0)));
		out.vipCredits = Math.max(0, Math.round(Number(infoData.vip || 0)));
		out.expiredIn7Day = Math.max(0, Math.round(Number(infoData.expiredIn7Day || 0)));
	} else if (totalData && typeof totalData.totalAmount === 'number') {
		out.ok = true;
		out.hasCredits = true;
		out.creditsRemain = Math.max(0, Math.round(totalData.totalAmount));
	}
	if (Array.isArray(detailList) && detailList.length > 0) {
		const grants = [];
		const expires = [];
		for (const item of detailList) {
			if (!item || typeof item !== 'object') continue;
			const amt = Number(item.amount || 0);
			const src = String(item.source || '');
			const title = String(item.title || '');
			const recv = Number(item.receiveTime || 0);
			const exp = Number(item.expiredTime || 0);
			if (amt > 0 && src !== 'failReturn' && !/返还/.test(title)) {
				grants.push({
					amount: Math.round(amt),
					receiveTime: recv,
					expiredTime: exp,
					expiredTimeStr: String(item.expiredTimeStr || ''),
				});
			} else if (amt < 0 && (src === 'job' || /到期/.test(title)) && recv > 0) {
				expires.push(recv);
			}
		}
		if (grants.length > 0) {
			const latest = grants[0];
			out.grantAmount = latest.amount;
			out.grantReceiveAt = latest.receiveTime;
			out.grantExpiredAt = latest.expiredTime;
			out.grantExpiredStr = latest.expiredTimeStr;
			if (!out.grantExpiredAt && grants.length > 1 && expires.length > 0) {
				const prevGrant = grants[1].receiveTime;
				for (const expTs of expires) {
					if (expTs > prevGrant && (out.prevExpiredAt === 0 || expTs < out.prevExpiredAt)) {
						out.prevGrantAt = prevGrant;
						out.prevExpiredAt = expTs;
					}
				}
			}
		}
	}
	const levelData = levelRes && levelRes.success && levelRes.data ? levelRes.data : null;
	if (levelData) {
		out.ok = true;
		out.memberType = String(levelData.memberType || '').trim();
		out.memberName = String(levelData.memberName || '').trim();
		out.subscribeStatus = Number(levelData.subscribeStatus || 0);
		out.memberExpireAt = Number(levelData.expireTime || 0);
	}
	const vipData = vipRes && vipRes.success && vipRes.data && typeof vipRes.data === 'object' ? vipRes.data : null;
	if (vipData) {
		out.ok = true;
		out.vipActive = true;
		out.vipName = String(vipData.vipName || vipData.memberName || vipData.name || '').trim();
		out.vipMonthlyCredit = Math.max(0, Math.round(Number(vipData.monthlyCredit || vipData.totalCredit || vipData.creditAmount || 0)));
		if (!out.memberExpireAt && Number(vipData.expireTime || 0) > 0) {
			out.memberExpireAt = Number(vipData.expireTime);
		}
	}
	const calcData = calcRes && calcRes.success && calcRes.data ? calcRes.data : null;
	if (calcData) {
		out.creditCost = Math.max(0, Math.round(Number(calcData.creditCost || 0)));
		out.insufficient = Boolean(calcData.insufficient);
		out.costText = String(calcData.costText || '').trim();
	}
	const wmData = wmRes && wmRes.success && wmRes.data ? wmRes.data : null;
	if (wmData) {
		out.watermarkFree = Boolean(wmData.aiWatermarkRemovalEnabled || wmData.brandWatermarkRemovalEnabled);
	}
	return out;
})()`

// NormalizeQwenStatus 将网页探测到的原始千问状态归一为持久化用的 AccountQuota。
func NormalizeQwenStatus(raw QwenStatus, now time.Time) (store.AccountQuota, bool) {
	if !raw.OK {
		return store.AccountQuota{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}

	isVIP := raw.VIPActive || raw.SubscribeStatus > 0 || (raw.MemberType != "" && !strings.EqualFold(raw.MemberType, "free"))
	tier := "free"
	memberName := strings.TrimSpace(raw.MemberName)
	if isVIP {
		tier = "vip"
		if strings.TrimSpace(raw.VIPName) != "" {
			memberName = strings.TrimSpace(raw.VIPName)
		}
		if memberName == "" || memberName == "免费套餐" {
			memberName = "创作会员"
		}
	} else if memberName == "" {
		memberName = "免费套餐"
	}

	remain := raw.CreditsRemain
	if !raw.HasCredits {
		remain = -1
	} else if remain < 0 {
		remain = 0
	}

	total := raw.GrantAmount
	if isVIP && raw.VIPMonthlyCredit > total {
		total = raw.VIPMonthlyCredit
	}
	if total <= 0 {
		if isVIP {
			total = max(remain, 0)
		} else if remain >= 0 {
			total = max(QwenFreeDefaultCredits, remain)
		}
	} else if remain > total {
		total = remain
	}

	cost := raw.CreditCost
	if cost <= 0 {
		cost = QwenDefaultCreditCost
	}

	resetAt, cycle := resolveQwenReset(raw, isVIP, now)

	conc := QwenFreeConcurrencyCap
	if isVIP {
		conc = QwenVIPConcurrencyUnlimited
	}

	return store.AccountQuota{
		CreditsRemain:      remain,
		CreditsTotal:       total,
		CreditCost:         cost,
		CreditsResetAt:     resetAt,
		CreditsCycle:       cycle,
		MemberTier:         tier,
		MemberName:         memberName,
		AccountConcurrency: conc,
		WatermarkFree:      raw.WatermarkFree || isVIP,
		SyncedAt:           now.Unix(),
	}, true
}

func resolveQwenReset(raw QwenStatus, isVIP bool, now time.Time) (int64, string) {
	resetSec := normalizeMillisOrSeconds(raw.GrantExpiredAt)
	if resetSec <= 0 && strings.TrimSpace(raw.GrantExpiredStr) != "" {
		if parsed, ok := parseQwenDateTime(raw.GrantExpiredStr, now.Location()); ok {
			resetSec = parsed.Unix()
		}
	}
	recvSec := normalizeMillisOrSeconds(raw.GrantReceiveAt)

	// 7 日周期账号在明细里可能不带 expiredTime，利用历史「登录领积分 -> 积分到期」差值推算。
	if resetSec <= 0 && recvSec > 0 {
		prevGrant := normalizeMillisOrSeconds(raw.PrevGrantAt)
		prevExp := normalizeMillisOrSeconds(raw.PrevExpiredAt)
		if prevGrant > 0 && prevExp > prevGrant {
			span := prevExp - prevGrant
			if span >= 5*86400 && span <= 9*86400 {
				return recvSec + 7*86400, "7d"
			}
			if span > 0 && span <= 36*3600 {
				t := time.Unix(recvSec, 0).In(now.Location())
				y, m, d := t.Date()
				return time.Date(y, m, d, 23, 59, 59, 0, now.Location()).Unix(), "daily"
			}
		}
	}

	if resetSec > 0 {
		cycle := ""
		if recvSec > 0 && resetSec > recvSec {
			hours := float64(resetSec-recvSec) / 3600.0
			switch {
			case hours <= 36:
				cycle = "daily"
			case hours <= 200:
				cycle = "7d"
			default:
				cycle = "monthly"
			}
		} else if isVIP {
			cycle = "monthly"
		}
		return resetSec, cycle
	}

	if isVIP {
		if exp := normalizeMillisOrSeconds(raw.MemberExpireAt); exp > 0 {
			return exp, "monthly"
		}
		return 0, "monthly"
	}

	// 兜底：有今日积分但明细未返回到期时间时，若存在 7 日内过期积分则标注 7d，否则留空由 UI 提示。
	if raw.ExpiredIn7Day > 0 {
		return 0, "7d"
	}
	return 0, ""
}

func normalizeMillisOrSeconds(ts int64) int64 {
	if ts <= 0 {
		return 0
	}
	if ts > 1_000_000_000_000 {
		return ts / 1000
	}
	return ts
}

func parseQwenDateTime(raw string, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.Local
	}
	s := strings.TrimSpace(raw)
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006/01/02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04",
		time.RFC3339,
	} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func pickQwenTarget(list []sliderTarget) (string, bool) {
	for _, item := range list {
		if item.Type != "page" || item.ID == "" {
			continue
		}
		u := strings.ToLower(item.URL)
		if strings.Contains(u, "qianwen.com") || strings.Contains(u, "tongyi.com") {
			return item.ID, true
		}
	}
	return "", false
}

// ProbeQwenStatus 附着到已经打开的国内千问页面，读取实时积分、刷新时间与会员信息。
// 只断开调试连接，不关闭标签页。
func ProbeQwenStatus(ctx context.Context, endpoint string) (QwenStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return QwenStatus{}, err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return QwenStatus{}, err
	}
	id, ok := pickQwenTarget(targets)
	if !ok {
		return QwenStatus{}, nil
	}
	if RegionBlockedURL(targetURL(targets, id)) {
		return QwenStatus{}, nil
	}
	var result QwenStatus
	if err := conn.evalJSON(ctx, id, qwenStatusProbeJS, &result, true, true); err != nil {
		return QwenStatus{}, err
	}
	return result, nil
}

// ProbeQwenStatus 在已有任务会话页面中读取实时千问积分与会员状态，不新开标签页。
func (s *Session) ProbeQwenStatus() (QwenStatus, error) {
	if s == nil {
		return QwenStatus{}, nil
	}
	out, err := s.EvaluateAsync(qwenStatusProbeJS)
	if err != nil {
		return QwenStatus{}, err
	}
	var result QwenStatus
	if err := ParseJSValue(out, &result); err != nil {
		return QwenStatus{}, err
	}
	return result, nil
}
