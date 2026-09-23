package service

import (
	"encoding/binary"
	"unicode/utf16"
)

// SST continuations change character width at record boundaries. Control fields
// remain record-local; formatting runs can span records without a width flag.
type priceSSTCursor struct {
	parts        [][]byte
	part, offset int
}

func (c *priceSSTCursor) next() bool {
	if c.part+1 >= len(c.parts) {
		return false
	}
	c.part++
	c.offset = 0
	return true
}
func (c *priceSSTCursor) control(size int) ([]byte, error) {
	if c.part >= len(c.parts) {
		return nil, errPriceXLS
	}
	if c.offset == len(c.parts[c.part]) && !c.next() {
		return nil, errPriceXLS
	}
	part := c.parts[c.part]
	if size > len(part)-c.offset {
		return nil, errPriceXLS
	}
	value := part[c.offset : c.offset+size]
	c.offset += size
	return value, nil
}
func (c *priceSSTCursor) skip(size int) error {
	for size > 0 {
		if c.offset == len(c.parts[c.part]) && !c.next() {
			return errPriceXLS
		}
		n := min(size, len(c.parts[c.part])-c.offset)
		c.offset += n
		size -= n
	}
	return nil
}
func (c *priceSSTCursor) string() (string, error) {
	header, err := c.control(3)
	if err != nil {
		return "", err
	}
	count := int(binary.LittleEndian.Uint16(header))
	flags := header[2]
	if count > priceImportBytes || flags&^byte(9) != 0 {
		return "", errPriceXLS
	}
	rich := 0
	if flags&8 != 0 {
		raw, err := c.control(2)
		if err != nil {
			return "", err
		}
		rich = int(binary.LittleEndian.Uint16(raw))
	}
	width := 1
	if flags&1 != 0 {
		width = 2
	}
	chars := make([]uint16, 0, count)
	for len(chars) < count {
		if c.offset == len(c.parts[c.part]) {
			if !c.next() {
				return "", errPriceXLS
			}
			flag, err := c.control(1)
			if err != nil || flag[0] > 1 {
				return "", errPriceXLS
			}
			width = 1 + int(flag[0])
		}
		remaining := len(c.parts[c.part]) - c.offset
		n := min(count-len(chars), remaining/width)
		if n == 0 {
			return "", errPriceXLS
		}
		for range n {
			var ch uint16
			if width == 1 {
				ch = uint16(c.parts[c.part][c.offset])
			} else {
				ch = binary.LittleEndian.Uint16(c.parts[c.part][c.offset:])
			}
			c.offset += width
			chars = append(chars, ch)
		}
	}
	if err := c.skip(rich * 4); err != nil {
		return "", err
	}
	for index := 0; index < len(chars); index++ {
		if chars[index] >= 0xd800 && chars[index] <= 0xdbff {
			index++
			if index >= len(chars) || chars[index] < 0xdc00 || chars[index] > 0xdfff {
				return "", errPriceXLS
			}
		} else if chars[index] >= 0xdc00 && chars[index] <= 0xdfff {
			return "", errPriceXLS
		}
	}
	value := string(utf16.Decode(chars))
	if len(value) > priceImportBytes {
		return "", errPriceXLS
	}
	return value, nil
}
func priceBIFFSST(parts [][]byte) ([]string, error) {
	if len(parts) == 0 || len(parts[0]) < 8 {
		return nil, errPriceXLS
	}
	cursor := priceSSTCursor{parts: parts, offset: 8}
	total, unique := binary.LittleEndian.Uint32(parts[0]), binary.LittleEndian.Uint32(parts[0][4:])
	if unique > priceSpreadsheetCells || total > priceSpreadsheetCells || unique > total {
		return nil, errPriceXLS
	}
	values := make([]string, 0, int(unique))
	for range unique {
		value, err := cursor.string()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	if cursor.part != len(parts)-1 || cursor.offset != len(parts[cursor.part]) {
		return nil, errPriceXLS
	}
	return values, nil
}
