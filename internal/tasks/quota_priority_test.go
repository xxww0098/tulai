package tasks

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"
)

func TestConcurrentDispatchPrefersLeastRemainingQuota(t *testing.T) {
	for _, platform := range []string{"豆包", "千问"} {
		t.Run(platform, func(t *testing.T) {
			st := openRotateStore(t)
			low := addNamed(t, st, platform, "已用 0")
			middle := addNamed(t, st, platform, "已用 80")
			high := addNamed(t, st, platform, "已用 95")
			full := addNamed(t, st, platform, "已满")
			disabled := addNamed(t, st, platform, "已禁用")
			// overlapStarted runs in a synctest bubble whose clock starts in 2000.
			now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).In(time.Local)
			for _, item := range []struct {
				id   int64
				used int
			}{{middle.ID, 80}, {high.ID, 95}, {full.ID, 100}, {disabled.ID, 99}} {
				if err := st.RecordUse(item.id, now, item.used, 0, 0); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.SetBrowserDisabled(disabled.ID, true); err != nil {
				t.Fatal(err)
			}
			got := overlapStarted(t, st, Options{Platforms: []string{platform}, Images: []string{"a.png", "b.png"}, Concurrency: 2, DailyCap: 100})
			if len(got) != 2 || !slices.Contains(got, high.ID) || !slices.Contains(got, middle.ID) || slices.Contains(got, low.ID) {
				t.Fatalf("应先使用剩余额度较少且可用的账号: %v", got)
			}
		})
	}
}

func TestQuotaPriorityKeepsStartingAccountWhenEqual(t *testing.T) {
	st := openRotateStore(t)
	addNamed(t, st, "豆包", "甲")
	second := addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	var used int64
	err := e.RunObserved(context.Background(), Options{Platforms: []string{"豆包"}, Images: []string{"a.png"}, StartBrowserIndex: 2},
		func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
			used = task.BrowserID
			return nil
		})
	if err != nil || used != second.ID {
		t.Fatalf("额度相同时应保留起始账号: used=%d err=%v", used, err)
	}
}

func TestQuotaPriorityUsesTodayAndSkipsAvoidedOrBusyAccounts(t *testing.T) {
	st := openRotateStore(t)
	yesterday := addNamed(t, st, "豆包", "昨天已用")
	middle := addNamed(t, st, "豆包", "今天已用 20")
	high := addNamed(t, st, "豆包", "今天已用 90")
	now := time.Now()
	for _, item := range []struct {
		id   int64
		when time.Time
		used int
	}{{yesterday.ID, now.AddDate(0, 0, -1), 99}, {middle.ID, now, 20}, {high.ID, now, 90}} {
		if err := st.RecordUse(item.id, item.when, item.used, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	e := NewEngine(st, nil, nil, "")
	if id, ok := e.pickIdleAccount("豆包", yesterday.ID, 0, nil, nil, now); !ok || id != high.ID {
		t.Fatalf("应忽略昨天的用量: id=%d ok=%v", id, ok)
	}
	if id, ok := e.pickIdleAccount("豆包", 0, high.ID, nil, nil, now); !ok || id != middle.ID {
		t.Fatalf("应避开发送状态未确认的账号: id=%d ok=%v", id, ok)
	}
	if id, ok := e.pickIdleAccount("豆包", 0, 0, map[int64]bool{high.ID: true}, nil, now); !ok || id != middle.ID {
		t.Fatalf("忙碌账号不能占用新的任务: id=%d ok=%v", id, ok)
	}
	if id, ok := e.pickIdleAccount("豆包", 0, 0, nil, map[int64]time.Time{high.ID: now.Add(time.Minute)}, now); !ok || id != middle.ID {
		t.Fatalf("发送间隔内应选择其他账号: id=%d ok=%v", id, ok)
	}
}

func TestQuotaPriorityRechecksAfterAccountFills(t *testing.T) {
	st := openRotateStore(t)
	low := addNamed(t, st, "豆包", "已用 0")
	middle := addNamed(t, st, "豆包", "已用 2")
	high := addNamed(t, st, "豆包", "已用 3")
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		for _, item := range []struct {
			id   int64
			used int
		}{{middle.ID, 2}, {high.ID, 3}} {
			if err := st.RecordUse(item.id, now, item.used, 0, 0); err != nil {
				t.Fatal(err)
			}
		}
		started := make(chan int64, 3)
		releaseHigh, releaseMiddle := make(chan struct{}), make(chan struct{})
		defer func() {
			select {
			case <-releaseHigh:
			default:
				close(releaseHigh)
			}
			select {
			case <-releaseMiddle:
			default:
				close(releaseMiddle)
			}
		}()
		finished := make(chan error, 1)
		e := NewEngine(st, nil, nil, "")
		go func() {
			finished <- e.RunObserved(context.Background(), Options{Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png", "c.png"}, DailyCap: 4},
				func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
					started <- task.BrowserID
					switch task.BrowserID {
					case high.ID:
						<-releaseHigh
					case middle.ID:
						<-releaseMiddle
					}
					return nil
				})
		}()
		synctest.Wait()
		if len(started) != 2 {
			t.Fatalf("首批应有两个账号: %d", len(started))
		}
		first := []int64{<-started, <-started}
		if !slices.Contains(first, high.ID) || !slices.Contains(first, middle.ID) {
			t.Fatalf("首批账号: %v", first)
		}
		close(releaseHigh)
		synctest.Wait()
		if len(started) != 1 || <-started != low.ID {
			t.Fatal("账号达到上限后应使用其他空闲账号")
		}
		close(releaseMiddle)
		synctest.Wait()
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	})
}

func TestQuotaPriorityKeepsSubmittedTaskOnItsAccount(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "已发送")
	second := addNamed(t, st, "豆包", "剩余额度少")
	if err := st.RecordUse(second.ID, time.Now(), 90, 0, 0); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(st, nil, nil, "")
	e.opts = Options{Platforms: []string{"豆包"}, DailyCap: 100}
	queue := []Task{{Index: 1, Platform: "豆包", BrowserID: first.ID, Status: StatusPending, submitted: true}}
	e.view = NewQueueView(queue)
	var used int64
	err := e.runPlatforms(context.Background(), nil, queue, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		used = task.BrowserID
		return nil
	})
	if err != nil || used != first.ID {
		t.Fatalf("已发送任务必须留在原账号: used=%d err=%v", used, err)
	}
}

func TestSubmittedTaskWaitsForItsBusyAccount(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "已发送")
	addNamed(t, st, "豆包", "空闲")
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		e.opts = Options{Platforms: []string{"豆包"}}
		queue := []Task{
			{Index: 1, Platform: "豆包", BrowserID: first.ID, Status: StatusPending, submitted: true},
			{Index: 2, Platform: "豆包", BrowserID: first.ID, Status: StatusPending, submitted: true},
		}
		e.view = NewQueueView(queue)
		started := make(chan Task, 2)
		release := make(chan struct{})
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
		}()
		finished := make(chan error, 1)
		go func() {
			finished <- e.runPlatforms(context.Background(), nil, queue, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
				started <- *task
				if task.Index == 1 {
					<-release
				}
				return nil
			})
		}()
		synctest.Wait()
		if len(started) != 1 {
			t.Fatalf("已发送任务应等待原账号: started=%d", len(started))
		}
		<-started
		close(release)
		synctest.Wait()
		if len(started) != 1 {
			t.Fatalf("原账号空闲后应继续执行: started=%d", len(started))
		}
		if task := <-started; task.Index != 2 || task.BrowserID != first.ID {
			t.Fatalf("已发送任务被改派: %+v", task)
		}
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	})
}
