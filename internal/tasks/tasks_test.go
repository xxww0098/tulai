package tasks

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/captcha"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestBuildQueueSequentialCycle(t *testing.T) {
	opts := Options{
		Platforms: []string{"豆包", "千问"},
		Prompts:   []string{"画一只猫", "画一条鱼", "画一朵云"},
		Images:    []string{"a.png", "b.png", "c.png", "d.png"},
	}
	browsers := map[string][]int64{"豆包": {1, 2}, "千问": {3}}
	q := BuildQueue(opts, browsers)
	if len(q) != 8 { // 4 图 × 2 平台
		t.Fatalf("任务数: %d", len(q))
	}
	// 顺序循环：第 n 个任务的提示词 = prompts[(n-1)%3]
	wantPrompts := []string{"画一只猫", "画一条鱼", "画一朵云", "画一只猫", "画一条鱼", "画一朵云", "画一只猫", "画一条鱼"}
	for i, task := range q {
		if task.Prompt != wantPrompts[i] {
			t.Errorf("#%d 提示词 = %q, want %q", task.Index, task.Prompt, wantPrompts[i])
		}
		if task.Status != StatusPending {
			t.Errorf("#%d 状态 = %s", task.Index, task.Status)
		}
	}
	// 浏览器轮转：豆包任务依次用 1,2,1；千问恒 3
	if q[0].BrowserID != 1 || q[2].BrowserID != 2 || q[4].BrowserID != 1 {
		t.Errorf("豆包浏览器轮转: %d %d %d", q[0].BrowserID, q[2].BrowserID, q[4].BrowserID)
	}
	for _, idx := range []int{1, 3, 5} {
		if q[idx].BrowserID != 3 {
			t.Errorf("千问浏览器: #%d = %d", q[idx].Index, q[idx].BrowserID)
		}
	}
}

func TestBuildQueueSuffix(t *testing.T) {
	opts := Options{
		Platforms:    []string{"豆包"},
		Prompts:      []string{"画一只猫"},
		PromptSuffix: "高清 4K",
		UseSuffix:    true,
		Images:       []string{"a.png"},
	}
	q := BuildQueue(opts, map[string][]int64{"豆包": {1}})
	if len(q) != 1 || !strings.Contains(q[0].Prompt, "画一只猫") || !strings.Contains(q[0].Prompt, "高清 4K") {
		t.Fatalf("后缀拼接: %q", q[0].Prompt)
	}
}

func TestBuildQueueTextOnly(t *testing.T) {
	opts := Options{
		Platforms:    []string{"豆包", "千问"},
		Prompts:      []string{"画一只猫", "  ", "画一条鱼"},
		PromptSuffix: "高清",
		UseSuffix:    true,
		Images:       nil,
	}
	q := BuildQueue(opts, map[string][]int64{"豆包": {1, 2}, "千问": {3}})
	if len(q) != 4 {
		t.Fatalf("文生图任务数: %d", len(q))
	}
	want := []string{"画一只猫", "画一只猫", "画一条鱼", "画一条鱼"}
	for i, task := range q {
		if task.Prompt != want[i]+"\n高清" {
			t.Fatalf("#%d prompt = %q", task.Index, task.Prompt)
		}
		if len(task.Images) != 0 {
			t.Fatalf("#%d unexpectedly uploads %v", task.Index, task.Images)
		}
	}
	if q[0].Platform != "豆包" || q[1].Platform != "千问" || q[0].BrowserID != 1 || q[2].BrowserID != 2 || q[1].BrowserID != 3 {
		t.Fatalf("platform rotation: %+v", q)
	}
}

func TestBuildQueueStartBrowserIndex(t *testing.T) {
	for _, tc := range []struct {
		start int
		want  []int64
	}{
		{-1, []int64{1, 4, 2, 4, 3, 4}},
		{0, []int64{1, 4, 2, 4, 3, 4}},
		{1, []int64{1, 4, 2, 4, 3, 4}},
		{2, []int64{2, 4, 3, 4, 1, 4}},
		{3, []int64{3, 4, 1, 4, 2, 4}},
		{5, []int64{2, 4, 3, 4, 1, 4}},
	} {
		opts := Options{Platforms: []string{"豆包", "千问"}, Images: []string{"a.png", "b.png", "c.png"}, StartBrowserIndex: tc.start}
		queue := BuildQueue(opts, map[string][]int64{"豆包": {1, 2, 3}, "千问": {4}})
		for i, task := range queue {
			if task.BrowserID != tc.want[i] {
				t.Errorf("起始序号 %d，任务 %d 浏览器 = %d，want %d", tc.start, i+1, task.BrowserID, tc.want[i])
			}
		}
	}
}

func TestBuildQueueTextOnlyOneTaskPerPrompt(t *testing.T) {
	opts := Options{
		Platforms:         []string{"豆包", "千问"},
		Prompts:           []string{" 画一只猫 ", "", "画一条鱼"},
		PromptSuffix:      "高清",
		UseSuffix:         true,
		StartBrowserIndex: 2,
	}
	q := BuildQueue(opts, map[string][]int64{"豆包": {1, 2, 3}, "千问": {4}})
	if len(q) != 4 {
		t.Fatalf("任务数 %d, want 4", len(q))
	}
	wantPrompt := []string{"画一只猫\n高清", "画一只猫\n高清", "画一条鱼\n高清", "画一条鱼\n高清"}
	wantPlat := []string{"豆包", "千问", "豆包", "千问"}
	wantBrowser := []int64{2, 4, 3, 4}
	for i, task := range q {
		if task.Prompt != wantPrompt[i] || task.Platform != wantPlat[i] || task.BrowserID != wantBrowser[i] || task.Images != nil || task.Index != i+1 {
			t.Fatalf("#%d plat=%s prompt=%q browser=%d images=%v index=%d", i, task.Platform, task.Prompt, task.BrowserID, task.Images, task.Index)
		}
	}
}

func TestBuildQueueTextOnlyBlankPrompts(t *testing.T) {
	q := BuildQueue(Options{Platforms: []string{"豆包"}, Prompts: []string{" ", "\n"}}, map[string][]int64{"豆包": {1}})
	if len(q) != 0 {
		t.Fatalf("空白提示词不应建任务: %+v", q)
	}
}

func TestBuildQueueImagesStillCycleWhenPresent(t *testing.T) {
	opts := Options{
		Platforms: []string{"豆包"},
		Prompts:   []string{"画一只猫", "画一条鱼"},
		Images:    []string{"a.png", "b.png", "c.png"},
	}
	q := BuildQueue(opts, map[string][]int64{"豆包": {1}})
	if len(q) != 3 {
		t.Fatalf("任务数 %d", len(q))
	}
	want := []string{"画一只猫", "画一条鱼", "画一只猫"}
	for i, task := range q {
		if task.Prompt != want[i] || len(task.Images) != 1 || task.Images[0] != opts.Images[i] {
			t.Fatalf("#%d prompt=%q images=%v", i, task.Prompt, task.Images)
		}
	}
}

func TestRunTextOnlySkipsUpload(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	addBrowsers(t, st, "豆包", "千问")
	e := NewEngine(st, nil, nil, "")
	strat := &submitStrategy{}
	err = e.run(context.Background(), Options{
		Platforms:   []string{"豆包", "千问"},
		Prompts:     []string{"画一只猫", "画一条鱼"},
		Concurrency: 1,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		if len(task.Images) != 0 {
			return fmt.Errorf("文生图任务带了源图: %v", task.Images)
		}
		return e.submitTask(task, strat, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(strat.calls, ",")
	if strings.Contains(joined, "upload") || strings.Count(joined, "clean") != 4 || strings.Count(joined, "mark") != 4 || strings.Count(joined, "send") != 4 {
		t.Fatalf("文生图应各做 4 次清理、标记、发送，且不上传: %s", joined)
	}
	snap := e.View().Snapshot()
	want := []struct{ plat, prompt string }{
		{"豆包", "画一只猫"},
		{"千问", "画一只猫"},
		{"豆包", "画一条鱼"},
		{"千问", "画一条鱼"},
	}
	if len(snap) != len(want) {
		t.Fatalf("任务数 %d", len(snap))
	}
	for i, task := range snap {
		if task.Platform != want[i].plat || task.Prompt != want[i].prompt || len(task.Images) != 0 || task.Status != StatusSuccess {
			t.Fatalf("#%d plat=%s prompt=%q images=%v status=%s err=%s", i, task.Platform, task.Prompt, task.Images, task.Status, task.Error)
		}
	}
}

func TestRunRejectsEmptyImagesAndPrompts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	addBrowsers(t, st, "豆包")
	e := NewEngine(st, nil, nil, "")
	err = e.Run(context.Background(), Options{Platforms: []string{"豆包"}}, func(Task) {
		t.Fatal("空队列不应开始任务")
	}, func(Progress) {})
	if err == nil || err.Error() != "任务队列为空：请添加提示词，或添加源图片" {
		t.Fatalf("err = %v", err)
	}
}

func TestRunChromeAndEdgeRequireBrowsers(t *testing.T) {
	for _, backend := range []string{"", "Chrome", "Edge", "Builtin", "ego lite"} {
		t.Run(backend, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			st.Set("browser_backend", backend)
			e := NewEngine(st, nil, nil, "")
			err = e.Run(context.Background(), Options{Platforms: []string{"千问"}, Prompts: []string{"画一只猫"}}, func(Task) {
				t.Fatal("没有浏览器实例时不应开始任务")
			}, func(Progress) {})
			if err == nil || err.Error() != "平台[千问]没有配置浏览器，请先在浏览器管理中添加" {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRunExtensionWithoutConfiguredBrowsers(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	st.Set(extbridge.SettingBrowserBackend, "extension")
	e := NewEngine(st, nil, nil, "")
	err = e.run(context.Background(), Options{Platforms: []string{"豆包", "千问"}, Prompts: []string{"画一只猫"}}, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	queue := e.View().Snapshot()
	if len(queue) != 2 || queue[0].BrowserID != -1 || queue[1].BrowserID != -2 {
		t.Fatalf("扩展调度占位: %+v", queue)
	}
	if len(st.Browsers()) != 0 {
		t.Fatal("扩展路径不应要求本机浏览器记录")
	}
}

func addBrowsers(t *testing.T, st *store.Store, platforms ...string) {
	t.Helper()
	for _, platform := range platforms {
		if _, err := st.AddBrowser(platform, platform, ""); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSessionCacheSeparatesBackends(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e := NewEngine(st, nil, nil, "")
	chrome, edge, builtin := &cdp.Session{}, &cdp.Session{}, &cdp.Session{}
	e.sessions["Chrome:豆包:1"] = chrome
	e.sessions["Edge:豆包:1"] = edge
	e.sessions["Builtin:豆包:1"] = builtin
	st.Set("browser_backend", "Chrome")
	if got, err := e.session(context.Background(), "豆包", 1, nil); err != nil || got != chrome {
		t.Fatalf("chrome session = %p, %v", got, err)
	}
	st.Set("browser_backend", "Edge")
	if got, err := e.session(context.Background(), "豆包", 1, nil); err != nil || got != edge {
		t.Fatalf("edge session = %p, %v", got, err)
	}
	st.Set("browser_backend", "Builtin")
	if got, err := e.session(context.Background(), "豆包", 1, nil); err != nil || got != builtin {
		t.Fatalf("builtin session = %p, %v", got, err)
	}
}

func TestRunStartBrowserDifferentPlatformCounts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	for _, platform := range []string{"豆包", "豆包", "豆包", "千问"} {
		if _, err := st.AddBrowser(platform, "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	e := NewEngine(st, nil, nil, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opts := Options{Platforms: []string{"豆包", "千问"}, Images: []string{"a.png"}, StartBrowserIndex: 3}
	if err := e.Run(ctx, opts, func(Task) {}, func(Progress) {}); err != nil {
		t.Fatal(err)
	}
	queue := e.View().Snapshot()
	if queue[0].BrowserID != 3 || queue[1].BrowserID != 4 {
		t.Fatalf("起始浏览器 = %d, %d，want 3, 4", queue[0].BrowserID, queue[1].BrowserID)
	}
}

func TestRunConcurrentProgress(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Chrome")
	const count = 2
	for range count {
		if _, err := st.AddBrowser("豆包", "test", ""); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{Platforms: []string{"豆包"}, Images: make([]string, count)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ready sync.WaitGroup
	ready.Add(count)
	release := make(chan struct{})
	go func() {
		ready.Wait()
		cancel()
		close(release)
	}()
	var progressMu sync.Mutex
	var progress []Progress
	e := NewEngine(st, nil, nil, "")
	viewStarted, viewDone, viewStopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(viewStopped)
		close(viewStarted)
		for {
			select {
			case <-viewDone:
				return
			default:
				if view := e.View(); view != nil {
					view.Snapshot()
				}
			}
		}
	}()
	<-viewStarted
	err = e.Run(ctx, opts, func(task Task) {
		if task.Status == StatusRunning {
			ready.Done()
			<-release
		}
	}, func(p Progress) {
		progressMu.Lock()
		progress = append(progress, p)
		progressMu.Unlock()
	})
	close(viewDone)
	<-viewStopped
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) != count+1 {
		t.Fatalf("进度回调次数 = %d，want %d", len(progress), count+1)
	}
	for i, p := range progress {
		if p.Done != i || p.Total != count {
			t.Errorf("进度 #%d = %+v，want %d/%d", i, p, i, count)
		}
	}
}

func TestRunSerializesEachBrowser(t *testing.T) {
	for _, backend := range []string{"Chrome", "Edge"} {
		t.Run(backend, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			st.Set("browser_backend", backend)
			opts := Options{Platforms: []string{"豆包"}, Images: make([]string, 4), Concurrency: 4}
			for range 2 {
				if _, err := st.AddBrowser("豆包", "test", ""); err != nil {
					t.Fatal(err)
				}
			}
			e := NewEngine(st, nil, nil, "")
			ctx, cancel := context.WithCancel(context.Background())
			started := make(chan Task, 4)
			release := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- e.Run(ctx, opts, func(task Task) {
					if task.Status == StatusRunning {
						started <- task
						<-release
					}
				}, func(Progress) {})
			}()
			defer func() {
				cancel()
				close(release)
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(time.Second):
					t.Error("取消后等待浏览器锁的任务未及时退出")
				}
				if len(started) != 0 {
					t.Error("取消后等待中的任务仍进入了运行状态")
				}
			}()
			running := make([]Task, 2)
			for i := range running {
				select {
				case running[i] = <-started:
				case <-time.After(time.Second):
					t.Fatal("应有 2 个独立执行空间进入运行状态")
				}
			}
			if len(running) == 2 && running[0].BrowserID == running[1].BrowserID {
				t.Fatal("同一个浏览器同时运行了两个任务")
			}
			select {
			case task := <-started:
				t.Errorf("同一执行空间的前一个任务尚未结束，任务 %d（浏览器 %d）已开始", task.Index, task.BrowserID)
			case <-time.After(50 * time.Millisecond):
			}
		})
	}
}

type resultBytesStrategy struct {
	strategy.Strategy
	data []byte
}

type submitStrategy struct {
	strategy.Strategy
	mu       sync.Mutex
	cleanErr error
	calls    []string
}

func (s *submitStrategy) note(name string) {
	s.mu.Lock()
	s.calls = append(s.calls, name)
	s.mu.Unlock()
}

func (s *submitStrategy) CleanComposer(*cdp.Session) error {
	s.note("clean")
	return s.cleanErr
}

func (s *submitStrategy) UploadFiles(*cdp.Session, []string) error {
	s.note("upload")
	return nil
}

func (s *submitStrategy) MarkTask(*cdp.Session) error {
	s.note("mark")
	return nil
}

func (s *submitStrategy) SendPrompt(*cdp.Session, string) error {
	s.note("send")
	return nil
}

func TestSubmitTaskSkipsUploadWithoutImages(t *testing.T) {
	strat := &submitStrategy{}
	err := (&Engine{}).submitTask(&Task{Prompt: "画一只猫"}, strat, nil)
	if err != nil || strings.Join(strat.calls, ",") != "clean,mark,send" {
		t.Fatalf("err=%v calls=%v", err, strat.calls)
	}
}

func TestSubmitTaskStopsAfterCleanFailure(t *testing.T) {
	cleanErr := errors.New("关闭客户端推广失败")
	for _, failure := range []error{cleanErr, nil} {
		strat := &submitStrategy{cleanErr: failure}
		err := (&Engine{}).submitTask(&Task{Images: []string{"source.png"}}, strat, nil)
		wantCalls := "clean"
		if failure == nil {
			wantCalls = "clean,upload,mark,send"
		}
		if !errors.Is(err, failure) || strings.Join(strat.calls, ",") != wantCalls {
			t.Fatalf("清理错误 %v: err=%v, calls=%v, want %s", failure, err, strat.calls, wantCalls)
		}
	}
}

func TestRunVerificationStopsBatchWithoutResend(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	addBrowsers(t, st, "豆包")
	e := NewEngine(st, nil, nil, "")
	strat := &submitStrategy{cleanErr: captcha.ErrVerificationRequired}
	opts := Options{Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png", "c.png", "d.png"}, Concurrency: 4, RetryCount: 3}
	var attempts int
	err = e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		attempts++
		return e.submitTask(task, strat, nil)
	})
	if err != nil || attempts != 1 || strings.Join(strat.calls, ",") != "clean" {
		t.Fatalf("verification must not be resent and must not cancel the run: %v attempts=%d calls=%v", err, attempts, strat.calls)
	}
	manual, held := 0, 0
	for _, task := range e.View().Snapshot() {
		if task.Error == "已停止" || task.Status == StatusSkipped {
			t.Fatalf("verification must not cancel other tasks: %+v", task)
		}
		if task.Status == StatusNeedsUser && task.Attempts == 1 {
			manual++
		}
		if task.Status == StatusFailed && strings.Contains(task.Error, "验证") {
			held++
		}
	}
	if manual != 1 || held != 3 {
		t.Fatalf("one manual task, the rest wait on that account: %+v", e.View().Snapshot())
	}
}

func TestRunVerificationRetainsSavedPartial(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	addBrowsers(t, st, "千问")
	e := NewEngine(st, nil, nil, "")
	data := resultPNG(t, 21)
	strat := &resultSequenceStrategy{rounds: [][]strategy.Result{{{Src: "download", Data: data}}}, waitErr: errors.Join(captcha.ErrVerificationRequired, errors.New("after first download"))}
	opts := Options{Platforms: []string{"千问"}, Images: []string{"source.png"}, RetryCount: 3, WaitResultSeconds: 60, OutputPath: t.TempDir()}
	err = e.run(context.Background(), opts, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
		return e.waitAndSave(ctx, task, strat, nil)
	})
	got := e.View().Snapshot()[0]
	if err != nil || got.Status != StatusNeedsUser || got.Error == "已停止" || got.Attempts != 1 || len(got.Results) != 1 || strat.calls != 1 {
		t.Fatalf("partial must survive without resend or cancelling the run: error=%v task=%+v waits=%d", err, got, strat.calls)
	}
	if saved, err := os.ReadFile(got.Results[0]); err != nil || !bytes.Equal(saved, data) {
		t.Fatalf("completed image not retained: %v", err)
	}
}

func TestExecuteDeadlineIsImageTimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	e := NewEngine(nil, nil, nil, "")
	got := e.execute(ctx, Task{Results: []string{"already-saved.png"}}, func(context.Context, *Task) error {
		t.Fatal("时限已经到了就不要再尝试")
		return nil
	})
	if got.Status != StatusFailed || got.Error != "出图超时" || len(got.Results) != 1 {
		t.Fatalf("整体超时应标成出图超时并留下已有结果: %+v", got)
	}
}

func TestExecuteCancellationBeatsVerification(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	e := NewEngine(nil, nil, nil, "")
	e.opts.RetryCount = 3
	got := e.execute(ctx, Task{Results: []string{"already-saved.png"}}, func(context.Context, *Task) error {
		cancel()
		return errors.Join(captcha.ErrVerificationRequired, context.Canceled)
	})
	if got.Status != StatusSkipped || got.Attempts != 1 || got.Error != "已停止" || len(got.Results) != 1 {
		t.Fatalf("cancellation must win and keep previous results: %+v", got)
	}
}

type failedSessionFixture struct {
	ctx        context.Context
	cancel     context.CancelFunc
	ego        bool
	events     []string
	handOffErr error
	onHandOff  func() error
}

func (s *failedSessionFixture) Ctx() context.Context { return s.ctx }
func (s *failedSessionFixture) IsEgo() bool          { return s.ego }
func (s *failedSessionFixture) EgoEvaluate(_ string, _ any) error {
	s.events = append(s.events, "manual")
	return s.ctx.Err()
}
func (s *failedSessionFixture) HandOff() error {
	if s.ctx.Err() != nil {
		return s.ctx.Err()
	}
	s.events = append(s.events, "handoff")
	if s.onHandOff != nil {
		return s.onHandOff()
	}
	return s.handOffErr
}
func (s *failedSessionFixture) Close() {
	s.events = append(s.events, "close")
	s.cancel()
}

func TestVerificationHandsOffBeforeSessionCancellation(t *testing.T) {
	for _, scenario := range []string{"ego", "handoff-error", "canceled", "chrome"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sessionCtx, sessionCancel := context.WithCancel(ctx)
			sess := &failedSessionFixture{ctx: sessionCtx, cancel: sessionCancel, ego: scenario != "chrome"}
			failure := errors.New("handoff failed")
			if scenario == "handoff-error" {
				sess.handOffErr = failure
			}
			if scenario == "canceled" {
				cancel()
			}
			err := closeFailedSession(ctx, sess, captcha.ErrVerificationRequired)
			want := "manual,handoff,close"
			if scenario == "canceled" || scenario == "chrome" {
				want = "close"
			}
			if strings.Join(sess.events, ",") != want {
				t.Fatalf("events=%v want=%s", sess.events, want)
			}
			if scenario == "canceled" {
				if !errors.Is(err, context.Canceled) || errors.Is(err, captcha.ErrVerificationRequired) {
					t.Fatalf("cancellation must win: %v", err)
				}
			} else if !errors.Is(err, captcha.ErrVerificationRequired) || scenario == "handoff-error" && !errors.Is(err, failure) {
				t.Fatalf("manual and handoff errors must survive Join: %v", err)
			}
		})
	}
}

func TestVerificationHandOffTimeoutRetainsNeedsUserUnlessParentStops(t *testing.T) {
	for _, scenario := range []string{"handoff-timeout", "parent-canceled", "parent-deadline"} {
		t.Run(scenario, func(t *testing.T) {
			parentLimit := time.Second
			if scenario == "parent-deadline" {
				parentLimit = 10 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), parentLimit)
			defer cancel()
			sessionCtx, sessionCancel := context.WithCancel(ctx)
			defer sessionCancel()
			sess := &failedSessionFixture{ctx: sessionCtx, cancel: sessionCancel, ego: true}
			sess.onHandOff = func() error {
				limit := time.Millisecond
				if scenario == "parent-deadline" {
					limit = time.Second
				}
				bounded, stop := context.WithTimeout(sessionCtx, limit)
				defer stop()
				if scenario == "parent-canceled" {
					cancel()
				}
				<-bounded.Done()
				return bounded.Err()
			}
			e := NewEngine(nil, nil, nil, "")
			e.opts.RetryCount = 3
			var attemptErr error
			got := e.execute(ctx, Task{Results: []string{"already-saved.png"}}, func(context.Context, *Task) error {
				attemptErr = closeFailedSession(ctx, sess, captcha.ErrVerificationRequired)
				return attemptErr
			})
			want := StatusNeedsUser
			if scenario == "parent-canceled" {
				want = StatusSkipped
			}
			if scenario == "parent-deadline" {
				want = StatusFailed
			}
			if got.Status != want || got.Attempts != 1 || len(got.Results) != 1 || strings.Join(sess.events, ",") != "manual,handoff,close" {
				t.Fatalf("error=%v task=%+v events=%v", attemptErr, got, sess.events)
			}
			if scenario == "handoff-timeout" {
				if !errors.Is(attemptErr, captcha.ErrVerificationRequired) || errors.Is(attemptErr, context.DeadlineExceeded) || !strings.Contains(got.Error, "交还浏览器超时") {
					t.Fatalf("private timeout must preserve verification: %v", attemptErr)
				}
			} else if scenario == "parent-deadline" {
				if !errors.Is(attemptErr, context.DeadlineExceeded) || got.Error != "出图超时" {
					t.Fatalf("整体超时要标成出图超时: error=%v task=%+v", attemptErr, got)
				}
			} else if !errors.Is(attemptErr, ctx.Err()) || errors.Is(attemptErr, captcha.ErrVerificationRequired) || got.Error != "已停止" {
				t.Fatalf("parent cancellation must win: error=%v task=%+v", attemptErr, got)
			}
		})
	}
}

func (s resultBytesStrategy) FetchResultBytes(*cdp.Session, string) ([]byte, error) {
	return s.data, nil
}

func resultPNG(t *testing.T, shade uint8) []byte {
	t.Helper()
	const n = 128
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	rnd := rand.New(rand.NewPCG(uint64(shade)+1, 7))
	for i := range img.Pix {
		img.Pix[i] = byte(rnd.Uint32())
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	if !images.AcceptGenerated(data.Bytes()) {
		t.Fatalf("fixture is not a generated image (%d bytes)", data.Len())
	}
	return data.Bytes()
}

type resultSequenceStrategy struct {
	strategy.Strategy
	rounds  [][]strategy.Result
	data    map[string][]byte
	calls   int
	onWait  func()
	onFetch func(string)
	waitErr error
}

func (s *resultSequenceStrategy) WaitForResult(*cdp.Session, time.Duration) ([]strategy.Result, error) {
	if s.onWait != nil {
		s.onWait()
	}
	i := min(s.calls, len(s.rounds)-1)
	s.calls++
	return s.rounds[i], s.waitErr
}

func (s *resultSequenceStrategy) FetchResultBytes(_ *cdp.Session, url string) ([]byte, error) {
	if s.onFetch != nil {
		s.onFetch(url)
	}
	if data, ok := s.data[url]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func TestSaveResultsFailureURLDiagnostic(t *testing.T) {
	e := &Engine{}
	start := len(logging.Log.Entries())
	_, err := e.saveResults(context.Background(), &Task{}, &resultSequenceStrategy{}, nil, []strategy.Result{{URL: "https://cdn.example/generated/result.png?token=private-signature#private-fragment"}})
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic strings.Builder
	for _, entry := range logging.Log.Entries()[start:] {
		diagnostic.WriteString(entry.Text)
	}
	text := diagnostic.String()
	if !strings.Contains(text, "scheme=https; host=cdn.example; path=/generated/result.png") || strings.Contains(text, "private-signature") || strings.Contains(text, "private-fragment") {
		t.Fatalf("抓取诊断不应包含URL签名或fragment: %s", text)
	}
}

func TestSaveResultsUsesDownloadedDataWithoutFetching(t *testing.T) {
	valid := resultPNG(t, 1)
	oversized := append([]byte(nil), valid[:33]...)
	binary.BigEndian.PutUint32(oversized[16:20], maxResultPixels+1)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	for _, tc := range []struct {
		name     string
		data     []byte
		canceled bool
		wantSave bool
	}{
		{name: "valid", data: valid, wantSave: true},
		{name: "truncated", data: valid[:33]},
		{name: "empty", data: []byte{}},
		{name: "unsupported", data: []byte("not an image")},
		{name: "bad-header", data: []byte("GIF123")},
		{name: "oversized", data: oversized},
		{name: "canceled", data: valid, canceled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			e := NewEngine(nil, nil, nil, "")
			e.opts.OutputPath = t.TempDir()
			strat := &resultSequenceStrategy{onFetch: func(string) {
				t.Fatal("已下载的结果不得再次抓取 URL")
			}}
			saved, err := e.saveResults(ctx, &Task{Images: []string{"source.png"}}, strat, nil, []strategy.Result{{URL: "https://example.test/result.png", Src: "download", Data: tc.data}})
			if tc.canceled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("已取消的下载不得保存: %v", err)
				}
			} else if !tc.wantSave {
				if err == nil || !strings.Contains(err.Error(), "网页下载结果 #1 校验失败") {
					t.Fatalf("损坏的已下载文件应立即报校验错误: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			wantCount := 0
			if tc.wantSave {
				wantCount = 1
			}
			if len(saved) != wantCount {
				t.Fatalf("下载结果保存 = %v, wantSave=%v", saved, tc.wantSave)
			}
			if tc.wantSave {
				if data, err := os.ReadFile(saved[0]); err != nil || !bytes.Equal(data, valid) {
					t.Fatalf("应保存下载字节原样: %v", err)
				}
			} else if files, err := os.ReadDir(e.opts.OutputPath); err != nil || len(files) != 0 || len(e.seenHash) != 0 {
				t.Fatalf("无效或取消的下载不得留下文件或去重占位: %v, %v", files, err)
			}
		})
	}
}

func TestWaitAndSaveContinuesAfterInvalidCandidates(t *testing.T) {
	source, generated := resultPNG(t, 1), resultPNG(t, 2)
	strat := &resultSequenceStrategy{
		rounds: [][]strategy.Result{{{URL: "source"}, {URL: "bad"}}, {{URL: "source"}, {URL: "bad"}, {URL: "generated"}}},
		data:   map[string][]byte{"source": source, "generated": generated},
	}
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir(), WaitResultSeconds: 3}
	e.srcHashes = map[string]bool{images.SHA256(source): true}
	task := &Task{Index: 1, Images: []string{"source.png"}}
	if err := e.waitAndSave(context.Background(), task, strat, nil); err != nil {
		t.Fatal(err)
	}
	if strat.calls != 2 || len(task.Results) != 1 {
		t.Fatalf("应等到第二批真实结果: 调用%d次，保存%v", strat.calls, task.Results)
	}
	data, err := os.ReadFile(task.Results[0])
	if err != nil || !bytes.Equal(data, generated) {
		t.Fatalf("保存的不是生成图: %v", err)
	}
}

func TestWaitAndSaveReturnsDiskError(t *testing.T) {
	output := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(output, []byte("blocks directory creation"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: output, WaitResultSeconds: 60}
	strat := &resultSequenceStrategy{rounds: [][]strategy.Result{{{URL: "generated"}}}, data: map[string][]byte{"generated": resultPNG(t, 1)}}
	if err := e.waitAndSave(context.Background(), &Task{}, strat, nil); err == nil || strat.calls != 1 {
		t.Fatalf("磁盘错误应立即返回: %v, 调用%d次", err, strat.calls)
	}
}

func TestWaitAndSaveRetainsDownloadedPartialOnFailure(t *testing.T) {
	downloadErr := errors.New("第二张网页下载失败")
	for _, failure := range []error{downloadErr, context.Canceled} {
		e := NewEngine(nil, nil, nil, "")
		e.opts = Options{OutputPath: t.TempDir(), WaitResultSeconds: 60}
		data := resultPNG(t, 1)
		strat := &resultSequenceStrategy{
			rounds:  [][]strategy.Result{{{Src: "download", Data: data}}},
			waitErr: failure,
		}
		task := &Task{Images: []string{"source.png"}}
		err := e.waitAndSave(context.Background(), task, strat, nil)
		if !errors.Is(err, failure) {
			t.Fatalf("部分下载必须保留原失败: got %v, want %v", err, failure)
		}
		if failure == downloadErr {
			if len(task.Results) != 1 {
				t.Fatalf("后续下载失败不能丢失第一张: %v", task.Results)
			}
			if saved, err := os.ReadFile(task.Results[0]); err != nil || !bytes.Equal(saved, data) {
				t.Fatalf("第一张下载应完整保留: %v", err)
			}
		} else if files, err := os.ReadDir(e.opts.OutputPath); err != nil || len(files) != 0 || len(task.Results) != 0 {
			t.Fatalf("取消优先，不得新增保存: %v, %v", files, err)
		}
	}
}

func TestWaitAndSaveRejectsInvalidDownloadAfterPartial(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir(), WaitResultSeconds: 60}
	valid := resultPNG(t, 1)
	strat := &resultSequenceStrategy{rounds: [][]strategy.Result{{
		{Src: "download", Data: valid}, {Src: "download", Data: valid[:33]},
	}}}
	task := &Task{Images: []string{"source.png"}}
	err := e.waitAndSave(context.Background(), task, strat, nil)
	if err == nil || !strings.Contains(err.Error(), "网页下载结果 #2 校验失败") || len(task.Results) != 1 {
		t.Fatalf("后续下载损坏应失败并保留首图: results=%v, err=%v", task.Results, err)
	}
	if data, err := os.ReadFile(task.Results[0]); err != nil || !bytes.Equal(data, valid) {
		t.Fatalf("首图应完整保留: %v", err)
	}
}

func TestWaitAndSaveHonorsCancellation(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts.WaitResultSeconds = 60
	strat := &resultSequenceStrategy{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.waitAndSave(ctx, &Task{}, strat, nil); err != context.Canceled || strat.calls != 0 {
		t.Fatalf("取消后不应继续请求: %v, 调用%d次", err, strat.calls)
	}
}

func TestWaitAndSaveCancellationDuringResultRead(t *testing.T) {
	for _, boundary := range []string{"wait", "fetch"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			strat := &resultSequenceStrategy{
				rounds: [][]strategy.Result{{{URL: "generated"}}},
				data:   map[string][]byte{"generated": resultPNG(t, 1)},
			}
			fetches := 0
			strat.onFetch = func(string) {
				fetches++
				if boundary == "fetch" {
					cancel()
				}
			}
			if boundary == "wait" {
				strat.onWait = cancel
			}
			e := NewEngine(nil, nil, nil, "")
			e.opts = Options{OutputPath: t.TempDir(), WaitResultSeconds: 60}
			task := &Task{Images: []string{"source.png"}}
			if err := e.waitAndSave(ctx, task, strat, nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("结果读取中取消应返回取消错误: %v", err)
			}
			files, err := os.ReadDir(e.opts.OutputPath)
			if err != nil || len(files) != 0 || len(task.Results) != 0 || len(e.seenHash) != 0 {
				t.Fatalf("取消后不应保存或保留去重占位: files=%v, results=%v, hashes=%v, err=%v", files, task.Results, e.seenHash, err)
			}
			if boundary == "wait" && fetches != 0 {
				t.Fatalf("等待时已取消，不应继续抓取: %d", fetches)
			}
		})
	}
}

func TestWaitAndSaveRetainsPartialResultsOnDiskError(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir(), WaitResultSeconds: 60}
	// 第一张文件名可保存，第二张冲突后的后缀会超过文件系统的 255 字节文件名上限。
	base := strings.Repeat("s", 248)
	blocker := filepath.Join(e.opts.OutputPath, base+"_02.png")
	if err := os.WriteFile(blocker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, second := resultPNG(t, 1), resultPNG(t, 2)
	strat := &resultSequenceStrategy{
		rounds: [][]strategy.Result{{{URL: "first"}, {URL: "second"}}},
		data:   map[string][]byte{"first": first, "second": second},
	}
	task := &Task{Images: []string{base + ".png"}}
	if err := e.waitAndSave(context.Background(), task, strat, nil); err == nil {
		t.Fatal("第二张保存错误应返回")
	}
	if len(task.Results) != 1 {
		t.Fatalf("已落盘的第一张应保留在任务结果中: %v", task.Results)
	}
	if data, err := os.ReadFile(task.Results[0]); err != nil || !bytes.Equal(data, first) {
		t.Fatalf("已保存的第一张应保持完整: %v", err)
	}
	if data, err := os.ReadFile(blocker); err != nil || string(data) != "keep" {
		t.Fatalf("第二张冲突文件不应覆盖: %v", err)
	}
	if !e.seenHash[images.SHA256(first)] || e.seenHash[images.SHA256(second)] {
		t.Fatalf("去重状态应只保留成功文件: %v", e.seenHash)
	}
}

func TestSaveResultsRejectsIncompleteAndOversizedImages(t *testing.T) {
	header := append([]byte(nil), resultPNG(t, 1)[:33]...)
	oversized := append([]byte(nil), header...)
	binary.BigEndian.PutUint32(oversized[16:20], 8193)
	binary.BigEndian.PutUint32(oversized[20:24], 4096)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"incomplete", header}, {"oversized", oversized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := image.DecodeConfig(bytes.NewReader(tc.data)); err != nil {
				t.Fatalf("用例须具有可读图片头: %v", err)
			}
			e := NewEngine(nil, nil, nil, "")
			e.opts = Options{OutputPath: t.TempDir()}
			saved, err := e.saveResults(context.Background(), &Task{Images: []string{"source.png"}}, resultBytesStrategy{data: tc.data}, nil, []strategy.Result{{URL: "invalid"}})
			if err != nil || len(saved) != 0 || len(e.seenHash) != 0 {
				t.Fatalf("不可完整解码或过大的图片不得计为成功: saved=%v, hashes=%v, err=%v", saved, e.seenHash, err)
			}
			files, err := os.ReadDir(e.opts.OutputPath)
			if err != nil || len(files) != 0 {
				t.Fatalf("无效图片不得留下文件: %v, %v", files, err)
			}
		})
	}
}

func TestSaveResultsUsesImageFormat(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 160, 160))
	rnd := rand.New(rand.NewPCG(3, 9))
	for y := 0; y < 160; y++ {
		for x := 0; x < 160; x++ {
			img.SetRGBA(x, y, color.RGBA{R: byte(rnd.Uint32()), G: byte(rnd.Uint32()), B: byte(rnd.Uint32()), A: 255})
		}
	}
	var jpg, animation bytes.Buffer
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := gif.Encode(&animation, img, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ext  string
		data []byte
	}{{".png", resultPNG(t, 1)}, {".jpg", jpg.Bytes()}, {".gif", animation.Bytes()}} {
		t.Run(tc.ext, func(t *testing.T) {
			e := NewEngine(nil, nil, nil, "")
			e.opts = Options{OutputPath: t.TempDir(), FilePrefix: "pre_", FilePrefixEnabled: true}
			saved, err := e.saveResults(context.Background(), &Task{Images: []string{"source.png"}}, resultBytesStrategy{data: tc.data}, nil, []strategy.Result{{URL: "result"}})
			if err != nil || len(saved) != 1 {
				t.Fatalf("保存结果 = %v, %v", saved, err)
			}
			if filepath.Base(saved[0]) != "pre_source"+tc.ext {
				t.Errorf("结果文件名 = %s, want pre_source%s", saved[0], tc.ext)
			}
			data, err := os.ReadFile(saved[0])
			if err != nil || !bytes.Equal(data, tc.data) {
				t.Errorf("保存后字节改变: %v", err)
			}
		})
	}
}

func TestSaveResultsWatermarkKeepsOriginalByDefaultAndOnNoMatch(t *testing.T) {
	data := resultPNG(t, 43)
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", enabled), func(t *testing.T) {
			e := NewEngine(nil, nil, nil, "")
			e.opts = Options{OutputPath: t.TempDir(), RemoveWatermark: enabled}
			start := len(logging.Log.Entries())
			saved, err := e.saveResults(context.Background(), &Task{Platform: "千问", Images: []string{"source.png"}}, resultBytesStrategy{}, nil, []strategy.Result{{Data: data}})
			if err != nil || len(saved) != 1 || filepath.Base(saved[0]) != "source.png" {
				t.Fatalf("默认或不匹配须返回原图: paths=%v, err=%v", saved, err)
			}
			if original, err := os.ReadFile(saved[0]); err != nil || !bytes.Equal(original, data) {
				t.Fatalf("原图字节必须完整保留: %v", err)
			}
			files, err := os.ReadDir(e.opts.OutputPath)
			if err != nil || len(files) != 1 {
				t.Fatalf("未处理不得新增修复文件: %v, %v", files, err)
			}
			warned := false
			for _, entry := range logging.Log.Entries()[start:] {
				if entry.Level == logging.Warning && strings.Contains(entry.Text, "未匹配") {
					warned = true
				}
			}
			if warned != enabled {
				t.Fatalf("启用且不匹配应有明确警告: enabled=%t, warned=%t", enabled, warned)
			}
		})
	}
}

func watermarkResultPNG(t *testing.T, platform string) []byte {
	t.Helper()
	file, ref, right, bottom := "doubao.png", 1536, 31, 27
	if platform == "千问" {
		file, ref, right, bottom = "qwen.png", 2048, 62, 63
	}
	const side = 768
	asset := filepath.Join("..", "images", "watermarks", file)
	data, err := os.ReadFile(asset)
	if err != nil {
		t.Fatal(err)
	}
	template, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	scale := float64(side) / float64(ref)
	width := int(math.Round(float64(template.Bounds().Dx()) * scale))
	template, err = images.Thumb(asset, width)
	if err != nil {
		t.Fatal(err)
	}
	x0 := side - int(math.Round(float64(right)*scale)) - template.Bounds().Dx()
	y0 := side - int(math.Round(float64(bottom)*scale)) - template.Bounds().Dy()
	background := color.NRGBA{25, 48, 72, 255}
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	draw.Draw(img, img.Bounds(), &image.Uniform{background}, image.Point{}, draw.Src)
	for y := 0; y < template.Bounds().Dy(); y++ {
		for x := 0; x < template.Bounds().Dx(); x++ {
			r, _, _, _ := template.At(x, y).RGBA()
			alpha := float64(r) / 65535
			mix := func(v uint8) uint8 { return uint8(math.Round(float64(v) + (255-float64(v))*alpha)) }
			img.SetNRGBA(x0+x, y0+y, color.NRGBA{mix(background.R), mix(background.G), mix(background.B), 255})
		}
	}
	encoded, err := images.EncodePNG(img)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestSaveResultsWatermarkMatchedCreatesCopyWithSingleResult(t *testing.T) {
	for _, platform := range []string{"豆包", "千问"} {
		data := watermarkResultPNG(t, platform)
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", platform, enabled), func(t *testing.T) {
				e := NewEngine(nil, nil, nil, "")
				e.opts = Options{OutputPath: t.TempDir(), RemoveWatermark: enabled, WaitResultSeconds: 60}
				task := &Task{Platform: platform, Images: []string{"source.png"}}
				strat := &resultSequenceStrategy{rounds: [][]strategy.Result{{{Data: data}}}}
				if err := e.waitAndSave(context.Background(), task, strat, nil); err != nil || len(task.Results) != 1 {
					t.Fatalf("一张生成图只应有一个结果路径: %v, %v", task.Results, err)
				}
				original := filepath.Join(e.opts.OutputPath, "source.png")
				if saved, err := os.ReadFile(original); err != nil || !bytes.Equal(saved, data) {
					t.Fatalf("原图始终保留下载字节: %v", err)
				}
				wantName, wantFiles := "source.png", 1
				if enabled {
					wantName, wantFiles = "source_去水印.png", 2
					repaired, err := os.ReadFile(task.Results[0])
					if err != nil || bytes.Equal(repaired, data) {
						t.Fatalf("命中应另存处理版: %v", err)
					}
					img, format, err := image.Decode(bytes.NewReader(repaired))
					if err != nil || format != "png" || img.Bounds().Dx() != 768 || img.Bounds().Dy() != 768 {
						t.Fatalf("处理版应为同尺寸完整 PNG: format=%s, err=%v", format, err)
					}
				}
				if filepath.Base(task.Results[0]) != wantName {
					t.Fatalf("结果路径 = %s, want %s", task.Results[0], wantName)
				}
				files, err := os.ReadDir(e.opts.OutputPath)
				if err != nil || len(files) != wantFiles {
					t.Fatalf("落盘文件数应为 %d: %v, %v", wantFiles, files, err)
				}
			})
		}
	}
}

func TestSaveResultsWatermarkWriteFailureKeepsOriginalWithoutRetry(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir(), RemoveWatermark: true, WaitResultSeconds: 60, RetryCount: 2}
	data := watermarkResultPNG(t, "豆包")
	strat := &resultSequenceStrategy{rounds: [][]strategy.Result{{{Data: data}}}}
	// 原图名合法，另存的中文后缀将超过文件系统单文件名上限。
	task := Task{Platform: "豆包", Images: []string{strings.Repeat("s", 248) + ".png"}}
	start := len(logging.Log.Entries())
	task = e.execute(context.Background(), task, func(ctx context.Context, task *Task) error {
		return e.waitAndSave(ctx, task, strat, nil)
	})
	if task.Status != StatusSuccess || task.Attempts != 1 || len(task.Results) != 1 || strat.calls != 1 {
		t.Fatalf("可选修复失败不得重发或丢失原图: task=%+v, calls=%d", task, strat.calls)
	}
	if saved, err := os.ReadFile(task.Results[0]); err != nil || !bytes.Equal(saved, data) {
		t.Fatalf("修复写失败必须保留原图: %v", err)
	}
	warned := false
	for _, entry := range logging.Log.Entries()[start:] {
		if entry.Level == logging.Warning && strings.Contains(entry.Text, "去水印版本保存失败") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("修复版本写失败须明确警告")
	}
	if files, err := os.ReadDir(e.opts.OutputPath); err != nil || len(files) != 1 {
		t.Fatalf("修复失败只应留完整原图: %v, %v", files, err)
	}
}

func TestSaveResultsWatermarkCancellationRetainsOriginal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logging.Log.SetSink(func(entry logging.Entry) {
		if strings.Contains(entry.Text, "正在计算去水印") {
			cancel()
		}
	})
	defer logging.Log.SetSink(nil)
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir(), RemoveWatermark: true, WaitResultSeconds: 60}
	data := resultPNG(t, 71)
	strat := &resultSequenceStrategy{rounds: [][]strategy.Result{{{Data: data}}}}
	task := &Task{Platform: "千问", Images: []string{"source.png"}}
	if err := e.waitAndSave(ctx, task, strat, nil); !errors.Is(err, context.Canceled) || len(task.Results) != 1 {
		t.Fatalf("处理中取消必须返回取消且保留原图路径: results=%v, err=%v", task.Results, err)
	}
	if original, err := os.ReadFile(task.Results[0]); err != nil || !bytes.Equal(original, data) {
		t.Fatalf("取消不得删除已完成原图: %v", err)
	}
	files, err := os.ReadDir(e.opts.OutputPath)
	if err != nil || len(files) != 1 || !e.seenHash[images.SHA256(data)] {
		t.Fatalf("取消只保留原图和已完成去重状态: files=%v, hashes=%v, err=%v", files, e.seenHash, err)
	}
}

func TestSaveResultsWatermarkSkipsAnimatedFormats(t *testing.T) {
	var animation bytes.Buffer
	palette := make(color.Palette, 256)
	for i := range palette {
		palette[i] = color.RGBA{R: uint8(i), G: uint8(255 - i), B: uint8(i * 3), A: 255}
	}
	frame := image.NewPaletted(image.Rect(0, 0, 180, 180), palette)
	rnd := rand.New(rand.NewPCG(11, 13))
	for i := range frame.Pix {
		frame.Pix[i] = byte(rnd.Uint32())
	}
	if err := gif.EncodeAll(&animation, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{1, 1}}); err != nil {
		t.Fatal(err)
	}
	pngData := resultPNG(t, 42)
	chunk := make([]byte, 20)
	binary.BigEndian.PutUint32(chunk, 8)
	copy(chunk[4:8], "acTL")
	binary.BigEndian.PutUint32(chunk[8:12], 2)
	binary.BigEndian.PutUint32(chunk[16:], crc32.ChecksumIEEE(chunk[4:16]))
	animatedPNG := append(append(append([]byte{}, pngData[:33]...), chunk...), pngData[33:]...)
	for _, tc := range []struct {
		format string
		data   []byte
	}{{"gif", animation.Bytes()}, {"png", animatedPNG}} {
		t.Run(tc.format, func(t *testing.T) {
			e := NewEngine(nil, nil, nil, "")
			e.opts = Options{OutputPath: t.TempDir(), RemoveWatermark: true}
			start := len(logging.Log.Entries())
			saved, err := e.saveResults(context.Background(), &Task{Platform: "千问", Images: []string{"source.png"}}, resultBytesStrategy{}, nil, []strategy.Result{{Data: tc.data}})
			if err != nil || len(saved) != 1 {
				t.Fatalf("动画应保留原图: %v, %v", saved, err)
			}
			if original, err := os.ReadFile(saved[0]); err != nil || !bytes.Equal(original, tc.data) {
				t.Fatalf("不得把动画转为静态图: %v", err)
			}
			warned := false
			for _, entry := range logging.Log.Entries()[start:] {
				if entry.Level == logging.Warning && strings.Contains(entry.Text, "跳过去水印") {
					warned = true
				}
			}
			if !warned {
				t.Fatal("跳过动画应明确警告")
			}
		})
	}
}

func TestWatermarkStaticWebP(t *testing.T) {
	for _, tc := range []struct {
		kind string
		flag byte
		want bool
	}{{"VP8X", 0, true}, {"VP8X", 2, false}, {"ANIM", 0, false}, {"ANMF", 0, false}} {
		data := make([]byte, 22)
		copy(data, "RIFF")
		copy(data[8:], "WEBP")
		copy(data[12:], tc.kind)
		binary.LittleEndian.PutUint32(data[16:], 2)
		data[20] = tc.flag
		if got := watermarkStaticImage(data, "webp"); got != tc.want {
			t.Errorf("%s flag=%d static=%t, want %t", tc.kind, tc.flag, got, tc.want)
		}
	}
}

func TestSaveResultsConcurrentNamesNeverOverwrite(t *testing.T) {
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: t.TempDir()}
	for _, name := range []string{"source.png", "source_1.png"} {
		if err := os.WriteFile(filepath.Join(e.opts.OutputPath, name), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const count = 32
	start := make(chan struct{})
	paths := make(chan string, count)
	var workers sync.WaitGroup
	for i := range count {
		data := resultPNG(t, uint8(i))
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			saved, err := e.saveResults(context.Background(), &Task{Images: []string{"source.png"}}, resultBytesStrategy{data: data}, nil, []strategy.Result{{URL: "result"}})
			if err != nil || len(saved) != 1 {
				t.Errorf("并发保存 = %v, %v", saved, err)
				return
			}
			paths <- saved[0]
		}()
	}
	close(start)
	workers.Wait()
	close(paths)
	unique := map[string]bool{}
	for path := range paths {
		if unique[path] {
			t.Errorf("并发结果覆盖同一个路径: %s", path)
		}
		unique[path] = true
	}
	if len(unique) != count {
		t.Errorf("独立结果文件数 = %d, want %d", len(unique), count)
	}
	for _, name := range []string{"source.png", "source_1.png"} {
		data, err := os.ReadFile(filepath.Join(e.opts.OutputPath, name))
		if err != nil || string(data) != "keep" {
			t.Errorf("既有文件被覆盖: %s, %v", name, err)
		}
	}
}

func TestSaveResultsRetryAfterDiskFailure(t *testing.T) {
	output := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(output, []byte("blocks directory creation"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: output}
	strat := resultBytesStrategy{data: resultPNG(t, 1)}
	task := &Task{Images: []string{"source.png"}}
	results := []strategy.Result{{URL: "result.png"}}
	if _, err := e.saveResults(context.Background(), task, strat, nil, results); err == nil {
		t.Fatal("目录被文件占用时应返回保存错误")
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	saved, err := e.saveResults(context.Background(), task, strat, nil, results)
	if err != nil || len(saved) != 1 {
		t.Fatalf("磁盘恢复后应能重试保存，got %v, %v", saved, err)
	}
	data, err := os.ReadFile(saved[0])
	if err != nil || string(data) != string(strat.data) {
		t.Fatalf("保存字节不匹配: %q, %v", data, err)
	}
	if saved, err := e.saveResults(context.Background(), task, strat, nil, results); err != nil || len(saved) != 0 {
		t.Fatalf("成功保存后应去重，got %v, %v", saved, err)
	}
}

func TestNameFor(t *testing.T) {
	opts := Options{
		NamingRule:        "原文件名",
		FilePrefix:        "pre_",
		FilePrefixEnabled: true,
		FileSuffix:        "_suf",
		FileSuffixEnabled: true,
	}
	got := opts.NameFor("/tmp/照片 01.png", 1, 3)
	if !strings.HasPrefix(got, "pre_") || !strings.Contains(got, "照片 01") || !strings.Contains(got, "_suf_01.png") {
		t.Errorf("命名: %s", got)
	}
	opts2 := Options{NamingRule: "时间戳"}
	if got := opts2.NameFor("x.png", 1, 1); !strings.HasSuffix(got, ".png") || filepath.Base(got) == "x.png" {
		t.Errorf("时间戳命名: %s", got)
	}
}

func TestTextOnlyResultName(t *testing.T) {
	opts := Options{NamingRule: "原文件名", SaveMode: "按源图片分文件夹", OutputPath: "/out"}
	src := resultSource(&Task{Prompt: "画一只猫\n高清 4K"})
	if got := opts.NameFor(src, 1, 1); got != "画一只猫.png" {
		t.Fatalf("文生图文件名: %s", got)
	}
	if got := opts.ResultDir(src); got != filepath.Join("/out", "画一只猫") {
		t.Fatalf("文生图目录: %s", got)
	}
	src = resultSource(&Task{Prompt: "a/b:c*?"})
	if got := opts.NameFor(src, 1, 1); got != "a_b_c__.png" {
		t.Fatalf("非法字符文件名: %s", got)
	}
	long := strings.Repeat("猫", 50)
	src = resultSource(&Task{Prompt: long})
	if got := opts.NameFor(src, 1, 1); got != strings.Repeat("猫", 40)+".png" {
		t.Fatalf("过长提示词文件名: %s", got)
	}
	if got := opts.NameFor(resultSource(&Task{}), 1, 1); got != "文生图.png" {
		t.Fatalf("空提示词文件名: %s", got)
	}
	if got := resultSource(&Task{Images: []string{"a/cat.png"}, Prompt: "忽略"}); got != "a/cat.png" {
		t.Fatalf("图生图仍用源图命名: %s", got)
	}
}

func TestSaveResultsTextOnlyUsesPromptName(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{OutputPath: dir, NamingRule: "原文件名", SaveMode: "按源图片分文件夹"}
	saved, err := e.saveResults(context.Background(), &Task{Prompt: "画一只猫"}, nil, nil, []strategy.Result{{Data: resultPNG(t, 9)}})
	if err != nil || len(saved) != 1 {
		t.Fatalf("保存文生图结果: %v, %v", saved, err)
	}
	want := filepath.Join(dir, "画一只猫", "画一只猫.png")
	if saved[0] != want {
		t.Fatalf("路径 = %s, want %s", saved[0], want)
	}
}

func TestResultDir(t *testing.T) {
	opts := Options{OutputPath: "/out", SaveMode: "单文件夹汇总"}
	if d := opts.ResultDir("a/b/cat.png"); d != "/out" {
		t.Errorf("单文件夹: %s", d)
	}
	opts.SaveMode = "按源图片分文件夹"
	if d := opts.ResultDir("a/b/cat.png"); d != filepath.Join("/out", "cat") {
		t.Errorf("分文件夹: %s", d)
	}
}

func TestRebuildRetryDropsSuccessAndAccount(t *testing.T) {
	items := []Task{
		{Index: 1, Platform: "豆包", Prompt: "猫", Images: []string{"a.png"}, Status: StatusSuccess, BrowserID: 4, Attempts: 2},
		{Index: 2, Platform: "千问", Prompt: "猫\n后缀", Images: []string{"a.png"}, Status: StatusFailed, Error: "额度", BrowserID: 9, Attempts: 3},
		{Index: 3, Platform: "豆包", Prompt: "只是词", Status: StatusNeedsUser, Attempts: 1},
	}
	got := RebuildRetry(items)
	if len(got) != 2 || got[0].Status != StatusPending || got[0].BrowserID != 0 || got[0].Attempts != 0 || got[0].Prompt != "猫\n后缀" || len(got[0].Images) != 1 || got[0].Images[0] != "a.png" {
		t.Fatalf("first retry = %+v", got)
	}
	if got[1].Prompt != "只是词" || len(got[1].Images) != 0 || got[1].Platform != "豆包" {
		t.Fatalf("second retry = %+v", got[1])
	}
	got[0].Images[0] = "changed"
	if items[1].Images[0] != "a.png" {
		t.Fatal("retry queue aliases the source image path")
	}
}

func TestQueueViewUpdate(t *testing.T) {
	items := []Task{{Index: 1, Status: StatusPending}, {Index: 2, Status: StatusPending}}
	v := NewQueueView(items)
	up := items[0]
	up.Status = StatusSuccess
	v.Update(up)
	snap := v.Snapshot()
	if snap[0].Status != StatusSuccess || snap[1].Status != StatusPending {
		t.Errorf("更新失败: %v", snap)
	}
}
