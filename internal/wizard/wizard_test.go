package wizard

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestPlanBrowserPrefersWindowsEdgeThenInstalledBrowsers(t *testing.T) {
	chrome := Found{Kind: browser.KindChrome, Path: `C:\chrome.exe`}
	edge := Found{Kind: browser.KindEdge, Path: `C:\msedge.exe`}
	chromium := Found{Kind: browser.KindChromium, Path: "/usr/bin/chromium"}

	win := PlanBrowser("windows", "amd64", []Found{chrome, edge}, false, true)
	if win.Backend != browser.KindEdge || win.Kind != browser.KindEdge || !win.CanNext || win.NeedsDownload || win.Title != "将使用系统 Edge" {
		t.Fatalf("windows with edge and chrome: %+v", win)
	}

	winChrome := PlanBrowser("windows", "amd64", []Found{chrome}, false, true)
	if winChrome.Backend != browser.KindChrome || winChrome.Title != "将使用本机 Chrome" || !winChrome.CanNext {
		t.Fatalf("windows chrome only: %+v", winChrome)
	}

	mac := PlanBrowser("darwin", "arm64", []Found{edge, chrome}, true, true)
	if mac.Backend != browser.KindChrome || mac.Kind != browser.KindChrome || mac.NeedsDownload {
		t.Fatalf("mac should prefer chrome over edge and builtin: %+v", mac)
	}

	macEdge := PlanBrowser("darwin", "arm64", []Found{edge}, false, true)
	if macEdge.Backend != browser.KindEdge || macEdge.Title != "将使用本机 Edge" {
		t.Fatalf("mac edge only: %+v", macEdge)
	}

	linux := PlanBrowser("linux", "amd64", []Found{chromium}, false, true)
	if linux.Backend != browser.KindChrome || linux.Kind != browser.KindChromium || linux.Title != "将使用本机 Chromium" || !linux.CanNext {
		t.Fatalf("chromium should be stored as chrome: %+v", linux)
	}
}

func TestPlanBrowserDownloadOrBlocked(t *testing.T) {
	need := PlanBrowser("linux", "amd64", nil, false, true)
	if need.CanNext || !need.NeedsDownload || need.Backend != browser.KindBuiltin {
		t.Fatalf("missing browsers should offer download: %+v", need)
	}
	ready := PlanBrowser("linux", "amd64", nil, true, true)
	if !ready.CanNext || ready.NeedsDownload || ready.Backend != browser.KindBuiltin || ready.Title != "将使用已下载的内置浏览器" {
		t.Fatalf("installed builtin should continue: %+v", ready)
	}
	blocked := PlanBrowser("windows", "arm64", nil, false, false)
	if blocked.CanNext || blocked.NeedsDownload || blocked.Blocked == "" {
		t.Fatalf("windows arm64 without a browser: %+v", blocked)
	}
	installedWins := PlanBrowser("linux", "amd64", []Found{{Kind: browser.KindChrome, Path: "/opt/google/chrome/chrome"}}, false, true)
	if installedWins.NeedsDownload || installedWins.Backend != browser.KindChrome {
		t.Fatalf("installed chrome beats download: %+v", installedWins)
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
	if got := NextAccountName(nil); got != "账号1" {
		t.Fatalf("first name = %q", got)
	}
	if got := NextAccountName([]string{"账号1", "工作"}); got != "账号2" {
		t.Fatalf("next name = %q", got)
	}
	if !CanChoosePlatform(true, false) || !CanChoosePlatform(false, true) || CanChoosePlatform(false, false) {
		t.Fatal("platform step should require one choice")
	}
	got := SelectedPlatforms(true, true)
	if len(got) != 2 || got[0] != "豆包" || got[1] != "千问" {
		t.Fatalf("order = %v", got)
	}
	q := NewQueue(got)
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
