package cdp

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

const (
	// GeminiQuotaCycleFiveHour 表示 Gemini 5 小时滚动额度窗口（CreditsRemain/CreditsTotal 按 0..100 百分比存储）。
	GeminiQuotaCycleFiveHour = "5h"
	// GeminiQuotaCycleWeekly 表示 Gemini 每周额度上限窗口（CreditsRemain/CreditsTotal 按 0..100 百分比存储）。
	GeminiQuotaCycleWeekly = "weekly"
	// GeminiQuotaCycleAICredits 表示 Gemini 超额/月度 AI 积分池（CreditsRemain 为剩余积分数值）。
	GeminiQuotaCycleAICredits = "ai_credits"
	// GeminiQuotaCycleWindow 表示数值型次数窗口（CreditsRemain/CreditsTotal 为显式次数）。
	GeminiQuotaCycleWindow = "window"
)

// GeminiStatus 保存从 Gemini 网页实时探测到的剩余额度窗口、刷新时间、订阅套餐与账号信息。
type GeminiStatus struct {
	OK                  bool    `json:"ok"`
	Source              string  `json:"source"`
	Username            string  `json:"username"`
	Email               string  `json:"email"`
	SubscriptionTier    string  `json:"subscriptionTier"`
	PlanName            string  `json:"planName"`
	CanSeeUsageLimits   bool    `json:"canSeeUsageLimits"`
	HasFiveHour         bool    `json:"hasFiveHour"`
	FiveHourPctUsed     float64 `json:"fiveHourPctUsed"`
	FiveHourResetAt     int64   `json:"fiveHourResetAt"` // 毫秒时间戳
	HasWeekly           bool    `json:"hasWeekly"`
	WeeklyPctUsed       float64 `json:"weeklyPctUsed"`
	WeeklyResetAt       int64   `json:"weeklyResetAt"` // 毫秒时间戳
	HasCredits          bool    `json:"hasCredits"`
	RemainingCredits    int     `json:"remainingCredits"`
	TotalCredits        int     `json:"totalCredits"`
	CreditsResetAt      int64   `json:"creditsResetAt"` // 毫秒时间戳
	UseOverageAiCredits bool    `json:"useOverageAiCredits"`
}

// geminiStatusProbeJS 在已登录的 Gemini（gemini.google.com）页面上下文内同源读取：
// 1) 官方 UsagePageQuery GraphQL（GXU_FIVE_HOURLY / GXU_WEEKLY / OVERAGE_CREDITS 与 subscriptionTier）；
// 2) 若 GraphQL 未返回额度池，回退到同源 batchexecute（jSf9Qc / qpEbW）；
// 3) 同时从 DOM 与 WIZ_global_data 提取用户名/邮箱与页面套餐标记，严禁使用 innerHTML/eval。
// 在非 gemini.google.com 域名上立即返回 {ok:false}，不发任何外部请求。
const geminiStatusProbeJS = `(async () => {
	const host = String(location.hostname || '').toLowerCase();
	if (!/(^|\.)gemini\.google\.com$/.test(host)) {
		return { ok: false };
	}
	const out = {
		ok: false,
		source: '',
		username: '',
		email: '',
		subscriptionTier: '',
		planName: '',
		canSeeUsageLimits: false,
		hasFiveHour: false,
		fiveHourPctUsed: 0,
		fiveHourResetAt: 0,
		hasWeekly: false,
		weeklyPctUsed: 0,
		weeklyResetAt: 0,
		hasCredits: false,
		remainingCredits: -1,
		totalCredits: 0,
		creditsResetAt: 0,
		useOverageAiCredits: false,
	};
	const wiz = (window.WIZ_global_data && typeof window.WIZ_global_data === 'object') ? window.WIZ_global_data : {};
	const email = typeof wiz.oPEP7c === 'string' && wiz.oPEP7c.indexOf('@') > 0 ? wiz.oPEP7c.trim() : '';
	out.email = email;

	const isEmailLike = (s) => /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(String(s || '').trim());
	const parseGoogleLabel = (raw) => {
		const text = String(raw || '').trim();
		if (!text) return '';
		let lines = text.split(/[\r\n]+/).map((line) => line.replace(/\s+/g, ' ').trim()).filter(Boolean);
		if (lines.length === 0) return '';
		lines[0] = lines[0].replace(/^Google\s*(?:Account|账号|账户|帳戶|帳號)\s*[:：]?\s*/i, '').trim();
		lines = lines.filter(Boolean);
		if (lines.length === 0) return '';
		const candidates = [];
		for (const line of lines) {
			const paren = line.match(/^([^()]+?)\s*\(([^()]+)\)\s*$/);
			if (paren) {
				candidates.push(paren[1].trim(), paren[2].trim());
				continue;
			}
			const inlineEmail = line.match(/^(.+?)\s+([^\s@]+@[^\s@]+\.[^\s@]+)$/);
			if (inlineEmail) {
				candidates.push(inlineEmail[1].trim(), inlineEmail[2].trim());
				continue;
			}
			const wrapped = line.match(/^\(([^()]+)\)$/);
			candidates.push(wrapped ? wrapped[1].trim() : line);
		}
		let fallbackEmail = '';
		for (const c of candidates) {
			const clean = String(c || '').replace(/\s+/g, ' ').trim();
			if (!clean || clean.length > 48) continue;
			if (/^(?:google|gemini|google account|google 账号|google 账户|google 帳戶|google 帳號)$/i.test(clean)) continue;
			if (isEmailLike(clean)) {
				if (!fallbackEmail) fallbackEmail = clean;
				continue;
			}
			return clean;
		}
		return fallbackEmail;
	};
	const readDomUsername = () => {
		const sels = [
			'[gem-open-account-menu]',
			'a[href*="accounts.google.com/SignOutOptions"]',
			'a.gb_B[aria-label]',
			'a.gb_A[aria-label]',
			'[data-ogsr-up] a[aria-label]',
			'a[aria-label*="Google Account"]',
			'button[aria-label*="Google Account"]',
			'a[aria-label*="Google 账号"]',
			'button[aria-label*="Google 账号"]',
			'a[aria-label*="Google 账户"]',
			'button[aria-label*="Google 账户"]',
			'a[aria-label*="Google 帳戶"]',
			'button[aria-label*="Google 帳戶"]',
			'a[aria-label*="Google 帳號"]',
			'button[aria-label*="Google 帳號"]',
		];
		for (const sel of sels) {
			const el = document.querySelector(sel);
			if (!el) continue;
			const lbl = el.getAttribute && (el.getAttribute('aria-label') || el.getAttribute('title'));
			const name = parseGoogleLabel(lbl);
			if (name) return name;
		}
		const pic = document.querySelector('user-profile-picture img');
		if (pic && pic.getAttribute) {
			const alt = parseGoogleLabel(pic.getAttribute('aria-label') || pic.getAttribute('alt'));
			if (alt) return alt;
		}
		return '';
	};
	out.username = readDomUsername() || email;

	const detectDomPlan = () => {
		try {
			const pills = document.querySelectorAll('header a, header button, [data-test-id*="pill"], [data-test-id*="badge"], [data-test-id*="upsell"], .pill-label');
			let sawUpgrade = false;
			for (const el of pills) {
				const t = String(el.textContent || '').replace(/\s+/g, ' ').trim();
				if (!t || t.length > 48) continue;
				if (/\bUltra\b/i.test(t) && !/upgrade|升级|试用|try/i.test(t)) return { tier: 'TIER2', name: 'AI Ultra' };
				if (/\b(?:AI\s+Pro|Gemini\s+Pro|Advanced|Pro)\b/i.test(t) && !/upgrade|升级|试用|try/i.test(t)) return { tier: 'TIER1', name: 'AI Pro' };
				if (/\b(?:AI\s+Plus|Plus)\b/i.test(t) && !/upgrade|升级|试用|try/i.test(t)) return { tier: 'TIER_HELIUM', name: 'AI Plus' };
				if (/upgrade|升级|试用|try\s+gemini\s+(?:advanced|pro)/i.test(t)) sawUpgrade = true;
			}
			if (sawUpgrade) return { tier: 'FREE', name: '免费版' };
		} catch (e) {}
		return { tier: '', name: '' };
	};

	const findUserPrefix = () => {
		const rawCandidates = [wiz.eptZe, wiz.Im6cmf];
		try {
			for (const v of Object.values(wiz)) {
				if (typeof v === 'string' && v.startsWith('/') && v.indexOf('/_/BardChatUi') >= 0) {
					rawCandidates.push(v);
				}
			}
		} catch (e) {}
		for (const c of rawCandidates) {
			if (typeof c !== 'string') continue;
			const idx = c.indexOf('/_/BardChatUi');
			if (idx >= 0 && c.startsWith('/')) {
				return c.slice(0, idx);
			}
		}
		const m = String(location.pathname || '').match(/^(\/u\/\d+)(?:\/|$)/);
		return m ? m[1] : '';
	};
	const userPrefix = findUserPrefix();
	const graphqlBase = userPrefix + '/_/BardChatUi/graphql';
	const batchExecBase = userPrefix + '/_/BardChatUi/data/batchexecute';
	const snlm0e = typeof wiz.SNlM0e === 'string' ? wiz.SNlM0e.trim() : '';
	const bl = typeof wiz.cfb2h === 'string' ? wiz.cfb2h.trim() : '';
	const fSid = typeof wiz.FdrFJe === 'string' ? wiz.FdrFJe.trim() : '';

	const fetchWithTimeout = async (url, init, timeoutMs) => {
		const ctrl = typeof AbortController === 'function' ? new AbortController() : null;
		const timer = ctrl ? setTimeout(() => { try { ctrl.abort(); } catch (e) {} }, timeoutMs || 3500) : null;
		try {
			return await fetch(url, { ...(init || {}), signal: ctrl ? ctrl.signal : undefined });
		} catch (e) {
			return null;
		} finally {
			if (timer) clearTimeout(timer);
		}
	};

	const normPctUsed = (raw) => {
		const n = Number(raw);
		if (!Number.isFinite(n) || n < 0) return null;
		if (n <= 1) return Math.min(100, Math.max(0, Math.round(n * 1000) / 10));
		return Math.min(100, Math.max(0, Math.round(n * 10) / 10));
	};

	const parseEpochMs = (raw) => {
		if (typeof raw === 'number' && Number.isFinite(raw) && raw > 0) {
			return raw > 1e12 ? Math.round(raw) : Math.round(raw * 1000);
		}
		if (typeof raw === 'string' && raw.trim()) {
			const num = Number(raw.trim());
			if (Number.isFinite(num) && num > 1e9) {
				return num > 1e12 ? Math.round(num) : Math.round(num * 1000);
			}
			const ts = Date.parse(raw.trim());
			if (Number.isFinite(ts) && ts > 0) return ts;
		}
		return 0;
	};

	const stripXssi = (txt) => String(txt || '').replace(/^\)\]\}'\s*/, '').trim();

	const applyGraphQLPayloads = (payloads) => {
		let matched = false;
		const visit = (node) => {
			if (!node || typeof node !== 'object') return;
			if (Array.isArray(node)) {
				for (const item of node) visit(item);
				return;
			}
			if (node.featureGates && typeof node.featureGates.canSeeUsageLimits === 'boolean') {
				out.canSeeUsageLimits = node.featureGates.canSeeUsageLimits;
			}
			const info = node.getUsageInfo && typeof node.getUsageInfo === 'object' ? node.getUsageInfo : null;
			if (info) {
				matched = true;
				if (typeof info.subscriptionTier === 'string' && info.subscriptionTier.trim()) {
					out.subscriptionTier = info.subscriptionTier.trim();
				}
				if (typeof info.useOverageAiCredits === 'boolean') {
					out.useOverageAiCredits = info.useOverageAiCredits;
				}
				if (Array.isArray(info.pools)) {
					for (const pool of info.pools) {
						if (!pool || typeof pool !== 'object') continue;
						const pType = String(pool.poolType || '');
						if (pType === 'GXU_FIVE_HOURLY') {
							const pct = normPctUsed(pool.percentageUsed);
							if (pct !== null) {
								out.hasFiveHour = true;
								out.fiveHourPctUsed = pct;
								out.fiveHourResetAt = parseEpochMs(pool.gxu && pool.gxu.windowRefreshesAt);
							}
						} else if (pType === 'GXU_WEEKLY') {
							const pct = normPctUsed(pool.percentageUsed);
							if (pct !== null) {
								out.hasWeekly = true;
								out.weeklyPctUsed = pct;
								out.weeklyResetAt = parseEpochMs(pool.gxu && pool.gxu.windowRefreshesAt);
							}
						} else if (pType === 'OVERAGE_CREDITS') {
							const rem = Number(pool.remainingCredits);
							if (Number.isFinite(rem) && rem >= 0) {
								out.hasCredits = true;
								out.remainingCredits = Math.round(rem);
							}
						}
					}
				}
			}
			for (const k of Object.keys(node)) {
				const v = node[k];
				if (v && typeof v === 'object') visit(v);
			}
		};
		visit(payloads);
		return matched;
	};

	const parseGraphQLResponse = (rawText) => {
		const cleaned = stripXssi(rawText);
		if (!cleaned) return null;
		try {
			return JSON.parse(cleaned);
		} catch (e) {}
		const chunks = [];
		for (const line of cleaned.split(/\r?\n/)) {
			const t = line.trim();
			if (!t || (!t.startsWith('{') && !t.startsWith('['))) continue;
			try { chunks.push(JSON.parse(t)); } catch (e) {}
		}
		return chunks.length > 0 ? chunks : null;
	};

	// 1. Primary: Official Gemini UsagePageQuery GraphQL
	const querySignature = '2/oee4e7iJB+YlSRDtGuxpv36rfWVddgii370z2JvCuVM=';
	const variables = {
		g1Request: {
			entrypoints: [
				'GEMINI_USAGE_SETTINGS_UPGRADE_BANNER',
				'GEMINI_USAGE_SETTINGS_AI_CREDITS_UPSELL_CARD',
				'GEMINI_USAGE_SETTINGS_UPGRADE_AND_CREDITS_BANNER',
			],
		},
		workspaceRequest: {
			recommendationsForSurfaceRequests: [{
				recommendationSurface: {
					rpOnramp: 'GEMINI_CHAT_USAGE_LIMITS_UPSELL',
					version: '1',
				},
				maxRecommendationCount: 1,
			}],
		},
	};
	const queryEndpoint = graphqlBase + '/schemas/GEMINI_WEB_GRAPHQL/executeQuery';
	let gqlMatched = false;
	try {
		const qs = new URLSearchParams({
			query_signature: querySignature,
			variables: JSON.stringify(variables),
		});
		const getUrl = queryEndpoint + '?' + qs.toString();
		let res = null;
		if (getUrl.length <= 2048) {
			res = await fetchWithTimeout(getUrl, {
				method: 'GET',
				credentials: 'same-origin',
				headers: { Accept: 'application/json' },
			}, 3800);
		}
		if (res && res.ok) {
			const parsed = parseGraphQLResponse(await res.text());
			if (parsed && applyGraphQLPayloads(parsed)) {
				gqlMatched = true;
				out.ok = true;
				out.source = 'graphql';
			}
		}
		if (!gqlMatched) {
			let token = snlm0e;
			const tokenRes = await fetchWithTimeout(graphqlBase + '/xsrfToken', {
				method: 'GET',
				credentials: 'same-origin',
				headers: { Accept: 'text/plain' },
			}, 2500);
			if (tokenRes && tokenRes.ok) {
				const fetched = (await tokenRes.text()).trim();
				if (fetched) token = fetched;
			}
			if (token) {
				const postRes = await fetchWithTimeout(queryEndpoint, {
					method: 'POST',
					credentials: 'same-origin',
					headers: {
						Accept: 'application/json',
						'Content-Type': 'application/json',
						'X-Framework-XSRF-Token': token,
					},
					body: JSON.stringify({ querySignature, variables }),
				}, 3800);
				if (postRes && postRes.ok) {
					const parsed = parseGraphQLResponse(await postRes.text());
					if (parsed && applyGraphQLPayloads(parsed)) {
						gqlMatched = true;
						out.ok = true;
						out.source = 'graphql';
					}
				}
			}
		}
	} catch (e) {}

	// 2. Fallback: batchexecute RPCs (jSf9Qc usage windows / qpEbW quota check)
	const hasMeter = () => out.hasFiveHour || out.hasWeekly || out.hasCredits;
	const callBatchExecute = async (rpcId, payloadStr) => {
		if (!snlm0e) return null;
		const params = new URLSearchParams({
			rpcids: rpcId,
			'source-path': '/usage',
			hl: 'zh-CN',
			rt: 'c',
		});
		if (bl) params.set('bl', bl);
		if (fSid) params.set('f.sid', fSid);
		const body = new URLSearchParams({
			'f.req': JSON.stringify([[[rpcId, payloadStr || '[]', null, 'generic']]]),
			at: snlm0e,
		});
		const res = await fetchWithTimeout(batchExecBase + '?' + params.toString(), {
			method: 'POST',
			credentials: 'same-origin',
			headers: { 'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8' },
			body: body.toString(),
		}, 3500);
		if (!res || !res.ok) return null;
		const raw = stripXssi(await res.text());
		for (const line of raw.split(/\r?\n/)) {
			const trimmed = line.trim();
			if (!trimmed.startsWith('[')) continue;
			try {
				const outer = JSON.parse(trimmed);
				if (!Array.isArray(outer)) continue;
				for (const entry of outer) {
					if (Array.isArray(entry) && entry[0] === 'wrb.fr' && entry[1] === rpcId && typeof entry[2] === 'string') {
						return JSON.parse(entry[2]);
					}
				}
			} catch (e) {}
		}
		return null;
	};

	const parseTimestampTuple = (node) => {
		if (!Array.isArray(node)) return 0;
		for (const item of node) {
			if (typeof item === 'number' && item > 1700000000 && item < 4102444800) {
				return Math.round(item * 1000);
			}
			if (Array.isArray(item) && typeof item[0] === 'number' && item[0] > 1700000000 && item[0] < 4102444800) {
				return Math.round(item[0] * 1000);
			}
		}
		return 0;
	};

	if (!hasMeter() && snlm0e) {
		const jData = await callBatchExecute('jSf9Qc', '[]');
		if (jData) {
			const scanWindows = (node) => {
				if (!Array.isArray(node)) return;
				if (
					node.length >= 3 &&
					typeof node[1] === 'number' && node[1] >= 0 && node[1] <= 100 &&
					(node[2] === 1 || node[2] === 2)
				) {
					const pct = normPctUsed(node[1]);
					const resetMs = parseTimestampTuple(node[3]);
					if (pct !== null) {
						if (node[2] === 1 && !out.hasFiveHour) {
							out.hasFiveHour = true;
							out.fiveHourPctUsed = pct;
							out.fiveHourResetAt = resetMs;
							out.ok = true;
							if (!out.source) out.source = 'batchexecute_jSf9Qc';
						} else if (node[2] === 2 && !out.hasWeekly) {
							out.hasWeekly = true;
							out.weeklyPctUsed = pct;
							out.weeklyResetAt = resetMs;
							out.ok = true;
							if (!out.source) out.source = 'batchexecute_jSf9Qc';
						}
					}
				}
				for (const child of node) {
					if (Array.isArray(child)) scanWindows(child);
				}
			};
			scanWindows(jData);
		}
	}

	if (!hasMeter() && snlm0e) {
		const qData = await callBatchExecute('qpEbW', '[]');
		if (qData) {
			const scanQuota = (node) => {
				if (!node || typeof node !== 'object') return;
				if (Array.isArray(node)) {
					for (const child of node) scanQuota(child);
					return;
				}
				const rem = Number(node.remaining ?? node.creditsRemaining ?? node.remainingQuota);
				const lim = Number(node.limit ?? node.total ?? node.quotaLimit);
				const pct = node.percentUsed ?? node.percentageUsed;
				if (Number.isFinite(rem) && rem >= 0) {
					out.hasCredits = true;
					out.remainingCredits = Math.round(rem);
					if (Number.isFinite(lim) && lim > 0) out.totalCredits = Math.round(lim);
					out.creditsResetAt = parseEpochMs(node.resetAt ?? node.resetsAt ?? node.resetEpochSec);
					out.ok = true;
					if (!out.source) out.source = 'batchexecute_qpEbW';
				} else if (pct !== undefined && pct !== null) {
					const p = normPctUsed(pct);
					if (p !== null) {
						out.hasFiveHour = true;
						out.fiveHourPctUsed = p;
						out.fiveHourResetAt = parseEpochMs(node.resetAt ?? node.resetsAt ?? node.resetEpochSec);
						out.ok = true;
						if (!out.source) out.source = 'batchexecute_qpEbW';
					}
				}
			};
			scanQuota(qData);
		}
	}

	// 3. DOM plan fallback (only augments tier when GraphQL did not return subscriptionTier)
	if (!out.subscriptionTier) {
		const domPlan = detectDomPlan();
		if (domPlan.tier) {
			out.subscriptionTier = domPlan.tier;
			out.planName = domPlan.name;
			if (!out.ok && (out.username || out.email)) {
				out.ok = true;
				out.source = 'dom';
			}
		}
	}

	return out;
})()`

// NormalizeGeminiStatus 将网页探测到的原始 Gemini 状态归一为持久化用的 AccountQuota。
func NormalizeGeminiStatus(raw GeminiStatus, now time.Time) (store.AccountQuota, bool) {
	if !raw.OK {
		return store.AccountQuota{}, false
	}
	if now.IsZero() {
		now = time.Now()
	}

	tier, memberName := normalizeGeminiPlan(raw.SubscriptionTier, raw.PlanName)

	remain := -1
	total := 0
	cost := 0
	var resetAt int64
	cycle := ""

	switch {
	case raw.HasFiveHour || raw.HasWeekly:
		fiveRemain := pctUsedToRemain(raw.FiveHourPctUsed)
		weeklyRemain := pctUsedToRemain(raw.WeeklyPctUsed)
		useWeekly := false
		if raw.HasWeekly && !raw.HasFiveHour {
			useWeekly = true
		} else if raw.HasWeekly && raw.HasFiveHour && (weeklyRemain == 0 || weeklyRemain < fiveRemain) {
			useWeekly = true
		}
		if useWeekly {
			remain = weeklyRemain
			total = 100
			cycle = GeminiQuotaCycleWeekly
			resetAt = normalizeMillisOrSeconds(raw.WeeklyResetAt)
		} else {
			remain = fiveRemain
			total = 100
			cycle = GeminiQuotaCycleFiveHour
			resetAt = normalizeMillisOrSeconds(raw.FiveHourResetAt)
		}
		// 若计算额度已耗尽且开启了超额 AI 积分，切到可用 AI 积分余额显示。
		if remain == 0 && raw.UseOverageAiCredits && raw.HasCredits && raw.RemainingCredits > 0 {
			remain = raw.RemainingCredits
			total = 0
			cycle = GeminiQuotaCycleAICredits
		}
	case raw.HasCredits && raw.RemainingCredits >= 0:
		remain = raw.RemainingCredits
		total = max(0, raw.TotalCredits)
		resetAt = normalizeMillisOrSeconds(raw.CreditsResetAt)
		if total > 0 {
			cost = 1
			if remain > total {
				total = remain
			}
			cycle = GeminiQuotaCycleWindow
		} else {
			cycle = GeminiQuotaCycleAICredits
		}
	}

	usagePct := -1
	platformLimited := false
	if (cycle == GeminiQuotaCycleFiveHour || cycle == GeminiQuotaCycleWeekly) && remain >= 0 {
		usagePct = max(0, min(100, 100-remain))
		platformLimited = remain == 0
	}

	return store.AccountQuota{
		CreditsRemain:      remain,
		CreditsTotal:       total,
		CreditCost:         cost,
		CreditsResetAt:     resetAt,
		CreditsCycle:       cycle,
		MemberTier:         tier,
		MemberName:         memberName,
		AccountConcurrency: 0,
		WatermarkFree:      false,
		UsagePercent:       usagePct,
		PlatformLimited:    platformLimited,
		SyncedAt:           now.Unix(),
	}, true
}

func pctUsedToRemain(used float64) int {
	if math.IsNaN(used) || math.IsInf(used, 0) {
		return 0
	}
	if used <= 0 {
		return 100
	}
	if used >= 100 {
		return 0
	}
	rem := 100 - int(math.Round(used))
	if rem < 0 {
		return 0
	}
	if rem > 100 {
		return 100
	}
	return rem
}

func normalizeGeminiPlan(rawTier, rawName string) (string, string) {
	t := strings.ToUpper(strings.TrimSpace(rawTier))
	name := strings.TrimSpace(rawName)
	switch {
	case t == "TIER2" || t == "TIER_NEON" || strings.Contains(t, "ULTRA"):
		if name == "" {
			name = "AI Ultra"
		}
		return "ultra", name
	case t == "TIER1" || strings.Contains(t, "PRO") || strings.Contains(t, "ADVANCED"):
		if name == "" {
			name = "AI Pro"
		}
		return "pro", name
	case t == "TIER_HELIUM" || strings.Contains(t, "PLUS"):
		if name == "" {
			name = "AI Plus"
		}
		return "plus", name
	default:
		if name == "" {
			name = "免费版"
		}
		return "free", name
	}
}

func pickGeminiTarget(list []sliderTarget) (string, bool) {
	for _, item := range list {
		if item.Type != "page" || item.ID == "" {
			continue
		}
		u := strings.ToLower(item.URL)
		if strings.Contains(u, "gemini.google.com") {
			return item.ID, true
		}
	}
	return "", false
}

// ProbeGeminiStatus 附着到已经打开的 Gemini 页面，读取实时剩余额度、刷新时间、套餐与账号名。
// 只断开调试连接，不关闭标签页。
func ProbeGeminiStatus(ctx context.Context, endpoint string) (GeminiStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := DialSlider(ctx, endpoint)
	if err != nil {
		return GeminiStatus{}, err
	}
	defer conn.Close()
	targets, err := conn.targets(ctx)
	if err != nil {
		return GeminiStatus{}, err
	}
	id, ok := pickGeminiTarget(targets)
	if !ok {
		return GeminiStatus{}, nil
	}
	if RegionBlockedURL(targetURL(targets, id)) {
		return GeminiStatus{}, nil
	}
	var result GeminiStatus
	if err := conn.evalJSON(ctx, id, geminiStatusProbeJS, &result, true, true); err != nil {
		return GeminiStatus{}, err
	}
	return result, nil
}

// ProbeGeminiStatus 在已有任务会话页面中读取实时 Gemini 剩余额度与套餐状态，不新开标签页。
func (s *Session) ProbeGeminiStatus() (GeminiStatus, error) {
	if s == nil {
		return GeminiStatus{}, nil
	}
	out, err := s.EvaluateAsync(geminiStatusProbeJS)
	if err != nil {
		return GeminiStatus{}, err
	}
	var result GeminiStatus
	if err := ParseJSValue(out, &result); err != nil {
		return GeminiStatus{}, err
	}
	return result, nil
}
