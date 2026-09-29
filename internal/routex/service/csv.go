package service

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// safeCSVCell prevents spreadsheet software from interpreting untrusted text as
// a formula while leaving the original value available for display.
func safeCSVCell(value string) string {
	visible := strings.TrimLeftFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
	if visible == "" {
		return value
	}
	first, _ := utf8.DecodeRuneInString(visible)
	if strings.ContainsRune("=+-@", first) {
		return "'" + value
	}
	return value
}
