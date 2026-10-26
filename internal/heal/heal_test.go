package heal

import (
	"errors"
	"testing"
	"time"
)

func TestDecideCrashRelaunchThenCap(t *testing.T) {
	label := "账号1"
	again := Decide(Input{Kind: KindCrash, Attempt: 1, MaxAttempts: 2, Label: label})
	if again.Action != ActionRelaunch || again.Log != "账号1 浏览器崩溃，已自动重启" || again.Wait != 0 {
		t.Fatalf("relaunch = %+v", again)
	}
	done := Decide(Input{Kind: KindCrash, Attempt: 2, MaxAttempts: 2, Label: label})
	if done.Action != ActionFail || done.Reason != "浏览器崩溃，自动重启后仍失败" {
		t.Fatalf("cap = %+v", done)
	}
	once := Decide(Input{Kind: KindCrash, Attempt: 1, MaxAttempts: 1, Label: label})
	if once.Action != ActionFail || once.Reason != "浏览器崩溃了" {
		t.Fatalf("no retry = %+v", once)
	}
}

func TestDecideStuckReloadsUntilCap(t *testing.T) {
	got := Decide(Input{Kind: KindStuck, Attempt: 1, MaxAttempts: 3, Label: "账号1"})
	if got.Action != ActionReload || got.Log != "账号1 页面卡住，已刷新重试" {
		t.Fatalf("reload = %+v", got)
	}
	done := Decide(Input{Kind: KindStuck, Attempt: 3, MaxAttempts: 3, Label: "账号1"})
	if done.Action != ActionFail || done.Reason != "页面长时间没有进展" {
		t.Fatalf("stuck cap = %+v", done)
	}
}

func TestDecideNetworkBackoff(t *testing.T) {
	first := Decide(Input{Kind: KindNetwork, Attempt: 1, MaxAttempts: 4, Label: "账号1"})
	if first.Action != ActionBackoff || first.Wait != 2*time.Second || first.Log != "账号1 网络中断，稍后自动重试" {
		t.Fatalf("backoff = %+v", first)
	}
	if got := Backoff(2); got != 4*time.Second {
		t.Fatalf("second = %s", got)
	}
	if got := Backoff(3); got != 8*time.Second {
		t.Fatalf("third = %s", got)
	}
	if got := Backoff(6); got != 8*time.Second {
		t.Fatalf("cap = %s", got)
	}
	done := Decide(Input{Kind: KindNetwork, Attempt: 4, MaxAttempts: 4, Label: "账号1"})
	if done.Action != ActionFail || done.Reason != "网络中断，请重试" {
		t.Fatalf("net cap = %+v", done)
	}
}

func TestDecideVerificationSliderThenSwitchOrPause(t *testing.T) {
	label := "账号2"
	slide := Decide(Input{Kind: KindVerify, Attempt: 1, MaxAttempts: 1, SliderAvailable: true, Label: label, OtherHealthy: true})
	if slide.Action != ActionTrySlider || slide.Log != "账号2 遇到滑块，正在自动拖动" {
		t.Fatalf("slider = %+v", slide)
	}
	switched := Decide(Input{Kind: KindVerify, Attempt: 1, MaxAttempts: 1, SliderAvailable: true, SliderTried: true, OtherHealthy: true, Label: label})
	if switched.Action != ActionSwitch || !switched.Focus || switched.Notice != "账号2 需要完成安全验证" {
		t.Fatalf("switch = %+v", switched)
	}
	if switched.Log != "账号2 验证未通过，已换到其他账号" {
		t.Fatalf("switch log = %q", switched.Log)
	}
	paused := Decide(Input{Kind: KindVerify, Attempt: 2, MaxAttempts: 3, SliderTried: true, Label: label})
	if paused.Action != ActionPause || paused.Reason != "需要在网页里完成验证" || !paused.Focus {
		t.Fatalf("pause = %+v", paused)
	}
	sent := Decide(Input{Kind: KindVerify, Attempt: 1, MaxAttempts: 2, SliderTried: true, OtherHealthy: true, Submitted: true, Label: label})
	if sent.Action != ActionPause {
		t.Fatalf("submitted verification must stay on the account: %+v", sent)
	}
	noSlider := Decide(Input{Kind: KindVerify, Attempt: 1, MaxAttempts: 2, OtherHealthy: true, Label: "账号1"})
	if noSlider.Action != ActionSwitch {
		t.Fatalf("without a slider, switch immediately: %+v", noSlider)
	}
}

func TestSentinels(t *testing.T) {
	if !errors.Is(ErrCrashed, ErrCrashed) || ErrStuck.Error() == "" || ErrNetwork.Error() == "" {
		t.Fatal("sentinels should stay comparable")
	}
}
