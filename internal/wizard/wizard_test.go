package wizard

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestPlanBrowserUsesCamoufoxOnly(t *testing.T) {
	for _, platform := range []string{"windows", "darwin", "linux"} {
		need := PlanBrowser(platform, "amd64", []Found{{Kind: "Chrome", Path: "old"}}, false, true)
		if need.CanNext || !need.NeedsDownload || need.Backend != browser.KindCamoufox {
			t.Fatalf("missing: %+v", need)
		}
		ready := PlanBrowser(platform, "amd64", nil, true, true)
		if !ready.CanNext || ready.NeedsDownload || ready.Backend != browser.KindCamoufox {
			t.Fatalf("ready: %+v", ready)
		}
	}
	blocked := PlanBrowser("windows", "arm64", nil, false, false)
	if blocked.CanNext || blocked.NeedsDownload || blocked.Blocked == "" {
		t.Fatalf("blocked: %+v", blocked)
	}
}

func TestShouldShowSkipsHealthyAccounts(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	ready := Account{Platform: "豆包", Name: "账号1", LoggedIn: true, Health: "ready"}
	if ShouldShow("", []Account{ready}, now) {
		t.Fatal("a logged-in account should skip the wizard before the flag is written")
	}
	if ShouldShow(CompleteYes, []Account{ready}, now) {
		t.Fatal("completed wizard with a healthy account should stay closed")
	}
	for _, platform := range []string{"豆包", "千问"} {
		connected := Account{Platform: platform, Name: "扩展账号abcd", Extension: true, Health: "ready"}
		if !ShouldShow("", []Account{connected}, now) {
			t.Fatalf("%s extension that only connected should still show the wizard", platform)
		}
		signed := connected
		signed.LoggedIn = true
		if ShouldShow("", []Account{signed}, now) {
			t.Fatalf("logged-in %s extension should skip the wizard", platform)
		}
	}
	cooling := Account{Platform: "豆包", LoggedIn: true, Health: "cooling", CoolUntil: now.Unix() + 60}
	if ShouldShow(CompleteYes, []Account{cooling}, now) {
		t.Fatal("temporary cooling should not reopen the wizard")
	}
	recovered := Account{Platform: "豆包", LoggedIn: true, Health: "exhausted", CoolUntil: now.Unix() - 5}
	if ShouldShow("", []Account{recovered}, now) {
		t.Fatal("an account whose quota window already passed is usable again")
	}
}

func TestShouldShowWhenNothingUsable(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	if !ShouldShow("", nil, now) {
		t.Fatal("first launch with no accounts should show the wizard")
	}
	if !ShouldShow(CompleteYes, nil, now) {
		t.Fatal("a finished wizard with no remaining accounts should show again")
	}
	shell := Account{Platform: "豆包", Name: "账号1", Health: "ready"}
	if !ShouldShow(CompleteYes, []Account{shell}, now) {
		t.Fatal("an added profile that was never logged in is not usable")
	}
	signedOut := Account{Platform: "豆包", LoggedIn: true, Health: "exhausted", CoolUntil: 0}
	if !ShouldShow(CompleteYes, []Account{signedOut}, now) {
		t.Fatal("a login that will not recover on its own should bring the wizard back")
	}
	blank := Account{Name: "账号1", LoggedIn: true, Health: "ready"}
	if !ShouldShow("", []Account{blank}, now) {
		t.Fatal("a row without a platform does not count")
	}
}

func TestCompleteFlagPersistsInConfigDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database", "config.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Get(KeyComplete) == CompleteYes {
		t.Fatal("a new database is already marked complete")
	}
	st.Set(KeyComplete, CompleteYes)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Get(KeyComplete) != CompleteYes {
		t.Fatalf("complete flag = %q", again.Get(KeyComplete))
	}
}

func TestAccountNamesAndLoginAdvance(t *testing.T) {
	// 豆包占了两号之后，千问仍从 1 起。旧的「浏览器3 / 浏览器4」不再出现。
	if got := NextInstanceName("千问", []string{"豆包1", "豆包2"}); got != "千问1" {
		t.Fatalf("qianwen first = %q", got)
	}
	if got := NextInstanceName("千问", []string{"豆包1", "豆包2", "千问1"}); got != "千问2" {
		t.Fatalf("qianwen second = %q", got)
	}
	if got := NextInstanceName("千问", []string{"浏览器3", "浏览器4", "千问2"}); got != "千问1" {
		t.Fatalf("smallest free qianwen = %q", got)
	}
	if got := NextInstanceName("豆包", []string{"豆包1", "豆包2", "千问1", "千问2"}); got != "豆包3" {
		t.Fatalf("doubao third = %q", got)
	}
	if got := NextInstanceName("豆包", nil); got != "豆包1" {
		t.Fatalf("first name = %q", got)
	}
	q := NewQueue([]string{"豆包", "千问"})
	if q.Current() != "豆包" || q.Done() {
		t.Fatalf("queue start: %+v", q)
	}
	q, poll := AfterLogin(q, false)
	if poll != PollWait || q.Current() != "豆包" {
		t.Fatalf("not logged in: %+v %v", q, poll)
	}
	if LoggedIn("out") || LoggedIn("unknown") || LoggedIn(" \"in\" ") {
		t.Fatal("only an exact in probe counts as logged in")
	}
	if !LoggedIn("in") || !LoggedIn(" in ") {
		t.Fatal("in probe should count")
	}
	q, poll = AfterLogin(q, true)
	if poll != PollNext || q.Current() != "千问" {
		t.Fatalf("after doubao: %+v %v", q, poll)
	}
	q, poll = AfterLogin(q, true)
	if poll != PollDone || !q.Done() {
		t.Fatalf("after both: %+v %v", q, poll)
	}
}

func TestFlowIsPlatformFirstThenBrowserOrInstall(t *testing.T) {
	want := map[string][]Step{
		"豆包":         {StepPlatform, StepBrowser, StepAccount, StepDone},
		"千问":         {StepPlatform, StepBrowser, StepAccount, StepDone},
		agy.Platform: {StepPlatform, StepInstall, StepAccount, StepDone},
	}
	for platform, steps := range want {
		got := Flow(platform)
		if len(got) != len(steps) {
			t.Fatalf("%s flow = %v", platform, got)
		}
		for i := range steps {
			if got[i] != steps[i] {
				t.Fatalf("%s flow = %v want %v", platform, got, steps)
			}
		}
	}
	for _, p := range []string{"豆包", "千问"} {
		for _, s := range Flow(p) {
			if s == StepInstall {
				t.Fatalf("%s should not ask to install agy", p)
			}
		}
	}
	for _, s := range Flow(agy.Platform) {
		if s == StepBrowser {
			t.Fatal("Antigravity does not use a browser")
		}
	}
}

func TestNextAndPrevWalkTheChosenPlatformsFlow(t *testing.T) {
	step := StepPlatform
	var seen []Step
	for {
		next, ok := Next(agy.Platform, step)
		if !ok {
			break
		}
		seen = append(seen, next)
		step = next
	}
	if len(seen) != 3 || seen[0] != StepInstall || seen[1] != StepAccount || seen[2] != StepDone {
		t.Fatalf("forward = %v", seen)
	}
	if _, ok := Next(agy.Platform, StepDone); ok {
		t.Fatal("done has no next step")
	}
	back, ok := Prev(agy.Platform, StepAccount)
	if !ok || back != StepInstall {
		t.Fatalf("back from login = %v %v", back, ok)
	}
	back, ok = Prev("豆包", StepAccount)
	if !ok || back != StepBrowser {
		t.Fatalf("back from login (doubao) = %v %v", back, ok)
	}
	if _, ok := Prev("豆包", StepPlatform); ok {
		t.Fatal("the first step has no previous step")
	}
	if _, ok := Next("豆包", StepInstall); ok {
		t.Fatal("install is not part of the doubao flow")
	}
}

func TestPlatformChoiceIsOneOfThree(t *testing.T) {
	got := Platforms()
	if len(got) != 3 || got[0] != "豆包" || got[1] != "千问" || got[2] != agy.Platform {
		t.Fatalf("platforms = %v", got)
	}
	if !Valid("千问") || !Valid(agy.Platform) || Valid("") || Valid("Antigravity2") {
		t.Fatal("valid platform names")
	}
	if DefaultChoice(nil) != "豆包" {
		t.Fatal("new config starts on doubao")
	}
	if DefaultChoice([]string{"千问", agy.Platform}) != "千问" {
		t.Fatal("first saved platform in display order")
	}
	if DefaultChoice([]string{agy.Platform}) != agy.Platform {
		t.Fatal("saved antigravity")
	}
	if DefaultChoice([]string{"未知"}) != "豆包" {
		t.Fatal("unknown names fall back")
	}
}

func TestMergeSelectionKeepsOneOnFirstRunAndAddsLater(t *testing.T) {
	first := MergeSelection([]string{"豆包", "千问"}, agy.Platform, true)
	if len(first) != 1 || first[0] != agy.Platform {
		t.Fatalf("first run keeps only the choice: %v", first)
	}
	later := MergeSelection([]string{agy.Platform}, "豆包", false)
	if len(later) != 2 || later[0] != "豆包" || later[1] != agy.Platform {
		t.Fatalf("reopen adds without dropping, in display order: %v", later)
	}
	same := MergeSelection([]string{"千问"}, "千问", false)
	if len(same) != 1 || same[0] != "千问" {
		t.Fatalf("same platform twice: %v", same)
	}
}
