package fingerprint

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// FingerprintVersion pins the official adryfish/fingerprint-chromium release
// that ships Windows x64, macOS Apple Silicon, and Linux x64 builds without
// the 150.x canvas readback renderer crash.
const FingerprintVersion = "148.0.7778.215"

const fingerprintReleaseBase = "https://github.com/adryfish/fingerprint-chromium/releases/download/" + FingerprintVersion

const maxFingerprintArchiveBytes = 2 << 30

type fingerprintArtifact struct {
	File   string
	SHA256 string
	RelExe string
}

// SHA-256 digests published on github.com/adryfish/fingerprint-chromium/releases/tag/148.0.7778.215.
var fingerprintArtifacts = map[string]fingerprintArtifact{
	"darwin/arm64": {
		File:   "ungoogled-chromium_148.0.7778.215-1.1_macos.dmg",
		SHA256: "b72f091e2e1a7583eed389c4b8e3534ed355e568af8c8bbf8fc30a25e23ca679",
		RelExe: "Chromium.app/Contents/MacOS/Chromium",
	},
	"windows/amd64": {
		File:   "ungoogled-chromium_148.0.7778.215-1.1_windows_x64.zip",
		SHA256: "9ef3f471b7a6641b4224532522b29141ce3746e27d55788d88e2fd951f362579",
		RelExe: "chrome.exe",
	},
	"linux/amd64": {
		File:   "ungoogled-chromium-148.0.7778.215-1-x86_64_linux.tar.xz",
		SHA256: "70d239830332e5820aa34dfcb284161cac0429eee25da642830afe04bda717f4",
		RelExe: "chrome",
	},
}

func FingerprintSupported(goos, goarch string) bool {
	if _, ok := nodeArtifacts[goos+"/"+goarch]; !ok {
		return false
	}
	_, ok := fingerprintArtifacts[goos+"/"+goarch]
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
	exe := filepath.Join(browsers, rel)
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return Runtime{}, false
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return Runtime{}, false
	}
	within, err := filepath.Rel(filepath.Join(root, "browsers"), resolved)
	if err != nil || !filepath.IsLocal(within) {
		return Runtime{}, false
	}
	rt := Runtime{Dir: dir, Node: nodePath(dir), Executable: exe}
	for _, f := range []string{rt.Node, exe} {
		if st, err := os.Stat(f); err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return Runtime{}, false
		}
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
	artifact, ok := fingerprintArtifacts[platform]
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
	lock, err := acquireInstallLock(ctx, root, func() { note(StageWaiting, "正在等待 Fingerprint 安装完成…") })
	if err != nil {
		return Runtime{}, err
	}
	defer releaseInstallLock(lock)
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

	archivePath := strings.TrimSpace(os.Getenv("TULAI_FINGERPRINT_ARCHIVE"))
	if archivePath != "" {
		note(StageUnpack, "正在校验本地 Fingerprint 安装包…")
		if err := verifyFileSHA256(archivePath, artifact.SHA256); err != nil {
			return Runtime{}, fmt.Errorf("校验 Fingerprint 安装包: %w", err)
		}
	} else {
		archivePath = filepath.Join(stage, artifact.File)
		defer os.Remove(archivePath)
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
		downloadURL := fingerprintReleaseBase + "/" + artifact.File
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
			_ = os.Remove(archivePath)
			env := append([]string{
				"TULAI_FINGERPRINT_URL=" + downloadURL,
				"TULAI_FINGERPRINT_DEST=" + archivePath,
			}, extraEnv...)
			_, dlErr = runInstall(ctx, stage, proxy, env, dlProgress, filepath.Join(stage, "bridge.mjs"), "download-fingerprint")
			if dlErr == nil {
				if verifyErr := verifyFileSHA256(archivePath, artifact.SHA256); verifyErr != nil {
					dlErr = verifyErr
				} else {
					break
				}
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
	if err := extractFingerprintArchive(ctx, archivePath, browsersDir, artifact); err != nil {
		return Runtime{}, fmt.Errorf("解压 Fingerprint 内核: %w", err)
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

func verifyFileSHA256(path, expectedHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(expectedHex)) {
		return fmt.Errorf("SHA-256 校验失败: got %s, want %s", got, expectedHex)
	}
	return nil
}

func extractFingerprintArchive(ctx context.Context, archive, dest string, artifact fingerprintArtifact) error {
	switch {
	case strings.HasSuffix(artifact.File, ".zip"):
		return extractFingerprintZip(ctx, archive, dest)
	case strings.HasSuffix(artifact.File, ".dmg"):
		return extractFingerprintDMG(ctx, archive, dest)
	case strings.HasSuffix(artifact.File, ".tar.xz"):
		return extractFingerprintTarXZ(ctx, archive, dest)
	default:
		return fmt.Errorf("不支持的 Fingerprint 归档格式: %s", artifact.File)
	}
}

func extractFingerprintZip(ctx context.Context, archive, dest string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	var total int64
	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		if !entry.Mode().IsRegular() {
			return errors.New("Fingerprint 压缩包包含非常规文件")
		}
		target, err := archivePath(dest, entry.Name)
		if err != nil {
			return errors.New("Fingerprint 压缩包路径无效")
		}
		if target == "" {
			continue
		}
		size := int64(entry.UncompressedSize64)
		if size < 0 || size > maxFingerprintArchiveBytes-total {
			return errors.New("Fingerprint 压缩包解压大小超限")
		}
		total += size
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		r, err := entry.Open()
		if err != nil {
			return err
		}
		mode := entry.Mode().Perm() & 0o755
		if mode == 0 {
			mode = 0o644
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			r.Close()
			return err
		}
		_, copyErr := io.CopyN(f, r, size)
		err = errors.Join(copyErr, f.Close(), r.Close())
		if err != nil {
			return err
		}
	}
	return nil
}

func extractFingerprintDMG(ctx context.Context, dmgPath, dest string) error {
	mountDir, err := os.MkdirTemp("", "tulai-fp-dmg-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(mountDir)

	attach := exec.CommandContext(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-noverify", "-mountpoint", mountDir, dmgPath)
	if out, err := attach.CombinedOutput(); err != nil {
		return fmt.Errorf("挂载 macOS 镜像失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	defer func() {
		detachCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = exec.CommandContext(detachCtx, "hdiutil", "detach", mountDir, "-force").Run()
	}()

	srcApp := filepath.Join(mountDir, "Chromium.app")
	if info, err := os.Lstat(srcApp); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("macOS 镜像中缺少 Chromium.app")
	}
	destApp := filepath.Join(dest, "Chromium.app")
	copyCmd := exec.CommandContext(ctx, "ditto", srcApp, destApp)
	if out, err := copyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("复制 Chromium.app 失败: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	// Clear quarantine metadata if present so Gatekeeper does not block execution.
	_ = exec.CommandContext(ctx, "xattr", "-dr", "com.apple.quarantine", destApp).Run()
	return nil
}

func extractFingerprintTarXZ(ctx context.Context, archive, dest string) error {
	xzBin, err := exec.LookPath("xz")
	if err != nil {
		xzBin, err = exec.LookPath("unxz")
		if err != nil {
			return fmt.Errorf("缺少 xz 解压工具: %w", err)
		}
	}
	xzCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(xzCtx, xzBin, "-dc", archive)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 xz 解压失败: %w", err)
	}
	waitDone := false
	defer func() {
		if !waitDone {
			cancel()
			_ = cmd.Wait()
		}
	}()

	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	type pendingSymlink struct {
		path   string
		target string
	}
	var symlinks []pendingSymlink
	var total int64
	entries := 0
	tr := tar.NewReader(stdout)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("读取 Linux 压缩包失败: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entries++
		name := strings.ReplaceAll(header.Name, "\\", "/")
		for strings.HasPrefix(name, "./") {
			name = strings.TrimPrefix(name, "./")
		}
		if strings.HasPrefix(name, "/") || strings.Contains(header.Name, "\\") {
			return errors.New("Fingerprint 压缩包路径无效")
		}
		target, err := archivePath(dest, name)
		if err != nil {
			return errors.New("Fingerprint 压缩包路径无效")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if target != "" {
				if err := os.MkdirAll(target, 0o700); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			if target == "" {
				return errors.New("Fingerprint 压缩包路径无效")
			}
			if header.Size < 0 || header.Size > maxFingerprintArchiveBytes-total {
				return errors.New("Fingerprint 压缩包解压大小超限")
			}
			total += header.Size
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			mode := os.FileMode(header.Mode).Perm() & 0o755
			if mode == 0 {
				mode = 0o644
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			_, copyErr := io.CopyN(f, tr, header.Size)
			if err := errors.Join(copyErr, f.Close()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if target == "" {
				return errors.New("Fingerprint 压缩包路径无效")
			}
			link := strings.TrimSpace(header.Linkname)
			if link == "" || strings.HasPrefix(link, "/") || strings.Contains(link, "\\") || filepath.IsAbs(link) {
				return errors.New("Fingerprint 压缩包包含不安全的符号链接")
			}
			relTarget, err := filepath.Rel(dest, target)
			if err != nil || !filepath.IsLocal(relTarget) {
				return errors.New("Fingerprint 压缩包路径无效")
			}
			resolvedRel := filepath.Clean(filepath.Join(filepath.Dir(relTarget), filepath.FromSlash(link)))
			if !filepath.IsLocal(resolvedRel) {
				return errors.New("Fingerprint 压缩包包含越界符号链接")
			}
			symlinks = append(symlinks, pendingSymlink{path: target, target: filepath.FromSlash(link)})
		default:
			return errors.New("Fingerprint 压缩包包含不支持的文件类型")
		}
	}
	waitDone = true
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("解压 tar.xz 失败: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	if entries == 0 {
		return errors.New("Fingerprint 压缩包为空")
	}
	// Create validated relative symlinks only after all regular files are in place,
	// then confirm every resolved link stays strictly inside dest.
	resolvedDest, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return err
	}
	for _, s := range symlinks {
		if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
			return err
		}
		if err := os.Symlink(s.target, s.path); err != nil {
			return err
		}
	}
	for _, s := range symlinks {
		resolved, err := filepath.EvalSymlinks(s.path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return errors.New("Fingerprint 压缩包包含无效符号链接")
		}
		within, err := filepath.Rel(resolvedDest, resolved)
		if err != nil || !filepath.IsLocal(within) {
			return errors.New("Fingerprint 压缩包包含越界符号链接")
		}
	}
	return nil
}
