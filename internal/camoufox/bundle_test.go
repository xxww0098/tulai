package camoufox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsRuntimeBundleRoundTrip(t *testing.T) {
	stage := t.TempDir()
	files := map[string]string{
		"node/node.exe":                                                    "node binary",
		"node/LICENSE":                                                     "license",
		"node_modules/camoufox/package.json":                               "{}",
		"node_modules/impit/package.json":                                  `{"optionalDependencies":{"impit-win32-x64-msvc":"0.14.8"}}`,
		"node_modules/impit-win32-x64-msvc/package.json":                   `{"version":"0.14.8"}`,
		"node_modules/impit-win32-x64-msvc/impit-node.win32-x64-msvc.node": "native binary",
	}
	for name, content := range files {
		path := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stage, "node_modules", ".package-lock.json"), []byte("local npm metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "bundle.zip")
	if err := writeWindowsRuntimeBundle(context.Background(), stage, output); err != nil {
		t.Fatal(err)
	}
	if err := writeWindowsRuntimeBundle(context.Background(), stage, output); err != nil {
		t.Fatalf("rebuilding bundle: %v", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := unpackRuntimeBundle(context.Background(), bytes.NewReader(data), int64(len(data)), dest, "windows/amd64"); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Fatalf("%s: got %q, err=%v", name, got, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "node_modules", ".package-lock.json")); !os.IsNotExist(err) {
		t.Fatalf("bundled local npm metadata: %v", err)
	}
	if err := unpackRuntimeBundle(context.Background(), bytes.NewReader(data), int64(len(data)), t.TempDir(), "windows/arm64"); err == nil {
		t.Fatal("accepted bundle for another platform")
	}
}

func TestWindowsRuntimeBundleRejectsUnsafeEntries(t *testing.T) {
	for _, name := range []string{"../escape", "node_modules/../../escape", `node_modules\..\escape`, "/absolute", "other/file"} {
		t.Run(name, func(t *testing.T) {
			var data bytes.Buffer
			archive := zip.NewWriter(&data)
			manifest, _ := archive.Create("manifest.json")
			content, _ := json.Marshal(expectedBundleManifest("windows/amd64"))
			_, _ = manifest.Write(content)
			entry, err := archive.Create(name)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = entry.Write([]byte("x"))
			if err := archive.Close(); err != nil {
				t.Fatal(err)
			}
			if err := unpackRuntimeBundle(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), t.TempDir(), "windows/amd64"); err == nil {
				t.Fatalf("accepted %q", name)
			}
		})
	}
}
