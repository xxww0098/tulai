package ui

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/xxww0098/tulai/internal/download"
)

func importTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/not-image" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<img src="/other.png">`))
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(b.Bytes())
	}))
	t.Cleanup(server.Close)
	return server
}

func TestDownloadImportPreviewQueueAndPersistence(t *testing.T) {
	server := importTestServer(t)
	a, store := newDownloadApp(t)
	a.dlDir = t.TempDir()
	data := "名称,主图,详情\n衣服," + server.URL + "/a.png," + server.URL + "/b.png\n衣服," + server.URL + "/c.png," + server.URL + "/a.png\n衣服," + server.URL + "/a.png," + server.URL + "/b.png\n错误,javascript:alert(1),\n"
	s, err := newDownloadImport("商品.csv", strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	s.names[0] = true
	s.changeNameColumn(0)
	s.folders = true
	s.refresh()
	if s.err != "" || len(s.images) != 6 || s.invalid != 1 {
		t.Fatalf("preview=%+v invalid=%d err=%s", s.images, s.invalid, s.err)
	}
	a.dlImport = s
	tt := ui.NewTester(a.view, 1280, 900)
	for _, label := range []string{"导入表格图片", "前 5 行预览", "多个图片列分别建立文件夹", "开始导入"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q: %v", label, tt.Texts())
		}
	}
	if err := tt.Click("开始导入"); err != nil {
		t.Fatal(err)
	}
	if s.open || len(a.dlTasks) != 4 || !strings.Contains(a.dlImportNotice, "1 个无效") || !strings.Contains(a.dlImportNotice, "2 个重复") {
		t.Fatalf("tasks=%d notice=%q", len(a.dlTasks), a.dlImportNotice)
	}
	seen := map[string]bool{}
	for _, task := range a.dlTasks {
		if task.Phase != dlDone || !task.ImportedImage || !strings.HasSuffix(task.Path, ".png") {
			t.Fatalf("task=%+v", task)
		}
		if _, err := os.Stat(task.Path); err != nil {
			t.Fatal(err)
		}
		if seen[task.Path] {
			t.Fatalf("duplicate path %s", task.Path)
		}
		seen[task.Path] = true
	}
	if filepath.Base(a.dlTasks[0].Dir) != "主图" || filepath.Base(a.dlTasks[1].Dir) != "详情" || a.dlTasks[0].TargetName != "衣服" || a.dlTasks[2].TargetName == "衣服" {
		t.Fatalf("names/folders=%+v", a.dlTasks)
	}
	restored := &App{store: store}
	restored.loadDownloadRecords()
	if len(restored.dlTasks) != 4 || !restored.dlTasks[0].ImportedImage || restored.dlTasks[0].TargetName != "衣服" {
		t.Fatalf("restored=%+v", restored.dlTasks)
	}
}

func TestDownloadImportNameSelectionOrder(t *testing.T) {
	s, err := newDownloadImport("order.csv", strings.NewReader("甲,网址,丙\nA,https://example.test/x.jpg,C\n"))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := newDownloadApp(t)
	a.dlImport = s
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("文件名列 3：丙"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("文件名列 1：甲"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(s.images) != 1 || s.images[0].Name != "C_A" {
		t.Fatalf("ordered selection=%+v", s.images)
	}
	if err := tt.Click("文件名列 3：丙"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("文件名列 3：丙"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(s.images) != 1 || s.images[0].Name != "A_C" {
		t.Fatalf("reselected order=%+v", s.images)
	}
}

func TestDownloadImportWorksheetSelection(t *testing.T) {
	a, _ := newDownloadApp(t)
	s := &downloadImportState{open: true, name: "测试.xlsx", sheet: "第一页", book: &download.Spreadsheet{Sheets: []download.SpreadsheetSheet{
		{Name: "第一页", Rows: [][]string{{"名称", "网址"}, {"一", "https://example.test/one.png"}}},
		{Name: "第二页", Rows: [][]string{{"https://example.test/two.png", "二"}}},
	}}}
	s.selectSheet()
	a.dlImport = s
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("选择工作表"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("第二页"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.sheet != "第二页" || s.index != 1 || s.preview.Start != 0 || len(s.images) != 1 || !strings.HasSuffix(s.images[0].URL, "/two.png") || s.preview.Columns[0].Name != "A" {
		t.Fatalf("sheet=%+v", s)
	}
}

func TestDownloadImportFolderNameCollisions(t *testing.T) {
	server := importTestServer(t)
	a, _ := newDownloadApp(t)
	a.dlDir = t.TempDir()
	images := []download.SpreadsheetImage{
		{URL: server.URL + "/a.png", Name: "图", Column: "x", Row: 2, ColumnIndex: 0},
		{URL: server.URL + "/b.png", Name: "图", Column: "x_3", Row: 2, ColumnIndex: 1},
		{URL: server.URL + "/c.png", Name: "图", Column: "x", Row: 2, ColumnIndex: 2},
	}
	added, dup, err := a.enqueueSpreadsheetImages(images, []bool{true, true, true}, true)
	if err != nil || added != 3 || dup != 0 {
		t.Fatalf("added=%d duplicates=%d err=%v", added, dup, err)
	}
	dirs := map[string]bool{}
	for _, task := range a.dlTasks {
		if dirs[task.Dir] {
			t.Fatalf("columns share folder %q", task.Dir)
		}
		dirs[task.Dir] = true
		if task.Phase != dlDone {
			t.Fatalf("task=%+v", task)
		}
	}
}

func TestDownloadImportRequiresImage(t *testing.T) {
	server := importTestServer(t)
	a, _ := newDownloadApp(t)
	a.dlDir = t.TempDir()
	images := []download.SpreadsheetImage{{URL: server.URL + "/not-image", Name: "图片", Column: "网址", Row: 2, ColumnIndex: 0}}
	added, _, err := a.enqueueSpreadsheetImages(images, []bool{true}, false)
	if err != nil || added != 1 || a.dlTasks[0].Phase != dlFailed || !strings.Contains(a.dlTasks[0].Err, "不是图片") {
		t.Fatalf("task=%+v", a.dlTasks[0])
	}
}

func TestDownloadImportEscapedNames(t *testing.T) {
	for input, want := range map[string]string{"../路径": "_路径", "CON": "_CON", "NUL.png": "_NUL.png", "...\x00/": "__"} {
		if got := safeSpreadsheetName(input); got != want {
			t.Errorf("safe name %q=%q want %q", input, got, want)
		}
	}
	long := safeSpreadsheetName(strings.Repeat("中", 100))
	if len(long) > 96 || !utf8.ValidString(long) {
		t.Fatalf("truncated name invalid: %q", long)
	}
}

func TestDownloadImportDoesNotOverwriteExistingURLTask(t *testing.T) {
	server := importTestServer(t)
	a, _ := newDownloadApp(t)
	a.dlDir = t.TempDir()
	oldPath := filepath.Join(a.dlDir, "a.png")
	if err := os.WriteFile(oldPath, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	a.dlTasks = []*downloadTask{{ID: 1, URL: server.URL + "/a.png", Dir: a.dlDir, Phase: dlDone}}
	a.dlNextID = 1
	added, _, err := a.enqueueSpreadsheetImages([]download.SpreadsheetImage{{URL: server.URL + "/a.png", Name: "a", Column: "图片", Row: 2, ColumnIndex: 0}}, []bool{true}, false)
	if err != nil || added != 1 || len(a.dlTasks) != 2 || a.dlTasks[1].TargetName == "a" || a.dlTasks[1].Phase != dlDone {
		t.Fatalf("existing collision: %+v", a.dlTasks)
	}
	old, err := os.ReadFile(oldPath)
	if err != nil || string(old) != "original" {
		t.Fatalf("existing file overwritten: %q %v", old, err)
	}
}

func TestDownloadImportRejectsSymlinkFolder(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlDir = t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(a.dlDir, "主图")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	images := []download.SpreadsheetImage{{URL: "https://example.test/a.png", Name: "图", Column: "主图", Row: 2, ColumnIndex: 0}, {URL: "https://example.test/b.png", Name: "图", Column: "详情", Row: 2, ColumnIndex: 1}}
	added, _, err := a.enqueueSpreadsheetImages(images, []bool{true, true}, true)
	if err == nil || added != 0 || len(a.dlTasks) != 0 {
		t.Fatalf("symlink accepted: added=%d err=%v", added, err)
	}
}
