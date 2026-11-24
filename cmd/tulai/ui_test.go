package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/prompts"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/ui"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func startTestUI(t *testing.T, withBrowser bool) (*nsgotest.App, *store.Store) {
	t.Helper()
	return startTestUIWithSize(t, withBrowser, 1280, 820)
}

func startTestUIWithSize(t *testing.T, withBrowser bool, width, height core.Points) (*nsgotest.App, *store.Store) {
	t.Helper()
	return startTestUIWithSetup(t, withBrowser, width, height, nil)
}

func startTestUIWithSetup(t *testing.T, withBrowser bool, width, height core.Points, setup func(*store.Store)) (*nsgotest.App, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	if withBrowser {
		if _, err := st.AddBrowser("豆包", "测试账号", filepath.Join(dir, "profile-1")); err != nil {
			t.Fatal(err)
		}
		st.Set("wizard_done", "1")
	}
	if setup != nil {
		setup(st)
	}

	bm := browser.NewManager()
	eng := tasks.NewEngine(st, bm, nil, dir)
	t.Cleanup(eng.Shutdown)

	ctrl := ui.NewApp(st, bm, eng, dir)
	ui.SetTestApp(ctrl)
	t.Cleanup(func() { ui.SetTestApp(nil) })

	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		ctrl.BindScope(cx)
		return Root(cx)
	}, nsgotest.Size(width, height), nsgotest.Idiom(core.IdiomDesktop))
	return app, st
}

func assertHasTexts(t *testing.T, app *nsgotest.App, want ...string) {
	t.Helper()
	got := app.Texts()
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Fatalf("missing text %q in %v", w, got)
		}
	}
}

func TestShellAndEightModulesRender(t *testing.T) {
	app, _ := startTestUI(t, true)
	assertHasTexts(t, app, "图来 tulai", "图片生成")

	steps := []struct {
		navID string
		title string
	}{
		{"id:nav-dedup", "去重"},
		{"id:nav-browser", "浏览器"},
		{"id:nav-prompts", "提示词"},
		{"id:nav-download", "下载"},
		{"id:nav-rename", "批量重命名"},
		{"id:nav-imageedit", "图片编辑"},
		{"id:nav-settings", "设置"},
		{"id:nav-workbench", "图片生成"},
	}

	for _, tc := range steps {
		app.Press(tc.navID)
		if got := app.Find("id:page-title").Text; got != tc.title {
			t.Fatalf("after %s: page-title = %q, want %q", tc.navID, got, tc.title)
		}
		if tc.navID == "id:nav-browser" {
			assertHasTexts(t, app, "Camoufox (默认)", "Chrome CDP", "复制令牌")
		}
	}
}

// nsgo 0.3.0 removed the Badge variant the platform and site tags drew with,
// so they use tulai/native's OutlineBadge. It keeps that variant's look: the
// palette's text, no fill, and a rim halfway between the background and the
// muted text. The colors are the palette's as the app runs it today.
func TestOutlineBadgeKeepsItsRim(t *testing.T) {
	const platform = "outline-probe-platform"
	for _, tc := range []struct {
		theme string
		text  uint32
		rim   uint32
	}{
		{"light", 0x333333FF, 0x999999C6},
		{"dark", 0xEAEAEAFF, 0x848484C6},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			dir := t.TempDir()
			app, _ := startTestUIWithSetup(t, true, 1280, 820, func(st *store.Store) {
				if _, err := st.AddBrowser(platform, "outline-probe", filepath.Join(dir, "probe")); err != nil {
					t.Fatal(err)
				}
				st.Set("ui_theme", tc.theme)
			})
			app.Press("id:nav-browser")

			label := app.Find(platform)
			badge := label.Parent
			if badge == nil {
				t.Fatalf("platform tag %q has no parent view", platform)
			}
			if got := uint32(label.TextStyle.Color); got != tc.text {
				t.Fatalf("%s: outline badge text = %#08x, want %#08x", tc.theme, got, tc.text)
			}
			s := badge.Style
			if got := uint32(s.BorderColor); got != tc.rim {
				t.Fatalf("%s: outline badge rim = %#08x, want %#08x", tc.theme, got, tc.rim)
			}
			if s.BorderWidth != 1 || s.CornerRadius != 12 || s.Background != 0 {
				t.Fatalf("%s: outline badge = width %v, radius %v, fill %#08x; want width 1, radius 12, no fill",
					tc.theme, s.BorderWidth, s.CornerRadius, uint32(s.Background))
			}
		})
	}
}

func TestNavigationWithLongAccountLists(t *testing.T) {
	dir := t.TempDir()
	app, st := startTestUIWithSetup(t, true, 1280, 820, func(st *store.Store) {
		for i := 2; i <= 60; i++ {
			name := fmt.Sprintf("测试账号 %02d", i)
			if _, err := st.AddBrowser("豆包", name, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
		}
	})
	accounts := st.Browsers()
	first, last := accounts[0], accounts[len(accounts)-1]
	lane := func(i int) core.Bounds {
		return app.Find(fmt.Sprintf("id:lane-toggle-%d", accounts[i].ID)).Bounds()
	}
	if a, b := lane(0), lane(1); b.Origin.Y <= a.Origin.Y || b.Origin.X < a.Origin.X-1 || b.Origin.X > a.Origin.X+1 {
		t.Fatalf("workbench account rows should stack vertically as a list: %+v, %+v", a, b)
	}
	if app.Exists("text:" + last.Name) {
		t.Fatal("workbench mounted an account far outside the viewport")
	}

	app.Press("id:nav-browser")
	const list = "id:browser-account-list"
	assertHasTexts(t, app, "账号列表 (60)", first.Name)
	if app.Exists("text:" + last.Name) {
		t.Fatal("browser page mounted an account far outside the viewport")
	}
	rowBox := func(name string) core.Bounds {
		return app.Find(fmt.Sprintf("id:browser-account-list >> text:%q", name)).Bounds()
	}
	if r0, r1 := rowBox(first.Name), rowBox(accounts[1].Name); r1.Origin.Y <= r0.Origin.Y || r1.Origin.X < r0.Origin.X-1 || r1.Origin.X > r0.Origin.X+1 {
		t.Fatalf("browser account rows should stack vertically as a list: %+v, %+v", r0, r1)
	}
	app.Press(fmt.Sprintf("text:%q", first.Name))
	input := fmt.Sprintf("id:browser-name-%d", first.ID)
	app.Clear(input)
	app.Type(input, "更新后的备注")
	app.Scroll(list, 5000)
	app.WaitFor("text:"+last.Name, 2*time.Second)
	if app.Exists(fmt.Sprintf("id:browser-name-%d", last.ID)) {
		t.Fatal("the recycled row inherited another account's editor")
	}
	app.Scroll(list, -5000)
	if got := app.Find(input).Text; got != "更新后的备注" {
		t.Fatalf("account draft after scrolling = %q", got)
	}
	app.Press(`text:"保存"`)
	app.WaitFor("text:更新后的备注", 2*time.Second)
	accounts = st.Browsers()
	if accounts[0].Name != "更新后的备注" || accounts[len(accounts)-1].Name != last.Name {
		t.Fatal("renaming after scrolling updated the wrong account")
	}
	app.Press("id:nav-settings")
	st.RenameBrowser(first.ID, "后台更新的备注")
	app.Press("id:nav-browser")
	assertHasTexts(t, app, "后台更新的备注")
}

func TestFloatingAccountViewSwitch(t *testing.T) {
	app, _ := startTestUIWithSetup(t, true, 1280, 820, func(st *store.Store) {
		if _, err := st.AddBrowser("豆包", "备用账号", filepath.Join(t.TempDir(), "profile")); err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddBrowser("千问", "千问账号", filepath.Join(t.TempDir(), "qianwen")); err != nil {
			t.Fatal(err)
		}
		// Keep this UI test offline and exercise the drawer's connection error state.
		for _, row := range st.Browsers() {
			if err := st.SetBrowserProxy(row.ID, "invalid-proxy"); err != nil {
				t.Fatal(err)
			}
		}
	})
	const drawer = "id:browser-drawer-dialog"
	open := func(name string) {
		t.Helper()
		selector := "label:查看浏览器 · " + name
		button := app.Find(selector).Bounds()
		if app.Exists(drawer) {
			panel := app.Find(drawer).Bounds()
			if button.Origin.X < panel.Origin.X+panel.Size.Width && button.Origin.X+button.Size.Width > panel.Origin.X &&
				button.Origin.Y < panel.Origin.Y+panel.Size.Height && button.Origin.Y+button.Size.Height > panel.Origin.Y {
				t.Fatalf("the floating window covers the view button for %q: %+v, window %+v", name, button, panel)
			}
		}
		app.PressAt(selector, core.Point{X: button.Size.Width / 2, Y: button.Size.Height / 2})
		app.WaitFor(drawer+" >> text:"+name, 2*time.Second)
		app.WaitFor(drawer+" >> text:无法打开内置页面：账号代理地址无效", 2*time.Second)
		root := app.Find("id:app-root").Bounds()
		panel := app.Find(drawer).Bounds()
		assertInsideDialog(t, "account drawer", root, panel)
		if panel.Origin.X <= root.Origin.X || panel.Origin.Y <= root.Origin.Y ||
			panel.Origin.X+panel.Size.Width >= root.Origin.X+root.Size.Width ||
			panel.Origin.Y+panel.Size.Height >= root.Origin.Y+root.Size.Height {
			t.Fatalf("expected the account window to float inside the page, got %+v in %+v", panel, root)
		}
		assertInsideDialog(t, "browser viewport", panel, app.Find("id:browser-drawer-stage").Bounds())
	}
	topbar := app.Find("id:topbar").Bounds()
	composer := app.Find("id:wb-composer").Bounds()
	open("备用账号")
	if app.FindMounted("id:topbar").Bounds() != topbar || app.FindMounted("id:wb-composer").Bounds() != composer {
		t.Fatal("opening the account window changed the workbench layout")
	}
	panelID := app.Find(drawer).ID
	start := app.Find(drawer).Bounds()
	app.Drag("id:browser-drawer-handle", core.Point{Y: 20}, core.Point{Y: 40})
	app.WaitUntil(2*time.Second, func() bool { return app.Find(drawer).Bounds().Origin.Y > start.Origin.Y+10 })
	moved := app.Find(drawer).Bounds()
	open("测试账号")
	if panel := app.Find(drawer); panel.ID != panelID || panel.Bounds() != moved {
		t.Fatal("switching accounts replaced or moved the floating window")
	}
	behind := app.Find("label:查看浏览器 · 备用账号").Bounds()
	center := core.Point{X: behind.Origin.X + behind.Size.Width/2, Y: behind.Origin.Y + behind.Size.Height/2}
	app.Drag("id:browser-drawer-handle", core.Point{X: center.X - moved.Origin.X - moved.Size.Width/2})
	stage := app.Find("id:browser-drawer-stage").Bounds()
	if !stage.Contains(center) {
		t.Fatalf("expected the dragged browser to cover the other account button, got %+v and %+v", stage, behind)
	}
	app.PressAt("id:browser-drawer-stage", core.Point{X: center.X - stage.Origin.X, Y: center.Y - stage.Origin.Y})
	app.WaitFor(drawer+" >> text:测试账号", 2*time.Second)
	app.Press(drawer + " >> text:关闭")
	app.WaitForGone(drawer, 2*time.Second)
	assertHasTexts(t, app, "图片生成")

	app.Press("label:收起侧边栏")
	open("测试账号")
	open("备用账号")
	app.Press(drawer + " >> text:关闭")
	app.WaitForGone(drawer, 2*time.Second)

	app.Resize(900, 620)
	app.Press("id:nav-browser")
	list := app.Find("id:browser-account-list").Bounds()
	open("测试账号")
	open("备用账号")
	open("千问账号")
	if app.FindMounted("id:browser-account-list").Bounds() != list {
		t.Fatal("opening the account window changed the account list layout")
	}
	app.Press("id:browser-drawer-stage")
	app.Key("escape")
	app.WaitForGone(drawer, 2*time.Second)
	assertHasTexts(t, app, "账号列表 (3)", "千问账号")
}

func TestWorkbenchThreeModes(t *testing.T) {
	app, _ := startTestUI(t, true)
	assertHasTexts(t, app, "自动去水印")

	washTab := app.Find("id:wb-mode-tabs >> text:批量洗图").Bounds()
	freeTab := app.Find("id:wb-mode-tabs >> text:自由生图").Bounds()
	fissionTab := app.Find("id:wb-mode-tabs >> text:裂变图片").Bounds()
	if !(washTab.Origin.X < freeTab.Origin.X && freeTab.Origin.X < fissionTab.Origin.X) {
		t.Fatalf("expected 批量洗图 first, got wash=%+v free=%+v fission=%+v", washTab, freeTab, fissionTab)
	}

	app.Press("text:自由生图")
	assertHasTexts(t, app, "按文本批量")

	app.Press("text:裂变图片")
	assertHasTexts(t, app, "跳过参考图直接按提示词裂变")

	app.Press("text:批量洗图")
	assertHasTexts(t, app, "自动去水印")
}

func TestWorkbenchShowsCompletedRun(t *testing.T) {
	app, st := startTestUI(t, true)
	assertHasTexts(t, app, "今日 0 次（成 0）")
	row := st.Browsers()[0]
	if err := st.RecordUse(row.ID, time.Now(), 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	const summary = "成功 1 · 失败 0 · 待验证 0 · 未完成 0"
	id, err := st.SaveActive(store.Session{Page: store.PageWash, Prompts: "进度测试", Progress: 1, ProgText: summary}, []store.SessionTask{
		{Platform: row.Platform, AccountID: row.ID, Status: string(tasks.StatusSuccess)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ArchiveSession(id); err != nil {
		t.Fatal(err)
	}
	app.Press("id:wb-history-btn")
	app.WaitFor("text:进度测试", 3*time.Second)
	app.Press(`text:"打开"`)
	app.WaitFor("text:"+summary, 3*time.Second)
	assertHasTexts(t, app, "今日 1 次（成 1）", "100%")
}

func TestThemeLogDrawerAndSettings(t *testing.T) {
	app, _ := startTestUI(t, true)

	// Verify topbar no longer exposes theme toggle or manual wizard trigger once initialized
	if app.Exists("id:topbar-theme-btn") {
		t.Fatal("topbar-theme-btn should not be present")
	}
	if app.Exists("id:topbar-wizard-btn") {
		t.Fatal("topbar-wizard-btn should not be present")
	}

	// Open and close LogDrawer
	app.Press("id:topbar-log-btn")
	app.Find("id:log-drawer")
	assertHasTexts(t, app, "运行日志")
	app.Press("label:关闭运行日志")
	if app.Exists("id:log-drawer") {
		t.Fatal("log drawer remained visible after closing")
	}

	// Navigate to Settings and verify 4 consolidated tabs without manual wizard button
	app.Press("id:nav-settings")
	assertHasTexts(t, app, "常规与输出", "账号与任务", "网络与高级", "关于与更新")
	if app.Exists("text:设置向导") {
		t.Fatal("settings toolbar should not expose 设置向导 button")
	}
	app.Press("text:网络与高级")
	assertHasTexts(t, app, "启用自定义代理")
	app.Press("text:关于与更新")
	assertHasTexts(t, app, "检查更新")
}

func TestFirstRunWizardCanBeSkipped(t *testing.T) {
	app, st := startTestUI(t, false)
	app.Find("id:wizard-dialog")
	assertHasTexts(t, app, "快速配置向导")
	app.Press("text:跳过配置")
	// The async action starts a dialog exit animation on the test clock.
	app.WaitUntil(3*time.Second, func() bool {
		app.Advance(200 * time.Millisecond)
		return !app.Exists("id:wizard-dialog")
	})
	if st.Get(wizard.KeyComplete) != wizard.CompleteYes {
		t.Fatal("skipping the wizard did not persist completion")
	}
}

func TestWorkbenchPlatformSingleSelect(t *testing.T) {
	app, st := startTestUI(t, true)
	if !slices.Equal(st.Platforms(), []string{"豆包"}) {
		t.Fatalf("initial st.Platforms() = %v, want [豆包]", st.Platforms())
	}

	for _, tc := range []struct {
		platform string
		blocked  string
	}{
		{"千问", "缺少账号：千问"},
		{"ChatGPT", "缺少账号：ChatGPT"},
		{"Antigravity", "缺少账号：Antigravity"},
		{"豆包", ""},
	} {
		app.Press("id:wb-platform-tabs >> text:" + tc.platform)
		if got := st.Platforms(); !slices.Equal(got, []string{tc.platform}) {
			t.Fatalf("after clicking %q: st.Platforms() = %v", tc.platform, got)
		}
		if tc.blocked != "" {
			assertHasTexts(t, app, tc.blocked)
		}
	}
}

func assertSideBySide(t *testing.T, app *nsgotest.App, pageName string, ids ...string) {
	t.Helper()
	var prev core.Bounds
	for i, id := range ids {
		b := app.Find("id:" + id).Frame
		if b.Size.Width <= 0 || b.Size.Height <= 0 {
			t.Fatalf("%s pane %s has non-positive size %+v", pageName, id, b.Size)
		}
		if i > 0 {
			if b.Origin.X < prev.Origin.X+prev.Size.Width-1 {
				t.Fatalf("%s pane %s (%+v) overlaps or is not right of %s (%+v)", pageName, id, b, ids[i-1], prev)
			}
			if b.Origin.Y != prev.Origin.Y {
				t.Fatalf("%s pane %s Y=%v differs from %s Y=%v", pageName, id, b.Origin.Y, ids[i-1], prev.Origin.Y)
			}
		}
		prev = b
	}
}

func TestDesktopPageLayouts(t *testing.T) {
	for _, sz := range []struct {
		w, h core.Points
	}{
		{1280, 820},
		{920, 640},
	} {
		t.Run(fmt.Sprintf("%gx%g", sz.w, sz.h), func(t *testing.T) {
			app, _ := startTestUIWithSize(t, true, sz.w, sz.h)

			assertSideBySide(t, app, "WorkbenchPage", "wb-composer", "wb-stage")

			app.Press("id:nav-dedup")
			assertSideBySide(t, app, "DedupPage", "dedup-queue", "dedup-report")

			app.Press("id:nav-prompts")
			assertSideBySide(t, app, "PromptsPage", "prompts-list", "prompts-editor")

			app.Press("id:nav-download")
			assertSideBySide(t, app, "DownloadPage", "dl-composer", "dl-queue")

			app.Press("id:nav-rename")
			assertSideBySide(t, app, "RenamePage", "rename-config", "rename-preview")

			app.Press("id:nav-imageedit")
			assertSideBySide(t, app, "ImageEditPage", "edit-files", "edit-canvas", "edit-inspector")
		})
	}
}

func TestWashLongPromptKeepsSourcesVisible(t *testing.T) {
	source := filepath.Join(t.TempDir(), "framed-collage.png")
	writeTestPNG(t, source)
	for _, size := range []struct{ w, h core.Points }{{920, 640}, {1280, 820}} {
		for _, dark := range []bool{false, true} {
			t.Run(fmt.Sprintf("%gx%g/dark=%v", size.w, size.h, dark), func(t *testing.T) {
				// A long single-line custom prompt used to push the source panel
				// completely below the viewport, just like the built-in template.
				requirements := strings.ReplaceAll(prompts.DoubaoWashPrompt, "\n", " ") + strings.Repeat(" 保留用户补充要求。", 80)
				app, st := startTestUIWithSetup(t, true, size.w, size.h, func(st *store.Store) {
					if dark {
						st.Set("ui_theme", "dark")
					}
					_, err := st.SaveActive(store.Session{Page: store.PageWash, Prompts: requirements,
						Sources: []store.SessionSource{{Path: source}}}, nil)
					if err != nil {
						t.Fatal(err)
					}
				})
				thumb := app.Find("id:wb-source-thumb-0").Bounds()
				if thumb.Origin.Y+thumb.Size.Height > size.h || thumb.Size.Height < 40 {
					t.Fatalf("source is not visible with a long prompt: %+v", thumb)
				}
				app.Press("id:wb-prompt-toggle")
				editor := app.Find("id:wb-prompt-editor").Bounds()
				if editor.Size.Height > 180 || editor.Size.Height < 120 {
					t.Fatalf("expanded prompt height must stay bounded: %+v", editor)
				}
				app.Press("id:wb-prompt-toggle")
				app.Find("id:wb-prompt-summary")
				sess, _, found, err := st.ActiveSession(store.PageWash)
				if err != nil || !found || sess.Prompts != requirements {
					t.Fatal("opening/closing prompt editor changed the saved requirements")
				}
			})
		}
	}
}

func TestCompletedWashResultsCanCollapse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "art.png")
	writeTestPNG(t, path)
	app, _ := startTestUIWithSetup(t, true, 1160, 740, func(st *store.Store) {
		_, err := st.SaveActive(store.Session{Page: store.PageWash, Prompts: "重绘为纯画", Progress: 1,
			Sources: []store.SessionSource{{Path: path, Washed: true, Output: path}}}, []store.SessionTask{
			{Ord: 1, Platform: "豆包", Source: path, Status: string(tasks.StatusSuccess), Outputs: path},
		})
		if err != nil {
			t.Fatal(err)
		}
	})
	app.Find("id:result-tile-image-1")
	app.Press("text:收起已完成")
	if app.Exists("id:result-tile-image-1") {
		t.Fatal("completed images stayed mounted after collapse")
	}
	app.Press("text:展开已完成")
	app.Find("id:result-tile-image-1")
}

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := range 32 {
		for x := range 32 {
			img.SetRGBA(x, y, color.RGBA{R: 37, G: 99, B: 235, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func assertInsideDialog(t *testing.T, name string, outer, inner core.Bounds) {
	t.Helper()
	const eps = 0.5
	if outer.Size.Width <= 0 || outer.Size.Height <= 0 {
		t.Fatalf("%s surface has non-positive size %+v", name, outer)
	}
	if inner.Size.Width <= 0 || inner.Size.Height <= 0 {
		t.Fatalf("%s child has non-positive size %+v", name, inner)
	}
	if inner.Origin.X < outer.Origin.X-eps ||
		inner.Origin.X+inner.Size.Width > outer.Origin.X+outer.Size.Width+eps ||
		inner.Origin.Y < outer.Origin.Y-eps ||
		inner.Origin.Y+inner.Size.Height > outer.Origin.Y+outer.Size.Height+eps {
		t.Fatalf("%s child %+v overflows surface %+v", name, inner, outer)
	}
}

func TestDialogsFitBoundsAndThumbnailsRender(t *testing.T) {
	srcDir := t.TempDir()
	var sources []store.SessionSource
	for i := range 7 {
		p := filepath.Join(srcDir, fmt.Sprintf("sample_%02d.png", i+1))
		writeTestPNG(t, p)
		sources = append(sources, store.SessionSource{Path: p})
	}

	for _, sz := range []struct {
		w, h core.Points
	}{
		{1280, 820},
		{640, 540},
	} {
		t.Run(fmt.Sprintf("%gx%g", sz.w, sz.h), func(t *testing.T) {
			app, _ := startTestUIWithSetup(t, true, sz.w, sz.h, func(st *store.Store) {
				if _, err := st.SaveActive(store.Session{
					Page:    store.PageWash,
					Sources: sources,
				}, nil); err != nil {
					t.Fatal(err)
				}
			})

			thumbNode := app.Find("id:wb-source-thumb-0")
			thumb := thumbNode.Bounds()
			if thumb.Size.Width < 40 || thumb.Size.Height < 40 {
				t.Fatalf("workbench source thumb collapsed: %+v", thumb)
			}
			if thumbNode.Image.Source.URI == "" || thumbNode.Image.ResizeMode != core.Contain {
				t.Fatalf("workbench source thumb image config unexpected: %+v", thumbNode.Image)
			}

			app.Press("text:管理全部 7 张素材")
			dlg := app.Find("id:gallery-dialog").Bounds()
			if sz.w >= 1280 && dlg.Size.Width <= 500 {
				t.Fatalf("gallery-dialog stayed too narrow (%+v); expected > 500pt", dlg)
			}
			if dlg.Origin.X+dlg.Size.Width > sz.w+0.5 || dlg.Origin.Y+dlg.Size.Height > sz.h+0.5 {
				t.Fatalf("gallery-dialog %+v exceeds window %gx%g", dlg, sz.w, sz.h)
			}

			body := app.Find("id:gallery-dialog-body").Bounds()
			if body.Size.Height < 200 {
				t.Fatalf("gallery-dialog-body height collapsed: %+v", body)
			}
			assertInsideDialog(t, "gallery-body", dlg, body)
			assertInsideDialog(t, "gallery-done-btn", dlg, app.Find("id:gallery-dialog >> text:完成").Bounds())
			removeBtns := app.Query("id:gallery-dialog >> text:移除")
			if len(removeBtns) == 0 {
				t.Fatal("expected at least one gallery remove button")
			}
			assertInsideDialog(t, "gallery-remove-btn", dlg, removeBtns[0].Bounds())

			gThumbNode := app.Find("id:gallery-thumb-0")
			gThumb := gThumbNode.Bounds()
			if gThumb.Size.Width < 44 || gThumb.Size.Height < 44 {
				t.Fatalf("gallery thumb collapsed: %+v", gThumb)
			}
			if gThumbNode.Image.Source.URI == "" || gThumbNode.Image.ResizeMode != core.Cover {
				t.Fatalf("gallery thumb image config unexpected: %+v", gThumbNode.Image)
			}
			app.Press("id:gallery-dialog >> text:完成")
			if app.Exists("id:gallery-dialog") {
				t.Fatal("gallery dialog remained visible after pressing 完成")
			}

			app.Press("id:wb-history-btn")
			histDlg := app.Find("id:history-dialog").Bounds()
			if sz.w >= 1280 && histDlg.Size.Width <= 500 {
				t.Fatalf("history-dialog stayed too narrow (%+v)", histDlg)
			}
			assertInsideDialog(t, "history-body", histDlg, app.Find("id:history-dialog-body").Bounds())
			assertInsideDialog(t, "history-new-btn", histDlg, app.Find("id:history-dialog >> text:新建空白记录").Bounds())
			// Dismiss history dialog cleanly
			app.Press("id:history-dialog >> text:新建空白记录")
			_ = histDlg

			wizApp, _ := startTestUIWithSize(t, false, sz.w, sz.h)
			wizSurf := wizApp.Find("id:wizard-dialog-surface").Bounds()
			if sz.w >= 1280 && wizSurf.Size.Width <= 550 {
				t.Fatalf("wizard-dialog-surface stayed too narrow (%+v)", wizSurf)
			}
			assertInsideDialog(t, "wizard-body", wizSurf, wizApp.Find("id:wizard-dialog").Bounds())
			wizApp.Press("text:跳过配置")
		})
	}
}

func TestLogPanelMatchesOriginalFloatingConsole(t *testing.T) {
	for _, sz := range []struct {
		w, h core.Points
	}{
		{1280, 820},
		{640, 540},
	} {
		t.Run(fmt.Sprintf("%gx%g", sz.w, sz.h), func(t *testing.T) {
			app, _ := startTestUIWithSize(t, true, sz.w, sz.h)

			app.Press("id:topbar-log-btn")
			drawer := app.Find("id:log-drawer")
			b := drawer.Bounds()
			if drawer.Style.Background != core.RGB(0x0B0E14) {
				t.Fatalf("log-drawer background = %#v, want %#v", drawer.Style.Background, core.RGB(0x0B0E14))
			}
			if b.Origin.X < -0.5 || b.Origin.Y < 41.5 || b.Origin.X+b.Size.Width > sz.w+0.5 || b.Origin.Y+b.Size.Height > sz.h+0.5 {
				t.Fatalf("log-drawer bounds %+v out of window %gx%g", b, sz.w, sz.h)
			}
			if sz.w >= 1280 {
				if b.Size.Width < 860 || b.Size.Width > 885 || b.Size.Height < 500 || b.Size.Height > 525 || b.Origin.X <= 300 {
					t.Fatalf("log-drawer 1280x820 geometry = %+v, want ~880x520 anchored top-right", b)
				}
			}

			assertHasTexts(t, app, "运行日志", "实时接收", "暂停跟随", "导出", "复制", "清空", "关闭", "全部来源", "实时跟随")

			app.Press("id:log-drawer >> text:暂停跟随")
			assertHasTexts(t, app, "继续跟随", "已暂停", "已暂停跟随")

			app.Press("label:关闭运行日志")
			if app.Exists("id:log-drawer") {
				t.Fatal("log drawer remained visible after closing")
			}
		})
	}
}

func TestWorkbenchInlineAccountManagement(t *testing.T) {
	app, st := startTestUI(t, true)
	rows := st.Browsers()
	if len(rows) != 1 {
		t.Fatalf("expected 1 seeded browser row, got %d", len(rows))
	}
	id := rows[0].ID

	assertHasTexts(t, app, "账号 · 豆包", "测试账号", "打开登录", "可视", "新建账号", "管理", "全部停用")

	// Toggle disabled switch in-place on Workbench
	toggleSel := fmt.Sprintf("id:lane-toggle-%d", id)
	app.Press(toggleSel)
	app.WaitFor("text:已禁用", 2*time.Second)
	if !st.Browsers()[0].Disabled {
		t.Fatal("expected browser row to be marked Disabled after toggle")
	}

	app.Press(toggleSel)
	app.WaitFor("text:需要登录", 2*time.Second)
	if st.Browsers()[0].Disabled {
		t.Fatal("expected browser row to be re-enabled after second toggle")
	}

	// Bulk disable / enable all accounts on current platform
	app.Press("id:lanes-disable-all")
	app.WaitFor("id:lanes-enable-all", 2*time.Second)
	if !st.Browsers()[0].Disabled {
		t.Fatal("expected 全部停用 to disable all 豆包 accounts")
	}
	app.Press("id:lanes-enable-all")
	app.WaitFor("id:lanes-disable-all", 2*time.Second)
	if st.Browsers()[0].Disabled {
		t.Fatal("expected 全部启用 to re-enable all 豆包 accounts")
	}

	// Collapsed quick-toggle pill bar lets user toggle account without expanding cards
	app.Press("label:折叠账号车道")
	pillSel := fmt.Sprintf("id:lane-pill-%d", id)
	app.Find(pillSel)
	app.Press(pillSel)
	app.WaitFor("id:lanes-enable-all", 2*time.Second)
	if !st.Browsers()[0].Disabled {
		t.Fatal("expected collapsed pill press to disable account")
	}
	app.Press(pillSel)
	app.WaitFor("id:lanes-disable-all", 2*time.Second)
	if st.Browsers()[0].Disabled {
		t.Fatal("expected second collapsed pill press to re-enable account")
	}
	app.Press("label:折叠账号车道")

	// Open and cancel inline quick-add bar
	app.Press("id:wb-stage >> text:新建账号")
	assertHasTexts(t, app, "创建并登录")
	app.Press("id:wb-stage >> text:取消")
	if app.Exists("id:wb-stage >> text:创建并登录") {
		t.Fatal("inline quick-add bar should close after 取消")
	}

	// Switch platform to 千问: only 千问 empty state shows, 豆包 account is hidden
	app.Press("id:wb-platform-tabs >> text:千问")
	assertHasTexts(t, app, "当前平台「千问」暂无可用账号", "新建 千问 账号")
	if app.Exists("id:wb-stage >> text:测试账号") {
		t.Fatal("豆包 account should not show under 千问 platform lanes")
	}

	// Jump to full Browser page via 管理 button
	app.Press("id:wb-stage >> text:管理")
	if got := app.Find("id:page-title").Text; got != "浏览器" {
		t.Fatalf("after pressing 管理: page-title = %q, want 浏览器", got)
	}
}

func TestWorkbenchSaaSResultGalleryAndComposerLayout(t *testing.T) {
	dir := t.TempDir()
	var sources []store.SessionSource
	var sessionTasks []store.SessionTask
	for i := range 6 {
		src := filepath.Join(dir, fmt.Sprintf("src_%02d.png", i+1))
		out := filepath.Join(dir, fmt.Sprintf("out_%02d.png", i+1))
		writeTestPNG(t, src)
		writeTestPNG(t, out)
		sources = append(sources, store.SessionSource{Path: src, Washed: true, Output: out})
		sessionTasks = append(sessionTasks, store.SessionTask{
			Ord:      i + 1,
			Platform: "豆包",
			Source:   src,
			Status:   string(tasks.StatusSuccess),
			Outputs:  out,
		})
	}

	type sizeMetrics struct {
		composerW float64
		cardW     float64
		cardH     float64
	}
	var metrics []sizeMetrics

	for _, sz := range []struct {
		w, h core.Points
	}{
		{920, 640},
		{1280, 820},
		{1600, 960},
	} {
		t.Run(fmt.Sprintf("%gx%g", sz.w, sz.h), func(t *testing.T) {
			app, _ := startTestUIWithSetup(t, true, sz.w, sz.h, func(st *store.Store) {
				if _, err := st.SaveActive(store.Session{
					Page:     store.PageWash,
					Prompts:  "重绘为纯画",
					Progress: 1,
					Sources:  sources,
				}, sessionTasks); err != nil {
					t.Fatal(err)
				}
			})

			// 1. Actual Select nodes (not just wrappers) must stretch cleanly inside composer without 200pt overflow.
			composer := app.Find("id:wb-composer").Bounds()
			if app.Exists("id:wb-wash-style-select") {
				styleSel := app.Find("id:wb-wash-style-select").Bounds()
				assertInsideDialog(t, "wb-wash-style-select", composer, styleSel)
				if styleSel.Size.Width < composer.Size.Width-56 {
					t.Fatalf("wb-wash-style-select width=%v too narrow inside composer width=%v", styleSel.Size.Width, composer.Size.Width)
				}
			}
			presetSel := app.Find("id:wb-output-preset-select").Bounds()
			ratioSel := app.Find("id:wb-output-ratio-select").Bounds()
			assertInsideDialog(t, "wb-output-preset-select", composer, presetSel)
			assertInsideDialog(t, "wb-output-ratio-select", composer, ratioSel)
			if presetSel.Origin.X+presetSel.Size.Width > ratioSel.Origin.X+0.5 {
				t.Fatalf("output preset select (%+v) overlaps output ratio select (%+v)", presetSel, ratioSel)
			}
			if presetSel.Size.Width < 100 || presetSel.Size.Width > composer.Size.Width/2 || ratioSel.Size.Width < 100 || ratioSel.Size.Width > composer.Size.Width/2 {
				t.Fatalf("expected 1fr/1fr output select widths in [100, %v], got preset=%+v ratio=%+v", composer.Size.Width/2, presetSel, ratioSel)
			}

			// 2. Gallery cards render cleanly in a fixed 3-column grid that scales with window size.
			gallery := app.Find("id:wb-result-gallery")
			if gallery.Bounds().Size.Width <= 0 {
				t.Fatalf("wb-result-gallery collapsed: %+v", gallery.Bounds())
			}
			var cards []core.Bounds
			for i := 1; i <= 6; i++ {
				img := app.Find(fmt.Sprintf("id:result-tile-image-%d", i)).Bounds()
				if img.Size.Width < 40 || img.Size.Height < 40 {
					t.Fatalf("result tile image %d collapsed: %+v", i, img)
				}
				card := app.Find(fmt.Sprintf("id:result-tile-card-%d", i)).Bounds()
				if card.Size.Width < 110 || card.Size.Height < 130 {
					t.Fatalf("result tile card %d collapsed: %+v", i, card)
				}
				for j, prev := range cards {
					overlapX := card.Origin.X < prev.Origin.X+prev.Size.Width-0.5 && card.Origin.X+card.Size.Width > prev.Origin.X+0.5
					overlapY := card.Origin.Y < prev.Origin.Y+prev.Size.Height-0.5 && card.Origin.Y+card.Size.Height > prev.Origin.Y+0.5
					if overlapX && overlapY {
						t.Fatalf("result tile card %d (%+v) overlaps card %d (%+v)", i, card, j+1, prev)
					}
				}
				cards = append(cards, card)
				app.Find(fmt.Sprintf("id:result-tile-menu-%d", i))
			}
			// Fixed 3 columns: cards 1, 2, 3 on row 1; card 4 starts row 2 aligned under card 1.
			if cards[1].Origin.Y != cards[0].Origin.Y || cards[2].Origin.Y != cards[0].Origin.Y ||
				cards[1].Origin.X <= cards[0].Origin.X+cards[0].Size.Width-1 ||
				cards[2].Origin.X <= cards[1].Origin.X+cards[1].Size.Width-1 {
				t.Fatalf("expected fixed 3-column first row, got %+v %+v %+v", cards[0], cards[1], cards[2])
			}
			if cards[3].Origin.Y <= cards[0].Origin.Y || cards[3].Origin.X < cards[0].Origin.X-1 || cards[3].Origin.X > cards[0].Origin.X+1 {
				t.Fatalf("expected card 4 on row 2 aligned with card 1, got card1=%+v card4=%+v", cards[0], cards[3])
			}
			metrics = append(metrics, sizeMetrics{
				composerW: float64(composer.Size.Width),
				cardW:     float64(cards[0].Size.Width),
				cardH:     float64(cards[0].Size.Height),
			})

			// 3. Zero repetitive inline text buttons (定位/打开/替换) or redundant 完成 stamp on completed cards.
			for _, unwanted := range []string{"定位", "打开", "替换", "完成"} {
				if matches := app.Query(`id:wb-result-gallery >> text:"` + unwanted + `"`); len(matches) > 0 {
					t.Fatalf("unexpected inline button/stamp %q mounted inside result gallery (%d found)", unwanted, len(matches))
				}
			}

			// 4. Single quiet menu trigger opens full card actions on demand.
			app.Press("id:result-tile-menu-1")
			app.WaitFor("role:menuitem 大图预览", 2*time.Second)
			assertHasTexts(t, app, "大图预览", "系统默认打开", "在文件夹中显示", "替换原图")
			app.Key("escape")
			app.WaitForGone("role:menuitem 大图预览", 2*time.Second)

			// 5. Clicking artwork opens enhanced Lightbox with Prev/Next navigation and actions.
			app.Press("id:result-tile-image-1")
			app.WaitFor("id:lightbox-dialog", 2*time.Second)
			assertHasTexts(t, app, "1 / 6", "在文件夹中显示", "系统默认打开", "替换原图")
			app.Find("id:lightbox-replace-btn")

			app.Press("label:下一张")
			app.WaitFor("text:2 / 6", 2*time.Second)
			app.Press("label:上一张")
			app.WaitFor("text:1 / 6", 2*time.Second)

			app.Press("id:lightbox-dialog >> text:关闭")
			app.WaitForGone("id:lightbox-dialog", 2*time.Second)
		})
	}

	if len(metrics) != 3 {
		t.Fatalf("expected 3 size measurements, got %d", len(metrics))
	}
	if !(metrics[0].composerW < metrics[1].composerW && metrics[1].composerW < metrics[2].composerW) {
		t.Fatalf("expected composer width to stretch smoothly with window size: %+v", metrics)
	}
	if !(metrics[0].cardW < metrics[1].cardW && metrics[1].cardW < metrics[2].cardW) {
		t.Fatalf("expected gallery card width to stretch smoothly with window size: %+v", metrics)
	}
	if !(metrics[0].cardH < metrics[1].cardH && metrics[1].cardH < metrics[2].cardH) {
		t.Fatalf("expected gallery card height (via aspectRatio) to stretch smoothly with window size: %+v", metrics)
	}
}

func TestWorkbenchEngineeringPipelineActiveAndPending(t *testing.T) {
	dir := t.TempDir()
	srcs := make([]string, 5)
	for i := range srcs {
		srcs[i] = filepath.Join(dir, fmt.Sprintf("src_%02d.png", i+1))
		writeTestPNG(t, srcs[i])
	}
	out1 := filepath.Join(dir, "out_01.png")
	writeTestPNG(t, out1)

	sources := []store.SessionSource{
		{Path: srcs[0], Washed: true, Output: out1},
		{Path: srcs[1]},
		{Path: srcs[2]},
		{Path: srcs[3]},
		{Path: srcs[4]},
	}
	sessionTasks := []store.SessionTask{
		{Ord: 1, Platform: "豆包", AccountName: "豆包1", Source: srcs[0], Status: string(tasks.StatusSuccess), Outputs: out1},
		{Ord: 2, Platform: "豆包", AccountName: "豆包1", Source: srcs[1], Status: string(tasks.StatusRunning)},
		{Ord: 3, Platform: "豆包", Source: srcs[2], Status: string(tasks.StatusPending)},
		{Ord: 4, Platform: "豆包", Source: srcs[3], Status: string(tasks.StatusPending)},
		{Ord: 5, Platform: "豆包", Source: srcs[4], Status: string(tasks.StatusPending)},
	}

	app, _ := startTestUIWithSetup(t, true, 1280, 820, func(st *store.Store) {
		if _, err := st.SaveActive(store.Session{
			Page:     store.PageWash,
			Prompts:  "重绘为纯画",
			Progress: 0.2,
			Sources:  sources,
		}, sessionTasks); err != nil {
			t.Fatal(err)
		}
	})

	app.WaitFor("id:wb-active-pipeline", 2*time.Second)
	app.WaitFor("id:wb-pending-queue", 2*time.Second)

	// 1. Active processing stage shows conveyor track, scan overlay, station and #02 order badge.
	conveyor := app.Find("id:wb-conveyor-track").Bounds()
	if conveyor.Size.Width <= 100 || conveyor.Size.Height < 16 {
		t.Fatalf("wb-conveyor-track collapsed: %+v", conveyor)
	}
	activePipe := app.Find("id:wb-active-pipeline")
	activeCard := app.Find("id:result-tile-card-2")
	assertInsideDialog(t, "result-tile-card-2", activePipe.Bounds(), activeCard.Bounds())
	scanOverlay := app.Find("id:active-scan-overlay-2").Bounds()
	assertInsideDialog(t, "active-scan-overlay-2", activeCard.Bounds(), scanOverlay)
	assertHasTexts(t, app, "正在洗图", "工位 · 豆包1")
	hasOrder02 := false
	for _, txt := range app.Texts() {
		if strings.Contains(txt, "#02") {
			hasOrder02 = true
			break
		}
	}
	if !hasOrder02 {
		t.Fatalf("expected active work-order badge containing #02 in texts: %v", app.Texts())
	}

	// 2. Waiting queue renders pending items #03, #04, #05 cleanly on the same horizontal row.
	pendingPipe := app.Find("id:wb-pending-queue").Bounds()
	var pendingBounds []core.Bounds
	for _, ord := range []int{3, 4, 5} {
		b := app.Find(fmt.Sprintf("id:pending-tile-%d", ord)).Bounds()
		if b.Size.Width < 40 || b.Size.Height < 50 {
			t.Fatalf("pending-tile-%d collapsed: %+v", ord, b)
		}
		assertInsideDialog(t, fmt.Sprintf("pending-tile-%d", ord), pendingPipe, b)
		pendingBounds = append(pendingBounds, b)
	}
	if pendingBounds[1].Origin.Y != pendingBounds[0].Origin.Y ||
		pendingBounds[2].Origin.Y != pendingBounds[0].Origin.Y ||
		pendingBounds[1].Origin.X <= pendingBounds[0].Origin.X+pendingBounds[0].Size.Width-1 ||
		pendingBounds[2].Origin.X <= pendingBounds[1].Origin.X+pendingBounds[1].Size.Width-1 {
		t.Fatalf("expected pending tickets #03..#05 on one non-overlapping horizontal row, got %+v", pendingBounds)
	}
	assertHasTexts(t, app, "#03", "#04", "#05")
	if matches := app.Query(`id:wb-pending-queue >> text:"等待"`); len(matches) < 3 {
		t.Fatalf("expected 3 等待 tags in wb-pending-queue, got %d", len(matches))
	}

	// 3. Left material pool reflects synced live statuses: 已洗 / 洗图中 / 等待.
	if matches := app.Query(`id:wb-composer >> text:"已洗"`); len(matches) == 0 {
		t.Fatal("expected 已洗 badge inside left source pool")
	}
	if matches := app.Query(`id:wb-composer >> text:"洗图中"`); len(matches) == 0 {
		t.Fatal("expected 洗图中 badge inside left source pool")
	}
	if matches := app.Query(`id:wb-composer >> text:"等待"`); len(matches) == 0 {
		t.Fatal("expected 等待 badge inside left source pool")
	}

	// 4. Zero repetitive inline text buttons or redundant 完成 stamp inside gallery.
	for _, unwanted := range []string{"定位", "打开", "替换", "完成"} {
		if matches := app.Query(`id:wb-result-gallery >> text:"` + unwanted + `"`); len(matches) > 0 {
			t.Fatalf("unexpected inline button/stamp %q mounted inside result gallery (%d found)", unwanted, len(matches))
		}
	}
}

func TestWorkbenchClearWashedSourcesButton(t *testing.T) {
	dir := t.TempDir()
	srcs := make([]string, 4)
	for i := range srcs {
		srcs[i] = filepath.Join(dir, fmt.Sprintf("wash_%02d.png", i+1))
		writeTestPNG(t, srcs[i])
	}
	sources := []store.SessionSource{
		{Path: srcs[0], Washed: true, Output: srcs[0]},
		{Path: srcs[1], Washed: true, Output: srcs[1]},
		{Path: srcs[2]},
		{Path: srcs[3]},
	}

	app, st := startTestUIWithSetup(t, true, 1280, 820, func(st *store.Store) {
		if _, err := st.SaveActive(store.Session{
			Page:    store.PageWash,
			Prompts: "重绘为纯画",
			Sources: sources,
		}, nil); err != nil {
			t.Fatal(err)
		}
	})

	app.WaitFor("id:wb-washed-bar", 2*time.Second)
	app.Find("id:wb-clear-washed-btn")
	assertHasTexts(t, app, "已洗 2 / 4", "参考素材 (4)")

	app.Press("id:wb-clear-washed-btn")
	app.WaitForGone("id:wb-washed-bar", 2*time.Second)
	assertHasTexts(t, app, "参考素材 (2)")

	sess, _, found, err := st.ActiveSession(store.PageWash)
	if err != nil || !found {
		t.Fatalf("expected active wash session persisted: found=%v err=%v", found, err)
	}
	if len(sess.Sources) != 2 || sess.Sources[0].Path != srcs[2] || sess.Sources[1].Path != srcs[3] {
		t.Fatalf("unexpected persisted sources after clear washed: %+v", sess.Sources)
	}
}
