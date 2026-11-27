package tasks

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

type spyJimengStrategy struct {
	targetCount      int
	targetResolution string
	uploadCalls      [][]string
	sentPrompts      []string
	sentCounts       []int
}

func (s *spyJimengStrategy) Platform() string { return "即梦" }
func (s *spyJimengStrategy) HomeURL() string {
	return "https://jimeng.jianying.com/ai-tool/generate/?type=image"
}
func (s *spyJimengStrategy) InstallHook(*cdp.Session) error  { return nil }
func (s *spyJimengStrategy) CleanComposer(*cdp.Session) error { return nil }
func (s *spyJimengStrategy) MarkTask(*cdp.Session) error      { return nil }
func (s *spyJimengStrategy) EnsureImageMode(*cdp.Session) error {
	return nil
}
func (s *spyJimengStrategy) SetOutputSize(_ *cdp.Session, _, resolution string) (bool, string) {
	if resolution != "" {
		s.targetResolution = resolution
	}
	return true, ""
}
func (s *spyJimengStrategy) SetImageCount(n int) {
	if n < 1 {
		n = 1
	}
	if n > strategy.JimengMaxUploadImages {
		n = strategy.JimengMaxUploadImages
	}
	s.targetCount = n
}
func (s *spyJimengStrategy) ImageCount() int {
	if s.targetCount >= 1 && s.targetCount <= strategy.JimengMaxUploadImages {
		return s.targetCount
	}
	return strategy.JimengDefaultCount
}
func (s *spyJimengStrategy) UploadFiles(_ *cdp.Session, files []string) error {
	s.uploadCalls = append(s.uploadCalls, append([]string(nil), files...))
	return nil
}
func (s *spyJimengStrategy) SendPrompt(_ *cdp.Session, prompt string) error {
	s.sentPrompts = append(s.sentPrompts, prompt)
	s.sentCounts = append(s.sentCounts, s.ImageCount())
	return nil
}
func (s *spyJimengStrategy) WaitForResult(*cdp.Session, time.Duration) ([]strategy.Result, error) {
	return nil, nil
}
func (s *spyJimengStrategy) FetchResultBytes(*cdp.Session, string) ([]byte, error) { return nil, nil }
func (s *spyJimengStrategy) PageState(*cdp.Session) map[string]any                 { return nil }

func TestSubmitOnceAnalyzesJimengCreditsForGenAndWash(t *testing.T) {
	t.Run("text-to-image gen caps 6 to 5 when live balance is 5", func(t *testing.T) {
		st := openRotateStore(t)
		acc := addNamed(t, st, "即梦", "即梦余5分")
		e := NewEngine(st, nil, nil, "")
		e.opts = Options{Platforms: []string{"即梦"}, WaitResult: true}
		e.jimengProbeFn = func(int64, *cdp.Session) (cdp.JimengStatus, error) {
			return cdp.JimengStatus{
				OK:               true,
				HasCredits:       true,
				CreditsRemain:    5,
				CreditCost:       1,
				PageCreditCost:   1,
				Model:            cdp.JimengTargetModel,
				Resolution:       "2K",
				CostModelMatched: true,
			}, nil
		}

		strat := &spyJimengStrategy{targetCount: strategy.JimengDefaultCount}
		task := &Task{Index: 1, Platform: "即梦", BrowserID: acc.ID, Prompt: "赛博朋克猫"}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce err = %v", err)
		}
		if !task.submitted || task.jimengCount != 5 || strat.ImageCount() != 5 {
			t.Fatalf("expected gen count adjusted to 5, got submitted=%v jimengCount=%d stratCount=%d", task.submitted, task.jimengCount, strat.ImageCount())
		}
		if !slices.Equal(strat.sentCounts, []int{5}) || len(strat.uploadCalls) != 0 {
			t.Fatalf("unexpected send/upload: sentCounts=%v uploads=%v", strat.sentCounts, strat.uploadCalls)
		}
	})

	t.Run("image-to-image gen caps 6 to 2 at 4K (2/img) when live balance is 5", func(t *testing.T) {
		st := openRotateStore(t)
		acc := addNamed(t, st, "即梦", "即梦4K号")
		// DB 缓存还是满额 60 分、2K 1分/张，必须以发送前实时 4K 探测结果为准。
		_ = st.UpdateBrowserQuota(acc.ID, store.AccountQuota{
			CreditsRemain: 60,
			CreditsTotal:  60,
			CreditCost:    1,
			SyncedAt:      time.Now().Unix(),
		})
		e := NewEngine(st, nil, nil, "")
		e.opts = Options{
			Platforms:  []string{"即梦"},
			OutputSize: OutputSize{Resolution: "4K"},
			WaitResult: true,
		}
		e.jimengProbeFn = func(int64, *cdp.Session) (cdp.JimengStatus, error) {
			return cdp.JimengStatus{
				OK:             true,
				HasCredits:     true,
				CreditsRemain:  5,
				PageCreditCost: 2,
				Model:          cdp.JimengTargetModel,
				Resolution:     "4K",
			}, nil
		}

		strat := &spyJimengStrategy{targetCount: strategy.JimengDefaultCount}
		task := &Task{Index: 2, Platform: "即梦", BrowserID: acc.ID, Prompt: "转水彩风格", Images: []string{"ref.png"}}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce err = %v", err)
		}
		if !task.submitted || task.jimengCount != 2 || !slices.Equal(strat.sentCounts, []int{2}) {
			t.Fatalf("expected 4K image-to-image gen count 2, got jimengCount=%d sentCounts=%v", task.jimengCount, strat.sentCounts)
		}
		if len(strat.uploadCalls) != 1 || !slices.Equal(strat.uploadCalls[0], []string{"ref.png"}) {
			t.Fatalf("expected 1 reference image uploaded, got %v", strat.uploadCalls)
		}
		// 同步 4K 任务余额时，账号默认 2K 单价（1分/张）不应被 4K 的 2分/张 污染。
		row := browserByID(t, st, acc.ID)
		if row.CreditsRemain != 5 || row.CreditCost != 1 {
			t.Fatalf("DB quota after 4K sync = remain:%d cost:%d, want remain:5 cost:1", row.CreditsRemain, row.CreditCost)
		}
	})

	t.Run("gen restores count from 5 back to 6 when live balance refreshed", func(t *testing.T) {
		st := openRotateStore(t)
		acc := addNamed(t, st, "即梦", "即梦已刷新")
		_ = st.UpdateBrowserQuota(acc.ID, store.AccountQuota{
			CreditsRemain: 5,
			CreditsTotal:  60,
			CreditCost:    1,
			SyncedAt:      time.Now().Unix(),
		})
		e := NewEngine(st, nil, nil, "")
		e.opts = Options{Platforms: []string{"即梦"}, WaitResult: true}
		e.jimengProbeFn = func(int64, *cdp.Session) (cdp.JimengStatus, error) {
			return cdp.JimengStatus{
				OK:               true,
				HasCredits:       true,
				CreditsRemain:    60,
				CreditCost:       1,
				PageCreditCost:   1,
				Model:            cdp.JimengTargetModel,
				Resolution:       "2K",
				CostModelMatched: true,
			}, nil
		}

		strat := &spyJimengStrategy{targetCount: 5}
		task := &Task{Index: 3, Platform: "即梦", BrowserID: acc.ID, Prompt: "雪山日出"}
		if err := e.submitOnce(task, strat, nil); err != nil {
			t.Fatalf("submitOnce err = %v", err)
		}
		if !slices.Equal(strat.sentCounts, []int{6}) || task.jimengCount != 6 {
			t.Fatalf("expected count restored to 6 after live refresh, got sentCounts=%v jimengCount=%d", strat.sentCounts, task.jimengCount)
		}
	})

	t.Run("gen blocks before upload or send when balance cannot afford 1 image", func(t *testing.T) {
		st := openRotateStore(t)
		acc := addNamed(t, st, "即梦", "即梦余1分4K")
		e := NewEngine(st, nil, nil, "")
		e.opts = Options{
			Platforms:  []string{"即梦"},
			OutputSize: OutputSize{Resolution: "4K"},
			WaitResult: true,
		}
		e.jimengProbeFn = func(int64, *cdp.Session) (cdp.JimengStatus, error) {
			return cdp.JimengStatus{
				OK:             true,
				HasCredits:     true,
				CreditsRemain:  1,
				PageCreditCost: 2,
				Model:          cdp.JimengTargetModel,
				Resolution:     "4K",
			}, nil
		}

		strat := &spyJimengStrategy{targetCount: 6}
		task := &Task{Index: 4, Platform: "即梦", BrowserID: acc.ID, Prompt: "山海经", Images: []string{"ref.png"}}
		err := e.submitOnce(task, strat, nil)
		if !errors.Is(err, strategy.ErrAccountQuota) {
			t.Fatalf("expected ErrAccountQuota, got %v", err)
		}
		if task.submitted || len(strat.uploadCalls) != 0 || len(strat.sentPrompts) != 0 {
			t.Fatalf("must not upload or send when insufficient credits: submitted=%v uploads=%v prompts=%v", task.submitted, strat.uploadCalls, strat.sentPrompts)
		}
	})

	t.Run("wash batch shrinks before upload when live balance is below batch size", func(t *testing.T) {
		st := openRotateStore(t)
		acc := addNamed(t, st, "即梦", "即梦洗图余4分")
		e := NewEngine(st, nil, nil, "")
		e.opts = Options{Platforms: []string{"即梦"}, Wash: true, WaitResult: true}
		e.jimengProbeFn = func(int64, *cdp.Session) (cdp.JimengStatus, error) {
			return cdp.JimengStatus{
				OK:               true,
				HasCredits:       true,
				CreditsRemain:    4,
				CreditCost:       1,
				PageCreditCost:   1,
				Model:            cdp.JimengTargetModel,
				Resolution:       "2K",
				CostModelMatched: true,
			}, nil
		}

		imgs := numberedWashSources(6)
		batch := make([]Task, 6)
		for i, img := range imgs {
			batch[i] = Task{Index: i + 1, Platform: "即梦", BrowserID: acc.ID, Prompt: "洗图", Images: []string{img}}
		}
		task := &Task{Index: 1, Platform: "即梦", BrowserID: acc.ID, Prompt: "洗图", Images: imgs, batch: batch}
		strat := &spyJimengStrategy{targetCount: 6}

		err := e.submitOnce(task, strat, nil)
		if !errors.Is(err, errJimengBatchShrink) {
			t.Fatalf("expected errJimengBatchShrink, got %v", err)
		}
		if task.submitted || len(strat.uploadCalls) != 0 || len(strat.sentPrompts) != 0 {
			t.Fatalf("wash shrink must happen before upload/send: submitted=%v uploads=%v prompts=%v", task.submitted, strat.uploadCalls, strat.sentPrompts)
		}
		if task.jimengShrinkCap != 4 || task.jimengShrinkBrowser != acc.ID {
			t.Fatalf("expected shrink cap=4 for browser %d, got cap=%d browser=%d", acc.ID, task.jimengShrinkCap, task.jimengShrinkBrowser)
		}
	})

	t.Run("fission blocks before upload when live balance is 0 and proceeds when 1", func(t *testing.T) {
		st := openRotateStore(t)
		acc := addNamed(t, st, "即梦", "即梦裂变号")
		e := NewEngine(st, nil, nil, "")
		e.opts = Options{Platforms: []string{"即梦"}, Fission: true, WaitResult: true}

		remain := 0
		e.jimengProbeFn = func(int64, *cdp.Session) (cdp.JimengStatus, error) {
			return cdp.JimengStatus{
				OK:               true,
				HasCredits:       true,
				CreditsRemain:    remain,
				CreditCost:       1,
				PageCreditCost:   1,
				Model:            cdp.JimengTargetModel,
				Resolution:       "2K",
				CostModelMatched: true,
			}, nil
		}

		strat := &spyJimengStrategy{targetCount: 1}
		task0 := &Task{Index: 1, Platform: "即梦", BrowserID: acc.ID, Prompt: "方向1", Images: []string{"mother.png"}}
		if err := e.submitOnce(task0, strat, nil); !errors.Is(err, strategy.ErrAccountQuota) {
			t.Fatalf("expected ErrAccountQuota at 0 credits, got %v", err)
		}
		if len(strat.uploadCalls) != 0 || len(strat.sentPrompts) != 0 {
			t.Fatalf("must not upload/send at 0 credits")
		}

		remain = 1
		task1 := &Task{Index: 2, Platform: "即梦", BrowserID: acc.ID, Prompt: "方向2", Images: []string{"mother.png"}}
		if err := e.submitOnce(task1, strat, nil); err != nil {
			t.Fatalf("expected success at 1 credit, got %v", err)
		}
		if !slices.Equal(strat.sentCounts, []int{1}) || len(strat.uploadCalls) != 1 {
			t.Fatalf("expected fission count 1 and 1 upload, got sentCounts=%v uploads=%v", strat.sentCounts, strat.uploadCalls)
		}
	})
}

func TestJimengGenDynamicCreditAnalysisAndCleanStop(t *testing.T) {
	t.Run("single account with 11 credits runs 6 then 5 then cleanly stops remaining task", func(t *testing.T) {
		st := openRotateStore(t)
		acc := addNamed(t, st, "即梦", "即梦11分")
		st.ConfirmLogin(acc.ID)
		e := NewEngine(st, nil, nil, "")
		defer e.CloseSessions()
		e.gap = func(time.Duration) time.Duration { return 0 }

		var mu sync.Mutex
		liveCredits := 11
		var counts []int
		e.jimengProbeFn = func(int64, *cdp.Session) (cdp.JimengStatus, error) {
			mu.Lock()
			cur := liveCredits
			mu.Unlock()
			return cdp.JimengStatus{
				OK:               true,
				HasCredits:       true,
				CreditsRemain:    cur,
				CreditCost:       1,
				PageCreditCost:   1,
				Model:            cdp.JimengTargetModel,
				Resolution:       "2K",
				CostModelMatched: true,
				CreditsResetAt:   nextLocalMidnight(time.Now()).Unix(),
			}, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := e.RunObserved(ctx, Options{
			Platforms:   []string{"即梦"},
			Prompts:     []string{"提示词1", "提示词2", "提示词3"},
			WaitResult:  true,
			Concurrency: 1,
			DailyCap:    100,
		}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
			strat := &spyJimengStrategy{targetCount: strategy.JimengDefaultCount}
			if err := e.submitOnce(task, strat, nil); err != nil {
				return err
			}
			used := strat.ImageCount()
			mu.Lock()
			counts = append(counts, used)
			liveCredits = max(liveCredits-used, 0)
			cur := liveCredits
			mu.Unlock()
			// 模拟出图完成后同步最新余额（第二单结束后余额归零）。
			e.persistJimengStatus(task.BrowserID, cdp.JimengStatus{
				OK:               true,
				HasCredits:       true,
				CreditsRemain:    cur,
				CreditCost:       1,
				PageCreditCost:   1,
				Model:            cdp.JimengTargetModel,
				Resolution:       "2K",
				CostModelMatched: true,
				CreditsResetAt:   nextLocalMidnight(time.Now()).Unix(),
			}, time.Now())
			task.Results = []string{"out.png"}
			return nil
		})
		if err != nil {
			t.Fatalf("RunObserved err=%v", err)
		}
		if !slices.Equal(counts, []int{6, 5}) {
			t.Fatalf("expected gen counts [6 5], got %v", counts)
		}
		snap := e.View().Snapshot()
		if len(snap) != 3 {
			t.Fatalf("snap len = %d", len(snap))
		}
		if snap[0].Status != StatusSuccess || snap[1].Status != StatusSuccess {
			t.Fatalf("first two tasks should succeed: %+v", snap)
		}
		if snap[2].Status != StatusSkipped || !snap[2].Undispatched || !strings.Contains(snap[2].Error, "积分已用尽") {
			t.Fatalf("third task should stop cleanly instead of hanging until midnight: %+v", snap[2])
		}
	})

	t.Run("wash shrink to 1 image and live 3/img pricing avoid infinite shrink loop", func(t *testing.T) {
		st := openRotateStore(t)
		low := addNamed(t, st, "即梦", "即梦余5分")
		full := addNamed(t, st, "即梦", "即梦60分")
		st.ConfirmLogin(low.ID)
		st.ConfirmLogin(full.ID)
		_ = st.UpdateBrowserQuota(full.ID, store.AccountQuota{
			CreditsRemain: 60,
			CreditsTotal:  60,
			CreditCost:    1,
			SyncedAt:      time.Now().Unix(),
		})
		e := NewEngine(st, nil, nil, "")
		defer e.CloseSessions()
		e.gap = func(time.Duration) time.Duration { return 0 }

		var mu sync.Mutex
		balances := map[int64]int{low.ID: 5, full.ID: 60}
		e.jimengProbeFn = func(id int64, _ *cdp.Session) (cdp.JimengStatus, error) {
			mu.Lock()
			rem := balances[id]
			mu.Unlock()
			// 模拟即梦网页 4K 实际单价上涨至 3/张：5 积分只能发 1 张（而非默认估算的 5/2=2 张）。
			return cdp.JimengStatus{
				OK:             true,
				HasCredits:     true,
				CreditsRemain:  rem,
				PageCreditCost: 3,
				Model:          cdp.JimengTargetModel,
				Resolution:     "4K",
			}, nil
		}

		type sentBatch struct {
			browserID int64
			count     int
		}
		var sent []sentBatch
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := e.RunObserved(ctx, Options{
			Platforms:   []string{"即梦"},
			Prompts:     []string{"洗图"},
			Images:      numberedWashSources(7),
			Wash:        true,
			OutputSize:  OutputSize{Resolution: "4K"},
			WaitResult:  true,
			Concurrency: 1,
			DailyCap:    100,
		}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
			strat := &spyJimengStrategy{targetCount: len(task.Images)}
			if err := e.submitOnce(task, strat, nil); err != nil {
				return err
			}
			mu.Lock()
			sent = append(sent, sentBatch{browserID: task.BrowserID, count: len(task.Images)})
			balances[task.BrowserID] = max(balances[task.BrowserID]-len(task.Images)*3, 0)
			rem := balances[task.BrowserID]
			mu.Unlock()
			e.persistJimengStatus(task.BrowserID, cdp.JimengStatus{
				OK:             true,
				HasCredits:     true,
				CreditsRemain:  rem,
				PageCreditCost: 3,
				Model:          cdp.JimengTargetModel,
				Resolution:     "4K",
			}, time.Now())
			if len(task.batch) > 0 {
				for i := range task.batch {
					task.batch[i].Results = []string{task.batch[i].Images[0] + ".out"}
				}
			} else {
				task.Results = []string{task.Images[0] + ".out"}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("RunObserved err=%v", err)
		}
		// low 有 5 分、3分/张 -> 缩减到 1 张（剩余 2 分不足 1 张后换号）；剩余 6 张全部落到 full（单批 6 张）。
		if len(sent) < 2 || sent[0].browserID != low.ID || sent[0].count != 1 {
			t.Fatalf("expected first sent batch to be low:1, got %+v", sent)
		}
		totalSent := 0
		for _, s := range sent {
			totalSent += s.count
		}
		if totalSent != 7 {
			t.Fatalf("expected all 7 images sent, got total=%d batches=%+v", totalSent, sent)
		}
		for _, item := range e.View().Snapshot() {
			if item.Status != StatusSuccess {
				t.Fatalf("all wash items should succeed: %+v", item)
			}
		}
		// low 剩余 2 积分（不足 4K 的 3分/张），DB 必须保留真实余分 2 而非强行清零。
		lowRow := browserByID(t, st, low.ID)
		if lowRow.CreditsRemain != 2 || lowRow.Health != store.HealthExhausted {
			t.Fatalf("low account after 4K exhaustion = remain:%d health:%s, want remain:2 health:exhausted", lowRow.CreditsRemain, lowRow.Health)
		}
	})
}
