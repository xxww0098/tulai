package imageedit

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"testing"
)

func TestProcessImageMatchesProcessWithCatmullRom(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	draw.Draw(src, src.Bounds(), image.NewUniform(color.NRGBA{R: 30, G: 60, B: 90, A: 255}), image.Point{}, draw.Src)
	draw.Draw(src, image.Rect(40, 40, 88, 88), image.NewUniform(color.NRGBA{R: 220, G: 20, B: 20, A: 255}), image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	opts := Options{ResizeWidth: 200, ResizeHeight: 100, ResizeMode: "contain", ResizeUpscale: true, Format: "png"}
	fromBytes, err := Process(context.Background(), buf.Bytes(), opts)
	if err != nil {
		t.Fatal(err)
	}
	fromImage, err := ProcessImage(context.Background(), src, "png", opts)
	if err != nil {
		t.Fatal(err)
	}
	if fromImage.Width != 200 || fromImage.Height != 100 || fromImage.Format != "png" {
		t.Fatalf("unexpected dimensions/format: %+v", fromImage)
	}
	if !bytes.Equal(fromBytes.Data, fromImage.Data) {
		t.Fatal("ProcessImage output diverged from Process")
	}
	decoded, err := png.Decode(bytes.NewReader(fromImage.Data))
	if err != nil {
		t.Fatal(err)
	}
	// 边缘白底与居中红色主体保持精确
	r, g, b, a := decoded.At(0, 50).RGBA()
	if r != 65535 || g != 65535 || b != 65535 || a != 65535 {
		t.Fatalf("expected white contain edge, got %d %d %d %d", r, g, b, a)
	}
	cr, cg, cb, _ := decoded.At(100, 50).RGBA()
	if cr < 50000 || cg > 8000 || cb > 8000 {
		t.Fatalf("expected red center subject, got %d %d %d", cr, cg, cb)
	}
}
