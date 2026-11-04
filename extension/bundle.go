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

//go:embed manifest.json background.js bridge_logic.js content.js network.js pair.js popup.html options.html options.js protocol.js quota.js refuse.js
//go:embed all:icons
//go:embed all:qianwen
//go:embed all:doubao
//go:embed all:download
//go:embed all:firefox
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

// RefuseJS 是豆包和千问共用的审核话术。桌面端页面脚本也注入这一份。
func RefuseJS() string {
	body, err := fs.ReadFile(packed, "refuse.js")
	if err != nil {
		return ""
	}
	return string(body)
}

// DoubaoRawJS 同时供扩展和桌面端提取豆包响应中的原图。
func DoubaoRawJS() string {
	body, err := fs.ReadFile(packed, "doubao/raw.js")
	if err != nil {
		return ""
	}
	return string(body)
}

// TemuSearchJS is shared by the download helper and the browser collector.
func TemuSearchJS() string {
	body, err := fs.ReadFile(packed, "download/temu.js")
	if err != nil {
		return ""
	}
	return string(body)
}

// InstallDownloadAddon prepares the helper used only by the download browser.
func InstallDownloadAddon(profileDir, endpoint, token string) (string, error) {
	dir := filepath.Join(profileDir, ".tulai-download-addon")
	if err := copyFS(packed, "download", dir); err != nil {
		return "", err
	}
	settings, err := json.Marshal(map[string]string{"endpoint": endpoint, "token": token})
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "settings.js")
	if err := os.WriteFile(path, append([]byte("const TULAI_DOWNLOAD = "), settings...), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return dir, nil
}

// InstallFirefoxBridge prepares a private per-run add-on for a bound account.
func InstallFirefoxBridge(dir, port, token, account string) (string, error) {
	if err := copyFS(packed, ".", dir); err != nil {
		return "", err
	}
	manifest, err := fs.ReadFile(packed, "firefox/manifest.json")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0o644); err != nil {
		return "", err
	}
	settings, err := json.Marshal(map[string]string{"port": port, "token": token, "profileId": account})
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "firefox", "settings.js")
	if err := os.WriteFile(path, append([]byte("globalThis.TULAI_FIREFOX_CONFIG = "), append(settings, '\n')...), 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}
	return dir, nil
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
