package browser

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/xxww0098/tulai/internal/camoufox"
	"github.com/xxww0098/tulai/internal/logging"
	"github.com/xxww0098/tulai/internal/store"
)

func TestProxyServerPrefersCustomThenEnv(t *testing.T) {
	env := func(key string) string {
		switch key {
		case "HTTPS_PROXY":
			return "http://10.0.0.8:8888"
		case "ALL_PROXY":
			return "socks5://10.0.0.9:1080"
		case "HTTP_PROXY":
			return "http://10.0.0.7:8080"
		default:
			return ""
		}
	}
	if got := ProxyServer("socks5://127.0.0.1:1080", env); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("custom = %q", got)
	}
	if got := ProxyServer("  http://127.0.0.1:7890/ ", env); got != "http://127.0.0.1:7890" {
		t.Fatalf("trimmed custom = %q", got)
	}
	if got := ProxyServer("ftp://127.0.0.1:21", env); got != "" {
		t.Fatalf("invalid custom should not fall through to env, got %q", got)
	}
	if got := ProxyServer("", env); got != "http://10.0.0.8:8888" {
		t.Fatalf("HTTPS_PROXY = %q", got)
	}
	httpsOff := func(key string) string {
		if key == "https_proxy" {
			return "socks5h://127.0.0.1:1080"
		}
		return ""
	}
	if got := ProxyServer("", httpsOff); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("lowercase socks5h = %q", got)
	}
	all := func(key string) string {
		if key == "ALL_PROXY" {
			return "127.0.0.1:7890"
		}
		return ""
	}
	if got := ProxyServer("", all); got != "http://127.0.0.1:7890" {
		t.Fatalf("host:port via ALL_PROXY = %q", got)
	}
	if got := ProxyServer("", func(string) string { return "" }); got != "" {
		t.Fatalf("no proxy = %q", got)
	}
	if _, ok := NormalizeProxy("http://127.0.0.1"); ok {
		t.Fatal("missing port should be rejected")
	}
}

func TestAccountRejectsInvalidGlobalProxy(t *testing.T) {
	previous := savedProxy()
	t.Cleanup(func() { SetProxy(previous) })
	SetProxy("ftp://127.0.0.1:21")
	m := NewManager()
	m.configDir = t.TempDir()
	b := store.Browser{ID: 1, Name: "account", UserDataDir: t.TempDir()}
	if _, err := m.Start(b, t.TempDir(), KindCamoufox, "", true); err == nil || !strings.Contains(err.Error(), "全局代理地址无效") {
		t.Fatalf("invalid global proxy should prevent startup: %v", err)
	}
	b.Proxy = "http://127.0.0.1:7890"
	if _, err := m.Start(b, t.TempDir(), KindCamoufox, "", true); err == nil || !strings.Contains(err.Error(), "Camoufox 尚未准备好") {
		t.Fatalf("valid account proxy should override invalid global proxy: %v", err)
	}
}

func TestResolveCurrentProxyRejectsInvalidSystemProxy(t *testing.T) {
	previous := savedProxy()
	t.Cleanup(func() { SetProxy(previous) })
	SetProxy("")
	for _, key := range proxyEnvKeys {
		t.Setenv(key, "")
	}
	t.Setenv("HTTPS_PROXY", "invalid")
	if _, err := ResolveCurrentProxy(); err == nil || !strings.Contains(err.Error(), "系统代理地址无效") {
		t.Fatalf("invalid system proxy should prevent direct connection: %v", err)
	}
	t.Setenv("ALL_PROXY", "socks5://127.0.0.1:1080")
	if got, err := ResolveCurrentProxy(); err != nil || got != "socks5://127.0.0.1:1080" {
		t.Fatalf("valid fallback proxy: %q %v", got, err)
	}
}

func TestNormalizeProxyKeyRedactsCredentialsAndNormalizes(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantEmpty bool
		sameAs    string
		diffFrom  string
	}{
		{name: "empty", raw: "   ", wantEmpty: true},
		{name: "invalid scheme", raw: "ftp://127.0.0.1:21", wantEmpty: true},
		{name: "missing port", raw: "http://proxy.example.com", wantEmpty: true},
		{
			name:   "scheme host case and trailing slash",
			raw:    "  HTTP://Proxy.Example.COM:8080/ ",
			sameAs: "proxy.example.com:8080",
		},
		{
			name:   "socks5h folds to socks5",
			raw:    "SOCKS5H://Proxy.Example.COM:1080",
			sameAs: "socks5://proxy.example.com:1080/",
		},
		{
			name:   "ipv6 host normalized",
			raw:    "socks5h://[2001:DB8::1]:1080",
			sameAs: "socks5://[2001:db8::1]:1080",
		},
		{
			name:     "credentials hashed and equivalent across host case",
			raw:      "http://alice-sess1:topsecret99@Proxy.Example.COM:8000/",
			sameAs:   "HTTP://alice-sess1:topsecret99@proxy.example.com:8000",
			diffFrom: "http://alice-sess2:topsecret99@proxy.example.com:8000",
		},
		{
			name:     "distinct ports stay distinct",
			raw:      "http://alice:pw@proxy.example.com:8001",
			diffFrom: "http://alice:pw@proxy.example.com:8002",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeProxyKey(tc.raw)
			if tc.wantEmpty {
				if got != "" {
					t.Fatalf("NormalizeProxyKey(%q) = %q, want empty", tc.raw, got)
				}
				return
			}
			if got == "" {
				t.Fatalf("NormalizeProxyKey(%q) unexpectedly empty", tc.raw)
			}
			for _, secret := range []string{"alice", "topsecret99", "pw"} {
				if strings.Contains(got, secret) {
					t.Fatalf("NormalizeProxyKey(%q) leaked credential %q in %q", tc.raw, secret, got)
				}
			}
			if tc.sameAs != "" && got != NormalizeProxyKey(tc.sameAs) {
				t.Fatalf("key mismatch: %q vs %q", got, NormalizeProxyKey(tc.sameAs))
			}
			if tc.diffFrom != "" && got == NormalizeProxyKey(tc.diffFrom) {
				t.Fatalf("expected distinct keys for %q and %q: %q", tc.raw, tc.diffFrom, got)
			}
		})
	}
}

func TestSamePlatformSharedExit(t *testing.T) {
	cases := []struct {
		name        string
		target      store.Browser
		browsers    []store.Browser
		globalProxy string
		want        []string
	}{
		{
			name:   "shared direct exits flagged",
			target: store.Browser{ID: 1, Platform: "豆包", Name: "豆包1"},
			browsers: []store.Browser{
				{ID: 1, Platform: "豆包", Name: "豆包1"},
				{ID: 2, Platform: "豆包", Name: "豆包2"},
			},
			want: []string{"豆包2"},
		},
		{
			name:        "global proxy fallback matches explicit identical proxy",
			target:      store.Browser{ID: 1, Platform: "千问", Name: "千问1"},
			globalProxy: "127.0.0.1:7890",
			browsers: []store.Browser{
				{ID: 1, Platform: "千问", Name: "千问1"},
				{ID: 2, Platform: "千问", Name: "千问2", Proxy: "HTTP://127.0.0.1:7890/"},
				{ID: 3, Platform: "千问", Name: "千问3"},
			},
			want: []string{"千问2", "千问3"},
		},
		{
			name:   "identical normalized proxy URL with credentials flagged",
			target: store.Browser{ID: 1, Platform: "ChatGPT", Name: "gpt1", Proxy: "socks5h://u1:pw@Proxy.Example.COM:1080/"},
			browsers: []store.Browser{
				{ID: 1, Platform: "ChatGPT", Name: "gpt1", Proxy: "socks5h://u1:pw@Proxy.Example.COM:1080/"},
				{ID: 2, Platform: "ChatGPT", Name: "gpt2", Proxy: "socks5://u1:pw@proxy.example.com:1080"},
			},
			want: []string{"gpt2"},
		},
		{
			name:   "overlapping multi-platform flagged",
			target: store.Browser{ID: 1, Platform: "豆包,千问", Name: "combo"},
			browsers: []store.Browser{
				{ID: 2, Platform: "千问", Name: "qw1"},
			},
			want: []string{"qw1"},
		},
		{
			name:   "distinct sticky-session usernames and ports not flagged",
			target: store.Browser{ID: 1, Platform: "豆包", Name: "db1", Proxy: "http://user-s1:pass@proxy.example.com:8000"},
			browsers: []store.Browser{
				{ID: 1, Platform: "豆包", Name: "db1", Proxy: "http://user-s1:pass@proxy.example.com:8000"},
				{ID: 2, Platform: "豆包", Name: "db2", Proxy: "http://user-s2:pass@proxy.example.com:8000"},
				{ID: 3, Platform: "豆包", Name: "db3", Proxy: "http://user-s1:pass@proxy.example.com:8001"},
				{ID: 4, Platform: "豆包", Name: "db4"},
			},
			want: nil,
		},
		{
			name:   "disabled accounts ignored",
			target: store.Browser{ID: 1, Platform: "豆包", Name: "db1"},
			browsers: []store.Browser{
				{ID: 2, Platform: "豆包", Name: "db-off", Disabled: true},
				{ID: 3, Platform: "豆包", Name: "db-on"},
			},
			want: []string{"db-on"},
		},
		{
			name:   "disabled target ignored",
			target: store.Browser{ID: 1, Platform: "豆包", Name: "db1", Disabled: true},
			browsers: []store.Browser{
				{ID: 2, Platform: "豆包", Name: "db2"},
			},
			want: nil,
		},
		{
			name:   "different platforms and extension accounts ignored",
			target: store.Browser{ID: 1, Platform: "豆包", Name: "db1"},
			browsers: []store.Browser{
				{ID: 2, Platform: "千问", Name: "qw1"},
				{ID: 3, Platform: "ChatGPT", Name: "gpt1"},
				{ID: 4, Platform: "Antigravity", Name: "agy1"},
				{ID: 5, Platform: "豆包", Name: "ext1", ExtAccount: "ext-profile-1"},
			},
			want: nil,
		},
		{
			name:   "invalid proxy does not match direct exit",
			target: store.Browser{ID: 1, Platform: "豆包", Name: "db1"},
			browsers: []store.Browser{
				{ID: 2, Platform: "豆包", Name: "bad-proxy", Proxy: "ftp://127.0.0.1:21"},
			},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SamePlatformSharedExit(tc.browsers, tc.target, tc.globalProxy)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("SamePlatformSharedExit() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCollisionRootsAndOptionsSerialization(t *testing.T) {
	base := t.TempDir()
	dataRoot := filepath.Join(base, "BrowserData")
	profileDir := filepath.Join(dataRoot, KindCamoufox, "account-1")
	configDir := filepath.Join(base, "cfg")
	st, err := store.Open(filepath.Join(base, "database", "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	roots := collisionRoots(dataRoot, profileDir, configDir, st)
	want := []string{
		filepath.Join(dataRoot, KindCamoufox),
		filepath.Join(base, "DownloadBrowser"),
		filepath.Join(configDir, "DownloadBrowser"),
	}
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("collisionRoots = %v, want %v", roots, want)
	}

	raw, err := json.Marshal(camoufox.Options{Profile: profileDir, CollisionRoots: roots})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	gotRoots, ok := payload["collisionRoots"].([]any)
	if !ok || len(gotRoots) != len(want) {
		t.Fatalf("collisionRoots JSON = %s", raw)
	}

	emptyRaw, err := json.Marshal(camoufox.Options{Profile: profileDir})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(emptyRaw), "collisionRoots") {
		t.Fatalf("empty CollisionRoots should be omitted: %s", emptyRaw)
	}
}

func TestManagerStartWarnsOnSharedSamePlatformExit(t *testing.T) {
	root := t.TempDir()
	setupMockCamoufoxRuntime(t, root)
	t.Setenv("TULAI_BROWSER_PIPE_FIXTURE", "none")

	st, err := store.Open(filepath.Join(root, "database", "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	const sharedProxy = "http://user-secret:pw-secret@127.0.0.1:7890"
	a1, err := st.AddBrowser("豆包", "豆包主力", filepath.Join(root, "BrowserData", "Camoufox", "a1"))
	if err != nil {
		t.Fatal(err)
	}
	a2, err := st.AddBrowser("豆包", "豆包备用", filepath.Join(root, "BrowserData", "Camoufox", "a2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetBrowserProxy(a1.ID, sharedProxy); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBrowserProxy(a2.ID, sharedProxy); err != nil {
		t.Fatal(err)
	}
	a1.Proxy = sharedProxy

	var logBuf bytes.Buffer
	prevWriter := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevWriter) })
	logStart := len(logging.Log.Entries())

	manager := NewManager()
	manager.configDir = root
	manager.SetStore(st)
	t.Cleanup(manager.StopAll)

	if _, err := manager.Start(a1, filepath.Join(root, "BrowserData"), KindCamoufox, "", true); err != nil {
		t.Fatalf("shared exit warning must not block start: %v", err)
	}
	stdLog := logBuf.String()
	if !strings.Contains(stdLog, "豆包备用") {
		t.Fatalf("expected standard log warning naming shared account, got %q", stdLog)
	}
	if strings.Contains(stdLog, "user-secret") || strings.Contains(stdLog, "pw-secret") {
		t.Fatalf("log leaked proxy credentials: %q", stdLog)
	}
	var foundWarning bool
	for _, entry := range logging.Log.Entries()[logStart:] {
		if entry.Level == logging.Warning && strings.Contains(entry.Text, "豆包备用") {
			if strings.Contains(entry.Text, "user-secret") || strings.Contains(entry.Text, "pw-secret") {
				t.Fatalf("UI log leaked proxy credentials: %q", entry.Text)
			}
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Fatal("expected logging.Log.Warning entry for shared same-platform exit")
	}
}

func setupMockCamoufoxRuntime(t *testing.T, root string) {
	t.Helper()
	dir := filepath.Join(root, "runtime", "camoufox", camoufox.Version)
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
	lock, err := os.ReadFile(filepath.Join("..", "camoufox", "driver", "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	marker, _ := json.Marshal(map[string]string{
		"executable": node,
		"version":    camoufox.Version,
		"platform":   runtime.GOOS + "/" + runtime.GOARCH,
	})
	native := map[string]string{
		"windows/amd64": "impit-win32-x64-msvc",
		"darwin/amd64":  "impit-darwin-x64",
		"darwin/arm64":  "impit-darwin-arm64",
		"linux/amd64":   "impit-linux-x64-gnu",
		"linux/arm64":   "impit-linux-arm64-gnu",
	}[runtime.GOOS+"/"+runtime.GOARCH]
	impitMeta, _ := json.Marshal(map[string]any{"optionalDependencies": map[string]string{native: "0.14.8"}})
	files := map[string][]byte{
		node: data, filepath.Join(dir, "package-lock.json"): lock,
		filepath.Join(dir, "ready.json"):                               marker,
		filepath.Join(dir, "node_modules", "camoufox", "package.json"): []byte("{}"),
		filepath.Join(dir, "node_modules", "impit", "package.json"):    impitMeta,
		filepath.Join(dir, "node_modules", native, "package.json"):     []byte(`{"version":"0.14.8"}`),
	}
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		files[filepath.Join(dir, "node_modules", native, "impit-node.win32-x64-msvc.node")] = []byte("x")
	}
	for name, body := range files {
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, body, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}
