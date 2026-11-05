package download

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicReplacesUnicodePath(t *testing.T) {
	target := filepath.Join(t.TempDir(), "下载 目录 [中文]", "图片.part.json")
	for _, body := range []string{`{"etag":"first"}`, `{"etag":"已更新"}`} {
		if err := writeAtomic(target, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(target)
		if err != nil || string(data) != body {
			t.Fatalf("saved=%q error=%v", data, err)
		}
		if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
			t.Fatalf("temporary file retained: %v", err)
		}
	}
}

func TestWriteAtomicFailureKeepsDestination(t *testing.T) {
	target := filepath.Join(t.TempDir(), "已有目录")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, "保留.txt")
	if err := os.WriteFile(marker, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(target, []byte("replacement"), 0o600); err == nil {
		t.Fatal("replacing a non-empty directory must fail")
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "original" {
		t.Fatalf("destination changed: %q %v", data, err)
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("failed write left a temporary file", err)
	}
}
