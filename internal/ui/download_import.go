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
	w, _ := c.Size()
	width := min(float32(600), max(float32(300), w-80))
	ui.Modal(c, &s.open, func() {
		ui.Column(c.Key("dl-import")).Width(width).Gap(10).Padding(2, 2, 2, 2).Children(func() {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "导入表格图片").FontSize(15).FontWeight(600).TextColor(tok.text).Shrink(0)
				uiBadge(c, "dl-import-file", s.name, "neutral", tok)
				ui.Spacer(c)
			})
			if len(s.book.Sheets) > 1 {
				labels := make([]string, len(s.book.Sheets))
				for i := range s.book.Sheets {
					labels[i] = s.book.Sheets[i].Name
				}
				ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "工作表").FontSize(tok.body).FontWeight(500).TextColor(tok.text).Shrink(0)
					motionSelect(c.Key("dl-import-sheet"), &s.sheet, labels).Grow(1).Label("选择工作表").OnChange(s.selectSheet)
				})
			}
			ui.Text(c, "选择图片网址列和文件名列；每个单元格作为一个网址。").FontSize(tok.caption).TextColor(tok.muted)
			uiCardSurface(ui.Column(c).FillWidth().Clip(), tok).Children(func() {
				ui.Row(c).FillWidth().Padding(5, 8).Gap(10).Background(tok.sunken).
					BorderWidth(0, 0, 1, 0).BorderColor(tok.border).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "图片网址").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).Width(74).Shrink(0)
					ui.Text(c, "文件名").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).Width(64).Shrink(0)
					ui.Text(c, "列名").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).Grow(1).MinWidth(0)
					ui.Text(c, "示例值").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).Width(200).Shrink(0)
				})
				ui.Scroll(c.Key("dl-import-columns")).FillWidth().MaxHeight(176).Children(func() {
					ui.Column(c).FillWidth().Children(func() {
						for i := range s.preview.Columns {
							col := s.preview.Columns[i]
							row := ui.Row(c.Key(fmt.Sprintf("dl-import-col-%d", i))).FillWidth().Padding(5, 8).Gap(10).AlignItems(ui.Center)
							if s.urls[i] || s.names[i] {
								row.Background(tok.accent.Alpha(0.05))
							}
							if i < len(s.preview.Columns)-1 {
								row.BorderWidth(0, 0, 1, 0).BorderColor(tok.border)
							}
							row.Children(func() {
								ui.Row(c).Width(74).Shrink(0).AlignItems(ui.Center).Children(func() {
									ui.Checkbox(c.Key(fmt.Sprintf("dl-import-url-%d", i)), &s.urls[i], "图片网址").
										Label(fmt.Sprintf("图片网址列 %d：%s", i+1, col.Name)).OnChange(s.refresh)
								})
								ui.Row(c).Width(64).Shrink(0).AlignItems(ui.Center).Children(func() {
									ui.Checkbox(c.Key(fmt.Sprintf("dl-import-name-%d", i)), &s.names[i], "文件名").
										Label(fmt.Sprintf("文件名列 %d：%s", i+1, col.Name)).OnChange(func() { s.changeNameColumn(i) })
								})
								ui.Text(c, col.Name).FontSize(tok.body).FontWeight(500).TextColor(tok.text).
									Grow(1).MinWidth(0).SingleLine().Tooltip(col.Name)
								ui.Text(c, importPreviewText(col.Sample, 50)).FontSize(tok.caption).TextColor(tok.muted).
									Width(200).Shrink(0).SingleLine().Tooltip(col.Sample)
							})
						}
					})
				})
			})
			ui.Checkbox(c.Key("dl-import-folders"), &s.folders, "多个图片列分别建立文件夹")
			sheet := s.book.Sheets[s.index]
			totalRows := max(0, len(sheet.Rows)-s.preview.Start)
			uiSectionTitle(c, "前 5 行预览", fmt.Sprintf("共 %d 行", totalRows), tok)
			previewCols := min(len(s.preview.Columns), 4)
			if previewCols == 0 && len(sheet.Rows) > s.preview.Start {
				previewCols = min(len(sheet.Rows[s.preview.Start]), 4)
			}
			uiCardSurface(ui.Column(c).FillWidth().Clip(), tok).Children(func() {
				if previewCols > 0 {
					ui.Row(c).FillWidth().Padding(4, 8).Gap(8).Background(tok.sunken).
						BorderWidth(0, 0, 1, 0).BorderColor(tok.border).AlignItems(ui.Center).Children(func() {
						ui.Text(c, "#").FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).
							Width(28).TextAlign(ui.End).FontFeatures("tnum").Shrink(0)
						for colIdx := 0; colIdx < previewCols; colIdx++ {
							colTitle := fmt.Sprintf("列 %d", colIdx+1)
							if colIdx < len(s.preview.Columns) && s.preview.Columns[colIdx].Name != "" {
								colTitle = s.preview.Columns[colIdx].Name
							}
							ui.Text(c, colTitle).FontSize(tok.caption).FontWeight(600).TextColor(tok.muted).
								Grow(1).Basis(0).MinWidth(0).SingleLine().Tooltip(colTitle)
						}
						if len(s.preview.Columns) > previewCols {
							ui.Text(c, fmt.Sprintf("+%d列", len(s.preview.Columns)-previewCols)).
								FontSize(tok.caption).TextColor(tok.muted).FontFeatures("tnum").Shrink(0)
						}
					})
				}
				endRow := min(len(sheet.Rows), s.preview.Start+5)
				for i := s.preview.Start; i < endRow; i++ {
					row := ui.Row(c.Key(fmt.Sprintf("dl-import-row-%d", i))).FillWidth().Padding(4, 8).Gap(8).AlignItems(ui.Center)
					if i < endRow-1 {
						row.BorderWidth(0, 0, 1, 0).BorderColor(tok.border)
					}
					row.Children(func() {
						ui.Text(c, fmt.Sprintf("%d", i+1)).FontSize(tok.caption).TextColor(tok.muted).
							Width(28).TextAlign(ui.End).FontFeatures("tnum").Shrink(0)
						cells := sheet.Rows[i]
						if previewCols == 0 {
							var fields []string
							for _, value := range cells {
								fields = append(fields, importPreviewText(value, 35))
							}
							text := strings.Join(fields, " · ")
							ui.Text(c, importPreviewText(text, 160)).FontSize(tok.caption).TextColor(tok.muted).
								Grow(1).MinWidth(0).SingleLine().Tooltip(text)
							return
						}
						for colIdx := 0; colIdx < previewCols; colIdx++ {
							val := "—"
							if colIdx < len(cells) && strings.TrimSpace(cells[colIdx]) != "" {
								val = importPreviewText(cells[colIdx], 35)
							}
							tip := val
							if colIdx < len(cells) {
								tip = cells[colIdx]
							}
							ui.Text(c, val).FontSize(tok.caption).TextColor(tok.text).
								Grow(1).Basis(0).MinWidth(0).SingleLine().Tooltip(tip)
						}
						if len(s.preview.Columns) > previewCols {
							ui.Text(c, "…").FontSize(tok.caption).TextColor(tok.muted).Shrink(0)
						}
					})
				}
			})
			if s.err != "" {
				ui.Row(c).FillWidth().Gap(6).Padding(6, 8).Radius(tok.radiusS).
					Background(tok.danger.Alpha(0.10)).Border(1, tok.danger.Alpha(0.24)).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconAlert).FontSize(13).TextColor(tok.danger)
					ui.Text(c, s.err).FontSize(tok.caption).TextColor(tok.danger).Grow(1).MinWidth(0).Wrap()
				})
			} else {
				message := fmt.Sprintf("将导入 %d 张图片", len(s.images))
				if s.invalid > 0 {
					message += fmt.Sprintf("，跳过 %d 个无效单元格", s.invalid)
				}
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					tone := "neutral"
					if len(s.images) > 0 {
						tone = "success"
					}
					uiBadge(c, "dl-import-summary", message, tone, tok)
				})
			}
			a.dlComposerDest(c, tok)
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				back := uiBtn(ui.Button(c, "返回"), tok).Height(28).Padding(0, 14).
					Background(tok.sunken).TextColor(tok.text).Border(1, tok.border)
				hoverPressPose(back)
				back.OnClick(func() { s.open = false; a.dlComposerOn = true })

				disabled := len(s.images) == 0 || s.err != ""
				start := uiBtn(ui.Button(c, "开始导入"), tok).Height(28).Padding(0, 14).
					Disabled(disabled)
				if disabled {
					start.Background(tok.sunken).TextColor(tok.muted).Border(1, tok.border)
				} else {
					bg := tok.accent
					if start.Hovered() {
						bg = c.Theme().AccentHover
					}
					start.Background(bg).TextColor(tok.onAccent).Border(1, bg).Cursor(ui.CursorPointer)
					hoverPressPose(start)
					start.OnClick(func() { a.submitDownloadImport(s) })
				}
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
