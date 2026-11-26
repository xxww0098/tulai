package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/download"
)

type downloadImportState struct {
	open        bool
	name        string
	book        *download.Spreadsheet
	sheet       string
	index       int
	preview     download.SpreadsheetPreview
	urls, names []bool
	nameOrder   []int
	folders     bool
	images      []download.SpreadsheetImage
	invalid     int
	err         string
}

func newDownloadImport(name string, input io.Reader) (*downloadImportState, error) {
	book, err := download.ReadSpreadsheet(name, input)
	if err != nil {
		return nil, err
	}
	state := &downloadImportState{open: true, name: filepath.Base(name), book: book, sheet: book.Sheets[0].Name}
	state.selectSheet()
	return state, nil
}

func (s *downloadImportState) selectSheet() {
	s.err = ""
	for i := range s.book.Sheets {
		if s.book.Sheets[i].Name != s.sheet {
			continue
		}
		s.index = i
		preview, err := s.book.Sheets[i].Preview()
		if err != nil {
			s.preview = download.SpreadsheetPreview{}
			s.urls = nil
			s.names = nil
			s.images = nil
			s.invalid = 0
			s.err = err.Error()
			return
		}
		s.preview = preview
		s.urls, s.names = make([]bool, len(preview.Columns)), make([]bool, len(preview.Columns))
		s.nameOrder = nil
		for col, info := range preview.Columns {
			s.urls[col] = info.URL
		}
		s.refresh()
		return
	}
	s.err = "没有找到选中的工作表"
}

func (s *downloadImportState) refresh() {
	if s.book == nil || s.index < 0 || s.index >= len(s.book.Sheets) {
		s.err = "工作表无效"
		return
	}
	images, invalid, err := s.book.Sheets[s.index].Images(s.preview, s.urls, s.nameOrder)
	s.images, s.invalid, s.err = images, invalid, ""
	if err != nil {
		s.err = err.Error()
	}
}

func (s *downloadImportState) changeNameColumn(col int) {
	if col < 0 || col >= len(s.names) {
		return
	}
	filtered := s.nameOrder[:0]
	for _, selected := range s.nameOrder {
		if selected != col {
			filtered = append(filtered, selected)
		}
	}
	s.nameOrder = filtered
	if s.names[col] {
		s.nameOrder = append(s.nameOrder, col)
	}
	s.refresh()
}

func (a *App) submitDownloadImport(s *downloadImportState) {
	if s == nil {
		return
	}
	if err := a.downloadImageFilter().Validate(); err != nil {
		s.err = err.Error()
		return
	}
	s.refresh()
	if s.err != "" {
		return
	}
	if !ensureDownloadDir(a.downloadDir()) {
		s.err = "无法创建保存目录"
		return
	}
	added, duplicate, err := a.enqueueSpreadsheetImages(s.images, s.urls, s.folders)
	if err != nil {
		s.err = err.Error()
		return
	}
	if added == 0 {
		s.err = "所选图片任务已经存在"
		return
	}
	a.dlImportNotice = fmt.Sprintf("已加入 %d 张表格图片", added)
	if s.invalid > 0 {
		a.dlImportNotice += fmt.Sprintf("，跳过 %d 个无效单元格", s.invalid)
	}
	if duplicate > 0 {
		a.dlImportNotice += fmt.Sprintf("，跳过 %d 个重复任务", duplicate)
	}
	s.open = false
	a.saveSettings()
	if !a.hasWindow() {
		a.runDownloadQueue()
	}
}

func (a *App) enqueueSpreadsheetImages(images []download.SpreadsheetImage, columns []bool, folders bool) (int, int, error) {
	for _, image := range images {
		if download.QzoneURLHasCredentials(image.URL) {
			return 0, 0, fmt.Errorf("QQ 相册网址包含登录令牌，请移除令牌后再导入")
		}
	}
	type occupiedPath struct {
		url      string
		imported bool
	}
	occupied := map[string]occupiedPath{}
	for _, task := range a.dlTasks {
		name := task.TargetName
		if name == "" {
			name = fileNameFromURL(task.URL)
		}
		name = spreadsheetFilename(name)
		occupied[strings.ToLower(filepath.Clean(filepath.Join(task.Dir, name)))] = occupiedPath{task.URL, task.ImportedImage}
	}
	count := 0
	for _, checked := range columns {
		if checked {
			count++
		}
	}
	usedFolders := map[string]int{}
	folderByColumn := map[int]string{}
	if folders && count > 1 {
		for _, image := range images {
			if _, ok := folderByColumn[image.ColumnIndex]; ok {
				continue
			}
			base := safeSpreadsheetName(image.Column)
			name := base
			for suffix := 0; ; suffix++ {
				if _, ok := usedFolders[strings.ToLower(name)]; !ok {
					break
				}
				name = fmt.Sprintf("%s_%d", base, image.ColumnIndex+1)
				if suffix > 0 {
					name += fmt.Sprintf("_%d", suffix+1)
				}
			}
			usedFolders[strings.ToLower(name)] = image.ColumnIndex
			folderByColumn[image.ColumnIndex] = name
		}
		for _, folder := range folderByColumn {
			info, err := os.Lstat(filepath.Join(a.downloadDir(), folder))
			if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
				return 0, 0, fmt.Errorf("图片列目录 %q 不是普通文件夹", folder)
			}
			if err != nil && !os.IsNotExist(err) {
				return 0, 0, fmt.Errorf("无法检查图片列目录 %q", folder)
			}
		}
	}
	added, duplicates := 0, 0
	for _, image := range images {
		dir := a.downloadDir()
		if folder := folderByColumn[image.ColumnIndex]; folder != "" {
			dir = filepath.Join(dir, folder)
		}
		name := spreadsheetFilename(image.Name)
		if count > 1 && !folders {
			name += "_" + safeSpreadsheetName(image.Column)
		}
		base := name
		for suffix := 0; ; suffix++ {
			key := strings.ToLower(filepath.Clean(filepath.Join(dir, name)))
			if prior, ok := occupied[key]; ok {
				if prior.url == image.URL && prior.imported {
					duplicates++
					break
				}
				name = fmt.Sprintf("%s_%d_%d", base, image.Row, image.ColumnIndex+1)
				if suffix > 0 {
					name += fmt.Sprintf("_%d", suffix+1)
				}
				continue
			}
			occupied[key] = occupiedPath{image.URL, true}
			a.dlNextID++
			task := newDownloadTask(a.dlNextID, image.URL, dir)
			task.Name = name
			task.TargetName = name
			task.Depth = 1
			task.ImportedImage = true
			task.ImageFilter = a.downloadImageFilter()
			a.dlTasks = append(a.dlTasks, task)
			added++
			break
		}
	}
	if added > 0 {
		a.dlFilter = dlFilterOpen
		a.saveDownloadRecords()
		a.pumpDownloads()
	}
	return added, duplicates, nil
}

func safeSpreadsheetName(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.Trim(s, " .")
	if s == "" {
		return "图片"
	}
	if len(s) > 96 {
		end := 0
		for _, r := range s {
			n := utf8.RuneLen(r)
			if end+n > 96 {
				break
			}
			end += n
		}
		s = strings.Trim(s[:end], " .")
		if s == "" {
			return "图片"
		}
	}
	upper := strings.ToUpper(strings.TrimSuffix(s, filepath.Ext(s)))
	if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" || len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '1' && upper[3] <= '9' {
		s = "_" + s
	}
	return s
}

func spreadsheetFilename(raw string) string {
	name := safeSpreadsheetName(raw)
	switch strings.ToLower(filepath.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".ico", ".avif", ".tif", ".tiff", ".heic", ".heif", ".svg":
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	if name == "" {
		return "图片"
	}
	return name
}

func (a *App) downloadDir() string {
	if dir := strings.TrimSpace(a.dlDir); dir != "" {
		return dir
	}
	return filepath.Join(a.resolvedOutputDir(), "下载")
}

func parseDownloadURLs(raw string) []string {
	valid, _ := parseDownloadURLsSplit(raw)
	return valid
}

func parseDownloadURLsSplit(raw string) ([]string, int) {
	seen := map[string]bool{}
	var valid []string
	invalid := 0
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		u, err := url.Parse(line)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || download.QzoneURLHasCredentials(line) {
			invalid++
			continue
		}
		valid = append(valid, line)
	}
	return valid, invalid
}

func browserRequiredSiteName(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	p := strings.ToLower(u.Path)
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".ico", ".avif", ".tif", ".tiff", ".heic", ".heif", ".svg":
		return ""
	}
	switch {
	case (host == "temu.com" || strings.HasSuffix(host, ".temu.com")) &&
		(strings.Contains(p, "/search_result") || p == "/search" || strings.HasPrefix(p, "/search/")):
		return "Temu 搜索"
	case host == "xhslink.com" || host == "xhslink.cn":
		return "小红书"
	case (host == "xiaohongshu.com" || strings.HasSuffix(host, ".xiaohongshu.com")) &&
		host != "ci.xiaohongshu.com" && !strings.HasPrefix(host, "sns-"):
		return "小红书"
	case host == "qzone.qq.com" || strings.HasSuffix(host, ".qzone.qq.com"):
		return "QQ 空间相册"
	case host == "facebook.com" || strings.HasSuffix(host, ".facebook.com"):
		return "Facebook"
	case host == "photoplus.cn" || strings.HasSuffix(host, ".photoplus.cn"):
		return "Photoplus"
	case host == "plusx.cn" || strings.HasSuffix(host, ".plusx.cn"):
		return "拍立享"
	case host == "onlyfans.com" || strings.HasSuffix(host, ".onlyfans.com"):
		return "OnlyFans"
	case host == "mihuashi.com" || strings.HasSuffix(host, ".mihuashi.com"):
		return "米画师"
	case host == "youpin.mi.com":
		return "小米有品"
	case host == "ymatou.com" || strings.HasSuffix(host, ".ymatou.com"):
		return "洋码头"
	case host == "vvic.com" || strings.HasSuffix(host, ".vvic.com"):
		return "搜款网"
	case host == "zcygov.cn" || strings.HasSuffix(host, ".zcygov.cn"):
		return "政采云"
	case host == "issuu.com" || strings.HasSuffix(host, ".issuu.com"):
		return "Issuu"
	default:
		return ""
	}
}

func (a *App) submitDownloadComposer() {
	if hasQzoneCredentials(a.dlURLs) {
		a.dlComposerErr = "QQ 相册网址包含登录令牌，请移除令牌后再提交"
		return
	}
	if err := a.downloadImageFilter().Validate(); err != nil {
		a.dlComposerErr = err.Error()
		return
	}
	if a.dlBrowserMode {
		valid, invalid := parseDownloadURLsSplit(a.dlURLs)
		if len(valid) == 0 {
			if invalid > 0 {
				a.dlComposerErr = "没有可用的 HTTP / HTTPS 地址"
			} else {
				a.dlComposerErr = "先粘贴至少一个地址"
			}
			return
		}
		if len(valid) > 1 {
			a.dlComposerErr = "指纹浏览器模式每次只能提交一个网址"
			return
		}
		a.submitDownloadBrowserURL(valid[0])
		return
	}
	valid, _ := parseDownloadURLsSplit(a.dlURLs)
	for _, raw := range valid {
		if site := browserRequiredSiteName(raw); site != "" {
			if len(valid) > 1 {
				a.dlComposerErr = fmt.Sprintf("%s需使用托管指纹浏览器单独抓取，请开启「使用托管指纹浏览器登录抓取」后单独提交", site)
			} else {
				a.dlComposerErr = fmt.Sprintf("%s需使用托管指纹浏览器抓取，请开启下方「使用托管指纹浏览器登录抓取」，点击「指纹浏览器抓取」打开目标页后再下载", site)
			}
			return
		}
	}
	a.enqueueDownloadComposer(a.dlURLs)
}

func (a *App) enqueueDownloadComposer(raw string) {
	if hasQzoneCredentials(raw) {
		a.dlComposerErr = "QQ 相册网址包含登录令牌，请移除令牌后再提交"
		return
	}
	if err := a.downloadImageFilter().Validate(); err != nil {
		a.dlComposerErr = err.Error()
		return
	}
	valid, invalid := parseDownloadURLsSplit(raw)
	if len(valid) == 0 {
		if invalid > 0 {
			a.dlComposerErr = "没有可用的 HTTP / HTTPS 地址"
		} else {
			a.dlComposerErr = "先粘贴至少一个地址"
		}
		return
	}
	if !ensureDownloadDir(a.downloadDir()) {
		a.dlComposerErr = "无法创建保存目录"
		return
	}
	added, _ := a.addDownloadURLs(raw)
	if added == 0 {
		a.dlComposerErr = "该网址已有下载任务，请在下载列表中查看"
		return
	}
	a.dlComposerErr = ""
	a.dlURLs = ""
	a.dlComposerOn = false
	a.saveSettings()
	if !a.hasWindow() {
		a.runDownloadQueue()
	}
}

func (a *App) runDownloadQueue() {
	for {
		var next *downloadTask
		for _, t := range a.dlTasks {
			if t.Phase == dlActive {
				next = t
				break
			}
		}
		if next == nil {
			return
		}
		for i := 0; i < 2000 && next.Phase == dlActive; i++ {
			time.Sleep(time.Millisecond)
		}
		if next.Phase == dlActive {
			return
		}
	}
}

func ensureDownloadDir(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	return os.MkdirAll(dir, 0o755) == nil
}

// ---- Download Browser (uses project-managed Fingerprint per AGENTS.md) ----

func (a *App) openDownloadBrowser() {
	if a.dlBrowserBusy {
		return
	}
	if hasQzoneCredentials(a.dlURLs) {
		a.dlBrowserErr = "QQ 相册网址包含登录令牌，请移除令牌后再打开"
		return
	}
	urls := parseDownloadURLs(a.dlURLs)
	if len(urls) != 1 && strings.TrimSpace(a.dlURLs) != "" {
		a.dlBrowserErr = "指纹浏览器每次只能打开一个网址"
		return
	}
	target := ""
	if len(urls) == 1 {
		target = urls[0]
	}
	a.openDownloadBrowserURL(target)
}

func normalizeDownloadBrowserErr(msg string) string {
	if msg == "" {
		return ""
	}
	return strings.ReplaceAll(msg, "下载浏览器", "托管指纹浏览器")
}

func (a *App) openDownloadBrowserURL(target string) {
	if a.dlBrowserBusy {
		return
	}
	if hasQzoneCredentials(target) {
		a.dlBrowserErr = "QQ 相册网址包含登录令牌，请移除令牌后再打开"
		return
	}
	if session := a.dlBrowser; session != nil && session.Alive() {
		if target != "" && a.downloadBrowserInUse() {
			a.dlBrowserErr = "请等待当前指纹浏览器下载结束或暂停后再打开其他网址"
			return
		}
		a.dlBrowserBusy, a.dlBrowserErr = true, ""
		if target != "" {
			a.dlBrowserURL = ""
		}
		a.emit("download")
		act := func() {
			var opErr error
			if target == "" {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				opErr = session.Focus(ctx)
				cancel()
			} else {
				opErr = session.OpenTarget(context.Background(), target)
			}
			a.postUI(func() {
				if a.dlBrowser != session {
					return
				}
				a.dlBrowserBusy = false
				if opErr != nil {
					a.dlBrowserErr = normalizeDownloadBrowserErr(opErr.Error())
				} else if target != "" {
					a.dlBrowserURL = target
				}
				a.pumpDownloads()
				a.emit("download")
			})
		}
		if !a.hasWindow() {
			act()
		} else {
			go act()
		}
		return
	}
	if a.downloadBrowserInUse() {
		a.dlBrowserErr = "请等待当前指纹浏览器下载结束或暂停后再打开其他网址"
		return
	}
	root := browser.ConfigDir()
	if root == "" || !a.hasWindow() {
		a.dlBrowserErr = "无法打开指纹浏览器"
		return
	}
	if !browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, browser.KindFingerprint) {
		a.dlBrowserErr = "当前系统架构暂不支持托管指纹浏览器 Fingerprint"
		return
	}
	if _, ok := browser.InstalledRuntime(root, browser.KindFingerprint); !ok {
		if a.runtimeBusy {
			a.dlBrowserErr = "正在安装托管指纹浏览器 Fingerprint，请等待完成后重试"
		} else {
			a.dlBrowserErr = "托管指纹浏览器 Fingerprint 尚未安装，请先在「设置 → 浏览器与网络」安装"
		}
		return
	}
	old, oldCancel := a.dlBrowser, a.dlBrowserCancel
	a.dlBrowser, a.dlBrowserURL = nil, ""
	a.dlBrowserBusy, a.dlBrowserErr = true, ""
	ctx, cancel := context.WithCancel(context.Background())
	a.dlBrowserCancel = cancel
	a.emit("download")
	go func() {
		if oldCancel != nil {
			oldCancel()
		}
		if old != nil {
			old.Close()
		}
		session, err := download.OpenRenderedAssisted(ctx, filepath.Join(root, "DownloadBrowser"), target, false,
			func(ctx context.Context, session *download.RenderedSession, rawURL, marker string) error {
				return a.downloadBrowserRequest(ctx, session, rawURL, marker)
			})
		if session != nil {
			context.AfterFunc(ctx, session.Close)
			go func() {
				<-session.Done()
				session.Close()
				a.postUI(func() {
					a.handleDownloadBrowserClosed(session)
				})
			}()
		}
		if ctx.Err() != nil {
			return
		}
		a.dlRun(func() {
			if ctx.Err() != nil {
				return
			}
			a.dlBrowserBusy = false
			if err != nil {
				a.dlBrowserErr = normalizeDownloadBrowserErr(err.Error())
				a.dlBrowserCancel = nil
				cancel()
				a.pumpDownloads()
				a.emit("download")
				return
			}
			if !session.Alive() {
				a.dlBrowserErr = "指纹浏览器已关闭，请重新打开"
				a.dlBrowserCancel = nil
				cancel()
				a.pumpDownloads()
				a.emit("download")
				return
			}
			a.dlBrowser, a.dlBrowserURL = session, target
			a.pumpDownloads()
			a.emit("download")
		})
	}()
}

func (a *App) handleDownloadBrowserClosed(session downloadBrowser) {
	if a.dlBrowser != session {
		return
	}
	inUse := a.downloadBrowserInUse() || (a.dlComposerOn && a.dlBrowserMode)
	activeTask := a.dlBrowserTask
	a.dlBrowser, a.dlBrowserURL = nil, ""
	a.dlBrowserBusy = false
	a.dlBrowserTask = nil
	if a.dlBrowserCancel != nil {
		a.dlBrowserCancel()
		a.dlBrowserCancel = nil
	}
	if activeTask != nil && activeTask.Phase == dlActive {
		activeTask.browserClosed = true
		if activeTask.cancel != nil {
			activeTask.cancel()
		} else {
			a.finishDownloadTask(activeTask, errors.New("指纹浏览器已关闭，请重新打开后重试"))
		}
	}
	if inUse {
		a.dlBrowserErr = "指纹浏览器已关闭，请重新打开"
	} else {
		a.dlBrowserErr = ""
	}
	a.pumpDownloads()
	a.emit("download", "browsers")
}

func (a *App) downloadBrowserInUse() bool {
	if a.dlBrowserTask != nil {
		if a.dlBrowserTask.Phase == dlActive && !a.dlBrowserTask.removed {
			return true
		}
		a.dlBrowserTask = nil
	}
	browserAlive := a.dlBrowser != nil && a.dlBrowser.Alive()
	for _, task := range a.dlTasks {
		if !task.Rendered {
			continue
		}
		if task.Phase == dlActive || (browserAlive && task.Phase == dlQueued) {
			return true
		}
	}
	return false
}

func (a *App) submitDownloadBrowserURL(rawURL string) {
	session := a.dlBrowser
	if a.dlBrowserBusy {
		a.dlComposerErr = "指纹浏览器正在处理网址，请稍后重试"
		return
	}
	if session == nil || !session.Alive() {
		a.dlComposerErr = "请先打开指纹浏览器并访问目标网页"
		return
	}
	if a.downloadBrowserInUse() {
		a.dlComposerErr = "请等待当前指纹浏览器下载结束或暂停"
		return
	}
	draft, opened := strings.TrimSpace(a.dlURLs), a.dlComposerOn
	a.dlBrowserBusy, a.dlComposerErr = true, ""
	bind := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := session.BindOpenPage(ctx, rawURL)
		a.dlRun(func() {
			if a.dlBrowser != session {
				a.dlBrowserBusy = false
				if a.dlComposerOn {
					a.dlComposerErr = "指纹浏览器已关闭，请重新打开"
				}
				a.pumpDownloads()
				a.emit("download")
				return
			}
			a.dlBrowserBusy = false
			if err == nil && !session.Alive() {
				err = errors.New("指纹浏览器已关闭，请重新打开")
			}
			if err == nil {
				a.dlBrowserURL, a.dlBrowserErr = rawURL, ""
			}
			if strings.TrimSpace(a.dlURLs) != draft || a.dlComposerOn != opened || !a.dlBrowserMode || a.dlGuideOn {
				if err != nil {
					msg := normalizeDownloadBrowserErr(err.Error())
					if a.dlComposerOn {
						a.dlComposerErr = msg
					} else {
						a.dlBrowserErr = msg
					}
				}
				a.pumpDownloads()
				a.emit("download")
				return
			}
			if err != nil {
				a.dlComposerErr = normalizeDownloadBrowserErr(err.Error())
				a.pumpDownloads()
				a.emit("download")
				return
			}
			a.enqueueDownloadComposer(rawURL)
			a.emit("download")
		})
	}
	if !a.hasWindow() {
		bind()
	} else {
		go bind()
	}
}

func (a *App) downloadBrowserRequest(ctx context.Context, session downloadBrowser, rawURL, marker string) error {
	result := make(chan error, 1)
	a.postUI(func() {
		if err := ctx.Err(); err != nil {
			result <- err
			return
		}
		if a.dlBrowser != session || !session.Alive() || a.dlBrowserBusy {
			result <- errors.New("指纹浏览器尚未就绪，请稍后重试")
			return
		}
		if a.downloadBrowserInUse() {
			result <- errors.New("请等待当前指纹浏览器下载结束或暂停")
			return
		}
		if err := a.downloadImageFilter().Validate(); err != nil {
			a.dlBrowserErr = err.Error()
			a.emit("download")
			result <- err
			return
		}
		for _, task := range a.dlTasks {
			if task.URL == rawURL && (task.Phase == dlActive || task.Phase == dlQueued) {
				result <- errors.New("该网址已在下载列表中")
				return
			}
		}
		a.dlBrowserBusy, a.dlBrowserErr = true, ""
		result <- nil
	})
	if err := <-result; err != nil {
		return err
	}
	bindCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	err := session.BindCurrent(bindCtx, rawURL, marker)
	cancel()
	enqueued := make(chan error, 1)
	a.postUI(func() {
		if a.dlBrowser != session {
			enqueued <- errors.New("指纹浏览器已关闭")
			return
		}
		a.dlBrowserBusy = false
		if err != nil {
			msg := normalizeDownloadBrowserErr(err.Error())
			a.dlBrowserErr = msg
			a.pumpDownloads()
			a.emit("download")
			enqueued <- errors.New(msg)
			return
		}
		if !ensureDownloadDir(a.downloadDir()) {
			a.dlBrowserErr = "无法创建保存目录"
			a.pumpDownloads()
			a.emit("download")
			enqueued <- errors.New("无法创建保存目录")
			return
		}
		a.dlBrowserURL, a.dlBrowserErr = rawURL, ""
		a.dlBrowserMode = true
		var existing *downloadTask
		for _, task := range a.dlTasks {
			if task.URL == rawURL && (task.Phase.terminal() || task.Phase == dlPaused) {
				existing = task
				break
			}
		}
		if existing != nil {
			wasRendered := existing.Rendered
			dirChanged := existing.Dir != a.downloadDir()
			existing.Rendered = true
			existing.Dir = a.downloadDir()
			existing.Depth = clampInt(int(a.dlDepth), 1, 32767)
			existing.ImageFilter = a.downloadImageFilter()
			if dirChanged {
				existing.Path = ""
			}
			if existing.Phase == dlPaused && wasRendered && !dirChanged {
				a.resumeTask(existing)
			} else {
				overwrite := existing.Phase == dlDone
				if existing.Phase == dlPaused {
					existing.Phase = dlFailed
				}
				a.requeueDownloadTask(existing, overwrite)
			}
		} else {
			added, _ := a.addDownloadURLs(rawURL)
			if added == 0 {
				a.pumpDownloads()
				a.emit("download")
				enqueued <- errors.New("无法加入下载任务")
				return
			}
		}
		a.dlFilter = dlFilterOpen
		a.emit("download")
		enqueued <- nil
	})
	return <-enqueued
}

// ---- Download Guide ----

type downloadGuideEntry struct{ site, pages, icons string }
type downloadGuideGroup struct {
	title   string
	entries []downloadGuideEntry
}

var downloadGuideGroups = []downloadGuideGroup{
	{"普通网页和图片搜索", []downloadGuideEntry{
		{"图片直链、普通网页", "图片直链或网页中可访问的图片；动态网页可尝试开启托管指纹浏览器模式。", ""},
		{"百度图片", "图片搜索结果页。", "baidu.com"},
		{"搜狗图片", "图片搜索结果页。", "sogou.com"},
		{"360 图片", "图片搜索结果页。", "360.cn"},
		{"Bing 图片", "搜索结果和部分带图片编号的单图详情页。", "bing.com"},
		{"Google 图片", "仅尝试历史搜索结果格式；当前实时搜索尚未验证。", "google.com"},
		{"yande.re", "单图和搜索结果。", "yande.re"},
		{"Gelbooru", "单图和搜索结果。", "gelbooru.com"},
		{"昵图", "作品和搜索结果的预览图。", "nipic.com"},
		{"汇图", "作品预览图。", "huitu.com"},
		{"Anime-Pictures", "作品和搜索结果；仅通过合成页面验证。", "anime-pictures.net"},
	}},
	{"电商商品与店铺", []downloadGuideEntry{
		{"亚马逊 Amazon", "商品详情和搜索结果；搜索页按商品保存主图。法国站尚未实测。", "amazon.com"},
		{"eBay 易贝", "商品详情和搜索结果；搜索页目前仅通过合成测试。", "ebay.com"},
		{"Temu", "商品详情及搜索结果；搜索通过托管指纹浏览器抓取，可自动加载或手动滚动后下载。", "temu.com"},
		{"Ozon", "商品图库和规格图；页面采集仅通过合成测试。", "ozon.ru"},
		{"1688", "商品详情、公开店铺相册及相册详情。", "1688.com"},
		{"淘宝", "商品详情的主图、部分规格图和详情图；可通过托管指纹浏览器登录。", "taobao.com"},
		{"天猫", "商品详情的主图、部分规格图和详情图；可通过托管指纹浏览器登录。", "tmall.com"},
		{"京东", "商品详情的主图、部分规格图和详情图。", "jd.com"},
		{"当当", "商品详情的主图、部分规格图和详情图。", "dangdang.com"},
		{"网易严选", "商品详情的主图、部分规格图和详情图。", "you.163.com"},
		{"拼多多", "商品详情；真实商品采集尚未验证。", "pinduoduo.com"},
		{"苏宁易购", "商品详情的主图、部分规格图和详情图。", "suning.com"},
		{"义乌购", "商品详情的主图、部分规格图和详情图。", "yiwugo.com"},
		{"微店", "商品详情的主图、部分规格图和详情图。", "weidian.com"},
		{"有赞", "商品详情的主图、部分规格图和详情图。", "youzan.com"},
		{"唯品会", "商品详情；当前页面或实时采集尚未完整验证。", "vip.com"},
		{"蘑菇街", "商品详情；当前页面或实时采集尚未完整验证。", "mogujie.com"},
		{"网商园", "商品详情；当前页面或实时采集尚未完整验证。", "wsy.com"},
		{"母婴天下", "商品详情；当前页面或实时采集尚未完整验证。", "mytx.cn"},
		{"速卖通 AliExpress", "商品详情；可能需要通过托管指纹浏览器登录或安全验证。", "aliexpress.com"},
		{"Alibaba 国际站", "商品详情；可能需要通过托管指纹浏览器登录或安全验证。", "alibaba.com"},
		{"Shopee", "商品详情中的图库或部分规格图；部分页面尚未实测。", "shopee.com"},
		{"Etsy", "商品详情中的图库或部分规格图；部分页面尚未实测。", "etsy.com"},
		{"SHEIN", "商品详情中的图库或部分规格图；部分页面尚未实测。", "shein.com"},
		{"Lazada", "商品详情中的图库和部分详情图。", "lazada.com"},
		{"日本雅虎商城", "商品详情中的图库和部分详情图。", "shopping.yahoo.co.jp"},
		{"敦煌网", "商品详情；部分页面可能要求安全验证。", "dhgate.com"},
		{"中国制造网", "商品详情；部分页面可能要求安全验证。", "made-in-china.com"},
		{"兰亭 LightInTheBox", "商品详情；部分页面可能要求安全验证。", "lightinthebox.com"},
		{"JJ's House", "商品详情的当前选中颜色图库。", "jjshouse.com"},
		{"百度爱采购", "商品详情和店铺商品；店铺完整分页尚未验证。", "b2b.baidu.com"},
		{"得物", "移动商品详情。", "dewu.com"},
		{"闲鱼", "商品详情；真实页面可能要求安全验证。", "goofish.com"},
		{"抖音商品", "商品详情；真实页面可能要求安全验证。", "douyin.com"},
		{"小米有品", "商品详情；请开启托管指纹浏览器并等待页面加载。", "youpin.mi.com"},
		{"洋码头", "商品详情；请开启托管指纹浏览器并等待页面加载。", "ymatou.com"},
		{"搜款网", "商品详情；请开启托管指纹浏览器并等待页面加载。", "vvic.com"},
		{"政采云", "商品详情；请开启托管指纹浏览器并等待页面加载。", "zcygov.cn"},
		{"Wellco", "商品详情和部分分类列表。", "wellco.ca"},
		{"Spoonflower", "单个设计和店铺当前页。", "spoonflower.com"},
		{"无印良品 MUJI", "旧版商品页面的部分展示图。", "muji.com"},
	}},
	{"社交、图文与文章", []downloadGuideEntry{
		{"小红书", "笔记及主页已加载的部分笔记；需托管指纹浏览器，主页尚未实测。", "xiaohongshu.com"},
		{"QQ 空间", "个人相册及相册列表；需托管指纹浏览器，真实登录尚未验证。", "qzone.qq.com"},
		{"QQ 群相册", "群相册及群相册列表；需托管指纹浏览器，真实登录尚未验证。", "qzone.qq.com"},
		{"微博", "帖子、图片墙和相册；可能需要登录。", "weibo.com"},
		{"哔哩哔哩 B站", "图文动态和空间相册。", "bilibili.com"},
		{"抖音图文", "单个图文作品中的图片。", "douyin.com"},
		{"快手", "单个图文作品中的图片。", "kuaishou.com"},
		{"TikTok", "公开图文和作者主页；主页需使用托管指纹浏览器。", "tiktok.com"},
		{"X / Twitter", "单个图片帖和作者主页；主页只读取已加载的部分内容。", "x.com"},
		{"Facebook", "主页照片列表预览；需托管指纹浏览器，真实登录内容尚未验证。", "facebook.com"},
		{"Instagram", "单个帖子的图片。", "instagram.com"},
		{"Tumblr", "单个帖子的图片。", "tumblr.com"},
		{"百度贴吧", "帖子图片及部分分页。", "tieba.baidu.com"},
		{"微信公众号", "公众号文章正文图片。", "mp.weixin.qq.com"},
		{"知乎", "知乎文章及回答中的图片。", "zhihu.com"},
		{"百度百家号", "单篇文章正文图片。", "baijiahao.baidu.com"},
		{"今日头条", "单篇文章正文图片。", "toutiao.com"},
		{"新浪博客", "文章正文和作者文章目录。", "blog.sina.com.cn"},
		{"微商相册", "单品和基础店铺；可能需要登录。", "szwego.com"},
		{"OnlyFans", "已授权作者主页的可访问图片；需托管指纹浏览器。", "onlyfans.com"},
	}},
	{"摄影、作品与相册", []downloadGuideEntry{
		{"Pixiv", "单个作品和作者作品列表；动图仅保存静态图片。", "pixiv.net"},
		{"Flickr", "单图、摄影作品与相册。", "flickr.com"},
		{"Imgur", "单图、帖子与相册。", "imgur.com"},
		{"豆瓣", "单图和个人相册；电影、人物图片尚未实测。", "douban.com"},
		{"图虫", "作品和作者作品列表。", "tuchong.com"},
		{"花瓣", "单图和画板；画板可能只取得部分图片。", "huaban.com"},
		{"Pinterest", "单图和画板；画板可能只取得部分图片。", "pinterest.com"},
		{"ArtStation", "作品和作者主页；部分主页只采集首批作品。", "artstation.com"},
		{"Behance", "作品和作者主页；部分主页只采集首批作品。", "behance.net"},
		{"500px", "单图或作品及相册；部分页面只能读取已加载内容。", "500px.com"},
		{"500px 中国站", "单图或作品及相册；部分页面只能读取已加载内容。", "500px.com.cn"},
		{"堆糖", "单图和专辑。", "duitang.com"},
		{"又拍 Yupoo", "用户主页、相册、分类和搜索；实时页面可能限制访问。", "yupoo.com"},
		{"DeviantArt", "单个作品或作品展示图。", "deviantart.com"},
		{"站酷", "单个作品或作品展示图。", "zcool.com.cn"},
		{"CNU", "单个作品或作品展示图。", "cnu.cc"},
		{"Rajče", "相册图片。", "rajce.net"},
		{"ImgSrc", "相册图片；指纹浏览器模式只读取当前页。", "imgsrc.ru"},
		{"35photo", "单个作品和作者作品列表。", "35photo.pro"},
		{"Fur Affinity", "单个作品和作者作品列表。", "furaffinity.net"},
		{"1x", "单图和图库当前已加载的作品。", "1x.com"},
		{"Unsplash", "单图和搜索结果；搜索只采集部分已加载图片。", "unsplash.com"},
		{"Pexels", "单图页面；当前页面解析仅通过合成测试。", "pexels.com"},
		{"Photoplus", "可公开查看和下载的活动图片；需托管指纹浏览器。", "photoplus.cn"},
		{"拍立享", "可公开查看和下载的活动图片；需托管指纹浏览器。", "plusx.cn"},
		{"米画师", "作品和作者页面；需托管指纹浏览器，真实登录内容尚未验证。", "mihuashi.com"},
		{"太平洋摄影部落", "作品页面展示图。", "pconline.com.cn"},
		{"E-Hentai", "图片集及阅读器页面；仅通过合成测试。", "e-hentai.org"},
		{"ASMHentai", "图片集及阅读器页面。", "asmhentai.com"},
		{"HentaiFox", "图片集及阅读器页面。", "hentaifox.com"},
	}},
	{"建筑、旅行与其他", []downloadGuideEntry{
		{"谷德 gooood", "文章及部分分类列表。", "gooood.cn"},
		{"ArchDaily", "文章及部分分类列表。", "archdaily.com"},
		{"Houzz", "项目展示图。", "houzz.com"},
		{"WikiArt", "作者作品列表。", "wikiart.org"},
		{"IMDb", "影片图片索引；当前仅通过合成测试。", "imdb.com"},
		{"时光网 Mtime", "影片及影人图片页面。", "mtime.com"},
		{"携程", "景点相册图片。", "ctrip.com"},
		{"房天下", "新房楼盘相册图片。", "fang.com"},
		{"大众点评", "门店相册；部分页面可能要求验证。", "dianping.com"},
		{"中关村在线", "产品图集图片。", "zol.com.cn"},
		{"汽车之家", "已识别的车系单图页。", "autohome.com.cn"},
		{"Issuu", "当前阅读器明确提供的页图；需托管指纹浏览器。", "issuu.com"},
	}},
	{"历史页面或尚未提供专用采集", []downloadGuideEntry{
		{"国美", "仅验证过历史页面；当前站点或图片可能不可用。", "gome.com.cn"},
		{"穷游相册", "仅验证过历史页面；当前站点或图片可能不可用。", "qyer.com"},
		{"网易博客", "仅验证过历史页面；当前站点或图片可能不可用。", "blog.163.com"},
		{"美空 MOKO", "仅验证过历史页面；当前站点或图片可能不可用。", "moko.cc"},
		{"四座网", "仅验证过历史页面；当前站点或图片可能不可用。", "sizuo.com"},
		{"Dribbble", "历史页面或合成页面已验证，实时采集尚未验证。", "dribbble.com"},
		{"IMHentai", "历史页面或合成页面已验证，实时采集尚未验证。", "imhentai.xxx"},
		{"网易摄影", "尚未提供对应的专用图片采集。", "163.com"},
		{"天涯", "尚未提供对应的专用图片采集。", "tianya.cn"},
		{"喔图", "尚未提供对应的专用图片采集。", "alltuu.com"},
		{"微销通", "尚未提供对应的专用图片采集。", "aliyizhan.com"},
		{"YouTube", "尚未提供对应的专用图片采集。", "youtube.com"},
		{"nhentai", "尚未提供对应的专用图片采集。", "nhentai.com"},
	}},
}

func matchingDownloadGuide(query string) []downloadGuideGroup {
	query = strings.ToLower(strings.TrimSpace(query))
	var groups []downloadGuideGroup
	for _, group := range downloadGuideGroups {
		var entries []downloadGuideEntry
		for _, entry := range group.entries {
			if query == "" || strings.Contains(strings.ToLower(group.title+" "+entry.site+" "+entry.pages+" "+entry.icons), query) {
				entries = append(entries, entry)
			}
		}
		if len(entries) != 0 {
			groups = append(groups, downloadGuideGroup{group.title, entries})
		}
	}
	return groups
}
