package camoufox

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestInstallLockAcrossProcesses(t *testing.T) {
	if root := os.Getenv("TULAI_TEST_INSTALL_LOCK"); root != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		lock, err := acquireInstallLock(ctx, root, func() { fmt.Println("waiting") })
		if err != nil {
			t.Fatal(err)
		}
		defer releaseInstallLock(lock)
		fmt.Println("acquired")
		return
	}
	root := t.TempDir()
	lock, err := acquireInstallLock(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { releaseInstallLock(lock) }()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestInstallLockAcrossProcesses$")
	cmd.Env = append(os.Environ(), "TULAI_TEST_INSTALL_LOCK="+root)
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	lines := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	select {
	case line := <-lines:
		if line != "waiting" {
			t.Fatalf("second installer did not wait: %q", line)
		}
	case <-ctx.Done():
		t.Fatal("second installer did not reach lock")
	}
	waitCtx, stopWaiting := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, err = acquireInstallLock(waitCtx, root, nil)
	stopWaiting()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled lock wait: %v", err)
	}
	releaseInstallLock(lock)
	lock = nil
	select {
	case line := <-lines:
		if line != "acquired" {
			t.Fatalf("second installer did not acquire released lock: %q", line)
		}
	case <-ctx.Done():
		t.Fatal("second installer did not acquire released lock")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
