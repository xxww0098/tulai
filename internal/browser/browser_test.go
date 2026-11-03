package browser

import (
	"github.com/xxww0098/tulai/internal/store"
	"path/filepath"
	"strings"
	"testing"
)

func TestCamoufoxProfileConfinementAndStableFallback(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"account-1", "../escape", `..\escape`, ".", "..", ""} {
		dir := ProfileDir(root, KindCamoufox, name)
		rel, err := filepath.Rel(root, dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("escaped profile: %q", dir)
		}
	}
	b := store.Browser{ID: 42, Name: "first"}
	before := accountProfilePath(root, KindCamoufox, b)
	b.Name = "renamed"
	if after := accountProfilePath(root, KindCamoufox, b); before != after {
		t.Fatalf("rename changed profile: %s %s", before, after)
	}
}
