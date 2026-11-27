package cdp

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestNormalizeJimengStatus(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, loc)

	t.Run("free account with 94 credits", func(t *testing.T) {
		exp := time.Date(2026, 11, 27, 23, 59, 59, 0, loc)
		q, ok := NormalizeJimengStatus(JimengStatus{
			OK:             true,
			HasCredits:     true,
			CreditsRemain:  94,
			GiftCredits:    60,
			PurchaseCredits: 34,
			CreditCost:     1,
			CreditsResetAt: exp.UnixMilli(),
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditsRemain != 94 || q.CreditsTotal != 94 || q.CreditCost != 1 {
			t.Fatalf("credits mismatch: %+v", q)
		}
		if q.CreditsCycle != "daily" || q.CreditsResetAt != exp.Unix() {
			t.Fatalf("cycle/reset mismatch: cycle=%q reset=%d want=%d", q.CreditsCycle, q.CreditsResetAt, exp.Unix())
		}
		if q.MemberTier != "free" || q.MemberName != "免费版" || q.WatermarkFree {
			t.Fatalf("member mismatch: %+v", q)
		}
	})

	t.Run("free account exhausted credits", func(t *testing.T) {
		q, ok := NormalizeJimengStatus(JimengStatus{
			OK:            true,
			HasCredits:    true,
			CreditsRemain: 0,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditsRemain != 0 || q.CreditsTotal != JimengFreeDefaultCredits || q.CreditCost != JimengDefaultCreditCost {
			t.Fatalf("exhausted free mismatch: %+v", q)
		}
	})

	t.Run("vip account monthly credits", func(t *testing.T) {
		exp := time.Date(2026, 12, 27, 23, 59, 59, 0, loc)
		q, ok := NormalizeJimengStatus(JimengStatus{
			OK:               true,
			HasCredits:       true,
			CreditsRemain:    420,
			VIPCredits:       420,
			VIPActive:        true,
			VIPLevel:         2,
			MemberName:       "标准会员",
			VIPMonthlyCredit: 500,
			VIPExpireAt:      exp.Unix(),
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.MemberTier != "vip" || q.MemberName != "标准会员" || q.CreditsTotal != 500 || !q.WatermarkFree {
			t.Fatalf("vip normalization mismatch: %+v", q)
		}
		if q.CreditsCycle != "monthly" || q.CreditsResetAt != exp.Unix() {
			t.Fatalf("vip cycle/reset mismatch: %+v", q)
		}
	})

	t.Run("expired past reset timestamp is ignored so fresh sync is not treated as expired", func(t *testing.T) {
		past := now.Add(-2 * time.Hour)
		q, ok := NormalizeJimengStatus(JimengStatus{
			OK:             true,
			HasCredits:     true,
			CreditsRemain:  5,
			CreditCost:     1,
			CreditsResetAt: past.UnixMilli(),
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditsResetAt != 0 {
			t.Fatalf("expired CreditsResetAt should be cleared, got %d", q.CreditsResetAt)
		}
	})

	t.Run("non-ok status ignored", func(t *testing.T) {
		if _, ok := NormalizeJimengStatus(JimengStatus{OK: false}, now); ok {
			t.Fatal("expected ok=false")
		}
	})

	t.Run("mismatched page model cost ignored", func(t *testing.T) {
		// 网页默认停留在 Seedream 5.5（1.5K，4/张）时，不得把 4/张 当作 Seedream 4.7 的单价保存。
		q, ok := NormalizeJimengStatus(JimengStatus{
			OK:            true,
			HasCredits:    true,
			CreditsRemain: 94,
			Model:         "Seedream 5.5",
			Resolution:    "1.5K",
			CreditCost:    4,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.CreditCost != JimengDefaultCreditCost {
			t.Fatalf("expected fallback cost=%d for mismatched model, got %d", JimengDefaultCreditCost, q.CreditCost)
		}

		// 即使模型是 Seedream 4.7，4K 分辨率（2/张）也不得覆盖 2K 默认单价。
		q4k, ok := NormalizeJimengStatus(JimengStatus{
			OK:            true,
			HasCredits:    true,
			CreditsRemain: 94,
			Model:         "Seedream 4.7",
			Resolution:    "4K",
			CreditCost:    2,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q4k.CreditCost != JimengDefaultCreditCost {
			t.Fatalf("expected fallback cost=%d for 4K resolution, got %d", JimengDefaultCreditCost, q4k.CreditCost)
		}

		// 当页面确实处于 Seedream 4.7 · 2K 时，如实采信网页显示的单张积分（含未来调价场景）。
		qLive, ok := NormalizeJimengStatus(JimengStatus{
			OK:               true,
			HasCredits:       true,
			CreditsRemain:    94,
			Model:            "Seedream 4.7",
			Resolution:       "2K",
			CostModelMatched: true,
			CreditCost:       2,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if qLive.CreditCost != 2 {
			t.Fatalf("expected live Seedream 4.7 2K cost=2, got %d", qLive.CreditCost)
		}
	})
}

func TestPickJimengTarget(t *testing.T) {
	id, ok := pickJimengTarget([]sliderTarget{
		{ID: "blank", Type: "page", URL: "about:blank"},
		{ID: "iframe", Type: "iframe", URL: "https://jimeng.jianying.com/ai-tool/generate/?type=image"},
		{ID: "other", Type: "page", URL: "https://www.doubao.com/chat/"},
		{ID: "jimeng", Type: "page", URL: "https://jimeng.jianying.com/ai-tool/generate/?type=image"},
	})
	if !ok || id != "jimeng" {
		t.Fatalf("pickJimengTarget = %q, ok=%v", id, ok)
	}
	if _, ok := pickJimengTarget([]sliderTarget{{ID: "other", Type: "page", URL: "https://example.com"}}); ok {
		t.Fatal("expected no match for non-jimeng target")
	}
}

func TestJimengStatusProbeJSExecution(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not available in PATH")
	}
	for _, needle := range []string{
		"/commerce/v1/benefits/user_credit",
		"/commerce/v3/subscription/get_user_lite_info",
		"/passport/account/info/v2/",
		"data-main-frame-2026-credit-display-anchor",
		"data-main-frame-2026-avatar-action",
	} {
		if !strings.Contains(jimengStatusProbeJS, needle) {
			t.Fatalf("jimengStatusProbeJS missing %q", needle)
		}
	}

	scriptJSON, err := json.Marshal(jimengStatusProbeJS)
	if err != nil {
		t.Fatal(err)
	}
	targetCostJSON, err := json.Marshal(jimengProbeTargetCostJS)
	if err != nil {
		t.Fatal(err)
	}
	harness := `
const vm = require('vm');
const probeSrc = ` + string(scriptJSON) + `;
const targetCostSrc = ` + string(targetCostJSON) + `;

function makeEl(opts) {
  const o = Object.assign({ w: 40, h: 20, text: '', attrs: {}, cls: '', tag: 'DIV', sub: {} }, opts || {});
  const node = {
    tagName: o.tag,
    isConnected: true,
    className: o.cls,
    innerText: o.text,
    textContent: o.text,
    disabled: Boolean(o.disabled),
    checked: Boolean(o.checked),
    value: o.value || '',
    getBoundingClientRect: () => ({ width: o.w, height: o.h }),
    getAttribute: (k) => {
      if (k === 'class') return node.className;
      if (k === 'value' && node.value !== '') return String(node.value);
      return k in o.attrs ? o.attrs[k] : null;
    },
    querySelector: (sel) => {
      if (sel === 'img') return o.img || null;
      for (const [k, v] of Object.entries(o.sub || {})) {
        if (sel.includes(k) && v.length > 0) return v[0];
      }
      return null;
    },
    querySelectorAll: (sel) => {
      const out = [];
      for (const [k, v] of Object.entries(o.sub || {})) {
        if (sel.includes(k)) out.push(...v);
      }
      return out;
    },
    closest: (sel) => (o.closestMap && o.closestMap[sel]) || null,
    click: () => { if (o.onClick) o.onClick(node); },
    _style: { visibility: 'visible', display: 'block', opacity: '1', pointerEvents: 'auto' },
  };
  return node;
}

function makeGenerator(opts) {
  const modeBox = makeEl({ attrs: { role: 'combobox', 'aria-label': '创作类型' }, text: opts.mode || '图片生成' });
  const modelBox = makeEl({ attrs: { role: 'combobox', 'aria-label': '模型' }, text: opts.model || 'Seedream 4.7' });
  const ratioBtn = makeEl({ tag: 'BUTTON', text: opts.ratio || '3:4 · 2K' });
  const actualNodes = opts.actualText ? [makeEl({ cls: 'actual-credits-Ou1oP4', text: opts.actualText })] : [];
  const priceDisplayNodes = opts.priceDisplayText ? [makeEl({ cls: 'credit-price-display-c3e7hY', text: opts.priceDisplayText })] : [];
  return makeEl({
    text: opts.genText || '',
    sub: {
      'role="combobox"': [modeBox, modelBox],
      'button': [ratioBtn],
      'actual-credits-': actualNodes,
      'credit-price-display-': priceDisplayNodes,
      'toolbar-actions-': opts.toolbarActionsText ? [makeEl({ cls: 'toolbar-actions-z_qZdq', text: opts.toolbarActionsText })] : [],
    },
  });
}

async function runCase(cfg) {
  let fetchCalls = 0;
  const ctx = {
    location: { hostname: cfg.hostname || 'jimeng.jianying.com' },
    AbortController: global.AbortController,
    setTimeout,
    clearTimeout,
    getComputedStyle: (el) => el._style || { visibility: 'visible', display: 'block', opacity: '1', pointerEvents: 'auto' },
    fetch: async (url) => {
      fetchCalls++;
      const payload = (cfg.api || {})[url];
      if (!payload) return { ok: false, status: 404 };
      return { ok: true, json: async () => payload };
    },
    document: {
      querySelector: (sel) => {
        for (const [k, v] of Object.entries(cfg.dom || {})) {
          if (sel.includes(k) && v.length > 0) return v[0];
        }
        return null;
      },
      querySelectorAll: (sel) => {
        const out = [];
        for (const [k, v] of Object.entries(cfg.dom || {})) {
          if (sel.includes(k)) out.push(...v);
        }
        return out;
      },
    },
  };
  const res = await vm.runInNewContext(probeSrc, ctx);
  res._fetchCalls = fetchCalls;
  return res;
}

async function runTargetCostSwitchCase() {
  const state = {
    mode: '图片生成',
    model: 'Seedream 5.5',
    ratio: '智能比例 1.5K',
    actual: '4/张',
    priceDisplay: '4/张 5/张',
    modelPopupOpen: false,
  };
  const modeBox = makeEl({ attrs: { role: 'combobox', 'aria-label': '创作类型' }, text: state.mode });
  const modelBox = makeEl({
    attrs: { role: 'combobox', 'aria-label': '模型' },
    text: state.model,
    onClick: () => { state.modelPopupOpen = true; },
  });
  const ratioBtn = makeEl({ tag: 'BUTTON', text: state.ratio });
  const actualEl = makeEl({ cls: 'actual-credits-Ou1oP4', text: state.actual });
  const priceDisplayEl = makeEl({ cls: 'credit-price-display-c3e7hY', text: state.priceDisplay });

  const gen = makeEl({
    sub: {
      'role="combobox"': [modeBox, modelBox],
      'button': [ratioBtn],
      'actual-credits-': [actualEl],
      'credit-price-display-': [priceDisplayEl],
    },
  });

  const opt55 = makeEl({
    attrs: { role: 'option' },
    text: 'Seedream 5.5',
    sub: { 'option-label-': [makeEl({ text: 'Seedream 5.5' })] },
  });
  const opt47 = makeEl({
    attrs: { role: 'option' },
    text: 'Seedream 4.7 画质全面优化',
    sub: { 'option-label-': [makeEl({ text: 'Seedream 4.7' })] },
    onClick: () => {
      state.model = 'Seedream 4.7';
      state.ratio = '智能比例 2K';
      modelBox.innerText = modelBox.textContent = state.model;
      ratioBtn.innerText = ratioBtn.textContent = state.ratio;
      state.modelPopupOpen = false;
      // 模拟网页切换模型后异步刷新单张积分（先短暂保留旧价，120ms 后更新为 1/张）
      setTimeout(() => {
        state.actual = '1/张';
        state.priceDisplay = '1/张';
        actualEl.innerText = actualEl.textContent = state.actual;
        priceDisplayEl.innerText = priceDisplayEl.textContent = state.priceDisplay;
      }, 120);
    },
  });
  const popup = makeEl({
    cls: 'lv-select-popup',
    attrs: { role: 'listbox' },
    sub: { 'role="option"': [opt55, opt47] },
  });

  const ctx = {
    location: { hostname: 'jimeng.jianying.com' },
    Date,
    setTimeout,
    clearTimeout,
    KeyboardEvent: function (t, o) { return Object.assign({ type: t }, o || {}); },
    getComputedStyle: (el) => el._style || { visibility: 'visible', display: 'block', opacity: '1', pointerEvents: 'auto' },
    document: {
      dispatchEvent: (ev) => { if (ev && ev.key === 'Escape') state.modelPopupOpen = false; },
      querySelector: (sel) => (sel.includes('dreamina.generator') ? gen : null),
      querySelectorAll: (sel) => {
        if (sel.includes('lv-select-popup') && state.modelPopupOpen) return [popup];
        return [];
      },
    },
  };
  return await vm.runInNewContext(targetCostSrc, ctx);
}

(async () => {
  const baseAPI = {
    '/commerce/v1/benefits/user_credit': {
      ret: '0',
      data: {
        credit: { gift_credit: 60, purchase_credit: 34, vip_credit: 0 },
      },
    },
    '/commerce/v3/subscription/get_user_lite_info': {
      ret: '0',
      data: { is_vip: false },
    },
    '/passport/account/info/v2/?aid=513695&account_sdk_source=web&sdk_version=2.2.6': {
      message: 'success',
      data: { user_id: 12345, screen_name: '即梦设计师' },
    },
  };
  const out = {
    wrongHost: await runCase({ hostname: 'www.doubao.com' }),
    apiSuccess: await runCase({
      api: baseAPI,
      dom: {
        'dreamina.generator': [makeGenerator({
          mode: '图片生成',
          model: 'Seedream 4.7',
          ratio: '3:4 · 2K',
          actualText: '1/张',
          priceDisplayText: '1/张',
        })],
      },
    }),
    defaultSeedream55Ignored: await runCase({
      api: baseAPI,
      dom: {
        'dreamina.generator': [makeGenerator({
          mode: '图片生成',
          model: 'Seedream 5.5',
          ratio: '智能比例 1.5K',
          actualText: '4/张',
          priceDisplayText: '4/张 5/张',
          genText: '图片生成 Seedream 5.5 智能比例 1.5K 4张 4/张 5/张',
        })],
      },
    }),
    resolution4KIgnored: await runCase({
      api: baseAPI,
      dom: {
        'dreamina.generator': [makeGenerator({
          mode: '图片生成',
          model: 'Seedream 4.7',
          ratio: '3:4 · 4K',
          actualText: '2/张',
          priceDisplayText: '2/张',
        })],
      },
    }),
    expiredDetailSkipped: await runCase({
      api: {
        '/commerce/v1/benefits/user_credit': {
          ret: '0',
          data: {
            credit: { gift_credit: 5, purchase_credit: 0, vip_credit: 0 },
            credit_detail_list: [
              { residual_credits: 0, total_credits: 60, credits_life_end: Math.floor(Date.now() / 1000) - 3600 },
              { residual_credits: 5, total_credits: 60, credits_life_end: Math.floor(Date.now() / 1000) + 7200 },
            ],
          },
        },
        '/commerce/v3/subscription/get_user_lite_info': { ret: '0', data: { is_vip: false } },
      },
    }),
    promptTextNotMistakenForPrice: await runCase({
      api: baseAPI,
      dom: {
        'dreamina.generator': [makeGenerator({
          mode: '图片生成',
          model: 'Seedream 4.7',
          ratio: '3:4 · 2K',
          genText: '请生成每组 9/张 风格化插画 图片生成 Seedream 4.7 3:4 · 2K 6张',
        })],
      },
    }),
    domFallback: await runCase({
      api: {},
      dom: {
        'data-main-frame-2026-credit-display-anchor': [makeEl({ text: '✦ 94' })],
        'data-main-frame-2026-avatar-action': [makeEl({ img: makeEl({ attrs: { alt: '创意画师' } }) })],
      },
    }),
    loggedOutBlocked: await runCase({
      api: {},
      dom: {
        'login-button-wrapper-': [makeEl({ text: '登录' })],
        'data-main-frame-2026-credit-display-anchor': [makeEl({ text: '94' })],
      },
    }),
    targetCostSwitch: await runTargetCostSwitchCase(),
  };
  process.stdout.write(JSON.stringify(out));
})().catch((e) => { console.error(e); process.exit(1); });
`
	raw, err := exec.Command(nodeBin, "-e", harness).CombinedOutput()
	if err != nil {
		t.Fatalf("node harness failed: %v\n%s", err, string(raw))
	}
	var got map[string]struct {
		JimengStatus
		FetchCalls int `json:"_fetchCalls"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, string(raw))
	}
	if got["wrongHost"].OK || got["wrongHost"].FetchCalls != 0 {
		t.Fatalf("wrongHost should abort without fetch: %+v", got["wrongHost"])
	}
	api := got["apiSuccess"]
	if !api.OK || !api.HasCredits || api.CreditsRemain != 94 || api.Username != "即梦设计师" || api.CreditCost != 1 || api.PageCreditCost != 1 || !api.CostModelMatched || api.Model != "Seedream 4.7" || api.Resolution != "2K" || api.Source != "user_credit" {
		t.Fatalf("apiSuccess mismatch: %+v", api)
	}
	s55 := got["defaultSeedream55Ignored"]
	if !s55.OK || !s55.HasCredits || s55.CreditsRemain != 94 || s55.CreditCost != 0 || s55.PageCreditCost != 0 || s55.CostModelMatched || s55.Model != "Seedream 5.5" || s55.Resolution != "1.5K" {
		t.Fatalf("defaultSeedream55Ignored must NOT adopt Seedream 5.5 4/张 price: %+v", s55)
	}
	r4k := got["resolution4KIgnored"]
	if !r4k.OK || r4k.CreditCost != 0 || r4k.PageCreditCost != 2 || r4k.CostModelMatched || r4k.Resolution != "4K" {
		t.Fatalf("resolution4KIgnored must keep default CreditCost=0 while exposing live 4K PageCreditCost=2: %+v", r4k)
	}
	expDetail := got["expiredDetailSkipped"]
	if !expDetail.OK || !expDetail.HasCredits || expDetail.CreditsRemain != 5 || expDetail.CreditsResetAt <= time.Now().Unix() {
		t.Fatalf("expiredDetailSkipped should skip past bucket and pick future reset timestamp: %+v", expDetail)
	}
	promptOnly := got["promptTextNotMistakenForPrice"]
	if !promptOnly.OK || !promptOnly.CostModelMatched || promptOnly.CreditCost != 0 {
		t.Fatalf("promptTextNotMistakenForPrice must NOT parse prompt text as price: %+v", promptOnly)
	}
	dom := got["domFallback"]
	if !dom.OK || !dom.HasCredits || dom.CreditsRemain != 94 || dom.Username != "创意画师" {
		t.Fatalf("domFallback mismatch: %+v", dom)
	}
	if got["loggedOutBlocked"].OK || got["loggedOutBlocked"].HasCredits {
		t.Fatalf("loggedOutBlocked should ignore DOM credits when login button visible: %+v", got["loggedOutBlocked"])
	}
	sw := got["targetCostSwitch"]
	if !sw.OK || sw.CreditCost != 1 || sw.Model != "Seedream 4.7" || sw.Resolution != "2K" {
		t.Fatalf("targetCostSwitch should switch from 5.5 (4/张) to 4.7 (1/张) without reading stale price: %+v", sw)
	}
}
