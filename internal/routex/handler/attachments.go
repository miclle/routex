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
	name, data, err := attachmentUpload(c)
	if err != nil {
		return err
	}
	result, err := ctrl.service.UploadAttachment(c.Request.Context(), currentAuthentication(c).User.ID, name, data)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, result)
	return nil
}

func (ctrl *Ctrl) UploadProjectAttachment(c *fox.Context) error {
	name, data, err := attachmentUpload(c)
	if err != nil {
		return err
	}
	result, err := ctrl.service.UploadProjectAttachment(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("project_id"), name, data)
	if err != nil {
		return err
	}
	c.JSON(http.StatusCreated, result)
	return nil
}

func attachmentUpload(c *fox.Context) (string, []byte, error) {
	if c.Request.URL.RawQuery != "" {
		return "", nil, apperrors.ErrBadRequest
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, objectstore.MaxBytes+(64<<10))
	if err := c.Request.ParseMultipartForm(objectstore.MaxBytes + (64 << 10)); err != nil {
		return "", nil, apperrors.ErrBadRequest
	}
	if c.Request.MultipartForm == nil {
		return "", nil, apperrors.ErrBadRequest
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	form := c.Request.MultipartForm
	if len(form.Value) != 0 || len(form.File) != 1 || len(form.File["file"]) != 1 {
		return "", nil, apperrors.ErrBadRequest
	}
	file, err := form.File["file"][0].Open()
	if err != nil {
		return "", nil, apperrors.ErrBadRequest
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, objectstore.MaxBytes+1))
	if err != nil || len(data) > objectstore.MaxBytes {
		return "", nil, apperrors.ErrBadRequest
	}
	return form.File["file"][0].Filename, data, nil
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
	writeAttachmentContent(c, record, data)
	return nil
}

func (ctrl *Ctrl) ProjectAttachment(c *fox.Context) (*service.AttachmentView, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ProjectAttachment(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("project_id"), c.Param("attachment_id"))
}

func (ctrl *Ctrl) ProjectAttachmentContent(c *fox.Context) error {
	if c.Request.URL.RawQuery != "" {
		return apperrors.ErrBadRequest
	}
	record, data, err := ctrl.service.ProjectAttachmentContent(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("project_id"), c.Param("attachment_id"))
	if err != nil {
		return err
	}
	writeAttachmentContent(c, record, data)
	return nil
}

func writeAttachmentContent(c *fox.Context, record *service.AttachmentView, data []byte) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": record.Name}))
	c.Header("Content-Security-Policy", "sandbox")
	c.Data(http.StatusOK, record.MIME, data)
}

func (ctrl *Ctrl) DeleteAttachment(c *fox.Context) (*service.AttachmentView, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.DeleteAttachment(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("attachment_id"))
}

func (ctrl *Ctrl) DeleteProjectAttachment(c *fox.Context) (*service.AttachmentView, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.DeleteProjectAttachment(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("project_id"), c.Param("attachment_id"))
}
