package ui

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestReopenCurrentSessionKeepsUnsavedChanges(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, prompts: "已保存草稿", historyOpen: true}
	if !a.persistWorkspace(pageHome) {
		t.Fatal("保存初始草稿失败")
	}
	a.prompts = "未保存的新编辑"
	a.queue = []tasks.Task{{Index: 1, Platform: "豆包", Prompt: a.prompts, Status: tasks.StatusNeedsReview, Results: []string{"新结果.png"}}}
	a.reopenSession(a.genSession)
	if a.prompts != "未保存的新编辑" || len(a.queue) != 1 || len(a.queue[0].Results) != 1 || a.queue[0].Results[0] != "新结果.png" || a.historyOpen {
		t.Fatalf("重新打开当前会话丢失内存内容: prompts=%q queue=%+v history=%v", a.prompts, a.queue, a.historyOpen)
	}
	saved, items, err := st.Session(a.genSession)
	if err != nil || saved.Prompts != a.prompts || len(items) != 1 || items[0].Outputs != "新结果.png" {
		t.Fatalf("当前会话未保存新编辑: session=%+v items=%+v err=%v", saved, items, err)
	}
}

func TestHistoryActivationFailureKeepsCurrentActive(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	history, err := st.SaveActive(store.Session{Page: store.PageGen, Prompts: "目标历史"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ArchiveSession(history); err != nil {
		t.Fatal(err)
	}
	a := &App{store: st, prompts: "当前草稿", historyOpen: true}
	if !a.persistWorkspace(pageHome) {
		t.Fatal("保存当前草稿失败")
	}
	current := a.genSession
	a.prompts = "刚修改的新草稿"
	db, err := sql.Open("sqlite", st.File())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(fmt.Sprintf("CREATE TRIGGER fail_activation BEFORE UPDATE ON sessions WHEN NEW.id=%d AND NEW.active=1 BEGIN SELECT RAISE(FAIL, 'cannot activate'); END", history)); err != nil {
		t.Fatal(err)
	}
	a.reopenSession(history)
	active, _, ok, err := st.ActiveSession(store.PageGen)
	if err != nil || !ok || active.ID != current || active.Prompts != "刚修改的新草稿" || a.genSession != current || a.prompts != "刚修改的新草稿" || !a.historyOpen {
		t.Fatalf("恢复失败改变了活动会话: active=%+v ok=%v err=%v prompts=%q id=%d history=%v", active, ok, err, a.prompts, a.genSession, a.historyOpen)
	}
}

func recoveryEngineWithPreviousResult(t *testing.T, st *store.Store) *tasks.Engine {
	t.Helper()
	row, err := st.AddBrowser("豆包", "fixture", "")
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(row.ID)
	eng := tasks.NewEngine(st, nil, nil, "")
	if err := eng.RunObserved(context.Background(), tasks.Options{Platforms: []string{"豆包"}, Prompts: []string{"旧任务"}}, func(tasks.Task) {}, func(tasks.Progress) {}, func(_ context.Context, task *tasks.Task) error {
		task.Results = []string{"旧结果.png"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return eng
}

func finishRecoveryRun(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for a.running && time.Now().Before(deadline) {
		a.flushUIUpdates()
		time.Sleep(time.Millisecond)
	}
	if a.running {
		t.Fatal("未结束没有可用账号的任务")
	}
}

func TestOtherWorkspaceStartDoesNotRevivePreviousQueue(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	eng := recoveryEngineWithPreviousResult(t, st)
	a := &App{store: st, engine: eng, prompts: "旧任务", queue: eng.View().Snapshot(), runPage: pageHome, outputPath: t.TempDir()}
	a.newWorkspace()
	a.prompts = "新草稿"
	if !a.persistWorkspace(pageHome) {
		t.Fatal("保存新草稿失败")
	}
	current := a.genSession
	a.tab = pageWash
	a.syncPage()
	a.prompts = "洗图新草稿"
	// 没有 Antigravity 账号，执行入口会立即失败，不调用真实 CLI 或浏览器。
	a.runQueue([]string{"Antigravity"}, []string{"新洗图"}, nil, nil)
	finishRecoveryRun(t, a)
	if queue := a.stateOf(pageHome).queue; len(queue) != 0 {
		t.Fatalf("换页启动复活了上一批快照: %+v", queue)
	}
	a.saveSettings()
	session, items, err := st.Session(current)
	if err != nil || len(items) != 0 || session.Prompts != "新草稿" {
		t.Fatalf("新草稿混入旧任务记录: session=%+v items=%+v err=%v", session, items, err)
	}
}

func TestRunPreparationFailureKeepsCurrentRecords(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint(retry), func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			eng := recoveryEngineWithPreviousResult(t, st)
			original := []tasks.Task{{Index: 7, Platform: "Antigravity", BrowserID: 42, Prompt: "新的待核对任务", Status: tasks.StatusNeedsReview, Error: "原始错误", Results: []string{"保留结果.png"}}}
			a := &App{store: st, engine: eng, prompts: "当前草稿", queue: original, progress: 0.5, outputPath: t.TempDir()}
			if !a.persistWorkspace(pageHome) {
				t.Fatal("保存原始记录失败")
			}
			var failed []tasks.Task
			if retry {
				failed = original
			}
			a.runQueue([]string{"Antigravity"}, []string{"新生成"}, nil, failed)
			for _, task := range a.queueItems() {
				if task.Prompt == "旧任务" {
					t.Fatalf("启动确认前显示了上一次引擎队列: %+v", task)
				}
			}
			finishRecoveryRun(t, a)
			if !reflect.DeepEqual(a.queue, original) || a.progress != 0.5 || a.progText == "" {
				t.Fatalf("启动准备失败覆盖了当前记录: queue=%+v progress=%v error=%q", a.queue, a.progress, a.progText)
			}
			_, items, err := st.Session(a.genSession)
			if err != nil || len(items) != 1 || items[0].Status != string(tasks.StatusNeedsReview) || items[0].Outputs != "保留结果.png" || items[0].Prompt != original[0].Prompt {
				t.Fatalf("启动准备失败丢失了落库记录: items=%+v err=%v", items, err)
			}
		})
	}
}

func TestInitialWorkspaceSaveFailureDoesNotStartRun(t *testing.T) {
	for _, retry := range []bool{false, true} {
		t.Run(fmt.Sprint(retry), func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			eng := recoveryEngineWithPreviousResult(t, st)
			original := []tasks.Task{{Index: 7, Platform: "Antigravity", BrowserID: 42, Prompt: "待核对任务", Status: tasks.StatusNeedsReview, Results: []string{"保留结果.png"}}}
			a := &App{store: st, engine: eng, prompts: "当前草稿", queue: original, progress: 0.5, runPage: pageWash, outputPath: t.TempDir()}
			if !a.persistWorkspace(pageHome) {
				t.Fatal("保存初始草稿失败")
			}
			db, err := sql.Open("sqlite", st.File())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec("CREATE TRIGGER fail_run_save BEFORE UPDATE ON sessions BEGIN SELECT RAISE(FAIL, 'cannot save'); END"); err != nil {
				t.Fatal(err)
			}
			before := eng.View()
			var failed []tasks.Task
			if retry {
				failed = original
			}
			a.runQueue([]string{"Antigravity"}, []string{"新生成"}, nil, failed)
			if a.running || a.useRunView || a.runPage != pageWash || !reflect.DeepEqual(a.queue, original) || a.progress != 0.5 || eng.View() != before || !strings.Contains(a.progText, "保存") {
				t.Fatalf("保存失败后仍启动或改变任务: running=%v view=%v runPage=%d queue=%+v progress=%v error=%q", a.running, a.useRunView, a.runPage, a.queue, a.progress, a.progText)
			}
		})
	}
}
