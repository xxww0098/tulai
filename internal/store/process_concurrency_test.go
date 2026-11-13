package store

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// This exercises separate processes, rather than only the per-Store mutex.
func TestCrossProcessWriteStability(t *testing.T) {
	const workers, iterations = 4, 40
	if worker := os.Getenv("TULAI_TEST_STORE_WORKER"); worker != "" {
		id, err := strconv.Atoi(worker)
		if err != nil {
			t.Fatal(err)
		}
		accountID, err := strconv.ParseInt(os.Getenv("TULAI_TEST_STORE_ACCOUNT"), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		s, err := Open(os.Getenv("TULAI_TEST_STORE_PATH"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		var sessionID int64
		at := time.Date(2026, time.November, 14, 10, 0, 0, 0, time.Local)
		for i := 0; i < iterations; i++ {
			if err := s.SetChecked(fmt.Sprintf("test_worker_%d", id), strconv.Itoa(i)); err != nil {
				t.Fatal(err)
			}
			if err := s.RecordUse(accountID, at, 1, 1, 0); err != nil {
				t.Fatal(err)
			}
			value := fmt.Sprintf("worker-%d-step-%d", id, i)
			sessionID, err = s.SaveActive(Session{ID: sessionID, Page: PageWash, Prompts: value,
				Sources: []SessionSource{{Path: fmt.Sprintf("%d.png", id), Washed: true, Output: value}}},
				[]SessionTask{{Ord: 1, Prompt: value, Status: "成功", Outputs: value}})
			if err != nil {
				t.Fatal(err)
			}
		}
		return
	}

	path := filepath.Join(t.TempDir(), "config.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.AddBrowser("豆包", "并发账号", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	var commands []*exec.Cmd
	outputs := make([]bytes.Buffer, workers)
	for i := 0; i < workers; i++ {
		cmd := exec.CommandContext(ctx, exe, "-test.run=^TestCrossProcessWriteStability$")
		cmd.Env = append(os.Environ(), "TULAI_TEST_STORE_WORKER="+strconv.Itoa(i+1),
			"TULAI_TEST_STORE_PATH="+path, "TULAI_TEST_STORE_ACCOUNT="+strconv.FormatInt(b.ID, 10))
		cmd.Stdout, cmd.Stderr = &outputs[i], &outputs[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, cmd)
	}
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i, cmd := range commands {
		wg.Add(1)
		go func(i int, cmd *exec.Cmd) {
			defer wg.Done()
			if err := cmd.Wait(); err != nil {
				errs <- fmt.Errorf("worker %d: %w: %s", i+1, err, outputs[i].String())
			}
		}(i, cmd)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	accounts := s.Browsers()
	if len(accounts) != 1 || accounts[0].DayCount != workers*iterations || accounts[0].DaySuccess != workers*iterations {
		t.Fatalf("lost concurrent account counts: %+v", accounts)
	}
	for i := 1; i <= workers; i++ {
		if got := s.Get(fmt.Sprintf("test_worker_%d", i)); got != strconv.Itoa(iterations-1) {
			t.Fatalf("worker %d final setting = %q", i, got)
		}
	}
	var sessionCount, taskCount, sourceCount int
	for query, target := range map[string]*int{
		`SELECT COUNT(*) FROM sessions WHERE page='wash'`:       &sessionCount,
		`SELECT COUNT(*) FROM session_tasks`:                    &taskCount,
		`SELECT COUNT(*) FROM session_sources WHERE washed = 1`: &sourceCount,
	} {
		if err := s.db.QueryRow(query).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if sessionCount != workers || taskCount != workers || sourceCount != workers {
		t.Fatalf("lost sessions/tasks/sources: %d/%d/%d", sessionCount, taskCount, sourceCount)
	}
	var integrity string
	if err := s.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%q err=%v", integrity, err)
	}
}
