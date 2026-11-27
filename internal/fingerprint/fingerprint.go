package fingerprint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xxww0098/realbrowser/fingerprintkernel/engine"
)

const FingerprintVersion = engine.PinnedVersion

func FingerprintSupported(goos, goarch string) bool {
	if _, ok := nodeArtifacts[goos+"/"+goarch]; !ok {
		return false
	}
	_, ok := engine.PinnedRelease(goos, goarch)
	return ok
}

func fingerprintRuntimeDir(dataDir string) string {
	return filepath.Join(dataDir, "runtime", "fingerprint", FingerprintVersion+"-"+driverVersion+"-"+nodeVersion+"-"+runtime.GOOS+"-"+runtime.GOARCH)
}

func InstalledFingerprint(dataDir string) (Runtime, bool) {
	if strings.TrimSpace(dataDir) == "" {
		return Runtime{}, false
	}
	return fingerprintAt(fingerprintRuntimeDir(dataDir))
}

func fingerprintAt(dir string) (Runtime, bool) {
	node := nodePath(dir)
	if info, err := os.Stat(node); err != nil || !info.Mode().IsRegular() || info.Size() == 0 || nodePackageVersion(dir, "playwright-core") != driverVersion {
		return Runtime{}, false
	}
	kernels := engine.Store{Dir: filepath.Join(dir, "kernels")}
	// Existing runtimes installed before the kernel owned first installation.
	if marker, err := os.ReadFile(filepath.Join(dir, "browser-path")); err == nil {
		if exe, ok := engine.ManagedExecutable(filepath.Join(dir, "browsers"), strings.TrimSpace(string(marker))); ok {
			kernels.Base = engine.InstalledRelease{Version: FingerprintVersion, Executable: exe}
		}
	}
	active, ok := kernels.Installed()
	if !ok {
		return Runtime{}, false
	}
	return Runtime{Dir: dir, Node: node, Version: active.Version, Executable: active.Executable}, true
}

func fingerprintMirrors() ([]string, bool) {
	value := strings.TrimSpace(os.Getenv("TULAI_FINGERPRINT_MIRROR"))
	if value == "" {
		return append([]string(nil), kernelMirrorDefaults...), false
	}
	if strings.EqualFold(value, "off") || strings.EqualFold(value, "none") {
		return nil, false
	}
	var out []string
	seen := make(map[string]bool)
	for _, part := range strings.Split(value, ",") {
		base := strings.TrimRight(strings.TrimSpace(part), "/")
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		out = append(out, base)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// EnsureFingerprint builds an independent fingerprint-chromium runtime using
// the pinned Node + playwright-core driver and a SHA-256 verified upstream binary.
func EnsureFingerprint(ctx context.Context, dataDir, proxy string, progress func(Progress)) (Runtime, error) {
	installMu.Lock()
	defer installMu.Unlock()
	if strings.TrimSpace(dataDir) == "" {
		return Runtime{}, errors.New("没有应用数据目录")
	}
	platform := runtime.GOOS + "/" + runtime.GOARCH
	_, ok := engine.PinnedRelease(runtime.GOOS, runtime.GOARCH)
	nodeArt, hasNode := nodeArtifacts[platform]
	if !ok || !hasNode {
		return Runtime{}, fmt.Errorf("Fingerprint (fingerprint-chromium) 暂不支持 %s（目前支持 Windows x64、macOS Apple Silicon 和 Linux x64）", platform)
	}
	final, err := filepath.Abs(fingerprintRuntimeDir(dataDir))
	if err != nil {
		return Runtime{}, err
	}
	root := filepath.Dir(final)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Runtime{}, err
	}
	note := func(stage, message string) {
		if progress != nil {
			progress(Progress{Stage: stage, Text: message})
		}
	}
	lock, err := engine.AcquireInstallLock(ctx, root, func() { note(StageWaiting, "正在等待 Fingerprint 安装完成…") })
	if err != nil {
		return Runtime{}, err
	}
	defer engine.ReleaseInstallLock(lock)
	if rt, ok := fingerprintAt(final); ok {
		return rt, writeDriver(final)
	}
	stage, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return Runtime{}, err
	}
	defer os.RemoveAll(stage)
	if err := prepareRuntime(ctx, stage, proxy, nodeArt, progress); err != nil {
		return Runtime{}, err
	}

	source := engine.PinnedSource{Archive: strings.TrimSpace(os.Getenv("TULAI_FINGERPRINT_ARCHIVE"))}
	if source.Archive != "" {
		note(StageUnpack, "正在校验本地 Fingerprint 安装包…")
	} else {
		note(StageBrowser, "正在下载 Fingerprint 浏览器内核…")
		mirrors, explicit := fingerprintMirrors()
		for i, extraEnv := range kernelAttempts(mirrors, explicit, proxy) {
			source.Downloads = append(source.Downloads, func(ctx context.Context, url, dest string) error {
				if i > 0 {
					note(StageBrowser, "正在通过备用线路重试下载 Fingerprint 浏览器内核…")
				}
				env := append([]string{"TULAI_FINGERPRINT_URL=" + url, "TULAI_FINGERPRINT_DEST=" + dest}, extraEnv...)
				_, err := runInstall(ctx, stage, proxy, env, progress, filepath.Join(stage, "bridge.mjs"), "download-fingerprint")
				if err == nil {
					note(StageUnpack, "正在解压并校验 Fingerprint…")
				}
				return err
			})
		}
	}
	kernels := engine.Store{Dir: filepath.Join(stage, "kernels"), Pinned: source}
	if _, err := kernels.Ensure(ctx, nil); err != nil {
		return Runtime{}, fmt.Errorf("安装 Fingerprint 内核: %w", err)
	}
	if _, ok := fingerprintAt(stage); !ok {
		return Runtime{}, errors.New("Fingerprint 浏览器安装不完整")
	}
	if err := publishRuntime(stage, final); err != nil {
		return Runtime{}, err
	}
	rt, ok := fingerprintAt(final)
	if !ok {
		return Runtime{}, errors.New("无法读取已安装的 Fingerprint 浏览器")
	}
	if progress != nil {
		progress(Progress{Stage: StageDone, Text: "Fingerprint 已就绪", Done: 1, Total: 1})
	}
	return rt, nil
}
