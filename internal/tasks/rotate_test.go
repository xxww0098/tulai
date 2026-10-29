package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestQuotaSwitchesToOtherAccount(t *testing.T) {
	st := openRotateStore(t)
	first, second := addNamed(t, st, "豆包", "甲"), addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	var used []int64
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		used = append(used, task.BrowserID)
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
	if !errors.Is(err, captcha.ErrVerificationRequired) || calls != 1 || queue[0].Status != StatusNeedsUser || queue[1].Status != StatusPending {
		t.Fatalf("single-account captcha should stop the batch: calls=%d err=%v queue=%+v", calls, err, queue)
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

func TestUncertainSendDoesNotRotate(t *testing.T) {
	st := openRotateStore(t)
	addNamed(t, st, "豆包", "甲")
	addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	calls := 0
	err := e.run(context.Background(), Options{
		Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"}, Concurrency: 1, RetryCount: 3,
	}, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
		calls++
		return strategy.ErrSubmissionUncertain
	})
	queue := e.View().Snapshot()
	failed := 0
	for _, task := range queue {
		if task.Status == StatusSuccess {
			t.Fatalf("uncertain send should stop the batch: %+v", queue)
		}
		if task.Status == StatusFailed && strings.Contains(task.Error, "未确认") {
			failed++
		}
	}
	if err != nil || calls < 1 || calls > 2 || failed != 1 {
		t.Fatalf("uncertain send should still stop the batch: calls=%d failed=%d err=%v queue=%+v", calls, failed, err, queue)
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
