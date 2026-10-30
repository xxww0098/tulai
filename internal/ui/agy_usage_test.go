package ui

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/store"
)

func TestAgyQuotaBarsAndTooltip(t *testing.T) {
	usage := agy.GeminiUsage{
		FiveHour: agy.UsageWindow{Remaining: 1, Reset: time.Date(2026, 10, 30, 16, 42, 18, 0, time.UTC)},
		Weekly:   agy.UsageWindow{Remaining: 0.9719377756118774, Reset: time.Date(2026, 11, 5, 2, 12, 24, 0, time.UTC)},
	}
	now := time.Now()
	fiveTip := agy.WindowTip(agy.Window5h, usage.FiveHour.Remaining, usage.FiveHour.Reset, now)
	weekTip := agy.WindowTip(agy.WindowWeek, usage.Weekly.Remaining, usage.Weekly.Reset, now)
	a := &App{
		platformAuto: true,
		stageTab:     2,
		stagePicked:  true,
		prompts:      "一只猫",
		dailyCap:     20,
		browserRows: []store.Browser{{
			ID: 1, Name: "acc1", Platform: agy.Platform, UserDataDir: t.TempDir(),
			IsLoggedIn: true, Health: store.HealthReady,
			DayKey: now.Format("2006-01-02"), DayCount: 7,
		}},
		agyUsage: map[int64]agyUsageSlot{
			1: {view: agy.UsageView{State: agy.UsageOK, Usage: usage, At: now}},
		},
	}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("额度") {
		t.Fatal("缺少额度列")
	}
	if tt.HasText("今日") || tt.HasText("7/20") || countExact(tt, "7") != 0 {
		t.Fatalf("额度列还在显示今日次数: %v", tt.Texts())
	}
	five, ok := tt.Find(fiveTip)
	if !ok || five.H < 2 || five.H > 8 || five.W < 20 {
		t.Fatalf("5h bar %+v", five)
	}
	week, ok := tt.Find(weekTip)
	if !ok || week.H < 2 || week.H > 8 || week.W < 20 {
		t.Fatalf("week bar %+v", week)
	}
	if week.Y <= five.Y || abs32(week.X-five.X) > 1 {
		t.Fatalf("bars %+v %+v", five, week)
	}
	if countExact(tt, fiveTip) != 1 || countExact(tt, weekTip) != 1 {
		t.Fatal("用量说明不该在格子里再画一遍")
	}
	hoverTip(t, tt, fiveTip)
	hoverTip(t, tt, weekTip)
}

func TestAgyQuotaDashReasons(t *testing.T) {
	now := time.Now()
	fail := "读取额度失败：超时"
	a := &App{
		platformAuto: true,
		stageTab:     2,
		stagePicked:  true,
		prompts:      "一只猫",
		browserRows: []store.Browser{
			{ID: 1, Name: "读", Platform: agy.Platform, UserDataDir: t.TempDir(), IsLoggedIn: true, Health: store.HealthReady},
			{ID: 2, Name: "登", Platform: agy.Platform, UserDataDir: t.TempDir(), IsLoggedIn: false, Health: store.HealthReady},
			{ID: 3, Name: "败", Platform: agy.Platform, UserDataDir: t.TempDir(), IsLoggedIn: true, Health: store.HealthReady},
		},
		agyUsage: map[int64]agyUsageSlot{
			2: {view: agy.UsageView{State: agy.UsageNeedLogin, Reason: agy.ReasonLogin, At: now}},
			3: {view: agy.UsageView{State: agy.UsageFailed, Reason: fail, At: now}},
		},
	}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(300 * time.Millisecond)
	for _, reason := range []string{agy.ReasonReading, agy.ReasonLogin, fail} {
		r, ok := tt.Find(reason)
		if !ok || r.H < 10 {
			t.Fatalf("dash %q %+v", reason, r)
		}
		if countExact(tt, reason) != 1 {
			t.Fatalf("%q count %d", reason, countExact(tt, reason))
		}
		hoverTip(t, tt, reason)
	}
}

func TestAgyUsageRefreshGates(t *testing.T) {
	st := openUsageStore(t)
	home := t.TempDir()
	writeEmptyAgyToken(t, home)
	row, err := st.AddBrowser(agy.Platform, "acc1", home)
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	usage := agy.GeminiUsage{
		FiveHour: agy.UsageWindow{Remaining: 0.5, Reset: time.Now().Add(time.Hour)},
		Weekly:   agy.UsageWindow{Remaining: 0.5, Reset: time.Now().Add(48 * time.Hour)},
	}
	calls := 0
	a := &App{
		store:        st,
		agyUsageSync: true,
		agyUsageFetch: func(context.Context, string, string) (agy.GeminiUsage, error) {
			calls++
			return usage, nil
		},
	}
	a.pullDueAgyUsage(context.Background())
	if calls != 1 || a.agyUsageView(row.ID).State != agy.UsageOK {
		t.Fatalf("first fetch calls=%d view=%+v", calls, a.agyUsageView(row.ID))
	}
	a.pullDueAgyUsage(context.Background())
	if calls != 1 {
		t.Fatalf("within 5 minutes fetched again: %d", calls)
	}
	a.markAgyUsageForce(row.ID)
	a.pullDueAgyUsage(context.Background())
	if calls != 2 {
		t.Fatalf("task finish should refresh: %d", calls)
	}

	a.agyUsageMu.Lock()
	slot := a.agyUsage[row.ID]
	slot.view.At = time.Now().Add(-6 * time.Minute)
	a.agyUsage[row.ID] = slot
	a.agyUsageMu.Unlock()
	a.pullDueAgyUsage(context.Background())
	if calls != 3 {
		t.Fatalf("after 5 minutes should refresh: %d", calls)
	}

	busy := true
	a.agyUsageBusy = func(int64) bool { return busy }
	a.markAgyUsageForce(row.ID)
	a.pullDueAgyUsage(context.Background())
	if calls != 3 {
		t.Fatalf("busy account was fetched: %d", calls)
	}
	busy = false
	a.pullDueAgyUsage(context.Background())
	if calls != 4 {
		t.Fatalf("force should run after generation: %d", calls)
	}

	a.agyUsageMu.Lock()
	slot = a.agyUsage[row.ID]
	slot.view.At = time.Time{}
	a.agyUsage[row.ID] = slot
	a.agyUsageMu.Unlock()
	a.agyUsageFetch = func(context.Context, string, string) (agy.GeminiUsage, error) {
		calls++
		return agy.GeminiUsage{}, context.Canceled
	}
	before := a.agyUsageView(row.ID)
	a.pullDueAgyUsage(context.Background())
	if got := a.agyUsageView(row.ID); got.State != before.State || got.Usage != before.Usage {
		t.Fatalf("cancel replaced bars: %+v", got)
	}
}

func TestAgyUsageRefreshOnStart(t *testing.T) {
	st := openUsageStore(t)
	home := t.TempDir()
	row, err := st.AddBrowser(agy.Platform, "acc1", home)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	a := &App{
		store:        st,
		agyUsageSync: true,
		agyUsageFetch: func(context.Context, string, string) (agy.GeminiUsage, error) {
			calls++
			return agy.GeminiUsage{}, nil
		},
	}
	a.startAgyUsage()
	t.Cleanup(a.stopAgyUsage)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.agyUsageView(row.ID).Reason == agy.ReasonLogin {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.agyUsageView(row.ID).Reason != agy.ReasonLogin || calls != 0 {
		t.Fatalf("start view=%+v calls=%d", a.agyUsageView(row.ID), calls)
	}
}

func TestAgyUsageRefreshHoldAndLogin(t *testing.T) {
	st := openUsageStore(t)
	home := t.TempDir()
	writeEmptyAgyToken(t, home)
	row, err := st.AddBrowser(agy.Platform, "acc1", home)
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	reset := time.Now().Add(3 * time.Hour).UTC().Truncate(time.Second)
	a := &App{
		store:        st,
		agyUsageSync: true,
		agyUsageFetch: func(context.Context, string, string) (agy.GeminiUsage, error) {
			return agy.GeminiUsage{
				FiveHour: agy.UsageWindow{Remaining: 0, Reset: reset},
				Weekly:   agy.UsageWindow{Remaining: 0.4, Reset: reset.Add(24 * time.Hour)},
			}, nil
		},
	}
	a.pullDueAgyUsage(context.Background())
	got := findUsageBrowser(st, row.ID)
	if got.LastError != "额度不足" || got.CoolUntil != reset.Unix() || got.Usable(time.Now()) {
		t.Fatalf("hold %+v", got)
	}
	if a.agyUsageView(row.ID).State != agy.UsageOK {
		t.Fatalf("bars should stay while the hold is set: %+v", a.agyUsageView(row.ID))
	}

	writeEmptyAgyToken(t, home)
	a.agyUsageFetch = func(context.Context, string, string) (agy.GeminiUsage, error) {
		return agy.GeminiUsage{}, agy.ErrNeedLogin
	}
	a.markAgyUsageForce(row.ID)
	a.pullDueAgyUsage(context.Background())
	if _, err := os.Stat(agy.TokenPath(home)); err != nil {
		t.Fatal("读额度需登录时不应删凭证:", err)
	}
	got = findUsageBrowser(st, row.ID)
	if got.IsLoggedIn || got.LastError != agy.ErrNeedLogin.Error() || got.CoolUntil != 0 {
		t.Fatalf("login hold %+v", got)
	}
	if view := a.agyUsageView(row.ID); view.State != agy.UsageNeedLogin || view.Reason != agy.ReasonLogin {
		t.Fatalf("login view %+v", view)
	}
}

func hoverTip(t *testing.T, tt *ui.Tester, tip string) {
	t.Helper()
	before := countExact(tt, tip)
	r, ok := tt.Find(tip)
	if !ok {
		t.Fatalf("找不到 %q", tip)
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Advance(700 * time.Millisecond)
	if got := countExact(tt, tip); got != before+1 {
		t.Fatalf("%s 悬停没有出现提示: before=%d after=%d", tip, before, got)
	}
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func openUsageStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func writeEmptyAgyToken(t *testing.T, home string) {
	t.Helper()
	path := agy.TokenPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
}

func findUsageBrowser(st *store.Store, id int64) store.Browser {
	for _, row := range st.Browsers() {
		if row.ID == id {
			return row
		}
	}
	return store.Browser{}
}
