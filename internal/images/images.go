// Package images 提供图片合法性校验、SHA256 摘要、水印清理与缩略图编码。
package images

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// 界面图标和豆包的空 SVG 壳落成 PNG 后只有几百到一千字节，边长远小于生成图。
const (
	MinGeneratedBytes = 1536
	MinGeneratedSide  = 128
)

// AcceptGenerated 只认够大的位图。魔法数字对了但边长或体积像图标的，不当成出图结果。
func AcceptGenerated(b []byte) bool {
	if len(b) < MinGeneratedBytes || !IsValidImageBytes(b) {
		return false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width < MinGeneratedSide || cfg.Height < MinGeneratedSide {
		return false
	}
	return true
}

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

// Thumb 解码图片并缩放到 box 内（保持比例，双线性），失败返回错误。
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

// resize 双线性缩放（基于 x/image/draw 向量化路径，避免逐像素 interface 拆箱）。
func resize(src image.Image, w, h int) image.Image {
	sb := src.Bounds()
	if sb.Dx() == w && sb.Dy() == h && sb.Min == (image.Point{}) {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, sb, xdraw.Src, nil)
	return dst
}

// EncodePNG 编码为无损 PNG（使用 BestSpeed 提速 3-5 倍）。
func EncodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
