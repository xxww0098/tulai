package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// This test links a disposable test app with a temporary key, then lets the
// real MyGo updater install into its isolated directory.
func TestSignedMyGoInstall(t *testing.T) {
	if releaseJSON := os.Getenv("TULAI_TEST_SIGNED_RELEASE"); releaseJSON != "" {
		var rel Release
		if err := json.Unmarshal([]byte(releaseJSON), &rel); err != nil {
			t.Fatal(err)
		}
		mygo.App.SetVersion("1.0.0")
		up, err := NewClient("1.0.0").CheckSigned(context.Background(), rel)
		if err != nil {
			t.Fatal(err)
		}
		err = up.Install(context.Background(), nil)
		if os.Getenv("TULAI_TEST_BAD_SIGNATURE") == "1" {
			if err == nil || !strings.Contains(err.Error(), "not signed") {
				t.Fatalf("bad signature returned %v", err)
			}
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("unsupported MyGo update platform")
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	name := "proof.txt"
	if runtime.GOOS == "darwin" {
		name = "tulai.app/proof.txt"
	}
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive.Bytes())
	signature := ed25519.Sign(private, sum[:])
	badSignature := append([]byte(nil), signature...)
	badSignature[0] ^= 1
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/archive" {
			_, _ = w.Write(archive.Bytes())
			return
		}
		sig := signature
		if r.URL.Query().Get("bad") == "1" {
			sig = badSignature
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": "1.1.0", "url": server.URL + "/archive", "size": archive.Len(),
			"signature": base64.StdEncoding.EncodeToString(sig),
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	seed := filepath.Join(dir, "helper")
	if runtime.GOOS == "windows" {
		seed += ".exe"
	}
	flags := "-X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageUpdateFeed=https://example.com/update -X github.com/egoist/mygo.packageUpdateKey=" + base64.StdEncoding.EncodeToString(pub)
	cmd := exec.Command("go", "test", "-c", "-ldflags", flags, "-o", seed, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}
	binary, err := os.ReadFile(seed)
	if err != nil {
		t.Fatal(err)
	}
	target := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "darwin" {
		target = "darwin-universal"
	}
	manifest := "update-" + target + ".json"
	for _, bad := range []bool{true, false} {
		folder := filepath.Join(dir, fmt.Sprintf("case-%t", bad))
		app := filepath.Join(folder, "tulai.app")
		binDir := app
		if runtime.GOOS == "darwin" {
			binDir = filepath.Join(app, "Contents", "MacOS")
		}
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			t.Fatal(err)
		}
		bin := filepath.Join(binDir, filepath.Base(seed))
		if err := os.WriteFile(bin, binary, 0o755); err != nil {
			t.Fatal(err)
		}
		rel := Release{Tag: "v1.1.0", Assets: []Asset{
			{Name: manifest, BrowserDownloadURL: fmt.Sprintf("%s/%s?bad=%d", server.URL, manifest, boolIntForTest(bad))},
			{Name: "archive.tar.gz", BrowserDownloadURL: server.URL + "/archive", Size: int64(archive.Len())},
		}}
		encoded, err := json.Marshal(rel)
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, "-test.run=^TestSignedMyGoInstall$")
		cmd.Env = append(os.Environ(), "TULAI_TEST_SIGNED_RELEASE="+string(encoded), fmt.Sprintf("TULAI_TEST_BAD_SIGNATURE=%d", boolIntForTest(bad)), "HOME="+folder, "XDG_DATA_HOME="+filepath.Join(folder, "data"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("bad=%v: %v\n%s", bad, err, out)
		}
		proof := filepath.Join(app, "proof.txt")
		if !bad && runtime.GOOS == "darwin" {
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if _, err := os.Stat(proof); err == nil {
					break
				}
				time.Sleep(25 * time.Millisecond)
			}
		}
		data, err := os.ReadFile(proof)
		if bad && !os.IsNotExist(err) || !bad && (err != nil || string(data) != "ok") {
			t.Fatalf("bad=%v proof=%q err=%v", bad, data, err)
		}
	}
}

func boolIntForTest(b bool) int {
	if b {
		return 1
	}
	return 0
}
