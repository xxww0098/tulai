package main

import (
	"embed"

	"github.com/nsgo-dev/nsgo/tsx/compiler"
)

//go:embed ui
var tsxFS embed.FS

// Root compiles and mounts tulai's NSGo TSX + Tailwind frontend.
var Root = compiler.App(tsxFS, "ui/App.tsx")
