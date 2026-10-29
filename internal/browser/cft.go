package browser

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed cft_manifest.json
var cftManifestJSON []byte

// 国内镜像在前，官方存储在后。两边是同一个压缩包。
var defaultMirrors = []string{
	"https://cdn.npmmirror.com/binaries/chrome-for-testing",
	"https://storage.googleapis.com/chrome-for-testing-public",
}

var errChecksum = errors.New("校验和不匹配")
var errIncomplete = errors.New("下载未完成")

var downloadClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 downloadProxy,
		ResponseHeaderTimeout: 45 * time.Second,
		TLSHandshakeTimeout:   20 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	},
}

type cftManifest struct {
	Version   string                     `json:"version"`
	Platforms map[string]cftPlatformSpec `json:"platforms"`
}

type cftPlatformSpec struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Exe    string `json:"exe"`
}

type cftArtifact struct {
	Version  string
	Platform string
	File     string
	SHA256   string
	Exe      string
}

var (
	manifestOnce sync.Once
	manifestVal  cftManifest
	manifestErr  error
)

func loadManifest() (cftManifest, error) {
	manifestOnce.Do(func() {
		manifestErr = json.Unmarshal(cftManifestJSON, &manifestVal)
	})
	return manifestVal, manifestErr
}

// PinnedCfTVersion 是当前锁定的 Chrome for Testing 版本。
func PinnedCfTVersion() string {
	manifest, err := loadManifest()
	if err != nil {
		return ""
	}
	return manifest.Version
}

func cftPlatformID(goos, goarch string) (string, bool) {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "linux64", true
	case "linux/arm64":
		return "linux-arm64", true
	case "darwin/amd64":
		return "mac-x64", true
	case "darwin/arm64":
		return "mac-arm64", true
	case "windows/amd64":
		return "win64", true
	default:
		return "", false
	}
}

func artifactFor(goos, goarch string) (cftArtifact, error) {
	platform, ok := cftPlatformID(goos, goarch)
	if !ok {
		return cftArtifact{}, errors.New("当前架构没有可下载的内置浏览器")
	}
	manifest, err := loadManifest()
	if err != nil {
		return cftArtifact{}, fmt.Errorf("读取内置浏览器清单失败: %w", err)
	}
	spec, ok := manifest.Platforms[platform]
	if !ok || spec.File == "" || spec.Exe == "" || spec.SHA256 == "" {
		return cftArtifact{}, fmt.Errorf("内置浏览器清单缺少 %s", platform)
	}
	return cftArtifact{
		Version:  manifest.Version,
		Platform: platform,
		File:     spec.File,
		SHA256:   strings.ToLower(spec.SHA256),
		Exe:      spec.Exe,
	}, nil
}

func (l lookup) builtin(dataDir string) (Runtime, error) {
	art, err := artifactFor(l.goos, l.goarch)
	if err != nil {
		return Runtime{}, err
	}
	exe := installedExe(dataDir, art)
	if exe == "" || l.stat == nil || l.stat(exe) != nil {
		return Runtime{}, os.ErrNotExist
	}
	return Runtime{Kind: KindBuiltin, Path: exe, Version: art.Version}, nil
}

func installedExe(dataDir string, art cftArtifact) string {
	if strings.TrimSpace(dataDir) == "" {
		return ""
	}
	return filepath.Join(dataDir, "runtime", "cft", art.Version, filepath.FromSlash(art.Exe))
}

// InstalledBuiltin 报告锁定版本的内置浏览器是否已经解压好。
func InstalledBuiltin(dataDir string) (Runtime, bool) {
	rt, err := hostLookup(runtime.GOOS, runtime.GOARCH).builtin(dataDir)
	if err != nil {
		return Runtime{}, false
	}
	return rt, true
}

// EnsureCfT 在 dataDir/runtime/cft/<版本>/ 准备锁定版本的 Chrome for Testing。
// mirrors 为空时先用 npmmirror，再回落到 storage.googleapis.com。
// progress 的 done/total 是已下载字节；total 为 0 表示长度未知。
func EnsureCfT(ctx context.Context, dataDir string, mirrors []string, progress func(done, total int64)) (Runtime, error) {
	return installCfT(ctx, dataDir, mirrors, progress, false, nil)
}

// RedownloadCfT 重新下载并替换当前锁定版本，成功后再删掉其它版本目录。
func RedownloadCfT(ctx context.Context, dataDir string, mirrors []string, progress func(done, total int64)) (Runtime, error) {
	return installCfT(ctx, dataDir, mirrors, progress, true, nil)
}

func installCfT(ctx context.Context, dataDir string, mirrors []string, progress func(done, total int64), force bool, client *http.Client) (Runtime, error) {
	if strings.TrimSpace(dataDir) == "" {
		return Runtime{}, errors.New("没有应用数据目录，无法下载内置浏览器")
	}
	art, err := artifactFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Runtime{}, err
	}
	return installArtifact(ctx, dataDir, art, mirrors, client, progress, force)
}

func installArtifact(ctx context.Context, dataDir string, art cftArtifact, mirrors []string, client *http.Client, progress func(done, total int64), force bool) (Runtime, error) {
	if strings.TrimSpace(dataDir) == "" {
		return Runtime{}, errors.New("没有应用数据目录，无法下载内置浏览器")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	exe := installedExe(dataDir, art)
	if !force {
		if _, err := os.Stat(exe); err == nil {
			return Runtime{Kind: KindBuiltin, Path: exe, Version: art.Version}, nil
		}
	}
	if len(mirrors) == 0 {
		mirrors = defaultMirrors
	}
	if client == nil {
		client = downloadClient
	}
	root := filepath.Join(dataDir, "runtime", "cft")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Runtime{}, err
	}
	partial := filepath.Join(root, art.Version+".zip.partial")
	if force {
		_ = os.Remove(partial)
	}
	if !hashFileIs(partial, art.SHA256) {
		if err := downloadArtifact(ctx, client, mirrors, art, partial, progress); err != nil {
			return Runtime{}, err
		}
	}
	if progress != nil {
		if info, err := os.Stat(partial); err == nil {
			progress(info.Size(), info.Size())
		}
	}
	tmp := filepath.Join(root, art.Version+".tmp")
	_ = os.RemoveAll(tmp)
	if err := extractZip(partial, tmp, art.Exe); err != nil {
		_ = os.RemoveAll(tmp)
		_ = os.Remove(partial)
		return Runtime{}, err
	}
	final := filepath.Join(root, art.Version)
	backup := final + ".old"
	_ = os.RemoveAll(backup)
	hadFinal := false
	if _, err := os.Stat(final); err == nil {
		hadFinal = true
		if err := os.Rename(final, backup); err != nil {
			_ = os.RemoveAll(tmp)
			return Runtime{}, fmt.Errorf("替换内置浏览器失败: %w", err)
		}
	}
	if err := os.Rename(tmp, final); err != nil {
		if hadFinal {
			_ = os.Rename(backup, final)
		}
		_ = os.RemoveAll(tmp)
		return Runtime{}, fmt.Errorf("解压内置浏览器失败: %w", err)
	}
	_ = os.RemoveAll(backup)
	_ = os.Remove(partial)
	removeOtherCfT(root, art.Version)
	installed := filepath.Join(final, filepath.FromSlash(art.Exe))
	if _, err := os.Stat(installed); err != nil {
		return Runtime{}, fmt.Errorf("解压后找不到内置浏览器: %w", err)
	}
	return Runtime{Kind: KindBuiltin, Path: installed, Version: art.Version}, nil
}

func downloadArtifact(ctx context.Context, client *http.Client, mirrors []string, art cftArtifact, partial string, progress func(done, total int64)) error {
	var last error
	for _, base := range mirrors {
		url := strings.TrimRight(base, "/") + "/" + art.Version + "/" + art.Platform + "/" + art.File
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			err = downloadFile(ctx, client, url, partial, art.SHA256, progress)
			if err == nil {
				return nil
			}
			if errors.Is(err, errChecksum) || !errors.Is(err, errIncomplete) {
				break
			}
		}
		last = fmt.Errorf("%s: %w", mirrorLabel(base), err)
	}
	if last == nil {
		last = errors.New("没有下载地址")
	}
	return fmt.Errorf("下载内置浏览器失败: %w", last)
}

func mirrorLabel(base string) string {
	switch {
	case strings.Contains(base, "npmmirror"):
		return "npmmirror"
	case strings.Contains(base, "googleapis"):
		return "storage.googleapis.com"
	default:
		return base
	}
}

func downloadFile(ctx context.Context, client *http.Client, url, dest, wantSHA string, progress func(done, total int64)) error {
	offset := int64(0)
	if info, err := os.Stat(dest); err == nil {
		offset = info.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusRequestedRangeNotSatisfiable:
		if hashFileIs(dest, wantSHA) {
			return nil
		}
		_ = os.Remove(dest)
		return errChecksum
	case http.StatusOK:
		offset = 0
	case http.StatusPartialContent:
	default:
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if resp.StatusCode == http.StatusOK {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	file, err := os.OpenFile(dest, flags, 0o644)
	if err != nil {
		return err
	}
	total := bodyTotal(resp, offset)
	written, copyErr := copyProgress(file, resp.Body, offset, total, progress)
	closeErr := file.Close()
	if copyErr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if total > 0 && written < total {
			return errIncomplete
		}
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if total > 0 && written < total {
		return errIncomplete
	}
	if total > 0 && written > total {
		_ = os.Remove(dest)
		return errChecksum
	}
	if !hashFileIs(dest, wantSHA) {
		_ = os.Remove(dest)
		return errChecksum
	}
	return nil
}

func bodyTotal(resp *http.Response, offset int64) int64 {
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		if i := strings.LastIndex(cr, "/"); i >= 0 {
			if n, err := strconv.ParseInt(cr[i+1:], 10, 64); err == nil && n > 0 {
				return n
			}
		}
	}
	if resp.ContentLength >= 0 {
		if resp.StatusCode == http.StatusPartialContent {
			return offset + resp.ContentLength
		}
		return resp.ContentLength
	}
	return 0
}

func copyProgress(dst io.Writer, src io.Reader, base, total int64, progress func(done, total int64)) (int64, error) {
	buf := make([]byte, 256*1024)
	written := base
	if progress != nil {
		progress(written, total)
	}
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return written, werr
			}
			written += int64(n)
			if progress != nil {
				progress(written, total)
			}
		}
		if err == io.EOF {
			return written, nil
		}
		if err != nil {
			return written, err
		}
	}
}

func hashFileIs(path, want string) bool {
	if path == "" || want == "" {
		return false
	}
	got, err := fileSHA256(path)
	return err == nil && strings.EqualFold(got, want)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func extractZip(src, dest, exe string) error {
	reader, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("无法打开内置浏览器压缩包: %w", err)
	}
	defer reader.Close()
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	var unpacked uint64
	const maxUnpacked = 2 << 30
	for _, file := range reader.File {
		target, err := safeZipPath(dest, file.Name)
		if err != nil {
			return err
		}
		unpacked += file.UncompressedSize64
		if unpacked > maxUnpacked {
			return errors.New("内置浏览器压缩包过大")
		}
		mode := file.Mode()
		if mode&os.ModeSymlink != 0 {
			if err := extractSymlink(file, dest, target); err != nil {
				return err
			}
			continue
		}
		if file.FileInfo().IsDir() || strings.HasSuffix(file.Name, "/") {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := extractFile(file, target, mode); err != nil {
			return err
		}
	}
	exePath := filepath.Join(dest, filepath.FromSlash(exe))
	info, err := os.Stat(exePath)
	if err != nil {
		return fmt.Errorf("压缩包里没有浏览器可执行文件: %w", err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		if err := os.Chmod(exePath, info.Mode().Perm()|0o755); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(file *zip.File, target string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	perm := mode.Perm()
	if perm == 0 {
		perm = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	src, err := file.Open()
	if err != nil {
		out.Close()
		return err
	}
	_, copyErr := io.Copy(out, src)
	closeSrc := src.Close()
	closeOut := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeSrc != nil {
		return closeSrc
	}
	return closeOut
}

func extractSymlink(file *zip.File, dest, target string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(src, 4096))
	closeErr := src.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	link := string(data)
	if link == "" || strings.Contains(link, "\x00") || filepath.IsAbs(link) {
		return errors.New("压缩包符号链接不安全")
	}
	resolved := filepath.Join(filepath.Dir(target), link)
	if !withinDir(dest, resolved) {
		return errors.New("压缩包符号链接不安全")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	_ = os.Remove(target)
	return os.Symlink(link, target)
}

func safeZipPath(dest, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") {
		return "", errors.New("压缩包路径不安全")
	}
	var parts []string
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." {
			continue
		}
		if part == ".." || strings.Contains(part, ":") {
			return "", errors.New("压缩包路径不安全")
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", errors.New("压缩包路径不安全")
	}
	target := filepath.Join(append([]string{dest}, parts...)...)
	if !withinDir(dest, target) {
		return "", errors.New("压缩包路径不安全")
	}
	return target, nil
}

func withinDir(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func removeOtherCfT(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == keep {
			continue
		}
		if name == keep+".tmp" || name == keep+".old" || name == keep+".zip.partial" {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, name))
	}
}
