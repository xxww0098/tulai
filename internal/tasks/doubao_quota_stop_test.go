package tasks

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestDoubaoQuotaStopsRemainingWashAfterActiveBatches(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	first := addNamed(t, st, "豆包", "豆包1")
	second := addNamed(t, st, "豆包", "豆包2")
	st.ConfirmLogin(first.ID)
	st.ConfirmLogin(second.ID)
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()
	e.gap = func(time.Duration) time.Duration { return 0 }

	var sources []string
	for i := 1; i <= 25; i++ {
		sources = append(sources, fmt.Sprintf("%02d.png", i))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan int64, 2)
	firstFailed := make(chan struct{})
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	var firstOnce, secondOnce, failedOnce sync.Once
	releaseFirst := func() { firstOnce.Do(func() { close(firstRelease) }) }
	releaseSecond := func() { secondOnce.Do(func() { close(secondRelease) }) }
	finished := make(chan error, 1)
	runDone := make(chan struct{})
	defer func() {
		cancel()
		releaseFirst()
		releaseSecond()
		<-runDone
	}()
	var progress Progress
	go func() {
		defer close(runDone)
		finished <- e.RunObserved(ctx, Options{
			Platforms: []string{"豆包"}, Prompts: []string{"洗图"}, Images: sources,
			Wash: true, WaitResult: true, Concurrency: 2, DailyCap: 100,
		}, func(task Task) {
			if task.BrowserID == first.ID && task.Status == StatusFailed {
				failedOnce.Do(func() { close(firstFailed) })
			}
		}, func(p Progress) { progress = p }, func(ctx context.Context, task *Task) error {
			started <- task.BrowserID
			release := secondRelease
			if task.BrowserID == first.ID {
				release = firstRelease
				// Even a failed batch can contain a result already saved for one source.
				if len(task.batch) > 0 {
					task.batch[0].Results = []string{"saved.png"}
				}
			}
			select {
			case <-release:
				task.submitted = true
				return strategy.ErrAccountQuota
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("两个账号没有同时开始洗图")
		}
	}
	releaseFirst()
	select {
	case <-firstFailed:
	case <-time.After(5 * time.Second):
		t.Fatal("第一个账号的额度错误未被记录")
	}
	select {
	case err := <-finished:
		t.Fatalf("另一个批次还在执行时任务提前结束: %v", err)
	default:
	}
	releaseSecond()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("所有账号额度用完后，剩余任务没有自动停止")
	}
	var success, failed, stopped int
	for _, task := range e.View().Snapshot() {
		switch task.Status {
		case StatusSuccess:
			success++
			if len(task.Results) != 1 || task.Results[0] != "saved.png" {
				t.Fatalf("已保存结果丢失: %+v", task)
			}
		case StatusFailed:
			failed++
		case StatusSkipped:
			stopped++
			if !task.Undispatched || task.Worked() || !strings.Contains(task.Error, "额度已用完") {
				t.Fatalf("未派发的任务不应算失败或已处理: %+v", task)
			}
		default:
			t.Fatalf("任务结束后仍在排队或运行: %+v", task)
		}
	}
	if success != 1 || failed != 19 || stopped != 5 || progress.Done != 25 || progress.Total != 25 {
		t.Fatalf("结果统计不正确: success=%d failed=%d stopped=%d progress=%+v", success, failed, stopped, progress)
	}
	if retry := RebuildRetry(e.View().Snapshot()); len(retry) != 24 {
		t.Fatalf("未完成的源图必须能继续处理: retry=%d", len(retry))
	}
}

func TestDoubaoQuotaStopRequiresEveryAccountToBeFull(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	first := addNamed(t, st, "豆包", "豆包1")
	second := addNamed(t, st, "豆包", "豆包2")
	e := NewEngine(st, nil, nil, "")
	e.opts.DailyCaps = map[string]int{"豆包": 0}
	now := time.Now()
	st.UpdateBrowserHealth(first.ID, store.HealthExhausted, now.Add(time.Hour).Unix(), "账号额度已用尽")
	if e.allDoubaoAccountsFull(now) {
		t.Fatal("还有可用账号时不应停止")
	}
	st.UpdateBrowserHealth(second.ID, store.HealthCooling, now.Add(time.Minute).Unix(), "请求过于频繁")
	if e.allDoubaoAccountsFull(now) {
		t.Fatal("另一个账号只是暂时冷却，不应停止")
	}
	st.UpdateBrowserHealth(second.ID, store.HealthExhausted, 0, "未登录")
	if e.allDoubaoAccountsFull(now) {
		t.Fatal("另一个账号等待登录，不应视为额度用尽")
	}
	st.UpdateBrowserHealth(second.ID, store.HealthExhausted, now.Add(time.Hour).Unix(), "账号额度已用尽")
	if !e.allDoubaoAccountsFull(now) {
		t.Fatal("两个账号的额度都用完时应停止")
	}
	if err := st.SetBrowserDisabled(second.ID, true); err != nil {
		t.Fatal(err)
	}
	if e.allDoubaoAccountsFull(now) {
		t.Fatal("另一个账号被禁用时应保留等待恢复的机会")
	}
	if err := st.SetBrowserDisabled(second.ID, false); err != nil {
		t.Fatal(err)
	}
	e.SetAccountManual(second.ID, true)
	if e.allDoubaoAccountsFull(now) {
		t.Fatal("人工接管中的账号不应触发自动停止")
	}
	e.SetAccountManual(second.ID, false)
	st.UpdateBrowserHealth(second.ID, store.HealthExhausted, now.Add(-time.Minute).Unix(), "账号额度已用尽")
	if e.allDoubaoAccountsFull(now) {
		t.Fatal("额度已恢复的账号不应被视为用完")
	}
	if err := st.RecordUse(second.ID, now, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	e.opts.DailyCaps["豆包"] = 1
	if !e.allDoubaoAccountsFull(now) {
		t.Fatal("平台额度用完和本地日上限都应计入")
	}
}

func TestDoubaoQuotaStopDoesNotCancelOtherPlatform(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	addNamed(t, st, "豆包", "豆包1")
	addNamed(t, st, "千问", "千问1")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()
	e.gap = func(time.Duration) time.Duration { return 0 }
	ctx, cancel := context.WithCancel(context.Background())
	qianwenRelease := make(chan struct{})
	qianwenStarted := make(chan struct{})
	doubaoStopped := make(chan struct{})
	finished := make(chan error, 1)
	runDone := make(chan struct{})
	var releaseOnce, startedOnce, stoppedOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(qianwenRelease) }) }
	defer func() {
		cancel()
		release()
		<-runDone
	}()
	go func() {
		defer close(runDone)
		finished <- e.RunObserved(ctx, Options{Platforms: []string{"豆包", "千问"}, Prompts: []string{"一", "二", "三"}, Concurrency: 2},
			func(task Task) {
				if task.Platform == "豆包" && task.Status == StatusSkipped {
					stoppedOnce.Do(func() { close(doubaoStopped) })
				}
			}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				if task.Platform == "豆包" {
					task.submitted = true
					return strategy.ErrAccountQuota
				}
				startedOnce.Do(func() { close(qianwenStarted) })
				select {
				case <-qianwenRelease:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
	}()
	for _, signal := range []<-chan struct{}{qianwenStarted, doubaoStopped} {
		select {
		case <-signal:
		case <-time.After(5 * time.Second):
			t.Fatal("豆包队列没有停止，或千问任务没有开始")
		}
	}
	select {
	case err := <-finished:
		t.Fatalf("豆包停止时不应取消仍在运行的千问任务: %v", err)
	default:
	}
	release()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("千问的剩余任务没有完成")
	}
	var failed, stopped, succeeded int
	for _, task := range e.View().Snapshot() {
		switch {
		case task.Platform == "豆包" && task.Status == StatusFailed:
			failed++
		case task.Platform == "豆包" && task.Status == StatusSkipped && task.Undispatched:
			stopped++
		case task.Platform == "千问" && task.Status == StatusSuccess:
			succeeded++
		default:
			t.Fatalf("其他平台被中断或任务状态错误: %+v", task)
		}
	}
	if failed != 1 || stopped != 2 || succeeded != 3 {
		t.Fatalf("豆包失败=%d、未完成=%d，千问成功=%d", failed, stopped, succeeded)
	}
}
