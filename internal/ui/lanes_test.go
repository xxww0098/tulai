package ui

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func loggedLane(id int64, name, platform string) store.Browser {
	return store.Browser{
		ID: id, Name: name, Platform: platform, IsLoggedIn: true, Health: store.HealthReady,
		DayKey: time.Now().Format("2006-01-02"),
	}
}

func TestLaneHeaderAndIdlePanel(t *testing.T) {
	if got := LaneHeader(3, 1, 4); got != "3 个账号同时出图 · 完成 1 / 共 4" {
		t.Fatal(got)
	}
	a := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		prompts:         "水彩一只猫",
		sources:         []sourceImage{{Path: "风景.png"}},
		browserRows: []store.Browser{
			loggedLane(1, "豆包主力", "豆包"),
			loggedLane(2, "千问备用", "千问"),
		},
	}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(500 * time.Millisecond)
	for _, label := range []string{"豆包主力", "千问备用", "空闲", "2 个账号同时出图 · 完成 0 / 共 0", "今日", "0/20"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q", label)
		}
	}
	if tt.FramePending() {
		t.Fatal("idle panel kept asking for frames")
	}
}

func TestRunButtonStaysCompactAndCrossfadesToStop(t *testing.T) {
	a := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		prompts:         "一只猫",
		outputPath:      "图片/输出结果",
		sources:         []sourceImage{{Path: "风景.png"}},
		browserRows:     []store.Browser{loggedLane(1, "豆包主力", "豆包")},
	}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(300 * time.Millisecond)
	start, ok := tt.Find("开始出图")
	if !ok || start.W > 220 || start.W < 48 {
		t.Fatalf("start button %+v", start)
	}
	for _, label := range []string{"1 张图 · 1 个账号同时出图", "图片/输出结果"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q", label)
		}
	}
	a.running = true
	tt.Frame()
	tt.Advance(motionFast + 40*time.Millisecond)
	if tt.HasText("开始出图") {
		t.Fatal("start label stayed after the crossfade")
	}
	stop, ok := tt.Find("停止")
	if !ok || stop.W > 220 {
		t.Fatalf("stop button %+v", stop)
	}
	if tt.FramePending() {
		t.Fatal("stop button kept asking for frames")
	}
}

func TestLaneStripWhenStepIsNotCurrent(t *testing.T) {
	a := &App{platformAuto: true, concurrencyAuto: true, browserRows: []store.Browser{loggedLane(1, "豆包主力", "豆包")}}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Advance(400 * time.Millisecond)
	if !tt.HasText("空闲") || !tt.HasText("豆包主力") {
		t.Fatal("compact strip missing")
	}
	if tt.HasText("今日") {
		t.Fatal("compact strip showed the quota bar")
	}
	if tt.FramePending() {
		t.Fatal("compact strip kept asking for frames")
	}
}

func TestRunningLaneShowsTheJobAndFliesToTheResult(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	a := &App{
		platformAuto: true,
		prompts:      "水彩一只猫",
		sources:      []sourceImage{{Path: "风景.png"}},
		browserRows:  []store.Browser{{ID: 1, Name: "豆包主力", Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady, DayKey: today}},
		queue: []tasks.Task{{
			Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning,
			Images: []string{"风景.png"}, Prompt: "水彩一只猫",
		}},
	}
	tt := ui.NewTester(a.view, 1280, 980)
	tt.Advance(400 * time.Millisecond)
	for _, label := range []string{"出图中", "水彩一只猫", "已用"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q", label)
		}
	}
	a.queue = []tasks.Task{{
		Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusSuccess,
		Images: []string{"风景.png"}, Prompt: "水彩一只猫", Results: []string{"out.png"},
	}}
	tt.Frame()
	early, ok := tt.Find("产出豆包主力")
	if !ok {
		t.Fatal("result badge missing")
	}
	tt.Advance(motionUI + 40*time.Millisecond)
	late, ok := tt.Find("产出豆包主力")
	if !ok {
		t.Fatal("result badge left")
	}
	if early.Y > late.Y-4 {
		t.Fatalf("result should travel down from the lane: early=%v late=%v", early.Y, late.Y)
	}
	tt.Advance(400 * time.Millisecond)
	if tt.FramePending() {
		t.Fatal("settled result kept asking for frames")
	}
}

func TestRunningLaneHonorsReducedMotion(t *testing.T) {
	a := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		prompts:         "一只猫",
		sources:         []sourceImage{{Path: "风景.png"}},
		browserRows:     []store.Browser{loggedLane(1, "豆包主力", "豆包")},
		queue: []tasks.Task{{
			Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning,
			Prompt: "一只猫",
		}},
	}
	a.preloadThumbs([]string{"风景.png"})
	tt := ui.NewTester(a.view, 1280, 900)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	early, ok := tt.Find("出图中")
	if !ok {
		t.Fatal("chip missing")
	}
	tt.Advance(motionUI + 80*time.Millisecond)
	late, ok := tt.Find("出图中")
	if !ok {
		t.Fatal("chip left")
	}
	if delta := early.X - late.X; delta > 0.5 || delta < -0.5 {
		t.Fatalf("reduced motion still slid the lane: early=%v late=%v", early.X, late.X)
	}
	if tt.FramePending() {
		t.Fatal("reduced motion kept asking for frames")
	}
}

func TestLaneLoginAndDailyFull(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	opened := make(chan int64, 1)
	a := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		prompts:         "一只猫",
		sources:         []sourceImage{{Path: "风景.png"}},
		dailyCap:        20,
		accountOpen: func(b store.Browser) error {
			opened <- b.ID
			return nil
		},
		browserRows: []store.Browser{
			{ID: 7, Name: "没登录", Platform: "豆包", Health: store.HealthExhausted, LastError: "未登录"},
			{ID: 8, Name: "今天满了", Platform: "千问", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 20},
		},
	}
	tt := ui.NewTester(a.view, 1280, 980)
	tt.Advance(300 * time.Millisecond)
	for _, label := range []string{"需要登录", "去重登", "今日已满", "20/20", "今天满了"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q", label)
		}
	}
	if err := tt.Click("去重登"); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-opened:
		if id != 7 {
			t.Fatalf("opened %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("去重登 did not open the account")
	}
}

func TestLaneChipShowsSendGap(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "主力", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, true, true)
	e := tasks.NewEngine(st, nil, nil, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan error, 1)
	var once sync.Once
	a := &App{store: st, engine: e, platformAuto: true, concurrencyAuto: true, prompts: "一只猫", dailyCap: 20}
	go func() {
		finished <- e.RunObserved(ctx, tasks.Options{
			Platforms: []string{"豆包"}, Images: []string{"a.png", "b.png"},
			Concurrency: 1, SendWait: 20, DailyCap: 20,
		}, func(tasks.Task) {}, func(tasks.Progress) {}, func(_ context.Context, task *tasks.Task) error {
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
	tt := ui.NewTester(a.view, 1100, 720)
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText("出图中") || !tt.HasText("主力") {
		t.Fatal("running lane missing")
	}
	close(release)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		board := e.AccountBoard(time.Now(), 20)
		if len(board.Lanes) == 1 && board.Lanes[0].State == tasks.LaneGap {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	tt.Frame()
	if !tt.HasText("等待间隔") {
		t.Fatal("gap chip missing")
	}
	if !tt.FramePending() {
		t.Fatal("countdown ring should keep painting")
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	tt.Advance(motionUI + 80*time.Millisecond)
	if tt.FramePending() {
		t.Fatal("reduced motion countdown kept painting every frame")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("run did not stop")
	}
}
