package extbundle

import (
	"os"
	"path/filepath"
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
}

func TestInstallFirefoxBridgeUsesBoundIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bridge")
	got, err := InstallFirefoxBridge(dir, "18965", "private-token", "tulai-bound-42")
	if err != nil || got != dir {
		t.Fatalf("install=%s err=%v", got, err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil || !strings.Contains(string(manifest), `"webRequestBlocking"`) || !strings.Contains(string(manifest), `"page-bridge@tulai.local"`) {
		t.Fatalf("Firefox manifest missing required capabilities: %v", err)
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
