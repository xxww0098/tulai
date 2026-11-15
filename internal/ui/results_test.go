package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func writeSizedTestPNG(t *testing.T, path string, w, h int, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((int(c.R) + x*40/max(1, w)) % 256),
				G: uint8((int(c.G) + y*40/max(1, h)) % 256),
				B: c.B,
				A: 255,
			})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

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
	wantDoneSources := []string{"/a/源图1.jpg", "/a/源图1.jpg", "/c/源图3.jpg", "/d/源图4.jpg", "/e/源图5.jpg"}
	wantDonePaths := []string{"1a.png", "1b.png", "3.png", "4.png", "5.png"}

	for _, width := range []float32{320, 1024, 1280} {
		rows, _ := a.buildResultRows(width)
		if len(rows) != 2 || !rows[0].header || rows[0].title != "豆包1" || len(rows[1].tiles) != 1 {
			t.Fatalf("width %.0f: active rows should keep only running 豆包1 task, got %+v", width, rows)
		}
		activeTile := rows[1].tiles[0]
		if taskSource(activeTile.task) != "/b/源图2.jpg" || activeTile.path != "" {
			t.Fatalf("width %.0f: active tile = %q / %q", width, taskSource(activeTile.task), activeTile.path)
		}

		split := a.buildResultSplit(width, tasksForPlatform(a.queueItems(), a.stagePlatform()))
		if split.doneTasks != 4 || len(split.doneTiles) != len(wantDonePaths) {
			t.Fatalf("width %.0f: done split mismatch tasks=%d tiles=%d", width, split.doneTasks, len(split.doneTiles))
		}
		for i, tile := range split.doneTiles {
			if taskSource(tile.task) != wantDoneSources[i] || tile.path != wantDonePaths[i] {
				t.Fatalf("width %.0f: done tile %d = %q / %q", width, i, taskSource(tile.task), tile.path)
			}
		}
		doneRows := buildDoneResultRows(split.doneTiles, 3)
		if len(doneRows) != 2 || len(doneRows[0].tiles) != 3 || len(doneRows[1].tiles) != 2 {
			t.Fatalf("width %.0f: doneRows wrap mismatch: %+v", width, doneRows)
		}
	}
}

func TestWashResultsRenderAcrossAndWrap(t *testing.T) {
	cols, _ := resultLayout(1280)
	if cols < 6 {
		t.Fatalf("expected compact multi-column layout on 1280px, got cols=%d", cols)
	}
	activeQueue := make([]tasks.Task, cols+2)
	for i := range activeQueue {
		activeQueue[i] = tasks.Task{
			Index: i + 1, Platform: "豆包", Status: tasks.StatusRunning,
			Images: []string{fmt.Sprintf("源图%d.jpg", i+1)},
		}
	}
	a := &App{tab: pageWash, statePage: pageWash, queue: activeQueue}
	tt := ui.NewTester(a.view, 1280, 1200)
	first, ok := tt.Find("结果卡源图1.jpg")
	if !ok {
		t.Fatal("first running card missing")
	}
	second, ok := tt.Find("结果卡源图2.jpg")
	if !ok || second.X <= first.X || second.Y != first.Y {
		t.Fatalf("second running card should sit beside first: first=%v second=%v", first, second)
	}
	wrapped, ok := tt.Find(fmt.Sprintf("结果卡源图%d.jpg", cols+1))
	if !ok || wrapped.Y <= first.Y || wrapped.X != first.X {
		t.Fatalf("card %d should wrap to second row: first=%v wrapped=%v", cols+1, first, wrapped)
	}

	doneQueue := make([]tasks.Task, 10)
	for i := range doneQueue {
		doneQueue[i] = tasks.Task{
			Index: i + 1, Platform: "豆包", Status: tasks.StatusSuccess,
			Images:  []string{fmt.Sprintf("源图%d.jpg", i+1)},
			Results: []string{fmt.Sprintf("结果%d.png", i+1)},
		}
	}
	b := &App{tab: pageWash, statePage: pageWash, queue: doneQueue}
	bt := ui.NewTester(b.view, 1280, 1200)
	if _, ok := bt.Find("结果卡结果1.png"); ok {
		t.Fatal("completed card should not stay persistently in main results grid")
	}
	if !bt.HasText("已完成 10") {
		t.Fatalf("done count pill missing: %v", bt.Texts())
	}
	if err := bt.Click("已完成 10"); err != nil {
		t.Fatal(err)
	}
	if _, ok := bt.Find("已完成结果弹窗"); !ok {
		t.Fatal("done popover failed to open")
	}
	d1, ok := bt.Find("结果卡结果1.png")
	if !ok {
		t.Fatal("first done card missing in popover")
	}
	d2, ok := bt.Find("结果卡结果2.png")
	if !ok || d2.X <= d1.X || d2.Y != d1.Y {
		t.Fatalf("second done card should sit beside first: d1=%v d2=%v", d1, d2)
	}
	d9, ok := bt.Find("结果卡结果9.png")
	if !ok || d9.Y <= d1.Y || d9.X != d1.X {
		t.Fatalf("ninth done card should wrap under 8-col done grid: d1=%v d9=%v", d1, d9)
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
				loggedLane(4, "豆包4", "豆包"),
			},
		}
		tt := ui.NewTester(a.view, 1280, 2400)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
		started := tt.Now().Add(-2050 * time.Millisecond)

		var queue []tasks.Task
		for i := 1; i <= 6; i++ {
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
		for i := 1; i <= 4; i++ {
			queue = append(queue, tasks.Task{
				Index:       6 + i,
				Platform:    "豆包",
				BrowserID:   2,
				AccountName: "豆包2",
				Prompt:      fmt.Sprintf("豆包2任务%02d", i),
				Status:      tasks.StatusRunning,
				StartedAt:   started,
			})
		}
		for i := 1; i <= 6; i++ {
			queue = append(queue, tasks.Task{
				Index:       10 + i,
				Platform:    "豆包",
				BrowserID:   2,
				AccountName: "豆包2",
				Prompt:      fmt.Sprintf("豆包2完成%02d", i),
				Status:      tasks.StatusSuccess,
				Elapsed:     12 * time.Second,
				Results:     []string{fmt.Sprintf("out2-%02d.png", i)},
			})
		}
		for i := 1; i <= 2; i++ {
			queue = append(queue, tasks.Task{
				Index:       16 + i,
				Platform:    "豆包",
				BrowserID:   4,
				AccountName: "豆包4",
				Prompt:      fmt.Sprintf("豆包4完成%02d", i),
				Status:      tasks.StatusSuccess,
				Elapsed:     9 * time.Second,
				Results:     []string{fmt.Sprintf("out4-%02d.png", i)},
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
		for i := 1; i <= 6; i++ {
			card := assertVisible(t, tt, fmt.Sprintf("结果卡豆包1任务%02d", i), 1280, 2400)
			if card.Y < hdr1.Y+hdr1.H-1 || card.Y+card.H > hdr2.Y+1 {
				t.Fatalf("豆包1 task %d out of group bounds: card=%v hdr1=%v hdr2=%v", i, card, hdr1, hdr2)
			}
		}
		for i := 1; i <= 4; i++ {
			card := assertVisible(t, tt, fmt.Sprintf("结果卡豆包2任务%02d", i), 1280, 2400)
			if card.Y < hdr2.Y+hdr2.H-1 {
				t.Fatalf("豆包2 task %d should sit below 豆包2 header: card=%v hdr2=%v", i, card, hdr2)
			}
		}
		for _, want := range []string{"进行中 6", "进行中 4", "耗时 2秒", "已完成 8"} {
			if !tt.HasText(want) {
				t.Fatalf("missing expected status/duration text %q in %v", want, tt.Texts())
			}
		}
		for _, lane := range []string{"车道豆包3", "车道豆包4"} {
			if _, ok := tt.Find(lane); !ok {
				t.Fatalf("account %s should still show in top lane table", lane)
			}
		}
		for _, hidden := range []string{"账号分组豆包3", "账号分组豆包4", "排队中", "未开始排队任务1", "未派发停止任务1", "out2-01.png", "out4-01.png"} {
			if tt.HasText(hidden) {
				t.Fatalf("idle/completed-only/queued item %q should stay hidden in main results pane", hidden)
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

		if err := tt.Click("已完成 8"); err != nil {
			t.Fatal(err)
		}
		assertVisible(t, tt, "结果卡out2-01.png", 1280, 2400)
		assertVisible(t, tt, "结果卡out4-01.png", 1280, 2400)
		if err := tt.Click("关闭已完成面板"); err != nil {
			t.Fatal(err)
		}
		if tt.HasText("out2-01.png") {
			t.Fatal("completed popover items remained visible after closing")
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
					{Index: 1, Platform: "豆包", BrowserID: 1, AccountName: "豆包1", Images: []string{"源1.jpg"}, Status: tasks.StatusRunning, StartedAt: tt.Now().Add(-5050 * time.Millisecond)},
					{Index: 2, Platform: "豆包", BrowserID: 2, AccountName: "豆包2", Images: []string{"源2.jpg"}, Status: tasks.StatusSuccess, Elapsed: 9 * time.Second, Results: []string{"洗2.png"}},
					{Index: 3, Platform: "豆包", BrowserID: 3, Images: []string{"排队源图.jpg"}, Status: tasks.StatusPending},
				}
			} else {
				a.queue = []tasks.Task{
					{Index: 1, Platform: "豆包", BrowserID: 1, AccountName: "豆包1", Prompt: "裂变一", Status: tasks.StatusRunning, StartedAt: tt.Now().Add(-5050 * time.Millisecond)},
					{Index: 2, Platform: "豆包", BrowserID: 2, AccountName: "豆包2", Prompt: "裂变二", Status: tasks.StatusSuccess, Elapsed: 9 * time.Second, Results: []string{"裂变2.png"}},
					{Index: 3, Platform: "豆包", BrowserID: 3, Prompt: "裂变停止项", Status: tasks.StatusSkipped, Undispatched: true, Error: "已停止"},
				}
			}
			tt.Frame()
			tt.Advance(250 * time.Millisecond)

			assertVisible(t, tt, "账号分组豆包1", 1280, 1400)
			if !tt.HasText("耗时 5秒") || !tt.HasText("已完成 1") {
				t.Fatalf("%s missing running elapsed or done pill: %v", tc.name, tt.Texts())
			}
			if _, ok := tt.Find("车道豆包3"); !ok {
				t.Fatalf("%s: idle 豆包3 missing from top lane table", tc.name)
			}
			for _, hidden := range []string{"账号分组豆包2", "账号分组豆包3", "排队中", "排队源图.jpg", "裂变停止项", "洗2.png", "裂变2.png"} {
				if tt.HasText(hidden) {
					t.Fatalf("%s: hidden/completed item %q rendered in main results pane: %v", tc.name, hidden, tt.Texts())
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
	if len(rows) != 0 {
		t.Fatalf("completed/unworked restored tasks should not populate main active rows: %+v", rows)
	}
	split := restored.buildResultSplit(1280, tasksForPlatform(restored.queueItems(), restored.stagePlatform()))
	if split.doneTasks != 1 || len(split.doneTiles) != 1 || split.doneTiles[0].path != "out1.png" {
		t.Fatalf("restored done split mismatch: %+v", split)
	}
}

func TestResultTileHoldShowsLargePreview(t *testing.T) {
	dir := t.TempDir()
	portraitPath := filepath.Join(dir, "portrait-swing.png")
	landscapePath := filepath.Join(dir, "landscape-done.png")
	writeSizedTestPNG(t, portraitPath, 240, 360, color.RGBA{R: 120, G: 160, B: 230, A: 255})
	writeSizedTestPNG(t, landscapePath, 360, 240, color.RGBA{R: 230, G: 150, B: 110, A: 255})

	a := &App{
		platformAuto: true,
		platDoubao:   true,
		browserRows:  []store.Browser{loggedLane(1, "豆包1", "豆包")},
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	a.queue = []tasks.Task{
		{
			Index:       1,
			Platform:    "豆包",
			BrowserID:   1,
			AccountName: "豆包1",
			Status:      tasks.StatusRunning,
			StartedAt:   tt.Now().Add(-15 * time.Second),
			Images:      []string{portraitPath},
			Prompt:      "一只蓝猫在秋千上",
			Rewritten:   true,
		},
		{
			Index:       2,
			Platform:    "豆包",
			BrowserID:   1,
			AccountName: "豆包1",
			Status:      tasks.StatusSuccess,
			Elapsed:     8 * time.Second,
			Results:     []string{landscapePath},
			Prompt:      "晴空山景",
		},
	}
	tt.Frame()

	shot, ok := tt.Find("结果图")
	if !ok {
		t.Fatal("running tile shot missing")
	}
	tt.Move(shot.X+shot.W/2, shot.Y+shot.H/2)
	tt.Advance(100 * time.Millisecond)
	if _, ok := tt.Find("大图预览"); ok {
		t.Fatal("preview appeared before tooltip hold delay elapsed")
	}
	tt.Advance(150 * time.Millisecond)
	prev, ok := tt.Find("大图预览")
	if !ok {
		t.Fatal("large preview did not appear after 250ms hold")
	}
	if prev.X < 0 || prev.Y < 0 || prev.X+prev.W > 1280 || prev.Y+prev.H > 800 {
		t.Fatalf("running preview clipped outside viewport: %+v", prev)
	}
	if prev.H <= prev.W {
		t.Fatalf("portrait preview should preserve portrait aspect ratio: %+v", prev)
	}
	for _, want := range []string{filepath.Base(portraitPath), "豆包1", "耗时 15秒", "已改写："} {
		if !tt.HasText(want) {
			t.Fatalf("running hold preview missing %q in %v", want, tt.Texts())
		}
	}

	tt.Move(8, 8)
	tt.Frame()
	if _, ok := tt.Find("大图预览"); ok {
		t.Fatal("preview did not close after moving pointer away")
	}

	if err := tt.Click("已完成 1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("已完成结果弹窗"); !ok {
		t.Fatal("done popover did not open")
	}
	doneCard, ok := tt.Find("结果卡" + filepath.Base(landscapePath))
	if !ok {
		t.Fatal("done card missing inside popover")
	}
	tt.Move(doneCard.X+doneCard.W/2, doneCard.Y+doneCard.W/2)
	tt.Advance(250 * time.Millisecond)
	donePrev, ok := tt.Find("大图预览")
	if !ok {
		t.Fatal("hold preview did not appear inside done popover")
	}
	if donePrev.X < 0 || donePrev.Y < 0 || donePrev.X+donePrev.W > 1280 || donePrev.Y+donePrev.H > 800 {
		t.Fatalf("done popover preview clipped outside viewport: %+v", donePrev)
	}
	if !a.resultsDoneOpen {
		t.Fatal("done popover closed when hold preview opened")
	}
	if _, ok := tt.Find("已完成结果弹窗"); !ok {
		t.Fatal("done popover missing while preview active")
	}
	if !tt.HasText(filepath.Base(landscapePath)) {
		t.Fatalf("done preview missing landscape title: %v", tt.Texts())
	}

	tt.ClickAt(8, 8)
	tt.Frame()
	if a.resultsDoneOpen {
		t.Fatal("done popover failed to dismiss on outside click")
	}
}
