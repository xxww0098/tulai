package tasks

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/strategy"
)

// 千问回「请求异常」：换新对话只重试一次，再错就冷却这个号，活交给另一个号。
func TestReplyFaultRetriesOnceThenCoolsAccount(t *testing.T) {
	st := openRotateStore(t)
	first, second := addNamed(t, st, "千问", "甲"), addNamed(t, st, "千问", "乙")
	e := NewEngine(st, nil, nil, "")
	var mu sync.Mutex
	hits := map[int64]int{}
	start := time.Now()
	err := e.run(context.Background(), Options{
		Platforms: []string{"千问"}, Images: []string{"a.png"}, Concurrency: 1, RetryCount: 3,
	}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		mu.Lock()
		hits[task.BrowserID]++
		mu.Unlock()
		if task.BrowserID == first.ID {
			if task.submitted {
				t.Error("重试必须换新对话重新发送")
			}
			task.submitted = true
			return fmt.Errorf("%w：请求异常，请检查网络连接", strategy.ErrPlatformReply)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 30*time.Second {
		t.Fatalf("回复异常不该干等超时: %s", time.Since(start))
	}
	if hits[first.ID] != 2 {
		t.Fatalf("同一个号只能再试一次: %v", hits)
	}
	if hits[second.ID] != 1 {
		t.Fatalf("另一个号要接走这张图: %v", hits)
	}
	if browserByID(t, st, first.ID).Usable(time.Now()) {
		t.Fatal("连着回报错的号要先冷却")
	}
	if q := e.View().Snapshot(); len(q) != 1 || q[0].Status != StatusSuccess {
		t.Fatalf("换号后应出图成功: %+v", q)
	}
}
