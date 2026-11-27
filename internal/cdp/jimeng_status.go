package cdp

import (
	"context"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

const (
	// JimengFreeDefaultCredits 是即梦免费账号默认每日获赠积分参考基准（未下发明确总量时用于进度条显示）。
	JimengFreeDefaultCredits = 60
	// JimengDefaultCreditCost 是即梦 Seedream 4.7（2K）单张生图默认积分消耗（网页显示 1/张）。
	JimengDefaultCreditCost = 1
	// JimengTargetMode 是即梦默认生图创作类型，用于校对页面单张积分显示。
	JimengTargetMode = "图片生成"
	// JimengTargetModel 是即梦默认生图模型，仅当页面选中该模型及目标分辨率时采信页面「X/张」单价。
	JimengTargetModel = "Seedream 4.7"
	// JimengTargetResolution 是即梦默认生图分辨率。
	JimengTargetResolution = "2k"
)

// JimengStatus 保存从即梦（jimeng.jianying.com）网页实时探测到的积分、刷新/到期时间、会员与用户名状态。
type JimengStatus struct {
	OK               bool   `json:"ok"`
	Source           string `json:"source"`
	Username         string `json:"username"`
	CreditsRemain    int    `json:"creditsRemain"`
	HasCredits       bool   `json:"hasCredits"`
	CreditsTotal     int    `json:"creditsTotal"`
	GiftCredits      int    `json:"giftCredits"`
	VIPCredits       int    `json:"vipCredits"`
	PurchaseCredits  int    `json:"purchaseCredits"`
	FreeCredits      int    `json:"freeCredits"`
	CreditCost       int    `json:"creditCost"`
	PageCreditCost   int    `json:"pageCreditCost"` // 页面当前选中 Seedream 4.7（任意分辨率，如 2K/4K）实时显示的单张积分消耗
	Model            string `json:"model"`
	Resolution       string `json:"resolution"`
	CostModelMatched bool   `json:"costModelMatched"`
	CreditsResetAt   int64  `json:"creditsResetAt"` // 毫秒或秒时间戳
	CreditsCycle     string `json:"creditsCycle"`   // "daily" | "monthly" | "yearly" | ""
	MemberType       string `json:"memberType"`     // "free" | "vip" | ""
	MemberName       string `json:"memberName"`     // 如 "免费版"、"基础会员"、"标准会员"
	VIPActive        bool   `json:"vipActive"`
	VIPLevel         int    `json:"vipLevel"`
	VIPExpireAt      int64  `json:"vipExpireAt"` // 毫秒或秒时间戳
	VIPMonthlyCredit int    `json:"vipMonthlyCredit"`
	WatermarkFree    bool   `json:"watermarkFree"`
}

// jimengStatusProbeJS 在已登录的即梦（jimeng.jianying.com）页面上下文内同源读取：
// 1) POST /commerce/v1/benefits/user_credit 读取实时积分余额（gift/vip/purchase/free 积分与到期时间）；
// 2) POST /commerce/v3/subscription/get_user_lite_info 与 /commerce/v1/subscription/user_info 读取会员等级；
// 3) GET /passport/account/info/v2/?aid=513695&account_sdk_source=web&sdk_version=2.2.6 读取登录用户名；
// 4) DOM 兜底读取顶栏积分（[data-main-frame-2026-credit-display-anchor]）、目标模型（Seedream 4.7 · 2K）单张消耗与头像用户名。
// 在非 jimeng.jianying.com 域名上立即返回 {ok:false}，不发任何外部请求。
const jimengStatusProbeJS = `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)jimeng\.jianying\.com$/.test(host)) {
		return { ok: false };
	}
	const out = {
		ok: false,
		source: '',
		username: '',
		creditsRemain: -1,
		hasCredits: false,
		creditsTotal: 0,
		giftCredits: 0,
		vipCredits: 0,
		purchaseCredits: 0,
		freeCredits: 0,
		creditCost: 0,
		pageCreditCost: 0,
		model: '',
		resolution: '',
		costModelMatched: false,
		creditsResetAt: 0,
		creditsCycle: '',
		memberType: '',
		memberName: '',
		vipActive: false,
		vipLevel: 0,
		vipExpireAt: 0,
		vipMonthlyCredit: 0,
		watermarkFree: false,
	};

	const fetchJSON = async (url, init, timeoutMs) => {
		const ctrl = typeof AbortController === 'function' ? new AbortController() : null;
		const timer = ctrl ? setTimeout(() => { try { ctrl.abort(); } catch (e) {} }, timeoutMs || 3000) : null;
		try {
			const res = await fetch(url, {
				credentials: 'include',
				...(init || {}),
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
	const postJSON = (url, payload) => fetchJSON(url, {
		method: 'POST',
		headers: { 'content-type': 'application/json' },
		body: JSON.stringify(payload || {}),
	});
	const getJSON = (url) => fetchJSON(url, { method: 'GET' });

	const numField = (obj, keys) => {
		if (!obj || typeof obj !== 'object') return null;
		for (const k of keys) {
			const v = obj[k];
			if (v === undefined || v === null || v === '') continue;
			const n = Number(v);
			if (Number.isFinite(n) && n >= 0) return Math.round(n);
		}
		return null;
	};
	const strField = (obj, keys) => {
		if (!obj || typeof obj !== 'object') return '';
		for (const k of keys) {
			const v = obj[k];
			if (typeof v === 'string' && v.trim()) return v.trim();
		}
		return '';
	};
	const isApiOk = (res) => {
		if (!res || typeof res !== 'object') return false;
		if (res.ret !== undefined && res.ret !== null && String(res.ret) !== '0' && String(res.ret).toLowerCase() !== 'success') return false;
		if (res.code !== undefined && res.code !== null && Number(res.code) !== 0) return false;
		if (res.error_code !== undefined && res.error_code !== null && Number(res.error_code) !== 0) return false;
		if (typeof res.message === 'string' && res.message.toLowerCase() === 'error') return false;
		return true;
	};

	const [creditRes, subLiteRes, subInfoRes, passportV2Res] = await Promise.all([
		postJSON('/commerce/v1/benefits/user_credit', {}),
		postJSON('/commerce/v3/subscription/get_user_lite_info', {}),
		postJSON('/commerce/v1/subscription/user_info', {}),
		getJSON('/passport/account/info/v2/?aid=513695&account_sdk_source=web&sdk_version=2.2.6'),
	]);

	let passportRes = passportV2Res;
	const extractPassportUser = (res) => {
		if (!res || typeof res !== 'object') return '';
		const d = res.data && typeof res.data === 'object' ? res.data : null;
		if (!d || d.is_visitor_account === true) return '';
		const uid = d.user_id || d.uid || d.user_id_str || d.sec_user_id;
		const name = strField(d, ['screen_name', 'name', 'nick_name', 'nickname', 'user_name', 'username']);
		if (name && (uid || String(res.message || '').toLowerCase() === 'success' || Number(d.error_code || 0) === 0)) {
			return name;
		}
		return '';
	};
	out.username = extractPassportUser(passportRes);
	if (!out.username) {
		const [passportWebRes, mwebUserRes] = await Promise.all([
			getJSON('/passport/web/account/info/?aid=513695&account_sdk_source=web'),
			postJSON('/mweb/v1/get_user_info', {}),
		]);
		out.username = extractPassportUser(passportWebRes);
		if (!out.username && mwebUserRes && isApiOk(mwebUserRes) && mwebUserRes.data && typeof mwebUserRes.data === 'object') {
			const md = mwebUserRes.data;
			const uo = (md.user && typeof md.user === 'object' ? md.user : null) ||
				(md.user_info && typeof md.user_info === 'object' ? md.user_info : null) || md;
			if (uo && uo.is_visitor_account !== true) {
				out.username = strField(uo, ['screen_name', 'name', 'nick_name', 'nickname', 'user_name', 'username']);
			}
		}
	}
	if (out.username) {
		out.ok = true;
		out.source = out.source || 'passport';
	}

	if (creditRes && isApiOk(creditRes) && creditRes.data && typeof creditRes.data === 'object') {
		const root = creditRes.data;
		const c = (root.credit && typeof root.credit === 'object') ? root.credit : root;
		const gift = numField(c, ['gift_credit', 'gift_credits']);
		const purchase = numField(c, ['purchase_credit', 'purchase_credits']);
		const vip = numField(c, ['vip_credit', 'vip_credits']);
		const free = numField(c, ['free_credit', 'free_credits']);
		const explicitTotal = numField(c, ['total_remain_credits', 'total_credit', 'total_credits', 'remain_credit', 'remaining_credit', 'credits', 'credit']);

		if (gift !== null) out.giftCredits = gift;
		if (purchase !== null) out.purchaseCredits = purchase;
		if (vip !== null) out.vipCredits = vip;
		if (free !== null) out.freeCredits = free;

		const hasBucket = gift !== null || purchase !== null || vip !== null || free !== null;
		const bucketSum = (gift || 0) + (purchase || 0) + (vip || 0) + (free || 0);
		if (explicitTotal !== null) {
			out.hasCredits = true;
			out.creditsRemain = explicitTotal;
		} else if (hasBucket) {
			out.hasCredits = true;
			out.creditsRemain = bucketSum;
		}

		const nowSec = Math.floor(Date.now() / 1000);
		const toFutureSec = (ts) => {
			if (!ts || !Number.isFinite(ts) || ts <= 0) return 0;
			const sec = ts > 1e12 ? Math.floor(ts / 1000) : Math.floor(ts);
			return sec > nowSec ? sec : 0;
		};
		const detailList = Array.isArray(root.credit_detail_list) ? root.credit_detail_list :
			Array.isArray(root.credits_detail) ? root.credits_detail :
			Array.isArray(c.credit_detail_list) ? c.credit_detail_list :
			Array.isArray(c.credits_detail) ? c.credits_detail : null;
		if (detailList && detailList.length > 0) {
			let sumRemain = 0;
			let sumTotal = 0;
			let earliestReset = 0;
			for (const item of detailList) {
				if (!item || typeof item !== 'object') continue;
				const rem = numField(item, ['residual_credits', 'remain_credits', 'remain_credit']);
				const tot = numField(item, ['total_credits', 'total_credit']);
				const exp = toFutureSec(numField(item, ['credits_life_end', 'expire_time', 'expired_at']));
				if (rem !== null) sumRemain += rem;
				if (tot !== null) sumTotal += tot;
				if (exp > 0 && (earliestReset === 0 || exp < earliestReset)) {
					earliestReset = exp;
				}
			}
			if (!out.hasCredits && (sumRemain > 0 || sumTotal > 0)) {
				out.hasCredits = true;
				out.creditsRemain = sumRemain;
			}
			if (sumTotal > out.creditsTotal) out.creditsTotal = sumTotal;
			if (earliestReset > 0 && out.creditsResetAt === 0) out.creditsResetAt = earliestReset;
		}
		const resetTs = toFutureSec(numField(c, ['credits_life_end', 'gift_credit_expire_time', 'expire_time', 'next_refresh_time', 'reset_time']));
		if (resetTs > 0 && out.creditsResetAt === 0) {
			out.creditsResetAt = resetTs;
		}
		if (out.hasCredits) {
			out.ok = true;
			out.source = 'user_credit';
		}
	}

	const applySub = (res) => {
		if (!res || !isApiOk(res) || !res.data || typeof res.data !== 'object') return;
		const d = res.data;
		const info = (d.vip_info && typeof d.vip_info === 'object' ? d.vip_info : null) ||
			(d.subscription && typeof d.subscription === 'object' ? d.subscription : null) || d;
		const level = numField(info, ['vip_level', 'member_level', 'level', 'tier']) || numField(d, ['vip_level', 'member_level']) || 0;
		const isVip = Boolean(info.is_vip || d.is_vip || info.is_member || d.is_member || info.vip_active || level > 0 ||
			String(info.flag || d.flag || '').toLowerCase() === 'vip' ||
			Number(info.status || d.status || 0) === 1 && level > 0);
		if (level > out.vipLevel) out.vipLevel = level;
		if (isVip) {
			out.vipActive = true;
			out.memberType = 'vip';
		}
		const name = strField(info, ['vip_name', 'member_name', 'plan_name', 'product_name', 'title']) ||
			strField(d, ['vip_name', 'member_name', 'plan_name']);
		if (name && !out.memberName) out.memberName = name;
		const exp = numField(info, ['expire_time', 'vip_expire_time', 'end_time', 'expired_at']) ||
			numField(d, ['expire_time', 'vip_expire_time', 'end_time']);
		if (exp && exp > 0 && out.vipExpireAt === 0) out.vipExpireAt = exp;
		const monthly = numField(info, ['monthly_credit', 'monthly_credits', 'total_credit', 'total_credits', 'benefit_credit']) || 0;
		if (monthly > out.vipMonthlyCredit) out.vipMonthlyCredit = monthly;
		if (Boolean(info.watermark_free || info.no_watermark || d.watermark_free)) {
			out.watermarkFree = true;
		}
		if (isVip || name || exp) {
			out.ok = true;
			if (!out.source) out.source = 'subscription';
		}
	};
	applySub(subLiteRes);
	applySub(subInfoRes);

	// DOM 兜底：顶栏积分徽章（如 ✦ 94）、单张积分消耗（如 1/张）与头像用户名
	const visible = (el) => {
		if (!el || !el.getBoundingClientRect) return false;
		const r = el.getBoundingClientRect();
		if (r.width < 2 || r.height < 2) return false;
		const s = getComputedStyle(el);
		return s.display !== 'none' && s.visibility !== 'hidden' && parseFloat(s.opacity) !== 0;
	};
	const hasLoginBtn = (() => {
		const nodes = document.querySelectorAll('[class*="login-button-wrapper-"],[class*="login-action-shell-"],[class*="project-login-prompt-"]');
		for (const el of nodes) if (visible(el)) return true;
		return false;
	})();

	if (!out.hasCredits && !hasLoginBtn) {
		const anchors = document.querySelectorAll('[data-main-frame-2026-credit-display-anchor],[class*="credit-display"],[class*="credit-count"],[class*="credit-num"]');
		for (const el of anchors) {
			if (!visible(el)) continue;
			const txt = String(el.innerText || el.textContent || '').replace(/,/g, '').trim();
			const m = txt.match(/(?:^|\s)(\d{1,7})(?:\s|$)/);
			if (m) {
				out.hasCredits = true;
				out.creditsRemain = parseInt(m[1], 10);
				out.ok = true;
				out.source = out.source || 'dom_credit';
				break;
			}
		}
	}

	const gen = document.querySelector('[data-testid="dreamina.generator"]');
	if (gen) {
		const norm = (v) => String(v || '').replace(/\s+/g, ' ').trim();
		const targetMode = '` + JimengTargetMode + `';
		const targetModel = '` + JimengTargetModel + `';
		const targetRes = '` + JimengTargetResolution + `';
		const escapeRe = (s) => String(s || '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
		const modelMatchesTarget = (txt) => {
			const clean = norm(txt).toLowerCase();
			const want = norm(targetModel).toLowerCase();
			if (!clean || !want) return false;
			if (clean === want) return true;
			const pattern = escapeRe(want).replace(/\s+/g, '\\s+');
			return new RegExp('(?:^|[^a-z0-9.])' + pattern + '(?:$|[^a-z0-9.])', 'i').test(clean);
		};
		const readCombobox = (ariaLabel) => {
			if (!gen.querySelectorAll) return '';
			const boxes = Array.from(gen.querySelectorAll('[role="combobox"]'));
			for (const el of boxes) {
				if (!visible(el)) continue;
				const aria = norm(el.getAttribute && el.getAttribute('aria-label'));
				if (aria === ariaLabel) return norm(el.innerText || el.textContent);
			}
			return '';
		};
		const readResolution = () => {
			if (!gen.querySelectorAll) return '';
			const btns = Array.from(gen.querySelectorAll('button,[role="button"]'));
			for (const el of btns) {
				if (!visible(el)) continue;
				const role = el.getAttribute && el.getAttribute('role');
				if (role === 'combobox') continue;
				const tid = el.getAttribute && el.getAttribute('data-testid');
				if (tid === 'dreamina.generator.submit') continue;
				const aria = norm(el.getAttribute && el.getAttribute('aria-label'));
				if (/^选择生成数量/.test(aria) || /上传参考/.test(aria)) continue;
				const t = norm(el.innerText || el.textContent);
				if (!t || /^\d+\s*张$/.test(t)) continue;
				const m = t.match(/\b(1\.5K|[1-4]K)\b/i);
				if (m) return m[1].toUpperCase();
			}
			return '';
		};
		const parsePerImageCost = (raw) => {
			const m = String(raw || '').match(/(\d+)\s*\/\s*张/);
			if (!m) return 0;
			const c = parseInt(m[1], 10);
			return Number.isFinite(c) && c > 0 ? c : 0;
		};
		const readActualPriceCost = () => {
			if (!gen.querySelectorAll) return 0;
			const selectors = [
				'[class*="actual-credits-"]',
				'[class*="credit-price-display-"]',
				'[class*="toolbar-actions-"]',
			];
			for (const sel of selectors) {
				const nodes = Array.from(gen.querySelectorAll(sel));
				for (const el of nodes) {
					if (!visible(el)) continue;
					const c = parsePerImageCost(el.innerText || el.textContent);
					if (c > 0) return c;
				}
			}
			return 0;
		};

		const modeText = readCombobox('创作类型');
		const modelText = readCombobox('模型');
		const resText = readResolution();
		out.model = modelText;
		out.resolution = resText;

		const modeOk = !modeText || modeText.indexOf(targetMode) >= 0;
		const modelOk = modelMatchesTarget(modelText);
		const resOk = !resText || resText.toLowerCase() === targetRes.toLowerCase();

		// 只要页面处于目标创作类型 + 目标模型（图片生成 + Seedream 4.7），记录当前分辨率的实时单张消耗（供本次任务按 2K/4K 实时核算余额）；
		// 但仅当分辨率同为默认 2K 时才写入 creditCost / costModelMatched，避免 4K（2/张）或 Seedream 5.5（4/张）覆盖账号默认的 2K 单价。
		if (modeOk && modelOk) {
			const c = readActualPriceCost();
			if (c > 0) out.pageCreditCost = c;
			if (resOk) {
				out.costModelMatched = true;
				if (c > 0) out.creditCost = c;
			}
		}
	}

	if (!out.username && !hasLoginBtn) {
		const av = document.querySelector('[data-main-frame-2026-avatar-action],[class*="avatar-action-"]');
		if (av) {
			const img = av.querySelector('img');
			const cand = (img && (img.getAttribute('alt') || img.getAttribute('title'))) ||
				av.getAttribute('aria-label') || av.getAttribute('title') || '';
			const clean = String(cand || '').replace(/\s+/g, ' ').trim();
			if (clean && clean.length <= 48 && !/^(?:头像|用户头像|avatar|登录|即梦|即梦ai|dreamina|profile)$/i.test(clean)) {
				out.username = clean;
				out.ok = true;
				out.source = out.source || 'dom_avatar';
			}
		}
	}

	return out;
})()`

// JimengMatchesModelResolution 判断页面当前模型与分辨率是否匹配目标模型（Seedream 4.7）和指定分辨率。
// targetResolution 为空时回退到默认分辨率（2k）。
func JimengMatchesModelResolution(model, resolution, targetResolution string) bool {
	m := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(model)), " "))
	want := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(JimengTargetModel)), " "))
	if m == "" || want == "" {
		return false
	}
	modelMatch := m == want
	if !modelMatch {
		idx := strings.Index(m, want)
		if idx >= 0 {
			beforeOK := idx == 0 || !isModelTokenChar(m[idx-1])
			afterIdx := idx + len(want)
			afterOK := afterIdx == len(m) || !isModelTokenChar(m[afterIdx])
			modelMatch = beforeOK && afterOK
		}
	}
	if !modelMatch {
		return false
	}
	wantRes := strings.TrimSpace(targetResolution)
	if wantRes == "" {
		wantRes = JimengTargetResolution
	}
	res := strings.TrimSpace(resolution)
	if res != "" && !strings.EqualFold(res, wantRes) {
		return false
	}
	return true
}

// jimengMatchesTargetModel 判断页面当前模型与分辨率是否与图来默认即梦生图配置（Seedream 4.7 · 2K）一致。
func jimengMatchesTargetModel(model, resolution string) bool {
	return JimengMatchesModelResolution(model, resolution, JimengTargetResolution)
}

func isModelTokenChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '.'
}

// NormalizeJimengStatus 将网页探测到的原始即梦状态归一为持久化用的 AccountQuota。
func NormalizeJimengStatus(raw JimengStatus, now time.Time) (store.AccountQuota, bool) {
	if !raw.OK {
		return store.AccountQuota{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}

	isVIP := raw.VIPActive || raw.VIPLevel > 0 || raw.VIPCredits > 0 || strings.EqualFold(strings.TrimSpace(raw.MemberType), "vip")
	tier := "free"
	memberName := strings.TrimSpace(raw.MemberName)
	if isVIP {
		tier = "vip"
		if memberName == "" || memberName == "免费版" {
			memberName = "即梦会员"
		}
	} else if memberName == "" {
		memberName = "免费版"
	}

	remain := raw.CreditsRemain
	if !raw.HasCredits {
		remain = -1
	} else if remain < 0 {
		remain = 0
	}

	total := raw.CreditsTotal
	if isVIP && raw.VIPMonthlyCredit > total {
		total = raw.VIPMonthlyCredit
	}
	if total <= 0 {
		if isVIP {
			total = max(remain, 0)
		} else if remain >= 0 {
			total = max(JimengFreeDefaultCredits, remain)
		}
	} else if remain > total {
		total = remain
	}

	cost := raw.CreditCost
	// 若探测携带了具体模型/分辨率但并非目标配置（Seedream 4.7 · 2K），绝不采信其他模型的单价（如 5.5 的 4/张）。
	if (strings.TrimSpace(raw.Model) != "" || strings.TrimSpace(raw.Resolution) != "") && !jimengMatchesTargetModel(raw.Model, raw.Resolution) {
		cost = 0
	}
	if cost <= 0 {
		cost = JimengDefaultCreditCost
	}

	resetSec := normalizeMillisOrSeconds(raw.CreditsResetAt)
	if resetSec > 0 && resetSec <= now.Unix() {
		// 忽略已经过期的历史积分桶截止时间，避免刚刚同步的实时余额被误判为 QuotaExpired。
		resetSec = 0
	}
	cycle := strings.TrimSpace(raw.CreditsCycle)
	if resetSec <= 0 && isVIP {
		if vipReset := normalizeMillisOrSeconds(raw.VIPExpireAt); vipReset > now.Unix() {
			resetSec = vipReset
		}
	}
	if cycle == "" {
		if isVIP {
			cycle = "monthly"
		} else if raw.GiftCredits > 0 || raw.FreeCredits > 0 || raw.HasCredits {
			cycle = "daily"
		}
	}

	return store.AccountQuota{
		CreditsRemain:  remain,
		CreditsTotal:   total,
		CreditCost:     cost,
		CreditsResetAt: resetSec,
		CreditsCycle:   cycle,
		MemberTier:     tier,
		MemberName:     memberName,
		WatermarkFree:  raw.WatermarkFree || isVIP,
		SyncedAt:       now.Unix(),
	}, true
}

func pickJimengTarget(list []sliderTarget) (string, bool) {
	for _, item := range list {
		if item.Type != "page" || item.ID == "" {
			continue
		}
		u := strings.ToLower(item.URL)
		if strings.Contains(u, "jimeng.jianying.com") {
			return item.ID, true
		}
	}
	return "", false
}

// ProbeJimengStatus 附着到已经打开的即梦页面，读取实时积分、会员与用户名状态。
// 只断开调试连接，不关闭标签页。
func ProbeJimengStatus(ctx context.Context, endpoint string) (JimengStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return JimengStatus{}, err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return JimengStatus{}, err
	}
	id, ok := pickJimengTarget(targets)
	if !ok {
		return JimengStatus{}, nil
	}
	if RegionBlockedURL(targetURL(targets, id)) {
		return JimengStatus{}, nil
	}
	var result JimengStatus
	if err := conn.evalJSON(ctx, id, jimengStatusProbeJS, &result, true, true); err != nil {
		return JimengStatus{}, err
	}
	return result, nil
}

// jimengProbeTargetCostJS 在即梦后台同步浏览器中将工具栏切换到图来实际使用的模型与分辨率
// （图片生成 + Seedream 4.7 + 2K），再读取网页实时显示的每张积分单价（如 1/张）。
const jimengProbeTargetCostJS = `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)jimeng\.jianying\.com$/.test(host)) {
		return { ok: false, creditCost: 0, model: '', resolution: '' };
	}
	const targetMode = '` + JimengTargetMode + `';
	const targetModel = '` + JimengTargetModel + `';
	const targetRes = '` + JimengTargetResolution + `';
	const norm = (v) => String(v || '').replace(/\s+/g, ' ').trim();
	const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
	const escapeRe = (s) => String(s || '').replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
	const modelMatchesTarget = (txt) => {
		const clean = norm(txt).toLowerCase();
		const want = norm(targetModel).toLowerCase();
		if (!clean || !want) return false;
		if (clean === want) return true;
		const pattern = escapeRe(want).replace(/\s+/g, '\\s+');
		return new RegExp('(?:^|[^a-z0-9.])' + pattern + '(?:$|[^a-z0-9.])', 'i').test(clean);
	};
	const shown = (el) => {
		if (!el || !el.isConnected || !el.getBoundingClientRect) return false;
		if (el.closest && el.closest('[aria-hidden="true"],[data-generator-preview-ignore="true"],[class*="prompt-editor-sizer"]')) return false;
		const r = el.getBoundingClientRect();
		if (r.width <= 4 || r.height <= 4) return false;
		const st = getComputedStyle(el);
		return st.display !== 'none' && st.visibility !== 'hidden' && parseFloat(st.opacity) > 0.05;
	};
	const visible = (el) => {
		if (!shown(el)) return false;
		const st = getComputedStyle(el);
		return st.pointerEvents !== 'none' && !el.disabled && el.getAttribute('aria-disabled') !== 'true';
	};
	const openPopups = () => Array.from(document.querySelectorAll('.lv-select-popup,.lv-popover-inner-content,[role="listbox"]')).filter(shown);
	const closePopups = async () => {
		if (!openPopups().length) return;
		try { document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true })); } catch (e) {}
		for (let i = 0; i < 6 && openPopups().length; i++) await sleep(25);
	};
	const findGen = () => document.querySelector('[data-testid="dreamina.generator"]');
	const findCombobox = (gen, ariaLabel) => {
		if (!gen) return null;
		return Array.from(gen.querySelectorAll('[role="combobox"]')).find((el) => shown(el) && norm(el.getAttribute('aria-label')) === ariaLabel) || null;
	};
	const findRatioBtn = (gen) => {
		if (!gen) return null;
		return Array.from(gen.querySelectorAll('button,[role="button"]')).find((el) => {
			if (!shown(el) || el.getAttribute('role') === 'combobox' || el.getAttribute('data-testid') === 'dreamina.generator.submit') return false;
			const aria = norm(el.getAttribute('aria-label'));
			if (/^选择生成数量/.test(aria) || /上传参考/.test(aria)) return false;
			const t = norm(el.innerText || el.textContent);
			return t && !/^\d+\s*张$/.test(t) && /比例|\d+:\d+|1\.5K|[1-4]K/i.test(t);
		}) || null;
	};
	const readRes = (gen) => {
		const btn = findRatioBtn(gen);
		if (!btn) return '';
		const m = norm(btn.innerText || btn.textContent).match(/\b(1\.5K|[1-4]K)\b/i);
		return m ? m[1].toUpperCase() : '';
	};
	const parseCost = (raw) => {
		const m = String(raw || '').match(/(\d+)\s*\/\s*张/);
		if (!m) return 0;
		const c = parseInt(m[1], 10);
		return Number.isFinite(c) && c > 0 ? c : 0;
	};
	const readPriceCost = (gen) => {
		if (!gen || !gen.querySelectorAll) return 0;
		for (const sel of ['[class*="actual-credits-"]', '[class*="credit-price-display-"]', '[class*="toolbar-actions-"]']) {
			for (const el of Array.from(gen.querySelectorAll(sel))) {
				if (!shown(el)) continue;
				const c = parseCost(el.innerText || el.textContent);
				if (c > 0) return c;
			}
		}
		return 0;
	};
	const findSelectOption = (wantText, matchFn) => {
		const wantLow = norm(wantText).toLowerCase();
		const popups = Array.from(document.querySelectorAll('.lv-select-popup,[role="listbox"]')).filter(shown);
		for (let p = popups.length - 1; p >= 0; p--) {
			const opts = Array.from(popups[p].querySelectorAll('li[role="option"],[role="option"]')).filter(visible);
			const hit = opts.find((opt) => {
				const lblEl = opt.querySelector && opt.querySelector('[class*="option-label-"]');
				const lbl = norm(lblEl ? (lblEl.innerText || lblEl.textContent) : (opt.innerText || opt.textContent));
				return matchFn ? matchFn(lbl) : (lbl.toLowerCase() === wantLow || lbl.toLowerCase().indexOf(wantLow) >= 0);
			});
			if (hit) return hit;
		}
		return null;
	};

	const deadline = Date.now() + 4200;
	let gen = null;
	while (Date.now() < deadline) {
		gen = findGen();
		if (gen && findCombobox(gen, '模型')) break;
		await sleep(50);
	}
	if (!gen) return { ok: false, creditCost: 0, model: '', resolution: '' };

	const prevCost = readPriceCost(gen);
	let switched = false;
	let switchedAt = 0;

	const ensureBox = async (ariaLabel, wantText, matchFn) => {
		const box = findCombobox(gen, ariaLabel);
		if (!box) return false;
		const cur = norm(box.innerText || box.textContent);
		if (matchFn ? matchFn(cur) : cur.indexOf(wantText) >= 0) return true;
		await closePopups();
		box.click();
		let opt = null;
		const openUntil = Math.min(deadline, Date.now() + 1200);
		while (Date.now() < openUntil) {
			opt = findSelectOption(wantText, matchFn);
			if (opt) break;
			await sleep(30);
		}
		if (!opt) {
			await closePopups();
			return false;
		}
		opt.click();
		switched = true;
		switchedAt = Date.now();
		const confirmUntil = Math.min(deadline, Date.now() + 1200);
		while (Date.now() < confirmUntil) {
			const again = findCombobox(gen, ariaLabel);
			const nowTxt = again ? norm(again.innerText || again.textContent) : '';
			if (matchFn ? matchFn(nowTxt) : nowTxt.indexOf(wantText) >= 0) {
				await closePopups();
				return true;
			}
			await sleep(30);
		}
		await closePopups();
		return false;
	};

	if (findCombobox(gen, '创作类型')) {
		if (!(await ensureBox('创作类型', targetMode))) {
			return { ok: false, creditCost: 0, model: norm((findCombobox(gen, '模型') || {}).innerText), resolution: readRes(gen) };
		}
	}
	if (!(await ensureBox('模型', targetModel, modelMatchesTarget))) {
		return { ok: false, creditCost: 0, model: norm((findCombobox(gen, '模型') || {}).innerText), resolution: readRes(gen) };
	}

	// 若当前分辨率不是目标分辨率（2K），切换到 2K 再读价。
	const curRes = readRes(gen);
	if (curRes && curRes.toLowerCase() !== targetRes.toLowerCase()) {
		const rBtn = findRatioBtn(gen);
		if (rBtn) {
			await closePopups();
			rBtn.click();
			let pop = null;
			const openUntil = Math.min(deadline, Date.now() + 1200);
			while (Date.now() < openUntil) {
				pop = Array.from(document.querySelectorAll('.lv-popover-inner-content,[role="dialog"]')).find((p) => shown(p) && p.querySelector('input[type="radio"]')) || null;
				if (pop) break;
				await sleep(30);
			}
			if (pop) {
				const inputs = Array.from(pop.querySelectorAll('input[type="radio"]'));
				const resInp = inputs.find((inp) => String(inp.value || inp.getAttribute('value') || '').toLowerCase() === targetRes.toLowerCase());
				if (resInp && !resInp.checked) {
					const target = (resInp.closest && resInp.closest('label')) || resInp;
					target.click();
					switched = true;
					switchedAt = Date.now();
					const confirmUntil = Math.min(deadline, Date.now() + 1000);
					while (Date.now() < confirmUntil) {
						if (readRes(gen).toLowerCase() === targetRes.toLowerCase()) break;
						await sleep(30);
					}
				}
			}
			await closePopups();
		}
	}

	const finalModel = norm((findCombobox(gen, '模型') || {}).innerText);
	const finalRes = readRes(gen);
	if (!modelMatchesTarget(finalModel) || (finalRes && finalRes.toLowerCase() !== targetRes.toLowerCase())) {
		return { ok: false, creditCost: 0, model: finalModel, resolution: finalRes };
	}

	// 刚切换模型/分辨率时等待页面重新渲染单张积分，避免读到切换前的旧价格（例如 Seedream 5.5 的 4/张）。
	let lastCost = 0;
	let stableAt = 0;
	while (Date.now() < deadline) {
		const c = readPriceCost(gen);
		const now = Date.now();
		if (c > 0) {
			if (!switched) {
				return { ok: true, creditCost: c, model: finalModel, resolution: finalRes };
			}
			const elapsed = now - switchedAt;
			if (c !== lastCost) {
				lastCost = c;
				stableAt = now;
			}
			const changedFromPrev = prevCost <= 0 || c !== prevCost;
			if (changedFromPrev && elapsed >= 150 && now - stableAt >= 120) {
				return { ok: true, creditCost: c, model: finalModel, resolution: finalRes };
			}
			if (!changedFromPrev && elapsed >= 900 && now - stableAt >= 200) {
				return { ok: true, creditCost: c, model: finalModel, resolution: finalRes };
			}
		} else {
			lastCost = 0;
			stableAt = 0;
		}
		await sleep(60);
	}
	return { ok: false, creditCost: 0, model: finalModel, resolution: finalRes };
})()`

// ProbeJimengTargetModelCost 在即梦后台同步浏览器中将页面切换到目标模型与分辨率（Seedream 4.7 · 2K）
// 后读取网页实际显示的每张积分单价。仅应在后台探测浏览器中调用，避免打扰用户手动打开的页面。
func ProbeJimengTargetModelCost(ctx context.Context, endpoint string) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return 0, err
	}
	id, ok := pickJimengTarget(targets)
	if !ok {
		return 0, nil
	}
	if RegionBlockedURL(targetURL(targets, id)) {
		return 0, nil
	}
	var result struct {
		OK         bool   `json:"ok"`
		CreditCost int    `json:"creditCost"`
		Model      string `json:"model"`
		Resolution string `json:"resolution"`
	}
	if err := conn.evalJSON(ctx, id, jimengProbeTargetCostJS, &result, true, true); err != nil {
		return 0, err
	}
	if !result.OK || result.CreditCost <= 0 || !jimengMatchesTargetModel(result.Model, result.Resolution) {
		return 0, nil
	}
	return result.CreditCost, nil
}

// ProbeJimengStatus 在已有任务会话页面中读取实时即梦积分与会员状态，不新开标签页。
func (s *Session) ProbeJimengStatus() (JimengStatus, error) {
	if s == nil {
		return JimengStatus{}, nil
	}
	out, err := s.EvaluateAsync(jimengStatusProbeJS)
	if err != nil {
		return JimengStatus{}, err
	}
	var result JimengStatus
	if err := ParseJSValue(out, &result); err != nil {
		return JimengStatus{}, err
	}
	return result, nil
}
