package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/dedup"
	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func dedupTestImage(t *testing.T, path string, w, h int, seed uint8) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{R: uint8(x * 255 / max(1, w-1)), G: uint8(y * 255 / max(1, h-1)), B: seed, A: 255}
			if x > w*2/3 && y < h/3 {
				c = color.RGBA{R: 8, G: 12, B: 20, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建 %s: %v", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("编码 %s: %v", path, err)
	}
}

func newDedupApp() *App {
	return &App{
		engine: tasks.NewEngine(nil, nil, nil, ""),
		tab:    pageDedup,
		dedup:  dedupState{level: dedupDefaultLevel},
	}
}

func waitDedup(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !a.dedup.busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("去重没有在 10 秒内结束")
}

func TestDedupPageHasThreeStepsOnly(t *testing.T) {
	a := newDedupApp()
	tt := ui.NewTester(a.view, 1280, 840)
	for _, label := range []string{"去重", "图片", "把图片拖到这里", "去重力度", "严格", "标准", "宽松", "开始去重", "更多设置", "存到"} {
		assertVisible(t, tt, label, 1280, 840)
	}
	// 汉明距离和位数不该露在界面上。
	if tt.HasText("近似阈值") || tt.HasText("63") || tt.HasText("位") {
		t.Fatalf("去重页还留着技术细节: %v", tt.Texts())
	}
	if tt.HasText("提示词内容") || tt.HasText("开始出图") {
		t.Fatalf("去重页画了生图的控件: %v", tt.Texts())
	}
}

func TestDedupLevelsMapToDistance(t *testing.T) {
	a := newDedupApp()
	for level, want := range map[int]int{dedupStrict: 4, dedupStandard: 8, dedupLoose: 16} {
		a.dedup.level = level
		if got := a.dedupThreshold(); got != want {
			t.Fatalf("档位 %d 的距离 = %d, want %d", level, got, want)
		}
	}
	a.dedup.level = -5
	if got := a.dedupThreshold(); got != dedupLevelDistance[dedupStrict] {
		t.Fatalf("负档位应夹到严格，得到 %d", got)
	}
	a.dedup.level = 99
	if got := a.dedupThreshold(); got != dedupLevelDistance[dedupLoose] {
		t.Fatalf("越界档位应夹到宽松，得到 %d", got)
	}
}

func TestDedupPickingALevelIsOneClick(t *testing.T) {
	a := newDedupApp()
	tt := ui.NewTester(a.view, 1280, 840)
	tt.Frame()
	if err := tt.Click("宽松"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.dedup.level != dedupLoose {
		t.Fatalf("level = %d, want 宽松", a.dedup.level)
	}
	if !tt.HasText(dedupLevelHint(dedupLoose)) {
		t.Fatalf("档位说明没跟着换: %v", tt.Texts())
	}
	if opts := a.dedupOptions(); opts.MaxDistance != 16 {
		t.Fatalf("Options = %+v", opts)
	}
}

func TestDedupIngestSkipsKnownPathsAndBadFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	broken := filepath.Join(dir, "broken.png")
	dedupTestImage(t, a, 40, 30, 5)
	dedupTestImage(t, b, 40, 30, 90)
	if err := os.WriteFile(broken, []byte("not an image"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{}
	app.dedupIngest([]string{a, b, a, broken, filepath.Join(dir, "missing.png")})
	if len(app.dedup.items) != 2 {
		t.Fatalf("收进 %d 张，want 2: %+v", len(app.dedup.items), app.dedup.items)
	}
	if app.dedup.note != "新加 2 张" {
		t.Fatalf("note = %q", app.dedup.note)
	}
	app.dedupIngest([]string{a})
	if len(app.dedup.items) != 2 || app.dedup.note != "没有新的图片" {
		t.Fatalf("重复导入: %+v note=%q", app.dedup.items, app.dedup.note)
	}
}

func TestDedupPageDoesNotTouchWorkbenchState(t *testing.T) {
	a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), prompts: "画一只猫"}
	a.sources = []sourceImage{{Path: "/gen/a.png"}}
	a.queue = []tasks.Task{{Index: 3}}

	a.tab = pageDedup
	a.syncPage()
	if a.statePage != statePageFor(pageHome) {
		t.Fatalf("去重页换掉了工作台状态: statePage=%d", a.statePage)
	}
	if a.prompts != "画一只猫" || len(a.sources) != 1 || a.queue[0].Index != 3 {
		t.Fatalf("去重页动了生图状态: prompts=%q sources=%v queue=%v", a.prompts, a.sources, a.queue)
	}
}

func TestDedupOutputDir(t *testing.T) {
	a := &App{outputPath: "/tmp/tulai-out"}
	want := filepath.Join("/tmp/tulai-out", "去重", time.Now().Format("2006-01-02"))
	if got := a.dedupOutDir(); got != want {
		t.Fatalf("默认输出 = %q, want %q", got, want)
	}
	a.dedup.outDir = "/tmp/mine"
	if got := a.dedupOutDir(); got != "/tmp/mine" {
		t.Fatalf("手选输出 = %q", got)
	}
}

func TestDedupRunCopiesToFolderAndShowsResult(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.png")
	b := filepath.Join(dir, "b.png")
	c := filepath.Join(dir, "c.png")
	dedupTestImage(t, a, 120, 80, 20)
	dedupTestImage(t, b, 120, 80, 20) // 和 a 一模一样
	dedupTestImage(t, c, 60, 60, 200) // 另一张
	out := filepath.Join(dir, "out")

	app := newDedupApp()
	app.dedup.level = dedupStrict
	app.dedup.outDir = out
	app.dedupIngest([]string{a, b, c})
	app.dedupStart()
	waitDedup(t, app)

	rep := app.dedup.report
	if rep.Scanned != 3 || rep.Duplicates != 1 || rep.Unique != 2 || len(rep.Groups) != 1 {
		t.Fatalf("报告 = %+v", rep)
	}
	if rep.Options.MaxDistance != 4 {
		t.Fatalf("报告里的口径 = %+v", rep.Options)
	}
	for _, name := range []string{"a.png", "c.png", dedup.ReportName} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatalf("输出缺少 %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "b.png")); err == nil {
		t.Fatal("重复的 b.png 不该被复制")
	}
	if app.dedup.err != "" {
		t.Fatalf("err = %q", app.dedup.err)
	}

	tt := ui.NewTester(app.view, 1280, 1200)
	for _, label := range []string{"结果", "打开文件夹", "开始去重"} {
		assertVisible(t, tt, label, 1280, 1200)
	}
	hit := false
	for _, txt := range tt.Texts() {
		if strings.HasPrefix(txt, "1 组重复") {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatalf("结果摘要没出现: %v", tt.Texts())
	}
}

func TestDedupChoicesSurviveRestart(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, engine: tasks.NewEngine(nil, nil, nil, "")}
	a.dedup.level = dedupLoose
	a.dedup.outDir = "/tmp/去重结果"
	a.dedup.flatColorOK = true
	a.saveSettings()

	restored := &App{store: st, dedup: dedupState{level: dedupDefaultLevel}}
	restored.loadSettings()
	if restored.dedup.level != dedupLoose {
		t.Fatalf("档位 = %d, want 宽松", restored.dedup.level)
	}
	if restored.dedup.outDir != "/tmp/去重结果" {
		t.Fatalf("输出目录 = %q", restored.dedup.outDir)
	}
	if !restored.dedup.flatColorOK {
		t.Fatal("纯色底开关没有恢复")
	}
	if opts := restored.dedupOptions(); opts.MaxDistance != 16 || !opts.IgnoreFlatColor {
		t.Fatalf("Options = %+v", opts)
	}

	// 存过「严格」也要读回来，不能被当成没存过。
	a.dedup.level = dedupStrict
	a.saveSettings()
	restored.loadSettings()
	if restored.dedup.level != dedupStrict {
		t.Fatalf("存过 0 之后读回 = %d, want 严格", restored.dedup.level)
	}
}

func TestDedupDefaultsWhenNothingSaved(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a := &App{store: st, dedup: dedupState{level: dedupDefaultLevel}}
	a.loadSettings()
	if a.dedup.level != dedupStandard {
		t.Fatalf("空库档位 = %d, want 标准", a.dedup.level)
	}
	if a.dedup.outDir != "" || a.dedup.flatColorOK {
		t.Fatalf("空库不该有输出目录或开着的护栏开关: %+v", a.dedup)
	}
}

func TestDedupAdvancedToggleIsBehindOneClick(t *testing.T) {
	a := newDedupApp()
	tt := ui.NewTester(a.view, 1280, 900)
	tt.Frame()
	// 折叠着就看不到，点开才出现。
	if tt.HasText("纯色底也算重复") {
		t.Fatalf("更多设置默认不该展开: %v", tt.Texts())
	}
	if err := tt.Click("更多设置"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	if err := tt.Click("纯色底也算重复"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	if !a.dedup.flatColorOK {
		t.Fatal("点了开关没生效")
	}
	if !tt.HasText("已关掉护栏：纯色底图之间也会判重复，误判会变多。") {
		t.Fatalf("关掉护栏后提示没换: %v", tt.Texts())
	}
	if opts := a.dedupOptions(); !opts.IgnoreFlatColor {
		t.Fatalf("Options 没跟上开关: %+v", opts)
	}
}
