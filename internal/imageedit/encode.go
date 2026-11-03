package imageedit

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/webp"
)

// encodeImage 按目标格式编码。JPEG 先铺白底，透明区域不会变黑。
func encodeImage(img *image.NRGBA, format string, quality int) ([]byte, error) {
	var buf bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, flattenWhite(img), &jpeg.Options{Quality: quality})
	case "png":
		err = png.Encode(&buf, img)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	case "webp":
		err = webp.Encode(&buf, img, webp.Options{Quality: quality, Method: 4})
	case "avif":
		err = avif.Encode(&buf, img, avif.Options{Quality: quality, Speed: 10})
	default:
		return nil, fmt.Errorf("imageedit: unsupported output format %q", format)
	}
	if err != nil {
		return nil, fmt.Errorf("imageedit: encode %s: %w", format, err)
	}
	return buf.Bytes(), nil
}
