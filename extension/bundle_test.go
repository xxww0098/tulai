package extbundle

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEmbeddedFSExcludesTestsAndDocs(t *testing.T) {
	if RefuseJS() == "" || DoubaoRawJS() == "" || DoubaoPageJS() == "" || DoubaoSourceMapJS() == "" {
		t.Fatal("embedded helper scripts must be non-empty")
	}
	err := fs.WalkDir(packed, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(p, "_test.js") || strings.EqualFold(filepath.Base(p), "README.md") || strings.HasSuffix(p, ".go") {
			t.Errorf("embedded FS must not contain %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallDownloadAddonKeepsConnectionDetailsPrivate(t *testing.T) {
	dir, _, err := InstallDownloadAddonWithMarkers(t.TempDir(), "http://127.0.0.1:12345/download", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"temu.js", "content.js", "settings.js"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(TemuSearchJS(), "function tulaiTemuSnapshot") {
		t.Fatal("TEMU 搜索脚本未打包")
	}
	path := filepath.Join(dir, "settings.js")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("下载助手配置权限错误: %v, %v", info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "test-secret") || !strings.Contains(string(data), "127.0.0.1:12345") {
		t.Fatalf("下载助手连接信息错误: %v", err)
	}

	dir1, m1, err := InstallDownloadAddonWithMarkers(t.TempDir(), "http://127.0.0.1:12345/download", "s1")
	if err != nil {
		t.Fatal(err)
	}
	_, m2, err := InstallDownloadAddonWithMarkers(t.TempDir(), "http://127.0.0.1:12345/download", "s2")
	if err != nil {
		t.Fatal(err)
	}
	for _, attr := range []string{m1.HostAttr, m1.RequestAttr, m1.SnapshotAttr} {
		if !strings.HasPrefix(attr, "data-") || len(attr) < 13 || strings.Contains(strings.ToLower(attr), "tulai") {
			t.Fatalf("invalid randomized attr %q", attr)
		}
	}
	if m1.HostAttr == m2.HostAttr || m1.RequestAttr == m2.RequestAttr || m1.SnapshotAttr == m2.SnapshotAttr {
		t.Fatalf("markers were not randomized across installs: %+v vs %+v", m1, m2)
	}
	content1, err := os.ReadFile(filepath.Join(dir1, "content.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{m1.HostAttr, m1.RequestAttr, m1.SnapshotAttr} {
		if !strings.Contains(string(content1), needle) {
			t.Fatalf("installed download/content.js missing marker %q", needle)
		}
	}
	scrubbedContent := strings.ReplaceAll(string(content1), "tulaiTemuSnapshot()", "")
	for _, forbidden := range []string{"tulai", "data-pb-dl-host", "data-pb-dl-req", "data-pb-dl-snap"} {
		if strings.Contains(strings.ToLower(scrubbedContent), forbidden) {
			t.Fatalf("installed download/content.js leaked %q", forbidden)
		}
	}
}
