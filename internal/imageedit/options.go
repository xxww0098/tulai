// Package imageedit 是纯 Go 的图片批处理引擎：格式转换、裁剪、旋转翻转、
// 改尺寸、亮度/对比度/饱和度、模糊和锐化。
//
// 选项校验与处理顺序对齐 moza 的 imageedit(sharp)实现：先裁剪，再旋转翻转和
// 灰度，然后改尺寸，最后调整色调并编码。JPEG 输出把透明区域铺成白色。
package imageedit

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// MaxPixels 是单张图片的处理上限，和 moza 一致。
const MaxPixels = 50_000_000

var (
	// ErrAnimatedGIF 表示输入是动图。动图不能悄悄压成一帧。
	ErrAnimatedGIF = errors.New("imageedit: animated GIF editing is not supported")
	// ErrImageTooLarge 表示图片超过 5000 万像素。
	ErrImageTooLarge = errors.New("imageedit: image exceeds the 50 megapixel processing limit")
)

// Options 是界面和配方保存的原始选项。空值采用默认：格式保持原样、
// 质量 90、裁剪居中、缩放 inside。
type Options struct {
	Format       string `json:"format,omitempty"`
	CropWidth    int    `json:"crop_width,omitempty"`
	CropHeight   int    `json:"crop_height,omitempty"`
	CropPosition string `json:"crop_position,omitempty"`
	CropRatio    string `json:"crop_ratio,omitempty"`
	// CropLeft/CropTop 是裁剪框在源图上的绝对位置。nil 表示按 CropPosition 定位。
	CropLeft       *int    `json:"crop_left,omitempty"`
	CropTop        *int    `json:"crop_top,omitempty"`
	ResizeWidth    int     `json:"resize_width,omitempty"`
	ResizeHeight   int     `json:"resize_height,omitempty"`
	ResizeMode     string  `json:"resize_mode,omitempty"`
	Rotate         int     `json:"rotate,omitempty"`
	FlipHorizontal bool    `json:"flip_horizontal,omitempty"`
	FlipVertical   bool    `json:"flip_vertical,omitempty"`
	Grayscale      bool    `json:"grayscale,omitempty"`
	JPEGQuality    int     `json:"jpeg_quality,omitempty"`
	Brightness     int     `json:"brightness,omitempty"`
	Contrast       int     `json:"contrast,omitempty"`
	Saturation     int     `json:"saturation,omitempty"`
	Blur           float64 `json:"blur,omitempty"`
	Sharpen        bool    `json:"sharpen,omitempty"`
}

// Int 返回指向 v 的指针，方便填写裁剪偏移。
func Int(v int) *int { return &v }

// Normalized 校验并补全后的选项。
type Normalized struct {
	Format         string // original / jpeg / png / webp / avif
	CropWidth      int
	CropHeight     int
	CropPosition   string
	CropRatio      string
	CropLeft       int // -1 表示按 CropPosition 定位
	CropTop        int
	ResizeWidth    int
	ResizeHeight   int
	ResizeMode     string // inside / fill / contain
	Rotate         int
	FlipHorizontal bool
	FlipVertical   bool
	Grayscale      bool
	JPEGQuality    int
	Brightness     int
	Contrast       int
	Saturation     int
	Blur           float64
	Sharpen        bool
}

// Normalize 校验选项并返回规范形式。
func Normalize(o Options) (Normalized, error) {
	out := Normalized{
		Format:         "original",
		CropWidth:      o.CropWidth,
		CropHeight:     o.CropHeight,
		CropPosition:   "center",
		CropLeft:       -1,
		CropTop:        -1,
		ResizeWidth:    o.ResizeWidth,
		ResizeHeight:   o.ResizeHeight,
		ResizeMode:     "inside",
		Rotate:         o.Rotate,
		FlipHorizontal: o.FlipHorizontal,
		FlipVertical:   o.FlipVertical,
		Grayscale:      o.Grayscale,
		JPEGQuality:    o.JPEGQuality,
		Brightness:     o.Brightness,
		Contrast:       o.Contrast,
		Saturation:     o.Saturation,
		Blur:           o.Blur,
		Sharpen:        o.Sharpen,
	}
	if o.CropLeft != nil {
		out.CropLeft = *o.CropLeft
	}
	if o.CropTop != nil {
		out.CropTop = *o.CropTop
	}

	switch strings.ToLower(strings.TrimSpace(o.Format)) {
	case "", "original":
		out.Format = "original"
	case "jpg", "jpeg":
		out.Format = "jpeg"
	case "png":
		out.Format = "png"
	case "webp":
		out.Format = "webp"
	case "avif":
		out.Format = "avif"
	default:
		return out, errors.New("imageedit: format must be original, jpeg, png, webp, or avif")
	}
	if out.CropWidth < 0 || out.CropWidth > 65535 || out.CropHeight < 0 || out.CropHeight > 65535 {
		return out, errors.New("imageedit: crop dimensions must be between 0 and 65535")
	}
	if out.CropLeft < -1 || out.CropLeft > 65535 || out.CropTop < -1 || out.CropTop > 65535 {
		return out, errors.New("imageedit: crop offsets must be -1 or between 0 and 65535")
	}
	if (out.CropLeft >= 0) != (out.CropTop >= 0) {
		return out, errors.New("imageedit: crop_left and crop_top must be set together")
	}
	if out.ResizeWidth < 0 || out.ResizeWidth > 65535 || out.ResizeHeight < 0 || out.ResizeHeight > 65535 {
		return out, errors.New("imageedit: resize dimensions must be between 0 and 65535")
	}

	switch strings.ToLower(strings.TrimSpace(o.CropPosition)) {
	case "", "center":
		out.CropPosition = "center"
	case "top", "bottom", "left", "right",
		"top-left", "top-right", "bottom-left", "bottom-right":
		out.CropPosition = strings.ToLower(strings.TrimSpace(o.CropPosition))
	default:
		return out, errors.New("imageedit: invalid crop position")
	}

	if ratio := strings.TrimSpace(o.CropRatio); ratio != "" {
		rw, rh, ok := parseRatio(ratio)
		if !ok {
			return out, errors.New(`imageedit: crop_ratio must be W:H like "16:9"`)
		}
		if rw <= 0 || rh <= 0 {
			return out, errors.New("imageedit: crop_ratio must be positive")
		}
		out.CropRatio = formatRatio(rw) + ":" + formatRatio(rh)
	}

	switch strings.ToLower(strings.TrimSpace(o.ResizeMode)) {
	case "", "inside":
		out.ResizeMode = "inside"
	case "fill", "contain":
		out.ResizeMode = strings.ToLower(strings.TrimSpace(o.ResizeMode))
	default:
		return out, errors.New("imageedit: resize_mode must be inside, fill, or contain")
	}

	if out.Rotate != 0 && out.Rotate != 90 && out.Rotate != 180 && out.Rotate != 270 {
		return out, errors.New("imageedit: rotate must be 0, 90, 180, or 270")
	}
	if out.JPEGQuality == 0 {
		out.JPEGQuality = 90
	}
	if out.JPEGQuality < 1 || out.JPEGQuality > 100 {
		return out, errors.New("imageedit: JPEG quality must be between 1 and 100")
	}
	for _, v := range []struct {
		name  string
		value int
	}{
		{"brightness", out.Brightness},
		{"contrast", out.Contrast},
		{"saturation", out.Saturation},
	} {
		if v.value < -100 || v.value > 100 {
			return out, fmt.Errorf("imageedit: %s must be an integer between -100 and 100", v.name)
		}
	}
	if math.IsNaN(out.Blur) || out.Blur < 0 || (out.Blur > 0 && out.Blur < 0.3) || out.Blur > 50 {
		return out, errors.New("imageedit: blur must be 0 or a sigma between 0.3 and 50")
	}
	return out, nil
}

// Enabled 判断这组选项是否要动图片。跟 moza 的判断一致。
func Enabled(o Options) bool {
	format := strings.ToLower(strings.TrimSpace(o.Format))
	return (format != "" && format != "original") ||
		o.CropWidth > 0 || o.CropHeight > 0 ||
		strings.TrimSpace(o.CropRatio) != "" ||
		(o.CropLeft != nil && o.CropTop != nil && *o.CropLeft >= 0 && *o.CropTop >= 0) ||
		o.ResizeWidth > 0 || o.ResizeHeight > 0 ||
		o.Rotate != 0 || o.FlipHorizontal || o.FlipVertical || o.Grayscale ||
		(o.JPEGQuality != 0 && o.JPEGQuality != 90) ||
		o.Brightness != 0 || o.Contrast != 0 || o.Saturation != 0 ||
		o.Blur > 0 || o.Sharpen
}

// OutputExtension 返回编码结果对应的扩展名（不带点）。
func OutputExtension(n Normalized, sourceExt string) string {
	switch n.Format {
	case "jpeg":
		return "jpg"
	case "png":
		return "png"
	case "webp":
		return "webp"
	case "avif":
		return "avif"
	default:
		ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(sourceExt), "."))
		if ext == "jpeg" {
			ext = "jpg"
		}
		return ext
	}
}

// DefaultCropPosition 把九宫格位置当作裁剪锚点。
func DefaultCropPosition() string { return "center" }

func parseRatio(s string) (float64, float64, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, false
	}
	rw, err := strconv.ParseFloat(parts[0], 64)
	if err != nil || strings.HasPrefix(parts[0], "+") {
		return 0, 0, false
	}
	rh, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || strings.HasPrefix(parts[1], "+") {
		return 0, 0, false
	}
	return rw, rh, true
}

func formatRatio(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func containedSize(width, height, maxWidth, maxHeight int) (int, int) {
	if maxWidth == 0 {
		maxWidth = width
	}
	if maxHeight == 0 {
		maxHeight = height
	}
	if width <= maxWidth && height <= maxHeight {
		return width, height
	}
	scale := float64(maxWidth) / float64(width)
	if other := float64(maxHeight) / float64(height); other < scale {
		scale = other
	}
	nw := max(1, int(math.Floor(float64(width)*scale+0.5)))
	nh := max(1, int(math.Floor(float64(height)*scale+0.5)))
	return nw, nh
}

func cropOffset(sourceWidth, sourceHeight, width, height int, position string) (int, int) {
	x := (sourceWidth - width) / 2
	y := (sourceHeight - height) / 2
	switch {
	case strings.Contains(position, "left"):
		x = 0
	case strings.Contains(position, "right"):
		x = sourceWidth - width
	}
	switch {
	case strings.Contains(position, "top"):
		y = 0
	case strings.Contains(position, "bottom"):
		y = sourceHeight - height
	}
	return x, y
}
