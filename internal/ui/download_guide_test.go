package ui

import (
	"fmt"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/browser"
)

func TestDownloadGuideDescribesSupportedPagesAndLimits(t *testing.T) {
	for _, tc := range []struct{ query, site, detail string }{
		{"AMAZON", "亚马逊", "搜索结果"},
		{"ozon", "Ozon", "仅通过合成测试"},
		{"Temu", "Temu", "下载浏览器"},
		{"小红书", "小红书", "主页尚未实测"},
		{"360.cn", "百度图片", "图片搜索结果"},
		{"网易摄影", "网易摄影", "尚未提供"},
	} {
		var matched bool
		for _, group := range matchingDownloadGuide(tc.query) {
			for _, entry := range group.entries {
				if strings.Contains(entry.site, tc.site) && strings.Contains(entry.pages, tc.detail) {
					matched = true
				}
			}
		}
		if !matched {
			t.Errorf("%q 没有对应页面类型或限制", tc.query)
		}
	}
	if len(matchingDownloadGuide("没有这个网站123")) != 0 {
		t.Fatal("未知网站不应匹配专用页面说明")
	}
}

func TestDownloadGuideOpensSitesInDedicatedBrowser(t *testing.T) {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy"} {
		t.Setenv(key, "")
	}
	browser.SetProxy("")
	t.Cleanup(func() { browser.SetProxy("") })
	a, _ := newDownloadApp(t)
	session := &fakeDownloadBrowser{}
	a.dlBrowser, a.dlBrowserURL = session, "https://previous.test/"
	a.dlComposerOn, a.dlGuideOn, a.dlGuideSearch = true, true, "百度图片"
	a.dlURLs = "https://example.test/keep\nhttps://example.test/also-keep"
	tt := ui.NewTester(a.view, 1024, 700)
	for _, tc := range []struct{ label, target string }{
		{"打开 baidu.com", "https://image.baidu.com/"},
		{"打开 sogou.com", "https://pic.sogou.com/"},
		{"打开 360.cn", "https://image.so.com/"},
	} {
		if err := tt.Click(tc.label); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for a.dlBrowserBusy && time.Now().Before(deadline) {
			a.flushUIUpdates()
			time.Sleep(time.Millisecond)
		}
		if a.dlBrowserBusy {
			t.Fatal("打开网址超时")
		}
		if session.target != tc.target || a.dlBrowserURL != tc.target || a.dlBrowser != session {
			t.Fatalf("%s 未在已有专用浏览器打开: target=%q browserURL=%q error=%q", tc.label, session.target, a.dlBrowserURL, a.dlBrowserErr)
		}
		if !a.dlBrowserMode || !a.dlGuideOn || a.dlURLs != "https://example.test/keep\nhttps://example.test/also-keep" {
			t.Fatalf("打开网站后任务状态被改变: mode=%v guide=%v urls=%q", a.dlBrowserMode, a.dlGuideOn, a.dlURLs)
		}
		tt.Frame()
	}
	task := newDownloadTask(1, a.dlBrowserURL, t.TempDir())
	task.Rendered, task.Phase = true, dlQueued
	a.dlTasks = []*downloadTask{task}
	if err := tt.Click("打开 baidu.com"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !strings.Contains(a.dlBrowserErr, "暂停") || !tt.HasText("请等当前浏览器下载结束或暂停后再打开其他网址") || session.target != "https://image.so.com/" {
		t.Fatalf("排队任务未阻止切换网站: %q", a.dlBrowserErr)
	}
}

func TestDownloadGuideShowsBrowserLaunchError(t *testing.T) {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy"} {
		t.Setenv(key, "")
	}
	browser.SetProxy("")
	t.Cleanup(func() { browser.SetProxy("") })
	a, _ := newDownloadApp(t)
	a.dlComposerOn, a.dlGuideOn, a.dlGuideSearch = true, true, "Ozon"
	tt := ui.NewTester(a.view, 480, 500)
	if err := tt.Click("打开 ozon.ru"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("无法打开下载浏览器") {
		t.Fatalf("未显示浏览器启动错误: %v", tt.Texts())
	}
	assertVisible(t, tt, "无法打开下载浏览器", 480, 500)
	assertVisible(t, tt, "返回新建任务", 480, 500)
}

func TestDownloadGuideSiteIcons(t *testing.T) {
	files, err := downloadGuideIconFiles.ReadDir("assets/download-sites")
	if err != nil || len(files) != len(downloadGuideIcons) {
		t.Fatalf("站点图标无法读取或解码: files=%d icons=%d err=%v", len(files), len(downloadGuideIcons), err)
	}
	used := make(map[string]bool)
	for _, group := range downloadGuideGroups {
		for _, entry := range group.entries {
			if entry.site != "图片直链、普通网页" && entry.icons == "" {
				t.Errorf("%s 缺少图标映射", entry.site)
			}
			for _, domain := range strings.Fields(entry.icons) {
				used[domain] = true
				target, err := url.Parse(downloadGuideURL(domain))
				if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil {
					t.Errorf("%s 的快捷入口无效: %q, %v", domain, downloadGuideURL(domain), err)
				}
			}
		}
	}
	for host, want := range map[string]string{"bing.com": "https://www.bing.com/images", "google.com": "https://www.google.com/imghp"} {
		if got := downloadGuideURL(host); got != want {
			t.Errorf("%s 入口 = %q, want %q", host, got, want)
		}
	}
	for domain := range downloadGuideIcons {
		if !used[domain] {
			t.Errorf("%s 的图标没有对应站点", domain)
		}
	}
	for _, domain := range []string{"baidu.com", "sogou.com", "360.cn", "ozon.ru", "xiaohongshu.com", "ymatou.com", "zcygov.cn", "cnu.cc"} {
		if !used[domain] || downloadGuideIcons[domain] == nil {
			t.Errorf("%s 的站点图标未配置", domain)
		}
	}
	for _, tc := range []struct{ query, title string }{
		{"苏宁", "苏宁易购、义乌购、微店、有赞"},
		{"Wellco", "Wellco"},
		{"网易摄影", "网易摄影、天涯、喔图、微销通、YouTube、nhentai"},
	} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-dark-%t", tc.query, dark), func(t *testing.T) {
				a, _ := newDownloadApp(t)
				a.dlComposerOn, a.dlGuideOn, a.dlGuideSearch = true, true, tc.query
				tt := ui.NewTester(a.view, 480, 500)
				tt.SetDark(dark)
				assertVisible(t, tt, tc.title, 480, 500)
				assertVisible(t, tt, "返回新建任务", 480, 500)
				saveDownloadGuideShot(t, tt, fmt.Sprintf("guide-icons-%s-dark-%t.png", tc.query, dark))
			})
		}
	}
}

func TestDownloadGuideSearchAndReturn(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlComposerOn = true
	a.dlURLs = "https://example.com/gallery"
	a.dlBrowserMode = true
	tt := ui.NewTester(a.view, 1280, 800)
	assertVisible(t, tt, "支持哪些页面？", 1280, 800)
	if err := tt.Click("支持哪些页面？"); err != nil {
		t.Fatal(err)
	}
	assertVisible(t, tt, "搜索网站或页面类型", 1280, 800)
	assertVisible(t, tt, "返回新建任务", 1280, 800)
	list := assertVisible(t, tt, "支持的页面列表", 1280, 800)
	tt.Scroll(list.X+list.W/2, list.Y+list.H/2, 0, 800)
	if a.dlGuideScroll.Y <= 0 {
		t.Fatal("页面列表无法滚动")
	}
	if err := tt.Click("搜索网站或页面类型"); err != nil {
		t.Fatal(err)
	}
	tt.Type("oZoN")
	if a.dlGuideScroll.Y != 0 {
		t.Fatalf("搜索后没有回到结果顶部: %v", a.dlGuideScroll.Y)
	}
	if !tt.HasText("Ozon") || !tt.HasText("仅通过合成测试") || !tt.HasText("1 条说明") || tt.HasText("eBay 易贝") || tt.HasText("向下滚动查看更多站点") {
		t.Fatalf("搜索结果不正确: %v", tt.Texts())
	}
	assertVisible(t, tt, "Ozon", 1280, 800)
	saveDownloadGuideShot(t, tt, "guide-search.png")
	a.dlGuideSearch = "没有这个网站123"
	tt.Frame()
	if !tt.HasText("没有找到匹配的站点") || !tt.HasText("0 条说明") {
		t.Fatalf("无结果提示缺失: %v", tt.Texts())
	}
	assertVisible(t, tt, "没有找到匹配的站点；也可以尝试粘贴普通网页地址。", 1280, 800)
	saveDownloadGuideShot(t, tt, "guide-empty.png")
	if err := tt.Click("返回新建任务"); err != nil {
		t.Fatal(err)
	}
	if a.dlGuideOn || !a.dlComposerOn || a.dlURLs != "https://example.com/gallery" || !a.dlBrowserMode {
		t.Fatalf("返回后任务内容发生变化: guide=%v composer=%v urls=%q browser=%v", a.dlGuideOn, a.dlComposerOn, a.dlURLs, a.dlBrowserMode)
	}
	if err := tt.Click("支持哪些页面？"); err != nil {
		t.Fatal(err)
	}
	if a.dlGuideSearch != "" {
		t.Fatalf("重新打开指南未清空搜索: %q", a.dlGuideSearch)
	}
	tt.Key(0, ui.KeyEscape)
	if a.dlComposerOn {
		t.Fatal("Escape 没有关闭新建任务")
	}
	tt.Frame()
	if a.dlGuideOn {
		t.Fatal("关闭任务后指南状态没有重置")
	}
}

func TestDownloadGuideEntryFitsComposer(t *testing.T) {
	for _, size := range [][2]int{{1280, 800}, {1024, 700}, {760, 600}, {480, 500}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			a, _ := newDownloadApp(t)
			a.dlComposerOn = true
			tt := ui.NewTester(a.view, size[0], size[1])
			for _, label := range []string{"支持哪些页面？", "链接", "取消", "立即下载"} {
				assertVisible(t, tt, label, float32(size[0]), float32(size[1]))
			}
			scroll := assertVisible(t, tt, "下载任务选项", float32(size[0]), float32(size[1]))
			tt.Scroll(scroll.X+scroll.W/2, scroll.Y+scroll.H/2, 0, 1000)
			assertVisible(t, tt, "下载设置", float32(size[0]), float32(size[1]))
			assertVisible(t, tt, "立即下载", float32(size[0]), float32(size[1]))
		})
	}
}

func TestDownloadComposerBrowserModeFitsSmallWindow(t *testing.T) {
	a, _ := newDownloadApp(t)
	a.dlComposerOn, a.dlBrowserMode = true, true
	tt := ui.NewTester(a.view, 480, 500)
	for _, label := range []string{"支持哪些页面？", "链接", "取消", "立即下载"} {
		assertVisible(t, tt, label, 480, 500)
	}
	scroll := assertVisible(t, tt, "下载任务选项", 480, 500)
	tt.Scroll(scroll.X+scroll.W/2, scroll.Y+scroll.H/2, 0, 1500)
	assertVisible(t, tt, "下载设置", 480, 500)
	assertVisible(t, tt, "立即下载", 480, 500)
}

func TestDownloadGuideLayout(t *testing.T) {
	for _, size := range [][2]int{{1280, 800}, {1024, 700}, {760, 600}, {480, 500}} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d-dark-%t", size[0], size[1], dark), func(t *testing.T) {
				a, _ := newDownloadApp(t)
				a.dlComposerOn, a.dlGuideOn = true, true
				tt := ui.NewTester(a.view, size[0], size[1])
				tt.SetDark(dark)
				for _, label := range []string{"支持哪些页面？", "搜索网站或页面类型", "网站与页面", "普通网页和图片搜索", "返回新建任务"} {
					assertVisible(t, tt, label, float32(size[0]), float32(size[1]))
				}
				saveDownloadGuideShot(t, tt, fmt.Sprintf("guide-%dx%d-dark-%t.png", size[0], size[1], dark))
			})
		}
	}
}

func saveDownloadGuideShot(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("TULAI_DOWNLOAD_GUIDE_SHOTS")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, tt.Image()); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
