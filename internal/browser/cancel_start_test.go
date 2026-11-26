package browser

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/fingerprint"
	"github.com/xxww0098/tulai/internal/store"
)

// The test binary acts as the private launcher pipe; no browser is started.
func TestMain(m *testing.M) {
	hold := os.Getenv("TULAI_BROWSER_PIPE_FIXTURE")
	if hold == "" {
		os.Exit(m.Run())
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil || request.Method == "cancel" {
			continue
		}
		if request.Method == hold {
			_ = os.WriteFile(os.Getenv("TULAI_BROWSER_PIPE_READY"), nil, 0o600)
			continue
		}
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]string{"targetId": "fixture-page"}})
		if request.Method == "Browser.close" {
			os.Exit(0)
		}
	}
	os.Exit(0)
}

func TestTaskStartCanCancelWhileWaitingForAccount(t *testing.T) {
	manager := NewManager()
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	manager.starts.Store(int64(1), gate)
	defer func() { <-gate }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := manager.StartTask(ctx, store.Browser{ID: 1}, t.TempDir(), KindFingerprint, "about:blank")
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("start bypassed the account lock: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting start = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled start kept waiting for the account")
	}
}

func TestTaskStartCancellationAndProcessLifetime(t *testing.T) {
	root := fakeLauncherRuntime(t)
	for _, hold := range []string{"start", "Page.navigate", "none"} {
		t.Run(hold, func(t *testing.T) {
			t.Setenv("TULAI_BROWSER_PIPE_FIXTURE", hold)
			ready := filepath.Join(t.TempDir(), "ready")
			t.Setenv("TULAI_BROWSER_PIPE_READY", ready)
			manager := NewManager()
			manager.configDir = root
			t.Cleanup(manager.StopAll)
			dataRoot := t.TempDir()
			account := store.Browser{ID: 1, Port: 49981, Name: "fixture", UserDataDir: ProfileDir(dataRoot, KindFingerprint, "fixture")}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := manager.StartTask(ctx, account, dataRoot, KindFingerprint, "https://example.invalid/")
				done <- err
			}()
			if hold != "none" {
				deadline := time.Now().Add(5 * time.Second)
				for {
					if _, err := os.Stat(ready); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("launcher did not reach the held request")
					}
					time.Sleep(5 * time.Millisecond)
				}
				cancel()
			}
			select {
			case err := <-done:
				if hold != "none" {
					if !errors.Is(err, context.Canceled) || manager.IsRunning(account.ID) {
						t.Fatalf("canceled startup left an account running: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("launcher request did not finish after cancellation")
			}
			cancel()
			proc, err := fingerprint.Find(fingerprint.Endpoint(account.Port))
			if err != nil || !proc.Alive() {
				t.Fatalf("task cancellation closed a successfully started process: %v", err)
			}
			if err := proc.Call(context.Background(), proc.Page, "fixture-ping", nil, nil); err != nil {
				t.Fatalf("process did not survive task cancellation: %v", err)
			}
			manager.Stop(account.ID)
			if manager.IsRunning(account.ID) {
				t.Fatal("stopping the account did not release its browser")
			}
		})
	}
}
