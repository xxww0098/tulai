package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"xbs/internal/update"
)

func TestUpdateCheckCommand(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/xxww0098/tulai/releases/latest" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("Authorization was set")
		}
		json.NewEncoder(w).Encode(update.Release{
			Tag:     "v0.2.0",
			HTMLURL: "https://github.com/xxww0098/tulai/releases/tag/v0.2.0",
			Assets:  []update.Asset{{Name: "xbs-extension-0.3.4.zip"}},
		})
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	client := update.NewClient(ProductVersion)
	client.API = srv.URL
	code := runUpdateWith([]string{"--check"}, updateRun{Client: client, Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit %d stderr %s", code, stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, "当前版本: "+ProductVersion) || !strings.Contains(text, "最新版本: 0.2.0") || !strings.Contains(text, "有新版本") {
		t.Fatalf("stdout:\n%s", text)
	}
	if strings.Contains(stderr.String(), "安装") {
		t.Fatalf("check downloaded or installed: %s", stderr.String())
	}
}

func TestUpdateCheckUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(update.Release{Tag: "v0.1.0", HTMLURL: "https://github.com/xxww0098/tulai/releases/tag/v0.1.0"})
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	client := update.NewClient("0.1.0")
	client.API = srv.URL
	code := runUpdateWith([]string{"--check"}, updateRun{Client: client, Stdout: &stdout, Stderr: &stderr})
	if code != 0 || !strings.Contains(stdout.String(), "已是最新版本") {
		t.Fatalf("code %d stdout %s stderr %s", code, stdout.String(), stderr.String())
	}
}

func TestUpdateInstallsUserArchive(t *testing.T) {
	var tarBuf bytes.Buffer
	gz := gzip.NewWriter(&tarBuf)
	tw := tar.NewWriter(gz)
	body := "#!/bin/sh\necho from-release\n"
	if err := tw.WriteHeader(&tar.Header{Name: "xbs", Mode: 0o755, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	desktop := "[Desktop Entry]\nExec=xbs\n"
	if err := tw.WriteHeader(&tar.Header{Name: "xbs.desktop", Mode: 0o644, Size: int64(len(desktop))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(desktop)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	payload := tarBuf.Bytes()
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])
	sumsText := sumHex + "  xbs-0.2.0-linux-amd64.tar.gz\n"
	sumsSum := sha256.Sum256([]byte(sumsText))

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/xxww0098/tulai/releases/latest":
			json.NewEncoder(w).Encode(update.Release{
				Tag:     "v0.2.0",
				HTMLURL: "https://github.com/xxww0098/tulai/releases/tag/v0.2.0",
				Assets: []update.Asset{
					{Name: "xbs-extension-0.3.4.zip", State: "uploaded", BrowserDownloadURL: srv.URL + "/ext", Size: 1, Digest: "sha256:" + strings.Repeat("a", 64)},
					{Name: "SHA256SUMS", State: "uploaded", BrowserDownloadURL: srv.URL + "/sums", Size: int64(len(sumsText)), Digest: "sha256:" + hex.EncodeToString(sumsSum[:])},
					{Name: "xbs-0.2.0-linux-amd64.tar.gz", State: "uploaded", BrowserDownloadURL: srv.URL + "/asset", Size: int64(len(payload)), Digest: "sha256:" + sumHex},
				},
			})
		case "/sums":
			w.Write([]byte(sumsText))
		case "/asset":
			w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	client := update.NewClient("0.1.0")
	client.API = srv.URL
	client.DownloadDir = filepath.Join(root, "cache")
	code := runUpdateWith(nil, updateRun{
		Client:     client,
		Stdout:     &stdout,
		Stderr:     &stderr,
		Home:       filepath.Join(root, "home"),
		DataHome:   filepath.Join(root, "home", ".local", "share"),
		Executable: filepath.Join(root, "dev", "xbs"),
		GOOS:       "linux",
		GOARCH:     "amd64",
		Start: func(string, ...string) error {
			t.Fatal("user install started a helper")
			return nil
		},
	})
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}
	installed, err := os.ReadFile(filepath.Join(root, "home", ".local", "xbs.app", "xbs"))
	if err != nil || !strings.Contains(string(installed), "from-release") {
		t.Fatalf("installed %q err %v\nstdout %s", installed, err, stdout.String())
	}
	if !strings.Contains(stderr.String(), "没有 Apple 公证") {
		t.Fatalf("missing unsigned warning: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "home", ".local", "bin", "xbs")); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runUpdateWith([]string{"--bogus"}, updateRun{Stdout: &stdout, Stderr: &stderr}); code != 2 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(stderr.String(), "tulai") || !strings.Contains(stderr.String(), "xbs update") {
		t.Fatalf("stderr %s", stderr.String())
	}
}
