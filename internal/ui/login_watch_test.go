package ui

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
)

func TestWatchLoginClosesFreshAccountAfterLogin(t *testing.T) {
	for _, platform := range []string{"豆包", "千问"} {
		t.Run(platform, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			row, err := st.AddManagedBrowser(platform, platform+"1", filepath.Join(t.TempDir(), "profile"), browser.KindFingerprint)
			if err != nil {
				t.Fatal(err)
			}
			st.SetRunning(row.ID, true)
			states := make(chan string, 2)
			states <- "out"
			probed := make(chan string, 2)
			closed := make(chan struct{}, 1)
			a := &App{
				store:          st,
				manager:        browser.NewManager(),
				browserBackend: browser.KindFingerprint,
				loginPoll:      10 * time.Millisecond,
				wizardProbe: func(ctx context.Context, _ string) (string, error) {
					select {
					case state := <-states:
						probed <- state
						return state, nil
					case <-ctx.Done():
						return "", ctx.Err()
					}
				},
				afterClose: func() { closed <- struct{}{} },
			}
			_, events := a.subscribeEvents()
			a.markFreshLogin(row.ID)
			a.watchLogin(row, loginEndpoint(row))

			select {
			case got := <-probed:
				if got != "out" {
					t.Fatalf("first probe = %q, want out", got)
				}
			case <-time.After(time.Second):
				t.Fatal("login was not probed")
			}
			select {
			case <-closed:
				t.Fatal("browser closed before login")
			case <-time.After(40 * time.Millisecond):
			}

			states <- "in"
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("new account browser stayed open after login")
			}
			a.flushUIUpdates()

			rows := st.Browsers()
			if len(rows) != 1 || !rows[0].IsLoggedIn || rows[0].IsRunning {
				t.Fatalf("account after login: %+v", rows)
			}
			if len(a.browserRows) != 1 || !a.browserRows[0].IsLoggedIn || a.browserRows[0].IsRunning {
				t.Fatalf("cached browserRows after login: %+v", a.browserRows)
			}
			a.mu.Lock()
			gotClosed := append([]int64(nil), a.loginClosed...)
			a.mu.Unlock()
			if !slices.Equal(gotClosed, []int64{row.ID}) {
				t.Fatalf("loginClosed = %v, want [%d]", gotClosed, row.ID)
			}
			topics := drainTopics(events)
			for _, want := range []string{"browsers", "workbench", "shell"} {
				if !slices.Contains(topics, want) {
					t.Fatalf("topics = %v, missing %q", topics, want)
				}
			}
		})
	}
}

func TestWatchLoginClosesUnloggedOrExpiredAccountAfterLogin(t *testing.T) {
	cases := []struct {
		name  string
		setup func(st *store.Store, id int64)
	}{
		{
			name: "existing unlogged",
			setup: func(st *store.Store, id int64) {
				st.UpdateBrowser(id, true, false)
			},
		},
		{
			name: "expired login hold",
			setup: func(st *store.Store, id int64) {
				st.UpdateBrowser(id, true, true)
				st.UpdateBrowserHealth(id, store.HealthExhausted, 0, "账号未登录")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			row, err := st.AddManagedBrowser("豆包", "豆包1", filepath.Join(t.TempDir(), "profile"), browser.KindFingerprint)
			if err != nil {
				t.Fatal(err)
			}
			tc.setup(st, row.ID)
			row = st.Browsers()[0]

			closed := make(chan struct{}, 1)
			a := &App{
				store:          st,
				manager:        browser.NewManager(),
				browserBackend: browser.KindFingerprint,
				loginPoll:      10 * time.Millisecond,
				wizardProbe: func(context.Context, string) (string, error) {
					return "in", nil
				},
				afterClose: func() { closed <- struct{}{} },
			}
			a.watchLogin(row, loginEndpoint(row))
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("unlogged/expired account browser stayed open after login")
			}
			a.flushUIUpdates()
			got := st.Browsers()[0]
			if !got.IsLoggedIn || got.IsRunning || got.Health != store.HealthReady {
				t.Fatalf("after relogin: %+v", got)
			}
		})
	}
}

func TestWatchLoginKeepsHealthyLoggedInAccountOpen(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddManagedBrowser("豆包", "豆包1", filepath.Join(t.TempDir(), "profile"), browser.KindFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(row.ID)
	row = st.Browsers()[0]

	// First poll sees a brief single "out" (e.g. hydration flash), second sees "in".
	probes := []string{"out", "in"}
	idx := 0
	followed := make(chan struct{}, 1)
	closed := make(chan struct{}, 1)
	a := &App{
		store:          st,
		manager:        browser.NewManager(),
		browserBackend: browser.KindFingerprint,
		loginPoll:      10 * time.Millisecond,
		wizardProbe: func(context.Context, string) (string, error) {
			if idx < len(probes) {
				s := probes[idx]
				idx++
				return s, nil
			}
			return "in", nil
		},
		loginFollow: func(store.Browser, string) { followed <- struct{}{} },
		afterClose:  func() { closed <- struct{}{} },
	}
	a.watchLogin(row, loginEndpoint(row))
	select {
	case <-followed:
	case <-time.After(time.Second):
		t.Fatal("login was not observed")
	}
	select {
	case <-closed:
		t.Fatal("healthy logged-in account browser should remain open")
	case <-time.After(80 * time.Millisecond):
	}
	got := st.Browsers()[0]
	if !got.IsLoggedIn || !got.IsRunning {
		t.Fatalf("healthy browser state changed unexpectedly: %+v", got)
	}
}

func TestWatchLoginClosesStaleSessionAfterRepeatedOutThenIn(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddManagedBrowser("豆包", "豆包1", filepath.Join(t.TempDir(), "profile"), browser.KindFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(row.ID)
	row = st.Browsers()[0]

	probes := []string{"out", "out", "unknown", "in"}
	idx := 0
	closed := make(chan struct{}, 1)
	a := &App{
		store:          st,
		manager:        browser.NewManager(),
		browserBackend: browser.KindFingerprint,
		loginPoll:      10 * time.Millisecond,
		wizardProbe: func(context.Context, string) (string, error) {
			if idx < len(probes) {
				s := probes[idx]
				idx++
				return s, nil
			}
			return "in", nil
		},
		afterClose: func() { closed <- struct{}{} },
	}
	a.watchLogin(row, loginEndpoint(row))
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("stale session browser should close after re-login")
	}
	a.flushUIUpdates()
	got := st.Browsers()[0]
	if !got.IsLoggedIn || got.IsRunning {
		t.Fatalf("after stale session relogin: %+v", got)
	}
}

func TestCloseLoginWindowSkipsDrawerViewedAccount(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddManagedBrowser("豆包", "豆包1", filepath.Join(t.TempDir(), "profile"), browser.KindFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(row.ID)
	a := &App{
		store:            st,
		manager:          browser.NewManager(),
		browserViewingID: row.ID,
	}
	a.closeLoginWindow(row)
	if len(a.loginClosed) != 0 {
		t.Fatalf("drawer-viewed account should not be closed: %v", a.loginClosed)
	}
	if !st.Browsers()[0].IsRunning {
		t.Fatal("drawer-viewed account should remain running")
	}
}

func TestWatchLoginWithActorSerializesAcceptAndClose(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddManagedBrowser("豆包", "1", filepath.Join(t.TempDir(), "profile"), browser.KindFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	st.SetRunning(row.ID, true)
	closed := make(chan struct{}, 1)
	a := &App{
		store:          st,
		manager:        browser.NewManager(),
		browserBackend: browser.KindFingerprint,
		loginPoll:      10 * time.Millisecond,
		wizardProbe: func(context.Context, string) (string, error) {
			return "in", nil
		},
		afterClose: func() { closed <- struct{}{} },
	}
	a.StartActor()
	defer a.StopActor()

	a.markFreshLogin(row.ID)
	a.watchLogin(row, loginEndpoint(row))
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("actor-backed watchLogin did not close fresh account window")
	}

	svc := &BrowserService{app: a}
	deadline := time.Now().Add(time.Second)
	for {
		state := svc.GetBrowserState(context.Background())
		if len(state.Rows) == 1 && state.Rows[0].IsLoggedIn && !state.Rows[0].IsRunning && state.Rows[0].Health == "就绪" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("unexpected browser state after login close: %+v", state.Rows)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWatchLoginAutoRenamesFreshAccountFromUsername(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Seed an existing Doubao account with username "xxww" to verify same-platform dedup
	// while Qianwen with the same username keeps the exact name.
	existingDoubao, err := st.AddManagedBrowser("豆包", "xxww", filepath.Join(t.TempDir(), "p0"), browser.KindFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	st.ConfirmLogin(existingDoubao.ID)

	for _, tc := range []struct {
		platform string
		probes   []string
		wantName string
	}{
		{platform: "豆包", probes: []string{"", "xxww"}, wantName: "xxww (2)"},
		{platform: "千问", probes: []string{"", "Qwen3464"}, wantName: "Qwen3464"},
		{platform: "千问", probes: []string{"xxww"}, wantName: "xxww"},
		{platform: "豆包", probes: []string{"", ""}, wantName: "豆包1"},
	} {
		t.Run(tc.platform+"_"+tc.wantName, func(t *testing.T) {
			var used []string
			for _, r := range st.Browsers() {
				used = append(used, r.Name)
			}
			placeholder := nextInstanceName(tc.platform, used)
			row, err := st.AddManagedBrowser(tc.platform, placeholder, filepath.Join(t.TempDir(), "p"), browser.KindFingerprint)
			if err != nil {
				t.Fatal(err)
			}
			st.SetRunning(row.ID, true)
			idx := 0
			closed := make(chan struct{}, 1)
			a := &App{
				store:           st,
				manager:         browser.NewManager(),
				browserBackend:  browser.KindFingerprint,
				loginPoll:       10 * time.Millisecond,
				accountNameWait: 60 * time.Millisecond,
				accountNamePoll: 10 * time.Millisecond,
				wizardProbe: func(context.Context, string) (string, error) {
					return "in", nil
				},
				accountNameProbe: func(context.Context, string) (string, error) {
					if idx < len(tc.probes) {
						v := tc.probes[idx]
						idx++
						return v, nil
					}
					return "", nil
				},
				afterClose: func() { closed <- struct{}{} },
			}
			a.markFreshLogin(row.ID)
			a.markAutoName(row.ID, row.Name)
			a.watchLogin(row, loginEndpoint(row))

			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("fresh account browser did not close after login")
			}
			a.flushUIUpdates()

			var got store.Browser
			for _, r := range st.Browsers() {
				if r.ID == row.ID {
					got = r
					break
				}
			}
			if !got.IsLoggedIn || got.Name != tc.wantName {
				t.Fatalf("account = %+v, want logged in with name %q", got, tc.wantName)
			}
		})
	}
}

func TestWatchLoginRespectsManualRenameBeforeLogin(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	row, err := st.AddManagedBrowser("豆包", "豆包1", filepath.Join(t.TempDir(), "p"), browser.KindFingerprint)
	if err != nil {
		t.Fatal(err)
	}
	st.SetRunning(row.ID, true)

	states := make(chan string, 2)
	states <- "out"
	probedOut := make(chan struct{}, 1)
	closed := make(chan struct{}, 1)
	a := &App{
		store:           st,
		manager:         browser.NewManager(),
		browserBackend:  browser.KindFingerprint,
		loginPoll:       10 * time.Millisecond,
		accountNameWait: 50 * time.Millisecond,
		accountNamePoll: 10 * time.Millisecond,
		wizardProbe: func(ctx context.Context, _ string) (string, error) {
			select {
			case s := <-states:
				if s == "out" {
					probedOut <- struct{}{}
				}
				return s, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
		accountNameProbe: func(context.Context, string) (string, error) {
			return "不应覆盖的名字", nil
		},
		afterClose: func() { closed <- struct{}{} },
	}
	a.markFreshLogin(row.ID)
	a.markAutoName(row.ID, row.Name)
	a.watchLogin(row, loginEndpoint(row))

	select {
	case <-probedOut:
	case <-time.After(time.Second):
		t.Fatal("did not observe out state")
	}

	// User manually renames account before login finishes.
	a.beginRename(row)
	a.renameDraft = "我的主力号"
	a.commitRename()

	states <- "in"
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("fresh account browser did not close")
	}
	a.flushUIUpdates()
	if got := st.Browsers()[0].Name; got != "我的主力号" {
		t.Fatalf("manual rename was overwritten: got %q", got)
	}
}

func TestWizardLoopAutoRenamesNewAccountOnLogin(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{
		store:           st,
		manager:         browser.NewManager(),
		browserBackend:  browser.KindFingerprint,
		wizardQueue:     newWizardQueue([]string{"千问"}),
		wizardLaunch:    func(b store.Browser) (string, error) { return loginEndpoint(b), nil },
		wizardOpen:      func(string, string) error { return nil },
		wizardProbe:     func(context.Context, string) (string, error) { return "in", nil },
		accountNameProbe: func(context.Context, string) (string, error) { return "Qwen8899", nil },
		accountNameWait: 50 * time.Millisecond,
		accountNamePoll: 10 * time.Millisecond,
	}
	a.wizardLoop(a.wizardGen, nil)
	a.flushUIUpdates()

	rows := st.Browsers()
	if len(rows) != 1 || !rows[0].IsLoggedIn || rows[0].Name != "Qwen8899" {
		t.Fatalf("wizard account after login = %+v, want logged-in Qwen8899", rows)
	}
}

func TestSyncAccountUsernamesBatchOverwriteDedupAndStop(t *testing.T) {
	root := t.TempDir()
	st, err := store.Open(filepath.Join(root, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	d1, err := st.AddBrowser("豆包", "老名字1", filepath.Join(root, "d1"))
	if err != nil {
		t.Fatal(err)
	}
	d2, err := st.AddBrowser("豆包", "豆包2", filepath.Join(root, "d2"))
	if err != nil {
		t.Fatal(err)
	}
	q1, err := st.AddBrowser("千问", "老千问", filepath.Join(root, "q1"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{d1.ID, d2.ID, q1.ID} {
		st.ConfirmLogin(id)
	}

	app := NewApp(st, nil, nil, root)
	app.StartActor()
	defer app.StopActor()

	svc := &BrowserService{app: app}
	ctx := context.Background()

	// 任务运行中不允许同步用户名
	_ = app.call(ctx, func() { app.running = true })
	if err := svc.SyncAccountUsernames(ctx, []int64{d1.ID}); err == nil {
		t.Fatal("expected error while tasks are running")
	}
	_ = app.call(ctx, func() { app.running = false })

	started := make(chan int64, 4)
	release := make(chan struct{})
	_ = app.call(ctx, func() {
		app.syncAccountNameFn = func(runCtx context.Context, b store.Browser) (string, error) {
			started <- b.ID
			select {
			case <-release:
			case <-runCtx.Done():
				return "", runCtx.Err()
			}
			switch b.ID {
			case d1.ID:
				return "xxww", nil
			case d2.ID:
				return "xxww", nil
			case q1.ID:
				return "Qwen3464", nil
			default:
				return "", nil
			}
		}
	})

	if err := svc.SyncAccountUsernames(ctx, []int64{d1.ID, d2.ID, q1.ID}); err != nil {
		t.Fatalf("SyncAccountUsernames failed: %v", err)
	}

	// 豆包(d1)与千问(q1)应跨平台并行启动
	seen := map[int64]bool{}
	for range 2 {
		select {
		case id := <-started:
			seen[id] = true
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for parallel platform sync start, seen=%v", seen)
		}
	}
	if !seen[d1.ID] || !seen[q1.ID] {
		t.Fatalf("expected %d and %d to run in parallel, got %v", d1.ID, q1.ID, seen)
	}

	mid := svc.GetBrowserState(ctx)
	if !mid.BatchActive || mid.BatchKind != "syncing" {
		t.Fatalf("expected active syncing batch, got active=%v kind=%q text=%q", mid.BatchActive, mid.BatchKind, mid.BatchText)
	}
	for _, r := range mid.Rows {
		if !r.Pending || r.PendingOp != "syncing" {
			t.Fatalf("row %d should be pending syncing, got pending=%v op=%q", r.ID, r.Pending, r.PendingOp)
		}
	}

	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		after := svc.GetBrowserState(ctx)
		if !after.BatchActive {
			got := map[int64]string{}
			for _, r := range after.Rows {
				got[r.ID] = r.Name
			}
			if got[d1.ID] != "xxww" || got[d2.ID] != "xxww (2)" || got[q1.ID] != "Qwen3464" {
				t.Fatalf("synced names = %+v, want d1=xxww d2=xxww (2) q1=Qwen3464", got)
			}
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatal("syncing batch did not finish in time")
}
