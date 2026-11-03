package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/store"
)

func TestRenamePageNavigationAndModes(t *testing.T) {
	a := &App{}
	tt := ui.NewTester(a.view, 1280, 800)
	if err := tt.Click("批量重命名"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.tab != pageRename {
		t.Fatalf("tab = %d", a.tab)
	}
	for _, label := range []string{"查找替换", "智能重命名", "插入内容", "自动编号", "手动编辑", "添加文件", "读取文件夹内容", "执行重命名"} {
		if !tt.HasText(label) {
			t.Errorf("missing %s: %v", label, tt.Texts())
		}
	}
	if err := tt.Click("选择查找模板"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("前 N 位"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.fileRename.rules[0].template != 1 {
		t.Fatal("find template was not selected")
	}
	if err := tt.Click("自动编号"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.fileRename.mode != 3 || !tt.HasText("补零位数") {
		t.Fatalf("number controls not shown: %v", tt.Texts())
	}
}

func TestRenamePlanApplyUndoAndConflict(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"image10.JPG", "image2.JPG"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{}
	a.renameIngest([]string{filepath.Join(dir, "image10.JPG"), filepath.Join(dir, "image2.JPG"), filepath.Join(dir, "image2.JPG")})
	if len(a.fileRename.items) != 2 || filepath.Base(a.fileRename.items[0].path) != "image2.JPG" {
		t.Fatalf("items = %+v", a.fileRename.items)
	}
	a.fileRename.mode = 3
	a.fileRename.numPrefix = "图"
	changes, err := a.renamePlan()
	if err != nil || filepath.Base(changes[0].To) != "图1.JPG" {
		t.Fatalf("plan = %+v, %v", changes, err)
	}
	a.renameApply(changes, false)
	if a.fileRename.err != "" || len(a.fileRename.undo) != 2 {
		t.Fatalf("apply = %+v", a.fileRename)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "图1.JPG")); err != nil || string(got) != "image2.JPG" {
		t.Fatalf("renamed file = %q, %v", got, err)
	}
	a.renameApply(a.fileRename.undo, true)
	if a.fileRename.err != "" || len(a.fileRename.undo) != 0 {
		t.Fatalf("undo = %+v", a.fileRename)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "image2.JPG")); err != nil || string(got) != "image2.JPG" {
		t.Fatalf("restored file = %q, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "图1.JPG"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.renamePlan(); err == nil || !strings.Contains(err.Error(), "目标已存在") {
		t.Fatalf("collision = %v", err)
	}
}

func TestRenameUndoRejectsReplacedFile(t *testing.T) {
	dir := t.TempDir()
	original, renamed, saved := filepath.Join(dir, "old.txt"), filepath.Join(dir, "1.txt"), filepath.Join(dir, "saved.txt")
	if err := os.WriteFile(original, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{}
	a.renameIngest([]string{original})
	a.fileRename.mode = 3
	changes, err := a.renamePlan()
	if err != nil {
		t.Fatal(err)
	}
	a.renameApply(changes, false)
	if err := os.Rename(renamed, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(renamed, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	a.renameApply(a.fileRename.undo, true)
	if !strings.Contains(a.fileRename.err, "源文件已变化") {
		t.Fatalf("undo error = %q", a.fileRename.err)
	}
	got, err := os.ReadFile(renamed)
	if err != nil || string(got) != "replacement" {
		t.Fatalf("replacement = %q, %v", got, err)
	}
	got, err = os.ReadFile(saved)
	if err != nil || string(got) != "original" {
		t.Fatalf("original = %q, %v", got, err)
	}
}

func TestRenameRecoveryMessageOverridesPreviewError(t *testing.T) {
	a := &App{tab: pageRename}
	a.fileRename.init()
	a.fileRename.items = []renameItem{{path: filepath.Join(t.TempDir(), "missing.txt")}}
	a.fileRename.err = "请手动恢复 /tmp/temporary-file 到原位置"
	tt := ui.NewTester(a.view, 1000, 760)
	if !tt.HasText(a.fileRename.err) {
		t.Fatalf("recovery message hidden: %v", tt.Texts())
	}
}

func TestRenameModelSettingsPersist(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a := &App{store: st, captchaBackend: "free", customURL: "https://example.test/v1/chat/completions", customKey: "key", customModel: "model"}
	a.saveRenameModel()
	if st.Get("captcha_custom_url") != a.customURL || st.Get("captcha_custom_key") != "key" || st.Get("captcha_custom_model") != "model" || a.captchaBackend != "free" {
		t.Fatal("model settings were not saved independently of the captcha backend")
	}
}

func TestRenameBlocksUpdateAndWaitsForQuit(t *testing.T) {
	a := &App{}
	a.fileRename.busy = true
	if !a.tasksRunning() {
		t.Fatal("update did not defer while renaming")
	}
	done, waited := make(chan struct{}), make(chan struct{})
	a.fileRename.done = done
	go func() { a.waitRename(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("quit did not wait for the rename")
	default:
	}
	close(done)
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("quit stayed blocked after rename")
	}
	a.fileRename.busy = false
	a.updatePending, a.updateBusy = true, true
	if !a.renameUpdateActive() {
		t.Fatal("rename allowed during installation")
	}
}

func TestRenameFolderPasteAndReorder(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, ".hidden"), filepath.Join(nested, "one.txt")} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, failed := renameReadFolders([]string{dir})
	if len(files) != 2 || failed != 0 {
		t.Fatalf("files=%v, failed=%d", files, failed)
	}
	stage := filepath.Join(dir, ".tulai-rename-recovery")
	if err := os.MkdirAll(filepath.Join(stage, "files"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "恢复说明.txt"), []byte("guide"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "files", "original"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, failed = renameReadFolders([]string{dir})
	if len(files) != 2 || failed != 1 {
		t.Fatalf("recovery directory was not skipped: %v, %d", files, failed)
	}
	a := &App{}
	a.renameIngest(files)
	a.renameReorder([]int{0}, len(a.fileRename.items))
	if !a.renamePasteAt(0, "first.txt\nsecond.txt\n") {
		t.Fatal("multiline paste was not handled")
	}
	a.fileRename.mode = 4
	changes, err := a.renamePlan()
	if err != nil || filepath.Base(changes[0].To) != "first.txt" || filepath.Base(changes[1].To) != "second.txt" {
		t.Fatalf("manual plan = %+v, %v", changes, err)
	}
}

func TestRenamePageEditsPreviewsAndApplies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(path, []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &App{tab: pageRename}
	a.renameIngest([]string{path})
	tt := ui.NewTester(a.view, 1000, 760)
	if err := tt.Click("查找 1"); err != nil {
		t.Fatal(err)
	}
	tt.Type("old")
	if err := tt.Click("替换为 1"); err != nil {
		t.Fatal(err)
	}
	tt.Type("new")
	tt.Frame()
	if !tt.HasText("new.txt") {
		t.Fatalf("preview absent: %v", tt.Texts())
	}
	if err := tt.Click("执行重命名"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil || string(got) != "contents" {
		t.Fatalf("renamed = %q, %v", got, err)
	}
	tt.Frame()
	if err := tt.Click("撤销上次重命名"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("undo: %v", err)
	}
}

func TestRenamePageDragReorders(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	a := &App{tab: pageRename}
	a.renameIngest(paths)
	tt := ui.NewTester(a.view, 1000, 760)
	first, ok := tt.Find("a.txt")
	if !ok {
		t.Fatal("first row not visible")
	}
	last, ok := tt.Find("c.txt")
	if !ok {
		t.Fatal("last row not visible")
	}
	tt.Press(first.X+20, first.Y+first.H/2)
	tt.Move(last.X+20, last.Y+last.H)
	tt.Release(last.X+20, last.Y+last.H)
	if got := filepath.Base(a.fileRename.items[2].path); got != "a.txt" {
		t.Fatalf("last row = %s", got)
	}
}

func TestRenamePagePastesFilesAndMultipleNames(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{tab: pageRename}
	tt := ui.NewTester(a.view, 1000, 760)
	data, err := transfer.FileData(paths...)
	if err != nil {
		t.Fatal(err)
	}
	tt.SetClipboardData(data)
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if len(a.fileRename.items) != 2 {
		t.Fatalf("pasted files: %+v", a.fileRename.items)
	}
	if err := tt.Click("手动编辑"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("新名称 a.txt"); err != nil {
		t.Fatal(err)
	}
	tt.SetClipboard("first.txt\nsecond.txt\n")
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if a.fileRename.items[0].draft != "first.txt" || a.fileRename.items[1].draft != "second.txt" {
		t.Fatalf("pasted names: %+v", a.fileRename.items)
	}
	if err := tt.Click("复制原名称"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != "a.txt\nb.txt" {
		t.Fatalf("copied names = %q", got)
	}
}

func TestExportRenameShots(t *testing.T) {
	dir := os.Getenv("TULAI_RENAME_SHOTS")
	if dir == "" {
		t.Skip("set TULAI_RENAME_SHOTS to export screenshots")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := t.TempDir()
	for _, name := range []string{"夏日风景 01.JPG", "夏日风景 02.JPG", "夏日风景 10.JPG"} {
		if err := os.WriteFile(filepath.Join(files, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, shot := range []struct {
		name                string
		width, height, mode int
		populated, dark     bool
	}{
		{"empty", 1280, 800, 0, false, false}, {"replace", 1280, 800, 0, true, false},
		{"insert", 1280, 800, 2, true, false}, {"manual", 800, 700, 4, true, false}, {"ai", 800, 700, 1, true, false},
		{"number-narrow", 640, 640, 3, true, false}, {"dark", 1000, 760, 0, true, true},
		{"ai-settings", 800, 700, 1, true, false},
		{"template-narrow", 640, 640, 0, true, false},
	} {
		a := &App{tab: pageRename}
		if shot.populated {
			a.renameIngest([]string{filepath.Join(files, "夏日风景 01.JPG"), filepath.Join(files, "夏日风景 02.JPG"), filepath.Join(files, "夏日风景 10.JPG")})
		}
		a.fileRename.init()
		a.fileRename.mode = shot.mode
		if shot.name == "ai-settings" {
			a.fileRename.aiSettings = true
		}
		if shot.populated {
			a.fileRename.rules[0].Find, a.fileRename.rules[0].With = "夏日", "旅行"
		}
		if shot.name == "template-narrow" {
			a.fileRename.rules[0].template = 3
		}
		view := a.view
		if shot.dark {
			view = func(c *ui.Context) {
				theme := *c.Theme()
				theme.Dark = true
				c.SetTheme(&theme)
				a.view(c)
			}
		}
		tt := ui.NewTester(view, shot.width, shot.height)
		tt.Advance(400 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, shot.name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
		_ = f.Close()
	}
}
