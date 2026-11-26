package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpproxy"
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
	// Mirrors are ghproxy-style prefixes used for multi-source racing and fallback.
	Mirrors []string
	// PreferMirror puts configured Mirrors ahead of direct GitHub requests.
	PreferMirror bool
	// ProxyURL optionally configures an HTTP or SOCKS5 proxy when HTTP is nil.
	ProxyURL string
	// OnProgress is optional and may be called concurrently from download workers.
	OnProgress func(Progress)

	clientMu    sync.Mutex
	cachedHTTP  *http.Client
	cachedProxy string
}

var errReleaseNotEligible = errors.New("release not eligible")

// NewClient returns a client for the public tulai releases.
func NewClient(version string) *Client {
	return &Client{API: DefaultAPI, Owner: Owner, Repo: Repo, Version: version}
}

// Latest reads /repos/{owner}/{repo}/releases/latest via API mirrors/direct,
// falling back to github.com/{owner}/{repo}/releases/latest when API rate limits occur.
func (c *Client) Latest(ctx context.Context) (Release, error) {
	rel, err := c.latestFromAPI(ctx)
	if err == nil {
		return rel, nil
	}
	if errors.Is(err, errReleaseNotEligible) || c.api() != DefaultAPI || ctx.Err() != nil {
		return Release{}, err
	}
	webRel, webErr := c.latestFromWeb(ctx)
	if webErr == nil {
		return webRel, nil
	}
	return Release{}, err
}

func (c *Client) latestFromAPI(ctx context.Context) (Release, error) {
	endpoint := strings.TrimRight(c.api(), "/") + path.Join("/repos", c.owner(), c.repo(), "releases/latest")
	var parsed Release
	_, err := c.getValidated(ctx, endpoint, "application/vnd.github+json", maxReleaseJSON, func(body []byte, _ string) error {
		var rel Release
		if err := json.Unmarshal(body, &rel); err != nil {
			return fmt.Errorf("解析 GitHub 发布失败: %w", err)
		}
		if strings.TrimSpace(rel.Tag) == "" {
			return errors.New("发布响应缺少标签")
		}
		if rel.Draft {
			return fmt.Errorf("%w: 最新发布仍是草稿", errReleaseNotEligible)
		}
		if rel.Prerelease {
			return fmt.Errorf("%w: 最新发布是预发布版本", errReleaseNotEligible)
		}
		if _, err := rel.Version(); err != nil {
			return fmt.Errorf("%w: %v", errReleaseNotEligible, err)
		}
		parsed = rel
		return nil
	})
	if err != nil {
		return Release{}, err
	}
	return parsed, nil
}

func (c *Client) latestFromWeb(ctx context.Context) (Release, error) {
	latestURL := fmt.Sprintf("https://github.com/%s/%s/releases/latest", c.owner(), c.repo())
	tag, err := c.discoverLatestTag(ctx, latestURL)
	if err != nil {
		return Release{}, err
	}
	ver, err := ParseVersion(tag)
	if err != nil {
		return Release{}, err
	}
	downloadBase := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/", c.owner(), c.repo(), tag)
	sumsURL := downloadBase + "SHA256SUMS"
	var sums map[string]string
	sumsBody, err := c.getValidated(ctx, sumsURL, "text/plain, */*", maxChecksums, func(body []byte, _ string) error {
		parsed, err := ParseChecksums(string(body))
		if err != nil {
			return err
		}
		sums = parsed
		return nil
	})
	if err != nil {
		return Release{}, err
	}
	sumsDigest := sha256.Sum256(sumsBody)
	assets := make([]Asset, 0, len(sums)+1)
	assets = append(assets, Asset{
		Name:               "SHA256SUMS",
		BrowserDownloadURL: sumsURL,
		Size:               int64(len(sumsBody)),
		Digest:             "sha256:" + hex.EncodeToString(sumsDigest[:]),
		State:              "uploaded",
	})
	for name, digest := range sums {
		if name == "SHA256SUMS" || name == "SHA256SUMS.txt" {
			continue
		}
		assets = append(assets, Asset{
			Name:               name,
			BrowserDownloadURL: downloadBase + name,
			Digest:             "sha256:" + digest,
			State:              "uploaded",
		})
	}
	return Release{
		Tag:     tag,
		Name:    "tulai " + ver,
		HTMLURL: fmt.Sprintf("https://github.com/%s/%s/releases/tag/%s", c.owner(), c.repo(), tag),
		Assets:  assets,
	}, nil
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
		if err := c.downloadChecked(ctx, rel.Tag, sumsAsset, dest, checkChecksumsFile, digest); err != nil {
			return nil, fmt.Errorf("SHA256SUMS: %w", err)
		}
	} else if err := c.downloadChecked(ctx, rel.Tag, sumsAsset, dest, checkChecksumsFile); err != nil {
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
	if c.HTTP != nil {
		clone := *c.HTTP
		clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("重定向过多")
			}
			return c.allow(req.URL)
		}
		return &clone
	}
	proxyKey := strings.TrimSpace(c.ProxyURL)
	c.clientMu.Lock()
	defer c.clientMu.Unlock()
	if c.cachedHTTP != nil && c.cachedProxy == proxyKey {
		return c.cachedHTTP
	}
	tr := &http.Transport{
		Proxy:                 updateProxyFunc(proxyKey),
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 12 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	c.cachedProxy = proxyKey
	c.cachedHTTP = &http.Client{
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("重定向过多")
			}
			return c.allow(req.URL)
		},
	}
	return c.cachedHTTP
}

func updateProxyFunc(raw string) func(*http.Request) (*url.URL, error) {
	if raw == "" {
		return http.ProxyFromEnvironment
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return http.ProxyFromEnvironment
	}
	config := httpproxy.Config{
		HTTPProxy:  parsed.String(),
		HTTPSProxy: parsed.String(),
		NoProxy:    httpproxy.FromEnvironment().NoProxy,
	}
	proxyForURL := config.ProxyFunc()
	return func(req *http.Request) (*url.URL, error) {
		return proxyForURL(req.URL)
	}
}

func (c *Client) hasProxy() bool {
	if strings.TrimSpace(c.ProxyURL) != "" {
		return true
	}
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			return true
		}
	}
	return false
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
