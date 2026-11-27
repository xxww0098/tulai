package fingerprint

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xxww0098/realbrowser/browserkernel"
)

const FingerprintVersion = browserkernel.PinnedVersion

func FingerprintSupported(goos, goarch string) bool {
	if _, ok := nodeArtifacts[goos+"/"+goarch]; !ok {
		return false
	}
	_, ok := browserkernel.PinnedRelease(goos, goarch)
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
	marker, err := os.ReadFile(filepath.Join(dir, "browser-path"))
	rel := strings.TrimSpace(string(marker))
	if err != nil || !filepath.IsLocal(rel) || nodePackageVersion(dir, "playwright-core") != driverVersion {
		return Runtime{}, false
	}
	browsers := filepath.Join(dir, "browsers")
	exe, ok := browserkernel.ManagedExecutable(browsers, rel)
	if !ok {
		return Runtime{}, false
	}
	rt := Runtime{Dir: dir, Node: nodePath(dir), Executable: exe, Version: FingerprintVersion}
	if info, err := os.Stat(rt.Node); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return Runtime{}, false
	}
	if update, ok := browserkernel.ActiveRelease(filepath.Join(dir, "kernels")); ok {
		rt.Executable, rt.Version = update.Executable, update.Version
	}
	return rt, true
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
	artifact, ok := browserkernel.PinnedRelease(runtime.GOOS, runtime.GOARCH)
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
	lock, err := browserkernel.AcquireInstallLock(ctx, root, func() { note(StageWaiting, "正在等待 Fingerprint 安装完成…") })
	if err != nil {
		return Runtime{}, err
	}
	defer browserkernel.ReleaseInstallLock(lock)
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

	localArchive := strings.TrimSpace(os.Getenv("TULAI_FINGERPRINT_ARCHIVE"))
	archivePath := localArchive
	if archivePath != "" {
		note(StageUnpack, "正在校验本地 Fingerprint 安装包…")
		if err := browserkernel.VerifyFileSHA256(ctx, archivePath, artifact.SHA256); err != nil {
			return Runtime{}, fmt.Errorf("校验 Fingerprint 安装包: %w", err)
		}
	} else {
		note(StageBrowser, "正在下载 Fingerprint 浏览器内核…")
		var dlProgress func(Progress)
		if progress != nil {
			dlProgress = func(p Progress) {
				p.Stage = StageBrowser
				p.Text = "正在下载 Fingerprint 浏览器内核…"
				if p.Total > 0 && p.Done >= p.Total {
					p.Stage = StageUnpack
					p.Text = "正在解压并校验 Fingerprint…"
				}
				progress(p)
			}
		}
		asset := artifact.Asset()
		mirrors, explicit := fingerprintMirrors()
		var dlErr error
		for i, extraEnv := range kernelAttempts(mirrors, explicit, proxy) {
			if i > 0 {
				text := "正在通过备用线路重试下载 Fingerprint 浏览器内核…"
				if len(extraEnv) > 0 {
					text = "正在通过镜像重试下载 Fingerprint 浏览器内核…"
				}
				note(StageBrowser, text)
			}
			archivePath, dlErr = asset.SaveDownload(ctx, stage, func(dest string) error {
				env := append([]string{"TULAI_FINGERPRINT_URL=" + asset.DownloadURL, "TULAI_FINGERPRINT_DEST=" + dest}, extraEnv...)
				_, err := runInstall(ctx, stage, proxy, env, dlProgress, filepath.Join(stage, "bridge.mjs"), "download-fingerprint")
				return err
			})
			if dlErr == nil {
				break
			}
			if ctx.Err() != nil {
				return Runtime{}, ctx.Err()
			}
		}
		if dlErr != nil {
			return Runtime{}, fmt.Errorf("下载 Fingerprint 内核失败: %w", dlErr)
		}
	}

	note(StageUnpack, "正在解压并校验 Fingerprint…")
	browsersDir := filepath.Join(stage, "browsers")
	if err := os.MkdirAll(browsersDir, 0o700); err != nil {
		return Runtime{}, err
	}
	if err := artifact.Unpack(ctx, archivePath, browsersDir); err != nil {
		return Runtime{}, fmt.Errorf("解压 Fingerprint 内核: %w", err)
	}
	if localArchive == "" {
		if err := os.Remove(archivePath); err != nil {
			return Runtime{}, err
		}
	}
	relExe := filepath.FromSlash(artifact.RelExe)
	if err := os.WriteFile(filepath.Join(stage, "browser-path"), []byte(relExe), 0o600); err != nil {
		return Runtime{}, err
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
