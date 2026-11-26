package fingerprint

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsRuntimeBundleRoundTrip(t *testing.T) {
	stage := t.TempDir()
	files := map[string]string{
		"node/node.exe": "node binary",
		"node/LICENSE":  "license",
		"node_modules/playwright-core/package.json": `{"version":"` + driverVersion + `"}`,
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

func TestCanonicalLockNormalizesCRLF(t *testing.T) {
	lf, err := driver.ReadFile("driver/package-lock.json")
	if err != nil {
		t.Fatal(err)
	}
	crlf := bytes.ReplaceAll(canonicalLock(lf), []byte("\n"), []byte("\r\n"))
	if !bytes.Equal(canonicalLock(crlf), canonicalLock(lf)) {
		t.Fatal("canonicalLock did not normalize CRLF to LF")
	}
	sum := sha256.Sum256(canonicalLock(crlf))
	if got, want := hex.EncodeToString(sum[:]), expectedBundleManifest("windows/amd64").Lock; got != want {
		t.Fatalf("normalized lock sha256 = %s, want %s", got, want)
	}

	var data bytes.Buffer
	archive := zip.NewWriter(&data)
	manifestEntry, err := archive.Create("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	badManifest := expectedBundleManifest("windows/amd64")
	badManifest.Lock = strings.Repeat("0", 64)
	content, err := json.Marshal(badManifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifestEntry.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	err = unpackRuntimeBundle(context.Background(), bytes.NewReader(data.Bytes()), int64(data.Len()), t.TempDir(), "windows/amd64")
	if err == nil || err.Error() != "内置启动环境版本或依赖锁不匹配" {
		t.Fatalf("unpack err = %v, want 内置启动环境版本或依赖锁不匹配", err)
	}
}

func TestBundledWindowsArchiveUnpacks(t *testing.T) {
	const bundlePath = "bundled/windows-amd64.zip"
	f, err := os.Open(bundlePath)
	if os.IsNotExist(err) && os.Getenv("TULAI_REQUIRE_WINDOWS_BUNDLE") != "1" {
		t.Skip("bundled/windows-amd64.zip not present")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := unpackRuntimeBundle(context.Background(), f, info.Size(), t.TempDir(), "windows/amd64"); err != nil {
		t.Fatal(err)
	}
}
