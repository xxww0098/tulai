package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/store"
)

func TestReadyAtWakeupDoesNotStarveOtherAccountWhileOneBusy(t *testing.T) {
	st := openRotateStore(t)
	a := addNamed(t, st, "豆包", "甲")
	b := addNamed(t, st, "豆包", "乙")
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		e.gap = func(d time.Duration) time.Duration { return d }
		releaseA := make(chan struct{})
		releaseB2 := make(chan struct{})
		started := make(chan struct {
			idx int
			id  int64
		}, 6)
		var bCount atomic.Int32
		done := make(chan error, 1)
		go func() {
			done <- e.RunObserved(context.Background(), Options{
				Platforms:   []string{"豆包"},
				Images:      []string{"1.png", "2.png", "3.png"},
				Concurrency: 2,
				SendWait:    2,
			}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				started <- struct {
					idx int
					id  int64
				}{task.Index, task.BrowserID}
				if task.BrowserID == a.ID {
					select {
					case <-releaseA:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				if bCount.Add(1) == 1 {
					return nil
				}
				select {
				case <-releaseB2:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()

		// 初始时甲占住第一单，乙需等同出口错峰 2.5s 后接第二单；推进 3s 让两单都开跑且乙立刻完成进入 2s 发送间隔。
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if len(started) != 2 {
			close(releaseA)
			close(releaseB2)
			synctest.Wait()
			t.Fatalf("expected 2 initial starts (a busy, b finished into gap), got %d", len(started))
		}
		<-started
		<-started

		// 再推进 2s 让乙的发送间隔到期，此时甲仍阻塞，第三单必须立刻由乙接手而不是饿死等甲结束。
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case got := <-started:
			if got.id != b.ID {
				close(releaseA)
				close(releaseB2)
				synctest.Wait()
				t.Fatalf("third task ran on account %d, want idle account %d", got.id, b.ID)
			}
		default:
			close(releaseA)
			close(releaseB2)
			synctest.Wait()
			t.Fatal("third task starved while account b exited send-wait and account a was still busy")
		}
		close(releaseA)
		close(releaseB2)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestSharedExitStaggerDoesNotBlockIndependentProxyAccount(t *testing.T) {
	st := openRotateStore(t)
	a := addNamed(t, st, "豆包", "直连1")
	b := addNamed(t, st, "豆包", "直连2")
	c := addNamed(t, st, "豆包", "独享代理")
	if err := st.SetBrowserProxy(c.ID, "http://10.0.0.2:8080"); err != nil {
		t.Fatal(err)
	}
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		e.gap = func(time.Duration) time.Duration { return 0 }
		release := make(chan struct{})
		started := make(chan int64, 4)
		done := make(chan error, 1)
		go func() {
			done <- e.RunObserved(context.Background(), Options{
				Platforms:   []string{"豆包"},
				Images:      []string{"1.png", "2.png"},
				Concurrency: 2,
				SendWait:    1, // 启用共享出口错峰检查
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
		// 不推进时间（t=0，远早于 2.5s 错峰）：直连账号占一个并发位后，独享代理账号 c 应立刻接第二单。
		synctest.Wait()
		var got []int64
		for len(started) > 0 {
			got = append(got, <-started)
		}
		close(release)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || !slices.Contains(got, c.ID) || (!slices.Contains(got, a.ID) && !slices.Contains(got, b.ID)) {
			t.Fatalf("expected immediate overlap with independent proxy account %d, got %v (a=%d b=%d)", c.ID, got, a.ID, b.ID)
		}
	})
}

func TestSharedExitStaggerDoesNotSelfBlockSameAccount(t *testing.T) {
	st := openRotateStore(t)
	a := addNamed(t, st, "豆包", "甲")
	b := addNamed(t, st, "豆包", "乙")
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		e.gap = func(time.Duration) time.Duration { return 0 }
		type record struct {
			id int64
			at time.Duration
		}
		base := time.Now()
		var mu sync.Mutex
		var records []record
		var aCalls atomic.Int32
		releaseAll := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			done <- e.RunObserved(context.Background(), Options{
				Platforms:   []string{"豆包"},
				Images:      []string{"1.png", "2.png", "3.png"},
				Concurrency: 2,
				SendWait:    1, // 启用共享出口错峰（2.5s）
			}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				mu.Lock()
				records = append(records, record{id: task.BrowserID, at: time.Since(base)})
				mu.Unlock()
				if task.BrowserID == a.ID && aCalls.Add(1) == 1 {
					// 甲第一单 100ms 结束，第二单应立刻复用甲（不自阻断 2.5s）。
					time.Sleep(100 * time.Millisecond)
					return nil
				}
				select {
				case <-releaseAll:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()

		// t=0: 甲接第 1 单；t=100ms: 甲完成第 1 单后应立刻接第 2 单；此时乙仍受同出口错峰阻挡。
		time.Sleep(200 * time.Millisecond)
		synctest.Wait()
		mu.Lock()
		early := append([]record(nil), records...)
		mu.Unlock()
		if len(early) != 2 || early[0].id != a.ID || early[1].id != a.ID || early[1].at >= time.Second {
			close(releaseAll)
			synctest.Wait()
			t.Fatalf("same account must not self-stagger while sibling waits: early=%+v (a=%d b=%d)", early, a.ID, b.ID)
		}

		// 再推进至超过 2.5s 错峰后，乙应接第 3 单。
		time.Sleep(2600 * time.Millisecond)
		synctest.Wait()
		mu.Lock()
		all := append([]record(nil), records...)
		mu.Unlock()
		close(releaseAll)
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if len(all) != 3 || all[2].id != b.ID || all[2].at < sameExitStagger {
			t.Fatalf("sibling account should stagger after %s: all=%+v (a=%d b=%d)", sameExitStagger, all, a.ID, b.ID)
		}
	})
}

func TestSharedExitDiversityPrefersIdleProxyOnEqualQuota(t *testing.T) {
	st := openRotateStore(t)
	a := addNamed(t, st, "豆包", "直连1")
	b := addNamed(t, st, "豆包", "直连2")
	c := addNamed(t, st, "豆包", "代理C")
	if err := st.SetBrowserProxy(c.ID, "http://10.0.0.3:8080"); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(st, nil, nil, "")
	now := time.Now()
	all := st.Browsers()
	rows := e.accountsFromRows(all, "豆包", now)
	exitKeys := buildPlatformExitKeys(all, rows, "豆包")

	// 额度相等且 a 正忙于直连出口时，优先选独立出口的 c 而不是同为直连的 b。
	busy := map[int64]bool{a.ID: true}
	busyExit := map[string]int{exitKeys[a.ID]: 1}
	id, ok := e.pickIdleAccountFromRows(rows, "豆包", 0, 0, busy, nil, exitKeys, nil, busyExit, now, nil, false)
	if !ok || id != c.ID {
		t.Fatalf("equal-quota pick = (%d, %v), want independent proxy account %d (b=%d)", id, ok, c.ID, b.ID)
	}

	// 若 b 今日已用次数更高且未受错峰阻挡，仍遵守集中耗尽高用量账号的既有策略。
	if err := st.RecordUse(b.ID, now, 5, 5, 0); err != nil {
		t.Fatal(err)
	}
	all = st.Browsers()
	rows = e.accountsFromRows(all, "豆包", now)
	id, ok = e.pickIdleAccountFromRows(rows, "豆包", 0, 0, busy, nil, exitKeys, nil, busyExit, now, nil, false)
	if !ok || id != b.ID {
		t.Fatalf("higher-quota pick = (%d, %v), want higher-used account %d", id, ok, b.ID)
	}
}

func TestLiveConcurrencyScaleUpAndDownDuringRun(t *testing.T) {
	st := openRotateStore(t)
	for i := range 4 {
		addNamed(t, st, "豆包", fmt.Sprintf("豆包%d", i+1))
	}
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		e.gap = func(time.Duration) time.Duration { return 0 }
		started := make(chan int, 8)
		releases := make([]chan struct{}, 6)
		for i := range releases {
			releases[i] = make(chan struct{})
		}
		done := make(chan error, 1)
		go func() {
			done <- e.RunObserved(context.Background(), Options{
				Platforms:   []string{"豆包"},
				Images:      []string{"1", "2", "3", "4", "5", "6"},
				Concurrency: 1,
			}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				started <- task.Index
				select {
				case <-releases[task.Index-1]:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
		}()

		synctest.Wait()
		if len(started) != 1 {
			t.Fatalf("initial Concurrency=1 should start 1 task, got %d", len(started))
		}
		first := <-started

		// 运行中实时扩容到 4：应立刻再派发 3 个任务并发执行。
		e.SetConcurrency("豆包", 4)
		synctest.Wait()
		if len(started) != 3 {
			t.Fatalf("live scale up to 4 should start 3 more tasks immediately, got %d", len(started))
		}
		batch := []int{first, <-started, <-started, <-started}

		// 运行中实时缩容回 1：前 3 个结束时（仍有 >=1 个在跑）不应派发新任务。
		e.SetConcurrency("豆包", 1)
		for _, idx := range batch[:3] {
			close(releases[idx-1])
		}
		synctest.Wait()
		if len(started) != 0 {
			t.Fatalf("live scale down to 1 must not dispatch while 1 task still active, got %d", len(started))
		}

		// 第 4 个结束后归零，应恰好只派发 1 个（第 5 单）。
		close(releases[batch[3]-1])
		synctest.Wait()
		if len(started) != 1 {
			t.Fatalf("after draining to 0 with live limit=1, want 1 task started, got %d", len(started))
		}
		fifth := <-started
		close(releases[fifth-1])
		synctest.Wait()
		if len(started) != 1 {
			t.Fatalf("after 5th completes with live limit=1, want 6th started, got %d", len(started))
		}
		sixth := <-started
		close(releases[sixth-1])
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestHumanCaptchaPauseReleasesConcurrencySlotWhenAutoEnabled(t *testing.T) {
	t.Run("auto concurrency lets third healthy account step in", func(t *testing.T) {
		saveCaptchaHooks(t)
		humanVerifyLimit = 2 * time.Minute
		var blocked atomic.Bool
		blocked.Store(true)
		captchaVisible = func(*cdp.Session) (bool, error) { return blocked.Load(), nil }

		st := openRotateStore(t)
		a := addNamed(t, st, "豆包", "甲")
		b := addNamed(t, st, "豆包", "乙")
		c := addNamed(t, st, "豆包", "丙")

		synctest.Test(t, func(t *testing.T) {
			e := NewEngine(st, nil, nil, "")
			e.gap = func(time.Duration) time.Duration { return 0 }
			started := make(chan int64, 6)
			enterHold := make(chan struct{})
			releaseB := make(chan struct{})
			cDone := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- e.RunObserved(context.Background(), Options{
					Platforms:   []string{"豆包"},
					Images:      []string{"1", "2", "3"},
					Concurrency: 2,
				}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
					started <- task.BrowserID
					switch task.BrowserID {
					case a.ID:
						<-enterHold
						return e.holdForHuman(ctx, nil, task)
					case b.ID:
						<-releaseB
						return nil
					case c.ID:
						close(cDone)
						return nil
					}
					return nil
				})
			}()

			synctest.Wait()
			if len(started) != 2 {
				t.Fatalf("want 2 initial tasks (a and b), got %d", len(started))
			}
			firstTwo := []int64{<-started, <-started}
			if !slices.Contains(firstTwo, a.ID) || !slices.Contains(firstTwo, b.ID) {
				t.Fatalf("unexpected initial accounts: %v", firstTwo)
			}

			// 甲进入人工验证码等待后应释放并发计数位，丙立刻补位跑完第 3 单。
			close(enterHold)
			synctest.Wait()
			select {
			case got := <-started:
				if got != c.ID {
					t.Fatalf("third task ran on %d, want %d", got, c.ID)
				}
			default:
				t.Fatal("account c did not step in while account a awaited human captcha")
			}
			<-cDone

			// 放行验证与乙，全部收尾成功。
			blocked.Store(false)
			time.Sleep(500 * time.Millisecond)
			close(releaseB)
			synctest.Wait()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	})

	t.Run("serial concurrency=1 stays strictly serial during captcha wait", func(t *testing.T) {
		saveCaptchaHooks(t)
		humanVerifyLimit = 2 * time.Minute
		var blocked atomic.Bool
		blocked.Store(true)
		captchaVisible = func(*cdp.Session) (bool, error) { return blocked.Load(), nil }

		st := openRotateStore(t)
		a := addNamed(t, st, "豆包", "甲")
		_ = addNamed(t, st, "豆包", "乙")

		synctest.Test(t, func(t *testing.T) {
			e := NewEngine(st, nil, nil, "")
			e.gap = func(time.Duration) time.Duration { return 0 }
			started := make(chan int64, 4)
			done := make(chan error, 1)
			go func() {
				done <- e.RunObserved(context.Background(), Options{
					Platforms:   []string{"豆包"},
					Images:      []string{"1", "2"},
					Concurrency: 1,
				}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
					started <- task.BrowserID
					if task.Index == 1 && task.BrowserID == a.ID {
						return e.holdForHuman(ctx, nil, task)
					}
					return nil
				})
			}()

			synctest.Wait()
			if len(started) != 1 {
				t.Fatalf("Concurrency=1 must not launch second task during captcha wait, started=%d", len(started))
			}
			<-started

			blocked.Store(false)
			time.Sleep(500 * time.Millisecond)
			synctest.Wait()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if len(started) != 1 {
				t.Fatalf("second task should run after first finishes, remaining started=%d", len(started))
			}
		})
	})
}

func TestConcurrencyOnePlatformCapMatchesLimit(t *testing.T) {
	st := openRotateStore(t)
	for i := range 3 {
		addNamed(t, st, "豆包", fmt.Sprintf("豆包%d", i+1))
	}
	now := time.Now()
	e := NewEngine(st, nil, nil, "")

	e.opts.Concurrency = 1
	if got := e.platformCap("豆包", now); got != 1 {
		t.Fatalf("platformCap with Concurrency=1 = %d, want 1", got)
	}
	e.opts.Concurrency = 0
	if got := e.platformCap("豆包", now); got != PlatformInFlight {
		t.Fatalf("platformCap with Concurrency=0 = %d, want %d", got, PlatformInFlight)
	}
	e.SetConcurrency("豆包", 5)
	if got := e.platformCap("豆包", now); got != 3 {
		t.Fatalf("platformCap with live=5 and 3 accounts = %d, want 3", got)
	}
	e.SetConcurrency("豆包", 1)
	if got := e.platformCap("豆包", now); got != 1 {
		t.Fatalf("platformCap with live=1 = %d, want 1", got)
	}
}

func TestSubmittedTaskNeverRotatesWhenAccountCoolsOrCaps(t *testing.T) {
	st := openRotateStore(t)
	a := addNamed(t, st, "豆包", "甲")
	_ = addNamed(t, st, "豆包", "乙")
	now := time.Now()
	e := NewEngine(st, nil, nil, "")
	e.opts.DailyCap = 5

	// 场景 1：原账号达今日上限，已发送任务仍留在原账号取回结果，绝不改派给乙。
	if err := st.RecordUse(a.ID, now, 5, 5, 0); err != nil {
		t.Fatal(err)
	}
	task := Task{Index: 1, Platform: "豆包", BrowserID: a.ID, submitted: true}
	wait, dead := e.prepareAccountRows(e.accountsFor("豆包"), &task, now)
	if dead || !wait.IsZero() || task.BrowserID != a.ID {
		t.Fatalf("capped submitted task = (browser=%d wait=%v dead=%v), want browser=%d ready", task.BrowserID, wait, dead, a.ID)
	}

	// 场景 2：原账号冷却中，等待冷却结束而非改派给乙。
	coolUntil := now.Add(30 * time.Second).Truncate(time.Second)
	st.UpdateBrowserHealth(a.ID, store.HealthCooling, coolUntil.Unix(), "冷却")
	task = Task{Index: 2, Platform: "豆包", BrowserID: a.ID, submitted: true}
	wait, dead = e.prepareAccountRows(e.accountsFor("豆包"), &task, now)
	if dead || !wait.Equal(coolUntil) || task.BrowserID != a.ID {
		t.Fatalf("cooling submitted task = (browser=%d wait=%v dead=%v), want browser=%d wait=%v", task.BrowserID, wait, dead, a.ID, coolUntil)
	}

	// 场景 3：原账号被禁用，等待恢复而非改派给乙。
	st.UpdateBrowserHealth(a.ID, store.HealthReady, 0, "")
	if err := st.SetBrowserDisabled(a.ID, true); err != nil {
		t.Fatal(err)
	}
	task = Task{Index: 3, Platform: "豆包", BrowserID: a.ID, submitted: true}
	wait, dead = e.prepareAccountRows(e.accountsFor("豆包"), &task, now)
	if dead || wait.IsZero() || task.BrowserID != a.ID {
		t.Fatalf("disabled submitted task = (browser=%d wait=%v dead=%v), want browser=%d wait>0", task.BrowserID, wait, dead, a.ID)
	}
}

func TestFailureRotationReleasesSlotImmediatelyWithNonBlockingBackoff(t *testing.T) {
	st := openRotateStore(t)
	a := addNamed(t, st, "豆包", "甲")
	b := addNamed(t, st, "豆包", "乙")
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		e.gap = func(time.Duration) time.Duration { return 0 }
		base := time.Now()
		releaseB := make(chan struct{})
		task3At := make(chan time.Duration, 1)
		task1RetryAt := make(chan time.Duration, 1)
		done := make(chan error, 1)
		go func() {
			done <- e.RunObserved(context.Background(), Options{
				Platforms:   []string{"豆包"},
				Images:      []string{"1", "2", "3"},
				Concurrency: 2,
				RetryCount:  1,
			}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				switch task.Index {
				case 1:
					if task.Attempts == 1 {
						return errors.New("临时出错")
					}
					task1RetryAt <- time.Since(base)
					return nil
				case 2:
					<-releaseB
					return nil
				case 3:
					task3At <- time.Since(base)
					return nil
				}
				return nil
			})
		}()

		synctest.Wait()
		// 第 1 单失败换号后应立刻释放甲和并发名额，第 3 单必须在 t=0（而非等 3s）就开始运行。
		select {
		case at := <-task3At:
			if at >= time.Second {
				close(releaseB)
				synctest.Wait()
				t.Fatalf("task 3 waited %s for task 1 rotation backoff, want immediate dispatch", at)
			}
		default:
			close(releaseB)
			synctest.Wait()
			t.Fatalf("task 3 did not dispatch immediately after task 1 failure-rotation (a=%d b=%d)", a.ID, b.ID)
		}

		// 放行乙，验证第 1 单遵守 3s 非阻塞退避后再重试并成功。
		close(releaseB)
		time.Sleep(2500 * time.Millisecond)
		synctest.Wait()
		select {
		case at := <-task1RetryAt:
			t.Fatalf("task 1 retried too early at %s, want >= 3s", at)
		default:
		}
		time.Sleep(1 * time.Second)
		synctest.Wait()
		select {
		case at := <-task1RetryAt:
			if at < 3*time.Second {
				t.Fatalf("task 1 retry elapsed = %s, want >= 3s", at)
			}
		default:
			t.Fatal("task 1 did not retry after 3s non-blocking backoff")
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestBudgetContextEventDrivenAndIsolatesPriorTaskPause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.beginLanes()
		defer e.finishLanes()

		// 任务 1 在账号 7 上暂停 2 分钟后恢复。
		e.pauseBudget(7)
		time.Sleep(2 * time.Minute)
		e.resumeBudget(7)

		// 任务 2 在同一账号上拿到 10s 预算：不得继承任务 1 遗留的 2 分钟暂停时长。
		ctx2, cancel2 := e.budgetContext(context.Background(), 7, 10*time.Second)
		defer cancel2()
		time.Sleep(9 * time.Second)
		synctest.Wait()
		if ctx2.Err() != nil {
			t.Fatalf("task 2 expired early at 9s: %v", ctx2.Err())
		}
		time.Sleep(1 * time.Second)
		synctest.Wait()
		if !errors.Is(context.Cause(ctx2), context.DeadlineExceeded) {
			t.Fatalf("task 2 leaked prior pause budget: err=%v cause=%v", ctx2.Err(), context.Cause(ctx2))
		}

		// 任务 3 在账号 8 上运行 4s 后暂停 30s，再恢复后应补足剩余 6s 才到期。
		ctx3, cancel3 := e.budgetContext(context.Background(), 8, 10*time.Second)
		defer cancel3()
		time.Sleep(4 * time.Second)
		e.pauseBudget(8)
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if ctx3.Err() != nil {
			t.Fatalf("task 3 expired while paused: %v", ctx3.Err())
		}
		e.resumeBudget(8)
		time.Sleep(5 * time.Second)
		synctest.Wait()
		if ctx3.Err() != nil {
			t.Fatalf("task 3 expired at 9s active time: %v", ctx3.Err())
		}
		time.Sleep(1 * time.Second)
		synctest.Wait()
		if !errors.Is(context.Cause(ctx3), context.DeadlineExceeded) {
			t.Fatalf("task 3 did not expire at 10s active time: err=%v cause=%v", ctx3.Err(), context.Cause(ctx3))
		}
	})
}

func TestLazySourceHashAndBatchNoteUse(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "src.bin")
	payload := []byte("tulai-lazy-streaming-hash-payload")
	if err := os.WriteFile(imgPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	want := images.SHA256(payload)

	st := openRotateStore(t)
	a := addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, dir)

	got := e.sourceHash(imgPath)
	if got != want {
		t.Fatalf("sourceHash = %q, want %q", got, want)
	}
	// 删除原文件后仍应命中惰性缓存。
	if err := os.Remove(imgPath); err != nil {
		t.Fatal(err)
	}
	if cached := e.sourceHash(imgPath); cached != want {
		t.Fatalf("cached sourceHash = %q, want %q", cached, want)
	}
	// 测试显式注入的 e.srcHashes 优先于缓存。
	e.srcHashes = map[string]string{imgPath: "injected-hash"}
	if injected := e.sourceHash(imgPath); injected != "injected-hash" {
		t.Fatalf("injected sourceHash = %q, want injected-hash", injected)
	}

	// 验证批量洗图 noteUseMembers 汇总统计精准写入 SQLite。
	members := make([]Task, 10)
	for i := range members {
		status := StatusSuccess
		if i >= 8 {
			status = StatusFailed
		}
		members[i] = Task{
			Index:     i + 1,
			Platform:  "豆包",
			BrowserID: a.ID,
			slot:      a.ID,
			ranOn:     a.ID,
			Attempts:  1,
			Status:    status,
		}
	}
	e.noteUseMembers(members, 0)
	row, ok := e.browserByID(a.ID)
	if !ok {
		t.Fatal("account missing after noteUseMembers")
	}
	now := time.Now()
	okToday, failToday := row.ResultsToday(now)
	if row.UsedToday(now) != 10 || okToday != 8 || failToday != 2 {
		t.Fatalf("aggregated daily usage = (used=%d ok=%d fail=%d), want (10, 8, 2)",
			row.UsedToday(now), okToday, failToday)
	}
}

func TestLargeQueueDispatchPerformanceAndEventDrivenManualWake(t *testing.T) {
	st := openRotateStore(t)
	a := addNamed(t, st, "豆包", "甲")
	_ = addNamed(t, st, "豆包", "乙")

	// 事件驱动 manualWake：解除人工接管后应立即唤醒 enterAccount，不等待 100ms 轮询。
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		e.SetAccountManual(a.ID, true)
		entered := make(chan bool, 1)
		go func() {
			entered <- e.enterAccount(context.Background(), a.ID)
		}()
		synctest.Wait()
		select {
		case <-entered:
			t.Fatal("enterAccount should block while account is in manual takeover")
		default:
		}
		base := time.Now()
		e.SetAccountManual(a.ID, false)
		synctest.Wait()
		select {
		case ok := <-entered:
			if !ok || time.Since(base) != 0 {
				t.Fatalf("manualWake enterAccount = (ok=%v elapsed=%s), want (true, 0s)", ok, time.Since(base))
			}
		default:
			t.Fatal("enterAccount did not wake immediately on SetAccountManual(false)")
		}
		e.setActive(a.ID, false)
	})

	// 500 任务大队列调度性能回归：单轮快照 + O(1) QueueView + freshBlocked 剪枝。
	const total = 500
	imgs := make([]string, total)
	for i := range imgs {
		imgs[i] = fmt.Sprintf("img-%03d.png", i+1)
	}
	e := NewEngine(st, nil, nil, "")
	e.gap = func(time.Duration) time.Duration { return 0 }
	var completed atomic.Int32
	start := time.Now()
	err := e.RunObserved(context.Background(), Options{
		Platforms:   []string{"豆包"},
		Images:      imgs,
		Concurrency: 2,
		DailyCap:    0,
	}, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
		completed.Add(1)
		return nil
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Load() != total {
		t.Fatalf("completed = %d, want %d", completed.Load(), total)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("500-task dispatch took %s, expected fast completion", elapsed)
	}
}
