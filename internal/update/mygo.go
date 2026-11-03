package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
)

// MyGoInstall reports whether MyGo can replace this app without touching a
// shared directory or a package-manager installation.
func MyGoInstall(goos, executable string) bool {
	if real, err := filepath.EvalSymlinks(executable); err == nil {
		executable = real
	}
	slash := strings.ToLower(toSlash(executable))
	switch goos {
	case "darwin":
		_, ok := MacBundle(executable)
		return ok
	case "windows":
		return strings.HasSuffix(slash, "/tulai/tulai.exe") && DetectMode(goos, executable) == ModeWindowsSetup
	case "linux":
		return strings.HasSuffix(slash, "/.local/tulai.app/tulai") && os.Getenv("APPIMAGE") == ""
	default:
		return false
	}
}

// CheckSigned reads this release's MyGo manifest and keeps its source for Install.
func (c *Client) CheckSigned(ctx context.Context, rel Release) (*mygo.Update, error) {
	source := &signedSource{client: c, release: rel, goos: runtime.GOOS, arch: runtime.GOARCH}
	mygo.Updater.SetSource(source)
	up, err := mygo.Updater.Check(ctx)
	if err != nil {
		if errors.Is(err, mygo.ErrUpdatesDisabled) {
			return nil, fmt.Errorf("当前安装位置不可写或未启用签名更新，请手动安装新版: %w", err)
		}
		return nil, err
	}
	version, err := rel.Version()
	if err != nil {
		return nil, err
	}
	if up == nil || up.Version != version {
		return nil, errors.New("MyGo 更新清单与 GitHub 发布版本不一致")
	}
	return up, nil
}

type signedSource struct {
	client  *Client
	release Release
	goos    string
	arch    string
}

func (s *signedSource) Manifest(ctx context.Context, _ bool) (io.ReadCloser, error) {
	target := s.goos + "-" + s.arch
	if s.goos == "darwin" {
		target = "darwin-universal"
	}
	name := "update-" + target + ".json"
	for _, asset := range s.release.Assets {
		if asset.Name == name && (asset.State == "" || asset.State == "uploaded") {
			if asset.Size > 1<<20 {
				return nil, errors.New("MyGo 更新清单过大")
			}
			return s.open(ctx, asset)
		}
	}
	return nil, fmt.Errorf("发布里没有签名更新清单 %s", name)
}

func (s *signedSource) Open(ctx context.Context, raw string) (io.ReadCloser, error) {
	for _, asset := range s.release.Assets {
		if asset.BrowserDownloadURL == raw && (asset.State == "" || asset.State == "uploaded") {
			return s.open(ctx, asset)
		}
	}
	return nil, errors.New("更新清单引用了当前发布之外的文件")
}

func (s *signedSource) open(ctx context.Context, asset Asset) (io.ReadCloser, error) {
	dir, err := os.MkdirTemp("", "tulai-signed-update-")
	if err != nil {
		return nil, err
	}
	file := filepath.Join(dir, asset.Name)
	var wants []string
	if digest := digestHexOrEmpty(asset.Digest); digest != "" {
		wants = append(wants, digest)
	}
	if err = s.client.fetchAsset(ctx, s.release.Tag, asset, file, wants); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	f, err := os.Open(file)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &temporaryUpdateFile{File: f, dir: dir}, nil
}

type temporaryUpdateFile struct {
	*os.File
	dir string
}

func (f *temporaryUpdateFile) Close() error {
	err := f.File.Close()
	removeErr := os.RemoveAll(f.dir)
	if err != nil {
		return err
	}
	return removeErr
}
