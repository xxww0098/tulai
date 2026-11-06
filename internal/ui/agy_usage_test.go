package ui

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
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
	for _, label := range []string{"Gemini 已用", "5小时", "7天", "0%", "3%"} {
		if !tt.HasText(label) {
			t.Fatalf("缺少 Gemini 用量标识 %q", label)
		}
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

func TestAgyQuotaViewRestoresSavedReading(t *testing.T) {
	st := openUsageStore(t)
	row, err := st.AddBrowser(agy.Platform, "账号", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	usage := agy.GeminiUsage{FiveHour: agy.UsageWindow{Remaining: 0.7, Reset: now.Add(time.Hour)}, Weekly: agy.UsageWindow{Remaining: 0.4, Reset: now.Add(48 * time.Hour)}}
	tasks.NoteAgyQuota(st, row.ID, usage, now)
	a := &App{store: st}
	for _, loading := range []bool{false, true} {
		if loading {
			a.agyUsage = map[int64]agyUsageSlot{row.ID: {inflight: true}}
		}
		view := a.agyUsageView(row.ID)
		if view.State != agy.UsageOK || !view.At.Equal(now) || view.Usage.FiveHour.Remaining != 0.7 || view.Usage.Weekly.Remaining != 0.4 {
			t.Fatalf("saved UI quota = %+v", view)
		}
	}
	a.resetAgyUsage(row.ID)
	if _, ok := tasks.SavedAgyUsage(st, row.ID); ok {
		t.Fatal("new OAuth identity retained the old account's quota")
	}
	if view := a.agyUsageView(row.ID); view.State != agy.UsageReading {
		t.Fatalf("new identity still shows old usage: %+v", view)
	}
}

func TestAgyPreflightWaitsForExistingQuotaRead(t *testing.T) {
	st := openUsageStore(t)
	home := t.TempDir()
	writeEmptyAgyToken(t, home)
	row, err := st.AddBrowser(agy.Platform, "账号", home)
	if err != nil {
		t.Fatal(err)
	}
	started, release, fetched, preflight := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	a := &App{store: st, agyUsageFetch: func(ctx context.Context, _, _ string) (agy.GeminiUsage, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return agy.GeminiUsage{}, ctx.Err()
		}
		now := time.Now()
		return agy.GeminiUsage{FiveHour: agy.UsageWindow{Remaining: 0.9, Reset: now.Add(time.Hour)}, Weekly: agy.UsageWindow{Remaining: 0.7, Reset: now.Add(48 * time.Hour)}}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !a.beginAgyUsage(row.ID, time.Now()) {
		t.Fatal("quota read did not start")
	}
	go func() { defer close(fetched); defer a.finishAgyUsage(row.ID); a.fetchAgyAccount(ctx, row, "") }()
	<-started
	go func() { defer close(preflight); a.refreshAgyUsageBeforeRun(ctx) }()
	select {
	case <-preflight:
		t.Fatal("batch did not wait for quota read")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-preflight:
	case <-ctx.Done():
		t.Fatal("batch remained blocked after quota read")
	}
	<-fetched
	if calls.Load() != 1 {
		t.Fatal("batch duplicated an ongoing quota query")
	}
	if view, ok := tasks.SavedAgyUsage(st, row.ID); !ok || view.Usage.Weekly.Remaining != 0.7 {
		t.Fatal("batch started before quota was saved")
	}
}

func TestAgyIdentityResetDiscardsInflightResponse(t *testing.T) {
	for _, loginError := range []bool{false, true} {
		t.Run(map[bool]string{false: "old-quota", true: "old-login-error"}[loginError], func(t *testing.T) {
			st := openUsageStore(t)
			home := t.TempDir()
			writeEmptyAgyToken(t, home)
			row, err := st.AddBrowser(agy.Platform, "新身份", home)
			if err != nil {
				t.Fatal(err)
			}
			st.ConfirmLogin(row.ID)
			started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
			a := &App{store: st, agyUsageFetch: func(context.Context, string, string) (agy.GeminiUsage, error) {
				close(started)
				<-release
				if loginError {
					return agy.GeminiUsage{}, agy.ErrNeedLogin
				}
				now := time.Now()
				return agy.GeminiUsage{FiveHour: agy.UsageWindow{Remaining: 0, Reset: now.Add(time.Hour)}, Weekly: agy.UsageWindow{Remaining: 0.5, Reset: now.Add(24 * time.Hour)}}, nil
			}}
			if !a.beginAgyUsage(row.ID, time.Now()) {
				t.Fatal("旧身份额度读取未开始")
			}
			go func() {
				defer close(finished)
				defer a.finishAgyUsage(row.ID)
				a.fetchAgyAccount(context.Background(), row, "")
			}()
			<-started
			a.resetAgyUsage(row.ID)
			close(release)
			<-finished
			if saved, ok := tasks.SavedAgyUsage(st, row.ID); ok {
				t.Fatalf("旧身份额度覆盖新身份: %+v", saved)
			}
			if got := findUsageBrowser(st, row.ID); !got.Usable(time.Now()) || !got.IsLoggedIn {
				t.Fatalf("旧身份响应使新登录失效: %+v", got)
			}
			if view := a.agyUsageView(row.ID); view.State != agy.UsageReading {
				t.Fatalf("旧身份响应替换读取状态: %+v", view)
			}
			if !a.beginAgyUsage(row.ID, time.Now()) {
				t.Fatal("旧读取结束后不能刷新新身份")
			}
			a.finishAgyUsage(row.ID)
		})
	}
}

func TestAgyPreflightWaitsForResetIdentity(t *testing.T) {
	st := openUsageStore(t)
	home := t.TempDir()
	writeEmptyAgyToken(t, home)
	row, err := st.AddBrowser(agy.Platform, "账号", home)
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(row.ID)
	started, release, freshStarted, freshRelease := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	finished, preflight := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	a := &App{store: st, agyUsageFetch: func(ctx context.Context, _, _ string) (agy.GeminiUsage, error) {
		if calls.Add(1) == 1 {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return agy.GeminiUsage{}, ctx.Err()
			}
		} else {
			close(freshStarted)
			select {
			case <-freshRelease:
			case <-ctx.Done():
				return agy.GeminiUsage{}, ctx.Err()
			}
		}
		now := time.Now()
		return agy.GeminiUsage{FiveHour: agy.UsageWindow{Remaining: 0.8, Reset: now.Add(time.Hour)}, Weekly: agy.UsageWindow{Remaining: 0.6, Reset: now.Add(48 * time.Hour)}}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if !a.beginAgyUsage(row.ID, time.Now()) {
		t.Fatal("旧额度读取未开始")
	}
	go func() { defer close(finished); defer a.finishAgyUsage(row.ID); a.fetchAgyAccount(ctx, row, "") }()
	<-started
	waiting := make(chan struct{}, 1)
	a.agyUsageBusy = func(int64) bool {
		select {
		case waiting <- struct{}{}:
		default:
		}
		return false
	}
	go func() { defer close(preflight); a.refreshAgyUsageBeforeRun(ctx) }()
	<-waiting
	a.resetAgyUsage(row.ID)
	close(release)
	select {
	case <-freshStarted:
	case <-preflight:
		t.Fatal("首单没有等待新身份额度")
	case <-ctx.Done():
		t.Fatal("新身份额度读取未开始")
	}
	a.pullDueAgyUsage(ctx)
	select {
	case <-preflight:
		t.Fatal("新身份额度未完成时首单已放行")
	default:
	}
	close(freshRelease)
	select {
	case <-preflight:
	case <-ctx.Done():
		t.Fatal("新身份额度完成后首单仍未放行")
	}
	<-finished
	if view, ok := tasks.SavedAgyUsage(st, row.ID); !ok || view.Usage.Weekly.Remaining != 0.6 || calls.Load() != 2 {
		t.Fatalf("新身份额度未正确保存: view=%+v ok=%v calls=%d", view, ok, calls.Load())
	}
}

func TestAgyCanceledResponseKeepsQuotaAndHealth(t *testing.T) {
	for _, loginError := range []bool{false, true} {
		t.Run(map[bool]string{false: "quota", true: "login-error"}[loginError], func(t *testing.T) {
			st := openUsageStore(t)
			home := t.TempDir()
			writeEmptyAgyToken(t, home)
			row, err := st.AddBrowser(agy.Platform, "账号", home)
			if err != nil {
				t.Fatal(err)
			}
			st.ConfirmLogin(row.ID)
			now := time.Now()
			usage := agy.GeminiUsage{FiveHour: agy.UsageWindow{Remaining: 0.8, Reset: now.Add(time.Hour)}, Weekly: agy.UsageWindow{Remaining: 0.6, Reset: now.Add(48 * time.Hour)}}
			tasks.NoteAgyQuota(st, row.ID, usage, now)
			before, _ := tasks.SavedAgyUsage(st, row.ID)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			a := &App{store: st, agyUsageFetch: func(context.Context, string, string) (agy.GeminiUsage, error) {
				cancel()
				if loginError {
					return agy.GeminiUsage{}, agy.ErrNeedLogin
				}
				return agy.GeminiUsage{FiveHour: agy.UsageWindow{Remaining: 0, Reset: now.Add(time.Hour)}, Weekly: usage.Weekly}, nil
			}}
			if !a.beginAgyUsage(row.ID, now) {
				t.Fatal("额度读取未开始")
			}
			a.fetchAgyAccount(ctx, row, "")
			a.finishAgyUsage(row.ID)
			if view, ok := tasks.SavedAgyUsage(st, row.ID); !ok || view.Usage != before.Usage {
				t.Fatalf("已取消的响应替换了旧额度: %+v ok=%v", view, ok)
			}
			if got := findUsageBrowser(st, row.ID); !got.IsLoggedIn || !got.Usable(now) {
				t.Fatalf("已取消的响应改写了账号健康: %+v", got)
			}
			if view := a.agyUsageView(row.ID); view.State != agy.UsageOK || view.Usage != before.Usage {
				t.Fatalf("已取消的响应替换了界面额度: %+v", view)
			}
		})
	}
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
	if err := os.WriteFile(path, []byte(`{"token":{"refresh_token":"test-refresh"},"auth_method":"consumer"}`), 0o600); err != nil {
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
