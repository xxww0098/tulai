//go:build tulai_fingerprint_bundle

package fingerprint

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
