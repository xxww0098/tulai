package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	// Owner and Repo are the public GitHub repository that publishes desktop builds.
	Owner = "xxww0098"
	Repo  = "tulai"
	// DefaultAPI is the official GitHub API. Public releases need no token.
	DefaultAPI = "https://api.github.com"

	maxReleaseJSON = 2 << 20
	maxChecksums   = 1 << 20
	maxDownload    = 256 << 20
)

// Asset is one file on a GitHub release.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
	State              string `json:"state"`
}

// Release is a GitHub release, without the fields this updater does not use.
type Release struct {
	Tag        string  `json:"tag_name"`
	Name       string  `json:"name"`
	Body       string  `json:"body"`
	HTMLURL    string  `json:"html_url"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Version returns the product version of the tag.
func (r Release) Version() (string, error) {
	return ParseVersion(r.Tag)
}

// Prepared is a verified download, or an up-to-date result with an empty Path.
type Prepared struct {
	Release  Release
	Asset    Asset
	Path     string
	UpToDate bool
}

// Client reads public GitHub releases. It never sends an Authorization header.
type Client struct {
	API         string
	Owner       string
	Repo        string
	Version     string
	HTTP        *http.Client
	DownloadDir string
	// Mirrors are ghproxy-style prefixes tried after the official URL fails.
	Mirrors []string
	// OnProgress is optional and may be called from the download goroutine.
	OnProgress func(Progress)
}

// NewClient returns a client for the public tulai releases.
func NewClient(version string) *Client {
	return &Client{API: DefaultAPI, Owner: Owner, Repo: Repo, Version: version}
}

// Latest reads /repos/{owner}/{repo}/releases/latest.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	endpoint := strings.TrimRight(c.api(), "/") + path.Join("/repos", c.owner(), c.repo(), "releases/latest")
	body, err := c.get(ctx, endpoint, "application/vnd.github+json", maxReleaseJSON)
	if err != nil {
		return Release{}, err
	}
	var rel Release
	if err := json.Unmarshal(body, &rel); err != nil {
		return Release{}, fmt.Errorf("解析 GitHub 发布失败: %w", err)
	}
	if rel.Draft {
		return Release{}, errors.New("最新发布仍是草稿")
	}
	if rel.Prerelease {
		return Release{}, errors.New("最新发布是预发布版本")
	}
	if _, err := rel.Version(); err != nil {
		return Release{}, err
	}
	return rel, nil
}

// Prepare downloads the asset for mode when latest is newer than current.
func (c *Client) Prepare(ctx context.Context, current string, mode Mode, arch string) (Prepared, error) {
	rel, err := c.Latest(ctx)
	if err != nil {
		return Prepared{}, err
	}
	return c.Download(ctx, rel, current, mode, arch)
}

// Download checks current against rel and, when rel is newer, downloads its asset.
func (c *Client) Download(ctx context.Context, rel Release, current string, mode Mode, arch string) (Prepared, error) {
	latest, err := rel.Version()
	if err != nil {
		return Prepared{}, err
	}
	newer, err := Newer(current, latest)
	if err != nil {
		return Prepared{}, err
	}
	if !newer {
		return Prepared{Release: rel, UpToDate: true}, nil
	}
	asset, err := Select(rel.Assets, KindFor(mode), latest, arch)
	if err != nil {
		return Prepared{}, fmt.Errorf("%s: %w", rel.Tag, err)
	}
	sums, err := c.checksums(ctx, rel)
	if err != nil {
		return Prepared{}, err
	}
	want, ok := sums[NormalizeAssetName(asset.Name)]
	if !ok {
		return Prepared{}, fmt.Errorf("SHA256SUMS 没有 %s", asset.Name)
	}
	if hexDigest, ok := digestHex(asset.Digest); ok && !sameHex(want, hexDigest) {
		return Prepared{}, fmt.Errorf("SHA256SUMS 与 GitHub 摘要不一致：%s", asset.Name)
	}
	dir, err := c.downloadDir()
	if err != nil {
		return Prepared{}, err
	}
	dest := filepath.Join(dir, asset.Name)
	if err := c.downloadVerified(ctx, rel.Tag, asset, dest, want, digestHexOrEmpty(asset.Digest)); err != nil {
		return Prepared{}, err
	}
	return Prepared{Release: rel, Asset: asset, Path: dest}, nil
}

func (c *Client) checksums(ctx context.Context, rel Release) (map[string]string, error) {
	var sumsAsset Asset
	found := false
	for _, asset := range rel.Assets {
		if asset.State != "" && asset.State != "uploaded" {
			continue
		}
		if NormalizeAssetName(asset.Name) == "SHA256SUMS" || NormalizeAssetName(asset.Name) == "SHA256SUMS.txt" {
			sumsAsset = asset
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("发布里没有 SHA256SUMS，已拒绝安装")
	}
	dir, err := c.downloadDir()
	if err != nil {
		return nil, err
	}
	dest := filepath.Join(dir, "SHA256SUMS")
	if digest, ok := digestHex(sumsAsset.Digest); ok {
		if err := c.downloadVerified(ctx, rel.Tag, sumsAsset, dest, digest); err != nil {
			return nil, fmt.Errorf("SHA256SUMS: %w", err)
		}
	} else if err := c.downloadFile(ctx, rel.Tag, sumsAsset, dest); err != nil {
		return nil, err
	}
	defer os.Remove(dest)
	info, err := os.Stat(dest)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxChecksums {
		return nil, errors.New("SHA256SUMS 过大")
	}
	text, err := os.ReadFile(dest)
	if err != nil {
		return nil, err
	}
	return ParseChecksums(string(text))
}

func (c *Client) decorate(req *http.Request, accept string) {
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", c.userAgent())
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Del("Authorization")
}

func (c *Client) userAgent() string {
	version := c.Version
	if version == "" {
		version = "0"
	}
	return "tulai/" + version + " (+https://github.com/" + c.owner() + "/" + c.repo() + ")"
}

func (c *Client) http() *http.Client {
	base := c.HTTP
	if base == nil {
		base = &http.Client{}
	}
	clone := *base
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("重定向过多")
		}
		return c.allow(req.URL)
	}
	return &clone
}

func (c *Client) allowAssetURL(raw, tag, name string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if isLoopback(u.Hostname()) {
		return nil
	}
	if err := c.allow(u); err != nil {
		return err
	}
	want := &url.URL{
		Scheme: "https",
		Host:   "github.com",
		Path:   path.Join("/", c.owner(), c.repo(), "releases/download", tag, name),
	}
	if u.Scheme != want.Scheme || !strings.EqualFold(u.Host, want.Host) || u.Path != want.Path {
		return fmt.Errorf("拒绝的下载地址 %s", raw)
	}
	return nil
}

func (c *Client) allow(u *url.URL) error {
	if u == nil || u.Hostname() == "" {
		return errors.New("缺少地址")
	}
	host := strings.ToLower(u.Hostname())
	if isLoopback(host) {
		return nil
	}
	if u.Scheme != "https" {
		return fmt.Errorf("拒绝非 HTTPS 地址 %s", u.Host)
	}
	if api, err := url.Parse(c.api()); err == nil && strings.EqualFold(api.Hostname(), host) {
		return nil
	}
	if c.allowsMirror(u) {
		return nil
	}
	switch host {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com":
		return nil
	default:
		return fmt.Errorf("拒绝来自 %s 的更新地址", host)
	}
}

func (c *Client) downloadDir() (string, error) {
	if c.DownloadDir != "" {
		if err := os.MkdirAll(c.DownloadDir, 0o755); err != nil {
			return "", err
		}
		return c.DownloadDir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return os.MkdirTemp("", "tulai-update-")
	}
	dir := filepath.Join(base, "tulai", "updates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func (c *Client) api() string {
	if c.API != "" {
		return c.API
	}
	return DefaultAPI
}

func (c *Client) owner() string {
	if c.Owner != "" {
		return c.Owner
	}
	return Owner
}

func (c *Client) repo() string {
	if c.Repo != "" {
		return c.Repo
	}
	return Repo
}

// ParseChecksums reads sha256sum output. Names may contain spaces.
func ParseChecksums(text string) (map[string]string, error) {
	out := make(map[string]string)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if len(line) < 66 || !isHex(line[:64]) {
			return nil, fmt.Errorf("无法解析校验行 %q", line)
		}
		rest := line[64:]
		var name string
		switch {
		case strings.HasPrefix(rest, "  "):
			name = rest[2:]
		case strings.HasPrefix(rest, " *"):
			name = rest[2:]
		default:
			return nil, fmt.Errorf("无法解析校验行 %q", line)
		}
		if name == "" {
			return nil, fmt.Errorf("校验行缺少文件名")
		}
		out[NormalizeAssetName(name)] = strings.ToLower(line[:64])
	}
	if len(out) == 0 {
		return nil, errors.New("SHA256SUMS 是空的")
	}
	return out, nil
}

func verifyFile(path string, wants ...string) error {
	var hexes []string
	for _, want := range wants {
		want = strings.ToLower(strings.TrimSpace(want))
		if want == "" {
			continue
		}
		if !isHex(want) || len(want) != 64 {
			return fmt.Errorf("摘要无效")
		}
		hexes = append(hexes, want)
	}
	if len(hexes) == 0 {
		return errors.New("没有 SHA256 可校验")
	}
	for _, want := range hexes[1:] {
		if want != hexes[0] {
			return errors.New("SHA256SUMS 与 GitHub 摘要不一致")
		}
	}
	sum, err := hashFile(path)
	if err != nil {
		return err
	}
	if sum != hexes[0] {
		return fmt.Errorf("摘要不符")
	}
	return nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func digestHex(digest string) (string, bool) {
	hexPart, ok := strings.CutPrefix(strings.ToLower(strings.TrimSpace(digest)), "sha256:")
	if !ok || len(hexPart) != 64 || !isHex(hexPart) {
		return "", false
	}
	return hexPart, true
}

func digestHexOrEmpty(digest string) string {
	hexPart, _ := digestHex(digest)
	return hexPart
}

func sameHex(a, b string) bool {
	return strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(a), "sha256:"), strings.TrimPrefix(strings.TrimSpace(b), "sha256:"))
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func isLoopback(host string) bool {
	host = strings.Trim(strings.ToLower(host), "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
