package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/strategy"
)

func saveCaptchaHooks(t *testing.T) {
	t.Helper()
	visible, manual, alert, limit := captchaVisible, captchaManual, captchaAlert, humanVerifyLimit
	t.Cleanup(func() {
		captchaVisible, captchaManual, captchaAlert, humanVerifyLimit = visible, manual, alert, limit
	})
	captchaManual = func(*cdp.Session) {}
	captchaAlert = func(string, string) {}
}

func TestHumanWaitPausesImageBudgetAndContinues(t *testing.T) {
	saveCaptchaHooks(t)
	var start time.Time
	captchaVisible = func(*cdp.Session) (bool, error) {
		if start.IsZero() {
			return true, nil
		}
		return time.Since(start) < 1500*time.Millisecond, nil
	}
	alerts := 0
	captchaAlert = func(title, body string) {
		alerts++
		if title != "需要人工验证" || !strings.Contains(body, "千问2") {
			t.Errorf("alert %q %q", title, body)
		}
	}
	strat := &flowStrategy{wait: func(n int) ([]strategy.Result, error) {
		if n == 1 {
			return nil, captcha.ErrVisible
		}
		return nil, errors.New("stop")
	}}
	st := openRotateStore(t)
	row := addNamed(t, st, "千问", "千问2")
	e := NewEngine(st, nil, nil, "")
	e.opts.WaitResultSeconds = 1
	task := &Task{Index: 3, Platform: "千问", BrowserID: row.ID, Status: StatusRunning, submitted: true}
	start = time.Now()
	err := e.waitAndSave(context.Background(), task, strat, nil)
	if err == nil || err.Error() != "stop" {
		t.Fatalf("验证通过后应继续检测，得到 %v（用时 %s）", err, time.Since(start))
	}
	if time.Since(start) < time.Second {
		t.Fatal("测试没有真正停住等人")
	}
	if strat.waits != 2 || strat.sends != 0 || alerts != 1 || !task.submitted {
		t.Fatalf("waits=%d sends=%d alert=%d submitted=%v", strat.waits, strat.sends, alerts, task.submitted)
	}
}

func TestCaptchaBeforeSendResendsAfterVerify(t *testing.T) {
	saveCaptchaHooks(t)
	step, opened := 0, false
	captchaVisible = func(*cdp.Session) (bool, error) {
		return step == 1 && !opened, nil
	}
	captchaAlert = func(string, string) { opened = true }
	strat := &flowStrategy{}
	strat.send = func() error {
		step++
		if step == 1 {
			return errors.New("slider")
		}
		return nil
	}
	e := &Engine{}
	task := &Task{Index: 1, Platform: "豆包", Prompt: "一只猫", Status: StatusRunning}
	if err := e.submitTask(context.Background(), task, strat, nil); err != nil || !task.submitted || strat.sends != 2 {
		t.Fatalf("没发出去的提示词要在验证通过后重发: err=%v submitted=%v sends=%d", err, task.submitted, strat.sends)
	}
}

func TestHumanWaitSurvivesImageDeadlineAndKeepsSubmittedTask(t *testing.T) {
	saveCaptchaHooks(t)
	humanVerifyLimit = 80 * time.Millisecond
	captchaVisible = func(*cdp.Session) (bool, error) { return true, nil }
	alerts := 0
	e := NewEngine(nil, nil, nil, "")
	e.view = NewQueueView([]Task{{Index: 2, Platform: "豆包", BrowserID: 7, Status: StatusRunning}})
	var during Status
	captchaAlert = func(string, string) {
		alerts++
		if !e.AccountAwaitingUser(7) {
			t.Error("verification must allow manual input while the task is waiting")
		}
		if e.View() != nil && len(e.View().Snapshot()) > 0 {
			during = e.View().Snapshot()[0].Status
		}
	}
	logFrom := len(logging.Log.Entries())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := e.holdForHuman(ctx, nil, &Task{Index: 2, Platform: "豆包", BrowserID: 7, Status: StatusRunning})
	if !errors.Is(err, errHumanGaveUp) {
		t.Fatalf("err=%v", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatalf("出图超时把人工等待掐断了: %s", time.Since(start))
	}
	if alerts != 1 || during != StatusAwaitUser || e.AccountAwaitingUser(7) {
		t.Fatalf("alert=%d during=%s still awaiting=%v", alerts, during, e.AccountAwaitingUser(7))
	}
	notes := 0
	for _, entry := range logging.Log.Entries()[logFrom:] {
		if strings.Contains(entry.Text, "需要人工验证，请打开账号的查看浏览器并接管操作") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("验证码日志应只在进入等待时记一条，实际 %d", notes)
	}

	st := openRotateStore(t)
	first := addNamed(t, st, "千问", "千问1")
	second := addNamed(t, st, "千问", "千问2")
	eng := NewEngine(st, nil, nil, "")
	var used []int64
	runErr := eng.run(context.Background(), Options{
		Platforms: []string{"千问"}, Images: []string{"a.png"}, Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		used = append(used, task.BrowserID)
		if task.BrowserID == first.ID {
			task.submitted = true
			return errHumanGaveUp
		}
		return nil
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	queue := eng.View().Snapshot()
	if len(queue) != 1 || queue[0].Status != StatusNeedsUser || !queue[0].submitted {
		t.Fatalf("已提交任务应保留原提交并等待验证: %+v used=%v", queue, used)
	}
	if len(used) != 1 || used[0] != first.ID {
		t.Fatalf("used=%v first=%d second=%d", used, first.ID, second.ID)
	}
	health := browserByID(t, st, first.ID)
	if health.Usable(time.Now()) || !strings.Contains(health.LastError, "需要人工验证") {
		t.Fatalf("超时账号应标成需验证: %+v", health)
	}
	if !browserByID(t, st, second.ID).Usable(time.Now()) {
		t.Fatal("另一个账号应还能用")
	}
}

func TestCaptchaPausesOuterBudget(t *testing.T) {
	saveCaptchaHooks(t)
	calls := 0
	captchaVisible = func(*cdp.Session) (bool, error) {
		calls++
		return calls < 3, nil
	}
	e := NewEngine(nil, nil, nil, "")
	ctx, cancel := e.budgetContext(context.Background(), 7, 50*time.Millisecond)
	defer cancel()
	err := e.holdForHuman(ctx, nil, &Task{Index: 1, Platform: "千问", BrowserID: 7, Status: StatusRunning})
	if err != nil {
		t.Fatal(err)
	}
	if calls < 3 {
		t.Fatalf("没有等到验证结束就返回了: calls=%d", calls)
	}
	if ctx.Err() != nil {
		t.Fatalf("验证期间外层时限到了: %v", ctx.Err())
	}
	deadline := time.Now().Add(800 * time.Millisecond)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
	}
	if !errors.Is(context.Cause(ctx), context.DeadlineExceeded) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("放开之后时限应继续走: err=%v cause=%v", ctx.Err(), context.Cause(ctx))
	}
}

func TestHumanLimitKeepsNeedsUserAfterGenerationDeadline(t *testing.T) {
	saveCaptchaHooks(t)
	humanVerifyLimit = 80 * time.Millisecond
	captchaVisible = func(*cdp.Session) (bool, error) { return true, nil }
	e := NewEngine(nil, nil, nil, "")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	got := e.execute(ctx, Task{Index: 1, Platform: "豆包", BrowserID: 7, submitted: true}, func(ctx context.Context, task *Task) error {
		return e.holdForHuman(ctx, nil, task)
	})
	if got.Status != StatusNeedsUser || !got.submitted || got.Error != errHumanGaveUp.Error() {
		t.Fatalf("human validation was classified as generation timeout: %+v", got)
	}
}

func TestCaptchaWaitsForManualTakeoverToBeReleased(t *testing.T) {
	saveCaptchaHooks(t)
	humanVerifyLimit = 3 * time.Second
	calls := 0
	captchaVisible = func(*cdp.Session) (bool, error) {
		calls++
		return calls == 1, nil
	}
	e := NewEngine(nil, nil, nil, "")
	entered := make(chan struct{})
	captchaAlert = func(string, string) {
		e.SetAccountManual(7, true)
		close(entered)
	}
	done := make(chan error, 1)
	go func() {
		done <- e.holdForHuman(context.Background(), nil, &Task{Index: 1, Platform: "豆包", BrowserID: 7})
	}()
	<-entered
	if !e.AccountAwaitingUser(7) {
		t.Fatal("account must allow manual input during verification")
	}
	select {
	case err := <-done:
		t.Fatalf("task resumed while manual takeover was active: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	e.SetAccountManual(7, false)
	select {
	case err := <-done:
		if err != nil || e.AccountAwaitingUser(7) {
			t.Fatalf("task did not resume after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task did not resume after release")
	}
}

func TestHumanWaitStopsWhenUserStops(t *testing.T) {
	saveCaptchaHooks(t)
	captchaVisible = func(*cdp.Session) (bool, error) { return true, nil }
	alerts := 0
	captchaAlert = func(string, string) { alerts++ }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (&Engine{}).holdForHuman(ctx, nil, &Task{Index: 1, Platform: "豆包"})
	if !errors.Is(err, context.Canceled) || alerts != 0 {
		t.Fatalf("err=%v alerts=%d", err, alerts)
	}
}

type flowStrategy struct {
	strategy.Strategy
	sends int
	waits int
	send  func() error
	wait  func(int) ([]strategy.Result, error)
}

func (s *flowStrategy) Platform() string                         { return "千问" }
func (s *flowStrategy) HomeURL() string                          { return "https://www.qianwen.com/" }
func (s *flowStrategy) CleanComposer(*cdp.Session) error         { return nil }
func (s *flowStrategy) UploadFiles(*cdp.Session, []string) error { return nil }
func (s *flowStrategy) MarkTask(*cdp.Session) error              { return nil }
func (s *flowStrategy) SendPrompt(*cdp.Session, string) error {
	s.sends++
	if s.send != nil {
		return s.send()
	}
	return nil
}
func (s *flowStrategy) WaitForResult(*cdp.Session, time.Duration) ([]strategy.Result, error) {
	s.waits++
	if s.wait != nil {
		return s.wait(s.waits)
	}
	return nil, errors.New("stop")
}
func (s *flowStrategy) FetchResultBytes(*cdp.Session, string) ([]byte, error) {
	return nil, errors.New("missing")
}
func (s *flowStrategy) PageState(*cdp.Session) map[string]any { return map[string]any{} }

func TestTaskBudgetIncludesOneHumanWait(t *testing.T) {
	e := &Engine{}
	e.opts.WaitResultSeconds = 30
	if e.taskBudget() < 30*time.Second+5*time.Minute {
		t.Fatalf("budget = %s", e.taskBudget())
	}
}
