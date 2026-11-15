package ui

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAppIconAsset(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "resources", "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != cfg.Height || cfg.Width < 1024 {
		t.Fatalf("icon is %dx%d, want a square PNG of at least 1024", cfg.Width, cfg.Height)
	}
}

func TestReadAppIconOutsideProject(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("..", "..", "resources", "icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	got, err := readAppIcon()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("app icon differs from resources/icon.png")
	}
}

func TestDoubaoPlatformIconAsset(t *testing.T) {
	if len(doubaoPNG) == 0 {
		t.Fatal("embedded doubaoPNG is empty")
	}
	if iconDoubao == nil {
		t.Fatal("iconDoubao bitmap is nil")
	}
	bmp, err := ui.DecodeBitmap(doubaoPNG)
	if err != nil || bmp == nil {
		t.Fatalf("DecodeBitmap(doubaoPNG) failed: bmp=%v err=%v", bmp, err)
	}
}
