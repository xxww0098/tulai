package cdp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeDoubaoStatusFreePaidAndLimited(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 11, 27, 13, 30, 0, 0, loc)

	t.Run("free healthy account keeps CreditsRemain=-1 and tracks usagePercent", func(t *testing.T) {
		q, ok := NormalizeDoubaoStatus(DoubaoStatus{
			OK:                    true,
			HasActiveSubscription: false,
			IsFreeUser:            true,
			ShortName:             "免费体验",
			QuotaRemainingAmount:  "0",
			HasUsagePercent:       true,
			UsedPercent:           25,
			ResetCardCount:        1,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.MemberTier != "free" || q.MemberName != "免费体验" || q.CreditsRemain != -1 || q.UsagePercent != 25 || q.PlatformLimited || q.ResetCardCount != 1 {
			t.Fatalf("unexpected free doubao quota: %+v", q)
		}
	})

	t.Run("free rate-limited account marks PlatformLimited and reset timestamp", func(t *testing.T) {
		resumeAt := now.Add(20 * time.Minute).Unix()
		q, ok := NormalizeDoubaoStatus(DoubaoStatus{
			OK:              true,
			IsFreeUser:      true,
			HasUsagePercent: true,
			UsedPercent:     100,
			IsLimited:       true,
			RateLimited:     true,
			RateLimitTime:   resumeAt,
			RateLimitTips:   "发送过于频繁，请稍后再试",
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if !q.PlatformLimited || q.UsagePercent != 100 || q.CreditsResetAt != resumeAt || q.LimitTips != "发送过于频繁，请稍后再试" {
			t.Fatalf("unexpected limited doubao quota: %+v", q)
		}
	})

	t.Run("paid subscription parses remaining quota, total and renewal time", func(t *testing.T) {
		nextDeduct := time.Date(2026, 12, 27, 0, 0, 0, 0, loc).Unix()
		q, ok := NormalizeDoubaoStatus(DoubaoStatus{
			OK:                    true,
			HasActiveSubscription: true,
			IsFreeUser:            false,
			SKUKey:                "doubao_personal_std",
			ProductName:           "个人订阅",
			QuotaRemainingAmount:  "8420",
			NextDeductTime:        nextDeduct,
			HasUsagePercent:       true,
			UsedPercent:           6,
			ResetCardCount:        2,
		}, now)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if q.MemberTier != "vip" || q.MemberName != "标准订阅" || q.CreditsRemain != 8420 || q.CreditsTotal != 9000 {
			t.Fatalf("unexpected paid doubao quota: %+v", q)
		}
		if q.CreditsCycle != "monthly" || q.CreditsResetAt != nextDeduct || q.ResetCardCount != 2 {
			t.Fatalf("unexpected paid doubao reset/cards: %+v", q)
		}
	})
}

func TestDoubaoStatusProbeSendsUTF8EncodingHeaderForRateLimit(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	script := `
const assert = require('assert');
const fs = require('fs');
const vm = require('vm');
const probeJS = fs.readFileSync(process.argv[2], 'utf8');

async function runCase(rateResponse) {
  const seenHeaders = {};
  const ctx = vm.createContext({
    location: { hostname: 'www.doubao.com' },
    AbortController,
    setTimeout,
    clearTimeout,
    fetch: async (url, opts) => {
      seenHeaders[url] = opts && opts.headers ? opts.headers['content-type'] : '';
      if (url.includes('/im/message/send_rate_limit')) {
        return { ok: true, json: async () => rateResponse };
      }
      if (url.includes('/quota/summary/')) {
        return { ok: true, json: async () => ({ code: 0, data: { current_subscription: { display: { short_name: '免费体验' } } } }) };
      }
      if (url.includes('/card/list/')) {
        return { ok: true, json: async () => ({ code: 0, data: { limit_status: { is_free_user: true, is_limited: false, used_percent: 0 } } }) };
      }
      return { ok: true, json: async () => ({ code: 0, data: { available_count: 0 } }) };
    },
  });
  const out = await vm.runInContext(probeJS, ctx);
  return { out, seenHeaders };
}

(async () => {
  const okRes = await runCase({
    cmd: 2260,
    status_code: 0,
    status_desc: 'OK',
    downlink_body: {
      check_message_send_rate_limit_downlink_body: {
        is_limit: true,
        limit_time: 1800000000,
        limit_tips: '发送过于频繁，请稍后再试',
      },
    },
  });
  const rlURL = 'https://www.doubao.com/im/message/send_rate_limit';
  assert.strictEqual(okRes.seenHeaders[rlURL], 'application/json; encoding=utf-8', 'send_rate_limit 必须携带 encoding=utf-8 头');
  for (const [url, ct] of Object.entries(okRes.seenHeaders)) {
    if (url !== rlURL) assert.strictEqual(ct, 'application/json', '其他商业化接口保持 application/json');
  }
  assert.strictEqual(okRes.out.rateLimited, true);
  assert.strictEqual(okRes.out.rateLimitTime, 1800000000);
  assert.strictEqual(okRes.out.rateLimitTips, '发送过于频繁，请稍后再试');

  const badRes = await runCase({
    cmd: 0,
    status_code: 712012002,
    status_desc: '不支持编码类型',
    downlink_body: {
      check_message_send_rate_limit_downlink_body: { is_limit: true, limit_time: 123, limit_tips: 'ignored' },
    },
  });
  assert.strictEqual(badRes.out.rateLimited, false, '非 0 status_code 的频控响应不得误判为限流');
  console.log('doubao status probe ok');
})().catch(err => { console.error(err); process.exit(1); });
`
	dir := t.TempDir()
	probePath := filepath.Join(dir, "doubao_probe.js")
	if err := os.WriteFile(probePath, []byte(doubaoStatusProbeJS), 0o600); err != nil {
		t.Fatalf("write probe script: %v", err)
	}
	harnessPath := filepath.Join(dir, "doubao_probe_harness.cjs")
	if err := os.WriteFile(harnessPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write harness script: %v", err)
	}
	out, err := exec.Command("node", harnessPath, probePath).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "doubao status probe ok") {
		t.Fatalf("node probe check failed: err=%v out=%s", err, string(out))
	}
}
