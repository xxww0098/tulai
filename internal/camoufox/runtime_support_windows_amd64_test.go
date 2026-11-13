package camoufox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureRepairsExistingWindowsRuntimeSupport(t *testing.T) {
	root := t.TempDir()
	dir := runtimeDir(root)
	exe := filepath.Join(dir, "browser.exe")
	moduleDir := filepath.Join(dir, "node_modules", "impit-win32-x64-msvc")
	files := map[string][]byte{
		nodePath(dir): []byte("node"),
		exe:           []byte("browser"),
		filepath.Join(dir, "node_modules", "camoufox", "package.json"): []byte("{}"),
		filepath.Join(dir, "node_modules", "impit", "package.json"):    []byte(`{"optionalDependencies":{"impit-win32-x64-msvc":"0.14.8"}}`),
		filepath.Join(moduleDir, "package.json"):                       []byte(`{"version":"0.14.8"}`),
		filepath.Join(moduleDir, "impit-node.win32-x64-msvc.node"):     []byte("native module"),
	}
	for name, contents := range files {
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeDriver(dir); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(map[string]string{"executable": exe, "version": Version, "platform": "windows/amd64"})
	if err := os.WriteFile(filepath.Join(dir, "ready.json"), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	dll := filepath.Join(moduleDir, "vcruntime140.dll")
	for _, contents := range [][]byte{nil, []byte("damaged DLL")} {
		if contents != nil {
			if err := os.WriteFile(dll, contents, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := Ensure(context.Background(), root, "", nil); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(dll)
		if err != nil || !bytes.Equal(got, vcruntime140) {
			t.Fatalf("existing runtime was not repaired: %v", err)
		}
	}
	digest := sha256.Sum256(vcruntime140)
	if got := hex.EncodeToString(digest[:]); got != "d1f4225df2cd877dbf130d5668a021dce3f94118455ff5ec952061c30afc9ce7" {
		t.Fatalf("unexpected embedded redistributable: %s", got)
	}
}
