package handler

import (
	"net/http"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type PreviewPriceImportRequest struct {
	service.PriceImportDocument
}
type CommitPriceImportRequest struct {
	service.PriceImportDocument
	ETag   string `json:"etag"`
	Digest string `json:"preview_digest"`
}

func (ctrl *Ctrl) PreviewPriceImport(c *fox.Context) (*service.PriceImportPreview, error) {
	var request PreviewPriceImportRequest
	if err := decodeStrictRequest(c, &request); err != nil {
		return nil, err
	}
	return ctrl.service.PreviewPriceDocument(c.Request.Context(), currentAuthentication(c).User.ID, request.PriceImportDocument)
}
func (ctrl *Ctrl) CommitPriceImport(c *fox.Context) error {
	var request CommitPriceImportRequest
	if err := decodeStrictRequest(c, &request); err != nil {
		return err
	}
	result, err := ctrl.service.CommitPriceDocument(c.Request.Context(), currentAuthentication(c).User.ID, request.PriceImportDocument, request.ETag, request.Digest)
	if err != nil {
		return err
	}
	status := http.StatusOK
	if result.Preview != nil && !result.Preview.Valid {
		status = http.StatusUnprocessableEntity
	}
	c.JSON(status, result)
	return nil
}

func (ctrl *Ctrl) ExportPriceCSV(c *fox.Context) error {
	result, err := ctrl.service.ExportPriceCSV(c.Request.Context(), currentAuthentication(c).User.ID)
	if err != nil {
		return err
	}
	c.Header("Content-Disposition", `attachment; filename="routex-prices.csv"`)
	c.Header("ETag", `"`+result.ETag+`"`)
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", result.CSV)
	return nil
}

// Binary workbooks use base64 JSON and retain the same authenticated CSRF path.
func jsonPriceImportRequest(c *fox.Context) error {
	if c.ContentType() != "application/json" {
		return apperrors.ErrBadRequest
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	c.Next()
	return nil
}
