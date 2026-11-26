//go:build !tulai_fingerprint_bundle || !windows || !amd64

package fingerprint

import "io"

func embeddedRuntimeBundle() (io.ReaderAt, int64, bool) {
	return nil, 0, false
}
