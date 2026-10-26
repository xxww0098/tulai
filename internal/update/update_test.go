package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.0", "0.1.0", 0},
		{"v0.1.0", "0.1.0", 0},
		{"0.1.0", "0.2.0", -1},
		{"0.10.0", "0.9.0", 1},
		{"0.1.1", "0.1.0", 1},
		{"0.2.0-rc.1", "0.2.0", -1},
		{"0.2.0", "0.2.0-rc.1", 1},
		{"0.2.0-rc.1", "0.2.0-rc.2", -1},
	}
	for _, tc := range cases {
		got, err := Compare(tc.a, tc.b)
		if err != nil {
			t.Fatalf("Compare(%s, %s): %v", tc.a, tc.b, err)
		}
		if got != tc.want {
			t.Fatalf("Compare(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
	if _, err := Compare("1.2", "1.2.3"); err == nil {
		t.Fatal("accepted a version that is not x.y.z")
	}
	newer, err := Newer("0.1.0", "v0.1.0")
	if err != nil || newer {
		t.Fatalf("same release looked newer: %v %v", newer, err)
	}
	newer, err = Newer("0.1.0", "0.1.1")
	if err != nil || !newer {
		t.Fatalf("patch release was not newer: %v %v", newer, err)
	}
}

func TestSelectIgnoresExtensionAndAcceptsGitHubDots(t *testing.T) {
	const version = "0.1.0"
	assets := []Asset{
		{Name: "xbs-extension-0.3.4.zip", State: "uploaded"},
		{Name: "SHA256SUMS", State: "uploaded"},
		{Name: "install.sh", State: "uploaded"},
		{Name: "xbs.0.1.0.dmg", State: "uploaded"},
		{Name: "xbs.Setup.0.1.0.amd64.exe", State: "uploaded"},
		{Name: "xbs.Setup.0.1.0.arm64.exe", State: "uploaded"},
		{Name: "xbs-0.1.0-windows-amd64.exe", State: "uploaded"},
		{Name: "xbs-0.1.0-windows-arm64.exe", State: "uploaded"},
		{Name: "xbs-0.1.0-linux-amd64.tar.gz", State: "uploaded"},
		{Name: "xbs-0.1.0-linux-arm64.tar.gz", State: "uploaded"},
		{Name: "xbs_0.1.0_amd64.deb", State: "uploaded"},
		{Name: "xbs_0.1.0_arm64.deb", State: "uploaded"},
	}
	// Spaced build names normalize to the same GitHub asset names.
	spaced := []Asset{
		{Name: "xbs 0.1.0.dmg", State: "uploaded"},
		{Name: "xbs Setup 0.1.0 amd64.exe", State: "uploaded"},
	}
	checks := []struct {
		kind Kind
		arch string
		name string
	}{
		{KindMacDMG, "arm64", "xbs.0.1.0.dmg"},
		{KindMacDMG, "amd64", "xbs.0.1.0.dmg"},
		{KindWindowsSetup, "amd64", "xbs.Setup.0.1.0.amd64.exe"},
		{KindWindowsExe, "arm64", "xbs-0.1.0-windows-arm64.exe"},
		{KindLinuxTar, "amd64", "xbs-0.1.0-linux-amd64.tar.gz"},
		{KindLinuxDeb, "arm64", "xbs_0.1.0_arm64.deb"},
	}
	for _, tc := range checks {
		asset, err := Select(assets, tc.kind, version, tc.arch)
		if err != nil {
			t.Fatalf("Select %s %s: %v", tc.kind, tc.arch, err)
		}
		if asset.Name != tc.name {
			t.Fatalf("Select %s %s = %s, want %s", tc.kind, tc.arch, asset.Name, tc.name)
		}
		if strings.Contains(asset.Name, "extension") {
			t.Fatal("selected the page-bridge extension")
		}
	}
	dmg, err := Select(spaced, KindMacDMG, version, "amd64")
	if err != nil || NormalizeAssetName(dmg.Name) != "xbs.0.1.0.dmg" {
		t.Fatalf("spaced dmg: %+v %v", dmg, err)
	}
	setup, err := Select(spaced, KindWindowsSetup, version, "amd64")
	if err != nil || NormalizeAssetName(setup.Name) != "xbs.Setup.0.1.0.amd64.exe" {
		t.Fatalf("spaced setup: %+v %v", setup, err)
	}
	if _, err := Select(assets, KindLinuxTar, version, "386"); err == nil {
		t.Fatal("selected an asset for an unpublished architecture")
	}
}

func TestDetectMode(t *testing.T) {
	cases := []struct {
		goos, exe string
		want      Mode
	}{
		{"darwin", "/Applications/xbs.app/Contents/MacOS/xbs", ModeMacBundle},
		{"darwin", "/tmp/go-build/xbs", ModeMacCopy},
		{"windows", `C:\Users\a\AppData\Local\Programs\xbs\xbs.exe`, ModeWindowsSetup},
		{"windows", `C:\Program Files\xbs\xbs.exe`, ModeWindowsSetup},
		{"windows", `C:\Users\a\AppData\Local\Temp\go-build\xbs.exe`, ModeWindowsSetup},
		{"windows", `D:\Portable\xbs.exe`, ModeWindowsExe},
		{"linux", "/usr/bin/xbs", ModeLinuxDeb},
		{"linux", "/opt/xbs/xbs", ModeLinuxDeb},
		{"linux", "/home/a/xbs-0.1.2-linux-amd64.AppImage", ModeLinuxBinary},
		{"linux", "/home/a/.local/xbs.app/xbs", ModeLinuxUser},
		{"linux", "/tmp/go-build/xbs", ModeLinuxUser},
	}
	for _, tc := range cases {
		if got := DetectMode(tc.goos, tc.exe); got != tc.want {
			t.Fatalf("DetectMode(%s, %s) = %s, want %s", tc.goos, tc.exe, got, tc.want)
		}
	}
}

func TestParseChecksumsWithSpaces(t *testing.T) {
	text := "" +
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  xbs 0.1.0.dmg\n" +
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb *xbs-0.1.0-linux-amd64.tar.gz\n" +
		"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc  xbs-extension-0.3.4.zip\n"
	sums, err := ParseChecksums(text)
	if err != nil {
		t.Fatal(err)
	}
	if sums["xbs.0.1.0.dmg"] != strings.Repeat("a", 64) {
		t.Fatalf("dmg sum: %v", sums)
	}
	if sums["xbs-0.1.0-linux-amd64.tar.gz"] != strings.Repeat("b", 64) {
		t.Fatalf("tar sum: %v", sums)
	}
	if sums["xbs-extension-0.3.4.zip"] == "" {
		t.Fatal("extension checksum was dropped")
	}
}

func TestPrepareDownloadsVerifiedAsset(t *testing.T) {
	payload := []byte("xbs-update-test")
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])
	sumsText := sumHex + "  xbs-0.2.0-linux-amd64.tar.gz\n"
	sumsSum := sha256.Sum256([]byte(sumsText))
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			sawAuth = true
		}
		if r.Header.Get("User-Agent") == "" {
			t.Errorf("missing user agent on %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/repos/xxww0098/xbs/releases/latest":
			rel := Release{
				Tag:  "v0.2.0",
				Name: "xbs 0.2.0",
				Body: "notes",
				Assets: []Asset{
					{Name: "xbs-extension-0.9.9.zip", State: "uploaded", BrowserDownloadURL: serverURL(r, "/nope"), Size: 1},
					{Name: "SHA256SUMS", State: "uploaded", BrowserDownloadURL: serverURL(r, "/sums"), Size: int64(len(sumsText)), Digest: "sha256:" + hex.EncodeToString(sumsSum[:])},
					{Name: "xbs-0.2.0-linux-amd64.tar.gz", State: "uploaded", BrowserDownloadURL: serverURL(r, "/asset"), Size: int64(len(payload)), Digest: "sha256:" + sumHex},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(rel)
		case "/sums":
			w.Write([]byte(sumsText))
		case "/asset":
			w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	client := NewClient("0.1.0")
	client.API = srv.URL
	client.DownloadDir = dir
	got, err := client.Prepare(context.Background(), "0.1.0", ModeLinuxUser, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if got.UpToDate || got.Asset.Name != "xbs-0.2.0-linux-amd64.tar.gz" {
		t.Fatalf("prepared %+v", got)
	}
	body, err := os.ReadFile(got.Path)
	if err != nil || !bytes.Equal(body, payload) {
		t.Fatalf("file %q err %v", body, err)
	}
	if sawAuth {
		t.Fatal("sent an Authorization header")
	}

	same, err := client.Prepare(context.Background(), "0.2.0", ModeLinuxUser, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !same.UpToDate {
		t.Fatal("equal version was offered as an update")
	}
}

func TestPrepareRejectsBadChecksumAndForeignURL(t *testing.T) {
	payload := []byte("nope")
	sum := sha256.Sum256(payload)
	sumHex := hex.EncodeToString(sum[:])
	sumsText := strings.Repeat("ab", 32) + "  xbs-0.2.0-linux-amd64.tar.gz\n"
	sumsSum := sha256.Sum256([]byte(sumsText))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	client := NewClient("0.1.0")
	client.API = srv.URL
	client.DownloadDir = t.TempDir()
	if _, err := client.Prepare(context.Background(), "0.1.0", ModeLinuxUser, "amd64"); err == nil {
		t.Fatal("accepted a file whose digest disagreed with SHA256SUMS")
	}

	if err := client.allowAssetURL("https://evil.example/xbs.tar.gz", "v0.2.0", "xbs.tar.gz"); err == nil {
		t.Fatal("accepted a download host outside GitHub")
	}
	if err := client.allowAssetURL("https://github.com/other/xbs/releases/download/v0.2.0/xbs.tar.gz", "v0.2.0", "xbs.tar.gz"); err == nil {
		t.Fatal("accepted another repository")
	}
	if err := client.allowAssetURL("http://github.com/xxww0098/xbs/releases/download/v0.2.0/xbs.tar.gz", "v0.2.0", "xbs.tar.gz"); err == nil {
		t.Fatal("accepted plain HTTP")
	}
}

func TestRedirectStaysOnAllowedHosts(t *testing.T) {
	client := NewClient("0.1.0")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/file", http.StatusFound)
	}))
	defer srv.Close()
	asset := Asset{Name: "SHA256SUMS", BrowserDownloadURL: srv.URL + "/sums", Size: 1}
	if err := client.downloadFile(context.Background(), "v0.1.0", asset, filepath.Join(t.TempDir(), "SHA256SUMS")); err == nil {
		t.Fatal("followed a redirect off GitHub")
	}
}

func TestInstallLinuxUserArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "app.tar.gz")
	writeTar(t, archive, map[string]string{
		"xbs":         "#!/bin/sh\necho updated\n",
		"xbs.desktop": "[Desktop Entry]\nType=Application\nName=xbs\nExec=xbs\nIcon=\n",
	})
	home := filepath.Join(dir, "home")
	exe := filepath.Join(home, ".local", "xbs.app", "xbs")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := Install(Options{
		Mode:       ModeLinuxUser,
		Executable: exe,
		AssetPath:  archive,
		Version:    "0.2.0",
		Home:       home,
		DataHome:   filepath.Join(home, ".local", "share"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Quit || out.Helper != "/bin/sh" || out.Relaunch {
		t.Fatalf("in-place user install did not arrange a restart: %+v", out)
	}
	body, err := os.ReadFile(filepath.Join(home, ".local", "xbs.app", "xbs"))
	if err != nil || !strings.Contains(string(body), "updated") {
		t.Fatalf("installed binary %q err %v", body, err)
	}
	link, err := os.Readlink(filepath.Join(home, ".local", "bin", "xbs"))
	if err != nil || link != filepath.Join(home, ".local", "xbs.app", "xbs") {
		t.Fatalf("symlink %q err %v", link, err)
	}
	desktop, err := os.ReadFile(filepath.Join(home, ".local", "share", "applications", "xbs.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	wantExec := `Exec="` + filepath.Join(home, ".local", "xbs.app", "xbs") + `"`
	if !strings.Contains(string(desktop), wantExec) || !strings.Contains(string(desktop), "Name=tulai") || strings.Contains(string(desktop), "Name=xbs") {
		t.Fatalf("desktop entry:\n%s", desktop)
	}
	info, err := os.Stat(filepath.Join(home, ".local", "xbs.app", "xbs"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("binary is not executable: %v %v", info, err)
	}
}

func TestInstallRejectsTarPathEscape(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "bad.tar.gz")
	writeTar(t, archive, map[string]string{"../xbs": "nope"})
	if _, err := Install(Options{
		Mode:      ModeLinuxUser,
		AssetPath: archive,
		Version:   "0.2.0",
		Home:      filepath.Join(dir, "home"),
	}); err == nil {
		t.Fatal("installed a tar that escapes the app directory")
	}
	if _, err := os.Stat(filepath.Join(dir, "home", ".local", "xbs")); err == nil {
		t.Fatal("escaped file was written")
	}
}

func TestInstallDebIsManual(t *testing.T) {
	dir := t.TempDir()
	deb := filepath.Join(dir, "xbs_0.2.0_amd64.deb")
	if err := os.WriteFile(deb, []byte("deb"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	out, err := Install(Options{Mode: ModeLinuxDeb, AssetPath: deb, Version: "0.2.0", Executable: "/usr/bin/xbs"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Relaunch || out.Quit || !strings.Contains(out.ManualCommand, "sudo dpkg -i ") || out.OpenPath == "" {
		t.Fatalf("deb outcome: %+v", out)
	}
	if _, err := os.Stat(out.InstalledPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "home", ".local", "xbs.app")); err == nil {
		t.Fatal("deb install wrote a user app")
	}
}

func TestWindowsReplaceScript(t *testing.T) {
	script := WindowsScript()
	for _, needle := range []string{"Get-Process", "Move-Item -LiteralPath $Source", "Start-Process -FilePath $Destination", "Start-Process -FilePath $Installer", ".previous", "-ArgumentList '/S'"} {
		if !strings.Contains(script, needle) {
			t.Fatalf("script missing %s:\n%s", needle, script)
		}
	}
	args := WindowsHelperArgs("replace.ps1", 42, `D:\Portable\new.exe`, `D:\Portable\xbs.exe`, "")
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "42") || !strings.Contains(joined, `D:\Portable\xbs.exe`) {
		t.Fatalf("args: %#v", args)
	}
	out, err := Install(Options{
		Mode:       ModeWindowsExe,
		Executable: `D:\Portable\xbs.exe`,
		AssetPath:  filepath.Join(t.TempDir(), "missing-not-read"),
		Version:    "0.2.0",
		PID:        42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Quit || out.Helper != "powershell.exe" || !strings.Contains(strings.Join(out.HelperArgs, " "), "42") {
		t.Fatalf("windows outcome: %+v", out)
	}
	// The portable file is not opened until PowerShell runs.
	if runtime.GOOS == "windows" {
		t.Log("script prepared for the Windows replacer")
	}
}

func TestMacDestination(t *testing.T) {
	bundle, inPlace := MacDestination("/Applications/xbs.app/Contents/MacOS/xbs", "/Users/a")
	if bundle != "/Applications/xbs.app" || !inPlace {
		t.Fatalf("bundle dest %s inPlace %v", bundle, inPlace)
	}
	copy, inPlace := MacDestination("/tmp/xbs", "/Users/a")
	if copy != "/Users/a/Applications/xbs.app" || inPlace {
		t.Fatalf("copy dest %s inPlace %v", copy, inPlace)
	}
	root := t.TempDir()
	app := filepath.Join(root, "mounted", "xbs.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	found, err := FindBundle(root, "xbs.app")
	if err != nil || found != app {
		t.Fatalf("found %s err %v", found, err)
	}
}

func TestLiveLatestRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("network")
	}
	client := NewClient("0.1.0")
	client.DownloadDir = t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rel, err := client.Latest(ctx)
	if err != nil {
		t.Fatal(err)
	}
	version, err := rel.Version()
	if err != nil || version != "0.1.0" {
		t.Fatalf("latest = %q (%v), release %+v", version, err, rel.Tag)
	}
	for _, kind := range []Kind{KindLinuxTar, KindLinuxDeb, KindWindowsExe, KindWindowsSetup, KindMacDMG} {
		arch := "amd64"
		if _, err := Select(rel.Assets, kind, version, arch); err != nil {
			t.Fatalf("live asset %s: %v", kind, err)
		}
	}
	var ext Asset
	for _, asset := range rel.Assets {
		if strings.HasPrefix(asset.Name, "xbs-extension-") {
			ext = asset
		}
	}
	if ext.Name != "xbs-extension-0.3.4.zip" {
		t.Fatalf("extension asset = %q", ext.Name)
	}
	sums, err := client.checksums(ctx, rel)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"xbs.0.1.0.dmg", "xbs.Setup.0.1.0.amd64.exe", "xbs-0.1.0-linux-amd64.tar.gz", "xbs-extension-0.3.4.zip"} {
		if len(sums[name]) != 64 {
			t.Fatalf("SHA256SUMS missing %s: %v", name, sums)
		}
	}
}

func serverURL(r *http.Request, p string) string {
	return "http://" + r.Host + p
}

func writeTar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
