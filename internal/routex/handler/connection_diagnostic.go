package handler

import (
	"io"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) TestConnection(c *fox.Context) (*service.ConnectionDiagnosticResult, error) {
	if err := connectionMetadataRequest(c); err != nil {
		return nil, err
	}
	etag, err := connectionMetadataETag(c)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 4*1024+1))
	if err != nil || len(raw) > 4*1024 {
		return nil, apperrors.ErrBadRequest
	}
	var input service.ConnectionDiagnosticInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	return ctrl.service.TestConnection(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("connection_id"), etag, input)
}
