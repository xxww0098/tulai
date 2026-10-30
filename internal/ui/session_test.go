package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

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
	if b.genSession != a.genSession || b.prompts != "窗边的猫" || len(b.sources) != 1 || b.sources[0].Path != img {
		t.Fatalf("生图恢复失败 prompts=%q sources=%v id=%d", b.prompts, b.sources, b.genSession)
	}
	if len(b.queue) != 1 || b.queue[0].Status != tasks.StatusSuccess || len(b.queue[0].Results) != 1 || b.queue[0].Results[0] != out || b.progText != "1 / 1" {
		t.Fatalf("生图任务恢复失败 %+v %q", b.queue, b.progText)
	}
	w := b.stateOf(pageWash)
	if b.washSession != washID || w.prompts != "白底" || len(w.sources) != 1 || w.sources[0].Path != img {
		t.Fatalf("洗图恢复失败 %+v id=%d", w, b.washSession)
	}
	if len(w.queue) != 1 || w.queue[0].Status != tasks.StatusFailed || w.queue[0].Error != "上传失败" || w.queue[0].Images[0] != img {
		t.Fatalf("洗图任务恢复失败 %+v", w.queue)
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
	if len(a.queue) != 1 || a.queue[0].Status != tasks.StatusPending || a.queue[0].Prompt != "一只猫" {
		t.Fatalf("retry did not requeue: %+v", a.queue)
	}
	deadline := time.Now().Add(3 * time.Second)
	for eng.IsRunning() && time.Now().Before(deadline) {
		time.Sleep(15 * time.Millisecond)
	}
	if eng.IsRunning() {
		eng.Stop()
		t.Fatal("retry engine did not finish")
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
