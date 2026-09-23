package service

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strconv"
	"strings"
)

type priceWorkbookSheet struct {
	Name  string `xml:"name,attr"`
	State string `xml:"state,attr"`
	ID    string `xml:"id,attr"`
}
type priceWorkbookRelationship struct {
	ID     string `xml:"Id,attr"`
	Type   string `xml:"Type,attr"`
	Target string `xml:"Target,attr"`
	Mode   string `xml:"TargetMode,attr"`
}
type priceXMLText struct {
	Text string `xml:"t"`
	Runs []struct {
		Text string `xml:"t"`
	} `xml:"r"`
}

func (s priceXMLText) value() string {
	value := s.Text
	for _, run := range s.Runs {
		value += run.Text
	}
	return value
}

type priceXMLCell struct {
	Reference string       `xml:"r,attr"`
	Kind      string       `xml:"t,attr"`
	Value     string       `xml:"v"`
	Inline    priceXMLText `xml:"is"`
	Formula   *struct{}    `xml:"f"`
}

func readPriceXLSX(raw []byte) (*priceSpreadsheet, error) {
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	if len(archive.File) > priceSpreadsheetEntries {
		return nil, errors.New("ZIP entry limit")
	}
	parts := map[string][]byte{}
	total := int64(0)
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name := file.Name
		if name == "" || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") || name == ".." || strings.Contains(name, "\\") || file.Flags&1 != 0 {
			return nil, errors.New("unsupported ZIP entry")
		}
		lower := strings.ToLower(name)
		if strings.Contains(lower, "vbaproject") || strings.Contains(lower, "externallink") || strings.Contains(lower, "activex") || strings.Contains(lower, "embeddings/") || strings.HasSuffix(lower, ".bin") {
			return nil, errors.New("active workbook content")
		}
		if _, exists := parts[name]; exists {
			return nil, errors.New("duplicate ZIP entry")
		}
		if file.UncompressedSize64 > priceSpreadsheetExpandedBytes {
			return nil, errors.New("expanded ZIP limit")
		}
		reader, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, priceSpreadsheetExpandedBytes-total+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		total += int64(len(data))
		if total > priceSpreadsheetExpandedBytes {
			return nil, errors.New("expanded ZIP limit")
		}
		if strings.HasSuffix(lower, ".xml") || strings.HasSuffix(lower, ".rels") {
			if err := validateSpreadsheetXML(data); err != nil {
				return nil, err
			}
			if strings.HasSuffix(lower, ".rels") {
				var relationships struct {
					XMLName xml.Name                    `xml:"Relationships"`
					Items   []priceWorkbookRelationship `xml:"Relationship"`
				}
				if xml.Unmarshal(data, &relationships) != nil {
					return nil, errors.New("invalid relationships")
				}
				for _, rel := range relationships.Items {
					if rel.Mode != "" && rel.Mode != "Internal" {
						return nil, errors.New("external relationship")
					}
				}
			}
		}
		parts[name] = data
	}
	contentTypes := parts["[Content_Types].xml"]
	var contentTypeList struct {
		XMLName xml.Name `xml:"Types"`
		Items   []struct {
			ContentType string `xml:"ContentType,attr"`
		} `xml:",any"`
	}
	if len(contentTypes) == 0 || xml.Unmarshal(contentTypes, &contentTypeList) != nil {
		return nil, errors.New("invalid content types")
	}
	for _, item := range contentTypeList.Items {
		if strings.Contains(strings.ToLower(item.ContentType), "macro") || strings.Contains(strings.ToLower(item.ContentType), "vba") {
			return nil, errors.New("active workbook content")
		}
	}
	if len(contentTypes) == 0 {
		return nil, errors.New("unsupported workbook type")
	}
	var workbook struct {
		XMLName xml.Name             `xml:"workbook"`
		Sheets  []priceWorkbookSheet `xml:"sheets>sheet"`
	}
	if xml.Unmarshal(parts["xl/workbook.xml"], &workbook) != nil || len(workbook.Sheets) != 1 {
		return nil, errors.New("one worksheet required")
	}
	selected := workbook.Sheets[0]
	if selected.Name == "" || (selected.State != "" && selected.State != "visible") {
		return nil, errors.New("one visible worksheet required")
	}
	var relationships struct {
		XMLName xml.Name                    `xml:"Relationships"`
		Items   []priceWorkbookRelationship `xml:"Relationship"`
	}
	if xml.Unmarshal(parts["xl/_rels/workbook.xml.rels"], &relationships) != nil {
		return nil, errors.New("missing workbook relationships")
	}
	sheetParts := 0
	for name := range parts {
		if strings.HasPrefix(name, "xl/worksheets/") && strings.HasSuffix(name, ".xml") {
			sheetParts++
		}
	}
	if sheetParts != 1 {
		return nil, errors.New("one worksheet required")
	}
	target := ""
	for _, rel := range relationships.Items {
		if rel.ID == selected.ID {
			if target != "" || !strings.HasSuffix(rel.Type, "/worksheet") {
				return nil, errors.New("unsupported sheet relationship")
			}
			if strings.HasPrefix(rel.Target, "/") {
				target = strings.TrimPrefix(rel.Target, "/")
			} else {
				target = path.Join("xl", rel.Target)
			}
		}
	}
	if !strings.HasPrefix(target, "xl/worksheets/") || parts[target] == nil {
		return nil, errors.New("missing worksheet")
	}
	stringsTable := []string{}
	if data, exists := parts["xl/sharedStrings.xml"]; exists {
		var table struct {
			XMLName xml.Name       `xml:"sst"`
			Items   []priceXMLText `xml:"si"`
		}
		if err := xml.Unmarshal(data, &table); err != nil {
			return nil, err
		}
		if len(table.Items) > priceSpreadsheetCells {
			return nil, errors.New("shared string limit")
		}
		for _, item := range table.Items {
			value := item.value()
			if len(value) > priceImportBytes {
				return nil, errors.New("cell text limit")
			}
			stringsTable = append(stringsTable, value)
		}
	}
	sheet := newPriceSpreadsheet()
	sheet.Name = selected.Name
	decoder := xml.NewDecoder(bytes.NewReader(parts[target]))
	row := 0
	elements := []string{}
	sheetDataSeen := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if _, end := token.(xml.EndElement); end {
			if len(elements) > 0 {
				elements = elements[:len(elements)-1]
			}
			continue
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if len(elements) == 0 && start.Name.Local != "worksheet" {
			return nil, errors.New("invalid worksheet root")
		}
		if start.Name.Local == "sheetData" {
			if len(elements) != 1 || elements[0] != "worksheet" || sheetDataSeen {
				return nil, errors.New("invalid sheet data")
			}
			sheetDataSeen = true
		}
		if start.Name.Local == "row" && (len(elements) != 2 || elements[1] != "sheetData") {
			return nil, errors.New("invalid row nesting")
		}
		if start.Name.Local == "c" {
			if len(elements) != 3 || elements[2] != "row" {
				return nil, errors.New("invalid cell nesting")
			}
		} else {
			elements = append(elements, start.Name.Local)
		}
		switch start.Name.Local {
		case "mergeCell":
			return nil, errors.New("merged cells are unsupported")
		case "row":
			row++
			for _, attr := range start.Attr {
				if attr.Name.Local == "r" {
					row, err = strconv.Atoi(attr.Value)
					if err != nil {
						return nil, err
					}
				}
			}
			if row < 1 || row > priceImportRows+1 {
				return nil, errors.New("sheet row limit")
			}
		case "c":
			var cell priceXMLCell
			if err := decoder.DecodeElement(&cell, &start); err != nil {
				return nil, err
			}
			cellRow, col, err := priceCellReference(cell.Reference)
			if err != nil || cellRow != row {
				return nil, errors.New("invalid cell reference")
			}
			value := priceSpreadsheetCell{Value: cell.Value, Formula: cell.Formula != nil}
			switch cell.Kind {
			case "s":
				index, err := strconv.Atoi(cell.Value)
				if err != nil || index < 0 || index >= len(stringsTable) {
					return nil, errors.New("invalid shared string reference")
				}
				value.Kind = "text"
				value.Value = stringsTable[index]
			case "inlineStr":
				value.Kind = "text"
				value.Value = cell.Inline.value()
			case "b":
				value.Kind = "boolean"
			case "", "n":
				value.Kind = "number"
			default:
				value.Kind = "error"
			}
			if len(value.Value) > priceImportBytes {
				return nil, errors.New("cell text limit")
			}
			if err := sheet.put(cellRow, col, value); err != nil {
				return nil, err
			}
		}
	}
	if !sheetDataSeen {
		return nil, errors.New("missing sheet data")
	}
	return sheet, nil
}
func priceCellReference(reference string) (int, int, error) {
	if len(reference) < 2 || reference[0] < 'A' || reference[0] > 'I' {
		return 0, 0, errors.New("cell column limit")
	}
	row, err := strconv.Atoi(reference[1:])
	if err != nil || row < 1 || row > priceImportRows+1 || strconv.Itoa(row) != reference[1:] {
		return 0, 0, errors.New("cell row limit")
	}
	return row, int(reference[0]-'A') + 1, nil
}
