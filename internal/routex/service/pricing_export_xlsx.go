package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"strconv"
	"unicode/utf8"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/pkg/pricing"
)

var priceXLSXTooLarge = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "price catalogue exceeds the XLSX export limit"}
var priceXLSXInvalidText = &apperrors.Error{Code: http.StatusUnprocessableEntity, Message: "price catalogue contains text unsupported by XLSX"}

type PriceXLSXExport struct {
	ETag string
	XLSX []byte
}

func (s *Service) ExportPriceXLSX(ctx context.Context, actorID string) (*PriceXLSXExport, error) {
	rows := [][]string{}
	etag, err := s.exportPriceRows(ctx, actorID, priceXLSXTooLarge, func(row []string) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	raw, err := serializePriceXLSX(rows)
	if err != nil {
		return nil, err
	}
	return &PriceXLSXExport{ETag: etag, XLSX: raw}, nil
}

// serializePriceXLSX emits a complete bounded workbook, never a partial file.
func serializePriceXLSX(rows [][]string) ([]byte, error) {
	if len(rows) > priceExportModels*pricing.MaxRates {
		return nil, priceXLSXTooLarge
	}
	var sheet bytes.Buffer
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	header := append(append([]string{}, priceCSVColumns...), "upstream_name")
	for index, row := range append([][]string{header}, rows...) {
		if err := writePriceXLSXRow(&sheet, index+1, row); err != nil {
			return nil, err
		}
		if sheet.Len() > priceSpreadsheetExpandedBytes {
			return nil, priceXLSXTooLarge
		}
	}
	sheet.WriteString(`</sheetData></worksheet>`)
	parts := []struct{ name, xml string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Prices" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/worksheets/sheet1.xml", sheet.String()},
	}
	// Five internal parts, shallow XML and no active content. Reuse the parser's
	// structural limits without claiming large exports fit its import row bounds.
	expanded := 0
	for _, part := range parts {
		expanded += len(part.xml)
		if expanded > priceSpreadsheetExpandedBytes || validateSpreadsheetXML([]byte(part.xml)) != nil {
			return nil, priceXLSXTooLarge
		}
	}
	var output priceXLSXBuffer
	archive := zip.NewWriter(&output)
	for _, part := range parts {
		writer, err := archive.Create(part.name)
		if err != nil {
			return nil, err
		}
		if _, err := writer.Write([]byte(part.xml)); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return bytes.Clone(output.Bytes()), nil
}

func writePriceXLSXRow(sheet *bytes.Buffer, index int, row []string) error {
	if len(row) != len(priceCSVColumns)+1 {
		return priceXLSXInvalidText
	}
	sheet.WriteString(`<row r="` + strconv.Itoa(index) + `">`)
	for column, text := range row {
		if !validPriceXLSXText(text) {
			return priceXLSXInvalidText
		}
		sheet.WriteString(`<c r="` + sheetCellAddress(index, column+1) + `" t="inlineStr"><is><t xml:space="preserve">`)
		if err := xml.EscapeText(sheet, []byte(text)); err != nil {
			return err
		}
		sheet.WriteString(`</t></is></c>`)
	}
	sheet.WriteString(`</row>`)
	return nil
}

func validPriceXLSXText(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if r != '\t' && r != '\n' && r != '\r' && (r < 0x20 || r == 0xfffe || r == 0xffff) {
			return false
		}
	}
	return true
}

type priceXLSXBuffer struct{ bytes.Buffer }

func (buffer *priceXLSXBuffer) Write(raw []byte) (int, error) {
	if len(raw) > priceSpreadsheetBytes-buffer.Len() {
		return 0, priceXLSXTooLarge
	}
	return buffer.Buffer.Write(raw)
}
