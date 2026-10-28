package browser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xxww0098/tulai/internal/store"
)

func TestRemoveProfileDeletesOnlyItsOwnDirectory(t *testing.T) {
	root := t.TempDir()
	keep := store.Browser{Name: "账号1", Platform: "豆包"}
	drop := store.Browser{Name: "账号2", Platform: "千问"}
	keepDir := ProfileDir(root, KindEdge, keep.Name)
	dropDir := ProfileDir(root, KindEdge, drop.Name)
	if keepDir == dropDir {
		t.Fatal("two account names share one profile directory")
	}
	for _, dir := range []string{keepDir, dropDir} {
		if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Local State"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfile(root, KindEdge, store.Browser{Name: "账号2", UserDataDir: outside}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal("profile cleanup removed a directory outside the data root")
	}
	if err := RemoveProfile(root, KindEdge, drop); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dropDir); !os.IsNotExist(err) {
		t.Fatalf("removed profile still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(keepDir, "Local State")); err != nil {
		t.Fatal(err)
	}
	ext := store.Browser{Name: "扩展账号abcd", ExtAccount: "profile-a"}
	extDir := ProfileDir(root, KindEdge, ext.Name)
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProfile(root, KindEdge, ext); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(extDir); err != nil {
		t.Fatal("an extension account without its own directory must not be deleted by name")
	}
}
