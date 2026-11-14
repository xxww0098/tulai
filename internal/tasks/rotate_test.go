package tasks

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/heal"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestGenericFailureSwitchesToAnotherAccount(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	var used []int64
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png"}, Concurrency: 1, RetryCount: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		used = append(used, task.BrowserID)
		if task.Attempts == 1 {
			return errors.New("页面返回失败")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	queue := e.View().Snapshot()
	if len(queue) != 1 || queue[0].Status != StatusSuccess {
		t.Fatalf("generic failure should retry on another account: %+v", queue)
	}
	if len(used) < 2 || used[0] == used[1] {
		t.Fatalf("used = %v, want the retry on another account", used)
	}
	if !browserByID(t, st, first.ID).Usable(time.Now()) {
		t.Fatal("a generic failure should not mark the first account unavailable")
	}
}

func TestQuotaSwitchesToOtherAccount(t *testing.T) {
	st := openRotateStore(t)
	first, second := addNamed(t, st, "豆包", "甲"), addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	var mu sync.Mutex
	var used []int64
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		mu.Lock()
		used = append(used, task.BrowserID)
		mu.Unlock()
		if task.BrowserID == first.ID {
			return strategy.ErrAccountQuota
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	queue := e.View().Snapshot()
	for _, task := range queue {
		if task.Status != StatusSuccess {
			t.Fatalf("quota on one account should not fail the batch: %+v used=%v", task, used)
		}
	}
	hitFirst := false
	for _, id := range used {
		if id == first.ID {
			hitFirst = true
		}
	}
	if len(used) < 2 || !hitFirst {
		t.Fatalf("quota account should be tried, then the other account finishes the batch: %v", used)
	}
	health := browserByID(t, st, first.ID)
	if health.Health != store.HealthExhausted || health.Usable(time.Now()) {
		t.Fatalf("exhausted account should stay out: %+v", health)
	}
	if browserByID(t, st, second.ID).Usable(time.Now()) == false {
		t.Fatal("the other account should stay available")
	}
}

func TestQuotaOnOnePlatformLeavesTheOtherRunning(t *testing.T) {
	st := openRotateStore(t)
	addNamed(t, st, "豆包", "甲")
	addNamed(t, st, "千问", "乙")
	e := NewEngine(st, nil, nil, "")
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包", "千问"}, Images: []string{"a.png"}, Concurrency: 2,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		if task.Platform == "豆包" {
			return errors.New("今日免费次数已用完")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	queue := e.View().Snapshot()
	if len(queue) != 2 || queue[0].Status != StatusFailed || queue[1].Status != StatusSuccess {
		t.Fatalf("only the exhausted platform should fail: %+v", queue)
	}
	if queue[0].Attempts != 1 {
		t.Fatalf("quota should not burn retries: %+v", queue[0])
	}
}

func TestCaptchaWithOneAccountStillStopsTheBatch(t *testing.T) {
	st := openRotateStore(t)
	addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	calls := 0
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
		calls++
		return captcha.ErrVerificationRequired
	})
	queue := e.View().Snapshot()
	if err != nil || calls != 1 || queue[0].Status != StatusNeedsUser || queue[0].Error == "已停止" || queue[1].Status == StatusSkipped || queue[1].Error == "已停止" {
		t.Fatalf("captcha must not cancel the batch: calls=%d err=%v queue=%+v", calls, err, queue)
	}
	if queue[1].Status != StatusFailed || !strings.Contains(queue[1].Error, "验证") {
		t.Fatalf("the next task should stop on the account that needs the user, not hang: %+v", queue[1])
	}
}

func TestCaptchaSwitchesWhenAnotherAccountIsFree(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	addNamed(t, st, "豆包", "乙")
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png"}, Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		if task.BrowserID == first.ID {
			return captcha.ErrVerificationRequired
		}
		return nil
	})
	queue := e.View().Snapshot()
	if err != nil || len(queue) != 1 || queue[0].Status != StatusSuccess || queue[0].BrowserID == first.ID {
		t.Fatalf("captcha should move the task: err=%v queue=%+v", err, queue)
	}
	if label := browserByID(t, st, first.ID); label.Health != store.HealthCooling {
		t.Fatalf("captcha account should cool: %+v", label)
	}
}

func TestSubmittedCaptchaDoesNotResendOnAnotherAccount(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	var calls atomic.Int32
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		calls.Add(1)
		if task.BrowserID == first.ID {
			task.submitted = true
			return captcha.ErrVerificationRequired
		}
		return nil
	})
	queue := e.View().Snapshot()
	if err != nil || calls.Load() < 2 || queue[0].Status != StatusNeedsUser || queue[0].submitted != true || queue[1].Status != StatusSuccess {
		t.Fatalf("a sent captcha must not be replayed, and the batch should continue: calls=%d err=%v queue=%+v", calls.Load(), err, queue)
	}
}

func TestUncertainSendNeverRetriesOnAnotherAccount(t *testing.T) {
	for _, platform := range []string{"豆包", "千问", "ChatGPT"} {
		t.Run(platform, func(t *testing.T) {
			st := openRotateStore(t)
			addNamed(t, st, platform, "甲")
			addNamed(t, st, platform, "乙")
			e := NewEngine(st, nil, nil, "")
			var calls atomic.Int32
			err := e.run(context.Background(), Options{
				Platforms: []string{platform}, Prompts: []string{"画猫", "画狗"}, Concurrency: 1, RetryCount: 3,
			}, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
				calls.Add(1)
				return strategy.ErrSubmissionUncertain
			})
			queue := e.View().Snapshot()
			if err != nil || calls.Load() != 2 || len(queue) != 2 {
				t.Fatalf("each task must stop after its first uncertain send: calls=%d err=%v queue=%+v", calls.Load(), err, queue)
			}
			for _, task := range queue {
				if task.Status != StatusFailed || task.Attempts != 1 || task.rotated || !strings.Contains(task.Error, "未确认") {
					t.Fatalf("uncertain send must fail only its own task: %+v", task)
				}
			}
		})
	}
}

func TestUncertainSendPrecedesOtherRecovery(t *testing.T) {
	for _, platform := range []string{"豆包", "千问", "ChatGPT", "Antigravity"} {
		for _, cause := range []error{heal.ErrNetwork, heal.ErrCrashed, captcha.ErrVerificationRequired, errHumanGaveUp, strategy.ErrAccountQuota, strategy.ErrAccountRate, strategy.ErrModeration, strategy.ErrPlatformReply} {
			t.Run(platform+"/"+cause.Error(), func(t *testing.T) {
				e := NewEngine(nil, nil, nil, "")
				e.opts.RetryCount = 3
				failure := errors.Join(strategy.ErrSubmissionUncertain, cause)
				calls := 0
				got := e.execute(context.Background(), Task{Platform: platform, BrowserID: 7, Results: []string{"saved.png"}}, func(context.Context, *Task) error {
					calls++
					return failure
				})
				if calls != 1 || got.Status != StatusFailed || got.BrowserID != 7 || got.rotated || got.Error != failure.Error() || len(got.Results) != 1 {
					t.Fatalf("uncertain send entered automatic recovery: calls=%d task=%+v", calls, got)
				}
			})
		}
	}
}

func TestAcceptedTasksNeverRotateOnAccountFault(t *testing.T) {
	for _, platform := range []string{"豆包", "千问", "ChatGPT"} {
		for _, failure := range []error{strategy.ErrAccountQuota, strategy.ErrAccountRate, errExtOffline, errDoubaoLogin, captcha.ErrVerificationRequired, errors.New("额度不足")} {
			t.Run(platform+"/"+failure.Error(), func(t *testing.T) {
				st := openRotateStore(t)
				owner := addNamed(t, st, platform, "原账号")
				addNamed(t, st, platform, "其他账号")
				e := NewEngine(st, nil, nil, "")
				task := Task{Platform: platform, BrowserID: owner.ID, submitted: true, sentURL: "original", sentWrap: "original-wrap", Results: []string{"saved.png"}}
				if e.maybeRotate(&task, failure, time.Now()) || task.BrowserID != owner.ID || !task.submitted || task.rotated || task.sentURL != "original" || task.sentWrap != "original-wrap" || len(task.Results) != 1 {
					t.Fatalf("accepted task lost its original generation: %+v", task)
				}
			})
		}
	}
}

func TestUncertainSendRetainsCancellationAndTimeoutPriority(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			e := NewEngine(nil, nil, nil, "")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause, want := error(context.Canceled), StatusSkipped
			if expired {
				cause, want = context.DeadlineExceeded, StatusFailed
			}
			got := e.execute(ctx, Task{}, func(context.Context, *Task) error {
				if !expired {
					cancel()
				}
				return errors.Join(strategy.ErrSubmissionUncertain, cause)
			})
			if got.Status != want || (expired && (!strings.Contains(got.Error, "出图超时") || !strings.Contains(got.Error, strategy.ErrSubmissionUncertain.Error()))) || (!expired && got.Error != "已停止") {
				t.Fatalf("context outcome must keep priority: %+v", got)
			}
		})
	}
}

func TestAuthSwitchesAccount(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	addNamed(t, st, "豆包", "乙")
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png"}, Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		if task.BrowserID == first.ID {
			return errDoubaoLogin
		}
		return nil
	})
	got := browserByID(t, st, first.ID)
	queue := e.View().Snapshot()
	if err != nil || queue[0].Status != StatusSuccess || got.Health != store.HealthExhausted || got.CoolUntil != 0 {
		t.Fatalf("logged-out account should wait for 恢复: err=%v health=%+v queue=%+v", err, got, queue)
	}
}

func TestDailyCapSkipsFullAccount(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	second := addNamed(t, st, "豆包", "乙")
	st.AddUse(first.ID, time.Now())
	e := NewEngine(st, nil, nil, "")
	var used []int64
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 0, DailyCap: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		used = append(used, task.BrowserID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	queue := e.View().Snapshot()
	if len(used) != 1 || used[0] != second.ID {
		t.Fatalf("full account should be skipped: used=%v queue=%+v", used, queue)
	}
	if queue[0].Status != StatusSuccess || queue[1].Status != StatusFailed || !strings.Contains(queue[1].Error, "今日已达上限") {
		t.Fatalf("second task has nowhere to go: %+v", queue)
	}
	if browserByID(t, st, first.ID).UsedToday(time.Now()) != 1 || browserByID(t, st, second.ID).UsedToday(time.Now()) != 1 {
		t.Fatalf("counts = 甲 %d 乙 %d", browserByID(t, st, first.ID).UsedToday(time.Now()), browserByID(t, st, second.ID).UsedToday(time.Now()))
	}
}

func TestDailyCapAllFullFailsClosed(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	second := addNamed(t, st, "豆包", "乙")
	now := time.Now()
	st.AddUse(first.ID, now)
	st.AddUse(second.ID, now)
	e := NewEngine(st, nil, nil, "")
	calls := 0
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png"}, Concurrency: 0, DailyCap: 1,
	}, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
		calls++
		return nil
	})
	queue := e.View().Snapshot()
	if err != nil || calls != 0 || len(queue) != 1 || queue[0].Status != StatusFailed || !strings.Contains(queue[0].Error, "今日已达上限") {
		t.Fatalf("calls=%d err=%v queue=%+v", calls, err, queue)
	}
}

func TestDailyCapUnderLimitStillRunsTogether(t *testing.T) {
	st := openRotateStore(t)
	addNamed(t, st, "豆包", "甲")
	addNamed(t, st, "豆包", "乙")
	got := overlapStarted(t, st, Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 0, DailyCap: 1,
	})
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("two accounts under the cap should run together: %v", got)
	}
}

func openRotateStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	st.Set("browser_backend", "Chrome")
	return st
}

func addNamed(t *testing.T, st *store.Store, platform, name string) store.Browser {
	t.Helper()
	b, err := st.AddBrowser(platform, name, "")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func browserByID(t *testing.T, st *store.Store, id int64) store.Browser {
	t.Helper()
	for _, b := range st.Browsers() {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("missing browser %d", id)
	return store.Browser{}
}

func TestFailureRotationWaitsForBusyTargetAccount(t *testing.T) {
	for _, tc := range []struct {
		name string
		wash bool
	}{
		{name: "single", wash: false},
		{name: "wash_batch", wash: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := openRotateStore(t)
			if tc.wash {
				st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
				st.Set(extbridge.SettingExtPort, freePort(t))
			}
			first := addNamed(t, st, "豆包", "豆包1")
			second := addNamed(t, st, "豆包", "豆包2")
			e := NewEngine(st, nil, nil, "")
			e.gap = func(time.Duration) time.Duration { return 0 }
			defer e.CloseSessions()

			opts := Options{
				Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: []string{"a.png", "b.png"},
				Concurrency: 2, RetryCount: 1,
			}
			if tc.wash {
				opts.Images = []string{"1.png", "2.png", "3.png", "4.png"}
				opts.Wash = true
				opts.WaitResult = true
			}

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			releaseFirst := make(chan struct{})
			firstStarted := make(chan struct{}, 1)
			secondFailed := make(chan struct{}, 1)
			var mu sync.Mutex
			activeOn := map[int64]int{}
			var overlapErr error
			var attempts []int64
			done := make(chan error, 1)

			go func() {
				done <- e.RunObserved(ctx, opts, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
					mu.Lock()
					if activeOn[task.BrowserID] > 0 && overlapErr == nil {
						overlapErr = fmt.Errorf("account %d ran concurrently", task.BrowserID)
					}
					activeOn[task.BrowserID]++
					attempts = append(attempts, task.BrowserID)
					mu.Unlock()
					defer func() {
						mu.Lock()
						activeOn[task.BrowserID]--
						mu.Unlock()
					}()

					if task.BrowserID == second.ID && task.Attempts == 1 {
						select {
						case secondFailed <- struct{}{}:
						default:
						}
						return errors.New("未找到「图像生成」入口")
					}
					if task.BrowserID == first.ID {
						select {
						case firstStarted <- struct{}{}:
						default:
						}
						select {
						case <-releaseFirst:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					for i := range task.batch {
						task.batch[i].Results = []string{task.batch[i].Images[0] + ".out"}
					}
					if len(task.batch) == 0 && len(task.Images) > 0 {
						task.Results = []string{task.Images[0] + ".out"}
					}
					return nil
				})
			}()

			select {
			case <-firstStarted:
			case <-ctx.Done():
				t.Fatal("first account did not start")
			}
			select {
			case <-secondFailed:
			case <-ctx.Done():
				t.Fatal("second account did not run first attempt")
			}
			// Wait past the 3s retry backoff while 豆包1 is still busy:
			// the rotated batch from 豆包2 must wait for 豆包1's slot instead of hijacking 豆包1 or bouncing back to 豆包2.
			time.Sleep(3300 * time.Millisecond)
			mu.Lock()
			gotOverlap := overlapErr
			attemptCountBeforeRelease := len(attempts)
			mu.Unlock()
			if gotOverlap != nil {
				close(releaseFirst)
				t.Fatal(gotOverlap)
			}
			if attemptCountBeforeRelease != 2 {
				close(releaseFirst)
				t.Fatalf("expected rotated task to wait while 豆包1 was busy, got attempts before release: %v", attempts)
			}
			close(releaseFirst)

			if err := <-done; err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			if overlapErr != nil {
				mu.Unlock()
				t.Fatal(overlapErr)
			}
			mu.Unlock()
			for _, task := range e.View().Snapshot() {
				if task.Status != StatusSuccess {
					t.Fatalf("expected all tasks to succeed after waiting for target slot: %+v", task)
				}
			}
		})
	}
}

func TestFailureRotationAlternatesAccountsWithUpdatedRanOn(t *testing.T) {
	st := openRotateStore(t)
	first := addNamed(t, st, "豆包", "甲")
	second := addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	e.gap = func(time.Duration) time.Duration { return 0 }
	var used []int64
	var ranOns []int64
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png"}, Concurrency: 1, RetryCount: 2,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		used = append(used, task.BrowserID)
		ranOns = append(ranOns, task.ranOn)
		if task.Attempts <= 2 {
			return errors.New("页面返回失败")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{first.ID, second.ID, first.ID}
	if len(used) != 3 || used[0] != want[0] || used[1] != want[1] || used[2] != want[2] {
		t.Fatalf("used = %v, want %v", used, want)
	}
	if len(ranOns) != 3 || ranOns[0] != want[0] || ranOns[1] != want[1] || ranOns[2] != want[2] {
		t.Fatalf("ranOn = %v, want %v", ranOns, want)
	}
}
