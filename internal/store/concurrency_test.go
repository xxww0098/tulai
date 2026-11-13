package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/logging"
)

func openConcurrentStores(t *testing.T, n int) []*Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.db")
	stores := make([]*Store, n)
	for i := range stores {
		var err error
		stores[i], err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { stores[i].Close() })
	}
	return stores
}

func TestSettingWriteFailureIsReportedWithoutLoggingValue(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChecked("test_private_setting", "do-not-log-this-value"); err == nil {
		t.Fatal("failed write was silently discarded")
	}
	entries := logging.Log.Entries()
	if len(entries) == 0 {
		t.Fatal("failed write did not create a log entry")
	}
	last := entries[len(entries)-1]
	if last.Level != logging.Error || !strings.Contains(last.Text, "test_private_setting") || strings.Contains(last.Text, "do-not-log-this-value") {
		t.Fatalf("unsafe or missing write error: %+v", last)
	}
}

func TestAccountDatabaseFailuresAreLogged(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s.UpdateBrowserHealth(7, HealthExhausted, 0, "private reason")
	entries := logging.Log.Entries()
	if len(entries) == 0 || !strings.Contains(entries[len(entries)-1].Text, "保存账号 7 的健康状态失败") || strings.Contains(entries[len(entries)-1].Text, "private reason") {
		t.Fatal("account write failure was not safely logged")
	}
	if rows := s.Browsers(); rows != nil {
		t.Fatalf("closed store returned accounts: %+v", rows)
	}
	entries = logging.Log.Entries()
	if len(entries) == 0 || !strings.Contains(entries[len(entries)-1].Text, "读取账号列表失败") {
		t.Fatal("account read failure was not logged")
	}
}

func TestWriteTransactionReservesWriterBeforeReading(t *testing.T) {
	stores := openConcurrentStores(t, 2)
	tx, err := stores[0].db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var previous string
	if err := tx.QueryRow(`SELECT value FROM settings WHERE key='daily_cap'`).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := stores[1].db.Exec(`UPDATE settings SET value='7' WHERE key='daily_cap'`)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("another writer completed while read-then-write transaction was open: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	if _, err := tx.Exec(`UPDATE settings SET value='3' WHERE key='daily_cap'`); err != nil {
		t.Fatalf("read-then-write transaction failed: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("waiting writer failed: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("writer did not resume after the transaction committed")
	}
	if got := stores[0].Get("daily_cap"); got != "7" {
		t.Fatalf("last committed cap = %q", got)
	}
}

func TestReadSnapshotDoesNotBlockWriter(t *testing.T) {
	stores := openConcurrentStores(t, 2)
	tx, err := stores[0].db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var before string
	if err := tx.QueryRow(`SELECT value FROM settings WHERE key='daily_cap'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- stores[1].SetChecked("daily_cap", "9") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a read-only snapshot blocked a writer in WAL mode")
	}
	var during string
	if err := tx.QueryRow(`SELECT value FROM settings WHERE key='daily_cap'`).Scan(&during); err != nil {
		t.Fatal(err)
	}
	if during != before {
		t.Fatalf("read snapshot changed during write: before=%q during=%q", before, during)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := stores[0].Get("daily_cap"); got != "9" {
		t.Fatalf("committed value = %q", got)
	}
}

func TestConcurrentStoresAllocateDistinctAccountPorts(t *testing.T) {
	stores := openConcurrentStores(t, 8)
	start := make(chan struct{})
	errs := make(chan error, len(stores))
	var wg sync.WaitGroup
	for i, s := range stores {
		wg.Add(1)
		go func(i int, s *Store) {
			defer wg.Done()
			<-start
			for j := 0; j < 8; j++ {
				if _, err := s.AddBrowser("豆包", fmt.Sprintf("account-%d-%d", i, j), ""); err != nil {
					errs <- err
					return
				}
			}
		}(i, s)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("add account: %v", err)
	}
	rows := stores[0].Browsers()
	if len(rows) != len(stores)*8 {
		t.Fatalf("accounts=%d want=%d", len(rows), len(stores)*8)
	}
	ports := map[int]bool{}
	for _, row := range rows {
		if ports[row.Port] {
			t.Fatalf("two accounts received port %d", row.Port)
		}
		ports[row.Port] = true
	}
}

func TestConcurrentExtensionRegistrationIsIdempotent(t *testing.T) {
	stores := openConcurrentStores(t, 8)
	start := make(chan struct{})
	type outcome struct {
		id  int64
		err error
	}
	out := make(chan outcome, len(stores))
	for _, s := range stores {
		go func(s *Store) {
			<-start
			row, err := s.UpsertExtensionAccount("豆包", "same-profile", "同一账号")
			out <- outcome{id: row.ID, err: err}
		}(s)
	}
	close(start)
	var id int64
	for range stores {
		r := <-out
		if r.err != nil {
			t.Errorf("concurrent registration: %v", r.err)
			continue
		}
		if id == 0 {
			id = r.id
		} else if id != r.id {
			t.Errorf("same profile received different IDs: %d and %d", id, r.id)
		}
	}
	if rows := stores[0].Browsers(); len(rows) != 1 {
		t.Fatalf("same profile produced %d accounts", len(rows))
	}
}

func TestConcurrentSessionReadIsOneSnapshot(t *testing.T) {
	stores := openConcurrentStores(t, 2)
	makeSession := func(id int64, token string) (Session, []SessionTask) {
		return Session{ID: id, Page: PageWash, Prompts: token, ProgText: token,
				Sources: []SessionSource{{Path: "source.png", Washed: true, Output: token}}},
			[]SessionTask{{Ord: 1, Prompt: token, Source: "source.png", Status: token, Outputs: token}}
	}
	first, items := makeSession(0, "v0")
	id, err := stores[0].SaveActive(first, items)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		<-start
		for i := 1; i <= 250; i++ {
			sess, tasks := makeSession(id, fmt.Sprintf("v%d", i))
			if _, err := stores[1].SaveActive(sess, tasks); err != nil {
				finished <- err
				return
			}
		}
		finished <- nil
	}()
	close(start)
	for i := 0; i < 800; i++ {
		var sess Session
		var tasks []SessionTask
		var err error
		if i%2 == 0 {
			sess, tasks, err = stores[0].Session(id)
		} else {
			var ok bool
			sess, tasks, ok, err = stores[0].ActiveSession(PageWash)
			if err == nil && !ok {
				t.Fatal("active session disappeared")
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(sess.Sources) != 1 || len(tasks) != 1 || sess.Prompts != sess.Sources[0].Output || sess.Prompts != tasks[0].Prompt {
			t.Fatalf("mixed session snapshot: session=%q sources=%+v tasks=%+v", sess.Prompts, sess.Sources, tasks)
		}
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
