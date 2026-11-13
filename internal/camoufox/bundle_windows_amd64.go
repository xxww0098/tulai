//go:build tulai_camoufox_bundle

package camoufox

import (
	_ "embed"
	"io"
	"strings"
)

//go:embed bundled/windows-amd64.zip
var windowsRuntimeBundle string

func embeddedRuntimeBundle() (io.ReaderAt, int64, bool) {
	return strings.NewReader(windowsRuntimeBundle), int64(len(windowsRuntimeBundle)), true
}
