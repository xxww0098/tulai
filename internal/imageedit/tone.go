package imageedit

import (
	"image"
	"math"
)

// adjustTone 依次应用亮度、饱和度、对比度。数值都是 -100..100，0 不变。
func adjustTone(src *image.NRGBA, brightness, contrast, saturation int) {
	if brightness == 0 && contrast == 0 && saturation == 0 {
		return
	}
	bf := 1 + float64(brightness)/100
	sf := 1 + float64(saturation)/100
	gain := 1 + float64(contrast)/100
	offset := 128 * (1 - gain)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*src.Stride + x*4
			r := float64(src.Pix[i])
			g := float64(src.Pix[i+1])
			b := float64(src.Pix[i+2])
			if brightness != 0 {
				r *= bf
				g *= bf
				b *= bf
			}
			if saturation != 0 {
				gray := 0.2126*r + 0.7152*g + 0.0722*b
				r = gray + (r-gray)*sf
				g = gray + (g-gray)*sf
				b = gray + (b-gray)*sf
			}
			if contrast != 0 {
				r = r*gain + offset
				g = g*gain + offset
				b = b*gain + offset
			}
			src.Pix[i] = clampByte(r)
			src.Pix[i+1] = clampByte(g)
			src.Pix[i+2] = clampByte(b)
		}
	}
}

// blurImage 高斯模糊。小半径用精确卷积，大半径用三次盒式模糊近似，
// 这样 sigma 50 也不会慢到不可用。颜色按预乘 alpha 处理，透明边缘不发黑。
func blurImage(src *image.NRGBA, sigma float64) *image.NRGBA {
	if sigma <= 0 {
		return src
	}
	work := premultiply(src)
	var out *image.NRGBA
	if sigma < 2 {
		out = gaussianBlur(work, sigma, true)
	} else {
		radius := int(math.Round((math.Sqrt(4*sigma*sigma+1) - 1) / 2))
		if radius < 1 {
			radius = 1
		}
		out = work
		for i := 0; i < 3; i++ {
			out = boxBlurPass(out, radius, true)
			out = boxBlurPass(out, radius, false)
		}
	}
	return unpremultiply(out)
}

// sharpenImage 用 unsharp mask 提清晰度：原图加上与原图减模糊图的差。
func sharpenImage(src *image.NRGBA) *image.NRGBA {
	blurred := gaussianBlur(src, 1, false)
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(src.Bounds())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*src.Stride + x*4
			for c := 0; c < 3; c++ {
				v := float64(src.Pix[i+c])
				b := float64(blurred.Pix[i+c])
				dst.Pix[i+c] = clampByte(v + (v - b))
			}
			dst.Pix[i+3] = src.Pix[i+3]
		}
	}
	return dst
}

func premultiply(src *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*dst.Stride + x*4
			a := int(dst.Pix[i+3])
			if a == 255 {
				continue
			}
			dst.Pix[i] = uint8(int(dst.Pix[i]) * a / 255)
			dst.Pix[i+1] = uint8(int(dst.Pix[i+1]) * a / 255)
			dst.Pix[i+2] = uint8(int(dst.Pix[i+2]) * a / 255)
		}
	}
	return dst
}

func unpremultiply(src *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*src.Stride + x*4
			a := int(src.Pix[i+3])
			if a == 0 {
				src.Pix[i], src.Pix[i+1], src.Pix[i+2] = 0, 0, 0
				continue
			}
			if a == 255 {
				continue
			}
			for c := 0; c < 3; c++ {
				v := int(src.Pix[i+c]) * 255 / a
				if v > 255 {
					v = 255
				}
				src.Pix[i+c] = uint8(v)
			}
		}
	}
	return src
}

// gaussianBlur 可分离高斯卷积。clampEdges 为 true 时按预乘 alpha 的通道处理。
func gaussianBlur(src *image.NRGBA, sigma float64, premul bool) *image.NRGBA {
	radius := int(math.Ceil(sigma * 3))
	if radius < 1 {
		radius = 1
	}
	kernel := make([]float64, radius*2+1)
	var sum float64
	for i := -radius; i <= radius; i++ {
		v := math.Exp(-float64(i*i) / (2 * sigma * sigma))
		kernel[i+radius] = v
		sum += v
	}
	for i := range kernel {
		kernel[i] /= sum
	}
	tmp := image.NewNRGBA(src.Bounds())
	dst := image.NewNRGBA(src.Bounds())
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	// 横向
	for y := 0; y < h; y++ {
		row := y * src.Stride
		for x := 0; x < w; x++ {
			var acc [4]float64
			for k := -radius; k <= radius; k++ {
				sx := x + k
				if sx < 0 {
					sx = 0
				} else if sx >= w {
					sx = w - 1
				}
				i := row + sx*4
				wt := kernel[k+radius]
				acc[0] += float64(src.Pix[i]) * wt
				acc[1] += float64(src.Pix[i+1]) * wt
				acc[2] += float64(src.Pix[i+2]) * wt
				acc[3] += float64(src.Pix[i+3]) * wt
			}
			i := row + x*4
			for c := 0; c < 4; c++ {
				tmp.Pix[i+c] = clampByte(acc[c])
			}
		}
	}
	// 纵向
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var acc [4]float64
			for k := -radius; k <= radius; k++ {
				sy := y + k
				if sy < 0 {
					sy = 0
				} else if sy >= h {
					sy = h - 1
				}
				i := sy*tmp.Stride + x*4
				wt := kernel[k+radius]
				acc[0] += float64(tmp.Pix[i]) * wt
				acc[1] += float64(tmp.Pix[i+1]) * wt
				acc[2] += float64(tmp.Pix[i+2]) * wt
				acc[3] += float64(tmp.Pix[i+3]) * wt
			}
			i := y*dst.Stride + x*4
			for c := 0; c < 4; c++ {
				dst.Pix[i+c] = clampByte(acc[c])
			}
		}
	}
	return dst
}

// boxBlurPass 一次盒式模糊，横向或纵向，用滑动窗口求和，复杂度与半径无关。
func boxBlurPass(src *image.NRGBA, radius int, horizontal bool) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	window := radius*2 + 1
	if horizontal {
		for y := 0; y < h; y++ {
			row := y * src.Stride
			var acc [4]int
			for k := -radius; k <= radius; k++ {
				x := clampInt(k, 0, w-1)
				i := row + x*4
				for c := 0; c < 4; c++ {
					acc[c] += int(src.Pix[i+c])
				}
			}
			for x := 0; x < w; x++ {
				i := row + x*4
				for c := 0; c < 4; c++ {
					dst.Pix[i+c] = uint8(acc[c] / window)
				}
				xo := clampInt(x-radius, 0, w-1)
				xn := clampInt(x+radius+1, 0, w-1)
				io := row + xo*4
				in := row + xn*4
				for c := 0; c < 4; c++ {
					acc[c] += int(src.Pix[in+c]) - int(src.Pix[io+c])
				}
			}
		}
		return dst
	}
	for x := 0; x < w; x++ {
		var acc [4]int
		for k := -radius; k <= radius; k++ {
			y := clampInt(k, 0, h-1)
			i := y*src.Stride + x*4
			for c := 0; c < 4; c++ {
				acc[c] += int(src.Pix[i+c])
			}
		}
		for y := 0; y < h; y++ {
			i := y*dst.Stride + x*4
			for c := 0; c < 4; c++ {
				dst.Pix[i+c] = uint8(acc[c] / window)
			}
			yo := clampInt(y-radius, 0, h-1)
			yn := clampInt(y+radius+1, 0, h-1)
			io := yo*src.Stride + x*4
			in := yn*src.Stride + x*4
			for c := 0; c < 4; c++ {
				acc[c] += int(src.Pix[in+c]) - int(src.Pix[io+c])
			}
		}
	}
	return dst
}

func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
