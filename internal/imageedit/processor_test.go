package imageedit

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"testing"
)

func pngSource(t *testing.T, w, h int, px func(x, y int) color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, px(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeResult(t *testing.T, data []byte) *image.NRGBA {
	t.Helper()
	meta, err := inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	img, err := decodeSource(data, meta.format)
	if err != nil {
		t.Fatal(err)
	}
	return toNRGBA(img)
}

func pixel(img *image.NRGBA, x, y int) [4]uint8 {
	i := y*img.Stride + x*4
	return [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
}

func TestProcessCenterCrop(t *testing.T) {
	src := pngSource(t, 5, 3, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 40), G: uint8(y * 70), B: 25, A: 255}
	})
	res, err := Process(context.Background(), src, Options{Format: "png", CropWidth: 3, CropHeight: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 3 || res.Height != 2 {
		t.Fatalf("size = %dx%d", res.Width, res.Height)
	}
	img := decodeResult(t, res.Data)
	if got := pixel(img, 0, 0); got != [4]uint8{40, 0, 25, 255} {
		t.Fatalf("top-left = %v", got)
	}
	if got := pixel(img, 2, 1); got != [4]uint8{120, 70, 25, 255} {
		t.Fatalf("bottom-right = %v", got)
	}
}

func TestProcessCropPositionAndExplicitOffset(t *testing.T) {
	src := pngSource(t, 4, 4, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 50), G: uint8(y * 50), A: 255}
	})
	res, err := Process(context.Background(), src, Options{
		Format: "png", CropWidth: 2, CropHeight: 2, CropPosition: "bottom-right",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pixel(decodeResult(t, res.Data), 0, 0); got != [4]uint8{100, 100, 0, 255} {
		t.Fatalf("bottom-right crop = %v", got)
	}

	res, err = Process(context.Background(), src, Options{
		Format: "png", CropWidth: 2, CropHeight: 2,
		CropLeft: Int(0), CropTop: Int(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := pixel(decodeResult(t, res.Data), 0, 0); got != [4]uint8{0, 0, 0, 255} {
		t.Fatalf("top-left crop = %v", got)
	}
}

func TestProcessRotateFlipGrayscale(t *testing.T) {
	src := pngSource(t, 2, 3, func(x, y int) color.NRGBA {
		if x == 0 && y == 0 {
			return color.NRGBA{R: 255, A: 255}
		}
		return color.NRGBA{A: 255}
	})
	res, err := Process(context.Background(), src, Options{Format: "png", Rotate: 90})
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 3 || res.Height != 2 {
		t.Fatalf("rotated size = %dx%d", res.Width, res.Height)
	}
	img := decodeResult(t, res.Data)
	if got := pixel(img, 2, 0); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("90° red pixel = %v, want at top-right", got)
	}

	res, err = Process(context.Background(), src, Options{Format: "png", Rotate: 270})
	if err != nil {
		t.Fatal(err)
	}
	img = decodeResult(t, res.Data)
	if got := pixel(img, 0, 1); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("270° red pixel = %v, want at bottom-left", got)
	}

	res, err = Process(context.Background(), src, Options{Format: "png", FlipHorizontal: true})
	if err != nil {
		t.Fatal(err)
	}
	img = decodeResult(t, res.Data)
	if got := pixel(img, 1, 0); got != [4]uint8{255, 0, 0, 255} {
		t.Fatalf("horizontal flip red pixel = %v", got)
	}

	graySrc := pngSource(t, 2, 2, func(x, y int) color.NRGBA {
		return color.NRGBA{R: 200, G: 40, B: 80, A: 255}
	})
	res, err = Process(context.Background(), graySrc, Options{Format: "png", Grayscale: true})
	if err != nil {
		t.Fatal(err)
	}
	got := pixel(decodeResult(t, res.Data), 0, 0)
	if got[0] != got[1] || got[1] != got[2] {
		t.Fatalf("grayscale pixel = %v", got)
	}
}

func TestProcessResizeInside(t *testing.T) {
	src := pngSource(t, 8, 4, func(x, y int) color.NRGBA {
		return color.NRGBA{R: 10, G: 20, B: 30, A: 255}
	})
	cases := []struct{ w, h, wantW, wantH int }{
		{3, 3, 3, 2},
		{4, 0, 4, 2},
		{20, 20, 8, 4},
	}
	for _, c := range cases {
		res, err := Process(context.Background(), src, Options{
			Format: "png", ResizeWidth: c.w, ResizeHeight: c.h,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.Width != c.wantW || res.Height != c.wantH {
			t.Errorf("%dx%d -> %dx%d, want %dx%d", c.w, c.h, res.Width, res.Height, c.wantW, c.wantH)
		}
	}
}

func TestProcessJPEGFlattensWhite(t *testing.T) {
	src := pngSource(t, 8, 8, func(x, y int) color.NRGBA {
		return color.NRGBA{R: 220, G: 20, B: 40, A: 0}
	})
	res, err := Process(context.Background(), src, Options{Format: "jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != "jpeg" {
		t.Fatalf("format = %q", res.Format)
	}
	img := decodeResult(t, res.Data)
	got := pixel(img, 4, 4)
	if got[0] < 0xf0 || got[1] < 0xf0 || got[2] < 0xf0 {
		t.Fatalf("transparent became %v, want near white", got[:3])
	}
}

func TestProcessKeepsOriginalFormat(t *testing.T) {
	src := pngSource(t, 4, 3, func(x, y int) color.NRGBA { return color.NRGBA{A: 255} })
	res, err := Process(context.Background(), src, Options{Format: "original", CropWidth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format != "png" || res.Width != 2 || res.Height != 3 {
		t.Fatalf("result = %s %dx%d", res.Format, res.Width, res.Height)
	}
}

func TestProcessWebPAndAVIF(t *testing.T) {
	src := pngSource(t, 8, 6, func(x, y int) color.NRGBA {
		return color.NRGBA{R: 200, G: 120, B: 40, A: 255}
	})
	webpRes, err := Process(context.Background(), src, Options{Format: "webp", JPEGQuality: 80})
	if err != nil {
		t.Fatal(err)
	}
	if meta, err := inspect(webpRes.Data); err != nil || meta.format != "webp" {
		t.Fatalf("webp meta = %+v, %v", meta, err)
	}
	avifRes, err := Process(context.Background(), src, Options{Format: "avif", JPEGQuality: 60})
	if err != nil {
		t.Fatal(err)
	}
	if meta, err := inspect(avifRes.Data); err != nil || meta.format != "avif" {
		t.Fatalf("avif meta = %+v, %v", meta, err)
	}
}

func TestProcessToneAndBlur(t *testing.T) {
	src := pngSource(t, 8, 8, func(x, y int) color.NRGBA {
		return color.NRGBA{R: uint8(x * 20), G: uint8(y * 20), B: 100, A: 255}
	})
	bright, err := Process(context.Background(), src, Options{Format: "png", Brightness: 50})
	if err != nil {
		t.Fatal(err)
	}
	if got := pixel(decodeResult(t, bright.Data), 7, 7); got[0] <= 140 || got[2] <= 100 {
		t.Fatalf("brightness pixel = %v", got)
	}
	blurred, err := Process(context.Background(), src, Options{Format: "png", Blur: 2})
	if err != nil {
		t.Fatal(err)
	}
	if blurred.Width != 8 {
		t.Fatalf("blur size = %dx%d", blurred.Width, blurred.Height)
	}
	if _, err := Process(context.Background(), src, Options{Format: "png", Sharpen: true}); err != nil {
		t.Fatal(err)
	}
	sat, err := Process(context.Background(), src, Options{Format: "png", Saturation: -100})
	if err != nil {
		t.Fatal(err)
	}
	got := pixel(decodeResult(t, sat.Data), 4, 4)
	if abs(int(got[0])-int(got[1])) > 1 || abs(int(got[1])-int(got[2])) > 1 {
		t.Fatalf("desaturated pixel = %v", got)
	}
}

func TestProcessRejectsAnimatedGIF(t *testing.T) {
	pal := color.Palette{color.Black, color.White}
	frame := func(c color.Color) *image.Paletted {
		p := image.NewPaletted(image.Rect(0, 0, 1, 1), pal)
		p.Set(0, 0, c)
		return p
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{frame(color.Black), frame(color.White)},
		Delay: []int{0, 0},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Process(context.Background(), buf.Bytes(), Options{Format: "png"}); !errors.Is(err, ErrAnimatedGIF) {
		t.Fatalf("err = %v, want ErrAnimatedGIF", err)
	}
}

func TestProcessRejectsTooLarge(t *testing.T) {
	// 只有 IHDR 的 PNG：读图长够用，不用真的分配 5000 万像素。
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 10000)
	binary.BigEndian.PutUint32(ihdr[4:], 10000)
	ihdr[8] = 8
	ihdr[9] = 2
	writeChunk := func(typ string, data []byte) {
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(data)))
		buf.Write(length[:])
		body := append([]byte(typ), data...)
		buf.Write(body)
		var sum [4]byte
		binary.BigEndian.PutUint32(sum[:], crc32.ChecksumIEEE(body))
		buf.Write(sum[:])
	}
	writeChunk("IHDR", ihdr)
	writeChunk("IEND", nil)
	if _, err := Process(context.Background(), buf.Bytes(), Options{Format: "png"}); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("err = %v, want ErrImageTooLarge", err)
	}
}

func TestPreviewDownscalesToJPEG(t *testing.T) {
	src := pngSource(t, 8, 6, func(x, y int) color.NRGBA {
		return color.NRGBA{R: 90, G: 120, B: 160, A: 255}
	})
	data, err := Preview(context.Background(), src, Options{}, 4)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	if meta.format != "jpeg" || meta.width != 4 || meta.height != 3 {
		t.Fatalf("preview = %s %dx%d", meta.format, meta.width, meta.height)
	}
}

func TestProcessAborts(t *testing.T) {
	src := pngSource(t, 4, 4, func(x, y int) color.NRGBA { return color.NRGBA{A: 255} })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Process(ctx, src, Options{Format: "png"}); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
