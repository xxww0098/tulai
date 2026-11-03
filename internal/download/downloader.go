// Package download 把一个地址下载到磁盘上的一个文件。
//
// 它只负责单次传输：断点续传、归档占用、每个站点的并发额度、总带宽、同一路径的互斥和重试。
// 任务的生命周期、抽取和页面抓取不在这里。
package download

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Item 是要下载的一个条目。
type Item struct {
	URL           string
	AlternateURLs []string          // 仅主地址返回 404 时依次尝试同源地址
	Filename      string            // 非空时直接用这个名字，不再套模板
	Extension     string            // 不带点
	Headers       map[string]string // 额外请求头，例如 Referer
	ArchiveKey    string            // 非空时用于去重
	Metadata      map[string]string // 给文件名模板用
	RequireImage  bool              // 网页发现的条目必须是图片，不能把错误页当图片保存
	PublicOnly    bool              // 搜索结果不接受显式本地地址或本地重定向
}

// Job 是这次下载的目标。
type Job struct {
	Directory   string
	Overwrite   bool
	ImageFilter ImageFilter
}

// ImageFilter 限制文件大小和图片宽高；0 表示不限制，边界值包含在内。
type ImageFilter struct {
	MinBytes, MaxBytes                       int64
	MinWidth, MaxWidth, MinHeight, MaxHeight int
}

// Validate 检查筛选上下限，空的上限表示不限制。
func (f ImageFilter) Validate() error {
	if f.MinBytes < 0 || f.MaxBytes < 0 || f.MinWidth < 0 || f.MaxWidth < 0 || f.MinHeight < 0 || f.MaxHeight < 0 {
		return errors.New("图片筛选条件不能为负数")
	}
	if (f.MaxBytes > 0 && f.MinBytes > f.MaxBytes) ||
		(f.MaxWidth > 0 && f.MinWidth > f.MaxWidth) ||
		(f.MaxHeight > 0 && f.MinHeight > f.MaxHeight) {
		return errors.New("图片筛选的最小值不能大于最大值")
	}
	return nil
}

// Progress 是一次传输的进展。
type Progress struct {
	URL          string
	Filename     string
	Stage        string // skip start writing done error retry
	BytesWritten int64
	Total        int64 // 小于 0 表示服务器没有给长度
	Err          error
}

// ProgressFunc 接收进展。可以为空。
type ProgressFunc func(Progress)

// Options 是下载器的依赖。Getter 是唯一必需的。
type Options struct {
	Getter    Getter
	Archive   Archive
	Template  Template
	HostLimit *HostLimiter
	Bandwidth *BandwidthLimiter
	PathLock  *PathLocker
	Retries   int
	RetryBase time.Duration
	ChunkSize int                         // 一次读取的上限，小于等于 0 时用 128KiB
	Wait      func(context.Context) error // 暂停钩子，返回时表示可以继续
}

// Downloader 下载单个条目。
type Downloader struct{ opt Options }

// New 建一个下载器。
func New(opt Options) *Downloader { return &Downloader{opt: opt} }

// Download 把 item 下载到 job 的目录里，按 ShouldRetry 的规则重试。
func (d *Downloader) Download(ctx context.Context, job Job, item Item, progress ProgressFunc) error {
	if err := job.ImageFilter.Validate(); err != nil {
		return &LocalError{Err: err}
	}
	var err error
	for attempt := 0; attempt <= d.opt.Retries; attempt++ {
		err = d.once(ctx, job, item, progress)
		if err == nil || attempt == d.opt.Retries || ctx.Err() != nil || !ShouldRetry(err) {
			return err
		}
		delay := RetryDelay(err, attempt, d.opt.RetryBase)
		var status *HTTPStatusError
		if errors.As(err, &status) && d.opt.HostLimit != nil {
			target := status.URL
			if target == "" {
				target = item.URL
			}
			d.opt.HostLimit.Cooldown(target, delay)
		}
		send(ctx, progress, Progress{URL: item.URL, Stage: "retry", Err: err})
		if waitErr := Wait(ctx, delay); waitErr != nil {
			return waitErr
		}
	}
	return err
}

func (d *Downloader) once(ctx context.Context, job Job, item Item, progress ProgressFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// 1. 占用归档键。失败时退回，成功才保留。
	var complete func(bool)
	committed := false
	if !job.Overwrite && d.opt.Archive != nil && item.ArchiveKey != "" {
		already, done := d.opt.Archive.Acquire(item.ArchiveKey)
		if already {
			send(ctx, progress, Progress{URL: item.URL, Stage: "skip", Filename: "[archived]"})
			return ErrSkipped
		}
		complete = done
	}
	commit := func() {
		if complete != nil {
			complete(true)
			committed = true
		} else if d.opt.Archive != nil && item.ArchiveKey != "" {
			d.opt.Archive.Add(item.ArchiveKey)
		}
	}
	defer func() {
		if !committed && complete != nil {
			complete(false)
		}
	}()

	// 2. 解析目标路径。
	name, err := d.renderName(item)
	if err != nil {
		return &LocalError{Err: err}
	}
	dest := filepath.Join(job.Directory, filepath.FromSlash(name))
	unlock := func() {}
	if d.opt.PathLock != nil {
		lockPath := dest
		if hasImageExtension(dest) {
			lockPath = strings.TrimSuffix(dest, filepath.Ext(dest))
		}
		unlock, err = d.opt.PathLock.Lock(ctx, lockPath)
		if err != nil {
			return err
		}
	}
	defer unlock()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return &LocalError{Err: fmt.Errorf("创建目录: %w", err)}
	}

	// 3. 目标已存在且不覆盖时跳过。
	if info, statErr := os.Stat(dest); statErr == nil && !info.IsDir() && info.Size() > 0 && !job.Overwrite {
		send(ctx, progress, Progress{URL: item.URL, Stage: "skip", Filename: dest})
		return ErrSkipped
	}
	if item.RequireImage && !hasImageExtension(dest) && !job.Overwrite {
		for _, ext := range []string{".jpg", ".png", ".gif", ".webp", ".bmp", ".ico", ".avif", ".tiff", ".heic", ".svg"} {
			if info, statErr := os.Stat(dest + ext); statErr == nil && !info.IsDir() && info.Size() > 0 {
				send(ctx, progress, Progress{URL: item.URL, Stage: "skip", Filename: dest + ext})
				return ErrSkipped
			}
		}
	}

	sources, err := itemSources(item)
	if err != nil {
		return &LocalError{Err: err}
	}

	// 4. 从 .part 续传。
	partPath := dest + ".part"
	if job.Overwrite {
		_ = DiscardPartial(partPath)
	}
	loaded, err := loadPartialForSources(partPath, item.URL, sources)
	if err != nil {
		return &LocalError{Err: err}
	}
	offset := int64(0)
	meta := PartialMeta{URL: item.URL, Total: -1}
	sourceIndex := 0
	havePartial := loaded != nil
	if loaded != nil {
		offset = loaded.Size
		meta = loaded.Meta
		if meta.SourceURL != "" {
			for i, source := range sources {
				if source == meta.SourceURL {
					sourceIndex = i
					break
				}
			}
		}
	}

	// 5. 申请这个站点的并发名额。
	releaseHost := func() {}
	if d.opt.HostLimit != nil {
		releaseHost, err = d.opt.HostLimit.Acquire(ctx, item.URL)
		if err != nil {
			return err
		}
	}
	defer releaseHost()

	send(ctx, progress, Progress{URL: item.URL, Filename: dest, Stage: "start"})

	var resp *Response
	tried := make([]bool, len(sources))
	restarted := false
	for {
		source := sources[sourceIndex]
		resp, err = d.request(ctx, item, source, offset, meta)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusNotFound && len(sources) > 1 {
			_ = resp.Body.Close()
			if err := ctx.Err(); err != nil {
				return err
			}
			if havePartial {
				if err := DiscardPartial(partPath); err != nil {
					return &LocalError{Err: fmt.Errorf("清理失效来源的半成品: %w", err)}
				}
				offset = 0
				meta = PartialMeta{URL: item.URL, Total: -1}
				havePartial = false
			}
			tried[sourceIndex] = true
			next := -1
			for i := range sources {
				if !tried[i] {
					next = i
					break
				}
			}
			if next < 0 {
				return &HTTPStatusError{URL: firstNonEmpty(resp.FinalURL, source), StatusCode: resp.StatusCode,
					RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
			}
			sourceIndex, restarted = next, false
			continue
		}

		// 续传响应必须来自同一地址，并且从正确的位置开始。
		restart := resp.StatusCode == http.StatusRequestedRangeNotSatisfiable
		if offset > 0 && resp.StatusCode == http.StatusPartialContent {
			span, spanErr := parseContentRange(resp.Header.Get("Content-Range"))
			validatorChanged := (meta.ETag != "" && resp.Header.Get("ETag") != meta.ETag) ||
				(meta.ETag == "" && meta.LastModified != "" && resp.Header.Get("Last-Modified") != meta.LastModified) ||
				(meta.FinalURL != "" && meta.FinalURL != firstNonEmpty(resp.FinalURL, source))
			length := contentLength(resp)
			restart = spanErr != nil || span == nil || span.Start != offset || validatorChanged ||
				(meta.Total > 0 && meta.Total != span.Total) ||
				(length >= 0 && length != span.End-span.Start+1)
		}
		if (restart || (offset > 0 && resp.StatusCode == http.StatusOK)) && !restarted {
			_ = resp.Body.Close()
			if err := DiscardPartial(partPath); err != nil {
				return &LocalError{Err: fmt.Errorf("清理不匹配的半成品: %w", err)}
			}
			offset = 0
			meta = PartialMeta{URL: item.URL, Total: -1}
			havePartial = false
			restarted = true
			continue
		}
		break
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return &HTTPStatusError{
			URL:        firstNonEmpty(resp.FinalURL, sources[sourceIndex]),
			StatusCode: resp.StatusCode,
			RetryAfter: ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
		}
	}

	total := contentLength(resp)
	if resp.StatusCode == http.StatusPartialContent {
		span, spanErr := parseContentRange(resp.Header.Get("Content-Range"))
		length := contentLength(resp)
		if spanErr != nil || span == nil || span.Start != offset || (length >= 0 && length != span.End-span.Start+1) {
			detail := "范围起点不符"
			if spanErr != nil {
				detail = spanErr.Error()
			}
			return &IntegrityError{Err: fmt.Errorf("Content-Range 无效: %s", detail)}
		}
		total = span.Total
	} else {
		offset = 0
	}

	// 6. 写入 .part。
	flag := os.O_CREATE | os.O_WRONLY
	if resp.StatusCode == http.StatusPartialContent && offset > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	out, err := os.OpenFile(partPath, flag, 0o644)
	if err != nil {
		return &LocalError{Err: fmt.Errorf("打开半成品: %w", err)}
	}
	closed := false
	closeOut := func() {
		if !closed {
			_ = out.Close()
			closed = true
		}
	}
	defer closeOut()

	selectedSource := sources[sourceIndex]
	if selectedSource == item.URL {
		selectedSource = ""
	}
	if err := WritePartialMeta(partPath, PartialMeta{
		URL:          item.URL,
		SourceURL:    selectedSource,
		FinalURL:     firstNonEmpty(resp.FinalURL, sources[sourceIndex]),
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		Total:        total,
	}); err != nil {
		return &LocalError{Err: fmt.Errorf("写半成品元数据: %w", err)}
	}

	written, err := d.stream(ctx, resp.Body, out, progress, item.URL, dest, offset, total)
	if err != nil {
		if ctx.Err() == nil {
			send(ctx, progress, Progress{URL: item.URL, Filename: dest, Stage: "error", Err: err})
		}
		return err
	}
	if err := out.Sync(); err != nil {
		return &LocalError{Err: fmt.Errorf("落盘半成品: %w", err)}
	}
	closeOut()

	if total >= 0 && written+offset != total {
		err := &IntegrityError{Err: fmt.Errorf("长度不符: 写了 %d，应为 %d", written+offset, total)}
		send(ctx, progress, Progress{URL: item.URL, Filename: dest, Stage: "error", Err: err})
		return err
	}
	info, err := os.Stat(partPath)
	if err != nil {
		return &LocalError{Err: fmt.Errorf("检查半成品: %w", err)}
	}
	if total >= 0 && info.Size() != total {
		err := &IntegrityError{Err: fmt.Errorf("半成品长度是 %d，应为 %d", info.Size(), total)}
		send(ctx, progress, Progress{URL: item.URL, Filename: dest, Stage: "error", Err: err})
		return err
	}
	if item.RequireImage {
		f, openErr := os.Open(partPath)
		if openErr != nil {
			return &LocalError{Err: openErr}
		}
		var header [512]byte
		n, readErr := io.ReadFull(f, header[:])
		_ = f.Close()
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return &LocalError{Err: readErr}
		}
		data := header[:n]
		ext := imageExtension(data)
		if ext == "" {
			_ = DiscardPartial(partPath)
			return errors.New("服务器返回的内容不是图片")
		}
		originalDest := dest
		if !hasImageExtension(dest) {
			dest += ext
		} else {
			current := strings.ToLower(filepath.Ext(dest))
			switch current {
			case ".jpeg":
				current = ".jpg"
			case ".tif":
				current = ".tiff"
			case ".heif":
				current = ".heic"
			}
			if current != ext {
				dest = strings.TrimSuffix(dest, filepath.Ext(dest)) + ext
			}
		}
		if dest != originalDest {
			if info, statErr := os.Stat(dest); statErr == nil && !info.IsDir() && info.Size() > 0 && !job.Overwrite {
				_ = DiscardPartial(partPath)
				send(ctx, progress, Progress{URL: item.URL, Stage: "skip", Filename: dest})
				return ErrSkipped
			}
		}
	}
	matched, filterErr := job.ImageFilter.matches(partPath, info.Size())
	if err := ctx.Err(); err != nil {
		return err
	}
	if filterErr != nil || !matched {
		if cleanupErr := DiscardPartial(partPath); cleanupErr != nil {
			return &LocalError{Err: fmt.Errorf("清理被筛选的半成品: %w", cleanupErr)}
		}
		if filterErr != nil {
			return &LocalError{Err: filterErr}
		}
		send(ctx, progress, Progress{URL: item.URL, Stage: "skip", Err: ErrFiltered})
		return ErrFiltered
	}

	// 7. 改名提交，再删元数据。
	if err := replace(partPath, dest); err != nil {
		return &LocalError{Err: fmt.Errorf("提交半成品: %w", err)}
	}
	_ = os.Remove(partialMetaPath(partPath))

	// 8. 记入归档。
	commit()
	send(ctx, progress, Progress{
		URL: item.URL, Filename: dest, Stage: "done", BytesWritten: written + offset, Total: total,
	})
	return nil
}

func (f ImageFilter) matches(partPath string, size int64) (bool, error) {
	if (f.MinBytes > 0 && size < f.MinBytes) || (f.MaxBytes > 0 && size > f.MaxBytes) {
		return false, nil
	}
	if f.MinWidth == 0 && f.MaxWidth == 0 && f.MinHeight == 0 && f.MaxHeight == 0 {
		return true, nil
	}
	file, err := os.Open(partPath)
	if err != nil {
		return false, fmt.Errorf("无法读取图片尺寸: %w", err)
	}
	config, _, err := image.DecodeConfig(file)
	_ = file.Close()
	if err != nil {
		return false, fmt.Errorf("无法读取图片尺寸（格式可能不受支持）: %w", err)
	}
	return (f.MinWidth == 0 || config.Width >= f.MinWidth) &&
		(f.MaxWidth == 0 || config.Width <= f.MaxWidth) &&
		(f.MinHeight == 0 || config.Height >= f.MinHeight) &&
		(f.MaxHeight == 0 || config.Height <= f.MaxHeight), nil
}

func hasImageExtension(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".ico", ".avif", ".tif", ".tiff", ".heic", ".heif", ".svg":
		return true
	}
	return false
}

func imageExtension(data []byte) string {
	if len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "avif", "avis":
			return ".avif"
		case "heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1":
			return ".heic"
		}
	}
	if len(data) >= 4 && (string(data[:4]) == "II*\x00" || string(data[:4]) == "MM\x00*") {
		return ".tiff"
	}
	start := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(string(data), "\xef\xbb\xbf")))
	if strings.HasPrefix(start, "<svg") || (strings.HasPrefix(start, "<?xml") && strings.Contains(start, "<svg")) {
		return ".svg"
	}
	return map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/gif": ".gif", "image/webp": ".webp", "image/bmp": ".bmp", "image/vnd.microsoft.icon": ".ico"}[http.DetectContentType(data)]
}

func itemSources(item Item) ([]string, error) {
	sources := []string{item.URL}
	if len(item.AlternateURLs) == 0 {
		return sources, nil
	}
	parse := func(raw string) (*url.URL, error) {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" ||
			(!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
			return nil, errors.New("备用图片地址及其主地址必须是不含账号密码的 HTTP / HTTPS 地址")
		}
		return u, nil
	}
	primary, err := parse(item.URL)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{item.URL: true}
	for _, raw := range item.AlternateURLs {
		u, err := parse(raw)
		if err != nil {
			return nil, err
		}
		if !sameOrigin(primary, u) {
			return nil, errors.New("备用图片地址必须与主地址同源")
		}
		if !seen[raw] {
			seen[raw] = true
			sources = append(sources, raw)
		}
	}
	return sources, nil
}

func (d *Downloader) request(ctx context.Context, item Item, source string, offset int64, meta PartialMeta) (*Response, error) {
	if item.PublicOnly {
		ctx = context.WithValue(ctx, publicDownloadKey{}, true)
	}
	resp, err := d.opt.Getter.Get(ctx, source, func(r *http.Request) {
		for k, v := range item.Headers {
			r.Header.Set(k, v)
		}
		if offset > 0 {
			r.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
			if meta.ETag != "" {
				r.Header.Set("If-Range", meta.ETag)
			} else if meta.LastModified != "" {
				r.Header.Set("If-Range", meta.LastModified)
			}
		}
	})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &TransferError{Err: err}
	}
	return resp, nil
}

// stream 把响应体写到文件，遵守暂停和总带宽。
func (d *Downloader) stream(ctx context.Context, body io.Reader, out *os.File, progress ProgressFunc, url, filename string, base, total int64) (int64, error) {
	chunk := d.opt.ChunkSize
	if chunk <= 0 {
		chunk = 128 * 1024
	}
	reader := bufio.NewReaderSize(body, chunk)
	buf := make([]byte, chunk)
	var written int64
	var lastSent int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		if d.opt.Wait != nil {
			if err := d.opt.Wait(ctx); err != nil {
				return written, err
			}
		}
		readSize := chunk
		if d.opt.Bandwidth != nil {
			readSize = d.opt.Bandwidth.ReadSize(readSize)
			if err := d.opt.Bandwidth.WaitN(ctx, readSize); err != nil {
				return written, err
			}
		}
		n, readErr := reader.Read(buf[:readSize])
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return written, &LocalError{Err: err}
			}
			written += int64(n)
			if written-lastSent >= 256*1024 {
				send(ctx, progress, Progress{
					URL: url, Filename: filename, Stage: "writing", BytesWritten: written + base, Total: total,
				})
				lastSent = written
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			if ctx.Err() != nil {
				return written, ctx.Err()
			}
			return written, &TransferError{Err: readErr}
		}
	}
}

func (d *Downloader) renderName(item Item) (string, error) {
	if item.Filename != "" {
		name := item.Filename
		if item.Extension != "" && !strings.HasSuffix(name, "."+item.Extension) {
			name += "." + item.Extension
		}
		if err := safeRelative(name); err != nil {
			return "", err
		}
		return name, nil
	}
	rendered := ""
	if d.opt.Template != nil {
		rendered = d.opt.Template.Render(item.Metadata)
	}
	if rendered == "" {
		rendered = fallbackName(item.URL)
	}
	if !strings.Contains(path.Base(rendered), ".") && item.Extension != "" {
		rendered += "." + item.Extension
	}
	if err := safeRelative(rendered); err != nil {
		return "", err
	}
	return rendered, nil
}

// safeRelative 拒绝绝对路径和跳出目标目录的名字。
func safeRelative(name string) error {
	if name == "" || path.IsAbs(name) || filepath.IsAbs(filepath.FromSlash(name)) {
		return fmt.Errorf("文件名不是相对路径: %q", name)
	}
	cleaned := path.Clean(name)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("文件名跳出目标目录: %q", name)
	}
	return nil
}

func fallbackName(rawURL string) string {
	u := rawURL
	if i := strings.LastIndex(u, "/"); i >= 0 {
		u = u[i+1:]
	}
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	if u == "" {
		return "file"
	}
	return u
}

func send(ctx context.Context, progress ProgressFunc, p Progress) {
	if progress != nil && ctx.Err() == nil {
		progress(p)
	}
}

func contentLength(resp *Response) int64 {
	v := resp.Header.Get("Content-Length")
	if v == "" {
		return -1
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return -1
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// contentSpan 是 Content-Range 里的一段。
type contentSpan struct {
	Start int64
	End   int64
	Total int64
}

// parseContentRange 解析 "bytes START-END/TOTAL"。
func parseContentRange(value string) (*contentSpan, error) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "bytes ") {
		return nil, errors.New("缺少 bytes 单位")
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes "), "/")
	if len(parts) != 2 || parts[1] == "*" {
		return nil, errors.New("缺少总长度")
	}
	bounds := strings.Split(parts[0], "-")
	if len(bounds) != 2 {
		return nil, errors.New("字节范围无效")
	}
	start, err1 := strconv.ParseInt(bounds[0], 10, 64)
	end, err2 := strconv.ParseInt(bounds[1], 10, 64)
	total, err3 := strconv.ParseInt(parts[1], 10, 64)
	switch {
	case err1 != nil:
		return nil, errors.New("范围起点无效")
	case err2 != nil:
		return nil, errors.New("范围终点无效")
	case err3 != nil:
		return nil, errors.New("总长度无效")
	}
	if start < 0 || end < start || total <= end {
		return nil, errors.New("字节范围前后不一致")
	}
	return &contentSpan{Start: start, End: end, Total: total}, nil
}
