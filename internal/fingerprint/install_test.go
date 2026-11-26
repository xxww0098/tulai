package fingerprint

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		`{"downloaded":"/tmp/fingerprint.zip"}`,
		`{"event":"console","text":"hi"}`,
		"not json",
		"",
	} {
		if _, ok := parseProgressLine([]byte(line)); ok {
			t.Fatalf("parsed %q as progress", line)
		}
	}
}

func TestNativeLauncherErrorDistinguishesPolicyAndMissingModule(t *testing.T) {
	for _, msg := range []string{"An Application Control policy has blocked this file.", "应用程序控制策略已阻止此文件。"} {
		if err := launcherComponentError("playwright-core", errors.New(msg)); !errors.Is(err, errApplicationControl) || !strings.Contains(err.Error(), "playwright-core") {
			t.Fatalf("policy reason lost: %v", err)
		}
	}
	err := launcherComponentError("playwright-core", errors.New("Cannot find module"))
	if errors.Is(err, errApplicationControl) || !strings.Contains(err.Error(), "Cannot find module") {
		t.Fatalf("missing module reported as policy: %v", err)
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
	if err != nil || len(entries) != len(bridgeScripts)+2 {
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
		t.Fatal("Windows ARM64 has no Fingerprint browser")
	}
	if Supported("darwin", "amd64") {
		t.Fatal("macOS Intel has no Fingerprint browser")
	}
}

func TestInstalledRequiresPinnedPlaywrightAndBinary(t *testing.T) {
	root := t.TempDir()
	dir := fingerprintRuntimeDir(root)
	exe := filepath.Join(dir, "browsers", "chrome")
	pwMeta := []byte(`{"version":"` + driverVersion + `"}`)
	files := map[string][]byte{
		nodePath(dir):                      []byte("x"),
		exe:                                []byte("x"),
		filepath.Join(dir, "browser-path"): []byte("chrome"),
		filepath.Join(dir, "node_modules", "playwright-core", "package.json"): pwMeta,
	}
	for p, body := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeDriver(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := Installed(root); !ok {
		t.Fatal("complete Fingerprint runtime rejected")
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "playwright-core", "package.json"), []byte(`{"version":"0.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Installed(root); ok {
		t.Fatal("mismatched playwright-core version accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "playwright-core", "package.json"), pwMeta, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "browser-path"), []byte("../escape"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := Installed(root); ok {
		t.Fatal("escaping browser-path accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "browser-path"), []byte("chrome"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(exe)
	if _, ok := Installed(root); ok {
		t.Fatal("missing browser accepted")
	}
}

func TestRuntimeEnvDropsInheritedMirrorOverride(t *testing.T) {
	t.Setenv("TULAI_FINGERPRINT_MIRROR", "https://leak.example")
	t.Setenv("TULAI_FINGERPRINT_MIRRORS", "https://leak2.example")
	t.Setenv("TULAI_FINGERPRINT_DIRECT_FIRST", "1")
	for _, item := range runtimeEnv("", t.TempDir()) {
		if strings.HasPrefix(item, "TULAI_FINGERPRINT_MIRROR=") ||
			strings.HasPrefix(item, "TULAI_FINGERPRINT_MIRRORS=") ||
			strings.HasPrefix(item, "TULAI_FINGERPRINT_DIRECT_FIRST=") {
			t.Fatalf("inherited mirror override: %q", item)
		}
	}
}

func TestKernelMirrorAttempts(t *testing.T) {
	mirrors, explicit := fingerprintMirrors()
	if explicit || len(mirrors) != len(kernelMirrorDefaults) || mirrors[0] != kernelMirrorDefaults[0] {
		t.Fatalf("default mirrors = %v explicit=%v", mirrors, explicit)
	}
	defaultCSV := strings.Join(kernelMirrorDefaults, ",")
	if got, want := kernelAttempts(mirrors, false, ""),
		[][]string{{"TULAI_FINGERPRINT_MIRRORS=" + defaultCSV}, nil}; !equalAttempts(got, want) {
		t.Fatalf("default direct attempts = %v, want %v", got, want)
	}
	if got, want := kernelAttempts(mirrors, false, "http://127.0.0.1:7890"),
		[][]string{
			{"TULAI_FINGERPRINT_MIRRORS=" + defaultCSV, "TULAI_FINGERPRINT_DIRECT_FIRST=1"},
			{"TULAI_FINGERPRINT_MIRRORS=" + defaultCSV},
			nil,
		}; !equalAttempts(got, want) {
		t.Fatalf("default proxy attempts = %v, want %v", got, want)
	}
	t.Setenv("TULAI_FINGERPRINT_MIRROR", "https://mirror.example/")
	mirrors, explicit = fingerprintMirrors()
	if len(mirrors) != 1 || mirrors[0] != "https://mirror.example" || !explicit {
		t.Fatalf("explicit mirrors = %v explicit=%v", mirrors, explicit)
	}
	wantAll := strings.Join(append([]string{"https://mirror.example"}, kernelMirrorDefaults...), ",")
	if got, want := kernelAttempts(mirrors, explicit, ""),
		[][]string{{"TULAI_FINGERPRINT_MIRROR=https://mirror.example", "TULAI_FINGERPRINT_MIRRORS=" + wantAll}, nil}; !equalAttempts(got, want) {
		t.Fatalf("explicit attempts = %v, want %v", got, want)
	}
	t.Setenv("TULAI_FINGERPRINT_MIRROR", "off")
	if mirrors, _ := fingerprintMirrors(); len(mirrors) != 0 {
		t.Fatalf("disabled mirrors = %v", mirrors)
	}
	if got, want := kernelAttempts(nil, false, ""), [][]string{nil}; !equalAttempts(got, want) {
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
