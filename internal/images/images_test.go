package images

import "testing"

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
