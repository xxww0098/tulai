//go:build tulai_camoufox_bundle

package camoufox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedWindowsRuntimeLoadsWithoutNetwork(t *testing.T) {
	// Do not accidentally find VC runtime DLLs in the build machine's toolchain.
	windows := os.Getenv("SystemRoot")
	t.Setenv("PATH", filepath.Join(windows, "System32")+string(os.PathListSeparator)+windows)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("NAPI_RS_NATIVE_LIBRARY_PATH", "missing.node")
	t.Setenv("NAPI_RS_ENFORCE_VERSION_CHECK", "1")
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
	dll := filepath.Join(dir, "node_modules", "impit-win32-x64-msvc", "vcruntime140.dll")
	// An invalid local DLL must break the import even on a machine with the
	// system redistributable installed. Otherwise the host could mask a broken
	// bundled runtime during CI.
	if err := os.WriteFile(dll, []byte("invalid DLL"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runInstall(ctx, dir, "", nil, nil, "--input-type=module", "-e", "await import('camoufox')"); err == nil {
		t.Fatal("launcher loaded despite a broken module-local VC runtime")
	}
	if err := ensureNodeRuntimeSupport(dir); err != nil {
		t.Fatal(err)
	}
	if err := verifyNodePackages(ctx, dir, ""); err != nil {
		t.Fatalf("repaired launcher cannot load: %v", err)
	}
	probe := `await import('camoufox'); console.log(JSON.stringify(process.report.getReport().sharedObjects.filter(p => p.toLowerCase().endsWith('vcruntime140.dll'))))`
	output, err := runInstall(ctx, dir, "", nil, nil, "--input-type=module", "-e", probe)
	if err != nil {
		t.Fatal(err)
	}
	var loaded []string
	if err := json.Unmarshal(output, &loaded); err != nil {
		t.Fatalf("reading loaded modules: %v: %s", err, output)
	}
	if len(loaded) != 1 || !strings.EqualFold(filepath.Clean(strings.TrimPrefix(loaded[0], `\\?\`)), filepath.Clean(dll)) {
		t.Fatalf("loaded VC runtime %v, want %s", loaded, dll)
	}
}
