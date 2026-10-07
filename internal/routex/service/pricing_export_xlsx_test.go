package service

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/miclle/routex/pkg/pricing"
)

func TestPriceXLSXExportLiteralRoundtrip(t *testing.T) {
	rows := [][]string{
		{"pmo_one", "INPUT_TOKEN", "base", "1M_TOKEN", "USD", "0", "false", "128000", "\t=SUM(1,2) & <literal> 中文"},
		{"pmo_one", "OUTPUT_TOKEN", "base", "1M_TOKEN", "USD", "0.000000000000000001", "true", "128000", "\t=SUM(1,2) & <literal> 中文"},
	}
	raw, err := serializePriceXLSX(rows)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("export is not a genuine XLSX package: %v", err)
	}
	if len(archive.File) != 5 {
		t.Fatalf("package entries: got %d, want 5", len(archive.File))
	}
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		closeErr := reader.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read exported XML: %v %v", err, closeErr)
		}
		if validateSpreadsheetXML(data) != nil || bytes.Contains(data, []byte("<f>")) || bytes.Contains(data, []byte("TargetMode=\"External\"")) {
			t.Fatal("exported active or structurally invalid XML")
		}
	}
	sheet, err := readPriceXLSX(raw)
	if err != nil || sheet.Name != "Prices" {
		t.Fatalf("read exported sheet: %v", err)
	}
	header := append(append([]string{}, priceCSVColumns...), "upstream_name")
	for ri, row := range append([][]string{header}, rows...) {
		for ci, value := range row {
			cell := sheet.Cells[ri+1][ci+1]
			if cell.Value != value || cell.Kind != "text" || cell.Formula {
				t.Fatalf("literal cell %d/%d changed: %#v", ri, ci, cell)
			}
		}
	}
	parsed := parsePriceSpreadsheet("routex-prices.xlsx", raw)
	if len(parsed.Errors) != 0 || len(parsed.Rows) != 2 || parsed.Rows[1].Rate.Amount != "0.000000000000000001" || parsed.Rows[0].Rate.Enabled {
		t.Fatalf("exact upload roundtrip changed prices: %+v", parsed.Errors)
	}
}

func TestPriceXLSXExportEmpty(t *testing.T) {
	raw, err := serializePriceXLSX(nil)
	if err != nil {
		t.Fatal(err)
	}
	sheet, err := readPriceXLSX(raw)
	if err != nil || len(sheet.Cells) != 1 {
		t.Fatalf("empty catalogue must export headers only: %v", err)
	}
	if parsed := parsePriceSpreadsheet("routex-prices.xlsx", raw); len(parsed.Errors) == 0 {
		t.Fatal("empty export must not become a valid empty replacement import")
	}
}

func TestPriceXLSXExportRejectsInvalidText(t *testing.T) {
	for _, value := range []string{"\x00name", "name\x01", string([]byte{0xff}), "name\ufffe", "name\uffff"} {
		t.Run(strings.ReplaceAll(value, "\x00", "NUL"), func(t *testing.T) {
			row := []string{"pmo_one", "INPUT_TOKEN", "base", "1M_TOKEN", "USD", "0", "true", "0", value}
			raw, err := serializePriceXLSX([][]string{row})
			if !errors.Is(err, priceXLSXInvalidText) || raw != nil {
				t.Fatal("invalid exact recorded text returned partial workbook")
			}
		})
	}
}

func TestPriceXLSXExportDoesNotMutateRows(t *testing.T) {
	row := []string{"pmo_one", "INPUT_TOKEN", "base", "1M_TOKEN", "USD", "0", "false", "0", "+SUM(1,2)"}
	want := append([]string{}, row...)
	if _, err := serializePriceXLSX([][]string{row}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(row, want) {
		t.Fatal("workbook serialization changed stored CSV snapshot values")
	}
}

func TestPriceXLSXExportBounds(t *testing.T) {
	row := []string{"pmo_one", "INPUT_TOKEN", "base", "1M_TOKEN", "USD", "0", "true", "0", "Name"}
	for _, sample := range []struct {
		name string
		rows [][]string
	}{
		{"rate ceiling", make([][]string, priceExportModels*pricing.MaxRates+1)},
		{"XML tokens", repeatedPriceXLSXRows(row, 2000)},
		{"expanded bytes", repeatedPriceXLSXRows([]string{strings.Repeat("A", priceImportBytes), "INPUT_TOKEN", "base", "1M_TOKEN", "USD", "0", "true", "0", strings.Repeat("B", priceImportBytes)}, 70)},
		{"compressed bytes", highEntropyPriceXLSXRows(1200)},
	} {
		t.Run(sample.name, func(t *testing.T) {
			raw, err := serializePriceXLSX(sample.rows)
			if !errors.Is(err, priceXLSXTooLarge) || raw != nil {
				t.Fatalf("bound must fail without partial workbook: %v %d", err, len(raw))
			}
		})
	}
	var validXML bytes.Buffer
	validXML.WriteString("<worksheet><sheetData>")
	for index, row := range highEntropyPriceXLSXRows(1200) {
		if err := writePriceXLSXRow(&validXML, index+1, row); err != nil {
			t.Fatal(err)
		}
	}
	validXML.WriteString("</sheetData></worksheet>")
	if validXML.Len() > priceSpreadsheetExpandedBytes-2048 || validateSpreadsheetXML(validXML.Bytes()) != nil {
		t.Fatal("compressed-bound fixture also exceeds expanded/token bounds")
	}
	var buffer priceXLSXBuffer
	if n, err := buffer.Write(make([]byte, priceSpreadsheetBytes)); err != nil || n != priceSpreadsheetBytes {
		t.Fatal("exact compressed boundary rejected", n, err)
	}
	if n, err := buffer.Write([]byte{1}); !errors.Is(err, priceXLSXTooLarge) || n != 0 || buffer.Len() != priceSpreadsheetBytes {
		t.Fatal("overflow wrote partial bytes", n, err)
	}
}
func repeatedPriceXLSXRows(row []string, count int) [][]string {
	rows := make([][]string, count)
	for index := range rows {
		rows[index] = append([]string{}, row...)
	}
	return rows
}
func highEntropyPriceXLSXRows(count int) [][]string {
	rows := make([][]string, count)
	for index := range rows {
		rows[index] = make([]string, 9)
		for column := range rows[index] {
			var value strings.Builder
			for block := range 4 {
				sum := sha256.Sum256([]byte(fmt.Sprintf("%d/%d/%d", index, column, block)))
				value.WriteString(hex.EncodeToString(sum[:]))
			}
			rows[index][column] = value.String()
		}
	}
	return rows
}
func TestPriceXLSXExportLargeIsNotImportCapacityPromise(t *testing.T) {
	row := []string{"pmo_one", "INPUT_TOKEN", "base", "1M_TOKEN", "USD", "0", "true", "0", "Name"}
	raw, err := serializePriceXLSX(repeatedPriceXLSXRows(row, priceImportRows+1))
	if err != nil {
		t.Fatal(err)
	}
	if parsed := parsePriceSpreadsheet("routex-prices.xlsx", raw); len(parsed.Errors) == 0 {
		t.Fatal("large export silently bypassed existing upload row bound")
	}
}
func TestPriceXLSXExportPreservesLiteralWhitespaceAndPrefixes(t *testing.T) {
	for _, name := range []string{"=1+1", "+SUM(1,2)", "-1+1", "@SUM(1,2)", "\ufeff多行\nName\r\t "} {
		row := []string{"pmo_one", "INPUT_TOKEN", "base", "1M_TOKEN", "USD", "0", "false", "0", name}
		raw, err := serializePriceXLSX([][]string{row})
		if err != nil {
			t.Fatal(err)
		}
		sheet, err := readPriceXLSX(raw)
		if err != nil || sheet.Cells[2][9].Value != name || sheet.Cells[2][9].Formula {
			t.Fatal("literal display text changed", err)
		}
	}
}
