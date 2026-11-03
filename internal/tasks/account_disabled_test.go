package tasks

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

func TestDisabledAccountDoesNotReceiveNewTasks(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "主力", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, true, true)
	e := NewEngine(st, nil, nil, "")
	e.gap = func(time.Duration) time.Duration { return 0 }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan int, 2)
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- e.RunObserved(ctx, Options{Platforms: []string{"豆包"}, Prompts: []string{"一", "二"}, Concurrency: 1},
			func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
				started <- task.Index
				if task.Index == 1 {
					<-release
				}
				return nil
			})
	}()
	select {
	case index := <-started:
		if index != 1 {
			t.Fatalf("首个任务 = %d", index)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("首个任务没有开始")
	}
	if err := e.SetAccountDisabled(row.ID, true); err != nil {
		t.Fatal(err)
	}
	if got := e.AccountBoard(time.Now(), 100).Lanes[0]; !got.Disabled || got.State != LaneRunning {
		t.Fatalf("运行中的账号: %+v", got)
	}
	close(release)
	select {
	case index := <-started:
		t.Fatalf("禁用后又开始任务 %d", index)
	case err := <-finished:
		t.Fatalf("等待中的任务不应结束: %v", err)
	case <-time.After(400 * time.Millisecond):
	}
	if got := e.AccountBoard(time.Now(), 100).Lanes[0]; got.State != LaneDisabled {
		t.Fatalf("完成当前任务后: %+v", got)
	}
	if err := e.SetAccountDisabled(row.ID, false); err != nil {
		t.Fatal(err)
	}
	select {
	case index := <-started:
		if index != 2 {
			t.Fatalf("重新启用后的任务 = %d", index)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("重新启用后未恢复任务")
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("任务未完成")
	}
}

func TestDisabledAccountReassignsAndKeepsWaiting(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	first, _ := st.AddBrowser("豆包", "甲", "")
	second, _ := st.AddBrowser("豆包", "乙", "")
	e := NewEngine(st, nil, nil, "")
	if err := e.SetAccountDisabled(first.ID, true); err != nil {
		t.Fatal(err)
	}
	item := Task{Platform: "豆包", BrowserID: first.ID}
	if wait, dead := e.prepareAccount(&item, time.Now()); dead || !wait.IsZero() || item.BrowserID != second.ID {
		t.Fatalf("未切换到可用账号: task=%+v wait=%v dead=%v", item, wait, dead)
	}
	if err := e.SetAccountDisabled(second.ID, true); err != nil {
		t.Fatal(err)
	}
	item.BrowserID = 0
	if wait, dead := e.prepareAccount(&item, time.Now()); dead || wait.IsZero() {
		t.Fatalf("全部禁用时应等待: wait=%v dead=%v", wait, dead)
	}
	if e.enterAccount(context.Background(), first.ID) {
		t.Fatal("禁用账号不应开始任务")
	}
}
