package extbundle

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed manifest.json background.js bridge_logic.js content.js pair.js popup.html options.html options.js protocol.js quota.js
//go:embed all:icons
//go:embed all:qianwen
//go:embed all:doubao
var packed embed.FS

// Version 是嵌进二进制的扩展清单版本。
func Version() string {
	raw, err := fs.ReadFile(packed, "manifest.json")
	if err != nil {
		return ""
	}
	var manifest struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return ""
	}
	return strings.TrimSpace(manifest.Version)
}

// Install 把扩展解压到 <configDir>/extension/<版本>/，并同步到稳定目录 current。
// 版本没变时不重写文件。旧版本目录会删掉。返回浏览器应该加载的 current 路径。
func Install(configDir string) (string, error) {
	ver := Version()
	if ver == "" {
		return "", errors.New("扩展清单没有版本")
	}
	if strings.TrimSpace(configDir) == "" {
		return "", errors.New("没有用户配置目录")
	}
	base := filepath.Join(configDir, "extension")
	versionDir := filepath.Join(base, ver)
	current := filepath.Join(base, "current")
	if err := extract(versionDir); err != nil {
		return "", err
	}
	if err := syncDir(versionDir, current); err != nil {
		return "", err
	}
	cleanOld(base, ver)
	return current, nil
}

func extract(dest string) error {
	if _, err := os.Stat(filepath.Join(dest, "manifest.json")); err == nil {
		return nil
	}
	return copyFS(packed, ".", dest)
}

func syncDir(src, dest string) error {
	if sameVersion(dest) {
		return nil
	}
	_ = os.RemoveAll(dest)
	return copyTree(src, dest)
}

func sameVersion(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return false
	}
	var manifest struct {
		Version string `json:"version"`
	}
	return json.Unmarshal(raw, &manifest) == nil && strings.TrimSpace(manifest.Version) == Version()
}

func copyFS(src fs.FS, root, dest string) error {
	return fs.WalkDir(src, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dest, 0o755)
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := fs.ReadFile(src, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
}

func copyTree(src, dest string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
}

func cleanOld(base, keep string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == keep || name == "current" {
			continue
		}
		_ = os.RemoveAll(filepath.Join(base, name))
	}
}
