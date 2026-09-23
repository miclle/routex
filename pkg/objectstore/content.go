package objectstore

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidateAttachment validates a bounded declared file, not arbitrary active web
// content. PDF bytes are retained without rendering or executing the document.
func ValidateAttachment(name string, data []byte) (string, error) {
	if !utf8.ValidString(name) || len(name) == 0 || utf8.RuneCountInString(name) > 200 || strings.ContainsAny(name, "/\\\r\n\x00") || len(data) == 0 || len(data) > MaxBytes {
		return "", ErrConfig
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrConfig
		}
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		if !bytes.Contains(data[max(0, len(data)-1024):], []byte("%%EOF")) {
			return "", ErrConfig
		}
		return "application/pdf", nil
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 8192 || cfg.Height > 8192 || cfg.Width*cfg.Height > 32_000_000 {
		return "", ErrConfig
	}
	switch format {
	case "png":
		return "image/png", nil
	case "jpeg":
		return "image/jpeg", nil
	}
	return "", ErrConfig
}
