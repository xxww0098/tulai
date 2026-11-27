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
	"sync"
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
	{"qwen-label.png", "即梦", 2048, 57, 57, true},
}

var (
	watermarkTplOnce sync.Once
	watermarkTplMap  map[string]image.Image
	watermarkTplErr  error
)

func loadWatermarkTemplate(file string) (image.Image, error) {
	watermarkTplOnce.Do(func() {
		watermarkTplMap = make(map[string]image.Image, len(watermarkStyles))
		for _, s := range watermarkStyles {
			if _, ok := watermarkTplMap[s.file]; ok {
				continue
			}
			data, err := watermarkFiles.ReadFile("watermarks/" + s.file)
			if err != nil {
				watermarkTplErr = fmt.Errorf("读取水印模板: %w", err)
				return
			}
			tpl, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				watermarkTplErr = fmt.Errorf("解析水印模板: %w", err)
				return
			}
			watermarkTplMap[s.file] = tpl
		}
	})
	if watermarkTplErr != nil {
		return nil, watermarkTplErr
	}
	tpl, ok := watermarkTplMap[file]
	if !ok {
		return nil, fmt.Errorf("读取水印模板: 未知模板 %s", file)
	}
	return tpl, nil
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
	if platform != "豆包" && platform != "千问" && platform != "千问国际" && platform != "即梦" {
		return nil, false, nil
	}
	isQwen := platform == "千问" || platform == "千问国际"
	isJimeng := platform == "即梦"
	stylePlatform := platform
	if isQwen {
		stylePlatform = "千问"
	}
	// 千问暂仅校准方图模板；豆包与即梦已开放 4:3、3:4、16:9、9:16、3:2、2:3 等全比例。
	if platform != "豆包" && !isJimeng && b.Dx() != b.Dy() {
		return nil, false, nil
	}
	// 16 位图片保持原样，避免另存 8 位 PNG 时降低原图精度。
	switch src.ColorModel() {
	case color.Gray16Model, color.RGBA64Model, color.NRGBA64Model:
		return nil, false, nil
	}
	short := min(b.Dx(), b.Dy())
	maxEdge, threshold := 2048, 0.85
	if isQwen || isJimeng {
		// 千问与即梦在不同输出尺寸重新栅格化「AI生成」字形，缩小后比较轮廓可减轻像素相位差。
		maxEdge, threshold = 768, 0.60
	}
	workScale := math.Min(1, float64(maxEdge)/float64(max(b.Dx(), b.Dy())))
	w, h := int(math.Round(float64(b.Dx())*workScale)), int(math.Round(float64(b.Dy())*workScale))
	var dst *image.NRGBA
	for _, style := range watermarkStyles {
		if style.platform != stylePlatform {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		template, err := loadWatermarkTemplate(style.file)
		if err != nil {
			return nil, false, err
		}
		var (
			area  image.Rectangle
			score float64
		)
		if isJimeng {
			area, score, err = matchJimengTopLeftWatermark(ctx, src, w, h, template, style)
		} else {
			area, score, err = matchWatermarkInCorner(ctx, src, w, h, template, style)
		}
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
		if isQwen {
			radius = max(2, int(math.Ceil(5*float64(short)/float64(style.ref))))
		} else if isJimeng {
			// 即梦在不同画幅下水印可能按长边或短边缩放，按实际命中的水印高度比例计算膨胀半径。
			radius = max(2, int(math.Ceil(5*float64(area.Dy())/float64(template.Bounds().Dy()))))
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

// extractGrayBuffer 提取任意 image.Image 的 [0,1] 归一化灰度数组，对常见像素格式走零接口拆箱快路径。
func extractGrayBuffer(src image.Image, rect image.Rectangle) []float64 {
	w, h := rect.Dx(), rect.Dy()
	gray := make([]float64, w*h)
	const inv255x3 = 1.0 / (3.0 * 255.0)
	const inv65535x3 = 1.0 / (3.0 * 65535.0)
	switch img := src.(type) {
	case *image.NRGBA:
		for y := 0; y < h; y++ {
			rowOff := (rect.Min.Y+y-img.Rect.Min.Y)*img.Stride + (rect.Min.X-img.Rect.Min.X)*4
			dstOff := y * w
			for x := 0; x < w; x++ {
				i := rowOff + x*4
				a := float64(img.Pix[i+3])
				if a == 255 {
					gray[dstOff+x] = float64(int(img.Pix[i])+int(img.Pix[i+1])+int(img.Pix[i+2])) * inv255x3
				} else if a > 0 {
					// 与 NRGBA.RGBA() 预乘语义保持一致
					scale := a / (255.0 * 255.0 * 3.0)
					gray[dstOff+x] = float64(int(img.Pix[i])+int(img.Pix[i+1])+int(img.Pix[i+2])) * scale
				}
			}
		}
		return gray
	case *image.RGBA:
		for y := 0; y < h; y++ {
			rowOff := (rect.Min.Y+y-img.Rect.Min.Y)*img.Stride + (rect.Min.X-img.Rect.Min.X)*4
			dstOff := y * w
			for x := 0; x < w; x++ {
				i := rowOff + x*4
				gray[dstOff+x] = float64(int(img.Pix[i])+int(img.Pix[i+1])+int(img.Pix[i+2])) * inv255x3
			}
		}
		return gray
	case *image.Gray:
		for y := 0; y < h; y++ {
			rowOff := (rect.Min.Y+y-img.Rect.Min.Y)*img.Stride + (rect.Min.X - img.Rect.Min.X)
			dstOff := y * w
			for x := 0; x < w; x++ {
				gray[dstOff+x] = float64(img.Pix[rowOff+x]) / 255.0
			}
		}
		return gray
	case *image.YCbCr:
		for y := 0; y < h; y++ {
			sy := rect.Min.Y + y
			dstOff := y * w
			for x := 0; x < w; x++ {
				sx := rect.Min.X + x
				yi := img.YOffset(sx, sy)
				ci := img.COffset(sx, sy)
				r, g, bl := color.YCbCrToRGB(img.Y[yi], img.Cb[ci], img.Cr[ci])
				gray[dstOff+x] = float64(int(r)+int(g)+int(bl)) * inv255x3
			}
		}
		return gray
	default:
		for y := 0; y < h; y++ {
			dstOff := y * w
			for x := 0; x < w; x++ {
				r, g, bl, _ := src.At(rect.Min.X+x, rect.Min.Y+y).RGBA()
				gray[dstOff+x] = float64(r+g+bl) * inv65535x3
			}
		}
		return gray
	}
}

func highPassFromGray(gray []float64, w, h int) []float64 {
	stride := w + 1
	integral := make([]float64, stride*(h+1))
	for y := 0; y < h; y++ {
		row := 0.0
		srcOff := y * w
		prevRow := y * stride
		curRow := (y + 1) * stride
		for x := 0; x < w; x++ {
			row += gray[srcOff+x]
			integral[curRow+x+1] = integral[prevRow+x+1] + row
		}
	}
	hp := make([]float64, w*h)
	for y := 0; y < h; y++ {
		y0, y1 := max(0, y-3), min(h, y+4)
		i0, i1 := y0*stride, y1*stride
		rowOff := y * w
		for x := 0; x < w; x++ {
			x0, x1 := max(0, x-3), min(w, x+4)
			sum := integral[i1+x1] - integral[i0+x1] - integral[i1+x0] + integral[i0+x0]
			hp[rowOff+x] = gray[rowOff+x] - sum/float64((x1-x0)*(y1-y0))
		}
	}
	return hp
}

func watermarkHighPass(src image.Image) []float64 {
	b := src.Bounds()
	return highPassFromGray(extractGrayBuffer(src, b), b.Dx(), b.Dy())
}

// extractWorkROIGray 仅对工作分辨率下角落 ROI 区域采样并提取灰度，避免对整幅 2K/4K 大图做缩放与高通滤波。
func extractWorkROIGray(src image.Image, w, h int, roi image.Rectangle) []float64 {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == w && sh == h {
		return extractGrayBuffer(src, roi.Add(b.Min))
	}
	rw, rh := roi.Dx(), roi.Dy()
	gray := make([]float64, rw*rh)
	// 先裁剪包含 ROI 所需的源图子块，用 x/image/draw 双线性直接缩放到 ROI 尺寸。
	sx0 := max(0, int(math.Floor(float64(roi.Min.X)*float64(sw)/float64(w))))
	sy0 := max(0, int(math.Floor(float64(roi.Min.Y)*float64(sh)/float64(h))))
	sx1 := min(sw, int(math.Ceil(float64(roi.Max.X)*float64(sw)/float64(w))))
	sy1 := min(sh, int(math.Ceil(float64(roi.Max.Y)*float64(sh)/float64(h))))
	if sx1 <= sx0 || sy1 <= sy0 {
		return gray
	}
	// 使用精确像素中心双线性映射到源图灰度，与全图 resize(src, w, h) 在 ROI 内保持一致。
	subRect := image.Rect(b.Min.X+sx0, b.Min.Y+sy0, b.Min.X+sx1, b.Min.Y+sy1)
	subGray := extractGrayBuffer(src, subRect)
	subW, subH := subRect.Dx(), subRect.Dy()
	fsw, fsh := float64(sw), float64(sh)
	fw, fh := float64(w), float64(h)
	for ry := 0; ry < rh; ry++ {
		fy := (float64(roi.Min.Y+ry)+0.5)*fsh/fh - 0.5 - float64(sy0)
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		y1 := y0 + 1
		if y0 < 0 {
			y0, ty = 0, 0
		} else if y0 >= subH {
			y0, ty = subH-1, 0
		}
		if y1 < 0 {
			y1 = 0
		} else if y1 >= subH {
			y1 = subH - 1
		}
		row0, row1 := y0*subW, y1*subW
		dstOff := ry * rw
		for rx := 0; rx < rw; rx++ {
			fx := (float64(roi.Min.X+rx)+0.5)*fsw/fw - 0.5 - float64(sx0)
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			x1 := x0 + 1
			if x0 < 0 {
				x0, tx = 0, 0
			} else if x0 >= subW {
				x0, tx = subW-1, 0
			}
			if x1 < 0 {
				x1 = 0
			} else if x1 >= subW {
				x1 = subW - 1
			}
			top := subGray[row0+x0]*(1-tx) + subGray[row0+x1]*tx
			bot := subGray[row1+x0]*(1-tx) + subGray[row1+x1]*tx
			gray[dstOff+rx] = top*(1-ty) + bot*ty
		}
	}
	return gray
}

func matchWatermarkInCorner(ctx context.Context, src image.Image, w, h int, template image.Image, style watermarkStyle) (image.Rectangle, float64, error) {
	scale := float64(min(w, h)) / float64(style.ref)
	factors := []float64{0.96, 0.98, 1, 1.02, 1.04}
	shift := 4
	maxFactor := 1.04
	if w != h {
		factors = []float64{0.88, 0.92, 0.96, 0.98, 1, 1.02, 1.04, 1.08, 1.12}
		shift = 8
		maxFactor = 1.12
	}
	maxTW := max(3, int(math.Round(float64(template.Bounds().Dx())*scale*maxFactor)))
	maxTH := max(3, int(math.Round(float64(template.Bounds().Dy())*scale*maxFactor)))
	bx := int(math.Round(float64(style.x) * scale))
	by := int(math.Round(float64(style.y) * scale))
	// 留出 7x7 高通盒滤半径(3) + 位移余量，确保边界像素与全图高通一致。
	const pad = 4
	var roi image.Rectangle
	if style.topLeft {
		roi = image.Rect(max(0, bx-shift-pad), max(0, by-shift-pad), min(w, bx+maxTW+shift+pad), min(h, by+maxTH+shift+pad))
	} else {
		roi = image.Rect(max(0, w-bx-maxTW-shift-pad), max(0, h-by-maxTH-shift-pad), min(w, w-bx+shift+pad), min(h, h-by+shift+pad))
	}
	if roi.Empty() {
		return image.Rectangle{}, 0, nil
	}
	roiGray := extractWorkROIGray(src, w, h, roi)
	hp := highPassFromGray(roiGray, roi.Dx(), roi.Dy())
	return matchWatermarkWindow(ctx, hp, roi, w, h, template, style, factors, shift)
}

func matchWatermark(ctx context.Context, hp []float64, w, h int, template image.Image, style watermarkStyle) (image.Rectangle, float64, error) {
	factors := []float64{0.96, 0.98, 1, 1.02, 1.04}
	shift := 4
	if w != h {
		factors = []float64{0.88, 0.92, 0.96, 0.98, 1, 1.02, 1.04, 1.08, 1.12}
		shift = 8
	}
	return matchWatermarkWindow(ctx, hp, image.Rect(0, 0, w, h), w, h, template, style, factors, shift)
}

func matchWatermarkWindow(ctx context.Context, hp []float64, roi image.Rectangle, w, h int, template image.Image, style watermarkStyle, factors []float64, shift int) (image.Rectangle, float64, error) {
	best := 0.0
	var area image.Rectangle
	scale := float64(min(w, h)) / float64(style.ref)
	rw := roi.Dx()
	for _, factor := range factors {
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
		for dy := -shift; dy <= shift; dy++ {
			if err := ctx.Err(); err != nil {
				return area, best, err
			}
			for dx := -shift; dx <= shift; dx++ {
				x0, y0 := x+dx, y+dy
				if x0 < 1 || y0 < 1 || x0+tw >= w || y0+th >= h {
					continue
				}
				if x0 < roi.Min.X || y0 < roi.Min.Y || x0+tw > roi.Max.X || y0+th > roi.Max.Y {
					continue
				}
				rx0, ry0 := x0-roi.Min.X, y0-roi.Min.Y
				xy, yy := 0.0, 0.0
				for row := 0; row < th; row++ {
					tpRow := tp[row*tw : (row+1)*tw]
					hpRow := hp[(ry0+row)*rw+rx0 : (ry0+row)*rw+rx0+tw]
					for col := 0; col < tw; col++ {
						a, v := tpRow[col], hpRow[col]
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

func signedHighPassCorr(tp []float64, tw int, hp []float64, rw, x0, y0, ix0, iy0, ix1, iy1 int) float64 {
	xy, tt, yy := 0.0, 0.0, 0.0
	for row := iy0; row < iy1; row++ {
		tpRow := tp[row*tw:]
		hpRow := hp[(y0+row)*rw+x0:]
		for col := ix0; col < ix1; col++ {
			a, v := tpRow[col], hpRow[col]
			xy += a * v
			tt += a * a
			yy += v * v
		}
	}
	if tt < 1e-6 || yy < 1e-6 {
		return 0
	}
	return xy / math.Sqrt(tt*yy)
}

// matchJimengTopLeftWatermark 匹配即梦左上角「AI生成」圆角标签水印。
// 即梦在不同画幅（如 3:4、4:3、16:9、9:16、1:1）下会按长边、几何均值或短边缩放该标识，
// 因此同时覆盖三组基准尺度，并用「整框 + 内部字形同极性」双重相关拦截空白方框误报。
func matchJimengTopLeftWatermark(ctx context.Context, src image.Image, w, h int, template image.Image, style watermarkStyle) (image.Rectangle, float64, error) {
	tplW := float64(template.Bounds().Dx())
	tplH := float64(template.Bounds().Dy())
	ref := float64(style.ref)
	longEdge := float64(max(w, h))
	shortEdge := float64(min(w, h))
	baseScales := []float64{shortEdge / ref, math.Sqrt(float64(w*h)) / ref, longEdge / ref}
	factors := []float64{0.90, 0.95, 1.00, 1.05, 1.10}
	if w == h {
		baseScales = []float64{shortEdge / ref}
		factors = []float64{0.92, 0.96, 1.00, 1.04, 1.08}
	}
	seenTW := make(map[int]bool, len(baseScales)*len(factors))
	widths := make([]int, 0, len(baseScales)*len(factors))
	maxTW, maxTH := 0, 0
	for _, bs := range baseScales {
		for _, f := range factors {
			tw := max(16, int(math.Round(tplW*bs*f)))
			if seenTW[tw] || seenTW[tw-1] || seenTW[tw+1] {
				continue
			}
			th := max(8, int(math.Round(float64(tw)*tplH/tplW)))
			if tw+4 >= w || th+4 >= h {
				continue
			}
			seenTW[tw] = true
			widths = append(widths, tw)
			if tw > maxTW {
				maxTW = tw
			}
			if th > maxTH {
				maxTH = th
			}
		}
	}
	if len(widths) == 0 {
		return image.Rectangle{}, 0, nil
	}
	maxOff := max(12, int(math.Round(0.048*longEdge)))
	const pad = 4
	roi := image.Rect(0, 0, min(w, maxOff+maxTW+pad), min(h, maxOff+maxTH+pad))
	if roi.Empty() {
		return image.Rectangle{}, 0, nil
	}
	roiGray := extractWorkROIGray(src, w, h, roi)
	rw, rh := roi.Dx(), roi.Dy()
	hp := highPassFromGray(roiGray, rw, rh)

	var bestArea image.Rectangle
	bestRank := 0.0
	bestOut := 0.0
	for _, tw := range widths {
		if err := ctx.Err(); err != nil {
			return bestArea, bestOut, err
		}
		th := max(8, int(math.Round(float64(tw)*tplH/tplW)))
		if tw+2 >= rw || th+2 >= rh {
			continue
		}
		tp := watermarkHighPass(resize(template, tw, th))
		total := 0.0
		for _, v := range tp {
			total += v * v
		}
		if total < 0.001 {
			continue
		}
		limX := min(maxOff, rw-tw-2)
		limY := min(maxOff, rh-th-2)
		if limX < 1 || limY < 1 {
			continue
		}
		ix0 := max(1, int(math.Round(float64(tw)*0.12)))
		ix1 := min(tw-1, int(math.Round(float64(tw)*0.88)))
		iy0 := max(1, int(math.Round(float64(th)*0.18)))
		iy1 := min(th-1, int(math.Round(float64(th)*0.82)))
		// 全像素（1px 步长）遍历左上角窗口，避免细笔画在 2px 粗步长下恰好落入高通负瓣导致漏检。
		for y0 := 1; y0 <= limY; y0++ {
			if err := ctx.Err(); err != nil {
				return bestArea, bestOut, err
			}
			for x0 := 1; x0 <= limX; x0++ {
				xy, yy := 0.0, 0.0
				for row := 0; row < th; row++ {
					tpRow := tp[row*tw : (row+1)*tw]
					hpRow := hp[(y0+row)*rw+x0 : (y0+row)*rw+x0+tw]
					for col := 0; col < tw; col++ {
						a, v := tpRow[col], hpRow[col]
						xy += a * v
						yy += v * v
					}
				}
				if yy < 1e-6 {
					continue
				}
				fullS := xy / math.Sqrt(total*yy)
				absFull := math.Abs(fullS)
				if absFull < 0.50 {
					continue
				}
				innerS := signedHighPassCorr(tp, tw, hp, rw, x0, y0, ix0, iy0, ix1, iy1)
				if fullS*innerS <= 0 {
					continue
				}
				absInner := math.Abs(innerS)
				// 同时满足整框与内部字形同极性高相关，避免左上角恰好有空白圆角方框时误判。
				valid := (absFull >= 0.60 && absInner >= 0.45) || (absFull >= 0.50 && absInner >= 0.60)
				if !valid {
					continue
				}
				rank := 0.6*absFull + 0.4*absInner
				if rank > bestRank {
					bestRank = rank
					bestOut = math.Max(absFull, absInner)
					bestArea = image.Rect(x0, y0, x0+tw, y0+th)
				}
			}
		}
	}
	return bestArea, bestOut, nil
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
