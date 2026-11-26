package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestAutoDeleteHistoryCommitBoundariesAndWashReuse(t *testing.T) {
	t.Run("deletes on confirmed saved success when enabled", func(t *testing.T) {
		var calls []string
		e := &Engine{
			opts: Options{AutoDeleteHistory: true, WaitResult: true},
			deleteConvFn: func(_ context.Context, platform string, _ *cdp.Session, convID string) error {
				calls = append(calls, platform+":"+convID)
				return nil
			},
			convIDFn: func(*cdp.Session) string { return "conv-123" },
		}
		task := &Task{Index: 1, Platform: "豆包", submitted: true, Results: []string{"/tmp/out.png"}}
		e.maybeAutoDeleteHistory(context.Background(), task, nil, nil)
		if len(calls) != 1 || calls[0] != "豆包:conv-123" {
			t.Fatalf("calls = %v, want [豆包:conv-123]", calls)
		}
	})

	t.Run("skips when disabled or WaitResult is false", func(t *testing.T) {
		var calls int
		for _, opts := range []Options{
			{AutoDeleteHistory: false, WaitResult: true},
			{AutoDeleteHistory: true, WaitResult: false},
		} {
			e := &Engine{
				opts: opts,
				deleteConvFn: func(context.Context, string, *cdp.Session, string) error {
					calls++
					return nil
				},
				convIDFn: func(*cdp.Session) string { return "conv-1" },
			}
			task := &Task{Index: 1, Platform: "千问", submitted: true, sentURL: "qw-1", Results: []string{"/tmp/out.png"}}
			e.maybeAutoDeleteHistory(context.Background(), task, nil, nil)
		}
		if calls != 0 {
			t.Fatalf("calls = %d, want 0", calls)
		}
	})

	t.Run("never deletes uncertain or failed tasks", func(t *testing.T) {
		var calls int
		e := &Engine{
			opts: Options{AutoDeleteHistory: true, WaitResult: true},
			deleteConvFn: func(context.Context, string, *cdp.Session, string) error {
				calls++
				return nil
			},
			convIDFn: func(*cdp.Session) string { return "conv-uncertain" },
		}
		uncertain := &Task{Index: 1, Platform: "千问", submitted: true, sentURL: "qw-uncertain", Results: []string{"/tmp/out.png"}}
		e.maybeAutoDeleteHistory(context.Background(), uncertain, nil, strategy.ErrSubmissionUncertain)
		noSaved := &Task{Index: 2, Platform: "豆包", submitted: true}
		e.maybeAutoDeleteHistory(context.Background(), noSaved, nil, nil)
		if calls != 0 {
			t.Fatalf("calls = %d, want 0", calls)
		}
	})

	t.Run("defers Doubao wash reuse chat until flush and cancels if later turn fails", func(t *testing.T) {
		sess := &cdp.Session{}
		var deleted []string
		e := &Engine{
			opts: Options{AutoDeleteHistory: true, WaitResult: true, Wash: true},
			washChats: map[*cdp.Session]washChat{
				sess: {url: "https://www.doubao.com/chat/wash-1", count: 2},
			},
			deleteConvFn: func(_ context.Context, platform string, _ *cdp.Session, convID string) error {
				deleted = append(deleted, platform+":"+convID)
				return nil
			},
			convIDFn: func(*cdp.Session) string { return "wash-1" },
		}
		t1 := &Task{Index: 1, Platform: "豆包", submitted: true, Results: []string{"/tmp/a.png"}}
		e.maybeAutoDeleteHistory(context.Background(), t1, sess, nil)
		if len(deleted) != 0 {
			t.Fatalf("should defer during reuse, got %v", deleted)
		}
		// 同一复用会话下一轮发生不确定失败：必须撤销挂起的删除
		t2 := &Task{Index: 2, Platform: "豆包", submitted: true}
		e.maybeAutoDeleteHistory(context.Background(), t2, sess, strategy.ErrSubmissionUncertain)
		e.flushPendingAutoDelete(context.Background())
		if len(deleted) != 0 {
			t.Fatalf("should cancel deferred delete after failure, got %v", deleted)
		}

		// 若全部成功则在批次结束 flush 时删除
		e.maybeAutoDeleteHistory(context.Background(), t1, sess, nil)
		e.flushPendingAutoDelete(context.Background())
		if len(deleted) != 1 || deleted[0] != "豆包:wash-1" {
			t.Fatalf("deleted = %v, want [豆包:wash-1]", deleted)
		}
	})

	t.Run("delete failure is best-effort and does not panic", func(t *testing.T) {
		e := &Engine{
			opts: Options{AutoDeleteHistory: true, WaitResult: true},
			deleteConvFn: func(context.Context, string, *cdp.Session, string) error {
				return errors.New("ui changed")
			},
			convIDFn: func(*cdp.Session) string { return "conv-err" },
		}
		task := &Task{Index: 3, Platform: "豆包", submitted: true, Results: []string{"/tmp/c.png"}}
		e.maybeAutoDeleteHistory(context.Background(), task, nil, nil)
	})
}
