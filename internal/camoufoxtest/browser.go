// Package camoufoxtest starts isolated, local browser fixtures using the installed runtime.
package camoufoxtest

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xxww0098/tulai/internal/camoufox"
)

var next atomic.Int64

func ConfigDir(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("短测不启动 Camoufox")
	}
	if dir := os.Getenv("TULAI_CAMOUFOX_TEST_DATA"); dir != "" {
		if _, ok := camoufox.Installed(dir); !ok {
			t.Fatal("TULAI_CAMOUFOX_TEST_DATA 指向的 Camoufox 运行环境不完整")
		}
		return dir
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	dir = filepath.Join(dir, "tulai")
	if _, ok := camoufox.Installed(dir); !ok {
		t.Skip("请先运行 go run ./cmd/tulai-camoufox-smoke 安装 Camoufox")
	}
	return dir
}

func Start(t testing.TB, rawURL string) (*camoufox.Process, string) {
	t.Helper()
	dir := ConfigDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p, err := camoufox.Start(ctx, dir, camoufox.Options{Profile: t.TempDir(), Headless: true}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	port := int(40000 + next.Add(1))
	camoufox.Register(port, p)
	t.Cleanup(func() { p.Close(); camoufox.Unregister(port, p) })
	if rawURL != "" && rawURL != "about:blank" {
		if err := p.Call(ctx, p.Page, "Page.navigate", map[string]any{"url": rawURL}, nil); err != nil {
			t.Fatal(err)
		}
	}
	return p, camoufox.Endpoint(port)
}

// StartWithProxy is used only by HTTP fixtures that map a public-looking origin to localhost.
func StartWithProxy(t testing.TB, proxy string) (context.Context, context.CancelFunc, func()) {
	t.Helper()
	dir := ConfigDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	geoip := false
	p, err := camoufox.Start(ctx, dir, camoufox.Options{Profile: t.TempDir(), Headless: true, Proxy: proxy, GeoIP: &geoip, IgnoreHTTPSErrors: true}, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	tab, closeTab := context.WithCancel(camoufox.PageContext(p.Context(), p, p.Page))
	return tab, closeTab, p.Close
}
