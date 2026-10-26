package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMirrorFallbackAndChecksum(t *testing.T) {
	payload := []byte("mirror-bytes")
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])
	bad := []byte("captive-portal")
	sumsText := sumHex + "  xbs-0.2.0-linux-amd64.tar.gz\n"
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
		case "/repos/xxww0098/xbs/releases/latest":
			rel := Release{
				Tag: "v0.2.0",
				Assets: []Asset{
					{Name: "SHA256SUMS", State: "uploaded", BrowserDownloadURL: serverURL(r, "/sums"), Size: int64(len(sumsText)), Digest: "sha256:" + hex.EncodeToString(sumsSum[:])},
					{Name: "xbs-0.2.0-linux-amd64.tar.gz", State: "uploaded", BrowserDownloadURL: serverURL(r, "/asset"), Size: int64(len(payload)), Digest: "sha256:" + sumHex},
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
	sumsText := sumHex + "  xbs-0.2.0-linux-amd64.tar.gz\n"
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
		case "/repos/xxww0098/xbs/releases/latest":
			json.NewEncoder(w).Encode(Release{
				Tag: "v0.2.0",
				Assets: []Asset{
					{Name: "SHA256SUMS", State: "uploaded", BrowserDownloadURL: serverURL(r, "/sums"), Size: int64(len(sumsText)), Digest: "sha256:" + hex.EncodeToString(sumsSum[:])},
					{Name: "xbs-0.2.0-linux-amd64.tar.gz", State: "uploaded", BrowserDownloadURL: serverURL(r, "/asset"), Size: int64(len(good)), Digest: "sha256:" + sumHex},
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
	asset := Asset{Name: "xbs.bin", BrowserDownloadURL: srv.URL + "/xbs.bin", Size: int64(len(payload))}
	dest := filepath.Join(t.TempDir(), "xbs.bin")
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
	dest := filepath.Join(dir, "xbs.bin")
	if err := os.WriteFile(dest+".partial", []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	asset := Asset{Name: "xbs.bin", BrowserDownloadURL: srv.URL + "/xbs.bin", Size: 8}
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
	assets := []Asset{{Name: "xbs-0.2.0-linux-arm64.tar.gz", State: "uploaded"}}
	got, err := Select(assets, KindLinuxAppImage, "0.2.0", "arm64")
	if err != nil || got.Name != "xbs-0.2.0-linux-arm64.tar.gz" {
		t.Fatalf("fallback %+v %v", got, err)
	}
	with := append(assets, Asset{Name: "xbs-0.2.0-linux-arm64.AppImage", State: "uploaded"})
	got, err = Select(with, KindLinuxAppImage, "0.2.0", "arm64")
	if err != nil || got.Name != "xbs-0.2.0-linux-arm64.AppImage" {
		t.Fatalf("appimage %+v %v", got, err)
	}
}
