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
	for _, platform := range []string{"豆包", "千问"} {
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
	for _, platform := range []string{"豆包", "千问", "unknown"} {
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

func TestWatermarkDownloadedSamples(t *testing.T) {
	root := os.Getenv("XBS_WATERMARK_SAMPLES")
	if root == "" {
		t.Skip("set XBS_WATERMARK_SAMPLES for local downloaded-image QA")
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
