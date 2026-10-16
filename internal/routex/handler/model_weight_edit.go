package handler

import (
	"io"
	"strconv"

	"github.com/fox-gonic/fox"
	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

type ModelWeightEditResponse struct {
	Model          *ModelResponse `json:"model"`
	ReviewETag     string         `json:"review_etag"`
	RuntimeApplied bool           `json:"runtime_applied"`
}

func modelWeightEditResponse(c *fox.Context, review *service.ModelWeightEditReview, err error) (*ModelWeightEditResponse, error) {
	if err != nil {
		return nil, err
	}
	c.Header("ETag", strconv.Quote(review.ReviewETag))
	return &ModelWeightEditResponse{modelResponse(*review.Model), review.ReviewETag, review.RuntimeApplied}, nil
}

func (ctrl *Ctrl) ReviewModelWeightEdit(c *fox.Context) (*ModelWeightEditResponse, error) {
	q, err := modelWeightRequest(c)
	if err != nil || len(q) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	review, err := ctrl.service.ReviewModelWeightEdit(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"))
	return modelWeightEditResponse(c, review, err)
}

func (ctrl *Ctrl) SetReviewedModelWeights(c *fox.Context) (*ModelWeightEditResponse, error) {
	q, err := modelWeightRequest(c)
	if err != nil || len(q) != 0 {
		return nil, apperrors.ErrBadRequest
	}
	etag, err := memberMetadataETag(c)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 32*1024+1))
	if err != nil || len(raw) > 32*1024 {
		return nil, apperrors.ErrBadRequest
	}
	var input service.ModelWeightEditInput
	if err := input.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	review, err := ctrl.service.SetReviewedModelWeights(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("model_id"), etag, input.Weights)
	return modelWeightEditResponse(c, review, err)
}
