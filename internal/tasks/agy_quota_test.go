package tasks

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
	"github.com/xxww0098/tulai/internal/store"
)

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
	id, ok := pickHealthy(rows, 0, now, 100)
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
