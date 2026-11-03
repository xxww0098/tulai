package tasks

import (
	"context"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/xxww0098/tulai/internal/store"
)

func TestManualAccountWaitsAndResumesWithoutChangingLogin(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Builtin")
	row, err := st.AddBrowser("豆包", "豆包1", "saved-profile")
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(row.ID)
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(st, nil, nil, "")
		e.SetAccountManual(row.ID, true)
		started := make(chan int64, 1)
		finished := make(chan error, 1)
		go func() {
			finished <- e.RunObserved(context.Background(), Options{Platforms: []string{"豆包"}, Prompts: []string{"test"}}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
				started <- task.BrowserID
				return nil
			})
		}()
		synctest.Wait()
		select {
		case id := <-started:
			t.Fatalf("manual account received a task: %d", id)
		default:
		}
		e.SetAccountManual(row.ID, false)
		synctest.Wait()
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
		if id := <-started; id != row.ID {
			t.Fatalf("resumed account = %d", id)
		}
		got := st.Browsers()[0]
		if !got.IsLoggedIn || got.Health != store.HealthReady || got.UserDataDir != "saved-profile" {
			t.Fatalf("manual mode changed the stored account: %+v", got)
		}
	})
}

func TestManualAccountDoesNotBlockOtherAccounts(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Set("browser_backend", "Builtin")
	first, _ := st.AddBrowser("豆包", "豆包1", "")
	second, _ := st.AddBrowser("豆包", "豆包2", "")
	e := NewEngine(st, nil, nil, "")
	e.SetAccountManual(first.ID, true)
	err = e.RunObserved(context.Background(), Options{Platforms: []string{"豆包"}, Prompts: []string{"test"}}, func(Task) {}, func(Progress) {}, func(_ context.Context, task *Task) error {
		if task.BrowserID != second.ID {
			t.Errorf("task assigned to %d instead of free account %d", task.BrowserID, second.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestManualGatePreventsDispatchRaceAndHonorsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(nil, nil, nil, "")
		e.SetAccountManual(1, true)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan bool, 1)
		go func() { done <- e.enterAccount(ctx, 1) }()
		time.Sleep(time.Second)
		if e.AccountBusy(1) {
			t.Fatal("manual account became active")
		}
		cancel()
		synctest.Wait()
		if <-done {
			t.Fatal("canceled worker entered manual account")
		}
	})
}
