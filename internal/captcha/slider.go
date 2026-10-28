package captcha

import (
	"image"
	"math"
	"math/rand"
	"time"

	"github.com/xxww0098/tulai/internal/cdp"
)

// TrackStep 是按下之后的一条采样。DX/DY 相对按下点，DT 是抵达这一点之前要等的时间。
type TrackStep struct {
	DX float64
	DY float64
	DT time.Duration
}

// HumanTrack 生成一段只向前的拖动。seed 固定时轨迹可复现。
// 不回拉：阿里云对落在目标左侧的轨迹更敏感。Y 只有逐渐收掉的轻微抖动。
func HumanTrack(dx float64, seed int64) []TrackStep {
	if dx <= 0 {
		return nil
	}
	steps := int(math.Round(dx/7)) + 14
	if steps < 18 {
		steps = 18
	}
	if steps > 42 {
		steps = 42
	}
	rng := rand.New(rand.NewSource(seed))
	out := make([]TrackStep, 0, steps)
	prev := 0.0
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		eased := 1 - math.Pow(1-t, 3)
		x := dx * eased
		if i == steps || x < prev {
			if i == steps {
				x = dx
			}
			if x < prev {
				x = prev
			}
		}
		y := math.Sin(float64(i)*0.85) * 1.1 * (1 - t)
		y += (rng.Float64() - 0.5) * 0.35 * (1 - t)
		base := 16.0
		if t < 0.18 || t > 0.82 {
			base = 28
		}
		dt := time.Duration(base+rng.Float64()*8) * time.Millisecond
		out = append(out, TrackStep{DX: x, DY: y, DT: dt})
		prev = x
	}
	return out
}

// SlideTravel 是滑块右边贴到轨道右边需要移动的距离。
func SlideTravel(track, handle cdp.SliderRect) float64 {
	return (track.X + track.W) - (handle.X + handle.W)
}

// PuzzleTravel 把缺口在背景图上的位移换成鼠标位移。
// 拼图块在图上能走的距离，和滑块在轨道上能走的距离，按同一个比例对应。
// 两边缺一边时，鼠标跟图片 CSS 像素 1:1。
func PuzzleTravel(gapNaturalX, naturalW, displayW, pieceLeft, imageTravel, trackTravel float64) float64 {
	if naturalW <= 1 || displayW <= 1 {
		return 0
	}
	delta := gapNaturalX*(displayW/naturalW) - pieceLeft
	if imageTravel > 8 && trackTravel > 8 {
		return delta / imageTravel * trackTravel
	}
	return delta
}

// DragPlan 把距离变成主框架视口里的鼠标事件。按下前先移到滑块上，避免从 (0,0) 瞬移。
func DragPlan(cx, cy, dx float64, seed int64) []cdp.SliderEvent {
	if dx <= 0 {
		return nil
	}
	events := []cdp.SliderEvent{
		{Type: "mouseMoved", X: cx - 72, Y: cy + 9, Wait: 40 * time.Millisecond},
		{Type: "mouseMoved", X: cx - 18, Y: cy + 2, Wait: 30 * time.Millisecond},
		{Type: "mouseMoved", X: cx, Y: cy, Wait: 50 * time.Millisecond},
		{Type: "mousePressed", X: cx, Y: cy, Buttons: 1, Wait: 70 * time.Millisecond},
	}
	for _, step := range HumanTrack(dx, seed) {
		events = append(events, cdp.SliderEvent{
			Type:    "mouseMoved",
			X:       cx + step.DX,
			Y:       cy + step.DY,
			Buttons: 1,
			Wait:    step.DT,
		})
	}
	last := events[len(events)-1]
	events = append(events, cdp.SliderEvent{
		Type: "mouseReleased", X: last.X, Y: last.Y, Wait: 30 * time.Millisecond,
	})
	return events
}

// Gap 是拼图块图片左上角应该对齐到的背景像素。
type Gap struct {
	X          int
	Confidence float64
}

// FindPuzzleGap 用拼图块的不透明形状在背景上找更暗、边缘也更贴的位置。
// 不依赖 OpenCV 或付费识别。置信度低表示画面里没有明显缺口。
func FindPuzzleGap(bg, piece image.Image) (Gap, error) {
	bgG := toGray(bg)
	if len(bgG) < 8 || len(bgG[0]) < 8 {
		return Gap{}, errGapSize
	}
	mask := pieceMask(piece)
	ph, pw := len(mask), len(mask[0])
	bh, bw := len(bgG), len(bgG[0])
	if ph < 4 || pw < 4 || ph > bh || pw > bw {
		return Gap{}, errGapSize
	}
	contour := contourOf(mask)
	sobel := sobelMag(bgG)
	best := -1e18
	bestX, bestY := 0, 0
	consider := func(x, y int) {
		s := gapScore(bgG, sobel, mask, contour, x, y)
		if s > best {
			best = s
			bestX, bestY = x, y
		}
	}
	for y := 0; y+ph <= bh; y += 3 {
		for x := 0; x+pw <= bw; x += 2 {
			consider(x, y)
		}
	}
	for y := maxInt(0, bestY-3); y <= minInt(bh-ph, bestY+3); y++ {
		for x := maxInt(0, bestX-4); x <= minInt(bw-pw, bestX+4); x++ {
			consider(x, y)
		}
	}
	// 紧挨着最佳点的位置几乎一样暗，不能拿来当“另一个缺口”。
	second := -1e18
	far := pw / 2
	if far < 6 {
		far = 6
	}
	for y := 0; y+ph <= bh; y += 3 {
		for x := 0; x+pw <= bw; x += 2 {
			dx, dy := x-bestX, y-bestY
			if dx < 0 {
				dx = -dx
			}
			if dy < 0 {
				dy = -dy
			}
			if dx <= far && dy <= far {
				continue
			}
			s := gapScore(bgG, sobel, mask, contour, x, y)
			if s > second {
				second = s
			}
		}
	}
	conf := 0.0
	if second > -1e17 {
		conf = (best - second) / 40
	}
	if conf < 0 {
		conf = 0
	}
	if conf > 1 {
		conf = 1
	}
	return Gap{X: bestX, Confidence: conf}, nil
}

func gapScore(bg, sobel [][]float64, mask, contour [][]bool, x, y int) float64 {
	var sum, n, edge, en float64
	ph, pw := len(mask), len(mask[0])
	for py := 0; py < ph; py += 2 {
		for px := 0; px < pw; px += 2 {
			if !mask[py][px] {
				continue
			}
			sum += bg[y+py][x+px]
			n++
			if contour[py][px] {
				edge += sobel[y+py][x+px]
				en++
			}
		}
	}
	if n == 0 {
		return -1e18
	}
	e := 0.0
	if en > 0 {
		e = edge / en
	}
	return -sum/n + 0.45*e
}

func pieceMask(img image.Image) [][]bool {
	b := img.Bounds()
	h, w := b.Dy(), b.Dx()
	mask := make([][]bool, h)
	opaque, clear := 0, 0
	for y := 0; y < h; y++ {
		mask[y] = make([]bool, w)
		for x := 0; x < w; x++ {
			_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a > 0x8000 {
				mask[y][x] = true
				opaque++
			} else {
				clear++
			}
		}
	}
	if opaque == 0 || clear*20 < opaque {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				mask[y][x] = true
			}
		}
	}
	return mask
}

func contourOf(mask [][]bool) [][]bool {
	h, w := len(mask), len(mask[0])
	out := make([][]bool, h)
	for y := 0; y < h; y++ {
		out[y] = make([]bool, w)
		for x := 0; x < w; x++ {
			if !mask[y][x] {
				continue
			}
			if x == 0 || y == 0 || x == w-1 || y == h-1 || !mask[y][x-1] || !mask[y][x+1] || !mask[y-1][x] || !mask[y+1][x] {
				out[y][x] = true
			}
		}
	}
	return out
}

func sobelMag(g [][]float64) [][]float64 {
	h, w := len(g), len(g[0])
	out := make([][]float64, h)
	for y := 0; y < h; y++ {
		out[y] = make([]float64, w)
		for x := 0; x < w; x++ {
			xm, xp := maxInt(0, x-1), minInt(w-1, x+1)
			ym, yp := maxInt(0, y-1), minInt(h-1, y+1)
			gx := g[y][xp] - g[y][xm]
			gy := g[yp][x] - g[ym][x]
			if gx < 0 {
				gx = -gx
			}
			if gy < 0 {
				gy = -gy
			}
			out[y][x] = gx + gy
		}
	}
	return out
}
