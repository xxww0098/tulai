package ui

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
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
