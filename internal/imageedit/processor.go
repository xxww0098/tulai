package imageedit

import (
	"context"
	"errors"
	"fmt"
	"image"
	"math"
)

// Result 是一张图处理后的编码结果。
type Result struct {
	Data         []byte
	Format       string // jpeg / png / gif / webp / avif
	Width        int
	Height       int
	SourceFormat string
}

// Process 解码 source，按 options 处理并编码。
func Process(ctx context.Context, source []byte, options Options) (*Result, error) {
	img, sourceFormat, err := render(ctx, source, options)
	if err != nil {
		return nil, err
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	n, err := Normalize(options)
	if err != nil {
		return nil, err
	}
	target := n.Format
	if target == "original" {
		target = sourceFormat
	}
	data, err := encodeImage(img, target, n.JPEGQuality)
	if err != nil {
		return nil, err
	}
	return &Result{
		Data:         data,
		Format:       target,
		Width:        img.Bounds().Dx(),
		Height:       img.Bounds().Dy(),
		SourceFormat: sourceFormat,
	}, nil
}

// Preview 生成界面预览：按选项处理后等比缩到 maxSide 内，再编码成 JPEG。
// 和 moza 的预览一致，不放大。
func Preview(ctx context.Context, source []byte, options Options, maxSide int) ([]byte, error) {
	if maxSide <= 0 {
		maxSide = 1000
	}
	img, _, err := render(ctx, source, options)
	if err != nil {
		return nil, err
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w > maxSide || h > maxSide {
		nw, nh := containedSize(w, h, maxSide, maxSide)
		img = resizeImage(img, nw, nh)
	}
	var buf []byte
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	buf, err = encodeImage(img, "jpeg", 78)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// Thumbnail 生成方形缩略图：短边缩到 box，再居中裁成正方形。
func Thumbnail(source []byte, box int) (*image.NRGBA, error) {
	meta, err := inspect(source)
	if err != nil {
		return nil, err
	}
	img, err := decodeSource(source, meta.format)
	if err != nil {
		return nil, err
	}
	n := toNRGBA(img)
	w, h := n.Bounds().Dx(), n.Bounds().Dy()
	scale := float64(box) / float64(min(w, h))
	nw := max(box, int(math.Floor(float64(w)*scale+0.5)))
	nh := max(box, int(math.Floor(float64(h)*scale+0.5)))
	if nw != w || nh != h {
		n = resizeImage(n, nw, nh)
	}
	x := (n.Bounds().Dx() - box) / 2
	y := (n.Bounds().Dy() - box) / 2
	return cropImage(n, x, y, box, box), nil
}

// render 跑完整处理流程，返回还没编码的图。
func render(ctx context.Context, source []byte, options Options) (*image.NRGBA, string, error) {
	if err := checkContext(ctx); err != nil {
		return nil, "", err
	}
	n, err := Normalize(options)
	if err != nil {
		return nil, "", err
	}
	meta, err := inspect(source)
	if err != nil {
		return nil, "", err
	}
	srcWidth, srcHeight := meta.width, meta.height
	if srcWidth <= 0 || srcHeight <= 0 || srcWidth > MaxPixels/srcHeight {
		return nil, "", fmt.Errorf("%w: %dx%d", ErrImageTooLarge, srcWidth, srcHeight)
	}
	if n.CropWidth > srcWidth || n.CropHeight > srcHeight {
		return nil, "", fmt.Errorf(
			"imageedit: crop %dx%d exceeds source %dx%d",
			n.CropWidth, n.CropHeight, srcWidth, srcHeight,
		)
	}

	decoded, err := decodeSource(source, meta.format)
	if err != nil {
		if errors.Is(err, ErrAnimatedGIF) {
			return nil, "", ErrAnimatedGIF
		}
		return nil, "", fmt.Errorf("imageedit: decode: %w", err)
	}
	img := toNRGBA(decoded)
	width, height := srcWidth, srcHeight

	// 1. 裁剪。只给比例时取居中最大矩形。
	cropW := width
	cropH := height
	if n.CropWidth > 0 {
		cropW = n.CropWidth
	}
	if n.CropHeight > 0 {
		cropH = n.CropHeight
	}
	if n.CropRatio != "" && n.CropWidth <= 0 && n.CropHeight <= 0 {
		rw, rh, ok := parseRatio(n.CropRatio)
		if ok && rh != 0 {
			r := rw / rh
			if float64(srcWidth)/float64(srcHeight) > r {
				cropH = srcHeight
				cropW = max(1, int(math.Round(float64(srcHeight)*r)))
			} else {
				cropW = srcWidth
				cropH = max(1, int(math.Round(float64(srcWidth)/r)))
			}
		}
	}
	if cropW > srcWidth || cropH > srcHeight {
		return nil, "", fmt.Errorf(
			"imageedit: crop %dx%d exceeds source %dx%d",
			cropW, cropH, srcWidth, srcHeight,
		)
	}
	if cropW != srcWidth || cropH != srcHeight {
		var x, y int
		if n.CropLeft >= 0 && n.CropTop >= 0 {
			x = min(n.CropLeft, srcWidth-cropW)
			y = min(n.CropTop, srcHeight-cropH)
		} else {
			x, y = cropOffset(srcWidth, srcHeight, cropW, cropH, n.CropPosition)
		}
		img = cropImage(img, x, y, cropW, cropH)
		width, height = cropW, cropH
	}
	if err := checkContext(ctx); err != nil {
		return nil, "", err
	}

	// 2. 旋转、翻转、灰度。翻转按界面文案的语义：horizontal 是左右镜像。
	if n.Rotate != 0 {
		img = rotateImage(img, n.Rotate)
		if n.Rotate == 90 || n.Rotate == 270 {
			width, height = height, width
		}
	}
	if n.FlipHorizontal {
		img = flipHorizontal(img)
	}
	if n.FlipVertical {
		img = flipVertical(img)
	}
	if n.Grayscale {
		img = grayscaleImage(img)
	}

	// 3. 改尺寸：inside 等比缩进且不放大；fill 拉伸到指定宽高；
	// contain 等比缩进后铺白底补满。
	if n.ResizeWidth > 0 || n.ResizeHeight > 0 {
		switch n.ResizeMode {
		case "fill", "contain":
			tw := n.ResizeWidth
			if tw == 0 {
				tw = width
			}
			th := n.ResizeHeight
			if th == 0 {
				th = height
			}
			if n.ResizeMode == "contain" {
				img = containImage(img, tw, th)
			} else {
				img = resizeImage(img, tw, th)
			}
			width, height = tw, th
		default:
			nw, nh := containedSize(width, height, n.ResizeWidth, n.ResizeHeight)
			if nw != width || nh != height {
				img = resizeImage(img, nw, nh)
				width, height = nw, nh
			}
		}
	}
	if err := checkContext(ctx); err != nil {
		return nil, "", err
	}

	// 4. 色调调整放在改尺寸之后，模糊和锐化作用在最终像素上。
	adjustTone(img, n.Brightness, n.Contrast, n.Saturation)
	if n.Blur > 0 {
		img = blurImage(img, n.Blur)
	}
	if n.Sharpen {
		img = sharpenImage(img)
	}
	if err := checkContext(ctx); err != nil {
		return nil, "", err
	}
	return img, meta.format, nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("imageedit: context canceled: %w", ctx.Err())
	default:
		return nil
	}
}
