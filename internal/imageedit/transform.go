package imageedit

import (
	"image"
	"image/color"
	"image/draw"

	xdraw "golang.org/x/image/draw"
)

// toNRGBA 统一成 8 位 RGBA，后面的处理都在它上面做。
func toNRGBA(src image.Image) *image.NRGBA {
	if n, ok := src.(*image.NRGBA); ok {
		return n
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

func cropImage(src *image.NRGBA, x, y, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, image.Pt(x, y), draw.Src)
	return dst
}

// rotateImage 顺时针旋转。90/270 之后宽高互换。
func rotateImage(src *image.NRGBA, deg int) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	switch deg {
	case 90:
		// 顺时针 90°：目标宽高互换，源 (x,y) 落在 (h-1-y, x)。
		dst := image.NewNRGBA(image.Rect(0, 0, h, w))
		for dy := 0; dy < w; dy++ {
			for dx := 0; dx < h; dx++ {
				si := (h-1-dx)*src.Stride + dy*4
				di := dy*dst.Stride + dx*4
				copy(dst.Pix[di:di+4], src.Pix[si:si+4])
			}
		}
		return dst
	case 180:
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		for dy := 0; dy < h; dy++ {
			for dx := 0; dx < w; dx++ {
				si := (h-1-dy)*src.Stride + (w-1-dx)*4
				di := dy*dst.Stride + dx*4
				copy(dst.Pix[di:di+4], src.Pix[si:si+4])
			}
		}
		return dst
	case 270:
		// 顺时针 270°（逆时针 90°）：源 (x,y) 落在 (y, w-1-x)。
		dst := image.NewNRGBA(image.Rect(0, 0, h, w))
		for dy := 0; dy < w; dy++ {
			for dx := 0; dx < h; dx++ {
				si := dx*src.Stride + (w-1-dy)*4
				di := dy*dst.Stride + dx*4
				copy(dst.Pix[di:di+4], src.Pix[si:si+4])
			}
		}
		return dst
	default:
		return src
	}
}

// flipHorizontal 左右镜像；flipVertical 上下镜像。
func flipHorizontal(src *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		row := y * src.Stride
		for x := 0; x < w; x++ {
			si := row + (w-1-x)*4
			di := row + x*4
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

func flipVertical(src *image.NRGBA) *image.NRGBA {
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		si := (h - 1 - y) * src.Stride
		di := y * dst.Stride
		copy(dst.Pix[di:di+w*4], src.Pix[si:si+w*4])
	}
	return dst
}

func grayscaleImage(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := y*src.Stride + x*4
			r, g, b := int(src.Pix[i]), int(src.Pix[i+1]), int(src.Pix[i+2])
			v := uint8((2126*r + 7152*g + 722*b + 5000) / 10000)
			dst.Pix[i] = v
			dst.Pix[i+1] = v
			dst.Pix[i+2] = v
			dst.Pix[i+3] = src.Pix[i+3]
		}
	}
	return dst
}

func resizeImage(src *image.NRGBA, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst
}

// containImage 等比缩进目标画布并居中，空出来的地方铺白色。
func containImage(src *image.NRGBA, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	fillNRGBA(dst, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	nw, nh := containedSize(src.Bounds().Dx(), src.Bounds().Dy(), w, h)
	x := (w - nw) / 2
	y := (h - nh) / 2
	xdraw.BiLinear.Scale(dst, image.Rect(x, y, x+nw, y+nh), src, src.Bounds(), xdraw.Src, nil)
	return dst
}

// flattenWhite 把透明区域铺到白底上，JPEG 输出前使用。
func flattenWhite(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	fillNRGBA(dst, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)
	return dst
}

func fillNRGBA(dst *image.NRGBA, c color.NRGBA) {
	row := make([]byte, dst.Stride)
	for x := 0; x < dst.Bounds().Dx(); x++ {
		row[x*4] = c.R
		row[x*4+1] = c.G
		row[x*4+2] = c.B
		row[x*4+3] = c.A
	}
	for y := 0; y < dst.Bounds().Dy(); y++ {
		copy(dst.Pix[y*dst.Stride:(y+1)*dst.Stride], row)
	}
}
