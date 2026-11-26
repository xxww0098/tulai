package tasks

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDecideHealCrashRelaunchThenCap(t *testing.T) {
	label := "账号1"
	again := decideHeal(healInput{Kind: healKindCrash, Attempt: 1, MaxAttempts: 2, Label: label})
	if again.Action != healActionRelaunch || again.Log != "账号1 浏览器崩溃，已自动重启" || again.Wait != 0 {
		t.Fatalf("relaunch = %+v", again)
	}
	done := decideHeal(healInput{Kind: healKindCrash, Attempt: 2, MaxAttempts: 2, Label: label})
	if done.Action != healActionFail || done.Reason != "浏览器崩溃，自动重启后仍失败" {
		t.Fatalf("cap = %+v", done)
	}
	once := decideHeal(healInput{Kind: healKindCrash, Attempt: 1, MaxAttempts: 1, Label: label})
	if once.Action != healActionFail || once.Reason != "浏览器崩溃了" {
		t.Fatalf("no retry = %+v", once)
	}
}

func TestDecideHealStuckReloadsUntilCap(t *testing.T) {
	got := decideHeal(healInput{Kind: healKindStuck, Attempt: 1, MaxAttempts: 3, Label: "账号1"})
	if got.Action != healActionReload || got.Log != "账号1 页面卡住，已刷新重试" {
		t.Fatalf("reload = %+v", got)
	}
	done := decideHeal(healInput{Kind: healKindStuck, Attempt: 3, MaxAttempts: 3, Label: "账号1"})
	if done.Action != healActionFail || done.Reason != "页面长时间没有进展" {
		t.Fatalf("stuck cap = %+v", done)
	}
}

func TestDecideHealNetworkBackoff(t *testing.T) {
	first := decideHeal(healInput{Kind: healKindNetwork, Attempt: 1, MaxAttempts: 4, Label: "账号1"})
	if first.Action != healActionBackoff || first.Wait != 2*time.Second || first.Log != "账号1 网络中断，稍后自动重试" {
		t.Fatalf("backoff = %+v", first)
	}
	if got := healBackoff(2); got != 4*time.Second {
		t.Fatalf("second = %s", got)
	}
	if got := healBackoff(3); got != 8*time.Second {
		t.Fatalf("third = %s", got)
	}
	if got := healBackoff(6); got != 8*time.Second {
		t.Fatalf("cap = %s", got)
	}
	done := decideHeal(healInput{Kind: healKindNetwork, Attempt: 4, MaxAttempts: 4, Label: "账号1"})
	if done.Action != healActionFail || done.Reason != "网络中断，请重试" {
		t.Fatalf("net cap = %+v", done)
	}
}

func TestDecideHealVerificationSliderThenSwitchOrPause(t *testing.T) {
	label := "账号2"
	slide := decideHeal(healInput{Kind: healKindVerify, Attempt: 1, MaxAttempts: 1, SliderAvailable: true, Label: label, OtherHealthy: true})
	if slide.Action != healActionTrySlider || slide.Log != "账号2 遇到滑块，正在自动拖动" {
		t.Fatalf("slider = %+v", slide)
	}
	switched := decideHeal(healInput{Kind: healKindVerify, Attempt: 1, MaxAttempts: 1, SliderAvailable: true, SliderTried: true, OtherHealthy: true, Label: label})
	if switched.Action != healActionSwitch || !switched.Focus || switched.Notice != "账号2 需要完成安全验证" {
		t.Fatalf("switch = %+v", switched)
	}
	if switched.Log != "账号2 验证未通过，已换到其他账号" {
		t.Fatalf("switch log = %q", switched.Log)
	}
	paused := decideHeal(healInput{Kind: healKindVerify, Attempt: 2, MaxAttempts: 3, SliderTried: true, Label: label})
	if paused.Action != healActionPause || paused.Reason != "需要在网页里完成验证" || !paused.Focus {
		t.Fatalf("pause = %+v", paused)
	}
	sent := decideHeal(healInput{Kind: healKindVerify, Attempt: 1, MaxAttempts: 2, SliderTried: true, OtherHealthy: true, Submitted: true, Label: label})
	if sent.Action != healActionPause {
		t.Fatalf("submitted verification must stay on the account: %+v", sent)
	}
	noSlider := decideHeal(healInput{Kind: healKindVerify, Attempt: 1, MaxAttempts: 2, OtherHealthy: true, Label: "账号1"})
	if noSlider.Action != healActionSwitch {
		t.Fatalf("without a slider, switch immediately: %+v", noSlider)
	}
}

func TestHealSentinels(t *testing.T) {
	if !errors.Is(errHealCrashed, errHealCrashed) || errHealStuck.Error() == "" || errHealNetwork.Error() == "" {
		t.Fatal("sentinels should stay comparable")
	}
}

func TestLoginProbeDue(t *testing.T) {
	now := time.Date(2026, 10, 26, 15, 0, 0, 0, time.Local)
	if !loginProbeDue(time.Time{}, now, false) || !loginProbeDue(time.Time{}, now, true) {
		t.Fatal("first check should run")
	}
	if loginProbeDue(now, now.Add(4*time.Second), true) {
		t.Fatal("dispatch check should wait out the short gap")
	}
	if !loginProbeDue(now, now.Add(loginMinGap), true) {
		t.Fatal("dispatch check is due at the short gap")
	}
	if loginProbeDue(now, now.Add(44*time.Second), false) {
		t.Fatal("periodic check is early")
	}
	if !loginProbeDue(now, now.Add(loginInterval), false) {
		t.Fatal("periodic check is due at the interval")
	}
}

func TestObserveLoginLogoutVerifyAndRestore(t *testing.T) {
	out := observeLoginState("", loginStateOut, "账号2")
	if out.Mark != "logout" || out.Notice != "账号2 需要重新扫码登录" || !out.Focus {
		t.Fatalf("logout = %+v", out)
	}
	if out.Log != "账号2 已退出登录，请重新扫码" {
		t.Fatalf("logout log = %q", out.Log)
	}
	again := observeLoginState(loginStateOut, loginStateOut, "账号2")
	if again.Mark != "" || again.Notice != "" {
		t.Fatalf("repeat logout should stay quiet: %+v", again)
	}

	challenge := observeLoginState(loginStateIn, loginStateChallenge, "账号2")
	if challenge.Mark != "verify" || challenge.Notice != "账号2 需要完成安全验证" || !challenge.Focus {
		t.Fatalf("verify = %+v", challenge)
	}
	if observeLoginState(loginStateChallenge, loginStateChallenge, "账号2").Mark != "" {
		t.Fatal("repeat verification should stay quiet")
	}

	back := observeLoginState(loginStateOut, loginStateIn, "账号2")
	if back.Mark != "restore" || !back.ClearNotice || back.Log != "账号2 已恢复，回到轮换" || back.Focus {
		t.Fatalf("restore = %+v", back)
	}
	if observeLoginState(loginStateChallenge, loginStateIn, "账号1").Mark != "restore" {
		t.Fatal("clearing a challenge should restore rotation")
	}
	if observeLoginState(loginStateIn, loginStateIn, "账号2").Mark != "" {
		t.Fatal("staying logged in should not reset the account")
	}
	if observeLoginState("", loginStateIn, "账号2").Mark != "confirm" {
		t.Fatal("first healthy read should confirm the login")
	}
}

func TestObserveLoginIgnoresUnknownAndDialErrors(t *testing.T) {
	if got := observeLoginState(loginStateIn, loginStateUnknown, "账号1"); got.Mark != "" || got.Notice != "" {
		t.Fatalf("unknown = %+v", got)
	}
	if got := normalizeLoginState("nope", nil); got != loginStateUnknown {
		t.Fatalf("normalize = %q", got)
	}
	if got := normalizeLoginState(loginStateOut, errors.New("connection refused")); got != loginStateDown {
		t.Fatalf("dial error = %q", got)
	}
	if got := observeLoginState(loginStateIn, loginStateDown, "账号1"); got.Mark != "" {
		t.Fatalf("down = %+v", got)
	}
	if got := normalizeLoginState("  challenge ", nil); got != loginStateChallenge {
		t.Fatalf("trim = %q", got)
	}
	region := observeLoginState(loginStateIn, loginStateRegion, "豆包")
	if region.Mark != "region" || region.Focus || !strings.Contains(region.Notice, "代理") {
		t.Fatalf("region = %+v", region)
	}
	if again := observeLoginState(loginStateRegion, loginStateRegion, "豆包"); again.Notice != "" {
		t.Fatalf("repeat region = %+v", again)
	}
	if back := observeLoginState(loginStateRegion, loginStateIn, "豆包"); back.Mark != "restore" || !back.ClearNotice {
		t.Fatalf("region cleared = %+v", back)
	}
}
