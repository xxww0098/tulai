package ui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/xxww0098/tulai/internal/browser"
	"github.com/xxww0098/tulai/internal/logging"
)

// DownloadService exposes batch image/page download, CSV/XLSX import, and
// managed Fingerprint download browser IPCs.
type DownloadService struct {
	app *App
}

// DownloadTaskDTO represents one active, completed, or canceled download item.
type DownloadTaskDTO struct {
	ID             int     `json:"id"`
	URL            string  `json:"url"`
	Name           string  `json:"name"`
	Site           string  `json:"site"`
	Dir            string  `json:"dir"`
	Phase          string  `json:"phase"`
	Tone           string  `json:"tone"`
	Written        int64   `json:"written"`
	Total          int64   `json:"total"`
	Speed          int64   `json:"speed"`
	ProgressText   string  `json:"progressText"`
	Fraction       float64 `json:"fraction"`
	Err            string  `json:"err"`
	Path           string  `json:"path"`
	Checked        bool    `json:"checked"`
	IsPage         bool    `json:"isPage"`
	ImagesDone     int     `json:"imagesDone"`
	ImagesTotal    int     `json:"imagesTotal"`
	ImagesFiltered int     `json:"imagesFiltered"`
	Rendered       bool    `json:"rendered"`
	Terminal       bool    `json:"terminal"`
}

// DownloadGuideEntryDTO describes one supported website in the download guide.
type DownloadGuideEntryDTO struct {
	Site  string `json:"site"`
	Pages string `json:"pages"`
	Icons string `json:"icons"`
}

// DownloadGuideGroupDTO groups supported websites by category.
type DownloadGuideGroupDTO struct {
	Title   string                  `json:"title"`
	Entries []DownloadGuideEntryDTO `json:"entries"`
}

// DownloadImportColDTO describes one previewed spreadsheet column during import.
type DownloadImportColDTO struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Sample string `json:"sample"`
	IsURL  bool   `json:"isUrl"`
	IsName bool   `json:"isName"`
}

// DownloadImportDTO describes the CSV/XLSX sheet import dialog state.
type DownloadImportDTO struct {
	Open       bool                   `json:"open"`
	FileName   string                 `json:"fileName"`
	Sheets     []string               `json:"sheets"`
	Sheet      string                 `json:"sheet"`
	Columns    []DownloadImportColDTO `json:"columns"`
	Folders    bool                   `json:"folders"`
	ImageCount int                    `json:"imageCount"`
	Invalid    int                    `json:"invalid"`
	Err        string                 `json:"err"`
}

// DownloadStateDTO represents the complete downloads module snapshot.
type DownloadStateDTO struct {
	Filter             int                     `json:"filter"`
	Search             string                  `json:"search"`
	SearchOn           bool                    `json:"searchOn"`
	GroupBySite        bool                    `json:"groupBySite"`
	SelectedID         int                     `json:"selectedId"`
	ComposerOn         bool                    `json:"composerOn"`
	ComposerErr        string                  `json:"composerErr"`
	URLs               string                  `json:"urls"`
	Dir                string                  `json:"dir"`
	ResolvedDir        string                  `json:"resolvedDir"`
	BrowserMode        bool                    `json:"browserMode"`
	BrowserBusy        bool                    `json:"browserBusy"`
	BrowserOpen        bool                    `json:"browserOpen"`
	BrowserURL         string                  `json:"browserUrl"`
	BrowserErr         string                  `json:"browserErr"`
	BrowserReady       bool                    `json:"browserReady"`
	BrowserInstallBusy bool                    `json:"browserInstallBusy"`
	BrowserSupported   bool                    `json:"browserSupported"`
	GuideOn            bool                    `json:"guideOn"`
	GuideSearch        string                  `json:"guideSearch"`
	GuideGroups        []DownloadGuideGroupDTO `json:"guideGroups"`
	Import             DownloadImportDTO       `json:"import"`
	ImportNotice       string                  `json:"importNotice"`
	Depth              int                     `json:"depth"`
	MinKB              int                     `json:"minKb"`
	MaxKB              int                     `json:"maxKb"`
	MinW               int                     `json:"minW"`
	MaxW               int                     `json:"maxW"`
	MinH               int                     `json:"minH"`
	MaxH               int                     `json:"maxH"`
	Tasks              []DownloadTaskDTO       `json:"tasks"`
	CountAll           int                     `json:"countAll"`
	CountOpen          int                     `json:"countOpen"`
	CountDone          int                     `json:"countDone"`
	CountCanceled      int                     `json:"countCanceled"`
	TotalSpeed         int64                   `json:"totalSpeed"`
	ActiveCount        int                     `json:"activeCount"`
	QueuedCount        int                     `json:"queuedCount"`
	PausedCount        int                     `json:"pausedCount"`
	FailedCount        int                     `json:"failedCount"`
}

func emptyDownloadState() DownloadStateDTO {
	return DownloadStateDTO{
		BrowserSupported: browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, browser.KindFingerprint),
		GuideGroups:      []DownloadGuideGroupDTO{},
		Import: DownloadImportDTO{
			Sheets:  []string{},
			Columns: []DownloadImportColDTO{},
		},
		Tasks: []DownloadTaskDTO{},
	}
}

// GetDownloadState returns the current download task queue, filter, guide, and import state.
func (s *DownloadService) GetDownloadState(ctx context.Context) DownloadStateDTO {
	if s.app == nil {
		return emptyDownloadState()
	}
	out, err := callValue(s.app, ctx, func() DownloadStateDTO {
		a := s.app
		cAll, cOpen, cDone, cCanceled := 0, 0, 0, 0
		var totalSpeed int64
		activeCount, queuedCount, pausedCount, failedCount := 0, 0, 0, 0
		for _, t := range a.dlTasks {
			cAll++
			switch t.Phase {
			case dlDone:
				cDone++
			case dlCanceled:
				cCanceled++
			case dlActive:
				cOpen++
				activeCount++
				if t.Speed > 0 {
					totalSpeed += t.Speed
				}
			case dlQueued:
				cOpen++
				queuedCount++
			case dlPaused:
				cOpen++
				pausedCount++
			case dlFailed:
				cOpen++
				failedCount++
			default:
				cOpen++
			}
		}
		view := a.downloadView()
		taskDTOs := make([]DownloadTaskDTO, 0, len(view))
		for _, t := range view {
			frac := 0.0
			progText := ""
			if t.IsPage {
				if t.ImagesTotal > 0 {
					frac = min(1, float64(t.ImagesDone)/float64(t.ImagesTotal))
					progText = fmt.Sprintf("%d/%d 张", t.ImagesDone, t.ImagesTotal)
				} else if t.ImagesDone > 0 {
					progText = fmt.Sprintf("%d 张", t.ImagesDone)
				}
			} else {
				if t.Total > 0 {
					frac = min(1, float64(t.Written)/float64(t.Total))
					progText = fmt.Sprintf("%s / %s", humanSize(t.Written), humanSize(t.Total))
				} else if t.Written > 0 {
					progText = humanSize(t.Written)
				}
			}
			if t.Phase == dlDone && frac == 0 {
				frac = 1
			}
			taskDTOs = append(taskDTOs, DownloadTaskDTO{
				ID:             t.ID,
				URL:            t.URL,
				Name:           t.Name,
				Site:           t.Site,
				Dir:            t.Dir,
				Phase:          t.Phase.label(),
				Tone:           t.Phase.tone(),
				Written:        t.Written,
				Total:          t.Total,
				Speed:          t.Speed,
				ProgressText:   progText,
				Fraction:       frac,
				Err:            t.Err,
				Path:           t.Path,
				Checked:        t.Checked,
				IsPage:         t.IsPage,
				ImagesDone:     t.ImagesDone,
				ImagesTotal:    t.ImagesTotal,
				ImagesFiltered: t.ImagesFiltered,
				Rendered:       t.Rendered,
				Terminal:       t.Phase.terminal(),
			})
		}
		guideGroups := make([]DownloadGuideGroupDTO, 0)
		if a.dlGuideOn {
			for _, g := range matchingDownloadGuide(a.dlGuideSearch) {
				entries := make([]DownloadGuideEntryDTO, 0, len(g.entries))
				for _, e := range g.entries {
					entries = append(entries, DownloadGuideEntryDTO{
						Site:  e.site,
						Pages: e.pages,
						Icons: e.icons,
					})
				}
				guideGroups = append(guideGroups, DownloadGuideGroupDTO{
					Title:   g.title,
					Entries: entries,
				})
			}
		}
		imp := DownloadImportDTO{
			Sheets:  []string{},
			Columns: []DownloadImportColDTO{},
		}
		if st := a.dlImport; st != nil && st.open {
			imp.Open = true
			imp.FileName = st.name
			imp.Sheet = st.sheet
			imp.Folders = st.folders
			imp.ImageCount = len(st.images)
			imp.Invalid = st.invalid
			imp.Err = st.err
			if st.book != nil {
				for _, sh := range st.book.Sheets {
					imp.Sheets = append(imp.Sheets, sh.Name)
				}
			}
			for col, info := range st.preview.Columns {
				imp.Columns = append(imp.Columns, DownloadImportColDTO{
					Index:  col,
					Name:   info.Name,
					Sample: info.Sample,
					IsURL:  col < len(st.urls) && st.urls[col],
					IsName: col < len(st.names) && st.names[col],
				})
			}
		}
		browserSupported := browser.RuntimeSupported(runtime.GOOS, runtime.GOARCH, browser.KindFingerprint)
		browserReady := a.runtimeReady
		if !browserReady {
			_, browserReady = browser.InstalledRuntime(browser.ConfigDir(), browser.KindFingerprint)
		}
		return DownloadStateDTO{
			Filter:             a.dlFilter,
			Search:             a.dlSearch,
			SearchOn:           a.dlSearchOn,
			GroupBySite:        a.dlGroupBySite,
			SelectedID:         a.dlSelectedID,
			ComposerOn:         a.dlComposerOn,
			ComposerErr:        a.dlComposerErr,
			URLs:               a.dlURLs,
			Dir:                a.dlDir,
			ResolvedDir:        a.downloadDir(),
			BrowserMode:        a.dlBrowserMode,
			BrowserBusy:        a.dlBrowserBusy,
			BrowserOpen:        a.dlBrowser != nil && a.dlBrowser.Alive(),
			BrowserURL:         a.dlBrowserURL,
			BrowserErr:         a.dlBrowserErr,
			BrowserReady:       browserReady,
			BrowserInstallBusy: a.runtimeBusy,
			BrowserSupported:   browserSupported,
			GuideOn:            a.dlGuideOn,
			GuideSearch:        a.dlGuideSearch,
			GuideGroups:        guideGroups,
			Import:             imp,
			ImportNotice:       a.dlImportNotice,
			Depth:              int(a.dlDepth),
			MinKB:              int(a.dlMinKB),
			MaxKB:              int(a.dlMaxKB),
			MinW:               int(a.dlMinWidth),
			MaxW:               int(a.dlMaxWidth),
			MinH:               int(a.dlMinHeight),
			MaxH:               int(a.dlMaxHeight),
			Tasks:              taskDTOs,
			CountAll:           cAll,
			CountOpen:          cOpen,
			CountDone:          cDone,
			CountCanceled:      cCanceled,
			TotalSpeed:         totalSpeed,
			ActiveCount:        activeCount,
			QueuedCount:        queuedCount,
			PausedCount:        pausedCount,
			FailedCount:        failedCount,
		}
	})
	if err != nil {
		return emptyDownloadState()
	}
	return out
}

// SetDownloadFilter switches the active download status filter tab.
func (s *DownloadService) SetDownloadFilter(ctx context.Context, filter int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.dlFilter = filter
		s.app.dlSelectedID = 0
		s.app.clearDownloadChecked()
		s.app.emit("download")
	})
}

// SetDownloadField updates a download page field or filter threshold.
func (s *DownloadService) SetDownloadField(ctx context.Context, field, value string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		persist := false
		switch field {
		case "search":
			a.dlSearch = value
		case "searchOn":
			a.dlSearchOn = value == "true" || value == "1"
		case "groupBySite":
			a.dlGroupBySite = value == "true" || value == "1"
		case "selectedID", "selectedId":
			a.dlSelectedID = parseIntDefault(value, 0)
		case "composerOn":
			a.dlComposerOn = value == "true" || value == "1"
			a.dlComposerErr = ""
		case "urls":
			a.dlURLs = value
		case "dir":
			a.dlDir = strings.TrimSpace(value)
			a.dlComposerErr = ""
			persist = true
		case "browserMode":
			a.dlBrowserMode = value == "true" || value == "1"
			a.dlComposerErr = ""
			persist = true
		case "guideSearch":
			a.dlGuideSearch = value
		case "dismissImportNotice":
			a.dlImportNotice = ""
		case "dismissBrowserErr":
			a.dlBrowserErr = ""
		case "dismissComposerErr":
			a.dlComposerErr = ""
		case "dismissNotice":
			a.dlComposerErr = ""
			a.dlBrowserErr = ""
			a.dlImportNotice = ""
		case "depth":
			a.dlDepth = float64(clampInt(parseIntDefault(value, 1), 1, 32767))
			a.dlComposerErr = ""
			persist = true
		case "minKB", "minKb":
			a.dlMinKB = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
			a.dlComposerErr = ""
			persist = true
		case "maxKB", "maxKb":
			a.dlMaxKB = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
			a.dlComposerErr = ""
			persist = true
		case "minW":
			a.dlMinWidth = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
			a.dlComposerErr = ""
			persist = true
		case "maxW":
			a.dlMaxWidth = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
			a.dlComposerErr = ""
			persist = true
		case "minH":
			a.dlMinHeight = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
			a.dlComposerErr = ""
			persist = true
		case "maxH":
			a.dlMaxHeight = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
			a.dlComposerErr = ""
			persist = true
		}
		if persist {
			a.saveSettings()
		}
		a.emit("download")
	})
}

// SubmitDownloadURLs submits newline-separated URLs into the download queue.
func (s *DownloadService) SubmitDownloadURLs(ctx context.Context, urls string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.dlURLs = urls
		s.app.submitDownloadComposer()
		s.app.emit("download")
	})
}

// DownloadRowAction performs pause, resume, cancel, retry, remove, open, or reveal on one task.
func (s *DownloadService) DownloadRowAction(ctx context.Context, id int, action string) error {
	if s.app == nil {
		return nil
	}
	type shellAction struct {
		kind        string
		taskPath    string
		taskDir     string
		fallbackDir string
	}
	act, err := callValue(s.app, ctx, func() shellAction {
		a := s.app
		t := a.findTask(id)
		if t == nil {
			return shellAction{}
		}
		switch action {
		case "pause":
			a.pauseTask(t)
		case "resume":
			a.resumeTask(t)
		case "cancel":
			a.cancelTask(t)
		case "retry":
			a.retryTask(t)
		case "remove":
			a.removeTask(t)
		case "open", "reveal":
			return shellAction{
				kind:        action,
				taskPath:    strings.TrimSpace(t.Path),
				taskDir:     strings.TrimSpace(t.Dir),
				fallbackDir: a.downloadDir(),
			}
		}
		a.emit("download")
		return shellAction{}
	})
	if err != nil {
		return err
	}
	if act.kind == "" {
		return nil
	}
	target := ""
	fellBackToDir := false
	if act.taskPath != "" {
		if _, statErr := os.Stat(act.taskPath); statErr == nil {
			target = act.taskPath
		} else {
			fellBackToDir = true
		}
	}
	if target == "" {
		for _, candidate := range []string{act.taskDir, act.fallbackDir} {
			if candidate == "" {
				continue
			}
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				target = candidate
				break
			}
		}
	}
	if target == "" {
		missing := act.taskPath
		if missing == "" {
			missing = act.taskDir
		}
		if missing == "" {
			missing = act.fallbackDir
		}
		if fellBackToDir {
			return fmt.Errorf("文件与保存目录均不存在：%s", missing)
		}
		return fmt.Errorf("保存目录不存在：%s", missing)
	}
	var opErr error
	switch act.kind {
	case "open":
		opErr = s.app.openPath(target)
	case "reveal":
		opErr = s.app.revealPath(target)
	}
	if opErr != nil {
		return opErr
	}
	if fellBackToDir {
		_ = s.app.call(ctx, func() {
			s.app.pushToast("info", "原文件已移动或删除，已打开保存目录")
		})
	}
	return nil
}

// ToggleDownloadCheck toggles row selection checkbox state for a download task.
func (s *DownloadService) ToggleDownloadCheck(ctx context.Context, id int, on bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if t := s.app.findTask(id); t != nil {
			t.Checked = on
			s.app.emit("download")
		}
	})
}

// DownloadPickDir opens a native folder picker for the download output directory.
func (s *DownloadService) DownloadPickDir(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	paths, err := s.app.pickDirsAsync(ctx, "选择下载目录", false)
	if err != nil {
		logging.Log.Errorf("选择下载目录失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		_ = s.app.call(ctx, func() {
			s.app.dlDir = paths[0]
			s.app.dlComposerErr = ""
			s.app.saveSettings()
			s.app.emit("download")
		})
	}
	return nil
}

// DownloadOpenDir opens the resolved download output directory in the OS file manager.
func (s *DownloadService) DownloadOpenDir(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	dir, err := callValue(s.app, ctx, func() string { return s.app.downloadDir() })
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("无法打开下载目录：%w", err)
	}
	return s.app.openPath(dir)
}

// OpenDownloadBrowser launches, focuses, or navigates the project-managed Fingerprint download browser.
func (s *DownloadService) OpenDownloadBrowser(ctx context.Context, target string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		target = strings.TrimSpace(target)
		if target != "" {
			if hasQzoneCredentials(target) {
				a.dlBrowserErr = "QQ 相册网址包含登录令牌，请移除令牌后再打开"
			} else {
				urls, invalid := parseDownloadURLsSplit(target)
				switch {
				case len(urls) == 1:
					a.openDownloadBrowserURL(urls[0])
				case len(urls) > 1:
					a.dlBrowserErr = "指纹浏览器每次只能打开一个网址"
				case invalid > 0:
					a.dlBrowserErr = "没有可用的 HTTP / HTTPS 地址"
				default:
					a.openDownloadBrowserURL("")
				}
			}
		} else {
			a.openDownloadBrowserURL("")
		}
		a.emit("download")
	})
}

// OpenDownloadGuide opens or closes the supported-sites guide modal.
func (s *DownloadService) OpenDownloadGuide(ctx context.Context, open bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		s.app.dlGuideOn = open
		s.app.emit("download")
	})
}

// PickDownloadSheet opens a file picker to load a CSV or XLSX spreadsheet of image URLs.
func (s *DownloadService) PickDownloadSheet(ctx context.Context) error {
	if s.app == nil {
		return nil
	}
	paths, err := s.app.pickFilesAsync(ctx, "选择图片网址表格", false, []string{"csv", "xlsx"}, "CSV / XLSX")
	if err != nil || len(paths) == 0 {
		return err
	}
	return s.ImportDownloadSheet(ctx, paths[0])
}

// ImportDownloadSheet loads a local CSV or XLSX spreadsheet path into the import preview dialog.
func (s *DownloadService) ImportDownloadSheet(ctx context.Context, path string) error {
	if s.app == nil {
		return nil
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("请选择 CSV 或 XLSX 文件")
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv", ".xlsx":
	case ".xls":
		return fmt.Errorf("暂不支持旧版 .xls 格式，请另存为 .xlsx 或 .csv 后再导入")
	default:
		return fmt.Errorf("请选择 CSV 或 XLSX 文件")
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return fmt.Errorf("表格文件不存在")
		}
		return fmt.Errorf("无法读取表格文件：%w", statErr)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("请选择普通表格文件")
	}
	f, openErr := os.Open(path)
	if openErr != nil {
		return openErr
	}
	defer f.Close()
	state, impErr := newDownloadImport(path, f)
	if impErr != nil {
		logging.Log.Errorf("读取表格失败: %v", impErr)
		return impErr
	}
	return s.app.call(ctx, func() {
		s.app.dlImport = state
		s.app.dlComposerOn = false
		s.app.emit("download")
	})
}

// DownloadImportAction updates or submits the CSV/XLSX sheet import dialog.
func (s *DownloadService) DownloadImportAction(ctx context.Context, action, value string, index int) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		a := s.app
		if a.dlImport == nil {
			return
		}
		st := a.dlImport
		switch action {
		case "close":
			st.open = false
		case "sheet":
			st.sheet = value
			st.selectSheet()
		case "toggleURL":
			if index >= 0 && index < len(st.urls) {
				st.urls[index] = !st.urls[index]
				st.refresh()
			}
		case "toggleName":
			if index >= 0 && index < len(st.names) {
				st.names[index] = !st.names[index]
				st.changeNameColumn(index)
			}
		case "toggleFolders":
			st.folders = !st.folders
		case "submit":
			a.submitDownloadImport(st)
		}
		a.emit("download")
	})
}

// DownloadPauseResumeAll pauses or resumes all active download tasks.
func (s *DownloadService) DownloadPauseResumeAll(ctx context.Context, pause bool) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		if pause {
			s.app.pauseAllDownloads()
		} else {
			s.app.resumeAllDownloads()
		}
		s.app.emit("download")
	})
}

// DownloadClearTrash prunes completed records or clears canceled/trash downloads.
func (s *DownloadService) DownloadClearTrash(ctx context.Context, mode string) {
	if s.app == nil {
		return
	}
	_ = s.app.call(ctx, func() {
		switch mode {
		case "prune":
			s.app.pruneDownloads()
		case "clear":
			s.app.clearDownloadTrash()
		}
		s.app.emit("download")
	})
}
