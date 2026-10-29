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

func TestTaskTargetStaysInBackground(t *testing.T) {
	p := pageTarget("https://www.doubao.com/chat/", true)
	if p["background"] != true || p["newWindow"] != false {
		t.Fatalf("出图新标签必须留在后台: %#v", p)
	}
}

func TestNeedsMinimizeOnlyWhenStillOnScreen(t *testing.T) {
	if needsMinimize("minimized", 0, 0) || needsMinimize("normal", -2400, -2400) {
		t.Fatal("已经藏好的窗口不要再改")
	}
	if !needsMinimize("normal", 0, 0) || !needsMinimize("normal", -2400, 12) {
		t.Fatal("被夹回屏幕的窗口要最小化")
	}
}

func TestReleaseLoginFrontLetsTheNextRunPark(t *testing.T) {
	holdFront("http://127.0.0.1:9340")
	if !frontHeld("http://127.0.0.1:9340") {
		t.Fatal("登录窗口应占着前台")
	}
	ReleaseLoginFront("http://127.0.0.1:9340")
	if frontHeld("http://127.0.0.1:9340") {
		t.Fatal("关掉登录窗口后，出图仍被挡在屏幕里")
	}
}

func TestFrontHoldIsPerDebugPort(t *testing.T) {
	holdFront("http://127.0.0.1:9336")
	t.Cleanup(func() { releaseFront("http://127.0.0.1:9336") })
	if !frontHeld("ws://127.0.0.1:9336/devtools/browser/abc") {
		t.Fatal("登录占用要按调试端口认")
	}
	if frontHeld("http://127.0.0.1:9337") {
		t.Fatal("别的端口不该被占用")
	}
	s := &Session{client: &Client{wsURL: "ws://127.0.0.1:9336/devtools/browser/abc"}}
	s.KeepOffscreen()
	if frontHeld(s.client.wsURL) {
		t.Fatal("出图要解除登录的前台占用")
	}
}

func TestPresentForCaptchaHoldsFrontUntilParked(t *testing.T) {
	prev := presentForCaptcha
	presentForCaptcha = func(string) {}
	t.Cleanup(func() { presentForCaptcha = prev })
	s := &Session{client: &Client{wsURL: "http://127.0.0.1:9338"}}
	s.PresentForCaptcha()
	if !frontHeld(s.client.wsURL) {
		t.Fatal("验证码是出图过程中唯一该把窗口拿回前台的情况")
	}
	s.KeepOffscreen()
	if frontHeld(s.client.wsURL) {
		t.Fatal("验证通过后要放开前台，窗口才能回到屏幕外")
	}
}

func TestOffscreenParkYieldsToCaptcha(t *testing.T) {
	ep := "http://127.0.0.1:9391"
	if keepParked(ep, false) {
		t.Fatal("没有人在看窗口时，这条限制不该挡着")
	}
	holdFront(ep)
	t.Cleanup(func() { releaseFront(ep) })
	if !keepParked(ep, false) {
		t.Fatal("验证码或去处理占用前台时，不能再写 -2400")
	}
	if keepParked(ep, true) {
		t.Fatal("拿到前台不受这条限制")
	}
}

func TestTypeChunksKeepTheWholePrompt(t *testing.T) {
	if got := strings.Join(typeChunks("你好hello"), ""); got != "你好hello" {
		t.Fatalf("chunks = %q", got)
	}
	parts := typeChunks("123456789012345")
	if len(parts) != 2 || parts[0] != "12345678" || parts[1] != "9012345" {
		t.Fatalf("parts = %#v", parts)
	}
	if len(typeChunks("")) != 1 || typeChunks("")[0] != "" {
		t.Fatal("empty prompt still inserts once so the field can be cleared")
	}
}

func TestParkStepsDoesNotRestateNormalOffscreen(t *testing.T) {
	if parkSteps("normal", -2400, -2400) != nil {
		t.Fatal("已经在屏幕外的正常窗口不能再 setWindowBounds，否则会被激活到前台")
	}
	maximized := parkSteps("maximized", 0, 0)
	if len(maximized) != 2 {
		t.Fatalf("最大化要先恢复再挪走: %#v", maximized)
	}
	if _, ok := maximized[0]["windowState"]; !ok || len(maximized[0]) != 1 {
		t.Fatalf("恢复不能带坐标，否则 Chrome 会丢掉坐标并只 Restore: %#v", maximized[0])
	}
	if _, ok := maximized[1]["windowState"]; ok {
		t.Fatalf("挪到屏幕外不能再带 windowState=normal: %#v", maximized[1])
	}
	if maximized[1]["left"] != -2400 || maximized[1]["top"] != -2400 {
		t.Fatalf("坐标: %#v", maximized[1])
	}
	normal := parkSteps("normal", 80, 48)
	if len(normal) != 1 {
		t.Fatalf("已经是正常窗口只改坐标: %#v", normal)
	}
	if _, ok := normal[0]["windowState"]; ok {
		t.Fatal("正常窗口再写 windowState=normal 会把窗口拉到前面")
	}
}

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

func TestQianwenKeepsOneTab(t *testing.T) {
	pages := []pageRef{
		{ID: "a", URL: "https://www.qianwen.com/"},
		{ID: "b", URL: "https://www.qianwen.com/chat/1"},
		{ID: "c", URL: "https://www.tongyi.com/"},
		{ID: "d", URL: "about:blank"},
		{ID: "e", URL: "https://www.doubao.com/chat/"},
	}
	keep, closeIDs, create := chooseBackgroundPage(pages, "https://www.tongyi.com/")
	if create || keep != "c" {
		t.Fatalf("keep=%s create=%v close=%v", keep, create, closeIDs)
	}
	closed := map[string]bool{}
	for _, id := range closeIDs {
		closed[id] = true
	}
	if !closed["a"] || !closed["b"] || !closed["d"] || closed["c"] || closed["e"] || len(closeIDs) != 3 {
		t.Fatalf("close=%v", closeIDs)
	}
	extra := extraPages(pages, "https://www.tongyi.com/", "b")
	left := map[string]bool{}
	for _, id := range extra {
		left[id] = true
	}
	if !left["a"] || !left["c"] || !left["d"] || left["b"] || left["e"] {
		t.Fatalf("extra=%v", extra)
	}
}
