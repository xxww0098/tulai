package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestWashResultRowsKeepSourcesAcrossColumns(t *testing.T) {
	a := &App{
		statePage: pageWash,
		browserRows: []store.Browser{
			loggedLane(1, "豆包1", "豆包"),
			loggedLane(2, "豆包2", "豆包"),
		},
		queue: []tasks.Task{
			{Index: 1, Platform: "豆包", BrowserID: 1, AccountName: "豆包1", Status: tasks.StatusSuccess, Images: []string{"/a/源图1.jpg"}, Results: []string{"1a.png", "1b.png"}},
			{Index: 2, Platform: "豆包", BrowserID: 1, AccountName: "豆包1", Status: tasks.StatusRunning, Images: []string{"/b/源图2.jpg"}},
			{Index: 3, Platform: "豆包", BrowserID: 1, AccountName: "豆包1", Status: tasks.StatusSuccess, Images: []string{"/c/源图3.jpg"}, Results: []string{"3.png"}},
			{Index: 4, Platform: "豆包", BrowserID: 2, AccountName: "豆包2", Status: tasks.StatusSuccess, Images: []string{"/d/源图4.jpg"}, Results: []string{"4.png"}},
			{Index: 5, Platform: "豆包", BrowserID: 2, AccountName: "豆包2", Status: tasks.StatusSuccess, Images: []string{"/e/源图5.jpg"}, Results: []string{"5.png"}},
			{Index: 6, Platform: "豆包", BrowserID: 2, AccountName: "豆包2", Status: tasks.StatusPending, Images: []string{"/f/排队源图.jpg"}},
		},
	}
	wantSources := []string{"/a/源图1.jpg", "/a/源图1.jpg", "/b/源图2.jpg", "/c/源图3.jpg", "/d/源图4.jpg", "/e/源图5.jpg"}
	wantPaths := []string{"1a.png", "1b.png", "", "3.png", "4.png", "5.png"}
	for _, width := range []float32{320, 1024, 1280} {
		rows, _ := a.buildResultRows(width)
		cols, _ := resultLayout(width)
		wantRows := 1 + (4+cols-1)/cols + 1 + (2+cols-1)/cols
		if len(rows) != wantRows {
			t.Fatalf("width %.0f: got %d rows for %d columns, want %d", width, len(rows), cols, wantRows)
		}
		var headers []string
		n := 0
		for _, row := range rows {
			if row.header {
				headers = append(headers, row.title)
				continue
			}
			if len(row.tiles) > cols {
				t.Fatalf("width %.0f: unexpected row len %+v", width, row)
			}
			for _, tile := range row.tiles {
				if taskSource(tile.task) != wantSources[n] || tile.path != wantPaths[n] {
					t.Fatalf("width %.0f: tile %d = %q / %q", width, n, taskSource(tile.task), tile.path)
				}
				n++
			}
		}
		if len(headers) != 2 || headers[0] != "豆包1" || headers[1] != "豆包2" {
			t.Fatalf("width %.0f: headers = %v", width, headers)
		}
		if n != len(wantPaths) {
			t.Fatalf("width %.0f: got %d tiles", width, n)
		}
	}
}

func TestWashResultsRenderAcrossAndWrap(t *testing.T) {
	queue := make([]tasks.Task, 6)
	for i := range queue {
		queue[i] = tasks.Task{
			Index: i + 1, Platform: "豆包", Status: tasks.StatusSuccess,
			Images:  []string{fmt.Sprintf("源图%d.jpg", i+1)},
			Results: []string{fmt.Sprintf("结果%d.png", i+1)},
		}
	}
	a := &App{tab: pageWash, statePage: pageWash, queue: queue}
	tt := ui.NewTester(a.view, 1280, 1200)
	first, ok := tt.Find("结果卡结果1.png")
	if !ok {
		t.Fatal("first result missing")
	}
	second, ok := tt.Find("结果卡结果2.png")
	if !ok || second.X <= first.X || second.Y != first.Y {
		t.Fatalf("second result should be beside first: first=%v second=%v", first, second)
	}
	sixth, ok := tt.Find("结果卡结果6.png")
	if !ok || sixth.Y <= first.Y || sixth.X != first.X {
		t.Fatalf("sixth result should wrap: first=%v sixth=%v", first, sixth)
	}
	for i := range queue {
		if !tt.HasText(fmt.Sprintf("源图%d.jpg", i+1)) {
			t.Fatalf("source label %d missing", i+1)
		}
	}
}


func TestResultsGroupByAccountAndHideIdleAndQueued(t *testing.T) {
	t.Run("home", func(t *testing.T) {
		a := &App{
			platformAuto: true,
			platDoubao:   true,
			browserRows: []store.Browser{
				loggedLane(1, "豆包1", "豆包"),
				loggedLane(2, "豆包2", "豆包"),
				loggedLane(3, "豆包3", "豆包"),
			},
		}
		tt := ui.NewTester(a.view, 1280, 2400)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		started := tt.Now().Add(-2050 * time.Millisecond)

		var queue []tasks.Task
		for i := 1; i <= 10; i++ {
			queue = append(queue, tasks.Task{
				Index:       i,
				Platform:    "豆包",
				BrowserID:   1,
				AccountName: "豆包1",
				Prompt:      fmt.Sprintf("豆包1任务%02d", i),
				Status:      tasks.StatusRunning,
				StartedAt:   started,
			})
		}
		for i := 1; i <= 10; i++ {
			queue = append(queue, tasks.Task{
				Index:       10 + i,
				Platform:    "豆包",
				BrowserID:   2,
				AccountName: "豆包2",
				Prompt:      fmt.Sprintf("豆包2任务%02d", i),
				Status:      tasks.StatusSuccess,
				Elapsed:     12 * time.Second,
				Results:     []string{fmt.Sprintf("out2-%02d.png", i)},
			})
		}
		queue = append(queue,
			tasks.Task{Index: 21, Platform: "豆包", BrowserID: 3, Prompt: "未开始排队任务1", Status: tasks.StatusPending},
			tasks.Task{Index: 22, Platform: "豆包", BrowserID: 3, Prompt: "未开始排队任务2", Status: tasks.StatusPending},
			tasks.Task{Index: 23, Platform: "豆包", BrowserID: 3, Prompt: "未派发停止任务1", Status: tasks.StatusSkipped, Undispatched: true, Error: "已停止"},
			tasks.Task{Index: 24, Platform: "豆包", BrowserID: 3, Prompt: "未派发停止任务2", Status: tasks.StatusSkipped, Undispatched: true, Error: "已停止"},
		)
		a.queue = queue
		tt.Frame()
		tt.Advance(250 * time.Millisecond)

		hdr1 := assertVisible(t, tt, "账号分组豆包1", 1280, 2400)
		hdr2 := assertVisible(t, tt, "账号分组豆包2", 1280, 2400)
		if hdr1.Y >= hdr2.Y {
			t.Fatalf("豆包1 header should be above 豆包2 header: hdr1=%v hdr2=%v", hdr1, hdr2)
		}
		for i := 1; i <= 10; i++ {
			card := assertVisible(t, tt, fmt.Sprintf("结果卡豆包1任务%02d", i), 1280, 2400)
			if card.Y < hdr1.Y+hdr1.H-1 || card.Y+card.H > hdr2.Y+1 {
				t.Fatalf("豆包1 task %d out of group bounds: card=%v hdr1=%v hdr2=%v", i, card, hdr1, hdr2)
			}
		}
		for i := 1; i <= 10; i++ {
			card := assertVisible(t, tt, fmt.Sprintf("结果卡out2-%02d.png", i), 1280, 2400)
			if card.Y < hdr2.Y+hdr2.H-1 {
				t.Fatalf("豆包2 task %d should sit below 豆包2 header: card=%v hdr2=%v", i, card, hdr2)
			}
		}
		for _, want := range []string{"进行中", "耗时 2秒", "完成", "耗时 12秒"} {
			if !tt.HasText(want) {
				t.Fatalf("missing expected status/duration text %q in %v", want, tt.Texts())
			}
		}
		if _, ok := tt.Find("车道豆包3"); !ok {
			t.Fatal("idle account 豆包3 should still show in top lane table")
		}
		for _, hidden := range []string{"账号分组豆包3", "排队中", "未开始排队任务1", "未派发停止任务1"} {
			if tt.HasText(hidden) {
				t.Fatalf("idle/undispatched item %q should stay hidden in results pane", hidden)
			}
		}
		if tt.FramePending() {
			t.Fatal("results clock kept frame pending between ticks under ReduceMotion")
		}
		tt.Advance(time.Second)
		if !tt.HasText("耗时 3秒") || tt.HasText("耗时 2秒") {
			t.Fatalf("running task elapsed did not tick after 1s: %v", tt.Texts())
		}
		if tt.FramePending() {
			t.Fatal("results clock kept frame pending after 1s tick under ReduceMotion")
		}
	})

	for _, tc := range []struct {
		name string
		page int
	}{
		{name: "wash", page: pageWash},
		{name: "fission", page: pageFission},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &App{
				tab:          tc.page,
				statePage:    tc.page,
				platformAuto: true,
				platDoubao:   true,
				browserRows: []store.Browser{
					loggedLane(1, "豆包1", "豆包"),
					loggedLane(2, "豆包2", "豆包"),
					loggedLane(3, "豆包3", "豆包"),
				},
			}
			tt := ui.NewTester(a.view, 1280, 1400)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
			if tc.page == pageWash {
				a.queue = []tasks.Task{
					{Index: 1, Platform: "豆包", BrowserID: 1, AccountName: "豆包1", Images: []string{"源1.jpg"}, Status: tasks.StatusSuccess, Elapsed: 5 * time.Second, Results: []string{"洗1.png"}},
					{Index: 2, Platform: "豆包", BrowserID: 2, AccountName: "豆包2", Images: []string{"源2.jpg"}, Status: tasks.StatusSuccess, Elapsed: 9 * time.Second, Results: []string{"洗2.png"}},
					{Index: 3, Platform: "豆包", BrowserID: 3, Images: []string{"排队源图.jpg"}, Status: tasks.StatusPending},
				}
			} else {
				a.queue = []tasks.Task{
					{Index: 1, Platform: "豆包", BrowserID: 1, AccountName: "豆包1", Prompt: "裂变一", Status: tasks.StatusSuccess, Elapsed: 4 * time.Second, Results: []string{"裂变1.png"}},
					{Index: 2, Platform: "豆包", BrowserID: 2, AccountName: "豆包2", Prompt: "裂变二", Status: tasks.StatusRunning, StartedAt: tt.Now().Add(-7050 * time.Millisecond)},
					{Index: 3, Platform: "豆包", BrowserID: 3, Prompt: "裂变停止项", Status: tasks.StatusSkipped, Undispatched: true, Error: "已停止"},
				}
			}
			tt.Frame()
			tt.Advance(250 * time.Millisecond)

			hdr1 := assertVisible(t, tt, "账号分组豆包1", 1280, 1400)
			hdr2 := assertVisible(t, tt, "账号分组豆包2", 1280, 1400)
			if hdr1.Y >= hdr2.Y {
				t.Fatalf("%s: 豆包1 header should precede 豆包2 header", tc.name)
			}
			if _, ok := tt.Find("车道豆包3"); !ok {
				t.Fatalf("%s: idle 豆包3 missing from top lane table", tc.name)
			}
			if tt.HasText("账号分组豆包3") || tt.HasText("排队中") || tt.HasText("排队源图.jpg") || tt.HasText("裂变停止项") {
				t.Fatalf("%s: idle or unworked item rendered in results pane: %v", tc.name, tt.Texts())
			}
			if tc.page == pageWash {
				if !tt.HasText("耗时 5秒") || !tt.HasText("耗时 9秒") {
					t.Fatalf("wash durations missing: %v", tt.Texts())
				}
			} else {
				if !tt.HasText("耗时 4秒") || !tt.HasText("耗时 7秒") {
					t.Fatalf("fission durations missing: %v", tt.Texts())
				}
			}
		})
	}
}

func TestEnginePopulatesTaskElapsedAndSessionPersists(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	b1, err := st.AddBrowser("豆包", "豆包1", "")
	if err != nil {
		t.Fatal(err)
	}
	b2, err := st.AddBrowser("豆包", "豆包2", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(b1.ID, false, true)
	st.UpdateBrowser(b2.ID, false, true)
	st.UpdateBrowserHealth(b2.ID, store.HealthCooling, time.Now().Add(time.Hour).Unix(), "冷却中")

	eng := tasks.NewEngine(st, nil, nil, "")
	err = eng.RunObserved(context.Background(), tasks.Options{
		Platforms:   []string{"豆包"},
		Prompts:     []string{"已跑任务", "未派发任务"},
		Concurrency: 1,
		SendWait:    5,
	}, func(task tasks.Task) {
		if task.Index == 1 && task.Status == tasks.StatusSuccess {
			eng.Stop()
		}
	}, func(tasks.Progress) {}, func(_ context.Context, task *tasks.Task) error {
		time.Sleep(15 * time.Millisecond)
		task.Results = []string{"out1.png"}
		return nil
	})
	if err != nil {
		t.Fatalf("RunObserved failed: %v", err)
	}
	snap := eng.View().Snapshot()
	if len(snap) != 2 {
		t.Fatalf("got %d tasks, want 2: %+v", len(snap), snap)
	}
	worked, unworked := snap[0], snap[1]
	if worked.Status != tasks.StatusSuccess || worked.Elapsed <= 0 || worked.BrowserID != b1.ID || worked.AccountName != "豆包1" || !worked.Worked() {
		t.Fatalf("worked task mismatch: %+v", worked)
	}
	if unworked.Status != tasks.StatusSkipped || !unworked.Undispatched || unworked.Worked() {
		t.Fatalf("unworked stopped task mismatch: %+v", unworked)
	}

	app := &App{
		store:       st,
		statePage:   pageHome,
		prompts:     "已跑任务\n未派发任务",
		browserRows: st.Browsers(),
		queue:       snap,
	}
	if !app.persistWorkspace(pageHome) {
		t.Fatal("persistWorkspace failed")
	}

	restored := &App{store: st, statePage: pageHome}
	restored.browserRows = st.Browsers()
	restored.restoreWorkspace(pageHome)
	if len(restored.queue) != 2 {
		t.Fatalf("restored %d tasks, want 2: %+v", len(restored.queue), restored.queue)
	}
	rWorked, rUnworked := restored.queue[0], restored.queue[1]
	if rWorked.AccountName != "豆包1" || rWorked.Elapsed != worked.Elapsed.Truncate(time.Millisecond) || !rWorked.Worked() {
		t.Fatalf("restored worked task mismatch: %+v (orig=%+v)", rWorked, worked)
	}
	if rUnworked.Status != tasks.StatusSkipped || !rUnworked.Undispatched || rUnworked.Worked() {
		t.Fatalf("restored unworked task mismatch: %+v", rUnworked)
	}
	rows, _ := restored.buildResultRows(1280)
	if len(rows) != 2 || !rows[0].header || rows[0].title != "豆包1" || len(rows[1].tiles) != 1 {
		t.Fatalf("restored grouped rows mismatch: %+v", rows)
	}
}
