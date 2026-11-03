package ui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/download"
	"github.com/xxww0098/tulai/internal/logging"
)

// dlPhase 是下载任务的状态，对着 Moza 的 pending/downloading/paused/done/failed/canceled。
type dlPhase int

type downloadBrowser interface {
	Capture(context.Context) (download.Discovery, error)
	Getter() download.Getter
	Alive() bool
	Close()
}

const (
	dlQueued dlPhase = iota
	dlActive
	dlPaused
	dlDone
	dlFailed
	dlCanceled
)

func (p dlPhase) label() string {
	switch p {
	case dlActive:
		return "下载中"
	case dlPaused:
		return "已暂停"
	case dlDone:
		return "已完成"
	case dlFailed:
		return "失败"
	case dlCanceled:
		return "已取消"
	default:
		return "等待中"
	}
}

// tone 决定状态徽标的颜色：蓝、绿、红、灰。
func (p dlPhase) tone() string {
	switch p {
	case dlActive:
		return "transfer"
	case dlDone:
		return "success"
	case dlFailed:
		return "danger"
	default:
		return "neutral"
	}
}

// terminal 表示任务已经结束，可以重试、打开目录或移除。
func (p dlPhase) terminal() bool {
	return p == dlDone || p == dlFailed || p == dlCanceled
}

// downloadTask 是队列里的一条下载，也是列表里的一行。
type downloadTask struct {
	ID             int
	URL            string
	Name           string
	Site           string
	Dir            string
	Phase          dlPhase
	Written        int64
	Total          int64
	Speed          int64
	Err            string
	Path           string
	Added          time.Time
	Checked        bool
	Depth          int
	IsPage         bool
	ImagesDone     int
	ImagesTotal    int
	ImagesFiltered int
	TargetName     string
	ImageFilter    download.ImageFilter
	Rendered       bool
	ImportedImage  bool

	cancel    context.CancelFunc
	paused    bool
	overwrite bool
	lastAt    time.Time
	lastBytes int64
}

func newDownloadTask(id int, raw, dir string) *downloadTask {
	return &downloadTask{
		ID:    id,
		URL:   raw,
		Name:  fileNameFromURL(raw),
		Site:  dlSite(raw),
		Dir:   dir,
		Phase: dlQueued,
		Added: time.Now(),
	}
}

// dlSite 是分组和标题用的站点名，去掉 www.。
func dlSite(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "其他来源"
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

func fileNameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	name := filepath.Base(u.Path)
	if name == "." || name == "/" || name == "" {
		return u.Hostname()
	}
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	return name
}

// compactURL 去掉查询串，列表里只留站点和路径，避免露出签名。
func compactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	s := u.Host + u.EscapedPath()
	if s == "" {
		return u.Host
	}
	return s
}

func taskProgress(t *downloadTask) float64 {
	if t.ImagesTotal > 0 {
		return float64(t.ImagesDone) / float64(t.ImagesTotal) * 100
	}
	if t.Total > 0 {
		p := float64(t.Written) / float64(t.Total) * 100
		if p < 0 {
			return 0
		}
		if p > 100 {
			return 100
		}
		return p
	}
	if t.Phase == dlDone {
		return 100
	}
	return 0
}

func taskSizeText(t *downloadTask) string {
	if t.ImagesTotal > 0 {
		return fmt.Sprintf("%d/%d 张", t.ImagesDone, t.ImagesTotal)
	}
	if t.Total > 0 {
		return formatBytes(t.Total)
	}
	if t.Written > 0 {
		return formatBytes(t.Written)
	}
	return "—"
}

func formatBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	switch {
	case i == 0 || v >= 100:
		return fmt.Sprintf("%.0f %s", v, units[i])
	case v >= 10:
		return fmt.Sprintf("%.1f %s", v, units[i])
	default:
		return fmt.Sprintf("%.2f %s", v, units[i])
	}
}

func formatSpeed(n int64) string {
	if n <= 0 {
		return "—"
	}
	return formatBytes(n) + "/s"
}

// formatETA 和迅雷一样给「12秒 / 3分20秒 / 1小时5分」。
func formatETA(seconds int64) string {
	if seconds < 1 {
		return "即将完成"
	}
	if seconds < 60 {
		return fmt.Sprintf("%d秒", seconds)
	}
	if seconds < 3600 {
		m, s := seconds/60, seconds%60
		if s > 0 {
			return fmt.Sprintf("%d分%d秒", m, s)
		}
		return fmt.Sprintf("%d分钟", m)
	}
	if seconds < 86400 {
		h, m := seconds/3600, (seconds%3600)/60
		if m > 0 {
			return fmt.Sprintf("%d小时%d分", h, m)
		}
		return fmt.Sprintf("%d小时", h)
	}
	d, h := seconds/86400, (seconds%86400)/3600
	if h > 0 {
		return fmt.Sprintf("%d天%d小时", d, h)
	}
	return fmt.Sprintf("%d天", d)
}

func taskETA(t *downloadTask) string {
	if t.Phase != dlActive && t.Phase != dlPaused {
		return "—"
	}
	if t.Speed <= 0 || t.Total <= t.Written {
		return "—"
	}
	return formatETA((t.Total - t.Written) / t.Speed)
}

// dlFilter 是顶部四个页签。
const (
	dlFilterOpen = iota
	dlFilterDone
	dlFilterCanceled
	dlFilterAll
)

var dlFilterLabels = []string{"正在下载", "已完成", "回收站", "下载记录"}

func taskInFilter(t *downloadTask, filter int) bool {
	switch filter {
	case dlFilterDone:
		return t.Phase == dlDone
	case dlFilterCanceled:
		return t.Phase == dlCanceled
	case dlFilterOpen:
		return t.Phase != dlDone && t.Phase != dlCanceled
	default:
		return true
	}
}

func matchesTaskSearch(t *downloadTask, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return true
	}
	hay := strings.ToLower(t.URL + " " + t.Site + " " + t.Name + " " + t.Dir + " " + t.Err)
	return strings.Contains(hay, q)
}

// downloadView 是当前页签和搜索词筛出来的任务。
func (a *App) downloadView() []*downloadTask {
	out := make([]*downloadTask, 0, len(a.dlTasks))
	for _, t := range a.dlTasks {
		if taskInFilter(t, a.dlFilter) && matchesTaskSearch(t, a.dlSearch) {
			out = append(out, t)
		}
	}
	return out
}

// dlSiteGroup 是按站点分组时的一组任务。
type dlSiteGroup struct {
	Label string
	Tasks []*downloadTask
}

func groupBySite(tasks []*downloadTask) []dlSiteGroup {
	index := map[string]int{}
	var groups []dlSiteGroup
	for _, t := range tasks {
		i, ok := index[t.Site]
		if !ok {
			i = len(groups)
			index[t.Site] = i
			groups = append(groups, dlSiteGroup{Label: t.Site})
		}
		groups[i].Tasks = append(groups[i].Tasks, t)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return strings.ToLower(groups[i].Label) < strings.ToLower(groups[j].Label)
	})
	return groups
}

// addDownloadURLs 把一段文本里的地址加进队列，返回有效和无效的行数。
func (a *App) addDownloadURLs(text string) (added, invalid int) {
	dir := a.downloadDir()
	seen := map[string]bool{}
	for _, t := range a.dlTasks {
		seen[t.URL] = true
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || download.QzoneURLHasCredentials(line) {
			invalid++
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		a.dlNextID++
		task := newDownloadTask(a.dlNextID, line, dir)
		task.Depth = clampInt(int(a.dlDepth), 1, 32767)
		task.ImageFilter = a.downloadImageFilter()
		task.Rendered = a.dlBrowserMode
		for _, existing := range a.dlTasks {
			if existing.Dir == dir && strings.EqualFold(fileNameFromURL(existing.URL), fileNameFromURL(line)) {
				task.TargetName = downloadName(fileNameFromURL(line), line)
				break
			}
		}
		a.dlTasks = append(a.dlTasks, task)
		added++
	}
	if added > 0 {
		a.dlFilter = dlFilterOpen
		a.saveDownloadRecords()
		a.pumpDownloads()
	}
	return added, invalid
}

// pumpDownloads 按并发设置派任务，超出的留在「等待中」。
func (a *App) pumpDownloads() {
	if a.renameUpdateActive() {
		return
	}
	defer a.maybeInstallPending()
	limit := clampInt(int(a.downloadConc), 1, 8)
	active := 0
	for _, t := range a.dlTasks {
		if t.Phase == dlActive {
			active++
		}
	}
	for _, t := range a.dlTasks {
		if active >= limit {
			break
		}
		if t.Phase == dlQueued {
			a.startDownloadTask(t)
			active++
		}
	}
}

// startDownloadTask 起一条下载。只改状态，真正跑在协程里。
func (a *App) startDownloadTask(t *downloadTask) {
	if t == nil || t.Phase == dlActive {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.downloadsRunning++
	t.cancel = cancel
	t.Phase = dlActive
	t.Err = ""
	t.Speed = 0
	t.ImagesFiltered = 0
	t.lastAt = time.Time{}
	t.lastBytes = t.Written
	a.saveDownloadRecords()

	opts := a.downloadOptions()
	var rendered downloadBrowser
	if t.Rendered {
		if a.dlBrowser == nil || !a.dlBrowser.Alive() || a.dlBrowserURL != t.URL {
			cancel()
			a.finishDownloadTask(t, errors.New("请在新建任务中打开相同网址的下载浏览器，登录后再继续"))
			return
		}
		rendered = a.dlBrowser
		opts.Getter = rendered.Getter()
	}
	dl := download.New(opts)
	job := download.Job{Directory: t.Dir, Overwrite: t.overwrite, ImageFilter: t.ImageFilter}
	t.overwrite = false
	rawURL, targetName, depth, workers := t.URL, t.TargetName, clampInt(t.Depth, 1, 32767), clampInt(int(a.downloadConc), 1, 8)
	pageDir := ""
	if t.IsPage {
		pageDir = t.Path
	}
	run := func() {
		if t.ImportedImage {
			err := dl.Download(ctx, job, download.Item{URL: rawURL, Filename: targetName, RequireImage: true}, func(p download.Progress) {
				a.dlRun(func() { a.applyTaskProgress(t, p) })
			})
			a.dlRun(func() { a.finishDownloadTask(t, err) })
			return
		}
		var found download.Discovery
		var err error
		if rendered != nil {
			found, err = rendered.Capture(ctx)
		} else {
			for attempt := 0; ; attempt++ {
				found, err = download.Discover(ctx, opts.Getter, rawURL, depth)
				if err == nil || found.IsPage || attempt >= opts.Retries || !download.ShouldRetry(err) {
					break
				}
				if waitErr := download.Wait(ctx, download.RetryDelay(err, attempt, opts.RetryBase)); waitErr != nil {
					err = waitErr
					break
				}
			}
		}
		if err == nil && !found.IsPage {
			first := found.Response
			if first != nil {
				defer func() {
					if first != nil {
						_ = first.Body.Close()
					}
				}()
				getter := opts.Getter
				opts.Getter = download.GetterFunc(func(ctx context.Context, raw string, prepare func(*http.Request)) (*download.Response, error) {
					if first != nil {
						resp := first
						first = nil
						req, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
						if requestErr != nil {
							_ = resp.Body.Close()
							return nil, requestErr
						}
						if prepare != nil {
							prepare(req)
						}
						if req.Header.Get("Range") == "" {
							return resp, nil
						}
						_ = resp.Body.Close()
					}
					return getter.Get(ctx, raw, prepare)
				})
				dl = download.New(opts)
			}
			err = dl.Download(ctx, job, download.Item{URL: rawURL, Filename: targetName, RequireImage: job.ImageFilter != (download.ImageFilter{})}, func(p download.Progress) {
				a.dlRun(func() { a.applyTaskProgress(t, p) })
			})
		} else if found.IsPage && ctx.Err() == nil {
			err = a.downloadPageImages(ctx, t, dl, job, found, pageDir, workers, err)
		}
		a.dlRun(func() { a.finishDownloadTask(t, err) })
	}
	if a.window == nil {
		run()
	} else {
		go run()
	}
}

func (a *App) downloadPageImages(ctx context.Context, t *downloadTask, dl *download.Downloader, job download.Job, found download.Discovery, pageDir string, workers int, discoveryErr error) error {
	if pageDir == "" {
		pageDir = filepath.Join(job.Directory, downloadName(found.Title, t.URL))
	}
	job.Directory = pageDir
	for i := range found.Items {
		found.Items[i].Filename = downloadName(fileNameFromURL(found.Items[i].URL), found.Items[i].URL)
		if title := found.Items[i].Metadata["album_title"]; title != "" {
			folder := downloadName(title, found.Items[i].Metadata["album_id"])
			found.Items[i].Filename = path.Join(folder, found.Items[i].Filename)
		}
		found.Items[i].RequireImage = true
	}
	a.dlRun(func() {
		t.IsPage, t.Path = true, pageDir
		t.ImagesDone, t.ImagesTotal, t.ImagesFiltered = 0, len(found.Items), 0
		t.Written, t.Total = 0, 0
		if found.Title != "" {
			t.Name = found.Title
		}
		a.saveDownloadRecords()
	})
	if len(found.Items) == 0 {
		if discoveryErr != nil {
			return discoveryErr
		}
		return errors.New("网页中没有找到图片")
	}
	results := download.RunBatch(ctx, dl, job, found.Items, workers, func(p download.BatchProgress) {
		a.dlRun(func() {
			if t.Phase == dlActive && p.Done > t.ImagesDone {
				t.ImagesDone = p.Done
			}
		})
	})
	failed, filtered := 0, 0
	var first error
	for _, result := range results {
		if errors.Is(result.Err, download.ErrFiltered) {
			filtered++
		} else if result.Err != nil && !errors.Is(result.Err, download.ErrSkipped) {
			failed++
			if first == nil {
				first = result.Err
			}
		}
	}
	a.dlRun(func() { t.ImagesFiltered = filtered })
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if failed > 0 {
		discoveryErr = errors.Join(discoveryErr, fmt.Errorf("%d 张图片下载失败：%w", failed, first))
	}
	return discoveryErr
}

// downloadName keeps paths stable across retries and distinguishes equal names from different URLs.
func downloadName(name, rawURL string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		name = "图片"
	}
	runes := []rune(name)
	if len(runes) > 96 {
		name = string(runes[:96])
	}
	ext := filepath.Ext(name)
	sum := sha256.Sum256([]byte(rawURL))
	return fmt.Sprintf("%s-%x%s", strings.TrimSuffix(name, ext), sum[:6], ext)
}

func downloadTaskDir(t *downloadTask) string {
	if t.IsPage && t.Path != "" {
		return t.Path
	}
	return t.Dir
}

// dlRun 让状态改动落到界面线程；测试里没有窗口时直接执行。
func (a *App) dlRun(fn func()) {
	win := a.window
	if win == nil {
		fn()
		return
	}
	win.Update(fn)
}

func (a *App) applyTaskProgress(t *downloadTask, p download.Progress) {
	if t.Phase != dlActive {
		return
	}
	if p.Filename != "" && p.Filename != "[archived]" {
		t.Path = p.Filename
		if name := filepath.Base(p.Filename); name != "" && name != "." {
			t.Name = name
		}
	}
	if p.BytesWritten > 0 {
		t.Written = p.BytesWritten
	}
	if p.Total > 0 {
		t.Total = p.Total
	}
	now := time.Now()
	if t.lastAt.IsZero() {
		t.lastAt, t.lastBytes = now, t.Written
		return
	}
	dt := now.Sub(t.lastAt)
	if dt < 300*time.Millisecond {
		return
	}
	if delta := t.Written - t.lastBytes; delta >= 0 && dt > 0 {
		inst := int64(float64(delta) / dt.Seconds())
		if t.Speed == 0 {
			t.Speed = inst
		} else {
			t.Speed = (t.Speed*2 + inst) / 3
		}
	}
	t.lastAt, t.lastBytes = now, t.Written
}

func (a *App) finishDownloadTask(t *downloadTask, err error) {
	if a.downloadsRunning > 0 {
		a.downloadsRunning--
	}
	t.cancel = nil
	t.Speed = 0
	t.lastAt = time.Time{}
	switch {
	case errors.Is(err, download.ErrFiltered):
		t.Phase = dlDone
		t.Err = ""
		t.ImagesFiltered = 1
		t.Path = ""
	case err == nil || errors.Is(err, download.ErrSkipped):
		t.Phase = dlDone
		t.Err = ""
		if t.Total < t.Written {
			t.Total = t.Written
		}
		if t.Written <= 0 && t.Total > 0 {
			t.Written = t.Total
		}
	case errors.Is(err, context.Canceled):
		if t.paused {
			t.Phase = dlPaused
		} else {
			t.Phase = dlCanceled
			t.Written = 0
			t.Total = 0
		}
	default:
		t.Phase = dlFailed
		t.Err = err.Error()
		logging.Log.Warning("下载失败 %s：%v", compactURL(t.URL), err)
	}
	t.paused = false
	a.saveDownloadRecords()
	a.pumpDownloads()
}

func (a *App) findTask(id int) *downloadTask {
	for _, t := range a.dlTasks {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (a *App) pauseTask(t *downloadTask) {
	if t == nil {
		return
	}
	switch t.Phase {
	case dlActive:
		t.paused = true
		if t.cancel != nil {
			t.cancel()
		}
	case dlQueued:
		t.Phase = dlPaused
		a.saveDownloadRecords()
		a.maybeInstallPending()
	}
}

func (a *App) resumeTask(t *downloadTask) {
	if t == nil || t.Phase == dlActive {
		return
	}
	t.Phase = dlQueued
	t.Err = ""
	a.saveDownloadRecords()
	a.pumpDownloads()
}

func (a *App) cancelTask(t *downloadTask) {
	if t == nil {
		return
	}
	switch t.Phase {
	case dlActive:
		t.paused = false
		if t.cancel != nil {
			t.cancel()
		}
	case dlQueued, dlPaused:
		t.Phase = dlCanceled
		t.Written = 0
		t.Total = 0
		a.saveDownloadRecords()
		a.pumpDownloads()
	}
}

func (a *App) retryTask(t *downloadTask) {
	if t == nil || !t.Phase.terminal() {
		return
	}
	t.overwrite = t.Phase == dlDone
	t.Phase = dlQueued
	t.Err = ""
	if !t.IsPage {
		t.Path = ""
	}
	t.Written = 0
	t.Total = 0
	t.ImagesDone = 0
	t.Speed = 0
	a.saveDownloadRecords()
	a.pumpDownloads()
}

func (a *App) removeTask(t *downloadTask) {
	if t == nil {
		return
	}
	if t.Phase == dlActive {
		t.paused = false
		if t.cancel != nil {
			t.cancel()
		}
	}
	out := a.dlTasks[:0]
	for _, item := range a.dlTasks {
		if item != t {
			out = append(out, item)
		}
	}
	a.dlTasks = out
	if a.dlSelectedID == t.ID {
		a.dlSelectedID = 0
	}
	a.saveDownloadRecords()
	a.pumpDownloads()
}

func (a *App) pauseAllDownloads() {
	n := 0
	for _, t := range a.dlTasks {
		if t.Phase == dlActive || t.Phase == dlQueued {
			a.pauseTask(t)
			n++
		}
	}
	if n > 0 {
		logging.Log.Info("已暂停 %d 个下载任务", n)
	}
}

func (a *App) resumeAllDownloads() {
	n := 0
	for _, t := range a.dlTasks {
		if t.Phase == dlPaused {
			t.Phase = dlQueued
			n++
		}
	}
	if n > 0 {
		a.saveDownloadRecords()
		a.pumpDownloads()
		logging.Log.Info("已继续 %d 个下载任务", n)
	}
}

// pruneDownloads 是「清理」：移掉已完成和失败的记录，保留进行中和回收站。
func (a *App) pruneDownloads() int {
	out := a.dlTasks[:0]
	removed := 0
	for _, t := range a.dlTasks {
		if t.Phase == dlDone || t.Phase == dlFailed {
			removed++
			continue
		}
		out = append(out, t)
	}
	a.dlTasks = out
	if removed > 0 {
		a.saveDownloadRecords()
		logging.Log.Info("已清理 %d 条下载记录", removed)
	}
	return removed
}

func (a *App) clearDownloadTrash() int {
	out := a.dlTasks[:0]
	removed := 0
	for _, t := range a.dlTasks {
		if t.Phase == dlCanceled {
			removed++
			continue
		}
		out = append(out, t)
	}
	a.dlTasks = out
	if removed > 0 {
		a.saveDownloadRecords()
		logging.Log.Info("回收站已清空：%d 条", removed)
	}
	return removed
}

func (a *App) checkedDownloadTasks() []*downloadTask {
	var out []*downloadTask
	for _, t := range a.dlTasks {
		if t.Checked {
			out = append(out, t)
		}
	}
	return out
}

func (a *App) clearDownloadChecked() {
	for _, t := range a.dlTasks {
		t.Checked = false
	}
}

// downloadOptions 把设置页里的下载参数交给下载引擎。
func (a *App) downloadOptions() download.Options {
	var band *download.BandwidthLimiter
	if a.downloadRateKB > 0 {
		band = download.NewBandwidthLimiter(int64(a.downloadRateKB) * 1024)
	}
	limit := clampInt(int(a.downloadConc), 1, 8)
	if a.dlHost == nil {
		a.dlHost = download.NewHostLimiter(limit)
	} else {
		a.dlHost.SetLimit(limit)
	}
	if a.dlPathLock == nil {
		a.dlPathLock = download.NewPathLocker()
	}
	return download.Options{
		Getter:    download.NewHTTPGetter(browser.CurrentProxyServer()),
		HostLimit: a.dlHost,
		Bandwidth: band,
		PathLock:  a.dlPathLock,
		Retries:   clampInt(int(a.downloadRetries), 0, 10),
	}
}

func (a *App) downloadImageFilter() download.ImageFilter {
	return download.ImageFilter{
		MinBytes:  int64(clampInt(int(a.dlMinKB), 0, 10000000)) * 1024,
		MaxBytes:  int64(clampInt(int(a.dlMaxKB), 0, 10000000)) * 1024,
		MinWidth:  clampInt(int(a.dlMinWidth), 0, 10000000),
		MaxWidth:  clampInt(int(a.dlMaxWidth), 0, 10000000),
		MinHeight: clampInt(int(a.dlMinHeight), 0, 10000000),
		MaxHeight: clampInt(int(a.dlMaxHeight), 0, 10000000),
	}
}

// dlRecord 是落盘的下载记录。进行中的任务重启后按暂停恢复。
type dlRecord struct {
	ID             int                  `json:"id"`
	URL            string               `json:"url"`
	Name           string               `json:"name"`
	Site           string               `json:"site"`
	Dir            string               `json:"dir"`
	Phase          int                  `json:"phase"`
	Written        int64                `json:"written"`
	Total          int64                `json:"total"`
	Err            string               `json:"err"`
	Path           string               `json:"path"`
	Added          int64                `json:"added"`
	Depth          int                  `json:"depth,omitempty"`
	IsPage         bool                 `json:"is_page,omitempty"`
	ImagesDone     int                  `json:"images_done,omitempty"`
	ImagesTotal    int                  `json:"images_total,omitempty"`
	ImagesFiltered int                  `json:"images_filtered,omitempty"`
	TargetName     string               `json:"target_name,omitempty"`
	ImageFilter    download.ImageFilter `json:"image_filter,omitempty"`
	Rendered       bool                 `json:"rendered,omitempty"`
	ImportedImage  bool                 `json:"imported_image,omitempty"`
}

const dlRecordsKey = "download_records"

func (a *App) saveDownloadRecords() {
	if a.store == nil {
		return
	}
	records := make([]dlRecord, 0, len(a.dlTasks))
	for _, t := range a.dlTasks {
		records = append(records, dlRecord{
			ID: t.ID, URL: t.URL, Name: t.Name, Site: t.Site, Dir: t.Dir,
			Phase: int(t.Phase), Written: t.Written, Total: t.Total,
			Err: t.Err, Path: t.Path, Added: t.Added.Unix(),
			Depth: t.Depth, IsPage: t.IsPage, ImagesDone: t.ImagesDone, ImagesTotal: t.ImagesTotal,
			TargetName: t.TargetName, ImageFilter: t.ImageFilter, ImagesFiltered: t.ImagesFiltered, Rendered: t.Rendered, ImportedImage: t.ImportedImage,
		})
	}
	data, err := json.Marshal(records)
	if err != nil {
		logging.Log.Errorf("保存下载记录失败: %v", err)
		return
	}
	a.store.Set(dlRecordsKey, string(data))
}

func (a *App) loadDownloadRecords() {
	if a.store == nil {
		return
	}
	raw := a.store.Get(dlRecordsKey)
	if strings.TrimSpace(raw) == "" {
		return
	}
	var records []dlRecord
	if err := json.Unmarshal([]byte(raw), &records); err != nil {
		logging.Log.Errorf("读取下载记录失败: %v", err)
		return
	}
	a.dlTasks = nil
	for _, r := range records {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		t := &downloadTask{
			ID: r.ID, URL: r.URL, Name: r.Name, Site: r.Site, Dir: r.Dir,
			Phase: dlPhase(r.Phase), Written: r.Written, Total: r.Total,
			Err: r.Err, Path: r.Path, Added: time.Unix(r.Added, 0),
			Depth: r.Depth, IsPage: r.IsPage, ImagesDone: r.ImagesDone, ImagesTotal: r.ImagesTotal,
			TargetName: r.TargetName, ImageFilter: r.ImageFilter, ImagesFiltered: r.ImagesFiltered, Rendered: r.Rendered, ImportedImage: r.ImportedImage,
		}
		// 上一轮没跑完的，重新打开时按暂停放在队列里，等用户点继续。
		if t.Phase == dlActive || t.Phase == dlQueued {
			t.Phase = dlPaused
		}
		if t.Site == "" {
			t.Site = dlSite(t.URL)
		}
		if t.Name == "" {
			t.Name = fileNameFromURL(t.URL)
		}
		if t.ID > a.dlNextID {
			a.dlNextID = t.ID
		}
		a.dlTasks = append(a.dlTasks, t)
	}
}
