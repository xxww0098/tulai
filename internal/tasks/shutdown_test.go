package tasks

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/extbridge"
	"github.com/xxww0098/tulai/internal/logging"
)

func TestShutdownStopsRunAndWaitsBeforeStoreClose(t *testing.T) {
	st := openRotateStore(t)
	st.Set(extbridge.SettingBrowserBackend, "camoufox_extension")
	st.Set(extbridge.SettingExtPort, freePort(t))
	addNamed(t, st, "豆包", "豆包2")

	e := NewEngine(st, nil, nil, "")
	t.Cleanup(func() { e.Shutdown() })

	release := make(chan struct{})
	defer close(release)
	sess := dialFakeExt(t, extbridge.PlatformDoubao, func(env extbridge.Envelope) extbridge.Envelope {
		name, _ := env.Name()
		if name == extbridge.CmdWaitResult {
			select {
			case <-release:
			case <-time.After(10 * time.Second):
			}
		}
		return ackOK(env, nil)
	})

	var sources []string
	for i := 1; i <= 15; i++ {
		sources = append(sources, fmt.Sprintf("img-%02d.png", i))
	}

	var logMu sync.Mutex
	var logged []string
	logging.Log.SetSink(func(entry logging.Entry) {
		logMu.Lock()
		logged = append(logged, entry.Text)
		logMu.Unlock()
	})
	defer logging.Log.SetSink(nil)

	inWait := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		done <- e.RunObserved(context.Background(), Options{
			Platforms:   []string{"豆包"},
			Prompts:     []string{"洗图"},
			Images:      sources,
			Wash:        true,
			WaitResult:  true,
			Concurrency: 1,
		}, func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
			select {
			case inWait <- struct{}{}:
			default:
			}
			_, err := extCall(ctx, sess, extbridge.CmdWaitResult, "1", "", map[string]any{"timeoutMs": 10000}, 10*time.Second, "豆包")
			return err
		})
	}()

	select {
	case <-inWait:
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not enter extCall")
	}

	e.Shutdown()
	_ = st.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected run error after Shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("engine run did not finish before Shutdown returned")
	}

	if e.IsRunning() {
		t.Fatal("engine still reports running after Shutdown")
	}
	for _, task := range e.View().Snapshot() {
		if task.Status != StatusSkipped || task.Error != "已停止" {
			t.Fatalf("task %d expected skipped/stopped, got %+v", task.Index, task)
		}
	}

	logMu.Lock()
	snapshotLogs := append([]string(nil), logged...)
	logMu.Unlock()
	for _, line := range snapshotLogs {
		if strings.Contains(line, "database is closed") || strings.Contains(line, extbridge.ENotConnected) {
			t.Fatalf("unexpected shutdown error log: %q", line)
		}
	}

	if err := e.Run(context.Background(), Options{Platforms: []string{"豆包"}, Prompts: []string{"test"}}, func(Task) {}, func(Progress) {}); err == nil {
		t.Fatal("expected new Run to be rejected after Shutdown")
	}
}
