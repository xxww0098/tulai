package ui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nsgo-dev/nsgo/tsx"

	"github.com/xxww0098/tulai/internal/logging"
)

func init() {
	tsx.RegisterFunctions("tulai/backend", map[string]any{
		"getDownloadState":       getDownloadStateBridge,
		"setDownloadFilter":      setDownloadFilterBridge,
		"setDownloadField":       setDownloadFieldBridge,
		"submitDownloadURLs":     submitDownloadURLsBridge,
		"downloadRowAction":      downloadRowActionBridge,
		"toggleDownloadCheck":    toggleDownloadCheckBridge,
		"downloadPickDir":        downloadPickDirBridge,
		"downloadOpenDir":        downloadOpenDirBridge,
		"openDownloadBrowser":    openDownloadBrowserBridge,
		"openDownloadGuide":      openDownloadGuideBridge,
		"pickDownloadSheet":      pickDownloadSheetBridge,
		"downloadImportAction":   downloadImportActionBridge,
		"downloadPauseResumeAll": downloadPauseResumeAllBridge,
		"downloadClearTrash":     downloadClearTrashBridge,
	})
}

type DownloadTaskDTO struct {
	ID             int
	URL            string
	Name           string
	Site           string
	Dir            string
	Phase          string
	Tone           string
	Written        int64
	Total          int64
	Speed          int64
	ProgressText   string
	Fraction       float64
	Err            string
	Path           string
	Checked        bool
	IsPage         bool
	ImagesDone     int
	ImagesTotal    int
	ImagesFiltered int
	Rendered       bool
	Terminal       bool
}

type DownloadGuideEntryDTO struct {
	Site  string
	Pages string
	Icons string
}

type DownloadGuideGroupDTO struct {
	Title   string
	Entries []DownloadGuideEntryDTO
}

type DownloadImportColDTO struct {
	Index  int
	Name   string
	Sample string
	IsURL  bool
	IsName bool
}

type DownloadImportDTO struct {
	Open       bool
	FileName   string
	Sheets     []string
	Sheet      string
	Columns    []DownloadImportColDTO
	Folders    bool
	ImageCount int
	Invalid    int
	Err        string
}

type DownloadStateDTO struct {
	Filter        int
	Search        string
	SearchOn      bool
	GroupBySite   bool
	SelectedID    int
	ComposerOn    bool
	ComposerErr   string
	URLs          string
	Dir           string
	ResolvedDir   string
	BrowserMode   bool
	BrowserBusy   bool
	BrowserOpen   bool
	BrowserURL    string
	BrowserErr    string
	GuideOn       bool
	GuideSearch   string
	GuideGroups   []DownloadGuideGroupDTO
	Import        DownloadImportDTO
	ImportNotice  string
	Depth         int
	MinKB         int
	MaxKB         int
	MinW          int
	MaxW          int
	MinH          int
	MaxH          int
	Tasks         []DownloadTaskDTO
	CountAll      int
	CountOpen     int
	CountDone     int
	CountCanceled int
}

func getDownloadStateBridge() DownloadStateDTO {
	a := getApp()
	if a == nil {
		return DownloadStateDTO{}
	}
	cAll, cOpen, cDone, cCanceled := 0, 0, 0, 0
	for _, t := range a.dlTasks {
		cAll++
		switch {
		case t.Phase == dlDone:
			cDone++
		case t.Phase == dlCanceled:
			cCanceled++
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
				frac = float64(t.ImagesDone) / float64(t.ImagesTotal)
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
	var guideGroups []DownloadGuideGroupDTO
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
	var imp DownloadImportDTO
	if s := a.dlImport; s != nil && s.open {
		imp.Open = true
		imp.FileName = s.name
		imp.Sheet = s.sheet
		imp.Folders = s.folders
		imp.ImageCount = len(s.images)
		imp.Invalid = s.invalid
		imp.Err = s.err
		if s.book != nil {
			for _, sh := range s.book.Sheets {
				imp.Sheets = append(imp.Sheets, sh.Name)
			}
		}
		for col, info := range s.preview.Columns {
			imp.Columns = append(imp.Columns, DownloadImportColDTO{
				Index:  col,
				Name:   info.Name,
				Sample: info.Sample,
				IsURL:  col < len(s.urls) && s.urls[col],
				IsName: col < len(s.names) && s.names[col],
			})
		}
	}
	return DownloadStateDTO{
		Filter:        a.dlFilter,
		Search:        a.dlSearch,
		SearchOn:      a.dlSearchOn,
		GroupBySite:   a.dlGroupBySite,
		SelectedID:    a.dlSelectedID,
		ComposerOn:    a.dlComposerOn,
		ComposerErr:   a.dlComposerErr,
		URLs:          a.dlURLs,
		Dir:           a.dlDir,
		ResolvedDir:   a.downloadDir(),
		BrowserMode:   a.dlBrowserMode,
		BrowserBusy:   a.dlBrowserBusy,
		BrowserOpen:   a.dlBrowser != nil && a.dlBrowser.Alive(),
		BrowserURL:    a.dlBrowserURL,
		BrowserErr:    a.dlBrowserErr,
		GuideOn:       a.dlGuideOn,
		GuideSearch:   a.dlGuideSearch,
		GuideGroups:   guideGroups,
		Import:        imp,
		ImportNotice:  a.dlImportNotice,
		Depth:         int(a.dlDepth),
		MinKB:         int(a.dlMinKB),
		MaxKB:         int(a.dlMaxKB),
		MinW:          int(a.dlMinWidth),
		MaxW:          int(a.dlMaxWidth),
		MinH:          int(a.dlMinHeight),
		MaxH:          int(a.dlMaxHeight),
		Tasks:         taskDTOs,
		CountAll:      cAll,
		CountOpen:     cOpen,
		CountDone:     cDone,
		CountCanceled: cCanceled,
	}
}

func setDownloadFilterBridge(filter int) {
	if a := getApp(); a != nil {
		a.dlFilter = filter
		a.dlSelectedID = 0
		a.clearDownloadChecked()
		a.emit("download")
	}
}

func setDownloadFieldBridge(field, value string) {
	a := getApp()
	if a == nil {
		return
	}
	switch field {
	case "search":
		a.dlSearch = value
	case "searchOn":
		a.dlSearchOn = value == "true" || value == "1"
	case "groupBySite":
		a.dlGroupBySite = value == "true" || value == "1"
	case "selectedID":
		a.dlSelectedID = parseIntDefault(value, 0)
	case "composerOn":
		a.dlComposerOn = value == "true" || value == "1"
		a.dlComposerErr = ""
	case "urls":
		a.dlURLs = value
	case "browserMode":
		a.dlBrowserMode = value == "true" || value == "1"
	case "guideSearch":
		a.dlGuideSearch = value
	case "dismissImportNotice":
		a.dlImportNotice = ""
	case "depth":
		a.dlDepth = float64(clampInt(parseIntDefault(value, 1), 1, 32767))
	case "minKB":
		a.dlMinKB = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
	case "maxKB":
		a.dlMaxKB = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
	case "minW":
		a.dlMinWidth = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
	case "maxW":
		a.dlMaxWidth = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
	case "minH":
		a.dlMinHeight = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
	case "maxH":
		a.dlMaxHeight = float64(clampInt(parseIntDefault(value, 0), 0, 10000000))
	}
	a.emit("download")
}

func submitDownloadURLsBridge(urls string) {
	a := getApp()
	if a == nil {
		return
	}
	a.dlURLs = urls
	a.submitDownloadComposer()
	a.emit("download")
}

func downloadRowActionBridge(ctx context.Context, id int, action string) error {
	a := getApp()
	if a == nil {
		return nil
	}
	t := a.findTask(id)
	if t == nil {
		return nil
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
	case "open":
		if strings.TrimSpace(t.Path) != "" {
			return a.openPath(t.Path)
		}
		return a.openPath(downloadTaskDir(t))
	case "reveal":
		target := strings.TrimSpace(t.Path)
		if target == "" {
			target = downloadTaskDir(t)
		}
		return a.revealPath(target)
	}
	a.emit("download")
	return nil
}

func toggleDownloadCheckBridge(id int, on bool) {
	a := getApp()
	if a == nil {
		return
	}
	if t := a.findTask(id); t != nil {
		t.Checked = on
		a.emit("download")
	}
}

func downloadPickDirBridge(ctx context.Context) error {
	a := getApp()
	if a == nil {
		return nil
	}
	paths, err := a.pickDirsAsync(ctx, "选择下载目录", false)
	if err != nil {
		logging.Log.Errorf("选择下载目录失败: %v", err)
		return err
	}
	if len(paths) > 0 {
		_ = a.onScope(ctx, func() {
			a.dlDir = paths[0]
			a.emit("download")
		})
	}
	return nil
}

func downloadOpenDirBridge(ctx context.Context) error {
	a := getApp()
	if a == nil {
		return nil
	}
	dir := a.downloadDir()
	_ = os.MkdirAll(dir, 0o755)
	return a.openPath(dir)
}

func openDownloadBrowserBridge(target string) {
	a := getApp()
	if a == nil {
		return
	}
	target = strings.TrimSpace(target)
	if target != "" {
		a.openDownloadBrowserURL(target)
	} else {
		a.openDownloadBrowser()
	}
	a.emit("download")
}

func openDownloadGuideBridge(open bool) {
	if a := getApp(); a != nil {
		a.dlGuideOn = open
		a.emit("download")
	}
}

func pickDownloadSheetBridge(ctx context.Context) error {
	a := getApp()
	if a == nil {
		return nil
	}
	paths, err := a.pickFilesAsync(ctx, "选择图片网址表格", false, []string{"csv", "xlsx"}, "CSV / XLSX")
	if err != nil || len(paths) == 0 {
		return err
	}
	info, statErr := os.Stat(paths[0])
	if statErr != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("请选择普通表格文件")
	}
	f, openErr := os.Open(paths[0])
	if openErr != nil {
		return openErr
	}
	defer f.Close()
	state, impErr := newDownloadImport(paths[0], f)
	if impErr != nil {
		logging.Log.Errorf("读取表格失败: %v", impErr)
		return impErr
	}
	_ = a.onScope(ctx, func() {
		a.dlImport = state
		a.emit("download")
	})
	return nil
}

func downloadImportActionBridge(action, value string, index int) {
	a := getApp()
	if a == nil || a.dlImport == nil {
		return
	}
	s := a.dlImport
	switch action {
	case "close":
		s.open = false
	case "sheet":
		s.sheet = value
		s.selectSheet()
	case "toggleURL":
		if index >= 0 && index < len(s.urls) {
			s.urls[index] = !s.urls[index]
			s.refresh()
		}
	case "toggleName":
		if index >= 0 && index < len(s.names) {
			s.names[index] = !s.names[index]
			s.changeNameColumn(index)
		}
	case "toggleFolders":
		s.folders = !s.folders
	case "submit":
		a.submitDownloadImport(s)
	}
	a.emit("download")
}

func downloadPauseResumeAllBridge(pause bool) {
	a := getApp()
	if a == nil {
		return
	}
	if pause {
		a.pauseAllDownloads()
	} else {
		a.resumeAllDownloads()
	}
	a.emit("download")
}

func downloadClearTrashBridge(mode string) {
	a := getApp()
	if a == nil {
		return
	}
	switch mode {
	case "prune":
		a.pruneDownloads()
	case "clear":
		a.clearDownloadTrash()
	}
	a.emit("download")
}
