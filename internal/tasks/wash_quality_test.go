package tasks

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"testing"

	"github.com/xxww0098/tulai/internal/dedup"
	"github.com/xxww0098/tulai/internal/images"
	"github.com/xxww0098/tulai/internal/strategy"
)

func washQualityTexturedRect(w, h int, seed uint8) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r := uint8((x*7 + y*11 + int(seed)*19) % 200)
			g := uint8((x*13 + y*5 + int(seed)*23) % 180)
			b := uint8((x*3 + y*17 + int(seed)*29) % 190)
			img.SetNRGBA(x, y, color.NRGBA{R: r + 20, G: g + 20, B: b + 20, A: 255})
		}
	}
	return img
}

func TestInspectWashQualityDetectsLetterboxBarsAndSkipsWhiteBackground(t *testing.T) {
	clean := washQualityTexturedRect(512, 512, 1)
	if rep := inspectWashQuality(clean, nil); !rep.OK() {
		t.Fatalf("clean textured image flagged: %+v", rep)
	}

	sideWhite := washQualityTexturedRect(512, 512, 2)
	white := image.NewUniform(color.NRGBA{255, 255, 255, 255})
	draw.Draw(sideWhite, image.Rect(0, 0, 32, 512), white, image.Point{}, draw.Src)
	draw.Draw(sideWhite, image.Rect(480, 0, 512, 512), white, image.Point{}, draw.Src)
	rep := inspectWashQuality(sideWhite, nil)
	if rep.OK() || len(rep.LetterboxSides) != 2 || rep.LetterboxSides[0] != "left" || rep.LetterboxSides[1] != "right" {
		t.Fatalf("expected left/right white bar detection, got %+v", rep)
	}

	topBotBlack := washQualityTexturedRect(512, 512, 3)
	black := image.NewUniform(color.NRGBA{0, 0, 0, 255})
	draw.Draw(topBotBlack, image.Rect(0, 0, 512, 32), black, image.Point{}, draw.Src)
	draw.Draw(topBotBlack, image.Rect(0, 480, 512, 512), black, image.Point{}, draw.Src)
	repBlack := inspectWashQuality(topBotBlack, nil)
	if repBlack.OK() || len(repBlack.LetterboxSides) != 2 || repBlack.LetterboxSides[0] != "top" || repBlack.LetterboxSides[1] != "bottom" {
		t.Fatalf("expected top/bottom black bar detection, got %+v", repBlack)
	}

	product := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	draw.Draw(product, product.Bounds(), white, image.Point{}, draw.Src)
	subj := washQualityTexturedRect(256, 256, 4)
	draw.Draw(product, image.Rect(128, 128, 384, 384), subj, image.Point{}, draw.Src)
	if repProd := inspectWashQuality(product, nil); !repProd.OK() {
		t.Fatalf("white-background product photo must not be flagged: %+v", repProd)
	}

	tiny := washQualityTexturedRect(128, 128, 5)
	draw.Draw(tiny, image.Rect(0, 0, 16, 128), black, image.Point{}, draw.Src)
	draw.Draw(tiny, image.Rect(112, 0, 128, 128), black, image.Point{}, draw.Src)
	if repTiny := inspectWashQuality(tiny, nil); !repTiny.OK() {
		t.Fatalf("tiny image must be skipped: %+v", repTiny)
	}
}

func TestInspectWashQualityDetectsNearIdenticalSourceEcho(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			v := uint8(40 + (x*180)/512)
			if y > 256 {
				v = 255 - v
			}
			if (x/64+y/64)%2 == 0 {
				v = uint8(min(255, int(v)+35))
			}
			src.SetNRGBA(x, y, color.NRGBA{R: v, G: uint8(int(v) * 3 / 4), B: 255 - v, A: 255})
		}
	}
	srcSig := dedup.HashImage(src)
	if srcSig.Flat {
		t.Fatal("source fixture must not be flat")
	}

	repSame := inspectWashQuality(src, &srcSig)
	if !repSame.NearIdenticalSource || repSame.SourceDistance > 2 || repSame.Summary() == "" {
		t.Fatalf("expected near-identical source alert, got %+v", repSame)
	}

	washed := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			v := uint8(230 - (y*190)/512)
			if x > 256 {
				v = uint8(min(255, int(v)+40))
			}
			washed.SetNRGBA(x, y, color.NRGBA{R: 255 - v, G: v, B: uint8(int(v) / 2), A: 255})
		}
	}
	repDiff := inspectWashQuality(washed, &srcSig)
	if repDiff.NearIdenticalSource {
		t.Fatalf("genuinely altered output flagged as source echo: %+v", repDiff)
	}
}

func TestSaveResultsRunsWashQualityForQianwen(t *testing.T) {
	dir := t.TempDir()
	e := NewEngine(nil, nil, nil, "")
	e.opts = Options{Wash: true, OutputPath: dir}

	letterboxed := washQualityTexturedRect(512, 512, 7)
	black := image.NewUniform(color.NRGBA{0, 0, 0, 255})
	draw.Draw(letterboxed, image.Rect(0, 0, 32, 512), black, image.Point{}, draw.Src)
	draw.Draw(letterboxed, image.Rect(480, 0, 512, 512), black, image.Point{}, draw.Src)
	raw, err := images.EncodePNG(letterboxed)
	if err != nil {
		t.Fatal(err)
	}

	task := &Task{Index: 1, Platform: "千问", Prompt: "洗图"}
	saved, err := e.saveResults(context.Background(), task, nil, nil, []strategy.Result{{Data: raw}})
	if err != nil || len(saved) != 1 {
		t.Fatalf("saveResults = (%v, %v)", saved, err)
	}
	if task.QualityNote == "" {
		t.Fatal("expected Qianwen wash letterbox output to populate QualityNote")
	}
}
