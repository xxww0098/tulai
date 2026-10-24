package images

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

//go:embed watermarks
var watermarkFiles embed.FS

type watermarkStyle struct {
	file, platform string
	ref, x, y      int
	topLeft        bool
}

var watermarkStyles = []watermarkStyle{
	{"doubao.png", "豆包", 1536, 31, 27, false},
	{"qwen.png", "千问", 2048, 62, 63, false},
	{"qwen-label.png", "千问", 2048, 57, 57, true},
}

// RemoveWatermark 匹配已校准的角落水印并局部修复；原图不变，未匹配时不返回处理版。
func RemoveWatermark(ctx context.Context, src image.Image, platform string) (image.Image, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if src == nil {
		return nil, false, fmt.Errorf("去水印: 无有效图片")
	}
	b := src.Bounds()
	if b.Dx() < 128 || b.Dy() < 128 || int64(b.Dx()) > (32*1024*1024)/int64(b.Dy()) {
		return nil, false, nil
	}
	// shortcut: 仅校准当前方图网页导出；其他比例需独立采集模板后再开放。
	if b.Dx() != b.Dy() {
		return nil, false, nil
	}
	if platform != "豆包" && platform != "千问" {
		return nil, false, nil
	}
	// 16 位图片保持原样，避免另存 8 位 PNG 时降低原图精度。
	switch src.ColorModel() {
	case color.Gray16Model, color.RGBA64Model, color.NRGBA64Model:
		return nil, false, nil
	}
	short := min(b.Dx(), b.Dy())
	maxEdge, threshold := 2048, 0.85
	if platform == "千问" {
		// 千问在不同输出尺寸重新栅格化字形，缩小后比较轮廓可减轻像素相位差。
		maxEdge, threshold = 768, 0.60
	}
	workScale := math.Min(1, float64(maxEdge)/float64(max(b.Dx(), b.Dy())))
	w, h := int(math.Round(float64(b.Dx())*workScale)), int(math.Round(float64(b.Dy())*workScale))
	work := resize(src, w, h)
	hp := watermarkHighPass(work)
	var dst *image.NRGBA
	for _, style := range watermarkStyles {
		if style.platform != platform {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		data, err := watermarkFiles.ReadFile("watermarks/" + style.file)
		if err != nil {
			return nil, false, fmt.Errorf("读取水印模板: %w", err)
		}
		template, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, false, fmt.Errorf("解析水印模板: %w", err)
		}
		area, score, err := matchWatermark(ctx, hp, w, h, template, style)
		if err != nil {
			return nil, false, err
		}
		if score < threshold {
			continue
		}
		if dst == nil {
			dst = image.NewNRGBA(b)
			draw.Draw(dst, b, src, b.Min, draw.Src)
		}
		area = image.Rect(b.Min.X+int(math.Round(float64(area.Min.X)/workScale)), b.Min.Y+int(math.Round(float64(area.Min.Y)/workScale)), b.Min.X+int(math.Round(float64(area.Max.X)/workScale)), b.Min.Y+int(math.Round(float64(area.Max.Y)/workScale)))
		radius := max(1, int(math.Ceil(float64(short)/float64(style.ref))))
		if platform == "千问" {
			radius = max(2, int(math.Ceil(5*float64(short)/float64(style.ref))))
		}
		if err := repairWatermark(ctx, dst, template, area, radius); err != nil {
			return nil, false, err
		}
	}
	if dst == nil {
		return nil, false, nil
	}
	return dst, true, nil
}

func watermarkHighPass(src image.Image) []float64 {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	integral := make([]float64, (w+1)*(h+1))
	gray := func(x, y int) float64 {
		r, g, bl, _ := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return float64(r+g+bl) / (3 * 65535)
	}
	for y := 0; y < h; y++ {
		row := 0.0
		for x := 0; x < w; x++ {
			row += gray(x, y)
			integral[(y+1)*(w+1)+x+1] = integral[y*(w+1)+x+1] + row
		}
	}
	hp := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			x0, x1 := max(0, x-3), min(w, x+4)
			y0, y1 := max(0, y-3), min(h, y+4)
			sum := integral[y1*(w+1)+x1] - integral[y0*(w+1)+x1] - integral[y1*(w+1)+x0] + integral[y0*(w+1)+x0]
			hp[y*w+x] = gray(x, y) - sum/float64((x1-x0)*(y1-y0))
		}
	}
	return hp
}

func matchWatermark(ctx context.Context, hp []float64, w, h int, template image.Image, style watermarkStyle) (image.Rectangle, float64, error) {
	best := 0.0
	var area image.Rectangle
	scale := float64(min(w, h)) / float64(style.ref)
	for _, factor := range []float64{0.96, 0.98, 1, 1.02, 1.04} {
		tw := max(3, int(math.Round(float64(template.Bounds().Dx())*scale*factor)))
		th := max(3, int(math.Round(float64(template.Bounds().Dy())*scale*factor)))
		tp := watermarkHighPass(resize(template, tw, th))
		total := 0.0
		for _, v := range tp {
			total += v * v
		}
		if total < 0.001 {
			continue
		}
		x := int(math.Round(float64(style.x) * scale))
		y := int(math.Round(float64(style.y) * scale))
		if !style.topLeft {
			x, y = w-x-tw, h-y-th
		}
		for dy := -4; dy <= 4; dy++ {
			if err := ctx.Err(); err != nil {
				return area, best, err
			}
			for dx := -4; dx <= 4; dx++ {
				x0, y0 := x+dx, y+dy
				if x0 < 1 || y0 < 1 || x0+tw >= w || y0+th >= h {
					continue
				}
				xy, yy := 0.0, 0.0
				for row := 0; row < th; row++ {
					for col := 0; col < tw; col++ {
						a, v := tp[row*tw+col], hp[(y0+row)*w+x0+col]
						xy += a * v
						yy += v * v
					}
				}
				if yy == 0 {
					continue
				}
				score := math.Abs(xy) / math.Sqrt(total*yy)
				if score > best {
					best, area = score, image.Rect(x0, y0, x0+tw, y0+th)
				}
			}
		}
	}
	return area, best, nil
}

func repairWatermark(ctx context.Context, dst *image.NRGBA, template image.Image, area image.Rectangle, radius int) error {
	maskImage := resize(template, area.Dx(), area.Dy())
	patch := area.Inset(-radius - 2).Intersect(dst.Bounds())
	w, h := patch.Dx(), patch.Dy()
	mask := make([]bool, w*h)
	for y := 0; y < area.Dy(); y++ {
		if y%16 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		for x := 0; x < area.Dx(); x++ {
			r, g, b, _ := maskImage.At(x, y).RGBA()
			if max(r, g, b) < 8*257 {
				continue
			}
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					p := image.Pt(area.Min.X+x+dx, area.Min.Y+y+dy)
					if p.In(patch) {
						mask[(p.Y-patch.Min.Y)*w+p.X-patch.Min.X] = true
					}
				}
			}
		}
	}
	values := make([][3]float64, w*h)
	var holes []int
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*w + x
			c := dst.NRGBAAt(patch.Min.X+x, patch.Min.Y+y)
			values[i] = [3]float64{float64(c.R), float64(c.G), float64(c.B)}
			if mask[i] {
				holes = append(holes, i)
			}
		}
	}
	// 从边界向内初始化，再只在字形蒙版内求离散调和背景，保留框外像素。
	known := make([]bool, len(mask))
	queue := make([]int, 0, len(mask))
	for i := range mask {
		known[i] = !mask[i]
		if known[i] {
			queue = append(queue, i)
		}
	}
	for pos := 0; pos < len(queue); pos++ {
		i := queue[pos]
		for _, j := range []int{i - w, i + w, i - 1, i + 1} {
			if j < 0 || j >= len(mask) || known[j] || (j/w != i/w && (j == i-1 || j == i+1)) {
				continue
			}
			values[j], known[j] = values[i], true
			queue = append(queue, j)
		}
	}
	for iteration := 0; iteration < 300; iteration++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		change := 0.0
		for _, i := range holes {
			if i < w || i >= len(mask)-w || i%w == 0 || i%w == w-1 {
				continue
			}
			for c := 0; c < 3; c++ {
				v := (values[i-w][c] + values[i+w][c] + values[i-1][c] + values[i+1][c]) / 4
				change = math.Max(change, math.Abs(v-values[i][c]))
				values[i][c] = v
			}
		}
		if change < 0.02 {
			break
		}
	}
	textureDY := -patch.Dy() - 8
	if patch.Min.Y+textureDY < dst.Bounds().Min.Y+2 {
		textureDY = patch.Dy() + 8
	}
	for _, i := range holes {
		x, y := patch.Min.X+i%w, patch.Min.Y+i/w
		c := dst.NRGBAAt(x, y)
		// 邻近未遮挡区域的高频亮度恢复细纹理，避免留下平滑字形轮廓。
		texture := 0.0
		q := image.Pt(x, y+textureDY)
		if q.In(dst.Bounds().Inset(2)) {
			mean := 0.0
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					v := dst.NRGBAAt(q.X+dx, q.Y+dy)
					mean += (float64(v.R) + float64(v.G) + float64(v.B)) / 3
				}
			}
			v := dst.NRGBAAt(q.X, q.Y)
			texture = math.Max(-18, math.Min(18, (float64(v.R)+float64(v.G)+float64(v.B))/3-mean/25))
		}
		component := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(255, v+texture)))) }
		c.R, c.G, c.B = component(values[i][0]), component(values[i][1]), component(values[i][2])
		dst.SetNRGBA(x, y, c)
	}
	return nil
}
