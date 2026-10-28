package tasks

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestClassifyFailure(t *testing.T) {
	cases := []struct {
		status Status
		raw    string
		class  FailClass
		button string
	}{
		{StatusFailed, "审核不通过: 内容不符合规范", FailModeration, "改提示词重试"},
		{StatusFailed, strategy.ErrModeration.Error(), FailModeration, "改提示词重试"},
		{StatusFailed, "E_MODERATION: 疑似敏感", FailModeration, "改提示词重试"},
		{StatusFailed, "登录已失效", FailAuth, "去重登"},
		{StatusFailed, "千问页面未登录，请在扩展打开的网页登录后再开始任务", FailAuth, "去重登"},
		{StatusFailed, "账号额度已用尽", FailQuota, "换账号重试"},
		{StatusFailed, "今日已达上限", FailQuota, "换账号重试"},
		{StatusFailed, "请求过于频繁", FailQuota, "换账号重试"},
		{StatusNeedsUser, "", FailCaptcha, "重试"},
		{StatusFailed, captcha.ErrVerificationRequired.Error(), FailCaptcha, "重试"},
		{StatusFailed, "需要安全验证", FailCaptcha, "重试"},
		{StatusFailed, "websocket: connection refused", FailNetwork, "重试"},
		{StatusFailed, "扩展账号未连接", FailNetwork, "重试"},
		{StatusFailed, "上传: 连接被拒绝", FailUpload, "换图"},
		{StatusFailed, "E_UPLOAD_FAIL: 源图是空文件", FailUpload, "换图"},
		{StatusFailed, "something broke", FailUnknown, "重试"},
		{StatusFailed, "", FailUnknown, "重试"},
	}
	for _, tc := range cases {
		got := ClassifyFailure(tc.status, tc.raw)
		if got.Class != tc.class || got.Button != tc.button || got.Sentence == "" {
			t.Errorf("Classify(%q) = %+v, want class %d button %s", tc.raw, got, tc.class, tc.button)
		}
	}
}

func TestSoftenPromptStripsWordsAndStaysMild(t *testing.T) {
	got := SoftenPrompt("血腥的战场，武器特写", []string{"血腥", "武器"})
	if strings.Contains(got, "血腥") || strings.Contains(got, "武器") || !strings.Contains(got, mildPhrase) {
		t.Fatalf("soften = %q", got)
	}
	again := SoftenPrompt(got, nil)
	if strings.Count(again, mildPhrase) != 1 {
		t.Fatalf("mild phrase repeated: %q", again)
	}
	if SoftenPrompt("   ", nil) == "" || !strings.Contains(SoftenPrompt("", DefaultRiskyWords()), "温和的插画") {
		t.Fatal("empty prompt should still become a mild sentence")
	}
}

func TestModerationRewritesTwiceThenStops(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts.RetryCount = 5
	e.opts.RiskyWords = []string{"血腥"}
	var prompts []string
	got := e.execute(context.Background(), Task{Index: 1, Prompt: "血腥的战场"}, func(_ context.Context, task *Task) error {
		prompts = append(prompts, task.Prompt)
		return strategy.ErrModeration
	})
	if len(prompts) != 3 {
		t.Fatalf("want original plus 2 rewrites, got %v", prompts)
	}
	if prompts[0] != "血腥的战场" || strings.Contains(prompts[1], "血腥") || strings.Contains(prompts[2], "血腥") {
		t.Fatalf("prompts = %v", prompts)
	}
	if !strings.Contains(prompts[1], mildPhrase) || prompts[1] != prompts[2] {
		t.Fatalf("second rewrite should keep the mild sentence: %v", prompts)
	}
	if !got.Rewritten || got.Status != StatusFailed || got.Attempts != 1 || !strings.Contains(got.Error, "审核不通过") {
		t.Fatalf("task = %+v", got)
	}
}

func TestJitterStayInBounds(t *testing.T) {
	base := 10 * time.Second
	if got := JitterWait(0, 0.4); got != 0 {
		t.Fatalf("zero base = %s", got)
	}
	if got := JitterWait(base, 0); got != 5*time.Second {
		t.Fatalf("low = %s", got)
	}
	if got := JitterWait(base, 1); got != 15*time.Second {
		t.Fatalf("high = %s", got)
	}
	for i := 0; i <= 20; i++ {
		got := JitterWait(base, float64(i)/20)
		if got < 5*time.Second || got > 15*time.Second {
			t.Fatalf("unit %d → %s", i, got)
		}
	}
}
