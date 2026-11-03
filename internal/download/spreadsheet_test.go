package download

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestSpreadsheetCSVPreviewAndImages(t *testing.T) {
	data := "\xef\xbb\xbf名称,主图,详情\r\n\"衣服,红\",https://example.test/a.jpg,https://example.test/b.jpg\r\n\r\n第二件,\"https://example.test/c.jpg\nhttps://example.test/d.jpg\",\r\n"
	book, err := ReadSpreadsheet("商品.CSV", strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	sheet := book.Sheets[0]
	preview, err := sheet.Preview()
	if err != nil || preview.Start != 1 || len(preview.Columns) != 3 || preview.Columns[0].Name != "名称" || preview.Columns[0].URL || !preview.Columns[1].URL || !preview.Columns[2].URL || preview.Columns[0].Sample != "衣服,红" || len(sheet.Rows) != 4 {
		t.Fatalf("preview=%+v rows=%+v err=%v", preview, sheet.Rows, err)
	}
	images, invalid, err := sheet.Images(preview, []bool{false, true, true}, []int{0})
	if err != nil || invalid != 1 || len(images) != 2 || images[0].Name != "衣服,红" || images[0].Row != 2 || images[1].Column != "详情" {
		t.Fatalf("images=%+v invalid=%d err=%v", images, invalid, err)
	}
	noNames, _, err := sheet.Images(preview, []bool{false, true, false}, nil)
	if err != nil || len(noNames) != 1 || noNames[0].Name != "2" {
		t.Fatalf("fallback=%+v err=%v", noNames, err)
	}
}

func TestSpreadsheetFilenameColumnOrder(t *testing.T) {
	sheet := SpreadsheetSheet{Rows: [][]string{{"甲", "网址", "丙"}, {"A", "https://example.test/x.jpg", "C"}}}
	preview, err := sheet.Preview()
	if err != nil {
		t.Fatal(err)
	}
	images, _, err := sheet.Images(preview, []bool{false, true, false}, []int{2, 0})
	if err != nil || len(images) != 1 || images[0].Name != "C_A" {
		t.Fatalf("ordered names=%+v err=%v", images, err)
	}
}

func TestSpreadsheetCSVNoHeaderAndGBK(t *testing.T) {
	raw, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte("https://example.test/one.jpg,名称\nhttps://example.test/two.jpg,衣服\n"))
	if err != nil {
		t.Fatal(err)
	}
	book, err := ReadSpreadsheet("a.csv", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := book.Sheets[0].Preview()
	if err != nil || preview.Start != 0 || preview.Columns[0].Name != "A" || preview.Columns[1].Name != "B" || preview.Columns[1].Sample != "名称" {
		t.Fatalf("no header=%+v err=%v", preview, err)
	}
}

func TestSpreadsheetCSVEmptyLineLimit(t *testing.T) {
	_, err := ReadSpreadsheet("empty.csv", strings.NewReader(strings.Repeat("\n", maxSpreadsheetRows+1)))
	if err == nil || !strings.Contains(err.Error(), "行数") {
		t.Fatalf("empty line limit: %v", err)
	}
}

func syntheticXLSX(t *testing.T, sheet1, sheet2 string, replacements map[string]string) []byte {
	t.Helper()
	files := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="第一表" sheetId="1" r:id="rId1"/><sheet name="第二表" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet2.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>名称</t></si><si><t>图片</t></si><si><r><t>https://example.test/</t></r><r><t>original.png</t></r></si></sst>`,
		"xl/worksheets/sheet1.xml":   sheet1,
		"xl/worksheets/sheet2.xml":   sheet2,
	}
	for name, value := range replacements {
		if value == "" {
			delete(files, name)
		} else {
			files[name] = value
		}
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for name, data := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const testSheet1 = `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="inlineStr"><is><t>信息</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><r><t>夏</t></r><r><t>装</t></r></is></c><c r="B2" t="s"><v>2</v></c><c r="C2" t="b"><v>1</v></c><c r="D2"><f>1+1</f><v>2</v></c></row></sheetData></worksheet>`
const testSheet2 = `<worksheet><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>https://example.test/second.png</t></is></c><c r="B1" t="str"><f>CONCAT("x","y")</f><v>xy</v></c></row></sheetData></worksheet>`

func TestSpreadsheetXLSXSheetsAndCellTypes(t *testing.T) {
	book, err := ReadSpreadsheet("a.xlsx", bytes.NewReader(syntheticXLSX(t, testSheet1, testSheet2, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if len(book.Sheets) != 2 || book.Sheets[0].Name != "第一表" || book.Sheets[1].Name != "第二表" {
		t.Fatalf("sheets=%+v", book.Sheets)
	}
	first := book.Sheets[0].Rows
	if first[1][0] != "夏装" || first[1][1] != "https://example.test/original.png" || first[1][2] != "true" || first[1][3] != "2" {
		t.Fatalf("cells=%+v", first)
	}
	preview, err := book.Sheets[1].Preview()
	if err != nil || preview.Start != 0 || !preview.Columns[0].URL || preview.Columns[1].Sample != "xy" {
		t.Fatalf("second preview=%+v err=%v", preview, err)
	}
}

func TestSpreadsheetRejectsInvalidAndOversized(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
		want string
	}{
		{"bad.xls", []byte("data"), "CSV 或 XLSX"},
		{"huge.csv", bytes.Repeat([]byte{'x'}, maxSpreadsheetFile+1), "16 MiB"},
		{"bad.csv", []byte("\"unfinished"), "CSV 格式"},
		{"bad.xlsx", []byte("not a zip"), "ZIP"},
		{"missing.xlsx", syntheticXLSX(t, testSheet1, testSheet2, map[string]string{"xl/worksheets/sheet2.xml": ""}), "缺少"},
		{"formula.xlsx", syntheticXLSX(t, `<worksheet><sheetData><row r="1"><c r="A1"><f>1+1</f></c></row></sheetData></worksheet>`, testSheet2, nil), "缓存"},
		{"type.xlsx", syntheticXLSX(t, `<worksheet><sheetData><row r="1"><c r="A1" t="unknown"><v>x</v></c></row></sheetData></worksheet>`, testSheet2, nil), "不支持"},
		{"badref.xlsx", syntheticXLSX(t, `<worksheet><sheetData><row r="1"><c r="A2" t="s"><v>2</v></c></row></sheetData></worksheet>`, testSheet2, nil), "坐标"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadSpreadsheet(tc.name, bytes.NewReader(tc.data))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestSpreadsheetRejectsUnsafeArchiveAndExpandedLimit(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{{"../escape.xml", 1}, {"huge.xml", maxSpreadsheetXML + 1}} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			z := zip.NewWriter(&b)
			w, err := z.Create(tc.name)
			if err != nil {
				t.Fatal(err)
			}
			chunk := bytes.Repeat([]byte{'x'}, 8192)
			for remaining := tc.size; remaining > 0; {
				n := len(chunk)
				if remaining < n {
					n = remaining
				}
				if _, err = w.Write(chunk[:n]); err != nil {
					t.Fatal(err)
				}
				remaining -= n
			}
			if err = z.Close(); err != nil {
				t.Fatal(err)
			}
			_, err = ReadSpreadsheet("test.xlsx", bytes.NewReader(b.Bytes()))
			if err == nil {
				t.Fatalf("accepted %s", tc.name)
			}
		})
	}
}

func TestSpreadsheetRejectsMissingRelationship(t *testing.T) {
	rels := `<Relationships><Relationship Id="rId1" Type="x/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`
	// Use inline strings in the first sheet so the second sheet's missing relation is reached.
	data := syntheticXLSX(t, testSheet2, testSheet2, map[string]string{"xl/_rels/workbook.xml.rels": rels})
	_, err := ReadSpreadsheet("test.xlsx", bytes.NewReader(data))
	if err == nil || !strings.Contains(err.Error(), "关系") {
		t.Fatalf("missing relationship: %v", err)
	}
}

func TestSpreadsheetRejectsExcessSheetsAndExpandedSharedValues(t *testing.T) {
	var book strings.Builder
	book.WriteString(`<workbook><sheets>`)
	for i := 0; i < maxSpreadsheetSheets+1; i++ {
		fmt.Fprintf(&book, `<sheet name="S%d"/>`, i)
	}
	book.WriteString(`</sheets></workbook>`)
	data := syntheticXLSX(t, testSheet1, testSheet2, map[string]string{"xl/workbook.xml": book.String()})
	if _, err := ReadSpreadsheet("test.xlsx", bytes.NewReader(data)); err == nil || !strings.Contains(err.Error(), "工作表数量") {
		t.Fatalf("sheet limit=%v", err)
	}
	shared := []string{strings.Repeat("x", maxSpreadsheetValue)}
	var sheet strings.Builder
	sheet.WriteString(`<worksheet><sheetData>`)
	for row := 1; row <= 2; row++ {
		fmt.Fprintf(&sheet, `<row r="%d">`, row)
		for col := 0; col < 600; col++ {
			fmt.Fprintf(&sheet, `<c r="%s%d" t="s"><v>0</v></c>`, spreadsheetColumnName(col), row)
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	total := 0
	if _, err := spreadsheetWorksheet([]byte(sheet.String()), shared, &total); err == nil || !strings.Contains(err.Error(), "内容超过") {
		t.Fatalf("expanded values=%v", err)
	}
}

func TestSpreadsheetRejectsTraversalRelationship(t *testing.T) {
	rels := `<Relationships><Relationship Id="rId1" Type="x/worksheet" Target="../xl/worksheets/sheet1.xml"/><Relationship Id="rId2" Type="x/worksheet" Target="worksheets/sheet2.xml"/></Relationships>`
	data := syntheticXLSX(t, testSheet2, testSheet2, map[string]string{"xl/_rels/workbook.xml.rels": rels})
	if _, err := ReadSpreadsheet("test.xlsx", bytes.NewReader(data)); err == nil || !strings.Contains(err.Error(), "路径") {
		t.Fatalf("traversal relation=%v", err)
	}
}

func TestSpreadsheetColumnLetters(t *testing.T) {
	for n, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 701: "ZZ", 702: "AAA"} {
		if got := spreadsheetColumnName(n); got != want {
			t.Error(fmt.Sprintf("column %d = %q", n, got))
		}
	}
}
