package update

import (
	"fmt"
	"strconv"
	"strings"
)

const appName = "xbs"

// Mode is how the running copy should be updated.
type Mode string

const (
	// ModeLinuxUser installs the tar.gz into ~/.local, the same place as install.sh.
	ModeLinuxUser Mode = "linux-user"
	// ModeLinuxDeb is a package under /opt or /usr/bin. The system package
	// manager has to install it; the app does not ask for a password.
	ModeLinuxDeb Mode = "linux-deb"
	// ModeWindowsExe replaces the portable executable after this process exits.
	ModeWindowsExe Mode = "windows-exe"
	// ModeWindowsSetup runs the NSIS installer after this process exits.
	ModeWindowsSetup Mode = "windows-setup"
	// ModeMacBundle replaces the .app that is running.
	ModeMacBundle Mode = "mac-bundle"
	// ModeMacCopy copies the .app into ~/Applications.
	ModeMacCopy Mode = "mac-copy"
)

// Kind is one published desktop asset.
type Kind string

const (
	KindLinuxTar     Kind = "linux-tar"
	KindLinuxDeb     Kind = "linux-deb"
	KindWindowsExe   Kind = "windows-exe"
	KindWindowsSetup Kind = "windows-setup"
	KindMacDMG       Kind = "mac-dmg"
)

// UnsignedWarning is shown before a download. The SHA256 check does not
// replace a signature.
const UnsignedWarning = "安装包来自公开的 GitHub Releases，没有 Apple 公证，也没有 Windows 代码签名。校验 SHA256SUMS 只能说明文件和这次发布一致。macOS 上 Gatekeeper 可能拦截，Windows SmartScreen 可能报警。页面桥扩展不会跟着桌面端一起更新。"

// NormalizeAssetName makes a GitHub asset name comparable to SHA256SUMS.
// GitHub stores spaces in uploaded names as dots; the checksum file still
// uses the original names.
func NormalizeAssetName(name string) string {
	return strings.ReplaceAll(name, " ", ".")
}

// DetectMode chooses an install mode from the running executable.
func DetectMode(goos, executable string) Mode {
	executable = strings.TrimSpace(executable)
	switch goos {
	case "darwin":
		if _, ok := MacBundle(executable); ok {
			return ModeMacBundle
		}
		return ModeMacCopy
	case "windows":
		slash := strings.ToLower(toSlash(executable))
		if strings.Contains(slash, "/go-build") || strings.Contains(slash, "/program files") || strings.Contains(slash, "/programs/") {
			return ModeWindowsSetup
		}
		return ModeWindowsExe
	default:
		slash := toSlash(executable)
		if slash == "/usr/bin/xbs" || strings.HasPrefix(slash, "/opt/xbs/") {
			return ModeLinuxDeb
		}
		return ModeLinuxUser
	}
}

// KindFor reports which asset an install mode downloads.
func KindFor(mode Mode) Kind {
	switch mode {
	case ModeLinuxDeb:
		return KindLinuxDeb
	case ModeWindowsExe:
		return KindWindowsExe
	case ModeWindowsSetup:
		return KindWindowsSetup
	case ModeMacBundle, ModeMacCopy:
		return KindMacDMG
	default:
		return KindLinuxTar
	}
}

// AssetName is the GitHub asset name for a desktop build. Spaces are already
// written as dots, which is how the v0.1.0 release stores the disk image and
// the Windows installers.
func AssetName(kind Kind, version, arch string) (string, error) {
	version = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(version), "v"), "V")
	if _, _, err := splitVersion(version); err != nil {
		return "", err
	}
	if kind != KindMacDMG {
		switch arch {
		case "amd64", "arm64":
		default:
			return "", fmt.Errorf("没有 %s 的安装包", arch)
		}
	} else {
		switch arch {
		case "amd64", "arm64":
		default:
			return "", fmt.Errorf("没有 %s 的安装包", arch)
		}
	}
	switch kind {
	case KindLinuxTar:
		return fmt.Sprintf("%s-%s-linux-%s.tar.gz", appName, version, arch), nil
	case KindLinuxDeb:
		return fmt.Sprintf("%s_%s_%s.deb", appName, version, arch), nil
	case KindWindowsExe:
		return fmt.Sprintf("%s-%s-windows-%s.exe", appName, version, arch), nil
	case KindWindowsSetup:
		return fmt.Sprintf("%s.Setup.%s.%s.exe", appName, version, arch), nil
	case KindMacDMG:
		return fmt.Sprintf("%s.%s.dmg", appName, version), nil
	default:
		return "", fmt.Errorf("未知安装包类型 %s", kind)
	}
}

// Select finds the asset for kind. Extension zips and other files are ignored.
func Select(assets []Asset, kind Kind, version, arch string) (Asset, error) {
	want, err := AssetName(kind, version, arch)
	if err != nil {
		return Asset{}, err
	}
	for _, asset := range assets {
		if asset.State != "" && asset.State != "uploaded" {
			continue
		}
		if NormalizeAssetName(asset.Name) == want {
			return asset, nil
		}
	}
	return Asset{}, fmt.Errorf("发布里没有 %s", want)
}

// ParseVersion returns the x.y.z version, keeping a pre-release suffix.
func ParseVersion(s string) (string, error) {
	nums, pre, err := splitVersion(s)
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("%d.%d.%d", nums[0], nums[1], nums[2])
	if pre != "" {
		out += "-" + pre
	}
	return out, nil
}

// Compare reports whether a is older (-1), the same (0) or newer (1) than b.
// A release is newer than a pre-release with the same numbers.
func Compare(a, b string) (int, error) {
	an, ap, err := splitVersion(a)
	if err != nil {
		return 0, err
	}
	bn, bp, err := splitVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 3; i++ {
		switch {
		case an[i] < bn[i]:
			return -1, nil
		case an[i] > bn[i]:
			return 1, nil
		}
	}
	switch {
	case ap == bp:
		return 0, nil
	case ap == "":
		return 1, nil
	case bp == "":
		return -1, nil
	case ap < bp:
		return -1, nil
	default:
		return 1, nil
	}
}

// Newer reports whether latest should replace current.
func Newer(current, latest string) (bool, error) {
	order, err := Compare(latest, current)
	if err != nil {
		return false, err
	}
	return order > 0, nil
}

func splitVersion(s string) (nums []int, pre string, err error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if s == "" {
		return nil, "", fmt.Errorf("版本号为空")
	}
	main, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return nil, "", fmt.Errorf("版本号须为 x.y.z：%s", s)
	}
	nums = make([]int, 3)
	for i, part := range parts {
		n, convErr := strconv.Atoi(part)
		if convErr != nil || n < 0 || part == "" {
			return nil, "", fmt.Errorf("版本号无效：%s", s)
		}
		nums[i] = n
	}
	return nums, pre, nil
}

func toSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
