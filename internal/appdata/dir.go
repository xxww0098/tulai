// Package appdata locates the per-user tulai data directory.
package appdata

import (
	"os"
	"path/filepath"
)

// Dir is shared by the application and its managed browser runtime.
func Dir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "tulai"), nil
}
