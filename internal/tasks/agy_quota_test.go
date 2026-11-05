package tasks

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/store"
)

func TestAgyQuotaPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	row, err := st.AddBrowser(agy.Platform, "账号", t.TempDir())
	if err != nil {
		st.Close()
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Second)
	usage := agy.GeminiUsage{
		FiveHour: agy.UsageWindow{Remaining: 0.8, Reset: now.Add(time.Hour)},
		Weekly:   agy.UsageWindow{Remaining: 0.6, Reset: now.Add(48 * time.Hour)},
	}
	NoteAgyQuota(st, row.ID, usage, now)
	st.Close()
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	view, ok := SavedAgyUsage(st, row.ID)
	if !ok || !view.At.Equal(now) || view.Usage.FiveHour.Remaining != 0.8 || view.Usage.Weekly.Remaining != 0.6 || !view.Usage.FiveHour.Reset.Equal(usage.FiveHour.Reset) || !view.Usage.Weekly.Reset.Equal(usage.Weekly.Reset) {
		t.Fatalf("saved quota = %+v", view)
	}
}

func TestAgyUsesMostRemainingAndSwitchesWhenEitherWindowExhausts(t *testing.T) {
	for _, exhausted := range []string{"5h", "weekly", "CLI"} {
		t.Run(exhausted, func(t *testing.T) {
			st := openRotateStore(t)
			low := addAgyAccount(t, st, "较少", t.TempDir(), "ok")
			high := addAgyAccount(t, st, "最多", t.TempDir(), "ok")
			weeklyLow := addAgyAccount(t, st, "7天较少", t.TempDir(), "ok")
			now := time.Now()
			quota := func(five, week float64) agy.GeminiUsage {
				return agy.GeminiUsage{
					FiveHour: agy.UsageWindow{Remaining: five, Reset: now.Add(time.Hour)},
					Weekly:   agy.UsageWindow{Remaining: week, Reset: now.Add(48 * time.Hour)},
				}
			}
			NoteAgyQuota(st, low.ID, quota(0.4, 0.6), now)
			NoteAgyQuota(st, high.ID, quota(0.8, 0.7), now)
			NoteAgyQuota(st, weeklyLow.ID, quota(0.95, 0.1), now)
			e := NewEngine(st, nil, nil, "")
			var used []int64
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := e.RunObserved(ctx, Options{Platforms: []string{agy.Platform}, Images: []string{"1", "2", "3"}, TaskSwitching: true, TaskSwitchingCount: 1}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
				used = append(used, task.BrowserID)
				if len(used) == 1 {
					return nil
				}
				if len(used) == 2 {
					switch exhausted {
					case "5h":
						NoteAgyQuota(st, high.ID, quota(0, 0.7), now)
					case "weekly":
						NoteAgyQuota(st, high.ID, quota(0.8, 0), now)
					case "CLI":
						return &agy.QuotaError{Until: now.Add(time.Hour)}
					}
				}
				return nil
			})
			want := []int64{high.ID, high.ID, low.ID}
			if exhausted == "CLI" {
				want = append(want, low.ID)
			}
			if err != nil || !reflect.DeepEqual(used, want) {
				t.Fatalf("account order = %v, want %v, err = %v", used, want, err)
			}
		})
	}
}

func TestAgyQuotaPrioritySkipsUnavailableAndExpiredReadings(t *testing.T) {
	st := openRotateStore(t)
	unknown := addAgyAccount(t, st, "未读取", t.TempDir(), "ok")
	low := addAgyAccount(t, st, "较少", t.TempDir(), "ok")
	high := addAgyAccount(t, st, "最多", t.TempDir(), "ok")
	now := time.Now()
	for id, fraction := range map[int64]float64{low.ID: 0.3, high.ID: 0.9} {
		NoteAgyQuota(st, id, agy.GeminiUsage{FiveHour: agy.UsageWindow{Remaining: fraction, Reset: now.Add(time.Hour)}, Weekly: agy.UsageWindow{Remaining: fraction, Reset: now.Add(48 * time.Hour)}}, now)
	}
	e := NewEngine(st, nil, nil, "")
	if id, ok := e.pickIdleAccount(agy.Platform, unknown.ID, 0, nil, nil, now); !ok || id != high.ID {
		t.Fatalf("highest quota not selected: %d", id)
	}
	if id, ok := e.pickIdleAccount(agy.Platform, high.ID, 0, map[int64]bool{high.ID: true}, nil, now); !ok || id != low.ID {
		t.Fatalf("busy account selected: %d", id)
	}
	if id, ok := pickHealthy(st, st.Browsers(), high.ID, now, 0); !ok || id != low.ID {
		t.Fatalf("rotation priority = %d", id)
	}
	if agyRemaining(st, high.ID, now.Add(2*time.Hour)) != -1 {
		t.Fatal("expired quota was ranked as current")
	}
}

func TestAgyQuotaRefreshPrecedesFirstDispatchAndCanBeStopped(t *testing.T) {
	st := openRotateStore(t)
	first := addAgyAccount(t, st, "较少", t.TempDir(), "ok")
	second := addAgyAccount(t, st, "最多", t.TempDir(), "ok")
	e := NewEngine(st, nil, nil, "")
	e.SetAgyUsageRefresh(func(context.Context) {
		now := time.Now()
		for id, fraction := range map[int64]float64{first.ID: 0.2, second.ID: 0.8} {
			NoteAgyQuota(st, id, agy.GeminiUsage{FiveHour: agy.UsageWindow{Remaining: fraction, Reset: now.Add(time.Hour)}, Weekly: agy.UsageWindow{Remaining: fraction, Reset: now.Add(48 * time.Hour)}}, now)
		}
	})
	var picked int64
	opts := Options{Platforms: []string{agy.Platform}, Images: []string{"1"}}
	if err := e.RunObserved(context.Background(), opts, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error { picked = task.BrowserID; return nil }); err != nil || picked != second.ID {
		t.Fatalf("first dispatch used stale quota: id=%d err=%v", picked, err)
	}
	reading, done := make(chan struct{}), make(chan error, 1)
	e.SetAgyUsageRefresh(func(ctx context.Context) { close(reading); <-ctx.Done() })
	picked = 0
	go func() {
		done <- e.RunObserved(context.Background(), opts, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error { picked = task.BrowserID; return nil })
	}()
	select {
	case <-reading:
	case <-time.After(3 * time.Second):
		t.Fatal("quota refresh did not start")
	}
	e.Stop()
	select {
	case <-done:
		if picked != 0 {
			t.Fatal("task was sent after stopping quota refresh")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stopping quota refresh did not stop the batch")
	}
}

func TestAgyUsageZeroSkipsRotation(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now()
	soon := now.Add(2 * time.Hour)
	later := now.Add(48 * time.Hour)
	first, err := st.AddBrowser(agy.Platform, "acc1", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.AddBrowser(agy.Platform, "acc2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(first.ID, false, true)
	st.UpdateBrowser(second.ID, false, true)
	NoteAgyQuota(st, first.ID, agy.GeminiUsage{
		FiveHour: agy.UsageWindow{Remaining: 0, Reset: soon},
		Weekly:   agy.UsageWindow{Remaining: 0.5, Reset: later},
	}, now)
	rows := st.Browsers()
	held := findBrowser(rows, first.ID)
	if held == nil || held.Usable(now) || held.LastError != "额度不足" || held.CoolUntil != soon.Unix() {
		t.Fatalf("hold %+v", held)
	}
	if openAccount(*held, now, 100) {
		t.Fatal("额度用尽的账号仍会接任务")
	}
	id, ok := pickHealthy(st, rows, 0, now, 100)
	if !ok || id != second.ID {
		t.Fatalf("picked %d ok=%v", id, ok)
	}

	NoteAgyQuota(st, second.ID, agy.GeminiUsage{
		FiveHour: agy.UsageWindow{Remaining: 0.2, Reset: soon},
		Weekly:   agy.UsageWindow{Remaining: 0.2, Reset: later},
	}, now)
	if row := findBrowser(st.Browsers(), second.ID); row == nil || !row.Usable(now) {
		t.Fatalf("remaining should stay usable: %+v", row)
	}
	NoteAgyQuota(st, first.ID, agy.GeminiUsage{
		FiveHour: agy.UsageWindow{Remaining: 1, Reset: soon},
		Weekly:   agy.UsageWindow{Remaining: 1, Reset: later},
	}, now)
	if row := findBrowser(st.Browsers(), first.ID); row == nil || row.Usable(now) || row.LastError != "额度不足" || row.CoolUntil != soon.Unix() {
		t.Fatalf("later usage must keep the hold: %+v", row)
	}
}

func TestAgyUsageZeroResetStaysExhausted(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	now := time.Now()
	row, err := st.AddBrowser(agy.Platform, "acc1", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	NoteAgyQuota(st, row.ID, agy.GeminiUsage{
		FiveHour: agy.UsageWindow{Remaining: 0},
		Weekly:   agy.UsageWindow{Remaining: 0.4, Reset: now.Add(time.Hour)},
	}, now)
	got := findBrowser(st.Browsers(), row.ID)
	if got == nil || got.CoolUntil != 0 || got.LastError != "额度不足" || got.Usable(now.Add(48*time.Hour)) {
		t.Fatalf("zero reset should stay exhausted: %+v", got)
	}
}

func TestAgyUsageDoesNotClearLoginHold(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	row, err := st.AddBrowser(agy.Platform, "acc1", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowserHealth(row.ID, store.HealthExhausted, 0, agy.ErrNeedLogin.Error())
	NoteAgyQuota(st, row.ID, agy.GeminiUsage{
		FiveHour: agy.UsageWindow{Remaining: 0, Reset: time.Now().Add(time.Hour)},
		Weekly:   agy.UsageWindow{Remaining: 1, Reset: time.Now().Add(time.Hour)},
	}, time.Now())
	got := findBrowser(st.Browsers(), row.ID)
	if got == nil || got.LastError != agy.ErrNeedLogin.Error() || got.CoolUntil != 0 {
		t.Fatalf("login hold changed: %+v", got)
	}
}
