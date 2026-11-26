package update

import (
	"context"
	"encoding/json"
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
	apiAttempt         = 12 * time.Second
	smallAttempt       = 10 * time.Second
	downloadAttempt    = 4 * time.Minute
	minRangeAssetBytes = 1 << 20
)

// Progress is one download progress callback.
type Progress struct {
	Name        string
	Done        int64
	Total       int64
	BytesPerSec float64
	Source      string
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

type downloadCandidate struct {
	url     string
	label   string
	primary bool
}

// ApplyMirrorSetting stores the mirror prefixes and preference derived from setting.
func (c *Client) ApplyMirrorSetting(setting string) {
	if c == nil {
		return
	}
	c.Mirrors = MirrorSetting(setting, DefaultMirrors)
	c.PreferMirror = HasExplicitMirror(setting)
}

func (c *Client) candidateEntries(raw string) ([]downloadCandidate, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("缺少地址")
	}
	primary := downloadCandidate{
		url:     raw,
		label:   candidateLabel(raw, true),
		primary: true,
	}
	if c == nil || len(c.Mirrors) == 0 {
		return []downloadCandidate{primary}, nil
	}
	seen := map[string]bool{raw: true}
	var mirrors []downloadCandidate
	for _, prefix := range c.Mirrors {
		next, err := prefixURL(prefix, raw)
		if err != nil || seen[next] {
			continue
		}
		seen[next] = true
		mirrors = append(mirrors, downloadCandidate{
			url:     next,
			label:   candidateLabel(next, false),
			primary: false,
		})
	}
	if len(mirrors) == 0 {
		return []downloadCandidate{primary}, nil
	}
	// When an explicit mirror is chosen or no proxy is active, put mirrors ahead of direct GitHub.
	if c.PreferMirror || !c.hasProxy() {
		out := make([]downloadCandidate, 0, len(mirrors)+1)
		out = append(out, mirrors...)
		out = append(out, primary)
		return out, nil
	}
	out := make([]downloadCandidate, 0, len(mirrors)+1)
	out = append(out, primary)
	out = append(out, mirrors...)
	return out, nil
}

func candidateLabel(raw string, primary bool) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		if primary {
			return "GitHub"
		}
		return "镜像源"
	}
	host := strings.ToLower(u.Hostname())
	if primary && (host == "github.com" || host == "api.github.com" || strings.HasSuffix(host, ".githubusercontent.com")) {
		return "GitHub"
	}
	return host
}

func (c *Client) candidateURLs(raw string) ([]string, error) {
	cands, err := c.candidateEntries(raw)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(cands))
	for i, cand := range cands {
		out[i] = cand.url
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
		normalized := strings.TrimSpace(prefix)
		if normalized == "" {
			continue
		}
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
	return c.getValidated(ctx, endpoint, accept, limit, nil)
}

type raceResult struct {
	body     []byte
	err      error
	terminal bool
}

func (c *Client) getValidated(ctx context.Context, endpoint, accept string, limit int64, validate func(body []byte, finalURL string) error) ([]byte, error) {
	cands, err := c.candidateEntries(endpoint)
	if err != nil {
		return nil, err
	}
	if len(cands) == 1 {
		attempt, cancel := context.WithTimeout(ctx, apiAttempt)
		defer cancel()
		body, finalURL, err := c.getOne(attempt, cands[0], accept, limit)
		if err != nil {
			return nil, err
		}
		if validate != nil {
			if err := validate(body, finalURL); err != nil {
				return nil, err
			}
		}
		return body, nil
	}

	raceCtx, cancelRace := context.WithCancel(ctx)
	defer cancelRace()

	ch := make(chan raceResult, len(cands))
	stagger := time.Duration(0)
	if c.PreferMirror || c.hasProxy() {
		stagger = 220 * time.Millisecond
	}

	launch := func(cand downloadCandidate) {
		go func() {
			attempt, cancel := context.WithTimeout(raceCtx, apiAttempt)
			defer cancel()
			body, finalURL, err := c.getOne(attempt, cand, accept, limit)
			if err == nil && validate != nil {
				if verr := validate(body, finalURL); verr != nil {
					err = verr
				}
			}
			terminal := err != nil && (!shouldFallback(err) || errors.Is(err, errReleaseNotEligible))
			ch <- raceResult{body: body, err: err, terminal: terminal}
		}()
	}

	launched := 1
	launch(cands[0])
	if stagger > 0 {
		timer := time.NewTimer(stagger)
		select {
		case res := <-ch:
			timer.Stop()
			if res.err == nil {
				return res.body, nil
			}
			if res.terminal {
				return nil, res.err
			}
			for _, cand := range cands[1:] {
				launch(cand)
			}
			launched = len(cands)
			return awaitRaceResults(ch, launched-1, res.err)
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
	for _, cand := range cands[1:] {
		launch(cand)
		launched++
	}
	return awaitRaceResults(ch, launched, nil)
}

func awaitRaceResults(ch <-chan raceResult, remaining int, firstErr error) ([]byte, error) {
	lastErr := firstErr
	for i := 0; i < remaining; i++ {
		res := <-ch
		if res.err == nil {
			return res.body, nil
		}
		if res.terminal {
			return nil, res.err
		}
		lastErr = res.err
	}
	if lastErr == nil {
		lastErr = errors.New("请求 GitHub 失败")
	}
	return nil, lastErr
}

func (c *Client) getOne(ctx context.Context, cand downloadCandidate, accept string, limit int64) ([]byte, string, error) {
	u, err := url.Parse(cand.url)
	if err != nil {
		return nil, "", err
	}
	if err := c.allow(u); err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cand.url, nil)
	if err != nil {
		return nil, "", err
	}
	c.decorate(req, accept)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("请求 GitHub 失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > limit {
		return nil, "", &statusError{code: resp.StatusCode, msg: "GitHub 响应过大", tryNext: !cand.primary}
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", &statusError{code: resp.StatusCode, msg: "没有已发布的正式版本", tryNext: !cand.primary}
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, "", &statusError{
			code:    resp.StatusCode,
			msg:     "GitHub API 拒绝了这次未登录的请求（公开仓库每小时次数有限）",
			tryNext: true,
		}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", &statusError{
			code:    resp.StatusCode,
			msg:     fmt.Sprintf("GitHub API 返回 HTTP %d", resp.StatusCode),
			tryNext: resp.StatusCode >= 500 || !cand.primary,
		}
	}
	finalURL := cand.url
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return body, finalURL, nil
}

func (c *Client) discoverLatestTag(ctx context.Context, latestURL string) (string, error) {
	cands, err := c.candidateEntries(latestURL)
	if err != nil {
		return "", err
	}
	raceCtx, cancelRace := context.WithCancel(ctx)
	defer cancelRace()

	type tagResult struct {
		tag string
		err error
	}
	ch := make(chan tagResult, len(cands))
	for _, cand := range cands {
		go func(cand downloadCandidate) {
			attempt, cancel := context.WithTimeout(raceCtx, apiAttempt)
			defer cancel()
			body, finalURL, err := c.getOne(attempt, cand, "application/json, text/html;q=0.9", maxReleaseJSON)
			if err != nil {
				ch <- tagResult{err: err}
				return
			}
			tag := extractReleaseTag(body, finalURL, c.owner(), c.repo())
			if tag == "" {
				ch <- tagResult{err: errors.New("未找到最新版本标签")}
				return
			}
			ch <- tagResult{tag: tag}
		}(cand)
	}
	var lastErr error
	for range cands {
		res := <-ch
		if res.err == nil {
			return res.tag, nil
		}
		lastErr = res.err
	}
	if lastErr == nil {
		lastErr = errors.New("未能获取最新版本")
	}
	return "", lastErr
}

func extractReleaseTag(body []byte, finalURL, owner, repo string) string {
	var meta struct {
		TagName string `json:"tag_name"`
	}
	if json.Unmarshal(body, &meta) == nil && strings.TrimSpace(meta.TagName) != "" {
		tag := strings.TrimSpace(meta.TagName)
		if _, err := ParseVersion(tag); err == nil {
			return tag
		}
	}
	if tag := tagFromPath(finalURL); tag != "" {
		return tag
	}
	marker := "/" + owner + "/" + repo + "/releases/tag/"
	text := string(body)
	for {
		idx := strings.Index(text, marker)
		if idx < 0 {
			return ""
		}
		rest := text[idx+len(marker):]
		end := strings.IndexAny(rest, "\"'<>?#& \t\r\n")
		cand := rest
		if end >= 0 {
			cand = rest[:end]
		}
		if _, err := ParseVersion(cand); err == nil {
			return cand
		}
		text = rest
	}
}

func tagFromPath(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	const needle = "/releases/tag/"
	idx := strings.Index(u.Path, needle)
	if idx < 0 {
		return ""
	}
	tag := strings.Trim(u.Path[idx+len(needle):], "/")
	if slash := strings.IndexByte(tag, '/'); slash >= 0 {
		tag = tag[:slash]
	}
	if _, err := ParseVersion(tag); err != nil {
		return ""
	}
	return tag
}

func checkChecksumsFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = ParseChecksums(string(data))
	return err
}

func (c *Client) downloadFile(ctx context.Context, tag string, asset Asset, dest string) error {
	return c.fetchAsset(ctx, tag, asset, dest, nil, nil)
}

// downloadVerified downloads asset from the fastest available source and checks wants.
// A finished download that does not match is discarded and remaining sources are tried.
func (c *Client) downloadVerified(ctx context.Context, tag string, asset Asset, dest string, wants ...string) error {
	return c.fetchAsset(ctx, tag, asset, dest, nil, wants)
}

func (c *Client) downloadChecked(ctx context.Context, tag string, asset Asset, dest string, extra func(string) error, wants ...string) error {
	return c.fetchAsset(ctx, tag, asset, dest, extra, wants)
}

func (c *Client) fetchAsset(ctx context.Context, tag string, asset Asset, dest string, extraCheck func(string) error, wants []string) error {
	if asset.Name == "" || asset.Name != filepath.Base(asset.Name) || strings.Contains(asset.Name, "..") {
		return fmt.Errorf("拒绝的文件名 %q", asset.Name)
	}
	if asset.Size > maxDownload {
		return fmt.Errorf("%s 超过大小限制", asset.Name)
	}
	cands, err := c.candidateEntries(asset.BrowserDownloadURL)
	if err != nil {
		return err
	}

	excluded := make(map[string]bool, len(cands))
	var last error
	for {
		var active []downloadCandidate
		for _, cand := range cands {
			if excluded[cand.url] {
				continue
			}
			if err := c.allowCandidate(cand.url, cand.primary, tag, asset.Name); err != nil {
				last = err
				excluded[cand.url] = true
				continue
			}
			active = append(active, cand)
		}
		if len(active) == 0 {
			break
		}

		// Small assets (<1 MiB) and tiny fixtures use direct GET+resume
		// without initial Range headers so tiny transfers finish in 1 RTT.
		if asset.Size > 0 && asset.Size < minRangeAssetBytes {
			cand := active[0]
			os.Remove(dest)
			os.Remove(dest + ".partial")
			maxRetries := 3
			if len(active) > 1 {
				maxRetries = 1
			}
			if err := c.downloadWithResume(ctx, cand, asset, dest, maxRetries); err != nil {
				last = err
				excluded[cand.url] = true
				os.Remove(dest)
				os.Remove(dest + ".partial")
				if !shouldFallback(err) {
					return err
				}
				continue
			}
			if err := validateDownloadedFile(dest, asset.Name, extraCheck, wants); err != nil {
				last = err
				excluded[cand.url] = true
				os.Remove(dest)
				os.Remove(dest + ".partial")
				continue
			}
			return nil
		}

		usedURLs, err := c.downloadLargeAsset(ctx, active, asset, dest)
		if err != nil {
			last = err
			os.Remove(dest)
			os.Remove(dest + ".partial")
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return err
			}
			for _, u := range usedURLs {
				excluded[u] = true
			}
			if len(usedURLs) == 0 {
				for _, cand := range active {
					excluded[cand.url] = true
				}
			}
			continue
		}
		if err := validateDownloadedFile(dest, asset.Name, extraCheck, wants); err != nil {
			last = err
			os.Remove(dest)
			os.Remove(dest + ".partial")
			for _, u := range usedURLs {
				excluded[u] = true
			}
			if len(usedURLs) == 0 {
				for _, cand := range active {
					excluded[cand.url] = true
				}
			}
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

func validateDownloadedFile(dest, name string, extraCheck func(string) error, wants []string) error {
	if len(wants) > 0 {
		if err := verifyFile(dest, wants...); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if extraCheck != nil {
		if err := extraCheck(dest); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

func (c *Client) downloadWithResume(ctx context.Context, cand downloadCandidate, asset Asset, dest string, maxRetries int) error {
	if maxRetries < 1 {
		maxRetries = 1
	}
	var last error
	timeout := downloadAttempt
	if asset.Size > 0 && asset.Size < minRangeAssetBytes {
		timeout = smallAttempt
	}
	for attempt := 0; attempt < maxRetries; attempt++ {
		try, cancel := context.WithTimeout(ctx, timeout)
		err := c.downloadOnce(try, cand, asset, dest)
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

func (c *Client) downloadOnce(ctx context.Context, cand downloadCandidate, asset Asset, dest string) error {
	tmp := dest + ".partial"
	var offset int64
	if info, err := os.Stat(tmp); err == nil && !info.IsDir() {
		offset = info.Size()
	}
	if asset.Size > 0 && offset > asset.Size {
		os.Remove(tmp)
		offset = 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cand.url, nil)
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
	writer := &progressWriter{
		w:      f,
		base:   offset,
		total:  asset.Size,
		name:   asset.Name,
		source: cand.label,
		meter:  newSpeedMeter(),
		fn:     c.OnProgress,
	}
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
		c.OnProgress(Progress{Name: asset.Name, Done: got, Total: total, Source: cand.label})
	}
	return nil
}

type progressWriter struct {
	w      io.Writer
	base   int64
	n      int64
	total  int64
	name   string
	source string
	meter  *speedMeter
	fn     func(Progress)
	last   time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.n += int64(n)
	var bps float64
	if p.meter != nil && n > 0 {
		bps = p.meter.record(p.base + p.n)
	}
	if p.fn != nil && (p.last.IsZero() || time.Since(p.last) > 120*time.Millisecond) {
		p.last = time.Now()
		p.fn(Progress{Name: p.name, Done: p.base + p.n, Total: p.total, BytesPerSec: bps, Source: p.source})
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
