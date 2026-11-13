package camoufox

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func PrepareWindowsRuntimeBundle(ctx context.Context, output string, progress func(Progress)) error {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return errors.New("Windows x64 启动环境包必须在 Windows x64 上构建")
	}
	if strings.TrimSpace(output) == "" {
		return errors.New("缺少启动环境包输出路径")
	}
	stage, err := os.MkdirTemp("", "tulai-runtime-bundle-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	artifact := nodeArtifacts["windows/amd64"]
	archive := filepath.Join(stage, artifact.File)
	if err := downloadNode(ctx, archive, artifact, "", progress); err != nil {
		return err
	}
	if err := extractNode(archive, filepath.Join(stage, "node")); err != nil {
		return err
	}
	if err := writeDriver(stage); err != nil {
		return err
	}
	if progress != nil {
		progress(Progress{Stage: StagePackages, Text: "正在构建固定版本的 Camoufox 启动库…"})
	}
	if err := installLockedPackages(ctx, stage, "", progress); err != nil {
		return err
	}
	if err := ensureNodeRuntimeSupport(stage); err != nil {
		return err
	}
	if err := verifyNodePackages(ctx, stage, ""); err != nil {
		return fmt.Errorf("构建内置启动库失败: %w", err)
	}
	if progress != nil {
		progress(Progress{Stage: StagePackages, Text: "正在压缩内置启动环境…"})
	}
	return writeWindowsRuntimeBundle(ctx, stage, output)
}

func writeWindowsRuntimeBundle(ctx context.Context, stage, output string) error {
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(output), ".tulai-runtime-*.zip")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	archive := zip.NewWriter(f)
	writeFile := func(name, path string, info fs.FileInfo) error {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("启动环境包含非常规文件: %s", name)
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name, header.Method = name, zip.Deflate
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, &bundleContextReader{ctx: ctx, reader: input})
		return errors.Join(err, input.Close())
	}
	manifest, _ := json.Marshal(expectedBundleManifest("windows/amd64"))
	entry, err := archive.Create("manifest.json")
	if err == nil {
		_, err = entry.Write(manifest)
	}
	for _, name := range []string{"node/node.exe", "node/LICENSE"} {
		if err != nil {
			break
		}
		path := filepath.Join(stage, filepath.FromSlash(name))
		var info fs.FileInfo
		info, err = os.Lstat(path)
		if err == nil {
			err = writeFile(name, path, info)
		}
	}
	if err == nil {
		root := filepath.Join(stage, "node_modules")
		err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if path == filepath.Join(root, ".bin") {
				return filepath.SkipDir
			}
			if path == filepath.Join(root, ".package-lock.json") {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(stage, path)
			if err != nil {
				return err
			}
			return writeFile(filepath.ToSlash(relative), path, info)
		})
	}
	err = errors.Join(err, archive.Close(), f.Close())
	if err != nil {
		return err
	}
	if err := os.Rename(temporary, output); err != nil {
		return fmt.Errorf("保存内置启动环境: %w", err)
	}
	return nil
}
