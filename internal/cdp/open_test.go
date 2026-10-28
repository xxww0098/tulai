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

	"github.com/xxww0098/tulai/internal/browser"
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
	count, matching, blank := 0, 0, 0
	for _, p := range pages {
		if p.Type != "page" {
			continue
		}
		count++
		if p.URL == url {
			matching++
		}
		if p.URL == "about:blank" || p.URL == "" {
			blank++
		}
	}
	if matching != 1 || blank != 0 {
		t.Fatalf("打开后可见页应是登录页，got %d页 %d登录页 %d空白页", count, matching, blank)
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	visible, err := VisibleURL(ctx2, wsURL)
	if err != nil || visible != url {
		t.Fatalf("visible = %q err=%v", visible, err)
	}
}

func TestChooseLoginPagePrefersTheVisibleBlankTab(t *testing.T) {
	want := "https://www.doubao.com/chat/"
	id, nav := chooseLoginPage([]pageHit{
		{ID: "blank", URL: "about:blank", Vis: "visible"},
		{ID: "login", URL: want, Vis: "hidden"},
	}, want)
	if id != "login" || nav {
		t.Fatalf("hidden login should be focused, id=%s nav=%v", id, nav)
	}
	id, nav = chooseLoginPage([]pageHit{
		{ID: "blank", URL: "about:blank", Vis: "visible"},
	}, want)
	if id != "blank" || !nav {
		t.Fatalf("visible blank should be navigated, id=%s nav=%v", id, nav)
	}
	id, nav = chooseLoginPage([]pageHit{
		{ID: "welcome", URL: "edge://welcome", Vis: "visible"},
	}, "https://www.tongyi.com/")
	if id != "welcome" || !nav {
		t.Fatalf("edge welcome should be navigated, id=%s nav=%v", id, nav)
	}
	if !blankLike("about:blank") || !blankLike("chrome://newtab") || !blankLike("https://ntp.msn.com/edge/ntp") {
		t.Fatal("blank pages")
	}
	if !landed("https://www.doubao.com/chat/", want) || !landed("https://login.doubao.com/", want) {
		t.Fatal("same site")
	}
	if landed("about:blank", want) || landed("https://example.com/", want) {
		t.Fatal("not the login site")
	}
	qianwen := "https://www.tongyi.com/"
	if !landed("https://www.qianwen.com/?ch=tongyi_redirect", qianwen) || !landed("https://chat.qwen.ai/", qianwen) {
		t.Fatal("qianwen login redirect should count as the tongyi page")
	}
	if landed("https://not-qianwen.com/", qianwen) || landed("https://www.doubao.com/chat/", qianwen) {
		t.Fatal("unrelated host counted as qianwen")
	}
}
