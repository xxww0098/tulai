package camoufox

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestParseProgressLine(t *testing.T) {
	p, ok := parseProgressLine([]byte(`{"event":"progress","stage":"browser","done":123,"total":456}`))
	if !ok || p.Stage != StageBrowser || p.Done != 123 || p.Total != 456 || p.Text != stageText(StageBrowser) {
		t.Fatalf("progress = %+v ok=%v", p, ok)
	}
	p, ok = parseProgressLine([]byte(`{"event":"progress","stage":"browser","done":456,"total":456}`))
	if !ok || p.Text != stageText(StageUnpack) {
		t.Fatalf("completed progress = %+v ok=%v", p, ok)
	}
	for _, line := range []string{
		`{"executable":"/tmp/camoufox"}`,
		`{"event":"console","text":"hi"}`,
		"not json",
		"",
	} {
		if _, ok := parseProgressLine([]byte(line)); ok {
			t.Fatalf("parsed %q as progress", line)
		}
	}
}

func TestProgressReaderReportsBytes(t *testing.T) {
	var calls [][2]int64
	reader := &progressReader{
		r:      strings.NewReader(strings.Repeat("x", 4096)),
		total:  4096,
		report: func(done, total int64) { calls = append(calls, [2]int64{done, total}) },
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatal(err)
	}
	last := calls[len(calls)-1]
	if last[0] != 4096 || last[1] != 4096 {
		t.Fatalf("last report = %v, calls=%v", last, calls)
	}
}

func TestConcurrentDriverRefreshUsesUniqueStagingFiles(t *testing.T) {
	dir := t.TempDir()
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			if err := writeDriver(dir); err != nil {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 4 {
		t.Fatalf("unfinished driver refresh: %v %v", entries, err)
	}
	for _, entry := range entries {
		want, _ := driver.ReadFile("driver/" + entry.Name())
		got, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil || string(got) != string(want) {
			t.Fatalf("partial %s: %v", entry.Name(), err)
		}
	}
}

func TestNodeArchiveCannotEscapeOrInstallLinks(t *testing.T) {
	for _, name := range []string{"node/../../outside", "node/../outside", `node\..\outside`, "node//absolute"} {
		if _, err := archivePath(t.TempDir(), name); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
	for _, tc := range []struct {
		name string
		mode os.FileMode
		fail bool
	}{{"node/bin/node", 0755, false}, {"node/../../escape", 0755, true}, {"node/bin/node", os.ModeSymlink | 0755, true}} {
		t.Run(tc.name+tc.mode.String(), func(t *testing.T) {
			archive := filepath.Join(t.TempDir(), "node.zip")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			z := zip.NewWriter(f)
			header := &zip.FileHeader{Name: tc.name}
			header.SetMode(tc.mode)
			w, err := z.CreateHeader(header)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte("node"))
			_ = z.Close()
			_ = f.Close()
			root := t.TempDir()
			err = extractNode(archive, root)
			if (err != nil) != tc.fail {
				t.Fatalf("extract err=%v", err)
			}
			if !tc.fail {
				if b, err := os.ReadFile(filepath.Join(root, "bin", "node")); err != nil || string(b) != "node" {
					t.Fatalf("extraction %s %v", b, err)
				}
			}
		})
	}
}

func TestNodeArtifactsMatchPlatform(t *testing.T) {
	for platform, file := range map[string]string{
		"darwin/arm64":  "node-v24.22.2-darwin-arm64.tar.gz",
		"darwin/amd64":  "node-v24.22.2-darwin-x64.tar.gz",
		"windows/amd64": "node-v24.22.2-win-x64.zip",
		"linux/amd64":   "node-v24.22.2-linux-x64.tar.gz",
		"linux/arm64":   "node-v24.22.2-linux-arm64.tar.gz",
	} {
		artifact, ok := nodeArtifacts[platform]
		if !ok || artifact.File != file || len(artifact.SHA256) != 64 {
			t.Errorf("%s: %+v", platform, artifact)
		}
	}
	if Supported("windows", "arm64") {
		t.Fatal("Windows ARM64 has no Camoufox browser")
	}
}

func TestInstalledRequiresPinnedDependenciesAndBinary(t *testing.T) {
	root := t.TempDir()
	dir := runtimeDir(root)
	exe := filepath.Join(dir, "browser")
	for _, p := range []string{nodePath(dir), exe, filepath.Join(dir, "node_modules", "camoufox", "package.json")} {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeDriver(dir); err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(map[string]string{"executable": exe, "version": Version, "platform": runtime.GOOS + "/" + runtime.GOARCH})
	if err := os.WriteFile(filepath.Join(dir, "ready.json"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Installed(root); !ok {
		t.Fatal("complete runtime rejected")
	}
	for _, platform := range []string{"", "different/platform"} {
		wrong, _ := json.Marshal(map[string]string{"executable": exe, "version": Version, "platform": platform})
		if err := os.WriteFile(filepath.Join(dir, "ready.json"), wrong, 0600); err != nil {
			t.Fatal(err)
		}
		if _, ok := Installed(root); ok {
			t.Fatalf("accepted runtime for %q", platform)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ready.json"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Installed(root); ok {
		t.Fatal("changed dependency lock accepted")
	}
	if err := writeDriver(dir); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(exe)
	if _, ok := Installed(root); ok {
		t.Fatal("missing browser accepted")
	}
}

func TestRuntimeCacheIsSharedByInstallAndLaunch(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runtime", "camoufox")
	want := "TULAI_CAMOUFOX_CACHE=" + filepath.Join(root, "cache", runtime.GOOS+"-"+runtime.GOARCH)
	for _, dir := range []string{filepath.Join(root, ".install-test"), filepath.Join(root, Version)} {
		env := runtimeEnv("", dir)
		if env[len(env)-1] != want {
			t.Fatalf("cache for %s = %q, want %q", dir, env[len(env)-1], want)
		}
	}
}
