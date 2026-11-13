package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/dedup"
	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestFissionQueueUsesOneMotherAndFourDistinctDirections(t *testing.T) {
	opts := Options{Fission: true, Platforms: []string{"豆包"}, Images: []string{"mother.png"}, Prompts: prompts.FissionPrompts("保留花纹")}
	queue := BuildQueue(opts, map[string][]int64{"豆包": {11, 12}})
	if len(queue) != 4 {
		t.Fatalf("裂变任务数 = %d", len(queue))
	}
	seen := map[string]bool{}
	for i, task := range queue {
		if len(task.Images) != 1 || task.Images[0] != "mother.png" || !strings.Contains(task.Prompt, "保留花纹") ||
			!strings.Contains(task.Prompt, "不超过 60%") || !strings.Contains(task.Prompt, fmt.Sprintf("第 %d/4", i+1)) || seen[task.Prompt] {
			t.Fatalf("第 %d 个方向不正确: %+v", i+1, task)
		}
		seen[task.Prompt] = true
	}
	for _, invalid := range []Options{
		{Fission: true, Platforms: opts.Platforms, Prompts: opts.Prompts},
		{Fission: true, Platforms: opts.Platforms, Images: []string{"a", "b"}, Prompts: opts.Prompts},
		{Fission: true, Platforms: []string{"豆包", "千问"}, Images: opts.Images, Prompts: opts.Prompts},
		{Fission: true, Platforms: opts.Platforms, Images: opts.Images, Prompts: opts.Prompts[:3]},
	} {
		if got := BuildQueue(invalid, nil); len(got) != 0 {
			t.Fatalf("错误输入不能创建队列: %+v", got)
		}
	}
}

func TestFissionSavesOnlyOneResultPerDirection(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Fission: true, OutputPath: dir}
	task := &Task{Images: []string{"mother.png"}}
	saved, err := e.saveResults(context.Background(), task, nil, nil, []strategy.Result{{Data: resultPNG(t, 1)}, {Data: resultPNG(t, 2)}})
	if err != nil || len(saved) != 1 || filepath.Base(saved[0]) != "mother_01.png" || filepath.Dir(saved[0]) != filepath.Join(dir, "裂变图片", time.Now().Format("2006-01-02")) {
		t.Fatalf("裂变结果 = %v, %v", saved, err)
	}
	task.Results = saved
	more, err := e.saveResults(context.Background(), task, nil, nil, []strategy.Result{{Data: resultPNG(t, 3)}})
	if err != nil || len(more) != 0 || !task.matchedSavedResult {
		t.Fatalf("同一方向不应重复保存: %v, %v", more, err)
	}
}

func TestFissionDispatchesFourSeparateTasks(t *testing.T) {
	st := openRotateStore(t)
	addNamed(t, st, "豆包", "甲")
	e := NewEngine(st, nil, nil, "")
	defer e.CloseSessions()
	opts := Options{Fission: true, Platforms: []string{"豆包"}, Images: []string{"mother.png"},
		Prompts: prompts.FissionPrompts(""), Concurrency: 1, WaitResult: true}
	var progress Progress
	err := e.RunObserved(context.Background(), opts, func(Task) {}, func(p Progress) { progress = p }, func(_ context.Context, task *Task) error {
		if len(task.Images) != 1 || task.Images[0] != "mother.png" || len(task.batch) != 0 {
			return fmt.Errorf("裂变请求必须单独携带母图: %+v", task.Images)
		}
		task.Results = []string{fmt.Sprintf("result-%d.png", task.Index)}
		return nil
	})
	if err != nil || progress.Done != 4 || progress.Total != 4 {
		t.Fatalf("裂变调度失败: progress=%+v err=%v", progress, err)
	}
	for _, task := range e.View().Snapshot() {
		if task.Status != StatusSuccess || len(task.Results) != 1 {
			t.Fatalf("裂变任务未成功: %+v", task)
		}
	}
}

func TestFissionSimilarityThreshold(t *testing.T) {
	if !fissionTooSimilar(0, (uint64(1)<<25)-1) || fissionTooSimilar(0, (uint64(1)<<26)-1) {
		t.Fatal("63 位 pHash 需至少相差 26 位")
	}
}

func TestFissionDoesNotCountAnUnsentAttempt(t *testing.T) {
	st := openTaskStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	account, err := st.AddBrowser("豆包", "测试账号", "")
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(st, nil, nil, t.TempDir())
	e.opts = Options{Fission: true, WaitResult: true}
	task := &Task{Platform: "豆包", BrowserID: account.ID, Images: []string{"mother.png"}}
	if err := e.attemptDoubaoExtension(context.Background(), task); err == nil || !task.noUse {
		t.Fatalf("浏览器启动前失败不能计入发送次数: err=%v noUse=%v", err, task.noUse)
	}
	task.Attempts, task.ranOn, task.Status = 1, account.ID, StatusFailed
	e.noteUse(*task, 0)
	for _, row := range st.Browsers() {
		if row.ID == account.ID {
			_, failed := row.ResultsToday(time.Now())
			if row.UsedToday(time.Now()) != 0 || failed != 1 {
				t.Fatalf("未发送的失败记录不应消耗次数: %+v", row)
			}
			return
		}
	}
	t.Fatal("测试账号丢失")
}

func TestFissionReviewRejectsSimilarAndUnreadableResultsAndProtectsPriorSuccess(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	if err := os.WriteFile(first, resultPNG(t, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	firstSig, err := dedup.Hash(first)
	if err != nil {
		t.Fatal(err)
	}
	var different string
	for seed := uint8(2); seed < 40; seed++ {
		path := filepath.Join(dir, fmt.Sprintf("%d.png", seed))
		if err := os.WriteFile(path, resultPNG(t, seed), 0o600); err != nil {
			t.Fatal(err)
		}
		sig, err := dedup.Hash(path)
		if err != nil {
			t.Fatal(err)
		}
		if dedup.Hamming(firstSig.Hash, sig.Hash) >= 26 {
			different = path
			break
		}
	}
	if different == "" {
		t.Fatal("无法构造结构不同的图片")
	}
	items := []Task{
		{Index: 1, Platform: "豆包", Prompt: "一", Status: StatusSuccess, Results: []string{first}},
		{Index: 2, Platform: "豆包", Prompt: "二", Status: StatusSuccess, Results: []string{first}},
		{Index: 3, Platform: "豆包", Prompt: "三", Status: StatusSuccess, Results: []string{different}},
		{Index: 4, Platform: "豆包", Prompt: "四", Status: StatusSuccess, Results: []string{filepath.Join(dir, "missing.png")}},
	}
	got := ReviewFissionResults(items, nil)
	if got[0].Status != StatusSuccess || got[1].Status != StatusNeedsReview || !strings.Contains(got[1].Error, "100.0%") || got[2].Status != StatusSuccess || got[3].Status != StatusNeedsReview {
		t.Fatalf("检测结果错误: %+v", got)
	}
	// 第一个方向重试后撞上此前已经通过的第二个，必须保留第二个。
	retry := []Task{items[0]}
	got = ReviewFissionResults(items[:2], retry)
	if got[0].Status != StatusNeedsReview || got[1].Status != StatusSuccess {
		t.Fatalf("重试不应覆盖已通过的结果: %+v", got)
	}
}
