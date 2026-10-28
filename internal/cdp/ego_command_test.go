package cdp

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func decodeEgoImport(t *testing.T, entry string) *url.URL {
	t.Helper()
	const prefix, suffix = "await import(Buffer.from('", "','base64').toString())"
	if !strings.HasPrefix(entry, prefix) || !strings.HasSuffix(entry, suffix) || strings.ContainsAny(entry, "\"%!&|<>\r\n") {
		t.Fatalf("unsafe import entry: %q", entry)
	}
	for _, r := range entry {
		if r > 127 {
			t.Fatalf("non-ASCII import entry: %q", entry)
		}
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(entry, prefix), suffix))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(string(data))
	if err != nil || u.Scheme != "file" {
		t.Fatalf("invalid file URL: %q, %v", data, err)
	}
	return u
}

func TestEgoImportPreservesUnicodeAndSpecialPaths(t *testing.T) {
	for _, tc := range []struct{ path, host, wantPath string }{
		{"/tmp/中文 路径/a'b%!.mjs", "", "/tmp/中文 路径/a'b%!.mjs"},
		{"C:/Users/中文 name/a'b%!.mjs", "", "/C:/Users/中文 name/a'b%!.mjs"},
		{"//server/share/中文 name/a.mjs", "server", "/share/中文 name/a.mjs"},
	} {
		u := decodeEgoImport(t, egoImportEntry(tc.path))
		if u.Host != tc.host || u.Path != tc.wantPath {
			t.Fatalf("%q => %s, want host=%q path=%q", tc.path, u, tc.host, tc.wantPath)
		}
	}
}

func egoTestTempDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "中文 路径 & ! %TULAI_UNSET%")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TMPDIR", "TEMP", "TMP"} {
		t.Setenv(name, dir)
	}
	return dir
}

func TestEgoCommandWritesUTF8AndCleansScript(t *testing.T) {
	dir := egoTestTempDir(t)
	const source = "console.log('小帮手 — 中文脚本');\n"
	cmd, cleanup, err := egoCommand(context.Background(), filepath.Join(dir, "ego-browser.exe"), source)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	u := decodeEgoImport(t, cmd.Args[len(cmd.Args)-1])
	if !strings.HasSuffix(u.Path, "/bridge.mjs") {
		t.Fatalf("script is not .mjs: %s", u)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatalf("temporary script directory: %v, %v", files, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, files[0].Name(), "bridge.mjs"))
	if err != nil || string(data) != source {
		t.Fatalf("UTF-8 script changed: %q, %v", data, err)
	}
	cleanup()
	if files, err := os.ReadDir(dir); err != nil || len(files) != 0 {
		t.Fatalf("temporary script not removed: %v, %v", files, err)
	}
}

func TestEgoBatchCommandQuotesOnlySafeEntry(t *testing.T) {
	entry := egoImportEntry("C:/中文 %name% & !/bridge.mjs")
	want := `cmd.exe /d /v:off /s /c ""%TULAI_EGO_LAUNCHER%" nodejs -e "` + entry + `""`
	if got := egoBatchCommandLine(entry); got != want || strings.Count(got, "%") != 2 {
		t.Fatalf("unexpected cmd command line: %q", got)
	}
}

func TestEgoBridgeUTF8FileImport(t *testing.T) {
	if testing.Short() || os.Getenv("TULAI_EGO_E2E") != "1" {
		t.Skip("需显式 TULAI_EGO_E2E=1；仅创建本机 IPC，不访问 TaskSpace 或网页")
	}
	dir := egoTestTempDir(t)
	transport, err := startEgoTransport(context.Background(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	transport.close()
	if files, err := os.ReadDir(dir); err != nil || len(files) != 0 {
		t.Fatalf("control process left temporary scripts: %v, %v", files, err)
	}
}
