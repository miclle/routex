package handler

import (
	"io"
	"strconv"

	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) GetConnectionStatus(c *fox.Context) (*service.ConnectionStatusRecord, error) {
	if err := connectionMetadataRequest(c); err != nil {
		return nil, err
	}
	result, err := ctrl.service.GetConnectionStatus(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"))
	if err == nil {
		c.Header("ETag", strconv.Quote(result.ETag))
	}
	return result, err
}
func (ctrl *Ctrl) WriteConnectionStatus(c *fox.Context) (*service.ConnectionStatusWriteResult, error) {
	if err := connectionMetadataRequest(c); err != nil {
		return nil, err
	}
	etag, err := connectionMetadataETag(c)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		return nil, apperrors.ErrBadRequest
	}
	var input service.ConnectionStatusInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	result, err := ctrl.service.WriteConnectionStatus(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"), etag, input)
	if err == nil {
		c.Header("ETag", strconv.Quote(result.Connection.ETag))
	}
	return result, err
}
