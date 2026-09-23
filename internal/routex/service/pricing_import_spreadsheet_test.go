package service

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestPriceSpreadsheetBIFF8Literal(t *testing.T) {
	raw, err := os.ReadFile("testdata/prices/literal-biff8.xls")
	if err != nil {
		t.Fatal(err)
	}
	parsed := parsePriceSpreadsheet("prices.xls", raw)
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors: %+v", parsed.Errors)
	}
	if len(parsed.Items) != 1 || parsed.Items[0].Rates[0].Amount != "123456789012345678.123456789012345678" {
		t.Fatalf("lost precision: %+v", parsed.Items)
	}
}

func TestPriceSpreadsheetContinuedSST(t *testing.T) {
	raw, err := os.ReadFile("testdata/prices/continued-sst-biff8.xls")
	if err != nil {
		t.Fatal(err)
	}
	parsed := parsePriceSpreadsheet("prices.xls", raw)
	if len(parsed.Errors) != 0 {
		t.Fatalf("errors: %+v", parsed.Errors)
	}
	if len(parsed.Items) != 20 || len(parsed.Rows) != 80 {
		t.Fatalf("unexpected count: %d %d", len(parsed.Items), len(parsed.Rows))
	}
}

func spreadsheetFixture(t testing.TB, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/prices/" + name + "-biff8.xls")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func testPriceXLSX(t testing.TB, change func(map[string]string)) []byte {
	t.Helper()
	rows := [][]string{priceCSVColumns, {"pmo_one", "INPUT_TOKEN", "base", "1M_TOKEN", "USD", "123456789012345678.123456789012345678", "true", "0"}}
	var sheet strings.Builder
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for ri, row := range rows {
		fmt.Fprintf(&sheet, `<row r="%d">`, ri+1)
		for ci, value := range row {
			fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, sheetCellAddress(ri+1, ci+1), value)
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	parts := map[string]string{
		"[Content_Types].xml":        `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Prices" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   sheet.String(),
	}
	if change != nil {
		change(parts)
	}
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	names := make([]string, 0, len(parts))
	for name := range parts {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, parts[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func hasWorkbookError(parsed parsedPriceCSV, code string) bool {
	for _, err := range parsed.Errors {
		if err.Code == code {
			return true
		}
	}
	return false
}
func TestPriceSpreadsheetXLSXLiteralAndLocations(t *testing.T) {
	raw := testPriceXLSX(t, nil)
	parsed := parsePriceSpreadsheet("prices.XLSX", raw)
	if len(parsed.Errors) != 0 || len(parsed.Items) != 1 || parsed.Items[0].Rates[0].Amount != "123456789012345678.123456789012345678" {
		t.Fatalf("literal decimal lost: %+v", parsed)
	}
	if parsed.Source != "xlsx" || parsed.Sheet != "Prices" || parsed.digest("etag") == priceImportDigest("etag", parsed.Items) {
		t.Fatal("format provenance missing")
	}
	raw = testPriceXLSX(t, func(p map[string]string) {
		p["xl/worksheets/sheet1.xml"] = strings.Replace(p["xl/worksheets/sheet1.xml"], `<c r="F2" t="inlineStr"><is><t>123456789012345678.123456789012345678</t></is></c>`, `<c r="F2"><f>1+1</f><v>2</v></c>`, 1)
	})
	parsed = parsePriceSpreadsheet("prices.xlsx", raw)
	if !hasWorkbookError(parsed, "formula_cell") {
		t.Fatalf("formula accepted: %+v", parsed)
	}
	for _, err := range parsed.Errors {
		if err.Code == "formula_cell" && (err.Sheet != "Prices" || err.Cell != "F2" || err.Row != 2) {
			t.Fatalf("unlocatable error: %+v", err)
		}
	}
}
func TestPriceSpreadsheetRejectsUnsafeContainers(t *testing.T) {
	cases := map[string]func(map[string]string){
		"wrong_root": func(p map[string]string) {
			p["xl/worksheets/sheet1.xml"] = strings.ReplaceAll(p["xl/worksheets/sheet1.xml"], "worksheet", "notWorksheet")
		},
		"wrong_nesting": func(p map[string]string) {
			p["xl/worksheets/sheet1.xml"] = strings.ReplaceAll(strings.ReplaceAll(p["xl/worksheets/sheet1.xml"], "<sheetData>", "<sheetData><nested>"), "</sheetData>", "</nested></sheetData>")
		},
		"external": func(p map[string]string) {
			p["xl/_rels/workbook.xml.rels"] = strings.Replace(p["xl/_rels/workbook.xml.rels"], `Target="worksheets/sheet1.xml"`, `Target="https://example.invalid/sheet.xml" TargetMode="External"`, 1)
		},
		"hidden": func(p map[string]string) {
			p["xl/workbook.xml"] = strings.Replace(p["xl/workbook.xml"], `name="Prices"`, `name="Prices" state="hidden"`, 1)
		},
		"second_sheet": func(p map[string]string) { p["xl/worksheets/sheet2.xml"] = `<worksheet/>` },
		"vba":          func(p map[string]string) { p["xl/vbaProject.bin"] = "macro" },
		"macro_type": func(p map[string]string) {
			p["[Content_Types].xml"] = `<Types><Override ContentType="application/vnd.ms-excel.sheet.macroEnabled.main+xml"/></Types>`
		},
		"zip_bomb":  func(p map[string]string) { p["large.xml"] = strings.Repeat("x", priceSpreadsheetExpandedBytes+1) },
		"traversal": func(p map[string]string) { p["../file"] = "value" },
		"xml_directive": func(p map[string]string) {
			p["xl/workbook.xml"] = `<!DOCTYPE workbook [<!ENTITY x SYSTEM "file:///etc/passwd">]><workbook/>`
		},
		"depth": func(p map[string]string) { p["deep.xml"] = strings.Repeat("<x>", 65) + strings.Repeat("</x>", 65) },
		"rows": func(p map[string]string) {
			p["xl/worksheets/sheet1.xml"] = strings.ReplaceAll(p["xl/worksheets/sheet1.xml"], `r="2"`, `r="162"`)
		},
		"columns": func(p map[string]string) {
			p["xl/worksheets/sheet1.xml"] = strings.ReplaceAll(p["xl/worksheets/sheet1.xml"], `r="H2"`, `r="J2"`)
		},
		"merged": func(p map[string]string) {
			p["xl/worksheets/sheet1.xml"] = strings.Replace(p["xl/worksheets/sheet1.xml"], `</worksheet>`, `<mergeCells><mergeCell ref="A1:B1"/></mergeCells></worksheet>`, 1)
		},
		"duplicate_cell": func(p map[string]string) {
			p["xl/worksheets/sheet1.xml"] = strings.Replace(p["xl/worksheets/sheet1.xml"], `r="H2"`, `r="G2"`, 1)
		},
		"invalid_shared_string": func(p map[string]string) {
			p["xl/worksheets/sheet1.xml"] = strings.Replace(p["xl/worksheets/sheet1.xml"], `<c r="A2" t="inlineStr"><is><t>pmo_one</t></is></c>`, `<c r="A2" t="s"><v>999</v></c>`, 1)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if parsed := parsePriceSpreadsheet("prices.xlsx", testPriceXLSX(t, change)); len(parsed.Errors) == 0 {
				t.Fatal("unsafe workbook accepted")
			}
		})
	}
	if parsed := parsePriceSpreadsheet("prices.xlsx", make([]byte, priceSpreadsheetBytes+1)); !hasWorkbookError(parsed, "file_too_large") {
		t.Fatal("size cap ignored")
	}
	raw := spreadsheetFixture(t, "literal")
	for _, size := range []int{0, 7, 511, len(raw) - 1} {
		if parsed := parsePriceSpreadsheet("prices.xls", raw[:size]); len(parsed.Errors) == 0 {
			t.Fatalf("truncated file %d accepted", size)
		}
	}
	for _, change := range []func([]byte){
		func(b []byte) { binary.LittleEndian.PutUint32(b[44:], math.MaxUint32) },
		func(b []byte) {
			fat := int(binary.LittleEndian.Uint32(b[76:]))
			dir := int(binary.LittleEndian.Uint32(b[48:]))
			binary.LittleEndian.PutUint32(b[(fat+1)*512+dir*4:], uint32(dir))
		},
		func(b []byte) {
			dir := int(binary.LittleEndian.Uint32(b[48:]))
			binary.LittleEndian.PutUint32(b[(dir+1)*512+128+68:], 1)
		},
	} {
		copy := bytes.Clone(raw)
		change(copy)
		if parsed := parsePriceSpreadsheet("prices.xls", copy); len(parsed.Errors) == 0 {
			t.Fatal("cyclic/oversized OLE accepted")
		}
	}
}
func TestPriceSpreadsheetRejectsCachedOrRoundedAmounts(t *testing.T) {
	for _, test := range []struct{ name, code string }{{"formula", "formula_cell"}, {"numeric-amount", "amount_not_text"}} {
		t.Run(test.name, func(t *testing.T) {
			parsed := parsePriceSpreadsheet("prices.xls", spreadsheetFixture(t, test.name))
			if !hasWorkbookError(parsed, test.code) {
				t.Fatalf("unsafe amount accepted: %+v", parsed.Errors)
			}
		})
	}
	raw := testPriceXLSX(t, func(p map[string]string) {
		p["xl/worksheets/sheet1.xml"] = strings.Replace(p["xl/worksheets/sheet1.xml"], `<c r="F2" t="inlineStr"><is><t>123456789012345678.123456789012345678</t></is></c>`, `<c r="F2"><v>123456789012345680</v></c>`, 1)
	})
	if parsed := parsePriceSpreadsheet("prices.xlsx", raw); !hasWorkbookError(parsed, "amount_not_text") {
		t.Fatal("rounded numeric amount accepted")
	}
}
func TestPriceSpreadsheetTransport(t *testing.T) {
	csv := "header"
	for _, input := range []PriceImportDocument{{}, {CSV: &csv, Filename: "rates.xls"}, {Filename: "rates.xls"}} {
		if _, err := input.parse(); err == nil {
			t.Fatal("ambiguous/incomplete transport accepted")
		}
	}
	parsed, err := (PriceImportDocument{Filename: "rates.xlsx", ContentBase64: base64.StdEncoding.EncodeToString(testPriceXLSX(t, nil))}).parse()
	if err != nil || len(parsed.Errors) != 0 {
		t.Fatalf("base64 rejected: %+v %v", parsed.Errors, err)
	}
	parsed, err = (PriceImportDocument{Filename: "rates.xls", ContentBase64: "!!!"}).parse()
	if err != nil || !hasWorkbookError(parsed, "invalid_encoding") {
		t.Fatal("invalid encoding not reported")
	}
}
func FuzzPriceSpreadsheetContainers(f *testing.F) {
	f.Add("prices.xls", spreadsheetFixture(f, "literal"))
	f.Add("prices.xls", spreadsheetFixture(f, "continued-sst"))
	f.Add("prices.xlsx", testPriceXLSX(f, nil))
	f.Add("prices.xls", []byte{0xd0, 0xcf})
	f.Add("prices.xlsx", []byte("PK"))
	f.Fuzz(func(t *testing.T, name string, raw []byte) {
		if len(raw) > priceSpreadsheetBytes {
			return
		}
		_ = parsePriceSpreadsheet(name, raw)
	})
}

func TestPriceSpreadsheetSSTContinuationWidthAndBounds(t *testing.T) {
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header, 1)
	binary.LittleEndian.PutUint32(header[4:], 1)
	// Four characters: compressed AB then a UTF-16 continuation containing 中文.
	first := append(bytes.Clone(header), 4, 0, 0, 'A', 'B')
	second := []byte{1, 0x2d, 0x4e, 0x87, 0x65}
	values, err := priceBIFFSST([][]byte{first, second})
	if err != nil || len(values) != 1 || values[0] != "AB中文" {
		t.Fatalf("SST continuation width lost: %v %v", values, err)
	}
	for _, parts := range [][][]byte{
		{first, []byte{2, 0x2d, 0x4e, 0x87, 0x65}},   // invalid character-width flag
		{first, []byte{1, 0x2d}},                     // truncated UTF-16 code unit
		{append(bytes.Clone(header), 4, 0, 1, 0x2d)}, // split UTF-16 code unit
		{append(bytes.Clone(header), 1, 0, 4)},       // unsupported phonetic string
	} {
		if _, err := priceBIFFSST(parts); err == nil {
			t.Fatal("invalid SST accepted")
		}
	}
	huge := bytes.Clone(header)
	binary.LittleEndian.PutUint32(huge, math.MaxUint32)
	binary.LittleEndian.PutUint32(huge[4:], math.MaxUint32)
	if _, err := priceBIFFSST([][]byte{huge}); err == nil {
		t.Fatal("unbounded shared string count accepted")
	}
}
