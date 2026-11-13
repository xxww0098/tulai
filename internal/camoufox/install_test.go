package camoufox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	if err != nil || len(entries) != 6 {
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

func TestImpitNativePackageMatchesPlatform(t *testing.T) {
	for platform, want := range map[string]string{
		"windows/amd64": "impit-win32-x64-msvc",
		"darwin/amd64":  "impit-darwin-x64",
		"darwin/arm64":  "impit-darwin-arm64",
		"linux/amd64":   "impit-linux-x64-gnu",
		"linux/arm64":   "impit-linux-arm64-gnu",
	} {
		goos, goarch, _ := strings.Cut(platform, "/")
		if got := impitNativePackage(goos, goarch); got != want {
			t.Errorf("%s: got %q, want %q", platform, got, want)
		}
	}
	if got := impitNativePackage("windows", "arm64"); got != "" {
		t.Fatalf("unsupported platform returned %q", got)
	}
}

func TestImpitOptionalVersion(t *testing.T) {
	dir := t.TempDir()
	impitDir := filepath.Join(dir, "node_modules", "impit")
	if err := os.MkdirAll(impitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(impitDir, "package.json"), []byte(`{"optionalDependencies":{"impit-win32-x64-msvc":"0.14.8"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := impitOptionalVersion(dir, "impit-win32-x64-msvc"); got != "0.14.8" {
		t.Fatalf("version = %q", got)
	}
	if got := impitOptionalVersion(dir, "missing"); got != "" {
		t.Fatalf("missing package version = %q", got)
	}
	nativeDir := filepath.Join(dir, "node_modules", "impit-win32-x64-msvc")
	if err := os.MkdirAll(nativeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "package.json"), []byte(`{"version":"0.14.8"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := nodePackageVersion(dir, "impit-win32-x64-msvc"); got != "0.14.8" {
		t.Fatalf("native version = %q", got)
	}
}

func TestInstalledRequiresPinnedDependenciesAndBinary(t *testing.T) {
	root := t.TempDir()
	dir := runtimeDir(root)
	exe := filepath.Join(dir, "browser")
	nativeName := impitNativePackage(runtime.GOOS, runtime.GOARCH)
	if nativeName == "" {
		t.Fatalf("test platform %s/%s has no native package", runtime.GOOS, runtime.GOARCH)
	}
	native := filepath.Join(dir, "node_modules", nativeName, "package.json")
	impit := filepath.Join(dir, "node_modules", "impit", "package.json")
	nativeMeta := []byte(`{"version":"0.14.8"}`)
	impitMeta, _ := json.Marshal(map[string]any{"optionalDependencies": map[string]string{nativeName: "0.14.8"}})
	files := map[string][]byte{
		nodePath(dir): []byte("x"), exe: []byte("x"), filepath.Join(dir, "node_modules", "camoufox", "package.json"): []byte("x"),
		impit: impitMeta, native: nativeMeta,
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		files[filepath.Join(dir, "node_modules", nativeName, "impit-node.win32-x64-msvc.node")] = []byte("x")
	}
	for p, body := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0600); err != nil {
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
	if err := os.WriteFile(native, []byte(`{"version":"0.14.9"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Installed(root); ok {
		t.Fatal("mismatched native dependency accepted")
	}
	if err := os.WriteFile(native, nativeMeta, 0600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(native)
	if _, ok := Installed(root); ok {
		t.Fatal("missing native dependency accepted")
	}
	if err := os.WriteFile(native, nativeMeta, 0600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		binary := filepath.Join(dir, "node_modules", nativeName, "impit-node.win32-x64-msvc.node")
		if err := os.Remove(binary); err != nil {
			t.Fatal(err)
		}
		if _, ok := Installed(root); ok {
			t.Fatal("missing native binary accepted")
		}
		if err := os.WriteFile(binary, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
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

func TestRuntimeEnvDropsInheritedMirrorOverride(t *testing.T) {
	t.Setenv("TULAI_CAMOUFOX_MIRROR", "https://leak.example")
	for _, item := range runtimeEnv("", t.TempDir()) {
		if strings.HasPrefix(item, "TULAI_CAMOUFOX_MIRROR=") {
			t.Fatalf("inherited mirror override: %q", item)
		}
	}
}

func TestKernelMirrorAttempts(t *testing.T) {
	if base, explicit := kernelMirror(); base != kernelMirrorDefault || explicit {
		t.Fatalf("default mirror = %q explicit=%v", base, explicit)
	}
	if got, want := kernelAttempts(kernelMirrorDefault, false),
		[][]string{nil, {"TULAI_CAMOUFOX_MIRROR=" + kernelMirrorDefault}}; !equalAttempts(got, want) {
		t.Fatalf("default attempts = %v, want %v", got, want)
	}
	t.Setenv("TULAI_CAMOUFOX_MIRROR", "https://mirror.example/")
	base, explicit := kernelMirror()
	if base != "https://mirror.example" || !explicit {
		t.Fatalf("explicit mirror = %q explicit=%v", base, explicit)
	}
	if got, want := kernelAttempts(base, explicit),
		[][]string{{"TULAI_CAMOUFOX_MIRROR=https://mirror.example"}, nil}; !equalAttempts(got, want) {
		t.Fatalf("explicit attempts = %v, want %v", got, want)
	}
	t.Setenv("TULAI_CAMOUFOX_MIRROR", "off")
	if base, _ := kernelMirror(); base != "" {
		t.Fatalf("disabled mirror = %q", base)
	}
	if got, want := kernelAttempts("", false), [][]string{nil}; !equalAttempts(got, want) {
		t.Fatalf("disabled attempts = %v, want %v", got, want)
	}
}

func equalAttempts(got, want [][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if len(got[i]) != len(want[i]) {
			return false
		}
		for j := range got[i] {
			if got[i][j] != want[i][j] {
				return false
			}
		}
	}
	return true
}

func TestDownloadNodeFallsBackToMirror(t *testing.T) {
	payload := []byte("node archive")
	digest := sha256.Sum256(payload)
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer mirror.Close()
	restore := nodeDistBases
	nodeDistBases = []string{primary.URL, mirror.URL}
	defer func() { nodeDistBases = restore }()

	dest := filepath.Join(t.TempDir(), "node.zip")
	var texts []string
	err := downloadNode(context.Background(), dest, nodeArtifact{File: "node.zip", SHA256: hex.EncodeToString(digest[:])}, "",
		func(p Progress) {
			if p.Text != "" {
				texts = append(texts, p.Text)
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("downloaded %d bytes: %v", len(data), err)
	}
	if len(texts) == 0 || texts[len(texts)-1] != "官方源下载启动环境失败，正在通过国内镜像重试…" {
		t.Fatalf("mirror retry not reported: %v", texts)
	}
}

func TestDownloadNodeTriesEveryBaseBeforeFailing(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer primary.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer mirror.Close()
	restore := nodeDistBases
	nodeDistBases = []string{primary.URL, mirror.URL}
	defer func() { nodeDistBases = restore }()

	dest := filepath.Join(t.TempDir(), "node.zip")
	err := downloadNode(context.Background(), dest, nodeArtifact{File: "node.zip", SHA256: strings.Repeat("0", 64)}, "", nil)
	if err == nil || !strings.Contains(err.Error(), primary.URL) || !strings.Contains(err.Error(), mirror.URL) {
		t.Fatalf("error = %v, want failures from both bases", err)
	}
	if _, statErr := os.Stat(dest); statErr == nil {
		t.Fatal("failed download left a partial archive behind")
	}
}
