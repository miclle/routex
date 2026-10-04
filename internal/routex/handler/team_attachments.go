package handler

import (
	"errors"
	"net/http"

	"github.com/fox-gonic/fox"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) prepareTeamAttachment(c *fox.Context, write bool) (*service.TeamSessionIdentity, error) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "sandbox")
	identity, _, err := ctrl.prepareTeamGateway(c, write)
	if err != nil {
		return nil, err
	}
	if err := validateTeamNativeInputs(c.Request, false); err != nil {
		return nil, err
	}
	if c.Request.Method != http.MethodPost && (c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0) {
		return nil, &service.GatewayError{Status: 400, Code: "invalid_request_error", Message: "Attachment request bodies are not supported."}
	}
	return identity, nil
}

func (ctrl *Ctrl) UploadTeamAttachment(c *fox.Context) error {
	identity, err := ctrl.prepareTeamAttachment(c, true)
	if err != nil {
		writeGatewayError(c, err)
		return nil
	}
	name, data, err := attachmentUpload(c)
	if err != nil {
		return teamAttachmentError(c, err)
	}
	view, err := ctrl.service.UploadTeamAttachment(c.Request.Context(), identity, name, data)
	if err != nil {
		return teamAttachmentError(c, err)
	}
	c.JSON(http.StatusCreated, view)
	return nil
}

func (ctrl *Ctrl) GetTeamAttachment(c *fox.Context) error {
	identity, err := ctrl.prepareTeamAttachment(c, false)
	if err != nil {
		writeGatewayError(c, err)
		return nil
	}
	view, err := ctrl.service.TeamAttachment(c.Request.Context(), identity, c.Param("attachment_id"))
	if err != nil {
		return teamAttachmentError(c, err)
	}
	c.JSON(http.StatusOK, view)
	return nil
}

func (ctrl *Ctrl) TeamAttachmentContent(c *fox.Context) error {
	identity, err := ctrl.prepareTeamAttachment(c, false)
	if err != nil {
		writeGatewayError(c, err)
		return nil
	}
	view, data, err := ctrl.service.TeamAttachmentContent(c.Request.Context(), identity, c.Param("attachment_id"))
	if err != nil {
		return teamAttachmentError(c, err)
	}
	writeAttachmentContent(c, view, data)
	return nil
}

func (ctrl *Ctrl) DeleteTeamAttachment(c *fox.Context) error {
	identity, err := ctrl.prepareTeamAttachment(c, true)
	if err != nil {
		writeGatewayError(c, err)
		return nil
	}
	view, err := ctrl.service.DeleteTeamAttachment(c.Request.Context(), identity, c.Param("attachment_id"))
	if err != nil {
		return teamAttachmentError(c, err)
	}
	c.JSON(http.StatusOK, view)
	return nil
}

func teamAttachmentError(c *fox.Context, err error) error {
	var gateway *service.GatewayError
	if errors.As(err, &gateway) {
		writeGatewayError(c, gateway)
		return nil
	}
	return err
}
