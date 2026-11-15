package extbundle

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// DownloadAddonMarkers identifies the per-install DOM attributes used by the download add-on.
type DownloadAddonMarkers struct {
	HostAttr     string
	RequestAttr  string
	SnapshotAttr string
}

const (
	defaultDownloadHostAttr     = "data-pb-dl-host"
	defaultDownloadRequestAttr  = "data-pb-dl-req"
	defaultDownloadSnapshotAttr = "data-pb-dl-snap"
)

// DefaultDownloadAddonMarkers returns the default un-substituted attribute names.
func DefaultDownloadAddonMarkers() DownloadAddonMarkers {
	return DownloadAddonMarkers{
		HostAttr:     defaultDownloadHostAttr,
		RequestAttr:  defaultDownloadRequestAttr,
		SnapshotAttr: defaultDownloadSnapshotAttr,
	}
}

func randHex(nBytes int) (string, error) {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func distinctRandomTokens(prefix string, n int) ([]string, error) {
	seen := make(map[string]bool, n)
	out := make([]string, 0, n)
	for len(out) < n {
		h, err := randHex(8)
		if err != nil {
			return nil, err
		}
		tok := prefix + h
		if !seen[tok] {
			seen[tok] = true
			out = append(out, tok)
		}
	}
	return out, nil
}

func formatGeckoUUID(raw []byte) string {
	var b [16]byte
	copy(b[:], raw)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("{%08x-%04x-%04x-%04x-%012x}", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func randomGeckoUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return formatGeckoUUID(b[:]), nil
}

func geckoIDForAccount(account string) (string, error) {
	trimmed := strings.TrimSpace(account)
	if trimmed == "" {
		return randomGeckoUUID()
	}
	sum := sha256.Sum256([]byte("gecko:" + trimmed))
	return formatGeckoUUID(sum[:16]), nil
}

func setNestedString(root map[string]any, keys []string, value string) {
	cur := root
	for i := 0; i < len(keys)-1; i++ {
		next, ok := cur[keys[i]].(map[string]any)
		if !ok || next == nil {
			next = map[string]any{}
			cur[keys[i]] = next
		}
		cur = next
	}
	cur[keys[len(keys)-1]] = value
}

func replaceFileTokens(path string, replacer *strings.Replacer) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(replacer.Replace(string(raw))), 0o644)
}

// InstallDownloadAddonWithMarkers prepares the download helper add-on with per-install
// randomized Gecko ID and DOM markers, returning the add-on path and active markers.
func InstallDownloadAddonWithMarkers(profileDir, endpoint, token string) (string, DownloadAddonMarkers, error) {
	dir := filepath.Join(profileDir, ".tulai-download-addon")
	if err := copyFS(packed, "download", dir); err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	attrs, err := distinctRandomTokens("data-", 3)
	if err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	markers := DownloadAddonMarkers{
		HostAttr:     attrs[0],
		RequestAttr:  attrs[1],
		SnapshotAttr: attrs[2],
	}
	replacer := strings.NewReplacer(
		defaultDownloadHostAttr, markers.HostAttr,
		defaultDownloadRequestAttr, markers.RequestAttr,
		defaultDownloadSnapshotAttr, markers.SnapshotAttr,
	)
	if err := replaceFileTokens(filepath.Join(dir, "content.js"), replacer); err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	manifestRaw, err := fs.ReadFile(packed, "download/manifest.json")
	if err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	var manifestMap map[string]any
	if err := json.Unmarshal(manifestRaw, &manifestMap); err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	geckoID, err := randomGeckoUUID()
	if err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	manifestMap["name"] = "Media Helper"
	manifestMap["description"] = "Collect and save page media items."
	setNestedString(manifestMap, []string{"browser_specific_settings", "gecko", "id"}, geckoID)
	manifestBytes, err := json.MarshalIndent(manifestMap, "", "  ")
	if err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(manifestBytes, '\n'), 0o644); err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	settings, err := json.Marshal(map[string]string{"endpoint": endpoint, "token": token})
	if err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	path := filepath.Join(dir, "settings.js")
	if err := os.WriteFile(path, append([]byte("const TULAI_DOWNLOAD = "), settings...), 0o600); err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", DownloadAddonMarkers{}, err
	}
	return dir, markers, nil
}

// InstallDownloadAddon prepares the helper used only by the download browser.
func InstallDownloadAddon(profileDir, endpoint, token string) (string, error) {
	dir, _, err := InstallDownloadAddonWithMarkers(profileDir, endpoint, token)
	return dir, err
}

// InstallFirefoxBridge prepares a private per-run add-on for a bound account.
func InstallFirefoxBridge(dir, port, token, account string) (string, error) {
	if err := copyFS(packed, ".", dir); err != nil {
		return "", err
	}
	manifestRaw, err := fs.ReadFile(packed, "firefox/manifest.json")
	if err != nil {
		return "", err
	}
	var manifestMap map[string]any
	if err := json.Unmarshal(manifestRaw, &manifestMap); err != nil {
		return "", err
	}
	geckoID, err := geckoIDForAccount(account)
	if err != nil {
		return "", err
	}
	manifestMap["name"] = "Page Bridge"
	setNestedString(manifestMap, []string{"browser_action", "default_title"}, "Page Bridge")
	setNestedString(manifestMap, []string{"browser_specific_settings", "gecko", "id"}, geckoID)
	manifest, err := json.MarshalIndent(manifestMap, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(manifest, '\n'), 0o644); err != nil {
		return "", err
	}
	tokens, err := distinctRandomTokens("_", 5)
	if err != nil {
		return "", err
	}
	replacer := strings.NewReplacer(
		"__PB_PROBE__", tokens[0],
		"__PB_ARM__", tokens[1],
		"__PB_DISARM__", tokens[2],
		"__PB_OK__", tokens[3],
		"__PB_FAIL__", tokens[4],
	)
	for _, rel := range []string{
		filepath.Join("qianwen", "chooser.js"),
		filepath.Join("qianwen", "page.js"),
	} {
		if err := replaceFileTokens(filepath.Join(dir, rel), replacer); err != nil {
			return "", err
		}
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
