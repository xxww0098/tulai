package ui

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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

func (a *App) pickDownloadSpreadsheet() {
	window := a.window
	if window == nil {
		return
	}
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: window, Title: "选择图片网址表格", Filters: []mygo.FileFilter{{Name: "CSV / XLSX", Extensions: []string{"csv", "xlsx"}}}})
		if err == nil && len(paths) == 0 {
			return
		}
		var state *downloadImportState
		if err == nil {
			var info os.FileInfo
			info, err = os.Stat(paths[0])
			if err == nil && !info.Mode().IsRegular() {
				err = fmt.Errorf("请选择普通表格文件")
			}
		}
		if err == nil {
			var f *os.File
			f, err = os.Open(paths[0])
			if err == nil {
				state, err = newDownloadImport(paths[0], f)
				f.Close()
			}
		}
		window.Update(func() {
			if err != nil {
				a.dlComposerErr = fmt.Sprintf("读取表格失败：%v", err)
				return
			}
			a.dlComposerOn = false
			a.dlImport = state
		})
	}()
}

func (a *App) downloadImportModal(c *ui.Context) {
	s := a.dlImport
	if s == nil {
		return
	}
	if !s.open {
		a.dlImport = nil
		return
	}
	tok := a.look
	ui.Modal(c, &s.open, func() {
		ui.Column(c.Key("dl-import")).Width(560).Gap(8).Padding(4, 4, 4, 4).Children(func() {
			ui.Text(c, "导入表格图片").FontSize(18).FontWeight(500).TextColor(tok.text)
			ui.Text(c, s.name).FontSize(12).TextColor(tok.muted).SingleLine()
			if len(s.book.Sheets) > 1 {
				labels := make([]string, len(s.book.Sheets))
				for i := range s.book.Sheets {
					labels[i] = s.book.Sheets[i].Name
				}
				ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "工作表").FontSize(13)
					motionSelect(c.Key("dl-import-sheet"), &s.sheet, labels).Grow(1).Label("选择工作表").OnChange(s.selectSheet)
				})
			}
			ui.Text(c, "选择图片网址列和文件名列；每个单元格作为一个网址。").FontSize(12).TextColor(tok.muted)
			ui.Scroll(c.Key("dl-import-columns")).FillWidth().MaxHeight(210).Children(func() {
				ui.Column(c).FillWidth().Gap(6).Children(func() {
					for i := range s.preview.Columns {
						col := s.preview.Columns[i]
						ui.Row(c.Key(fmt.Sprintf("dl-import-col-%d", i))).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
							ui.Checkbox(c.Key(fmt.Sprintf("dl-import-url-%d", i)), &s.urls[i], "图片网址").Label(fmt.Sprintf("图片网址列 %d：%s", i+1, col.Name)).OnChange(s.refresh)
							ui.Checkbox(c.Key(fmt.Sprintf("dl-import-name-%d", i)), &s.names[i], "文件名").Label(fmt.Sprintf("文件名列 %d：%s", i+1, col.Name)).OnChange(func() { s.changeNameColumn(i) })
							ui.Text(c, col.Name).FontSize(12).Grow(1).MinWidth(0).SingleLine().Tooltip(col.Name)
							ui.Text(c, importPreviewText(col.Sample, 50)).FontSize(12).TextColor(tok.muted).Width(210).SingleLine().Tooltip(col.Sample)
						})
					}
				})
			})
			ui.Checkbox(c.Key("dl-import-folders"), &s.folders, "多个图片列分别建立文件夹")
			ui.Text(c, "前 5 行预览").FontSize(13).FontWeight(500)
			sheet := s.book.Sheets[s.index]
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				for i := s.preview.Start; i < len(sheet.Rows) && i < s.preview.Start+5; i++ {
					var fields []string
					for _, value := range sheet.Rows[i] {
						fields = append(fields, importPreviewText(value, 35))
					}
					text := fmt.Sprintf("%d  %s", i+1, strings.Join(fields, " · "))
					ui.Text(c.Key(fmt.Sprintf("dl-import-row-%d", i)), importPreviewText(text, 160)).FontSize(12).TextColor(tok.muted).SingleLine().Tooltip(text)
				}
			})
			if s.err != "" {
				ui.Text(c, s.err).FontSize(12).TextColor(tok.danger)
			} else {
				message := fmt.Sprintf("将导入 %d 张图片", len(s.images))
				if s.invalid > 0 {
					message += fmt.Sprintf("，跳过 %d 个无效单元格", s.invalid)
				}
				ui.Text(c, message).FontSize(12).TextColor(tok.muted)
			}
			a.dlComposerDest(c, tok)
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				ui.Button(c, "返回").OnClick(func() { s.open = false; a.dlComposerOn = true })
				ui.Button(c, "开始导入").Disabled(len(s.images) == 0 || s.err != "").OnClick(func() { a.submitDownloadImport(s) })
			})
		})
	})
}

func importPreviewText(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
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
	if a.window == nil {
		a.runDownloadQueue()
	}
}

// enqueueSpreadsheetImages reserves names before starting any worker.
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
