package store

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAbruptWriterExitKeepsDatabaseConsistent(t *testing.T) {
	if phase := os.Getenv("TULAI_TEST_CRASH_PHASE"); phase != "" {
		s, err := Open(os.Getenv("TULAI_TEST_CRASH_DB"))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := s.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(`UPDATE settings SET value='37' WHERE key='daily_cap'`); err != nil {
			t.Fatal(err)
		}
		if phase == "committed" {
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
		fmt.Fprintln(os.Stdout, "TULAI_TEST_WRITER_READY")
		for {
			time.Sleep(time.Hour)
		}
	}

	for _, phase := range []string{"uncommitted", "committed"} {
		t.Run(phase, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			account, err := s.AddBrowser("豆包", "保留账号", "")
			if err != nil {
				t.Fatal(err)
			}
			id, err := s.SaveActive(Session{Page: PageWash, Prompts: "保留任务"}, []SessionTask{{Ord: 1, Status: "成功"}})
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
			cmd := exec.Command(exe, "-test.run=^TestAbruptWriterExitKeepsDatabaseConsistent$")
			cmd.Env = append(os.Environ(), "TULAI_TEST_CRASH_PHASE="+phase, "TULAI_TEST_CRASH_DB="+path)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			ready := make(chan string, 1)
			go func() {
				line, _ := bufio.NewReader(stdout).ReadString('\n')
				ready <- line
			}()
			select {
			case line := <-ready:
				if strings.TrimSpace(line) != "TULAI_TEST_WRITER_READY" {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					t.Fatalf("writer failed before ready: %q %s", line, stderr.String())
				}
			case <-time.After(20 * time.Second):
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("writer did not start")
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait() // An abnormal exit is intentional.
			s, err = Open(path)
			if err != nil {
				t.Fatalf("reopen after interrupted writer: %v", err)
			}
			defer s.Close()
			want := "0"
			if phase == "committed" {
				want = "37"
			}
			if got := s.Get("daily_cap"); got != want {
				t.Fatalf("%s value after crash = %q, want %q", phase, got, want)
			}
			browsers := s.Browsers()
			sess, items, ok, err := s.ActiveSession(PageWash)
			if err != nil || !ok || len(browsers) != 1 || browsers[0].ID != account.ID || sess.ID != id || len(items) != 1 || items[0].Status != "成功" {
				t.Fatalf("account or session changed after crash: browsers=%+v session=%+v items=%+v err=%v", browsers, sess, items, err)
			}
			var result string
			if err := s.db.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil || result != "ok" {
				t.Fatalf("integrity=%q err=%v", result, err)
			}
		})
	}
}
