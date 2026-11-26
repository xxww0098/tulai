package update

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMirrorFallbackAndChecksum(t *testing.T) {
	payload := []byte("mirror-bytes")
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])
	bad := []byte("captive-portal")
	sumsText := sumHex + "  tulai-0.2.0-linux-amd64.tar.gz\n"
	sumsSum := sha256.Sum256([]byte(sumsText))

	var mirrorHits int
	var primary *httptest.Server
	var mirror *httptest.Server
	mirror = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits++
		if !strings.Contains(r.URL.RequestURI(), "/asset") {
			http.NotFound(w, r)
			return
		}
		w.Write(payload)
	}))
	defer mirror.Close()
	primary = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/xxww0098/tulai/releases/latest":
			rel := Release{
				Tag: "v0.2.0",
				Assets: []Asset{
					{Name: "SHA256SUMS", State: "uploaded", BrowserDownloadURL: serverURL(r, "/sums"), Size: int64(len(sumsText)), Digest: "sha256:" + hex.EncodeToString(sumsSum[:])},
					{Name: "tulai-0.2.0-linux-amd64.tar.gz", State: "uploaded", BrowserDownloadURL: serverURL(r, "/asset"), Size: int64(len(payload)), Digest: "sha256:" + sumHex},
				},
			}
			json.NewEncoder(w).Encode(rel)
		case "/sums":
			w.Write([]byte(sumsText))
		case "/asset":
			w.WriteHeader(http.StatusBadGateway)
			w.Write(bad)
		default:
			http.NotFound(w, r)
		}
	}))
	defer primary.Close()

	client := NewClient("0.1.2")
	client.API = primary.URL
	client.DownloadDir = t.TempDir()
	client.Mirrors = []string{mirror.URL + "/"}
	got, err := client.Prepare(context.Background(), "0.1.2", ModeLinuxUser, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(got.Path)
	if err != nil || string(body) != string(payload) {
		t.Fatalf("body %q err %v", body, err)
	}
	if mirrorHits == 0 {
		t.Fatal("mirror was not used")
	}
}

func TestChecksumMismatchTriesMirror(t *testing.T) {
	good := []byte("good-bytes")
	sum := sha256.Sum256(good)
	sumHex := hex.EncodeToString(sum[:])
	sumsText := sumHex + "  tulai-0.2.0-linux-amd64.tar.gz\n"
	sumsSum := sha256.Sum256([]byte(sumsText))
	var mirrorHits int
	var primary *httptest.Server
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits++
		w.Write(good)
	}))
	defer mirror.Close()
	primary = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/xxww0098/tulai/releases/latest":
			json.NewEncoder(w).Encode(Release{
				Tag: "v0.2.0",
				Assets: []Asset{
					{Name: "SHA256SUMS", State: "uploaded", BrowserDownloadURL: serverURL(r, "/sums"), Size: int64(len(sumsText)), Digest: "sha256:" + hex.EncodeToString(sumsSum[:])},
					{Name: "tulai-0.2.0-linux-amd64.tar.gz", State: "uploaded", BrowserDownloadURL: serverURL(r, "/asset"), Size: int64(len(good)), Digest: "sha256:" + sumHex},
				},
			})
		case "/sums":
			w.Write([]byte(sumsText))
		case "/asset":
			w.Write([]byte("not-the-release"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer primary.Close()

	client := NewClient("0.1.0")
	client.API = primary.URL
	client.DownloadDir = t.TempDir()
	client.Mirrors = []string{mirror.URL + "/"}
	got, err := client.Prepare(context.Background(), "0.1.0", ModeLinuxUser, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(got.Path)
	if err != nil || string(body) != string(good) {
		t.Fatalf("body %q err %v", body, err)
	}
	if mirrorHits == 0 {
		t.Fatal("checksum miss did not fall through to the mirror")
	}
}

func TestAPIMirrorFallback(t *testing.T) {
	var primary *httptest.Server
	var mirror *httptest.Server
	mirror = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RequestURI(), "releases/latest") {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(Release{Tag: "v0.3.0", Name: "tulai 0.3.0"})
	}))
	defer mirror.Close()
	primary = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer primary.Close()

	client := NewClient("0.1.2")
	client.API = primary.URL
	client.Mirrors = []string{mirror.URL + "/"}
	rel, err := client.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	version, err := rel.Version()
	if err != nil || version != "0.3.0" {
		t.Fatalf("version %q err %v", version, err)
	}
}

func TestResumeDownload(t *testing.T) {
	payload := []byte("0123456789abcdef")
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Range") == "" {
			w.Write(payload[:6])
			return
		}
		if r.Header.Get("Range") != "bytes=6-" {
			t.Errorf("range %q", r.Header.Get("Range"))
			http.Error(w, "range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", "bytes 6-15/16")
		w.WriteHeader(http.StatusPartialContent)
		w.Write(payload[6:])
	}))
	defer srv.Close()
	asset := Asset{Name: "tulai.bin", BrowserDownloadURL: srv.URL + "/tulai.bin", Size: int64(len(payload))}
	dest := filepath.Join(t.TempDir(), "tulai.bin")
	client := NewClient("0.1.2")
	if err := client.downloadFile(context.Background(), "v0.2.0", asset, dest); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(dest)
	if err != nil || string(body) != string(payload) {
		t.Fatalf("resumed %q err %v", body, err)
	}
	if calls < 2 {
		t.Fatalf("resume did not retry: %d", calls)
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatal("partial file left after success")
	}
}

func TestDownloadCleansUpOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "tulai.bin")
	if err := os.WriteFile(dest+".partial", []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	asset := Asset{Name: "tulai.bin", BrowserDownloadURL: srv.URL + "/tulai.bin", Size: 8}
	client := NewClient("0.1.2")
	if err := client.downloadFile(context.Background(), "v0.2.0", asset, dest); err == nil {
		t.Fatal("accepted a failed download")
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatal("partial file left after failure")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("dest left after failure")
	}
}

func TestSelectAppImageFallsBackToTar(t *testing.T) {
	assets := []Asset{{Name: "tulai-0.2.0-linux-arm64.tar.gz", State: "uploaded"}}
	got, err := Select(assets, KindLinuxAppImage, "0.2.0", "arm64")
	if err != nil || got.Name != "tulai-0.2.0-linux-arm64.tar.gz" {
		t.Fatalf("fallback %+v %v", got, err)
	}
	with := append(assets, Asset{Name: "tulai-0.2.0-linux-arm64.AppImage", State: "uploaded"})
	got, err = Select(with, KindLinuxAppImage, "0.2.0", "arm64")
	if err != nil || got.Name != "tulai-0.2.0-linux-arm64.AppImage" {
		t.Fatalf("appimage %+v %v", got, err)
	}
}

func TestCandidateOrderingRespectsExplicitMirrorAndProxy(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "")
	}
	const target = "https://github.com/xxww0098/tulai/releases/download/v0.2.0/tulai.bin"

	autoNoProxy := NewClient("0.1.0")
	autoNoProxy.ApplyMirrorSetting("")
	urls, err := autoNoProxy.candidateURLs(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(urls[0], DefaultMirrors[0]) || urls[len(urls)-1] != target {
		t.Fatalf("auto no-proxy order = %v", urls)
	}

	autoWithProxy := NewClient("0.1.0")
	autoWithProxy.ProxyURL = "http://127.0.0.1:7890"
	autoWithProxy.ApplyMirrorSetting("")
	urls, err = autoWithProxy.candidateURLs(target)
	if err != nil {
		t.Fatal(err)
	}
	if urls[0] != target || !strings.HasPrefix(urls[1], DefaultMirrors[0]) {
		t.Fatalf("auto with-proxy order = %v", urls)
	}

	explicit := NewClient("0.1.0")
	explicit.ProxyURL = "http://127.0.0.1:7890"
	explicit.ApplyMirrorSetting("https://custom.example/")
	urls, err = explicit.candidateURLs(target)
	if err != nil {
		t.Fatal(err)
	}
	if urls[0] != "https://custom.example/"+target || urls[len(urls)-1] != target {
		t.Fatalf("explicit mirror order = %v", urls)
	}

	directOnly := NewClient("0.1.0")
	directOnly.ApplyMirrorSetting("off")
	urls, err = directOnly.candidateURLs(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != target {
		t.Fatalf("direct only = %v", urls)
	}
}

func TestAPIRaceSkips403AndHTMLMirrors(t *testing.T) {
	badAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Invalid input.", http.StatusForbidden)
	}))
	defer badAPI.Close()

	htmlMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>error page</html>"))
	}))
	defer htmlMirror.Close()

	goodMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(Release{Tag: "v0.3.1", Name: "tulai 0.3.1"})
	}))
	defer goodMirror.Close()

	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer primary.Close()

	client := NewClient("0.1.0")
	client.API = primary.URL
	client.Mirrors = []string{badAPI.URL + "/", htmlMirror.URL + "/", goodMirror.URL + "/"}
	rel, err := client.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ver, err := rel.Version(); err != nil || ver != "0.3.1" {
		t.Fatalf("version = %q, err = %v", ver, err)
	}
}

func TestExtractReleaseTag(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		finalURL string
		want     string
	}{
		{
			name: "json",
			body: `{"id":1,"tag_name":"v0.1.38"}`,
			want: "v0.1.38",
		},
		{
			name:     "redirect_url",
			body:     "<html></html>",
			finalURL: "https://github.com/xxww0098/tulai/releases/tag/v0.1.39",
			want:     "v0.1.39",
		},
		{
			name: "html_link",
			body: `<a href="/xxww0098/tulai/releases/tag/v0.2.0">Release</a>`,
			want: "v0.2.0",
		},
	}
	for _, tc := range cases {
		if got := extractReleaseTag([]byte(tc.body), tc.finalURL, "xxww0098", "tulai"); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func serveRangePayload(w http.ResponseWriter, r *http.Request, payload []byte) {
	rangeHdr := r.Header.Get("Range")
	if rangeHdr == "" {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
		return
	}
	rawSpec := strings.TrimPrefix(rangeHdr, "bytes=")
	startStr, endStr, ok := strings.Cut(rawSpec, "-")
	if !ok {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	start, err1 := strconv.ParseInt(startStr, 10, 64)
	end := int64(len(payload) - 1)
	if endStr != "" {
		parsedEnd, err2 := strconv.ParseInt(endStr, 10, 64)
		if err2 != nil {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if parsedEnd < end {
			end = parsedEnd
		}
	}
	if err1 != nil || start < 0 || start >= int64(len(payload)) || end < start {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(payload[start : end+1])
}

func TestLargeAssetRacesFastRangeMirrorAndReportsProgress(t *testing.T) {
	payload := make([]byte, (4<<20)+137)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])

	var slowHits, fastHits atomic.Int32
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slowHits.Add(1)
		time.Sleep(180 * time.Millisecond)
		serveRangePayload(w, r, payload)
	}))
	defer slow.Close()

	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fastHits.Add(1)
		serveRangePayload(w, r, payload)
	}))
	defer fast.Close()

	client := NewClient("0.1.0")
	client.Mirrors = []string{fast.URL + "/"}
	var mu sync.Mutex
	var events []Progress
	client.OnProgress = func(p Progress) {
		mu.Lock()
		events = append(events, p)
		mu.Unlock()
	}

	dest := filepath.Join(t.TempDir(), "tulai-big.exe")
	asset := Asset{
		Name:               "tulai-big.exe",
		BrowserDownloadURL: slow.URL + "/tulai-big.exe",
		Size:               int64(len(payload)),
	}
	if err := client.downloadVerified(context.Background(), "v0.2.0", asset, dest, sumHex); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded mismatch: len=%d want=%d err=%v", len(got), len(payload), err)
	}
	if fastHits.Load() < 2 {
		t.Fatalf("fast Range mirror served only %d requests", fastHits.Load())
	}
	if slowHits.Load() > 1 {
		t.Fatalf("slow primary hit %d times (>1 probe)", slowHits.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(events) == 0 {
		t.Fatal("expected progress events")
	}
	last := events[len(events)-1]
	if last.Done != int64(len(payload)) || last.Total != int64(len(payload)) || last.Source == "" {
		t.Fatalf("unexpected final progress: %+v", last)
	}
}

func TestLargeAssetFailsOverChunksAndBlacklistsBadMirror(t *testing.T) {
	payload := make([]byte, (4<<20)+511)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	corrupt := append([]byte(nil), payload...)
	corrupt[len(corrupt)-1] ^= 0xff

	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])

	// Bad mirror wins the initial probe fast, but serves corrupted last byte.
	badMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveRangePayload(w, r, corrupt)
	}))
	defer badMirror.Close()

	// Backup mirror starts slightly slower during probe, then serves healthy chunks cleanly.
	var backupHits atomic.Int32
	backupMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if backupHits.Add(1) == 1 {
			time.Sleep(60 * time.Millisecond)
		}
		serveRangePayload(w, r, payload)
	}))
	defer backupMirror.Close()

	client := NewClient("0.1.0")
	client.Mirrors = []string{badMirror.URL + "/", backupMirror.URL + "/"}
	client.PreferMirror = true

	dest := filepath.Join(t.TempDir(), "tulai-failover.exe")
	asset := Asset{
		Name:               "tulai-failover.exe",
		BrowserDownloadURL: backupMirror.URL + "/tulai-failover.exe",
		Size:               int64(len(payload)),
	}
	if err := client.downloadVerified(context.Background(), "v0.2.0", asset, dest, sumHex); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("failover payload mismatch: err=%v", err)
	}
}

func TestLargeAsset200StreamResumesVia206Fallback(t *testing.T) {
	payload := make([]byte, (2<<20)+333)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])

	// Mirror ignores Range (returns 200) and truncates midway.
	dropMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload[:700*1024])
	}))
	defer dropMirror.Close()

	// Backup supports 206 Range, delayed on initial probe so dropMirror wins first.
	var rangeCalls atomic.Int32
	rangeBackup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Range"), "bytes=0-") {
			time.Sleep(80 * time.Millisecond)
		}
		rangeCalls.Add(1)
		serveRangePayload(w, r, payload)
	}))
	defer rangeBackup.Close()

	client := NewClient("0.1.0")
	client.Mirrors = []string{dropMirror.URL + "/"}
	client.PreferMirror = true

	dest := filepath.Join(t.TempDir(), "tulai-200-resume.exe")
	asset := Asset{
		Name:               "tulai-200-resume.exe",
		BrowserDownloadURL: rangeBackup.URL + "/tulai-200-resume.exe",
		Size:               int64(len(payload)),
	}
	if err := client.downloadVerified(context.Background(), "v0.2.0", asset, dest, sumHex); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("200->206 resume mismatch: len=%d want=%d err=%v", len(got), len(payload), err)
	}
	if rangeCalls.Load() < 2 {
		t.Fatalf("expected 206 resume fallback, calls=%d", rangeCalls.Load())
	}
}

func TestClientRoutesThroughConfiguredProxyURL(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "all_proxy", "no_proxy"} {
		t.Setenv(key, "")
	}
	client := NewClient("0.1.0")
	client.ProxyURL = "http://127.0.0.1:7890"
	tr, ok := client.http().Transport.(*http.Transport)
	if !ok || tr.Proxy == nil {
		t.Fatalf("expected transport with proxy function, got %T", client.http().Transport)
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/xxww0098/tulai/releases/latest", nil)
	got, err := tr.Proxy(req)
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy url = %v err = %v", got, err)
	}

	client.ProxyURL = "socks5://127.0.0.1:1080"
	tr, _ = client.http().Transport.(*http.Transport)
	got, err = tr.Proxy(req)
	if err != nil || got == nil || got.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("socks5 proxy url = %v err = %v", got, err)
	}

	client.ProxyURL = ""
	tr, _ = client.http().Transport.(*http.Transport)
	got, err = tr.Proxy(req)
	if err != nil || got != nil {
		t.Fatalf("expected direct when proxy cleared, got %v err = %v", got, err)
	}
}

func TestDiscoverLatestTagRacesMirrors(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream error", http.StatusBadGateway)
	}))
	defer bad.Close()

	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.2.4"}`))
	}))
	defer good.Close()

	client := NewClient("0.1.0")
	client.Mirrors = []string{good.URL + "/"}
	client.PreferMirror = true
	tag, err := client.discoverLatestTag(context.Background(), bad.URL+"/xxww0098/tulai/releases/latest")
	if err != nil || tag != "v0.2.4" {
		t.Fatalf("discoverLatestTag = %q err = %v", tag, err)
	}
}
