package handler

import (
	"net/http"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) RuntimeStatus(c *fox.Context) service.RuntimeStatus {
	return ctrl.service.RuntimeStatus()
}

func runtimePublicationResponseHeaders(c *fox.Context) error {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Next()
	return nil
}

func runtimePublicationRequest(c *fox.Context) error {
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		return apperrors.ErrBadRequest
	}
	return nil
}

func runtimePublicationBody(c *fox.Context) (service.RuntimePublicationCommandInput, error) {
	fields, err := secretRotationBody(c, false)
	if err != nil {
		return service.RuntimePublicationCommandInput{}, err
	}
	return service.RuntimePublicationCommandInput{RequestID: fields["request_id"], Reason: fields["reason"]}, nil
}

func (ctrl *Ctrl) PublishRuntime(c *fox.Context) error {
	if err := runtimePublicationRequest(c); err != nil {
		return err
	}
	in, err := runtimePublicationBody(c)
	if err != nil {
		return err
	}
	result, err := ctrl.service.PublishRuntimeCommand(c.Request.Context(), currentAuthentication(c), in)
	if err != nil {
		return err
	}
	return runtimePublicationReply(c, result)
}

func (ctrl *Ctrl) GetRuntimePublicationCommand(c *fox.Context) error {
	if err := runtimePublicationRequest(c); err != nil {
		return err
	}
	requestID := c.Param("requestID")
	if !secretRotationUUID.MatchString(requestID) {
		return apperrors.ErrBadRequest
	}
	result, err := ctrl.service.GetRuntimePublicationCommand(c.Request.Context(), currentAuthentication(c), requestID)
	if err != nil {
		return err
	}
	return runtimePublicationReply(c, result)
}

func runtimePublicationReply(c *fox.Context, result *service.RuntimePublicationCommandReceipt) error {
	if result == nil {
		return apperrors.ErrInternal
	}
	status := http.StatusOK
	switch result.Status {
	case "pending", "unknown":
		status = http.StatusAccepted
	case "refresh_returned_nil", "refresh_returned_error":
	default:
		return apperrors.ErrInternal
	}
	c.JSON(status, result)
	return nil
}
