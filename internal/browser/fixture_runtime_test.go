package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/xxww0098/tulai/internal/fingerprint"
)

// fakeLauncherRuntime 写出一套私有启动器运行环境：node 就是本测试二进制，
// TestMain 按 TULAI_BROWSER_PIPE_FIXTURE 扮演 bridge.mjs，不会启动真实浏览器。
func fakeLauncherRuntime(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "runtime", "fingerprint", fingerprint.FingerprintVersion+"-1.62.0-v24.22.2-"+runtime.GOOS+"-"+runtime.GOARCH)
	node := filepath.Join(dir, "node", "bin", "node")
	if runtime.GOOS == "windows" {
		node = filepath.Join(dir, "node", "node.exe")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		node:                                     data,
		filepath.Join(dir, "browser-path"):       []byte("chrome"),
		filepath.Join(dir, "browsers", "chrome"): []byte("x"),
		filepath.Join(dir, "node_modules", "playwright-core", "package.json"): []byte(`{"version":"1.62.0"}`),
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, body, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
