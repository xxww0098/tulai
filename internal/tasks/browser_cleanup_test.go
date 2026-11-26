package tasks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/cdp"
	"github.com/xxww0098/tulai/internal/fingerprint"
	"github.com/xxww0098/tulai/internal/fingerprinttest"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/strategy"
)

func TestBatchClosesOnlyUsedBrowsers(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "stopped"} {
		t.Run(outcome, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			used, err := st.AddBrowser("豆包", "used", "")
			if err != nil {
				t.Fatal(err)
			}
			other, err := st.AddBrowser("千问", "unrelated", "")
			if err != nil {
				t.Fatal(err)
			}
			st.UpdateBrowser(used.ID, true, true)
			st.UpdateBrowser(other.ID, true, true)
			e := NewEngine(st, browser.NewManager(), nil, t.TempDir())
			err = e.run(context.Background(), Options{Platforms: []string{"豆包"}, Prompts: []string{"test"}},
				func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
					e.mu.Lock()
					e.runBrowsers[task.BrowserID] = true
					e.mu.Unlock()
					switch outcome {
					case "failure":
						return errors.New("test failure")
					case "stopped":
						e.Stop()
						return ctx.Err()
					default:
						return nil
					}
				})
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range st.Browsers() {
				switch row.ID {
				case used.ID:
					if row.IsRunning || !row.IsLoggedIn {
						t.Fatalf("used account should stop and keep its login: %+v", row)
					}
				case other.ID:
					if !row.IsRunning || !row.IsLoggedIn {
						t.Fatalf("unrelated account should be left alone: %+v", row)
					}
				}
			}
		})
	}
}

type cleanupFixtureStrategy struct {
	strategy.Strategy
	home string
	fail bool
}

func (s cleanupFixtureStrategy) HomeURL() string { return s.home }
func (s cleanupFixtureStrategy) InstallHook(*cdp.Session) error {
	if s.fail {
		return errors.New("test hook failure")
	}
	return nil
}

func TestBatchClosesRealHeadlessBrowsers(t *testing.T) {
	config := fingerprinttest.ConfigDir(t)
	fingerprinttest.WithoutProxy(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "kept", Path: "/", MaxAge: 3600})
		}
		if cookie, _ := r.Cookie("sessionid"); cookie != nil {
			fmt.Fprint(w, cookie.Value)
		}
	}))
	t.Cleanup(server.Close)
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var accounts []store.Browser
	for _, name := range []string{"one", "two"} {
		row, err := st.AddBrowser("豆包", name, browser.ProfileDir(root, browser.KindFingerprint, name))
		if err != nil {
			t.Fatal(err)
		}
		st.UpdateBrowser(row.ID, false, true)
		accounts = append(accounts, row)
	}
	mgr := browser.NewManagerWithConfig(config)
	t.Cleanup(mgr.StopAll)
	unrelated := store.Browser{ID: 9001, Name: "unrelated", Port: 49991, UserDataDir: browser.ProfileDir(root, browser.KindFingerprint, "unrelated")}
	if _, err := mgr.Start(unrelated, root, browser.KindFingerprint, server.URL+"/check", true); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(st, mgr, nil, root)
	fixture := cleanupFixtureStrategy{home: server.URL + "/login"}
	started := make(chan int, 2)
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	release := make(chan struct{})
	done := runBrowserCleanupBatch(t, func(ctx context.Context) error {
		return e.run(ctx, Options{Platforms: []string{"豆包"}, Prompts: []string{"one", "two"}, Concurrency: 2},
			func(task Task) {
				if task.Index == 1 && task.Status == StatusSuccess {
					close(firstDone)
				}
			}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				if _, err := e.session(ctx, task.Platform, task.BrowserID, fixture); err != nil {
					return err
				}
				started <- task.Index
				// Hold task 1 until task 2 also claims the second account past sameExitStagger;
				// otherwise a fast Fingerprint startup can finish task 1 before task 2 dispatches.
				if task.Index == 1 {
					select {
					case <-releaseFirst:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				if task.Index == 2 {
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			})
	})
	startDeadline := time.NewTimer(fingerprinttest.StartTimeout + 30*time.Second)
	defer startDeadline.Stop()
	for range 2 {
		select {
		case <-started:
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			t.Fatalf("batch ended before both accounts started: %+v", e.View().Snapshot())
		case <-startDeadline.C:
			t.Fatal("accounts did not start")
		}
	}
	close(releaseFirst)
	select {
	case <-firstDone:
	case <-time.After(5 * time.Second):
		t.Fatal("first task did not finish")
	}
	for _, row := range accounts {
		if !mgr.IsRunning(row.ID) {
			t.Fatal("a browser stopped while the batch was still running")
		}
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("batch did not finish cleanup")
	}
	for _, row := range st.Browsers() {
		if mgr.IsRunning(row.ID) || row.IsRunning || !row.IsLoggedIn {
			t.Fatalf("account did not stop cleanly with its login intact: %+v", row)
		}
	}
	if !mgr.IsRunning(unrelated.ID) {
		t.Fatal("an unrelated browser was closed")
	}
	ep, err := mgr.StartTask(t.Context(), accounts[0], root, browser.KindFingerprint, server.URL+"/check")
	if err != nil {
		t.Fatal(err)
	}
	proc, err := fingerprint.Find(ep)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var out struct{ Result struct{ Value string } }
	if err := proc.Call(ctx, proc.Page, "Runtime.evaluate", map[string]any{"expression": "document.body.textContent"}, &out); err != nil || out.Result.Value != "kept" {
		t.Fatalf("login cookie after restart = %q, %v", out.Result.Value, err)
	}
	mgr.Stop(accounts[0].ID)
	fixture.fail = true
	err = e.run(t.Context(), Options{Platforms: []string{"豆包"}, Prompts: []string{"fail"}, Concurrency: 1},
		func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
			_, err := e.session(ctx, task.Platform, task.BrowserID, fixture)
			return err
		})
	if err != nil || mgr.IsRunning(accounts[0].ID) || mgr.IsRunning(accounts[1].ID) {
		t.Fatalf("browser survived a session setup failure: %v", err)
	}
	if task := e.View().Snapshot()[0]; task.Status != StatusFailed || !strings.Contains(task.Error, "test hook failure") {
		t.Fatalf("expected a session hook failure: %+v", task)
	}
	fixture.fail = false
	stopping := make(chan struct{})
	done = runBrowserCleanupBatch(t, func(ctx context.Context) error {
		return e.run(ctx, Options{Platforms: []string{"豆包"}, Prompts: []string{"stop"}, Concurrency: 1},
			func(Task) {}, func(Progress) {}, func(ctx context.Context, task *Task) error {
				if _, err := e.session(ctx, task.Platform, task.BrowserID, fixture); err != nil {
					return err
				}
				close(stopping)
				<-ctx.Done()
				return ctx.Err()
			})
	})
	select {
	case <-stopping:
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("stop test finished before starting: %+v", e.View().Snapshot())
	case <-time.After(fingerprinttest.StartTimeout + 30*time.Second):
		t.Fatal("stop test did not start")
	}
	e.Stop()
	select {
	case err := <-done:
		if err != nil || mgr.IsRunning(accounts[0].ID) || mgr.IsRunning(accounts[1].ID) || !mgr.IsRunning(unrelated.ID) {
			t.Fatalf("stop did not clean up only the used browser: %v", err)
		}
	case <-time.After(25 * time.Second):
		t.Fatal("stop did not finish cleanup")
	}
}

func runBrowserCleanupBatch(t *testing.T, run func(context.Context) error) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- run(t.Context())
	}()
	t.Cleanup(func() {
		for range done {
		}
	})
	return done
}
