package camoufox

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const maxBundleSize = 512 << 20

type runtimeBundleManifest struct {
	Format   int    `json:"format"`
	Platform string `json:"platform"`
	Node     string `json:"node"`
	Launcher string `json:"launcher"`
	Lock     string `json:"lock_sha256"`
}

func expectedBundleManifest(platform string) runtimeBundleManifest {
	lock, _ := driver.ReadFile("driver/package-lock.json")
	digest := sha256.Sum256(lock)
	return runtimeBundleManifest{Format: 1, Platform: platform, Node: nodeVersion, Launcher: Version, Lock: hex.EncodeToString(digest[:])}
}

func unpackRuntimeBundle(ctx context.Context, source io.ReaderAt, size int64, dest, platform string) error {
	archive, err := zip.NewReader(source, size)
	if err != nil {
		return fmt.Errorf("读取内置启动环境: %w", err)
	}
	var manifest *zip.File
	seen := make(map[string]bool, len(archive.File))
	var total uint64
	for _, file := range archive.File {
		name := file.Name
		clean := strings.TrimSuffix(name, "/")
		key := strings.ToLower(clean)
		if name == "" || strings.Contains(name, `\`) || path.Clean(clean) != clean || !filepath.IsLocal(filepath.FromSlash(name)) || seen[key] {
			return errors.New("内置启动环境包含无效或重复的路径")
		}
		seen[key] = true
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.Mode().IsRegular() || (name != "manifest.json" && name != "node/node.exe" && name != "node/LICENSE" && !strings.HasPrefix(name, "node_modules/")) {
			return errors.New("内置启动环境包含不允许的文件")
		}
		if file.UncompressedSize64 > maxBundleSize-total {
			return errors.New("内置启动环境解压大小超限")
		}
		total += file.UncompressedSize64
		if name == "manifest.json" {
			manifest = file
		}
	}
	if manifest == nil || manifest.UncompressedSize64 > 8192 {
		return errors.New("内置启动环境缺少有效的版本信息")
	}
	manifestReader, err := manifest.Open()
	if err != nil {
		return err
	}
	data, err := io.ReadAll(manifestReader)
	err = errors.Join(err, manifestReader.Close())
	var got runtimeBundleManifest
	if err != nil || json.Unmarshal(data, &got) != nil || got != expectedBundleManifest(platform) {
		return errors.New("内置启动环境版本或依赖锁不匹配")
	}
	for _, file := range archive.File {
		if file.Name == "manifest.json" || file.FileInfo().IsDir() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(file.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		input, err := file.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, err = io.Copy(output, &bundleContextReader{ctx: ctx, reader: input})
		err = errors.Join(err, input.Close(), output.Close())
		if err != nil {
			return fmt.Errorf("解压内置启动环境 %s: %w", file.Name, err)
		}
	}
	for _, name := range []string{
		"node/node.exe",
		"node_modules/camoufox/package.json",
		"node_modules/impit-win32-x64-msvc/impit-node.win32-x64-msvc.node",
	} {
		info, err := os.Stat(filepath.Join(dest, filepath.FromSlash(name)))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("内置启动环境缺少 %s", name)
		}
	}
	const native = "impit-win32-x64-msvc"
	if got, want := nodePackageVersion(dest, native), impitOptionalVersion(dest, native); want == "" || got != want {
		return errors.New("内置启动环境的原生组件版本不匹配")
	}
	return nil
}

type bundleContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *bundleContextReader) Read(data []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(data)
}
