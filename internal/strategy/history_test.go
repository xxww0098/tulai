package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
)

func TestHistoryCleanerInterfaceImplemented(t *testing.T) {
	for _, platform := range []string{"豆包", "千问"} {
		strat := Factory(platform)
		if _, ok := strat.(HistoryCleaner); !ok {
			t.Fatalf("strategy %s does not implement HistoryCleaner", platform)
		}
	}
}

func TestClearHistoryLoopProgressStopAndKeepRecent(t *testing.T) {
	// 使用非 nil 空 Session 占位，跳过需要真实浏览器的 LoginState（在无 client 时安全返回 error 被忽略）
	dummy := &cdp.Session{}

	t.Run("deletes until done and reports progress", func(t *testing.T) {
		items := []string{"第一条", "第二条", "第三条"}
		var seen []string
		res, err := clearHistoryLoop(context.Background(), dummy, ClearHistoryOptions{
			KeepRecent: 1,
			MaxDelete:  10,
			DelayMin:   time.Millisecond,
			DelayMax:   2 * time.Millisecond,
			OnProgress: func(deleted int, title string) {
				seen = append(seen, title)
			},
		}, func(_ context.Context, skipIDs []string, keep int) (historyStepOutcome, error) {
			if keep != 1 {
				t.Fatalf("keepRecent = %d, want 1", keep)
			}
			if len(items) <= keep {
				return historyStepOutcome{OK: true, Done: true, Remaining: len(items)}, nil
			}
			title := items[keep]
			items = append(items[:keep], items[keep+1:]...)
			return historyStepOutcome{OK: true, Deleted: true, ID: title, Title: title, Remaining: len(items)}, nil
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if res.Deleted != 2 || res.Remaining != 1 || res.Stopped != "" {
			t.Fatalf("unexpected result: %+v", res)
		}
		if len(seen) != 2 || seen[0] != "第二条" || seen[1] != "第三条" {
			t.Fatalf("unexpected progress: %v", seen)
		}
	})

	t.Run("aborts immediately on rate limit or captcha", func(t *testing.T) {
		res, err := clearHistoryLoop(context.Background(), dummy, ClearHistoryOptions{
			DelayMin: time.Millisecond,
			DelayMax: 2 * time.Millisecond,
		}, func(_ context.Context, _ []string, _ int) (historyStepOutcome, error) {
			return historyStepOutcome{OK: false, Stopped: "rate_limit"}, nil
		})
		if !errors.Is(err, ErrAccountRate) {
			t.Fatalf("err = %v, want ErrAccountRate", err)
		}
		if res.Stopped != "rate_limit" {
			t.Fatalf("stopped = %q, want rate_limit", res.Stopped)
		}
	})

	t.Run("skips failing item and bails after 3 consecutive failures", func(t *testing.T) {
		calls := 0
		_, err := clearHistoryLoop(context.Background(), dummy, ClearHistoryOptions{
			DelayMin: time.Millisecond,
			DelayMax: 2 * time.Millisecond,
		}, func(_ context.Context, skipIDs []string, _ int) (historyStepOutcome, error) {
			calls++
			if len(skipIDs) != calls-1 {
				t.Fatalf("skipIDs = %v on call %d", skipIDs, calls)
			}
			return historyStepOutcome{OK: false, ID: "bad", Error: "删除失败"}, nil
		})
		if err == nil || calls != maxConsecutiveDelFails {
			t.Fatalf("calls=%d err=%v, want %d calls and error", calls, err, maxConsecutiveDelFails)
		}
	})

	t.Run("honors context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		res, err := clearHistoryLoop(ctx, dummy, ClearHistoryOptions{}, func(_ context.Context, _ []string, _ int) (historyStepOutcome, error) {
			t.Fatal("step should not run on canceled ctx")
			return historyStepOutcome{}, nil
		})
		if !errors.Is(err, context.Canceled) || res.Stopped != "cancelled" {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	})
}

func TestClearHistoryFastModeAndFallbacks(t *testing.T) {
	dummy := &cdp.Session{}

	t.Run("captures template after first DOM delete and batch-replays remaining", func(t *testing.T) {
		server := map[string]bool{"c1": true, "c2": true, "c3": true, "c4": true, "c5": true}
		order := []string{"c1", "c2", "c3", "c4", "c5"}
		liveIDs := func() []string {
			var out []string
			for _, id := range order {
				if server[id] {
					out = append(out, id)
				}
			}
			return out
		}
		domCalls := 0
		waveCalls := 0
		res, err := clearHistoryExec(context.Background(), dummy, ClearHistoryOptions{
			KeepRecent: 1,
			MaxDelete:  50,
			DelayMin:   time.Millisecond,
			DelayMax:   2 * time.Millisecond,
			FastMode:   true,
		}, historyDriver{
			installFast: func(context.Context) error { return nil },
			domStep: func(_ context.Context, _ []string, keep int) (historyStepOutcome, error) {
				domCalls++
				ids := liveIDs()
				if len(ids) <= keep {
					return historyStepOutcome{OK: true, Done: true, Remaining: len(ids)}, nil
				}
				id := ids[keep]
				delete(server, id)
				return historyStepOutcome{OK: true, Deleted: true, ID: id, Title: id, Remaining: len(ids) - 1}, nil
			},
			takeTemplate: func(context.Context) (json.RawMessage, error) {
				return json.RawMessage(`{"method":"POST","urlTemplate":"/del"}`), nil
			},
			collectIDs: func(_ context.Context, _ []string, keep, maxCount int) (historyCollectResult, error) {
				ids := liveIDs()
				keepIDs := ids[:min(keep, len(ids))]
				var del []string
				if len(ids) > keep {
					del = ids[keep:]
					if len(del) > maxCount {
						del = del[:maxCount]
					}
				}
				return historyCollectResult{OK: true, IDs: del, KeepIDs: keepIDs, Titles: map[string]string{}, TotalSeen: len(ids)}, nil
			},
			replayOne: func(_ context.Context, _ json.RawMessage, id string, _ bool) (fastReplayResult, error) {
				delete(server, id)
				return fastReplayResult{OK: true, ID: id}, nil
			},
			replayWave: func(_ context.Context, _ json.RawMessage, ids []string, _ int, _ bool) (fastReplayResult, error) {
				waveCalls++
				for _, id := range ids {
					delete(server, id)
				}
				return fastReplayResult{OK: true, DeletedIDs: ids}, nil
			},
			reloadAndWait: func(context.Context) error { return nil },
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if domCalls != 1 || waveCalls != 1 {
			t.Fatalf("domCalls=%d waveCalls=%d, want 1 and 1", domCalls, waveCalls)
		}
		if res.Deleted != 4 || res.FastDeleted != 3 || res.Remaining != 1 {
			t.Fatalf("res = %+v, want deleted=4 fastDeleted=3 remaining=1", res)
		}
	})

	t.Run("falls back to DOM when false-positive template does not remove item on server", func(t *testing.T) {
		server := map[string]bool{"c1": true, "c2": true, "c3": true}
		order := []string{"c1", "c2", "c3"}
		liveIDs := func() []string {
			var out []string
			for _, id := range order {
				if server[id] {
					out = append(out, id)
				}
			}
			return out
		}
		domCalls := 0
		res, err := clearHistoryExec(context.Background(), dummy, ClearHistoryOptions{
			KeepRecent: 0,
			MaxDelete:  10,
			DelayMin:   time.Millisecond,
			DelayMax:   2 * time.Millisecond,
			FastMode:   true,
		}, historyDriver{
			installFast: func(context.Context) error { return nil },
			domStep: func(_ context.Context, _ []string, _ int) (historyStepOutcome, error) {
				domCalls++
				ids := liveIDs()
				if len(ids) == 0 {
					return historyStepOutcome{OK: true, Done: true, Remaining: 0}, nil
				}
				delete(server, ids[0])
				return historyStepOutcome{OK: true, Deleted: true, ID: ids[0], Remaining: len(ids) - 1}, nil
			},
			takeTemplate: func(context.Context) (json.RawMessage, error) {
				return json.RawMessage(`{"method":"POST","urlTemplate":"/noop"}`), nil
			},
			collectIDs: func(_ context.Context, _ []string, _, _ int) (historyCollectResult, error) {
				ids := liveIDs()
				return historyCollectResult{OK: true, IDs: ids, Titles: map[string]string{}, TotalSeen: len(ids)}, nil
			},
			// 假阳性：回放返回 OK 但未从服务端删掉
			replayOne: func(_ context.Context, _ json.RawMessage, id string, _ bool) (fastReplayResult, error) {
				return fastReplayResult{OK: true, ID: id}, nil
			},
			replayWave: func(_ context.Context, _ json.RawMessage, ids []string, _ int, _ bool) (fastReplayResult, error) {
				return fastReplayResult{OK: true, DeletedIDs: ids}, nil
			},
			reloadAndWait: func(context.Context) error { return nil },
		})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if res.Deleted != 3 || res.FastDeleted != 0 || domCalls < 3 {
			t.Fatalf("expected fallback to DOM with accurate counts, got res=%+v domCalls=%d", res, domCalls)
		}
	})
}

func TestDoubaoNativeBatchClearMultiRoundAndFallback(t *testing.T) {
	t.Run("loops native batch delete across pages and reports progress", func(t *testing.T) {
		round := 0
		var progress []int
		res, handled, err := runDoubaoNativeBatchClear(context.Background(), ClearHistoryOptions{
			KeepRecent: 0,
			MaxDelete:  100,
			OnProgress: func(deleted int, _ string) { progress = append(progress, deleted) },
		}, func(_ context.Context, keep, budget int) (doubaoBatchStepOutcome, error) {
			round++
			if round == 1 {
				return doubaoBatchStepOutcome{OK: true, Done: false, DeletedCount: 20, Remaining: 15}, nil
			}
			return doubaoBatchStepOutcome{OK: true, Done: true, DeletedCount: 15, Remaining: 0}, nil
		})
		if err != nil || !handled {
			t.Fatalf("handled=%v err=%v", handled, err)
		}
		if res.Deleted != 35 || res.FastDeleted != 35 || res.Remaining != 0 {
			t.Fatalf("unexpected res: %+v", res)
		}
		if len(progress) != 2 || progress[0] != 20 || progress[1] != 35 {
			t.Fatalf("unexpected progress: %v", progress)
		}
	})

	t.Run("returns handled=false when thread/list entry is unsupported so caller falls back", func(t *testing.T) {
		_, handled, err := runDoubaoNativeBatchClear(context.Background(), ClearHistoryOptions{}, func(_ context.Context, _, _ int) (doubaoBatchStepOutcome, error) {
			return doubaoBatchStepOutcome{OK: false, Unsupported: true, Error: "未找到菜单"}, nil
		})
		if err != nil || handled {
			t.Fatalf("expected handled=false err=nil, got handled=%v err=%v", handled, err)
		}
	})
}
