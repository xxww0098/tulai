package browser

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/store"
)

func TestFingerprintProfileConfinementAndStableFallback(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"account-1", "../escape", `..\escape`, ".", "..", ""} {
		dir := ProfileDir(root, KindFingerprint, name)
		rel, err := filepath.Rel(root, dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("escaped profile: %q", dir)
		}
	}
	b := store.Browser{ID: 42, Name: "first"}
	before := accountProfilePath(root, KindFingerprint, b)
	b.Name = "renamed"
	if after := accountProfilePath(root, KindFingerprint, b); before != after {
		t.Fatalf("rename changed profile: %s %s", before, after)
	}
}
