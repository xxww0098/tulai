package extbundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestInstallIsIdempotentAndCleansOldVersions(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "extension", "0.0.1")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	ver := Version()
	if ver == "" {
		t.Fatal("empty version")
	}
	want := filepath.Join(root, "extension", "current")
	if first != want {
		t.Fatalf("dir = %s", first)
	}
	if _, err := os.Stat(filepath.Join(root, "extension", ver, "background.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(first, "doubao", "raw.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(first, "network.js")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old version still present: %v", err)
	}
	info, err := os.Stat(filepath.Join(first, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Install(root)
	if err != nil || second != first {
		t.Fatalf("second = %s %v", second, err)
	}
	again, err := os.Stat(filepath.Join(first, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(info.ModTime()) {
		t.Fatal("reinstall rewrote the stable directory")
	}
}

var geckoUUIDPattern = regexp.MustCompile(`^\{[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\}$`)

func readManifestMap(t *testing.T, path string) (map[string]any, []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out, raw
}

func manifestGeckoID(m map[string]any) string {
	bss, _ := m["browser_specific_settings"].(map[string]any)
	gecko, _ := bss["gecko"].(map[string]any)
	id, _ := gecko["id"].(string)
	return id
}

func TestInstallDownloadAddonKeepsConnectionDetailsPrivate(t *testing.T) {
	dir, err := InstallDownloadAddon(t.TempDir(), "http://127.0.0.1:12345/download", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "background.js", "temu.js", "content.js"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(TemuSearchJS(), "function tulaiTemuSnapshot") {
		t.Fatal("TEMU 搜索脚本未打包")
	}
	path := filepath.Join(dir, "settings.js")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("下载助手配置权限错误: %v, %v", info, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "test-secret") || !strings.Contains(string(data), "127.0.0.1:12345") {
		t.Fatalf("下载助手连接信息错误: %v", err)
	}

	dir1, m1, err := InstallDownloadAddonWithMarkers(t.TempDir(), "http://127.0.0.1:12345/download", "s1")
	if err != nil {
		t.Fatal(err)
	}
	dir2, m2, err := InstallDownloadAddonWithMarkers(t.TempDir(), "http://127.0.0.1:12345/download", "s2")
	if err != nil {
		t.Fatal(err)
	}
	for _, attr := range []string{m1.HostAttr, m1.RequestAttr, m1.SnapshotAttr} {
		if !strings.HasPrefix(attr, "data-") || len(attr) < 13 || strings.Contains(strings.ToLower(attr), "tulai") {
			t.Fatalf("invalid randomized attr %q", attr)
		}
	}
	if m1.HostAttr == m2.HostAttr || m1.RequestAttr == m2.RequestAttr || m1.SnapshotAttr == m2.SnapshotAttr {
		t.Fatalf("markers were not randomized across installs: %+v vs %+v", m1, m2)
	}
	manifest1, rawManifest1 := readManifestMap(t, filepath.Join(dir1, "manifest.json"))
	manifest2, _ := readManifestMap(t, filepath.Join(dir2, "manifest.json"))
	if manifest1["name"] != "Media Helper" {
		t.Fatalf("manifest name = %#v", manifest1["name"])
	}
	if strings.Contains(strings.ToLower(string(rawManifest1)), "tulai") {
		t.Fatalf("download manifest leaked tulai: %s", rawManifest1)
	}
	id1, id2 := manifestGeckoID(manifest1), manifestGeckoID(manifest2)
	if !geckoUUIDPattern.MatchString(id1) || !geckoUUIDPattern.MatchString(id2) || id1 == id2 {
		t.Fatalf("gecko IDs invalid or identical: %q vs %q", id1, id2)
	}
	bg, _ := manifest1["background"].(map[string]any)
	bgScripts, _ := bg["scripts"].([]any)
	if len(bgScripts) != 2 || bgScripts[0] != "settings.js" || bgScripts[1] != "background.js" {
		t.Fatalf("unexpected background scripts: %#v", bgScripts)
	}
	cs, _ := manifest1["content_scripts"].([]any)
	if len(cs) == 0 || strings.Contains(strings.ToLower(string(rawManifest1)), "settings.js\" in content") {
		t.Fatalf("invalid content_scripts: %#v", cs)
	}
	csBytes, _ := json.Marshal(cs)
	if strings.Contains(string(csBytes), "settings.js") {
		t.Fatalf("settings.js must only appear in background.scripts: %s", csBytes)
	}
	content1, err := os.ReadFile(filepath.Join(dir1, "content.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{m1.HostAttr, m1.RequestAttr, m1.SnapshotAttr} {
		if !strings.Contains(string(content1), needle) {
			t.Fatalf("installed download/content.js missing marker %q", needle)
		}
	}
	scrubbedContent := strings.ReplaceAll(string(content1), "tulaiTemuSnapshot()", "")
	for _, forbidden := range []string{"tulai", "data-pb-dl-host", "data-pb-dl-req", "data-pb-dl-snap"} {
		if strings.Contains(strings.ToLower(scrubbedContent), forbidden) {
			t.Fatalf("installed download/content.js leaked %q", forbidden)
		}
	}
}

func TestInstallFirefoxBridgeUsesBoundIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bridge")
	got, err := InstallFirefoxBridge(dir, "18965", "private-token", "tulai-bound-42")
	if err != nil || got != dir {
		t.Fatalf("install=%s err=%v", got, err)
	}
	manifestMap, manifest := readManifestMap(t, filepath.Join(dir, "manifest.json"))
	if !strings.Contains(string(manifest), `"webRequestBlocking"`) {
		t.Fatalf("Firefox manifest missing webRequestBlocking: %s", manifest)
	}
	if manifestMap["name"] != "Page Bridge" {
		t.Fatalf("manifest name = %#v", manifestMap["name"])
	}
	ba, _ := manifestMap["browser_action"].(map[string]any)
	if ba["default_title"] != "Page Bridge" {
		t.Fatalf("default_title = %#v", ba["default_title"])
	}
	if strings.Contains(strings.ToLower(string(manifest)), "tulai") || strings.Contains(string(manifest), "doubao/raw.js") {
		t.Fatalf("Firefox manifest should not mention tulai or doubao/raw.js: %s", manifest)
	}
	id42 := manifestGeckoID(manifestMap)
	if !geckoUUIDPattern.MatchString(id42) {
		t.Fatalf("invalid gecko id %q", id42)
	}
	dirSame := filepath.Join(t.TempDir(), "bridge-same")
	if _, err := InstallFirefoxBridge(dirSame, "18965", "private-token", " tulai-bound-42 "); err != nil {
		t.Fatal(err)
	}
	mSame, _ := readManifestMap(t, filepath.Join(dirSame, "manifest.json"))
	if manifestGeckoID(mSame) != id42 {
		t.Fatalf("deterministic gecko id mismatch: %q != %q", manifestGeckoID(mSame), id42)
	}
	dirOther := filepath.Join(t.TempDir(), "bridge-other")
	if _, err := InstallFirefoxBridge(dirOther, "18965", "private-token", "tulai-bound-43"); err != nil {
		t.Fatal(err)
	}
	mOther, _ := readManifestMap(t, filepath.Join(dirOther, "manifest.json"))
	if manifestGeckoID(mOther) == id42 || !geckoUUIDPattern.MatchString(manifestGeckoID(mOther)) {
		t.Fatalf("distinct account gecko id expected, got %q vs %q", manifestGeckoID(mOther), id42)
	}
	dirAnon1 := filepath.Join(t.TempDir(), "bridge-anon1")
	dirAnon2 := filepath.Join(t.TempDir(), "bridge-anon2")
	if _, err := InstallFirefoxBridge(dirAnon1, "18965", "t1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := InstallFirefoxBridge(dirAnon2, "18965", "t2", ""); err != nil {
		t.Fatal(err)
	}
	mAnon1, _ := readManifestMap(t, filepath.Join(dirAnon1, "manifest.json"))
	mAnon2, _ := readManifestMap(t, filepath.Join(dirAnon2, "manifest.json"))
	if !geckoUUIDPattern.MatchString(manifestGeckoID(mAnon1)) || !geckoUUIDPattern.MatchString(manifestGeckoID(mAnon2)) || manifestGeckoID(mAnon1) == manifestGeckoID(mAnon2) {
		t.Fatalf("empty account should yield unique random UUIDs: %q vs %q", manifestGeckoID(mAnon1), manifestGeckoID(mAnon2))
	}

	chooser, err := os.ReadFile(filepath.Join(dir, "qianwen", "chooser.js"))
	if err != nil {
		t.Fatal(err)
	}
	qPage, err := os.ReadFile(filepath.Join(dir, "qianwen", "page.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(chooser)), "tulai") {
		t.Fatalf("installed qianwen/chooser.js leaked tulai")
	}
	for _, placeholder := range []string{"__PB_PROBE__", "__PB_ARM__", "__PB_DISARM__", "__PB_OK__", "__PB_FAIL__"} {
		if strings.Contains(string(chooser), placeholder) || strings.Contains(string(qPage), placeholder) {
			t.Fatalf("unreplaced placeholder %s", placeholder)
		}
	}
	tokPattern := regexp.MustCompile(`'(_[0-9a-f]{8,})'`)
	chooserTokens := tokPattern.FindAllStringSubmatch(string(chooser), -1)
	if len(chooserTokens) < 5 {
		t.Fatalf("expected 5 randomized event tokens in chooser.js, got %d", len(chooserTokens))
	}
	for _, m := range chooserTokens {
		if !strings.Contains(string(qPage), m[1]) {
			t.Fatalf("token %s from chooser.js missing in qianwen/page.js", m[1])
		}
	}

	config := filepath.Join(dir, "firefox", "settings.js")
	info, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("bridge configuration permissions: %v", info.Mode())
	}
	data, err := os.ReadFile(config)
	if err != nil || !strings.Contains(string(data), "private-token") || !strings.Contains(string(data), "tulai-bound-42") || !strings.Contains(string(data), "18965") {
		t.Fatalf("bridge identity configuration: %v", err)
	}
}
