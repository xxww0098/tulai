package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestRunDispatchesIdleBrowserPastBusyTask(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	for _, platform := range []string{"豆包", "千问", "千问"} {
		if _, err := st.AddBrowser(platform, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release := make(chan struct{})
		started := make(chan int, 4)
		finished := make(chan error, 1)
		e := NewEngine(st, nil, nil, "")
		opts := Options{Platforms: []string{"豆包", "千问"}, Images: []string{"a.png", "b.png"}, Concurrency: 2}
		go func() {
			finished <- e.run(ctx, opts, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				started <- task.Index
				if task.Index == 1 {
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			})
		}()
		synctest.Wait()
		seen := map[int]bool{}
		for len(started) > 0 {
			seen[<-started] = true
		}
		if !seen[1] || !seen[2] || !seen[4] || seen[3] {
			t.Fatalf("忙碌浏览器的任务 #3 应等待，空闲浏览器的 #4 应先运行: %v", seen)
		}
		close(release)
		synctest.Wait()
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		for _, task := range e.View().Snapshot() {
			if task.Status != StatusSuccess {
				t.Fatalf("任务未完成: %+v", task)
			}
		}
	})
}

func TestRunSendWaitPerResourceWithoutTrailingDelay(t *testing.T) {
	for _, backend := range []string{"Chrome", "Edge"} {
		t.Run(backend, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			st.Set("browser_backend", backend)
			opts := Options{Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 4, SendWait: 10}
			if _, err := st.AddBrowser("豆包", "test", ""); err != nil {
				t.Fatal(err)
			}
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				var times []time.Time
				e := NewEngine(st, nil, nil, "")
				err := e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
					times = append(times, time.Now())
					return nil
				})
				gap := time.Duration(0)
				if len(times) == 2 {
					gap = times[1].Sub(times[0])
				}
				if err != nil || len(times) != 2 || gap < 5*time.Second || gap > 15*time.Second || time.Since(start) != gap {
					t.Fatalf("同资源间隔应落在 5–15 秒，末项结束不再等待: times=%v elapsed=%v err=%v", times, time.Since(start), err)
				}
			})
		})
	}
}

func TestRunCoolingBrowserDoesNotOccupyWorker(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	for _, platform := range []string{"豆包", "千问"} {
		if _, err := st.AddBrowser(platform, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var mu sync.Mutex
		var times []time.Duration
		e := NewEngine(st, nil, nil, "")
		e.gap = func(d time.Duration) time.Duration { return d }
		opts := Options{Platforms: []string{"豆包", "千问"}, Images: []string{"a.png", "b.png"}, SendWait: 10}
		err := e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
			mu.Lock()
			times = append(times, time.Since(start))
			mu.Unlock()
			return nil
		})
		var at0, at10 int
		for _, elapsed := range times {
			switch elapsed {
			case 0:
				at0++
			case 10 * time.Second:
				at10++
			}
		}
		if err != nil || len(times) != 4 || at0 != 2 || at10 != 2 {
			t.Fatalf("两个平台应同时开跑，各自的下一单等 10 秒: times=%v err=%v", times, err)
		}
	})
}

func TestRunStopDuringResourceCooldown(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	addBrowsers(t, st, "豆包")
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		opts := Options{Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png", "c.png"}, Concurrency: 4, SendWait: 60}
		start := time.Now()
		time.AfterFunc(time.Second, e.Stop)
		calls := 0
		err := e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
			calls++
			return nil
		})
		queue := e.View().Snapshot()
		if err != nil || calls != 1 || time.Since(start) != time.Second || queue[1].Status != StatusPending || queue[2].Status != StatusPending {
			t.Fatalf("停止应立即打断间隔，后续任务不得启动: calls=%d elapsed=%v queue=%+v err=%v", calls, time.Since(start), queue, err)
		}
	})
}

func TestRunStopCancelsActiveTaskAndPreservesResults(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	addBrowsers(t, st, "豆包")
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		opts := Options{Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 4, RetryCount: 3}
		finished := make(chan error, 1)
		calls := 0
		go func() {
			finished <- e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				calls++
				task.Results = []string{"saved.png"}
				<-ctx.Done()
				return ctx.Err()
			})
		}()
		synctest.Wait()
		start := time.Now()
		e.Stop()
		synctest.Wait()
		if err := <-finished; err != nil || e.IsRunning() || calls != 1 || time.Since(start) != 0 {
			t.Fatalf("停止应立即取消活动任务且不重试: calls=%d running=%t elapsed=%v err=%v", calls, e.IsRunning(), time.Since(start), err)
		}
		queue := e.View().Snapshot()
		if queue[0].Status != StatusSkipped || queue[0].Error != "已停止" || len(queue[0].Results) != 1 {
			t.Fatalf("停止须保留已保存的部分结果: %+v", queue[0])
		}
		for _, task := range queue[1:] {
			if task.Status != StatusPending || task.Attempts != 0 {
				t.Fatalf("停止后待执行任务不得提交: %+v", task)
			}
		}
	})
}

func TestExecuteWaitsOnlyBetweenRetries(t *testing.T) {
	for _, retries := range []int{0, 2} {
		synctest.Test(t, func(t *testing.T) {
			e := NewEngine(nil, nil, nil, "")
			e.opts.RetryCount = retries
			start := time.Now()
			got := e.execute(context.Background(), Task{}, func(context.Context, *Task) error { return errors.New("failed") })
			if got.Status != StatusFailed || got.Attempts != retries+1 || time.Since(start) != time.Duration(retries)*3*time.Second {
				t.Fatalf("只应在仍有重试时等待: task=%+v elapsed=%v", got, time.Since(start))
			}
		})
	}
}

func TestExecuteCancellationDuringRetryWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		e := NewEngine(nil, nil, nil, "")
		e.opts.RetryCount = 2
		time.AfterFunc(time.Second, cancel)
		got := e.execute(ctx, Task{Results: []string{"saved.png"}}, func(context.Context, *Task) error { return errors.New("failed") })
		if got.Status != StatusSkipped || got.Attempts != 1 || got.Error != "已停止" || len(got.Results) != 1 {
			t.Fatalf("重试等待期间取消应保留结果并显示已停止: %+v", got)
		}
	})
}

func TestRunUncertainSubmissionDoesNotStopTheBatch(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	addBrowsers(t, st, "豆包")
	e := NewEngine(st, nil, nil, "")
	opts := Options{Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png", "c.png", "d.png"}, Concurrency: 4, RetryCount: 3}
	strat := &resumeStrategy{failure: "send", err: strategy.ErrSubmissionUncertain}
	calls := 0
	err = e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		calls++
		return e.submitTask(task, strat, nil)
	})
	queue := e.View().Snapshot()
	if err != nil || calls != 4 || queue[0].Status != StatusFailed || queue[0].Attempts != 1 || queue[0].submitted {
		t.Fatalf("发送未确认只失败这一条，不能重发也不能停批: calls=%d queue=%+v err=%v", calls, queue, err)
	}
	for _, task := range queue[1:] {
		if task.Status != StatusSuccess || task.Error == "已停止" {
			t.Fatalf("其他任务应继续跑完: %+v", task)
		}
	}
}

func TestOneFailedWorkerDoesNotCancelTheOther(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	first, err := st.AddBrowser("豆包", "豆包1", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.AddBrowser("豆包", "豆包2", "")
	if err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan int64, 8)
		finished := make(chan error, 1)
		e := NewEngine(st, nil, nil, "")
		opts := Options{Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 2, RetryCount: 3}
		go func() {
			finished <- e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				started <- task.BrowserID
				if task.BrowserID == second.ID {
					return strategy.ErrSubmissionUncertain
				}
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		synctest.Wait()
		sawFailed := 0
		for len(started) > 0 {
			if id := <-started; id == second.ID {
				sawFailed++
			}
		}
		close(release)
		synctest.Wait()
		err := <-finished
		if err != nil || sawFailed != 1 {
			t.Fatalf("失败的账号应先跑一次且不能卡住批次: saw=%d err=%v", sawFailed, err)
		}
		queue := e.View().Snapshot()
		if len(queue) != 2 {
			t.Fatalf("queue: %+v", queue)
		}
		for _, task := range queue {
			if task.Status != StatusSuccess || task.Error == "已停止" {
				t.Fatalf("一条失败不能取消另一条: first=%d second=%d task=%+v", first.ID, second.ID, task)
			}
		}
		retried := 0
		for len(started) > 0 {
			if id := <-started; id == first.ID {
				retried++
			}
		}
		if retried < 1 {
			t.Fatal("未确认的发送应改到另一个空闲账号再试一次")
		}
	})
}

// overlapStarted 跑到第一批任务都堵在 attempt 里，返回已经开始的浏览器 ID。
func overlapStarted(t *testing.T, st *store.Store, opts Options) []int64 {
	t.Helper()
	var got []int64
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		started := make(chan int64, 8)
		finished := make(chan error, 1)
		e := NewEngine(st, nil, nil, "")
		go func() {
			finished <- e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				started <- task.BrowserID
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		synctest.Wait()
		for len(started) > 0 {
			got = append(got, <-started)
		}
		close(release)
		synctest.Wait()
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	})
	return got
}

func TestAutoConcurrencyRunsOneJobPerAccount(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	for range 3 {
		if _, err := st.AddBrowser("豆包", "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	got := overlapStarted(t, st, Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png", "c.png", "d.png"},
	})
	if len(got) != 2 {
		t.Fatalf("豆包同时最多 2 条，实际 %v", got)
	}
	seen := map[int64]bool{}
	for _, id := range got {
		if seen[id] {
			t.Fatalf("同一账号跑了两条: %v", got)
		}
		seen[id] = true
	}
}

func TestManualConcurrencyCapsBelowAccountCount(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	for range 3 {
		if _, err := st.AddBrowser("豆包", "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	got := overlapStarted(t, st, Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png", "c.png", "d.png"}, Concurrency: 2,
	})
	if len(got) != 2 {
		t.Fatalf("手动上限 2 时应只跑 2 条: %v", got)
	}
}

func TestAutoConcurrencyFollowsAccountRecovery(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	first, err := st.AddBrowser("豆包", "甲", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.AddBrowser("豆包", "乙", "")
	if err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		until := time.Now().Add(5 * time.Second).Unix()
		st.UpdateBrowserHealth(first.ID, store.HealthCooling, until, "冷却")
		st.UpdateBrowserHealth(second.ID, store.HealthCooling, until, "冷却")
		release := make(chan struct{})
		started := make(chan int64, 4)
		finished := make(chan error, 1)
		e := NewEngine(st, nil, nil, "")
		go func() {
			finished <- e.run(context.Background(), Options{
				Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"},
			}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				started <- task.BrowserID
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		synctest.Wait()
		if len(started) != 0 {
			t.Fatalf("冷却中的账号不应出图: %d", len(started))
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if len(started) != 2 {
			t.Fatalf("两个账号恢复后应同时出图: %d", len(started))
		}
		close(release)
		synctest.Wait()
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	})
}

func TestAutoConcurrencyPicksUpRecoveredAccount(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	first, err := st.AddBrowser("豆包", "甲", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.AddBrowser("豆包", "乙", "")
	if err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		st.UpdateBrowserHealth(second.ID, store.HealthCooling, time.Now().Add(5*time.Second).Unix(), "冷却")
		release := make(chan struct{})
		started := make(chan int64, 4)
		finished := make(chan error, 1)
		e := NewEngine(st, nil, nil, "")
		go func() {
			finished <- e.run(context.Background(), Options{
				Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"},
			}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				started <- task.BrowserID
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()
		synctest.Wait()
		if len(started) != 1 {
			t.Fatalf("冷却期间只应跑甲: %d", len(started))
		}
		if id := <-started; id != first.ID {
			t.Fatalf("冷却期间只应跑甲: %d", id)
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if len(started) != 1 {
			t.Fatalf("乙恢复后应和甲一起出图: %d", len(started))
		}
		if id := <-started; id != second.ID {
			t.Fatalf("第二单应落到乙: %d", id)
		}
		close(release)
		synctest.Wait()
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	})
}

func TestExtensionAccountsRunInParallel(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set(extbridge.SettingBrowserBackend, "extension")
	if _, err := st.UpsertExtensionAccount("千问", "profile-a", "甲"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertExtensionAccount("千问", "profile-b", "乙"); err != nil {
		t.Fatal(err)
	}
	got := overlapStarted(t, st, Options{
		Platforms: []string{"千问"}, Images: []string{"a.png", "b.png", "c.png"},
	})
	if len(got) != 2 {
		t.Fatalf("两个扩展配置文件应同时出图: %v", got)
	}
	if got[0] == got[1] {
		t.Fatalf("两个任务落在同一个扩展账号: %v", got)
	}
}

func TestExtensionPlaceholderIDsDoNotShareASlot(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set(extbridge.SettingBrowserBackend, "extension")
	got := overlapStarted(t, st, Options{
		Platforms: []string{"豆包", "千问"}, Images: []string{"a.png"}, Concurrency: 2,
	})
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("扩展占位 -1 与 -2 应各占一个名额: %v", got)
	}
}

func TestSameExtensionProfileStaysSerial(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set(extbridge.SettingBrowserBackend, "extension")
	qianwen, err := st.UpsertExtensionAccount("千问", "profile-a", "甲")
	if err != nil {
		t.Fatal(err)
	}
	doubao, err := st.UpsertExtensionAccount("豆包", "profile-a", "甲")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.UpsertExtensionAccount("豆包", "profile-b", "乙")
	if err != nil {
		t.Fatal(err)
	}
	got := overlapStarted(t, st, Options{
		Platforms: []string{"豆包", "千问"}, Images: []string{"a.png", "b.png"},
	})
	seen := map[int64]bool{}
	for _, id := range got {
		seen[id] = true
	}
	if seen[qianwen.ID] && seen[doubao.ID] {
		t.Fatalf("同一扩展配置文件的千问和豆包不应同时出图: %v", got)
	}
	if !seen[other.ID] {
		t.Fatalf("另一个配置文件应同时出图: %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("应有两个配置文件在跑: %v", got)
	}
}

func TestTwoAccountsOverlapOneTaskTime(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	if _, err := st.AddBrowser("豆包", "豆包1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBrowser("豆包", "豆包2", ""); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		var cur, max atomic.Int32
		start := time.Now()
		e := NewEngine(st, nil, nil, "")
		e.gap = func(d time.Duration) time.Duration { return d }
		err := e.run(context.Background(), Options{
			Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"},
		}, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
			n := cur.Add(1)
			for {
				old := max.Load()
				if n <= old || max.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(200 * time.Millisecond)
			cur.Add(-1)
			return nil
		})
		elapsed := time.Since(start)
		if err != nil || max.Load() != 2 || elapsed < 200*time.Millisecond || elapsed > 350*time.Millisecond {
			t.Fatalf("two accounts should overlap: max=%d elapsed=%s err=%v", max.Load(), elapsed, err)
		}
	})
}

func TestQianwenIsNotBlockedByDoubao(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	if _, err := st.AddBrowser("豆包", "豆包1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddBrowser("千问", "千问1", ""); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var qianwenDone atomic.Bool
		finished := make(chan error, 1)
		e := NewEngine(st, nil, nil, "")
		go func() {
			finished <- e.run(context.Background(), Options{
				Platforms: []string{"豆包", "千问"}, Images: []string{"a.png"},
			}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				if task.Platform == "豆包" {
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				qianwenDone.Store(true)
				return nil
			})
		}()
		synctest.Wait()
		if !qianwenDone.Load() {
			t.Fatal("千问应在豆包还堵着的时候完成")
		}
		close(release)
		synctest.Wait()
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	})
}

func TestFourInFlightAcrossPlatforms(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	for range 2 {
		if _, err := st.AddBrowser("豆包", "豆包", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddBrowser("千问", "千问", ""); err != nil {
			t.Fatal(err)
		}
	}
	got := overlapStarted(t, st, Options{
		Platforms: []string{"豆包", "千问"}, Images: []string{"a.png", "b.png"},
	})
	if len(got) != 4 {
		t.Fatalf("两个平台应同时跑 4 条: %v", got)
	}
	seen := map[int64]int{}
	for _, id := range got {
		seen[id]++
		if seen[id] > 1 {
			t.Fatalf("同一账号跑了两条: %v", got)
		}
	}
}
