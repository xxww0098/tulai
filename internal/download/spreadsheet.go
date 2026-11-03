package download

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// shortcut: 表格文件最多 16 MiB、10 万行、1024 列和 100 万个单元格；更大数据需要流式导入。
const (
	maxSpreadsheetFile  = 16 << 20
	maxSpreadsheetRows  = 100000
	maxSpreadsheetCols  = 1024
	maxSpreadsheetCells = 1000000
	maxSpreadsheetItems = 10000
	maxSpreadsheetValue = 64 << 10
	maxSpreadsheetName  = 4096
)

type Spreadsheet struct{ Sheets []SpreadsheetSheet }
type SpreadsheetSheet struct {
	Name string
	Rows [][]string
}
type SpreadsheetColumn struct {
	Name, Sample string
	URL          bool
}
type SpreadsheetPreview struct {
	Columns []SpreadsheetColumn
	Start   int
}
type SpreadsheetImage struct {
	URL, Name, Column string
	Row, ColumnIndex  int
}

// ReadSpreadsheet reads a selected file without using a local file URL.
func ReadSpreadsheet(name string, source io.Reader) (*Spreadsheet, error) {
	if source == nil {
		return nil, errors.New("表格文件不可读取")
	}
	data, err := io.ReadAll(io.LimitReader(source, maxSpreadsheetFile+1))
	if err != nil {
		return nil, fmt.Errorf("读取表格失败: %w", err)
	}
	if len(data) > maxSpreadsheetFile {
		return nil, errors.New("表格文件超过 16 MiB")
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".csv":
		rows, err := spreadsheetCSV(data)
		if err != nil {
			return nil, err
		}
		return &Spreadsheet{Sheets: []SpreadsheetSheet{{Name: filepath.Base(name), Rows: rows}}}, nil
	case ".xlsx":
		return spreadsheetXLSX(data)
	default:
		return nil, errors.New("请选择 CSV 或 XLSX 文件")
	}
}

func spreadsheetCSV(data []byte) ([][]string, error) {
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) {
		decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(data)
		if err != nil || !utf8.Valid(decoded) || bytes.Contains(decoded, []byte("\xef\xbf\xbd")) {
			return nil, errors.New("CSV 编码无效，请使用 UTF-8 或 GBK")
		}
		data = decoded
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	var rows [][]string
	allocated := 0
	for {
		start := r.InputOffset()
		row, err := r.Read()
		end := r.InputOffset()
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("CSV 格式无效: %w", err)
		}
		// encoding/csv skips empty lines; retain them so row numbers match the file.
		for start < end {
			if data[start] == '\n' {
				start++
			} else if data[start] == '\r' && start+1 < end && data[start+1] == '\n' {
				start += 2
			} else {
				break
			}
			if len(rows) >= maxSpreadsheetRows {
				return nil, errors.New("CSV 行数超过上限")
			}
			rows = append(rows, nil)
		}
		if err == io.EOF {
			break
		}
		if len(row) > maxSpreadsheetCols {
			return nil, errors.New("CSV 列数超过 1024")
		}
		for _, v := range row {
			if len(v) > maxSpreadsheetValue {
				return nil, errors.New("CSV 单元格超过 64 KiB")
			}
		}
		allocated += len(row)
		if len(rows) >= maxSpreadsheetRows || allocated > maxSpreadsheetCells {
			return nil, errors.New("CSV 行数或单元格数超过上限")
		}
		rows = append(rows, row)
	}
	if len(rows) > maxSpreadsheetRows {
		return nil, errors.New("CSV 行数超过上限")
	}
	if len(rows) == 0 {
		return nil, errors.New("CSV 文件没有数据")
	}
	return rows, nil
}

func spreadsheetLooksURL(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// Preview infers the first row and default URL columns from the reference rules.
func (s SpreadsheetSheet) Preview() (SpreadsheetPreview, error) {
	var preview SpreadsheetPreview
	if len(s.Rows) == 0 {
		return preview, errors.New("工作表没有数据")
	}
	cols := 0
	for _, row := range s.Rows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	if cols == 0 {
		return preview, errors.New("工作表没有单元格")
	}
	header := true
	for _, value := range s.Rows[0] {
		if spreadsheetLooksURL(value) {
			header = false
			break
		}
	}
	if header {
		preview.Start = 1
	}
	preview.Columns = make([]SpreadsheetColumn, cols)
	for col := 0; col < cols; col++ {
		name := ""
		if header && col < len(s.Rows[0]) {
			name = strings.TrimSpace(s.Rows[0][col])
		}
		if name == "" {
			name = spreadsheetColumnName(col)
		}
		preview.Columns[col].Name = name
		if preview.Start < len(s.Rows) && col < len(s.Rows[preview.Start]) {
			preview.Columns[col].Sample = s.Rows[preview.Start][col]
		}
	}
	for _, row := range s.Rows[preview.Start:] {
		for col, value := range row {
			if spreadsheetLooksURL(value) {
				preview.Columns[col].URL = true
			}
		}
	}
	return preview, nil
}

func spreadsheetColumnName(index int) string {
	name := ""
	for n := index + 1; n > 0; n = (n - 1) / 26 {
		name = string(rune('A'+(n-1)%26)) + name
	}
	return name
}

// Images keeps each cell as one URL and preserves its row/column identity.
func (s SpreadsheetSheet) Images(preview SpreadsheetPreview, urlColumns []bool, nameColumns []int) ([]SpreadsheetImage, int, error) {
	if len(urlColumns) != len(preview.Columns) || preview.Start < 0 || preview.Start > len(s.Rows) {
		return nil, 0, errors.New("表格列选择无效")
	}
	seenNames := map[int]bool{}
	for _, col := range nameColumns {
		if col < 0 || col >= len(preview.Columns) || seenNames[col] {
			return nil, 0, errors.New("文件名列选择无效")
		}
		seenNames[col] = true
	}
	selected := 0
	for _, checked := range urlColumns {
		if checked {
			selected++
		}
	}
	if selected == 0 {
		return nil, 0, errors.New("请至少选择一个图片网址列")
	}
	var images []SpreadsheetImage
	invalid := 0
	for rowIndex := preview.Start; rowIndex < len(s.Rows); rowIndex++ {
		row := s.Rows[rowIndex]
		if len(row) > len(preview.Columns) {
			return nil, invalid, errors.New("表格列选择与数据不一致")
		}
		var parts []string
		nameLength := 0
		for _, col := range nameColumns {
			if col >= len(row) {
				continue
			}
			value := strings.TrimSpace(row[col])
			if value == "" {
				continue
			}
			nameLength += len(value) + 1
			if nameLength > maxSpreadsheetName {
				return nil, invalid, errors.New("组合文件名超过 4096 字节")
			}
			parts = append(parts, value)
		}
		name := strings.Join(parts, "_")
		if name == "" {
			name = strconv.Itoa(rowIndex + 1)
		}
		for col, value := range row {
			if !urlColumns[col] {
				continue
			}
			raw := strings.TrimSpace(value)
			if raw == "" {
				continue
			}
			u, err := url.Parse(raw)
			if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(raw, "\r\n\t ") {
				invalid++
				continue
			}
			if len(images) >= maxSpreadsheetItems {
				return nil, invalid, errors.New("图片任务超过 10000 条，请拆分表格")
			}
			images = append(images, SpreadsheetImage{URL: raw, Name: name, Column: preview.Columns[col].Name, Row: rowIndex + 1, ColumnIndex: col})
		}
	}
	if len(images) == 0 {
		return nil, invalid, errors.New("所选列没有可用的图片网址")
	}
	return images, invalid, nil
}
