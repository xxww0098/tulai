package extbundle

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallIsIdempotentAndCleansOldVersions(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "extension", "0.0.1")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	ver := Version()
	if ver == "" {
		t.Fatal("empty version")
	}
	want := filepath.Join(root, "extension", "current")
	if first != want {
		t.Fatalf("dir = %s", first)
	}
	if _, err := os.Stat(filepath.Join(root, "extension", ver, "background.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old version still present: %v", err)
	}
	info, err := os.Stat(filepath.Join(first, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Install(root)
	if err != nil || second != first {
		t.Fatalf("second = %s %v", second, err)
	}
	again, err := os.Stat(filepath.Join(first, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(info.ModTime()) {
		t.Fatal("reinstall rewrote the stable directory")
	}
}
