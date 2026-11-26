package images

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestAcceptGeneratedRejectsIconSizedPNG(t *testing.T) {
	icon := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	for i := range icon.Pix {
		icon.Pix[i] = byte(i)
	}
	var small bytes.Buffer
	if err := png.Encode(&small, icon); err != nil {
		t.Fatal(err)
	}
	if AcceptGenerated(small.Bytes()) {
		t.Fatalf("24px icon png (%d bytes) must not count as a result", small.Len())
	}
	const n = 128
	photo := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			photo.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: uint8(x * y), A: 255})
		}
	}
	var big bytes.Buffer
	if err := png.Encode(&big, photo); err != nil {
		t.Fatal(err)
	}
	if !AcceptGenerated(big.Bytes()) {
		t.Fatalf("128px noisy png (%d bytes) should be a result", big.Len())
	}
}

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
