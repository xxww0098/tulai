package cdp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"xbs/internal/browser"
)

func TestOpenURLKeepsPageAfterDisconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过 Chrome E2E")
	}
	chrome, err := browser.FindChromePath()
	if err != nil {
		t.Skip("本机无 Chrome，跳过 E2E")
	}
	profile := t.TempDir()
	opts := append(chromedp.DefaultExecAllocatorOptions[:], chromedp.ExecPath(chrome), chromedp.UserDataDir(profile))
	alloc, stop := chromedp.NewExecAllocator(context.Background(), opts...)
	defer stop()
	ctx, closeTab := chromedp.NewContext(alloc)
	defer closeTab()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
	if err != nil {
		t.Fatal(err)
	}
	address := strings.Fields(string(data))
	if len(address) != 2 {
		t.Fatalf("无效 CDP 地址: %q", data)
	}
	wsURL := "ws://127.0.0.1:" + address[0] + address[1]
	requested := make(chan struct{}, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			requested <- struct{}{}
		}
		w.Write([]byte("<html><body>login</body></html>"))
	}))
	defer server.Close()
	url := server.URL + "/login"
	for range 3 {
		if err := OpenURL(wsURL, url); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-requested:
	case <-ctx.Done():
		t.Fatal("控制连接退出后登录页未加载")
	}
	pages, err := chromedp.Targets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count, matching := 0, 0
	for _, p := range pages {
		if p.Type == "page" {
			count++
			if p.URL == url {
				matching++
			}
		}
	}
	if count != 2 || matching != 1 {
		t.Fatalf("三次打开后应保留1个原始空白页与1个登录页，got %d页，%d登录页", count, matching)
	}
}
