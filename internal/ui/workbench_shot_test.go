package ui

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/store"
	"github.com/xxww0098/tulai/internal/tasks"
)

func TestExportWorkbenchShots(t *testing.T) {
	dir := os.Getenv("TULAI_SHOTS")
	if dir == "" {
		t.Skip("set TULAI_SHOTS to write workbench frames")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pics := filepath.Join(t.TempDir(), "pics")
	if err := os.MkdirAll(pics, 0o755); err != nil {
		t.Fatal(err)
	}
	land := filepath.Join(pics, "风景.png")
	face := filepath.Join(pics, "人像.png")
	still := filepath.Join(pics, "静物.png")
	out := filepath.Join(pics, "猫.png")
	writeShot(t, land, 640, 360, func(x, y, w, h int) color.NRGBA {
		sky := uint8(70 + 140*y/h)
		if (x-w/2)*(x-w/2)+(y-h/3)*(y-h/3) < 48*48 {
			return color.NRGBA{R: 250, G: 200, B: 60, A: 255}
		}
		if y > h*2/3 {
			return color.NRGBA{R: 40, G: 140, B: 70, A: 255}
		}
		return color.NRGBA{R: 70, G: 150, B: sky, A: 255}
	})
	writeShot(t, face, 360, 640, func(x, y, w, h int) color.NRGBA {
		dx, dy := x-w/2, y-h/3
		if dx*dx+dy*dy < 70*70 {
			return color.NRGBA{R: 230, G: 170, B: 140, A: 255}
		}
		return color.NRGBA{R: 40, G: 55, B: 90, A: 255}
	})
	writeShot(t, still, 480, 480, func(x, y, w, h int) color.NRGBA {
		dx, dy := x-w/2, y-h/2+20
		if dx*dx+dy*dy < 90*90 {
			return color.NRGBA{R: 150, G: 70, B: 170, A: 255}
		}
		return color.NRGBA{R: 245, G: 236, B: 220, A: 255}
	})
	writeShot(t, out, 512, 512, func(x, y, w, h int) color.NRGBA {
		dx, dy := x-w/2, y-h/2
		if dx*dx+dy*dy < 120*120 {
			return color.NRGBA{R: 255, G: 120, B: 80, A: 255}
		}
		return color.NRGBA{R: 20, G: 30, B: 50, A: 255}
	})

	save := func(name string, a *App) {
		t.Helper()
		tt := ui.NewTester(a.view, 1280, 900)
		tt.Advance(400 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}

	const shotOut = "图片/输出结果"
	save("after-empty", &App{platformAuto: true, outputPath: shotOut})

	picked := &App{
		platformAuto: true,
		outputPath:   shotOut,
		sources: []sourceImage{
			{Path: land, Hash: "land"},
			{Path: face, Hash: "face"},
		},
	}
	picked.preloadThumbs([]string{land, face})
	save("after-selected", picked)

	run := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		running:         true,
		platDoubao:      true,
		outputPath:      shotOut,
		prompts:         "水彩，保留构图",
		browserRows: []store.Browser{
			{ID: 1, Name: "豆包主力", Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady},
			{ID: 2, Name: "千问备用", Platform: "千问", IsLoggedIn: true, Health: store.HealthReady},
		},
		sources: []sourceImage{
			{Path: land, Hash: "land"},
			{Path: face, Hash: "face"},
		},
		queue: []tasks.Task{
			{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusPending, Images: []string{land}, Prompt: "水彩"},
			{Index: 2, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning, Images: []string{face}, Prompt: "水彩"},
			{Index: 3, Platform: "千问", BrowserID: 2, Status: tasks.StatusFailed, Images: []string{still}, Prompt: "水彩", Error: "账号额度已用尽"},
			{Index: 4, Platform: "豆包", BrowserID: 1, Status: tasks.StatusSuccess, Prompt: "文生图一只猫", Results: []string{out}},
		},
	}
	run.preloadThumbs([]string{land, face, still, out})
	save("after-run", run)

	saveTall := func(name string, a *App) {
		t.Helper()
		tt := ui.NewTester(a.view, 1280, 1040)
		tt.Advance(450 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	today := time.Now().Format("2006-01-02")
	idle := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		outputPath:      shotOut,
		prompts:         "水彩，保留构图",
		sources:         []sourceImage{{Path: land, Hash: "land"}, {Path: face, Hash: "face"}},
		browserRows: []store.Browser{
			{ID: 1, Name: "豆包主力", Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 3},
			{ID: 2, Name: "千问备用", Platform: "千问", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 1},
		},
	}
	idle.preloadThumbs([]string{land, face})
	saveTall("lanes-idle", idle)

	running := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		running:         true,
		outputPath:      shotOut,
		prompts:         "水彩，保留构图",
		sources:         []sourceImage{{Path: land, Hash: "land"}, {Path: face, Hash: "face"}, {Path: still, Hash: "still"}},
		browserRows: []store.Browser{
			{ID: 1, Name: "豆包主力", Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 4},
			{ID: 2, Name: "豆包小号", Platform: "豆包", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 2},
			{ID: 3, Name: "千问备用", Platform: "千问", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 6},
		},
		queue: []tasks.Task{
			{Index: 1, Platform: "豆包", BrowserID: 1, Status: tasks.StatusRunning, Images: []string{land}, Prompt: "水彩，保留构图，傍晚的光"},
			{Index: 2, Platform: "豆包", BrowserID: 2, Status: tasks.StatusRunning, Images: []string{face}, Prompt: "人像改成胶片色"},
			{Index: 3, Platform: "千问", BrowserID: 3, Status: tasks.StatusRunning, Images: []string{still}, Prompt: "静物换成暖光"},
		},
	}
	running.preloadThumbs([]string{land, face, still, out})
	saveTall("lanes-running", running)

	blocked := &App{
		platformAuto:    true,
		concurrencyAuto: true,
		outputPath:      shotOut,
		dailyCap:        20,
		prompts:         "一只猫",
		sources:         []sourceImage{{Path: land, Hash: "land"}},
		browserRows: []store.Browser{
			{ID: 1, Name: "豆包没登录", Platform: "豆包", Health: store.HealthExhausted, LastError: "未登录"},
			{ID: 2, Name: "千问今天满了", Platform: "千问", IsLoggedIn: true, Health: store.HealthReady, DayKey: today, DayCount: 20},
		},
	}
	blocked.preloadThumbs([]string{land})
	saveTall("lanes-blocked", blocked)

	saveAt := func(name string, width, height int, app *App) {
		t.Helper()
		tt := ui.NewTester(app.view, width, height)
		tt.Advance(450 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	extra := []struct{ file, prompt string }{
		{"街景.png", "街边的傍晚"},
		{"夜色.png", "深蓝的夜色"},
		{"花.png", "一束白花"},
	}
	var extraSrc []sourceImage
	var extraQueue []tasks.Task
	for i, item := range extra {
		path := filepath.Join(pics, item.file)
		writeShot(t, path, 320, 320, func(x, y, w, h int) color.NRGBA {
			return color.NRGBA{R: uint8(40 + i*60), G: uint8(80 + x%80), B: uint8(120 + y%80), A: 255}
		})
		extraSrc = append(extraSrc, sourceImage{Path: path})
		extraQueue = append(extraQueue, tasks.Task{
			Index: 4 + i, Platform: "豆包", Status: tasks.StatusPending,
			Images: []string{path}, Prompt: item.prompt,
		})
	}
	six := &App{
		platformAuto: running.platformAuto, concurrencyAuto: running.concurrencyAuto,
		running: true, outputPath: running.outputPath, prompts: running.prompts,
		browserRows: running.browserRows,
		sources:     append(append([]sourceImage{}, running.sources...), extraSrc...),
		queue:       append(append([]tasks.Task{}, running.queue...), extraQueue...),
	}
	six.preloadThumbs(extraPaths(six.sources))
	saveAt("lanes-running-1024", 1024, 1100, running)
	saveAt("lanes-running-6", 1280, 1200, six)
	saveAt("lanes-running-6-1024", 1024, 1400, six)
}

func extraPaths(sources []sourceImage) []string {
	out := make([]string, len(sources))
	for i := range sources {
		out[i] = sources[i].Path
	}
	return out
}

func writeShot(t *testing.T, path string, w, h int, px func(x, y, w, h int) color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, px(x, y, w, h))
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
