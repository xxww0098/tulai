package ui

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestWorkspaceRestoresAfterRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	img := filepath.Join(dir, "ref.png")
	out := filepath.Join(dir, "out.png")
	writeTinyPNG(t, img)
	writeTinyPNG(t, out)

	a := &App{store: st}
	a.loadSettings()
	a.prompts = "窗边的猫"
	a.sources = []sourceImage{{Path: img}}
	a.queue = []tasks.Task{{
		Index: 1, Platform: "豆包", BrowserID: 3, Status: tasks.StatusSuccess,
		Prompt: "窗边的猫", Results: []string{out},
	}}
	a.progress = 1
	a.progText = "1 / 1"
	a.saveSettings()
	if a.genSession == 0 {
		t.Fatal("生图会话没有写入")
	}

	a.tab = pageWash
	a.syncPage()
	a.prompts = "白底"
	a.sources = []sourceImage{{Path: img}}
	a.queue = []tasks.Task{{
		Index: 1, Platform: "豆包", Status: tasks.StatusFailed,
		Prompt: "白底", Images: []string{img}, Error: "上传失败",
	}}
	a.progText = "0 / 1"
	a.saveSettings()
	washID := a.washSession
	if washID == 0 {
		t.Fatal("洗图会话没有写入")
	}

	b := &App{store: st}
	b.loadSettings()
	if b.tab != pageWash {
		t.Fatalf("应打开上次的洗图页，tab=%d", b.tab)
	}
	gen := b.stateOf(pageHome)
	if b.genSession != a.genSession || gen.prompts != "窗边的猫" || len(gen.sources) != 1 || gen.sources[0].Path != img {
		t.Fatalf("生图恢复失败 prompts=%q sources=%v id=%d", gen.prompts, gen.sources, b.genSession)
	}
	if len(gen.queue) != 1 || gen.queue[0].Status != tasks.StatusSuccess || len(gen.queue[0].Results) != 1 || gen.queue[0].Results[0] != out || gen.progText != "1 / 1" {
		t.Fatalf("生图任务恢复失败 %+v %q", gen.queue, gen.progText)
	}
	if b.washSession != washID || b.prompts != "白底" || len(b.sources) != 1 || b.sources[0].Path != img {
		t.Fatalf("洗图恢复失败 prompts=%q sources=%v id=%d", b.prompts, b.sources, b.washSession)
	}
	if len(b.queue) != 1 || b.queue[0].Status != tasks.StatusFailed || b.queue[0].Error != "上传失败" || b.queue[0].Images[0] != img {
		t.Fatalf("洗图任务恢复失败 %+v", b.queue)
	}
}

func TestInterruptedWorkspaceRestoresForReview(t *testing.T) {
	for _, page := range []int{pageHome, pageWash} {
		t.Run(workspacePageKey(page), func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.db")
			st, err := store.Open(file)
			if err != nil {
				t.Fatal(err)
			}
			var items []store.SessionTask
			statuses := []tasks.Status{tasks.StatusPending, tasks.StatusRunning, tasks.StatusRetry, tasks.StatusAwaitUser, "", "未知状态", "需核对", tasks.StatusSuccess, tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser}
			for i, status := range statuses {
				items = append(items, store.SessionTask{Ord: i + 1, Platform: "豆包", AccountID: 42,
					Prompt: "原提示词", Source: "原图.png", Outputs: "已保存.png", Status: string(status), Error: "原始错误"})
			}
			_, err = st.SaveActive(store.Session{Page: workspacePageKey(page), Progress: 0.9, ProgText: "正在生成旧任务"}, items)
			st.Close()
			if err != nil {
				t.Fatal(err)
			}
			st, err = store.Open(file)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			a := &App{store: st}
			a.loadSettings()
			state := a.stateOf(page)
			if a.running || len(state.queue) != len(items) {
				t.Fatalf("恢复历史不应启动任务: running=%v queue=%+v", a.running, state.queue)
			}
			for i, task := range state.queue {
				want := statuses[i]
				if i < 7 {
					want = "需核对"
				}
				if task.Status != want || task.BrowserID != 42 || task.Prompt != "原提示词" || len(task.Images) != 1 || task.Images[0] != "原图.png" || len(task.Results) != 1 || task.Results[0] != "已保存.png" || !strings.Contains(task.Error, "原始错误") {
					t.Fatalf("历史状态或记录丢失: item=%+v task=%+v want=%q", items[i], task, want)
				}
				if i < 7 {
					if tileStatus(task) != "需核对" {
						t.Fatalf("中断任务仍显示活动态: %+v", task)
					}
					reason, action, retry := tileFailure(task)
					if reason == "" || action != "核对后重新生成" || len(retry) != 1 {
						t.Fatalf("缺少核对后的恢复入口: reason=%q action=%q retry=%+v", reason, action, retry)
					}
				}
			}
			if strings.Contains(state.progText, "正在生成") || !strings.Contains(state.progText, "需核对 7") || state.progress != 4.0/11 {
				t.Fatalf("恢复了旧的运行进度: text=%q progress=%v", state.progText, state.progress)
			}
			if len(FailedTasks(state.queue)) != 2 {
				t.Fatalf("批量失败重试包含了需核对的任务: %+v", FailedTasks(state.queue))
			}
			if !a.persistWorkspace(page) {
				t.Fatal("保存归一化历史失败")
			}
			b := &App{store: st}
			b.loadSettings()
			for i, task := range b.stateOf(page).queue {
				if task.Status != state.queue[i].Status || task.Error != state.queue[i].Error {
					t.Fatalf("反复恢复改变了核对状态: before=%+v after=%+v", state.queue[i], task)
				}
			}
		})
	}
}

func TestReviewCardRequiresExplicitRegeneration(t *testing.T) {
	review := tasks.Task{Index: 1, Platform: "豆包", BrowserID: 42, Prompt: "原提示词", Status: tasks.StatusNeedsReview, Results: []string{"已保存.png"}}
	for _, status := range []tasks.Status{tasks.StatusSuccess, tasks.StatusFailed, tasks.StatusSkipped, tasks.StatusNeedsUser} {
		items := []tasks.Task{{Index: 2, Platform: "千问", Prompt: review.Prompt, Status: status}, review}
		cards := ProgressCards(items)
		if len(cards) != 1 || cards[0].Status != "需核对" || cards[0].Action != "核对后重新生成" || cards[0].Reason == "" || len(cards[0].Shots) != 1 {
			t.Fatalf("混合卡片绕过了核对提示: status=%s cards=%+v", status, cards)
		}
	}
	queue := tasks.RebuildRetry([]tasks.Task{review})
	if len(queue) != 1 || queue[0].Status != tasks.StatusPending || queue[0].Prompt != review.Prompt || queue[0].BrowserID != 0 || len(queue[0].Results) != 0 {
		t.Fatalf("主动重新生成没有创建新任务: %+v", queue)
	}
}

func TestRestoredReviewIsVisible(t *testing.T) {
	for _, width := range []int{980, 1280} {
		a := &App{platformAuto: true, stagePicked: true,
			queue: queueFromSession([]store.SessionTask{{Ord: 1, Platform: "豆包", Prompt: "原提示词", Status: string(tasks.StatusRunning)}})}
		tt := ui.NewTester(a.view, width, 900)
		tt.Advance(200 * time.Millisecond)
		for _, text := range []string{"需核对", "核对后重新生成", "上次任务已中断，请先核对原账号网页和已保存图片"} {
			if !tt.HasText(text) {
				t.Fatalf("恢复界面缺少 %q: %v", text, tt.Texts())
			}
		}
		for _, text := range []string{"生成中", "排队中", "全部重试失败项"} {
			if tt.HasText(text) {
				t.Fatalf("恢复界面仍保留活动态或批量重发入口 %q: %v", text, tt.Texts())
			}
		}
	}
}

func TestWashPageRestoresSourcesAndResults(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	cat := filepath.Join(dir, "cat.png")
	poster := filepath.Join(dir, "poster.png")
	mug := filepath.Join(dir, "mug.png")
	out := filepath.Join(dir, "poster_01.png")
	for _, path := range []string{cat, poster, mug, out} {
		writeTinyPNG(t, path)
	}

	a := &App{store: st, engine: tasks.NewEngine(st, nil, nil, ""), platformAuto: true}
	a.loadSettings()
	a.prompts = "窗边的猫"
	a.sources = []sourceImage{{Path: cat}}
	a.tab = pageWash
	a.syncPage()
	a.prompts = "只要主体，白底"
	a.sources = []sourceImage{{Path: poster}, {Path: mug}}
	a.queue = []tasks.Task{
		{Index: 1, Platform: "豆包", Status: tasks.StatusSuccess, Prompt: "只要主体，白底", Images: []string{poster}, Results: []string{out}},
		{Index: 2, Platform: "豆包", Status: tasks.StatusFailed, Prompt: "只要主体，白底", Images: []string{mug}, Error: "出图超时"},
	}
	a.progText = taskResultSummary(a.queue)
	a.stageTab = 0
	a.stagePicked = true
	a.saveSettings()

	b := &App{store: st, engine: tasks.NewEngine(st, nil, nil, ""), platformAuto: true}
	b.loadSettings()
	if b.tab != pageWash || b.prompts != "只要主体，白底" || len(b.sources) != 2 || len(b.queue) != 2 {
		t.Fatalf("洗图会话没回到界面 tab=%d prompts=%q sources=%d queue=%d", b.tab, b.prompts, len(b.sources), len(b.queue))
	}
	tt := ui.NewTester(b.view, 1280, 900)
	tt.Advance(200 * time.Millisecond)
	for _, label := range []string{"原图", "poster.png", "mug.png", "2 张", "poster_01.png", "出图超时，请重试", "洗图指令"} {
		if !tt.HasText(label) {
			t.Fatalf("洗图页缺少 %q，看到 %v", label, tt.Texts())
		}
	}
	if err := tt.Click("自由生图"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if b.tab != pageHome || b.prompts != "窗边的猫" || len(b.sources) != 1 || b.sources[0].Path != cat {
		t.Fatalf("生图被洗图盖住: tab=%d %q %+v", b.tab, b.prompts, b.sources)
	}
	if !tt.HasText("cat.png") {
		t.Fatalf("参考图没回来: %v", tt.Texts())
	}
}

func TestNewTaskDisabledWhileRunning(t *testing.T) {
	a := &App{
		platformAuto: true,
		prompts:      "不要清",
		queue:        []tasks.Task{{Index: 1, Platform: "豆包", Status: tasks.StatusPending, Prompt: "不要清"}},
		running:      true,
	}
	tt := ui.NewTester(a.view, 1280, 800)
	if err := tt.Click("新建任务"); err != nil {
		t.Fatal(err)
	}
	if a.prompts != "不要清" || len(a.queue) != 1 {
		t.Fatalf("running new task cleared the page: %q %+v", a.prompts, a.queue)
	}
	a.running = false
	tt.Frame()
	if err := tt.Click("新建任务"); err != nil {
		t.Fatal(err)
	}
	if a.prompts != "" || len(a.queue) != 0 {
		t.Fatalf("new task left prompts=%q queue=%+v", a.prompts, a.queue)
	}

	wash := &App{platformAuto: true, tab: pageWash, statePage: pageWash, prompts: "去掉水印"}
	wt := ui.NewTester(wash.view, 1280, 800)
	if err := wt.Click("新建任务"); err != nil {
		t.Fatal(err)
	}
	if wash.prompts != DefaultWashPrompt || len(wash.queue) != 0 {
		t.Fatalf("wash reset = %q queue=%d", wash.prompts, len(wash.queue))
	}
}

func TestNewTaskKeepsDraftWhenDatabaseClosed(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	a := &App{store: st, prompts: "未保存草稿", historyOpen: true}
	if !a.persistWorkspace(pageHome) {
		t.Fatal("保存初始草稿失败")
	}
	id := a.genSession
	a.prompts = "刚修改的草稿"
	st.Close()
	a.newWorkspace()
	if a.prompts != "刚修改的草稿" || a.genSession != id || !a.historyOpen {
		t.Fatalf("保存失败后草稿被清空: prompts=%q id=%d history=%v", a.prompts, a.genSession, a.historyOpen)
	}
}

func TestWorkspaceSwitchKeepsDraftWhenWriteFails(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		for _, stage := range []string{"保存", "收起"} {
			name := "新建/" + stage
			if reopen {
				name = "恢复/" + stage
			}
			t.Run(name, func(t *testing.T) {
				st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				history, err := st.SaveActive(store.Session{Page: store.PageGen, Prompts: "历史提示词"}, nil)
				if err != nil {
					t.Fatal(err)
				}
				a := &App{store: st, prompts: "当前提示词", historyOpen: true}
				if !a.persistWorkspace(pageHome) {
					t.Fatal("保存初始草稿失败")
				}
				id := a.genSession
				a.prompts = "刚修改的草稿"
				a.queue = []tasks.Task{{Index: 1, Platform: "豆包", Prompt: a.prompts, Status: tasks.StatusFailed, Error: "原始错误"}}
				db, err := sql.Open("sqlite", st.File())
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				when := "NEW.active=1"
				if stage == "收起" {
					when = "NEW.active=0 AND OLD.active=1"
				}
				if _, err := db.Exec("CREATE TRIGGER fail_workspace_write BEFORE UPDATE ON sessions WHEN " + when + " BEGIN SELECT RAISE(FAIL, 'write failed'); END"); err != nil {
					t.Fatal(err)
				}
				if reopen {
					a.reopenSession(history)
				} else {
					a.newWorkspace()
				}
				if a.prompts != "刚修改的草稿" || a.genSession != id || !a.historyOpen || len(a.queue) != 1 || a.queue[0].Error != "原始错误" {
					t.Fatalf("写入失败后切换了工作区: prompts=%q id=%d history=%v queue=%+v", a.prompts, a.genSession, a.historyOpen, a.queue)
				}
				current, _, ok, err := st.ActiveSession(store.PageGen)
				if err != nil || !ok || current.ID != id {
					t.Fatalf("活动会话改变: %+v ok=%v err=%v", current, ok, err)
				}
			})
		}
	}
}

func TestNewTaskClearsResultGrid(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "豆包主力", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	img := filepath.Join(dir, "session-cat-result.png")
	writeTinyPNG(t, img)
	eng := tasks.NewEngine(st, nil, nil, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := eng.RunObserved(ctx, tasks.Options{
		Platforms:  []string{"豆包"},
		Prompts:    []string{"一只猫"},
		OutputPath: dir,
	}, func(tasks.Task) {}, func(tasks.Progress) {}, func(_ context.Context, task *tasks.Task) error {
		task.Results = []string{img}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snap := eng.View().Snapshot()
	if len(snap) != 1 || snap[0].Status != tasks.StatusSuccess || len(snap[0].Results) != 1 {
		t.Fatalf("run %+v", snap)
	}
	a := &App{
		store: st, engine: eng, platformAuto: true,
		prompts: "一只猫", queue: snap, runPage: pageHome,
	}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(200 * time.Millisecond)
	if !tt.HasText("session-cat-result.png") {
		t.Fatalf("结果格没有上一批的图: %v", tt.Texts())
	}
	if err := tt.Click("新建任务"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("session-cat-result.png") || len(a.queue) != 0 || a.prompts != "" {
		t.Fatalf("新建任务还留着结果 prompts=%q queue=%+v texts=%v", a.prompts, a.queue, tt.Texts())
	}
	if !tt.HasText("出图结果会出现在这里") {
		t.Fatalf("空结果提示没出来: %v", tt.Texts())
	}
	a.historyOpen = true
	tt.Frame()
	if err := tt.Click("恢复"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.prompts != "一只猫" || len(a.queue) != 1 || len(a.queue[0].Results) != 1 || a.queue[0].Results[0] != img {
		t.Fatalf("历史没有把结果带回来: prompts=%q queue=%+v logs=%v", a.prompts, a.queue, logging.Log.Entries())
	}
	if !tt.HasText("session-cat-result.png") {
		t.Fatalf("恢复后结果格是空的: %v", tt.Texts())
	}
}

func TestHistoryReopenAndDeleteKeepsFiles(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	kept := filepath.Join(dir, "kept.png")
	writeTinyPNG(t, kept)
	victim, err := st.SaveActive(store.Session{
		Page: store.PageGen, Prompts: "要删除的", Images: kept,
	}, []store.SessionTask{{
		Ord: 1, Prompt: "要删除的", Platform: "豆包", Status: "成功", Outputs: kept, At: time.Now().Unix(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ArchiveSession(victim); err != nil {
		t.Fatal(err)
	}
	a := &App{store: st, platformAuto: true, prompts: "留下的", historyOpen: true}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Advance(80 * time.Millisecond)
	if !tt.HasText("要删除的") {
		t.Fatalf("history missing, texts %v", tt.Texts())
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("delete removed the image: %v", err)
	}
	if _, _, err := st.Session(victim); err == nil {
		t.Fatal("deleted session still loads")
	}
	if a.prompts != "留下的" {
		t.Fatalf("delete changed the current prompt %q", a.prompts)
	}

	restoreID, err := st.SaveActive(store.Session{Page: store.PageGen, Prompts: "历史提示词"}, []store.SessionTask{{
		Ord: 1, Prompt: "历史提示词", Platform: "豆包", Status: "等待", At: time.Now().Unix(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ArchiveSession(restoreID); err != nil {
		t.Fatal(err)
	}
	a.historyOpen = true
	tt.Frame()
	if err := tt.Click("恢复"); err != nil {
		t.Fatal(err)
	}
	if a.prompts != "历史提示词" || len(a.queue) != 1 || a.queue[0].Prompt != "历史提示词" {
		t.Fatalf("reopen = %q %+v", a.prompts, a.queue)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("reopen removed the image: %v", err)
	}
}

func TestHistoryClickStaysOpen(t *testing.T) {
	a := &App{
		platformAuto: true, prompts: "当前提示词",
		browserRows: []store.Browser{loggedLane(1, "豆包主力", "豆包")},
	}
	tt := ui.NewTester(a.view, 1024, 700)
	tt.Advance(80 * time.Millisecond)
	if _, ok := tt.Find("历史任务"); ok {
		t.Fatal("panel is open before the click")
	}
	if err := tt.Click("历史"); err != nil {
		t.Fatal(err)
	}
	panel, ok := tt.Find("历史任务")
	if !ok || !a.historyOpen {
		t.Fatal("opening click closed the history panel")
	}
	tt.Advance(300 * time.Millisecond)
	panel, ok = tt.Find("历史任务")
	if !ok || !a.historyOpen {
		t.Fatal("history panel closed after opening")
	}
	add, ok := tt.Find("+ 添加账号")
	if !ok {
		t.Fatal("add account missing")
	}
	if rectsOverlap(panel, add) {
		t.Fatalf("panel covers add: panel %v add %v", panel, add)
	}
	row, ok := tt.Find("车道豆包主力")
	if !ok {
		t.Fatal("account row missing")
	}
	if rectsOverlap(panel, row) {
		t.Fatalf("panel covers account row: panel %v row %v", panel, row)
	}
}

func TestHistoryPopupClearsHeaderAndLog(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id, err := st.SaveActive(store.Session{Page: store.PageGen, Prompts: "傍晚窗边的一只猫"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ArchiveSession(id); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		logging.Log.Info("日志对齐行 %02d", i)
	}
	for _, name := range []string{"豆包主力", "豆包小号"} {
		row, err := st.AddBrowser("豆包", name, "")
		if err != nil {
			t.Fatal(err)
		}
		st.UpdateBrowser(row.ID, false, true)
	}
	a := &App{
		store: st, platformAuto: true, historyOpen: true, prompts: "当前提示词",
	}
	for _, sz := range [][2]int{{1024, 700}, {1280, 800}} {
		tt := ui.NewTester(a.view, sz[0], sz[1])
		tt.Advance(120 * time.Millisecond)
		panel, ok := tt.Find("历史任务")
		if !ok {
			t.Fatalf("%v: history panel missing", sz)
		}
		add, ok := tt.Find("+ 添加账号")
		if !ok {
			t.Fatal("添加账号 missing")
		}
		logBtn, ok := tt.Find("运行日志")
		if !ok {
			t.Fatal("运行日志 button missing")
		}
		if logBtn.X <= float32(sz[0])/2 || logBtn.Y+logBtn.H > topbarH+2 {
			t.Fatalf("%v: log trigger not in topbar right: %v", sz, logBtn)
		}
		if rectsOverlap(panel, add) {
			t.Fatalf("%v: history covers 添加账号 panel %v add %v", sz, panel, add)
		}
		if rectsOverlap(panel, logBtn) {
			t.Fatalf("%v: history covers 运行日志 panel %v log %v", sz, panel, logBtn)
		}
		if panel.Y+panel.H > add.Y+1 {
			t.Fatalf("%v: history is not above 添加账号 panel %v add %v", sz, panel, add)
		}
		for _, name := range []string{"豆包主力", "豆包小号"} {
			row, found := tt.Find("车道" + name)
			if !found {
				t.Fatalf("%v: account row %s missing", sz, name)
			}
			if rectsOverlap(panel, row) {
				t.Fatalf("%v: history covers %s panel %v row %v", sz, name, panel, row)
			}
			if row.Y+1 < panel.Y+panel.H {
				t.Fatalf("%v: %s is not below history panel %v row %v", sz, name, panel, row)
			}
			if row.Y < 0 || row.Y+row.H > float32(sz[1])+1 {
				t.Fatalf("%v: %s left the window row %v", sz, name, row)
			}
		}
		for _, label := range []string{"今日", "操作"} {
			cell, found := tt.Find(label)
			if !found {
				t.Fatalf("%v: %s missing", sz, label)
			}
			if rectsOverlap(panel, cell) {
				t.Fatalf("%v: history covers %s panel %v cell %v", sz, label, panel, cell)
			}
		}
		if err := tt.Click("运行日志"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(150 * time.Millisecond)
		if _, ok := tt.Find("运行日志区"); !ok {
			t.Fatal("运行日志 popover missing")
		}
		scroll, ok := tt.Find("运行日志内容")
		if !ok {
			t.Fatal("log scroll missing")
		}
		var saw bool
		for _, text := range tt.Texts() {
			if !strings.HasPrefix(text, "日志对齐行") {
				continue
			}
			line, found := tt.Find(text)
			if !found {
				continue
			}
			if line.Y < scroll.Y-0.5 && line.Y+line.H > scroll.Y+1 {
				t.Fatalf("%v: log line %q sliced line %v scroll %v", sz, text, line, scroll)
			}
			if line.Y >= scroll.Y-0.5 && line.Y+line.H <= scroll.Y+scroll.H+0.5 {
				saw = true
			}
		}
		if !saw {
			t.Fatalf("%v: no full log line inside %v", sz, scroll)
		}
		if err := tt.Click("运行日志"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(120 * time.Millisecond)
	}
}

func TestResultFolderButtonSitsBesideTheName(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "猫.png")
	writeTinyPNG(t, out)
	a := &App{platformAuto: true, queue: []tasks.Task{{
		Index: 1, Platform: "豆包", Status: tasks.StatusSuccess, Prompt: "一只猫",
		Results: []string{out},
	}}}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Advance(80 * time.Millisecond)
	btn, ok := tt.Find("在文件夹中显示")
	if !ok {
		t.Fatal("folder button missing")
	}
	name, ok := tt.Find("猫.png")
	if !ok {
		t.Fatal("filename missing")
	}
	tile, ok := tt.Find("结果图")
	if !ok {
		t.Fatal("tile missing")
	}
	if btn.W > 36 || btn.H > 36 || btn.W > tile.W*0.5 {
		t.Fatalf("folder button is not compact: btn %v tile %v", btn, tile)
	}
	if btn.X <= name.X || btn.Y+btn.H < name.Y-2 || name.Y+name.H < btn.Y-2 {
		t.Fatalf("folder button is not beside the name: btn %v name %v", btn, name)
	}
	for _, text := range tt.Texts() {
		if text == "文件夹" {
			t.Fatal("tile-width 文件夹 label is still shown")
		}
	}
}

func TestMissingResultFilePlaceholder(t *testing.T) {
	a := &App{queue: []tasks.Task{{
		Index: 1, Platform: "豆包", Status: tasks.StatusSuccess, Prompt: "一只猫",
		Results: []string{filepath.Join(t.TempDir(), "gone.png")},
	}}}
	tt := ui.NewTester(a.view, 1280, 800)
	if !tt.HasText("文件已移除") {
		t.Fatalf("missing placeholder, texts %v", tt.Texts())
	}
	n := 0
	for _, text := range tt.Texts() {
		if text == "结果图" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("missing file should still be a result tile, saw %d", n)
	}
}

func TestFailedRetryRequeues(t *testing.T) {
	fakeInstalledCamoufox(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddBrowser("豆包", "豆包主力", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, false, true)
	eng := tasks.NewEngine(st, nil, nil, "")
	a := &App{
		store: st, engine: eng, platformAuto: true,
		queue: []tasks.Task{{
			Index: 1, Platform: "豆包", BrowserID: row.ID, Status: tasks.StatusFailed,
			Prompt: "一只猫", Error: "出图失败",
		}},
	}
	tt := ui.NewTester(a.view, 1280, 800)
	if err := tt.Click("重试"); err != nil {
		t.Fatal(err)
	}
	finishRecoveryRun(t, a)
	if view := eng.View(); view == nil {
		t.Fatal("retry did not create a run")
	} else if queue := view.Snapshot(); len(queue) != 1 || queue[0].Prompt != "一只猫" || queue[0].Attempts != 1 {
		t.Fatalf("retry did not execute the original prompt: %+v", queue)
	}
}

func TestEmptyResultHugsOneAccount(t *testing.T) {
	a := &App{
		platformAuto: true, concurrencyAuto: true,
		browserRows: []store.Browser{loggedLane(1, "豆包主力", "豆包")},
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Advance(200 * time.Millisecond)
	lane, ok := tt.Find("车道豆包主力")
	if !ok {
		t.Fatal("lane missing")
	}
	empty, ok := tt.Find("出图结果会出现在这里")
	if !ok {
		t.Fatal("empty hint missing")
	}
	if empty.Y > lane.Y+lane.H+120 {
		t.Fatalf("empty hint sits too far below the only account: lane=%v empty=%v", lane, empty)
	}
}

func TestShutdownSavesActiveSessionBeforeStoppingEngine(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.db")
	st, err := store.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	img := filepath.Join(t.TempDir(), "src.png")
	writeTinyPNG(t, img)

	eng := tasks.NewEngine(st, nil, nil, "")
	a := &App{
		store:     st,
		engine:    eng,
		tab:       pageWash,
		statePage: pageWash,
		runPage:   pageWash,
		running:   true,
		prompts:   "洗图",
		sources:   []sourceImage{{Path: img, Washing: true}},
		queue: []tasks.Task{{
			Index:    1,
			Platform: "豆包",
			Prompt:   "洗图",
			Images:   []string{img},
			Status:   tasks.StatusRunning,
		}},
		progText: "0 / 1",
	}

	// Simulate OnQuit: snapshot active workspace while DB is still open, then mark quitting and clear queued UI callbacks.
	a.saveSettings()
	a.mu.Lock()
	a.quitting = true
	a.uiUpdates = nil
	a.mu.Unlock()

	// Late postUI callbacks arriving after quitting must be ignored and must not overwrite the active snapshot.
	a.postUI(func() {
		a.running = false
		a.queue[0].Status = tasks.StatusSkipped
		a.queue[0].Error = "已停止"
		a.persistWorkspace(pageWash)
	})
	a.flushUIUpdates()
	eng.Shutdown()
	st.Close()

	reopened, err := store.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored := &App{store: reopened}
	restored.loadSettings()
	ws := restored.stateOf(pageWash)
	if len(ws.queue) != 1 || ws.queue[0].Status != tasks.StatusNeedsReview {
		t.Fatalf("expected interrupted task to restore as NeedsReview, got %+v", ws.queue)
	}
}

func TestWashSoloPathsSelectsModerationUnwashed(t *testing.T) {
	a := &App{
		statePage: pageWash,
		sources: []sourceImage{
			{Path: "/in/mod.png", Washed: false},
			{Path: "/in/washed_mod.png", Washed: true},
			{Path: "/in/timeout.png", Washed: false},
			{Path: "/in/recovered_reason.png", Washed: false},
		},
		queue: []tasks.Task{
			{Images: []string{"/in/mod.png"}, Status: tasks.StatusFailed, Error: "E_MODERATION: 审核不通过"},
			{Images: []string{"/in/washed_mod.png"}, Status: tasks.StatusFailed, Error: "E_MODERATION: 审核不通过"},
			{Images: []string{"/in/timeout.png"}, Status: tasks.StatusFailed, Error: "出图超时"},
			{Images: []string{"/in/recovered_reason.png"}, Status: tasks.StatusFailed, Error: "E_MODERATION: 审核不通过"},
			{Images: []string{"/in/recovered_reason.png"}, Status: tasks.StatusFailed, Error: "出图超时"},
		},
	}
	got := a.washSoloPaths()
	want := []string{"/in/mod.png"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("washSoloPaths() = %v, want %v", got, want)
	}
}
