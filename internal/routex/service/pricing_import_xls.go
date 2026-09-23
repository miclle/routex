package service

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strconv"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"
)

var errPriceXLS = errors.New("unsupported or malformed BIFF8 workbook")

// The container reader follows FAT chains. This separate budget bounds repeated
// reads even for adversarial chains before any workbook records are decoded.
type priceOLEReader struct {
	reader *bytes.Reader
	budget int64
	reads  int
}

func (r *priceOLEReader) ReadAt(p []byte, off int64) (int, error) {
	r.reads++
	r.budget -= int64(len(p))
	if r.reads > 8192 || r.budget < 0 || off < 0 || off > r.reader.Size() || int64(len(p)) > r.reader.Size()-off {
		return 0, errPriceXLS
	}
	return r.reader.ReadAt(p, off)
}
func readPriceXLS(raw []byte) (*priceSpreadsheet, error) {
	if len(raw) < 512 || len(raw) > priceSpreadsheetBytes || !bytes.Equal(raw[:8], []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
		return nil, errPriceXLS
	}
	version, shift := binary.LittleEndian.Uint16(raw[26:]), binary.LittleEndian.Uint16(raw[30:])
	if (version != 3 || shift != 9) && (version != 4 || shift != 12) {
		return nil, errPriceXLS
	}
	sectorSize := 1 << shift
	if len(raw)%sectorSize != 0 || len(raw) < sectorSize*2 || binary.LittleEndian.Uint16(raw[28:]) != 0xfffe || binary.LittleEndian.Uint16(raw[32:]) != 6 {
		return nil, errPriceXLS
	}
	sectors := uint32(len(raw)/sectorSize - 1)
	directory := binary.LittleEndian.Uint32(raw[48:])
	if directory >= sectors || binary.LittleEndian.Uint32(raw[44:]) == 0 {
		return nil, errPriceXLS
	}
	root := raw[(int(directory)+1)*sectorSize:]
	if root[66] != 5 || binary.LittleEndian.Uint32(root[68:]) != math.MaxUint32 || binary.LittleEndian.Uint32(root[72:]) != math.MaxUint32 {
		return nil, errPriceXLS
	}
	for _, offset := range []int{40, 44, 64, 72} {
		if binary.LittleEndian.Uint32(raw[offset:]) > sectors {
			return nil, errPriceXLS
		}
	}
	reader, err := mscfb.New(&priceOLEReader{reader: bytes.NewReader(raw), budget: int64(len(raw)) * 64})
	if err != nil {
		return nil, errPriceXLS
	}
	if len(reader.File) > 128 {
		return nil, errPriceXLS
	}
	var workbook []byte
	for index, file := range reader.File {
		if index == 0 {
			continue
		}
		if len(file.Path) > 0 || file.FileInfo().IsDir() {
			return nil, errPriceXLS
		}
		switch file.Name {
		case "Workbook":
			if workbook != nil || file.Size <= 0 || file.Size > priceSpreadsheetBytes {
				return nil, errPriceXLS
			}
			workbook, err = io.ReadAll(io.LimitReader(file, priceSpreadsheetBytes+1))
			if err != nil || int64(len(workbook)) != file.Size {
				return nil, errPriceXLS
			}
		case "EncryptedPackage", "EncryptionInfo":
			return nil, &priceWorkbookIssue{"encrypted_workbook", "Encrypted workbooks are unsupported. Save an unencrypted local copy."}
		case "SummaryInformation", "DocumentSummaryInformation":
			if file.Initial != 5 {
				return nil, errPriceXLS
			}
		default:
			return nil, errPriceXLS // Includes encrypted packages, VBA and embedded objects.
		}
	}
	if workbook == nil {
		return nil, errPriceXLS
	}
	return readPriceBIFF(workbook)
}

type priceBIFFRecord struct {
	id   uint16
	data []byte
	next int
}

func priceBIFFAt(raw []byte, offset int) (priceBIFFRecord, error) {
	if offset < 0 || offset > len(raw)-4 {
		return priceBIFFRecord{}, errPriceXLS
	}
	length := int(binary.LittleEndian.Uint16(raw[offset+2:]))
	if length > 8224 || length > len(raw)-offset-4 {
		return priceBIFFRecord{}, errPriceXLS
	}
	return priceBIFFRecord{binary.LittleEndian.Uint16(raw[offset:]), raw[offset+4 : offset+4+length], offset + 4 + length}, nil
}
func priceBIFFBOF(record priceBIFFRecord, kind uint16) bool {
	return record.id == 0x0809 && len(record.data) == 16 && binary.LittleEndian.Uint16(record.data) == 0x0600 && binary.LittleEndian.Uint16(record.data[2:]) == kind
}

// Decode the BIFF8 string representation without interpreting formatting. The
// deliberately bounded subset rejects phonetic data.
func priceBIFFString(raw []byte, short bool) (string, int, error) {
	header := 3
	count := 0
	if short {
		header = 2
		if len(raw) < header {
			return "", 0, errPriceXLS
		}
		count = int(raw[0])
	} else {
		if len(raw) < header {
			return "", 0, errPriceXLS
		}
		count = int(binary.LittleEndian.Uint16(raw))
	}
	flags := raw[header-1]
	if flags&^byte(0x09) != 0 || count > priceImportBytes {
		return "", 0, errPriceXLS
	}
	rich := 0
	if flags&8 != 0 {
		if len(raw) < header+2 {
			return "", 0, errPriceXLS
		}
		rich = int(binary.LittleEndian.Uint16(raw[header:]))
		header += 2
	}
	width := 1
	if flags&1 != 0 {
		width = 2
	}
	end := header + count*width
	if end > len(raw) || rich > (len(raw)-end)/4 {
		return "", 0, errPriceXLS
	}
	chars := make([]uint16, count)
	for index := range chars {
		if width == 1 {
			chars[index] = uint16(raw[header+index])
		} else {
			chars[index] = binary.LittleEndian.Uint16(raw[header+index*2:])
		}
	}
	// Invalid UTF-16 must not silently turn into a different identifier.
	for index := 0; index < len(chars); index++ {
		if chars[index] >= 0xd800 && chars[index] <= 0xdbff {
			index++
			if index >= len(chars) || chars[index] < 0xdc00 || chars[index] > 0xdfff {
				return "", 0, errPriceXLS
			}
		} else if chars[index] >= 0xdc00 && chars[index] <= 0xdfff {
			return "", 0, errPriceXLS
		}
	}
	value := string(utf16.Decode(chars))
	if len(value) > priceImportBytes {
		return "", 0, errPriceXLS
	}
	return value, end + rich*4, nil
}
func readPriceBIFF(raw []byte) (*priceSpreadsheet, error) {
	first, err := priceBIFFAt(raw, 0)
	if err != nil || !priceBIFFBOF(first, 5) {
		return nil, errPriceXLS
	}
	sheet := newPriceSpreadsheet()
	sheetOffset := -1
	offset := first.next
	table := []string{}
	hasSST := false
globals:
	for {
		record, err := priceBIFFAt(raw, offset)
		if err != nil {
			return nil, err
		}
		offset = record.next
		switch record.id {
		case 0x000a:
			if len(record.data) != 0 {
				return nil, errPriceXLS
			}
			break globals
		case 0x0085:
			if sheetOffset != -1 || len(record.data) < 8 || record.data[4] != 0 || record.data[5] != 0 {
				return nil, errPriceXLS
			}
			sheetOffset = int(binary.LittleEndian.Uint32(record.data))
			name, size, err := priceBIFFString(record.data[6:], true)
			if err != nil || size != len(record.data)-6 || name == "" {
				return nil, errPriceXLS
			}
			sheet.Name = name
		case 0x002f:
			return nil, &priceWorkbookIssue{"encrypted_workbook", "Encrypted workbooks are unsupported. Save an unencrypted local copy."}
		case 0x00fc:
			if hasSST {
				return nil, errPriceXLS
			}
			hasSST = true
			parts := [][]byte{record.data}
			for {
				next, readErr := priceBIFFAt(raw, offset)
				if readErr != nil {
					return nil, readErr
				}
				if next.id != 0x003c {
					break
				}
				parts = append(parts, next.data)
				offset = next.next
			}
			table, err = priceBIFFSST(parts)
			if err != nil {
				return nil, err
			}
		// Formatting, protection flags and workbook display metadata only. Formula,
		// continuation, macro and external-reference records are not in this list.
		case 0x00e1, 0x00c1, 0x00e2, 0x005c, 0x0042, 0x0161, 0x013d, 0x009c, 0x0019, 0x0012, 0x0063, 0x0013, 0x01af, 0x01bc, 0x0040, 0x008d, 0x003d, 0x0022, 0x000e, 0x01b7, 0x00da, 0x0031, 0x041e, 0x00e0, 0x0293, 0x0160, 0x008c, 0x0092, 0x01c1, 0x01b6:
		default:
			return nil, unsupportedPriceBIFF()
		}
	}
	if sheetOffset != offset {
		return nil, errPriceXLS
	}
	bof, err := priceBIFFAt(raw, sheetOffset)
	if err != nil || !priceBIFFBOF(bof, 0x10) {
		return nil, errPriceXLS
	}
	offset = bof.next
	for {
		record, err := priceBIFFAt(raw, offset)
		if err != nil {
			return nil, err
		}
		offset = record.next
		if record.id == 0x000a {
			if len(record.data) != 0 {
				return nil, errPriceXLS
			}
			for _, b := range raw[offset:] {
				if b != 0 {
					return nil, errPriceXLS
				}
			}
			return sheet, nil
		}
		if err := priceBIFFCell(sheet, record, table); err != nil {
			return nil, err
		}
	}
}
func priceBIFFNumber(bits uint64) string {
	value := math.Float64frombits(bits)
	// Only the enumerated threshold integers may be accepted as native numbers.
	// Native amounts are rejected regardless of their formatted appearance.
	if value == 0 || value == 128000 || value == 200000 {
		return strconv.FormatInt(int64(value), 10)
	}
	return "unsupported_number"
}
func priceBIFFRK(raw uint32) string {
	var value float64
	if raw&2 != 0 {
		value = float64(int32(raw) >> 2)
	} else {
		value = math.Float64frombits(uint64(raw&^3) << 32)
	}
	if raw&1 != 0 {
		value /= 100
	}
	return priceBIFFNumber(math.Float64bits(value))
}
func priceBIFFCell(sheet *priceSpreadsheet, record priceBIFFRecord, table []string) error {
	data := record.data
	put := func(col int, cell priceSpreadsheetCell) error {
		if len(data) < 6 {
			return errPriceXLS
		}
		return sheet.put(int(binary.LittleEndian.Uint16(data))+1, col+1, cell)
	}
	col := 0
	if len(data) >= 4 {
		col = int(binary.LittleEndian.Uint16(data[2:]))
	}
	switch record.id {
	case 0x00fd:
		if len(data) != 10 {
			return errPriceXLS
		}
		index := binary.LittleEndian.Uint32(data[6:])
		if uint64(index) >= uint64(len(table)) {
			return errPriceXLS
		}
		return put(col, priceSpreadsheetCell{Value: table[index], Kind: "text"})
	case 0x0203:
		if len(data) != 14 {
			return errPriceXLS
		}
		return put(col, priceSpreadsheetCell{Value: priceBIFFNumber(binary.LittleEndian.Uint64(data[6:])), Kind: "number"})
	case 0x027e:
		if len(data) != 10 {
			return errPriceXLS
		}
		return put(col, priceSpreadsheetCell{Value: priceBIFFRK(binary.LittleEndian.Uint32(data[6:])), Kind: "number"})
	case 0x00bd:
		if len(data) < 12 || (len(data)-6)%6 != 0 {
			return errPriceXLS
		}
		last := int(binary.LittleEndian.Uint16(data[len(data)-2:]))
		if last < col || last-col+1 != (len(data)-6)/6 {
			return errPriceXLS
		}
		for index := col; index <= last; index++ {
			if err := put(index, priceSpreadsheetCell{Value: priceBIFFRK(binary.LittleEndian.Uint32(data[6+(index-col)*6:])), Kind: "number"}); err != nil {
				return err
			}
		}
		return nil
	case 0x0205:
		if len(data) != 8 {
			return errPriceXLS
		}
		kind := "boolean"
		if data[7] != 0 {
			kind = "error"
		}
		return put(col, priceSpreadsheetCell{Value: strconv.Itoa(int(data[6])), Kind: kind})
	case 0x0006:
		if len(data) < 22 {
			return errPriceXLS
		}
		return put(col, priceSpreadsheetCell{Kind: "error", Formula: true})
	case 0x0201, 0x00be: // Blank cells contain formatting only; no cached values.
		return nil
	// Worksheet layout, print settings and protection flags contain no cell values.
	case 0x000d, 0x000c, 0x000f, 0x0011, 0x0010, 0x005f, 0x0080, 0x0225, 0x0081, 0x0200, 0x002a, 0x002b, 0x0082, 0x001b, 0x001a, 0x0014, 0x0015, 0x0083, 0x0084, 0x0026, 0x0027, 0x0028, 0x0029, 0x00a1, 0x0012, 0x00dd, 0x0019, 0x0063, 0x0013, 0x0208, 0x023e, 0x020b, 0x00d7, 0x001d, 0x0041, 0x0055, 0x007d:
		return nil
	default:
		return unsupportedPriceBIFF()
	}
}
