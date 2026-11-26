package main

import (
	_ "embed"

	"github.com/xxww0098/tulai/internal/ui"
)

//go:embed assets/Icon.png
var appIconPNG []byte

func init() {
	ui.SetAppIcon(appIconPNG)
}
