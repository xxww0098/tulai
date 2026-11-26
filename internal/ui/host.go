package ui

import (
	"context"
	"os"
	"strings"
	"sync"

	"github.com/egoist/mygo"
)

// TopicsEvent notifies the frontend which state domains changed (comma-separated, coalesced).
type TopicsEvent struct {
	Topics string `json:"topics"`
}

// FileDropEvent carries native OS absolute file/folder paths dropped onto the window.
type FileDropEvent struct {
	Paths []string `json:"paths"`
	X     int      `json:"x"`
	Y     int      `json:"y"`
}

var (
	// EventTopics pushes coalesced topic invalidations to the frontend store.
	EventTopics = mygo.NewEvent[TopicsEvent]("tulai:topics")
	// EventFileDrop forwards OS file/folder drops (with absolute paths and viewport coordinates).
	EventFileDrop = mygo.NewEvent[FileDropEvent]("tulai:file-drop")
)

var (
	appIconMu               sync.RWMutex
	appIconData             []byte
	packagedVersionOverride string
)

// SetAppIcon registers the PNG bytes served for the app brand mark.
// cmd/tulai populates it from cmd/tulai/assets/Icon.png so there is one canonical icon asset.
func SetAppIcon(pngBytes []byte) {
	appIconMu.Lock()
	appIconData = append([]byte(nil), pngBytes...)
	appIconMu.Unlock()
}

func getAppIconData() []byte {
	appIconMu.RLock()
	data := appIconData
	appIconMu.RUnlock()
	return data
}

// PackagedVersion returns the version linked into a production packaged build
// (via `mygo build`), or "" for unpackaged source/dev builds (`go run`, `go test`, `mygo dev`).
func PackagedVersion() string {
	if v := strings.TrimSpace(packagedVersionOverride); v != "" {
		return v
	}
	if mygo.IsDev() {
		return ""
	}
	return strings.TrimSpace(mygo.App.Version())
}

// FileFilter limits the files shown by an OS open/save dialog.
type FileFilter struct {
	Name       string
	Extensions []string
}

// DesktopHost isolates OS and MyGo desktop interactions so App stays 100% testable headlessly.
type DesktopHost interface {
	PickFiles(ctx context.Context, title string, multiple bool, filters []FileFilter) ([]string, error)
	PickDirectories(ctx context.Context, title string, multiple bool) ([]string, error)
	SaveFile(ctx context.Context, defaultName, mime string, data []byte) (saved bool, path string, err error)
	OpenPath(path string) error
	RevealPath(path string) error
	TrashPath(path string) error
	SetClipboard(text string) error
	SetClipboardImage(pngData []byte) error
	ConfirmAlert(title, message, confirmLabel string) bool
	SetDarkTheme(dark bool)
	EmitTopics(topics string)
	Quit()
	Restart()
}

// Services groups all 9 domain services bound to one App instance for runtime or tests.
type Services struct {
	Core      *CoreService
	Workbench *WorkbenchService
	Browser   *BrowserService
	Prompts   *PromptsService
	Download  *DownloadService
	Dedup     *DedupService
	Rename    *RenameService
	ImageEdit *ImageEditService
	Settings  *SettingsService
}

// NewServices constructs all 9 domain services backed by app.
func NewServices(app *App) Services {
	return Services{
		Core:      &CoreService{app: app},
		Workbench: &WorkbenchService{app: app},
		Browser:   &BrowserService{app: app},
		Prompts:   &PromptsService{app: app},
		Download:  &DownloadService{app: app},
		Dedup:     &DedupService{app: app},
		Rename:    &RenameService{app: app},
		ImageEdit: &ImageEditService{app: app},
		Settings:  &SettingsService{app: app},
	}
}

// RegisterBindings binds all 9 tulai domain services to MyGo IPC.
func RegisterBindings(app *App) {
	svcs := NewServices(app)
	mygo.BindAs("Core", svcs.Core)
	mygo.BindAs("Workbench", svcs.Workbench)
	mygo.BindAs("Browser", svcs.Browser)
	mygo.BindAs("Prompts", svcs.Prompts)
	mygo.BindAs("Download", svcs.Download)
	mygo.BindAs("Dedup", svcs.Dedup)
	mygo.BindAs("Rename", svcs.Rename)
	mygo.BindAs("ImageEdit", svcs.ImageEdit)
	mygo.BindAs("Settings", svcs.Settings)
}

// RegisterBindingsOnly binds all service method signatures without initializing DB/state.
// Used by cmd/tulai's MYGO_GENERATE fast-path for `go tool mygo generate`.
func RegisterBindingsOnly() {
	RegisterBindings(nil)
}

type mygoHost struct {
	mu  sync.RWMutex
	win *mygo.Window
}

func (h *mygoHost) setWindow(w *mygo.Window) {
	h.mu.Lock()
	h.win = w
	h.mu.Unlock()
}

func (h *mygoHost) window(ctx context.Context) *mygo.Window {
	if ctx != nil {
		if w := mygo.CallerWindow(ctx); w != nil {
			return w
		}
	}
	h.mu.RLock()
	w := h.win
	h.mu.RUnlock()
	return w
}

func (h *mygoHost) PickFiles(ctx context.Context, title string, multiple bool, filters []FileFilter) ([]string, error) {
	var mf []mygo.FileFilter
	for _, f := range filters {
		if len(f.Extensions) == 0 {
			continue
		}
		mf = append(mf, mygo.FileFilter{
			Name:       f.Name,
			Extensions: append([]string(nil), f.Extensions...),
		})
	}
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:   h.window(ctx),
		Title:    title,
		Filters:  mf,
		Multiple: multiple,
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

func (h *mygoHost) PickDirectories(ctx context.Context, title string, multiple bool) ([]string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            h.window(ctx),
		Title:             title,
		Directory:         true,
		Multiple:          multiple,
		CreateDirectories: true,
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

func (h *mygoHost) SaveFile(ctx context.Context, defaultName, mime string, data []byte) (bool, string, error) {
	opts := mygo.SaveDialogOptions{
		Parent:            h.window(ctx),
		DefaultPath:       defaultName,
		CreateDirectories: true,
	}
	if strings.TrimSpace(mime) == "text/plain" {
		opts.Filters = []mygo.FileFilter{{Name: "Log / Text", Extensions: []string{"log", "txt"}}}
	}
	path, err := mygo.Dialog.Save(opts)
	if err != nil {
		return false, "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return false, "", nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return false, "", err
	}
	return true, path, nil
}

func (h *mygoHost) OpenPath(path string) error {
	return mygo.Shell.OpenPath(path)
}

func (h *mygoHost) RevealPath(path string) error {
	mygo.Shell.ShowItemInFolder(path)
	return nil
}

func (h *mygoHost) TrashPath(path string) error {
	return mygo.Shell.TrashItem(path)
}

func (h *mygoHost) SetClipboard(text string) error {
	mygo.Clipboard.WriteText(text)
	return nil
}

func (h *mygoHost) SetClipboardImage(pngData []byte) error {
	return mygo.Clipboard.WriteImage(pngData)
}

func (h *mygoHost) ConfirmAlert(title, message, confirmLabel string) bool {
	if strings.TrimSpace(confirmLabel) == "" {
		confirmLabel = "确定"
	}
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent:        h.window(nil),
		Type:          mygo.MessageQuestion,
		Title:         title,
		Message:       title,
		Detail:        message,
		Buttons:       []string{"取消", confirmLabel},
		DefaultButton: 1,
		CancelButton:  0,
	})
	return err == nil && res.Button == 1
}

func (h *mygoHost) SetDarkTheme(dark bool) {
	applyMyGoTheme(dark)
}

func (h *mygoHost) EmitTopics(topics string) {
	if strings.TrimSpace(topics) == "" {
		return
	}
	_ = EventTopics.Broadcast(TopicsEvent{Topics: topics})
}

func (h *mygoHost) Quit() {
	mygo.App.Quit()
}

func (h *mygoHost) Restart() {
	mygo.App.Relaunch()
}

func applyMyGoTheme(dark bool) {
	if dark {
		mygo.Theme.SetSource(mygo.ThemeDark)
	} else {
		mygo.Theme.SetSource(mygo.ThemeLight)
	}
}
