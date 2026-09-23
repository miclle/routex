package handler

import (
	"io"
	"mime"
	"net/http"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
	"github.com/miclle/routex/pkg/objectstore"
)

func (ctrl *Ctrl) UploadAttachment(c *fox.Context) error {
	if c.Request.URL.RawQuery != "" {
		return apperrors.ErrBadRequest
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, objectstore.MaxBytes+(64<<10))
	if err := c.Request.ParseMultipartForm(objectstore.MaxBytes + (64 << 10)); err != nil {
		return apperrors.ErrBadRequest
	}
	if c.Request.MultipartForm == nil {
		return apperrors.ErrBadRequest
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	form := c.Request.MultipartForm
	if len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
		return apperrors.ErrBadRequest
	}
	file, err := form.File["file"][0].Open()
	if err != nil {
		return apperrors.ErrBadRequest
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, objectstore.MaxBytes+1))
	if err != nil || len(data) > objectstore.MaxBytes {
		return apperrors.ErrBadRequest
	}
	result, err := ctrl.service.UploadAttachment(c.Request.Context(), currentAuthentication(c).User.ID, form.File["file"][0].Filename, data)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, result)
	return nil
}
func (ctrl *Ctrl) Attachment(c *fox.Context) (*service.AttachmentView, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.Attachment(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("attachment_id"))
}
func (ctrl *Ctrl) AttachmentContent(c *fox.Context) error {
	if c.Request.URL.RawQuery != "" {
		return apperrors.ErrBadRequest
	}
	record, data, err := ctrl.service.AttachmentContent(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("attachment_id"))
	if err != nil {
		return err
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": record.Name}))
	c.Header("Content-Security-Policy", "sandbox")
	c.Data(http.StatusOK, record.MIME, data)
	return nil
}
func (ctrl *Ctrl) DeleteAttachment(c *fox.Context) (*service.AttachmentView, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.DeleteAttachment(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("attachment_id"))
}
