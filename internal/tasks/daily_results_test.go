package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestDailyResultsFollowCompletedTasksAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	row, err := st.AddBrowser("豆包", "甲", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, true, true)
	e := NewEngine(st, nil, nil, "")
	e.gap = func(time.Duration) time.Duration { return 0 }
	err = e.RunObserved(context.Background(), Options{Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}}, func(Task) {}, func(Progress) {},
		func(_ context.Context, task *Task) error {
			if task.Index == 2 {
				return errors.New("出图失败")
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	board := NewEngine(st, nil, nil, "").AccountBoard(time.Now(), 100)
	if len(board.Lanes) != 1 || board.Lanes[0].UsedToday != 2 || board.Lanes[0].SuccessToday != 1 || board.Lanes[0].FailedToday != 1 {
		t.Fatalf("restored board: %+v", board)
	}
}

func TestDailyResultsFollowAccountAfterRotation(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	second := addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	e.gap = func(time.Duration) time.Duration { return 0 }
	err := e.RunObserved(context.Background(), Options{Platforms: []string{"豆包"}, Images: []string{"a.png"}}, func(Task) {}, func(Progress) {},
		func(_ context.Context, task *Task) error {
			if task.ranOn == 0 {
				task.ranOn = task.BrowserID
			}
			if task.BrowserID == first.ID {
				return strategy.ErrAccountQuota
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	firstSuccess, firstFailed := browserByID(t, st, first.ID).ResultsToday(time.Now())
	secondSuccess, secondFailed := browserByID(t, st, second.ID).ResultsToday(time.Now())
	if firstSuccess != 0 || firstFailed != 0 || secondSuccess != 1 || secondFailed != 0 {
		t.Fatalf("first=%d/%d second=%d/%d", firstSuccess, firstFailed, secondSuccess, secondFailed)
	}
}

func TestDailyResultsIgnoreUnfinishedTasks(t *testing.T) {
	st := openRotateStore(t)
	row := addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	for _, status := range []Status{StatusSkipped, StatusNeedsUser, StatusPending, StatusSuccess, StatusFailed} {
		e.noteUse(Task{slot: row.ID, Attempts: 1, Status: status}, 0)
	}
	e.noteUse(Task{slot: row.ID, Status: StatusFailed}, 0)
	e.noteUse(Task{slot: row.ID, Attempts: 1, Status: StatusSuccess, soften: 2}, 2)
	b := browserByID(t, st, row.ID)
	if success, failed := b.ResultsToday(time.Now()); b.UsedToday(time.Now()) != 6 || success != 2 || failed != 1 {
		t.Fatalf("unfinished tasks should not count as results: %+v success=%d failed=%d", b, success, failed)
	}
}
