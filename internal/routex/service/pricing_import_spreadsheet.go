package service

import (
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"io"
	"strconv"
	"strings"
)

const priceSpreadsheetBytes = 512 << 10
const priceSpreadsheetExpandedBytes = 4 << 20
const priceSpreadsheetEntries = 128
const priceSpreadsheetCells = (priceImportRows + 1) * 9

type priceSpreadsheetCell struct {
	Value, Kind string
	Formula     bool
}
type priceSpreadsheet struct {
	Name   string
	Cells  map[int]map[int]priceSpreadsheetCell
	Errors []PriceImportError
}

func newPriceSpreadsheet() *priceSpreadsheet {
	return &priceSpreadsheet{Cells: map[int]map[int]priceSpreadsheetCell{}, Errors: []PriceImportError{}}
}
func sheetCellAddress(row, col int) string { return string(rune('A'+col-1)) + strconv.Itoa(row) }
func (s *priceSpreadsheet) add(row, col int, code, message string) {
	cell := ""
	if row > 0 && col > 0 {
		cell = sheetCellAddress(row, col)
	}
	s.Errors = append(s.Errors, PriceImportError{Row: row, Column: s.Cells[1][col].Value, Cell: cell, Sheet: s.Name, Code: code, Message: message})
}
func (s *priceSpreadsheet) put(row, col int, cell priceSpreadsheetCell) error {
	if row < 1 || row > priceImportRows+1 || col < 1 || col > 9 {
		return errors.New("sheet bounds")
	}
	if s.Cells[row] == nil {
		s.Cells[row] = map[int]priceSpreadsheetCell{}
	}
	if _, exists := s.Cells[row][col]; exists {
		return errors.New("duplicate cell")
	}
	s.Cells[row][col] = cell
	return nil
}

// validateSpreadsheetXML limits structural work before decoding XML records.
// Go's XML decoder never fetches external entities; directives are rejected too.
func validateSpreadsheetXML(raw []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	depth, tokens, roots := 0, 0, 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		tokens++
		if tokens > 100000 {
			return errors.New("XML token limit")
		}
		switch token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				if roots > 1 {
					return errors.New("multiple XML roots")
				}
			}
			depth++
			if depth > 64 {
				return errors.New("XML depth limit")
			}
		case xml.EndElement:
			depth--
		case xml.Directive:
			return errors.New("XML directives are unsupported")
		}
	}
	if depth != 0 || roots != 1 {
		return errors.New("incomplete XML")
	}
	return nil
}

func spreadsheetCSV(sheet *priceSpreadsheet) parsedPriceCSV {
	// Canonical validation stays in the existing CSV parser. Physical CSV row
	// positions are mapped back to workbook rows even when strings contain lines.
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	positions := map[int]int{}
	headerWidth := 0
	for col, cell := range sheet.Cells[1] {
		if cell.Value != "" || cell.Formula {
			headerWidth = max(headerWidth, col)
		}
	}
	if headerWidth == 0 {
		sheet.add(1, 1, "missing_header", "Row 1 must contain the price import headers.")
		return parsedPriceCSV{Rows: []priceImportRow{}, Items: []PriceInput{}, Errors: sheet.Errors}
	}
	sheetRows := []int{1}
	for row := 2; row <= priceImportRows+1; row++ {
		if len(sheet.Cells[row]) > 0 {
			sheetRows = append(sheetRows, row)
		}
	}
	line := 1
	for _, row := range sheetRows {
		values := make([]string, headerWidth)
		nonempty := row == 1
		for col := 1; col <= 9; col++ {
			cell, exists := sheet.Cells[row][col]
			if !exists {
				continue
			}
			if cell.Value == "" && !cell.Formula && cell.Kind != "error" {
				continue
			}
			nonempty = true
			if col > headerWidth {
				sheet.add(row, col, "unexpected_cell", "Values outside the header columns are unsupported.")
				continue
			}
			column := sheet.Cells[1][col].Value
			switch {
			case cell.Formula:
				sheet.add(row, col, "formula_cell", "Formulas and cached formula results are unsupported; replace the cell with a literal value.")
			case cell.Kind == "text":
				values[col-1] = cell.Value
			case row == 1:
				sheet.add(row, col, "header_not_text", "Price import headers must be literal text.")
			case column == "amount":
				sheet.add(row, col, "amount_not_text", "Amount cells must be text to preserve exact decimal precision.")
			case column == "enabled" && cell.Kind == "boolean" && (cell.Value == "0" || cell.Value == "1"):
				values[col-1] = strconv.FormatBool(cell.Value == "1")
			case column == "context_threshold" && cell.Kind == "number":
				values[col-1] = cell.Value
			default:
				sheet.add(row, col, "unsupported_cell", "Use literal text, or a boolean for enabled and an exact integer for context_threshold.")
			}
		}
		if !nonempty {
			continue
		}
		positions[line] = row
		before := buffer.Len()
		if err := writer.Write(values); err != nil {
			sheet.add(row, 0, "invalid_workbook", "The workbook could not be normalized.")
			break
		}
		writer.Flush()
		line += bytes.Count(buffer.Bytes()[before:], []byte("\n"))
	}
	parsed := parsePriceCSV(buffer.String())
	parsed.Sheet = sheet.Name
	parsed.Columns = map[string]int{}
	for col := 1; col <= headerWidth; col++ {
		parsed.Columns[sheet.Cells[1][col].Value] = col
	}
	for index := range parsed.Rows {
		parsed.Rows[index].Line = positions[parsed.Rows[index].Line]
	}
	for index := range parsed.Errors {
		if row, ok := positions[parsed.Errors[index].Row]; ok {
			parsed.Errors[index].Row = row
			parsed.Errors[index].Sheet = sheet.Name
			parsed.Errors[index].Cell = parsed.cell(row, parsed.Errors[index].Column)
		}
	}
	parsed.Errors = append(parsed.Errors, sheet.Errors...)
	return parsed
}

func spreadsheetFileError(code, message string) parsedPriceCSV {
	return parsedPriceCSV{Rows: []priceImportRow{}, Items: []PriceInput{}, Errors: []PriceImportError{{Row: 0, Code: code, Message: message}}}
}
func parsePriceSpreadsheet(filename string, raw []byte) parsedPriceCSV {
	if len(raw) == 0 || len(raw) > priceSpreadsheetBytes {
		return spreadsheetFileError("file_too_large", "Workbook files must contain 1 byte to 512 KiB.")
	}
	var sheet *priceSpreadsheet
	var err error
	switch {
	case strings.HasSuffix(strings.ToLower(filename), ".xlsx"):
		sheet, err = readPriceXLSX(raw)
	case strings.HasSuffix(strings.ToLower(filename), ".xls"):
		sheet, err = readPriceXLS(raw)
	default:
		return spreadsheetFileError("unsupported_file", "Use an .xlsx or BIFF8 .xls workbook.")
	}
	if err != nil {
		var issue *priceWorkbookIssue
		if errors.As(err, &issue) {
			return spreadsheetFileError(issue.code, issue.message)
		}
		switch err.Error() {
		case "one worksheet required", "one visible worksheet required":
			return spreadsheetFileError("unsupported_sheets", "Use exactly one visible worksheet; additional or hidden worksheets are unsupported.")
		case "active workbook content", "unsupported workbook type":
			return spreadsheetFileError("active_content", "Macros, embedded objects, and active workbook content are unsupported.")
		case "external relationship":
			return spreadsheetFileError("external_links", "External workbook links are unsupported; use literal values.")
		case "expanded ZIP limit", "ZIP entry limit":
			return spreadsheetFileError("expanded_file_limit", "The workbook exceeds the expanded size or entry limit.")
		case "merged cells are unsupported":
			return spreadsheetFileError("merged_cells", "Merged cells are unsupported; use one value per cell.")
		}
		return spreadsheetFileError("invalid_workbook", "The workbook is malformed or uses unsupported features. Use one visible worksheet with literal text amounts; legacy files must use the documented BIFF8 subset.")
	}
	parsed := spreadsheetCSV(sheet)
	parsed.Source = strings.TrimPrefix(strings.ToLower(filename[strings.LastIndex(filename, "."):]), ".")
	return parsed
}

type priceWorkbookIssue struct{ code, message string }

func (e *priceWorkbookIssue) Error() string { return e.message }
func unsupportedPriceBIFF() error {
	return &priceWorkbookIssue{"unsupported_biff", "This BIFF8 workbook contains an unsupported record or string feature. Use a plain visible worksheet with literal values and no external links, phonetic strings, or shared formulas."}
}
