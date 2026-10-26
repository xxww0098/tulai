package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	apiAttempt      = 20 * time.Second
	downloadAttempt = 4 * time.Minute
)

// Progress is one download progress callback.
type Progress struct {
	Name  string
	Done  int64
	Total int64
}

type statusError struct {
	code    int
	msg     string
	tryNext bool
}

func (e *statusError) Error() string { return e.msg }

type shortError struct {
	got, want int64
}

func (e *shortError) Error() string {
	return fmt.Sprintf("下载不完整（%d / %d）", e.got, e.want)
}

// ApplyMirrorSetting stores the prefixes used after GitHub fails.
func (c *Client) ApplyMirrorSetting(setting string) {
	if c == nil {
		return
	}
	c.Mirrors = MirrorSetting(setting, DefaultMirrors)
}

func (c *Client) candidateURLs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("缺少地址")
	}
	out := []string{raw}
	seen := map[string]bool{raw: true}
	if c == nil {
		return out, nil
	}
	for _, prefix := range c.Mirrors {
		next, err := prefixURL(prefix, raw)
		if err != nil || seen[next] {
			continue
		}
		seen[next] = true
		out = append(out, next)
	}
	return out, nil
}

// prefixURL builds a ghproxy-style URL: prefix + original URL.
func prefixURL(prefix, raw string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", errors.New("空镜像")
	}
	if !strings.Contains(prefix, "://") {
		prefix = "https://" + prefix
	}
	u, err := url.Parse(prefix)
	if err != nil || u.Hostname() == "" {
		return "", fmt.Errorf("镜像地址无效")
	}
	if u.Scheme != "https" && !isLoopback(u.Hostname()) {
		return "", fmt.Errorf("镜像必须是 HTTPS")
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return prefix + raw, nil
}

func (c *Client) allowsMirror(u *url.URL) bool {
	if c == nil || u == nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	for _, prefix := range c.Mirrors {
		normalized := prefix
		if !strings.Contains(normalized, "://") {
			normalized = "https://" + normalized
		}
		pu, err := url.Parse(normalized)
		if err != nil || pu.Hostname() == "" {
			continue
		}
		if !strings.EqualFold(pu.Hostname(), host) {
			continue
		}
		if isLoopback(host) {
			return true
		}
		return u.Scheme == "https"
	}
	return false
}

func (c *Client) allowCandidate(raw string, primary bool, tag, name string) error {
	if primary {
		return c.allowAssetURL(raw, tag, name)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if isLoopback(u.Hostname()) {
		return nil
	}
	if c.allowsMirror(u) {
		return nil
	}
	return fmt.Errorf("拒绝的镜像 %s", u.Host)
}

func (c *Client) get(ctx context.Context, endpoint, accept string, limit int64) ([]byte, error) {
	urls, err := c.candidateURLs(endpoint)
	if err != nil {
		return nil, err
	}
	var last error
	for _, raw := range urls {
		attempt, cancel := context.WithTimeout(ctx, apiAttempt)
		body, err := c.getOne(attempt, raw, accept, limit)
		cancel()
		if err == nil {
			return body, nil
		}
		last = err
		if !shouldFallback(err) {
			return nil, err
		}
	}
	if last == nil {
		last = errors.New("请求 GitHub 失败")
	}
	return nil, last
}

func (c *Client) getOne(ctx context.Context, endpoint, accept string, limit int64) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	if err := c.allow(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.decorate(req, accept)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub 失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, &statusError{code: resp.StatusCode, msg: "GitHub 响应过大"}
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, &statusError{code: resp.StatusCode, msg: "没有已发布的正式版本"}
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, &statusError{
			code:    resp.StatusCode,
			msg:     "GitHub API 拒绝了这次未登录的请求（公开仓库每小时次数有限）",
			tryNext: true,
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{
			code:    resp.StatusCode,
			msg:     fmt.Sprintf("GitHub API 返回 HTTP %d", resp.StatusCode),
			tryNext: resp.StatusCode >= 500,
		}
	}
	return body, nil
}

func (c *Client) downloadFile(ctx context.Context, tag string, asset Asset, dest string) error {
	return c.fetchAsset(ctx, tag, asset, dest, nil)
}

// downloadVerified downloads asset from GitHub, then mirrors, and checks wants.
// A finished download that does not match is discarded and the next mirror is tried.
func (c *Client) downloadVerified(ctx context.Context, tag string, asset Asset, dest string, wants ...string) error {
	return c.fetchAsset(ctx, tag, asset, dest, wants)
}

func (c *Client) fetchAsset(ctx context.Context, tag string, asset Asset, dest string, wants []string) error {
	if asset.Name == "" || asset.Name != filepath.Base(asset.Name) || strings.Contains(asset.Name, "..") {
		return fmt.Errorf("拒绝的文件名 %q", asset.Name)
	}
	if asset.Size > maxDownload {
		return fmt.Errorf("%s 超过大小限制", asset.Name)
	}
	urls, err := c.candidateURLs(asset.BrowserDownloadURL)
	if err != nil {
		return err
	}
	var last error
	for i, raw := range urls {
		if err := c.allowCandidate(raw, i == 0, tag, asset.Name); err != nil {
			last = err
			continue
		}
		os.Remove(dest)
		os.Remove(dest + ".partial")
		if err := c.downloadWithResume(ctx, raw, asset, dest); err != nil {
			last = err
			os.Remove(dest)
			os.Remove(dest + ".partial")
			if !shouldFallback(err) {
				return err
			}
			continue
		}
		if len(wants) == 0 {
			return nil
		}
		if err := verifyFile(dest, wants...); err != nil {
			last = fmt.Errorf("%s: %w", asset.Name, err)
			os.Remove(dest)
			os.Remove(dest + ".partial")
			continue
		}
		return nil
	}
	os.Remove(dest)
	os.Remove(dest + ".partial")
	if last == nil {
		last = errors.New("下载失败")
	}
	return last
}

func (c *Client) downloadWithResume(ctx context.Context, raw string, asset Asset, dest string) error {
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		try, cancel := context.WithTimeout(ctx, downloadAttempt)
		err := c.downloadOnce(try, raw, asset, dest)
		cancel()
		if err == nil {
			return nil
		}
		last = err
		if !retryableDownload(err) {
			os.Remove(dest + ".partial")
			return err
		}
	}
	os.Remove(dest + ".partial")
	if last == nil {
		last = errors.New("下载失败")
	}
	return last
}

func (c *Client) downloadOnce(ctx context.Context, raw string, asset Asset, dest string) error {
	tmp := dest + ".partial"
	var offset int64
	if info, err := os.Stat(tmp); err == nil && !info.IsDir() {
		offset = info.Size()
	}
	if asset.Size > 0 && offset > asset.Size {
		os.Remove(tmp)
		offset = 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	c.decorate(req, "application/octet-stream")
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("下载 %s 失败: %w", asset.Name, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		offset = 0
	case http.StatusPartialContent:
		if offset <= 0 {
			return &statusError{code: resp.StatusCode, msg: fmt.Sprintf("下载 %s 失败: HTTP 206", asset.Name), tryNext: true}
		}
	case http.StatusRequestedRangeNotSatisfiable:
		os.Remove(tmp)
		return &shortError{got: 0, want: asset.Size}
	default:
		tryNext := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound
		return &statusError{code: resp.StatusCode, msg: fmt.Sprintf("下载 %s 失败: HTTP %d", asset.Name, resp.StatusCode), tryNext: tryNext}
	}

	flag := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(tmp, flag, 0o644)
	if err != nil {
		return err
	}
	remain := maxDownload - offset
	if remain < 0 {
		remain = 0
	}
	writer := &progressWriter{w: f, base: offset, total: asset.Size, name: asset.Name, fn: c.OnProgress}
	n, copyErr := io.Copy(writer, io.LimitReader(resp.Body, remain+1))
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > remain {
		os.Remove(tmp)
		return fmt.Errorf("%s 超过大小限制", asset.Name)
	}
	got := offset + n
	if asset.Size > 0 && got != asset.Size {
		return &shortError{got: got, want: asset.Size}
	}
	if asset.Size > maxDownload || got > maxDownload {
		os.Remove(tmp)
		return fmt.Errorf("%s 超过大小限制", asset.Name)
	}
	os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	if c.OnProgress != nil {
		total := asset.Size
		if total <= 0 {
			total = got
		}
		c.OnProgress(Progress{Name: asset.Name, Done: got, Total: total})
	}
	return nil
}

type progressWriter struct {
	w     io.Writer
	base  int64
	n     int64
	total int64
	name  string
	fn    func(Progress)
	last  time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.n += int64(n)
	if p.fn != nil && (p.last.IsZero() || time.Since(p.last) > 100*time.Millisecond) {
		p.last = time.Now()
		p.fn(Progress{Name: p.name, Done: p.base + p.n, Total: p.total})
	}
	return n, err
}

func shouldFallback(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var se *statusError
	if errors.As(err, &se) {
		return se.tryNext
	}
	return true
}

func retryableDownload(err error) bool {
	if err == nil {
		return false
	}
	var short *shortError
	if errors.As(err, &short) {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}
