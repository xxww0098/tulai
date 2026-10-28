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

	save("after-empty", &App{platformAuto: true})

	picked := &App{
		platformAuto: true,
		sources: []sourceImage{
			{Path: land, Hash: "land"},
			{Path: face, Hash: "face"},
		},
	}
	picked.preloadThumbs([]string{land, face})
	save("after-selected", picked)

	run := &App{
		platformAuto: true,
		platDoubao:   true,
		outputPath:   "输出结果",
		prompts:      "水彩，保留构图",
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
