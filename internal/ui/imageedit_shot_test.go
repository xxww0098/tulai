package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestExportImageEditShots(t *testing.T) {
	dir := os.Getenv("TULAI_EDIT_SHOTS")
	if dir == "" {
		t.Skip("set TULAI_EDIT_SHOTS to write image edit frames")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pics := t.TempDir()
	paths := make([]string, 0, 4)
	for i, size := range [][2]int{{960, 540}, {480, 720}, {600, 600}} {
		path := filepath.Join(pics, []string{"山景.png", "人像.png", "静物.png"}[i])
		img := image.NewNRGBA(image.Rect(0, 0, size[0], size[1]))
		for y := 0; y < size[1]; y++ {
			for x := 0; x < size[0]; x++ {
				img.SetNRGBA(x, y, color.NRGBA{
					R: uint8(50 + 120*x/size[0]),
					G: uint8(70 + 120*y/size[1]),
					B: uint8(120 + 80*(x+y)/(size[0]+size[1])),
					A: 255,
				})
			}
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		paths = append(paths, path)
	}

	build := func() *App {
		a := &App{tab: pageImageEdit}
		for _, p := range paths {
			a.edit.files = append(a.edit.files, &editFile{Path: p, Name: filepath.Base(p), Status: "pending"})
		}
		a.edit.selected = 0
		a.edit.locked = true
		a.edit.mode = "crop"
		a.edit.split = 50
		a.edit.zoom = 1
		data, err := os.ReadFile(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		a.edit.orig = ui.NewBitmap(img)
		a.edit.out = a.edit.orig
		a.edit.srcW, a.edit.srcH = img.Bounds().Dx(), img.Bounds().Dy()
		a.edit.outW, a.edit.outH = a.edit.srcW, a.edit.srcH
		a.edit.origPath = paths[0]
		a.edit.previewSig = a.editPreviewSignature(paths[0])
		for _, f := range a.edit.files {
			f.Thumb = ui.NewBitmap(img)
		}
		a.editEnsureMaps()
		return a
	}

	save := func(name string, a *App, width, height int, dark bool) {
		t.Helper()
		view := a.view
		if dark {
			view = func(c *ui.Context) {
				theme := *c.Theme()
				theme.Dark = true
				c.SetTheme(&theme)
				a.view(c)
			}
		}
		tt := ui.NewTester(view, width, height)
		tt.Advance(400 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
	}

	save("edit-empty", &App{tab: pageImageEdit}, 1280, 860, false)
	save("edit-loaded", build(), 1280, 860, false)

	crop := build()
	crop.edit.enabled.Crop = true
	crop.edit.expanded["crop"] = true
	crop.edit.opts.CropWidth, crop.edit.opts.CropHeight = 640, 360
	crop.edit.dimW, crop.edit.dimH = 640, 360
	crop.edit.dimWText, crop.edit.dimHText = "640", "360"
	crop.edit.enabled.Adjust = true
	crop.edit.expanded["adjust"] = true
	crop.edit.opts.Brightness = 12
	crop.edit.opts.Contrast = 8
	crop.edit.previewSig = crop.editPreviewSignature(paths[0])
	save("edit-crop", crop, 1280, 860, false)
	save("edit-dark", build(), 1100, 760, true)

	running := build()
	running.edit.running = true
	running.edit.progressDone, running.edit.progressTotal = 1, 3
	running.edit.progressCurrent = "山景.png"
	save("edit-running", running, 1280, 860, false)

	picked := build()
	picked.edit.picks = map[string]bool{paths[0]: true, paths[2]: true}
	save("edit-picked", picked, 1280, 860, false)

	failed := build()
	failed.edit.outDir = "/Users/xxww/图片/输出"
	failed.edit.files[1].Status = "failed"
	failed.edit.files[1].Err = "无法读取图片：unexpected EOF"
	failed.edit.files[2].Status = "ok"
	failed.edit.files[0].Status = "ok"
	failed.edit.notice = "完成 2 个，失败 1 个 → /Users/xxww/图片/输出"
	failed.edit.resultOpen = true
	save("edit-results", failed, 1280, 860, false)
}
