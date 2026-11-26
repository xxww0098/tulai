//go:build tulai_fingerprint_bundle

package fingerprint

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEmbeddedWindowsRuntimeLoadsWithoutNetwork(t *testing.T) {
	windows := os.Getenv("SystemRoot")
	t.Setenv("PATH", filepath.Join(windows, "System32")+string(os.PathListSeparator)+windows)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	previous := nodeDistBases
	nodeDistBases = []string{"http://127.0.0.1:1"}
	t.Cleanup(func() { nodeDistBases = previous })
	dir := t.TempDir()
	if err := prepareRuntime(ctx, dir, "", nodeArtifacts["windows/amd64"], nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(npmPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("bundled runtime unexpectedly includes npm: %v", err)
	}
	if err := verifyNodePackages(ctx, dir, ""); err != nil {
		t.Fatalf("bundled playwright-core cannot load: %v", err)
	}
}
