package imageedit

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/webp"
	"golang.org/x/image/bmp"
)

// sourceMeta 是解码前就能拿到的信息。
type sourceMeta struct {
	format string
	width  int
	height int
}

// inspect 只看文件头和图长宽，不解全图。和 moza 的 sharp metadata 一致。
func inspect(data []byte) (sourceMeta, error) {
	format := sniffFormat(data)
	if format == "" {
		return sourceMeta{}, errors.New("imageedit: inspect: unrecognized image format")
	}
	var (
		cfg image.Config
		err error
	)
	switch format {
	case "jpeg":
		cfg, err = jpeg.DecodeConfig(bytes.NewReader(data))
	case "png":
		cfg, err = png.DecodeConfig(bytes.NewReader(data))
	case "gif":
		cfg, err = gif.DecodeConfig(bytes.NewReader(data))
	case "webp":
		cfg, err = webp.DecodeConfig(bytes.NewReader(data))
	case "avif":
		cfg, err = avif.DecodeConfig(bytes.NewReader(data))
	case "bmp":
		cfg, err = bmp.DecodeConfig(bytes.NewReader(data))
	default:
		err = fmt.Errorf("unsupported format %s", format)
	}
	if err != nil {
		return sourceMeta{}, fmt.Errorf("imageedit: inspect: %w", err)
	}
	return sourceMeta{format: format, width: cfg.Width, height: cfg.Height}, nil
}

// sniffFormat 用文件头判断格式。AVIF 和 HEIC 都是 heif 容器，按 ftyp 品牌区分。
func sniffFormat(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "jpeg"
	case len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))):
		return "gif"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "webp"
	case len(data) >= 12 && bytes.Equal(data[4:8], []byte("ftyp")) && isAVIFBrands(data):
		return "avif"
	case len(data) >= 2 && data[0] == 'B' && data[1] == 'M':
		return "bmp"
	}
	return ""
}

// isAVIFBrands 读 ISO-BMFF 的 ftyp 品牌。
func isAVIFBrands(data []byte) bool {
	declared := int(uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3]))
	end := len(data)
	if declared >= 16 && declared < end {
		end = declared
	}
	brands := [][]byte{data[8:12]}
	for off := 16; off+4 <= end; off += 4 {
		brands = append(brands, data[off:off+4])
	}
	for _, b := range brands {
		if bytes.Equal(b, []byte("avif")) || bytes.Equal(b, []byte("avis")) {
			return true
		}
	}
	return false
}

// decodeSource 解出整张图。动图直接拒绝，避免压成一帧。
func decodeSource(data []byte, format string) (image.Image, error) {
	switch format {
	case "jpeg":
		return jpeg.Decode(bytes.NewReader(data))
	case "png":
		return png.Decode(bytes.NewReader(data))
	case "gif":
		all, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if len(all.Image) > 1 {
			return nil, ErrAnimatedGIF
		}
		return all.Image[0], nil
	case "webp":
		all, err := webp.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if len(all.Image) > 1 {
			return nil, ErrAnimatedGIF
		}
		return all.Image[0], nil
	case "avif":
		all, err := avif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if len(all.Image) > 1 {
			return nil, ErrAnimatedGIF
		}
		return all.Image[0], nil
	case "bmp":
		return bmp.Decode(bytes.NewReader(data))
	default:
		return nil, fmt.Errorf("imageedit: unsupported input format %q", format)
	}
}

// Info 返回图片格式和像素尺寸，只读文件头。
func Info(data []byte) (format string, width, height int, err error) {
	meta, err := inspect(data)
	if err != nil {
		return "", 0, 0, err
	}
	return meta.format, meta.width, meta.height, nil
}

// Load 把图片解码成 8 位 RGBA，界面预览和缩略图共用。
// JPEG、PNG、GIF（首帧）、WebP、AVIF、BMP 都可以读。
func Load(data []byte) (*image.NRGBA, error) {
	meta, err := inspect(data)
	if err != nil {
		return nil, err
	}
	img, err := decodeSource(data, meta.format)
	if err != nil {
		return nil, err
	}
	return toNRGBA(img), nil
}

func hasAlpha(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return !o.Opaque()
	}
	return true
}
