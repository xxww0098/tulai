//go:build !windows || !amd64

package camoufox

func ensureNodeRuntimeSupport(string) error { return nil }
