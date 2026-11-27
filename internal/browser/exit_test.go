package browser

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

func fixtureManager(t *testing.T) (*Manager, string) {
	t.Helper()
	t.Setenv("TULAI_BROWSER_PIPE_FIXTURE", "none")
	root := fakeLauncherRuntime(t)
	manager := NewManager()
	manager.configDir = root
	t.Cleanup(manager.StopAll)
	return manager, root
}

func fixtureAccount(t *testing.T, dataRoot string, id int64, port int) store.Browser {
	t.Helper()
	return store.Browser{ID: id, Port: port, Name: "fixture", UserDataDir: ProfileDir(dataRoot, KindFingerprint, "fixture-"+strconv.FormatInt(id, 10))}
}

// 用户关掉窗口时启动器进程随之退出，界面必须收到这个退出，才能把「停止」换回「打开」。
func TestClosedBrowserIsReportedAndForgotten(t *testing.T) {
	manager, root := fixtureManager(t)
	exited := make(chan int64, 4)
	manager.SetExitHandler(func(id int64) { exited <- id })
	account := fixtureAccount(t, root, 11, 49982)
	if _, err := manager.StartTask(context.Background(), account, root, KindFingerprint, "about:blank"); err != nil {
		t.Fatal(err)
	}
	if !manager.IsRunning(account.ID) {
		t.Fatal("fixture browser did not start")
	}
	proc, ok := manager.instances.Get(account.ID)
	if !ok || proc.Process == nil {
		t.Fatal("missing process record")
	}
	handle, err := os.FindProcess(proc.PID())
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-exited:
		if id != account.ID {
			t.Fatalf("exit handler reported account %d, want %d", id, account.ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a closed window never reached the exit handler")
	}
	if manager.IsRunning(account.ID) {
		t.Fatal("a closed browser is still reported as running")
	}
	if _, kept := manager.instances.Get(account.ID); kept {
		t.Fatal("a closed browser left its process record behind")
	}
}

// 主动停止是应用自己关的浏览器，不能当成用户关窗口上报。
func TestStoppedBrowserKeepsTheExitHandlerQuiet(t *testing.T) {
	manager, root := fixtureManager(t)
	exited := make(chan int64, 4)
	manager.SetExitHandler(func(id int64) { exited <- id })
	account := fixtureAccount(t, root, 12, 49983)
	if _, err := manager.StartTask(context.Background(), account, root, KindFingerprint, "about:blank"); err != nil {
		t.Fatal(err)
	}
	proc, ok := manager.instances.Get(account.ID)
	if !ok || proc.Process == nil {
		t.Fatal("missing process record")
	}
	manager.Stop(account.ID)
	select {
	case <-proc.Context().Done():
	case <-time.After(10 * time.Second):
		t.Fatal("stopping an account left its process running")
	}
	select {
	case id := <-exited:
		t.Fatalf("stopping account %d was reported as a closed window", id)
	case <-time.After(300 * time.Millisecond):
	}
}

// 换形态重开（例如从可见窗口切成任务后台）会替换进程，同样不能上报成关窗口。
func TestReopenedBrowserIsNotReportedAsClosed(t *testing.T) {
	manager, root := fixtureManager(t)
	exited := make(chan int64, 4)
	manager.SetExitHandler(func(id int64) { exited <- id })
	account := fixtureAccount(t, root, 13, 49984)
	if _, err := manager.Start(account, root, KindFingerprint, "about:blank", false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.StartTask(context.Background(), account, root, KindFingerprint, "about:blank"); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-exited:
		t.Fatalf("reopening account %d was reported as a closed window", id)
	case <-time.After(500 * time.Millisecond):
	}
	if !manager.IsRunning(account.ID) {
		t.Fatal("the reopened browser is not running")
	}
}
