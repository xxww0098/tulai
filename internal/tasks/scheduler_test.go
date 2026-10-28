package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

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
				if err != nil || len(times) != 2 || times[1].Sub(times[0]) != 10*time.Second || time.Since(start) != 10*time.Second {
					t.Fatalf("同资源须间隔10秒，末项结束不再等待: times=%v elapsed=%v err=%v", times, time.Since(start), err)
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
		var indexes []int
		var times []time.Duration
		e := NewEngine(st, nil, nil, "")
		opts := Options{Platforms: []string{"豆包", "千问"}, Images: []string{"a.png", "b.png"}, Concurrency: 1, SendWait: 10}
		err := e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
			indexes = append(indexes, task.Index)
			times = append(times, time.Since(start))
			return nil
		})
		if err != nil || len(times) != 4 || times[0] != 0 || times[1] != 0 || times[2] != 10*time.Second || times[3] != 10*time.Second {
			t.Fatalf("浏览器冷却不能阻塞独立浏览器: times=%v err=%v", times, err)
		}
		for i, index := range indexes {
			if index != i+1 {
				t.Fatalf("单并发且资源就绪时应保持队列顺序: %v", indexes)
			}
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

func TestRunUncertainSubmissionStopsWithoutResend(t *testing.T) {
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
	if err != nil || calls != 1 || queue[0].Status != StatusFailed || queue[0].Attempts != 1 || queue[0].submitted {
		t.Fatalf("发送状态不确定应停止批次，不能重发: calls=%d queue=%+v err=%v", calls, queue, err)
	}
	for _, task := range queue[1:] {
		if task.Status != StatusPending || task.Attempts != 0 {
			t.Fatalf("不确定的生成仍可能进行，不能启动后续任务: %+v", task)
		}
	}
}
