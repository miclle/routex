package handler

import (
	"github.com/fox-gonic/fox"

	apperrors "github.com/miclle/routex/internal/routex/errors"
	"github.com/miclle/routex/internal/routex/service"
)

func (ctrl *Ctrl) ProviderQuality(c *fox.Context) (*service.ProviderQuality, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ProviderQualitySummary(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_id"))
}

func (ctrl *Ctrl) ProviderQualityPolicy(c *fox.Context) (*service.ProviderQualityPolicy, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	return ctrl.service.ProviderQualityPolicy(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_id"))
}

func (ctrl *Ctrl) WriteProviderQualityPolicy(c *fox.Context) (*service.ProviderQualityPolicy, error) {
	if c.Request.URL.RawQuery != "" {
		return nil, apperrors.ErrBadRequest
	}
	var input service.ProviderQualityPolicyInput
	if err := decodeStrictRequest(c, &input); err != nil {
		return nil, err
	}
	return ctrl.service.WriteProviderQualityPolicy(c.Request.Context(), currentAuthentication(c).User.ID, c.Param("provider_id"), input)
}
