package ui

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

type stubDesktopHost struct {
	mu          sync.Mutex
	topics      []string
	darkHistory []bool
	clipboard      string
	clipboardImage []byte
	trashed        []string
	trashErr       error
	trashFn        func(string) error
	topicCh        chan string
}

func (s *stubDesktopHost) PickFiles(context.Context, string, bool, []FileFilter) ([]string, error) {
	return nil, nil
}
func (s *stubDesktopHost) PickDirectories(context.Context, string, bool) ([]string, error) {
	return nil, nil
}
func (s *stubDesktopHost) SaveFile(context.Context, string, string, []byte) (bool, string, error) {
	return false, "", nil
}
func (s *stubDesktopHost) OpenPath(string) error   { return nil }
func (s *stubDesktopHost) RevealPath(string) error { return nil }
func (s *stubDesktopHost) TrashPath(path string) error {
	s.mu.Lock()
	fn := s.trashFn
	err := s.trashErr
	s.mu.Unlock()
	if fn != nil {
		if err := fn(path); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	s.mu.Lock()
	s.trashed = append(s.trashed, path)
	s.mu.Unlock()
	return nil
}
func (s *stubDesktopHost) SetClipboard(text string) error {
	s.mu.Lock()
	s.clipboard = text
	s.mu.Unlock()
	return nil
}
func (s *stubDesktopHost) SetClipboardImage(pngData []byte) error {
	s.mu.Lock()
	s.clipboardImage = append([]byte(nil), pngData...)
	s.mu.Unlock()
	return nil
}
func (s *stubDesktopHost) ConfirmAlert(string, string, string) bool { return true }
func (s *stubDesktopHost) SetDarkTheme(dark bool) {
	s.mu.Lock()
	s.darkHistory = append(s.darkHistory, dark)
	s.mu.Unlock()
}
func (s *stubDesktopHost) EmitTopics(topics string) {
	s.mu.Lock()
	s.topics = append(s.topics, topics)
	s.mu.Unlock()
	if s.topicCh != nil {
		select {
		case s.topicCh <- topics:
		default:
		}
	}
}
func (s *stubDesktopHost) Quit()    {}
func (s *stubDesktopHost) Restart() {}

func TestControllerActorSerializesConcurrentIPCAndEventPump(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	bm := browser.NewManager()
	eng := tasks.NewEngine(st, bm, nil, dir)
	defer eng.Shutdown()

	a := NewApp(st, bm, eng, dir)
	host := &stubDesktopHost{topicCh: make(chan string, 32)}
	a.BindHost(host)
	a.StartActor()
	stopPump := a.startEventPump(host)
	defer func() {
		a.StopActor()
		stopPump()
	}()

	coreSvc := &CoreService{app: a}
	wbSvc := &WorkbenchService{app: a}
	settingsSvc := &SettingsService{app: a}
	ctx := context.Background()

	// Hammer concurrent IPC reads/writes + postUI mutations under -race.
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 20 {
				if j%3 == 0 {
					coreSvc.SetDarkTheme(ctx, (id+j)%2 == 0)
				}
				if j%4 == 0 {
					wbSvc.SetWorkbenchField(ctx, "prompts", "actor-stress")
				}
				if j%5 == 0 {
					settingsSvc.SetSettingField(ctx, "numPrefix", "p-")
				}
				_ = coreSvc.GetShellState(ctx)
				_ = wbSvc.GetWorkbenchState(ctx)
				a.postUI(func() {
					a.emit("workbench")
				})
			}
		}(i)
	}
	wg.Wait()

	// Event pump must have delivered coalesced topics to the host.
	select {
	case got := <-host.topicCh:
		if strings.TrimSpace(got) == "" {
			t.Fatal("received empty topic batch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event pump topic emission")
	}

	// Verify dark theme propagated to host
	coreSvc.SetDarkTheme(ctx, true)
	host.mu.Lock()
	nDark := len(host.darkHistory)
	lastDark := nDark > 0 && host.darkHistory[nDark-1]
	host.mu.Unlock()
	if !lastDark {
		t.Fatalf("expected final host dark theme true, history len=%d", nDark)
	}
}
