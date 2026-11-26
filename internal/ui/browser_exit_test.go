package ui

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func drainTopics(ch <-chan string) []string {
	var topics []string
	for {
		select {
		case topic := <-ch:
			topics = append(topics, topic)
		default:
			return topics
		}
	}
}

// 用户手动关掉账号窗口后，账号行必须从「停止」回到「打开」。
func TestClosedAccountWindowRefreshesTheRow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	row, err := st.AddBrowser("豆包", "账号1", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, true, true)
	a := &App{store: st, manager: browser.NewManager(), browserBackend: browser.KindFingerprint}
	a.refreshBrowsers()
	a.browserRows[0].IsRunning = true
	a.loginWatchGen = map[int64]int{row.ID: 3}
	_, events := a.subscribeEvents()

	a.handleBrowserExit(row.ID)
	a.flushUIUpdates()

	if rows := st.Browsers(); len(rows) != 1 || rows[0].IsRunning {
		t.Fatalf("关闭窗口后账号仍标记为运行: %+v", rows)
	}
	if len(a.browserRows) != 1 || a.browserRows[0].IsRunning {
		t.Fatalf("账号行仍显示运行: %+v", a.browserRows)
	}
	if got := a.loginWatchGen[row.ID]; got != 4 {
		t.Fatalf("关闭窗口后登录轮询仍继续: 代数 = %d", got)
	}
	topics := drainTopics(events)
	if !slices.Contains(topics, "browsers") || !slices.Contains(topics, "workbench") {
		t.Fatalf("关闭窗口没有刷新账号列表: %v", topics)
	}
}

// 刚添加还没登录的账号，关窗口即放弃，和启动时发现窗口已关闭的出口一致。
func TestClosedUnloggedNewAccountIsDropped(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	row, err := st.AddBrowser("豆包", "账号2", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, true, false)
	a := &App{store: st, manager: browser.NewManager(), browserBackend: browser.KindFingerprint, dataRoot: t.TempDir()}
	a.markFreshLogin(row.ID)
	a.refreshBrowsers()

	a.handleBrowserExit(row.ID)
	a.flushUIUpdates()

	if left := st.Browsers(); len(left) != 0 {
		t.Fatalf("未登录的新账号应在关闭窗口后移除: %+v", left)
	}
}

// 定时巡检发现缓存状态和真实运行状态漂移时，应自动同步并通知工作台和账号池。
func TestSyncRunningBrowsersReconcilesDriftedState(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	row, err := st.AddBrowser("豆包", "巡检号", "")
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateBrowser(row.ID, true, true)
	a := &App{store: st, manager: browser.NewManager(), browserBackend: browser.KindFingerprint}
	a.refreshBrowsers()
	a.browserRows[0].IsRunning = true
	_, events := a.subscribeEvents()

	a.syncRunningBrowsers()

	if a.browserRows[0].IsRunning {
		t.Fatalf("巡检后缓存仍显示运行: %+v", a.browserRows)
	}
	if rows := st.Browsers(); len(rows) != 1 || rows[0].IsRunning {
		t.Fatalf("巡检后数据库仍标记运行: %+v", rows)
	}
	topics := drainTopics(events)
	if !slices.Contains(topics, "browsers") || !slices.Contains(topics, "workbench") {
		t.Fatalf("巡检修正状态后未通知刷新: %v", topics)
	}
}
