package tasks

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

func TestMapLaneStates(t *testing.T) {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.Local)
	job := &LaneJob{Index: 3, Prompt: "水彩猫", Image: "猫.png", Started: now.Add(-8 * time.Second)}
	base := LaneFact{ID: 1, Platform: "豆包", Name: "主力", LoggedIn: true, Health: store.HealthReady, Cap: 20}
	cases := []struct {
		name   string
		fact   LaneFact
		state  LaneState
		action string
		job    bool
	}{
		{name: "idle", fact: base, state: LaneIdle},
		{name: "running", fact: func() LaneFact { f := base; f.Busy = true; f.Job = job; f.UsedToday = 20; return f }(), state: LaneRunning, job: true},
		{name: "gap", fact: func() LaneFact {
			f := base
			f.NextReady = now.Add(4 * time.Second)
			f.Gap = 8 * time.Second
			return f
		}(), state: LaneGap},
		{name: "cooling", fact: func() LaneFact {
			f := base
			f.Health = store.HealthCooling
			f.CoolUntil = now.Add(time.Minute).Unix()
			f.LastError = "请求过于频繁"
			return f
		}(), state: LaneCooling},
		{name: "full count", fact: func() LaneFact { f := base; f.UsedToday = 20; return f }(), state: LaneFull},
		{name: "full quota", fact: func() LaneFact {
			f := base
			f.Health = store.HealthExhausted
			f.CoolUntil = now.Add(time.Hour).Unix()
			f.LastError = "额度已用尽"
			f.UsedToday = 2
			return f
		}(), state: LaneFull},
		{name: "login", fact: func() LaneFact { f := base; f.LoggedIn = false; f.UsedToday = 20; return f }(), state: LaneLogin, action: "去重登"},
		{name: "captcha", fact: func() LaneFact {
			f := base
			f.Health = store.HealthCooling
			f.CoolUntil = 0
			f.LastError = "需要安全验证"
			return f
		}(), state: LaneCaptcha, action: "去处理"},
		{name: "extension offline", fact: func() LaneFact {
			f := base
			f.Extension = true
			f.LoggedIn = false
			f.Offline = true
			return f
		}(), state: LaneOffline},
		{name: "local offline", fact: func() LaneFact {
			f := base
			f.Offline = true
			f.Health = store.HealthCooling
			f.CoolUntil = now.Add(time.Minute).Unix()
			f.LastError = "未连接"
			return f
		}(), state: LaneOffline, action: "去处理"},
		{name: "past gap is idle", fact: func() LaneFact { f := base; f.NextReady = now.Add(-time.Second); return f }(), state: LaneIdle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MapLane(tc.fact, now)
			if got.State != tc.state || got.Action != tc.action {
				t.Fatalf("state %s action %q, want %s %q", got.State, got.Action, tc.state, tc.action)
			}
			if tc.job && (got.Job == nil || got.Job.Prompt != "水彩猫" || got.Job.Image != "猫.png") {
				t.Fatalf("job %+v", got.Job)
			}
			if !tc.job && got.Job != nil {
				t.Fatalf("unexpected job %+v", got.Job)
			}
			if got.Cap != 20 || got.Name != "主力" {
				t.Fatalf("lane %+v", got)
			}
		})
	}
}

func TestAssembleSharesExtensionSlot(t *testing.T) {
	now := time.Now()
	rows := []store.Browser{
		{ID: 2, Platform: "千问", Name: "甲", ExtAccount: "p", IsLoggedIn: true, Health: store.HealthReady},
		{ID: 5, Platform: "豆包", Name: "甲", ExtAccount: "p", IsLoggedIn: true, Health: store.HealthReady},
	}
	board := Assemble(rows, LaneLive{
		Busy: map[int64]int64{2: 5},
		Jobs: map[int64]LaneJob{5: {Index: 1, Prompt: "猫", Image: "a.png", Started: now}},
	}, 20, now)
	if len(board.Lanes) != 2 || board.Active != 2 {
		t.Fatalf("board %+v", board)
	}
	if board.Lanes[0].State != LaneRunning || board.Lanes[0].Job != nil {
		t.Fatalf("sibling %+v", board.Lanes[0])
	}
	if board.Lanes[1].State != LaneRunning || board.Lanes[1].Job == nil || board.Lanes[1].Job.Prompt != "猫" {
		t.Fatalf("worker %+v", board.Lanes[1])
	}
}

func TestAccountBoardFollowsTheJob(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "甲", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, true, true)
	e := NewEngine(st, nil, nil, "")
	e.gap = func(time.Duration) time.Duration { return 2 * time.Second }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	var once sync.Once
	go func() {
		finished <- e.RunObserved(ctx, Options{
			Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"},
			Concurrency: 1, SendWait: 2, DailyCap: 20,
		}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
			if task.Index == 1 {
				once.Do(func() { close(started) })
				<-release
			}
			return nil
		})
	}()
	select {
	case <-started:
	case err := <-finished:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("job did not start")
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = e.AccountBoard(time.Now(), 20)
			}
		}
	}()

	board := e.AccountBoard(time.Now(), 20)
	if len(board.Lanes) != 1 || board.Lanes[0].State != LaneRunning || board.Lanes[0].Job == nil || board.Lanes[0].Job.Image != "a.png" {
		t.Fatalf("while running %+v", board.Lanes)
	}
	close(release)

	deadline := time.Now().Add(3 * time.Second)
	var gap AccountLane
	for time.Now().Before(deadline) {
		board = e.AccountBoard(time.Now(), 20)
		if len(board.Lanes) == 1 && board.Lanes[0].State == LaneGap {
			gap = board.Lanes[0]
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if gap.State != LaneGap || !gap.NextReadyAt.After(time.Now()) || gap.Gap != 2*time.Second {
		t.Fatalf("gap %+v", gap)
	}
	close(stop)
	wg.Wait()
	cancel()
	select {
	case err := <-finished:
		if err != nil && err != context.Canceled {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run did not stop")
	}
}

func TestAccountBoardCountsTodayAndReturnsIdle(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("千问", "乙", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, true, true)
	e := NewEngine(st, nil, nil, "")
	e.gap = func(time.Duration) time.Duration { return 0 }
	err = e.RunObserved(context.Background(), Options{
		Platforms: []string{"千问"}, Images: []string{"a.png"}, DailyCap: 20,
	}, func(Task) {}, func(Progress) {}, func(context.Context, *Task) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	board := e.AccountBoard(time.Now(), 20)
	if len(board.Lanes) != 1 || board.Lanes[0].State != LaneIdle || board.Lanes[0].UsedToday != 1 || board.Lanes[0].Job != nil {
		t.Fatalf("after run %+v", board.Lanes)
	}
}
