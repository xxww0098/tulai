// Package fingerprinttest starts isolated, local Fingerprint browser fixtures using the installed runtime.
package fingerprinttest

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/appdata"
	"github.com/xxww0098/tulai/internal/fingerprint"
)

var next atomic.Int64

const StartTimeout = 3 * time.Minute

func WithoutProxy(t testing.TB) {
	t.Helper()
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		t.Setenv(key, "")
	}
}

func ConfigDir(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("短测不启动 Fingerprint")
	}
	if dir := os.Getenv("TULAI_FINGERPRINT_TEST_DATA"); dir != "" {
		if _, ok := fingerprint.InstalledFingerprint(dir); !ok {
			t.Fatal("TULAI_FINGERPRINT_TEST_DATA 指向的 Fingerprint 运行环境不完整")
		}
		return dir
	}
	dir, err := appdata.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fingerprint.InstalledFingerprint(dir); !ok {
		t.Skip("请先安装 Fingerprint 浏览器运行环境")
	}
	return dir
}

func Start(t testing.TB, rawURL string) (*fingerprint.Process, string) {
	t.Helper()
	dir := ConfigDir(t)
	ctx, cancel := context.WithTimeout(t.Context(), StartTimeout)
	defer cancel()
	p, err := fingerprint.Start(ctx, dir, fingerprint.Options{Profile: t.TempDir(), Headless: true, DirectNetwork: true}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	port := int(40000 + next.Add(1))
	fingerprint.Register(port, p)
	t.Cleanup(func() { p.Close(); fingerprint.Unregister(port, p) })
	if rawURL != "" && rawURL != "about:blank" {
		if err := p.Call(ctx, p.Page, "Page.navigate", map[string]any{"url": rawURL}, nil); err != nil {
			t.Fatal(err)
		}
	}
	return p, fingerprint.Endpoint(port)
}

// StartWithProxy is used only by HTTP fixtures that map a public-looking origin to localhost.
func StartWithProxy(t testing.TB, proxy string) (context.Context, context.CancelFunc, func()) {
	t.Helper()
	dir := ConfigDir(t)
	ctx, cancel := context.WithTimeout(t.Context(), StartTimeout)
	defer cancel()
	p, err := fingerprint.Start(ctx, dir, fingerprint.Options{Profile: t.TempDir(), Headless: true, Proxy: proxy, IgnoreHTTPSErrors: true}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	tab, closeTab := context.WithCancel(fingerprint.PageContext(p.Context(), p, p.Page))
	return tab, closeTab, p.Close
}
