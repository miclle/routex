package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
)

func (ctrl *Ctrl) SetProviderModelState(c *fox.Context) (*ProviderModelResponse, error) {
	var input struct {
		Enabled *bool  `json:"enabled"`
		ETag    string `json:"etag"`
	}
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	if input.Enabled == nil {
		return nil, apperrors.ErrBadRequest
	}
	model, err := ctrl.service.SetProviderModelState(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_model_id"), input.ETag, *input.Enabled)
	if err != nil {
		return nil, err
	}
	return &ProviderModelResponse{ID: model.ID, UpstreamName: model.UpstreamName, Enabled: !model.Disabled, ETag: model.ETag}, nil
}
