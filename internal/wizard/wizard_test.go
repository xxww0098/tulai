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

func TestShouldShowOnlyForNewUsers(t *testing.T) {
	cases := []struct {
		name     string
		complete string
		accounts []Account
		want     bool
	}{
		{name: "new", want: true},
		{name: "blank platform", accounts: []Account{{Name: "账号1", LoggedIn: true}}, want: true},
		{name: "completed with no accounts", complete: CompleteYes},
		{name: "existing logged in", accounts: []Account{{Platform: "豆包", LoggedIn: true}}},
		{name: "existing logged out", accounts: []Account{{Platform: "豆包"}}},
		{name: "existing exhausted", accounts: []Account{{Platform: "千问", LoggedIn: true, Health: "exhausted"}}},
		{name: "existing extension", accounts: []Account{{Platform: "豆包", Extension: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldShow(tc.complete, tc.accounts); got != tc.want {
				t.Fatalf("ShouldShow() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKeepsClosedStillRequiresARecoverableLogin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account Account
		want    bool
	}{
		{"ready", Account{Platform: "豆包", LoggedIn: true, Health: "ready"}, true},
		{"cooling", Account{Platform: "豆包", LoggedIn: true, Health: "cooling", CoolUntil: 1}, true},
		{"exhausted", Account{Platform: "豆包", LoggedIn: true, Health: "exhausted"}, false},
		{"signed out", Account{Platform: "豆包", Health: "ready"}, false},
		{"no platform", Account{LoggedIn: true, Health: "ready"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := KeepsClosed(tc.account, time.Time{}); got != tc.want {
				t.Fatalf("KeepsClosed() = %v, want %v", got, tc.want)
			}
		})
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
		"ChatGPT":    {StepPlatform, StepBrowser, StepAccount, StepDone},
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

func TestPlatformChoices(t *testing.T) {
	got := Platforms()
	if len(got) != 4 || got[0] != "豆包" || got[1] != "千问" || got[2] != agy.Platform || got[3] != "ChatGPT" {
		t.Fatalf("platforms = %v", got)
	}
	if !Valid("千问") || !Valid(agy.Platform) || !Valid("ChatGPT") || Valid("") || Valid("Antigravity2") {
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
