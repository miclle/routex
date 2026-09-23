package service

import (
	"context"
	"encoding/base64"
	"strings"

	apperrors "github.com/miclle/routex/internal/routex/errors"
)

// PriceImportDocument accepts exactly one transport. Format and provenance are
// derived on the server; callers cannot choose a repository or update source.
type PriceImportDocument struct {
	CSV           *string `json:"csv,omitempty"`
	Filename      string  `json:"filename,omitempty"`
	ContentBase64 string  `json:"content_base64,omitempty"`
}

func (document PriceImportDocument) parse() (parsedPriceCSV, error) {
	if document.CSV != nil {
		if document.Filename != "" || document.ContentBase64 != "" {
			return parsedPriceCSV{}, apperrors.ErrBadRequest
		}
		return parsePriceCSV(*document.CSV), nil
	}
	if document.Filename == "" || len(document.Filename) > 255 || document.ContentBase64 == "" {
		return parsedPriceCSV{}, apperrors.ErrBadRequest
	}
	if len(document.ContentBase64) > base64.StdEncoding.EncodedLen(priceSpreadsheetBytes) {
		return spreadsheetFileError("file_too_large", "Workbook files must not exceed 512 KiB."), nil
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(document.ContentBase64)
	if err != nil {
		return spreadsheetFileError("invalid_encoding", "Workbook content must use standard base64 encoding."), nil
	}
	return parsePriceSpreadsheet(document.Filename, raw), nil
}
func (parsed parsedPriceCSV) cell(row int, column string) string {
	col := parsed.Columns[column]
	if row <= 0 || col == 0 {
		return ""
	}
	return sheetCellAddress(row, col)
}
func (parsed parsedPriceCSV) digest(etag string) string {
	// Retain the established CSV digest. Spreadsheet formats additionally bind
	// server-derived provenance so the preview also covers that audit change.
	if parsed.Source == "csv" {
		return priceImportDigest(etag, parsed.Items)
	}
	return priceImportDigest(etag+"\x00"+parsed.Source, parsed.Items)
}
func (s *Service) PreviewPriceDocument(ctx context.Context, actorID string, document PriceImportDocument) (*PriceImportPreview, error) {
	parsed, err := document.parse()
	if err != nil {
		return nil, err
	}
	return s.previewPriceDocument(ctx, actorID, parsed)
}
func (s *Service) CommitPriceDocument(ctx context.Context, actorID string, document PriceImportDocument, etag, digest string) (*PriceImportCommit, error) {
	if strings.TrimSpace(etag) == "" {
		return nil, apperrors.ErrBadRequest
	}
	parsed, err := document.parse()
	if err != nil {
		return nil, err
	}
	return s.commitPriceDocument(ctx, actorID, parsed, etag, digest)
}
