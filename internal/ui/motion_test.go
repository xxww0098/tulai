package ui

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/tasks"
	"github.com/xxww0098/tulai/internal/wizard"
)

func TestWizardStepSlidesInFromTheSide(t *testing.T) {
	a := &App{wizardOn: true, wizardStep: wizard.StepPlatform, wizardChoice: "豆包", platDoubao: true}
	a.wizardPlan = wizard.BrowserPlan{Title: "将使用本机 Chrome", CanNext: true}
	tt := ui.NewTester(a.view, 1280, 900)
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	early, ok := tt.Find("准备浏览器")
	if !ok {
		t.Fatal("platform step did not show")
	}
	if darkPixels(tt, 440, 60, 950, 300) < 100 {
		t.Fatal("切换向导步骤时内容不应先消失")
	}
	tt.Advance(motionUI + 40*time.Millisecond)
	late, ok := tt.Find("准备浏览器")
	if !ok {
		t.Fatal("platform step disappeared")
	}
	if early.X <= late.X+2 || early.X > late.X+8 {
		t.Fatalf("forward step should arrive gently from the right: early=%v late=%v", early.X, late.X)
	}
	if tt.FramePending() {
		t.Fatal("step transition kept asking for frames")
	}
}

func TestWizardStepStaysPutWhenMotionIsReduced(t *testing.T) {
	a := &App{wizardOn: true, wizardStep: wizard.StepPlatform, wizardChoice: "豆包", platDoubao: true}
	a.wizardPlan = wizard.BrowserPlan{Title: "将使用本机 Chrome", CanNext: true}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	early, ok := tt.Find("准备浏览器")
	if !ok {
		t.Fatal("platform step did not show")
	}
	tt.Advance(motionUI + 40*time.Millisecond)
	late, ok := tt.Find("准备浏览器")
	if !ok {
		t.Fatal("platform step disappeared")
	}
	if delta := early.X - late.X; delta > 0.5 || delta < -0.5 {
		t.Fatalf("reduced motion still slid the step: early=%v late=%v", early.X, late.X)
	}
}

func TestWorkspaceModeSwitchStartsVisible(t *testing.T) {
	a := &App{platformAuto: true}
	tt := ui.NewTester(a.view, 1024, 700)
	tt.Advance(motionUI + 40*time.Millisecond)
	if err := tt.Click("批量洗图"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("洗图指令") {
		t.Fatal("洗图页面没有出现")
	}
	if darkPixels(tt, 80, 80, 400, 440) < 100 {
		t.Fatal("切换后的第一帧不应显示空白页面")
	}
	tt.Advance(motionUI + 40*time.Millisecond)
	if err := tt.Click("自由生图"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("提示词内容") {
		t.Fatal("生图页面没有出现")
	}
	if darkPixels(tt, 80, 80, 400, 440) < 100 {
		t.Fatal("切回生图的第一帧不应显示空白页面")
	}
}

func TestWizardTransitionDoesNotOverlayPreviousPage(t *testing.T) {
	a := &App{platformAuto: true, tab: pageWash, statePage: pageWash, prompts: DefaultWashPrompt}
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Advance(motionUI + 40*time.Millisecond)
	if err := tt.Click("设置向导"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(32 * time.Millisecond)
	if !tt.HasText("选择平台") {
		t.Fatal("设置向导没有出现")
	}
	if darkPixels(tt, 75, 330, 220, 700) > 20 {
		t.Fatal("向导的空白侧栏上仍有上一页的内容")
	}
}

func darkPixels(tt *ui.Tester, left, top, right, bottom int) int {
	img, count := tt.Image(), 0
	for y := top; y < bottom; y++ {
		for x := left; x < right; x++ {
			p := img.RGBAAt(x, y)
			if p.R < 160 && p.G < 160 && p.B < 160 {
				count++
			}
		}
	}
	return count
}

func TestPageSwitchReducedMotionAndRapidNavigation(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprintf("reduced=%v", reduced), func(t *testing.T) {
			a := &App{platformAuto: true, prompts: "保留提示词"}
			tt := ui.NewTester(a.view, 1024, 700)
			tt.SetPreferences(ui.Preferences{ReduceMotion: reduced, TextScale: 1})
			tt.Advance(motionUI + 40*time.Millisecond)
			if err := tt.Click("批量洗图"); err != nil {
				t.Fatal(err)
			}
			early, ok := tt.Find("洗图指令")
			if !ok {
				t.Fatal("洗图页面没有出现")
			}
			tt.Advance(48 * time.Millisecond)
			if reduced {
				later, _ := tt.Find("洗图指令")
				if dy := early.Y - later.Y; dy < -0.5 || dy > 0.5 {
					t.Fatalf("减少动态效果时页面仍在移动: early=%+v later=%+v", early, later)
				}
			}
			for _, page := range []string{"浏览器", "图片生成", "自由生图"} {
				if err := tt.Click(page); err != nil {
					t.Fatal(err)
				}
				tt.Advance(48 * time.Millisecond)
			}
			tt.Advance(motionUI + 40*time.Millisecond)
			if a.tab != pageHome || a.prompts != "保留提示词" || !tt.HasText("提示词内容") || tt.HasText("洗图指令") {
				t.Fatalf("快速切换后页面或输入错误: tab=%d prompts=%q", a.tab, a.prompts)
			}
			if tt.FramePending() {
				t.Fatal("页面切换结束后仍在请求动画帧")
			}
		})
	}
}

func TestThumbnailsSlideIntoPlace(t *testing.T) {
	a := &App{}
	tt := ui.NewTester(a.view, 1000, 720)
	a.sources = []sourceImage{{Path: "left.png", Hash: "l"}, {Path: "right.png", Hash: "r"}}
	tt.Frame()
	tt.Advance(motionUI + 40*time.Millisecond)
	before, ok := tt.Find("right.png")
	if !ok {
		t.Fatal("thumbnail missing")
	}
	a.sources = a.sources[1:]
	tt.Frame()
	mid, ok := tt.Find("right.png")
	if !ok {
		t.Fatal("thumbnail disappeared as the other left")
	}
	tt.Advance(motionUI + 40*time.Millisecond)
	after, ok := tt.Find("right.png")
	if !ok {
		t.Fatal("thumbnail missing after the move")
	}
	if mid.X < after.X+4 {
		t.Fatalf("remaining thumbnail should slide from where it was: before=%v mid=%v after=%v", before.X, mid.X, after.X)
	}
	if _, still := tt.Find("left.png"); still {
		t.Fatal("removed thumbnail stayed")
	}
}

func TestUpdateBannerSlidesDownAndStaysClickable(t *testing.T) {
	a := &App{}
	tt := ui.NewTester(a.view, 1280, 840)
	a.updateNotice = "9.9.9"
	tt.Frame()
	early, ok := tt.Find("发现新版本 9.9.9")
	if !ok {
		t.Fatal("banner missing")
	}
	// 刚出现的那一帧还停在起点。过一小段时间再点，和人眼看到按钮再去点一样。
	tt.Advance(80 * time.Millisecond)
	if err := tt.Click("稍后"); err != nil {
		t.Fatal(err)
	}
	if a.updateNotice != "" {
		t.Fatal("banner could not be dismissed while it was moving")
	}
	a.updateNotice = "9.9.9"
	tt.Frame()
	tt.Advance(motionUI + 40*time.Millisecond)
	late, ok := tt.Find("发现新版本 9.9.9")
	if !ok {
		t.Fatal("banner missing after the slide")
	}
	if early.Y > late.Y-4 {
		t.Fatalf("banner should drop in from above: early=%v late=%v", early.Y, late.Y)
	}
}

func TestPromptSelectChoosesWhileOpening(t *testing.T) {
	a := &App{tab: pagePrompts}
	tt := ui.NewTester(a.view, 1280, 840)
	if err := tt.Click("编辑 默认洗图"); err != nil {
		t.Fatal(err)
	}
	if a.promptEditChoice != "默认洗图" || a.promptEditPick == "" {
		t.Fatalf("choice=%q pick=%q", a.promptEditChoice, a.promptEditPick)
	}
}

func TestManyGeneratingCardsStayReadable(t *testing.T) {
	var queue []tasks.Task
	for i := 0; i < 40; i++ {
		queue = append(queue, tasks.Task{
			Index: i, Platform: "豆包", Status: tasks.StatusRunning,
			Images: []string{fmt.Sprintf("img-%02d.png", i)}, Prompt: "水彩",
		})
	}
	queue[0].Status = tasks.StatusPending
	queue[1].Status = tasks.StatusSuccess
	queue[1].Results = []string{"out.png"}
	queue[2].Status = tasks.StatusFailed
	queue[2].Error = "额度用尽"
	a := &App{queue: queue}
	tt := ui.NewTester(a.view, 1280, 2800)
	for _, label := range []string{"排队中", "进行中", "完成", "失败", "账号额度已用完", "换账号重试"} {
		if !tt.HasText(label) {
			t.Fatalf("missing %q", label)
		}
	}
	if _, ok := tt.Find("img-39.png"); !ok {
		t.Fatal("last card was not built")
	}
}

func TestExportMotionFrames(t *testing.T) {
	dir := os.Getenv("MOTION_FRAMES")
	if dir == "" {
		t.Skip("set MOTION_FRAMES to write frames")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	save := func(name string, tt *ui.Tester) {
		t.Helper()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}

	setup := &App{wizardOn: true, wizardStep: wizard.StepPlatform, wizardChoice: "豆包", platDoubao: true}
	setup.wizardPlan = wizard.BrowserPlan{
		Title: "将使用本机 Chrome", Detail: "本机已安装，下一步即可", CanNext: true,
	}
	tt := ui.NewTester(setup.view, 1280, 900)
	save("01-wizard-platform", tt)
	if err := tt.Click("千问"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(motionFast)
	save("02-wizard-platform-picked", tt)
	if err := tt.Click("下一步"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= 16; i++ {
		if i > 0 {
			tt.Advance(16 * time.Millisecond)
		}
		save(fmt.Sprintf("seq-%02d", i), tt)
		if i == 0 || i == 5 || i == 10 || i == 16 {
			save(fmt.Sprintf("03-wizard-step-%d", i), tt)
		}
	}

	work := &App{
		platDoubao: true, platformAuto: true,
		sources: []sourceImage{{Path: "风景.png", Hash: "a"}, {Path: "人像.png", Hash: "b"}},
		queue: []tasks.Task{
			{Index: 1, Platform: "豆包", Status: tasks.StatusPending, Images: []string{"风景.png"}, Prompt: "水彩"},
			{Index: 2, Platform: "豆包", Status: tasks.StatusRunning, Images: []string{"人像.png"}, Prompt: "水彩"},
			{Index: 3, Platform: "千问", Status: tasks.StatusFailed, Images: []string{"静物.png"}, Prompt: "水彩", Error: "额度用尽"},
			{Index: 4, Platform: "豆包", Status: tasks.StatusSuccess, Prompt: "文生图一只猫", Results: nil},
		},
	}
	wt := ui.NewTester(work.view, 1280, 900)
	wt.Advance(80 * time.Millisecond)
	save("04-workbench", wt)
	work.tab = pagePrompts
	wt.Frame()
	wt.Advance(motionUI)
	if err := wt.Click("编辑 默认洗图"); err != nil {
		t.Fatal(err)
	}
	wt.Advance(80 * time.Millisecond)
	save("06-prompt-menu", wt)
	compact := ui.NewTester((&App{tab: pagePrompts}).view, 760, 900)
	compact.Advance(motionUI)
	save("06-prompts-compact", compact)
	darkApp := &App{tab: pagePrompts}
	dark := ui.NewTester(func(c *ui.Context) {
		theme := *c.Theme()
		theme.Dark = true
		c.SetTheme(&theme)
		darkApp.view(c)
	}, 1280, 900)
	dark.Advance(motionUI)
	save("06-prompts-dark", dark)
	work.tab = pageHome
	work.updateNotice = "0.2.0"
	work.updateNotes = "下载和安装会显示进度"
	work.updateDownloading = true
	work.updateFraction = 0.45
	work.updateProgressText = "正在下载 tulai"
	wt.Frame()
	wt.Advance(120 * time.Millisecond)
	save("05-update-banner", wt)

	pages := &App{platformAuto: true}
	pt := ui.NewTester(pages.view, 1280, 900)
	pt.Advance(300 * time.Millisecond)
	if err := pt.Click("批量洗图"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= 8; i++ {
		if i > 0 {
			pt.Advance(32 * time.Millisecond)
		}
		save(fmt.Sprintf("page-%02d", i), pt)
	}
	if err := pt.Click("设置向导"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= 8; i++ {
		if i > 0 {
			pt.Advance(32 * time.Millisecond)
		}
		save(fmt.Sprintf("screen-%02d", i), pt)
	}
}
