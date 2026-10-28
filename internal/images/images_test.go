package images

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestIsValidImageBytes(t *testing.T) {
	for _, tt := range []struct {
		name string
		data []byte
		want bool
	}{
		{"empty", nil, false},
		{"text", []byte("not an image"), false},
		{"short RIFF", []byte("RIFF00"), false},
		{"RIFF header", []byte("RIFF0000"), false},
		{"truncated WEBP", []byte("RIFF0000WEB"), false},
		{"other RIFF", []byte("RIFF0000WAVE"), false},
		{"WEBP", []byte("RIFF0000WEBP"), true},
		{"PNG", []byte("\x89PNG\r\n\x1a\n"), true},
		{"JPEG", []byte{0xff, 0xd8, 0xff}, true},
		{"GIF", []byte("GIF89a"), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidImageBytes(tt.data); got != tt.want {
				t.Fatalf("IsValidImageBytes(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

func TestCoverThumbCropsWideCenter(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for x := 0; x < 8; x++ {
		c := color.RGBA{B: 255, A: 255}
		if x < 4 {
			c = color.RGBA{R: 255, A: 255}
		}
		for y := 0; y < 4; y++ {
			img.SetRGBA(x, y, c)
		}
	}
	path := filepath.Join(t.TempDir(), "wide.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got, err := CoverThumb(path, 4)
	if err != nil {
		t.Fatal(err)
	}
	b := got.Bounds()
	if b.Dx() != 4 || b.Dy() != 4 {
		t.Fatalf("size = %v", b)
	}
	left := color.RGBAModel.Convert(got.At(b.Min.X, b.Min.Y+b.Dy()/2)).(color.RGBA)
	right := color.RGBAModel.Convert(got.At(b.Max.X-1, b.Min.Y+b.Dy()/2)).(color.RGBA)
	if left.R < 200 || right.B < 200 {
		t.Fatalf("center crop left=%v right=%v", left, right)
	}
}
