// Package images 提供图片合法性校验、SHA256 去重与缩略图生成。
package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"
)

// IsValidImageBytes 校验字节流是否为 PNG/JPEG/GIF/WEBP 图片。
func IsValidImageBytes(b []byte) bool {
	switch {
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return true
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return true
	case len(b) >= 6 && string(b[:3]) == "GIF":
		return true
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return true
	}
	return false
}

// SHA256 计算字节流摘要（十六进制）。
func SHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Thumb 解码图片并缩放到 box 内（保持比例，双线性），失败返回原解码图。
func Thumb(path string, box int) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("解码 %s: %w", filepath.Base(path), err)
	}
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	if w <= box && h <= box {
		return img, nil
	}
	scale := float64(box) / float64(max(w, h))
	nw, nh := max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	return resize(img, nw, nh), nil
}

// resize 双线性缩放。
func resize(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	sw, sh := float64(sb.Dx()), float64(sb.Dy())
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// 目标像素中心映射回源坐标
			fx := (float64(x)+0.5)*sw/float64(w) - 0.5
			fy := (float64(y)+0.5)*sh/float64(h) - 0.5
			x0, y0 := int(fx), int(fy)
			tx, ty := fx-float64(x0), fy-float64(y0)
			x1, y1 := min(x0+1, sb.Dx()-1), min(y0+1, sb.Dy()-1)
			x0, y0 = clamp(x0, 0, sb.Dx()-1), clamp(y0, 0, sb.Dy()-1)
			c00, c10 := src.At(sb.Min.X+x0, sb.Min.Y+y0), src.At(sb.Min.X+x1, sb.Min.Y+y0)
			c01, c11 := src.At(sb.Min.X+x0, sb.Min.Y+y1), src.At(sb.Min.X+x1, sb.Min.Y+y1)
			dst.Set(x, y, bilinear(c00, c10, c01, c11, tx, ty))
		}
	}
	return dst
}

func bilinear(c00, c10, c01, c11 color.Color, tx, ty float64) color.RGBA {
	r00, g00, b00, a00 := c00.RGBA()
	r10, g10, b10, a10 := c10.RGBA()
	r01, g01, b01, a01 := c01.RGBA()
	r11, g11, b11, a11 := c11.RGBA()
	mixU := func(a, b uint32, t float64) float64 { return float64(a)*(1-t) + float64(b)*t }
	mix := func(a, b float64, t float64) float64 { return a*(1-t) + b*t }
	r := mix(mixU(r00, r10, tx), mixU(r01, r11, tx), ty) / 257
	g := mix(mixU(g00, g10, tx), mixU(g01, g11, tx), ty) / 257
	b := mix(mixU(b00, b10, tx), mixU(b01, b11, tx), ty) / 257
	a := mix(mixU(a00, a10, tx), mixU(a01, a11, tx), ty) / 257
	return color.RGBA{uint8(clamp(int(r), 0, 255)), uint8(clamp(int(g), 0, 255)), uint8(clamp(int(b), 0, 255)), uint8(clamp(int(a), 0, 255))}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// BaseName 不带扩展名的文件名。
func BaseName(path string) string {
	name := filepath.Base(path)
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// EncodePNG 编码为 PNG（用于测试/预览）。
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeJpegOrPng 解码字节流为图片。
func DecodeJpegOrPng(b []byte) (image.Image, error) {
	img, _, err := image.Decode(bytes.NewReader(b))
	if err == nil {
		return img, nil
	}
	// 兜底：尝试当 JPEG 强解
	img2, err2 := jpeg.Decode(bytes.NewReader(b))
	if err2 == nil {
		return img2, nil
	}
	return nil, err
}
