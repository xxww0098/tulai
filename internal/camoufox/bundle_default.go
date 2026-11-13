//go:build !tulai_camoufox_bundle || !windows || !amd64

package camoufox

import "io"

func embeddedRuntimeBundle() (io.ReaderAt, int64, bool) {
	return nil, 0, false
}
