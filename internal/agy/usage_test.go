package agy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseUsageFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseUsage(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.FiveHour.Remaining != 1 {
		t.Fatalf("5h remaining = %v", got.FiveHour.Remaining)
	}
	want5 := time.Date(2026, 10, 30, 16, 42, 18, 0, time.UTC)
	if !got.FiveHour.Reset.Equal(want5) {
		t.Fatalf("5h reset = %s", got.FiveHour.Reset)
	}
	if got.Weekly.Remaining < 0.97 || got.Weekly.Remaining > 0.98 {
		t.Fatalf("weekly remaining = %v", got.Weekly.Remaining)
	}
	wantWeek := time.Date(2026, 11, 5, 2, 12, 24, 0, time.UTC)
	if !got.Weekly.Reset.Equal(wantWeek) {
		t.Fatalf("weekly reset = %s", got.Weekly.Reset)
	}
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 10, 30, 18, 0, 0, 0, loc)
	if tip := WindowTip(Window5h, got.FiveHour.Remaining, got.FiveHour.Reset, now); tip != "5小时：已用 0%，00:42 重置" {
		t.Fatalf("5h tip = %s", tip)
	}
	if tip := WindowTip(WindowWeek, got.Weekly.Remaining, got.Weekly.Reset, now); tip != "7天：已用 3%，11-05 10:12 重置" {
		t.Fatalf("week tip = %s", tip)
	}
	if _, hold := QuotaHold(got); hold {
		t.Fatal("样本还有剩余，不应按额度不足")
	}
}

func TestParseUsageUnavailable(t *testing.T) {
	if _, err := ParseUsage([]byte("Authentication required. paste the code")); !errorsIsLogin(err) {
		t.Fatalf("auth = %v", err)
	}
	if _, err := ParseUsage([]byte("not json")); err == nil || err.Error() != "额度数据无法识别" {
		t.Fatalf("junk = %v", err)
	}
	body := []byte(`{"command":{"data":{"groups":[{"name":"Claude and GPT models","buckets":[{"window":"5h","remaining_fraction":0,"reset_time":"2026-10-30T16:42:18Z"},{"window":"weekly","remaining_fraction":0,"reset_time":"2026-11-05T02:12:24Z"}]}]}}}`)
	if _, err := ParseUsage(body); err == nil || err.Error() != "没有 Gemini 额度" {
		t.Fatalf("other group = %v", err)
	}
	missing := []byte(`{"command":{"data":{"groups":[{"name":"Gemini Models","buckets":[{"window":"5h","remaining_fraction":1,"reset_time":"2026-10-30T16:42:18Z"}]}]}}}`)
	if _, err := ParseUsage(missing); err == nil || err.Error() != "没有 Gemini 额度" {
		t.Fatalf("one window = %v", err)
	}
	view, ok := ViewFrom(GeminiUsage{}, errUsageTimeout, time.Now())
	if !ok || view.State != UsageFailed || view.Reason != "读取额度失败：超时" {
		t.Fatalf("timeout view %+v ok=%v", view, ok)
	}
	if _, ok := ViewFrom(GeminiUsage{}, errUsageCanceled, time.Now()); ok {
		t.Fatal("cancel should not become a failure row")
	}
	login, ok := ViewFrom(GeminiUsage{}, ErrNeedLogin, time.Now())
	if !ok || login.Reason != ReasonLogin {
		t.Fatalf("login view %+v", login)
	}
}

func TestQuotaHoldUsesLaterReset(t *testing.T) {
	soon := time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC)
	later := time.Date(2026, 11, 5, 2, 0, 0, 0, time.UTC)
	until, hold := QuotaHold(GeminiUsage{
		FiveHour: UsageWindow{Remaining: 0, Reset: soon},
		Weekly:   UsageWindow{Remaining: 0, Reset: later},
	})
	if !hold || !until.Equal(later) {
		t.Fatalf("both empty: hold=%v until=%s", hold, until)
	}
	until, hold = QuotaHold(GeminiUsage{
		FiveHour: UsageWindow{Remaining: 0, Reset: soon},
		Weekly:   UsageWindow{Remaining: 0.4, Reset: later},
	})
	if !hold || !until.Equal(soon) {
		t.Fatalf("5h empty: hold=%v until=%s", hold, until)
	}
	if _, hold = QuotaHold(GeminiUsage{
		FiveHour: UsageWindow{Remaining: 0.2, Reset: soon},
		Weekly:   UsageWindow{Remaining: 0.2, Reset: later},
	}); hold {
		t.Fatal("both remain")
	}
}

func TestFetchUsageReadsFixture(t *testing.T) {
	home := t.TempDir()
	writeToken(t, home)
	data, err := os.ReadFile(filepath.Join("testdata", "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "usage.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "mode.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", "/tmp/not-the-account")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	got, err := fetchUsage(context.Background(), usageScript(t), home, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.FiveHour.Remaining != 1 || got.Weekly.Remaining <= 0 {
		t.Fatalf("%+v", got)
	}
	if seen := strings.TrimSpace(readUsageFile(t, home, "seen-home.txt")); seen != home {
		t.Fatalf("HOME = %s", seen)
	}
	if seen := strings.TrimSpace(readUsageFile(t, home, "seen-proxy.txt")); seen != "http://127.0.0.1:9" {
		t.Fatalf("proxy = %s", seen)
	}
	cwd := strings.TrimSpace(readUsageFile(t, home, "seen-cwd.txt"))
	if cwd == home || !strings.Contains(cwd, "agy-usage-") {
		t.Fatalf("cwd = %s", cwd)
	}
	args := readUsageFile(t, home, "seen-args.txt")
	if !strings.Contains(args, "-p") || !strings.Contains(args, "/usage") || !strings.Contains(args, "--output-format") || !strings.Contains(args, "json") {
		t.Fatalf("args = %s", args)
	}
	if strings.Contains(args, "--model") {
		t.Fatalf("usage should not call a model: %s", args)
	}
}

func TestFetchUsageKillsAuthAndIgnoresStdin(t *testing.T) {
	home := t.TempDir()
	writeToken(t, home)
	if err := os.WriteFile(filepath.Join(home, "mode.txt"), []byte("auth"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := fetchUsage(context.Background(), usageScript(t), home, 5*time.Second)
	if !errorsIsLogin(err) {
		t.Fatalf("auth err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("auth wait %s", time.Since(start))
	}
	if _, statErr := os.Stat(filepath.Join(home, "alive.txt")); !os.IsNotExist(statErr) {
		t.Fatal("process kept running after authentication required")
	}

	home = t.TempDir()
	writeToken(t, home)
	if err := os.WriteFile(filepath.Join(home, "mode.txt"), []byte("stdin"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = fetchUsage(context.Background(), usageScript(t), home, 5*time.Second)
	if !errorsIsLogin(err) {
		t.Fatalf("stdin err = %v", err)
	}
	buf, statErr := os.ReadFile(filepath.Join(home, "stdin.txt"))
	if statErr == nil && strings.TrimSpace(string(buf)) != "none" {
		t.Fatalf("stdin got %q", buf)
	}
}

func TestFetchUsageTimeoutAndGarbage(t *testing.T) {
	home := t.TempDir()
	writeToken(t, home)
	if err := os.WriteFile(filepath.Join(home, "mode.txt"), []byte("sleep"), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := fetchUsage(context.Background(), usageScript(t), home, 300*time.Millisecond)
	if err == nil || err.Error() != "超时" {
		t.Fatalf("timeout = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("timeout took %s", time.Since(start))
	}

	home = t.TempDir()
	writeToken(t, home)
	if err := os.WriteFile(filepath.Join(home, "mode.txt"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = fetchUsage(context.Background(), usageScript(t), home, 5*time.Second)
	if err == nil || strings.Contains(err.Error(), "secret") || err.Error() == "" {
		t.Fatalf("bad = %v", err)
	}
	if _, err := fetchUsage(context.Background(), filepath.Join(t.TempDir(), "missing-agy"), home, time.Second); err == nil || err.Error() != "找不到 agy" {
		t.Fatalf("missing bin = %v", err)
	}

	home = t.TempDir()
	writeToken(t, home)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = fetchUsage(ctx, usageScript(t), home, 5*time.Second)
	if !errors.Is(err, errUsageCanceled) {
		t.Fatalf("cancel = %v", err)
	}
}

func errorsIsLogin(err error) bool {
	return err != nil && err.Error() == ErrNeedLogin.Error()
}

func usageScript(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agy-usage.sh")
	if err := os.WriteFile(path, []byte(usageScriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readUsageFile(t *testing.T, home, name string) string {
	t.Helper()
	buf, err := os.ReadFile(filepath.Join(home, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}

const usageScriptBody = `#!/bin/sh
printf '%s\n' "$PWD" > "$HOME/seen-cwd.txt"
printf '%s\n' "$HOME" > "$HOME/seen-home.txt"
printf '%s\n' "${HTTP_PROXY-}" > "$HOME/seen-proxy.txt"
printf '%s\n' "$*" > "$HOME/seen-args.txt"
mode=ok
if [ -f "$HOME/mode.txt" ]; then
  mode=$(tr -d '[:space:]' < "$HOME/mode.txt")
fi
case "$mode" in
auth)
  printf '%s\n' 'Authentication required. Please visit the URL'
  exec sleep 30
  ;;
stdin)
  printf '%s\n' 'Authentication required'
  if IFS= read -r line; then
    printf '%s\n' "$line" > "$HOME/stdin.txt"
  else
    printf 'none\n' > "$HOME/stdin.txt"
  fi
  exit 0
  ;;
sleep)
  exec sleep 30
  ;;
bad)
  printf '%s\n' 'not-json secret-should-not-leak'
  exit 1
  ;;
*)
  cat "$HOME/usage.json"
  exit 0
  ;;
esac
`
