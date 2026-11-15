//go:build !windows

package update

// SyncWindowsInstalledVersion has no registry to update on other platforms.
func SyncWindowsInstalledVersion(executable, version string) error { return nil }
