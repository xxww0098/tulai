package captcha

import (
	"image"
	"image/color"
	"math"
	"testing"
	"time"
)

func TestHumanTrackLandsAndDoesNotReverse(t *testing.T) {
	track := HumanTrack(186, 7)
	if len(track) < 18 {
		t.Fatalf("steps=%d", len(track))
	}
	prev := 0.0
	var sum time.Duration
	for i, step := range track {
		if step.DX < prev {
			t.Fatalf("step %d reversed %.2f -> %.2f", i, prev, step.DX)
		}
		if math.Abs(step.DY) > 2.5 {
			t.Fatalf("y jitter %.2f", step.DY)
		}
		prev = step.DX
		sum += step.DT
	}
	if math.Abs(track[len(track)-1].DX-186) > 0.01 {
		t.Fatalf("end=%v", track[len(track)-1])
	}
	if sum < 350*time.Millisecond || sum > 2*time.Second {
		t.Fatalf("duration %s", sum)
	}
	again := HumanTrack(186, 7)
	if again[3].DX != track[3].DX || again[3].DY != track[3].DY {
		t.Fatal("seed did not reproduce the track")
	}
}

func TestDragPlanPressesThenReleasesAtEnd(t *testing.T) {
	events := DragPlan(40, 80, 160, 3)
	if len(events) < 10 {
		t.Fatalf("events=%d", len(events))
	}
	if events[0].Type != "mouseMoved" || events[0].Buttons != 0 {
		t.Fatalf("approach %+v", events[0])
	}
	pressed := false
	var lastX float64
	for _, ev := range events {
		if ev.Type == "mousePressed" {
			pressed = true
			lastX = ev.X
			if ev.Buttons != 1 || ev.X != 40 || ev.Y != 80 {
				t.Fatalf("press %+v", ev)
			}
			continue
		}
		if !pressed || ev.Type != "mouseMoved" {
			continue
		}
		if ev.X+0.01 < lastX {
			t.Fatalf("moved backward %v -> %v", lastX, ev.X)
		}
		lastX = ev.X
	}
	end := events[len(events)-1]
	if end.Type != "mouseReleased" || math.Abs(end.X-200) > 0.01 || end.Buttons != 0 {
		t.Fatalf("release %+v", end)
	}
}

func TestPuzzleTravelScalesTrackToImage(t *testing.T) {
	got := PuzzleTravel(160, 280, 280, 0, 240, 240)
	if math.Abs(got-160) > 0.01 {
		t.Fatalf("1:1 travel %v", got)
	}
	got = PuzzleTravel(100, 200, 100, 0, 80, 240)
	// gap CSS = 100 * (100/200) = 50; mouse = 50/80*240 = 150
	if math.Abs(got-150) > 0.01 {
		t.Fatalf("scaled travel %v", got)
	}
}

func TestFindPuzzleGapOnDarkNotch(t *testing.T) {
	bg := image.NewNRGBA(image.Rect(0, 0, 280, 120))
	fill(bg, color.NRGBA{R: 210, G: 210, B: 210, A: 255})
	fillRect(bg, 160, 40, 40, 40, color.NRGBA{R: 30, G: 30, B: 30, A: 255})
	piece := image.NewNRGBA(image.Rect(0, 0, 40, 40))
	fill(piece, color.NRGBA{R: 180, G: 180, B: 180, A: 255})
	gap, err := FindPuzzleGap(bg, piece)
	if err != nil {
		t.Fatal(err)
	}
	if gap.X < 156 || gap.X > 164 {
		t.Fatalf("gap x=%d conf=%.2f", gap.X, gap.Confidence)
	}
	if gap.Confidence < 0.25 {
		t.Fatalf("confidence %.2f", gap.Confidence)
	}
}

func TestFindPuzzleGapPrefersShapeOverDarkRectangle(t *testing.T) {
	const (
		w, h   = 220, 90
		size   = 36
		holeX  = 140
		decoyX = 12
		holeY  = 24
	)
	bg := image.NewNRGBA(image.Rect(0, 0, w, h))
	fill(bg, color.NRGBA{R: 200, G: 200, B: 200, A: 255})
	fillRect(bg, decoyX, holeY, size, size, color.NRGBA{R: 25, G: 25, B: 25, A: 255})
	piece := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-size/2, y-size/2
			if dx*dx+dy*dy <= 14*14 {
				piece.SetNRGBA(x, y, color.NRGBA{R: 80, G: 80, B: 80, A: 255})
				bg.SetNRGBA(holeX+x, holeY+y, color.NRGBA{R: 25, G: 25, B: 25, A: 255})
			}
		}
	}
	gap, err := FindPuzzleGap(bg, piece)
	if err != nil {
		t.Fatal(err)
	}
	if gap.X < holeX-4 || gap.X > holeX+4 {
		t.Fatalf("matched decoy or missed hole: x=%d conf=%.2f", gap.X, gap.Confidence)
	}
}

func TestFindPuzzleGapUniformIsUnsure(t *testing.T) {
	bg := image.NewNRGBA(image.Rect(0, 0, 80, 40))
	fill(bg, color.NRGBA{R: 128, G: 128, B: 128, A: 255})
	piece := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	fill(piece, color.NRGBA{A: 255})
	gap, err := FindPuzzleGap(bg, piece)
	if err != nil {
		t.Fatal(err)
	}
	if gap.Confidence >= 0.25 {
		t.Fatalf("uniform confidence %.2f", gap.Confidence)
	}
}

func fill(img *image.NRGBA, c color.NRGBA) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

func fillRect(img *image.NRGBA, x0, y0, w, h int, c color.NRGBA) {
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}
