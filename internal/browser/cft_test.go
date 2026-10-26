package browser

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
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestPinnedManifest(t *testing.T) {
	manifest, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Version != "156.0.8078.47" {
		t.Fatalf("version = %s", manifest.Version)
	}
	want := map[string]string{
		"linux64":     "chrome-linux64/chrome",
		"linux-arm64": "chrome-linux-arm64/chrome",
		"mac-arm64":   "chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
		"mac-x64":     "chrome-mac-x64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
		"win64":       "chrome-win64/chrome.exe",
	}
	if len(manifest.Platforms) != len(want) {
		t.Fatalf("platforms = %d", len(manifest.Platforms))
	}
	for platform, exe := range want {
		spec := manifest.Platforms[platform]
		if spec.Exe != exe || spec.File == "" || len(spec.SHA256) != 64 {
			t.Fatalf("%s: %+v", platform, spec)
		}
		if _, err := hex.DecodeString(spec.SHA256); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := manifest.Platforms["win-arm64"]; ok {
		t.Fatal("win-arm64 must not be pinned")
	}
	if !strings.Contains(defaultMirrors[0], "npmmirror") || !strings.Contains(defaultMirrors[1], "storage.googleapis.com") {
		t.Fatalf("mirrors = %v", defaultMirrors)
	}
}

func TestDownloadVerifiesChecksumResumesAndFallsBack(t *testing.T) {
	payload := zipBytes(t, map[string]string{"chrome-linux64/chrome": "chrome-bin"}, nil)
	sum := sha256Hex(payload)
	var hits int
	resume := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/9.9.9/linux64/chrome-linux64.zip" {
			http.NotFound(w, r)
			return
		}
		hits++
		if hits == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload[:len(payload)/2])
			return
		}
		start := 0
		if _, err := parseRangeStart(r.Header.Get("Range"), &start); err != nil || start == 0 {
			t.Errorf("resume range = %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(len(payload)-1)+"/"+strconv.Itoa(len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start:])
	}))
	defer resume.Close()

	dir := t.TempDir()
	old := filepath.Join(dir, "runtime", "cft", "0.0.1")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	art := cftArtifact{Version: "9.9.9", Platform: "linux64", File: "chrome-linux64.zip", SHA256: sum, Exe: "chrome-linux64/chrome"}
	var lastDone, lastTotal int64
	rt, err := installArtifact(context.Background(), dir, art, []string{resume.URL}, resume.Client(), func(done, total int64) {
		lastDone, lastTotal = done, total
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if hits < 2 {
		t.Fatalf("hits = %d", hits)
	}
	if lastTotal != int64(len(payload)) || lastDone != lastTotal {
		t.Fatalf("progress %d/%d", lastDone, lastTotal)
	}
	if rt.Kind != KindBuiltin || rt.Version != "9.9.9" || rt.Path != installedExe(dir, art) {
		t.Fatalf("runtime = %+v", rt)
	}
	info, err := os.Stat(rt.Path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatalf("executable mode = %v", info.Mode())
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old version directory was kept")
	}
	before := hits
	again, err := installArtifact(context.Background(), dir, art, []string{resume.URL}, resume.Client(), nil, false)
	if err != nil || again.Path != rt.Path || hits != before {
		t.Fatalf("second install redownloaded: %+v %v hits %d", again, err, hits)
	}
}

func TestDownloadChecksumMismatchCleansPartial(t *testing.T) {
	payload := zipBytes(t, map[string]string{"chrome-linux64/chrome": "chrome-bin"}, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()
	dir := t.TempDir()
	art := cftArtifact{Version: "9.9.9", Platform: "linux64", File: "chrome-linux64.zip", SHA256: strings.Repeat("ab", 32), Exe: "chrome-linux64/chrome"}
	_, err := installArtifact(context.Background(), dir, art, []string{srv.URL}, srv.Client(), nil, false)
	if !errors.Is(err, errChecksum) && !strings.Contains(err.Error(), "校验") {
		t.Fatalf("err = %v", err)
	}
	partial := filepath.Join(dir, "runtime", "cft", art.Version+".zip.partial")
	if _, statErr := os.Stat(partial); !os.IsNotExist(statErr) {
		t.Fatal("partial file left behind after a bad checksum")
	}
	if _, statErr := os.Stat(installedExe(dir, art)); !os.IsNotExist(statErr) {
		t.Fatal("executable extracted from a bad archive")
	}
}

func TestDownloadFallsBackToSecondMirror(t *testing.T) {
	payload := zipBytes(t, map[string]string{"chrome-linux64/chrome": "chrome-bin"}, nil)
	var badHits, goodHits int
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		badHits++
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodHits++
		_, _ = w.Write(payload)
	}))
	defer good.Close()
	dir := t.TempDir()
	art := cftArtifact{Version: "9.9.9", Platform: "linux64", File: "chrome-linux64.zip", SHA256: sha256Hex(payload), Exe: "chrome-linux64/chrome"}
	rt, err := installArtifact(context.Background(), dir, art, []string{bad.URL, good.URL}, good.Client(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if badHits == 0 || goodHits == 0 || rt.Path == "" {
		t.Fatalf("bad=%d good=%d rt=%+v", badHits, goodHits, rt)
	}
}

func TestExtractRejectsZipSlipAndEscapingSymlink(t *testing.T) {
	slip := zipBytes(t, map[string]string{"../evil.txt": "nope", "ok.txt": "ok"}, nil)
	dir := t.TempDir()
	err := extractZipBytes(t, dir, slip, "ok.txt")
	if err == nil || !strings.Contains(err.Error(), "不安全") {
		t.Fatalf("slip err = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "evil.txt")); !os.IsNotExist(statErr) {
		t.Fatal("zip slip wrote outside the destination")
	}

	link := zipBytes(t, map[string]string{"chrome-linux64/chrome": "bin"}, map[string]string{"chrome-linux64/escape": "../../outside"})
	err = extractZipBytes(t, t.TempDir(), link, "chrome-linux64/chrome")
	if err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("symlink err = %v", err)
	}

	safe := zipBytes(t, map[string]string{"app/bin": "bin"}, map[string]string{"app/current": "bin"})
	dest := t.TempDir()
	if err := extractZipBytes(t, dest, safe, "app/bin"); err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(dest, "out", "app", "current"))
	if err != nil || got != "bin" {
		t.Fatalf("symlink = %q %v", got, err)
	}
}

func TestDownloadCancelKeepsPartial(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		w.WriteHeader(http.StatusOK)
		select {
		case <-started:
		default:
			close(started)
		}
		<-r.Context().Done()
	}))
	defer srv.Close()
	dir := t.TempDir()
	art := cftArtifact{Version: "9.9.9", Platform: "linux64", File: "chrome-linux64.zip", SHA256: strings.Repeat("cd", 32), Exe: "chrome-linux64/chrome"}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := installArtifact(ctx, dir, art, []string{srv.URL}, srv.Client(), nil, false)
		done <- err
	}()
	<-started
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Stat(installedExe(dir, art)); !os.IsNotExist(statErr) {
		t.Fatal("cancel extracted a browser")
	}
}

func extractZipBytes(t *testing.T, dest string, payload []byte, exe string) error {
	t.Helper()
	partial := filepath.Join(dest, "browser.zip")
	if err := os.WriteFile(partial, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	return extractZip(partial, filepath.Join(dest, "out"), exe)
}

func zipBytes(t *testing.T, files map[string]string, links map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(0o755)
		writer, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, body); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range links {
		hdr := &zip.FileHeader{Name: name, Method: zip.Store}
		hdr.SetMode(os.ModeSymlink | 0o755)
		writer, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, target); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func parseRangeStart(header string, start *int) (int, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, errors.New(header)
	}
	n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(header, "bytes="), "-"))
	if err != nil {
		return 0, err
	}
	*start = n
	return n, nil
}
