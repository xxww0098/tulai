package rename

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewJPEGPhotoTime(t *testing.T) {
	tiff := make([]byte, 64)
	copy(tiff, "II")
	binary.LittleEndian.PutUint16(tiff[2:], 42)
	binary.LittleEndian.PutUint32(tiff[4:], 8)
	binary.LittleEndian.PutUint16(tiff[8:], 1)
	binary.LittleEndian.PutUint16(tiff[10:], 0x8769)
	binary.LittleEndian.PutUint32(tiff[18:], 26)
	binary.LittleEndian.PutUint16(tiff[26:], 1)
	binary.LittleEndian.PutUint16(tiff[28:], 0x9003)
	binary.LittleEndian.PutUint16(tiff[30:], 2)
	binary.LittleEndian.PutUint32(tiff[32:], 20)
	binary.LittleEndian.PutUint32(tiff[36:], 44)
	copy(tiff[44:], "2026:10:31 09:08:07\x00")
	segment := append([]byte("Exif\x00\x00"), tiff...)
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe1, byte((len(segment) + 2) >> 8), byte(len(segment) + 2)}
	jpeg = append(append(jpeg, segment...), 0xff, 0xd9)
	path := filepath.Join(t.TempDir(), "photo.JPG")
	if err := os.WriteFile(path, jpeg, 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err := Preview([]string{path}, Options{Mode: Insert, Position: 0, InsertKind: "照片拍摄时间"})
	if err != nil || !strings.HasSuffix(changes[0].To, "2026-10-31_09-08-07photo.JPG") {
		t.Fatalf("photo time preview = %+v, %v", changes, err)
	}
	if got := exifDate(tiff[:12]); got != "" {
		t.Fatalf("truncated EXIF = %q", got)
	}
}
