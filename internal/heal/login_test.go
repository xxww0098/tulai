package heal

import (
	"errors"
	"testing"
	"time"
)

func TestLoginDue(t *testing.T) {
	now := time.Date(2026, 10, 26, 15, 0, 0, 0, time.Local)
	if !Due(time.Time{}, now, false) || !Due(time.Time{}, now, true) {
		t.Fatal("first check should run")
	}
	if Due(now, now.Add(4*time.Second), true) {
		t.Fatal("dispatch check should wait out the short gap")
	}
	if !Due(now, now.Add(LoginMinGap), true) {
		t.Fatal("dispatch check is due at the short gap")
	}
	if Due(now, now.Add(44*time.Second), false) {
		t.Fatal("periodic check is early")
	}
	if !Due(now, now.Add(LoginInterval), false) {
		t.Fatal("periodic check is due at the interval")
	}
}

func TestObserveLogoutVerifyAndRestore(t *testing.T) {
	out := Observe("", StateOut, "账号2")
	if out.Mark != "logout" || out.Notice != "账号2 需要重新扫码登录" || !out.Focus {
		t.Fatalf("logout = %+v", out)
	}
	if out.Log != "账号2 已退出登录，请重新扫码" {
		t.Fatalf("logout log = %q", out.Log)
	}
	again := Observe(StateOut, StateOut, "账号2")
	if again.Mark != "" || again.Notice != "" {
		t.Fatalf("repeat logout should stay quiet: %+v", again)
	}

	challenge := Observe(StateIn, StateChallenge, "账号2")
	if challenge.Mark != "verify" || challenge.Notice != "账号2 需要完成安全验证" || !challenge.Focus {
		t.Fatalf("verify = %+v", challenge)
	}
	if Observe(StateChallenge, StateChallenge, "账号2").Mark != "" {
		t.Fatal("repeat verification should stay quiet")
	}

	back := Observe(StateOut, StateIn, "账号2")
	if back.Mark != "restore" || !back.ClearNotice || back.Log != "账号2 已恢复，回到轮换" || back.Focus {
		t.Fatalf("restore = %+v", back)
	}
	if Observe(StateChallenge, StateIn, "账号1").Mark != "restore" {
		t.Fatal("clearing a challenge should restore rotation")
	}
	if Observe(StateIn, StateIn, "账号2").Mark != "" {
		t.Fatal("staying logged in should not reset the account")
	}
	if Observe("", StateIn, "账号2").Mark != "confirm" {
		t.Fatal("first healthy read should confirm the login")
	}
}

func TestObserveIgnoresUnknownAndDialErrors(t *testing.T) {
	if got := Observe(StateIn, StateUnknown, "账号1"); got.Mark != "" || got.Notice != "" {
		t.Fatalf("unknown = %+v", got)
	}
	if got := NormalizeState("nope", nil); got != StateUnknown {
		t.Fatalf("normalize = %q", got)
	}
	if got := NormalizeState(StateOut, errors.New("connection refused")); got != StateDown {
		t.Fatalf("dial error = %q", got)
	}
	if got := Observe(StateIn, StateDown, "账号1"); got.Mark != "" {
		t.Fatalf("down = %+v", got)
	}
	if got := NormalizeState("  challenge ", nil); got != StateChallenge {
		t.Fatalf("trim = %q", got)
	}
}
