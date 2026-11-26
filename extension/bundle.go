package extbundle

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed refuse.js
//go:embed doubao/page.js doubao/raw.js doubao/source_map.js
//go:embed download/content.js download/temu.js
var packed embed.FS

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

// DoubaoPageJS is shared by the extension and the desktop result boundary.
func DoubaoPageJS() string {
	body, err := fs.ReadFile(packed, "doubao/page.js")
	if err != nil {
		return ""
	}
	return string(body)
}

// DoubaoSourceMapJS matches generated images to this turn's uploaded attachments.
func DoubaoSourceMapJS() string {
	body, err := fs.ReadFile(packed, "doubao/source_map.js")
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

// DownloadAddonMarkers identifies the per-install DOM attributes used by the download assistant.
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

func replaceFileTokens(path string, replacer *strings.Replacer) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(replacer.Replace(string(raw))), 0o644)
}

// InstallDownloadAddonWithMarkers prepares the download assistant bundle with per-install
// randomized DOM markers and private local endpoint settings for the Fingerprint isolated world.
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
