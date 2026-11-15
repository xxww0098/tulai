//go:build windows

package update

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const windowsUninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\dev.tulai.app`

// SyncWindowsInstalledVersion updates the existing per-user installer entry
// only when it refers to the directory containing this copy of tulai.exe.
// Portable copies and installations belonging to another directory are left alone.
func SyncWindowsInstalledVersion(executable, version string) error {
	return syncWindowsInstalledVersion(registry.CURRENT_USER, windowsUninstallKey, executable, version)
}

func syncWindowsInstalledVersion(root registry.Key, keyPath, executable, version string) error {
	if !filepath.IsAbs(executable) || !strings.EqualFold(filepath.Base(executable), "tulai.exe") {
		return nil
	}
	version, err := ParseVersion(version)
	if err != nil {
		return fmt.Errorf("无效的安装版本: %w", err)
	}

	key, err := registry.OpenKey(root, keyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取安装登记: %w", err)
	}
	match, err := windowsInstallLocationMatches(key, executable)
	if err != nil || !match {
		key.Close()
		return err
	}
	current, _, err := key.GetStringValue("DisplayVersion")
	key.Close()
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("读取安装版本: %w", err)
	}
	if current == version {
		return nil
	}

	key, err = registry.OpenKey(root, keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("写入安装登记: %w", err)
	}
	defer key.Close()
	// The registration can change between the read and the write.
	match, err = windowsInstallLocationMatches(key, executable)
	if err != nil || !match {
		return err
	}
	if err := key.SetStringValue("DisplayVersion", version); err != nil {
		return fmt.Errorf("写入安装版本: %w", err)
	}
	return nil
}

func windowsInstallLocationMatches(key registry.Key, executable string) (bool, error) {
	location, _, err := key.GetStringValue("InstallLocation")
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取安装目录: %w", err)
	}
	if !filepath.IsAbs(location) {
		return false, nil
	}
	dir := filepath.Dir(executable)
	return strings.EqualFold(filepath.Clean(location), filepath.Clean(dir)) || samePath(location, dir), nil
}
