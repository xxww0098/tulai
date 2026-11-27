package images

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestWatermarkRepairsOnlyMatchedGlyphs(t *testing.T) {
	for _, platform := range []string{"豆包", "千问", "即梦"} {
		t.Run(platform, func(t *testing.T) {
			background := color.NRGBA{63, 100, 150, 255}
			src := image.NewNRGBA(image.Rect(0, 0, 768, 768))
			draw.Draw(src, src.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
			for _, style := range watermarkStyles {
				if style.platform != platform {
					continue
				}
				data, err := watermarkFiles.ReadFile("watermarks/" + style.file)
				if err != nil {
					t.Fatal(err)
				}
				im, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				s := 768 / float64(style.ref)
				w, h := int(math.Round(float64(im.Bounds().Dx())*s)), int(math.Round(float64(im.Bounds().Dy())*s))
				mask := resize(im, w, h)
				x, y := int(math.Round(float64(style.x)*s)), int(math.Round(float64(style.y)*s))
				if !style.topLeft {
					x, y = 768-x-w, 768-y-h
				}
				for row := 0; row < h; row++ {
					for col := 0; col < w; col++ {
						r, g, b, _ := mask.At(col, row).RGBA()
						a := float64(max(r, g, b)) / 65535
						mix := func(v uint8) uint8 { return uint8(math.Round(float64(v)*(1-a) + 255*a)) }
						src.SetNRGBA(x+col, y+row, color.NRGBA{mix(background.R), mix(background.G), mix(background.B), 255})
					}
				}
			}
			before := append([]byte(nil), src.Pix...)
			out, changed, err := RemoveWatermark(context.Background(), src, platform)
			if err != nil || !changed {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
			if !bytes.Equal(src.Pix, before) {
				t.Fatal("source image changed")
			}
			errorSum := 0.0
			originalError := 0.0
			for y := 0; y < 768; y++ {
				for x := 0; x < 768; x++ {
					v := color.NRGBAModel.Convert(out.At(x, y)).(color.NRGBA)
					old := src.NRGBAAt(x, y)
					errorSum += math.Abs(float64(v.R)-float64(background.R)) + math.Abs(float64(v.G)-float64(background.G)) + math.Abs(float64(v.B)-float64(background.B))
					originalError += math.Abs(float64(old.R)-float64(background.R)) + math.Abs(float64(old.G)-float64(background.G)) + math.Abs(float64(old.B)-float64(background.B))
				}
			}
			if errorSum >= originalError*.05 {
				t.Fatalf("repair error=%v original=%v", errorSum, originalError)
			}
			if _, changed, err := RemoveWatermark(context.Background(), out, platform); changed || err != nil {
				t.Fatalf("clean output matched again: changed=%v err=%v", changed, err)
			}
		})
	}
}

func TestWatermarkDoesNotInventMarks(t *testing.T) {
	src := image.NewNRGBA(image.Rect(10, 20, 778, 788))
	for y := 20; y < 788; y++ {
		for x := 10; x < 778; x++ {
			src.SetNRGBA(x, y, color.NRGBA{uint8(x*17 + y*13), uint8(x*5 + y), uint8(y * 7), 255})
		}
	}
	for _, platform := range []string{"豆包", "千问", "即梦", "unknown"} {
		if _, changed, err := RemoveWatermark(context.Background(), src, platform); changed || err != nil {
			t.Fatalf("%s changed=%v err=%v", platform, changed, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, changed, err := RemoveWatermark(ctx, src, "豆包"); changed || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled changed=%v err=%v", changed, err)
	}
}

func TestDoubaoWatermarkRepairsNonSquareRatiosWithoutFalsePositives(t *testing.T) {
	style := watermarkStyles[0] // 豆包
	data, err := watermarkFiles.ReadFile("watermarks/" + style.file)
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	background := color.NRGBA{72, 110, 148, 255}
	for _, dim := range []struct {
		name string
		w, h int
	}{
		{"4:3", 1024, 768},
		{"3:4", 768, 1024},
		{"16:9", 1280, 720},
		{"9:16", 720, 1280},
	} {
		t.Run(dim.name, func(t *testing.T) {
			src := image.NewNRGBA(image.Rect(0, 0, dim.w, dim.h))
			draw.Draw(src, src.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
			s := float64(min(dim.w, dim.h)) / float64(style.ref)
			mw, mh := int(math.Round(float64(im.Bounds().Dx())*s)), int(math.Round(float64(im.Bounds().Dy())*s))
			mask := resize(im, mw, mh)
			x := dim.w - int(math.Round(float64(style.x)*s)) - mw
			y := dim.h - int(math.Round(float64(style.y)*s)) - mh
			for row := 0; row < mh; row++ {
				for col := 0; col < mw; col++ {
					r, g, b, _ := mask.At(col, row).RGBA()
					a := float64(max(r, g, b)) / 65535
					mix := func(v uint8) uint8 { return uint8(math.Round(float64(v)*(1-a) + 255*a)) }
					src.SetNRGBA(x+col, y+row, color.NRGBA{mix(background.R), mix(background.G), mix(background.B), 255})
				}
			}
			out, changed, err := RemoveWatermark(context.Background(), src, "豆包")
			if err != nil || !changed {
				t.Fatalf("%s: changed=%v err=%v", dim.name, changed, err)
			}
			if _, again, err := RemoveWatermark(context.Background(), out, "豆包"); again || err != nil {
				t.Fatalf("%s: cleaned output matched again: changed=%v err=%v", dim.name, again, err)
			}

			// 无水印的纹理非方图不得误报
			clean := image.NewNRGBA(image.Rect(0, 0, dim.w, dim.h))
			for cy := 0; cy < dim.h; cy++ {
				for cx := 0; cx < dim.w; cx++ {
					clean.SetNRGBA(cx, cy, color.NRGBA{uint8(cx*17 + cy*13), uint8(cx*5 + cy), uint8(cy * 7), 255})
				}
			}
			if _, changed, err := RemoveWatermark(context.Background(), clean, "豆包"); changed || err != nil {
				t.Fatalf("%s: clean textured image falsely matched: changed=%v err=%v", dim.name, changed, err)
			}
		})
	}
}

func TestJimengWatermarkRepairsAllRatiosAndRejectsBlankBox(t *testing.T) {
	tpl, err := loadWatermarkTemplate("qwen-label.png")
	if err != nil {
		t.Fatal(err)
	}
	background := color.NRGBA{56, 94, 132, 255}
	for _, tc := range []struct {
		name      string
		w, h      int
		longScale bool
	}{
		{"1:1", 1024, 1024, true},
		{"3:4 long-edge", 768, 1024, true},
		{"3:4 short-edge", 768, 1024, false},
		{"4:3 long-edge", 1024, 768, true},
		{"16:9 long-edge", 1280, 720, true},
		{"9:16 short-edge", 720, 1280, false},
		{"2:3 long-edge", 800, 1200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := image.NewNRGBA(image.Rect(0, 0, tc.w, tc.h))
			draw.Draw(src, src.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
			refEdge := min(tc.w, tc.h)
			if tc.longScale {
				refEdge = max(tc.w, tc.h)
			}
			s := float64(refEdge) / 2048.0
			mw := int(math.Round(float64(tpl.Bounds().Dx()) * s))
			mh := int(math.Round(float64(tpl.Bounds().Dy()) * s))
			mask := resize(tpl, mw, mh)
			x := max(2, int(math.Round(50*s)))
			y := max(2, int(math.Round(42*s)))
			for row := 0; row < mh; row++ {
				for col := 0; col < mw; col++ {
					r, g, b, _ := mask.At(col, row).RGBA()
					a := float64(max(r, g, b)) / 65535
					mix := func(v uint8) uint8 { return uint8(math.Round(float64(v)*(1-a) + 255*a)) }
					src.SetNRGBA(x+col, y+row, color.NRGBA{mix(background.R), mix(background.G), mix(background.B), 255})
				}
			}
			out, changed, err := RemoveWatermark(context.Background(), src, "即梦")
			if err != nil || !changed {
				t.Fatalf("%s: changed=%v err=%v", tc.name, changed, err)
			}
			errorSum, originalError := 0.0, 0.0
			for py := 0; py < min(tc.h, y+mh+20); py++ {
				for px := 0; px < min(tc.w, x+mw+20); px++ {
					v := color.NRGBAModel.Convert(out.At(px, py)).(color.NRGBA)
					old := src.NRGBAAt(px, py)
					errorSum += math.Abs(float64(v.R)-float64(background.R)) + math.Abs(float64(v.G)-float64(background.G)) + math.Abs(float64(v.B)-float64(background.B))
					originalError += math.Abs(float64(old.R)-float64(background.R)) + math.Abs(float64(old.G)-float64(background.G)) + math.Abs(float64(old.B)-float64(background.B))
				}
			}
			if errorSum >= originalError*0.05 {
				t.Fatalf("%s: repair error=%v original=%v", tc.name, errorSum, originalError)
			}
			if _, again, err := RemoveWatermark(context.Background(), out, "即梦"); again || err != nil {
				t.Fatalf("%s: cleaned output matched again: changed=%v err=%v", tc.name, again, err)
			}

			// 无水印的纹理图不得误报
			clean := image.NewNRGBA(image.Rect(0, 0, tc.w, tc.h))
			for cy := 0; cy < tc.h; cy++ {
				for cx := 0; cx < tc.w; cx++ {
					clean.SetNRGBA(cx, cy, color.NRGBA{uint8(cx*17 + cy*13), uint8(cx*5 + cy), uint8(cy * 7), 255})
				}
			}
			if _, changed, err := RemoveWatermark(context.Background(), clean, "即梦"); changed || err != nil {
				t.Fatalf("%s: clean textured image falsely matched: changed=%v err=%v", tc.name, changed, err)
			}
		})
	}

	// 左上角只有空白圆角矩形边框（无「AI生成」文字）时不得误判。
	blankBox := image.NewNRGBA(image.Rect(0, 0, 768, 1024))
	draw.Draw(blankBox, blankBox.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	border := color.NRGBA{230, 230, 230, 255}
	for y := 18; y <= 72; y++ {
		for x := 20; x <= 140; x++ {
			if y < 22 || y > 68 || x < 24 || x > 136 {
				blankBox.SetNRGBA(x, y, border)
			}
		}
	}
	if _, changed, err := RemoveWatermark(context.Background(), blankBox, "即梦"); changed || err != nil {
		t.Fatalf("blank box without AI生成 text falsely matched: changed=%v err=%v", changed, err)
	}
}

func TestWatermarkDownloadedSamples(t *testing.T) {
	root := os.Getenv("TULAI_WATERMARK_SAMPLES")
	if root == "" {
		t.Skip("set TULAI_WATERMARK_SAMPLES for local downloaded-image QA")
	}
	for _, row := range []struct{ platform, path string }{{"豆包", "doubao/测试色彩图_1.png"}, {"千问", "qianwen/测试色彩图.png"}} {
		t.Run(row.platform, func(t *testing.T) {
			f, err := os.Open(filepath.Join(root, row.path))
			if err != nil {
				t.Fatal(err)
			}
			im, _, err := image.Decode(f)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
			maxEdge, threshold := 2048.0, 0.85
			if row.platform == "千问" {
				maxEdge, threshold = 768, 0.60
			}
			s := math.Min(1, maxEdge/float64(max(im.Bounds().Dx(), im.Bounds().Dy())))
			w, h := int(math.Round(float64(im.Bounds().Dx())*s)), int(math.Round(float64(im.Bounds().Dy())*s))
			hp := watermarkHighPass(resize(im, w, h))
			for _, style := range watermarkStyles {
				if style.platform != row.platform {
					continue
				}
				data, _ := watermarkFiles.ReadFile("watermarks/" + style.file)
				tpl, err := png.Decode(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				area, score, err := matchWatermark(context.Background(), hp, w, h, tpl, style)
				t.Log(style.file, area, score, err)
				if err != nil || score < threshold {
					t.Fatalf("missed known %s: %v %v", style.file, score, err)
				}
			}
			if _, changed, err := RemoveWatermark(context.Background(), im, row.platform); err != nil || !changed {
				t.Fatalf("changed=%v err=%v", changed, err)
			}
		})
	}
}

func BenchmarkRemoveWatermark2048(b *testing.B) {
	style := watermarkStyles[0]
	tpl, err := loadWatermarkTemplate(style.file)
	if err != nil {
		b.Fatal(err)
	}
	background := color.NRGBA{72, 110, 148, 255}
	marked := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	draw.Draw(marked, marked.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)
	s := 2048.0 / float64(style.ref)
	mw, mh := int(math.Round(float64(tpl.Bounds().Dx())*s)), int(math.Round(float64(tpl.Bounds().Dy())*s))
	mask := resize(tpl, mw, mh)
	x := 2048 - int(math.Round(float64(style.x)*s)) - mw
	y := 2048 - int(math.Round(float64(style.y)*s)) - mh
	for row := 0; row < mh; row++ {
		for col := 0; col < mw; col++ {
			r, g, bl, _ := mask.At(col, row).RGBA()
			a := float64(max(r, g, bl)) / 65535
			mix := func(v uint8) uint8 { return uint8(math.Round(float64(v)*(1-a) + 255*a)) }
			marked.SetNRGBA(x+col, y+row, color.NRGBA{mix(background.R), mix(background.G), mix(background.B), 255})
		}
	}
	clean := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	draw.Draw(clean, clean.Bounds(), image.NewUniform(background), image.Point{}, draw.Src)

	b.Run("with-watermark", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, changed, err := RemoveWatermark(context.Background(), marked, "豆包")
			if err != nil || !changed {
				b.Fatalf("changed=%v err=%v", changed, err)
			}
		}
	})
	b.Run("clean-miss", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, changed, err := RemoveWatermark(context.Background(), clean, "豆包")
			if err != nil || changed {
				b.Fatalf("changed=%v err=%v", changed, err)
			}
		}
	})
}

func BenchmarkResize2048To512(b *testing.B) {
	src := image.NewNRGBA(image.Rect(0, 0, 2048, 2048))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = resize(src, 512, 512)
	}
}
