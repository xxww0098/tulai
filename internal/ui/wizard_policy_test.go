package ui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestPlanAccountBrowser(t *testing.T) {
	need := planAccountBrowser(browser.KindFingerprint, false, true)
	if need.CanNext || !need.NeedsDownload || need.Backend != browser.KindFingerprint {
		t.Fatalf("missing: %+v", need)
	}
	fpReady := planAccountBrowser(browser.KindFingerprint, true, true)
	if !fpReady.CanNext || fpReady.NeedsDownload || fpReady.Backend != browser.KindFingerprint {
		t.Fatalf("fingerprint ready: %+v", fpReady)
	}
	fpBlocked := planAccountBrowser(browser.KindFingerprint, false, false)
	if fpBlocked.CanNext || fpBlocked.NeedsDownload || fpBlocked.Blocked == "" || fpBlocked.Backend != browser.KindFingerprint || fpBlocked.Kind != browser.KindFingerprint {
		t.Fatalf("fingerprint blocked: %+v", fpBlocked)
	}
}

func TestWizardShouldShowOnlyForNewUsers(t *testing.T) {
	cases := []struct {
		name     string
		complete string
		accounts []wizardAccount
		want     bool
	}{
		{name: "new", want: true},
		{name: "blank platform", accounts: []wizardAccount{{Name: "账号1", LoggedIn: true}}, want: true},
		{name: "completed with no accounts", complete: WizardCompleteYes},
		{name: "existing logged in", accounts: []wizardAccount{{Platform: "豆包", LoggedIn: true}}},
		{name: "existing logged out", accounts: []wizardAccount{{Platform: "豆包"}}},
		{name: "existing exhausted", accounts: []wizardAccount{{Platform: "千问", LoggedIn: true, Health: "exhausted"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wizardShouldShow(tc.complete, tc.accounts); got != tc.want {
				t.Fatalf("wizardShouldShow() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWizardKeepsClosedStillRequiresARecoverableLogin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account wizardAccount
		want    bool
	}{
		{"ready", wizardAccount{Platform: "豆包", LoggedIn: true, Health: "ready"}, true},
		{"cooling", wizardAccount{Platform: "豆包", LoggedIn: true, Health: "cooling", CoolUntil: 1}, true},
		{"exhausted", wizardAccount{Platform: "豆包", LoggedIn: true, Health: "exhausted"}, false},
		{"signed out", wizardAccount{Platform: "豆包", Health: "ready"}, false},
		{"no platform", wizardAccount{LoggedIn: true, Health: "ready"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wizardKeepsClosed(tc.account, time.Time{}); got != tc.want {
				t.Fatalf("wizardKeepsClosed() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWizardCompleteFlagPersistsInConfigDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database", "config.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Get(WizardKeyComplete) == WizardCompleteYes {
		t.Fatal("a new database is already marked complete")
	}
	st.Set(WizardKeyComplete, WizardCompleteYes)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Get(WizardKeyComplete) != WizardCompleteYes {
		t.Fatalf("complete flag = %q", again.Get(WizardKeyComplete))
	}
}

func TestNextInstanceNameAndLoginAdvance(t *testing.T) {
	if got := nextInstanceName("千问", []string{"豆包1", "豆包2"}); got != "千问1" {
		t.Fatalf("qianwen first = %q", got)
	}
	if got := nextInstanceName("千问", []string{"豆包1", "豆包2", "千问1"}); got != "千问2" {
		t.Fatalf("qianwen second = %q", got)
	}
	if got := nextInstanceName("千问", []string{"浏览器3", "浏览器4", "千问2"}); got != "千问1" {
		t.Fatalf("smallest free qianwen = %q", got)
	}
	if got := nextInstanceName("豆包", []string{"豆包1", "豆包2", "千问1", "千问2"}); got != "豆包3" {
		t.Fatalf("doubao third = %q", got)
	}
	if got := nextInstanceName("豆包", nil); got != "豆包1" {
		t.Fatalf("first name = %q", got)
	}
	q := newWizardQueue([]string{"豆包", "千问"})
	if q.Current() != "豆包" || q.Done() {
		t.Fatalf("queue start: %+v", q)
	}
	q, poll := wizardAfterLogin(q, false)
	if poll != wizardPollWait || q.Current() != "豆包" {
		t.Fatalf("not logged in: %+v %v", q, poll)
	}
	if wizardLoggedIn("out") || wizardLoggedIn("unknown") || wizardLoggedIn(" \"in\" ") {
		t.Fatal("only an exact in probe counts as logged in")
	}
	if !wizardLoggedIn("in") || !wizardLoggedIn(" in ") {
		t.Fatal("in probe should count")
	}
	q, poll = wizardAfterLogin(q, true)
	if poll != wizardPollNext || q.Current() != "千问" {
		t.Fatalf("after doubao: %+v %v", q, poll)
	}
	q, poll = wizardAfterLogin(q, true)
	if poll != wizardPollDone || !q.Done() {
		t.Fatalf("after both: %+v %v", q, poll)
	}
}

func TestWizardPlatformChoices(t *testing.T) {
	if !wizardValidPlatform("千问") || !wizardValidPlatform("千问国际") || !wizardValidPlatform("豆包") || !wizardValidPlatform("Gemini") || !wizardValidPlatform("即梦") || wizardValidPlatform("") || wizardValidPlatform("ChatGPT") || wizardValidPlatform("Antigravity") {
		t.Fatal("valid platform names")
	}
	if got := nextInstanceName("千问国际", []string{"千问1", "豆包1"}); got != "千问国际1" {
		t.Fatalf("qwen intl first = %q", got)
	}
	if got := nextInstanceName("千问国际", []string{"千问国际1", "千问1"}); got != "千问国际2" {
		t.Fatalf("qwen intl second = %q", got)
	}
	if got := nextInstanceName("Gemini", []string{"豆包1", "千问1"}); got != "Gemini1" {
		t.Fatalf("gemini first = %q", got)
	}
	if got := nextInstanceName("Gemini", []string{"Gemini1", "千问1"}); got != "Gemini2" {
		t.Fatalf("gemini second = %q", got)
	}
	if got := nextInstanceName("即梦", []string{"豆包1", "千问1"}); got != "即梦1" {
		t.Fatalf("jimeng first = %q", got)
	}
	if got := nextInstanceName("即梦", []string{"即梦1", "千问1"}); got != "即梦2" {
		t.Fatalf("jimeng second = %q", got)
	}
	if wizardDefaultChoice(nil) != "豆包" {
		t.Fatal("new config starts on doubao")
	}
	if wizardDefaultChoice([]string{"千问", "豆包"}) != "豆包" {
		t.Fatal("first saved platform in display order")
	}
	if wizardDefaultChoice([]string{"千问"}) != "千问" {
		t.Fatal("saved qianwen")
	}
	if wizardDefaultChoice([]string{"千问国际"}) != "千问国际" {
		t.Fatal("saved qwen intl")
	}
	if wizardDefaultChoice([]string{"Gemini"}) != "Gemini" {
		t.Fatal("saved gemini")
	}
	if wizardDefaultChoice([]string{"即梦"}) != "即梦" {
		t.Fatal("saved jimeng")
	}
	if wizardDefaultChoice([]string{"未知", "ChatGPT"}) != "豆包" {
		t.Fatal("unknown names fall back")
	}
}
