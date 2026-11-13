package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/logging"
)

func TestConcurrentWashLogsProgressElapsedAndFailures(t *testing.T) {
	if err := logging.Open(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer logging.Close()
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	st.Set(extbridge.SettingExtPort, freePort(t))
	first := addNamed(t, st, "豆包", "甲")
	second := addNamed(t, st, "豆包", "乙")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()
	// A previous run's individual task must not supply the elapsed time of
	// an item bundled into the new run.
	e.starts.Store(2, time.Now().Add(-24*time.Hour))
	e.starts.Store(4, time.Now().Add(-24*time.Hour))
	before := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan int64, 2)
	release := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		finished <- e.RunObserved(ctx, Options{
			Platforms: []string{"豆包"}, Prompts: []string{"洗图"},
			Images: []string{"a.png", "b.png", "c.png", "d.png"},
			Wash:   true, WaitResult: true, Concurrency: 2,
		}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
			started <- task.BrowserID
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			time.Sleep(30 * time.Millisecond)
			logging.Log.Info("扩展任务 %d 已上传 %d 张源图（0.1 秒）", task.Index, len(task.Images))
			if task.BrowserID == second.ID {
				task.submitted = true
				return errors.New("模拟下载错误")
			}
			for i := range task.batch {
				item := &task.batch[i]
				item.BrowserID = task.BrowserID
				item.Results = []string{item.Images[0] + ".out"}
				e.traceTask(item, "save", fmt.Sprintf("#%d 保存 1 张", item.Index), 1, 0, nil)
			}
			return nil
		})
	}()
	var ids []int64
	for len(ids) < 2 {
		select {
		case id := <-started:
			ids = append(ids, id)
		case <-ctx.Done():
			t.Fatal("双账号没有同时开始")
		}
	}
	if ids[0] == ids[1] || (ids[0] != first.ID && ids[1] != first.ID) {
		t.Fatalf("并发账号不对: %v", ids)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 4; index++ {
		value, ok := e.starts.Load(index)
		if !ok || value.(time.Time).Before(before) {
			t.Fatalf("第 %d 张沿用了旧的开始时间: %v", index, value)
		}
	}
	data, err := os.ReadFile(logging.Path())
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"任务开始：共 4 个", "[豆包/甲:", "[豆包/乙:", "已上传 2 张源图", "模拟下载错误", "任务结束", "run_id="} {
		if !strings.Contains(text, want) {
			t.Errorf("日志缺少 %q:\n%s", want, text)
		}
	}
	for done := 1; done <= 4; done++ {
		if want := fmt.Sprintf("进度 %d/4", done); !strings.Contains(text, want) {
			t.Errorf("日志缺少 %q:\n%s", want, text)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "#2 保存 1 张") && strings.Contains(line, "task_id=2") && strings.Contains(line, "ms") {
			return
		}
	}
	t.Fatalf("批内第二张缺少本轮耗时:\n%s", text)
}
