package download

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
)

// shortcut: XLSX 最多 32 个工作表、1024 个 ZIP 项和 64 MiB 解压数据；更大工作簿需要流式加载。
const (
	maxSpreadsheetSheets     = 32
	maxSpreadsheetZipEntries = 1024
	maxSpreadsheetExpanded   = 64 << 20
	maxSpreadsheetXML        = 32 << 20
)

type xlsxWorkbook struct {
	XMLName xml.Name `xml:"workbook"`
	Sheets  []struct {
		Name string `xml:"name,attr"`
		ID   string `xml:"id,attr"`
	} `xml:"sheets>sheet"`
}
type xlsxRelations struct {
	XMLName xml.Name `xml:"Relationships"`
	Entries []struct {
		ID     string `xml:"Id,attr"`
		Type   string `xml:"Type,attr"`
		Target string `xml:"Target,attr"`
		Mode   string `xml:"TargetMode,attr"`
	} `xml:"Relationship"`
}
type xlsxRich struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (r xlsxRich) value() string {
	var text strings.Builder
	text.WriteString(r.Text)
	for _, run := range r.Runs {
		text.WriteString(run.Text)
	}
	return text.String()
}

type xlsxCell struct {
	Ref     string    `xml:"r,attr"`
	Type    string    `xml:"t,attr"`
	Value   *string   `xml:"v"`
	Formula *string   `xml:"f"`
	Inline  *xlsxRich `xml:"is"`
}

func spreadsheetXLSX(data []byte) (*Spreadsheet, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("XLSX 不是有效的 ZIP 文件")
	}
	if len(archive.File) > maxSpreadsheetZipEntries {
		return nil, errors.New("XLSX 包内文件数量超过上限")
	}
	files := make(map[string]*zip.File, len(archive.File))
	var expanded uint64
	for _, f := range archive.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := f.Name
		if name == "" || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") || path.Clean(name) != name || strings.HasPrefix(name, "../") || files[name] != nil {
			return nil, errors.New("XLSX 包内文件路径无效或重复")
		}
		if f.UncompressedSize64 > maxSpreadsheetXML || expanded > maxSpreadsheetExpanded-f.UncompressedSize64 {
			return nil, errors.New("XLSX 解压数据超过上限")
		}
		expanded += f.UncompressedSize64
		files[name] = f
	}
	read := func(name string, limit int64) ([]byte, error) {
		f := files[name]
		if f == nil {
			return nil, fmt.Errorf("XLSX 缺少必要文件 %s", name)
		}
		if f.UncompressedSize64 > uint64(limit) {
			return nil, errors.New("XLSX XML 超过大小上限")
		}
		r, err := f.Open()
		if err != nil {
			return nil, errors.New("XLSX 文件无法解压")
		}
		defer r.Close()
		b, err := io.ReadAll(io.LimitReader(r, limit+1))
		if err != nil {
			return nil, errors.New("XLSX 文件解压失败")
		}
		if int64(len(b)) > limit {
			return nil, errors.New("XLSX XML 超过大小上限")
		}
		return b, nil
	}
	workbookData, err := read("xl/workbook.xml", 1<<20)
	if err != nil {
		return nil, err
	}
	relData, err := read("xl/_rels/workbook.xml.rels", 1<<20)
	if err != nil {
		return nil, err
	}
	var book xlsxWorkbook
	var relations xlsxRelations
	if xml.Unmarshal(workbookData, &book) != nil || xml.Unmarshal(relData, &relations) != nil {
		return nil, errors.New("XLSX 工作簿结构无效")
	}
	if len(book.Sheets) == 0 || len(book.Sheets) > maxSpreadsheetSheets {
		return nil, errors.New("XLSX 工作表数量无效或超过上限")
	}
	type rel struct{ typ, target string }
	rels := map[string]rel{}
	seenRelations := map[string]bool{}
	for _, entry := range relations.Entries {
		if entry.ID == "" || seenRelations[entry.ID] {
			return nil, errors.New("XLSX 工作簿关系无效或重复")
		}
		seenRelations[entry.ID] = true
		if strings.EqualFold(entry.Mode, "External") {
			continue
		}
		if entry.Mode != "" && !strings.EqualFold(entry.Mode, "Internal") {
			return nil, errors.New("XLSX 工作簿关系类型无效")
		}
		target := entry.Target
		if strings.ContainsAny(target, `\?#`) {
			return nil, errors.New("XLSX 工作簿关系路径无效")
		}
		for _, part := range strings.Split(target, "/") {
			if part == ".." || part == "." {
				return nil, errors.New("XLSX 工作簿关系路径无效")
			}
		}
		if strings.HasPrefix(target, "/") {
			target = strings.TrimPrefix(target, "/")
		} else {
			target = path.Join("xl", target)
		}
		if path.Clean(target) != target || !strings.HasPrefix(target, "xl/") {
			return nil, errors.New("XLSX 工作簿关系路径无效")
		}
		rels[entry.ID] = rel{typ: entry.Type, target: target}
	}
	var shared []string
	for _, entry := range rels {
		if !strings.HasSuffix(entry.typ, "/sharedStrings") {
			continue
		}
		if shared != nil {
			return nil, errors.New("XLSX 共享字符串关系重复")
		}
		b, err := read(entry.target, maxSpreadsheetXML)
		if err != nil {
			return nil, err
		}
		shared, err = spreadsheetSharedStrings(b)
		if err != nil {
			return nil, err
		}
	}
	bookResult := &Spreadsheet{}
	seenNames := map[string]bool{}
	totalValues := 0
	for _, sheet := range book.Sheets {
		if sheet.Name == "" || len(sheet.Name) > maxSpreadsheetValue || seenNames[strings.ToLower(sheet.Name)] {
			return nil, errors.New("XLSX 工作表名称无效或重复")
		}
		seenNames[strings.ToLower(sheet.Name)] = true
		relationship, ok := rels[sheet.ID]
		if !ok || !strings.HasSuffix(relationship.typ, "/worksheet") || !strings.HasPrefix(relationship.target, "xl/worksheets/") {
			return nil, fmt.Errorf("工作表 %q 的关系缺失或不受支持", sheet.Name)
		}
		b, err := read(relationship.target, maxSpreadsheetXML)
		if err != nil {
			return nil, err
		}
		rows, err := spreadsheetWorksheet(b, shared, &totalValues)
		if err != nil {
			return nil, fmt.Errorf("工作表 %q: %w", sheet.Name, err)
		}
		bookResult.Sheets = append(bookResult.Sheets, SpreadsheetSheet{Name: sheet.Name, Rows: rows})
	}
	return bookResult, nil
}

func spreadsheetSharedStrings(data []byte) ([]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var values []string
	root := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("XLSX 共享字符串格式无效")
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if !root {
			if start.Name.Local != "sst" {
				return nil, errors.New("XLSX 共享字符串格式无效")
			}
			root = true
		}
		if start.Name.Local != "si" {
			continue
		}
		var rich xlsxRich
		if decoder.DecodeElement(&rich, &start) != nil {
			return nil, errors.New("XLSX 共享字符串格式无效")
		}
		value := rich.value()
		if len(values) >= maxSpreadsheetCells || len(value) > maxSpreadsheetValue {
			return nil, errors.New("XLSX 共享字符串超过上限")
		}
		values = append(values, value)
	}
	if !root {
		return nil, errors.New("XLSX 共享字符串格式无效")
	}
	if values == nil {
		values = []string{}
	}
	return values, nil
}

func spreadsheetWorksheet(data []byte, shared []string, totalValues *int) ([][]string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var rows [][]string
	inData, inRow := false, false
	root := false
	row, nextRow, nextCol, slots, cells := 0, 1, 0, 0, 0
	rowExplicit, rowHasCell := false, false
	seen := map[string]bool{}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("XLSX 工作表 XML 格式无效")
		}
		switch element := token.(type) {
		case xml.StartElement:
			if !root {
				if element.Name.Local != "worksheet" {
					return nil, errors.New("XLSX 工作表 XML 格式无效")
				}
				root = true
			}
			switch element.Name.Local {
			case "sheetData":
				inData = true
			case "row":
				if !inData {
					continue
				}
				row, nextCol = nextRow, 0
				rowExplicit, rowHasCell = false, false
				for _, a := range element.Attr {
					if a.Name.Local == "r" {
						rowExplicit = true
						row, err = strconv.Atoi(a.Value)
						if err != nil {
							return nil, errors.New("XLSX 行号无效")
						}
					}
				}
				if row < 1 || row > maxSpreadsheetRows {
					return nil, errors.New("XLSX 行号超过上限")
				}
				nextRow, inRow = row+1, true
			case "c":
				if !inData || !inRow {
					continue
				}
				var cell xlsxCell
				if decoder.DecodeElement(&cell, &element) != nil {
					return nil, errors.New("XLSX 单元格格式无效")
				}
				col := nextCol
				if cell.Ref != "" {
					var cellRow int
					col, cellRow, err = spreadsheetCellRef(cell.Ref)
					if err == nil && !rowExplicit && !rowHasCell {
						row, nextRow = cellRow, cellRow+1
					}
					if err != nil || cellRow != row {
						return nil, errors.New("XLSX 单元格坐标无效")
					}
				}
				rowHasCell = true
				if col < 0 || col >= maxSpreadsheetCols {
					return nil, errors.New("XLSX 列数超过上限")
				}
				nextCol = col + 1
				key := strconv.Itoa(row) + ":" + strconv.Itoa(col)
				if seen[key] {
					return nil, errors.New("XLSX 单元格坐标重复")
				}
				seen[key] = true
				cells++
				if cells > maxSpreadsheetCells {
					return nil, errors.New("XLSX 单元格数量超过上限")
				}
				value, err := spreadsheetCellValue(cell, shared)
				if err != nil {
					return nil, fmt.Errorf("单元格 %s: %w", firstNonEmpty(cell.Ref, key), err)
				}
				if len(value) > maxSpreadsheetValue {
					return nil, errors.New("XLSX 单元格超过 64 KiB")
				}
				*totalValues += len(value)
				if *totalValues > maxSpreadsheetExpanded {
					return nil, errors.New("XLSX 单元格内容超过上限")
				}
				for len(rows) < row {
					rows = append(rows, nil)
				}
				if len(rows[row-1]) <= col {
					missing := col + 1 - len(rows[row-1])
					slots += missing
					if slots > maxSpreadsheetCells {
						return nil, errors.New("XLSX 单元格范围超过上限")
					}
					rows[row-1] = append(rows[row-1], make([]string, missing)...)
				}
				rows[row-1][col] = value
			}
		case xml.EndElement:
			if element.Name.Local == "row" && inData {
				inRow = false
			}
			if element.Name.Local == "sheetData" {
				inData = false
			}
		}
	}
	if !root {
		return nil, errors.New("XLSX 工作表 XML 格式无效")
	}
	return rows, nil
}

func spreadsheetCellRef(ref string) (int, int, error) {
	col, i := 0, 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		col = col*26 + int(ref[i]-'A') + 1
		i++
		if col > maxSpreadsheetCols {
			return 0, 0, errors.New("column limit")
		}
	}
	if i == 0 || i == len(ref) {
		return 0, 0, errors.New("invalid coordinate")
	}
	row, err := strconv.Atoi(ref[i:])
	if err != nil || row < 1 || row > maxSpreadsheetRows {
		return 0, 0, errors.New("invalid coordinate")
	}
	return col - 1, row, nil
}

func spreadsheetCellValue(cell xlsxCell, shared []string) (string, error) {
	if cell.Formula != nil && cell.Value == nil {
		return "", errors.New("公式缺少缓存值")
	}
	if cell.Type == "inlineStr" {
		if cell.Inline == nil {
			return "", errors.New("内联字符串缺失")
		}
		return cell.Inline.value(), nil
	}
	if cell.Value == nil {
		if cell.Type == "" || cell.Type == "n" {
			return "", nil
		}
		return "", errors.New("单元格值缺失")
	}
	s := *cell.Value
	switch cell.Type {
	case "s":
		index, err := strconv.Atoi(s)
		if err != nil || index < 0 || index >= len(shared) {
			return "", errors.New("共享字符串索引无效")
		}
		return shared[index], nil
	case "b":
		if s == "1" {
			return "true", nil
		}
		if s == "0" {
			return "false", nil
		}
		return "", errors.New("布尔值无效")
	case "", "n":
		if s == "" {
			return "", nil
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return "", errors.New("数值无效")
		}
		return s, nil
	case "str", "d":
		return s, nil
	case "e":
		return "", errors.New("单元格包含公式错误")
	default:
		return "", fmt.Errorf("不支持单元格类型 %q", cell.Type)
	}
}
