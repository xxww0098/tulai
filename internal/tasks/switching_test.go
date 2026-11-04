package tasks

import (
	"context"
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xxww0098/tulai/internal/agy"
)

func TestTaskSwitchingUsesAccountSegments(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enabled  bool
		count    int
		start    int
		accounts int
		want     []int
	}{
		{"two tasks per account", true, 2, 1, 2, []int{0, 0, 1, 1, 0, 0}},
		{"switch every task", true, 1, 1, 2, []int{0, 1, 0, 1, 0, 1}},
		{"invalid count defaults to one", true, 0, 1, 2, []int{0, 1, 0, 1, 0, 1}},
		{"start at second account", true, 2, 2, 2, []int{1, 1, 0, 0, 1, 1}},
		{"disabled", false, 1, 1, 2, []int{0, 0, 0, 0, 0, 0}},
		{"single account continues", true, 1, 1, 1, []int{0, 0, 0, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := openRotateStore(t)
			var ids []int64
			for i := 0; i < tc.accounts; i++ {
				ids = append(ids, addNamed(t, st, "豆包", string(rune('甲'+i))).ID)
			}
			e := NewEngine(st, nil, nil, "")
			e.gap = func(time.Duration) time.Duration { return 0 }
			var got []int
			err := e.RunObserved(context.Background(), Options{
				Platforms: []string{"豆包"}, Images: []string{"1", "2", "3", "4", "5", "6"},
				Concurrency: 1, TaskSwitching: tc.enabled, TaskSwitchingCount: tc.count, StartBrowserIndex: tc.start,
			}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
				for i, id := range ids {
					if task.BrowserID == id {
						got = append(got, i)
					}
				}
				return nil
			})
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("account sequence = %v, want %v, err=%v", got, tc.want, err)
			}
		})
	}
}

func TestTaskSwitchingSkipsDisabledAccounts(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	disabled := addNamed(t, st, "豆包", "乙")
	last := addNamed(t, st, "豆包", "丙")
	if err := st.SetBrowserDisabled(disabled.ID, true); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(st, nil, nil, "")
	var got []int64
	err := e.RunObserved(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"1", "2", "3", "4"},
		Concurrency: 1, TaskSwitching: true, TaskSwitchingCount: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		got = append(got, task.BrowserID)
		return nil
	})
	want := []int64{first.ID, last.ID, first.ID, last.ID}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("disabled account must be skipped: got=%v want=%v err=%v", got, want, err)
	}
}

func TestTaskSwitchingWorksForAntigravityAccounts(t *testing.T) {
	st := openRotateStore(t)
	first := addAgyAccount(t, st, "甲", t.TempDir(), "ok")
	second := addAgyAccount(t, st, "乙", t.TempDir(), "ok")
	e := NewEngine(st, nil, nil, "")
	var got []int64
	err := e.RunObserved(context.Background(), Options{
		Platforms: []string{agy.Platform}, Images: []string{"1", "2", "3", "4"},
		TaskSwitching: true, TaskSwitchingCount: 2,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		got = append(got, task.BrowserID)
		return nil
	})
	want := []int64{first.ID, first.ID, second.ID, second.ID}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Antigravity account sequence = %v, want %v, err=%v", got, want, err)
	}
}

func TestTaskSwitchingUsesFreeAccountWhileAnotherIsBusy(t *testing.T) {
	st := openRotateStore(t)
	a := addNamed(t, st, "豆包", "甲")
	b := addNamed(t, st, "豆包", "乙")
	c := addNamed(t, st, "豆包", "丙")
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		e.gap = func(time.Duration) time.Duration { return 0 }
		started := make(chan int64, 6)
		releaseA, releaseB := make(chan struct{}), make(chan struct{})
		done := make(chan error, 1)
		var aRuns int
		go func() {
			done <- e.RunObserved(context.Background(), Options{
				Platforms: []string{"豆包"}, Images: []string{"1", "2", "3", "4"},
				Concurrency: 2, TaskSwitching: true, TaskSwitchingCount: 2,
			}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
				started <- task.BrowserID
				if task.BrowserID == a.ID {
					aRuns++
					if aRuns == 1 {
						<-releaseA
					}
				} else if task.BrowserID == b.ID {
					<-releaseB
				}
				return nil
			})
		}()
		synctest.Wait()
		if len(started) != 2 {
			close(releaseA)
			close(releaseB)
			synctest.Wait()
			t.Fatalf("expected two active accounts, got %d", len(started))
		}
		first := []int64{<-started, <-started}
		close(releaseA)
		synctest.Wait()
		var later []int64
		for len(started) > 0 {
			later = append(later, <-started)
		}
		close(releaseB)
		synctest.Wait()
		err := <-done
		if err != nil || !slices.Contains(first, a.ID) || !slices.Contains(first, b.ID) || !reflect.DeepEqual(later, []int64{a.ID, c.ID}) {
			t.Fatalf("first=%v later=%v, want [%d %d] then [%d %d], err=%v", first, later, a.ID, b.ID, a.ID, c.ID, err)
		}
	})
}
