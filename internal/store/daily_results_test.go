package store

import (
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestDailyResultsUpgradePersistAndRollOver(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 11, 3, 12, 0, 0, 0, time.Local)
	if _, err := old.Exec(`CREATE TABLE browsers (id INTEGER PRIMARY KEY, platform TEXT NOT NULL, name TEXT NOT NULL, port INTEGER NOT NULL, user_data_dir TEXT, is_running INTEGER DEFAULT 0, is_logged_in INTEGER DEFAULT 0, day_key TEXT, day_count INTEGER);
		INSERT INTO browsers (id, platform, name, port, day_key, day_count) VALUES (1, '豆包', '甲', 9333, ?, 5)`, today.Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUse(1, today, 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordUse(1, today, 1, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b := browser(t, s, 1)
	success, failed := b.ResultsToday(today)
	if b.UsedToday(today) != 7 || success != 1 || failed != 1 {
		t.Fatalf("restored: %+v success=%d failed=%d", b, success, failed)
	}
	tomorrow := today.AddDate(0, 0, 1)
	if success, failed := b.ResultsToday(tomorrow); b.UsedToday(tomorrow) != 0 || success != 0 || failed != 0 {
		t.Fatalf("next day before write: %+v success=%d failed=%d", b, success, failed)
	}
	if err := s.RecordUse(1, tomorrow, 1, 0, 1); err != nil {
		t.Fatal(err)
	}
	b = browser(t, s, 1)
	if success, failed := b.ResultsToday(tomorrow); b.UsedToday(tomorrow) != 1 || success != 0 || failed != 1 {
		t.Fatalf("next day after write: %+v success=%d failed=%d", b, success, failed)
	}
}

func TestDailyResultsConcurrentWrites(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	b, err := s.AddBrowser("豆包", "甲", "")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.RecordUse(b.ID, now, 1, 1, 0); err != nil {
				t.Errorf("record use: %v", err)
			}
		}()
	}
	wg.Wait()
	b = browser(t, s, b.ID)
	if success, failed := b.ResultsToday(now); b.UsedToday(now) != 20 || success != 20 || failed != 0 {
		t.Fatalf("concurrent result: %+v success=%d failed=%d", b, success, failed)
	}
}
