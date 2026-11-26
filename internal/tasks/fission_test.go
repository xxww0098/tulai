package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/dedup"
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
	account, err := st.AddBrowser("豆包", "测试账号", "")
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(st, nil, nil, t.TempDir())
	e.opts = Options{Fission: true, WaitResult: true}
	task := &Task{Platform: "豆包", BrowserID: account.ID, Images: []string{"mother.png"}}
	if err := e.attempt(context.Background(), task); err == nil || !task.noUse {
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

func TestFissionTextOnlyQueueAndEngineValidation(t *testing.T) {
	ps := prompts.FissionTextPrompts("赛博朋克机械猫")
	opts := Options{
		Fission:         true,
		FissionTextOnly: true,
		Platforms:       []string{"千问"},
		Prompts:         ps,
	}
	queue := BuildQueue(opts, map[string][]int64{"千问": {21, 22}})
	if len(queue) != 4 {
		t.Fatalf("text-only fission queue len = %d, want 4", len(queue))
	}
	for i, task := range queue {
		if len(task.Images) != 0 || !strings.Contains(task.Prompt, "赛博朋克机械猫") {
			t.Fatalf("text-only fission task #%d invalid: %+v", i+1, task)
		}
	}
	for _, invalid := range []Options{
		{Fission: true, FissionTextOnly: true, Platforms: opts.Platforms, Images: []string{"mother.png"}, Prompts: ps},
		{Fission: true, FissionTextOnly: true, Platforms: opts.Platforms, Prompts: ps[:3]},
		{Fission: true, FissionTextOnly: true, Platforms: []string{"豆包", "千问"}, Prompts: ps},
	} {
		if got := BuildQueue(invalid, nil); len(got) != 0 {
			t.Fatalf("invalid text-only fission should not build queue: %+v", got)
		}
	}
}

func TestPickBestFissionCandidatePrefersDiverseOverSimilar(t *testing.T) {
	prior := []uint64{0}
	// Candidate 0 is too similar (10 bits diff < 26); Candidate 1 passes (30 bits diff >= 26)
	cands := []fissionCandidate{
		{index: 0, sig: dedup.Signature{Hash: (uint64(1) << 10) - 1}},
		{index: 1, sig: dedup.Signature{Hash: (uint64(1) << 30) - 1}},
	}
	if got := pickBestFissionCandidate(cands, prior, nil); got != 1 {
		t.Fatalf("pickBestFissionCandidate = %d, want 1", got)
	}
	// Without prior hashes, prefer candidate further from mother image
	mother := &dedup.Signature{Hash: 0}
	if got := pickBestFissionCandidate(cands, nil, mother); got != 1 {
		t.Fatalf("pickBestFissionCandidate (mother tie-break) = %d, want 1", got)
	}
}

func TestFissionSaveResultsMultiCandidateAvoidsCollision(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Fission: true, OutputPath: dir}

	firstBytes := resultPNG(t, 1)
	firstPath := filepath.Join(dir, "prior.png")
	if err := os.WriteFile(firstPath, firstBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	firstSig, err := dedup.Hash(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	e.fissionHashes = []uint64{firstSig.Hash}

	var diffBytes []byte
	for seed := uint8(2); seed < 60; seed++ {
		candBytes := resultPNG(t, seed)
		p := filepath.Join(dir, fmt.Sprintf("probe-%d.png", seed))
		if err := os.WriteFile(p, candBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		sig, err := dedup.Hash(p)
		if err == nil && dedup.Hamming(firstSig.Hash, sig.Hash) >= 26 {
			diffBytes = candBytes
			break
		}
	}
	if diffBytes == nil {
		t.Fatal("无法构造与先验图结构差异足够的候选图")
	}

	task := &Task{Index: 2, Platform: "千问", Images: []string{"mother.png"}}
	saved, err := e.saveResults(context.Background(), task, nil, nil, []strategy.Result{
		{Data: firstBytes}, // collides with prior
		{Data: diffBytes},  // distinct candidate!
	})
	if err != nil || len(saved) != 1 {
		t.Fatalf("saveResults = (%v, %v), want 1 saved file", saved, err)
	}
	reviewed := ReviewFissionResults([]Task{
		{Index: 1, Platform: "千问", Prompt: "方向一", Status: StatusSuccess, Results: []string{firstPath}},
		{Index: 2, Platform: "千问", Prompt: "方向二", Status: StatusSuccess, Results: saved},
	}, nil)
	if reviewed[1].Status != StatusSuccess {
		t.Fatalf("多候选择优后仍触发裂变撞图: %+v", reviewed[1])
	}
}

func TestPickBestFissionCandidatePrefersNonFlatOverFlatImage(t *testing.T) {
	// Even if a flat image (Hash=0) has larger Hamming distance to priorHashes than a valid image,
	// pickBestFissionCandidate must never choose a blank/flat candidate over a valid detailed image.
	prior := []uint64{^uint64(0)}
	cands := []fissionCandidate{
		{index: 0, sig: dedup.Signature{Hash: 0, Flat: true}},
		{index: 1, sig: dedup.Signature{Hash: (uint64(1) << 30) - 1, Flat: false}},
	}
	if got := pickBestFissionCandidate(cands, prior, nil); got != 1 {
		t.Fatalf("pickBestFissionCandidate chose flat image %d, want valid candidate 1", got)
	}
}

func TestFissionConcurrentSaveResultsReservesDistinctCandidatesAtomically(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Fission: true, OutputPath: dir}

	bytesA := resultPNG(t, 1)
	pathA := filepath.Join(dir, "probe-a.png")
	if err := os.WriteFile(pathA, bytesA, 0o600); err != nil {
		t.Fatal(err)
	}
	sigA, err := dedup.Hash(pathA)
	if err != nil {
		t.Fatal(err)
	}
	var bytesB []byte
	for seed := uint8(2); seed < 80; seed++ {
		cand := resultPNG(t, seed)
		p := filepath.Join(dir, fmt.Sprintf("probe-%d.png", seed))
		if err := os.WriteFile(p, cand, 0o600); err != nil {
			t.Fatal(err)
		}
		sig, err := dedup.Hash(p)
		if err == nil && !sig.Flat && dedup.Hamming(sigA.Hash, sig.Hash) >= 26 {
			bytesB = cand
			break
		}
	}
	if bytesB == nil {
		t.Fatal("无法构造结构差异足够的第二候选图")
	}

	// Two concurrent fission directions receive the exact same 2 candidates [A, B].
	// Atomic selection+reservation under fissionMu guarantees they pick distinct outputs.
	t1 := &Task{Index: 1, Platform: "千问", Prompt: "方向一", Images: []string{"mother.png"}}
	t2 := &Task{Index: 2, Platform: "千问", Prompt: "方向二", Images: []string{"mother.png"}}
	var saved1, saved2 []string
	var err1, err2 error
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		saved1, err1 = e.saveResults(context.Background(), t1, nil, nil, []strategy.Result{{Data: bytesA}, {Data: bytesB}})
	}()
	go func() {
		defer wg.Done()
		<-start
		saved2, err2 = e.saveResults(context.Background(), t2, nil, nil, []strategy.Result{{Data: bytesA}, {Data: bytesB}})
	}()
	close(start)
	wg.Wait()
	if err1 != nil || err2 != nil || len(saved1) != 1 || len(saved2) != 1 {
		t.Fatalf("concurrent saveResults failed: (%v, %v) (%v, %v)", saved1, err1, saved2, err2)
	}
	reviewed := ReviewFissionResults([]Task{
		{Index: 1, Platform: "千问", Prompt: "方向一", Status: StatusSuccess, Results: saved1},
		{Index: 2, Platform: "千问", Prompt: "方向二", Status: StatusSuccess, Results: saved2},
	}, nil)
	if reviewed[0].Status != StatusSuccess || reviewed[1].Status != StatusSuccess {
		t.Fatalf("并发裂变多候选择优仍发生撞图: %+v", reviewed)
	}
}

func TestAllModesAndPlatformsProtectDailyCountOnUnsentFailure(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		opts     Options
		images   []string
	}{
		{"doubao-free-text", "豆包", Options{WaitResult: true}, nil},
		{"doubao-free-img", "豆包", Options{WaitResult: true}, []string{"a.png"}},
		{"doubao-send-only", "豆包", Options{WaitResult: false}, nil},
		{"qianwen-free-text", "千问", Options{WaitResult: true}, nil},
		{"qianwen-wash", "千问", Options{Wash: true, WaitResult: true}, []string{"a.png"}},
		{"qianwen-fission", "千问", Options{Fission: true, WaitResult: true}, []string{"m.png"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := openTaskStore(t)
			acct, err := st.AddBrowser(tc.platform, "测试", "")
			if err != nil {
				t.Fatal(err)
			}
			e := NewEngine(st, nil, nil, t.TempDir())
			e.opts = tc.opts
			task := &Task{Platform: tc.platform, BrowserID: acct.ID, Prompt: "测试提示词", Images: tc.images}
			if err := e.attempt(context.Background(), task); err == nil || !task.noUse {
				t.Fatalf("%s: expected pre-send error with noUse=true, got err=%v noUse=%v", tc.name, err, task.noUse)
			}
			task.Attempts, task.ranOn, task.Status = 1, acct.ID, StatusFailed
			e.noteUse(*task, 0)
			for _, row := range st.Browsers() {
				if row.ID == acct.ID {
					_, failed := row.ResultsToday(time.Now())
					if row.UsedToday(time.Now()) != 0 || failed != 1 {
						t.Fatalf("%s: unsent failure counted against UsedToday=%d failed=%d", tc.name, row.UsedToday(time.Now()), failed)
					}
					return
				}
			}
			t.Fatal("account missing")
		})
	}

	// If attempt 1 truly sent (everSent=true) and retry attempt 2 fails before send, UsedToday must still count 1.
	t.Run("everSent-keeps-count-on-retry-presend-fail", func(t *testing.T) {
		st := openTaskStore(t)
		acct, err := st.AddBrowser("千问", "测试", "")
		if err != nil {
			t.Fatal(err)
		}
		e := NewEngine(st, nil, nil, t.TempDir())
		e.opts = Options{WaitResult: true}
		task := &Task{Platform: "千问", BrowserID: acct.ID, Prompt: "测试", everSent: true}
		if err := e.attempt(context.Background(), task); err == nil || task.noUse {
			t.Fatalf("expected error with noUse=false when everSent=true, got err=%v noUse=%v", err, task.noUse)
		}
		task.Attempts, task.ranOn, task.Status = 2, acct.ID, StatusFailed
		e.noteUse(*task, 0)
		for _, row := range st.Browsers() {
			if row.ID == acct.ID {
				if row.UsedToday(time.Now()) != 1 {
					t.Fatalf("UsedToday=%d, want 1", row.UsedToday(time.Now()))
				}
				return
			}
		}
		t.Fatal("account missing")
	})

	// Unsupported platform early return must also keep noUse=true so UsedToday is never charged.
	t.Run("unsupported-platform-presend-no-use", func(t *testing.T) {
		st := openTaskStore(t)
		acct, err := st.AddBrowser("豆包", "测试", "")
		if err != nil {
			t.Fatal(err)
		}
		e := NewEngine(st, nil, nil, t.TempDir())
		task := &Task{Platform: "未知平台", BrowserID: acct.ID, Prompt: "测试"}
		if err := e.attempt(context.Background(), task); err == nil || !task.noUse {
			t.Fatalf("unsupported platform must fail with noUse=true: err=%v noUse=%v", err, task.noUse)
		}
	})

	// Engine.RunObserved with nil onTask/onProgress callbacks must never panic.
	t.Run("nil-progress-callback-safe", func(t *testing.T) {
		st := openTaskStore(t)
		if _, err := st.AddBrowser("豆包", "测试", ""); err != nil {
			t.Fatal(err)
		}
		e := NewEngine(st, nil, nil, t.TempDir())
		err := e.RunObserved(context.Background(), Options{
			Platforms: []string{"豆包"},
			Prompts:   []string{"测试"},
		}, nil, nil, func(context.Context, *Task) error { return nil })
		if err != nil {
			t.Fatalf("RunObserved with nil callbacks failed: %v", err)
		}
	})
}
