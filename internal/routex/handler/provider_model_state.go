package handler

import (
	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) SetProviderModelState(c *fox.Context) (*ProviderModelResponse, error) {
	var input struct {
		Enabled              *bool   `json:"enabled"`
		SupportsImageInput   *bool   `json:"supports_image_input"`
		SupportsPDFInput     *bool   `json:"supports_pdf_input"`
		ETag                 string  `json:"etag"`
		CapabilityReviewETag *string `json:"capability_review_etag"`
	}
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	if input.Enabled == nil && input.SupportsImageInput == nil && input.SupportsPDFInput == nil {
		return nil, apperrors.ErrBadRequest
	}
	model, err := ctrl.service.UpdateProviderModel(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_model_id"), input.ETag, service.ProviderModelUpdate{CapabilityReviewETag: input.CapabilityReviewETag, Enabled: input.Enabled, SupportsImageInput: input.SupportsImageInput, SupportsPDFInput: input.SupportsPDFInput})
	if err != nil {
		return nil, err
	}
	response := providerModelResponse(*model)
	return &response, nil
}
