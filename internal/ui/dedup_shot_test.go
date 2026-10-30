package ui

import (
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/tasks"
)

// TestExportDedupShots 把去重页的三种状态写成 PNG，方便改版式时对照。
// 只在 TULAI_SHOTS 指向目录时跑，和出图页的截图测试一样。
func TestExportDedupShots(t *testing.T) {
	dir := os.Getenv("TULAI_SHOTS")
	if dir == "" {
		t.Skip("set TULAI_SHOTS to write dedup frames")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pics := filepath.Join(t.TempDir(), "pics")
	if err := os.MkdirAll(pics, 0o755); err != nil {
		t.Fatal(err)
	}
	// 三张同一张图的两个尺寸，加一张完全不同的，去重后留一张。
	red := filepath.Join(pics, "红色几何.png")
	blue := filepath.Join(pics, "蓝色几何.png")
	odd := filepath.Join(pics, "另一张.png")
	writeShot(t, red, 640, 480, func(x, y, w, h int) color.NRGBA {
		if x < w/3 {
			return color.NRGBA{R: 226, G: 74, B: 74, A: 255}
		}
		return color.NRGBA{R: 250, G: 236, B: 214, A: 255}
	})
	writeShot(t, blue, 640, 480, func(x, y, w, h int) color.NRGBA {
		if x < w/3 {
			return color.NRGBA{R: 226, G: 74, B: 74, A: 255}
		}
		return color.NRGBA{R: 250, G: 236, B: 214, A: 255}
	})
	writeShot(t, odd, 320, 240, func(x, y, w, h int) color.NRGBA {
		return color.NRGBA{R: 40, G: uint8(120 + 100*y/h), B: 210, A: 255}
	})

	save := func(name string, a *App) {
		t.Helper()
		tt := ui.NewTester(a.view, 1280, 1000)
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
	base := func() *App {
		a := &App{engine: tasks.NewEngine(nil, nil, nil, ""), tab: pageDedup, outputPath: "图片/输出结果"}
		a.dedup.level = dedupDefaultLevel
		return a
	}
	save("dedup-empty", base())

	picked := base()
	picked.dedupIngest([]string{pics})
	save("dedup-picked", picked)

	ran := base()
	ran.dedup.outDir = filepath.Join(dir, "out")
	ran.dedupIngest([]string{pics})
	ran.dedupStart()
	waitDedup(t, ran)
	save("dedup-done", ran)

	// 「更多设置」展开的样子。
	more := base()
	more.dedupIngest([]string{pics})
	more.dedup.more = true
	save("dedup-more", more)

	// 缩略图是异步解码的，第二帧才画得出来。
	shots := base()
	shots.dedupIngest([]string{pics})
	for _, p := range []string{red, blue, odd} {
		if img, err := images.CoverThumb(p, thumbPx); err == nil && img != nil {
			if shots.thumbs == nil {
				shots.thumbs = map[string]*ui.Bitmap{}
			}
			shots.thumbs[p] = ui.NewBitmap(img)
		}
	}
	save("dedup-thumbs", shots)
}
